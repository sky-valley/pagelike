package reactions

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Outbox states. Rows written while a request is still being answered are
// held and released once its response has been written (R-REACT-56).
const (
	stateHeld     = "held"
	statePending  = "pending"
	stateInflight = "inflight" // a generic attempt is running
	stateConsumed = "consumed" // a transition delivery was claimed: never sent again (R-REACT-82)
	stateDone     = "done"
	stateFailed   = "failed"
)

// Delivery timing (R-REACT-57, R-REACT-58).
const (
	connectTimeout = 10 * time.Second // connect + response headers
	attemptTimeout = 30 * time.Second
	maxResponse    = 64 << 10
	pollInterval   = time.Second
	idleTimeout    = 30 * time.Second // a worker with nothing to deliver stops
	maxConcurrent  = 16
)

// backoff is the delay before attempt k (k ≥ 2): min(2^(k-1) s, 30 s).
func backoff(k int) time.Duration {
	d := time.Second << (k - 1)
	if k > 6 || d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// outbox persists queued outbound requests in each site's outbox table and
// runs one delivery worker per open site.
type outbox struct {
	x       *Reactions
	started time.Time
	mu      sync.Mutex
	workers map[*site.Site]*worker
}

func newOutbox(x *Reactions) *outbox {
	return &outbox{x: x, started: time.Now(), workers: map[*site.Site]*worker{}}
}

// resumeAll starts workers for sites with undelivered requests left by an
// earlier process (R-REACT-59).
func (o *outbox) resumeAll() {
	if o.x.srv == nil || o.x.srv.Sites == nil {
		return
	}
	names, err := o.x.srv.Sites.List()
	if err != nil {
		return
	}
	for _, n := range names {
		s, err := o.x.srv.Sites.Get(context.Background(), n)
		if err != nil {
			continue
		}
		var c int
		if err := s.Store.DB().QueryRow(`SELECT COUNT(*) FROM outbox WHERE state IN (?, ?, ?, ?)`, stateHeld, statePending, stateInflight, stateConsumed).Scan(&c); err == nil && c > 0 {
			o.wake(s)
		}
	}
}

// wake makes the site's worker look for due requests now, starting it if
// it is not running. Workers run only while there is something to deliver.
func (o *outbox) wake(s *site.Site) {
	o.mu.Lock()
	defer o.mu.Unlock()
	w := o.workers[s]
	if w == nil {
		w = &worker{o: o, s: s, wake: make(chan struct{}, 1), sem: make(chan struct{}, maxConcurrent)}
		o.workers[s] = w
		go w.run()
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// retire removes an idle worker unless a wake-up is pending (checked under
// the lock wake sends under, so none is lost).
func (o *outbox) retire(w *worker) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	select {
	case <-w.wake:
		return false
	default:
	}
	if o.workers[w.s] == w {
		delete(o.workers, w.s)
	}
	return true
}

func (o *outbox) forget(w *worker) {
	o.mu.Lock()
	if o.workers[w.s] == w {
		delete(o.workers, w.s)
	}
	o.mu.Unlock()
}

// insertTx records requests inside a write transaction (R-REACT-59).
func insertTx(tx *store.Tx, reqs []*outRequest, state string) ([]int64, error) {
	var ids []int64
	for _, r := range reqs {
		b, err := json.Marshal(r)
		if err != nil {
			return ids, err
		}
		res, err := tx.Exec(`INSERT INTO outbox(created_ms, kind, request, attempts, max_attempts, next_ms, state) VALUES (?,?,?,0,?,?,?)`,
			tx.Now(), r.Kind, string(b), max(r.Attempts, 1), tx.Now(), state)
		if err != nil {
			return ids, err
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	return ids, nil
}

// insert records requests in their own transaction, ready for delivery.
func (o *outbox) insert(ctx context.Context, s *site.Site, reqs []*outRequest) error {
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, r := range reqs {
		b, err := json.Marshal(r)
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO outbox(created_ms, kind, request, attempts, max_attempts, next_ms, state) VALUES (?,?,?,0,?,?,?)`,
			now, r.Kind, string(b), max(r.Attempts, 1), now, statePending); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// release makes held rows deliverable.
func (o *outbox) release(ctx context.Context, s *site.Site, ids []int64) error {
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE outbox SET state = ?, next_ms = ? WHERE id = ? AND state = ?`, statePending, now, id, stateHeld); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- worker

type worker struct {
	o    *outbox
	s    *site.Site
	wake chan struct{}
	sem  chan struct{}
}

type entry struct {
	id       int64
	kind     string
	req      outRequest
	attempts int
	max      int
}

func (w *worker) db() *sql.DB { return w.s.Store.DB() }

func (w *worker) run() {
	if err := w.recover(); err != nil {
		w.o.forget(w)
		return // the site's database is gone (closed or removed)
	}
	idleSince := time.Now()
	for {
		due, next, err := w.due()
		if err != nil {
			w.o.forget(w)
			return
		}
		if len(due) > 0 || next > 0 || len(w.sem) > 0 {
			idleSince = time.Now()
		} else if time.Since(idleSince) > idleTimeout && w.o.retire(w) {
			return
		}
		for _, e := range due {
			if !w.claim(e) {
				continue
			}
			w.sem <- struct{}{}
			go func(e entry) {
				defer func() { <-w.sem }()
				w.deliver(e)
			}(e)
		}
		wait := pollInterval
		if next > 0 {
			if d := time.Until(time.UnixMilli(next)); d < wait {
				wait = max(d, 5*time.Millisecond)
			}
		}
		if len(due) > 0 {
			wait = 0 // more may be due
		}
		t := time.NewTimer(wait)
		select {
		case <-w.wake:
		case <-t.C:
		}
		t.Stop()
	}
}

// recover repairs rows left by a process that stopped mid-delivery:
// requests held by a response that was never finished are released,
// interrupted generic attempts are retried if attempts remain, and a
// transition delivery that was claimed is never sent again (R-REACT-59,
// R-REACT-82).
func (w *worker) recover() error {
	start := w.o.started.UnixMilli()
	now := time.Now().UnixMilli()
	if _, err := w.db().Exec(`UPDATE outbox SET state = ?, next_ms = ? WHERE state = ? AND created_ms < ?`, statePending, now, stateHeld, start); err != nil {
		return err
	}
	if _, err := w.db().Exec(`UPDATE outbox SET state = CASE WHEN attempts < max_attempts THEN ? ELSE ? END, next_ms = ? WHERE state = ? AND kind = ?`,
		statePending, stateFailed, now, stateInflight, kindHTTP); err != nil {
		return err
	}
	_, err := w.db().Exec(`UPDATE outbox SET state = ?, last_error = 'interrupted' WHERE state = ?`, stateFailed, stateConsumed)
	return err
}

// due returns the requests whose time has come (in queue order) and the
// time of the next pending one.
func (w *worker) due() ([]entry, int64, error) {
	now := time.Now().UnixMilli()
	rows, err := w.db().Query(`SELECT id, kind, request, attempts, max_attempts FROM outbox WHERE state = ? AND next_ms <= ? ORDER BY id LIMIT 64`, statePending, now)
	if err != nil {
		return nil, 0, err
	}
	var out []entry
	for rows.Next() {
		var e entry
		var raw string
		if err := rows.Scan(&e.id, &e.kind, &raw, &e.attempts, &e.max); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if json.Unmarshal([]byte(raw), &e.req) != nil {
			w.db().Exec(`UPDATE outbox SET state = ?, last_error = 'undecodable' WHERE id = ?`, stateFailed, e.id)
			continue
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next sql.NullInt64
	if err := w.db().QueryRow(`SELECT MIN(next_ms) FROM outbox WHERE state = ?`, statePending).Scan(&next); err != nil {
		return nil, 0, err
	}
	return out, next.Int64, nil
}

// claim marks an attempt as started before it is made: a transition
// delivery is consumed here, so it can never be sent twice.
func (w *worker) claim(e entry) bool {
	st := stateInflight
	if e.kind == kindTransition {
		st = stateConsumed
	}
	res, err := w.db().Exec(`UPDATE outbox SET state = ?, attempts = attempts + 1 WHERE id = ? AND state = ?`, st, e.id, statePending)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func (w *worker) deliver(e entry) {
	attempt := e.attempts + 1
	err := w.o.x.send(w.s, &e.req)
	switch {
	case err == nil:
		w.db().Exec(`UPDATE outbox SET state = ?, last_error = '' WHERE id = ?`, stateDone, e.id)
	case e.kind == kindHTTP && attempt < e.max:
		next := time.Now().Add(backoff(attempt + 1)).UnixMilli()
		w.db().Exec(`UPDATE outbox SET state = ?, next_ms = ?, last_error = ? WHERE id = ?`, statePending, next, err.Error(), e.id)
		select {
		case w.wake <- struct{}{}:
		default:
		}
	default:
		// Abandoned: nothing is reported to the client (R-REACT-57/82).
		w.o.x.log.Info("reactions: outbound request abandoned", "site", w.s.Name, "url", e.req.URL, "attempts", attempt, "err", err)
		w.db().Exec(`UPDATE outbox SET state = ?, last_error = ? WHERE id = ?`, stateFailed, err.Error(), e.id)
	}
}

// ---------------------------------------------------------------- sending

var errRedirect = errors.New("redirect not followed")

// send makes one attempt. Requests to a site this server serves are
// delivered in process as ordinary anonymous public-plane requests
// (R-REACT-49, R-REACT-83); others go to the network under the
// destination policy (R-REACT-61). Transport errors and non-2xx statuses
// (3xx included: redirects are not followed) are failures (R-REACT-58).
func (x *Reactions) send(s *site.Site, r *outRequest) error {
	u, err := url.Parse(r.URL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, bytes.NewReader(r.Body))
	if err != nil {
		return err
	}
	if len(r.Body) == 0 {
		req.Body = http.NoBody
	}
	req.ContentLength = int64(len(r.Body))
	for _, kv := range r.Header {
		req.Header.Add(kv[0], kv[1])
	}
	if x.servesHost(u.Host) {
		rec := &recorder{header: http.Header{}}
		req.RemoteAddr = "192.0.2.1:0" // never a loopback client (dev impersonation stays off)
		req.RequestURI = u.RequestURI()
		x.srv.ServeHTTP(rec, req)
		return statusErr(rec.code())
	}
	pol := policyFor(s)
	if err := pol.checkHost(u); err != nil {
		return err
	}
	client := &http.Client{
		Timeout: attemptTimeout,
		Transport: &http.Transport{
			Proxy:                 nil, // a proxy would bypass the destination checks
			DialContext:           (&net.Dialer{Timeout: connectTimeout, Control: pol.control(u)}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: connectTimeout,
			DisableKeepAlives:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
	resp.Body.Close()
	return statusErr(resp.StatusCode)
}

func statusErr(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	if code >= 300 && code < 400 {
		return fmt.Errorf("status %d (%w)", code, errRedirect)
	}
	return fmt.Errorf("status %d", code)
}

// servesHost reports whether host (with or without port) is the public
// plane of a site this server serves.
func (x *Reactions) servesHost(hostport string) bool {
	if x.srv == nil {
		return false
	}
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	dom := strings.ToLower(x.srv.Cfg.Domain)
	if label, ok := strings.CutSuffix(host, "."+dom); ok && label != "" && !strings.Contains(label, ".") {
		if strings.HasPrefix(label, "dav-") || label == "console" {
			return false
		}
		return x.srv.Sites.Exists(label)
	}
	if host == dom {
		return false
	}
	_, err := x.srv.Sites.ByAlias(context.Background(), host)
	return err == nil
}

// recorder captures an in-process response's status.
type recorder struct {
	header http.Header
	status int
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 && code >= 200 {
		r.status = code
	}
}
func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(p), nil
}
func (r *recorder) Flush() {}
func (r *recorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
