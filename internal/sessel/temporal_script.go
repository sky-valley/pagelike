package sessel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Server JavaScript's Temporal global (docs/spec/javascript.md R-JS-43; docs
// snapshot 2026-11-23, "Dates and times with Temporal"). The docs say it
// "uses the same date and time engine as Sessel's Temporal types", so the
// JavaScript classes are thin: every constructor, static and method call
// comes here as one JSON request, and this file applies the rules the docs
// give for JavaScript (the TC39 error classes, refused overflow, balanced
// durations, the unsupported parts) on top of the Sessel engine.

// JSTemporalError is an error JavaScript sees as a RangeError or TypeError.
type JSTemporalError struct{ Name, Message string }

func (e *JSTemporalError) Error() string { return e.Name + ": " + e.Message }

func jsRange(format string, a ...any) error {
	return &JSTemporalError{"RangeError", fmt.Sprintf(format, a...)}
}

func jsType(format string, a ...any) error {
	return &JSTemporalError{"TypeError", fmt.Sprintf(format, a...)}
}

// jsErr turns an engine error into a RangeError: what the engine refuses
// (a string it cannot read, a field out of range) is a RangeError in
// JavaScript.
func jsErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*JSTemporalError); ok {
		return err
	}
	if e, ok := AsError(err); ok {
		return jsRange("%s", e.Message)
	}
	return jsRange("%s", err.Error())
}

type (
	// jsBag is a property bag: a JavaScript object that is not a Temporal
	// value.
	jsBag map[string]any
	// jsBig is a BigInt.
	jsBig string
	// jsOther is a function or a symbol.
	jsOther string
)

type jsReq struct {
	K string  `json:"k"` // type name, or "Now"
	M string  `json:"m"` // member
	S *string `json:"s"` // the receiver's text; nil for a static
	A []any   `json:"a"`
}

// JSTemporal runs one request of the JavaScript Temporal global and
// returns its JSON result.
func JSTemporal(req []byte, now time.Time) (res []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, jsRange("the Temporal value is out of range")
		}
	}()
	return jsTemporal(req, now)
}

func jsTemporal(req []byte, now time.Time) ([]byte, error) {
	var r jsReq
	dec := json.NewDecoder(bytes.NewReader(req))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return nil, jsType("bad Temporal request")
	}
	args := make([]any, len(r.A))
	for i, a := range r.A {
		v, err := jsDecode(a)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	var (
		out any
		err error
	)
	switch {
	case r.K == "Now":
		out, err = jsNow(r.M, args, now)
	case r.S == nil:
		out, err = jsStatic(r.K, r.M, args)
	default:
		var self temporal
		if self, err = jsSelf(r.K, *r.S); err == nil {
			out, err = jsMethod(self, r.M, args)
		}
	}
	if err != nil {
		return nil, jsErr(err)
	}
	return json.Marshal(jsEncode(out))
}

// JSTemporalValue reads the text a JavaScript Temporal object of the given
// kind carries back into a Sessel value.
func JSTemporalValue(kind, text string) (Value, error) {
	t, err := jsSelf(kind, text)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// JSTemporalText is the text a JavaScript Temporal object carries for v,
// and its kind; ok is false when v is not a temporal value.
func JSTemporalText(v Value) (kind, text string, ok bool) {
	t, isT := v.(temporal)
	if !isT {
		return "", "", false
	}
	return t.temporalType(), jsString(t), true
}

func jsSelf(kind, s string) (temporal, error) {
	if kind == "Duration" {
		d, err := parseDuration(s)
		return d, err
	}
	v, err := temporalFromString(kind, s)
	if err != nil {
		return nil, err
	}
	return v.(temporal), nil
}

func jsDecode(raw any) (any, error) {
	switch x := raw.(type) {
	case nil, string, bool:
		return x, nil
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil, jsRange("number out of range")
		}
		return f, nil
	case map[string]any:
		if k, ok := x["$t"].(string); ok {
			s, _ := x["s"].(string)
			return jsSelf(k, s)
		}
		if s, ok := x["$big"].(string); ok {
			return jsBig(s), nil
		}
		if s, ok := x["$n"].(string); ok {
			switch s {
			case "Infinity":
				return math.Inf(1), nil
			case "-Infinity":
				return math.Inf(-1), nil
			}
			return math.NaN(), nil
		}
		if s, ok := x["$x"].(string); ok {
			return jsOther(s), nil
		}
		bag := jsBag{}
		if o, ok := x["$o"].(map[string]any); ok {
			for k, v := range o {
				d, err := jsDecode(v)
				if err != nil {
					return nil, err
				}
				bag[k] = d
			}
		}
		return bag, nil
	}
	return jsOther("object"), nil
}

func jsEncode(v any) any {
	switch x := v.(type) {
	case temporal:
		return map[string]any{"$t": x.temporalType(), "s": jsString(x), "f": jsFields(x)}
	case jsBig:
		return map[string]any{"$big": string(x)}
	case int:
		return int64(x)
	}
	return v
}

// jsString is toString(): Sessel's text, except that JavaScript writes a
// PlainMonthDay as 09-30.
func jsString(t temporal) string {
	switch x := t.(type) {
	case PlainMonthDay:
		return pad(x.M, 2) + "-" + pad(x.D, 2)
	case Duration:
		// A duration's time is one length (docs "Durations are balanced").
		if b, err := jsDur(jsDurFields(x)); err == nil {
			return b.String()
		}
	}
	return t.String()
}

