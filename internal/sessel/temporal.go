package sessel

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Temporal values (R-SESSEL-300..311): ISO 8601 calendar, TC39 Temporal
// semantics where the docs are silent.

type temporal interface {
	temporalType() string
	String() string
}

// PlainDate is a calendar date.
type PlainDate struct{ Y, M, D int }

// PlainTime is a wall-clock time; Ns is nanoseconds within the second.
type PlainTime struct{ H, Mi, S, Ns int }

// PlainDateTime is a date and a time without a zone.
type PlainDateTime struct {
	Date PlainDate
	Time PlainTime
}

// Instant is an exact moment (nanoseconds since the Unix epoch).
type Instant struct{ NS int64 }

// ZonedDateTime is an exact moment in a named time zone.
type ZonedDateTime struct {
	NS   int64
	Loc  *time.Location
	Zone string
}

// Duration holds the ten Temporal duration fields (all of one sign).
type Duration struct {
	Years, Months, Weeks, Days, Hours, Minutes, Seconds, Ms, Us, Ns int64
}

// PlainYearMonth is a year and month.
type PlainYearMonth struct{ Y, M int }

// PlainMonthDay is a month and day.
type PlainMonthDay struct{ M, D int }

func (PlainDate) temporalType() string      { return "PlainDate" }
func (PlainTime) temporalType() string      { return "PlainTime" }
func (PlainDateTime) temporalType() string  { return "PlainDateTime" }
func (Instant) temporalType() string        { return "Instant" }
func (ZonedDateTime) temporalType() string  { return "ZonedDateTime" }
func (Duration) temporalType() string       { return "Duration" }
func (PlainYearMonth) temporalType() string { return "PlainYearMonth" }
func (PlainMonthDay) temporalType() string  { return "PlainMonthDay" }

const (
	nsPerSec  = int64(time.Second)
	nsPerMin  = int64(time.Minute)
	nsPerHour = int64(time.Hour)
	nsPerDay  = 24 * nsPerHour
)

