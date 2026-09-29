package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
)

// Entity tags (docs/spec/reading-writing.md §12, §13; shapes live-observed
// 2026-09-28, docs/compat/decisions.md):
//   - stored-version tag: "<sha256 of the stored bytes>" (store.Tx.Put),
//     which for blobs is also the documented content-hash tag;
//   - element (fragment) tag: "<h>-<h>-<v>" where h is the sha256 of the
//     fragment as served and v the document's stored version, exactly as
//     PageLove forms it: any write to the document changes every element
//     tag, and a POST's tag is its inserted child's;
//   - composed whole-document tag: W/"<sha256 of the served bytes>", weak;
//   - JSON-LD: "<sha256 of the JSON>" for a whole document, W/"…" for an
//     all-matches answer;
//   - an emptied path (whole-document DELETE): the tag of empty content.

// ETagOf returns the strong ETag of a markup string.
func ETagOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// WeakETagOf returns the weak ETag of a representation.
func WeakETagOf(s []byte) string {
	sum := sha256.Sum256(s)
	return `W/"` + hex.EncodeToString(sum[:]) + `"`
}

// EmptyETag is the tag PageLove reports once a document is gone: the hash
// of empty content.
var EmptyETag = ETagOf("")

// FragmentETag returns the tag of a fragment of a document at version.
func FragmentETag(fragment string, version int64) string {
	sum := sha256.Sum256([]byte(fragment))
	h := hex.EncodeToString(sum[:])
	return `"` + h + "-" + h + "-" + strconv.FormatInt(version, 10) + `"`
}

// ElementETag returns the tag of an element (serialized) at version.
func ElementETag(n *html.Node, version int64) string {
	return FragmentETag(dom.OuterHTML(n), version)
}

// ComposedETag is the tag of a whole-document representation that
// composition changed: weak, over the served bytes (R-RW-96).
func ComposedETag(served []byte) string { return WeakETagOf(served) }

// entityTag is one member of an If-Match / If-None-Match list.
type entityTag struct {
	weak   bool
	opaque string // including the double quotes
}

// normalizeTag parses one entity tag. A tag written without quotes is
// compared as if quoted (compat decision R-RW-85: the SSE etag property is
// shown unquoted in the docs and beta-js copies it into If-Match).
func normalizeTag(t string) entityTag {
	t = strings.TrimSpace(t)
	var et entityTag
	if strings.HasPrefix(t, "W/") {
		et.weak, t = true, t[2:]
	}
	if len(t) < 2 || t[0] != '"' || t[len(t)-1] != '"' {
		t = `"` + strings.Trim(t, `"`) + `"`
	}
	et.opaque = t
	return et
}

// parseTagList splits a precondition header into "*" or a list of tags.
// Commas inside a quoted tag do not split it.
func parseTagList(h string) (star bool, tags []entityTag) {
	h = strings.TrimSpace(h)
	if h == "*" {
		return true, nil
	}
	start, quoted := 0, false
	flush := func(end int) {
		if part := strings.TrimSpace(h[start:end]); part != "" {
			if part == "*" {
				star = true
			} else {
				tags = append(tags, normalizeTag(part))
			}
		}
	}
	for i := 0; i < len(h); i++ {
		switch h[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(h))
	return star, tags
}

// IfMatch evaluates an If-Match header (RFC 9110 §13.1.1, strong
// comparison: weak tags never match) against the current tags of a
// resource; exists reports whether the resource exists.
func IfMatch(header string, exists bool, current ...string) bool {
	star, tags := parseTagList(header)
	if !exists {
		return false
	}
	if star {
		return true
	}
	for _, t := range tags {
		if t.weak {
			continue
		}
		for _, c := range current {
			if c == "" {
				continue
			}
			if ct := normalizeTag(c); !ct.weak && ct.opaque == t.opaque {
				return true
			}
		}
	}
	return false
}

// IfNoneMatch reports whether an If-None-Match header matches (RFC 9110
// §13.1.2, weak comparison): true means the condition is false, i.e. the
// request must be answered 304 (reads) or 412 (writes).
func IfNoneMatch(header string, exists bool, current ...string) bool {
	star, tags := parseTagList(header)
	if !exists {
		return false
	}
	if star {
		return true
	}
	for _, t := range tags {
		for _, c := range current {
			if c != "" && normalizeTag(c).opaque == t.opaque {
				return true
			}
		}
	}
	return false
}
