package sessel

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- accessors

func dateAccessor(d PlainDate, name string) (Value, bool) {
	switch name {
	case "year":
		return int64(d.Y), true
	case "month":
		return int64(d.M), true
	case "day":
		return int64(d.D), true
	case "monthCode":
		return "M" + pad(d.M, 2), true
	case "dayOfWeek":
		return int64(d.dayOfWeek()), true
	case "dayOfYear":
		return int64(d.dayOfYear()), true
	case "weekOfYear":
		_, w := d.isoWeek()
		return int64(w), true
	case "yearOfWeek":
		y, _ := d.isoWeek()
		return int64(y), true
	case "daysInMonth":
		return int64(daysInMonth(d.Y, d.M)), true
	case "daysInYear":
		return int64(daysInYear(d.Y)), true
	case "daysInWeek":
		return int64(7), true
	case "monthsInYear":
		return int64(12), true
	case "inLeapYear":
		return isLeap(d.Y), true
	}
	return nil, false
}

func timeAccessor(t PlainTime, name string) (Value, bool) {
	switch name {
	case "hour":
		return int64(t.H), true
	case "minute":
		return int64(t.Mi), true
	case "second":
		return int64(t.S), true
	case "millisecond":
		return int64(t.Ns / 1_000_000), true
	case "microsecond":
		return int64(t.Ns / 1_000 % 1_000), true
	case "nanosecond":
		return int64(t.Ns % 1_000), true
	}
	return nil, false
}

func epochAccessor(ns int64, name string) (Value, bool) {
	switch name {
	case "epochSeconds":
		return floorDiv(ns, nsPerSec), true
	case "epochMilliseconds":
		return floorDiv(ns, 1_000_000), true
	case "epochMicroseconds":
		return floorDiv(ns, 1_000), true
	case "epochNanoseconds":
		return ns, true
	}
	return nil, false
}

// temporalAccessor implements member access on Temporal values.
func temporalAccessor(t temporal, name string) (Value, error) {
	var v Value
	ok := false
	switch x := t.(type) {
	case PlainDate:
		v, ok = dateAccessor(x, name)
	case PlainTime:
		v, ok = timeAccessor(x, name)
	case PlainDateTime:
		if v, ok = dateAccessor(x.Date, name); !ok {
			v, ok = timeAccessor(x.Time, name)
		}
	case Instant:
		v, ok = epochAccessor(x.NS, name)
	case ZonedDateTime:
		w := x.wall()
		if v, ok = dateAccessor(w.Date, name); !ok {
			if v, ok = timeAccessor(w.Time, name); !ok {
				if v, ok = epochAccessor(x.NS, name); !ok {
					switch name {
					case "timeZoneId", "timeZone":
						v, ok = x.Zone, true
					case "offset":
						v, ok = offsetString(x.offsetSeconds()), true
					case "offsetNanoseconds":
						v, ok = int64(x.offsetSeconds())*nsPerSec, true
					case "hoursInDay":
						start := resolveWall(PlainDateTime{Date: w.Date}, x.Loc)
						end := resolveWall(PlainDateTime{Date: w.Date.addDays(1)}, x.Loc)
						v, ok = int64((end-start)/nsPerHour), true
					}
				}
			}
		}
	case Duration:
		for i, u := range durationUnits {
			if u == name {
				return x.fields()[i], nil
			}
		}
		switch name {
		case "sign":
			return int64(x.sign()), nil
		case "blank":
			return x.sign() == 0, nil
		}
	case PlainYearMonth:
		d := PlainDate{x.Y, x.M, 1}
		switch name {
		case "year", "month", "monthCode", "daysInMonth", "daysInYear", "inLeapYear", "monthsInYear":
			v, ok = dateAccessor(d, name)
		}
	case PlainMonthDay:
		switch name {
		case "month":
			v, ok = int64(x.M), true
		case "day":
			v, ok = int64(x.D), true
		case "monthCode":
			v, ok = "M"+pad(x.M, 2), true
		}
	}
	if !ok {
		return nil, typeErr("Temporal.%s has no member %q", t.temporalType(), name)
	}
	return v, nil
}

// ---------------------------------------------------------------- arithmetic

func (d Duration) scale(sign int) Duration {
	if sign < 0 {
		return d.negated()
	}
	return d
}

func addToDate(d PlainDate, dur Duration) (PlainDate, error) {
	r, err := d.addMonths(dur.Years*12+dur.Months, true)
	if err != nil {
		return r, err
	}
	return r.addDays(dur.Weeks*7 + dur.Days + dur.timeNanos()/nsPerDay), nil
}

