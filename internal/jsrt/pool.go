package jsrt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// worker is the parent's handle on one worker process.
type worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	conn     *conn
	pid      int
	frames   chan *frame
	exited   chan struct{}
	exitErr  error
	stderr   *ringBuffer
	calls    int
	peakRSS  int64
	overflow bool
	killed   atomic.Bool
	stopOnce sync.Once
	logger   interface {
		Info(string, ...any)
		Warn(string, ...any)
	}
}

// Worker exit codes the worker uses for its own limits.
const (
	exitRSS      = 3 // the worker's peak RSS passed its ceiling
	exitProtocol = 4
)

// spawn starts a worker process and waits for its ready frame.
func (rt *Runtime) spawn() (*worker, error) {
	exe, err := rt.executable()
	if err != nil {
		return nil, fmt.Errorf("jsrt: locate executable: %w", err)
	}
	cmd := exec.Command(exe, WorkerCommand)
	cmd.Env = []string{
		workerEnv + "=" + workerEnvValue,
		envRSS + "=" + strconv.FormatInt(rt.opts.RSSLimit, 10),
		envAS + "=" + strconv.FormatInt(rt.opts.AddressSpace, 10),
		envSlots + "=" + strconv.Itoa(rt.opts.StackSlots),
	}
	if rt.opts.ParallelPrepare {
		cmd.Env = append(cmd.Env, envParallel+"=1")
	}
	if v := os.Getenv("GORACE"); v != "" { // race-detector builds only read it at start-up
		cmd.Env = append(cmd.Env, "GORACE="+v)
	}
	cmd.Dir = "/"
	setProcAttr(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	w := &worker{cmd: cmd, stdin: stdin, frames: make(chan *frame, 4), exited: make(chan struct{}), stderr: &ringBuffer{max: 8 << 10}, logger: rt.opts.Logger}
	cmd.Stderr = w.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("jsrt: start worker: %w", err)
	}
	w.pid = cmd.Process.Pid
	w.conn = newConn(stdout, stdin)
	go func() {
		defer func() {
			w.exitErr = cmd.Wait()
			close(w.exited)
		}()
		for {
			f, err := w.conn.recv()
			if err != nil {
				close(w.frames)
				return
			}
			w.frames <- f
		}
	}()
	rt.mu.Lock()
	rt.live[w] = true
	rt.mu.Unlock()
	timer := time.NewTimer(rt.opts.StartTimeout)
	defer timer.Stop()
	select {
	case f, ok := <-w.frames:
		if !ok || f.T != "ready" || f.Ready == nil {
			w.kill()
			rt.forget(w)
			return nil, fmt.Errorf("jsrt: worker did not start: %s", w.deathReason())
		}
		rt.opts.Logger.Info("jsrt: worker started", "pid", w.pid, "lockdown", f.Ready.Lockdown)
		return w, nil
	case <-timer.C:
		w.kill()
		rt.forget(w)
		return nil, errors.New("jsrt: worker start timed out")
	}
}

func (w *worker) dead() bool {
	if w.killed.Load() {
		return true
	}
	select {
	case <-w.exited:
		return true
	default:
		return false
	}
}

// kill SIGKILLs the worker's process group and waits for it to be reaped.
func (w *worker) kill() {
	w.killed.Store(true)
	killGroup(w.cmd)
	select {
	case <-w.exited:
	case <-time.After(2 * time.Second):
	}
}

// stop ends a worker: EOF on stdin lets an idle worker exit by itself.
func (w *worker) stop() {
	w.stopOnce.Do(func() {
		if w.dead() {
			w.kill()
			return
		}
		w.killed.Store(true)
		_ = w.stdin.Close()
		go func() {
			select {
			case <-w.exited:
			case <-time.After(time.Second):
				killGroup(w.cmd)
			}
		}()
	})
}

// deathReason summarizes why a worker exited, from its status and stderr.
func (w *worker) deathReason() string {
	select {
	case <-w.exited:
	case <-time.After(time.Second):
	}
	s := "exited"
	if w.exitErr != nil {
		s = w.exitErr.Error()
	}
	if head := stderrHead(w.stderr.String()); head != "" {
		s += ": " + head
	}
	return s
}

