package javascript_test

import "testing"

// Number, String, Array, Boolean, parseInt, parseFloat, isNaN, isFinite đối chiếu với node. Một hàm toàn cục được gọi
// qua call (Number.call(null, "3") là Number("3")), vì mỗi ca là một lời gọi this.method(…).

func TestNumberAndBooleanAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`Number.call(null, "42")`, `Number.call(null)`, `Number.call(null, undefined)`, `Number.call(null, null)`,
		`Number.call(null, " 0x1F ")`, `Number.call(null, "1e3")`, `Number.call(null, "")`, `Number.call(null, "12px")`,
		`Number.call(null, [5])`, `Number.call(null, [1, 2])`, `Number.call(null, {valueOf: seven})`, `Number.call(null, {valueOf: boom})`,
		`Number.call(null, true)`, `Number.call(null, "-0")`, `Number.call(null, "0b101")`, `Number.call(null, "Infinity")`,
		`Number.call(null, date(5))`, `Number.call(null, "1_000")`, `Number.call(null, "\u00A0 7 ")`,
		`Number.isFinite(1)`, `Number.isFinite("1")`, `Number.isFinite(Infinity)`, `Number.isFinite(NaN)`, `Number.isFinite()`,
		`Number.isInteger(5)`, `Number.isInteger(5.5)`, `Number.isInteger("5")`, `Number.isInteger(-0)`, `Number.isInteger(Infinity)`,
		`Number.isInteger(1e300)`, `Number.isInteger(5.000000000000001)`,
		`Number.isNaN(NaN)`, `Number.isNaN("x")`, `Number.isNaN()`, `Number.isNaN(Number.NaN)`,
		`Number.isSafeInteger(9007199254740991)`, `Number.isSafeInteger(9007199254740992)`, `Number.isSafeInteger(-9007199254740991)`,
		`Number.isSafeInteger(1.5)`, `Number.isSafeInteger("1")`, `Number.isSafeInteger(Number.MAX_SAFE_INTEGER)`,
		`Number.isSafeInteger(Number.MIN_SAFE_INTEGER)`,
		`Number.parseInt("12px")`, `Number.parseFloat("1.5e3x")`,
		`Math.abs(Number.MIN_VALUE)`, `Math.abs(Number.MAX_VALUE)`, `Math.abs(Number.EPSILON)`, `Math.abs(Number.NEGATIVE_INFINITY)`,
		`Math.sign(Number.POSITIVE_INFINITY)`, `Math.abs(Number.MIN_SAFE_INTEGER)`,
		`Number.foo()`, `Number.toFixed(1)`, `Number.MAX_VALUE(1)`,
		`["1", " 2 ", "x", "", null].map(Number)`, `[3, "4"].map(Number.isInteger)`,
		`Boolean.call(null, 0)`, `Boolean.call(null, "0")`, `Boolean.call(null)`, `Boolean.call(null, [])`, `Boolean.call(null, NaN)`,
		`Boolean.call(null, -0)`, `Boolean.call(null, {})`, `[0, 1, "", "a", null, [], -0, NaN, undefined].filter(Boolean)`,
		`Math.max.apply(null, [1, 5, 3])`, `Math.max.apply(null)`, `Math.max.apply(null, 5)`, `Math.max.apply(null, "12")`,
		`Math.max.call(null, 1, 2)`, `Math.max.call()`, `double.call(null, 4)`, `double.apply(null, [4])`,
		`Math.max.apply(null, {length: 2, 0: 1, 1: 7})`, `Math.max.apply(null, {length: {valueOf: boom}})`, `boom.call(null)`,
	})
}

func TestStringAndArrayAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`String.call(null, 5)`, `String.call(null)`, `String.call(null, undefined)`, `String.call(null, null)`,
		`String.call(null, [1, [2, 3]])`, `String.call(null, {})`, `String.call(null, {toString: text})`, `String.call(null, {toString: boom})`,
		`String.call(null, -0)`, `String.call(null, 1e21)`, `String.call(null, true)`, `String.call(null, "x")`, `String.call(null, [null])`,
		`String.fromCharCode(72, 105)`, `String.fromCharCode()`, `String.fromCharCode(65601)`, `String.fromCharCode(-1)`,
		`String.fromCharCode(55357, 56832)`, `String.fromCharCode(55357)`, `String.fromCharCode(56832, 55357)`,
		`String.fromCharCode("65", [66])`, `String.fromCharCode(NaN, Infinity)`, `String.fromCharCode(65.9)`,
		`String.fromCharCode({valueOf: boom})`, `String.fromCharCode(4294967361)`,
		`String.fromCodePoint(128512)`, `String.fromCodePoint(65, 128512, 66)`, `String.fromCodePoint()`, `String.fromCodePoint(-1)`,
		`String.fromCodePoint(1114112)`, `String.fromCodePoint(1.5)`, `String.fromCodePoint(NaN)`, `String.fromCodePoint("65")`,
		`String.fromCodePoint(55357, 56832)`, `String.fromCodePoint(55357)`, `String.fromCodePoint(Infinity)`, `String.fromCodePoint(-0)`,
		`String.fromCodePoint(65, {valueOf: boom})`, `String.fromCodePoint(-1, {valueOf: boom})`, `String.fromCodePoint(1114111)`,
		`String.foo()`, `[1, null, "a", [2, 3]].map(String)`, `[72, 105].map(String.fromCharCode)`,
		`Array.call(null)`, `Array.call(null, 1, 2)`, `Array.call(null, "3")`, `Array.call(null, 0)`, `Array.call(null, -1)`,
		`Array.call(null, 1.5)`, `Array.call(null, NaN)`, `Array.call(null, 4294967296)`, `Array.call(null, [1])`, `Array.call(null, undefined)`,
		`Array.isArray([])`, `Array.isArray({})`, `Array.isArray("a")`, `Array.isArray()`, `Array.isArray({length: 0})`,
		`Array.of()`, `Array.of(7)`, `Array.of(1, "a", null)`, `Array.of(undefined)`,
		`Array.from([1, 2])`, `Array.from("a😀b")`, `Array.from("")`, `Array.from({length: 3})`,
		`Array.from({length: 2, 0: "a", 1: "b", 2: "c"})`, `Array.from({length: "2"})`, `Array.from({length: -1})`,
		`Array.from({length: 2.7})`, `Array.from({})`, `Array.from(5)`, `Array.from(true)`, `Array.from(null)`, `Array.from()`,
		`Array.from([1, 2], double)`, `Array.from("ab", upper)`, `Array.from({length: 3}, index)`, `Array.from([1], 5)`,
		`Array.from([1, 2], boom)`, `Array.from({length: {valueOf: boom}})`, `Array.from([1, 2], null)`, `Array.from(date(0))`,
		`Array.from([[1], [2]], identity)`, `Array.from({length: 2}, pair)`, `Array.from("\uD83Dx")`, `Array.from({length: NaN})`,
		`Array.from(undefined, double)`, `Array.from(null, 5)`, `Array.foo()`,
		`[[1], "ab", {length: 1}].map(Array.from)`, `[[], {}].map(Array.isArray)`,
	})
}

func TestParseIntAndParseFloatAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`parseInt.call(null, "12px")`, `parseInt.call(null, "  -42abc")`, `parseInt.call(null, "0x1F")`, `parseInt.call(null, "0x1F", 16)`,
		`parseInt.call(null, "0x1F", 10)`, `parseInt.call(null, "1F", 16)`, `parseInt.call(null, "ff", 36)`, `parseInt.call(null, "z", 36)`,
		`parseInt.call(null, "z", 37)`, `parseInt.call(null, "10", 1)`, `parseInt.call(null, "10", 0)`, `parseInt.call(null, "10", 2)`,
		`parseInt.call(null, "102", 2)`, `parseInt.call(null, "")`, `parseInt.call(null, "x")`, `parseInt.call(null, "-")`,
		`parseInt.call(null, "-0")`, `parseInt.call(null, 0.0000005)`, `parseInt.call(null, null)`, `parseInt.call(null, "null", 36)`,
		`parseInt.call(null, "123456789012345678901234567890")`, `parseInt.call(null, "9007199254740993")`,
		`parseInt.call(null, "11111111111111111111111111111111111111111111111111111111111", 2)`,
		`parseInt.call(null, "fffffffffffffffffff", 16)`, `parseInt.call(null, "777777777777777777777", 8)`,
		`parseInt.call(null, "10", 4294967312)`, `parseInt.call(null, "10", -4294967294)`, `parseInt.call(null, "10", "16")`,
		`parseInt.call(null, "10", {valueOf: boom})`, `parseInt.call(null, {toString: boom}, {valueOf: boom})`,
		`parseInt.call(null, "\u00A0 12")`, `parseInt.call(null, "\u2028\uFEFF12")`, `parseInt.call(null, "１２")`, `parseInt.call(null, "0x")`,
		`parseInt.call(null, "0X10")`, `parseInt.call(null, "+0x10")`, `parseInt.call(null, "-0x10")`, `parseInt.call(null, "0b11")`,
		`parseInt.call(null, "Infinity")`, `parseInt.call(null, "1e3")`, `parseInt.call(null)`, `parseInt.call(null, "12", undefined)`,
		`parseInt.call(null, "12", NaN)`, `parseInt.call(null, "12", 2.9)`, `parseInt.call(null, [" 7"])`, `parseInt.call(null, "0x10", 8)`,
		`parseInt.call(null, "Phường")`,
		`["10", "10", "10"].map(parseInt)`, `["1", "2", "3"].map(parseInt)`,
		`parseFloat.call(null, "3.14abc")`, `parseFloat.call(null, "  .5")`, `parseFloat.call(null, "-.5e1x")`, `parseFloat.call(null, "1e")`,
		`parseFloat.call(null, "1e+")`, `parseFloat.call(null, "Infinityx")`, `parseFloat.call(null, "-Infinity")`,
		`parseFloat.call(null, "+Infinity")`, `parseFloat.call(null, "infinity")`, `parseFloat.call(null, "0x10")`, `parseFloat.call(null, "")`,
		`parseFloat.call(null, "-0")`, `parseFloat.call(null, "1_000")`, `parseFloat.call(null, ".")`, `parseFloat.call(null, "1.2.3")`,
		`parseFloat.call(null, "1e400")`, `parseFloat.call(null, "5e-400")`, `parseFloat.call(null, [2.5])`, `parseFloat.call(null, {toString: boom})`,
		`parseFloat.call(null)`, `parseFloat.call(null, "\u2028 7")`, `parseFloat.call(null, "７")`, `parseFloat.call(null, "12345678901234567890.5")`,
		`parseFloat.call(null, "1.")`, `parseFloat.call(null, "+.5")`, `parseFloat.call(null, "-")`, `parseFloat.call(null, "e5")`,
		`parseFloat.call(null, "0.1e-2x")`, `parseFloat.call(null, "Infinit")`, `parseFloat.call(null, 1e21)`, `parseFloat.call(null, true)`,
		`["1.5", " 2", "x"].map(parseFloat)`,
		`isNaN.call(null, "abc")`, `isNaN.call(null, "12")`, `isNaN.call(null)`, `isNaN.call(null, {valueOf: boom})`, `isNaN.call(null, null)`,
		`isNaN.call(null, [])`, `isNaN.call(null, [1, 2])`,
		`isFinite.call(null, "12")`, `isFinite.call(null, "Infinity")`, `isFinite.call(null, null)`, `isFinite.call(null)`,
		`isFinite.call(null, 1e308)`, `isFinite.call(null, "1e309")`, `["1", "x"].map(isNaN)`,
	})
}
