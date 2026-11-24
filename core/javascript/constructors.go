package javascript

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Number, String, Array, Boolean gọi như hàm (Number("3") là 3, String(5) là "5", Array(1, 2) là [1, 2]) và dùng làm
// hàm gọi lại (arr.map(Number), arr.filter(Boolean)), cùng các khoá tĩnh của chúng, theo JavaScript, đối chiếu với
// node. Kitwork không có new Number(…), new String(…): object bọc một giá trị nguyên thuỷ. Chép từ engine2
// (core/builtins), chỗ khác nói ở định nghĩa.

// numberFunction là Number(x): ToNumber của x, 0 khi không có đối số (Number(undefined) là NaN). Khoá tĩnh: isFinite,
// isInteger, isNaN, isSafeInteger hỏi về một số mà không đổi đối số (Number.isNaN("x") là false, isNaN("x") là true);
// parseFloat, parseInt là chính hai hàm toàn cục; các hằng theo thứ tự node liệt kê.
var numberFunction = constructor("Number", 1, func(run Run, arguments []value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Number(0)
	}
	return ToNumber(run, arguments[0])
},
	member{"isFinite", native("isFinite", 1, numberWhere(func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }))},
	member{"isInteger", native("isInteger", 1, numberWhere(isInteger))},
	member{"isNaN", native("isNaN", 1, numberWhere(math.IsNaN))},
	member{"isSafeInteger", native("isSafeInteger", 1, numberWhere(func(n float64) bool {
		return isInteger(n) && math.Abs(n) <= maxSafeInteger
	}))},
	member{"parseFloat", parseFloatFunction},
	member{"parseInt", parseIntFunction},
	member{"MAX_VALUE", value.Number(math.MaxFloat64)},
	member{"MIN_VALUE", value.Number(math.SmallestNonzeroFloat64)},
	member{"NaN", value.Number(math.NaN())},
	member{"NEGATIVE_INFINITY", value.Number(math.Inf(-1))},
	member{"POSITIVE_INFINITY", value.Number(math.Inf(1))},
	member{"MAX_SAFE_INTEGER", value.Number(maxSafeInteger)},
	member{"MIN_SAFE_INTEGER", value.Number(-maxSafeInteger)},
	member{"EPSILON", value.Number(math.Nextafter(1, 2) - 1)},
)

// maxSafeInteger là 2^53 − 1: số nguyên lớn nhất mà số kế nó cũng viết đúng được.
const maxSafeInteger = 1<<53 - 1

// numberWhere là một khoá tĩnh của Number hỏi test về một số: false khi đối số không phải số.
func numberWhere(test func(float64) bool) func(Run, []value.Value) value.Value {
	return func(_ Run, arguments []value.Value) value.Value {
		a := argument(arguments, 0)
		return value.Boolean(a.IsNumber() && test(a.Number()))
	}
}

func isInteger(n float64) bool { return !math.IsInf(n, 0) && math.Trunc(n) == n }

// booleanFunction là Boolean(x): ToBoolean của x, false khi không có đối số.
var booleanFunction = native("Boolean", 1, func(_ Run, arguments []value.Value) value.Value {
	return value.Boolean(ToBoolean(argument(arguments, 0)))
})

// stringFunction là String(x): ToString của x, "" khi không có đối số (String(undefined) là "undefined"). Thời điểm và
// hàm không có chữ, như ToString của Kitwork (convert.go).
var stringFunction = constructor("String", 1, func(run Run, arguments []value.Value) value.Value {
	if len(arguments) == 0 {
		return value.String("")
	}
	return ToString(run, arguments[0])
},
	member{"fromCharCode", native("fromCharCode", 1, fromCharCode)},
	member{"fromCodePoint", native("fromCodePoint", 1, fromCodePoint)},
)

// fromCharCode là String.fromCharCode(…): mỗi đối số đổi thành số, theo thứ tự, rồi lấy 16 bit thấp (ToUint16) làm một
// đơn vị UTF-16; một nửa cao rồi một nửa thấp liền nhau là một ký tự.
func fromCharCode(run Run, arguments []value.Value) value.Value {
	units := make([]uint16, len(arguments))
	for i, a := range arguments {
		n, err := toNumber(run, a)
		if err != nil {
			return value.Error(err)
		}
		units[i] = uint16(toUint32(n))
	}
	return builtString(encodeUnits(units))
}

// fromCodePoint là String.fromCodePoint(…): mỗi đối số đổi thành số rồi kiểm, trước khi tới đối số sau, như
// JavaScript: một số nguyên từ 0 tới 0x10FFFF, khác là RangeError; trên 0xFFFF viết thành cặp của nó.
func fromCodePoint(run Run, arguments []value.Value) value.Value {
	units := make([]uint16, 0, len(arguments))
	for _, a := range arguments {
		n, err := toNumber(run, a)
		if err != nil {
			return value.Error(err)
		}
		if n != math.Trunc(n) || n < 0 || n > 0x10FFFF { // NaN khác chính nó; vô cực ngoài khoảng
			return rangeError("invalid code point %s", numberText(n))
		}
		if point := rune(n); point < 0x10000 {
			units = append(units, uint16(point))
		} else {
			point -= 0x10000
			units = append(units, uint16(0xD800+point>>10), uint16(0xDC00+point&0x3FF))
		}
	}
	return builtString(encodeUnits(units))
}

// builtString là s đã dựng, hay lỗi khi nó dài quá maxStringLength.
func builtString(s string) value.Value {
	if len(s) > maxStringLength {
		return tooLong()
	}
	return value.String(s)
}