var (
	jsDateFields = []string{"year", "month", "monthCode", "day", "dayOfWeek", "dayOfYear", "weekOfYear", "yearOfWeek", "daysInWeek", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"}
	jsTimeFields = []string{"hour", "minute", "second", "millisecond", "microsecond", "nanosecond"}
)

// jsFields is every getter of a value.
func jsFields(t temporal) map[string]any {
	f := map[string]any{}
	add := func(names ...string) {
		for _, n := range names {
			if v, err := temporalAccessor(t, n); err == nil {
				f[n] = v
			}
		}
	}
	switch x := t.(type) {
	case PlainDate:
		add(jsDateFields...)
		f["calendarId"] = "iso8601"
	case PlainTime:
		add(jsTimeFields...)
	case PlainDateTime:
		add(jsDateFields...)
		add(jsTimeFields...)
		f["calendarId"] = "iso8601"
	case ZonedDateTime:
		add(jsDateFields...)
		add(jsTimeFields...)
		add("timeZoneId", "offset", "epochMilliseconds")
		f["epochNanoseconds"] = jsEncode(jsBig(strconv.FormatInt(x.NS, 10)))
		f["calendarId"] = "iso8601"
	case Instant:
		add("epochMilliseconds")
		f["epochNanoseconds"] = jsEncode(jsBig(strconv.FormatInt(x.NS, 10)))
	case Duration:
		add(durationUnits...)
		add("sign", "blank")
	case PlainYearMonth:
		add("year", "month", "monthCode", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear")
		f["calendarId"] = "iso8601"
	case PlainMonthDay:
		add("monthCode", "day")
		f["calendarId"] = "iso8601"
	}
	return f
}

// jsBagOf is a Temporal value read as a property bag, as the standard
// reads one type where it wants another (through its getters).
func jsBagOf(t temporal) jsBag {
	b := jsBag{}
	for k, v := range jsFields(t) {
		switch x := v.(type) {
		case int64:
			b[k] = float64(x)
		case string:
			b[k] = x
		}
	}
	delete(b, "calendarId")
	if md, ok := t.(PlainMonthDay); ok {
		b["month"] = float64(md.M)
	}
	return b
}

// ------------------------------------------------------------- arguments

const jsMaxSafe = 1<<53 - 1

func jsArg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

// jsNum is ToNumber for the values that reach here.
func jsNum(v any, what string) (float64, error) {
	switch x := v.(type) {
	case nil:
		return math.NaN(), nil
	case float64:
		return x, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN(), nil
		}
		return f, nil
	case jsBig:
		return 0, jsType("%s must be a number, not a BigInt", what)
	case temporal:
		return 0, jsType("%s must be a number, not a Temporal value", what)
	case jsOther:
		if x == "symbol" {
			return 0, jsType("%s must be a number, not a symbol", what)
		}
	}
	return math.NaN(), nil
}

// jsInt is the standard's ToIntegerWithTruncation, with the docs' limit of
// 2^53 - 1 on every field.
func jsInt(v any, what string) (int64, error) {
	f, err := jsNum(v, what)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, jsRange("%s must be a finite number", what)
	}
	f = math.Trunc(f)
	if math.Abs(f) > jsMaxSafe {
		return 0, jsRange("%s is larger than 2^53 - 1", what)
	}
	return int64(f), nil
}

func jsIntOr(v any, def int64, what string) (int64, error) {
	if v == nil {
		return def, nil
	}
	return jsInt(v, what)
}

// jsWhole reads a duration field, which must be a whole number.
func jsWhole(v any, what string) (int64, error) {
	if v == nil {
		return 0, nil
	}
	f, err := jsNum(v, what)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, jsRange("%s must be a whole number", what)
	}
	if math.Abs(f) > jsMaxSafe {
		return 0, jsRange("%s is larger than 2^53 - 1", what)
	}
	return int64(f), nil
}

// jsOptions reads an options argument: undefined or an object.
func jsOptions(v any) (jsBag, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case jsBag:
		return x, nil
	case temporal:
		return jsBag{}, nil
	}
	return nil, jsType("options must be an object")
}

// jsUnsupported are the options of the standard the engine does not have.
var jsUnsupported = []string{"smallestUnit", "largestUnit", "roundingIncrement", "roundingMode", "fractionalSecondDigits", "calendarName", "timeZoneName", "disambiguation", "relativeTo", "offset"}

func jsNoOptions(o jsBag, more ...string) error {
	for _, k := range append(more, jsUnsupported...) {
		if v, ok := o[k]; ok && v != nil {
			return jsRange("the %s option is not supported", k)
		}
	}
	return nil
}

// jsOverflow accepts only the overflow behaviour the engine has: "reject"
// for from(), with() and the conversions, "constrain" for add and subtract.
func jsOverflow(v any, want string) (jsBag, error) {
	o, err := jsOptions(v)
	if err != nil {
		return nil, err
	}
	if err := jsNoOptions(o); err != nil {
		return nil, err
	}
	if ov, ok := o["overflow"]; ok && ov != nil {
		if s, _ := ov.(string); s != want {
			return nil, jsRange("overflow: only %q is supported here", want)
		}
	}
	return o, nil
}

func jsCalendarArg(v any) error {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if strings.EqualFold(x, "iso8601") {
			return nil
		}
		return jsRange("the only calendar is iso8601")
	}
	return jsType("a calendar is a string")
}

// jsBagCalendar checks a property bag's calendar: "iso8601", an ISO 8601
// string or a Temporal value that has a calendar.
func jsBagCalendar(b jsBag) error {
	switch x := b["calendar"].(type) {
	case nil:
		return nil
	case string:
		if strings.EqualFold(x, "iso8601") {
			return nil
		}
		if c, err := jsStripCalendar(x, true); err == nil {
			if _, err := parseISO(c); err == nil {
				return nil
			}
		}
		return jsRange("the only calendar is iso8601")
	case PlainDate, PlainDateTime, ZonedDateTime, PlainYearMonth, PlainMonthDay:
		return nil
	}
	return jsType("a calendar is a string or a Temporal value that has a calendar")
}

// ----------------------------------------------------------------- values

// The years a value may have (docs: "Years run from -9999 to 9999"), and
// the years an exact time can have: the engine keeps an instant as signed
// 64-bit nanoseconds since 1970, which ends in 2262.
const (
	jsMinYear, jsMaxYear         = -9999, 9999
	jsMinExactYear, jsMaxExactYr = 1678, 2261
)

func jsCheckDate(d PlainDate) (PlainDate, error) {
	if d.Y < jsMinYear || d.Y > jsMaxYear {
		return d, jsRange("the year %d is outside -9999 to 9999", d.Y)
	}
	return d, nil
}

func jsDate(y, m, d int64) (PlainDate, error) {
	if y < jsMinYear || y > jsMaxYear {
		return PlainDate{}, jsRange("the year %d is outside -9999 to 9999", y)
	}
	if m < 1 || m > 12 {
		return PlainDate{}, jsRange("the month %d is outside 1 to 12", m)
	}
	if d < 1 || d > int64(daysInMonth(int(y), int(m))) {
		return PlainDate{}, jsRange("the day %d does not exist in %d-%s", d, y, pad(int(m), 2))
	}
	return PlainDate{int(y), int(m), int(d)}, nil
}

func jsTime(f [6]int64) (PlainTime, error) {
	hi := [6]int64{23, 59, 59, 999, 999, 999}
	for i, v := range f {
		if v < 0 || v > hi[i] {
			return PlainTime{}, jsRange("the %s %d is outside 0 to %d", jsTimeFields[i], v, hi[i])
		}
	}
	return PlainTime{H: int(f[0]), Mi: int(f[1]), S: int(f[2]), Ns: int(f[3]*1_000_000 + f[4]*1_000 + f[5])}, nil
}

