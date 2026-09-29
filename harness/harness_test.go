package harness

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A .live sibling's observations never share a file with its case.
func TestObservationName(t *testing.T) {
	for id, want := range map[string]string{
		"modeling.uniqueness.duplicate-across-documents":      "modeling-uniqueness-duplicate-across-doc",
		"modeling.uniqueness.duplicate-across-documents.live": "modeling-uniqueness-duplicate-acros-live",
		"authz.discovery.whitespace-trimmed.live":             "authz-discovery-whitespace-trimmed-live",
		"rw.md.itemref.live":                                  "rw-md-itemref-live",
		"rw.md.itemref":                                       "rw-md-itemref",
		"liquid.compose.range-over-rendered-output":           "liquid-compose-range-over-rendered-outpu",
		"liquid.compose.range-over-rendered-output.live":      "liquid-compose-range-over-rendered-live",
		"liquid.escape.escape-filter.live":                    "liquid-escape-escape-filter-live",
	} {
		if got := ObservationName(id); got != want {
			t.Errorf("ObservationName(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestLocalCases runs every case under harness/cases against a local
// in-process pagelike. Set HARNESS_FILTER to narrow the run.
func TestLocalCases(t *testing.T) {
	cases, errs := Load("cases")
	for _, e := range errs {
		t.Errorf("load: %v", e)
	}
	filter := os.Getenv("HARNESS_FILTER")
	r := &Runner{Target: &LocalTarget{Quiet: true}}
	for _, c := range cases {
		if filter != "" && !strings.Contains(c.ID, filter) && !strings.Contains(c.File, filter) {
			continue
		}
		c := c
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			res := r.Run(context.Background(), c)
			switch res.Outcome {
			case Skip:
				t.Skip(res.Reason)
			case Fail:
				for _, f := range res.Failures {
					t.Error(f)
				}
			}
		})
	}
}