// call sends one evaluation and serves its callbacks until the result
// arrives. The parent deadline is the time budget plus grace, extended by
// the time the host spends in callbacks (the JavaScript thread is blocked
// then); the RSS ceiling is polled while the call runs.
func (w *worker) call(ctx context.Context, msg *callMsg, timeout time.Duration, opts Options, host Host) (*resultMsg, *Failure) {
	w.calls++
	if err := w.conn.send(&frame{T: "call", Call: msg}); err != nil {
		w.kill()
		return nil, &Failure{Variant: VariantInternal, Message: "send to worker: " + err.Error()}
	}
	killAt := time.Now().Add(timeout + opts.Grace)
	deadline := time.NewTimer(timeout + opts.Grace)
	defer deadline.Stop()
	poll := time.NewTicker(rssPollInterval)
	defer poll.Stop()
	cbctx := context.WithValue(ctx, inCallbackKey, true)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				return nil, w.deathFailure()
			}
			switch f.T {
			case "result":
				if f.Result == nil {
					w.kill()
					return nil, &Failure{Variant: VariantInternal, Message: "empty result frame"}
				}
				w.peakRSS = max(w.peakRSS, f.Result.RSS)
				return f.Result, nil
			case "cb":
				if f.CB == nil {
					w.kill()
					return nil, &Failure{Variant: VariantInternal, Message: "empty callback frame"}
				}
				t0 := time.Now()
				reply := serveCallback(cbctx, host, f.CB)
				// The JavaScript thread waited for the host: move the kill
				// deadline by the time spent here.
				killAt = killAt.Add(time.Since(t0))
				if !deadline.Stop() {
					select {
					case <-deadline.C:
					default:
					}
				}
				deadline.Reset(time.Until(killAt))
				if err := w.conn.send(&frame{T: "reply", Reply: reply}); err != nil {
					w.kill()
					return nil, &Failure{Variant: VariantInternal, Message: "send to worker: " + err.Error()}
				}
			default:
				w.kill()
				return nil, &Failure{Variant: VariantInternal, Message: "unexpected frame " + strconv.Quote(f.T)}
			}
		case <-deadline.C:
			w.kill()
			return nil, &Failure{Variant: VariantTimeout, Message: fmt.Sprintf("time budget of %v exhausted (worker killed)", timeout)}
		case <-poll.C:
			if rss := processRSS(w.pid); rss > 0 {
				w.peakRSS = max(w.peakRSS, rss)
				if rss > opts.RSSLimit {
					w.kill()
					return nil, &Failure{Variant: VariantOutOfMemory, Message: fmt.Sprintf("worker memory %d MiB passed the %d MiB ceiling (worker killed)", rss>>20, opts.RSSLimit>>20)}
				}
			}
		case <-ctx.Done():
			w.kill()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, &Failure{Variant: VariantTimeout, Message: "deadline exceeded (worker killed)"}
			}
			return nil, &Failure{Variant: VariantInternal, Message: "evaluation canceled: " + ctx.Err().Error()}
		}
	}
}

// deathFailure classifies an unexpected worker exit.
func (w *worker) deathFailure() *Failure {
	reason := w.deathReason()
	w.killed.Store(true)
	errText := w.stderr.String()
	if ee, ok := w.exitErr.(*exec.ExitError); ok && ee.ExitCode() == exitRSS {
		return &Failure{Variant: VariantOutOfMemory, Message: "worker memory passed its ceiling (worker exited)"}
	}
	switch {
	case strings.Contains(errText, "goroutine stack exceeds"), strings.Contains(errText, "stack overflow"):
		return &Failure{Variant: VariantThrew, Message: "InternalError: stack overflow"}
	case strings.Contains(errText, "out of memory"), strings.Contains(errText, "cannot allocate memory"):
		return &Failure{Variant: VariantOutOfMemory, Message: "worker ran out of memory"}
	case strings.Contains(reason, "cpu time limit"), strings.Contains(reason, "CPU time limit"):
		return &Failure{Variant: VariantTimeout, Message: "worker CPU limit reached"}
	}
	return &Failure{Variant: VariantInternal, Message: "JavaScript worker died: " + reason}
}

func stderrHead(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var keep []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "fatal") || strings.HasPrefix(l, "runtime:") || strings.HasPrefix(l, "panic") || strings.Contains(l, "error") || strings.Contains(l, "signal") {
			keep = append(keep, l)
		}
		if len(keep) >= 3 {
			break
		}
	}
	return strings.Join(keep, " / ")
}

// ringBuffer keeps the first and the last max/2 bytes written (the worker's
// stderr): a Go fatal error states its cause first, then dumps stacks.
type ringBuffer struct {
	mu   sync.Mutex
	max  int
	head []byte
	tail []byte
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if room := r.max/2 - len(r.head); room > 0 {
		k := min(room, len(p))
		r.head = append(r.head, p[:k]...)
		p = p[k:]
	}
	r.tail = append(r.tail, p...)
	if len(r.tail) > r.max/2 {
		r.tail = append(r.tail[:0:0], r.tail[len(r.tail)-r.max/2:]...)
	}
	return n, nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.head) + string(r.tail)
}