func jsTimeParts(t PlainTime) [6]int64 {
	return [6]int64{int64(t.H), int64(t.Mi), int64(t.S), int64(t.Ns / 1_000_000), int64(t.Ns / 1_000 % 1_000), int64(t.Ns % 1_000)}
}

// jsExact is the instant of a wall time in a zone.
func jsExact(dt PlainDateTime, loc *time.Location) (int64, error) {
	if dt.Date.Y < jsMinExactYear || dt.Date.Y > jsMaxExactYr {
		return 0, jsRange("an exact time is kept for the years %d to %d", jsMinExactYear, jsMaxExactYr)
	}
	return resolveWall(dt, loc), nil
}

func jsAddNS(a, b int64) (int64, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, jsRange("the exact time is out of range")
	}
	return s, nil
}

// jsDur checks a duration's fields and balances its time, as the docs'
// "Durations are balanced" and "Ranges" give.
func jsDur(f [10]int64) (Duration, error) {
	sign := 0
	for _, v := range f {
		s := 0
		if v > 0 {
			s = 1
		} else if v < 0 {
			s = -1
		}
		if s != 0 && sign != 0 && s != sign {
			return Duration{}, jsRange("a duration's fields must all have the same sign")
		}
		if s != 0 {
			sign = s
		}
	}
	for i := 0; i < 4; i++ {
		if f[i] < math.MinInt32 || f[i] > math.MaxInt32 {
			return Duration{}, jsRange("a duration's %s must fit a signed 32-bit integer", durationUnits[i])
		}
	}
	sizes := []int64{nsPerHour, nsPerMin, nsPerSec, 1_000_000, 1_000, 1}
	total := new(big.Int)
	for i, size := range sizes {
		total.Add(total, new(big.Int).Mul(big.NewInt(f[4+i]), big.NewInt(size)))
	}
	if !total.IsInt64() {
		return Duration{}, jsRange("a duration's time is out of range")
	}
	withDays := new(big.Int).Add(total, new(big.Int).Mul(big.NewInt(f[3]), big.NewInt(nsPerDay)))
	limit := new(big.Int).Mul(big.NewInt(1<<53), big.NewInt(nsPerSec))
	if withDays.CmpAbs(limit) >= 0 {
		return Duration{}, jsRange("a duration's days and time must be under 2^53 seconds")
	}
	d := balanceNanos(total.Int64(), 4)
	d.Years, d.Months, d.Weeks, d.Days = f[0], f[1], f[2], f[3]
	return d, nil
}

func jsDurFields(d Duration) [10]int64 {
	var f [10]int64
	copy(f[:], d.fields())
	return f
}

// ---------------------------------------------------------------- strings

var (
	jsCalRE      = regexp.MustCompile(`\[!?u-ca=([^\]]*)\]`)
	jsKeyAnnotRE = regexp.MustCompile(`\[!?[^\]=]+=[^\]]*\]`)
	jsFracHMRE   = regexp.MustCompile(`[.,]\d+[HM]`)
	jsOffSecRE   = regexp.MustCompile(`[+-]\d{2}:?\d{2}:?\d{2}(\[|$)`)
	jsOffsetOnly = regexp.MustCompile(`^[+-]\d{2}(:?\d{2})?(:?\d{2})?$`)
)

// jsStripCalendar removes key=value annotations; with a calendar the type
// has, the first [u-ca=…] must name ISO 8601.
func jsStripCalendar(s string, hasCalendar bool) (string, error) {
	if m := jsCalRE.FindStringSubmatch(s); m != nil && hasCalendar && !strings.EqualFold(m[1], "iso8601") {
		return "", jsRange("the only calendar is iso8601, not %q", m[1])
	}
	return jsKeyAnnotRE.ReplaceAllString(s, ""), nil
}

func jsFromString(kind, s string) (temporal, error) {
	if kind == "Duration" {
		if jsFracHMRE.MatchString(s) {
			return nil, jsRange("a duration may have a fraction only in its seconds: %q", s)
		}
		d, err := parseDuration(s)
		if err != nil {
			return nil, jsErr(err)
		}
		return jsDur(jsDurFields(d))
	}
	clean, err := jsStripCalendar(s, kind != "Instant" && kind != "PlainTime")
	if err != nil {
		return nil, err
	}
	if kind == "Instant" || kind == "ZonedDateTime" {
		p, err := parseISO(clean)
		if err != nil {
			return nil, jsErr(err)
		}
		if p.hasDate && (p.date.Y < jsMinExactYear || p.date.Y > jsMaxExactYr) {
			return nil, jsRange("an exact time is kept for the years %d to %d", jsMinExactYear, jsMaxExactYr)
		}
		if kind == "ZonedDateTime" && p.zone == "" {
			// "An offset without a time zone": the zone is that offset.
			if !p.hasDate || !(p.z || p.hasOff) {
				return nil, jsRange("a ZonedDateTime needs a time zone or an offset: %q", s)
			}
			if jsOffSecRE.MatchString(clean) {
				return nil, jsRange("an offset used as a time zone has no seconds: %q", s)
			}
			loc, id := time.UTC, "UTC"
			if !p.z {
				if loc, id, err = loadZone(offsetString(p.offset)); err != nil {
					return nil, jsErr(err)
				}
			}
			return ZonedDateTime{NS: PlainDateTime{p.date, p.time}.nanosOf() - int64(p.offset)*nsPerSec, Loc: loc, Zone: id}, nil
		}
	}
	v, err := temporalFromString(kind, clean)
	if err != nil {
		return nil, jsErr(err)
	}
	t := v.(temporal)
	switch x := t.(type) {
	case PlainDate:
		_, err = jsCheckDate(x)
	case PlainDateTime:
		_, err = jsCheckDate(x.Date)
	case PlainYearMonth:
		_, err = jsCheckDate(PlainDate{x.Y, x.M, 1})
	}
	return t, err
}

// --------------------------------------------------------- property bags

func jsHas(b jsBag, keys ...string) bool {
	for _, k := range keys {
		if v, ok := b[k]; ok && v != nil {
			return true
		}
	}
	return false
}

