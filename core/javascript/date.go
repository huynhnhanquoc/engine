package javascript

import (
	"math"
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Date của Kitwork, không phải object Date của JavaScript (engine2/docs/language.md mục 4.3): new Date(…) là Date(…)
// (compiler bỏ new) và cho một thời điểm (value.Time), so được thứ tự, cộng được khoảng thời gian, có getTime,
// valueOf, toISOString, format (kinds.go); không có getter, setter (getFullYear, setMonth…). Date() là bây giờ;
// Date(ms) là mốc ấy; Date(chữ) là mốc chữ viết, đọc như Date.parse; Date(năm, tháng, ngày, giờ, phút, giây, ms) là các
// phần ấy theo múi của nơi chạy, tháng đếm từ 0 như JavaScript. Chữ hay số không phải một mốc là RangeError, nơi
// JavaScript cho một "Invalid Date" để NaN lan đi không báo; mốc ngoài 1678–2262 (value.Time) cũng là lỗi.
// Date.parse và Date.UTC cho số như JavaScript, NaN khi không đọc được. Giờ địa phương là múi của nơi chạy, Run cho
// (Located): một Date cho mọi site, không dựng lại cho từng site.
//
// Phép tính từ các phần là MakeDay, MakeTime, MakeDate, TimeClip của ECMAScript trên float64, như V8 làm: phần quá
// khoảng của nó thì tràn sang (tháng 12 là tháng 1 năm sau), kết quả quá ±8.64e15 ms không là mốc, không số nguyên nào
// của Go tràn giữa đường. Chép từ engine2 (core/builtins/date.go); chỗ khác: giờ địa phương mà một lần chuyển giờ mùa
// hè bỏ qua hay lặp lại đọc như V8 đọc (fromLocal), engine2 đọc như Go.

// dateFunction là Date: Date(…), Date.now, Date.parse, Date.UTC.
var dateFunction = constructor("Date", 7, makeInstant,
	member{"now", native("now", 0, func(Run, []value.Value) value.Value {
		return value.Number(float64(time.Now().UnixMilli()))
	})},
	member{"parse", native("parse", 1, func(run Run, arguments []value.Value) value.Value {
		text, err := toText(run, argument(arguments, 0))
		if err != nil {
			return value.Error(err)
		}
		return value.Number(parseDate(text, locationOf(run)))
	})},
	member{"UTC", native("UTC", 7, func(run Run, arguments []value.Value) value.Value {
		ms, err := dateParts(run, arguments)
		if err != nil {
			return value.Error(err)
		}
		return value.Number(timeClip(ms))
	})},
)

// makeInstant là Date(…). Một đối số: thời điểm thì là chính nó; object, mảng thành giá trị nguyên thuỷ trước, như
// Date của JavaScript (valueOf riêng trước toString, mảng là chữ của nó); chữ thì đọc như Date.parse; khác thì đổi
// thành số mili giây. Hai đối số trở lên là các phần, theo múi của nơi chạy.
func makeInstant(run Run, arguments []value.Value) value.Value {
	switch len(arguments) {
	case 0:
		return value.Time(time.Now())
	case 1:
		a := arguments[0]
		if a.IsTime() {
			return a
		}
		if a.IsObject() || a.IsArray() {
			primitive, err := toPrimitive(run, a, "default")
			if err != nil {
				return value.Error(err)
			}
			a = primitive
		}
		if a.IsString() {
			return instant(parseDate(a.String(), locationOf(run)), a.String())
		}
		n, err := toNumber(run, a)
		if err != nil {
			return value.Error(err)
		}
		return instant(timeClip(n), "")
	}
	local, err := dateParts(run, arguments)
	if err != nil {
		return value.Error(err)
	}
	return instant(localTime(local, locationOf(run)), "")
}

// instant là thời điểm ms mili giây từ 1970; RangeError khi ms là NaN (text là chữ không đọc được, nêu trong lời báo),
// và lỗi của value.Time khi ngoài 1678–2262.
func instant(ms float64, text string) value.Value {
	if ms != ms {
		if text != "" {
			return rangeError("invalid date: %q", text)
		}
		return rangeError("invalid date")
	}
	return value.Time(time.UnixMilli(int64(ms)))
}

// ---- phép tính thời gian của ECMAScript (21.4.1), như V8 tính ----

const (
	millisecondsPerDay = 86_400_000
	// maxMilliseconds là khoảng thời gian của JavaScript: ±8.64e15 ms (100 triệu ngày) quanh 1970.
	maxMilliseconds = 8.64e15
	// maxLocalMilliseconds là giờ địa phương xa nhất trước khi đổi sang UTC: thêm mười ngày, để một mốc ở mép khoảng đọc
	// được ở mọi múi (kMaxTimeBeforeUTCInMs của V8).
	maxLocalMilliseconds = maxMilliseconds + 10*millisecondsPerDay
)

// truncated là ToIntegerOrInfinity không có luật -0 của nó: NaN là 0, bỏ phần lẻ về phía 0.
func truncated(x float64) float64 {
	if x != x {
		return 0
	}
	return math.Trunc(x)
}

// dateParts là mốc Date.UTC(năm, tháng, ngày, giờ, phút, giây, ms) viết, trước TimeClip: mỗi phần đổi thành số theo thứ
// tự, phần sau thứ bảy JavaScript không đọc; vắng năm là NaN, vắng ngày là 1, còn lại 0; năm 0 tới 99 là 1900 + nó.
func dateParts(run Run, arguments []value.Value) (float64, error) {
	parts := [7]float64{math.NaN(), 0, 1, 0, 0, 0, 0}
	for i := 0; i < len(arguments) && i < len(parts); i++ {
		n, err := toNumber(run, arguments[i])
		if err != nil {
			return 0, err
		}
		parts[i] = n
	}
	year := parts[0]
	if y := truncated(year); year == year && y >= 0 && y <= 99 {
		year = 1900 + y
	}
	return makeDate(makeDay(year, parts[1], parts[2]), makeTime(parts[3], parts[4], parts[5], parts[6])), nil
}

// makeDay là số ngày từ 1970 của năm, tháng (từ 0, tràn sang năm) và ngày: MakeDay của V8, với giới hạn riêng của nó
// cho năm và tháng, quá đó là NaN.
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
	// yearDelta giữ mọi năm của khoảng dương, để phép chia nguyên dưới đây làm tròn xuống
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
	return float64(day-1) + truncated(date)
}

