package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// Outcome of a case.
type Outcome string

const (
	Pass Outcome = "pass"
	Fail Outcome = "fail"
	Skip Outcome = "skip"
	// XFail is a disputed case (it asserts the losing side of a recorded
	// contradiction) that failed against the live target, as expected: the
	// live server confirmed the winning claim.
	XFail Outcome = "xfail"
)

// Case statuses the runner interprets.
const (
	// StatusDisputed marks the losing claim of a contradiction: skipped
	// locally, expected to fail live (XFail); passing live is a failure
	// ("XPASS"), because the recorded decision is then wrong.
	StatusDisputed = "disputed"
	// StatusLiveDivergence marks a case asserting what live PageLove does
	// where pagelike deliberately differs (docs/compat/decisions.md): it
	// runs only against the live target, to keep the divergence measured.
	StatusLiveDivergence = "live-divergence"
)

// Result is a case result.
type Result struct {
	Case     *Case
	Target   string
	Outcome  Outcome
	Reason   string
	Failures []string
	Obs      []Observation
	Duration time.Duration
}

// Observation is what a step saw (recorded for differential comparison).
type Observation struct {
	Step    string            `json:"step"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Events  []SSEEvent        `json:"events,omitempty"`
}

// Runner executes cases.
type Runner struct {
	Target  Target
	Verbose bool
	Out     io.Writer
	// Slow enables cases tagged requires: [slow].
	Slow bool
	// Timeout bounds one case (default 2 minutes; slow cases 20 minutes).
	Timeout time.Duration
	// AllowRoot runs root cases against the live target (opt-in: they touch
	// host-wide paths). Without it they are skipped live.
	AllowRoot bool
}

type run struct {
	env     *Env
	c       *Case
	mu      sync.Mutex
	vars    map[string]string
	streams map[string]*stream
	fails   []string
	obs     []Observation
}

// Run executes one case.
func (r *Runner) Run(ctx context.Context, c *Case) *Result {
	start := time.Now()
	res := &Result{Case: c, Target: r.Target.Name()}
	if c.Status == "skip" {
		res.Outcome, res.Reason = Skip, "status: skip"
		return res
	}
	live := r.Target.Name() == "live"
	if c.Status == StatusDisputed && !live {
		// A disputed case asserts the losing side of a recorded
		// contradiction; pagelike follows the winning claim, so the case
		// only runs live, to record which claim PageLove honours.
		res.Outcome, res.Reason = Skip, "status: disputed (losing claim; runs live only)"
		return res
	}
	if c.Status == StatusLiveDivergence && !live {
		// The case asserts PageLove's observed behaviour where pagelike
		// deliberately keeps a different one (docs/compat/decisions.md).
		res.Outcome, res.Reason = Skip, "status: live-divergence (asserts live PageLove; runs live only)"
		return res
	}
	if c.Root && live && !r.AllowRoot {
		res.Outcome, res.Reason = Skip, "root case (live runs are opt-in: --root)"
		return res
	}
	caps := r.Target.Caps()
	if c.Needs("slow") && !r.Slow {
		res.Outcome, res.Reason = Skip, "slow case (enable with --slow)"
		return res
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
		if c.Needs("slow") {
			timeout = 20 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, need := range c.Requires {
		switch need {
		case "multi-actor", "outbound-http", "restart", "webdav", "sse-control":
			if !caps[need] {
				res.Outcome, res.Reason = Skip, "target lacks "+need
				return res
			}
		}
	}
	if r.Target.Name() == "live" && !c.Live {
		res.Outcome, res.Reason = Skip, "case not marked live"
		return res
	}
	env, err := r.Target.Setup(ctx, c)
	if err != nil {
		res.Outcome, res.Reason = Fail, "setup: "+err.Error()
		res.Failures = []string{res.Reason}
		return res
	}
	defer r.Target.Teardown(ctx, env)
	rn := &run{env: env, c: c, vars: map[string]string{}, streams: map[string]*stream{}}
	for k, v := range c.Vars {
		rn.vars[k] = v
	}
	defer rn.closeStreams()
	for i := range c.Steps {
		if ctx.Err() != nil {
			rn.fail("case timed out after %v", timeout)
			break
		}
		rn.step(ctx, &c.Steps[i], i)
	}
	res.Obs = rn.obs
	res.Failures = rn.fails
	res.Duration = time.Since(start)
	if len(rn.fails) > 0 {
		res.Outcome = Fail
	} else {
		res.Outcome = Pass
	}
	if c.Status == StatusDisputed && live {
		// The expected outcome of a disputed case against live PageLove is
		// failure: the live server honours the winning claim.
		if res.Outcome == Fail {
			res.Outcome, res.Reason = XFail, "disputed (losing) claim failed live, as recorded"
		} else {
			res.Outcome = Fail
			res.Failures = append(res.Failures, "XPASS: the disputed (losing) claim held against live PageLove; revisit the decision")
		}
	}
	return res
}

func (rn *run) fail(format string, args ...any) {
	rn.mu.Lock()
	rn.fails = append(rn.fails, fmt.Sprintf(format, args...))
	rn.mu.Unlock()
}

// Expand substitutes ${P}, ${HOST} and environment variables.
func Expand(s string, env *Env) string {
	if !strings.Contains(s, "${") {
		return s
	}
	const esc = "\x00DOLLAR\x00"
	s = strings.ReplaceAll(s, "$${", esc)
	s = strings.ReplaceAll(s, "${P}", env.Prefix)
	s = strings.ReplaceAll(s, "${ORIGIN}", env.Origin)
	s = strings.ReplaceAll(s, "${HOST}", env.PublicHost)
	for k, v := range env.Vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return strings.ReplaceAll(s, esc, "${")
}

// expandTree expands every string in a decoded YAML value.
func (rn *run) expandTree(v any) any {
	switch x := v.(type) {
	case string:
		return rn.expand(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = rn.expandTree(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = rn.expandTree(e)
		}
		return out
	}
	return v
}

func (rn *run) expand(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	const esc = "\x00DOLLAR\x00"
	s = strings.ReplaceAll(s, "$${", esc)
	s = Expand(s, rn.env)
	rn.mu.Lock()
	for k, v := range rn.vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	rn.mu.Unlock()
	return strings.ReplaceAll(s, esc, "${")
}

func (rn *run) label(st *Step, i int) string {
	if st.Name != "" {
		return fmt.Sprintf("step %d (%s)", i+1, st.Name)
	}
	return fmt.Sprintf("step %d", i+1)
}

func (rn *run) step(ctx context.Context, st *Step, i int) {
	lbl := rn.label(st, i)
	if len(st.Extra) > 0 {
		keys := make([]string, 0, len(st.Extra))
		for k := range st.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rn.fail("%s: unsupported step keys %v", lbl, keys)
		return
	}
	switch {
	case st.SleepMS > 0 && st.Request == nil:
		select {
		case <-time.After(time.Duration(st.SleepMS) * time.Millisecond):
		case <-ctx.Done():
		}
	case st.Restart:
		if rn.env.Restart == nil {
			rn.fail("%s: target cannot restart", lbl)
			return
		}
		if err := rn.env.Restart(); err != nil {
			rn.fail("%s: restart: %v", lbl, err)
		}
	case st.PruneEvents:
		if rn.env.PruneEvents == nil {
			rn.fail("%s: target cannot prune stream events", lbl)
			return
		}
		if err := rn.env.PruneEvents(); err != nil {
			rn.fail("%s: prune_events: %v", lbl, err)
		}
	case len(st.Parallel) > 0:
		var wg sync.WaitGroup
		for j := range st.Parallel {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				rn.step(ctx, &st.Parallel[j], i*100+j)
			}(j)
		}
		wg.Wait()
	case st.SSEOpen != nil:
		rn.sseOpen(ctx, st.SSEOpen, lbl)
	case st.SSEExpect != nil:
		rn.sseExpect(st.SSEExpect, lbl, false)
	case st.SSEExpectNone != nil:
		rn.sseExpect(st.SSEExpectNone, lbl, true)
	case st.SSEClose != nil:
		if s := rn.streams[st.SSEClose.Name]; s != nil {
			s.close()
		}
	case st.Repeat != nil && st.Repeat.Step != nil:
		for k := 0; k < st.Repeat.Count && ctx.Err() == nil; k++ {
			rn.step(ctx, st.Repeat.Step, i)
		}
	case st.Request != nil:
		n := 1
		if st.Repeat != nil && st.Repeat.Count > 0 {
			n = st.Repeat.Count
		}
		for k := 0; k < n; k++ {
			rn.request(ctx, st, lbl)
		}
	case st.SSEComment != nil:
		rn.sseComment(st.SSEComment, lbl)
	case st.SSEClosed != nil:
		rn.sseClosed(st.SSEClosed, lbl)
	case st.SSEPause != nil:
		if s := rn.streams[st.SSEPause.Name]; s != nil {
			s.pause()
		}
	case st.SSEResume != nil:
		if s := rn.streams[st.SSEResume.Name]; s != nil {
			s.resume()
		}
	case st.SessionCtl != nil:
		rn.sessionControl(st.SessionCtl, lbl)
	case st.SinkConfig != nil:
		rn.sinkConfig(st.SinkConfig, lbl)
	case st.SinkExpect != nil:
		rn.sinkExpect(ctx, st.SinkExpect, lbl)
	case st.SinkExpectNone != nil:
		rn.sinkExpectNone(ctx, st.SinkExpectNone, lbl)
	case st.SinkExpectCount != nil:
		rn.sinkExpectCount(ctx, st.SinkExpectCount, lbl)
	}
}

func (rn *run) client(as string) (*Client, error) {
	if as == "" || as == "anonymous" {
		// Anonymous requests carry no cookies: every one is a fresh session.
		return newClient("anonymous"), nil
	}
	if as == "author" || as == "visitor" {
		// author: the authoring key; visitor: an anonymous browser that
		// keeps the cookies it is given for the whole case (one session).
		key := "__" + as
		rn.mu.Lock()
		defer rn.mu.Unlock()
		if c := rn.env.Actors[key]; c != nil {
			return c, nil
		}
		c := newClient(key)
		rn.env.Actors[key] = c
		return c, nil
	}
	if c := rn.env.Actors[as]; c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("unknown actor %q", as)
}

// response is a captured HTTP response.
type response struct {
	status int
	header http.Header
	body   []byte
}

func (rn *run) do(ctx context.Context, as, plane string, req *Request) (*response, error) {
	cl, err := rn.client(as)
	if err != nil {
		return nil, err
	}
	env := rn.env
	base, host := env.PublicURL, env.PublicHost
	// author defaults to the authoring plane; "plane: public" sends the
	// author's key to the public plane instead.
	toDav := plane == "dav" || (as == "author" && plane != "public")
	if toDav {
		base, host = env.DavURL, env.DavHost
	}
	p := rn.expand(req.Path)
	body := rn.expand(req.Body)
	if req.BodyFile != "" {
		b, err := os.ReadFile(req.BodyFile)
		if err != nil {
			return nil, err
		}
		body = string(b)
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	u := strings.TrimSuffix(base, "/") + escapeKeepQuery(p)
	hr, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Host = host
	cl.apply(hr)
	if as == "author" || (plane == "dav" && as == "") {
		// The authoring key belongs to the author; an anonymous request on
		// the dav plane goes without credentials.
		hr.Header.Set("Authorization", env.AuthorAuth)
	}
	for k, v := range req.Headers {
		hr.Header.Set(k, rn.expand(v))
	}
	if body == "" && (method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodDelete) {
		hr.Body = nil
		hr.ContentLength = 0
	}
	env.Limiter.Wait()
	resp, err := env.HTTP.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	cl.absorb(resp)
	return &response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

func escapeKeepQuery(p string) string {
	q := ""
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p, q = p[:i], p[i:]
	}
	return escapePath(p) + q
}

func (rn *run) request(ctx context.Context, st *Step, lbl string) {
	resp, err := rn.do(ctx, st.As, st.Plane, st.Request)
	if err != nil {
		rn.fail("%s: %v", lbl, err)
		return
	}
	rn.record(Observation{Step: lbl, Method: st.Request.Method, Path: rn.expand(st.Request.Path), Status: resp.status, Headers: normHeaders(resp.header), Body: string(resp.body)})
	if st.Expect != nil {
		for _, f := range rn.check(st.Expect, resp) {
			rn.fail("%s %s %s: %s", lbl, st.Request.Method, st.Request.Path, f)
		}
	}
	rn.capture(st.Capture, resp)
}

func (rn *run) record(o Observation) {
	rn.mu.Lock()
	rn.obs = append(rn.obs, o)
	rn.mu.Unlock()
}

// splitCapture separates a capture source from its filters: "src|unquote"
// strips surrounding double quotes (an ETag header compared with an SSE
// etag property), "src|regex:RE" keeps RE's first group (or whole match).
// A regex filter is last and may itself contain '|'. Sources starting with
// "regex:" (a body regexp) take no filters.
func splitCapture(src string) (string, []string) {
	if strings.HasPrefix(src, "regex:") {
		return src, nil
	}
	i := strings.IndexByte(src, '|')
	if i < 0 {
		return src, nil
	}
	base, rest := src[:i], src[i+1:]
	var fs []string
	for rest != "" {
		if strings.HasPrefix(rest, "regex:") {
			fs = append(fs, rest)
			break
		}
		f, r, _ := strings.Cut(rest, "|")
		fs = append(fs, f)
		rest = r
	}
	return base, fs
}

// applyFilters runs capture filters over a captured value.
func applyFilters(v string, fs []string) string {
	for _, f := range fs {
		switch {
		case f == "unquote":
			v = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(v, "W/"), `"`), `"`)
		case strings.HasPrefix(f, "regex:"):
			re, err := regexp.Compile(strings.TrimPrefix(f, "regex:"))
			if err != nil {
				return ""
			}
			m := re.FindStringSubmatch(v)
			switch {
			case m == nil:
				v = ""
			case len(m) > 1:
				v = m[1]
			default:
				v = m[0]
			}
		}
	}
	return v
}

func (rn *run) capture(spec map[string]string, resp *response) {
	for name, full := range spec {
		src, filters := splitCapture(full)
		v := ""
		switch {
		case strings.HasPrefix(src, "header."):
			v = resp.header.Get(strings.TrimPrefix(src, "header."))
		case src == "body":
			v = string(resp.body)
		case src == "status":
			v = fmt.Sprint(resp.status)
		case strings.HasPrefix(src, "sse."):
			parts := strings.SplitN(src, ".", 3)
			if len(parts) == 3 {
				if s := rn.streams[parts[1]]; s != nil && parts[2] == "connection" {
					v = s.connection()
				}
			}
		case strings.HasPrefix(src, "regex:"):
			if re, err := regexp.Compile(strings.TrimPrefix(src, "regex:")); err == nil {
				if m := re.FindSubmatch(resp.body); len(m) > 1 {
					v = string(m[1])
				}
			}
		}
		v = applyFilters(v, filters)
		rn.mu.Lock()
		rn.vars[name] = v
		rn.mu.Unlock()
	}
}

// normHeaders flattens headers, dropping volatile ones.
func normHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		lk := strings.ToLower(k)
		switch lk {
		case "date", "x-azure-ref", "x-cache", "set-cookie", "x-storage-consumed", "connection", "keep-alive":
			continue
		}
		if strings.HasPrefix(lk, "x-budget-") {
			continue
		}
		out[lk] = strings.Join(v, ", ")
	}
	return out
}

func (rn *run) check(e *Expect, resp *response) []string {
	var fs []string
	if len(e.Status) > 0 {
		ok := false
		for _, s := range e.Status {
			if s == resp.status {
				ok = true
			}
		}
		if !ok {
			fs = append(fs, fmt.Sprintf("status %d, want %v (body: %s)", resp.status, []int(e.Status), truncate(string(resp.body), 240)))
		}
	}
	for k, want := range e.Headers {
		got := resp.header.Get(k)
		if normHeaderValue(k, got) != normHeaderValue(k, rn.expand(want)) {
			fs = append(fs, fmt.Sprintf("header %s = %q, want %q", k, got, rn.expand(want)))
		}
	}
	for _, k := range e.HeadersPresent {
		if _, ok := resp.header[http.CanonicalHeaderKey(k)]; !ok {
			fs = append(fs, fmt.Sprintf("header %s missing", k))
		}
	}
	for _, k := range e.HeadersAbsent {
		if _, ok := resp.header[http.CanonicalHeaderKey(k)]; ok {
			fs = append(fs, fmt.Sprintf("header %s present (%q), want absent", k, resp.header.Get(k)))
		}
	}
	for k, pat := range e.HeaderMatches {
		re, err := regexp.Compile(rn.expand(pat))
		if err != nil {
			fs = append(fs, fmt.Sprintf("bad header regexp %q: %v", pat, err))
			continue
		}
		if !re.MatchString(resp.header.Get(k)) {
			fs = append(fs, fmt.Sprintf("header %s = %q, want match %q", k, resp.header.Get(k), pat))
		}
	}
	for k, sub := range e.HeaderContains {
		if !strings.Contains(strings.ToLower(resp.header.Get(k)), strings.ToLower(rn.expand(sub))) {
			fs = append(fs, fmt.Sprintf("header %s = %q, want to contain %q", k, resp.header.Get(k), sub))
		}
	}
	body := StripFooter(string(resp.body))
	if e.Body != nil && strings.TrimSpace(body) != strings.TrimSpace(rn.expand(*e.Body)) {
		fs = append(fs, fmt.Sprintf("body = %q, want %q", truncate(body, 400), rn.expand(*e.Body)))
	}
	if e.BodyHTML != nil && !HTMLEqual(body, rn.expand(*e.BodyHTML)) {
		fs = append(fs, fmt.Sprintf("body html = %q, want %q", truncate(body, 400), rn.expand(*e.BodyHTML)))
	}
	for _, sub := range e.BodyContains {
		if !strings.Contains(body, rn.expand(sub)) {
			fs = append(fs, fmt.Sprintf("body does not contain %q (body: %s)", rn.expand(sub), truncate(body, 400)))
		}
	}
	for _, sub := range e.BodyNotContains {
		if strings.Contains(body, rn.expand(sub)) {
			fs = append(fs, fmt.Sprintf("body contains %q", rn.expand(sub)))
		}
	}
	if e.BodyMatches != "" {
		if re, err := regexp.Compile(e.BodyMatches); err != nil || !re.MatchString(body) {
			fs = append(fs, fmt.Sprintf("body does not match %q", e.BodyMatches))
		}
	}
	if e.BodyEmpty && len(bytes.TrimSpace(resp.body)) > 0 {
		fs = append(fs, fmt.Sprintf("body not empty: %q", truncate(body, 200)))
	}
	if e.BodyJSON != nil {
		var got any
		if err := json.Unmarshal(resp.body, &got); err != nil {
			fs = append(fs, fmt.Sprintf("body is not JSON: %v", err))
		} else if want := rn.expandValues(normalizeYAML(e.BodyJSON)); !jsonEqual(want, got) {
			wb, _ := json.Marshal(want)
			fs = append(fs, fmt.Sprintf("json = %s, want %s", truncate(body, 400), wb))
		}
	}
	if e.ETagEquals != "" && resp.header.Get("ETag") != rn.expand(e.ETagEquals) {
		fs = append(fs, fmt.Sprintf("ETag %s, want equal to %s", resp.header.Get("ETag"), rn.expand(e.ETagEquals)))
	}
	if e.ETagDiffers != "" && resp.header.Get("ETag") == rn.expand(e.ETagDiffers) {
		fs = append(fs, fmt.Sprintf("ETag %s unchanged, want different", resp.header.Get("ETag")))
	}
	if e.MultipartParts != nil {
		n, err := countParts(resp)
		if err != nil {
			fs = append(fs, "multipart: "+err.Error())
		} else if n != *e.MultipartParts {
			fs = append(fs, fmt.Sprintf("multipart parts = %d, want %d", n, *e.MultipartParts))
		}
	}
	if e.Microdata != nil {
		want := rn.expandValues(normalizeYAML(e.Microdata))
		if m, ok := want.(map[string]any); ok && m["properties"] != nil {
			// {itemtype|type, properties}: one item, in the list form.
			want = []any{m}
		}
		if list, ok := want.([]any); ok {
			if f := checkMicrodataList(body, list); f != "" {
				fs = append(fs, f)
			}
		} else if f := checkMicrodata(body, want); f != "" {
			fs = append(fs, f)
		}
	}
	return fs
}

// footerRE matches the "Powered by Pagelove" block the free plan inserts
// immediately before </body> (live observation LO-1), with exactly the
// whitespace PageLove adds around it, so stripping restores the stored text.
var footerRE = regexp.MustCompile(`\n {16}<div style="\n {28}position: fixed;[^"]*">\s*Powered by\s*<a href="https://pagelove\.com/"[^>]*>Pagelove</a>\s*</div>\n {12}(</body>)`)

