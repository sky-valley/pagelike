package harness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sink is the local capture endpoint behind ${SINK} (docs/spec/reacting.md
// §14): an HTTP server that records every request it receives, per path
// relative to the case's base URL, and answers with configurable statuses.
// Cases drive it with the sink_config, sink_expect, sink_expect_none and
// sink_expect_count steps.
type Sink struct {
	srv  *httptest.Server
	base string // path prefix unique to the case run, e.g. /c-3f9a1b

	mu      sync.Mutex
	reqs    map[string][]*SinkRequest // by relative path, in arrival order
	used    map[string]int            // consumed requests per path
	cfg     map[string]*SinkConfig
	hits    map[string]int
	changed chan struct{} // closed and replaced on every arrival
}

// SinkRequest is one captured request.
type SinkRequest struct {
	Method string
	Path   string // relative to the sink base
	Query  url.Values
	Header http.Header
	Body   []byte
	At     time.Time
}

// SinkConfig sets the statuses returned on a path (step sink_config).
type SinkConfig struct {
	Path      string `yaml:"path"`
	Responses []int  `yaml:"responses"` // successive statuses; the last repeats (default [200])
	DelayMS   int    `yaml:"delay_ms"`
}

// SinkExpect waits for and checks the next unconsumed request on a path.
type SinkExpect struct {
	Path            string              `yaml:"path"`
	WithinMS        int                 `yaml:"within_ms"`
	Method          string              `yaml:"method"`
	Query           map[string]string   `yaml:"query"`
	Headers         map[string]string   `yaml:"headers"`
	HeaderValues    map[string][]string `yaml:"header_values"`
	HeaderMatches   map[string]string   `yaml:"header_matches"`
	HeadersAbsent   []string            `yaml:"headers_absent"`
	Body            *string             `yaml:"body"`
	BodyContains    StrList             `yaml:"body_contains"`
	BodyNotContains StrList             `yaml:"body_not_contains"`
	BodyMatches     string              `yaml:"body_matches"`
	BodyJSON        any                 `yaml:"body_json"`
	Microdata       any                 `yaml:"microdata"`
	Capture         map[string]string   `yaml:"capture"`
}

// SinkExpectNone asserts that no further request arrives on a path.
type SinkExpectNone struct {
	Path  string `yaml:"path"`
	ForMS int    `yaml:"for_ms"`
}

// SinkExpectCount waits until at least Count requests arrived on a path
// (consumed ones included), optionally bounding the gaps between arrivals.
type SinkExpectCount struct {
	Path      string `yaml:"path"`
	Count     int    `yaml:"count"`
	WithinMS  int    `yaml:"within_ms"`
	GapsMinMS []int  `yaml:"gaps_min_ms"`
	GapsMaxMS []int  `yaml:"gaps_max_ms"`
}

// NewSink starts a capture server on the loopback interface.
func NewSink() *Sink {
	s := &Sink{base: "/c-" + randHex(4), reqs: map[string][]*SinkRequest{}, used: map[string]int{},
		cfg: map[string]*SinkConfig{}, hits: map[string]int{}, changed: make(chan struct{})}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL is the base URL cases see as ${SINK}.
func (s *Sink) URL() string { return s.srv.URL + s.base }

// HostPort is the listener address (for outbound allowlists).
func (s *Sink) HostPort() string { return strings.TrimPrefix(s.srv.URL, "http://") }

// Close stops the server.
func (s *Sink) Close() {
	s.srv.CloseClientConnections()
	s.srv.Close()
}

func (s *Sink) serve(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, s.base)
	if rel == r.URL.Path {
		http.NotFound(w, r) // another case's (or a stray) request
		return
	}
	if rel == "" {
		rel = "/"
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	req := &SinkRequest{Method: r.Method, Path: rel, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body, At: time.Now()}
	s.mu.Lock()
	s.reqs[rel] = append(s.reqs[rel], req)
	n := s.hits[rel]
	s.hits[rel]++
	status, delay := http.StatusOK, 0
	if c := s.cfg[rel]; c != nil {
		delay = c.DelayMS
		if len(c.Responses) > 0 {
			status = c.Responses[min(n, len(c.Responses)-1)]
		}
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	w.WriteHeader(status)
}

// configure installs a response configuration for a path.
func (s *Sink) configure(c *SinkConfig) {
	s.mu.Lock()
	cc := *c
	s.cfg[c.Path] = &cc
	s.mu.Unlock()
}

// wait blocks until cond (called under the lock) holds or the deadline
// passes; it reports whether cond held.
func (s *Sink) wait(ctx context.Context, d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		s.mu.Lock()
		ok := cond()
		ch := s.changed
		s.mu.Unlock()
		if ok {
			return true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		t := time.NewTimer(left)
		select {
		case <-ch:
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return false
		}
		t.Stop()
	}
}

// ---------------------------------------------------------------- steps

func (rn *run) sinkOf(lbl string) *Sink {
	if rn.env.Sink == nil {
		rn.fail("%s: the target provides no outbound capture sink", lbl)
	}
	return rn.env.Sink
}

func (rn *run) sinkConfig(c *SinkConfig, lbl string) {
	s := rn.sinkOf(lbl)
	if s == nil {
		return
	}
	cc := *c
	cc.Path = rn.expand(c.Path)
	s.configure(&cc)
}

func (rn *run) sinkExpect(ctx context.Context, x *SinkExpect, lbl string) {
	s := rn.sinkOf(lbl)
	if s == nil {
		return
	}
	p := rn.expand(x.Path)
	wait := time.Duration(x.WithinMS) * time.Millisecond
	if wait == 0 {
		wait = 5 * time.Second
	}
	var got *SinkRequest
	ok := s.wait(ctx, wait, func() bool {
		if s.used[p] < len(s.reqs[p]) {
			got = s.reqs[p][s.used[p]]
			s.used[p]++
			return true
		}
		return false
	})
	if !ok {
		s.mu.Lock()
		var seen []string
		for k, v := range s.reqs {
			seen = append(seen, fmt.Sprintf("%s×%d", k, len(v)))
		}
		s.mu.Unlock()
		sort.Strings(seen)
		rn.fail("%s: no request on sink path %s within %v (sink saw: %v)", lbl, p, wait, seen)
		return
	}
	resp := &response{status: http.StatusOK, header: got.Header, body: got.Body}
	rn.record(Observation{Step: lbl, Method: got.Method, Path: "${SINK}" + got.Path, Headers: normHeaders(got.Header), Body: string(got.Body)})
	if x.Method != "" && !strings.EqualFold(got.Method, rn.expand(x.Method)) {
		rn.fail("%s: sink %s: method %s, want %s", lbl, p, got.Method, x.Method)
	}
	for k, v := range x.Query {
		if g := got.Query.Get(k); g != rn.expand(v) {
			rn.fail("%s: sink %s: query %s = %q, want %q", lbl, p, k, g, rn.expand(v))
		}
	}
	for k, want := range x.HeaderValues {
		have := got.Header.Values(k)
		exp := make([]string, len(want))
		for i := range want {
			exp[i] = rn.expand(want[i])
		}
		if strings.Join(have, "\x00") != strings.Join(exp, "\x00") {
			rn.fail("%s: sink %s: header %s values %q, want %q", lbl, p, k, have, exp)
		}
	}
	e := &Expect{Headers: x.Headers, HeaderMatches: x.HeaderMatches, HeadersAbsent: x.HeadersAbsent, Body: x.Body,
		BodyContains: x.BodyContains, BodyNotContains: x.BodyNotContains, BodyMatches: x.BodyMatches, BodyJSON: x.BodyJSON, Microdata: x.Microdata}
	for _, f := range rn.check(e, resp) {
		rn.fail("%s: sink %s: %s", lbl, p, f)
	}
	rn.capture(x.Capture, resp)
}

func (rn *run) sinkExpectNone(ctx context.Context, x *SinkExpectNone, lbl string) {
	s := rn.sinkOf(lbl)
	if s == nil {
		return
	}
	p := rn.expand(x.Path)
	wait := time.Duration(x.ForMS) * time.Millisecond
	if wait == 0 {
		wait = 1500 * time.Millisecond
	}
	var extra *SinkRequest
	if s.wait(ctx, wait, func() bool {
		if s.used[p] < len(s.reqs[p]) {
			extra = s.reqs[p][s.used[p]]
			return true
		}
		return false
	}) {
		rn.fail("%s: unexpected %s request on sink path %s: %s", lbl, extra.Method, p, truncate(string(extra.Body), 300))
	}
}

func (rn *run) sinkExpectCount(ctx context.Context, x *SinkExpectCount, lbl string) {
	s := rn.sinkOf(lbl)
	if s == nil {
		return
	}
	p := rn.expand(x.Path)
	wait := time.Duration(x.WithinMS) * time.Millisecond
	if wait == 0 {
		wait = 5 * time.Second
	}
	var arrivals []time.Time
	ok := s.wait(ctx, wait, func() bool {
		if len(s.reqs[p]) < x.Count {
			return false
		}
		arrivals = arrivals[:0]
		for _, r := range s.reqs[p] {
			arrivals = append(arrivals, r.At)
		}
		s.used[p] = len(s.reqs[p])
		return true
	})
	if !ok {
		s.mu.Lock()
		n := len(s.reqs[p])
		s.mu.Unlock()
		rn.fail("%s: %d requests on sink path %s within %v, want at least %d", lbl, n, p, wait, x.Count)
		return
	}
	for i := 1; i < len(arrivals); i++ {
		gap := int(arrivals[i].Sub(arrivals[i-1]) / time.Millisecond)
		if i-1 < len(x.GapsMinMS) && gap < x.GapsMinMS[i-1] {
			rn.fail("%s: sink %s: gap %d between arrivals %d and %d is %d ms, want >= %d", lbl, p, i, i, i+1, gap, x.GapsMinMS[i-1])
		}
		if i-1 < len(x.GapsMaxMS) && gap > x.GapsMaxMS[i-1] {
			rn.fail("%s: sink %s: gap %d between arrivals %d and %d is %d ms, want <= %d", lbl, p, i, i, i+1, gap, x.GapsMaxMS[i-1])
		}
	}
}
