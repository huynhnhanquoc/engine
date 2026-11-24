package javascript

import (
	"math"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Phép chuyển đổi của JavaScript. Hàm viết hoa (ToBoolean, ToNumber, ToString) là cho runtime, trả value.Value (một lỗi
// khi valueOf hay toString riêng của một object ném); hàm viết thường trả kiểu Go và một error, cho các phương thức ở
// đây. Loại JavaScript không có theo luật engine2 đã chốt: thời điểm là số mili giây (như valueOf của Date) nhưng không
// có chữ của riêng nó (JavaScript viết nó theo múi giờ của máy chạy), khoảng thời gian là số mili giây và chữ Go viết
// ("1h30m0s"), bytes là chữ của các byte, hàm không có chữ (JavaScript viết mã nguồn, Kitwork không giữ).

// ToBoolean là giá trị đúng sai của v: undefined, null, false, 0, -0, NaN và "" là false; mọi thứ khác là true.
func ToBoolean(v value.Value) bool {
	switch {
	case v.IsNullish(), v.IsInvalid():
		return false
	case v.IsBoolean():
		return v.Boolean()
	case v.IsNumber():
		n := v.Number()
		return n == n && n != 0
	case v.IsString():
		return v.String() != ""
	}
	return true
}

// ToNumber là Number(v).
func ToNumber(run Run, v value.Value) value.Value {
	n, err := toNumber(run, v)
	if err != nil {
		return value.Error(err)
	}
	return value.Number(n)
}

// ToString là String(v).
func ToString(run Run, v value.Value) value.Value {
	if v.IsString() {
		return v
	}
	s, err := toText(run, v)
	if err != nil {
		return value.Error(err)
	}
	return value.String(s)
}

func toNumber(run Run, v value.Value) (float64, error) {
	switch {
	case v.IsNumber():
		return v.Number(), nil
	case v.IsUndefined():
		return math.NaN(), nil
	case v.IsNull():
		return 0, nil
	case v.IsBoolean():
		if v.Boolean() {
			return 1, nil
		}
		return 0, nil
	case v.IsString():
		if s := v.String(); len(s) >= smallText {
			if failure := spend(run, uint64(len(s))/numberBytesPerUnit); failure.IsInvalid() {
				return 0, failure.Error()
			}
		}
		return stringToNumber(v.String()), nil
	case v.IsTime():
		return float64(v.Time().UnixMilli()), nil
	case v.IsDuration():
		return float64(v.Duration()) / 1e6, nil
	case v.IsInvalid():
		return 0, v.Error()
	case v.IsObject(), v.IsArray():
		primitive, err := toPrimitive(run, v, "number")
		if err != nil {
			return 0, err
		}
		return toNumber(run, primitive)
	}
	return math.NaN(), nil
}

func toText(run Run, v value.Value) (string, error) {
	switch {
	case v.IsString():
		return v.String(), nil
	case v.IsNumber():
		return numberText(v.Number()), nil
	case v.IsUndefined():
		return "undefined", nil
	case v.IsNull():
		return "null", nil
	case v.IsBoolean():
		if v.Boolean() {
			return "true", nil
		}
		return "false", nil
	case v.IsDuration():
		return v.Duration().String(), nil
	case v.IsBytes():
		return string(v.Bytes()), nil
	case v.IsInvalid():
		return "", v.Error()
	case v.IsTime():
		return "", value.Errorf(value.TypeCode, `a time has no text of its own: JavaScript writes it in the zone of the machine; write t.toISOString()`)
	case v.IsFunction():
		return "", value.Errorf(value.TypeCode, "a function has no text: JavaScript writes its source, which Kitwork does not keep")
	case v.IsStruct():
		return "", value.Errorf(value.TypeCode, "a struct of the engine has no text")
	case v.IsArray():
		return join(run, v, ",", nil)
	}
	primitive, err := toPrimitive(run, v, "string")
	if err != nil {
		return "", err
	}
	return toText(run, primitive)
}

// toPrimitive là ToPrimitive của một object hay một mảng; loại khác là chính nó. Mảng là các phần tử nối bằng dấu
// phẩy. Object qua valueOf và toString riêng của nó, theo thứ tự của gợi ý (hint "string": toString trước); không có
// toString riêng thì là "[object Object]" của Object.prototype, còn valueOf của Object.prototype trả chính object, không
// phải giá trị nguyên thuỷ. Một khoá cùng tên không phải hàm thì bị bỏ qua, như JavaScript.
func toPrimitive(run Run, v value.Value, hint string) (value.Value, error) {
	switch {
	case v.IsArray():
		s, err := join(run, v, ",", nil)
		return value.String(s), err
	case !v.IsObject():
		return v, nil
	}
	order := [2]string{"valueOf", "toString"}
	if hint == "string" {
		order = [2]string{"toString", "valueOf"}
	}
	for _, name := range order {
		own, found := v.Get(name)
		if !found {
			if name == "toString" {
				return value.String("[object Object]"), nil
			}
			continue
		}
		if !own.IsFunction() {
			continue
		}
		result := call(run, own)
		if result.IsInvalid() {
			return value.Value{}, result.Error()
		}
		if isPrimitive(result) {
			return result, nil
		}
	}
	return value.Value{}, value.Errorf(value.TypeCode, "cannot convert object to primitive value")
}

// isPrimitive là v là giá trị nguyên thuỷ của JavaScript: undefined, null, số, boolean, chuỗi.
func isPrimitive(v value.Value) bool {
	return v.IsNullish() || v.IsNumber() || v.IsBoolean() || v.IsString()
}

// join là các phần tử của mảng v, mỗi cái thành chữ (undefined và null là ""), nối bằng separator: Array.prototype.join.
// seen là các mảng đang được nối ở trên: một mảng chứa chính nó là "" ở chỗ ấy, như V8, không phải vòng lặp mãi.
func join(run Run, v value.Value, separator string, seen []value.Value) (string, error) {
	for _, outer := range seen {
		if outer.Identical(v) {
			return "", nil
		}
	}
	seen = append(seen, v)
	var b strings.Builder
	g := grower{run: run}
	for i := range v.Length() {
		if i > 0 {
			b.WriteString(separator)
		}
		item := v.Index(i)
		var piece string
		var err error
		switch {
		case item.IsNullish():
		case item.IsArray():
			piece, err = join(run, item, ",", seen)
		default:
			piece, err = toText(run, item)
		}
		if err != nil {
			return "", err
		}
		if b.Len()+len(piece) > maxStringLength {
			return "", tooLong().Error()
		}
		if failure := g.fits(uint64(b.Len() + len(separator) + len(piece))); failure.IsInvalid() {
			return "", failure.Error()
		}
		b.WriteString(piece)
	}
	return mergeSurrogates(b.String()), nil
}

// toInteger là ToIntegerOrInfinity(v): NaN là 0, vô cực giữ nguyên, số khác bỏ phần lẻ.
func toInteger(run Run, v value.Value) (float64, error) {
	n, err := toNumber(run, v)
	if err != nil {
		return 0, err
	}
	return toIntegerOrInfinity(n), nil
}

// toUint32 là ToUint32 của một số: NaN và vô cực là 0, số khác bỏ phần lẻ rồi quấn vào 0 tới 2^32 − 1.
func toUint32(n float64) uint32 { return uint32(toInt32(n)) }

// relativeIndex là chỉ số tương đối của slice và at: số âm đếm từ cuối, kết quả kẹp vào 0..length.
func relativeIndex(n float64, length int) int {
	i := toIntegerOrInfinity(n)
	if i < 0 {
		i += float64(length)
		if i < 0 {
			return 0
		}
	}
	if i > float64(length) {
		return length
	}
	return int(i)
}

// clampIndex là của substring: số âm là 0, quá cuối là cuối.
func clampIndex(n float64, length int) int {
	i := toIntegerOrInfinity(n)
	if i < 0 {
		return 0
	}
	if i > float64(length) {
		return length
	}
	return int(i)
}