// StripFooter removes PageLove's plan branding from a served document so
// bodies compare with what was stored (harness/README.md, Normalization).
func StripFooter(s string) string {
	if !strings.Contains(s, "Powered by") {
		return s
	}
	return footerRE.ReplaceAllString(s, "$1")
}

func normHeaderValue(k, v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(k) {
	case "allow", "vary", "accept-ranges", "accept-query":
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.ToUpper(strings.TrimSpace(parts[i]))
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	case "content-type":
		mt, params, err := mime.ParseMediaType(v)
		if err == nil {
			delete(params, "boundary")
			if len(params) == 0 {
				return mt
			}
			return mime.FormatMediaType(mt, params)
		}
	}
	return v
}

func countParts(resp *response) (int, error) {
	mt, params, err := mime.ParseMediaType(resp.header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") {
		return 0, fmt.Errorf("content-type %q is not multipart", resp.header.Get("Content-Type"))
	}
	mr := multipart.NewReader(bytes.NewReader(resp.body), params["boundary"])
	n := 0
	for {
		_, err := mr.NextPart()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

// HTMLEqual compares two HTML fragments ignoring inter-element whitespace
// and attribute order.
func HTMLEqual(a, b string) bool { return canonHTML(a) == canonHTML(b) }

func canonHTML(s string) string {
	nodes, err := dom.ParseBodyFragment(s)
	if err != nil {
		return strings.TrimSpace(s)
	}
	if strings.Contains(strings.ToLower(s), "<html") {
		if doc, err := dom.Parse([]byte(s)); err == nil {
			nodes = []*html.Node{doc}
		}
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			if t := strings.TrimSpace(n.Data); t != "" {
				b.WriteString(strings.Join(strings.Fields(t), " "))
			}
		case html.ElementNode:
			attrs := append([]html.Attribute(nil), n.Attr...)
			sort.Slice(attrs, func(i, j int) bool { return attrs[i].Key < attrs[j].Key })
			b.WriteString("<" + n.Data)
			for _, a := range attrs {
				b.WriteString(" " + a.Key + "=" + fmt.Sprintf("%q", a.Val))
			}
			b.WriteString(">")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			b.WriteString("</" + n.Data + ">")
		case html.DocumentNode:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}

// expandValues substitutes ${…} in every string of an expectation value.
func (rn *run) expandValues(v any) any {
	switch x := v.(type) {
	case string:
		return rn.expand(x)
	case map[string]any:
		out := map[string]any{}
		for k, vv := range x {
			out[k] = rn.expandValues(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = rn.expandValues(x[i])
		}
		return out
	}
	return v
}

func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, vv := range x {
			out[k] = normalizeYAML(vv)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, vv := range x {
			out[fmt.Sprint(k)] = normalizeYAML(vv)
		}
		return out
	case []any:
		for i := range x {
			x[i] = normalizeYAML(x[i])
		}
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return v
}

func jsonEqual(want, got any) bool {
	wb, _ := json.Marshal(want)
	var w any
	json.Unmarshal(wb, &w)
	return reflect.DeepEqual(w, got)
}

// checkMicrodata asserts expected properties on the first item in body.
// want is a map of property → value (string, list, or nil for absent).
func checkMicrodata(body string, want any) string {
	m, ok := want.(map[string]any)
	if !ok {
		return "microdata expectation must be a map"
	}
	doc, err := dom.Parse([]byte(body))
	if err != nil {
		return "microdata: cannot parse body"
	}
	items := microdata.AllItems(doc)
	if len(items) == 0 {
		return "microdata: no item in body"
	}
	it := items[0]
	if t, ok := m["@type"]; ok {
		if fmt.Sprint(t) != it.Type() {
			return fmt.Sprintf("microdata @type %q, want %q", it.Type(), t)
		}
	}
	for k, v := range m {
		if k == "@type" {
			continue
		}
		got := it.All(k)
		switch x := v.(type) {
		case nil:
			if len(got) > 0 {
				return fmt.Sprintf("microdata %s present (%q), want absent", k, got)
			}
		case []any:
			if len(got) != len(x) {
				return fmt.Sprintf("microdata %s = %q, want %v", k, got, x)
			}
			for i := range x {
				if strings.TrimSpace(got[i]) != fmt.Sprint(x[i]) {
					return fmt.Sprintf("microdata %s = %q, want %v", k, got, x)
				}
			}
		default:
			if len(got) == 0 || strings.TrimSpace(got[0]) != strings.TrimSpace(fmt.Sprint(x)) {
				return fmt.Sprintf("microdata %s = %q, want %q", k, got, fmt.Sprint(x))
			}
		}
	}
	return ""
}

// checkMicrodataList asserts that each expected {type, properties} item
// matches (as a subset) some item in body.
func checkMicrodataList(body string, want []any) string {
	doc, err := dom.Parse([]byte(body))
	if err != nil {
		return "microdata: cannot parse body"
	}
	items := microdata.AllItems(doc)
	for _, w := range want {
		wm, _ := w.(map[string]any)
		if wm == nil {
			return "microdata list entries must be maps"
		}
		typ, _ := wm["type"].(string)
		if it, ok := wm["itemtype"].(string); ok && typ == "" {
			typ = it
		}
		props, _ := wm["properties"].(map[string]any)
		found := false
		for _, it := range items {
			if typ != "" && !it.HasType(typ) {
				continue
			}
			ok := true
			for k, v := range props {
				got := it.All(k)
				switch x := v.(type) {
				case nil:
					ok = ok && len(got) == 0
				case []any:
					if len(got) != len(x) {
						ok = false
						break
					}
					for i := range x {
						if strings.TrimSpace(got[i]) != fmt.Sprint(x[i]) {
							ok = false
						}
					}
				default:
					ok = ok && len(got) > 0 && strings.TrimSpace(got[0]) == strings.TrimSpace(fmt.Sprint(x))
				}
			}
			if ok {
				found = true
				break
			}
		}
		if !found {
			var have []string
			for _, it := range items {
				have = append(have, it.Type())
			}
			return fmt.Sprintf("microdata: no item matches %v (items: %v)", wm, have)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------- SSE

// SSEEvent is one received event.
type SSEEvent struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event"`
	Data  string `json:"data"`
}

type stream struct {
	mu       sync.Mutex
	events   []SSEEvent
	comments []string
	cpos     int
	pos      int
	gate     chan struct{} // nil = running; non-nil = paused until closed
	status   int
	header   http.Header
	cancel   context.CancelFunc
	notify   chan struct{}
	done     chan struct{}
	// localAddr is the client end of the stream's connection; stall, when
	// the target provides it, stops or restarts the server writing to it.
	localAddr string
	stall     func(localAddr string, paused bool)
}

func (s *stream) pause() {
	s.mu.Lock()
	if s.gate == nil {
		s.gate = make(chan struct{})
	}
	s.mu.Unlock()
	if s.stall != nil && s.localAddr != "" {
		s.stall(s.localAddr, true)
	}
}

func (s *stream) resume() {
	if s.stall != nil && s.localAddr != "" {
		s.stall(s.localAddr, false)
	}
	s.mu.Lock()
	if s.gate != nil {
		close(s.gate)
		s.gate = nil
	}
	s.mu.Unlock()
}

func (s *stream) wait() {
	s.mu.Lock()
	g := s.gate
	s.mu.Unlock()
	if g != nil {
		<-g
	}
}

func (s *stream) close() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *stream) connection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.Event == "pagelove-connection" {
			return e.Data
		}
	}
	return ""
}

func (rn *run) closeStreams() {
	for _, s := range rn.streams {
		s.close()
	}
}

func (rn *run) sseOpen(ctx context.Context, o *SSEOpen, lbl string) {
	cl, err := rn.client(o.As)
	if err != nil {
		rn.fail("%s: %v", lbl, err)
		return
	}
	sctx, cancel := context.WithCancel(context.Background())
	env := rn.env
	hr, _ := http.NewRequestWithContext(sctx, http.MethodGet, strings.TrimSuffix(env.PublicURL, "/")+escapeKeepQuery(rn.expand(o.Path)), nil)
	hr.Host = env.PublicHost
	cl.apply(hr)
	hr.Header.Set("Accept", "text/event-stream")
	if o.LastEventID != "" {
		hr.Header.Set("Last-Event-ID", rn.expand(o.LastEventID))
	}
	for k, v := range o.Headers {
		hr.Header.Set(k, rn.expand(v))
	}
	s := &stream{cancel: cancel, notify: make(chan struct{}, 1), done: make(chan struct{}), stall: env.StallStream}
	rn.streams[o.Name] = s
	// Remember which connection carries the stream, so pausing it can also
	// stall the server's writes (local target).
	hr = hr.WithContext(httptrace.WithClientTrace(sctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { s.localAddr = info.Conn.LocalAddr().String() },
	}))
	streamClient := &http.Client{Transport: env.HTTP.Transport, CheckRedirect: noRedirect}
	env.Limiter.Wait()
	resp, err := streamClient.Do(hr)
	if err != nil {
		rn.fail("%s: sse open: %v", lbl, err)
		close(s.done)
		return
	}
	cl.absorb(resp)
	s.status, s.header = resp.StatusCode, resp.Header
	var body []byte
	if resp.StatusCode != http.StatusOK {
		body, _ = io.ReadAll(resp.Body)
	}
	rn.record(Observation{Step: lbl + " (sse open " + o.Name + ")", Method: http.MethodGet, Path: rn.expand(o.Path), Status: resp.StatusCode, Headers: normHeaders(resp.Header), Body: string(body)})
	if o.Expect != nil {
		for _, f := range rn.check(o.Expect, &response{status: resp.StatusCode, header: resp.Header, body: body}) {
			rn.fail("%s sse open %s: %s", lbl, o.Path, f)
		}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		close(s.done)
		return
	}
	go func() {
		defer close(s.done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(gatedReader{resp.Body, s})
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		var cur SSEEvent
		var data []string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if len(data) > 0 || cur.Event != "" {
					cur.Data = strings.Join(data, "\n")
					if cur.Event == "" {
						cur.Event = "message"
					}
					s.mu.Lock()
					s.events = append(s.events, cur)
					s.mu.Unlock()
					select {
					case s.notify <- struct{}{}:
					default:
					}
				}
				cur, data = SSEEvent{}, nil
			case strings.HasPrefix(line, ":"):
				s.mu.Lock()
				s.comments = append(s.comments, strings.TrimPrefix(strings.TrimPrefix(line, ":"), " "))
				s.mu.Unlock()
				select {
				case s.notify <- struct{}{}:
				default:
				}
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case strings.HasPrefix(line, "event:"):
				cur.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "id:"):
				cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			}
		}
	}()
}

