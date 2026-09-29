package harness

import (
	"net"
	"os"
	"sync"
	"time"
)

// Stalled readers for the local target.
//
// sse_pause stops the runner reading a stream, but kernel socket buffers on
// loopback absorb hundreds of kilobytes (macOS grows even a 4 KiB SO_RCVBUF
// to ~320 KiB), so a paused reader would never push back on the server. The
// local target therefore wraps the server's accepted connections: while a
// stream is paused, the server side of its connection stops accepting
// writes, exactly as a socket that no longer drains, and write deadlines are
// honoured so the server can still give up on it.

// stalls maps a client's local address (the server's remote address) to
// the connection gate the server writes through.
type stalls struct {
	mu    sync.Mutex
	gates map[string]*gate
}

func newStalls() *stalls { return &stalls{gates: map[string]*gate{}} }

func (s *stalls) gate(addr string) *gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gates[addr]
	if g == nil {
		g = &gate{changed: make(chan struct{})}
		s.gates[addr] = g
	}
	return g
}

// set pauses or resumes server writes to the client at addr.
func (s *stalls) set(addr string, paused bool) { s.gate(addr).set(paused) }

// gate blocks writes while paused.
type gate struct {
	mu       sync.Mutex
	paused   bool
	deadline time.Time
	changed  chan struct{} // closed and replaced on every change
}

func (g *gate) notify() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *gate) set(paused bool) {
	g.mu.Lock()
	g.paused = paused
	g.notify()
	g.mu.Unlock()
}

func (g *gate) setDeadline(t time.Time) {
	g.mu.Lock()
	g.deadline = t
	g.notify()
	g.mu.Unlock()
}

// wait returns once writes may proceed, or an error once the write
// deadline has passed while paused.
func (g *gate) wait() error {
	for {
		g.mu.Lock()
		paused, deadline, changed := g.paused, g.deadline, g.changed
		g.mu.Unlock()
		if !paused {
			return nil
		}
		if deadline.IsZero() {
			<-changed
			continue
		}
		d := time.Until(deadline)
		if d <= 0 {
			return os.ErrDeadlineExceeded
		}
		t := time.NewTimer(d)
		select {
		case <-changed:
			t.Stop()
		case <-t.C:
			return os.ErrDeadlineExceeded
		}
	}
}

// stallListener wraps the server's accepted connections in gates.
type stallListener struct {
	net.Listener
	stalls *stalls
}

func (l stallListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &stallConn{Conn: c, gate: l.stalls.gate(c.RemoteAddr().String())}, nil
}

type stallConn struct {
	net.Conn
	gate *gate
}

func (c *stallConn) Write(b []byte) (int, error) {
	if err := c.gate.wait(); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

func (c *stallConn) SetWriteDeadline(t time.Time) error {
	c.gate.setDeadline(t)
	return c.Conn.SetWriteDeadline(t)
}

func (c *stallConn) SetDeadline(t time.Time) error {
	c.gate.setDeadline(t)
	return c.Conn.SetDeadline(t)
}
