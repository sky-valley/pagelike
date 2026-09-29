// Package durability runs the pagelike binary as a real process and checks
// crash safety and concurrency guarantees end to end:
//
//   - every acknowledged write survives a SIGKILL of the server;
//   - after a crash the document parses and its state agrees exactly with the
//     replayable event log (state and events commit in one transaction);
//   - conditional read-modify-write under contention loses no update, and a
//     rejected (412) write leaves state unchanged;
//   - an SSE client that reconnects with Last-Event-ID after a restart
//     receives exactly the events it missed.
//
// Run with: go test ./test/durability -count=1 (add -v for timings).
package durability

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pagelike-durability-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "pagelike")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/pagelike")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type server struct {
	t    *testing.T
	data string
	port int
	cmd  *exec.Cmd
	key  string
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func newServer(t *testing.T) *server {
	s := &server{t: t, data: t.TempDir(), port: freePort(t)}
	run := func(args ...string) string {
		out, err := exec.Command(bin, append(args, "--data", s.data)...).Output()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("site", "create", "t")
	s.key = run("key", "create", "--site", "t")
	s.start()
	rules := `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
<td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">PUT</span>
<span itemprop="method">POST</span><span itemprop="method">DELETE</span></td><td itemprop="selector"></td><td itemprop="action">Allow</td></tr></table></body></html>`
	s.dav("PUT", "/rules.html", rules)
	return s
}

func (s *server) start() {
	s.cmd = exec.Command(bin, "serve", "--data", s.data, "--listen", fmt.Sprintf("127.0.0.1:%d", s.port))
	s.cmd.Stdout, s.cmd.Stderr = io.Discard, io.Discard
	if err := s.cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", s.port)); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatal("server did not start")
}

func (s *server) kill() {
	s.cmd.Process.Signal(syscall.SIGKILL)
	s.cmd.Wait()
}

func (s *server) stop() {
	s.cmd.Process.Signal(syscall.SIGTERM)
	s.cmd.Wait()
}

var client = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}

func (s *server) req(host, method, path string, hdr map[string]string, body string) (int, http.Header, string, error) {
	r, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", s.port, path), strings.NewReader(body))
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	resp, err := client.Do(r)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b), nil
}

func (s *server) dav(method, path, body string) {
	st, _, b, err := s.req("dav-t.localhost", method, path, map[string]string{"Authorization": "Bearer " + s.key}, body)
	if err != nil || st >= 300 {
		s.t.Fatalf("dav %s %s: %d %v %s", method, path, st, err, b)
	}
}

func (s *server) public(method, path string, hdr map[string]string, body string) (int, http.Header, string, error) {
	return s.req("t.localhost", method, path, hdr, body)
}

var itemRE = regexp.MustCompile(`<li id="(i-[0-9]+)">`)

// TestCrashKeepsAcknowledgedWritesAndEventsAgree hammers a document with
// concurrent appends, SIGKILLs the server mid-flight several times, and
// checks invariants after every restart.
func TestCrashKeepsAcknowledgedWritesAndEventsAgree(t *testing.T) {
	s := newServer(t)
	defer s.stop()
	s.dav("PUT", "/list.html", "<!DOCTYPE html>\n<html><body>\n<ul id=\"list\"></ul>\n</body></html>\n")

	var acked sync.Map
	var next atomic.Int64
	for round := 0; round < 4; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		for w := 0; w < 12; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					id := fmt.Sprintf("i-%d", next.Add(1))
					st, _, _, err := s.public("POST", "/list.html", map[string]string{"Range": "selector=#list"}, `<li id="`+id+`">`+id+`</li>`)
					if err == nil && st == 206 {
						acked.Store(id, true)
					}
				}
			}()
		}
		time.Sleep(time.Duration(300+rand.Intn(400)) * time.Millisecond)
		s.kill() // no graceful shutdown: in-flight transactions are cut off
		cancel()
		wg.Wait()
		s.start()

		st, _, body, err := s.public("GET", "/list.html", nil, "")
		if err != nil || st != 200 {
			t.Fatalf("round %d: GET after crash: %d %v", round, st, err)
		}
		present := map[string]bool{}
		for _, m := range itemRE.FindAllStringSubmatch(body, -1) {
			if present[m[1]] {
				t.Fatalf("round %d: %s duplicated after crash", round, m[1])
			}
			present[m[1]] = true
		}
		missing := 0
		acked.Range(func(k, _ any) bool {
			if !present[k.(string)] {
				missing++
				t.Errorf("round %d: acknowledged write %s lost", round, k)
			}
			return true
		})
		// Every item in the document has exactly one committed event, and
		// every event refers to an item in the document.
		events := s.replayAll(t, "/list.html")
		seen := map[string]bool{}
		for _, e := range events {
			if m := itemRE.FindStringSubmatch(e); m != nil {
				if seen[m[1]] {
					t.Fatalf("round %d: event for %s recorded twice", round, m[1])
				}
				seen[m[1]] = true
				if !present[m[1]] {
					t.Fatalf("round %d: event for %s but the item is not in the document", round, m[1])
				}
			}
		}
		for id := range present {
			if !seen[id] {
				t.Fatalf("round %d: item %s in the document without a committed event", round, id)
			}
		}
		n := 0
		acked.Range(func(_, _ any) bool { n++; return true })
		t.Logf("round %d: %d acknowledged, %d present (unacknowledged but committed: %d), %d events, %d lost", round, n, len(present), len(present)-n, len(seen), missing)
	}
}