func addToDateTime(dt PlainDateTime, dur Duration) (PlainDateTime, error) {
	tn := dt.Time.nanos() + dur.timeNanos()
	carry := floorDiv(tn, nsPerDay)
	date, err := dt.Date.addMonths(dur.Years*12+dur.Months, true)
	if err != nil {
		return dt, err
	}
	return PlainDateTime{Date: date.addDays(dur.Weeks*7 + dur.Days + carry), Time: timeFromNanos(tn)}, nil
}

func addToZoned(z ZonedDateTime, dur Duration) ZonedDateTime {
	ns := z.NS
	if dur.hasCalendar() || dur.Days != 0 {
		w := z.wall()
		date, _ := w.Date.addMonths(dur.Years*12+dur.Months, true)
		date = date.addDays(dur.Weeks*7 + dur.Days)
		ns = resolveWall(PlainDateTime{Date: date, Time: w.Time}, z.Loc)
	}
	return ZonedDateTime{NS: ns + dur.timeNanos(), Loc: z.Loc, Zone: z.Zone}
}

// relativeStart converts a relativeTo value to a wall date-time and zone.
func relativeStart(v Value) (PlainDateTime, *ZonedDateTime, error) {
	switch x := v.(type) {
	case PlainDate:
		return PlainDateTime{Date: x}, nil, nil
	case PlainDateTime:
		return x, nil, nil
	case ZonedDateTime:
		return x.wall(), &x, nil
	case string:
		p, err := parseISO(x)
		if err != nil || !p.hasDate {
			return PlainDateTime{}, nil, typeErr("invalid relativeTo %q", x)
		}
		return PlainDateTime{p.date, p.time}, nil, nil
	}
	return PlainDateTime{}, nil, typeErr("relativeTo must be a PlainDate, PlainDateTime or ZonedDateTime, not %s", TypeName(v))
}

// dateDiff is the calendar difference a→b with the given largest unit
// index (0 years, 1 months, 2 weeks, 3 days).
func dateDiff(a, b PlainDate, largest int) Duration {
	if largest >= 3 {
		return Duration{Days: b.days() - a.days()}
	}
	if largest == 2 {
		days := b.days() - a.days()
		return Duration{Weeks: days / 7, Days: days % 7}
	}
	sign := cmpDate(b, a)
	if sign == 0 {
		return Duration{}
	}
	total := int64(b.Y-a.Y)*12 + int64(b.M-a.M)
	for {
		mid, _ := a.addMonths(total, true)
		c := cmpDate(mid, b)
		if (sign > 0 && c > 0) || (sign < 0 && c < 0) {
			total -= int64(sign)
			continue
		}
		break
	}
	mid, _ := a.addMonths(total, true)
	d := Duration{Days: b.days() - mid.days()}
	if largest == 0 {
		d.Years, d.Months = total/12, total%12
	} else {
		d.Months = total
	}
	return d
}

func dateTimeDiff(a, b PlainDateTime, largest int) Duration {
	total := b.nanosOf() - a.nanosOf()
	if largest >= 3 {
		return balanceNanos(total, largest)
	}
	td := b.Time.nanos() - a.Time.nanos()
	end := b.Date
	switch {
	case total > 0 && td < 0:
		end, td = end.addDays(-1), td+nsPerDay
	case total < 0 && td > 0:
		end, td = end.addDays(1), td-nsPerDay
	}
	d := dateDiff(a.Date, end, largest)
	t := balanceNanos(td, 4)
	d.Hours, d.Minutes, d.Seconds, d.Ms, d.Us, d.Ns = t.Hours, t.Minutes, t.Seconds, t.Ms, t.Us, t.Ns
	return d
}

// largestUnitOption reads { largestUnit } from an until/since options
// argument; def is the default unit index.
func largestUnitOption(args []Value, def int) (int, error) {
	if len(args) < 2 || args[1] == nil {
		return def, nil
	}
	opts, ok := args[1].(*Dict)
	if !ok {
		return 0, typeErr("options must be a Dictionary")
	}
	if u, ok := opts.Lookup("largestUnit").(string); ok && u != "auto" {
		i := unitIndex(u)
		if i < 0 {
			return 0, typeErr("unknown unit %q", u)
		}
		return i, nil
	}
	return def, nil
}

// ---------------------------------------------------------------- methods

