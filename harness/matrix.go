package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RecordedOutcome is one case outcome read from an observations directory.
type RecordedOutcome struct {
	Case     string   `json:"case"`
	Target   string   `json:"target"`
	Outcome  Outcome  `json:"outcome"`
	Failures []string `json:"failures"`
}

// LoadOutcomes reads every observation JSON in dir (missing dir → empty).
func LoadOutcomes(dir string) map[string]RecordedOutcome {
	out := map[string]RecordedOutcome{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r RecordedOutcome
		if json.Unmarshal(b, &r) == nil && r.Case != "" {
			out[r.Case] = r
		}
	}
	return out
}

type matrixRow struct {
	area, feature                string
	cases, localPass, localFail  int
	localSkip, liveRun, livePass int
	liveDiverge, liveKept        int
	evidence                     map[string]int
	disputed                     int
}

// Matrix writes the compatibility matrix as Markdown: one row per area and
// feature, with case counts by evidence level, local results, and live
// PageLove results where recorded.
func Matrix(w io.Writer, cases []*Case, local, live map[string]RecordedOutcome, liveLabel string) {
	rows := map[string]*matrixRow{}
	var keys []string
	hasSibling := map[string]bool{}
	for _, c := range cases {
		if base, ok := strings.CutSuffix(c.ID, ".live"); ok {
			hasSibling[base] = true
		}
	}
	for _, c := range cases {
		area := c.Area
		if area == "" {
			area = strings.SplitN(c.ID, ".", 2)[0]
		}
		feature := c.Feature
		if feature == "" {
			parts := strings.Split(c.ID, ".")
			if len(parts) > 1 {
				feature = parts[1]
			} else {
				feature = "(general)"
			}
		}
		k := area + "\x00" + feature
		r := rows[k]
		if r == nil {
			r = &matrixRow{area: area, feature: feature, evidence: map[string]int{}}
			rows[k] = r
			keys = append(keys, k)
		}
		r.cases++
		ev := c.Evidence
		if ev == "" {
			ev = "unspecified"
		}
		r.evidence[ev]++
		if c.Status == "disputed" {
			r.disputed++
		}
		switch local[c.ID].Outcome {
		case Pass:
			r.localPass++
		case Fail:
			r.localFail++
		default:
			r.localSkip++
		}
		if o, ok := live[c.ID]; ok && c.Live {
			r.liveRun++
			switch {
			case o.Outcome == Pass:
				r.livePass++
			case o.Outcome == XFail || o.Outcome == Fail && (c.Status == StatusDisputed || hasSibling[c.ID]):
				// Expected: a disputed (losing) claim, or pagelike's kept
				// behaviour, whose .live sibling asserts PageLove's.
				r.liveKept++
			case o.Outcome == Fail:
				r.liveDiverge++
			}
		}
	}
	sort.Strings(keys)
	tot := matrixRow{evidence: map[string]int{}}
	fmt.Fprintf(w, "| Area | Feature | Cases | Evidence | Local pass / fail / skip | Live PageLove (%s): match / kept / differ |\n|---|---|---|---|---|---|\n", liveLabel)
	for _, k := range keys {
		r := rows[k]
		var ev []string
		for _, e := range []string{"documented", "client-source", "demo-source", "live-observed", "inferred", "unspecified"} {
			if n := r.evidence[e]; n > 0 {
				ev = append(ev, fmt.Sprintf("%s %d", abbrev(e), n))
			}
		}
		if r.disputed > 0 {
			ev = append(ev, fmt.Sprintf("disputed %d", r.disputed))
		}
		live := "—"
		if r.liveRun > 0 {
			live = fmt.Sprintf("%d / %d / %d", r.livePass, r.liveKept, r.liveDiverge)
		}
		fmt.Fprintf(w, "| %s | %s | %d | %s | %d / %d / %d | %s |\n", r.area, strings.ReplaceAll(r.feature, "|", "\\|"), r.cases, strings.Join(ev, ", "), r.localPass, r.localFail, r.localSkip, live)
		tot.cases += r.cases
		tot.localPass += r.localPass
		tot.localFail += r.localFail
		tot.localSkip += r.localSkip
		tot.liveRun += r.liveRun
		tot.livePass += r.livePass
		tot.liveDiverge += r.liveDiverge
		tot.liveKept += r.liveKept
	}
	fmt.Fprintf(w, "| **Total** | | **%d** | | **%d / %d / %d** | **%d / %d / %d** |\n", tot.cases, tot.localPass, tot.localFail, tot.localSkip, tot.livePass, tot.liveKept, tot.liveDiverge)
	fmt.Fprintf(w, "\nEvidence: doc = documented, client = official client source, demo = official app source, live = observed on PageLove, inf = inferred. Local results: `go run ./harness/cmd/harness run --observations <dir>`. Live results are the latest recorded observation of each case that runs live: \"match\" means PageLove satisfied the case's current expectations when it was last run; \"kept\" means it did not, as expected — a disputed (losing) claim, or pagelike's deliberately kept behaviour, whose .live sibling asserts PageLove's and is counted as a match; \"differ\" is an unexplained difference. See docs/compat/decisions.md and docs/compat/decisions-2026-09-29/.\n")
}

func abbrev(e string) string {
	switch e {
	case "documented":
		return "doc"
	case "client-source":
		return "client"
	case "demo-source":
		return "demo"
	case "live-observed":
		return "live"
	case "inferred":
		return "inf"
	}
	return e
}