// jsBagMonth reads month and monthCode, which must agree.
func jsBagMonth(b jsBag) (int64, bool, error) {
	var m int64
	has := false
	if v := b["month"]; v != nil {
		n, err := jsInt(v, "month")
		if err != nil {
			return 0, false, err
		}
		m, has = n, true
	}
	if v := b["monthCode"]; v != nil {
		code, ok := v.(string)
		if !ok {
			return 0, false, jsType("monthCode is a string such as M09")
		}
		if len(code) != 3 || code[0] != 'M' || code[1] < '0' || code[1] > '9' || code[2] < '0' || code[2] > '9' {
			return 0, false, jsRange("monthCode %q is not a month of the ISO 8601 calendar", code)
		}
		n := int64(code[1]-'0')*10 + int64(code[2]-'0')
		if has && n != m {
			return 0, false, jsRange("month %d and monthCode %s disagree", m, code)
		}
		m, has = n, true
	}
	return m, has, nil
}

// jsBagDate reads year, month (or monthCode) and day over base; a field
// missing without a base is a TypeError, one out of range a RangeError.
func jsBagDate(b jsBag, base *PlainDate) (PlainDate, error) {
	var y, m, d int64
	if base != nil {
		y, m, d = int64(base.Y), int64(base.M), int64(base.D)
	}
	if v := b["year"]; v != nil {
		n, err := jsInt(v, "year")
		if err != nil {
			return PlainDate{}, err
		}
		y = n
	} else if base == nil {
		return PlainDate{}, jsType("year is required")
	}
	mm, ok, err := jsBagMonth(b)
	if err != nil {
		return PlainDate{}, err
	}
	if ok {
		m = mm
	} else if base == nil {
		return PlainDate{}, jsType("month or monthCode is required")
	}
	if v := b["day"]; v != nil {
		n, err := jsInt(v, "day")
		if err != nil {
			return PlainDate{}, err
		}
		d = n
	} else if base == nil {
		return PlainDate{}, jsType("day is required")
	}
	return jsDate(y, m, d)
}

func jsBagTime(b jsBag, base PlainTime) (PlainTime, error) {
	f := jsTimeParts(base)
	for i, k := range jsTimeFields {
		if v := b[k]; v != nil {
			n, err := jsInt(v, k)
			if err != nil {
				return PlainTime{}, err
			}
			f[i] = n
		}
	}
	return jsTime(f)
}

// jsWithBag checks the argument of with(): an object that is not a
// Temporal value, without a calendar or time zone, with one of the fields.
func jsWithBag(v any, fields ...string) (jsBag, error) {
	b, ok := v.(jsBag)
	if !ok {
		return nil, jsType("with() takes an object of fields")
	}
	if jsHas(b, "calendar", "timeZone") {
		return nil, jsType("with() does not take a calendar or a time zone")
	}
	if !jsHas(b, fields...) {
		return nil, jsType("with() needs at least one field")
	}
	return b, nil
}

func jsZone(v any) (*time.Location, string, error) {
	switch x := v.(type) {
	case string:
		if jsOffsetOnly.MatchString(x) {
			return nil, "", jsRange("a time zone is a name such as Europe/London, not the offset %q", x)
		}
		loc, id, err := loadZone(x)
		return loc, id, jsErr(err)
	case ZonedDateTime:
		return x.Loc, x.Zone, nil
	}
	return nil, "", jsType("a time zone is a name such as Europe/London")
}

// ------------------------------------------------------------------ from

// jsFrom is the standard's conversion of an argument to a type: a string,
// a value of the type, or a property bag (another Temporal type is read as
// one, through its fields).
func jsFrom(kind string, v any) (temporal, error) {
	switch x := v.(type) {
	case string:
		return jsFromString(kind, x)
	case temporal:
		if x.temporalType() == kind {
			return x, nil
		}
		if kind == "Instant" {
			if z, ok := x.(ZonedDateTime); ok {
				return Instant{z.NS}, nil
			}
			return nil, jsType("an Instant is made from a string, an Instant or a ZonedDateTime")
		}
		return jsFromBag(kind, jsBagOf(x))
	case jsBag:
		return jsFromBag(kind, x)
	}
	return nil, jsType("Temporal.%s is made from a string or an object", kind)
}

func jsFromBag(kind string, b jsBag) (temporal, error) {
	switch kind {
	case "Duration":
		if !jsHas(b, durationUnits...) {
			return nil, jsType("a duration needs at least one field")
		}
		var f [10]int64
		for i, k := range durationUnits {
			n, err := jsWhole(b[k], k)
			if err != nil {
				return nil, err
			}
			f[i] = n
		}
		return jsDur(f)
	case "Instant":
		return nil, jsType("an Instant is made from a string, an Instant or a ZonedDateTime")
	case "ZonedDateTime":
		return nil, jsType("a ZonedDateTime is not made from a property bag here: pass a string or a ZonedDateTime")
	case "PlainTime":
		if !jsHas(b, jsTimeFields...) {
			return nil, jsType("a time needs at least one field")
		}
		return jsBagTime(b, PlainTime{})
	}
	if err := jsBagCalendar(b); err != nil {
		return nil, err
	}
	switch kind {
	case "PlainDate":
		return jsBagDate(b, nil)
	case "PlainDateTime":
		d, err := jsBagDate(b, nil)
		if err != nil {
			return nil, err
		}
		t, err := jsBagTime(b, PlainTime{})
		return PlainDateTime{d, t}, err
	case "PlainYearMonth":
		bb := jsBag{"year": b["year"], "month": b["month"], "monthCode": b["monthCode"], "day": 1.0}
		d, err := jsBagDate(bb, nil)
		return PlainYearMonth{d.Y, d.M}, err
	case "PlainMonthDay":
		bb := jsBag{"year": b["year"], "month": b["month"], "monthCode": b["monthCode"], "day": b["day"]}
		if bb["year"] == nil {
			bb["year"] = 1972.0
		} else if y, err := jsInt(bb["year"], "year"); err != nil {
			return nil, err
		} else {
			// The year only decides whether 29 February exists.
			bb["year"] = 1971.0
			if isLeap(int(y % 400)) {
				bb["year"] = 1972.0
			}
		}
		d, err := jsBagDate(bb, nil)
		return PlainMonthDay{d.M, d.D}, err
	}
	return nil, jsType("unknown Temporal type %s", kind)
}

// ---------------------------------------------------------------- statics