func (ev *evaluator) temporalMethod(t temporal, name string, args []Value) (Value, bool, error) {
	switch name {
	case "equals":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		o, err := ev.coerceTemporal(t.temporalType(), args[0])
		if err != nil {
			return nil, true, err
		}
		return temporalEquals(t, o), true, nil
	case "toString", "String", "toJSON":
		return t.String(), true, nil
	case "format":
		p, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		s, err := formatTemporal(t, p)
		return s, true, err
	case "toLocaleString":
		loc := "en-US"
		if len(args) > 0 && args[0] != nil {
			s, err := strArg(name, args, 0)
			if err != nil {
				return nil, true, err
			}
			loc = s
		}
		s, err := localeString(t, loc)
		return s, true, err
	}
	var (
		v   Value
		ok  = true
		err error
	)
	switch x := t.(type) {
	case PlainDate:
		v, ok, err = ev.plainDateMethod(x, name, args)
	case PlainTime:
		v, ok, err = ev.plainTimeMethod(x, name, args)
	case PlainDateTime:
		v, ok, err = ev.plainDateTimeMethod(x, name, args)
	case Instant:
		v, ok, err = ev.instantMethod(x, name, args)
	case ZonedDateTime:
		v, ok, err = ev.zonedMethod(x, name, args)
	case Duration:
		v, ok, err = ev.durationMethod(x, name, args)
	case PlainYearMonth:
		v, ok, err = ev.yearMonthMethod(x, name, args)
	case PlainMonthDay:
		v, ok, err = ev.monthDayMethod(x, name, args)
	}
	if !ok && err == nil && len(args) == 0 {
		// Accessors are also zero-argument methods (d.dayOfWeek()), the
		// only form live PageLove answers (2026-09-29); pagelike keeps the
		// documented property form too.
		if a, aerr := temporalAccessor(t, name); aerr == nil {
			return a, true, nil
		}
	}
	return v, ok, err
}

func (ev *evaluator) coerceTemporal(typ string, v Value) (temporal, error) {
	if t, ok := v.(temporal); ok && t.temporalType() == typ {
		return t, nil
	}
	r, err := ev.temporalFrom(typ, v)
	if err != nil {
		return nil, err
	}
	return r.(temporal), nil
}

func durArg(name string, args []Value) (Duration, error) {
	if len(args) < 1 {
		return Duration{}, typeErr("%s() takes a Duration", name)
	}
	return toDuration(args[0])
}

func dictArg(name string, args []Value) (*Dict, error) {
	if len(args) != 1 {
		return nil, typeErr("%s() takes a Dictionary", name)
	}
	d, ok := args[0].(*Dict)
	if !ok {
		return nil, typeErr("%s() takes a Dictionary, not %s", name, TypeName(args[0]))
	}
	return d, nil
}

func zoneArg(v Value) (*time.Location, string, error) {
	switch x := v.(type) {
	case string:
		return loadZone(x)
	case *Dict:
		if s, ok := x.Lookup("timeZone").(string); ok {
			return loadZone(s)
		}
	}
	return nil, "", typeErr("expected a time zone name, not %s", TypeName(v))
}

func signOf(name string) int {
	if name == "subtract" {
		return -1
	}
	return 1
}

func (ev *evaluator) plainDateMethod(d PlainDate, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		r, err := addToDate(d, dur.scale(signOf(name)))
		return r, true, err
	case "until", "since":
		o, err := ev.coerceTemporal("PlainDate", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 3)
		if err != nil {
			return nil, true, err
		}
		a, b := d, o.(PlainDate)
		if name == "since" {
			a, b = b, a
		}
		if lu > 3 {
			return balanceNanos((b.days()-a.days())*nsPerDay, lu), true, nil
		}
		return dateDiff(a, b, lu), true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		r, err := dateFromDict(dd, &d)
		return r, true, err
	case "toPlainDateTime":
		t := PlainTime{}
		if len(args) > 0 && args[0] != nil {
			tt, err := ev.coerceTemporal("PlainTime", args[0])
			if err != nil {
				return nil, true, err
			}
			t = tt.(PlainTime)
		}
		return PlainDateTime{Date: d, Time: t}, true, nil
	case "toZonedDateTime":
		if len(args) != 1 {
			return nil, true, typeErr("toZonedDateTime() takes a time zone")
		}
		loc, id, err := zoneArg(args[0])
		if err != nil {
			return nil, true, err
		}
		t := PlainTime{}
		if dd, ok := args[0].(*Dict); ok {
			if pt := dd.Lookup("plainTime"); pt != nil {
				tt, err := ev.coerceTemporal("PlainTime", pt)
				if err != nil {
					return nil, true, err
				}
				t = tt.(PlainTime)
			}
		}
		return ZonedDateTime{NS: resolveWall(PlainDateTime{Date: d, Time: t}, loc), Loc: loc, Zone: id}, true, nil
	case "toPlainYearMonth":
		return PlainYearMonth{d.Y, d.M}, true, nil
	case "toPlainMonthDay":
		return PlainMonthDay{d.M, d.D}, true, nil
	}
	return nil, false, nil
}

func argOrNil(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return nil
}