func (rn *run) sseExpect(x *SSEExpect, lbl string, none bool) {
	s := rn.streams[x.Name]
	if s == nil {
		rn.fail("%s: no stream %q", lbl, x.Name)
		return
	}
	wait := time.Duration(x.WithinMS) * time.Millisecond
	if none {
		wait = time.Duration(x.ForMS) * time.Millisecond
		if wait == 0 {
			wait = time.Duration(x.WithinMS) * time.Millisecond
		}
		if wait == 0 {
			wait = 1500 * time.Millisecond
		}
	}
	if wait == 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		s.mu.Lock()
		for s.pos < len(s.events) {
			ev := s.events[s.pos]
			s.pos++
			if x.Next && !none && !rn.eventMatches(x, ev) {
				s.mu.Unlock()
				rn.fail("%s: next event on %s is %s %q, which does not match", lbl, x.Name, ev.Event, truncate(ev.Data, 200))
				return
			}
			if rn.eventMatches(x, ev) {
				s.mu.Unlock()
				rn.record(Observation{Step: lbl, Events: []SSEEvent{ev}})
				if none {
					rn.fail("%s: unexpected %s event on %s: %s", lbl, ev.Event, x.Name, truncate(ev.Data, 300))
					return
				}
				for name, full := range x.Capture {
					src, filters := splitCapture(full)
					v := ev.Data
					switch src {
					case "id":
						v = ev.ID
					case "event":
						v = ev.Event
					}
					v = applyFilters(v, filters)
					rn.mu.Lock()
					rn.vars[name] = v
					rn.mu.Unlock()
				}
				return
			}
		}
		s.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			if !none {
				rn.failNoMatch(s, x, lbl, fmt.Sprintf("within %v", wait))
			}
			return
		}
		select {
		case <-s.notify:
		case <-s.done:
			// stream ended: drain what we have once more, then give up
			s.mu.Lock()
			more := s.pos < len(s.events)
			s.mu.Unlock()
			if !more {
				if !none {
					rn.failNoMatch(s, x, lbl, "before the stream closed")
				}
				return
			}
		case <-time.After(left):
		}
	}
}

