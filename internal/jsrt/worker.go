package jsrt

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// WorkerCommand is the argument that starts a binary in worker mode
// ("pagelike jsrt-worker").
const WorkerCommand = "jsrt-worker"

// The pool marks the processes it starts with workerEnv. The marker is how
// a spawned test binary or harness binary knows it is a worker before any
// flag parsing.
const (
	workerEnv      = "PAGELIKE_JSRT_WORKER"
	workerEnvValue = "pagelike-jsrt-worker-v1"
	envRSS         = "PAGELIKE_JSRT_RSS"
	envAS          = "PAGELIKE_JSRT_AS"
	envSlots       = "PAGELIKE_JSRT_SLOTS"
	envParallel    = "PAGELIKE_JSRT_PARALLEL"
)

func init() {
	// A pool-spawned process becomes a worker as soon as this package is
	// initialized, so every binary that links jsrt (the pagelike binary, the
	// harness, go test binaries) can serve as a worker even if its main or
	// TestMain does not call MaybeRunWorker.
	if os.Getenv(workerEnv) == workerEnvValue {
		os.Exit(runWorkerProcess())
	}
}

// MaybeRunWorker turns the process into a JavaScript worker when it was
// started as one (by a Runtime's pool, or as "<binary> jsrt-worker"): it
// serves evaluations on stdin/stdout and exits. Otherwise it returns
// immediately. main functions and TestMain call it first.
func MaybeRunWorker() {
	if os.Getenv(workerEnv) == workerEnvValue || len(os.Args) > 1 && os.Args[1] == WorkerCommand {
		os.Exit(runWorkerProcess())
	}
}

// workerConfig is read from the environment before it is cleared.
type workerConfig struct {
	rssLimit     int64
	addressSpace int64
	stackSlots   int
	// parallelPrepare prepares the next context on another goroutine while
	// a call is served, instead of after replying.
	parallelPrepare bool
}

func readWorkerConfig() workerConfig {
	c := workerConfig{rssLimit: 512 << 20, addressSpace: 4 << 30, stackSlots: DefaultStackSlots}
	if v, err := strconv.ParseInt(os.Getenv(envRSS), 10, 64); err == nil && v > 0 {
		c.rssLimit = v
	}
	if v, err := strconv.ParseInt(os.Getenv(envAS), 10, 64); err == nil && v > 0 {
		c.addressSpace = v
	}
	if v, err := strconv.Atoi(os.Getenv(envSlots)); err == nil && v > 0 {
		c.stackSlots = v
	}
	c.parallelPrepare = os.Getenv(envParallel) == "1"
	return c
}

// runWorkerProcess is the worker's main: lock the process down, then serve.
func runWorkerProcess() int {
	cfg := readWorkerConfig()
	notes := lockdown(cfg)
	// Go-side backstops: a runaway Go recursion (the engine's slot limit
	// keeps JavaScript far below this) dies quickly instead of growing to
	// 1 GB, and the GC works towards a soft ceiling.
	debug.SetMaxStack(512 << 20)
	debug.SetMemoryLimit(cfg.rssLimit / 2)
	// One thread evaluates, one frees contexts (and one prepares them, in
	// parallel mode); more threads only make an idle worker slower to wake.
	if cfg.parallelPrepare {
		runtime.GOMAXPROCS(3)
	} else {
		runtime.GOMAXPROCS(2)
	}
	go watchRSS(cfg.rssLimit)
	c := newConn(os.Stdin, os.Stdout)
	if err := c.send(&frame{T: "ready", Ready: &readyMsg{PID: os.Getpid(), Lockdown: strings.Join(notes, " ")}}); err != nil {
		return exitProtocol
	}
	return serveWorker(c, cfg)
}

// watchRSS exits the worker when its peak RSS passes limit. It is the fast
// local check; the parent polls too and kills the process group.
func watchRSS(limit int64) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if peakRSS() > limit {
			fmt.Fprintf(os.Stderr, "jsrt worker: peak RSS %d passed %d, exiting\n", peakRSS(), limit)
			os.Exit(exitRSS)
		}
	}
}

// serveWorker handles calls until stdin closes. Evaluations run on this one
// long-lived goroutine (its grown stack is reused rather than regrown for
// every call). After replying it prepares the next fresh context (or, in
// parallel mode, another goroutine already has); the last one is freed on a
// separate goroutine.
func serveWorker(c *conn, cfg workerConfig) int {
	e := newEngine(cfg)
	e.start()
	e.prepareNext()
	for {
		f, err := c.recv()
		if err != nil {
			if err == io.EOF {
				return 0
			}
			fmt.Fprintln(os.Stderr, "jsrt worker:", err)
			return exitProtocol
		}
		if f.T != "call" || f.Call == nil {
			fmt.Fprintln(os.Stderr, "jsrt worker: unexpected frame", f.T)
			return exitProtocol
		}
		res := e.evaluate(f.Call, c)
		res.RSS = peakRSS()
		if err := c.send(&frame{T: "result", Result: res}); err != nil {
			fmt.Fprintln(os.Stderr, "jsrt worker:", err)
			return exitProtocol
		}
		e.prepareNext()
	}
}