func (ev *evaluator) plainTimeMethod(t PlainTime, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		return timeFromNanos(t.nanos() + dur.scale(signOf(name)).timeNanos()), true, nil
	case "until", "since":
		o, err := ev.coerceTemporal("PlainTime", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 4)
		if err != nil {
			return nil, true, err
		}
		diff := o.(PlainTime).nanos() - t.nanos()
		if name == "since" {
			diff = -diff
		}
		return balanceNanos(diff, max(lu, 4)), true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		r, err := timeFromDict(dd, t)
		return r, true, err
	case "toPlainDateTime":
		dv, err := ev.coerceTemporal("PlainDate", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		return PlainDateTime{Date: dv.(PlainDate), Time: t}, true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) plainDateTimeMethod(dt PlainDateTime, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		r, err := addToDateTime(dt, dur.scale(signOf(name)))
		return r, true, err
	case "until", "since":
		o, err := ev.coerceTemporal("PlainDateTime", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 3)
		if err != nil {
			return nil, true, err
		}
		a, b := dt, o.(PlainDateTime)
		if name == "since" {
			a, b = b, a
		}
		return dateTimeDiff(a, b, lu), true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		date, err := dateFromDict(dd, &dt.Date)
		if err != nil {
			return nil, true, err
		}
		t, err := timeFromDict(dd, dt.Time)
		return PlainDateTime{date, t}, true, err
	case "withPlainTime":
		t := PlainTime{}
		if len(args) > 0 && args[0] != nil {
			tt, err := ev.coerceTemporal("PlainTime", args[0])
			if err != nil {
				return nil, true, err
			}
			t = tt.(PlainTime)
		}
		return PlainDateTime{dt.Date, t}, true, nil
	case "toPlainDate":
		return dt.Date, true, nil
	case "toPlainTime":
		return dt.Time, true, nil
	case "toZonedDateTime":
		if len(args) != 1 {
			return nil, true, typeErr("toZonedDateTime() takes a time zone")
		}
		loc, id, err := zoneArg(args[0])
		if err != nil {
			return nil, true, err
		}
		return ZonedDateTime{NS: resolveWall(dt, loc), Loc: loc, Zone: id}, true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) instantMethod(i Instant, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		if dur.hasCalendar() || dur.Days != 0 {
			return nil, true, typeErr("an Instant can only add hours or smaller units")
		}
		return Instant{NS: i.NS + dur.scale(signOf(name)).timeNanos()}, true, nil
	case "until", "since":
		o, err := ev.coerceTemporal("Instant", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 4)
		if err != nil {
			return nil, true, err
		}
		diff := o.(Instant).NS - i.NS
		if name == "since" {
			diff = -diff
		}
		return balanceNanos(diff, max(lu, 3)), true, nil
	case "toZonedDateTimeISO", "toZonedDateTime":
		if len(args) != 1 {
			return nil, true, typeErr("%s() takes a time zone", name)
		}
		loc, id, err := zoneArg(args[0])
		if err != nil {
			return nil, true, err
		}
		return ZonedDateTime{NS: i.NS, Loc: loc, Zone: id}, true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) zonedMethod(z ZonedDateTime, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		return addToZoned(z, dur.scale(signOf(name))), true, nil
	case "until", "since":
		o, err := ev.coerceTemporal("ZonedDateTime", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 4)
		if err != nil {
			return nil, true, err
		}
		a, b := z, o.(ZonedDateTime)
		if name == "since" {
			a, b = b, a
		}
		if lu <= 3 {
			return dateTimeDiff(a.wall(), b.withZone(a.Loc, a.Zone).wall(), lu), true, nil
		}
		return balanceNanos(b.NS-a.NS, lu), true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		loc, id := z.Loc, z.Zone
		if tz, ok := dd.Lookup("timeZone").(string); ok {
			l, i, err := loadZone(tz)
			if err != nil {
				return nil, true, err
			}
			loc, id = l, i
		}
		w := z.wall()
		date, err := dateFromDict(dd, &w.Date)
		if err != nil {
			return nil, true, err
		}
		t, err := timeFromDict(dd, w.Time)
		if err != nil {
			return nil, true, err
		}
		return ZonedDateTime{NS: resolveWall(PlainDateTime{date, t}, loc), Loc: loc, Zone: id}, true, nil
	case "withTimeZone":
		if len(args) != 1 {
			return nil, true, typeErr("withTimeZone() takes a time zone")
		}
		loc, id, err := zoneArg(args[0])
		if err != nil {
			return nil, true, err
		}
		return z.withZone(loc, id), true, nil
	case "withPlainTime":
		t := PlainTime{}
		if len(args) > 0 && args[0] != nil {
			tt, err := ev.coerceTemporal("PlainTime", args[0])
			if err != nil {
				return nil, true, err
			}
			t = tt.(PlainTime)
		}
		return ZonedDateTime{NS: resolveWall(PlainDateTime{z.wall().Date, t}, z.Loc), Loc: z.Loc, Zone: z.Zone}, true, nil
	case "startOfDay":
		return ZonedDateTime{NS: resolveWall(PlainDateTime{Date: z.wall().Date}, z.Loc), Loc: z.Loc, Zone: z.Zone}, true, nil
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

func (z ZonedDateTime) withZone(loc *time.Location, id string) ZonedDateTime {
	return ZonedDateTime{NS: z.NS, Loc: loc, Zone: id}
}

func (ev *evaluator) durationMethod(d Duration, name string, args []Value) (Value, bool, error) {
	switch name {
	case "negated":
		return d.negated(), true, nil
	case "abs":
		return d.abs(), true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		f := d.fields()
		for _, k := range dd.Keys() {
			idx := -1
			for i, u := range durationUnits {
				if u == k {
					idx = i
				}
			}
			if idx < 0 {
				return nil, true, typeErr("unknown duration field %q", k)
			}
			n, ok, err := dictInt(dd, k)
			if err != nil {
				return nil, true, err
			}
			if ok {
				f[idx] = n
			}
		}
		r, err := checkSigns(durationFromFields(f))
		return r, true, err
	case "add", "subtract":
		if len(args) < 1 || len(args) > 2 {
			return nil, true, typeErr("%s() takes a Duration and an optional relativeTo", name)
		}
		o, err := toDuration(args[0])
		if err != nil {
			return nil, true, err
		}
		o = o.scale(signOf(name))
		var rel Value
		if len(args) == 2 {
			rel = args[1]
			if dd, ok := rel.(*Dict); ok {
				rel = dd.Lookup("relativeTo")
			}
		}
		r, err := addDurations(d, o, rel)
		return r, true, err
	case "total":
		if len(args) < 1 || len(args) > 2 {
			return nil, true, typeErr("total() takes a unit and an optional relativeTo")
		}
		var unit string
		var rel Value
		switch x := args[0].(type) {
		case string:
			unit = x
		case *Dict:
			unit, _ = x.Lookup("unit").(string)
			rel = x.Lookup("relativeTo")
		default:
			return nil, true, typeErr("total() takes a unit name")
		}
		if len(args) == 2 {
			rel = args[1]
		}
		r, err := durationTotal(d, unit, rel)
		return r, true, err
	}
	return nil, false, nil
}

func addDurations(a, b Duration, rel Value) (Value, error) {
	if !a.hasCalendar() && !b.hasCalendar() {
		total := (a.Days+b.Days)*nsPerDay + a.timeNanos() + b.timeNanos()
		return balanceNanos(total, min(a.largestUnit(), b.largestUnit())), nil
	}
	if rel == nil {
		return nil, typeErr("adding durations with years, months or weeks needs a relativeTo")
	}
	start, _, err := relativeStart(rel)
	if err != nil {
		return nil, err
	}
	fa, fb := a.fields(), b.fields()
	sum := make([]int64, 10)
	for i := range sum {
		sum[i] = fa[i] + fb[i]
	}
	r := durationFromFields(sum)
	if a.sign()*b.sign() >= 0 {
		// same direction: component-wise, time balanced up to hours
		t := balanceNanos(r.timeNanos(), 4)
		r.Hours, r.Minutes, r.Seconds, r.Ms, r.Us, r.Ns = t.Hours, t.Minutes, t.Seconds, t.Ms, t.Us, t.Ns
		return r, nil
	}
	mid, err := addToDateTime(start, a)
	if err != nil {
		return nil, err
	}
	end, err := addToDateTime(mid, b)
	if err != nil {
		return nil, err
	}
	return dateTimeDiff(start, end, min(a.largestUnit(), b.largestUnit())), nil
}

func numberResult(f float64) (Value, error) {
	if f == math.Trunc(f) && math.Abs(f) < 9e18 {
		return int64(f), nil
	}
	return checkFloat(f)
}

func durationTotal(d Duration, unit string, rel Value) (Value, error) {
	ui := unitIndex(unit)
	if ui < 0 {
		return nil, typeErr("unknown unit %q", unit)
	}
	sizes := []int64{0, 0, 7 * nsPerDay, nsPerDay, nsPerHour, nsPerMin, nsPerSec, 1_000_000, 1_000, 1}
	if !d.hasCalendar() && ui >= 2 {
		total := d.Days*nsPerDay + d.timeNanos()
		return numberResult(float64(total) / float64(sizes[ui]))
	}
	if rel == nil {
		return nil, typeErr("total() of calendar units needs a relativeTo")
	}
	start, _, err := relativeStart(rel)
	if err != nil {
		return nil, err
	}
	end, err := addToDateTime(start, d)
	if err != nil {
		return nil, err
	}
	span := end.nanosOf() - start.nanosOf()
	if ui >= 2 {
		return numberResult(float64(span) / float64(sizes[ui]))
	}
	step := int64(1)
	if ui == 0 {
		step = 12
	}
	months := int64(end.Date.Y-start.Date.Y)*12 + int64(end.Date.M-start.Date.M)
	k := months / step
	at := func(n int64) int64 {
		dd, _ := start.Date.addMonths(n*step, true)
		return PlainDateTime{dd, start.Time}.nanosOf()
	}
	for span >= 0 && at(k) > end.nanosOf() {
		k--
	}
	for span >= 0 && at(k+1) <= end.nanosOf() {
		k++
	}
	for span < 0 && at(k) < end.nanosOf() {
		k++
	}
	for span < 0 && at(k-1) >= end.nanosOf() {
		k--
	}
	lo := at(k)
	next := k + 1
	if span < 0 {
		next = k - 1
	}
	hi := at(next)
	frac := 0.0
	if hi != lo {
		frac = float64(end.nanosOf()-lo) / float64(hi-lo)
		if span < 0 {
			frac = -frac
		}
	}
	return numberResult(float64(k) + frac)
}

func (ev *evaluator) yearMonthMethod(ym PlainYearMonth, name string, args []Value) (Value, bool, error) {
	switch name {
	case "add", "subtract":
		dur, err := durArg(name, args)
		if err != nil {
			return nil, true, err
		}
		dur = dur.scale(signOf(name))
		d, _ := PlainDate{ym.Y, ym.M, 1}.addMonths(dur.Years*12+dur.Months, true)
		return PlainYearMonth{d.Y, d.M}, true, nil
	case "until", "since":
		o, err := ev.coerceTemporal("PlainYearMonth", argOrNil(args, 0))
		if err != nil {
			return nil, true, err
		}
		lu, err := largestUnitOption(args, 0)
		if err != nil {
			return nil, true, err
		}
		b := o.(PlainYearMonth)
		months := int64(b.Y-ym.Y)*12 + int64(b.M-ym.M)
		if name == "since" {
			months = -months
		}
		if lu == 0 {
			return Duration{Years: months / 12, Months: months % 12}, true, nil
		}
		return Duration{Months: months}, true, nil
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		base := PlainDate{ym.Y, ym.M, 1}
		d, err := dateFromDict(dd, &base)
		return PlainYearMonth{d.Y, d.M}, true, err
	case "toPlainDate":
		day := int64(1)
		switch x := argOrNil(args, 0).(type) {
		case int64:
			day = x
		case *Dict:
			n, ok, err := dictInt(x, "day")
			if err != nil {
				return nil, true, err
			}
			if ok {
				day = n
			}
		default:
			return nil, true, typeErr("toPlainDate() takes a day")
		}
		return PlainDate{ym.Y, ym.M, clampInt(day, 1, daysInMonth(ym.Y, ym.M))}, true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) monthDayMethod(md PlainMonthDay, name string, args []Value) (Value, bool, error) {
	switch name {
	case "with":
		dd, err := dictArg(name, args)
		if err != nil {
			return nil, true, err
		}
		base := PlainDate{1972, md.M, md.D}
		d, err := dateFromDict(dd, &base)
		return PlainMonthDay{d.M, d.D}, true, err
	case "toPlainDate":
		var year int64
		switch x := argOrNil(args, 0).(type) {
		case int64:
			year = x
		case *Dict:
			n, ok, err := dictInt(x, "year")
			if err != nil {
				return nil, true, err
			}
			if !ok {
				return nil, true, typeErr("toPlainDate() needs a year")
			}
			year = n
		default:
			return nil, true, typeErr("toPlainDate() takes a year")
		}
		y := int(year)
		return PlainDate{y, md.M, min(md.D, daysInMonth(y, md.M))}, true, nil
	}
	return nil, false, nil
}

// ---------------------------------------------------------------- statics

func (ev *evaluator) temporalStatic(ns, name string, args []Value) (Value, bool, error) {
	if ns == "Temporal.Now" {
		v, err := ev.temporalNow(name, args)
		return v, true, err
	}
	typ := strings.TrimPrefix(ns, "Temporal.")
	if typ == ns {
		return nil, false, nil
	}
	switch name {
	case "from":
		if err := argCount(name, args, 1, 2); err != nil {
			return nil, true, err
		}
		v, err := ev.temporalFrom(typ, args[0])
		return v, true, err
	case "compare":
		if err := argCount(name, args, 2, 3); err != nil {
			return nil, true, err
		}
		a, err := ev.coerceTemporal(typ, args[0])
		if err != nil {
			return nil, true, err
		}
		b, err := ev.coerceTemporal(typ, args[1])
		if err != nil {
			return nil, true, err
		}
		c, ok := temporalCompare(a, b)
		if !ok {
			return nil, true, typeErr("Temporal.%s values cannot be compared", typ)
		}
		return int64(c), true, nil
	}
	if typ == "Instant" {
		mult := map[string]int64{"fromEpochSeconds": nsPerSec, "fromEpochMilliseconds": 1_000_000, "fromEpochMicroseconds": 1_000, "fromEpochNanoseconds": 1}
		if m, ok := mult[name]; ok {
			n, err := intArg(name, args, 0)
			if err != nil {
				return nil, true, err
			}
			if n > math.MaxInt64/m || n < math.MinInt64/m {
				return nil, true, typeErr("%s(): out of range", name)
			}
			return Instant{NS: n * m}, true, nil
		}
	}
	return nil, false, nil
}

func (ev *evaluator) temporalNow(name string, args []Value) (Value, error) {
	now := ev.now().UnixNano()
	loc, id := time.UTC, "UTC"
	if ev.env.Location != nil {
		loc, id = ev.env.Location, ev.env.Location.String()
	}
	if len(args) > 0 && args[0] != nil {
		l, i, err := zoneArg(args[0])
		if err != nil {
			return nil, err
		}
		loc, id = l, i
	}
	z := ZonedDateTime{NS: now, Loc: loc, Zone: id}
	switch name {
	case "instant":
		return Instant{NS: now}, nil
	case "zonedDateTimeISO", "zonedDateTime":
		return z, nil
	case "plainDateISO", "plainDate":
		return z.wall().Date, nil
	case "plainTimeISO", "plainTime":
		return z.wall().Time, nil
	case "plainDateTimeISO", "plainDateTime":
		return z.wall(), nil
	case "timeZoneId":
		return id, nil
	}
	return nil, typeErr("Temporal.Now has no function %q", name)
}

// ---------------------------------------------------------------- formatting

var (
	monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	dayNames   = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
)

type fmtFields struct {
	hasYear, hasMonth, hasDay, hasTime bool
	date                               PlainDate
	tm                                 PlainTime
	zone                               string // abbreviation, "" when none
	hasZone                            bool
}

func fieldsOf(t temporal) (fmtFields, error) {
	switch x := t.(type) {
	case PlainDate:
		return fmtFields{hasYear: true, hasMonth: true, hasDay: true, date: x}, nil
	case PlainTime:
		return fmtFields{hasTime: true, tm: x}, nil
	case PlainDateTime:
		return fmtFields{hasYear: true, hasMonth: true, hasDay: true, hasTime: true, date: x.Date, tm: x.Time}, nil
	case Instant:
		w := plainFromNanos(x.NS)
		return fmtFields{hasYear: true, hasMonth: true, hasDay: true, hasTime: true, date: w.Date, tm: w.Time, zone: "UTC", hasZone: true}, nil
	case ZonedDateTime:
		w := x.wall()
		return fmtFields{hasYear: true, hasMonth: true, hasDay: true, hasTime: true, date: w.Date, tm: w.Time, zone: zoneAbbrev(x), hasZone: true}, nil
	case PlainYearMonth:
		return fmtFields{hasYear: true, hasMonth: true, date: PlainDate{x.Y, x.M, 1}}, nil
	case PlainMonthDay:
		return fmtFields{hasMonth: true, hasDay: true, date: PlainDate{1972, x.M, x.D}}, nil
	}
	return fmtFields{}, typeErr("Temporal.%s cannot be formatted", t.temporalType())
}

func zoneAbbrev(z ZonedDateTime) string {
	if z.Zone == "UTC" {
		return "UTC"
	}
	abbr, off := time.Unix(0, z.NS).In(z.Loc).Zone()
	if abbr == "" || abbr[0] == '+' || abbr[0] == '-' {
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		s := "GMT" + sign + strconv.Itoa(off/3600)
		if m := off % 3600 / 60; m != 0 {
			s += ":" + pad(m, 2)
		}
		return s
	}
	return abbr
}

// formatTemporal implements .format(pattern) (R-SESSEL-310).
func formatTemporal(t temporal, pattern string) (string, error) {
	f, err := fieldsOf(t)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	rs := []rune(pattern)
	lacks := func(tok string) error {
		return typeErr("format token %q does not apply to Temporal.%s", tok, t.temporalType())
	}
	for i := 0; i < len(rs); {
		c := rs[i]
		if c == '\'' {
			j := i + 1
			if j < len(rs) && rs[j] == '\'' {
				b.WriteRune('\'')
				i += 2
				continue
			}
			for j < len(rs) {
				if rs[j] == '\'' {
					if j+1 < len(rs) && rs[j+1] == '\'' {
						b.WriteRune('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteRune(rs[j])
				j++
			}
			i = j + 1
			continue
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			b.WriteRune(c)
			i++
			continue
		}
		j := i
		for j < len(rs) && rs[j] == c {
			j++
		}
		n := j - i
		tok := string(rs[i:j])
		i = j
		h12 := f.tm.H % 12
		if h12 == 0 {
			h12 = 12
		}
		switch c {
		case 'y':
			if !f.hasYear {
				return "", lacks(tok)
			}
			if n == 2 {
				b.WriteString(pad(int(floorMod(int64(f.date.Y), 100)), 2))
			} else {
				b.WriteString(pad(f.date.Y, n))
			}
		case 'M':
			if !f.hasMonth {
				return "", lacks(tok)
			}
			switch {
			case n >= 4:
				b.WriteString(monthNames[f.date.M-1])
			case n == 3:
				b.WriteString(monthNames[f.date.M-1][:3])
			default:
				b.WriteString(pad(f.date.M, n))
			}
		case 'd':
			if !f.hasDay {
				return "", lacks(tok)
			}
			b.WriteString(pad(f.date.D, n))
		case 'E':
			if !f.hasDay || !f.hasYear {
				return "", lacks(tok)
			}
			name := dayNames[f.date.dayOfWeek()-1]
			if n <= 3 {
				name = name[:3]
			}
			b.WriteString(name)
		case 'H', 'h', 'm', 's', 'S', 'a':
			if !f.hasTime {
				return "", lacks(tok)
			}
			switch c {
			case 'H':
				b.WriteString(pad(f.tm.H, n))
			case 'h':
				b.WriteString(pad(h12, n))
			case 'm':
				b.WriteString(pad(f.tm.Mi, n))
			case 's':
				b.WriteString(pad(f.tm.S, n))
			case 'S':
				s := pad(f.tm.Ns, 9)
				for len(s) < n {
					s += "0"
				}
				b.WriteString(s[:n])
			case 'a':
				if f.tm.H < 12 {
					b.WriteString("AM")
				} else {
					b.WriteString("PM")
				}
			}
		case 'z':
			if !f.hasZone {
				return "", lacks(tok)
			}
			b.WriteString(f.zone)
		default:
			return "", typeErr("unsupported format token %q", tok)
		}
	}
	return b.String(), nil
}

type localeFmt struct {
	date, ym, md, time, sep string
}

var locales = map[string]localeFmt{
	"en-US": {"M/d/yyyy", "M/yyyy", "M/d", "h:mm:ss a", ", "},
	"en-GB": {"dd/MM/yyyy", "MM/yyyy", "dd/MM", "HH:mm:ss", ", "},
	"en-CA": {"yyyy-MM-dd", "yyyy-MM", "MM-dd", "h:mm:ss a", ", "},
	"en-AU": {"dd/MM/yyyy", "MM/yyyy", "dd/MM", "h:mm:ss a", ", "},
	"de-DE": {"d.M.yyyy", "M.yyyy", "d.M.", "HH:mm:ss", ", "},
	"fr-FR": {"dd/MM/yyyy", "MM/yyyy", "dd/MM", "HH:mm:ss", " "},
	"es-ES": {"d/M/yyyy", "M/yyyy", "d/M", "H:mm:ss", ", "},
	"it-IT": {"d/M/yyyy", "M/yyyy", "d/M", "HH:mm:ss", ", "},
	"nl-NL": {"d-M-yyyy", "M-yyyy", "d-M", "HH:mm:ss", ", "},
	"pt-BR": {"dd/MM/yyyy", "MM/yyyy", "dd/MM", "HH:mm:ss", ", "},
	"sv-SE": {"yyyy-MM-dd", "yyyy-MM", "d/M", "HH:mm:ss", " "},
	"ja-JP": {"yyyy/M/d", "yyyy/M", "M/d", "H:mm:ss", " "},
	"zh-CN": {"yyyy/M/d", "yyyy年M月", "M/d", "HH:mm:ss", " "},
}

var localeLang = map[string]string{"en": "en-US", "de": "de-DE", "fr": "fr-FR", "es": "es-ES", "it": "it-IT",
	"nl": "nl-NL", "pt": "pt-BR", "sv": "sv-SE", "ja": "ja-JP", "zh": "zh-CN"}

// localeString implements .toLocaleString(locale) for common locales
// (R-SESSEL-311); unknown locales use en-US.
func localeString(t temporal, locale string) (string, error) {
	lf, ok := locales[locale]
	if !ok {
		lang, _, _ := strings.Cut(locale, "-")
		lf, ok = locales[localeLang[strings.ToLower(lang)]]
		if !ok {
			lf = locales["en-US"]
		}
	}
	format := func(p string) string {
		s, _ := formatTemporal(t, p)
		return s
	}
	switch t.(type) {
	case PlainDate:
		return format(lf.date), nil
	case PlainTime:
		return format(lf.time), nil
	case PlainDateTime, Instant:
		return format(lf.date) + lf.sep + format(lf.time), nil
	case ZonedDateTime:
		return format(lf.date) + lf.sep + format(lf.time) + " " + format("z"), nil
	case PlainYearMonth:
		return format(lf.ym), nil
	case PlainMonthDay:
		return format(lf.md), nil
	case Duration:
		return t.String(), nil
	}
	return t.String(), nil
}
