package javascript

import (
	"math"
	"slices"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Các phép toán của JavaScript cho runtime: a + b, a - b…, a < b…, -a, +a. Mỗi phép theo đặc tả: ToPrimitive, rồi
// chuỗi hay số. Một vế là lỗi thì lỗi ấy là kết quả.

// Add là a + b: sau ToPrimitive, một vế là chuỗi thì nối chữ của hai vế, không thì cộng số.
func Add(run Run, a, b value.Value) value.Value {
	if a.IsNumber() && b.IsNumber() {
		return value.Number(a.Number() + b.Number())
	}
	if a.IsString() && b.IsString() {
		return concatenate(a.String(), b.String())
	}
	pa, err := toPrimitive(run, a, "number")
	if err != nil {
		return value.Error(err)
	}
	pb, err := toPrimitive(run, b, "number")
	if err != nil {
		return value.Error(err)
	}
	if pa.IsString() || pb.IsString() {
		sa, err := toText(run, pa)
		if err != nil {
			return value.Error(err)
		}
		sb, err := toText(run, pb)
		if err != nil {
			return value.Error(err)
		}
		return concatenate(sa, sb)
	}
	na, err := toNumber(run, pa)
	if err != nil {
		return value.Error(err)
	}
	nb, err := toNumber(run, pb)
	if err != nil {
		return value.Error(err)
	}
	return value.Number(na + nb)
}

// concatenate nối hai chuỗi, gộp một nửa cặp cuối a với nửa cặp đầu b thành một ký tự, như mọi chuỗi của Kitwork.
func concatenate(a, b string) value.Value {
	if len(a)+len(b) > maxStringLength {
		return tooLong()
	}
	return value.String(mergeSurrogates(a + b))
}

// Arithmetic là a - b, a * b, a / b, a % b, a ** b (op là "-", "*", "/", "%", "**"): ToNumber của hai vế, rồi phép số
// của JavaScript (% giữ dấu của số bị chia như math.Mod; ** của ±1 với vô cực, và của mũ NaN, là NaN).
func Arithmetic(run Run, op string, a, b value.Value) value.Value {
	x, err := toNumber(run, a)
	if err != nil {
		return value.Error(err)
	}
	y, err := toNumber(run, b)
	if err != nil {
		return value.Error(err)
	}
	switch op {
	case "-":
		return value.Number(x - y)
	case "*":
		return value.Number(x * y)
	case "/":
		return value.Number(x / y)
	case "%":
		return value.Number(math.Mod(x, y))
	case "**":
		if y != y || (math.Abs(x) == 1 && math.IsInf(y, 0)) {
			return value.Number(math.NaN())
		}
		return value.Number(math.Pow(x, y))
	}
	return typeError("no operator %q", op)
}

// Relation là a < b, a > b, a <= b, a >= b (op là "<", ">", "<=", ">="): sau ToPrimitive, hai chuỗi so theo đơn vị
// UTF-16, như JavaScript; không thì so số, và NaN cho false.
func Relation(run Run, op string, a, b value.Value) value.Value {
	pa, err := toPrimitive(run, a, "number")
	if err != nil {
		return value.Error(err)
	}
	pb, err := toPrimitive(run, b, "number")
	if err != nil {
		return value.Error(err)
	}
	var order int
	if pa.IsString() && pb.IsString() {
		if n := len(pa.String()) + len(pb.String()); n >= smallText { // hai chuỗi đổi thành đơn vị UTF-16 rồi so
			if failure := spend(run, uint64(n)/unitsBytesPerUnit); failure.IsInvalid() {
				return failure
			}
		}
		order = slices.Compare(newUnitText(pa.String()).allUnits(), newUnitText(pb.String()).allUnits())
	} else {
		x, err := toNumber(run, pa)
		if err != nil {
			return value.Error(err)
		}
		y, err := toNumber(run, pb)
		if err != nil {
			return value.Error(err)
		}
		if x != x || y != y {
			return value.Boolean(false)
		}
		switch {
		case x < y:
			order = -1
		case x > y:
			order = 1
		}
	}
	switch op {
	case "<":
		return value.Boolean(order < 0)
	case ">":
		return value.Boolean(order > 0)
	case "<=":
		return value.Boolean(order <= 0)
	case ">=":
		return value.Boolean(order >= 0)
	}
	return typeError("no operator %q", op)
}

// Negate là -a: ToNumber rồi đổi dấu (-0 của 0, như JavaScript).
func Negate(run Run, a value.Value) value.Value {
	n, err := toNumber(run, a)
	if err != nil {
		return value.Error(err)
	}
	return value.Number(-n)
}
