package engine

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

// CheckCanonical refuses a request path that names a document in more than
// one way, before any rule is read (docs AuthorizationRule §resource,
// snapshot 2026-11-23; R-RW-154). raw is the path as sent, still
// percent-encoded. Not canonical: an empty segment (except one trailing
// '/'), a "." or ".." segment, a backslash, an encoded '/', '\' or '.', an
// encoded letter, digit, '-', '_' or '~', NUL raw or encoded, a raw control
// byte, and escapes that do not decode to UTF-8. The message names the
// reason, never the path.
func CheckCanonical(raw string) error {
	if raw == "" || raw[0] != '/' {
		return invalidPath("the path must start with /")
	}
	var dec []byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == 0:
			return invalidPath("the path holds a NUL byte")
		case c < 0x20 || c == 0x7f:
			return invalidPath("the path holds a control character")
		case c == '\\':
			return invalidPath("the path holds a backslash")
		case c == '%':
			if i+2 >= len(raw) || !isHex(raw[i+1]) || !isHex(raw[i+2]) {
				return invalidPath("the path holds a malformed percent-escape")
			}
			b := unhex(raw[i+1])<<4 | unhex(raw[i+2])
			switch {
			case b == 0:
				return invalidPath("the path holds a NUL byte")
			case b == '/' || b == '\\' || b == '.':
				return invalidPath("the path holds an encoded /, \\ or .")
			case b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '~':
				return invalidPath("the path encodes a character that needs no encoding")
			}
			dec = append(dec, b)
			i += 2
		default:
			dec = append(dec, c)
		}
	}
	if !utf8.Valid(dec) {
		return invalidPath("the path does not decode to UTF-8")
	}
	segs := strings.Split(raw[1:], "/")
	for i, seg := range segs {
		switch {
		case seg == "" && i == len(segs)-1:
			// one trailing '/' addresses a directory (or the root)
		case seg == "":
			return invalidPath("the path has an empty segment")
		case seg == "." || seg == "..":
			return invalidPath("the path has a . or .. segment")
		}
	}
	return nil
}

func invalidPath(reason string) error {
	e := errdoc.Problems(http.StatusBadRequest, "InvalidPath", "Invalid path: %s", reason)
	e.ItemType = "https://pagelove.org/Error/InvalidPath"
	return e
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
