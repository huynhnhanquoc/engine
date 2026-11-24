package javascript

import (
	"strconv"
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Phương thức của số, boolean, thời điểm, khoảng thời gian, bytes và hàm. Thời điểm có getTime, valueOf, toISOString
// (UTC) và format (theo múi của nơi chạy); không có getter, setter của Date, như engine2. Khoảng thời gian và bytes là loại của Kitwork,
// không có trong JavaScript: chữ của chúng theo engine2.

var numberMethods = map[string]method{
	"toExponential": numberToExponential,
	"toFixed":       numberToFixed,
	"toPrecision":   numberToPrecision,
	"toString":      numberToString,
	"valueOf":       func(_ Run, this value.Value, _ []value.Value) value.Value { return this },
}

// numberToFixed là n.toFixed(digits): digits từ 0 tới 100; từ 1e21 trở lên viết như String(n).
func numberToFixed(run Run, this value.Value, arguments []value.Value) value.Value {
	digits, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	if digits < 0 || digits > 100 {
		return rangeError("toFixed() digits argument must be between 0 and 100")
	}
	return value.String(fixedText(this.Number(), int(digits)))
}

// numberToExponential là n.toExponential(fraction): vắng thì ít chữ số nhất đọc lại được n; số không hữu hạn viết
// thành chữ trước khi kiểm khoảng của fraction, như JavaScript.
func numberToExponential(run Run, this value.Value, arguments []value.Value) value.Value {
	fraction, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	n := this.Number()
	if text, nonFinite := nonFiniteText(n); nonFinite {
		return value.String(text)
	}
	if fraction < 0 || fraction > 100 {
		return rangeError("toExponential() argument must be between 0 and 100")
	}
	return value.String(exponentialText(n, int(fraction), argument(arguments, 0).IsUndefined()))
}

// numberToPrecision là n.toPrecision(precision): vắng thì String(n); precision từ 1 tới 100.
func numberToPrecision(run Run, this value.Value, arguments []value.Value) value.Value {
	n := this.Number()
	if argument(arguments, 0).IsUndefined() {
		return value.String(numberText(n))
	}
	precision, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	if text, nonFinite := nonFiniteText(n); nonFinite {
		return value.String(text)
	}
	if precision < 1 || precision > 100 {
		return rangeError("toPrecision() argument must be between 1 and 100")
	}
	return value.String(precisionText(n, int(precision)))
}

// numberToString là n.toString(radix): radix từ 2 tới 36, vắng là 10.
func numberToString(run Run, this value.Value, arguments []value.Value) value.Value {
	n := this.Number()
	radix := 10.0
	if r := argument(arguments, 0); !r.IsUndefined() {
		var err error
		if radix, err = toInteger(run, r); err != nil {
			return value.Error(err)
		}
	}
	if radix < 2 || radix > 36 {
		return rangeError("toString() radix must be between 2 and 36")
	}
	if radix == 10 {
		return value.String(numberText(n))
	}
	return value.String(radixText(n, int(radix)))
}

var booleanMethods = map[string]method{
	"toString": func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
	"valueOf":  func(_ Run, this value.Value, _ []value.Value) value.Value { return this },
}

var timeMethods = map[string]method{
	"format":      timeFormat,
	"getTime":     timeMilliseconds,
	"valueOf":     timeMilliseconds,
	"toISOString": timeISO,
	"toString":    func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
}

// timeMilliseconds là t.getTime(): số mili giây từ 1970 UTC, phần dưới mili giây bỏ đi như Date giữ.
func timeMilliseconds(_ Run, this value.Value, _ []value.Value) value.Value {
	return value.Number(float64(this.Time().UnixMilli()))
}

// timeISO là t.toISOString(): "2026-11-22T16:00:00.000Z", luôn UTC, luôn ba chữ số mili giây.
func timeISO(_ Run, this value.Value, _ []value.Value) value.Value {
	return value.String(isoText(this.Time()))
}

// timeFormat là t.format(layout): layout của Go ("2006-01-02 15:04", "15:04 MST"), viết theo múi giờ của nơi chạy
// (Located: múi của app với một request của site), như Date(năm, tháng, …) đọc; engine2 luôn viết theo UTC. Mốc là một,
// chỉ cách viết theo múi: t.toISOString() vẫn là UTC. layout không phải chữ là lỗi, không đổi thành chữ: t.format()
// hay t.format(5) là viết nhầm.
func timeFormat(run Run, this value.Value, arguments []value.Value) value.Value {
	layout := argument(arguments, 0)
	if !layout.IsString() {
		return typeError(`format takes a layout of Go, as "2006-01-02 15:04", got %s`, kindName(layout))
	}
	if len(layout.String()) > maxStringLength/2 { // một chữ của layout viết ra tới chín byte ("Wednesday" cho "Monday")
		return tooLong()
	}
	if failure := room(run, 9*uint64(len(layout.String()))); failure.IsInvalid() {
		return failure
	}
	return builtString(this.Time().In(locationOf(run)).Format(layout.String()))
}

// isoText là chữ của toISOString, cũng là chữ JSON.stringify viết cho một thời điểm (toJSON của Date).
func isoText(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

var durationMethods = map[string]method{
	"hours":        durationIn(time.Hour),
	"minutes":      durationIn(time.Minute),
	"seconds":      durationIn(time.Second),
	"milliseconds": durationIn(time.Millisecond),
	"toString":     func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
}

// durationIn là d.hours() và các chữ cùng loại: khoảng thời gian tính bằng đơn vị ấy, có phần lẻ.
func durationIn(unit time.Duration) method {
	return func(_ Run, this value.Value, _ []value.Value) value.Value {
		d := this.Duration()
		whole, rest := d/unit, d%unit
		return value.Number(float64(whole) + float64(rest)/float64(unit))
	}
}

var bytesMethods = map[string]method{
	"toString": func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
}

// Phương thức của một hàm: call và apply gọi nó, bỏ qua đối số đầu (this): hàm của Kitwork là hàm mũi tên, không đọc
// this, hàm có sẵn cũng không.
var functionMethods = map[string]method{
	"apply": functionApply,
	"call": func(run Run, this value.Value, arguments []value.Value) value.Value {
		return call(run, this, arguments[min(1, len(arguments)):]...)
	},
	"toString": func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
}

// functionApply là f.apply(this, list): list là một mảng, một object giống mảng ({ length: 2, 0: "a" }), hay null và
// undefined cho không đối số; thứ khác là TypeError như JavaScript.
func functionApply(run Run, this value.Value, arguments []value.Value) value.Value {
	switch list := argument(arguments, 1); {
	case list.IsNullish():
		return call(run, this)
	case list.IsArray():
		return call(run, this, list.Items()...)
	case list.IsObject():
		n, err := lengthOf(run, list)
		if err != nil {
			return value.Error(err)
		}
		items := make([]value.Value, n)
		for i := range items {
			items[i], _ = list.Get(strconv.Itoa(i))
		}
		return call(run, this, items...)
	}
	return typeError("apply takes an array of arguments")
}

// errNotAFunction là lỗi của một hàm gọi lại không phải hàm.
func errNotAFunction(v value.Value) error {
	return value.Errorf(value.TypeCode, "%s is not a function", kindName(v))
}
