// Package harness runs differential compatibility cases (see README.md)
// against a local pagelike instance or a live PageLove host.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Case is one compatibility case.
type Case struct {
	ID         string            `yaml:"id"`
	Title      string            `yaml:"title"`
	Area       string            `yaml:"area"`
	Feature    string            `yaml:"feature"`
	Evidence   string            `yaml:"evidence"`
	Source     string            `yaml:"source"`
	Confidence string            `yaml:"confidence"`
	Requires   []string          `yaml:"requires"`
	Live       bool              `yaml:"live"`
	Root       bool              `yaml:"root"`
	Hosted     bool              `yaml:"hosted"` // native managed-host boundary, local only
	Status     string            `yaml:"status"` // "disputed", "skip", …
	Notes      string            `yaml:"notes"`
	Site       SiteSetup         `yaml:"site"`
	Actors     map[string]Actor  `yaml:"actors"`
	Steps      []Step            `yaml:"steps"`
	Vars       map[string]string `yaml:"vars"`

	File string `yaml:"-"`
}

// SiteSetup is the initial state.
type SiteSetup struct {
	Settings map[string]any `yaml:"settings"`
	Files    []File         `yaml:"files"`
	Rules    []RuleSpec     `yaml:"rules"`
}

// File is an initial document.
type File struct {
	Path        string `yaml:"path"`
	ContentType string `yaml:"content_type"`
	Body        string `yaml:"body"`
	BodyFile    string `yaml:"body_file"`
}

// RuleSpec is shorthand for an AuthorizationRule.
type RuleSpec struct {
	Actor    StrList `yaml:"actor"`
	Resource StrList `yaml:"resource"`
	Method   StrList `yaml:"method"`
	Selector string  `yaml:"selector"`
	Action   string  `yaml:"action"`
}

// Actor is a named identity.
type Actor struct {
	Sub           string   `yaml:"sub"`
	Email         string   `yaml:"email"`
	EmailVerified bool     `yaml:"email_verified"`
	Name          string   `yaml:"name"`
	Roles         []string `yaml:"roles"`
}

// Step is one action in a case.
type Step struct {
	Publish *PublishedBundle  `yaml:"publish"` // local managed-host installation
	Name    string            `yaml:"name"`
	As      string            `yaml:"as"`
	Plane   string            `yaml:"plane"`
	Request *Request          `yaml:"request"`
	Expect  *Expect           `yaml:"expect"`
	Capture map[string]string `yaml:"capture"`
	Repeat  *RepeatSpec       `yaml:"repeat"`

	SSEOpen       *SSEOpen    `yaml:"sse_open"`
	SSEExpect     *SSEExpect  `yaml:"sse_expect"`
	SSEExpectNone *SSEExpect  `yaml:"sse_expect_none"`
	SSEClose      *SSEClose   `yaml:"sse_close"`
	SSEComment    *SSEComment `yaml:"sse_expect_comment"`
	SSEClosed     *SSEClose   `yaml:"sse_expect_closed"`
	SSEPause      *SSEClose   `yaml:"sse_pause"`
	SSEResume     *SSEClose   `yaml:"sse_resume"`
	SessionCtl    *SessionCtl `yaml:"session_control"`
	Parallel      []Step      `yaml:"parallel"`
	SleepMS       int         `yaml:"sleep_ms"`
	Restart       bool        `yaml:"restart"` // local only: restart the server process state
	// PruneEvents (local only, capability sse-control) drops every stored
	// stream event, as if the retention window had passed for all of them.
	PruneEvents bool `yaml:"prune_events"`

	// Outbound capture (${SINK}; sink.go).
	SinkConfig      *SinkConfig      `yaml:"sink_config"`
	SinkExpect      *SinkExpect      `yaml:"sink_expect"`
	SinkExpectNone  *SinkExpectNone  `yaml:"sink_expect_none"`
	SinkExpectCount *SinkExpectCount `yaml:"sink_expect_count"`

	// Extra captures unknown keys so unsupported step kinds fail loudly.
	Extra map[string]any `yaml:",inline"`
}

type PublishedBundle struct {
	Participation bool              `yaml:"participation"`
	Deleted       bool              `yaml:"deleted"`
	Version       string            `yaml:"version"`
	Generation    int64             `yaml:"generation"`
	Files         map[string]string `yaml:"files"`
}

// Request is an HTTP request.
type Request struct {
	Method   string            `yaml:"method"`
	Path     string            `yaml:"path"`
	Headers  map[string]string `yaml:"headers"`
	Body     string            `yaml:"body"`
	BodyFile string            `yaml:"body_file"`
}

// Expect lists assertions on a response.
type Expect struct {
	Status          IntList           `yaml:"status"`
	Headers         map[string]string `yaml:"headers"`
	HeadersPresent  []string          `yaml:"headers_present"`
	HeadersAbsent   []string          `yaml:"headers_absent"`
	HeaderMatches   map[string]string `yaml:"header_matches"`
	HeaderContains  map[string]string `yaml:"header_contains"`
	Body            *string           `yaml:"body"`
	BodyHTML        *string           `yaml:"body_html"`
	BodyContains    StrList           `yaml:"body_contains"`
	BodyNotContains StrList           `yaml:"body_not_contains"`
	BodyMatches     string            `yaml:"body_matches"`
	BodyJSON        any               `yaml:"body_json"`
	BodyEmpty       bool              `yaml:"body_empty"`
	Microdata       any               `yaml:"microdata"`
	ETagEquals      string            `yaml:"etag_equals"`
	ETagDiffers     string            `yaml:"etag_differs"`
	MultipartParts  *int              `yaml:"multipart_parts"`
}