func jsStatic(kind, m string, a []any) (any, error) {
	switch m {
	case "new":
		return jsNew(kind, a)
	case "from":
		v, err := jsFrom(kind, jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, err
		}
		return v, nil
	case "compare":
		x, err := jsFrom(kind, jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		y, err := jsFrom(kind, jsArg(a, 1))
		if err != nil {
			return nil, err
		}
		if kind == "Duration" {
			o, err := jsOptions(jsArg(a, 2))
			if err != nil {
				return nil, err
			}
			if err := jsNoOptions(o); err != nil {
				return nil, err
			}
			dx, dy := x.(Duration), y.(Duration)
			if dx == dy {
				return int64(0), nil
			}
			if dx.hasCalendar() || dy.hasCalendar() {
				return nil, jsRange("durations with years, months or weeks cannot be compared: relativeTo is not supported")
			}
		}
		return int64(jsCompare(x, y)), nil
	case "fromEpochMilliseconds":
		if kind != "Instant" {
			break
		}
		f, err := jsNum(jsArg(a, 0), "epochMilliseconds")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return nil, jsRange("epochMilliseconds must be a whole number")
		}
		if math.Abs(f) > float64(math.MaxInt64/1_000_000) {
			return nil, jsRange("the exact time is out of range")
		}
		return Instant{NS: int64(f) * 1_000_000}, nil
	case "fromEpochNanoseconds":
		if kind != "Instant" {
			break
		}
		return jsInstantOf(jsArg(a, 0))
	}
	return nil, jsType("Temporal.%s has no %s", kind, m)
}

func jsInstantOf(v any) (Instant, error) {
	b, ok := v.(jsBig)
	if !ok {
		return Instant{}, jsType("the epoch nanoseconds are a BigInt, such as 1000n")
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return Instant{}, jsRange("the exact time is out of range")
	}
	return Instant{NS: n}, nil
}

func jsCompare(x, y temporal) int {
	if a, ok := x.(PlainDateTime); ok {
		b := y.(PlainDateTime)
		if c := cmpDate(a.Date, b.Date); c != 0 {
			return c
		}
		return cmp3(a.Time.nanos() < b.Time.nanos(), a.Time.nanos() > b.Time.nanos())
	}
	c, _ := temporalCompare(x, y)
	return c
}

