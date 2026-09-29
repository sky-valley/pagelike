package schema

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/atom"
)

var metaAtom = atom.Meta

// Primitive type URLs (R-MOD-21).
const (
	TypeText          = "https://schema.host/Text"
	TypeString        = "https://schema.host/String"
	TypeURL           = "https://schema.host/URL"
	TypeNumber        = "https://schema.host/Number"
	TypeInteger       = "https://schema.host/Integer"
	TypeFloatingPoint = "https://schema.host/FloatingPoint"
	TypeBoolean       = "https://schema.host/Boolean"
	TypeDateTime      = "https://schema.host/DateTime"
	TypeDate          = "https://schema.host/Date"
	TypeCardinal      = "https://schema.host/Cardinal"
)

// isPrimitive reports whether t is one of the primitive type URLs.
func isPrimitive(t string) bool {
	switch t {
	case TypeText, TypeString, TypeURL, TypeNumber, TypeInteger, TypeFloatingPoint, TypeBoolean, TypeDateTime, TypeDate, TypeCardinal:
		return true
	}
	return false
}

var (
	// f64 syntax without whitespace, hex or separators.
	numberRE = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
	infNaNRE = regexp.MustCompile(`^[+-]?(?i:inf|infinity|nan)$`)
	intRE    = regexp.MustCompile(`^[+-]?[0-9]+$`)
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)
	// RFC 3339 date-time; the calendar and clock are checked separately.
	dateTimeRE = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$`)
	dateRE     = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})$`)
)

// checkPrimitive validates v against a primitive type (R-MOD-21/22).
// Values are verbatim; the empty string passes only Text/String.
func checkPrimitive(t, v string) bool {
	switch t {
	case TypeText, TypeString:
		return true
	case TypeURL:
		if v == "" || !schemeRE.MatchString(v) {
			return false
		}
		u, err := url.Parse(v)
		return err == nil && u.IsAbs()
	case TypeNumber, TypeFloatingPoint:
		if infNaNRE.MatchString(v) {
			return true
		}
		if !numberRE.MatchString(v) {
			return false
		}
		_, err := strconv.ParseFloat(v, 64)
		return err == nil || isRangeErr(err)
	case TypeInteger:
		if !intRE.MatchString(v) {
			return false
		}
		_, err := strconv.ParseInt(v, 10, 64)
		return err == nil
	case TypeBoolean:
		return v == "true" || v == "false"
	case TypeDateTime:
		m := dateTimeRE.FindStringSubmatch(v)
		if m == nil || !validDate(m[1], m[2], m[3]) {
			return false
		}
		h, _ := strconv.Atoi(m[4])
		mi, _ := strconv.Atoi(m[5])
		s, _ := strconv.Atoi(m[6])
		if h > 23 || mi > 59 || s > 60 {
			return false
		}
		if m[8] != "Z" {
			oh, _ := strconv.Atoi(m[8][1:3])
			om, _ := strconv.Atoi(m[8][4:6])
			if oh > 23 || om > 59 {
				return false
			}
		}
		return true
	case TypeDate:
		m := dateRE.FindStringSubmatch(v)
		return m != nil && validDate(m[1], m[2], m[3])
	case TypeCardinal:
		return validCardinality(v)
	}
	return true
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// validDate reports whether year-month-day is a real calendar date.
func validDate(ys, ms, ds string) bool {
	y, _ := strconv.Atoi(ys)
	m, _ := strconv.Atoi(ms)
	d, _ := strconv.Atoi(ds)
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	return d <= time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// validCardinality reports whether s is one of the four cardinalities.
func validCardinality(s string) bool {
	switch s {
	case "0..1", "1..1", "0..n", "1..n":
		return true
	}
	return false
}

// cardinalityOK checks a count of values against a cardinality ("" is 0..n).
func cardinalityOK(card string, n int) bool {
	switch card {
	case "0..1":
		return n <= 1
	case "1..1":
		return n == 1
	case "1..n":
		return n >= 1
	}
	return true
}

// cardinalityPhrase describes what a cardinality allows, in PageLove's
// words ("exactly 1 value", live 2026-09-29; the other two are inferred).
func cardinalityPhrase(card string) string {
	switch card {
	case "0..1":
		return "at most 1 value"
	case "1..1":
		return "exactly 1 value"
	case "1..n":
		return "at least 1 value"
	}
	return "any number of values"
}

// cardinalityDetail is PageLove's cardinality failure text:
// "cardinality 1..1 violated: expected exactly 1 value, found 0".
func cardinalityDetail(card string, n int) string {
	return fmt.Sprintf("cardinality %s violated: expected %s, found %d", card, cardinalityPhrase(card), n)
}

// isMulti reports whether reads of a property with this declared
// cardinality are lists (R-MOD-20: only an explicit 0..n or 1..n).
func isMulti(card string) bool { return card == "0..n" || card == "1..n" }

func quoteList(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = strconv.Quote(v)
	}
	return strings.Join(q, ", ")
}