// SSEOpen opens a stream.
type SSEOpen struct {
	Name        string            `yaml:"name"`
	Path        string            `yaml:"path"`
	As          string            `yaml:"as"`
	LastEventID string            `yaml:"last_event_id"`
	Headers     map[string]string `yaml:"headers"`
	Expect      *Expect           `yaml:"expect"`
}

// SSEExpect waits for (or asserts the absence of) an event.
type SSEExpect struct {
	Name            string            `yaml:"name"`
	Event           string            `yaml:"event"`
	WithinMS        int               `yaml:"within_ms"`
	ForMS           int               `yaml:"for_ms"`
	DataContains    StrList           `yaml:"data_contains"`
	DataNotContains StrList           `yaml:"data_not_contains"`
	DataMicrodata   map[string]any    `yaml:"data_microdata"`
	DataMatches     string            `yaml:"data_matches"`
	IDMatches       string            `yaml:"id_matches"`
	IDAbsent        bool              `yaml:"id_absent"`
	Next            bool              `yaml:"next"`
	HasID           *bool             `yaml:"has_id"`
	Capture         map[string]string `yaml:"capture"`
}

// SSEClose names a stream (close / expect-closed / pause / resume).
type SSEClose struct {
	Name     string `yaml:"name"`
	WithinMS int    `yaml:"within_ms"`
}

// SSEComment waits for a comment line.
type SSEComment struct {
	Name     string `yaml:"name"`
	Text     string `yaml:"text"`
	WithinMS int    `yaml:"within_ms"`
}

// SessionCtl manipulates an actor's session (local only).
type SessionCtl struct {
	Actor  string `yaml:"actor"`
	Action string `yaml:"action"` // expire | invalidate
}

// RepeatSpec is either an int (repeat this request step) or
// {count, step} (repeat an embedded step).
type RepeatSpec struct {
	Count int   `yaml:"count"`
	Step  *Step `yaml:"step"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (r *RepeatSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&r.Count)
	}
	type plain RepeatSpec
	return n.Decode((*plain)(r))
}

// IntList accepts an int or a list of ints.
type IntList []int

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *IntList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var xs []int
		if err := n.Decode(&xs); err != nil {
			return err
		}
		*l = xs
		return nil
	}
	var x int
	if err := n.Decode(&x); err != nil {
		return err
	}
	*l = IntList{x}
	return nil
}

// StrList accepts a string or a list of strings.
type StrList []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *StrList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var xs []string
		if err := n.Decode(&xs); err != nil {
			return err
		}
		*l = xs
		return nil
	}
	var x string
	if err := n.Decode(&x); err != nil {
		return err
	}
	*l = StrList{x}
	return nil
}

// Load reads every case under dir (recursively), in path order. Files may
// hold one case, a list of cases, or {cases: [...]}. Files named
// expressions.yaml hold expression tests and are skipped here.
func Load(dir string) ([]*Case, []error) {
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) && filepath.Base(p) != "expressions.yaml" {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	var out []*Case
	var errs []error
	for _, f := range files {
		cs, err := LoadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, cs...)
	}
	return out, errs
}

// LoadFile parses one case file.
func LoadFile(f string) ([]*Case, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", f, err)
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	doc := root.Content[0]
	var cases []*Case
	switch {
	case doc.Kind == yaml.SequenceNode:
		if err := doc.Decode(&cases); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
	case doc.Kind == yaml.MappingNode && hasKey(doc, "cases"):
		var w struct {
			Defaults Case    `yaml:"defaults"`
			Cases    []*Case `yaml:"cases"`
		}
		if err := doc.Decode(&w); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, c := range w.Cases {
			inheritDefaults(c, &w.Defaults)
		}
		cases = w.Cases
	default:
		var c Case
		if err := doc.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		cases = []*Case{&c}
	}
	for i, c := range cases {
		c.File = f
		if c.ID == "" {
			c.ID = fmt.Sprintf("%s#%d", filepath.Base(f), i)
		}
	}
	return cases, nil
}

func hasKey(n *yaml.Node, k string) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == k {
			return true
		}
	}
	return false
}

func inheritDefaults(c, d *Case) {
	if c.Area == "" {
		c.Area = d.Area
	}
	if c.Evidence == "" {
		c.Evidence = d.Evidence
	}
	if c.Source == "" {
		c.Source = d.Source
	}
	if c.Confidence == "" {
		c.Confidence = d.Confidence
	}
	if len(c.Requires) == 0 {
		c.Requires = d.Requires
	}
	if len(c.Site.Files) == 0 && len(c.Site.Rules) == 0 && c.Site.Settings == nil {
		c.Site = d.Site
	}
	if c.Actors == nil {
		c.Actors = d.Actors
	}
}

// Requires reports whether the case needs capability cap.
func (c *Case) Needs(cap string) bool {
	for _, r := range c.Requires {
		if r == cap {
			return true
		}
	}
	return false
}