// arrayFunction là Array(…): Array() là [], Array(a, b, …) là mảng các đối số, Array("3") là ["3"], Array(n) của một
// số là n lỗ của JavaScript: n phần tử undefined mang dấu lỗ (holes.go), nên Array(n).fill(0) là của JavaScript, còn
// Array(n).map(f) là lỗi cho tới khi mảng được điền kín. Một số không phải độ dài là RangeError như JavaScript; quá
// 2^24 là lỗi của Kitwork (maxArrayLength).
var arrayFunction = constructor("Array", 1, arrayCall,
	member{"isArray", native("isArray", 1, func(_ Run, arguments []value.Value) value.Value {
		return value.Boolean(argument(arguments, 0).IsArray())
	})},
	member{"from", native("from", 1, arrayFrom)},
	member{"of", native("of", 0, func(_ Run, arguments []value.Value) value.Value { return value.Array(arguments...) })},
)

func arrayCall(run Run, arguments []value.Value) value.Value {
	if len(arguments) != 1 || !arguments[0].IsNumber() {
		return value.Array(arguments...)
	}
	n := arguments[0].Number()
	if n != math.Trunc(n) || n < 0 {
		return rangeError("invalid array length")
	}
	if err := arrayLength(n); err != nil {
		return value.Error(err)
	}
	if failure := room(run, uint64(n)*value.ItemBytes); failure.IsInvalid() {
		return failure
	}
	holes := value.Array()
	holes.Push(make([]value.Value, int(n))...)
	return holes.WithHoles()
}

// maxArrayLength là số phần tử nhiều nhất một hàm có sẵn dựng một mảng từ một độ dài (Array.from({ length: n })):
// 2^24 phần tử là 384 MB giá trị. Quá nó là lỗi trước khi cấp phát; quá 2^32 − 1 là RangeError của chính JavaScript.
const maxArrayLength = 1 << 24

// arrayLength kiểm một độ dài sắp dựng.
func arrayLength(length float64) error {
	switch {
	case length > math.MaxUint32:
		return value.Errorf(value.RangeCode, "invalid array length")
	case length > maxArrayLength:
		return value.Errorf(value.RangeCode, "an array of %s items is more than the %d Kitwork makes at once", numberText(length), maxArrayLength)
	}
	return nil
}

// toLength là ToLength của JavaScript: phần nguyên, 0 cho NaN và số âm, tối đa 2^53 − 1.
func toLength(n float64) float64 {
	n = math.Trunc(n)
	if !(n > 0) {
		return 0
	}
	return math.Min(n, maxSafeInteger)
}

// lengthOf là độ dài của một object giống mảng ({ length: 2, 0: "a" }): khoá length đổi thành số rồi ToLength, đã kiểm.
func lengthOf(run Run, o value.Value) (int, error) {
	l, _ := o.Get("length")
	n, err := toNumber(run, l)
	if err != nil {
		return 0, err
	}
	length := toLength(n)
	if err := arrayLength(length); err != nil {
		return 0, err
	}
	return int(length), nil
}

// arrayFrom là Array.from(items, map): các phần tử của một mảng (đọc dần, như iterator của mảng), các ký tự của một
// chuỗi, các byte của bytes (mỗi byte một số), hay các khoá 0 tới length − 1 của một object giống mảng
// ({ length: 2 } là [undefined, undefined]); mỗi phần tử qua map(phần tử, chỉ số) khi có map, ngay khi được đọc. Số,
// boolean, thời điểm không có phần tử: []. null và undefined là lỗi như JavaScript; một hàm cũng vậy (JavaScript đọc
// nó như một object có length là số tham số), và một struct của engine.
func arrayFrom(run Run, arguments []value.Value) value.Value {
	items, mapper := argument(arguments, 0), argument(arguments, 1)
	if !mapper.IsUndefined() && !mapper.IsFunction() {
		return value.Error(errNotAFunction(mapper))
	}
	length := func() int { return 0 }
	var at func(i int) value.Value
	switch {
	case items.IsNullish():
		return typeError("%s is not iterable", kindName(items))
	case items.IsStruct():
		return value.Error(notData(items))
	case items.IsFunction():
		return typeError("Array.from cannot read a function as a list of items")
	case items.IsArray():
		length, at = items.Length, items.Index
	case items.IsString():
		if failure := room(run, uint64(utf8.RuneCountInString(items.String()))*value.ItemBytes); failure.IsInvalid() {
			return failure
		}
		points := codePoints(items.String())
		length, at = func() int { return len(points) }, func(i int) value.Value { return points[i] }
	case items.IsBytes():
		data := items.Bytes()
		length, at = func() int { return len(data) }, func(i int) value.Value { return value.Number(float64(data[i])) }
	case items.IsObject():
		n, err := lengthOf(run, items)
		if err != nil {
			return value.Error(err)
		}
		if failure := room(run, uint64(n)*value.ItemBytes); failure.IsInvalid() {
			return failure
		}
		length = func() int { return n }
		at = func(i int) value.Value {
			item, _ := items.Get(strconv.Itoa(i))
			return item
		}
	}
	out := value.Array()
	if mapper.IsFunction() { // các phần tử map đã làm: runtime thấy chúng khi đếm lại trong lúc map chạy
		defer letGo(run, keep(run, Kept{Items: &[]value.Value{out}}))
	}
	if failure := spend(run, uint64(length())*fromItemUnits); failure.IsInvalid() {
		return failure
	}
	buffer := make([]value.Value, 2)
	for i := 0; i < length(); i++ {
		if i >= maxArrayLength {
			return value.Error(arrayLength(maxArrayLength + 1)) // map thêm vào mảng nguồn mãi
		}
		item := at(i)
		if mapper.IsFunction() {
			if item = call(run, mapper, callArguments(buffer, item, value.Number(float64(i)))...); item.IsInvalid() {
				return item
			}
		}
		out.Push(item)
	}
	return out
}
