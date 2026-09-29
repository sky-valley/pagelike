package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

func init() { server.Extend(Register) }

// Register installs the text/sessel QUERY handler on a server's public plane.
func Register(s *server.Server) {
	if s.Public.Queries == nil {
		return
	}
	s.Public.Queries["text/sessel"] = Handle
}

// Handle evaluates a `QUERY … Content-Type: text/sessel` request
// (R-PROTO-70..85):
//
//   - authorized as method QUERY on the request path, never through the
//     default-GET mode or read access alone (R-PROTO-74);
//   - a document target binds self to its root element; a directory target
//     (trailing slash) leaves self unbound, and referencing it is 416;
//   - the program sees only that document (sessel.Env.DocumentOnly, live
//     2026-09-29): no other document, no provenance, no Pagelove.GET;
//   - a single element result is served as text/html (206), anything else
//     as application/sessel+json (200);
//   - Range: entries=a-b slices a List result (206 + Content-Range:
//     entries a-b/total); on a non-List result it is 416;
//   - empty bodies, parse errors, uncaught TypeErrors and unresolved
//     variables are 400; throws and other runtime errors 500; budget
//     exhaustion 503 (R-SESSEL-344).
func Handle(ctx context.Context, s *site.Site, snap *site.Snapshot, op *engine.ReadOp, body []byte, w http.ResponseWriter) error {
	target := op.Target
	if target == "" {
		target = op.Path
	}
	isDir := strings.HasSuffix(target, "/")
	authPath := op.Path
	if isDir {
		authPath = target
	}
	req := authz.Request{Principal: op.Principal, Method: "QUERY", Path: authPath, Header: op.Header, Query: op.Query}
	if !snap.Policy.Decide(req, nil).Allowed {
		return engine.Denied(op.Principal, authPath)
	}
	src := strings.TrimSpace(string(body))
	if src == "" {
		return errdoc.New(http.StatusBadRequest, "EmptyQuery", "a Sessel QUERY needs a program in the request body")
	}
	prog, err := sessel.Compile(src)
	if err != nil {
		return errdoc.New(http.StatusBadRequest, "SesselParseError", "Sessel compilation error: %v", err) // wording as live
	}

	host := NewHost(s, snap)
	env := &sessel.Env{
		Host:    host,
		Budget:  sessel.NewBudget(),
		Request: sessel.NewRequest("QUERY", target, op.Header, op.Query, op.Params, body, authOf(op.Principal)),
		// The program sees only the target document (live 2026-09-29):
		// QUERY is authorized on this path alone, so it reads nothing else.
		DocumentOnly: true,
	}
	if isDir {
		docs, err := s.Store.List(ctx, target, false)
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return errdoc.New(http.StatusNotFound, "NotFound", "%s was not found", target)
		}
	} else if d := host.Document(op.Path); d != nil {
		env.Self, env.HasSelf = d.Element(), true
	} else if _, err := s.Store.Get(ctx, op.Path); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errdoc.New(http.StatusNotFound, "NotFound", "%s was not found", op.Path)
		}
		return err
	}

	v, err := prog.Eval(ctx, env)
	if err != nil {
		return evalError(err)
	}
	return respond(w, op, v)
}

// authOf builds request.auth for the principal (R-SESSEL-291).
func authOf(p *identity.Principal) *sessel.Dict {
	if p == nil || !p.Authenticated {
		return sessel.NewAuth("", nil, nil)
	}
	claims := map[string]any{}
	for k, v := range p.Claims {
		claims[k] = v
	}
	set := func(k string, v any) {
		if _, ok := claims[k]; !ok {
			claims[k] = v
		}
	}
	set("sub", p.Sub)
	if p.Email != "" {
		set("email", p.Email)
		set("email_verified", p.EmailVerified)
	}
	if p.Name != "" {
		set("name", p.Name)
	}
	user := p.Username
	if user == "" {
		user = p.Sub
	}
	return sessel.NewAuth(user, claims, p.Roles)
}

// evalError maps an evaluation failure to a status (R-SESSEL-344).
func evalError(err error) error {
	e, ok := sessel.AsError(err)
	if !ok {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("sessel query: %w", err)
	}
	msg := fmt.Sprintf("Sessel %s: %s", e.Type, e.Message)
	switch {
	case e.Reason == sessel.ReasonSelfUnbound:
		return errdoc.New(http.StatusRequestedRangeNotSatisfiable, "SelfUnbound", "%s (a QUERY on a directory has no current document)", msg)
	case e.Reason == sessel.ReasonBudget || e.Reason == sessel.ReasonTimeout:
		return errdoc.New(http.StatusServiceUnavailable, "BudgetExceeded", "%s", msg)
	case e.Thrown:
		// An explicit throw answers 400 on live PageLove (2026-09-28),
		// superseding the documented 500 (decisions.md).
		return errdoc.New(http.StatusBadRequest, "SesselRuntimeError", "Sessel evaluation error: %s", e.Message)
	case e.Reason == sessel.ReasonUnresolved || e.Type == sessel.TypeErrorType:
		return errdoc.New(http.StatusBadRequest, "SesselEvaluationError", "%s", msg)
	}
	return errdoc.New(http.StatusInternalServerError, "SesselRuntimeError", "%s", msg)
}

var entriesRE = regexp.MustCompile(`^entries\s*=\s*(\d*)\s*-\s*(\d*)\s*$`)

func respond(w http.ResponseWriter, op *engine.ReadOp, v sessel.Value) error {
	entries := op.Range.Unit == "entries"
	h := w.Header()
	if el, ok := v.(*sessel.Element); ok {
		if entries {
			return errdoc.New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "Range: entries applies only to a List result")
		}
		out := []byte(dom.OuterHTML(el.Node))
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(http.StatusPartialContent)
		_, err := w.Write(out)
		return err
	}
	status := http.StatusOK
	if entries {
		list, ok := v.(sessel.List)
		if !ok {
			return errdoc.New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "Range: entries applies only to a List result")
		}
		if m := entriesRE.FindStringSubmatch(strings.TrimSpace(op.Range.Raw)); m != nil {
			n := len(list)
			if m[1] == "" { // suffix ranges are not supported
				return unsatisfiable(n)
			}
			a, errA := strconv.Atoi(m[1])
			b := n - 1
			var errB error
			if m[2] != "" {
				b, errB = strconv.Atoi(m[2])
			}
			switch {
			case errA != nil || errB != nil:
				return unsatisfiable(n)
			case m[2] != "" && a > b:
				// an invalid range is ignored: the full result
			case a >= n:
				return unsatisfiable(n)
			default:
				if b > n-1 {
					b = n - 1
				}
				v = list[a : b+1]
				h.Set("Content-Range", fmt.Sprintf("entries %d-%d/%d;", a, b, n)) // trailing ';' as live
				status = http.StatusPartialContent
			}
		}
	}
	out, err := sessel.EncodeQueryJSON(v)
	if err != nil {
		return evalError(err)
	}
	h.Set("Content-Type", sessel.MediaType)
	h.Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(status)
	_, err = w.Write(out)
	return err
}

func unsatisfiable(total int) error {
	return errdoc.New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "the entries range is outside the %d-entry result", total).
		WithHeader("Content-Range", fmt.Sprintf("entries */%d", total))
}
