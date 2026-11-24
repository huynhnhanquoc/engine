package builtins

import (
	"math"

	"github.com/kitwork/engine2/core/value"
)

// Number(x) is ToNumber, with the statics of JavaScript's Number. Number() alone makes its own parseInt
// and parseFloat; Install hands it the globals' (number), so that Number.parseInt === parseInt.
func Number() value.Value { return number(ParseInt(), ParseFloat()) }

func number(parseInt, parseFloat value.Value) value.Value {
	f := native("Number", 1, func(arguments ...value.Value) value.Value {
		if len(arguments) == 0 {
			return value.Number(0)
		}
		return arguments[0].ToNumber()
	})
	// the statics test a number without making one of their argument, unlike the globals
	f.Set("isFinite", native("isFinite", 1, func(arguments ...value.Value) value.Value {
		a := argument(arguments, 0)
		return value.Boolean(a.IsNumber() && !math.IsNaN(a.Number()) && !math.IsInf(a.Number(), 0))
	}))
	f.Set("isInteger", native("isInteger", 1, func(arguments ...value.Value) value.Value {
		a := argument(arguments, 0)
		return value.Boolean(a.IsNumber() && !math.IsInf(a.Number(), 0) && math.Trunc(a.Number()) == a.Number())
	}))
	f.Set("isNaN", native("isNaN", 1, func(arguments ...value.Value) value.Value {
		a := argument(arguments, 0)
		return value.Boolean(a.IsNumber() && math.IsNaN(a.Number()))
	}))
	f.Set("isSafeInteger", native("isSafeInteger", 1, func(arguments ...value.Value) value.Value {
		a := argument(arguments, 0)
		n := a.Number()
		return value.Boolean(a.IsNumber() && math.Trunc(n) == n && math.Abs(n) <= 9007199254740991)
	}))
	f.Set("parseFloat", parseFloat)
	f.Set("parseInt", parseInt)
	// the constants in the order node lists them; a slice, never a map, so the order is the same on
	// every run
	for _, constant := range []struct {
		name   string
		number float64
	}{
		{"MAX_VALUE", math.MaxFloat64},
		{"MIN_VALUE", 5e-324},
		{"NaN", math.NaN()},
		{"NEGATIVE_INFINITY", math.Inf(-1)},
		{"POSITIVE_INFINITY", math.Inf(1)},
		{"MAX_SAFE_INTEGER", 9007199254740991},
		{"MIN_SAFE_INTEGER", -9007199254740991},
		{"EPSILON", 2.220446049250313e-16},
	} {
		f.Set(constant.name, value.Number(constant.number))
	}
	return f
}

// ParseFloat is parseFloat(text): the longest decimal number the text begins with; "" and nothing
// are NaN.
func ParseFloat() value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if len(arguments) == 0 {
			return value.Number(math.NaN())
		}
		if convertsWithCode(arguments, 1) {
			return convertedThen("t", call, arguments)
		}
		return value.Number(value.ParseFloat(arguments[0].ToString().Text()))
	}
	return native("parseFloat", 1, call)
}

// ParseInt is parseInt(text, radix): the digits the text begins with, in the radix's base; "12px"
// is 12, "0x1f" 31, ("ff", 16) 255; no digit is NaN.
func ParseInt() value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if len(arguments) == 0 {
			return value.Number(math.NaN())
		}
		// the text first, then the radix, as JavaScript makes them; the failure of either is the result
		if convertsWithCode(arguments, 2) {
			return convertedThen("tn", call, arguments)
		}
		radix := 0.0
		if r := argument(arguments, 1); !r.IsUndefined() {
			radix = r.ToNumber().Number()
		}
		return value.ParseIntValue(arguments[0].ToString().Text(), radix)
	}
	return native("parseInt", 2, call)
}

// IsNaN is the global isNaN: its argument made a number first, so isNaN("abc") is true.
func IsNaN() value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if convertsWithCode(arguments, 1) {
			return convertedThen("n", call, arguments)
		}
		return value.Boolean(math.IsNaN(argument(arguments, 0).ToNumber().Number()))
	}
	return native("isNaN", 1, call)
}

// IsFinite is the global isFinite: its argument made a number, neither NaN nor an infinity.
func IsFinite() value.Value {
	var call func(arguments ...value.Value) value.Value
	call = func(arguments ...value.Value) value.Value {
		if convertsWithCode(arguments, 1) {
			return convertedThen("n", call, arguments)
		}
		n := argument(arguments, 0).ToNumber().Number()
		return value.Boolean(!math.IsNaN(n) && !math.IsInf(n, 0))
	}
	return native("isFinite", 1, call)
}
