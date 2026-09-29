package engine

import (
	"errors"
)

// Entry points kept from the vertical slice so packages written against it
// keep building; new code should use the replacements named below.

// ErrNotFound is returned when a document is missing.
var ErrNotFound = errors.New("not found")

// MatchETag reports whether an If-None-Match style header matches the
// current tags (weak comparison, "*" matches an existing resource).
//
// Deprecated: use IfMatch (strong) or IfNoneMatch (weak).
func MatchETag(header string, exists bool, current ...string) bool {
	return IfNoneMatch(header, exists, current...)
}

// WantsAllMatches reports whether a selector read asks for every match.
//
// Deprecated: use Negotiate.
func WantsAllMatches(accept string) bool { return Negotiate(accept, true) != RepHTML }

// WantsJSONLD reports whether JSON-LD was negotiated.
//
// Deprecated: use Negotiate.
func WantsJSONLD(accept string) bool { return Negotiate(accept, false) == RepJSONLD }
