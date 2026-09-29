// Command loadtest measures pagelike on the local machine: it starts the
// binary with a fresh data directory and reports latency percentiles and
// throughput for reads, selector reads, appends, a composed page, and SSE
// fan-out. Output is Markdown for docs/compat/report.md.
//
//	go run ./tools/loadtest [-duration 5s] [-subscribers 200]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	duration    = flag.Duration("duration", 5*time.Second, "per-scenario duration")
	subscribers = flag.Int("subscribers", 200, "SSE subscribers for the fan-out scenario")
	port        int
	client      = &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256}}
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Parse()
	dir, _ := os.MkdirTemp("", "pagelike-load")
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "pagelike")
	must(exec.Command("go", "build", "-o", bin, "./cmd/pagelike").Run())
	data := filepath.Join(dir, "data")
	run := func(a ...string) string {
		out, err := exec.Command(bin, append(a, "--data", data)...).Output()
		must(err)
		return strings.TrimSpace(string(out))
	}
	run("site", "create", "t")
	key := run("key", "create", "--site", "t")
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port = l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv := exec.Command(bin, "serve", "--data", data, "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	must(srv.Start())
	defer srv.Process.Kill()
	for i := 0; i < 200; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	dav := func(p, body string) {
		r, _ := http.NewRequest("PUT", fmt.Sprintf("http://127.0.0.1:%d%s", port, p), strings.NewReader(body))
		r.Host = "dav-t.localhost"
		r.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(r)
		must(err)
		resp.Body.Close()
	}
	dav("/rules.html", `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">POST</span></td><td itemprop="selector"></td><td itemprop="action">Allow</td></tr></table></body></html>`)
	var items strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&items, `<li id="i%d" itemscope itemtype="https://ex/Item"><span itemprop="name">Item %d</span><meta itemprop="n" content="%d"></li>`, i, i, i)
	}
	doc := `<!DOCTYPE html><html><head><title>load</title></head><body><h1>Load</h1><ul id="items">` + items.String() + `</ul><ul id="log"></ul></body></html>`
	dav("/page.html", doc)
	dav("/composed.html", `<!DOCTYPE html><html xmlns:p="https://pagelove.org/1.0" xmlns:r="https://pagelove.org/Binding/CSS"><body><ul r:items="[itemtype='https://ex/Item']" p:template="text/liquid">{% for i in items %}<li>{{ i.name }} #{{ i.n }}</li>{% endfor %}</ul></body></html>`)
	for i := 0; i < 20; i++ {
		dav(fmt.Sprintf("/w%d.html", i), `<!DOCTYPE html><html><body><ul id="log"></ul></body></html>`)
	}

	cpu := ""
	if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		cpu = strings.TrimSpace(string(out))
	}
	fmt.Printf("Machine: %s, %d CPUs, %s/%s, %s. Server and load generator on the same machine, loopback HTTP/1.1.\n\n", cpu, runtime.NumCPU(), runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Println("| Scenario | Concurrency | Requests/s | p50 | p95 | p99 | Errors |")
	fmt.Println("|---|---|---|---|---|---|---|")
	scenario("GET whole document (6 KB, stored bytes)", 32, func(i int) (int, error) { return get("/page.html", nil) })
	scenario("GET selector fragment (`Range: selector=#i25`)", 32, func(i int) (int, error) { return get("/page.html", map[string]string{"Range": "selector=#i25"}) })
	scenario("GET composed page (binding over 50 items + Liquid loop)", 32, func(i int) (int, error) { return get("/composed.html", nil) })
	var n atomic.Int64
	scenario("POST append, one document (serialized per site)", 16, func(i int) (int, error) {
		return post("/page.html", fmt.Sprintf(`<li id="l%d">x</li>`, n.Add(1)))
	})
	scenario("POST append, 20 documents in one site", 16, func(i int) (int, error) {
		return post(fmt.Sprintf("/w%d.html", i%20), fmt.Sprintf(`<li id="l%d">x</li>`, n.Add(1)))
	})
	fmt.Println()
	fanout(*subscribers)
}

func get(p string, hdr map[string]string) (int, error) {
	r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", port, p), nil)
	r.Host = "t.localhost"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	resp, err := client.Do(r)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

func post(p, body string) (int, error) {
	r, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d%s", port, p), strings.NewReader(body))
	r.Host = "t.localhost"
	r.Header.Set("Range", "selector=#log")
	resp, err := client.Do(r)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	return ds[int(float64(len(ds)-1)*p)]
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f ms", float64(d.Microseconds())/1000) }

func scenario(name string, conc int, f func(i int) (int, error)) {
	var mu sync.Mutex
	var lat []time.Duration
	var errs atomic.Int64
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var local []time.Duration
			for i := w; time.Now().Before(deadline); i += conc {
				t0 := time.Now()
				st, err := f(i)
				d := time.Since(t0)
				if err != nil || st >= 400 {
					errs.Add(1)
					continue
				}
				local = append(local, d)
			}
			mu.Lock()
			lat = append(lat, local...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	el := time.Since(start)
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	fmt.Printf("| %s | %d | %.0f | %s | %s | %s | %d |\n", name, conc, float64(len(lat))/el.Seconds(), ms(pct(lat, .5)), ms(pct(lat, .95)), ms(pct(lat, .99)), errs.Load())
}

// fanout opens n SSE subscribers on one document, performs writes at a
// steady rate, and measures write-commit → delivery latency at each
// subscriber.
func fanout(n int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := sync.Map{} // item id → send time
	var mu sync.Mutex
	var lat []time.Duration
	var ready sync.WaitGroup
	var delivered atomic.Int64
	for i := 0; i < n; i++ {
		ready.Add(1)
		go func() {
			r, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/page.html", port), nil)
			r.Host = "t.localhost"
			r.Header.Set("Accept", "text/event-stream")
			resp, err := client.Do(r)
			if err != nil {
				ready.Done()
				return
			}
			defer resp.Body.Close()
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 1<<20), 1<<22)
			once := false
			for sc.Scan() {
				line := sc.Text()
				if !once && strings.HasPrefix(line, "data:") {
					once = true
					ready.Done()
				}
				if i := strings.Index(line, `<li id="f`); i >= 0 {
					id := line[i+8 : i+8+strings.IndexByte(line[i+8:], '"')]
					if v, ok := sent.Load(id); ok {
						d := time.Since(v.(time.Time))
						mu.Lock()
						lat = append(lat, d)
						mu.Unlock()
						delivered.Add(1)
					}
				}
			}
		}()
	}
	ready.Wait()
	const writes = 100
	for i := 0; i < writes; i++ {
		id := fmt.Sprintf("f%d", i)
		sent.Store(id, time.Now())
		post("/page.html", fmt.Sprintf(`<li id="%s">fan</li>`, id))
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(time.Second)
	cancel()
	mu.Lock()
	defer mu.Unlock()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	fmt.Printf("SSE fan-out: %d subscribers on one document, %d writes at 50/s: %d of %d deliveries (%.1f%%); delivery latency after the write request was sent p50 %s, p95 %s, p99 %s.\n",
		n, writes, delivered.Load(), n*writes, 100*float64(delivered.Load())/float64(n*writes), ms(pct(lat, .5)), ms(pct(lat, .95)), ms(pct(lat, .99)))
}
