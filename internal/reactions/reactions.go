// Package reactions implements PageLove's automation primitives
// (docs/spec/reacting.md): Triggers, which run before core request
// processing; Processors, which run over its response; outbound HttpRequest
// actions, queued durably and sent after the response; TransitionConstraint
// state machines that validate writes; and TransitionHandler notifications.
//
// Wiring (docs/architecture.md):
//
//   - httpapi.Public.Intercept: the request pipeline of R-REACT-8
//     (authorization pre-check → trigger phase → core → processor phase →
//     response → dispatch). Triggers and processors run for every public
//     plane method, never on the WebDAV plane.
//   - engine.Hooks.Validate, phase engine.PhaseTransition (after every
//     schema check, R-REACT-71): TransitionConstraint validation (422
//     ConstraintViolation, 412 on races) and the platform-schema check of
//     TransitionConstraint items written through the serving path.
//   - engine.Hooks.AfterWrite: durable enqueue, inside the write's
//     transaction, of the requests queued by triggers and of transition
//     handler deliveries.
//
// Side-effect writes (Pagelove.PUT / Pagelove.DELETE) go through the full
// serving-path write pipeline (authorization, validation, transitions,
// events) in their own transaction, committed when the call returns
// (R-REACT-36: they must survive a later throw, so they cannot join the main
// write's transaction; the trigger phase runs before the site write mutex).
//
// Server JavaScript is reached through the JSRunner interface (js.go), with
// the slot of each call (JSCall.Slot: gates never honour a thrown
// HTTPResponse, actions do); internal/jsglue installs the runtime. Until a
// runtime is installed with SetJSRunner, JavaScript gates, actions and
// dynamic values fail clearly with 501 and a BindingFailure; a budget
// exhaustion answers 503 (R-JS-57). Pagelove.PUT writes a keyed instance's
// id through SetKeyIDFunc (schema.SetKeyID) when installed.
package reactions

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/httpapi"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

func init() { server.Extend(Register) }

// Limits (R-REACT-86, pagelike decisions).
const (
	MaxQueued       = 256     // outbound requests queued by one request
	MaxOutboundBody = 1 << 20 // bytes
	MaxSideEffects  = 64      // Pagelove.PUT/DELETE writes per request
)

// Reactions is one server's reaction machinery.
type Reactions struct {
	srv    *server.Server
	engine *engine.Engine
	log    *slog.Logger
	out    *outbox
}

// Register installs reactions on a server: the public-plane interceptor,
// the engine hooks and the outbox delivery workers.
func Register(s *server.Server) {
	x := &Reactions{srv: s, engine: s.Engine, log: s.Log}
	if x.log == nil {
		x.log = slog.Default()
	}
	x.out = newOutbox(x)
	s.Public.Intercept = x
	s.Engine.Hooks.AddValidate(engine.PhaseTransition, x.validate)
	s.Engine.Hooks.AfterWrite = append(s.Engine.Hooks.AfterWrite, x.afterWrite)
	go x.out.resumeAll()
}

// Request phases.
const (
	phaseArrival = iota
	phaseTriggers
	phaseCore
	phaseProcessors
	phaseDone
)

// reqState is the reactions state of one public-plane request. It travels
// in the request context so the engine hooks can find it.
type reqState struct {
	x         *Reactions
	site      *site.Site
	call      *httpapi.Call
	r         *http.Request
	method    string // as received
	path      string // normalized request path (Context.request.path)
	docPath   string // the document a read or selector write addresses
	target    string // the main write's engine path
	origin    string // scheme://host[:port] of the request
	principal *identity.Principal

	body        []byte // current body (after transformations)
	transformed bool

	cctx *sessel.Dict // the request's Context (R-REACT-27)
	req  *sessel.Dict // Context.request

	prior       *sessel.Document // target document as stored at arrival
	hasSelector bool

	mu        sync.Mutex
	phase     int
	sideDepth int              // > 0 while a side-effect write runs
	arrival   map[string]int64 // document versions at arrival (R-REACT-73)
	queue     []*outRequest    // queued outbound requests, in order
	persisted int              // queue entries recorded by the main write's transaction
	tentative []int64          // outbox rows written by a transaction not yet known to commit
	held      []int64          // committed rows released after the response
	writes    int
}

type stateKey struct{}

func withState(ctx context.Context, st *reqState) context.Context {
	return context.WithValue(ctx, stateKey{}, st)
}

func stateFrom(ctx context.Context) *reqState {
	st, _ := ctx.Value(stateKey{}).(*reqState)
	return st
}

// isMain reports whether a write is the request's own core write (not a
// side-effect write, not a nested write on another path).
func (st *reqState) isMain(w *engine.WriteCtx) bool {
	if st == nil || w.Op.Plane != engine.Public {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.phase == phaseCore && st.sideDepth == 0 && w.Op.Path == st.target && strings.EqualFold(w.Op.Method, st.method)
}

// addTentative records outbox rows written inside a transaction.
func (st *reqState) addTentative(ids ...int64) {
	st.mu.Lock()
	st.tentative = append(st.tentative, ids...)
	st.mu.Unlock()
}

// settle confirms (committed) or drops the tentative rows.
func (st *reqState) settle(committed bool) {
	st.mu.Lock()
	if committed {
		st.held = append(st.held, st.tentative...)
	}
	st.tentative = nil
	st.mu.Unlock()
}