// ---------------------------------------------------------------- calendar

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func daysInMonth(y, m int) int {
	switch m {
	case 2:
		if isLeap(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

func daysInYear(y int) int {
	if isLeap(y) {
		return 366
	}
	return 365
}

// daysFromCivil counts days since 1970-01-01 (proleptic Gregorian).
func daysFromCivil(y, m, d int) int64 {
	yy := int64(y)
	if m <= 2 {
		yy--
	}
	era := floorDiv(yy, 400)
	yoe := yy - era*400
	mp := int64((m + 9) % 12)
	doy := (153*mp+2)/5 + int64(d) - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func civilFromDays(z int64) PlainDate {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return PlainDate{int(y), int(m), int(d)}
}

func (d PlainDate) days() int64 { return daysFromCivil(d.Y, d.M, d.D) }

func (d PlainDate) dayOfWeek() int { return int(floorMod(d.days()+3, 7)) + 1 }

func (d PlainDate) dayOfYear() int { return int(d.days()-daysFromCivil(d.Y, 1, 1)) + 1 }

func weeksInYear(y int) int {
	p := func(y int) int { return int(floorMod(int64(y+y/4-y/100+y/400), 7)) }
	if p(y) == 4 || p(y-1) == 3 {
		return 53
	}
	return 52
}

func (d PlainDate) isoWeek() (int, int) {
	w := (d.dayOfYear() - d.dayOfWeek() + 10) / 7
	switch {
	case w < 1:
		return d.Y - 1, weeksInYear(d.Y - 1)
	case w > weeksInYear(d.Y):
		return d.Y + 1, 1
	}
	return d.Y, w
}

func cmpDate(a, b PlainDate) int {
	switch {
	case a.Y != b.Y:
		return cmp3(a.Y < b.Y, a.Y > b.Y)
	case a.M != b.M:
		return cmp3(a.M < b.M, a.M > b.M)
	}
	return cmp3(a.D < b.D, a.D > b.D)
}

func (t PlainTime) nanos() int64 {
	return int64(t.H)*nsPerHour + int64(t.Mi)*nsPerMin + int64(t.S)*nsPerSec + int64(t.Ns)
}

func timeFromNanos(n int64) PlainTime {
	n = floorMod(n, nsPerDay)
	return PlainTime{H: int(n / nsPerHour), Mi: int(n % nsPerHour / nsPerMin), S: int(n % nsPerMin / nsPerSec), Ns: int(n % nsPerSec)}
}

func (d PlainDate) addMonths(months int64, constrain bool) (PlainDate, error) {
	total := int64(d.Y)*12 + int64(d.M-1) + months
	y := int(floorDiv(total, 12))
	m := int(floorMod(total, 12)) + 1
	day := d.D
	if dim := daysInMonth(y, m); day > dim {
		if !constrain {
			return PlainDate{}, typeErr("date out of range")
		}
		day = dim
	}
	return PlainDate{y, m, day}, nil
}

func (d PlainDate) addDays(n int64) PlainDate { return civilFromDays(d.days() + n) }

// ---------------------------------------------------------------- strings

func pad(n int, w int) string {
	s := strconv.Itoa(absInt(n))
	for len(s) < w {
		s = "0" + s
	}
	if n < 0 {
		return "-" + s
	}
	return s
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func yearString(y int) string {
	if y >= 0 && y <= 9999 {
		return pad(y, 4)
	}
	if y < 0 {
		return "-" + pad(-y, 6)
	}
	return "+" + pad(y, 6)
}

func fracString(ns int) string {
	if ns == 0 {
		return ""
	}
	s := pad(ns, 9)
	return "." + strings.TrimRight(s, "0")
}

func (d PlainDate) String() string { return yearString(d.Y) + "-" + pad(d.M, 2) + "-" + pad(d.D, 2) }

func (t PlainTime) String() string {
	return pad(t.H, 2) + ":" + pad(t.Mi, 2) + ":" + pad(t.S, 2) + fracString(t.Ns)
}

func (dt PlainDateTime) String() string { return dt.Date.String() + "T" + dt.Time.String() }

func (i Instant) String() string {
	dt := plainFromNanos(i.NS)
	return dt.String() + "Z"
}

func offsetString(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return sign + pad(sec/3600, 2) + ":" + pad(sec%3600/60, 2)
}

func (z ZonedDateTime) offsetSeconds() int {
	_, off := time.Unix(0, z.NS).In(z.Loc).Zone()
	return off
}

func (z ZonedDateTime) wall() PlainDateTime {
	return plainFromNanos(z.NS + int64(z.offsetSeconds())*nsPerSec)
}

func (z ZonedDateTime) String() string {
	return z.wall().String() + offsetString(z.offsetSeconds()) + "[" + z.Zone + "]"
}

func (ym PlainYearMonth) String() string { return yearString(ym.Y) + "-" + pad(ym.M, 2) }

func (md PlainMonthDay) String() string { return "--" + pad(md.M, 2) + "-" + pad(md.D, 2) }

func (d Duration) fields() []int64 {
	return []int64{d.Years, d.Months, d.Weeks, d.Days, d.Hours, d.Minutes, d.Seconds, d.Ms, d.Us, d.Ns}
}

var durationUnits = []string{"years", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds", "microseconds", "nanoseconds"}

func durationFromFields(f []int64) Duration {
	return Duration{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7], f[8], f[9]}
}

func (d Duration) sign() int {
	for _, f := range d.fields() {
		if f > 0 {
			return 1
		}
		if f < 0 {
			return -1
		}
	}
	return 0
}

func (d Duration) negated() Duration {
	f := d.fields()
	for i := range f {
		f[i] = -f[i]
	}
	return durationFromFields(f)
}

func (d Duration) abs() Duration {
	if d.sign() < 0 {
		return d.negated()
	}
	return d
}

func (d Duration) String() string {
	s := d.sign()
	a := d.abs()
	var b strings.Builder
	if s < 0 {
		b.WriteByte('-')
	}
	b.WriteByte('P')
	for _, x := range []struct {
		v int64
		u string
	}{{a.Years, "Y"}, {a.Months, "M"}, {a.Weeks, "W"}, {a.Days, "D"}} {
		if x.v != 0 {
			b.WriteString(strconv.FormatInt(x.v, 10) + x.u)
		}
	}
	sub := a.Ms*1_000_000 + a.Us*1_000 + a.Ns
	secs := a.Seconds + sub/nsPerSec
	frac := int(sub % nsPerSec)
	if a.Hours != 0 || a.Minutes != 0 || secs != 0 || frac != 0 {
		b.WriteByte('T')
		if a.Hours != 0 {
			b.WriteString(strconv.FormatInt(a.Hours, 10) + "H")
		}
		if a.Minutes != 0 {
			b.WriteString(strconv.FormatInt(a.Minutes, 10) + "M")
		}
		if secs != 0 || frac != 0 {
			b.WriteString(strconv.FormatInt(secs, 10) + fracString(frac) + "S")
		}
	}
	if s == 0 {
		return "PT0S"
	}
	return b.String()
}

// plainFromNanos converts epoch nanoseconds (as UTC wall time).
func plainFromNanos(ns int64) PlainDateTime {
	days := floorDiv(ns, nsPerDay)
	return PlainDateTime{Date: civilFromDays(days), Time: timeFromNanos(ns - days*nsPerDay)}
}

// nanosOf converts a wall time to nanoseconds as if it were UTC.
func (dt PlainDateTime) nanosOf() int64 { return dt.Date.days()*nsPerDay + dt.Time.nanos() }

// ---------------------------------------------------------------- zones

var zoneCache sync.Map

func loadZone(name string) (*time.Location, string, error) {
	if strings.EqualFold(name, "UTC") || name == "Z" {
		return time.UTC, "UTC", nil
	}
	if l, ok := zoneCache.Load(name); ok {
		return l.(*time.Location), name, nil
	}
	if m := offsetRE.FindStringSubmatch(name); m != nil && m[0] == name {
		sec := offsetSeconds(m)
		loc := time.FixedZone(offsetString(sec), sec)
		zoneCache.Store(name, loc)
		return loc, offsetString(sec), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", typeErr("unknown time zone %q", name)
	}
	zoneCache.Store(name, loc)
	return loc, name, nil
}

func offsetAt(loc *time.Location, sec int64) int64 {
	_, off := time.Unix(sec, 0).In(loc).Zone()
	return int64(off)
}

// resolveWall maps a wall-clock time in loc to an instant with TC39
// "compatible" disambiguation (earlier in overlaps, later in gaps).
func resolveWall(dt PlainDateTime, loc *time.Location) int64 {
	wall := dt.nanosOf()
	wallSec := floorDiv(wall, nsPerSec)
	before := offsetAt(loc, wallSec-86400)
	after := offsetAt(loc, wallSec+86400)
	var best int64
	found := false
	for _, o := range []int64{before, after, offsetAt(loc, wallSec)} {
		inst := wall - o*nsPerSec
		if offsetAt(loc, floorDiv(inst, nsPerSec)) == o {
			if !found || inst < best {
				best, found = inst, true
			}
		}
	}
	if found {
		return best
	}
	return wall - before*nsPerSec
}

// ---------------------------------------------------------------- parsing

var (
	dateRE     = regexp.MustCompile(`^([+-]\d{6}|\d{4})-(\d{2})-(\d{2})`)
	timeRE     = regexp.MustCompile(`^(\d{2})(?::(\d{2})(?::(\d{2})(?:[.,](\d{1,9}))?)?)?`)
	offsetRE   = regexp.MustCompile(`^([+-])(\d{2}):?(\d{2})(?::?(\d{2}))?`)
	annotRE    = regexp.MustCompile(`^\[(!?)([^\]=]+)\]`)
	durationRE = regexp.MustCompile(`^([+-])?P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+(?:[.,]\d{1,9})?)H)?(?:(\d+(?:[.,]\d{1,9})?)M)?(?:(\d+(?:[.,]\d{1,9})?)S)?)?$`)
)

func offsetSeconds(m []string) int {
	h, _ := strconv.Atoi(m[2])
	mi, _ := strconv.Atoi(m[3])
	s := 0
	if len(m) > 4 && m[4] != "" {
		s, _ = strconv.Atoi(m[4])
	}
	sec := h*3600 + mi*60 + s
	if m[1] == "-" {
		sec = -sec
	}
	return sec
}

// isoParts is a parsed ISO 8601 date-time string.
type isoParts struct {
	date    PlainDate
	hasDate bool
	time    PlainTime
	hasTime bool
	z       bool
	offset  int
	hasOff  bool
	zone    string
}

func parseISO(s string) (isoParts, error) {
	var p isoParts
	rest := strings.TrimSpace(s)
	bad := func() (isoParts, error) { return isoParts{}, typeErr("invalid ISO 8601 string %q", s) }
	if m := dateRE.FindStringSubmatch(rest); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo < 1 || mo > 12 || d < 1 || d > daysInMonth(y, mo) {
			return isoParts{}, typeErr("date out of range in %q", s)
		}
		p.date, p.hasDate = PlainDate{y, mo, d}, true
		rest = rest[len(m[0]):]
		if rest != "" && (rest[0] == 'T' || rest[0] == 't' || rest[0] == ' ') {
			rest = rest[1:]
		} else if rest != "" && rest[0] != '[' && rest[0] != 'Z' && rest[0] != 'z' && rest[0] != '+' && rest[0] != '-' {
			return bad()
		} else {
			goto zone
		}
	} else if rest != "" && (rest[0] == 'T' || rest[0] == 't') {
		rest = rest[1:]
	}
	if m := timeRE.FindStringSubmatch(rest); m != nil && m[0] != "" {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		sec, _ := strconv.Atoi(m[3])
		if m[2] == "" && p.hasDate {
			return bad()
		}
		ns := 0
		if m[4] != "" {
			f := m[4]
			for len(f) < 9 {
				f += "0"
			}
			ns, _ = strconv.Atoi(f)
		}
		if sec == 60 {
			sec = 59
		}
		if h > 23 || mi > 59 || sec > 59 {
			return isoParts{}, typeErr("time out of range in %q", s)
		}
		p.time, p.hasTime = PlainTime{h, mi, sec, ns}, true
		rest = rest[len(m[0]):]
	} else if !p.hasDate {
		return bad()
	}
zone:
	if rest != "" && (rest[0] == 'Z' || rest[0] == 'z') {
		p.z = true
		rest = rest[1:]
	} else if m := offsetRE.FindStringSubmatch(rest); m != nil {
		p.offset, p.hasOff = offsetSeconds(m), true
		rest = rest[len(m[0]):]
	}
	for rest != "" {
		m := annotRE.FindStringSubmatch(rest)
		if m == nil {
			return bad()
		}
		if !strings.Contains(m[2], "=") && p.zone == "" {
			p.zone = m[2]
		}
		rest = rest[len(m[0]):]
	}
	return p, nil
}

func parseDuration(s string) (Duration, error) {
	t := strings.TrimSpace(s)
	m := durationRE.FindStringSubmatch(t)
	if m == nil || strings.HasSuffix(t, "T") || strings.Join(m[2:9], "") == "" {
		return Duration{}, typeErr("invalid duration %q", s)
	}
	var d Duration
	num := func(x string) int64 {
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	d.Years, d.Months, d.Weeks, d.Days = num(m[2]), num(m[3]), num(m[4]), num(m[5])
	units := []int64{nsPerHour, nsPerMin, nsPerSec}
	dst := []*int64{&d.Hours, &d.Minutes, &d.Seconds}
	var fracNs int64
	fracUnit := -1
	for i := 0; i < 3; i++ {
		v := strings.ReplaceAll(m[6+i], ",", ".")
		if v == "" {
			continue
		}
		if fracUnit >= 0 {
			return Duration{}, typeErr("invalid duration %q: only the smallest unit may have a fraction", s)
		}
		whole, frac, has := strings.Cut(v, ".")
		*dst[i] = num(whole)
		if has {
			for len(frac) < 9 {
				frac += "0"
			}
			fracNs = num(frac[:9]) * (units[i] / nsPerSec)
			fracUnit = i
		}
	}
	if fracNs != 0 {
		r := balanceNanos(fracNs, 5+fracUnit)
		d.Minutes += r.Minutes
		d.Seconds += r.Seconds
		d.Ms, d.Us, d.Ns = r.Ms, r.Us, r.Ns
	}
	if m[1] == "-" {
		d = d.negated()
	}
	return d, nil
}

// ---------------------------------------------------------------- from()

func dictInt(d *Dict, key string) (int64, bool, error) {
	v, ok := d.Get(key)
	if !ok || v == nil {
		return 0, false, nil
	}
	switch x := v.(type) {
	case int64:
		return x, true, nil
	case float64:
		if x == math.Trunc(x) {
			return int64(x), true, nil
		}
	}
	return 0, false, typeErr("%s must be an Integer, not %s", key, TypeName(v))
}

func clampInt(v int64, lo, hi int) int {
	if v < int64(lo) {
		return lo
	}
	if v > int64(hi) {
		return hi
	}
	return int(v)
}

func dateFromDict(d *Dict, base *PlainDate) (PlainDate, error) {
	var out PlainDate
	if base != nil {
		out = *base
	}
	get := func(k string, dst *int, lo, hi func() int, required bool) error {
		n, ok, err := dictInt(d, k)
		if err != nil {
			return err
		}
		if !ok {
			if required && base == nil {
				return typeErr("missing %s", k)
			}
			return nil
		}
		*dst = clampInt(n, lo(), hi())
		return nil
	}
	if err := get("year", &out.Y, func() int { return -271821 }, func() int { return 275760 }, true); err != nil {
		return out, err
	}
	if err := get("month", &out.M, func() int { return 1 }, func() int { return 12 }, true); err != nil {
		return out, err
	}
	out.D = min(out.D, daysInMonth(out.Y, max(out.M, 1)))
	if err := get("day", &out.D, func() int { return 1 }, func() int { return daysInMonth(out.Y, out.M) }, true); err != nil {
		return out, err
	}
	return out, nil
}

func timeFromDict(d *Dict, base PlainTime) (PlainTime, error) {
	out := base
	ms, us, ns := out.Ns/1_000_000, out.Ns/1_000%1_000, out.Ns%1_000
	for _, f := range []struct {
		k      string
		dst    *int
		lo, hi int
	}{{"hour", &out.H, 0, 23}, {"minute", &out.Mi, 0, 59}, {"second", &out.S, 0, 59},
		{"millisecond", &ms, 0, 999}, {"microsecond", &us, 0, 999}, {"nanosecond", &ns, 0, 999}} {
		n, ok, err := dictInt(d, f.k)
		if err != nil {
			return out, err
		}
		if ok {
			*f.dst = clampInt(n, f.lo, f.hi)
		}
	}
	out.Ns = ms*1_000_000 + us*1_000 + ns
	return out, nil
}

func durationFromDict(d *Dict) (Duration, error) {
	f := make([]int64, 10)
	any := false
	for _, k := range d.Keys() {
		idx := -1
		for i, u := range durationUnits {
			if u == k {
				idx = i
			}
		}
		if idx < 0 {
			return Duration{}, typeErr("unknown duration field %q", k)
		}
		n, ok, err := dictInt(d, k)
		if err != nil {
			return Duration{}, err
		}
		if ok {
			f[idx], any = n, true
		}
	}
	if !any && d.Len() > 0 {
		return Duration{}, typeErr("a duration needs at least one field")
	}
	return checkSigns(durationFromFields(f))
}

func checkSigns(d Duration) (Duration, error) {
	s := 0
	for _, v := range d.fields() {
		if v == 0 {
			continue
		}
		vs := 1
		if v < 0 {
			vs = -1
		}
		if s != 0 && vs != s {
			return Duration{}, typeErr("duration fields must all have the same sign")
		}
		s = vs
	}
	return d, nil
}

func toDuration(v Value) (Duration, error) {
	switch x := v.(type) {
	case Duration:
		return x, nil
	case string:
		return parseDuration(x)
	case *Dict:
		return durationFromDict(x)
	}
	return Duration{}, typeErr("expected a Temporal.Duration, not %s", TypeName(v))
}

// temporalFrom implements Temporal.X.from(v).
func (ev *evaluator) temporalFrom(typ string, v Value) (Value, error) {
	switch typ {
	case "Duration":
		return toDuration(v)
	}
	switch x := v.(type) {
	case string:
		return temporalFromString(typ, x)
	case *Dict:
		return fromDict(typ, x)
	case temporal:
		return convertTemporal(typ, x)
	}
	return nil, typeErr("Temporal.%s.from() takes a String or a Dictionary, not %s", typ, TypeName(v))
}

func temporalFromString(typ, s string) (Value, error) {
	if typ == "PlainMonthDay" {
		t := strings.TrimPrefix(strings.TrimSpace(s), "--")
		if m := regexp.MustCompile(`^(\d{2})-?(\d{2})$`).FindStringSubmatch(t); m != nil {
			mo, _ := strconv.Atoi(m[1])
			d, _ := strconv.Atoi(m[2])
			if mo < 1 || mo > 12 || d < 1 || d > daysInMonth(2000, mo) {
				return nil, typeErr("month-day out of range in %q", s)
			}
			return PlainMonthDay{mo, d}, nil
		}
	}
	if typ == "PlainYearMonth" {
		if m := regexp.MustCompile(`^([+-]\d{6}|\d{4})-(\d{2})$`).FindStringSubmatch(strings.TrimSpace(s)); m != nil {
			y, _ := strconv.Atoi(m[1])
			mo, _ := strconv.Atoi(m[2])
			if mo < 1 || mo > 12 {
				return nil, typeErr("month out of range in %q", s)
			}
			return PlainYearMonth{y, mo}, nil
		}
	}
	p, err := parseISO(s)
	if err != nil {
		return nil, err
	}
	switch typ {
	case "PlainDate", "PlainYearMonth", "PlainMonthDay":
		if !p.hasDate || p.z {
			return nil, typeErr("invalid %s %q", typ, s)
		}
		switch typ {
		case "PlainYearMonth":
			return PlainYearMonth{p.date.Y, p.date.M}, nil
		case "PlainMonthDay":
			return PlainMonthDay{p.date.M, p.date.D}, nil
		}
		return p.date, nil
	case "PlainTime":
		if !p.hasTime || p.z {
			return nil, typeErr("invalid PlainTime %q", s)
		}
		return p.time, nil
	case "PlainDateTime":
		if !p.hasDate || p.z {
			return nil, typeErr("invalid PlainDateTime %q", s)
		}
		return PlainDateTime{p.date, p.time}, nil
	case "Instant":
		if !p.hasDate || !(p.z || p.hasOff) {
			return nil, typeErr("an Instant needs a Z or an offset: %q", s)
		}
		return Instant{NS: PlainDateTime{p.date, p.time}.nanosOf() - int64(p.offset)*nsPerSec}, nil
	case "ZonedDateTime":
		if !p.hasDate || p.zone == "" {
			return nil, typeErr("a ZonedDateTime needs a [time zone]: %q", s)
		}
		loc, id, err := loadZone(p.zone)
		if err != nil {
			return nil, err
		}
		dt := PlainDateTime{p.date, p.time}
		var ns int64
		switch {
		case p.z:
			ns = dt.nanosOf()
		case p.hasOff:
			ns = dt.nanosOf() - int64(p.offset)*nsPerSec
			if offsetAt(loc, floorDiv(ns, nsPerSec)) != int64(p.offset) {
				return nil, typeErr("offset %s is invalid for %s at %s", offsetString(p.offset), id, dt)
			}
		default:
			ns = resolveWall(dt, loc)
		}
		return ZonedDateTime{NS: ns, Loc: loc, Zone: id}, nil
	}
	return nil, typeErr("unknown Temporal type %s", typ)
}

func fromDict(typ string, d *Dict) (Value, error) {
	switch typ {
	case "PlainDate":
		return dateFromDict(d, nil)
	case "PlainTime":
		return timeFromDict(d, PlainTime{})
	case "PlainDateTime":
		date, err := dateFromDict(d, nil)
		if err != nil {
			return nil, err
		}
		t, err := timeFromDict(d, PlainTime{})
		if err != nil {
			return nil, err
		}
		return PlainDateTime{date, t}, nil
	case "PlainYearMonth":
		dd := d.Copy()
		if _, ok := dd.Get("day"); !ok {
			dd.Set("day", int64(1))
		}
		date, err := dateFromDict(dd, nil)
		if err != nil {
			return nil, err
		}
		return PlainYearMonth{date.Y, date.M}, nil
	case "PlainMonthDay":
		dd := d.Copy()
		if _, ok := dd.Get("year"); !ok {
			dd.Set("year", int64(1972))
		}
		date, err := dateFromDict(dd, nil)
		if err != nil {
			return nil, err
		}
		return PlainMonthDay{date.M, date.D}, nil
	case "ZonedDateTime":
		tz, _ := d.Lookup("timeZone").(string)
		if tz == "" {
			return nil, typeErr("ZonedDateTime.from() needs timeZone")
		}
		loc, id, err := loadZone(tz)
		if err != nil {
			return nil, err
		}
		dd := d.Copy()
		dd.Delete("timeZone")
		dt, err := fromDict("PlainDateTime", dd)
		if err != nil {
			return nil, err
		}
		return ZonedDateTime{NS: resolveWall(dt.(PlainDateTime), loc), Loc: loc, Zone: id}, nil
	case "Instant":
		return nil, typeErr("Temporal.Instant.from() takes an ISO string")
	}
	return nil, typeErr("unknown Temporal type %s", typ)
}

func convertTemporal(typ string, t temporal) (Value, error) {
	if t.temporalType() == typ {
		return t, nil
	}
	switch x := t.(type) {
	case PlainDateTime:
		switch typ {
		case "PlainDate":
			return x.Date, nil
		case "PlainTime":
			return x.Time, nil
		}
	case ZonedDateTime:
		w := x.wall()
		switch typ {
		case "PlainDate":
			return w.Date, nil
		case "PlainTime":
			return w.Time, nil
		case "PlainDateTime":
			return w, nil
		case "Instant":
			return Instant{x.NS}, nil
		}
	case PlainDate:
		switch typ {
		case "PlainDateTime":
			return PlainDateTime{Date: x}, nil
		case "PlainYearMonth":
			return PlainYearMonth{x.Y, x.M}, nil
		case "PlainMonthDay":
			return PlainMonthDay{x.M, x.D}, nil
		}
	}
	return nil, typeErr("cannot convert Temporal.%s to Temporal.%s", t.temporalType(), typ)
}

// parseDateTime implements String.parseDateTime() (R-SESSEL-123).
func parseDateTime(s string) Value {
	p, err := parseISO(s)
	if err != nil || !p.hasDate {
		return nil
	}
	dt := PlainDateTime{p.date, p.time}
	switch {
	case p.zone != "":
		v, err := temporalFromString("ZonedDateTime", s)
		if err != nil {
			return nil
		}
		return v
	case p.z || p.hasOff:
		return Instant{NS: dt.nanosOf() - int64(p.offset)*nsPerSec}
	case p.hasTime:
		return dt
	}
	return p.date
}

// ---------------------------------------------------------------- equality / order

func temporalEquals(a, b temporal) bool {
	if a.temporalType() != b.temporalType() {
		return false
	}
	switch x := a.(type) {
	case ZonedDateTime:
		y := b.(ZonedDateTime)
		return x.NS == y.NS && x.Zone == y.Zone
	case Duration:
		return x == b.(Duration)
	case PlainMonthDay:
		return x == b.(PlainMonthDay)
	}
	c, ok := temporalCompare(a, b)
	return ok && c == 0
}

func temporalCompare(a, b temporal) (int, bool) {
	if a.temporalType() != b.temporalType() {
		return 0, false
	}
	switch x := a.(type) {
	case PlainDate:
		return cmpDate(x, b.(PlainDate)), true
	case PlainTime:
		y := b.(PlainTime)
		return cmp3(x.nanos() < y.nanos(), x.nanos() > y.nanos()), true
	case PlainDateTime:
		y := b.(PlainDateTime)
		return cmp3(x.nanosOf() < y.nanosOf(), x.nanosOf() > y.nanosOf()), true
	case Instant:
		y := b.(Instant)
		return cmp3(x.NS < y.NS, x.NS > y.NS), true
	case ZonedDateTime:
		y := b.(ZonedDateTime)
		return cmp3(x.NS < y.NS, x.NS > y.NS), true
	case PlainYearMonth:
		y := b.(PlainYearMonth)
		return cmpDate(PlainDate{x.Y, x.M, 1}, PlainDate{y.Y, y.M, 1}), true
	case Duration:
		y := b.(Duration)
		if x.hasCalendar() || y.hasCalendar() {
			return 0, false
		}
		xn, yn := x.timeNanos()+x.Days*nsPerDay, y.timeNanos()+y.Days*nsPerDay
		return cmp3(xn < yn, xn > yn), true
	}
	return 0, false
}

func (d Duration) hasCalendar() bool { return d.Years != 0 || d.Months != 0 || d.Weeks != 0 }

func (d Duration) timeNanos() int64 {
	return d.Hours*nsPerHour + d.Minutes*nsPerMin + d.Seconds*nsPerSec + d.Ms*1_000_000 + d.Us*1_000 + d.Ns
}

func unitIndex(u string) int {
	u = strings.ToLower(u)
	if !strings.HasSuffix(u, "s") {
		u += "s"
	}
	for i, x := range durationUnits {
		if x == u {
			return i
		}
	}
	return -1
}

func (d Duration) largestUnit() int {
	for i, f := range d.fields() {
		if f != 0 {
			return i
		}
	}
	return 6 // seconds
}

// balanceNanos distributes ns over units from largest (index) down.
func balanceNanos(ns int64, largest int) Duration {
	f := make([]int64, 10)
	sizes := []int64{0, 0, 0, nsPerDay, nsPerHour, nsPerMin, nsPerSec, 1_000_000, 1_000, 1}
	if largest < 3 {
		largest = 3
	}
	for i := largest; i < 10; i++ {
		f[i] = ns / sizes[i]
		ns -= f[i] * sizes[i]
	}
	return durationFromFields(f)
}
