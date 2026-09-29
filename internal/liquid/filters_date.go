package liquid

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Date filters (R-LIQ-170 … R-LIQ-178). Every input is normalized to UTC
// and every output is UTC; there are no time zones and no locales.

var isoDateTime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|z|[+-]\d{2}(?::?\d{2})?)?)?$`)

var unixSeconds = regexp.MustCompile(`^[+-]?\d+(?:\.\d+)?$`)

// parseDate reads a date filter's input (R-LIQ-170): an ISO date (midnight
// UTC), an ISO datetime (an offset converts to UTC, no offset means UTC,
// fractions are dropped), Unix seconds (a number or a numeric string),
// "now" (the request instant) or "today" (its midnight). empty is true for
// nil and "", which every date filter renders as "".
func parseDate(v any, now time.Time) (t time.Time, empty bool, err error) {
	switch x := unwrapSafe(v).(type) {
	case nil:
		return time.Time{}, true, nil
	case time.Time:
		return x.UTC(), false, nil
	case string:
		s := strings.TrimSpace(x)
		switch strings.ToLower(s) {
		case "":
			return time.Time{}, true, nil
		case "now":
			return now, false, nil
		case "today":
			y, m, d := now.Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), false, nil
		}
		if unixSeconds.MatchString(s) {
			f, perr := strconv.ParseFloat(s, 64)
			if perr == nil {
				return time.Unix(satTrunc(f), 0).UTC(), false, nil
			}
		}
		if t, ok := parseISO(s); ok {
			return t, false, nil
		}
		return time.Time{}, false, fmt.Errorf("invalid date %q", clipString(s, 60))
	}
	if i, f, isInt, ok := goNumber(v); ok {
		if !isInt {
			i = satTrunc(f)
		}
		return time.Unix(i, 0).UTC(), false, nil
	}
	return time.Time{}, false, fmt.Errorf("invalid date: expected a date string or a timestamp, got %s", describe(v))
}

// parseISO parses YYYY-MM-DD[(T| )HH:MM[:SS[.fraction]][Z|±HH:MM|±HHMM]].
func parseISO(s string) (time.Time, bool) {
	m := isoDateTime.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	num := func(i int) int { n, _ := strconv.Atoi(m[i]); return n }
	y, mo, d, h, mi, sec := num(1), num(2), num(3), num(4), num(5), num(6)
	t := time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d || t.Hour() != h || t.Minute() != mi || t.Second() != sec {
		return time.Time{}, false // out-of-range fields, which time.Date would normalize
	}
	if off := m[7]; off != "" && off != "Z" && off != "z" {
		digits := strings.ReplaceAll(off[1:], ":", "")
		oh, _ := strconv.Atoi(digits[:2])
		om := 0
		if len(digits) >= 4 {
			om, _ = strconv.Atoi(digits[2:4])
		}
		if oh > 23 || om > 59 {
			return time.Time{}, false
		}
		shift := time.Duration(oh)*time.Hour + time.Duration(om)*time.Minute
		if off[0] == '+' {
			t = t.Add(-shift)
		} else {
			t = t.Add(shift)
		}
	}
	return t, true
}

func describe(v any) string {
	switch v.(type) {
	case *Item:
		return "an item"
	case *Hash, map[string]any, *Request:
		return "a hash"
	case bool:
		return "a boolean"
	}
	if isList(v) {
		return "an array"
	}
	return fmt.Sprintf("%T", v)
}

// strftime formats exactly the directives PageLove documents (R-LIQ-171);
// any other directive is copied unchanged, % included.
func strftime(t time.Time, f string) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c != '%' || i+1 >= len(f) {
			b.WriteByte(c)
			continue
		}
		i++
		switch d := f[i]; d {
		case 'Y':
			fmt.Fprintf(&b, "%04d", t.Year())
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 'B':
			b.WriteString(t.Month().String())
		case 'b':
			b.WriteString(t.Month().String()[:3])
		case 'A':
			b.WriteString(t.Weekday().String())
		case 'a':
			b.WriteString(t.Weekday().String()[:3])
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'p':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'Z':
			b.WriteString("UTC")
		case 'z':
			b.WriteString("+0000")
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(d)
		}
	}
	return b.String()
}

const isoUTC = "%Y-%m-%dT%H:%M:%SZ"

func dateFilters() map[string]filterSpec {
	// format builds a filter that formats its input with a fixed layout;
	// extra arguments are ignored (R-LIQ-174).
	format := func(layout string) filterSpec {
		return filterSpec{max: -1, fn: func(c *fcall) (any, error) {
			t, empty, err := parseDate(c.in, c.st.now)
			if err != nil || empty {
				return "", err
			}
			return strftime(t, layout), nil
		}}
	}
	return map[string]filterSpec{
		"date": {max: 1, fn: func(c *fcall) (any, error) {
			t, empty, err := parseDate(c.in, c.st.now)
			if err != nil || empty {
				return "", err
			}
			f := "%Y-%m-%d"
			if c.has(0) && c.args[0] != nil {
				f = toString(c.args[0])
			}
			return strftime(t, f), nil
		}},
		"date_add": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			t, empty, err := parseDate(c.in, c.st.now)
			if err != nil || empty {
				return "", err
			}
			i, f, isInt, ok := number(c.args[0])
			if !ok {
				return nil, filterError("seconds must be a number, got %q", clipString(toString(c.args[0]), 40))
			}
			d := time.Duration(i) * time.Second
			if !isInt {
				d = time.Duration(f * float64(time.Second))
			}
			return strftime(t.Add(d), isoUTC), nil
		}},
		"unix_to_iso":         format(isoUTC),
		"date_to_string":      format("%d %b %Y"),
		"date_to_long_string": format("%d %B %Y"),
		"date_to_rfc822":      format("%a, %d %b %Y %H:%M:%S +0000"),
		"date_to_xmlschema":   format("%Y-%m-%dT%H:%M:%S+00:00"),
	}
}
