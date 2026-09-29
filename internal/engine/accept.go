package engine

import (
	"strconv"
	"strings"
)

// Representation is a negotiated read form of a markup document.
type Representation int

const (
	RepHTML      Representation = iota // the composed document or fragment
	RepJSONLD                          // microdata as JSON-LD
	RepMultipart                       // every selector match as multipart/mixed
)

// Negotiate picks the representation of a read (docs/spec/reading-writing.md
// R-RW-36, compat decision): JSON-LD iff its q-value is positive and greater
// than text/html's; with a selector range, the all-matches multipart form
// when multipart/mixed is listed explicitly (not through a wildcard) and
// preferred over both. Anything else, including no Accept, is HTML; a read
// is never refused with 406.
func Negotiate(accept string, selectorRange bool) Representation {
	if strings.TrimSpace(accept) == "" {
		return RepHTML
	}
	qH := AcceptQuality(accept, "text/html", false)
	qJ := AcceptQuality(accept, "application/ld+json", false)
	if selectorRange {
		if qM := AcceptQuality(accept, "multipart/mixed", true); qM > 0 && qM > qH && qM >= qJ {
			return RepMultipart
		}
	}
	if qJ > 0 && qJ > qH {
		return RepJSONLD
	}
	return RepHTML
}

// AcceptQuality returns the q-value an Accept header (RFC 9110 §12.5.1)
// gives media type t: the most specific matching range wins (type/subtype >
// type/* > */*), media-type parameters other than q are ignored, and 0 means
// not acceptable. With explicit set only an exact type/subtype range counts.
// An absent header accepts everything (1), except explicitly.
func AcceptQuality(accept, t string, explicit bool) float64 {
	if strings.TrimSpace(accept) == "" {
		if explicit {
			return 0
		}
		return 1
	}
	t = strings.ToLower(t)
	typ, _, _ := strings.Cut(t, "/")
	best, q := -1, 0.0
	for _, rng := range strings.Split(accept, ",") {
		params := strings.Split(rng, ";")
		mt := strings.ToLower(strings.TrimSpace(params[0]))
		spec := -1
		switch {
		case mt == t:
			spec = 2
		case explicit:
		case mt == typ+"/*":
			spec = 1
		case mt == "*/*":
			spec = 0
		}
		if spec < 0 || spec < best {
			continue
		}
		rq := 1.0
		for _, p := range params[1:] {
			k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
			if strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 && f <= 1 {
					rq = f
				} else {
					rq = 0
				}
			}
		}
		if spec > best {
			best, q = spec, rq
		} else if rq > q {
			q = rq
		}
	}
	return q
}
