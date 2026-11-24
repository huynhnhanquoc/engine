package javascript

import (
	"math"
	"math/bits"
	"math/rand/v2"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Math của JavaScript. Mỗi hàm đổi các đối số thành số trước, theo thứ tự ("2.5" là 2.5, [4] là 4, một object qua
// valueOf riêng của nó; lỗi của phép đổi là kết quả). sin, cos, exp, pow… tính như V8 tính (fdlibm, ieee754.go), nên
// kết quả trùng node tới bit cuối; math của Go lệch bit cuối ở khoảng một phần tư đối số. Các khoá theo thứ tự node
// liệt kê. Chép từ engine2 (core/builtins/math.go).
var mathNamespace = newNamespace("Math",
	unary("abs", math.Abs),
	unary("acos", acos),
	unary("acosh", acosh),
	unary("asin", asin),
	unary("asinh", asinh),
	unary("atan", atan),
	unary("atanh", atanh),
	binary("atan2", atan2),
	unary("ceil", math.Ceil),
	unary("cbrt", cbrt),
	unary("expm1", expm1),
	unary("clz32", countLeadingZeros),
	unary("cos", fdlibmCos),
	unary("cosh", cosh),
	unary("exp", exp),
	unary("floor", math.Floor),
	unary("fround", fround),
	member{"hypot", native("hypot", 2, hypot)},
	binary("imul", imul),
	unary("log", log),
	unary("log1p", log1p),
	unary("log2", log2),
	unary("log10", log10),
	extreme("max", func(a, b float64) bool { return a > b || a == 0 && b == 0 && !math.Signbit(a) }, math.Inf(-1)),
	extreme("min", func(a, b float64) bool { return a < b || a == 0 && b == 0 && math.Signbit(a) }, math.Inf(1)),
	binary("pow", power),
	member{"random", native("random", 0, func(Run, []value.Value) value.Value { return value.Number(rand.Float64()) })},
	unary("round", round),
	unary("sign", sign),
	unary("sin", fdlibmSin),
	unary("sinh", sinh),
	unary("sqrt", math.Sqrt),
	unary("tan", tan),
	unary("tanh", tanh),
	unary("trunc", math.Trunc),
	member{"E", value.Number(math.E)},
	member{"LN10", value.Number(math.Ln10)},
	member{"LN2", value.Number(math.Ln2)},
	member{"LOG10E", value.Number(math.Log10E)},
	member{"LOG2E", value.Number(math.Log2E)},
	member{"PI", value.Number(math.Pi)},
	member{"SQRT1_2", value.Number(1 / math.Sqrt2)},
	member{"SQRT2", value.Number(math.Sqrt2)},
)

// unary là một hàm Math của một số.
func unary(name string, f func(float64) float64) member {
	return member{name, native(name, 1, func(run Run, arguments []value.Value) value.Value {
		x, err := toNumber(run, argument(arguments, 0))
		if err != nil {
			return value.Error(err)
		}
		return value.Number(f(x))
	})}
}

// binary là một hàm Math của hai số: đối số đầu đổi trước, lỗi của nó dừng ở đó.
func binary(name string, f func(float64, float64) float64) member {
	return member{name, native(name, 2, func(run Run, arguments []value.Value) value.Value {
		x, err := toNumber(run, argument(arguments, 0))
		if err != nil {
			return value.Error(err)
		}
		y, err := toNumber(run, argument(arguments, 1))
		if err != nil {
			return value.Error(err)
		}
		return value.Number(f(x, y))
	})}
}

// numbers là mọi đối số đổi thành số, theo thứ tự; lỗi đầu tiên dừng ở đó.
func numbers(run Run, arguments []value.Value, into []float64) ([]float64, error) {
	out := into[:0] // một mảng của hàm gọi trên ngăn xếp: Math.max(a, b) không cấp phát
	for _, a := range arguments {
		n, err := toNumber(run, a)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// extreme là Math.max và Math.min: mọi đối số đổi thành số trước (cả sau một NaN, như JavaScript), một NaN thì kết
// quả là NaN, -0 nhỏ hơn 0; không đối số là none (-Infinity cho max, Infinity cho min).
func extreme(name string, better func(a, b float64) bool, none float64) member {
	return member{name, native(name, 2, func(run Run, arguments []value.Value) value.Value {
		var buffer [4]float64
		ns, err := numbers(run, arguments, buffer[:])
		if err != nil {
			return value.Error(err)
		}
		result := none
		for _, n := range ns {
			if n != n {
				return value.Number(math.NaN())
			}
			if better(n, result) {
				result = n
			}
		}
		return value.Number(result)
	})}
}

// hypot là Math.hypot(…) như V8 tính, để bit cuối trùng: mọi đối số đổi thành số trước; vô cực thắng NaN; không đối
// số hay toàn số 0 là 0; còn lại là căn của tổng bình phương, mỗi số chia cho số lớn nhất trước để không tràn, cộng
// bằng bù Kahan.
func hypot(run Run, arguments []value.Value) value.Value {
	var buffer [4]float64
	ns, err := numbers(run, arguments, buffer[:])
	if err != nil {
		return value.Error(err)
	}
	largest, anyNaN := 0.0, false
	for i, n := range ns {
		if n != n {
			anyNaN = true
			ns[i] = 0
			continue
		}
		ns[i] = math.Abs(n)
		largest = math.Max(largest, ns[i])
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
	for _, magnitude := range ns {
		n := magnitude / largest
		summand := float64(n*n) - compensation // làm tròn riêng, không gộp (ieee754.go)
		preliminary := sum + summand
		compensation = (preliminary - sum) - summand
		sum = preliminary
	}
	return value.Number(math.Sqrt(sum) * largest)
}

// round là Math.round: số nguyên gần nhất, nửa thì lên; -0 cho -0.5 tới 0; số từ 2^52 đã nguyên. floor(x + 0.5) sai
// với 0.49999999999999994.
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

// power là Math.pow: NaN khi số mũ là NaN, và khi 1 hay -1 mũ vô cực, nơi Go cho 1.
func power(x, y float64) float64 {
	if y != y || math.Abs(x) == 1 && math.IsInf(y, 0) {
		return math.NaN()
	}
	return pow(x, y)
}

// sign là Math.sign: 1, -1, hay chính x (0, -0, NaN).
func sign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x
}

// fround là Math.fround: float32 gần nhất, viết lại thành float64 (Go đổi theo gần nhất, hoà thì chẵn, như IEEE 754).
func fround(x float64) float64 { return float64(float32(x)) }

// countLeadingZeros là Math.clz32: số bit 0 trên bit 1 cao nhất của ToUint32(x), 32 cho 0.
func countLeadingZeros(x float64) float64 { return float64(bits.LeadingZeros32(toUint32(x))) }

// imul là Math.imul: 32 bit thấp của tích ToInt32(a) và ToInt32(b), có dấu.
func imul(a, b float64) float64 { return float64(int32(toUint32(a) * toUint32(b))) }
