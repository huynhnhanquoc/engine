// Package builtins_test checks core/builtins from outside: each case is a JavaScript expression node
// runs and the same expression written as calls on the installed globals.
package builtins_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

var globals = func() map[string]value.Value {
	g := map[string]value.Value{}
	builtins.Install(g)
	return g
}()

var (
	num = value.Number
	str = value.String
	arr = func(items ...value.Value) value.Value { return value.Array(items) }
)

// call is name.method(arguments…) on a global, or name(arguments…) when method is "".
func call(name, method string, arguments ...value.Value) value.Value {
	if method == "" {
		return globals[name].Call(arguments)
	}
	return globals[name].Invoke(method, arguments)
}

func get(name, property string) value.Value { return globals[name].Get(property) }

type parity struct {
	js  string
	got func() value.Value
}

var cases = []parity{
	// Math
	{`Math.round(2.5)`, func() value.Value { return call("Math", "round", num(2.5)) }},
	{`Math.round(-2.5)`, func() value.Value { return call("Math", "round", num(-2.5)) }},
	{`Math.round(0.49999999999999994)`, func() value.Value { return call("Math", "round", num(0.49999999999999994)) }},
	{`1 / Math.round(-0.4)`, func() value.Value { return num(1).Divide(call("Math", "round", num(-0.4))) }},
	{`Math.round("2.5")`, func() value.Value { return call("Math", "round", str("2.5")) }},
	{`Math.floor(-1.5)`, func() value.Value { return call("Math", "floor", num(-1.5)) }},
	{`Math.ceil(1.1)`, func() value.Value { return call("Math", "ceil", num(1.1)) }},
	{`Math.trunc(-1.9)`, func() value.Value { return call("Math", "trunc", num(-1.9)) }},
	{`Math.abs("-3")`, func() value.Value { return call("Math", "abs", str("-3")) }},
	{`1 / Math.sign(-0)`, func() value.Value { return num(1).Divide(call("Math", "sign", num(math.Copysign(0, -1)))) }},
	{`Math.sign(NaN)`, func() value.Value { return call("Math", "sign", num(math.NaN())) }},
	{`Math.max()`, func() value.Value { return call("Math", "max") }},
	{`Math.min()`, func() value.Value { return call("Math", "min") }},
	{`Math.max(1, 3, 2)`, func() value.Value { return call("Math", "max", num(1), num(3), num(2)) }},
	{`Math.max(1, NaN, 3)`, func() value.Value { return call("Math", "max", num(1), num(math.NaN()), num(3)) }},
	{`1 / Math.min(0, -0)`, func() value.Value { return num(1).Divide(call("Math", "min", num(0), num(math.Copysign(0, -1)))) }},
	{`1 / Math.max(-0, 0)`, func() value.Value { return num(1).Divide(call("Math", "max", num(math.Copysign(0, -1)), num(0))) }},
	{`Math.min([4], "2")`, func() value.Value { return call("Math", "min", arr(num(4)), str("2")) }},
	{`Math.pow(2, 10)`, func() value.Value { return call("Math", "pow", num(2), num(10)) }},
	{`Math.pow(1, Infinity)`, func() value.Value { return call("Math", "pow", num(1), num(math.Inf(1))) }},
	{`Math.sqrt(-1)`, func() value.Value { return call("Math", "sqrt", num(-1)) }},
	{`Math.hypot(3, 4)`, func() value.Value { return call("Math", "hypot", num(3), num(4)) }},
	{`Math.PI`, func() value.Value { return get("Math", "PI") }},
	{`Math.SQRT1_2`, func() value.Value { return get("Math", "SQRT1_2") }},
	{`typeof Math.random()`, func() value.Value { return call("Math", "random").Typeof() }},

	// Number, parseInt, parseFloat, isNaN, isFinite
	{`Number()`, func() value.Value { return call("Number", "") }},
	{`Number("12px")`, func() value.Value { return call("Number", "", str("12px")) }},
	{`Number(" 0x1f ")`, func() value.Value { return call("Number", "", str(" 0x1f ")) }},
	{`Number(null)`, func() value.Value { return call("Number", "", value.Null()) }},
	{`Number.MAX_SAFE_INTEGER`, func() value.Value { return get("Number", "MAX_SAFE_INTEGER") }},
	{`Number.EPSILON`, func() value.Value { return get("Number", "EPSILON") }},
	{`Number.MIN_VALUE`, func() value.Value { return get("Number", "MIN_VALUE") }},
	{`Number.isNaN("abc")`, func() value.Value { return call("Number", "isNaN", str("abc")) }},
	{`Number.isNaN(NaN)`, func() value.Value { return call("Number", "isNaN", num(math.NaN())) }},
	{`Number.isInteger(5.0)`, func() value.Value { return call("Number", "isInteger", num(5)) }},
	{`Number.isInteger(1e300)`, func() value.Value { return call("Number", "isInteger", num(1e300)) }},
	{`Number.isInteger("5")`, func() value.Value { return call("Number", "isInteger", str("5")) }},
	{`Number.isSafeInteger(2 ** 53)`, func() value.Value { return call("Number", "isSafeInteger", num(1<<53)) }},
	{`Number.isFinite("12")`, func() value.Value { return call("Number", "isFinite", str("12")) }},
	{`Number.parseInt("42abc")`, func() value.Value { return call("Number", "parseInt", str("42abc")) }},
	{`parseInt("12px")`, func() value.Value { return call("parseInt", "", str("12px")) }},
	{`parseInt("0x1f")`, func() value.Value { return call("parseInt", "", str("0x1f")) }},
	{`parseInt("ff", 16)`, func() value.Value { return call("parseInt", "", str("ff"), num(16)) }},
	{`parseInt("7", 37)`, func() value.Value { return call("parseInt", "", str("7"), num(37)) }},
	{`1 / parseInt("-0")`, func() value.Value { return num(1).Divide(call("parseInt", "", str("-0"))) }},
	{`parseInt("99999999999999999999")`, func() value.Value { return call("parseInt", "", str("99999999999999999999")) }},
	{`parseInt(0.0000005)`, func() value.Value { return call("parseInt", "", num(0.0000005)) }},
	{`parseInt("")`, func() value.Value { return call("parseInt", "", str("")) }},
	{`parseInt()`, func() value.Value { return call("parseInt", "") }},
	{`parseInt("101", 2)`, func() value.Value { return call("parseInt", "", str("101"), num(2)) }},
	{`parseFloat("3.5px")`, func() value.Value { return call("parseFloat", "", str("3.5px")) }},
	{`parseFloat(".5")`, func() value.Value { return call("parseFloat", "", str(".5")) }},
	{`parseFloat("-Infinityx")`, func() value.Value { return call("parseFloat", "", str("-Infinityx")) }},
	{`parseFloat("1e400")`, func() value.Value { return call("parseFloat", "", str("1e400")) }},
	{`parseFloat("abc")`, func() value.Value { return call("parseFloat", "", str("abc")) }},
	{`isNaN("abc")`, func() value.Value { return call("isNaN", "", str("abc")) }},
	{`isNaN("")`, func() value.Value { return call("isNaN", "", str("")) }},
	{`isNaN(undefined)`, func() value.Value { return call("isNaN", "", value.Undefined()) }},
	{`isFinite("12")`, func() value.Value { return call("isFinite", "", str("12")) }},
	{`isFinite(Infinity)`, func() value.Value { return call("isFinite", "", globals["Infinity"]) }},
	{`NaN`, func() value.Value { return globals["NaN"] }},
	{`-Infinity`, func() value.Value { return globals["Infinity"].Negate() }},

	// String, Boolean, Array, Object
	{`String()`, func() value.Value { return call("String", "") }},
	{`String(null)`, func() value.Value { return call("String", "", value.Null()) }},
	{`String([1, [2, 3]])`, func() value.Value { return call("String", "", arr(num(1), arr(num(2), num(3)))) }},
	{`String.fromCharCode(72, 105)`, func() value.Value { return call("String", "fromCharCode", num(72), num(105)) }},
	{`String.fromCharCode(55357, 56832).length`, func() value.Value { return call("String", "fromCharCode", num(55357), num(56832)).Get("length") }},
	{`String.fromCharCode(55357, 56832) === "😀"`, func() value.Value {
		return value.Boolean(call("String", "fromCharCode", num(55357), num(56832)).Equal(str("😀")))
	}},
	{`String.fromCharCode(65 + 65536)`, func() value.Value { return call("String", "fromCharCode", num(65+65536)) }},
	{`Boolean("")`, func() value.Value { return call("Boolean", "", str("")) }},
	{`Boolean("0")`, func() value.Value { return call("Boolean", "", str("0")) }},
	{`Boolean()`, func() value.Value { return call("Boolean", "") }},
	{`Array(3).length`, func() value.Value { return call("Array", "", num(3)).Get("length") }},
	{`Array(3).join("-")`, func() value.Value { return call("Array", "", num(3)).Join(str("-")) }},
	{`Array(1, 2).join()`, func() value.Value { return call("Array", "", num(1), num(2)).Join() }},
	{`Array("3").length`, func() value.Value { return call("Array", "", str("3")).Get("length") }},
	{`Array(-1)`, func() value.Value { return call("Array", "", num(-1)) }},
	{`Array(1.5)`, func() value.Value { return call("Array", "", num(1.5)) }},
	{`Array.isArray([])`, func() value.Value { return call("Array", "isArray", arr()) }},
	{`Array.isArray("a")`, func() value.Value { return call("Array", "isArray", str("a")) }},
	{`Object.keys({b: 1, 2: 0, a: 2}).join()`, func() value.Value {
		return call("Object", "keys", value.Object().Set("b", num(1)).Set("2", num(0)).Set("a", num(2))).Join()
	}},
	{`Object.values({b: 1, a: 2}).join()`, func() value.Value {
		return call("Object", "values", value.Object().Set("b", num(1)).Set("a", num(2))).Join()
	}},
	{`Object.entries({b: 1, a: 2}).join("|")`, func() value.Value {
		return call("Object", "entries", value.Object().Set("b", num(1)).Set("a", num(2))).Join(str("|"))
	}},
	{`Object.keys("ab").join()`, func() value.Value { return call("Object", "keys", str("ab")).Join() }},
	{`Object.keys(["x", "y"]).join()`, func() value.Value { return call("Object", "keys", arr(str("x"), str("y"))).Join() }},
	{`Object.values(["x", "y"]).join()`, func() value.Value { return call("Object", "values", arr(str("x"), str("y"))).Join() }},
	{`Object.entries("ab").join("|")`, func() value.Value { return call("Object", "entries", str("ab")).Join(str("|")) }},
	{`Object.keys(5).length`, func() value.Value { return call("Object", "keys", num(5)).Get("length") }},
	{`Object.keys(null)`, func() value.Value { return call("Object", "keys", value.Null()) }},
	{`JSON.stringify(Object.assign({a: 1}, {b: 2}, {a: 3}))`, func() value.Value {
		return value.Stringify(call("Object", "assign", value.Object().Set("a", num(1)), value.Object().Set("b", num(2)), value.Object().Set("a", num(3))), "")
	}},
	{`JSON.stringify(Object.fromEntries([["a", 1], ["b", [2]]]))`, func() value.Value {
		return value.Stringify(call("Object", "fromEntries", arr(arr(str("a"), num(1)), arr(str("b"), arr(num(2))))), "")
	}},
	{`JSON.stringify(Object())`, func() value.Value { return value.Stringify(call("Object", ""), "") }},

	// JSON
	{`JSON.stringify({a: [1, {b: 2}]}, null, 2)`, func() value.Value {
		return call("JSON", "stringify", value.Object().Set("a", arr(num(1), value.Object().Set("b", num(2)))), value.Null(), num(2))
	}},
	{`JSON.stringify([1, 2], null, "--")`, func() value.Value { return call("JSON", "stringify", arr(num(1), num(2)), value.Null(), str("--")) }},
	{`JSON.stringify([1], null, 20)`, func() value.Value { return call("JSON", "stringify", arr(num(1)), value.Null(), num(20)) }},
	{`JSON.stringify(undefined)`, func() value.Value { return call("JSON", "stringify", value.Undefined()) }},
	{`JSON.stringify([1, 2], (k, v) => v)`, func() value.Value {
		return call("JSON", "stringify", arr(num(1), num(2)), value.Function(func(a ...value.Value) value.Value { return a[1] }))
	}},
	{`JSON.stringify({a: 1, b: [2, 3], c: "x"}, (k, v) => typeof v === "number" ? v * 10 : v)`, func() value.Value {
		times10 := value.Function(func(a ...value.Value) value.Value {
			if a[1].IsNumber() {
				return a[1].Multiply(num(10))
			}
			return a[1]
		})
		return call("JSON", "stringify", value.Object().Set("a", num(1)).Set("b", arr(num(2), num(3))).Set("c", str("x")), times10)
	}},
	{`JSON.stringify({a: 1, b: 2, c: [1]}, (k, v) => k === "b" ? undefined : v)`, func() value.Value {
		dropB := value.Function(func(a ...value.Value) value.Value {
			if a[0].Text() == "b" {
				return value.Undefined()
			}
			return a[1]
		})
		return call("JSON", "stringify", value.Object().Set("a", num(1)).Set("b", num(2)).Set("c", arr(num(1))), dropB)
	}},
	{`JSON.stringify([1, 2], (k, v) => k === "0" ? undefined : v)`, func() value.Value {
		dropFirst := value.Function(func(a ...value.Value) value.Value {
			if a[0].Text() == "0" {
				return value.Undefined()
			}
			return a[1]
		})
		return call("JSON", "stringify", arr(num(1), num(2)), dropFirst)
	}},
	{`JSON.stringify({a: 1}, (k, v) => k === "" ? [v, v] : v)`, func() value.Value {
		wrap := value.Function(func(a ...value.Value) value.Value {
			if a[0].Text() == "" {
				return arr(a[1], a[1])
			}
			return a[1]
		})
		return call("JSON", "stringify", value.Object().Set("a", num(1)), wrap)
	}},
	{`JSON.stringify({c: 3, a: 1, b: {a: 5, z: 6}}, ["b", "a", 1, "b"])`, func() value.Value {
		o := value.Object().Set("c", num(3)).Set("a", num(1)).Set("b", value.Object().Set("a", num(5)).Set("z", num(6)))
		return call("JSON", "stringify", o, arr(str("b"), str("a"), num(1), str("b")))
	}},
	{`JSON.stringify([{a: 1, b: 2}], ["a"])`, func() value.Value {
		return call("JSON", "stringify", arr(value.Object().Set("a", num(1)).Set("b", num(2))), arr(str("a")))
	}},
	{`JSON.stringify({a: 1}, [], 2)`, func() value.Value { return call("JSON", "stringify", value.Object().Set("a", num(1)), arr(), num(2)) }},
	{`JSON.parse("[1, 2]").length`, func() value.Value { return call("JSON", "parse", str("[1, 2]")).Get("length") }},
	{`JSON.parse("{bad")`, func() value.Value { return call("JSON", "parse", str("{bad")) }},
	{`JSON.parse(5)`, func() value.Value { return call("JSON", "parse", num(5)) }},

	// Date: a Time value, not an object; only what both sides agree on is compared here
	{`new Date(0).toISOString()`, func() value.Value { return call("Date", "", num(0)).Invoke("toISOString", nil) }},
	{`new Date(1730000000123).getTime()`, func() value.Value { return call("Date", "", num(1730000000123)).Invoke("getTime", nil) }},
	{`new Date("2026-11-03").toISOString()`, func() value.Value { return call("Date", "", str("2026-11-03")).Invoke("toISOString", nil) }},
	{`new Date("2026-11-03T09:30:00Z").getTime()`, func() value.Value { return call("Date", "", str("2026-11-03T09:30:00Z")).Invoke("getTime", nil) }},
	{`new Date("2026-11-03T09:30:00.250+07:00").toISOString()`, func() value.Value {
		return call("Date", "", str("2026-11-03T09:30:00.250+07:00")).Invoke("toISOString", nil)
	}},
	{`Date.UTC(2026, 10, 3)`, func() value.Value { return call("Date", "UTC", num(2026), num(10), num(3)) }},
	{`Date.UTC(2026, 12, 1)`, func() value.Value { return call("Date", "UTC", num(2026), num(12), num(1)) }},
	{`Date.UTC(99, 0)`, func() value.Value { return call("Date", "UTC", num(99), num(0)) }},
	{`Date.UTC(NaN)`, func() value.Value { return call("Date", "UTC", num(math.NaN())) }},
	{`Date.parse("2026-11-03")`, func() value.Value { return call("Date", "parse", str("2026-11-03")) }},
	{`Date.parse("not a date")`, func() value.Value { return call("Date", "parse", str("not a date")) }},
	{`typeof Date.now()`, func() value.Value { return call("Date", "now").Typeof() }},
	{`Date.parse("2024")`, func() value.Value { return call("Date", "parse", str("2024")) }},
	{`Date.parse("2024-01")`, func() value.Value { return call("Date", "parse", str("2024-01")) }},
	{`Date.parse("2024Z")`, func() value.Value { return call("Date", "parse", str("2024Z")) }},
	{`Date.parse("+002024-01-02")`, func() value.Value { return call("Date", "parse", str("+002024-01-02")) }},
	{`Date.parse("2024-02-30")`, func() value.Value { return call("Date", "parse", str("2024-02-30")) }},
	{`Date.parse("2024-01-02T24:00Z")`, func() value.Value { return call("Date", "parse", str("2024-01-02T24:00Z")) }},
	{`Date.parse("2024-01-02T03:04:05Z ")`, func() value.Value { return call("Date", "parse", str("2024-01-02T03:04:05Z ")) }},
	{`Date.parse("Tue, 02 Jan 2024 03:04:05 GMT")`, func() value.Value { return call("Date", "parse", str("Tue, 02 Jan 2024 03:04:05 GMT")) }},
	{`new Date("2024").toISOString()`, func() value.Value { return call("Date", "", str("2024")).Invoke("toISOString", nil) }},
	{`new Date("2024-01").getTime()`, func() value.Value { return call("Date", "", str("2024-01")).Invoke("getTime", nil) }},
	{`new Date("2024-01-02T03:04:05.123456Z").toISOString()`, func() value.Value {
		return call("Date", "", str("2024-01-02T03:04:05.123456Z")).Invoke("toISOString", nil)
	}},
	{`new Date(["2024"]).toISOString()`, func() value.Value { return call("Date", "", arr(str("2024"))).Invoke("toISOString", nil) }},
	{`Date.UTC()`, func() value.Value { return call("Date", "UTC") }},
	{`Date.UTC(0.5)`, func() value.Value { return call("Date", "UTC", num(0.5)) }},
	{`Date.UTC(2024.7, 1.9)`, func() value.Value { return call("Date", "UTC", num(2024.7), num(1.9)) }},
	{`Date.UTC(2024, 13, 40, 25, 61, 61, 1001)`, func() value.Value {
		return call("Date", "UTC", num(2024), num(13), num(40), num(25), num(61), num(61), num(1001))
	}},
	{`Date.UTC(-1)`, func() value.Value { return call("Date", "UTC", num(-1)) }},
	{`Date.UTC(275760, 8, 13)`, func() value.Value { return call("Date", "UTC", num(275760), num(8), num(13)) }},
	{`Date.UTC(275760, 8, 14)`, func() value.Value { return call("Date", "UTC", num(275760), num(8), num(14)) }},
	{`Date.UTC(1e7)`, func() value.Value { return call("Date", "UTC", num(1e7)) }},
	{`Date.UTC(2024, 1e20)`, func() value.Value { return call("Date", "UTC", num(2024), num(1e20)) }},
	{`Date.UTC(1970, 0, 1, 0, 0, 0, -1)`, func() value.Value {
		return call("Date", "UTC", num(1970), num(0), num(1), num(0), num(0), num(0), num(-1))
	}},
	{`Date.UTC("2024", "1")`, func() value.Value { return call("Date", "UTC", str("2024"), str("1")) }},
	{`Date.length`, func() value.Value { return get("Date", "length") }},

	// Math: the functions added, each to the last bit (TestMathWithNode holds them over many more)
	{`Math.sinh(0.5)`, func() value.Value { return call("Math", "sinh", num(0.5)) }},
	{`Math.cosh(2)`, func() value.Value { return call("Math", "cosh", num(2)) }},
	{`Math.tanh("7")`, func() value.Value { return call("Math", "tanh", str("7")) }},
	{`Math.tanh(-Infinity)`, func() value.Value { return call("Math", "tanh", num(math.Inf(-1))) }},
	{`Math.asinh(2024)`, func() value.Value { return call("Math", "asinh", num(2024)) }},
	{`Math.acosh(0.5)`, func() value.Value { return call("Math", "acosh", num(0.5)) }},
	{`Math.acosh(65)`, func() value.Value { return call("Math", "acosh", num(65)) }},
	{`Math.atanh(1)`, func() value.Value { return call("Math", "atanh", num(1)) }},
	{`Math.atanh(0.5)`, func() value.Value { return call("Math", "atanh", num(0.5)) }},
	{`Math.expm1(1e-10)`, func() value.Value { return call("Math", "expm1", num(1e-10)) }},
	{`Math.log1p(-1)`, func() value.Value { return call("Math", "log1p", num(-1)) }},
	{`Math.log1p(1e-10)`, func() value.Value { return call("Math", "log1p", num(1e-10)) }},
	{`Math.fround(5.5)`, func() value.Value { return call("Math", "fround", num(5.5)) }},
	{`Math.fround(5.05)`, func() value.Value { return call("Math", "fround", num(5.05)) }},
	{`Math.fround(2 ** 128)`, func() value.Value { return call("Math", "fround", num(math.Ldexp(1, 128))) }},
	{`Math.imul(3, 4)`, func() value.Value { return call("Math", "imul", num(3), num(4)) }},
	{`Math.imul(0xffffffff, 5)`, func() value.Value { return call("Math", "imul", num(0xffffffff), num(5)) }},
	{`Math.imul(2 ** 31, 2)`, func() value.Value { return call("Math", "imul", num(1<<31), num(2)) }},
	{`Math.clz32(1)`, func() value.Value { return call("Math", "clz32", num(1)) }},
	{`Math.clz32(0)`, func() value.Value { return call("Math", "clz32", num(0)) }},
	{`Math.clz32(-1)`, func() value.Value { return call("Math", "clz32", num(-1)) }},
	{`Math.clz32(2 ** 32 + 8)`, func() value.Value { return call("Math", "clz32", num(1<<32+8)) }},
	{`Math.hypot()`, func() value.Value { return call("Math", "hypot") }},
	{`Math.hypot(-3)`, func() value.Value { return call("Math", "hypot", num(-3)) }},
	{`Math.hypot("7")`, func() value.Value { return call("Math", "hypot", str("7")) }},
	{`Math.hypot(2024, 0, 2)`, func() value.Value { return call("Math", "hypot", num(2024), num(0), num(2)) }},
	{`Math.hypot(NaN, Infinity)`, func() value.Value { return call("Math", "hypot", num(math.NaN()), num(math.Inf(1))) }},
	{`Math.hypot(NaN, 1)`, func() value.Value { return call("Math", "hypot", num(math.NaN()), num(1)) }},
	{`Math.hypot(1e200, 1e200)`, func() value.Value { return call("Math", "hypot", num(1e200), num(1e200)) }},
	{`1 / Math.hypot(-0)`, func() value.Value { return num(1).Divide(call("Math", "hypot", num(math.Copysign(0, -1)))) }},
	{`Math.sin(2)`, func() value.Value { return call("Math", "sin", num(2)) }},
	{`Math.tan(Math.PI / 2)`, func() value.Value { return call("Math", "tan", num(math.Pi/2)) }},
	{`Math.sin(1e22)`, func() value.Value { return call("Math", "sin", num(1e22)) }},
	{`Math.acos(0.5)`, func() value.Value { return call("Math", "acos", num(0.5)) }},
	{`Math.cbrt(0.5)`, func() value.Value { return call("Math", "cbrt", num(0.5)) }},
	{`Math.log2(3)`, func() value.Value { return call("Math", "log2", num(3)) }},
	{`Math.log10(2024)`, func() value.Value { return call("Math", "log10", num(2024)) }},
	{`Math.pow(10, 308)`, func() value.Value { return call("Math", "pow", num(10), num(308)) }},
	{`Math.pow(65, 66)`, func() value.Value { return call("Math", "pow", num(65), num(66)) }},
	{`Math.pow(-8, 1 / 3)`, func() value.Value { return call("Math", "pow", num(-8), num(1.0/3)) }},
	{`Math.exp(1)`, func() value.Value { return call("Math", "exp", num(1)) }},
	{`Object.getOwnPropertyNames(Math).join()`, func() value.Value { return call("Object", "getOwnPropertyNames", globals["Math"]).Join() }},
	{`Math.max.name`, func() value.Value { return get("Math", "max").Get("name") }},
	{`Math.max.length`, func() value.Value { return get("Math", "max").Get("length") }},
	{`Object.getOwnPropertyNames(Math.max).join()`, func() value.Value { return call("Object", "getOwnPropertyNames", get("Math", "max")).Join() }},

	// Number and the globals: one value wherever it is reached
	{`Number.parseInt === parseInt`, func() value.Value { return value.Boolean(get("Number", "parseInt").Equal(globals["parseInt"])) }},
	{`Number.parseFloat === parseFloat`, func() value.Value { return value.Boolean(get("Number", "parseFloat").Equal(globals["parseFloat"])) }},
	{`parseInt.length`, func() value.Value { return globals["parseInt"].Get("length") }},
	{`isNaN.name`, func() value.Value { return globals["isNaN"].Get("name") }},
	{`String.name`, func() value.Value { return get("String", "name") }},
	{`Array.from.length`, func() value.Value { return get("Array", "from").Get("length") }},
	{`Object.keys.name`, func() value.Value { return get("Object", "keys").Get("name") }},

	// Array.from and Array.of
	{`JSON.stringify(Array.from([1, 2]))`, func() value.Value { return value.Stringify(call("Array", "from", arr(num(1), num(2))), "") }},
	{`Array.from("😀x").length`, func() value.Value { return call("Array", "from", str("😀x")).Get("length") }},
	{`Array.from("😀x")[0] === "😀"`, func() value.Value {
		return value.Boolean(call("Array", "from", str("😀x")).Index(0).Equal(str("😀")))
	}},
	{`Array.from("2024").join("|")`, func() value.Value { return call("Array", "from", str("2024")).Join(str("|")) }},
	{`JSON.stringify(Array.from({ length: 2 }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", num(2))), "")
	}},
	{`JSON.stringify(Array.from({ length: 3, 0: "a", 2: "c" }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", num(3)).Set("0", str("a")).Set("2", str("c"))), "")
	}},
	{`JSON.stringify(Array.from({ length: "2" }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", str("2"))), "")
	}},
	{`JSON.stringify(Array.from({ length: -1 }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", num(-1))), "")
	}},
	{`JSON.stringify(Array.from({ length: 2.9 }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", num(2.9))), "")
	}},
	{`Array.from({ length: 2 ** 32 })`, func() value.Value { return call("Array", "from", value.Object().Set("length", num(1<<32))) }},
	{`JSON.stringify(Array.from([1, 2], (x, i) => x * 10 + i))`, func() value.Value {
		return value.Stringify(call("Array", "from", arr(num(1), num(2)), value.Function(func(a ...value.Value) value.Value {
			return a[0].Multiply(num(10)).Add(a[1])
		})), "")
	}},
	{`JSON.stringify(Array.from({ length: 3 }, (_, i) => i * i))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("length", num(3)), value.Function(func(a ...value.Value) value.Value {
			return a[1].Multiply(a[1])
		})), "")
	}},
	{`JSON.stringify(Array.from(5))`, func() value.Value { return value.Stringify(call("Array", "from", num(5)), "") }},
	{`JSON.stringify(Array.from(true))`, func() value.Value { return value.Stringify(call("Array", "from", value.Boolean(true)), "") }},
	{`JSON.stringify(Array.from({ a: 1 }))`, func() value.Value {
		return value.Stringify(call("Array", "from", value.Object().Set("a", num(1))), "")
	}},
	{`Array.from(null)`, func() value.Value { return call("Array", "from", value.Null()) }},
	{`Array.from()`, func() value.Value { return call("Array", "from") }},
	{`Array.from([1], 5)`, func() value.Value { return call("Array", "from", arr(num(1)), num(5)) }},
	{`JSON.stringify(Array.of())`, func() value.Value { return value.Stringify(call("Array", "of"), "") }},
	{`JSON.stringify(Array.of(3))`, func() value.Value { return value.Stringify(call("Array", "of", num(3)), "") }},
	{`JSON.stringify(Array.of(1, "a", [2]))`, func() value.Value {
		return value.Stringify(call("Array", "of", num(1), str("a"), arr(num(2))), "")
	}},
	{`Array(2 ** 32)`, func() value.Value { return call("Array", "", num(1<<32)) }},

	// Object.is, hasOwn, getOwnPropertyNames, fromEntries, assign
	{`Object.is(NaN, NaN)`, func() value.Value { return call("Object", "is", num(math.NaN()), num(math.NaN())) }},
	{`Object.is(0, -0)`, func() value.Value { return call("Object", "is", num(0), num(math.Copysign(0, -1))) }},
	{`Object.is(-0, -0)`, func() value.Value { return call("Object", "is", num(math.Copysign(0, -1)), num(math.Copysign(0, -1))) }},
	{`Object.is("a", "a")`, func() value.Value { return call("Object", "is", str("a"), str("a")) }},
	{`Object.is({}, {})`, func() value.Value { return call("Object", "is", value.Object(), value.Object()) }},
	{`Object.is()`, func() value.Value { return call("Object", "is") }},
	{`Object.is(null, undefined)`, func() value.Value { return call("Object", "is", value.Null(), value.Undefined()) }},
	{`Object.is(1, "1")`, func() value.Value { return call("Object", "is", num(1), str("1")) }},
	{`Object.hasOwn({ a: 1 }, "a")`, func() value.Value { return call("Object", "hasOwn", value.Object().Set("a", num(1)), str("a")) }},
	{`Object.hasOwn({ a: 1 }, "b")`, func() value.Value { return call("Object", "hasOwn", value.Object().Set("a", num(1)), str("b")) }},
	{`Object.hasOwn({ 1: 1 }, 1)`, func() value.Value { return call("Object", "hasOwn", value.Object().Set("1", num(1)), num(1)) }},
	{`Object.hasOwn([1, 2], 0)`, func() value.Value { return call("Object", "hasOwn", arr(num(1), num(2)), num(0)) }},
	{`Object.hasOwn([1, 2], 2)`, func() value.Value { return call("Object", "hasOwn", arr(num(1), num(2)), num(2)) }},
	{`Object.hasOwn([1, 2], "length")`, func() value.Value { return call("Object", "hasOwn", arr(num(1), num(2)), str("length")) }},
	{`Object.hasOwn([1, 2], "01")`, func() value.Value { return call("Object", "hasOwn", arr(num(1), num(2)), str("01")) }},
	{`Object.hasOwn("ab", 1)`, func() value.Value { return call("Object", "hasOwn", str("ab"), num(1)) }},
	{`Object.hasOwn("ab", "length")`, func() value.Value { return call("Object", "hasOwn", str("ab"), str("length")) }},
	{`Object.hasOwn(5, "x")`, func() value.Value { return call("Object", "hasOwn", num(5), str("x")) }},
	{`Object.hasOwn(Array, "isArray")`, func() value.Value { return call("Object", "hasOwn", globals["Array"], str("isArray")) }},
	{`Object.hasOwn(null, "a")`, func() value.Value { return call("Object", "hasOwn", value.Null(), str("a")) }},
	{`Object.getOwnPropertyNames({ b: 1, 2: 0, a: 2 }).join()`, func() value.Value {
		return call("Object", "getOwnPropertyNames", value.Object().Set("b", num(1)).Set("2", num(0)).Set("a", num(2))).Join()
	}},
	{`Object.getOwnPropertyNames([1, 2]).join()`, func() value.Value { return call("Object", "getOwnPropertyNames", arr(num(1), num(2))).Join() }},
	{`Object.getOwnPropertyNames("ab").join()`, func() value.Value { return call("Object", "getOwnPropertyNames", str("ab")).Join() }},
	{`Object.getOwnPropertyNames(5).length`, func() value.Value { return call("Object", "getOwnPropertyNames", num(5)).Get("length") }},
	{`Object.getOwnPropertyNames(undefined)`, func() value.Value { return call("Object", "getOwnPropertyNames", value.Undefined()) }},
	{`Object.fromEntries()`, func() value.Value { return call("Object", "fromEntries") }},
	{`Object.fromEntries([1, 2])`, func() value.Value { return call("Object", "fromEntries", arr(num(1), num(2))) }},
	{`Object.fromEntries("7")`, func() value.Value { return call("Object", "fromEntries", str("7")) }},
	{`JSON.stringify(Object.fromEntries(""))`, func() value.Value { return value.Stringify(call("Object", "fromEntries", str("")), "") }},
	{`JSON.stringify(Object.fromEntries([["a", 1], [2, "b"], [[3]]]))`, func() value.Value {
		return value.Stringify(call("Object", "fromEntries", arr(arr(str("a"), num(1)), arr(num(2), str("b")), arr(arr(num(3))))), "")
	}},
	{`JSON.stringify(Object.fromEntries([{ 0: "k", 1: "v" }]))`, func() value.Value {
		return value.Stringify(call("Object", "fromEntries", arr(value.Object().Set("0", str("k")).Set("1", str("v")))), "")
	}},
	{`Object.assign()`, func() value.Value { return call("Object", "assign") }},
	{`Object.assign(null, {})`, func() value.Value { return call("Object", "assign", value.Null(), value.Object()) }},
	{`JSON.stringify(Object.assign({}, "ab", null, [5], undefined, { c: 1 }))`, func() value.Value {
		return value.Stringify(call("Object", "assign", value.Object(), str("ab"), value.Null(), arr(num(5)), value.Undefined(), value.Object().Set("c", num(1))), "")
	}},

	// String.fromCodePoint
	{`String.fromCodePoint()`, func() value.Value { return call("String", "fromCodePoint") }},
	{`String.fromCodePoint(65, 66)`, func() value.Value { return call("String", "fromCodePoint", num(65), num(66)) }},
	{`String.fromCodePoint(128512) === "😀"`, func() value.Value {
		return value.Boolean(call("String", "fromCodePoint", num(128512)).Equal(str("😀")))
	}},
	{`String.fromCodePoint(128512).length`, func() value.Value { return call("String", "fromCodePoint", num(128512)).Get("length") }},
	{`String.fromCodePoint(0xD83D, 0xDE00) === "😀"`, func() value.Value {
		return value.Boolean(call("String", "fromCodePoint", num(0xD83D), num(0xDE00)).Equal(str("😀")))
	}},
	{`String.fromCodePoint("7").charCodeAt(0)`, func() value.Value {
		return call("String", "fromCodePoint", str("7")).Invoke("charCodeAt", []value.Value{num(0)})
	}},
	{`String.fromCodePoint(0x10FFFF).length`, func() value.Value { return call("String", "fromCodePoint", num(0x10FFFF)).Get("length") }},
	{`String.fromCodePoint(0x110000)`, func() value.Value { return call("String", "fromCodePoint", num(0x110000)) }},
	{`String.fromCodePoint(-1)`, func() value.Value { return call("String", "fromCodePoint", num(-1)) }},
	{`String.fromCodePoint(1.5)`, func() value.Value { return call("String", "fromCodePoint", num(1.5)) }},
	{`String.fromCodePoint(NaN)`, func() value.Value { return call("String", "fromCodePoint", num(math.NaN())) }},
	{`String.fromCodePoint(Infinity)`, func() value.Value { return call("String", "fromCodePoint", num(math.Inf(1))) }},
	{`String.fromCodePoint("a")`, func() value.Value { return call("String", "fromCodePoint", str("a")) }},
	{`String.fromCodePoint(-0).length`, func() value.Value { return call("String", "fromCodePoint", num(math.Copysign(0, -1))).Get("length") }},

	// encodeURIComponent, decodeURIComponent, encodeURI, decodeURI
	{`encodeURIComponent("a b&c/d?é😀")`, func() value.Value { return call("encodeURIComponent", "", str("a b&c/d?é😀")) }},
	{`encodeURI("a b&c/d?é😀")`, func() value.Value { return call("encodeURI", "", str("a b&c/d?é😀")) }},
	{`encodeURIComponent("-_.!~*'()")`, func() value.Value { return call("encodeURIComponent", "", str("-_.!~*'()")) }},
	{`encodeURIComponent(";/?:@&=+$,#")`, func() value.Value { return call("encodeURIComponent", "", str(";/?:@&=+$,#")) }},
	{`encodeURI(";/?:@&=+$,#")`, func() value.Value { return call("encodeURI", "", str(";/?:@&=+$,#")) }},
	{`encodeURI("[]{}|^<>%\"")`, func() value.Value { return call("encodeURI", "", str("[]{}|^<>%\"")) }},
	{`encodeURIComponent(null)`, func() value.Value { return call("encodeURIComponent", "", value.Null()) }},
	{`encodeURIComponent()`, func() value.Value { return call("encodeURIComponent", "") }},
	{`encodeURIComponent([1, 2])`, func() value.Value { return call("encodeURIComponent", "", arr(num(1), num(2))) }},
	{`encodeURIComponent(String.fromCharCode(0xD800))`, func() value.Value {
		return call("encodeURIComponent", "", call("String", "fromCharCode", num(0xD800)))
	}},
	{`encodeURI("a" + String.fromCharCode(0xDC00))`, func() value.Value {
		return call("encodeURI", "", str("a").Add(call("String", "fromCharCode", num(0xDC00))))
	}},
	{`encodeURIComponent(String.fromCharCode(0xD83D, 0xDE00))`, func() value.Value {
		return call("encodeURIComponent", "", call("String", "fromCharCode", num(0xD83D), num(0xDE00)))
	}},
	{`decodeURIComponent("%E2%82%AC%20a")`, func() value.Value { return call("decodeURIComponent", "", str("%E2%82%AC%20a")) }},
	{`decodeURIComponent("%e2%82%ac")`, func() value.Value { return call("decodeURIComponent", "", str("%e2%82%ac")) }},
	{`decodeURIComponent("%F0%9F%98%80") === "😀"`, func() value.Value {
		return value.Boolean(call("decodeURIComponent", "", str("%F0%9F%98%80")).Equal(str("😀")))
	}},
	{`decodeURIComponent("%23%3B%2f")`, func() value.Value { return call("decodeURIComponent", "", str("%23%3B%2f")) }},
	{`decodeURI("%23%3B%2f%41%20")`, func() value.Value { return call("decodeURI", "", str("%23%3B%2f%41%20")) }},
	{`decodeURI("%E2%82%AC")`, func() value.Value { return call("decodeURI", "", str("%E2%82%AC")) }},
	{`decodeURIComponent("plain é")`, func() value.Value { return call("decodeURIComponent", "", str("plain é")) }},
	{`decodeURIComponent("%")`, func() value.Value { return call("decodeURIComponent", "", str("%")) }},
	{`decodeURIComponent("%4")`, func() value.Value { return call("decodeURIComponent", "", str("%4")) }},
	{`decodeURIComponent("%G1")`, func() value.Value { return call("decodeURIComponent", "", str("%G1")) }},
	{`decodeURIComponent("%E2%82")`, func() value.Value { return call("decodeURIComponent", "", str("%E2%82")) }},
	{`decodeURIComponent("%E2%82%4")`, func() value.Value { return call("decodeURIComponent", "", str("%E2%82%4")) }},
	{`decodeURIComponent("%E2%41%AC")`, func() value.Value { return call("decodeURIComponent", "", str("%E2%41%AC")) }},
	{`decodeURIComponent("%80")`, func() value.Value { return call("decodeURIComponent", "", str("%80")) }},
	{`decodeURIComponent("%C0%80")`, func() value.Value { return call("decodeURIComponent", "", str("%C0%80")) }},
	{`decodeURIComponent("%ED%A0%80")`, func() value.Value { return call("decodeURIComponent", "", str("%ED%A0%80")) }},
	{`decodeURIComponent("%F4%90%80%80")`, func() value.Value { return call("decodeURIComponent", "", str("%F4%90%80%80")) }},
	{`decodeURIComponent("%FF")`, func() value.Value { return call("decodeURIComponent", "", str("%FF")) }},
	{`decodeURIComponent("%EF%BF%BD").charCodeAt(0)`, func() value.Value {
		return call("decodeURIComponent", "", str("%EF%BF%BD")).Invoke("charCodeAt", []value.Value{num(0)})
	}},
	{`decodeURI("%")`, func() value.Value { return call("decodeURI", "", str("%")) }},
	{`decodeURIComponent(encodeURIComponent("Phường 7, Quận 3"))`, func() value.Value {
		return call("decodeURIComponent", "", call("encodeURIComponent", "", str("Phường 7, Quận 3")))
	}},
	{`encodeURIComponent.length`, func() value.Value { return globals["encodeURIComponent"].Get("length") }},

	// JSON: a replacer that is neither a function nor an array is no replacer; the reviver
	{`JSON.stringify(2024, 0, 2)`, func() value.Value { return call("JSON", "stringify", num(2024), num(0), num(2)) }},
	{`JSON.stringify({ a: [1] }, "x", 1)`, func() value.Value {
		return call("JSON", "stringify", value.Object().Set("a", arr(num(1))), str("x"), num(1))
	}},
	{`JSON.stringify({ a: 1 }, { b: 2 })`, func() value.Value {
		return call("JSON", "stringify", value.Object().Set("a", num(1)), value.Object().Set("b", num(2)))
	}},
	{`JSON.stringify([1], true, true)`, func() value.Value {
		return call("JSON", "stringify", arr(num(1)), value.Boolean(true), value.Boolean(true))
	}},
	{`JSON.stringify([1], null, "😀😀😀😀😀😀")`, func() value.Value {
		return call("JSON", "stringify", arr(num(1)), value.Null(), str("😀😀😀😀😀😀"))
	}},
	{`JSON.stringify([1], null, 2.9)`, func() value.Value { return call("JSON", "stringify", arr(num(1)), value.Null(), num(2.9)) }},
	{`JSON.stringify(JSON.parse('{"a": 1, "b": 2}', (k, v) => k === "a" ? undefined : v))`, func() value.Value {
		return value.Stringify(call("JSON", "parse", str(`{"a": 1, "b": 2}`), value.Function(func(a ...value.Value) value.Value {
			if a[0].Text() == "a" {
				return value.Undefined()
			}
			return a[1]
		})), "")
	}},
	{`JSON.parse("[1, [2]]", (k, v) => Array.isArray(v) ? v.length : v * 10)`, func() value.Value {
		return call("JSON", "parse", str("[1, [2]]"), value.Function(func(a ...value.Value) value.Value {
			if a[1].IsArray() {
				return a[1].Get("length")
			}
			return a[1].Multiply(num(10))
		}))
	}},
	{`(() => { const seen = []; JSON.parse('{"a": {"b": [1]}, "c": 2}', (k, v) => { seen.push(k); return v; }); return seen.join("|"); })()`, func() value.Value {
		seen := arr()
		call("JSON", "parse", str(`{"a": {"b": [1]}, "c": 2}`), value.Function(func(a ...value.Value) value.Value {
			seen.Push(a[0])
			return a[1]
		}))
		return seen.Join(str("|"))
	}},
	{`JSON.parse("5", 1)`, func() value.Value { return call("JSON", "parse", str("5"), num(1)) }},

	// Date from parts in the host's zone, node's and Go's both the machine's own
	{`new Date(2024, 0, 31, 10, 30).getTime()`, func() value.Value {
		return call("Date", "", num(2024), num(0), num(31), num(10), num(30)).Invoke("getTime", nil)
	}},
	{`new Date(99, 0).getTime()`, func() value.Value { return call("Date", "", num(99), num(0)).Invoke("getTime", nil) }},
	{`new Date(2024, 12, 1).getTime()`, func() value.Value { return call("Date", "", num(2024), num(12), num(1)).Invoke("getTime", nil) }},
	{`new Date(2024, 0, 1, 0, 0, 0, -1).getTime()`, func() value.Value {
		return call("Date", "", num(2024), num(0), num(1), num(0), num(0), num(0), num(-1)).Invoke("getTime", nil)
	}},
	{`new Date("2024-01-02 03:04").getTime()`, func() value.Value { return call("Date", "", str("2024-01-02 03:04")).Invoke("getTime", nil) }},
	{`new Date("Jan 2, 2024 10:30 PM").getTime()`, func() value.Value {
		return call("Date", "", str("Jan 2, 2024 10:30 PM")).Invoke("getTime", nil)
	}},
}

