package builtins

import (
	"math"
	"math/bits"
	"math/rand/v2"

	"github.com/kitwork/engine2/core/value"
)

// Math is JavaScript's Math: every function makes its arguments numbers first ("2.5" is 2.5, [4] is
// 4), the constants are float64 as JavaScript has them. It is built with Set calls in the order node
// lists Math's keys, never by ranging over a Go map, so Object.keys(Math) and JSON.stringify(Math) give
// the same order on every run.
func Math() value.Value {
	m := value.Object()
	for _, f := range []struct {
		name     string
		function func(float64) float64
	}{
		{"abs", math.Abs},
		{"acos", acos},
		{"acosh", acosh},
		{"asin", asin},
		{"asinh", asinh},
		{"atan", atan},
		{"atanh", atanh},
	} {
		m.Set(f.name, unary(f.name, f.function))
	}
	m.Set("atan2", binary("atan2", atan2))
	for _, f := range []struct {
		name     string
		function func(float64) float64
	}{
		{"ceil", math.Ceil},
		{"cbrt", cbrt},
		{"expm1", expm1},
		{"clz32", countLeadingZeros},
		{"cos", fdlibmCos},
		{"cosh", cosh},
		{"exp", exp},
		{"floor", math.Floor},
		{"fround", fround},
	} {
		m.Set(f.name, unary(f.name, f.function))
	}
	m.Set("hypot", native("hypot", 2, hypot))
	m.Set("imul", binary("imul", imul))
	for _, f := range []struct {
		name     string
		function func(float64) float64
	}{
		{"log", log},
		{"log1p", log1p},
		{"log2", log2},
		{"log10", log10},
	} {
		m.Set(f.name, unary(f.name, f.function))
	}
	m.Set("max", extreme("max", func(a, b float64) bool { return a > b || a == 0 && b == 0 && !math.Signbit(a) }, math.Inf(-1)))
	m.Set("min", extreme("min", func(a, b float64) bool { return a < b || a == 0 && b == 0 && math.Signbit(a) }, math.Inf(1)))
	m.Set("pow", binary("pow", power))
	m.Set("random", native("random", 0, func(...value.Value) value.Value { return value.Number(rand.Float64()) }))
	for _, f := range []struct {
		name     string
		function func(float64) float64
	}{
		{"round", round},
		{"sign", sign},
		{"sin", fdlibmSin},
		{"sinh", sinh},
		{"sqrt", math.Sqrt},
		{"tan", tan},
		{"tanh", tanh},
		{"trunc", math.Trunc},
	} {
		m.Set(f.name, unary(f.name, f.function))
	}
	for _, constant := range []struct {
		name   string
		number float64
	}{
		{"E", math.E},
		{"LN10", math.Ln10},
		{"LN2", math.Ln2},
		{"LOG10E", math.Log10E},
		{"LOG2E", math.Log2E},
		{"PI", math.Pi},
		{"SQRT1_2", 1 / math.Sqrt2},
		{"SQRT2", math.Sqrt2},
	} {
		m.Set(constant.name, value.Number(constant.number))
	}
	return m
}

func toNumber(v value.Value) float64 { return v.ToNumber().Number() }

// numberOf is v made a number, as a Math function makes each argument one, or the failure of making it:
// an object's own valueOf that fails, which toNumber read as NaN (Math.max(price, 1) was 1, 05/11/2026),
// where JavaScript throws.
func numberOf(v value.Value) (float64, value.Value) {
	n := v.ToNumber()
	if n.IsInvalid() {
		return 0, n
	}
	return n.Number(), value.Value{}
}

// convertsWithCode is whether one of the first count arguments, the ones a builtin converts, could run
// code to convert (an object, an array), which the builtin then converts first (convertedThen, with hints
// for those count): the common call asks it and converts nothing more than it did. An argument past them
// is not looked at, as JavaScript does not convert it, and convertedThen's call then converts nothing.
func convertsWithCode(arguments []value.Value, count int) bool {
	for _, a := range arguments[:min(count, len(arguments))] {
		if a.ConvertsWithCode() {
			return true
		}
	}
	return false
}

// convertedThen is call with arguments converted as hints says (value.ConvertArguments), or the first
// failure of that: the way of a builtin one of whose arguments could run code to convert (convertsWithCode),
// out of the builtin, so that its common call keeps the frame and the code it had (a conversion in the
// builtin itself made Math.max 20% slower, 05/11/2026). call is the builtin, which then converts nothing.
func convertedThen(hints string, call func(arguments ...value.Value) value.Value, arguments []value.Value) value.Value {
	converted, failure := value.ConvertArguments(hints, arguments)
	if failure.IsInvalid() {
		return failure
	}
	return call(converted...)
}