// failNoMatch reports a missing event and records every event the stream
// carried (matched ones included) as an observation, so a live run shows
// what the server sent instead.
func (rn *run) failNoMatch(s *stream, x *SSEExpect, lbl, when string) {
	s.mu.Lock()
	seen := append([]SSEEvent(nil), s.events...)
	s.mu.Unlock()
	rn.record(Observation{Step: lbl + " (all events on " + x.Name + ")", Events: seen})
	var kinds []string
	for _, ev := range seen {
		k := ev.Event
		if ev.ID != "" {
			k += "#" + ev.ID
		}
		kinds = append(kinds, k)
	}
	rn.fail("%s: no matching %q event on %s %s (%d events seen: %s)", lbl, x.Event, x.Name, when, len(seen), strings.Join(kinds, ", "))
}

func (rn *run) eventMatches(x *SSEExpect, ev SSEEvent) bool {
	if x.Event != "" && x.Event != ev.Event {
		return false
	}
	if x.IDAbsent && ev.ID != "" {
		return false
	}
	if x.IDMatches != "" {
		re, err := regexp.Compile(rn.expand(x.IDMatches))
		if err != nil || !re.MatchString(ev.ID) {
			return false
		}
	}
	if x.DataMatches != "" {
		re, err := regexp.Compile(rn.expand(x.DataMatches))
		if err != nil || !re.MatchString(ev.Data) {
			return false
		}
	}
	for _, sub := range x.DataContains {
		if !strings.Contains(ev.Data, rn.expand(sub)) {
			return false
		}
	}
	for _, sub := range x.DataNotContains {
		if strings.Contains(ev.Data, rn.expand(sub)) {
			return false
		}
	}
	if x.HasID != nil && (*x.HasID) != (ev.ID != "") {
		return false
	}
	if len(x.DataMicrodata) > 0 {
		m := map[string]any{}
		for k, v := range x.DataMicrodata {
			if s, ok := v.(string); ok {
				m[k] = rn.expand(s)
			} else {
				m[k] = v
			}
		}
		if checkMicrodata(ev.Data, normalizeYAML(m)) != "" {
			return false
		}
	}
	return true
}