// TestParityWithNode compares every case's text and typeof with node's.
func TestParityWithNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	sources := make([]string, len(cases))
	for i, c := range cases {
		sources[i] = c.js
	}
	script := `
const sources = JSON.parse(require("fs").readFileSync(0, "utf8"));
const out = sources.map((src) => {
  try {
    const v = (0, eval)("(" + src + ")");
    return { text: String(v), type: typeof v };
  } catch (e) {
    return { text: "throws " + e.constructor.name, type: "throws" };
  }
});
process.stdout.write(JSON.stringify(out));`
	encoded, _ := json.Marshal(sources)
	command := exec.Command("node", "-e", script)
	command.Stdin = bytes.NewReader(encoded) // the cases are too long for a command line
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected []struct{ Text, Type string }
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("node output: %v\n%s", err, raw)
	}
	classes := map[string]string{value.TypeCode: "TypeError", value.RangeCode: "RangeError", value.InvalidInputCode: "SyntaxError"}
	failed := 0
	for i, c := range cases {
		got := c.got()
		gotText, gotType := got.ToString().Text(), got.Typeof().Text()
		if got.IsInvalid() {
			gotText, gotType = "throws "+got.Code(), "throws"
			if class, ok := classes[got.Code()]; ok {
				gotText = "throws " + class
			}
			// INVALID_INPUT is JavaScript's SyntaxError and also its URIError (a text that is no URI)
			if got.Code() == value.InvalidInputCode && expected[i].Text == "throws URIError" {
				gotText = "throws URIError"
			}
		}
		if gotText != expected[i].Text || gotType != expected[i].Type {
			failed++
			t.Errorf("%-48s node: %q (%s)   kitwork: %q (%s)", c.js, expected[i].Text, expected[i].Type, gotText, gotType)
		}
	}
	t.Logf("%d of %d cases agree with node", len(cases)-failed, len(cases))
}

