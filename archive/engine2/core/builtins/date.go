package builtins

import (
	"math"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// Date is Kitwork's, not JavaScript's: there is no Date object. `new Date(…)` compiles as Date(…)
// (the parser drops `new`), and Date gives a Time value, which compares by value, adds a Duration
// and answers getTime(), toISOString() and format(). Date() is now; Date(ms) that instant; Date(text)
// the instant the text writes, read as Date.parse reads it; Date(y, m, d, h, min, s, ms) those parts in
// the host's zone, the month counted from 0 as JavaScript counts it. A text or a number that is no
// instant is a RANGE failure, where JavaScript makes an "Invalid Date" object, and so is an instant a
// Time cannot hold (before 1678 or after 2262).
//
// Every computation from parts is ECMAScript's MakeDay, MakeTime, MakeDate and TimeClip in float64, as
// V8 does them, so a part past its range carries over (month 12 is January next year) and a result past
// ±8.64e15 ms is no instant, without a Go integer overflowing on the way.
func Date() value.Value {
	return DateIn(nil)
}

// DateIn is Date reading the parts of Date(y, m, …) and a text without an offset in location: a site's
// Date, in its app's module.timezone (UTC when none), built once when the site boots and laid over the
// shared set (04/11/2026). nil is the host's own zone, time.Local read at each call, which is Date().
// It is frozen, as every builtin is.
func DateIn(location *time.Location) value.Value {
	f := native("Date", 7, func(arguments ...value.Value) value.Value {
		switch {
		case len(arguments) == 0:
			return value.Time(time.Now())
		case len(arguments) == 1 && arguments[0].IsTime():
			return arguments[0]
		case len(arguments) == 1:
			a := arguments[0]
			// an array or an object is made a primitive first, as JavaScript's Date does (the default
			// hint: an object's own valueOf before its toString, an array its text), a failure of that
			// the call's (05/11/2026; it was made text, so new Date({ valueOf: () => 0 }) was invalid)
			if a.IsArray() || a.IsObject() {
				if a = a.ToPrimitive(); a.IsInvalid() {
					return a
				}
			}
			if a.IsString() || a.IsFunction() {
				text := a.ToString().Text()
				return instant(parseDate(text, location), "invalid date: "+text)
			}
			return instant(timeClip(a.ToNumber().Number()), "invalid date")
		}
		converted, failure := value.ConvertArguments(dateHints, arguments)
		if failure.IsInvalid() {
			return failure
		}
		return instant(localTime(dateParts(converted), location), "invalid date")
	})
	f.Set("now", native("now", 0, func(...value.Value) value.Value {
		return value.Number(float64(time.Now().UnixMilli()))
	}))
	f.Set("parse", native("parse", 1, converting("t", func(arguments ...value.Value) value.Value {
		return value.Number(parseDate(argument(arguments, 0).ToString().Text(), location))
	})))
	f.Set("UTC", native("UTC", 7, converting(dateHints, func(arguments ...value.Value) value.Value {
		return value.Number(timeClip(dateParts(arguments)))
	})))
	freezeAll(f)
	return f
}

// dateHints converts the seven parts new Date(…) and Date.UTC(…) read, each a number, and keeps the
// arguments past them, which JavaScript never reads (05/11/2026: "n*" converted an eighth, and its failure
// was the call's).
const dateHints = "nnnnnnn"

// instant is the Time of ms milliseconds since 1970, a RANGE failure with text when ms is NaN.
func instant(ms float64, text string) value.Value {
	if ms != ms {
		return value.Fail(value.RangeCode, text)
	}
	return value.Time(time.UnixMilli(int64(ms)))
}

// ---- ECMAScript's time arithmetic (21.4.1), as V8 computes it ----

const (
	millisecondsPerDay = 86_400_000
	// maxMilliseconds is JavaScript's time range, ±8.64e15 ms (100 million days) around 1970.
	maxMilliseconds = 8.64e15
	// maxLocalMilliseconds is how far a local time may lie before it is made UTC: ten days more, so
	// an instant at the edge of the range reads in every zone (V8's kMaxTimeBeforeUTCInMs).
	maxLocalMilliseconds = maxMilliseconds + 10*millisecondsPerDay
)

// toInteger is ToIntegerOrInfinity without its -0 rule: NaN is 0, the fraction dropped toward zero.
func toInteger(x float64) float64 {
	if x != x {
		return 0
	}
	return math.Trunc(x)
}

// dateParts is the time value Date.UTC(year, month, day, hours, minutes, seconds, ms) writes, before
// TimeClip: a missing year is NaN, a missing day 1, the rest 0, and a year from 0 to 99 is 1900 + it.
func dateParts(arguments []value.Value) float64 {
	parts := [7]float64{math.NaN(), 0, 1, 0, 0, 0, 0}
	for i := 0; i < len(arguments) && i < len(parts); i++ {
		parts[i] = toNumber(arguments[i])
	}
	year := parts[0]
	if y := toInteger(year); year == year && y >= 0 && y <= 99 {
		year = 1900 + y
	}
	return makeDate(makeDay(year, parts[1], parts[2]), makeTime(parts[3], parts[4], parts[5], parts[6]))
}

// makeDay is the day number of year, month (from 0, carried into the year) and date: V8's MakeDay,
// with its own bounds on the year and the month, past which the day is NaN.
func makeDay(year, month, date float64) float64 {
	if !(year >= -1_000_000 && year <= 1_000_000 && month >= -10_000_000 && month <= 10_000_000) || math.IsInf(date, 0) || date != date {
		return math.NaN()
	}
	y, m := int64(year), int64(month)
	y += m / 12
	m %= 12
	if m < 0 {
		m += 12
		y--
	}
	// yearDelta keeps every year of the range positive, so the integer divisions below round down
	const yearDelta = 399_999
	days := func(y int64) int64 {
		y += yearDelta
		return 365*y + y/4 - y/100 + y/400
	}
	day := days(y) - days(1970)
	if y%4 != 0 || y%100 == 0 && y%400 != 0 {
		day += [12]int64{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}[m]
	} else {
		day += [12]int64{0, 31, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335}[m]
	}
	return float64(day-1) + toInteger(date)
}

// makeTime is the milliseconds of hours, minutes, seconds and ms, each made whole first; NaN when one
// is not finite.
func makeTime(hours, minutes, seconds, milliseconds float64) float64 {
	for _, part := range []float64{hours, minutes, seconds, milliseconds} {
		if part != part || math.IsInf(part, 0) {
			return math.NaN()
		}
	}
	// each product rounded on its own (float64(…)), as V8 computes it: a fused multiply-add would round once
	return float64(toInteger(hours)*3_600_000) + float64(toInteger(minutes)*60_000) + float64(toInteger(seconds)*1000) + toInteger(milliseconds)
}

func makeDate(day, milliseconds float64) float64 {
	if day != day || milliseconds != milliseconds || math.IsInf(day, 0) || math.IsInf(milliseconds, 0) {
		return math.NaN()
	}
	return float64(day*millisecondsPerDay) + milliseconds
}

// timeClip is the time value an instant may have: NaN past ±8.64e15, else the whole milliseconds, 0
// for -0.
func timeClip(ms float64) float64 {
	if !(math.Abs(ms) <= maxMilliseconds) {
		return math.NaN()
	}
	return toInteger(ms) + 0
}

// localTime is a time value written in location made UTC and clipped, as Date(y, m, …) does.
func localTime(local float64, location *time.Location) float64 {
	if !(math.Abs(local) <= maxLocalMilliseconds) {
		return math.NaN()
	}
	return timeClip(fromLocal(local, location))
}

// fromLocal is the UTC milliseconds of a wall-clock time of location written as milliseconds: the
// zone's offset at that wall time taken away, as Go resolves it (a time a daylight-saving change skips
// or repeats is read as Go reads it). A nil location is the host's zone, read now.
func fromLocal(local float64, location *time.Location) float64 {
	if location == nil {
		location = time.Local
	}
	wall := time.UnixMilli(int64(local)).UTC()
	in := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), location)
	return float64(in.UnixMilli())
}
