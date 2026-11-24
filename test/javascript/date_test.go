package javascript_test

import (
	"math"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Date đối chiếu với node, giờ địa phương là testZone ở cả hai bên. Một mốc tả bằng date:<ms>.
func TestDateIsJavaScripts(t *testing.T) {
	compare(t, []string{
		// Date.UTC: các phần, tràn sang, năm 0 tới 99, mép khoảng
		`Date.UTC(2024, 0, 31)`, `Date.UTC(2024)`, `Date.UTC()`, `Date.UTC(2024, 12, 1)`, `Date.UTC(2024, -1, 1)`, `Date.UTC(2024, 1, 30)`,
		`Date.UTC(99, 0)`, `Date.UTC(100, 0)`, `Date.UTC(-1, 0)`, `Date.UTC(-0)`, `Date.UTC(2024, 0, 1, 25, 61, 61, 1001)`,
		`Date.UTC(2024, 0, 1.9, 1.5)`, `Date.UTC(275760, 8, 13)`, `Date.UTC(275760, 8, 13, 0, 0, 0, 1)`, `Date.UTC(-271821, 3, 20)`,
		`Date.UTC(-271821, 3, 19, 23, 59, 59, 999)`, `Date.UTC(NaN)`, `Date.UTC(2024, Infinity)`, `Date.UTC("2024", [1], {valueOf: seven})`,
		`Date.UTC(2024, 0, 1, 0, 0, 0, 0, {valueOf: boom})`, `Date.UTC({valueOf: boom})`, `Date.UTC(1000000, 0)`, `Date.UTC(1000001, 0)`,
		`Date.UTC(2024, 10000000)`, `Date.UTC(2024, 10000001)`, `Date.UTC(1970, 0, 1, 0, 0, 0, -0)`, `Date.UTC(2024, 0, 1, -1)`,
		`Date.UTC(null, null)`, `Date.UTC(1600, 1, 29)`, `Date.UTC(1700, 1, 29)`, `Date.UTC(2000, 1, 29)`,
		// Date.parse: dạng ISO (ngày một mình là UTC, có giờ không múi là giờ địa phương), dạng cũ của V8
		`Date.parse("2024-01-31")`, `Date.parse("2024-01-31T10:20")`, `Date.parse("2024-01-31T10:20:30.123Z")`,
		`Date.parse("2024-01-31T10:20:30.123456789Z")`, `Date.parse("2024-01-31T10:20+07:00")`, `Date.parse("2024-01-31T10:20+0700")`,
		`Date.parse("2024-01-31T24:00")`, `Date.parse("2024-01-31T24:00:01")`, `Date.parse("2024-02-30")`, `Date.parse("+002024-01-01")`,
		`Date.parse("-000000-01-01")`, `Date.parse("-000001-01-01T00:00Z")`, `Date.parse("2024")`, `Date.parse("2024-13")`,
		`Date.parse("Jan 2, 2024 10:30 PM")`, `Date.parse("Tue Jan 02 2024 10:04:05 GMT+0700 (Indochina Time)")`, `Date.parse("2024/01/02")`,
		`Date.parse("1/2/2024")`, `Date.parse("2 January 2024")`, `Date.parse("2024-01-02 10:00")`, `Date.parse("2024-01-02 10:00 UTC")`,
		`Date.parse("2024-01-02T10:00:00 EST")`, `Date.parse("hello")`, `Date.parse("")`, `Date.parse("2024-03-10T02:30")`,
		`Date.parse("2024-11-03T01:30")`, `Date.parse("Mar 10 2024 02:30")`, `Date.parse("12:00")`, `Date.parse("Thu, 01 Jan 1970 00:00:00 GMT")`,
		`Date.parse("1970-01-01T00:00:00.000-00:00")`, `Date.parse("2024-01-01T10:00:00z")`, `Date.parse("2024-01-01t10:00:00Z")`,
		`Date.parse("20240101")`, `Date.parse("2024-1-1")`, `Date.parse("Phường 2024")`, `Date.parse("2024-01-01 (comment) 10:00")`,
		`Date.parse(5)`, `Date.parse()`, `Date.parse(null)`, `Date.parse("\uD83D 2024")`, `Date.parse("Dec 25, 1995 12:00 pm")`,
		`Date.parse("12/25/95")`, `Date.parse("25 Dec 1995 13:30 +0430")`, `Date.parse("1995-12-25T13:30:00.5")`, `Date.parse({toString: boom})`,
		`Date.parse("2024-06-15T12:00:00+14:00")`, `Date.parse("Sat, 15 Jun 2024 12:00:00 PDT")`, `Date.parse("1700-01-01T00:00")`,
		// new Date: số, chữ, các phần theo giờ địa phương; giờ bị bỏ qua tiến lên, giờ lặp lại là lần đầu
		`new Date(0).getTime()`, `new Date(1700000000123).toISOString()`, `new Date(-1).getTime()`, `new Date(1.9).getTime()`,
		`new Date(-1.9).getTime()`, `new Date("2024-01-31").getTime()`, `new Date("2024-01-31T10:20").getTime()`,
		`new Date(2024, 0, 31).getTime()`, `new Date(2024, 0).getTime()`, `new Date(2024, 0, 31, 10, 20, 30, 123).getTime()`,
		`new Date(2024, 2, 10, 2, 30).getTime()`, `new Date(2024, 2, 10, 2, 0).getTime()`, `new Date(2024, 2, 10, 1, 59).getTime()`,
		`new Date(2024, 2, 10, 3, 0).getTime()`, `new Date(2024, 10, 3, 1, 30).getTime()`, `new Date(2024, 10, 3, 0, 59).getTime()`,
		`new Date(2024, 10, 3, 2, 0).getTime()`, `new Date(2024, 10, 3, 1, 0).getTime()`, `new Date(1700, 0, 1).getTime()`,
		`new Date(1883, 10, 18, 12, 3).getTime()`, `new Date(99, 0).getTime()`, `new Date(2024, 12, 1).getTime()`, `new Date(2024, 0, 0).getTime()`,
		`new Date(2024, 6, 1, 12).toISOString()`, `new Date(true).getTime()`, `new Date(null).getTime()`, `new Date([2024, 1]).getTime()`,
		`new Date({valueOf: seven}).getTime()`, `new Date({valueOf: boom}).getTime()`, `new Date(date(5)).getTime()`,
		`new Date("2024-01-31").valueOf()`, `new Date(2024, 0, 31).toISOString()`, `new Date(2024, 0, 31, 23, 59, 59, 999).toISOString()`,
		`new Date("Jan 2, 2024 10:30 PM").getTime()`, `new Date(2024, 0, 31, 10, 20, 30, 123, {valueOf: boom}).getTime()`,
		`new Date(2024, {valueOf: boom}).getTime()`,
	})
}

// Chỗ Kitwork khác: không có Invalid Date (lỗi thay cho NaN lan đi), thời điểm ngoài 1678–2262, format, Date theo
// múi của site, Date() là bây giờ.
func TestDateIsKitworks(t *testing.T) {
	global := func(name string) value.Value {
		g, _ := javascript.Global(name)
		return g
	}
	date := global("Date")
	make := func(arguments ...value.Value) string {
		v := date.Function().(*javascript.Native).Call(run{}, arguments...)
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	n := value.Number
	moment := value.Time(time.Date(2026, 11, 23, 8, 30, 5, 123456789, time.UTC))
	cases := []struct{ name, got, want string }{
		{"a text that is no date", make(value.String("hello")), `error: invalid date: "hello"`},
		{"an empty text", make(value.String("")), "error: invalid date"},
		{"an empty array is an empty text", make(value.Array()), "error: invalid date"},
		{"NaN", make(n(math.NaN())), "error: invalid date"},
		{"undefined", make(value.Undefined()), "error: invalid date"},
		{"past JavaScript's range", make(n(8.64e15 + 1)), "error: invalid date"},
		{"a part NaN", make(n(2024), n(math.NaN())), "error: invalid date"},
		{"an object with only toString", make(value.Object().Set("toString", value.Function(callbacks["text"]))), `error: invalid date: "t"`},
		{"past 2262", make(n(1e15)), "error: time 33658-09-27T01:46:40Z is outside the years 1678 to 2262"},
		{"before 1678", make(n(2024-2400), n(0)), "error: time -0376-01-01T04:56:02Z is outside the years 1678 to 2262"},
		{"a time is itself", make(moment), "date:" + "1795422605123"},
		{"a duration is milliseconds", make(value.Duration(90 * time.Minute)), "date:5400000"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, c.got, c.want)
		}
	}

	// Date() là bây giờ, giữ nano giây; Date.now() là mili giây
	before := time.Now()
	now := date.Function().(*javascript.Native).Call(nil)
	ms := javascript.Invoke(nil, date, "now", nil).Number()
	after := time.Now()
	if now.Time().Before(before) || now.Time().After(after) || ms < float64(before.UnixMilli()) || ms > float64(after.UnixMilli()) {
		t.Errorf("Date(), Date.now(): %v %v, between %v and %v", now.Time(), ms, before, after)
	}

	if d := date.Function().(*javascript.Native); d.Name() != "Date" || d.Length() != 7 {
		t.Errorf("Date: %q %d", d.Name(), d.Length())
	}
}

// zonedRun là Run của một nơi chạy có múi giờ, như runtime cài cho một request của site (javascript.Located).
type zonedRun struct {
	run
	location *time.Location
}

func (z zonedRun) Location() *time.Location { return z.location }

// t.format viết theo múi của nơi chạy: múi Run cho, không có thì time.Local (testZone ở đây); Date đọc các phần và chữ
// có giờ không có múi theo cùng múi ấy, nên chữ format viết, Date đọc lại được chính mốc ấy.
func TestFormatAndDateReadTheZoneOfTheSite(t *testing.T) {
	saigon, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	inSaigon, inUTC := zonedRun{location: saigon}, zonedRun{location: time.UTC}
	winter := value.Time(time.Date(2026, 11, 23, 8, 30, 5, 123456789, time.UTC))
	summer := value.Time(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
	layout := value.String("2006-01-02 15:04:05.000 MST")
	text := func(v value.Value) string {
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return v.String()
	}
	format := func(r javascript.Run, moment, layout value.Value) string {
		return text(javascript.Invoke(r, moment, "format", []value.Value{layout}))
	}
	for _, c := range []struct{ name, got, want string }{
		{"a site in Asia/Ho_Chi_Minh", format(inSaigon, winter, layout), "2026-11-23 15:30:05.123 +07"},
		{"a site in UTC", format(inUTC, winter, layout), "2026-11-23 08:30:05.123 UTC"},
		{"no run: the host's zone", format(nil, winter, layout), "2026-11-23 03:30:05.123 EST"},
		{"a run without a zone", format(run{}, winter, layout), "2026-11-23 03:30:05.123 EST"},
		{"a zone of nil", format(zonedRun{}, winter, layout), "2026-11-23 03:30:05.123 EST"},
		{"summer time where the host is", format(nil, summer, layout), "2026-07-01 08:00:00.000 EDT"},
		{"no summer time in Asia/Ho_Chi_Minh", format(inSaigon, summer, layout), "2026-07-01 19:00:00.000 +07"},
		{"words of the layout", format(inSaigon, winter, value.String("Monday Jan _2 3:04PM -07:00")), "Monday Nov 23 3:30PM +07:00"},
		{"toISOString stays UTC", text(javascript.Invoke(inSaigon, winter, "toISOString", nil)), "2026-11-23T08:30:05.123Z"},
		{"format without a layout", format(inSaigon, winter, value.Undefined()), `error: format takes a layout of Go, as "2006-01-02 15:04", got undefined`},
		{"format of a number", format(inSaigon, winter, value.Number(5)), `error: format takes a layout of Go, as "2006-01-02 15:04", got number`},
	} {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %s\n  want %s", c.name, c.got, c.want)
		}
	}

	// Date của cùng nơi chạy: các phần và chữ có giờ không có múi theo múi ấy; chữ chỉ có ngày luôn là UTC
	date, _ := javascript.Global("Date")
	call := func(r javascript.Run, arguments ...value.Value) string {
		return text(javascript.Invoke(r, date.Function().(*javascript.Native).Call(r, arguments...), "toISOString", nil))
	}
	n := value.Number
	for _, c := range []struct {
		name string
		r    javascript.Run
		want string
	}{
		{"UTC", inUTC, "2024-01-31T10:20:00.000Z"},
		{"Asia/Ho_Chi_Minh", inSaigon, "2024-01-31T03:20:00.000Z"},
		{"the host's zone", nil, "2024-01-31T15:20:00.000Z"},
	} {
		parts, local := call(c.r, n(2024), n(0), n(31), n(10), n(20)), call(c.r, value.String("2024-01-31T10:20"))
		parsed := javascript.Invoke(c.r, date, "parse", []value.Value{value.String("2024-01-31T10:20")})
		dateOnly := call(c.r, value.String("2024-01-31"))
		if parts != c.want || local != c.want || text(javascript.Invoke(nil, javascript.Invoke(nil, date, "call",
			[]value.Value{value.Null(), parsed}), "toISOString", nil)) != c.want || dateOnly != "2024-01-31T00:00:00.000Z" {
			t.Errorf("%s: parts %s, text %s, parse %s, a date alone %s", c.name, parts, local, describe(parsed), dateOnly)
		}
	}

	// format rồi Date ở cùng nơi chạy là chính mốc ấy (tới mili giây)
	written := format(inSaigon, winter, value.String("2006-01-02T15:04:05.000"))
	back := date.Function().(*javascript.Native).Call(inSaigon, value.String(written))
	if back.Time().UnixMilli() != winter.Time().UnixMilli() {
		t.Errorf("round trip through %q: %v", written, back.Time())
	}
}