// ---- Kitwork's own: error, safe, Date as a value ----

// error("…") is a FAIL failure with the text; safe gives { value, error }: error null and the value whatever it is
// (undefined too) when the function returns, value undefined and error { code, message } when it fails, and no other
// key, no ok (docs/language.md mục 5, 05/11/2026). fail is no builtin now, not one beside error.
func TestErrorAndSafe(t *testing.T) {
	stopped := call("error", "", str("out of stock"))
	if stopped.Code() != value.FailCode || stopped.Message() != "out of stock" {
		t.Fatalf("error(text) is a FAIL failure with that text: %s", stopped.Failure())
	}
	if _, kept := globals["fail"]; kept {
		t.Error("fail is still a builtin beside error")
	}
	caught := call("safe", "", value.Function(func(...value.Value) value.Value { return call("error", "", str("boom")) }))
	if got := strings.Join(caught.Keys(), ","); got != "value,error" || !caught.Get("value").IsUndefined() ||
		caught.Get("error").Get("code").Text() != "FAIL" || caught.Get("error").Get("message").Text() != "boom" {
		t.Errorf("safe(() => error(\"boom\")) = %s, keys %s", value.Stringify(caught, "").Text(), got)
	}
	for _, returned := range []value.Value{num(42), value.Boolean(false), value.Null(), value.Undefined()} {
		fine := call("safe", "", value.Function(func(...value.Value) value.Value { return returned }))
		if got := strings.Join(fine.Keys(), ","); got != "value,error" || !fine.Get("error").IsNull() || !fine.Get("value").Equal(returned) {
			t.Errorf("safe(() => %s) = %s, keys %s", returned.ToString().Text(), value.Stringify(fine, "").Text(), got)
		}
	}
	if got := call("safe", "", num(1)); got.Code() != value.TypeCode {
		t.Errorf("safe(1) is a TYPE failure: %s", got.ToString().Text())
	}
}

func TestDateIsATimeValue(t *testing.T) {
	a, b := call("Date", "", num(5)), call("Date", "", num(5))
	if !a.IsTime() || !a.Equal(b) {
		t.Error("two Dates of one instant are === (a Time value, not an object)")
	}
	if got := call("Date", "", str("not a date")); got.Code() != value.RangeCode {
		t.Errorf("an unreadable date is a RANGE failure: %s", got.ToString().Text())
	}
	if now := call("Date", ""); !now.IsTime() || time.Since(now.Time()) > time.Minute {
		t.Error("Date() is now")
	}
	later := a.Add(value.Duration(time.Hour))
	if later.Invoke("getTime", nil).Number() != 5+3600000 {
		t.Error("a Date adds a Duration")
	}
}