// makeTime là số mili giây của giờ, phút, giây, ms, mỗi phần bỏ phần lẻ trước; NaN khi một phần không hữu hạn.
func makeTime(hours, minutes, seconds, milliseconds float64) float64 {
	for _, part := range []float64{hours, minutes, seconds, milliseconds} {
		if part != part || math.IsInf(part, 0) {
			return math.NaN()
		}
	}
	// mỗi tích làm tròn riêng (float64(…)), như V8 tính: gộp nhân cộng sẽ làm tròn một lần
	return float64(truncated(hours)*3_600_000) + float64(truncated(minutes)*60_000) + float64(truncated(seconds)*1000) + truncated(milliseconds)
}

func makeDate(day, milliseconds float64) float64 {
	if day != day || milliseconds != milliseconds || math.IsInf(day, 0) || math.IsInf(milliseconds, 0) {
		return math.NaN()
	}
	return float64(day*millisecondsPerDay) + milliseconds
}

// timeClip là giá trị một mốc có thể có: NaN quá ±8.64e15, còn lại là mili giây nguyên, 0 cho -0.
func timeClip(ms float64) float64 {
	if !(math.Abs(ms) <= maxMilliseconds) {
		return math.NaN()
	}
	return truncated(ms) + 0
}

// localTime là một giờ địa phương của location, viết bằng mili giây, đổi sang UTC rồi TimeClip, như Date(năm, tháng, …).
func localTime(local float64, location *time.Location) float64 {
	if !(math.Abs(local) <= maxLocalMilliseconds) {
		return math.NaN()
	}
	return timeClip(fromLocal(local, location))
}

// fromLocal là mốc UTC (ms) của giờ địa phương local của location: bớt độ lệch của múi ở giờ ấy. Giờ mà một lần chuyển
// giờ mùa hè bỏ qua hay lặp lại đọc như V8 (ICU, UCAL_TZ_LOCAL_FORMER) và như ECMAScript: giờ bị bỏ qua đọc theo độ
// lệch trước lần chuyển, nên tiến lên (02:30 ngày 10/3/2024 ở New York là 03:30 EDT); giờ lặp lại là lần đầu (01:30
// ngày 3/11/2024 là EDT). time.Date của Go đọc giờ bị bỏ qua theo độ lệch sau, lùi về 01:30 EST. nil là time.Local.
func fromLocal(local float64, location *time.Location) float64 {
	if location == nil {
		location = time.Local
	}
	wall := int64(local)
	offset := func(utc int64) int64 {
		_, seconds := time.UnixMilli(utc).In(location).Zone()
		return int64(seconds) * 1000
	}
	// độ lệch một ngày trước và một ngày sau: bằng nhau là không có lần chuyển nào quanh giờ ấy
	before, after := offset(wall-millisecondsPerDay), offset(wall+millisecondsPerDay)
	if before == after {
		return float64(wall - before)
	}
	earlier, later := wall-before, wall-after
	earlierHolds, laterHolds := offset(earlier) == before, offset(later) == after
	switch {
	case earlierHolds && laterHolds:
		return float64(min(earlier, later)) // giờ lặp lại: lần đầu
	case laterHolds:
		return float64(later)
	case earlierHolds:
		return float64(earlier)
	}
	return float64(wall - before) // giờ bị bỏ qua: độ lệch trước lần chuyển
}