func jsNew(kind string, a []any) (any, error) {
	ints := func(from, n int, def int64, names ...string) ([]int64, error) {
		out := make([]int64, n)
		for i := range out {
			v := jsArg(a, from+i)
			var err error
			if def >= 0 {
				out[i], err = jsIntOr(v, def, names[i])
			} else {
				out[i], err = jsInt(v, names[i])
			}
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	switch kind {
	case "PlainDate":
		f, err := ints(0, 3, -1, "year", "month", "day")
		if err != nil {
			return nil, err
		}
		if err := jsCalendarArg(jsArg(a, 3)); err != nil {
			return nil, err
		}
		return jsDate(f[0], f[1], f[2])
	case "PlainTime":
		f, err := ints(0, 6, 0, jsTimeFields...)
		if err != nil {
			return nil, err
		}
		return jsTime([6]int64(f))
	case "PlainDateTime":
		f, err := ints(0, 3, -1, "year", "month", "day")
		if err != nil {
			return nil, err
		}
		t, err := ints(3, 6, 0, jsTimeFields...)
		if err != nil {
			return nil, err
		}
		if err := jsCalendarArg(jsArg(a, 9)); err != nil {
			return nil, err
		}
		d, err := jsDate(f[0], f[1], f[2])
		if err != nil {
			return nil, err
		}
		tm, err := jsTime([6]int64(t))
		return PlainDateTime{d, tm}, err
	case "ZonedDateTime":
		i, err := jsInstantOf(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		if _, ok := jsArg(a, 1).(string); !ok {
			return nil, jsType("the time zone is a name such as Europe/London")
		}
		loc, id, err := jsZone(jsArg(a, 1))
		if err != nil {
			return nil, err
		}
		if err := jsCalendarArg(jsArg(a, 2)); err != nil {
			return nil, err
		}
		return ZonedDateTime{NS: i.NS, Loc: loc, Zone: id}, nil
	case "Instant":
		return jsInstantOf(jsArg(a, 0))
	case "Duration":
		var f [10]int64
		for i, k := range durationUnits {
			n, err := jsWhole(jsArg(a, i), k)
			if err != nil {
				return nil, err
			}
			f[i] = n
		}
		return jsDur(f)
	case "PlainYearMonth":
		f, err := ints(0, 2, -1, "year", "month")
		if err != nil {
			return nil, err
		}
		if err := jsCalendarArg(jsArg(a, 2)); err != nil {
			return nil, err
		}
		// The reference day is checked and then dropped.
		ref, err := jsIntOr(jsArg(a, 3), 1, "the reference day")
		if err != nil {
			return nil, err
		}
		d, err := jsDate(f[0], f[1], ref)
		return PlainYearMonth{d.Y, d.M}, err
	case "PlainMonthDay":
		f, err := ints(0, 2, -1, "month", "day")
		if err != nil {
			return nil, err
		}
		if err := jsCalendarArg(jsArg(a, 2)); err != nil {
			return nil, err
		}
		ref, err := jsIntOr(jsArg(a, 3), 1972, "the reference year")
		if err != nil {
			return nil, err
		}
		d, err := jsDate(ref, f[0], f[1])
		return PlainMonthDay{d.M, d.D}, err
	}
	return nil, jsType("unknown Temporal type %s", kind)
}

func jsNow(m string, a []any, now time.Time) (any, error) {
	loc, id := time.UTC, "UTC"
	if v := jsArg(a, 0); v != nil && m != "instant" && m != "timeZoneId" {
		var err error
		if loc, id, err = jsZone(v); err != nil {
			return nil, err
		}
	}
	z := ZonedDateTime{NS: now.UnixNano(), Loc: loc, Zone: id}
	switch m {
	case "instant":
		return Instant{NS: z.NS}, nil
	case "timeZoneId":
		return "UTC", nil
	case "plainDateISO":
		return z.wall().Date, nil
	case "plainTimeISO":
		return z.wall().Time, nil
	case "plainDateTimeISO":
		return z.wall(), nil
	case "zonedDateTimeISO":
		return z, nil
	}
	return nil, jsType("Temporal.Now has no %s", m)
}

// ---------------------------------------------------------------- methods

func jsSign(m string) int {
	if m == "subtract" || m == "since" {
		return -1
	}
	return 1
}

// jsDurArg reads the duration of add and subtract, with its sign applied.
func jsDurArg(m string, a []any) (Duration, error) {
	t, err := jsFrom("Duration", jsArg(a, 0))
	if err != nil {
		return Duration{}, err
	}
	if _, err := jsOverflow(jsArg(a, 1), "constrain"); err != nil {
		return Duration{}, err
	}
	return t.(Duration).scale(jsSign(m)), nil
}

// jsOther reads the other value of until, since and equals.
func jsOtherArg(kind, m string, a []any) (temporal, error) {
	o, err := jsFrom(kind, jsArg(a, 0))
	if err != nil {
		return nil, err
	}
	if m != "equals" {
		opts, err := jsOptions(jsArg(a, 1))
		if err != nil {
			return nil, err
		}
		if err := jsNoOptions(opts); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func jsAddDate(d PlainDate, dur Duration) (PlainDate, error) {
	const far = 12 * 20000
	months := dur.Years*12 + dur.Months
	days := dur.Weeks*7 + dur.Days + dur.timeNanos()/nsPerDay
	if months > far || months < -far || days > far*31 || days < -far*31 {
		return d, jsRange("the result is outside the years -9999 to 9999")
	}
	r, err := d.addMonths(months, true)
	if err != nil {
		return d, jsErr(err)
	}
	return jsCheckDate(r.addDays(days))
}

func jsMethod(self temporal, m string, a []any) (any, error) {
	kind := self.temporalType()
	switch m {
	case "fields":
		return jsFields(self), nil
	case "toString":
		o, err := jsOptions(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		if err := jsNoOptions(o, "timeZone"); err != nil {
			return nil, err
		}
		return jsString(self), nil
	case "toLocaleString":
		return jsLocale(self, jsArg(a, 0), jsArg(a, 1))
	case "equals":
		if kind == "Duration" {
			break
		}
		o, err := jsOtherArg(kind, m, a)
		if err != nil {
			return nil, err
		}
		return temporalEquals(self, o), nil
	}
	var (
		out any
		ok  bool
		err error
	)
	switch x := self.(type) {
	case PlainDate:
		out, ok, err = jsPlainDate(x, m, a)
	case PlainTime:
		out, ok, err = jsPlainTime(x, m, a)
	case PlainDateTime:
		out, ok, err = jsPlainDateTime(x, m, a)
	case ZonedDateTime:
		out, ok, err = jsZoned(x, m, a)
	case Instant:
		out, ok, err = jsInstant(x, m, a)
	case Duration:
		out, ok, err = jsDuration(x, m, a)
	case PlainYearMonth:
		out, ok, err = jsYearMonth(x, m, a)
	case PlainMonthDay:
		out, ok, err = jsMonthDay(x, m, a)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, jsType("Temporal.%s has no %s", kind, m)
	}
	return out, nil
}

func jsTimeArg(v any) (PlainTime, error) {
	if v == nil {
		return PlainTime{}, nil
	}
	t, err := jsFrom("PlainTime", v)
	if err != nil {
		return PlainTime{}, err
	}
	return t.(PlainTime), nil
}

func jsPlainDate(d PlainDate, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		r, err := jsAddDate(d, dur)
		return r, true, err
	case "until", "since":
		o, err := jsOtherArg("PlainDate", m, a)
		if err != nil {
			return nil, true, err
		}
		return Duration{Days: int64(jsSign(m)) * (o.(PlainDate).days() - d.days())}, true, nil
	case "with":
		b, err := jsWithBag(jsArg(a, 0), "year", "month", "monthCode", "day")
		if err != nil {
			return nil, true, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		r, err := jsBagDate(b, &d)
		return r, true, err
	case "toPlainDateTime":
		t, err := jsTimeArg(jsArg(a, 0))
		return PlainDateTime{d, t}, true, err
	case "toZonedDateTime":
		zone := jsArg(a, 0)
		var t PlainTime
		if b, ok := zone.(jsBag); ok {
			if b["timeZone"] == nil {
				return nil, true, jsType("toZonedDateTime() needs a time zone")
			}
			var err error
			if t, err = jsTimeArg(b["plainTime"]); err != nil {
				return nil, true, err
			}
			zone = b["timeZone"]
		}
		loc, id, err := jsZone(zone)
		if err != nil {
			return nil, true, err
		}
		ns, err := jsExact(PlainDateTime{d, t}, loc)
		return ZonedDateTime{NS: ns, Loc: loc, Zone: id}, true, err
	case "toPlainYearMonth":
		return PlainYearMonth{d.Y, d.M}, true, nil
	case "toPlainMonthDay":
		return PlainMonthDay{d.M, d.D}, true, nil
	}
	return nil, false, nil
}

func jsPlainTime(t PlainTime, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		return timeFromNanos(t.nanos() + dur.timeNanos()%nsPerDay), true, nil
	case "until", "since":
		o, err := jsOtherArg("PlainTime", m, a)
		if err != nil {
			return nil, true, err
		}
		return balanceNanos(int64(jsSign(m))*(o.(PlainTime).nanos()-t.nanos()), 4), true, nil
	case "with":
		b, err := jsWithBag(jsArg(a, 0), jsTimeFields...)
		if err != nil {
			return nil, true, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		r, err := jsBagTime(b, t)
		return r, true, err
	}
	return nil, false, nil
}

// jsDateTimeDiff is b - a in days and time, without the engine's 64-bit
// nanosecond count of the whole span.
func jsDateTimeDiff(a, b PlainDateTime) Duration {
	days := b.Date.days() - a.Date.days()
	td := b.Time.nanos() - a.Time.nanos()
	switch {
	case days > 0 && td < 0:
		days, td = days-1, td+nsPerDay
	case days < 0 && td > 0:
		days, td = days+1, td-nsPerDay
	}
	d := balanceNanos(td, 4)
	d.Days = days
	return d
}

func jsAddDateTime(dt PlainDateTime, dur Duration) (PlainDateTime, error) {
	tn := dt.Time.nanos() + dur.timeNanos()%nsPerDay
	carry := floorDiv(tn, nsPerDay)
	whole := dur
	whole.Hours, whole.Minutes, whole.Seconds, whole.Ms, whole.Us, whole.Ns = 0, 0, 0, 0, 0, 0
	whole.Days += dur.timeNanos()/nsPerDay + carry
	date, err := jsAddDate(dt.Date, whole)
	return PlainDateTime{date, timeFromNanos(tn)}, err
}

func jsPlainDateTime(dt PlainDateTime, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		r, err := jsAddDateTime(dt, dur)
		return r, true, err
	case "until", "since":
		o, err := jsOtherArg("PlainDateTime", m, a)
		if err != nil {
			return nil, true, err
		}
		x, y := dt, o.(PlainDateTime)
		if m == "since" {
			x, y = y, x
		}
		return jsDateTimeDiff(x, y), true, nil
	case "with":
		b, err := jsWithBag(jsArg(a, 0), append([]string{"year", "month", "monthCode", "day"}, jsTimeFields...)...)
		if err != nil {
			return nil, true, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		d, err := jsBagDate(b, &dt.Date)
		if err != nil {
			return nil, true, err
		}
		t, err := jsBagTime(b, dt.Time)
		return PlainDateTime{d, t}, true, err
	case "toPlainDate":
		return dt.Date, true, nil
	case "toPlainTime":
		return dt.Time, true, nil
	case "toZonedDateTime":
		loc, id, err := jsZone(jsArg(a, 0))
		if err != nil {
			return nil, true, err
		}
		o, err := jsOptions(jsArg(a, 1))
		if err != nil {
			return nil, true, err
		}
		if err := jsNoOptions(o); err != nil {
			return nil, true, err
		}
		ns, err := jsExact(dt, loc)
		return ZonedDateTime{NS: ns, Loc: loc, Zone: id}, true, err
	}
	return nil, false, nil
}

func jsZoned(z ZonedDateTime, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		ns := z.NS
		if dur.hasCalendar() || dur.Days != 0 {
			w := z.wall()
			whole := dur
			whole.Hours, whole.Minutes, whole.Seconds, whole.Ms, whole.Us, whole.Ns = 0, 0, 0, 0, 0, 0
			date, err := jsAddDate(w.Date, whole)
			if err != nil {
				return nil, true, err
			}
			if ns, err = jsExact(PlainDateTime{date, w.Time}, z.Loc); err != nil {
				return nil, true, err
			}
		}
		ns, err = jsAddNS(ns, dur.timeNanos())
		return ZonedDateTime{NS: ns, Loc: z.Loc, Zone: z.Zone}, true, err
	case "until", "since":
		o, err := jsOtherArg("ZonedDateTime", m, a)
		if err != nil {
			return nil, true, err
		}
		return balanceNanos(int64(jsSign(m))*(o.(ZonedDateTime).NS-z.NS), 4), true, nil
	case "with":
		fields := append([]string{"year", "month", "monthCode", "day"}, jsTimeFields...)
		b, err := jsWithBag(jsArg(a, 0), append(fields, "offset")...)
		if err != nil {
			return nil, true, err
		}
		if !jsHas(b, fields...) {
			return nil, true, jsType("with() does not take an offset alone")
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		w := z.wall()
		d, err := jsBagDate(b, &w.Date)
		if err != nil {
			return nil, true, err
		}
		t, err := jsBagTime(b, w.Time)
		if err != nil {
			return nil, true, err
		}
		ns, err := jsExact(PlainDateTime{d, t}, z.Loc)
		return ZonedDateTime{NS: ns, Loc: z.Loc, Zone: z.Zone}, true, err
	case "withTimeZone":
		loc, id, err := jsZone(jsArg(a, 0))
		return ZonedDateTime{NS: z.NS, Loc: loc, Zone: id}, true, err
	case "toInstant":
		return Instant{z.NS}, true, nil
	case "toPlainDate":
		return z.wall().Date, true, nil
	case "toPlainTime":
		return z.wall().Time, true, nil
	case "toPlainDateTime":
		return z.wall(), true, nil
	}
	return nil, false, nil
}

func jsInstant(i Instant, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		if dur.hasCalendar() || dur.Days != 0 {
			return nil, true, jsRange("an Instant adds hours and smaller units only")
		}
		ns, err := jsAddNS(i.NS, dur.timeNanos())
		return Instant{ns}, true, err
	case "until", "since":
		o, err := jsOtherArg("Instant", m, a)
		if err != nil {
			return nil, true, err
		}
		return balanceNanos(int64(jsSign(m))*(o.(Instant).NS-i.NS), 4), true, nil
	case "toZonedDateTimeISO":
		loc, id, err := jsZone(jsArg(a, 0))
		return ZonedDateTime{NS: i.NS, Loc: loc, Zone: id}, true, err
	}
	return nil, false, nil
}

var jsUnitNS = map[string]int64{"day": nsPerDay, "hour": nsPerHour, "minute": nsPerMin, "second": nsPerSec, "millisecond": 1_000_000, "microsecond": 1_000, "nanosecond": 1}

func jsDuration(d Duration, m string, a []any) (any, bool, error) {
	switch m {
	case "negated":
		return d.negated(), true, nil
	case "abs":
		return d.abs(), true, nil
	case "with":
		b, ok := jsArg(a, 0).(jsBag)
		if !ok || !jsHas(b, durationUnits...) {
			return nil, true, jsType("with() takes an object with at least one duration field")
		}
		f := jsDurFields(d)
		for i, k := range durationUnits {
			if b[k] == nil {
				continue
			}
			n, err := jsWhole(b[k], k)
			if err != nil {
				return nil, true, err
			}
			f[i] = n
		}
		r, err := jsDur(f)
		return r, true, err
	case "add", "subtract":
		t, err := jsFrom("Duration", jsArg(a, 0))
		if err != nil {
			return nil, true, err
		}
		o := t.(Duration).scale(jsSign(m))
		if d.hasCalendar() || o.hasCalendar() {
			return nil, true, jsRange("durations with years, months or weeks cannot be added: relativeTo is not supported")
		}
		f := jsDurFields(d)
		for i, v := range jsDurFields(o) {
			f[i] += v
		}
		largest := 4
		if d.Days != 0 || o.Days != 0 {
			largest = 3
		}
		// Mixed signs settle on the total's sign.
		days := f[3]
		f[3] = 0
		sum, err := jsDur(f)
		if err != nil {
			return nil, true, err
		}
		total := new(big.Int).Add(big.NewInt(sum.timeNanos()), new(big.Int).Mul(big.NewInt(days), big.NewInt(nsPerDay)))
		if !total.IsInt64() {
			return nil, true, jsRange("the duration is out of range")
		}
		return balanceNanos(total.Int64(), largest), true, nil
	case "total":
		var unit any
		switch x := jsArg(a, 0).(type) {
		case string:
			unit = x
		case jsBag:
			if err := jsNoOptions(x); err != nil {
				return nil, true, err
			}
			unit = x["unit"]
		default:
			return nil, true, jsType("total() takes a unit")
		}
		name, _ := unit.(string)
		name = strings.TrimSuffix(name, "s")
		size, ok := jsUnitNS[name]
		if !ok {
			if name == "year" || name == "month" || name == "week" {
				return nil, true, jsRange("total() in %ss needs relativeTo, which is not supported", name)
			}
			return nil, true, jsRange("total() needs a unit such as hours")
		}
		if d.hasCalendar() {
			return nil, true, jsRange("total() of years, months or weeks needs relativeTo, which is not supported")
		}
		total := new(big.Int).Add(big.NewInt(d.timeNanos()), new(big.Int).Mul(big.NewInt(d.Days), big.NewInt(nsPerDay)))
		q, r := new(big.Int).QuoRem(total, big.NewInt(size), new(big.Int))
		qf, _ := new(big.Float).SetInt(q).Float64()
		f := qf + float64(r.Int64())/float64(size)
		if f == math.Trunc(f) && math.Abs(f) <= jsMaxSafe {
			return int64(f), true, nil
		}
		return f, true, nil
	}
	return nil, false, nil
}

func jsYearMonth(ym PlainYearMonth, m string, a []any) (any, bool, error) {
	switch m {
	case "add", "subtract":
		dur, err := jsDurArg(m, a)
		if err != nil {
			return nil, true, err
		}
		if dur.Weeks != 0 || dur.Days != 0 || dur.timeNanos() != 0 {
			return nil, true, jsRange("a PlainYearMonth adds years and months only")
		}
		d, err := jsAddDate(PlainDate{ym.Y, ym.M, 1}, dur)
		return PlainYearMonth{d.Y, d.M}, true, err
	case "until", "since":
		o, err := jsOtherArg("PlainYearMonth", m, a)
		if err != nil {
			return nil, true, err
		}
		b := o.(PlainYearMonth)
		months := int64(jsSign(m)) * (int64(b.Y-ym.Y)*12 + int64(b.M-ym.M))
		return Duration{Years: months / 12, Months: months % 12}, true, nil
	case "with":
		b, err := jsWithBag(jsArg(a, 0), "year", "month", "monthCode")
		if err != nil {
			return nil, true, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		base := PlainDate{ym.Y, ym.M, 1}
		d, err := jsBagDate(jsBag{"year": b["year"], "month": b["month"], "monthCode": b["monthCode"]}, &base)
		return PlainYearMonth{d.Y, d.M}, true, err
	case "toPlainDate":
		b, ok := jsArg(a, 0).(jsBag)
		if !ok || b["day"] == nil {
			return nil, true, jsType("toPlainDate() takes an object with a day")
		}
		day, err := jsInt(b["day"], "day")
		if err != nil {
			return nil, true, err
		}
		d, err := jsDate(int64(ym.Y), int64(ym.M), day)
		return d, true, err
	}
	return nil, false, nil
}

func jsMonthDay(md PlainMonthDay, m string, a []any) (any, bool, error) {
	switch m {
	case "with":
		b, err := jsWithBag(jsArg(a, 0), "year", "month", "monthCode", "day")
		if err != nil {
			return nil, true, err
		}
		if _, err := jsOverflow(jsArg(a, 1), "reject"); err != nil {
			return nil, true, err
		}
		bb := jsBag{"year": b["year"], "month": b["month"], "monthCode": b["monthCode"], "day": b["day"]}
		if bb["month"] == nil && bb["monthCode"] == nil {
			bb["month"] = float64(md.M)
		}
		if bb["day"] == nil {
			bb["day"] = float64(md.D)
		}
		r, err := jsFromBag("PlainMonthDay", bb)
		return r, true, err
	case "toPlainDate":
		b, ok := jsArg(a, 0).(jsBag)
		if !ok || b["year"] == nil {
			return nil, true, jsType("toPlainDate() takes an object with a year")
		}
		y, err := jsInt(b["year"], "year")
		if err != nil {
			return nil, true, err
		}
		d, err := jsDate(y, int64(md.M), int64(md.D))
		return d, true, err
	}
	return nil, false, nil
}

// ----------------------------------------------------------------- locale

// jsLocale is toLocaleString(locales, options). The docs give the shape
// (medium by default, dateStyle and timeStyle only, "full" as "long", the
// time with seconds when both are given); the text itself is this
// implementation's: English month names for en locales, the numeric forms
// of Sessel's toLocaleString for the others.
func jsLocale(t temporal, locales, options any) (string, error) {
	locale := "en-US"
	switch x := locales.(type) {
	case nil:
	case string:
		locale = x
	case jsBag: // an array of locales: the first
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			if s, ok := x[keys[0]].(string); ok {
				locale = s
			}
		}
	default:
		return "", jsType("a locale is a string")
	}
	o, err := jsOptions(options)
	if err != nil {
		return "", err
	}
	style := map[string]string{}
	for k, v := range o {
		if v == nil {
			continue
		}
		if k != "dateStyle" && k != "timeStyle" {
			return "", jsRange("toLocaleString() takes dateStyle and timeStyle only, not %s", k)
		}
		s, _ := v.(string)
		switch s {
		case "full":
			s = "long"
		case "long", "medium", "short":
		default:
			return "", jsRange("%s is full, long, medium or short", k)
		}
		style[k] = s
	}
	english := strings.HasPrefix(strings.ToLower(locale), "en")
	datePat := func(s string) string {
		if !english || (locale != "en-US" && !strings.EqualFold(locale, "en")) {
			return ""
		}
		switch s {
		case "long":
			return "MMMM d, yyyy"
		case "short":
			return "M/d/yy"
		}
		return "MMM d, yyyy"
	}
	timePat := func(s string) string {
		if !english || (locale != "en-US" && !strings.EqualFold(locale, "en")) {
			return ""
		}
		if s == "short" {
			return "h:mm a"
		}
		return "h:mm:ss a"
	}
	date := func(d PlainDate, s string) (string, error) {
		if p := datePat(s); p != "" {
			return formatTemporal(d, p)
		}
		return localeString(d, locale)
	}
	clock := func(tm PlainTime, s string) (string, error) {
		if p := timePat(s); p != "" {
			return formatTemporal(tm, p)
		}
		return localeString(tm, locale)
	}
	ds, ts := style["dateStyle"], style["timeStyle"]
	var d PlainDate
	var tm PlainTime
	hasDate, hasTime := true, true
	switch x := t.(type) {
	case Duration:
		return x.String(), nil
	case PlainDate:
		d, hasTime = x, false
	case PlainTime:
		tm, hasDate = x, false
	case PlainDateTime:
		d, tm = x.Date, x.Time
	case Instant:
		w := plainFromNanos(x.NS)
		d, tm = w.Date, w.Time
	case ZonedDateTime:
		w := x.wall()
		d, tm = w.Date, w.Time
	case PlainYearMonth:
		d, hasTime = PlainDate{x.Y, x.M, 1}, false
	case PlainMonthDay:
		d, hasTime = PlainDate{2000, x.M, x.D}, false
	}
	switch {
	case !hasTime:
		return date(d, ds)
	case !hasDate:
		return clock(tm, ts)
	case ds != "" && ts == "":
		return date(d, ds)
	case ts != "" && ds == "":
		return clock(tm, ts)
	}
	// Both, or neither: the date, then the time with seconds.
	a, err := date(d, ds)
	if err != nil {
		return "", err
	}
	b, err := clock(tm, "medium")
	return a + ", " + b, err
}
