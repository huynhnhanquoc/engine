package javascript

import (
	"math"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Ba phép so bằng của JavaScript, dựng trên value.Identical (cùng loại, cùng bit, cùng chỗ sau con trỏ). Chúng chỉ khác
// nhau ở số: === coi NaN khác mọi thứ và 0 bằng -0; SameValueZero (includes, Map) coi NaN bằng NaN và 0 bằng -0;
// SameValue (Object.is) coi NaN bằng NaN và 0 khác -0. Thời điểm và khoảng thời gian là giá trị, không phải object:
// hai cái cùng một mốc là bằng nhau.

// StrictEquals là a === b.
func StrictEquals(a, b value.Value) bool {
	if a.IsNumber() && b.IsNumber() {
		return a.Number() == b.Number()
	}
	return a.Identical(b)
}

// SameValueZero là a và b bằng nhau như includes so.
func SameValueZero(a, b value.Value) bool {
	if a.IsNumber() && b.IsNumber() {
		x, y := a.Number(), b.Number()
		return x == y || x != x && y != y
	}
	return a.Identical(b)
}

// SameValue là Object.is(a, b).
func SameValue(a, b value.Value) bool {
	if a.IsNumber() && b.IsNumber() {
		x, y := a.Number(), b.Number()
		if x != x || y != y {
			return x != x && y != y
		}
		return x == y && math.Signbit(x) == math.Signbit(y)
	}
	return a.Identical(b)
}