type gatedReader struct {
	r io.Reader
	s *stream
}

func (g gatedReader) Read(p []byte) (int, error) {
	g.s.wait()
	return g.r.Read(p)
}

func (rn *run) sseComment(x *SSEComment, lbl string) {
	s := rn.streams[x.Name]
	if s == nil {
		rn.fail("%s: no stream %q", lbl, x.Name)
		return
	}
	wait := time.Duration(x.WithinMS) * time.Millisecond
	if wait == 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		s.mu.Lock()
		for s.cpos < len(s.comments) {
			c := s.comments[s.cpos]
			s.cpos++
			if c == x.Text {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			rn.fail("%s: no comment %q on %s within %v", lbl, x.Text, x.Name, wait)
			return
		}
		select {
		case <-s.notify:
		case <-s.done:
			rn.fail("%s: stream %s closed before comment %q", lbl, x.Name, x.Text)
			return
		case <-time.After(left):
		}
	}
}

func (rn *run) sseClosed(x *SSEClose, lbl string) {
	s := rn.streams[x.Name]
	if s == nil {
		rn.fail("%s: no stream %q", lbl, x.Name)
		return
	}
	s.resume()
	wait := time.Duration(x.WithinMS) * time.Millisecond
	if wait == 0 {
		wait = 5 * time.Second
	}
	select {
	case <-s.done:
	case <-time.After(wait):
		rn.fail("%s: stream %s still open after %v", lbl, x.Name, wait)
	}
}

