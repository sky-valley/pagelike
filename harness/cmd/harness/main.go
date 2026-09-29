// Command harness runs pagelike's differential compatibility cases.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sky-valley/pagelike/harness"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: harness run|record|list|matrix [--target local|live] [--filter SUBSTR] [--ids ID,ID|@file] [--cases DIR] [--parallel N] [--slow] [--root] [-v]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	target := fs.String("target", "local", "local or live")
	filter := fs.String("filter", "", "only cases whose id or file contains this")
	ids := fs.String("ids", "", "only these cases (comma-separated exact ids, or @file)")
	dir := fs.String("cases", "harness/cases", "case directory")
	par := fs.Int("parallel", 8, "concurrent cases (local)")
	obs := fs.String("observations", "", "directory to write observations")
	verbose := fs.Bool("v", false, "verbose")
	slow := fs.Bool("slow", false, "include slow cases (retention windows, keepalives)")
	localObs := fs.String("local-observations", "harness/observations/local", "matrix: local results directory")
	liveObs := fs.String("live-observations", "harness/observations/live-2026-09-28", "matrix: live results directory")
	root := fs.Bool("root", false, "run root cases (host-wide paths) against the live target")
	fs.Parse(os.Args[2:])

	cases, errs := harness.Load(*dir)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "load:", e)
	}
	want := map[string]bool{}
	if strings.HasPrefix(*ids, "@") {
		// --ids @file reads the comma- or newline-separated list from a file.
		b, err := os.ReadFile(strings.TrimPrefix(*ids, "@"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "ids:", err)
			os.Exit(2)
		}
		*ids = strings.ReplaceAll(string(b), "\n", ",")
	}
	for _, id := range strings.Split(*ids, ",") {
		if id = strings.TrimSpace(id); id != "" && !strings.HasPrefix(id, "#") {
			want[id] = true
		}
	}
	var sel []*harness.Case
	for _, c := range cases {
		if len(want) > 0 && !want[c.ID] {
			continue
		}
		if *filter == "" || strings.Contains(c.ID, *filter) || strings.Contains(c.File, *filter) {
			sel = append(sel, c)
		}
	}
	if cmd == "matrix" {
		local := harness.LoadOutcomes(*localObs)
		live := map[string]harness.RecordedOutcome{}
		for _, d := range strings.Split(*liveObs, ",") { // later directories override earlier ones
			for k, v := range harness.LoadOutcomes(strings.TrimSpace(d)) {
				live[k] = v
			}
		}
		harness.Matrix(os.Stdout, cases, local, live, "recorded runs")
		return
	}
	for id := range want {
		found := false
		for _, c := range sel {
			found = found || c.ID == id
		}
		if !found {
			fmt.Fprintln(os.Stderr, "unknown case id:", id)
			os.Exit(2)
		}
	}
	if cmd == "list" {
		for _, c := range sel {
			fmt.Printf("%s\t%s\t%s\tlive=%v\t%s\tfeature=%s\trequires=%s\tstatus=%s\n", c.ID, c.Area, c.Evidence, c.Live, c.Title, c.Feature, strings.Join(c.Requires, ","), c.Status)
		}
		return
	}
	var t harness.Target
	switch *target {
	case "local":
		t = &harness.LocalTarget{Quiet: !*verbose}
	case "live":
		lt, err := liveTarget()
		if err != nil {
			fmt.Fprintln(os.Stderr, "live target:", err)
			os.Exit(2)
		}
		t = lt
		*par = 1
	default:
		fmt.Fprintln(os.Stderr, "unknown target", *target)
		os.Exit(2)
	}
	r := &harness.Runner{Target: t, Verbose: *verbose, Slow: *slow, AllowRoot: *root}
	results := make([]*harness.Result, len(sel))
	sem := make(chan struct{}, max(*par, 1))
	var wg sync.WaitGroup
	for i, c := range sel {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c *harness.Case) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = r.Run(context.Background(), c)
		}(i, c)
	}
	wg.Wait()
	s := harness.Report(os.Stdout, results, *verbose)
	if *obs != "" || cmd == "record" {
		out := *obs
		if out == "" {
			out = filepath.Join("harness", "observations", *target)
		}
		if err := harness.SaveObservations(out, results); err != nil {
			fmt.Fprintln(os.Stderr, "save:", err)
		}
	}
	if s.Fail > 0 || len(errs) > 0 {
		os.Exit(1)
	}
}

// liveTarget reads .secrets/pagelove.env (KEY=VALUE lines). It refuses to
// run without an explicitly named disposable host.
func liveTarget() (*harness.LiveTarget, error) {
	f, err := os.Open(".secrets/pagelove.env")
	if err != nil {
		return nil, fmt.Errorf("missing .secrets/pagelove.env (see harness/README.md): %w", err)
	}
	defer f.Close()
	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	t := &harness.LiveTarget{PublicHost: vals["PAGELOVE_HOST"], DavURL: vals["PAGELOVE_DAV_URL"], APIKey: vals["PAGELOVE_API_KEY"], RPS: 3, Actors: map[string]string{}}
	if t.PublicHost == "" || t.DavURL == "" || t.APIKey == "" {
		return nil, fmt.Errorf("PAGELOVE_HOST, PAGELOVE_DAV_URL and PAGELOVE_API_KEY are required")
	}
	if vals["PAGELOVE_DISPOSABLE"] != "yes" {
		return nil, fmt.Errorf("set PAGELOVE_DISPOSABLE=yes to confirm %s is a disposable test host", t.PublicHost)
	}
	for k, v := range vals {
		if strings.HasPrefix(k, "PAGELOVE_ACTOR_") && strings.HasSuffix(k, "_COOKIE") {
			name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(k, "PAGELOVE_ACTOR_"), "_COOKIE"))
			t.Actors[name] = v
		}
	}
	return t, nil
}
