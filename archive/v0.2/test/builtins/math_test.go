package builtins_test

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os/exec"
	"strconv"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// mathArguments are the numbers TestMathWithNode passes to every function of Math: the same pseudo-random
// numbers on every run (a fixed seed) over the ranges where the algorithms change course (below 1, the
// reduction of sin near multiples of pi/2, huge arguments that need 2/pi to many bits, subnormals), and
// the edges by hand.
func mathArguments() []float64 {
	random := rand.New(rand.NewPCG(1, 2))
	arguments := []float64{
		0, math.Copysign(0, -1), 0.5, 1, -1, 2, 3, 7, 2024, 65, 1e22, math.Pi / 2, math.Pi, 1e300, 1e-300,
		5e-324, 0.1, 0.25, 0.75, 1.5, 1e-10, 710, 709, -745, 20, 22, 0.999, 1.0000001, math.Inf(1),
		math.Inf(-1), math.NaN(), 0.6744, 0.4375, 2.4375, 1.1875, 0.975, 5.5e-17, 1 << 53, 1 << 19 * math.Pi,
		6.123233995736766e-17, 1.5707963267948966, 3.141592653589793, 4.71238898038469, 1e16, 1e-8,
	}
	for i := 0; i < 3000; i++ {
		switch i % 8 {
		case 0:
			arguments = append(arguments, random.Float64()*2-1)
		case 1:
			arguments = append(arguments, random.Float64()*20-10)
		case 2:
			arguments = append(arguments, math.Ldexp(random.Float64(), -random.IntN(60)))
		case 3:
			arguments = append(arguments, math.Ldexp(random.Float64(), random.IntN(1000)))
		case 4:
			arguments = append(arguments, -math.Ldexp(random.Float64(), random.IntN(30)))
		case 5:
			arguments = append(arguments, float64(random.IntN(2000)-1000)/7)
		case 6:
			arguments = append(arguments, 1+random.Float64()*1e-3)
		case 7:
			arguments = append(arguments, math.Ldexp(random.Float64(), -1022-random.IntN(52)))
		}
	}
	return arguments
}

var mathUnary = []string{
	"abs", "acos", "acosh", "asin", "asinh", "atan", "atanh", "ceil", "cbrt", "expm1", "clz32", "cos", "cosh",
	"exp", "floor", "fround", "log", "log1p", "log2", "log10", "round", "sign", "sin", "sinh", "sqrt", "tan",
	"tanh", "trunc",
}

// TestMathWithNode holds every function of Math to node's bits: each unary one over mathArguments, the
// binary ones (pow, atan2, imul, hypot) over pairs of them. A difference in the last bit is a failure.
func TestMathWithNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	arguments := mathArguments()
	bits := make([]string, len(arguments))
	for i, x := range arguments {
		bits[i] = strconv.FormatUint(math.Float64bits(x), 16)
	}
	// the second argument of the i-th binary call; pow takes a smaller base and exponent too, where its
	// result is neither 0 nor infinite
	second := func(i int) float64 { return arguments[(i*7+3)%len(arguments)] }
	encoded, _ := json.Marshal(map[string]any{"bits": bits, "unary": mathUnary})
	command := exec.Command("node", "-e", `
const input = JSON.parse(require("fs").readFileSync(0, "utf8"));
const view = new DataView(new ArrayBuffer(8));
const number = (hex) => { view.setBigUint64(0, BigInt("0x" + hex)); return view.getFloat64(0); };
const bits = (x) => { view.setFloat64(0, x); return view.getBigUint64(0).toString(16); };
const xs = input.bits.map(number);
const second = (i) => xs[(i * 7 + 3) % xs.length];
const out = {};
for (const name of input.unary) out[name] = xs.map((x) => bits(Math[name](x)));
out.atan2 = xs.map((x, i) => bits(Math.atan2(x, second(i))));
out.imul = xs.map((x, i) => bits(Math.imul(x * 1e9, second(i) * 1e9)));
out.hypot = xs.map((x, i) => bits(Math.hypot(x, second(i), i % 3 ? 1 : x)));
out.pow = xs.map((x, i) => bits(Math.pow(x, second(i))));
out.powSmall = xs.map((x, i) => bits(Math.pow(Math.abs(x) % 100, second(i) % 50)));
out.powInteger = xs.map((x, i) => bits(Math.pow(x, (i % 21) - 10)));
process.stdout.write(JSON.stringify(out));`)
	command.Stdin = bytes.NewReader(encoded) // too long for a command line
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected map[string][]string
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("node output: %v", err)
	}
	number := func(x float64) value.Value { return value.Number(x) }
	calls := map[string]func(i int, x float64) value.Value{
		"atan2": func(i int, x float64) value.Value { return call("Math", "atan2", number(x), number(second(i))) },
		"imul": func(i int, x float64) value.Value {
			return call("Math", "imul", number(x*1e9), number(second(i)*1e9))
		},
		"hypot": func(i int, x float64) value.Value {
			third := x
			if i%3 != 0 {
				third = 1
			}
			return call("Math", "hypot", number(x), number(second(i)), number(third))
		},
		"pow": func(i int, x float64) value.Value { return call("Math", "pow", number(x), number(second(i))) },
		"powSmall": func(i int, x float64) value.Value {
			return call("Math", "pow", number(math.Mod(math.Abs(x), 100)), number(math.Mod(second(i), 50)))
		},
		"powInteger": func(i int, x float64) value.Value { return call("Math", "pow", number(x), number(float64(i%21-10))) },
	}
	for _, name := range mathUnary {
		calls[name] = func(_ int, x float64) value.Value { return call("Math", name, number(x)) }
	}
	compared := 0
	for name, results := range expected {
		f, failed := calls[name], 0
		for i, x := range arguments {
			got := strconv.FormatUint(math.Float64bits(f(i, x).Number()), 16)
			if got != results[i] && !(isNaNBits(got) && isNaNBits(results[i])) {
				if failed < 5 {
					want, _ := strconv.ParseUint(results[i], 16, 64)
					t.Errorf("Math.%s, argument %d (%v): node %v, kitwork %v", name, i, x, math.Float64frombits(want), f(i, x).Number())
				}
				failed++
			}
		}
		if failed > 0 {
			t.Errorf("Math.%s: %d of %d differ from node", name, failed, len(arguments))
		}
		compared += len(arguments)
	}
	if len(expected) != len(calls) {
		t.Errorf("node answered %d functions, the test calls %d", len(expected), len(calls))
	}
	t.Logf("%d results of %d functions compared bit for bit with node", compared, len(expected))
}

func isNaNBits(hex string) bool {
	n, _ := strconv.ParseUint(hex, 16, 64)
	x := math.Float64frombits(n)
	return x != x
}