func (rn *run) sessionControl(x *SessionCtl, lbl string) {
	if rn.env.SessionControl == nil {
		rn.fail("%s: target has no session control", lbl)
		return
	}
	cl := rn.env.Actors[x.Actor]
	if cl == nil {
		rn.fail("%s: unknown actor %q", lbl, x.Actor)
		return
	}
	if err := rn.env.SessionControl(cl, x.Action); err != nil {
		rn.fail("%s: session_control: %v", lbl, err)
	}
}

// ---------------------------------------------------------------- output

// Summary aggregates results.
type Summary struct {
	Pass, Fail, Skip, XFail int
}

// Report writes a human-readable report.
func Report(w io.Writer, results []*Result, verbose bool) Summary {
	var s Summary
	for _, r := range results {
		switch r.Outcome {
		case Pass:
			s.Pass++
			if verbose {
				fmt.Fprintf(w, "PASS %s\n", r.Case.ID)
			}
		case Skip:
			s.Skip++
			if verbose {
				fmt.Fprintf(w, "SKIP %s (%s)\n", r.Case.ID, r.Reason)
			}
		case XFail:
			s.XFail++
			fmt.Fprintf(w, "XFAIL %s (%s)\n", r.Case.ID, r.Reason)
			if verbose {
				for _, f := range r.Failures {
					fmt.Fprintf(w, "     - %s\n", f)
				}
			}
		case Fail:
			s.Fail++
			fmt.Fprintf(w, "FAIL %s [%s]\n", r.Case.ID, r.Case.File)
			for _, f := range r.Failures {
				fmt.Fprintf(w, "     - %s\n", f)
			}
		}
	}
	if s.XFail > 0 {
		fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped, %d expected failures (disputed)\n", s.Pass, s.Fail, s.Skip, s.XFail)
	} else {
		fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped\n", s.Pass, s.Fail, s.Skip)
	}
	return s
}

// SaveObservations writes observations for later differential comparison.
func SaveObservations(dir string, results []*Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, r := range results {
		if r.Outcome == Skip {
			continue
		}
		b, _ := json.MarshalIndent(map[string]any{"case": r.Case.ID, "target": r.Target, "outcome": r.Outcome, "failures": r.Failures, "observations": r.Obs}, "", "  ")
		if err := os.WriteFile(dir+"/"+ObservationName(r.Case.ID)+".json", b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ObservationName is the file name (without ".json") of a case's recorded
// observations: the sanitized id cut to 40 characters. A ".live" sibling
// keeps its "-live" suffix even when the cut would drop it, so that it does
// not overwrite the observations of the case it measures.
func ObservationName(id string) string {
	name := sanitize(id)
	if base, ok := strings.CutSuffix(id, ".live"); ok && !strings.HasSuffix(name, "-live") {
		b := sanitize(base)
		if len(b) > 35 {
			b = strings.TrimRight(b[:35], "-")
		}
		name = b + "-live"
	}
	return name
}