// unary and binary are a Math function of one or two numbers; each argument is made a number first,
// in order, the failure of that the result.
func unary(name string, f func(float64) float64) value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if convertsWithCode(arguments, 1) {
			return convertedThen("n", call, arguments)
		}
		return value.Number(f(toNumber(argument(arguments, 0))))
	}
	return native(name, 1, call)
}

func binary(name string, f func(float64, float64) float64) value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if convertsWithCode(arguments, 2) {
			return convertedThen("nn", call, arguments)
		}
		return value.Number(f(toNumber(argument(arguments, 0)), toNumber(argument(arguments, 1))))
	}
	return native(name, 2, call)
}

// round is Math.round: the nearest whole number, a half up; -0 for -0.5 up to 0; a number past 2^52
// is already whole. floor(x + 0.5) got 0.49999999999999994 wrong.
func round(x float64) float64 {
	switch {
	case x != x, math.IsInf(x, 0), x == 0, math.Abs(x) >= 1<<52:
		return x
	case x > 0 && x < 0.5:
		return 0
	case x < 0 && x >= -0.5:
		return math.Copysign(0, -1)
	}
	return math.Floor(x + 0.5)
}

// power is Math.pow: NaN for an exponent NaN and for 1 or -1 to an infinite exponent, where Go says 1.
func power(x, y float64) float64 {
	if y != y || math.Abs(x) == 1 && math.IsInf(y, 0) {
		return math.NaN()
	}
	return pow(x, y)
}

func sign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x // 0, -0 and NaN are themselves
}

// fround is Math.fround: the nearest float32, as a float64 (Go's conversion rounds to nearest, ties
// to even, as IEEE 754 and JavaScript do).
func fround(x float64) float64 { return float64(float32(x)) }

// toUint32 is JavaScript's ToUint32: the integer part modulo 2^32, 0 for NaN and the infinities.
func toUint32(x float64) uint32 {
	if x != x || math.IsInf(x, 0) {
		return 0
	}
	x = math.Mod(math.Trunc(x), 1<<32)
	if x < 0 {
		x += 1 << 32
	}
	return uint32(x)
}

// countLeadingZeros is Math.clz32: the zero bits above the highest one of ToUint32(x), 32 for 0.
func countLeadingZeros(x float64) float64 { return float64(bits.LeadingZeros32(toUint32(x))) }

// imul is Math.imul: the low 32 bits of the product of ToInt32(a) and ToInt32(b), signed.
func imul(a, b float64) float64 { return float64(int32(toUint32(a) * toUint32(b))) }

// hypot is Math.hypot(…), as V8 computes it so the last bit agrees: every argument made a number first;
// an infinity wins over NaN; no argument, or only zeros, is 0; otherwise the square root of the sum of
// the squares, each argument divided by the largest first so nothing overflows, summed with Kahan's
// compensation.
func hypot(arguments ...value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Number(0)
	}
	magnitudes := make([]float64, len(arguments))
	largest, anyNaN := 0.0, false
	for i, a := range arguments {
		n, failure := numberOf(a)
		if failure.IsInvalid() {
			return failure
		}
		if n != n {
			anyNaN = true
			continue
		}
		magnitudes[i] = math.Abs(n)
		largest = math.Max(largest, magnitudes[i])
	}
	switch {
	case math.IsInf(largest, 1):
		return value.Number(math.Inf(1))
	case anyNaN:
		return value.Number(math.NaN())
	case largest == 0:
		return value.Number(0)
	}
	sum, compensation := 0.0, 0.0
	for _, magnitude := range magnitudes {
		n := magnitude / largest
		summand := float64(n*n) - compensation // rounded on its own, never fused (ieee754.go)
		preliminary := sum + summand
		compensation = (preliminary - sum) - summand
		sum = preliminary
	}
	return value.Number(math.Sqrt(sum) * largest)
}

// extreme is Math.min and Math.max: every argument a number, one NaN makes NaN, -0 below 0.
func extreme(name string, less func(a, b float64) bool, none float64) value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		result := none
		for i, a := range arguments {
			// an argument that could run code to convert: all are made numbers first, in order (the
			// ones before it are numbers already, which converting again leaves as they are)
			if a.ConvertsWithCode() {
				return convertedThen("n*", call, arguments)
			}
			n := toNumber(a)
			if i == 0 || n != n || less(n, result) {
				result = n
			}
		}
		if result != result {
			return value.Number(math.NaN())
		}
		return value.Number(result)
	}
	return native(name, 2, call)
}