// oldestEventID names the position before the first stored event, in
// PageLove's id shape ("v1~<path key>.<ms>-<seq>"); ids of any other shape
// are ignored rather than replayed from.
const oldestEventID = "v1~000000000000.0-0"

// replayAll subscribes with the oldest possible Last-Event-ID and collects
// the replayed mutation payloads (retention window: everything in this test).
func (s *server) replayAll(t *testing.T, path string) []string {
	r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", s.port, path), nil)
	r.Host = "t.localhost"
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Last-Event-ID", oldestEventID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(r.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []string
	var data strings.Builder
	name := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	idle := time.AfterFunc(1500*time.Millisecond, cancel)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = line[7:]
		case strings.HasPrefix(line, "data: "):
			data.WriteString(line[6:])
		case line == "":
			// Only element mutations (POST appends) count; the connection
			// event and whole-document writes are not items.
			if name == "mutation" && strings.Contains(data.String(), `itemprop="method">POST<`) {
				out = append(out, data.String())
			}
			data.Reset()
			name = ""
			idle.Reset(700 * time.Millisecond)
		}
	}
	return out
}

// TestConditionalWritesLoseNoUpdate runs contending read-modify-write
// increments guarded by If-Match; every successful write must be reflected
// and every 412 must leave the value unchanged.
func TestConditionalWritesLoseNoUpdate(t *testing.T) {
	s := newServer(t)
	defer s.stop()
	s.dav("PUT", "/counter.html", `<!DOCTYPE html><html><body><p id="n">0</p></body></html>`)
	var ok, conflicts atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				for {
					st, h, body, err := s.public("GET", "/counter.html", map[string]string{"Range": "selector=#n"}, "")
					if err != nil || st != 206 {
						t.Errorf("read: %d %v", st, err)
						return
					}
					v, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(body, `<p id="n">`), "</p>"))
					st, _, _, err = s.public("PUT", "/counter.html", map[string]string{"Range": "selector=#n", "If-Match": h.Get("ETag")}, fmt.Sprintf(`<p id="n">%d</p>`, v+1))
					if err != nil {
						t.Errorf("write: %v", err)
						return
					}
					if st == 206 {
						ok.Add(1)
						break
					}
					if st != 412 {
						t.Errorf("unexpected status %d", st)
						return
					}
					conflicts.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	_, _, body, _ := s.public("GET", "/counter.html", map[string]string{"Range": "selector=#n"}, "")
	final, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(body, `<p id="n">`), "</p>"))
	if int64(final) != ok.Load() || final != 16*25 {
		t.Fatalf("lost updates: counter=%d, successful writes=%d, want %d", final, ok.Load(), 16*25)
	}
	t.Logf("%d successful conditional writes, %d rejected with 412 and retried, final=%d", ok.Load(), conflicts.Load(), final)

	// A stale tag never overwrites the accepted value.
	st, _, _, _ := s.public("PUT", "/counter.html", map[string]string{"Range": "selector=#n", "If-Match": `"stale"`}, `<p id="n">-1</p>`)
	_, _, after, _ := s.public("GET", "/counter.html", map[string]string{"Range": "selector=#n"}, "")
	if st != 412 || !strings.Contains(after, fmt.Sprintf(">%d<", final)) {
		t.Fatalf("stale write: status %d, value now %s", st, after)
	}
}

// TestReconnectAfterRestartReplaysMissedEvents checks that a client which
// saw event E, missed writes during a restart, and reconnects with
// Last-Event-ID: E receives exactly the missed mutations, in order.
func TestReconnectAfterRestartReplaysMissedEvents(t *testing.T) {
	s := newServer(t)
	defer s.stop()
	s.dav("PUT", "/feed.html", `<!DOCTYPE html><html><body><ol id="f"></ol></body></html>`)
	s.public("POST", "/feed.html", map[string]string{"Range": "selector=#f"}, `<li id="i-1">one</li>`)
	events := s.replayAll(t, "/feed.html")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	last := s.lastEventID(t, "/feed.html")
	s.stop()
	s.start()
	for i := 2; i <= 5; i++ {
		s.public("POST", "/feed.html", map[string]string{"Range": "selector=#f"}, fmt.Sprintf(`<li id="i-%d">%d</li>`, i, i))
	}
	s.kill() // crash, restart, and only then does the client reconnect
	s.start()
	r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/feed.html", s.port), nil)
	r.Host = "t.localhost"
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Last-Event-ID", last)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(r.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	var got []string
	for sc.Scan() && len(got) < 4 {
		if m := itemRE.FindStringSubmatch(sc.Text()); m != nil {
			got = append(got, m[1])
		}
	}
	resp.Body.Close()
	if strings.Join(got, ",") != "i-2,i-3,i-4,i-5" {
		t.Fatalf("replay after restart = %v, want i-2..i-5", got)
	}
}

// lastEventID returns the id of the newest event for path.
func (s *server) lastEventID(t *testing.T, path string) string {
	r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", s.port, path), nil)
	r.Host = "t.localhost"
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Last-Event-ID", oldestEventID)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	resp, err := http.DefaultClient.Do(r.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	id := ""
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "id: ") {
			id = sc.Text()[4:]
		}
	}
	if id == "" {
		t.Fatal("no event id")
	}
	return id
}
