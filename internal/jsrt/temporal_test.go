package jsrt

import (
	"strings"
	"testing"
)

// The Temporal global over the Sessel date engine (R-JS-43; docs snapshot
// 2026-11-23). Each row is an expression and its text, or the name of the
// error it throws.
func TestTemporal(t *testing.T) {
	rows := []struct{ expr, want string }{
		// Arithmetic.
		{`Temporal.PlainDate.from("2024-01-31").add("P1M").toString()`, "2024-02-29"},
		{`Temporal.PlainDate.from("2026-03-01").subtract({ days: 1 }).toString()`, "2026-02-28"},
		{`Temporal.PlainDate.from("2026-01-01").until("2026-03-01").toString()`, "P59D"},
		{`Temporal.PlainDate.from("2026-01-01").since("2026-03-01").toString()`, "-P59D"},
		{`Temporal.PlainDate.from("9999-12-31").add({ days: 1 })`, "RangeError"},
		{`Temporal.PlainTime.from("23:30").add({ hours: 1 }).toString()`, "00:30:00"},
		{`Temporal.PlainTime.from("10:00").until("09:15:00.5").toString()`, "-PT44M59.5S"},
		{`Temporal.PlainDateTime.from("2026-01-31T23:00").add({ months: 1, hours: 2 }).toString()`, "2026-03-01T01:00:00"},
		{`Temporal.PlainDateTime.from("2026-01-01T12:00").until("2026-01-03T06:00").toString()`, "P1DT18H"},
		{`Temporal.PlainDateTime.from("0001-01-01T00:00").until("9999-12-31T00:00").days`, "3652058"},
		{`Temporal.PlainDateTime.compare("9998-01-01T00:00", "9999-01-01T00:00")`, "-1"},
		{`Temporal.ZonedDateTime.from("2026-03-28T12:00[Europe/London]").add({ days: 1 }).toString()`, "2026-03-29T12:00:00+01:00[Europe/London]"},
		{`Temporal.ZonedDateTime.from("2026-03-28T12:00[Europe/London]").add({ hours: 24 }).toString()`, "2026-03-29T13:00:00+01:00[Europe/London]"},
		{`Temporal.ZonedDateTime.from("2026-03-28T12:00[Europe/London]").withTimeZone("America/New_York").toString()`, "2026-03-28T08:00:00-04:00[America/New_York]"},
		{`Temporal.Instant.from("2026-01-01T00:00Z").add({ hours: 25, minutes: 1 }).toString()`, "2026-01-02T01:01:00Z"},
		{`Temporal.Instant.from("2026-01-01T00:00Z").toZonedDateTimeISO("Asia/Tokyo").toString()`, "2026-01-01T09:00:00+09:00[Asia/Tokyo]"},
		{`Temporal.Instant.from("2026-01-01T00:00:00.123456789Z").epochNanoseconds`, "1767225600123456789"},
		{`new Temporal.ZonedDateTime(0n, "UTC").toString()`, "1970-01-01T00:00:00+00:00[UTC]"},
		{`Temporal.PlainYearMonth.from("2026-11").add({ months: 3 }).toString()`, "2027-02"},
		{`Temporal.PlainYearMonth.from("2026-11").until("2028-01").toString()`, "P1Y2M"},
		{`Temporal.PlainYearMonth.from("2026-11").add({ days: 1 })`, "RangeError"},
		{`Temporal.PlainYearMonth.from("2026-02").toPlainDate({ day: 28 }).toString()`, "2026-02-28"},
		{`Temporal.PlainYearMonth.from("2026-02").toPlainDate({ day: 29 })`, "RangeError"},
		{`Temporal.PlainYearMonth.from("2026-02").daysInMonth`, "28"},
		{`Temporal.PlainMonthDay.from({ month: 2, day: 29 }).toString()`, "02-29"},
		{`Temporal.PlainMonthDay.from({ month: 2, day: 29, year: 2027 })`, "RangeError"},
		{`Temporal.PlainMonthDay.from("--12-25").with({ day: 26 }).monthCode`, "M12"},
		{`Temporal.PlainMonthDay.from("02-29").toPlainDate({ year: 2028 }).toString()`, "2028-02-29"},
		// Durations.
		{`Temporal.Duration.from({ hours: 1 }).add({ minutes: 90 }).toString()`, "PT2H30M"},
		{`Temporal.Duration.from({ days: 1 }).add({ hours: 30 }).toString()`, "P2DT6H"},
		{`Temporal.Duration.from({ hours: 1 }).subtract({ hours: 2 }).toString()`, "-PT1H"},
		{`Temporal.Duration.from({ months: 1 }).add({ days: 1 })`, "RangeError"},
		{`Temporal.Duration.from({ hours: 1, minutes: -1 })`, "RangeError"},
		{`Temporal.Duration.from("P1DT12H").total("days")`, "1.5"},
		{`Temporal.Duration.from("P1M").total("days")`, "RangeError"},
		{`Temporal.Duration.from("-PT90M").abs().toString() + Temporal.Duration.from("PT90M").negated().sign`, "PT1H30M-1"},
		{`new Temporal.Duration(0, 0, 0, 1, 25).toString()`, "P1DT25H"},
		{`new Temporal.Duration().blank`, "true"},
		{`Temporal.Duration.compare({ hours: 25 }, { days: 1 })`, "1"},
		{`Temporal.Duration.from({})`, "TypeError"},
		// Conversions.
		{`Temporal.PlainDate.from("2026-09-30").toPlainDateTime("14:30").toString()`, "2026-09-30T14:30:00"},
		{`Temporal.PlainDate.from("2026-09-30").toZonedDateTime({ timeZone: "Europe/London", plainTime: "09:00" }).toString()`, "2026-09-30T09:00:00+01:00[Europe/London]"},
		{`Temporal.PlainDate.from("2026-09-30").toPlainYearMonth().toString() + Temporal.PlainDate.from("2026-09-30").toPlainMonthDay().toString()`, "2026-0909-30"},
		{`Temporal.PlainDate.from(Temporal.PlainDateTime.from("2026-09-30T14:30")).toString()`, "2026-09-30"},
		{`Temporal.PlainTime.from(Temporal.ZonedDateTime.from("2026-09-30T14:30[Europe/London]")).toString()`, "14:30:00"},
		{`Temporal.ZonedDateTime.from("2026-09-30T14:30[Europe/London]").toInstant().toString()`, "2026-09-30T13:30:00Z"},
		{`Temporal.Instant.from(Temporal.ZonedDateTime.from("2026-09-30T14:30[Europe/London]")).epochMilliseconds`, "1790775000000"},
		{`Temporal.PlainDateTime.from({ year: 2026, monthCode: "M09", day: 30, hour: 5 }).toString()`, "2026-09-30T05:00:00"},
		{`Temporal.PlainDate.from({ year: 2026, month: 9, monthCode: "M10", day: 30 })`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").with({ day: 1 }).toString()`, "2026-09-01"},
		{`Temporal.PlainDate.from("2026-09-30").with({ month: 2 })`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").with({})`, "TypeError"},
		{`Temporal.PlainDate.from("2026-09-30").with("2026-01-01")`, "TypeError"},
		// Errors.
		{`Temporal.Instant.from({ toString() { return 5; } })`, "TypeError"},
		{`Temporal.Instant.from({ toString() { return "nope"; } })`, "RangeError"},
		{`Temporal.Instant.from({ toString() { return "1970-01-01T00:00Z"; } }).epochMilliseconds`, "0"},
		{`Temporal.Instant.from(Temporal.PlainDate.from("2026-01-01"))`, "RangeError"},
		{`Temporal.Instant.fromEpochMilliseconds(1.5)`, "RangeError"},
		{`Temporal.Instant.fromEpochNanoseconds(5)`, "TypeError"},
		{`Temporal.Instant.from("9999-01-01T00:00Z")`, "RangeError"},
		{`Temporal.PlainDate.from(5)`, "TypeError"},
		{`Temporal.PlainDate.from("2026-09-30T10:00Z")`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").toZonedDateTime("Nowhere/Land")`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").toZonedDateTime(5)`, "TypeError"},
		{`new Temporal.PlainDate(2026, 9, 30, "gregory")`, "RangeError"},
		{`new Temporal.PlainDate(2026, 9, 30, 5)`, "TypeError"},
		{`new Temporal.PlainTime(24)`, "RangeError"},
		{`Temporal.PlainTime.prototype.add.call(Temporal.PlainDate.from("2026-01-01"), "PT1H")`, "TypeError"},
		{`Temporal.PlainDate.from("2026-09-30").toString({ calendarName: "always" })`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").until("2026-10-30", { largestUnit: "months" })`, "RangeError"},
		{`Temporal.PlainDate.from("2026-09-30").until("2026-10-30", 5)`, "TypeError"},
		// Locale text.
		{`Temporal.PlainDate.from("2026-03-23").toLocaleString("en-US", { dateStyle: "full" })`, "March 23, 2026"},
		{`Temporal.PlainDate.from("2026-03-23").toLocaleString("en-US", { dateStyle: "short", timeStyle: "short" })`, "3/23/26"},
		{`Temporal.PlainDateTime.from("2026-03-23T14:05").toLocaleString()`, "Mar 23, 2026, 2:05:00 PM"},
		{`Temporal.PlainDateTime.from("2026-03-23T14:05").toLocaleString("en-US", { dateStyle: "long", timeStyle: "short" })`, "March 23, 2026, 2:05:00 PM"},
		{`Temporal.PlainTime.from("14:05").toLocaleString("en-US", { timeStyle: "short", dateStyle: "long" })`, "2:05 PM"},
		{`Temporal.PlainYearMonth.from("2026-03").toLocaleString()`, "Mar 1, 2026"},
		{`Temporal.PlainMonthDay.from("03-23").toLocaleString()`, "Mar 23, 2000"},
		{`Temporal.ZonedDateTime.from("2026-03-23T14:05[Europe/London]").toLocaleString()`, "Mar 23, 2026, 2:05:00 PM"},
		{`Temporal.PlainDate.from("2026-03-23").toLocaleString("de-DE")`, "23.3.2026"},
		{`Temporal.PlainDate.from("2026-03-23").toLocaleString("en-US", { dateStyle: "huge" })`, "RangeError"},
		// The classes.
		{`class D extends Temporal.PlainDate {}; const d = new D(2026, 1, 2); [d instanceof D, d instanceof Temporal.PlainDate, d.toString(), D.from("2026-01-02") instanceof D].join()`, "true,true,2026-01-02,false"},
		{`[Temporal.PlainDate.name, String(Temporal), String(Temporal.Now), Object.keys(Temporal).length, Object.keys(Temporal.PlainDate.from("2026-01-02")).length].join()`, "PlainDate,[object Temporal],[object Temporal.Now],0,0"},
		{`Reflect.construct(Temporal.PlainDate, [2026, 1, 2], Object)`, "TypeError"},
		{`Temporal.PlainDate.from("2026-01-02").toJSON()`, "2026-01-02"},
		{`new Date(Date.UTC(2026, 0, 2)).toTemporalInstant().toZonedDateTimeISO("UTC").toPlainDate().toString()`, "2026-01-02"},
		{`new Date(NaN).toTemporalInstant()`, "RangeError"},
	}
	for _, r := range rows {
		body := "return " + r.expr
		if strings.HasPrefix(r.expr, "class ") {
			body = lastReturn(r.expr)
		}
		res := mustEval(t, ExprRequest{Expression: `(() => { try { return String((() => { ` + body + ` })()); } catch (e) { return e.name; } })()`})
		if res.Value != r.want {
			t.Errorf("%s\n\tgot  %v\n\twant %s", r.expr, res.Value, r.want)
		}
	}
}

// lastReturn makes the last statement of src the returned value.
func lastReturn(src string) string {
	for i := len(src) - 1; i >= 0; i-- {
		if src[i] == ';' {
			return src[:i+1] + " return " + src[i+1:]
		}
	}
	return "return " + src
}
