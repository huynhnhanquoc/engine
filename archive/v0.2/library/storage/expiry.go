package storage

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// clock is the storage's time: when a value was written, when it expires.
var clock = time.Now

// expired says a value expiring at expireAt (zero for never) is gone at now.
func expired(expireAt, now time.Time) bool { return !expireAt.IsZero() && !now.Before(expireAt) }

// maxExpireMilliseconds is the longest span expire() takes as a number: the longest time.Duration, about 292 years.
const maxExpireMilliseconds = float64(math.MaxInt64 / int64(time.Millisecond))

// expiry is expire(…) read (mục 6), as a route's cache writes it: given the time of a write, how long until the
// value expires.
type expiry func(now time.Time) time.Duration

// readExpiry reads the argument of expire(…): a number of milliseconds, a span ("30s", "5m", "1h30m", "7d", "2w",
// "1mo" of 30 days, "1y" of 365), or a time of the calendar in the app's zone ("nextday 03:00", "daily 03:00",
// "weekly", "weekly fri 18:00", "monthly", "hourly", a bare "03:00"), the zone read as the value is written. nil when
// it reads none.
func readExpiry(argument value.Value, zone func() *time.Location) expiry {
	if argument.IsNumber() {
		n := argument.Number()
		if n < 1 || math.IsNaN(n) || n > maxExpireMilliseconds {
			return nil
		}
		span := time.Duration(n * float64(time.Millisecond))
		return func(time.Time) time.Duration { return span }
	}
	if !argument.IsString() {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(argument.Text()))
	if span, ok := readSpan(text); ok {
		return func(time.Time) time.Duration { return span }
	}
	if calendar := readCalendar(text); calendar != nil {
		return func(now time.Time) time.Duration { return calendar(now.In(zone())) }
	}
	return nil
}

// readSpan reads Go's spans ("30s", "1h30m", "250ms") and a whole number of days, weeks, months of 30 days or years of
// 365 ("7d", "2w", "1mo", "1y"), as the first engine's ParseDuration did.
func readSpan(text string) (time.Duration, bool) {
	if span, err := time.ParseDuration(text); err == nil {
		return span, span > 0
	}
	digits := 0
	for digits < len(text) && text[digits] >= '0' && text[digits] <= '9' {
		digits++
	}
	n, err := strconv.Atoi(text[:digits])
	if err != nil || n <= 0 {
		return 0, false
	}
	day := 24 * time.Hour
	units := map[string]time.Duration{"d": day, "w": 7 * day, "mo": 30 * day, "y": 365 * day}
	unit, ok := units[text[digits:]]
	if !ok || n > int(math.MaxInt64/int64(unit)) {
		return 0, false
	}
	return time.Duration(n) * unit, true
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// readHourMinute reads "HH:MM" or "HH".
func readHourMinute(field string) (hour, minute int, ok bool) {
	parts := strings.SplitN(field, ":", 2)
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, false
	}
	if len(parts) == 2 {
		if minute, err = strconv.Atoi(parts[1]); err != nil || minute < 0 || minute > 59 {
			return 0, 0, false
		}
	}
	return hour, minute, true
}

// readCalendar reads a time of the calendar, as the first engine's calendarExpiry did: the span from now (in the
// zone) to it; nil for another text.
func readCalendar(text string) func(now time.Time) time.Duration {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	hour, minute := 0, 0
	weekday, haveWeekday := time.Monday, false
	for _, field := range fields[1:] {
		if h, m, ok := readHourMinute(field); ok {
			hour, minute = h, m
		} else if day, ok := weekdays[field]; ok {
			weekday, haveWeekday = day, true
		} else {
			return nil
		}
	}
	today := func(now time.Time) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	}
	next := func(now time.Time) time.Duration {
		at := today(now)
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at.Sub(now)
	}
	switch fields[0] {
	case "hourly":
		return func(now time.Time) time.Duration {
			at := now.Truncate(time.Minute)
			at = at.Add(-time.Duration(at.Minute()) * time.Minute).Add(time.Hour)
			return at.Sub(now)
		}
	case "daily", "midnight", "today":
		return next
	case "nextday", "tomorrow":
		return func(now time.Time) time.Duration { return today(now).AddDate(0, 0, 1).Sub(now) }
	case "weekly":
		return func(now time.Time) time.Duration {
			target := time.Monday
			if haveWeekday {
				target = weekday
			}
			at := today(now).AddDate(0, 0, (int(target)-int(now.Weekday())+7)%7)
			if !at.After(now) {
				at = at.AddDate(0, 0, 7)
			}
			return at.Sub(now)
		}
	case "monthly":
		return func(now time.Time) time.Duration {
			return time.Date(now.Year(), now.Month()+1, 1, hour, minute, 0, 0, now.Location()).Sub(now)
		}
	}
	if len(fields) == 1 {
		if h, m, ok := readHourMinute(fields[0]); ok {
			hour, minute = h, m
			return next
		}
	}
	return nil
}
