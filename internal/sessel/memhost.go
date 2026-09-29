package sessel

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/dom"
)

// MemHost is an in-memory Host: a set of parsed documents and blobs, an
// optional class registry, clock and write provider. Tests use it; hosts
// over real storage can too when they already hold parsed documents.
type MemHost struct {
	mu      sync.Mutex
	docs    map[string]*Document
	blobs   map[string]*Blob
	sorted  []*Document
	Classes ClassRegistry
	// Glob matches a glob pattern against a path (default GlobMatch).
	Glob  func(pattern, path string) bool
	Clock func() time.Time
	W     Writer
}

// NewMemHost returns an empty in-memory host.
func NewMemHost() *MemHost {
	return &MemHost{docs: map[string]*Document{}, blobs: map[string]*Blob{}}
}

// AddHTML parses and stores an HTML document at path.
func (h *MemHost) AddHTML(path, markup string) (*Document, error) {
	root, err := dom.Parse([]byte(markup))
	if err != nil {
		return nil, err
	}
	d := &Document{Path: path, Type: "text/html", Root: root}
	h.AddDocument(d)
	return d, nil
}

// AddDocument stores a parsed document.
func (h *MemHost) AddDocument(d *Document) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.docs[d.Path] = d
	h.sorted = nil
}

// AddBlob stores a non-markup resource.
func (h *MemHost) AddBlob(path, contentType string, size int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := NewDict()
	m.Set("mimetype", contentType)
	m.Set("size", int64(size))
	h.blobs[path] = &Blob{Path: path, Meta: m}
}

// All returns every document in path order.
func (h *MemHost) All() []*Document {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sorted == nil {
		for _, d := range h.docs {
			h.sorted = append(h.sorted, d)
		}
		sort.Slice(h.sorted, func(i, j int) bool { return h.sorted[i].Path < h.sorted[j].Path })
	}
	return h.sorted
}

// IsGlob reports whether a from-source string contains glob metacharacters.
func IsGlob(p string) bool { return strings.ContainsAny(p, "*?[{") }

func (h *MemHost) Documents(ctx context.Context, pattern string) ([]*Document, error) {
	all := h.All()
	if pattern == "" {
		return all, nil
	}
	if !IsGlob(pattern) {
		h.mu.Lock()
		d := h.docs[pattern]
		h.mu.Unlock()
		if d == nil {
			return nil, nil
		}
		return []*Document{d}, nil
	}
	match := h.Glob
	if match == nil {
		match = GlobMatch
	}
	var out []*Document
	for _, d := range all {
		if match(pattern, d.Path) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (h *MemHost) Resource(ctx context.Context, path string) (Value, error) {
	h.mu.Lock()
	d := h.docs[path]
	b := h.blobs[path]
	h.mu.Unlock()
	switch {
	case d != nil:
		if d.Meta == nil {
			m := NewDict()
			m.Set("mimetype", "text/html")
			d.Meta = m
		}
		return d.Element(), nil
	case b != nil:
		return b, nil
	}
	return nil, nil
}

func (h *MemHost) Class(ctx context.Context, url string) (Class, error) {
	if h.Classes == nil {
		return nil, nil
	}
	return h.Classes.Class(url), nil
}

func (h *MemHost) Writer() Writer { return h.W }

func (h *MemHost) Now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}

// GlobMatch matches the anchored glob language of AuthorizationRule
// resources (R-SESSEL-206): '*' matches any run of characters including
// '/', '?' one character, [..] a class ([!..] negated), {a,b} alternation
// and '\' escapes the next character.
func GlobMatch(pattern, path string) bool {
	for _, p := range expandAlternation(pattern) {
		if globMatch(p, path) {
			return true
		}
	}
	return false
}

func expandAlternation(p string) []string {
	depth, start := 0, -1
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 {
				var out []string
				for _, alt := range splitAlternatives(p[start+1 : i]) {
					out = append(out, expandAlternation(p[:start]+alt+p[i+1:])...)
				}
				return out
			}
		}
	}
	return []string{p}
}

func splitAlternatives(s string) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[last:i])
				last = i + 1
			}
		}
	}
	return append(out, s[last:])
}

func globMatch(p, s string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(p, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
			p, s = p[1:], s[1:]
		case '[':
			if s == "" {
				return false
			}
			end := strings.IndexByte(p[1:], ']')
			if end < 0 {
				if s[0] != '[' {
					return false
				}
				p, s = p[1:], s[1:]
				continue
			}
			class := p[1 : end+1]
			neg := strings.HasPrefix(class, "!") || strings.HasPrefix(class, "^")
			if neg {
				class = class[1:]
			}
			in := false
			for i := 0; i < len(class); i++ {
				if i+2 < len(class) && class[i+1] == '-' {
					if class[i] <= s[0] && s[0] <= class[i+2] {
						in = true
					}
					i += 2
					continue
				}
				if class[i] == s[0] {
					in = true
				}
			}
			if in == neg {
				return false
			}
			p, s = p[end+2:], s[1:]
		case '\\':
			if len(p) > 1 {
				p = p[1:]
			}
			fallthrough
		default:
			if s == "" || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return s == ""
}
