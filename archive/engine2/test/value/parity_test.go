// Package value_test checks core/value from outside: every case is a JavaScript expression node runs
// and the same operation written against the package; the two must print the same text and typeof.
package value_test

import (
	"encoding/json"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

type parity struct {
	js  string
	got func() value.Value
}

var (
	num = value.Number
	str = value.String
	arr = func(items ...value.Value) value.Value { return value.Array(items) }
	nan = value.Number(math.NaN())
	yes = value.Boolean(true)
	no  = value.Boolean(false)
)

var (
	fn1        = value.Function(func(...value.Value) value.Value { return num(1) })
	identity   = value.Function(func(a ...value.Value) value.Value { return a[0] })
	double     = value.Function(func(a ...value.Value) value.Value { return a[0].Multiply(num(2)) })
	timesIndex = value.Function(func(a ...value.Value) value.Value { return a[0].Multiply(a[1]) })
	isEven     = value.Function(func(a ...value.Value) value.Value { return value.Boolean(a[0].Modulo(num(2)).Equal(num(0))) })
	plus       = value.Function(func(a ...value.Value) value.Value { return a[0].Add(a[1]) })
	minus      = value.Function(func(a ...value.Value) value.Value { return a[0].Subtract(a[1]) })
	zero       = value.Function(func(...value.Value) value.Value { return num(0) })
)

// backslash lets a case hold an escape sequence without writing one in this file.
const backslash = string(rune(92))

// cases pair a JavaScript expression with the package call a compiled program would make for it.
var cases = []parity{
	// numbers as text
	{`String(10)`, func() value.Value { return num(10).ToString() }},
	{`String(-0.1)`, func() value.Value { return num(-0.1).ToString() }},
	{`String(1234567.5)`, func() value.Value { return num(1234567.5).ToString() }},
	{`String(1e21)`, func() value.Value { return num(1e21).ToString() }},
	{`String(1e-7)`, func() value.Value { return num(1e-7).ToString() }},
	{`String(2**53)`, func() value.Value { return num(1 << 53).ToString() }},
	{`String(-0)`, func() value.Value { return num(math.Copysign(0, -1)).ToString() }},
	{`String(NaN)`, func() value.Value { return nan.ToString() }},
	{`String(-Infinity)`, func() value.Value { return num(math.Inf(-1)).ToString() }},
	{`String(123456789012345680000)`, func() value.Value { return num(123456789012345680000).ToString() }},
	{`String(5e-324)`, func() value.Value { return num(5e-324).ToString() }},
	{`String(0.1 * 3)`, func() value.Value { return num(0.1).Multiply(num(3)).ToString() }},
	{`String(1 / 3)`, func() value.Value { return num(1).Divide(num(3)).ToString() }},
	{`String(-1e-7)`, func() value.Value { return num(-1e-7).ToString() }},
	{`String(123e-20)`, func() value.Value { return num(123e-20).ToString() }},
	{`String(2 ** 64)`, func() value.Value { return num(math.Pow(2, 64)).ToString() }},
	{`(1.25).toFixed(1)`, func() value.Value { return num(1.25).ToFixed(num(1)) }},
	{`(2.5).toFixed(0)`, func() value.Value { return num(2.5).ToFixed(num(0)) }},
	{`(1.005).toFixed(2)`, func() value.Value { return num(1.005).ToFixed(num(2)) }},
	{`(-0).toFixed(2)`, func() value.Value { return num(math.Copysign(0, -1)).ToFixed(num(2)) }},
	{`(1).toFixed(100.5).length`, func() value.Value { return num(1).ToFixed(num(100.5)).Get("length") }},
	{`(1).toFixed(-0.5)`, func() value.Value { return num(1).ToFixed(num(-0.5)) }},
	{`(1).toFixed(101)`, func() value.Value { return num(1).ToFixed(num(101)) }},
	{`(255).toString(16)`, func() value.Value { return num(255).ToString(num(16)) }},
	{`(255).toString(2)`, func() value.Value { return num(255).ToString(num(2)) }},
	{`(-255).toString(36)`, func() value.Value { return num(-255).ToString(num(36)) }},
	{`(0.5).toString(2)`, func() value.Value { return num(0.5).ToString(num(2)) }},
	{`(0.1).toString(16).slice(0, 6)`, func() value.Value { return num(0.1).ToString(num(16)).Slice(num(0), num(6)) }},
	{`(255).toString(1)`, func() value.Value { return num(255).ToString(num(1)) }},
	{`(255).toString(10)`, func() value.Value { return num(255).ToString(num(10)) }},

	// text as numbers
	{`Number("42")`, func() value.Value { return str("42").ToNumber() }},
	{`Number(" 4.2 ")`, func() value.Value { return str(" 4.2 ").ToNumber() }},
	{`Number("")`, func() value.Value { return str("").ToNumber() }},
	{`Number("0x1f")`, func() value.Value { return str("0x1f").ToNumber() }},
	{`Number("0b101")`, func() value.Value { return str("0b101").ToNumber() }},
	{`Number("5a")`, func() value.Value { return str("5a").ToNumber() }},
	{`Number("1e3")`, func() value.Value { return str("1e3").ToNumber() }},
	{`Number("-Infinity")`, func() value.Value { return str("-Infinity").ToNumber() }},
	{`Number(".5")`, func() value.Value { return str(".5").ToNumber() }},
	{`Number("5.")`, func() value.Value { return str("5.").ToNumber() }},
	{`Number("1e")`, func() value.Value { return str("1e").ToNumber() }},
	{`Number("1_000")`, func() value.Value { return str("1_000").ToNumber() }},
	{`Number("+-1")`, func() value.Value { return str("+-1").ToNumber() }},
	{`Number(" 0x ")`, func() value.Value { return str(" 0x ").ToNumber() }},
	{`Number("\n5\n")`, func() value.Value { return str("\n5\n").ToNumber() }},
	{`Number("Infinityx")`, func() value.Value { return str("Infinityx").ToNumber() }},
	{`Number(null)`, func() value.Value { return value.Null().ToNumber() }},
	{`Number(undefined)`, func() value.Value { return value.Undefined().ToNumber() }},
	{`Number(true)`, func() value.Value { return yes.ToNumber() }},
	{`Number([5])`, func() value.Value { return arr(num(5)).ToNumber() }},
	{`Number([1,2])`, func() value.Value { return arr(num(1), num(2)).ToNumber() }},
	{`Number([])`, func() value.Value { return arr().ToNumber() }},
	{`Number({})`, func() value.Value { return value.Object().ToNumber() }},

	// typeof, truthiness
	{`typeof undefined`, func() value.Value { return value.Undefined().Typeof() }},
	{`typeof null`, func() value.Value { return value.Null().Typeof() }},
	{`typeof 1`, func() value.Value { return num(1).Typeof() }},
	{`typeof "a"`, func() value.Value { return str("a").Typeof() }},
	{`typeof true`, func() value.Value { return yes.Typeof() }},
	{`typeof []`, func() value.Value { return arr().Typeof() }},
	{`typeof {}`, func() value.Value { return value.Object().Typeof() }},
	{`typeof (() => 1)`, func() value.Value { return fn1.Typeof() }},
	{`!""`, func() value.Value { return str("").Not() }},
	{`!"0"`, func() value.Value { return str("0").Not() }},
	{`!0`, func() value.Value { return num(0).Not() }},
	{`!NaN`, func() value.Value { return nan.Not() }},
	{`![]`, func() value.Value { return arr().Not() }},
	{`!null`, func() value.Value { return value.Null().Not() }},

	// + and arithmetic
	{`"5" + 1`, func() value.Value { return str("5").Add(num(1)) }},
	{`1 + "5"`, func() value.Value { return num(1).Add(str("5")) }},
	{`5 + 1`, func() value.Value { return num(5).Add(num(1)) }},
	{`0.1 + 0.2`, func() value.Value { return num(0.1).Add(num(0.2)) }},
	{`null + 1`, func() value.Value { return value.Null().Add(num(1)) }},
	{`undefined + 1`, func() value.Value { return value.Undefined().Add(num(1)) }},
	{`true + true`, func() value.Value { return yes.Add(yes) }},
	{`[1,2] + ""`, func() value.Value { return arr(num(1), num(2)).Add(str("")) }},
	{`[1,[2,3]] + ""`, func() value.Value { return arr(num(1), arr(num(2), num(3))).Add(str("")) }},
	{`[null, undefined, 1] + ""`, func() value.Value { return arr(value.Null(), value.Undefined(), num(1)).Add(str("")) }},
	{`({}) + 1`, func() value.Value { return value.Object().Add(num(1)) }},
	{`[] + []`, func() value.Value { return arr().Add(arr()) }},
	{`"3" - 1`, func() value.Value { return str("3").Subtract(num(1)) }},
	{`"3" * "4"`, func() value.Value { return str("3").Multiply(str("4")) }},
	{`"a" * 2`, func() value.Value { return str("a").Multiply(num(2)) }},
	{`1 / 0`, func() value.Value { return num(1).Divide(num(0)) }},
	{`-1 / 0`, func() value.Value { return num(-1).Divide(num(0)) }},
	{`0 / 0`, func() value.Value { return num(0).Divide(num(0)) }},
	{`7 % 3`, func() value.Value { return num(7).Modulo(num(3)) }},
	{`-7 % 3`, func() value.Value { return num(-7).Modulo(num(3)) }},
	{`5 % 0`, func() value.Value { return num(5).Modulo(num(0)) }},
	{`5.5 % 2`, func() value.Value { return num(5.5).Modulo(num(2)) }},
	{`2 ** 10`, func() value.Value { return num(2).Power(num(10)) }},
	{`1 ** Infinity`, func() value.Value { return num(1).Power(num(math.Inf(1))) }},
	{`-"3"`, func() value.Value { return str("3").Negate() }},
	{`-""`, func() value.Value { return str("").Negate() }},
	{`-[]`, func() value.Value { return arr().Negate() }},
	{`-0 + 0`, func() value.Value { return num(math.Copysign(0, -1)).Add(num(0)) }},
	{`1 / (-0 * 1)`, func() value.Value { return num(1).Divide(num(math.Copysign(0, -1)).Multiply(num(1))) }},

	// equality and order
	{`1 === 1`, func() value.Value { return value.Boolean(num(1).Equal(num(1))) }},
	{`1 === "1"`, func() value.Value { return value.Boolean(num(1).Equal(str("1"))) }},
	{`1 == "1"`, func() value.Value { return value.Boolean(num(1).LooseEqual(str("1"))) }},
	{`null == undefined`, func() value.Value { return value.Boolean(value.Null().LooseEqual(value.Undefined())) }},
	{`null === undefined`, func() value.Value { return value.Boolean(value.Null().Equal(value.Undefined())) }},
	{`null == 0`, func() value.Value { return value.Boolean(value.Null().LooseEqual(num(0))) }},
	{`NaN === NaN`, func() value.Value { return value.Boolean(nan.Equal(nan)) }},
	{`NaN == NaN`, func() value.Value { return value.Boolean(nan.LooseEqual(nan)) }},
	{`true == 1`, func() value.Value { return value.Boolean(yes.LooseEqual(num(1))) }},
	{`true == "1"`, func() value.Value { return value.Boolean(yes.LooseEqual(str("1"))) }},
	{`"" == 0`, func() value.Value { return value.Boolean(str("").LooseEqual(num(0))) }},
	{`[1] == 1`, func() value.Value { return value.Boolean(arr(num(1)).LooseEqual(num(1))) }},
	{`[] == ""`, func() value.Value { return value.Boolean(arr().LooseEqual(str(""))) }},
	{`[] === []`, func() value.Value { return value.Boolean(arr().Equal(arr())) }},
	{`"a" === "a"`, func() value.Value { return value.Boolean(str("a").Equal(str("a"))) }},
	{`undefined == 0`, func() value.Value { return value.Boolean(value.Undefined().LooseEqual(num(0))) }},
	{`false == "0"`, func() value.Value { return value.Boolean(no.LooseEqual(str("0"))) }},
	{`false == ""`, func() value.Value { return value.Boolean(no.LooseEqual(str(""))) }},
	{`"0" == ""`, func() value.Value { return value.Boolean(str("0").LooseEqual(str(""))) }},
	{`[0] == false`, func() value.Value { return value.Boolean(arr(num(0)).LooseEqual(no)) }},
	{`[1,2] == "1,2"`, func() value.Value { return value.Boolean(arr(num(1), num(2)).LooseEqual(str("1,2"))) }},
	{`({}) == "[object Object]"`, func() value.Value { return value.Boolean(value.Object().LooseEqual(str("[object Object]"))) }},
	{`"5" < 10`, func() value.Value { return value.Boolean(str("5").Less(num(10))) }},
	{`"10" < "9"`, func() value.Value { return value.Boolean(str("10").Less(str("9"))) }},
	{`"a" < "b"`, func() value.Value { return value.Boolean(str("a").Less(str("b"))) }},
	{`NaN < 1`, func() value.Value { return value.Boolean(nan.Less(num(1))) }},
	{`NaN <= 1`, func() value.Value { return value.Boolean(nan.LessEqual(num(1))) }},
	{`1 <= 1`, func() value.Value { return value.Boolean(num(1).LessEqual(num(1))) }},
	{`null < 1`, func() value.Value { return value.Boolean(value.Null().Less(num(1))) }},
	{`undefined < 1`, func() value.Value { return value.Boolean(value.Undefined().Less(num(1))) }},
	{`[2] > 1`, func() value.Value { return value.Boolean(arr(num(2)).Greater(num(1))) }},
	{`"b" >= "a"`, func() value.Value { return value.Boolean(str("b").GreaterEqual(str("a"))) }},
	{`"5" > 10`, func() value.Value { return value.Boolean(str("5").Greater(num(10))) }},
	{`"a" <= 1`, func() value.Value { return value.Boolean(str("a").LessEqual(num(1))) }},
	{`[] < 1`, func() value.Value { return value.Boolean(arr().Less(num(1))) }},
	{`[2] >= "2"`, func() value.Value { return value.Boolean(arr(num(2)).GreaterEqual(str("2"))) }},
	{`null >= 0`, func() value.Value { return value.Boolean(value.Null().GreaterEqual(num(0))) }},
	{`"😀" < String.fromCharCode(65535)`, func() value.Value { return value.Boolean(str("😀").Less(str(string(rune(0xFFFF))))) }},
	{`"😀" < String.fromCharCode(57344)`, func() value.Value { return value.Boolean(str("😀").Less(str(string(rune(0xE000))))) }},
	{`"ｚ" < "😀"`, func() value.Value { return value.Boolean(str("ｚ").Less(str("😀"))) }},
	{`"é" < "f"`, func() value.Value { return value.Boolean(str("é").Less(str("f"))) }},
	{`"Phường" > "Phuong"`, func() value.Value { return value.Boolean(str("Phường").Greater(str("Phuong"))) }},

	// string methods, UTF-16 units
	{`"Phường".length`, func() value.Value { return str("Phường").Get("length") }},
	{`"😀".length`, func() value.Value { return str("😀").Get("length") }},
	{`"Phường"[2]`, func() value.Value { return str("Phường").Index(2) }},
	{`"abc"[5]`, func() value.Value { return str("abc").Index(5) }},
	{`"abc".toUpperCase()`, func() value.Value { return str("abc").ToUpperCase() }},
	{`"  a b ".trim()`, func() value.Value { return str("  a b ").Trim() }},
	{`(String.fromCharCode(160) + "x" + String.fromCharCode(65279)).trim()`, func() value.Value {
		return str(string(rune(0xA0)) + "x" + string(rune(0xFEFF))).Trim()
	}},
	{`"  a".trimStart()`, func() value.Value { return str("  a").TrimStart() }},
	{`"a  ".trimEnd()`, func() value.Value { return str("a  ").TrimEnd() }},
	{`"abc".trimLeft()`, func() value.Value { return str("abc").TrimLeft() }},
	{`" abc ".trimRight()`, func() value.Value { return str(" abc ").TrimRight() }},
	{`"hello".includes("ell")`, func() value.Value { return str("hello").Includes(str("ell")) }},
	{`"hello".includes("h", 1)`, func() value.Value { return str("hello").Includes(str("h"), num(1)) }},
	{`"hello".includes("")`, func() value.Value { return str("hello").Includes(str("")) }},
	{`"hello".startsWith("he")`, func() value.Value { return str("hello").StartsWith(str("he")) }},
	{`"hello".startsWith("ll", 2)`, func() value.Value { return str("hello").StartsWith(str("ll"), num(2)) }},
	{`"hello".endsWith("lo")`, func() value.Value { return str("hello").EndsWith(str("lo")) }},
	{`"hello".endsWith("ll", 4)`, func() value.Value { return str("hello").EndsWith(str("ll"), num(4)) }},
	{`"a,b,,c".split(",")`, func() value.Value { return str("a,b,,c").Split(str(",")) }},
	{`"a,b,c".split(",", 2)`, func() value.Value { return str("a,b,c").Split(str(","), num(2)) }},
	{`"Phường".split("")`, func() value.Value { return str("Phường").Split(str("")) }},
	{`"abc".split()`, func() value.Value { return str("abc").Split() }},
	{`"".split(",")`, func() value.Value { return str("").Split(str(",")) }},
	{`"".split("")`, func() value.Value { return str("").Split(str("")) }},
	{`"a,b,c".split(",", -1).length`, func() value.Value { return str("a,b,c").Split(str(","), num(-1)).Get("length") }},
	{`"a,b,c".split(",", undefined).length`, func() value.Value { return str("a,b,c").Split(str(","), value.Undefined()).Get("length") }},
	{`"a,b".split(undefined)`, func() value.Value { return str("a,b").Split(value.Undefined()) }},
	{`"aXbXc".replace("X", "-")`, func() value.Value { return str("aXbXc").Replace(str("X"), str("-")) }},
	{`"aXbXc".replaceAll("X", "-")`, func() value.Value { return str("aXbXc").ReplaceAll(str("X"), str("-")) }},
	{`"abc".replace("b")`, func() value.Value { return str("abc").Replace(str("b")) }},
	{`"aaa".replace("a", "$&$&")`, func() value.Value { return str("aaa").Replace(str("a"), str("$&$&")) }},
	{`"aaa".replace("a", "$$")`, func() value.Value { return str("aaa").Replace(str("a"), str("$$")) }},
	{"\"abc\".replace(\"b\", \"$`\")", func() value.Value { return str("abc").Replace(str("b"), str("$`")) }},
	{`"abc".replace("b", "$'")`, func() value.Value { return str("abc").Replace(str("b"), str("$'")) }},
	{`"abc".replaceAll("b", "$&$&")`, func() value.Value { return str("abc").ReplaceAll(str("b"), str("$&$&")) }},
	{`"abc".replace("b", "$1")`, func() value.Value { return str("abc").Replace(str("b"), str("$1")) }},
	{`"abc".replace("b", "$")`, func() value.Value { return str("abc").Replace(str("b"), str("$")) }},
	{`"abc".replaceAll("", "-")`, func() value.Value { return str("abc").ReplaceAll(str(""), str("-")) }},
	{`"abc".replace("", "-")`, func() value.Value { return str("abc").Replace(str(""), str("-")) }},
	{`"hello".slice(1, 3)`, func() value.Value { return str("hello").Slice(num(1), num(3)) }},
	{`"hello".slice(-2)`, func() value.Value { return str("hello").Slice(num(-2)) }},
	{`"hello".slice(3, 1)`, func() value.Value { return str("hello").Slice(num(3), num(1)) }},
	{`"hello".slice(-10, 2)`, func() value.Value { return str("hello").Slice(num(-10), num(2)) }},
	{`"Phường".slice(2, 4)`, func() value.Value { return str("Phường").Slice(num(2), num(4)) }},
	{`"hello".slice(NaN)`, func() value.Value { return str("hello").Slice(nan) }},
	{`"hello".slice(1, Infinity)`, func() value.Value { return str("hello").Slice(num(1), num(math.Inf(1))) }},
	{`"hello".substring(3, 1)`, func() value.Value { return str("hello").Substring(num(3), num(1)) }},
	{`"hello".substring(-2, 2)`, func() value.Value { return str("hello").Substring(num(-2), num(2)) }},
	{`"hello".substring(2)`, func() value.Value { return str("hello").Substring(num(2)) }},
	{`"hello".substring(NaN, 2)`, func() value.Value { return str("hello").Substring(nan, num(2)) }},
	{`"hello".indexOf("l")`, func() value.Value { return str("hello").IndexOf(str("l")) }},
	{`"hello".indexOf("l", 3)`, func() value.Value { return str("hello").IndexOf(str("l"), num(3)) }},
	{`"hello".indexOf("z")`, func() value.Value { return str("hello").IndexOf(str("z")) }},
	{`"hello".indexOf("")`, func() value.Value { return str("hello").IndexOf(str("")) }},
	{`"hello".indexOf("l", -Infinity)`, func() value.Value { return str("hello").IndexOf(str("l"), num(math.Inf(-1))) }},
	{`"Phường".indexOf("ng")`, func() value.Value { return str("Phường").IndexOf(str("ng")) }},
	{`"hello".lastIndexOf("l")`, func() value.Value { return str("hello").LastIndexOf(str("l")) }},
	{`"hello".lastIndexOf("l", 2)`, func() value.Value { return str("hello").LastIndexOf(str("l"), num(2)) }},
	{`"hello".lastIndexOf("h", -5)`, func() value.Value { return str("hello").LastIndexOf(str("h"), num(-5)) }},
	{`"hello".lastIndexOf("l", NaN)`, func() value.Value { return str("hello").LastIndexOf(str("l"), nan) }},
	{`"hello".charAt(1)`, func() value.Value { return str("hello").CharAt(num(1)) }},
	{`"hello".charAt(9)`, func() value.Value { return str("hello").CharAt(num(9)) }},
	{`"hello".charAt()`, func() value.Value { return str("hello").CharAt() }},
	{`"hello".charAt(1.5)`, func() value.Value { return str("hello").CharAt(num(1.5)) }},
	{`"hello".charAt(-1)`, func() value.Value { return str("hello").CharAt(num(-1)) }},
	{`"hello".charCodeAt(1)`, func() value.Value { return str("hello").CharCodeAt(num(1)) }},
	{`"hello".charCodeAt(9)`, func() value.Value { return str("hello").CharCodeAt(num(9)) }},
	{`"Phường".charCodeAt(2)`, func() value.Value { return str("Phường").CharCodeAt(num(2)) }},
	{`"hello".at(-1)`, func() value.Value { return str("hello").At(num(-1)) }},
	{`"hello".at(9)`, func() value.Value { return str("hello").At(num(9)) }},
	{`"ab".repeat(3)`, func() value.Value { return str("ab").Repeat(num(3)) }},
	{`"ab".repeat(0)`, func() value.Value { return str("ab").Repeat(num(0)) }},
	{`"ab".repeat(1e18)`, func() value.Value { return str("ab").Repeat(num(1e18)) }},
	{`"ab".repeat(2.9)`, func() value.Value { return str("ab").Repeat(num(2.9)) }},
	{`"ab".repeat(NaN)`, func() value.Value { return str("ab").Repeat(nan) }},
	{`"ab".repeat(-1)`, func() value.Value { return str("ab").Repeat(num(-1)) }},
	{`"5".padStart(3, "0")`, func() value.Value { return str("5").PadStart(num(3), str("0")) }},
	{`"5".padStart(3)`, func() value.Value { return str("5").PadStart(num(3)) }},
	{`"abc".padStart(7, "12")`, func() value.Value { return str("abc").PadStart(num(7), str("12")) }},
	{`"abc".padEnd(7, "12")`, func() value.Value { return str("abc").PadEnd(num(7), str("12")) }},
	{`"abc".padEnd(2)`, func() value.Value { return str("abc").PadEnd(num(2)) }},
	{`"abc".padEnd(5, "")`, func() value.Value { return str("abc").PadEnd(num(5), str("")) }},
	{`"abc".padStart(Infinity)`, func() value.Value { return str("abc").PadStart(num(math.Inf(1))) }},
	{`"abc".padEnd(Infinity, "x")`, func() value.Value { return str("abc").PadEnd(num(math.Inf(1)), str("x")) }},
	{`"a".concat("b", 1, null)`, func() value.Value { return str("a").Concat(str("b"), num(1), value.Null()) }},
	{`"abc".toString()`, func() value.Value { return str("abc").ToString() }},

	// surrogate halves
	{`"😀".slice(0, 1) + "😀".slice(1)`, func() value.Value { return str("😀").Slice(num(0), num(1)).Add(str("😀").Slice(num(1))) }},
	{`("😀".slice(0, 1) + "😀".slice(1)) === "😀"`, func() value.Value {
		return value.Boolean(str("😀").Slice(num(0), num(1)).Add(str("😀").Slice(num(1))).Equal(str("😀")))
	}},
	{`"😀".slice(0, 1).charCodeAt(0)`, func() value.Value { return str("😀").Slice(num(0), num(1)).CharCodeAt(num(0)) }},
	{`"😀x".slice(1).charCodeAt(0)`, func() value.Value { return str("😀x").Slice(num(1)).CharCodeAt(num(0)) }},
	{`"😀x".at(0).charCodeAt(0)`, func() value.Value { return str("😀x").At(num(0)).CharCodeAt(num(0)) }},
	{`"😀x"[1].charCodeAt(0)`, func() value.Value { return str("😀x").Index(1).CharCodeAt(num(0)) }},
	{`"😀x".split("").join("")`, func() value.Value { return str("😀x").Split(str("")).Join(str("")) }},
	{`"😀x".split("").length`, func() value.Value { return str("😀x").Split(str("")).Get("length") }},
	{`"😀".slice(0, 1).length`, func() value.Value { return str("😀").Slice(num(0), num(1)).Get("length") }},
	{`"x😀".padStart(4, "😀").charCodeAt(0)`, func() value.Value { return str("x😀").PadStart(num(4), str("😀")).CharCodeAt(num(0)) }},
	{`"😀".indexOf("😀".slice(1))`, func() value.Value { return str("😀").IndexOf(str("😀").Slice(num(1))) }},
	{`"😀".includes("😀".slice(0, 1))`, func() value.Value { return str("😀").Includes(str("😀").Slice(num(0), num(1))) }},

	// array methods
	{`[1,2,3].length`, func() value.Value { return arr(num(1), num(2), num(3)).Get("length") }},
	{`[1,2,3][1]`, func() value.Value { return arr(num(1), num(2), num(3)).Index(1) }},
	{`[1,2,3][5]`, func() value.Value { return arr(num(1), num(2), num(3)).Index(5) }},
	{`[1,2].push(3, 4)`, func() value.Value { return arr(num(1), num(2)).Push(num(3), num(4)) }},
	{`[1,2].pop()`, func() value.Value { return arr(num(1), num(2)).Pop() }},
	{`[].pop()`, func() value.Value { return arr().Pop() }},
	{`[1,2].shift()`, func() value.Value { return arr(num(1), num(2)).Shift() }},
	{`[].shift()`, func() value.Value { return arr().Shift() }},
	{`[1,2].unshift(0)`, func() value.Value { return arr(num(1), num(2)).Unshift(num(0)) }},
	{`[].unshift()`, func() value.Value { return arr().Unshift() }},
	{`[1,2,3].join("-")`, func() value.Value { return arr(num(1), num(2), num(3)).Join(str("-")) }},
	{`[1,2,3].join()`, func() value.Value { return arr(num(1), num(2), num(3)).Join() }},
	{`[1,null,undefined,2].join()`, func() value.Value { return arr(num(1), value.Null(), value.Undefined(), num(2)).Join() }},
	{`[[1,2],[3]].join()`, func() value.Value { return arr(arr(num(1), num(2)), arr(num(3))).Join() }},
	{`String([null,[undefined]])`, func() value.Value { return arr(value.Null(), arr(value.Undefined())).ToString() }},
	{`[1,2,3].reverse()`, func() value.Value { return arr(num(1), num(2), num(3)).Reverse() }},
	{`[1,2,3].slice(1)`, func() value.Value { return arr(num(1), num(2), num(3)).Slice(num(1)) }},
	{`[1,2,3].slice(-2, -1)`, func() value.Value { return arr(num(1), num(2), num(3)).Slice(num(-2), num(-1)) }},
	{`[1,2,3].slice(2, 1)`, func() value.Value { return arr(num(1), num(2), num(3)).Slice(num(2), num(1)) }},
	{`[1,2,3].indexOf(2)`, func() value.Value { return arr(num(1), num(2), num(3)).IndexOf(num(2)) }},
	{`[1,2,3].indexOf("2")`, func() value.Value { return arr(num(1), num(2), num(3)).IndexOf(str("2")) }},
	{`[1, "1"].indexOf("1")`, func() value.Value { return arr(num(1), str("1")).IndexOf(str("1")) }},
	{`[1,2,1].lastIndexOf(1)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1)) }},
	{`[1,2,1].lastIndexOf(1, 1)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1), num(1)) }},
	{`[1,2,1].lastIndexOf(1, -4)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1), num(-4)) }},
	{`[1,2,1].lastIndexOf(1, -Infinity)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1), num(math.Inf(-1))) }},
	{`[1,2,1].lastIndexOf(1, undefined)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1), value.Undefined()) }},
	{`[1,2,1].lastIndexOf(1, 5)`, func() value.Value { return arr(num(1), num(2), num(1)).LastIndexOf(num(1), num(5)) }},
	{`[1,2,3].includes(3)`, func() value.Value { return arr(num(1), num(2), num(3)).Includes(num(3)) }},
	{`[NaN].includes(NaN)`, func() value.Value { return arr(nan).Includes(nan) }},
	{`[NaN].indexOf(NaN)`, func() value.Value { return arr(nan).IndexOf(nan) }},
	{`[0].includes(-0)`, func() value.Value { return arr(num(0)).Includes(num(math.Copysign(0, -1))) }},
	{`[1,2].includes(1, 1)`, func() value.Value { return arr(num(1), num(2)).Includes(num(1), num(1)) }},
	{`[[1]].includes([1])`, func() value.Value { return arr(arr(num(1))).Includes(arr(num(1))) }},
	{`[1,2,3].at(-1)`, func() value.Value { return arr(num(1), num(2), num(3)).At(num(-1)) }},
	{`[1,2,3].at(1.5)`, func() value.Value { return arr(num(1), num(2), num(3)).At(num(1.5)) }},
	{`[1].concat([2, 3], 4)`, func() value.Value { return arr(num(1)).Concat(arr(num(2), num(3)), num(4)) }},
	{`[1,[2,[3,[4]]]].flat()`, func() value.Value { return arr(num(1), arr(num(2), arr(num(3), arr(num(4))))).Flat() }},
	{`[1,[2,[3,[4]]]].flat(Infinity)`, func() value.Value { return arr(num(1), arr(num(2), arr(num(3), arr(num(4))))).Flat(num(math.Inf(1))) }},
	{`[1,[2,[3]]].flat(0).length`, func() value.Value { return arr(num(1), arr(num(2), arr(num(3)))).Flat(num(0)).Get("length") }},
	{`[1,[2,[3]]].flat("2").length`, func() value.Value { return arr(num(1), arr(num(2), arr(num(3)))).Flat(str("2")).Get("length") }},
	{`[1,2].toString()`, func() value.Value { return arr(num(1), num(2)).ToString() }},
	{`String([])`, func() value.Value { return arr().ToString() }},
	{`String({})`, func() value.Value { return value.Object().ToString() }},
	{`[1,2,3,4,5].splice(1, 2).join()`, func() value.Value { return arr(num(1), num(2), num(3), num(4), num(5)).Splice(num(1), num(2)).Join() }},
	{`(() => { const a = [1,2,3,4,5]; a.splice(1, 2, "x", "y", "z"); return a.join() })()`, func() value.Value {
		a := arr(num(1), num(2), num(3), num(4), num(5))
		a.Splice(num(1), num(2), str("x"), str("y"), str("z"))
		return a.Join()
	}},
	{`(() => { const a = [1,2,3]; a.splice(-1); return a.join() })()`, func() value.Value {
		a := arr(num(1), num(2), num(3))
		a.Splice(num(-1))
		return a.Join()
	}},
	{`(() => { const a = [1,2,3]; a.splice(); return a.join() })()`, func() value.Value {
		a := arr(num(1), num(2), num(3))
		a.Splice()
		return a.Join()
	}},
	{`[1,2,3].splice(1, -5).length`, func() value.Value { return arr(num(1), num(2), num(3)).Splice(num(1), num(-5)).Get("length") }},
	{`[1,2,3].fill(0).join()`, func() value.Value { return arr(num(1), num(2), num(3)).Fill(num(0)).Join() }},
	{`[1,2,3,4].fill(9, 1, -1).join()`, func() value.Value { return arr(num(1), num(2), num(3), num(4)).Fill(num(9), num(1), num(-1)).Join() }},
	{`[1,2,3].fill("x", -1).join()`, func() value.Value { return arr(num(1), num(2), num(3)).Fill(str("x"), num(-1)).Join() }},

	// cycles
	{`(() => { const a = [1]; a.push(a); return String(a) })()`, func() value.Value {
		a := arr(num(1))
		a.Push(a)
		return a.ToString()
	}},
	{`(() => { const a = [1]; a.push(a); return Number(a) })()`, func() value.Value {
		a := arr(num(1))
		a.Push(a)
		return a.ToNumber()
	}},
	{`(() => { const a = [1]; a.push(a); return a == 1 })()`, func() value.Value {
		a := arr(num(1))
		a.Push(a)
		return value.Boolean(a.LooseEqual(num(1)))
	}},
	{`(() => { const a = [1]; a.push(a); return a.flat(Infinity) })()`, func() value.Value {
		a := arr(num(1))
		a.Push(a)
		return a.Flat(num(math.Inf(1)))
	}},

	// callbacks: fn1 is () => 1, double is x => x * 2, isEven is x => x % 2 === 0, plus is (a, b) => a + b
	{`[1,2,3].map(x => x * 2).join()`, func() value.Value { return arr(num(1), num(2), num(3)).Map(double).Join() }},
	{`[1,2,3].map((x, i) => x * i).join()`, func() value.Value { return arr(num(1), num(2), num(3)).Map(timesIndex).Join() }},
	{`[1,2,3,4].filter(x => x % 2 === 0).join()`, func() value.Value { return arr(num(1), num(2), num(3), num(4)).Filter(isEven).Join() }},
	{`[1,2,3,4].find(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(2), num(3), num(4)).Find(isEven) }},
	{`[1,3].find(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(3)).Find(isEven) }},
	{`[1,2,3,4].findIndex(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(2), num(3), num(4)).FindIndex(isEven) }},
	{`[1,3].findIndex(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(3)).FindIndex(isEven) }},
	{`[1,3].some(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(3)).Some(isEven) }},
	{`[1,2].some(x => x % 2 === 0)`, func() value.Value { return arr(num(1), num(2)).Some(isEven) }},
	{`[2,4].every(x => x % 2 === 0)`, func() value.Value { return arr(num(2), num(4)).Every(isEven) }},
	{`[2,3].every(x => x % 2 === 0)`, func() value.Value { return arr(num(2), num(3)).Every(isEven) }},
	{`[].every(x => x % 2 === 0)`, func() value.Value { return arr().Every(isEven) }},
	{`[1,2,3].reduce((a, b) => a + b)`, func() value.Value { return arr(num(1), num(2), num(3)).Reduce(plus) }},
	{`[1,2,3].reduce((a, b) => a + b, 10)`, func() value.Value { return arr(num(1), num(2), num(3)).Reduce(plus, num(10)) }},
	{`[].reduce((a, b) => a + b, "x")`, func() value.Value { return arr().Reduce(plus, str("x")) }},
	{`[].reduce((a, b) => a + b)`, func() value.Value { return arr().Reduce(plus) }},
	{`["b", "a"].reduce((a, b) => a + b)`, func() value.Value { return arr(str("b"), str("a")).Reduce(plus) }},
	{`[[1], [2, [3]]].flatMap(x => x).join()`, func() value.Value { return arr(arr(num(1)), arr(num(2), arr(num(3)))).FlatMap(identity).Join() }},
	{`[10, 9, 1, 100].sort().join()`, func() value.Value { return arr(num(10), num(9), num(1), num(100)).Sort().Join() }},
	{`[10, 9, 1, 100].sort((a, b) => a - b).join()`, func() value.Value { return arr(num(10), num(9), num(1), num(100)).Sort(minus).Join() }},
	{`["b", undefined, "a", null].sort().join()`, func() value.Value { return arr(str("b"), value.Undefined(), str("a"), value.Null()).Sort().Join() }},
	{`["Phường", "Phuong", "phuong"].sort().join()`, func() value.Value { return arr(str("Phường"), str("Phuong"), str("phuong")).Sort().Join() }},
	{`[3,1,2].sort(() => 0).join()`, func() value.Value { return arr(num(3), num(1), num(2)).Sort(zero).Join() }},
	{`(() => { let s = 0; [1,2,3].forEach(x => { s += x }); return s })()`, func() value.Value {
		sum := 0.0
		arr(num(1), num(2), num(3)).ForEach(value.Function(func(a ...value.Value) value.Value { sum += a[0].Number(); return value.Undefined() }))
		return num(sum)
	}},
	{`[1,2].forEach(x => x)`, func() value.Value { return arr(num(1), num(2)).ForEach(identity) }},
	{`[1].map(5)`, func() value.Value { return arr(num(1)).Map(num(5)) }},
	{`[].map(5).length`, func() value.Value { return arr().Map(num(5)).Get("length") }},

	// objects
	{`({a: 1}).a`, func() value.Value { return value.Object().Set("a", num(1)).Get("a") }},
	{`({a: 1}).b`, func() value.Value { return value.Object().Set("a", num(1)).Get("b") }},
	{`Object.keys({zeta: 1, alpha: 2, zeta: 3}).join()`, func() value.Value {
		o := value.Object().Set("zeta", num(1)).Set("alpha", num(2)).Set("zeta", num(3))
		return keysOf(o)
	}},
	{`Object.keys({b: 1, 2: 1, a: 1, 1: 1, "01": 1, 4294967295: 1}).join()`, func() value.Value {
		o := value.Object()
		for _, k := range []string{"b", "2", "a", "1", "01", "4294967295"} {
			o.Set(k, num(1))
		}
		return keysOf(o)
	}},
	{`({greet: () => "hi"}).greet()`, func() value.Value {
		return value.Object().Set("greet", value.Function(func(...value.Value) value.Value { return str("hi") })).Invoke("greet", nil)
	}},
	{`({greet: 1}).greet()`, func() value.Value { return value.Object().Set("greet", num(1)).Invoke("greet", nil) }},
	{`null.toString()`, func() value.Value { return value.Null().Invoke("toString", nil) }},
	{`undefined.length`, func() value.Value { return value.Undefined().Get("length") }},
	{`null[0]`, func() value.Value { return value.Null().Index(0) }},

	// objects made primitive through their own valueOf and toString
	{`({ valueOf: () => 42 }) + 1`, func() value.Value { return ownValueOf(num(42)).Add(num(1)) }},
	{`({ valueOf: () => 42 }) * 2`, func() value.Value { return ownValueOf(num(42)).Multiply(num(2)) }},
	{`({ valueOf: () => 5 }) > 3`, func() value.Value { return value.Boolean(ownValueOf(num(5)).Greater(num(3))) }},
	{`({ valueOf: () => 5 }) == 5`, func() value.Value { return value.Boolean(ownValueOf(num(5)).LooseEqual(num(5))) }},
	{`+({ valueOf: () => "7" })`, func() value.Value { return ownValueOf(str("7")).ToNumber() }},
	{`-({ valueOf: () => 2 })`, func() value.Value { return ownValueOf(num(2)).Negate() }},
	{`String({ toString: () => "x" })`, func() value.Value { return ownToString(str("x")).ToString() }},
	{`String({ valueOf: () => 42 })`, func() value.Value { return ownValueOf(num(42)).ToString() }},
	{`({ toString: () => "x" }) + 1`, func() value.Value { return ownToString(str("x")).Add(num(1)) }},
	{`[{ toString: () => "a" }, 2].join()`, func() value.Value { return arr(ownToString(str("a")), num(2)).Join() }},
	{`({ toString: 1, valueOf: 2 }) + 1`, func() value.Value {
		return value.Object().Set("toString", num(1)).Set("valueOf", num(2)).Add(num(1))
	}},
	{`({ toString: () => ({}), valueOf: () => ({}) }) + 1`, func() value.Value {
		empty := value.Function(func(...value.Value) value.Value { return value.Object() })
		return value.Object().Set("toString", empty).Set("valueOf", empty).Add(num(1))
	}},
	{`({ valueOf: () => ({}), toString: () => "s" }) + 1`, func() value.Value {
		return value.Object().Set("valueOf", value.Function(func(...value.Value) value.Value { return value.Object() })).
			Set("toString", value.Function(func(...value.Value) value.Value { return str("s") })).Add(num(1))
	}},
	{`({ valueOf: () => 1 }).toString()`, func() value.Value { return ownValueOf(num(1)).Invoke("toString", nil) }},

	// JSON
	{`JSON.stringify({b: 1, a: [1, "x", null, undefined, () => 1], c: undefined, d: {}})`, func() value.Value {
		o := value.Object().Set("b", num(1)).Set("a", arr(num(1), str("x"), value.Null(), value.Undefined(), fn1)).Set("c", value.Undefined()).Set("d", value.Object())
		return value.Stringify(o, "")
	}},
	{`JSON.stringify([NaN, Infinity, -0, 1e21, 0.1])`, func() value.Value {
		return value.Stringify(arr(nan, num(math.Inf(1)), num(math.Copysign(0, -1)), num(1e21), num(0.1)), "")
	}},
	{`JSON.stringify("a\"b" + String.fromCharCode(92) + "c\n\t" + String.fromCharCode(1) + "é😀")`, func() value.Value {
		return value.Stringify(str("a\"b"+backslash+"c\n\t\x01é😀"), "")
	}},
	{`JSON.stringify("😀".slice(0, 1))`, func() value.Value { return value.Stringify(str("😀").Slice(num(0), num(1)), "") }},
	{`JSON.stringify(undefined)`, func() value.Value { return value.Stringify(value.Undefined(), "") }},
	{`JSON.stringify(() => 1)`, func() value.Value { return value.Stringify(fn1, "") }},
	{`JSON.stringify(null)`, func() value.Value { return value.Stringify(value.Null(), "") }},
	{`JSON.stringify([])`, func() value.Value { return value.Stringify(arr(), "") }},
	{`JSON.stringify({a: {b: [1, {c: 2}]}}, null, 2)`, func() value.Value {
		return value.Stringify(value.Object().Set("a", value.Object().Set("b", arr(num(1), value.Object().Set("c", num(2))))), "  ")
	}},
	{`JSON.stringify({2: "b", 1: "a", x: 0})`, func() value.Value {
		return value.Stringify(value.Object().Set("2", str("b")).Set("1", str("a")).Set("x", num(0)), "")
	}},
	{`(() => { const a = [1]; a.push(a); return JSON.stringify(a) })()`, func() value.Value {
		a := arr(num(1))
		a.Push(a)
		return value.Stringify(a, "")
	}},
	{`JSON.parse('{"b": [1, 2.5e2, "x' + String.fromCharCode(92) + 'u00e9' + String.fromCharCode(92) + 'ud83d' + String.fromCharCode(92) + 'ude00", true, null], "a": {}}').b.join()`, func() value.Value {
		return value.ParseJSON(`{"b": [1, 2.5e2, "x` + backslash + `u00e9` + backslash + `ud83d` + backslash + `ude00", true, null], "a": {}}`).Get("b").Join()
	}},
	{`Object.keys(JSON.parse('{"b": 1, "a": 2, "1": 3}')).join()`, func() value.Value {
		return keysOf(value.ParseJSON(`{"b": 1, "a": 2, "1": 3}`))
	}},
	{`JSON.parse(' 42 ')`, func() value.Value { return value.ParseJSON(" 42 ") }},
	{`JSON.parse('"' + String.fromCharCode(92) + 'ud83d"').length`, func() value.Value {
		return value.ParseJSON(`"` + backslash + `ud83d"`).Get("length")
	}},
	{`JSON.parse('{"a":1,}')`, func() value.Value { return value.ParseJSON(`{"a":1,}`) }},
	{`JSON.parse('01')`, func() value.Value { return value.ParseJSON(`01`) }},
	{`JSON.parse('')`, func() value.Value { return value.ParseJSON(``) }},
	{`JSON.parse('[1] x')`, func() value.Value { return value.ParseJSON(`[1] x`) }},
	{`JSON.parse(JSON.stringify({a: [1, "x", {b: null}]})).a[2].b`, func() value.Value {
		o := value.Object().Set("a", arr(num(1), str("x"), value.Object().Set("b", value.Null())))
		return value.ParseJSON(value.Stringify(o, "").Text()).Get("a").Index(2).Get("b")
	}},

	// a method read without a call
	{`typeof "abc".at`, func() value.Value { return str("abc").Get("at").Typeof() }},
	{`typeof "abc".map`, func() value.Value { return str("abc").Get("map").Typeof() }},
	{`typeof [3, 1, 2].map`, func() value.Value { return arr(num(3), num(1), num(2)).Get("map").Typeof() }},
	{`[3, 1, 2].map ? "yes" : "no"`, func() value.Value {
		if arr(num(3), num(1), num(2)).Get("map").ToBoolean() {
			return str("yes")
		}
		return str("no")
	}},
	{`typeof [].trim`, func() value.Value { return arr().Get("trim").Typeof() }},
	{`typeof (12.5).toPrecision`, func() value.Value { return num(12.5).Get("toPrecision").Typeof() }},
	{`typeof (12.5).at`, func() value.Value { return num(12.5).Get("at").Typeof() }},
	{`typeof true.valueOf`, func() value.Value { return yes.Get("valueOf").Typeof() }},
	{`typeof ({}).hasOwnProperty`, func() value.Value { return value.Object().Get("hasOwnProperty").Typeof() }},
	{`typeof ({}).map`, func() value.Value { return value.Object().Get("map").Typeof() }},
	{`({at: 1}).at`, func() value.Value { return value.Object().Set("at", num(1)).Get("at") }},
	{`({toString: 5}).toString`, func() value.Value { return value.Object().Set("toString", num(5)).Get("toString") }},
	// a method called on the value it is read off is the value's call, Invoke, as the compiler makes x["m"](…) and
	// x?.m?.(…) (18/11/2026); read off and called apart, it has no value to work on, as JavaScript's has none
	{`"abc"["toUpperCase"]()`, func() value.Value { return str("abc").Invoke("toUpperCase", nil) }},
	{`"abc"?.at?.(-1)`, func() value.Value { return str("abc").Invoke("at", []value.Value{num(-1)}) }},
	{`[1, 2]?.at?.(-1)`, func() value.Value { return arr(num(1), num(2)).Invoke("at", []value.Value{num(-1)}) }},
	{`(() => { const a = [1]; a?.push?.(2); return a.join() })()`, func() value.Value {
		a := arr(num(1))
		a.Invoke("push", []value.Value{num(2)})
		return a.Join()
	}},
	{`({})["toString"]()`, func() value.Value { return value.Object().Invoke("toString", nil) }},
	{`({a: 1})["hasOwnProperty"]("a")`, func() value.Value {
		return value.Object().Set("a", num(1)).Invoke("hasOwnProperty", []value.Value{str("a")})
	}},
	{`(() => { const up = "abc".toUpperCase; return up() })()`, func() value.Value { return str("abc").Get("toUpperCase").Call(nil) }},
	{`(() => { const at = [1, 2].at; return at(0) })()`, func() value.Value { return arr(num(1), num(2)).Get("at").Call([]value.Value{num(0)}) }},
	{`[3, 1, 2].map === [1].map`, func() value.Value {
		return value.Boolean(arr(num(3), num(1), num(2)).Get("map").Equal(arr(num(1)).Get("map")))
	}},
	{`[].concat === "".concat`, func() value.Value { return value.Boolean(arr().Get("concat").Equal(str("").Get("concat"))) }},
	{`"a".trimLeft === "b".trimStart`, func() value.Value { return value.Boolean(str("a").Get("trimLeft").Equal(str("b").Get("trimStart"))) }},
	{`({}).valueOf === [].valueOf`, func() value.Value { return value.Boolean(value.Object().Get("valueOf").Equal(arr().Get("valueOf"))) }},
	{`({}).toString === [].toString`, func() value.Value { return value.Boolean(value.Object().Get("toString").Equal(arr().Get("toString"))) }},
	{`(1).toString === (2).toString`, func() value.Value { return value.Boolean(num(1).Get("toString").Equal(num(2).Get("toString"))) }},
	{`"a".hasOwnProperty === (5).hasOwnProperty`, func() value.Value {
		return value.Boolean(str("a").Get("hasOwnProperty").Equal(num(5).Get("hasOwnProperty")))
	}},
	{`"a".trimLeft.name`, func() value.Value { return str("a").Get("trimLeft").Get("name") }},
	{`"a".trimRight.name`, func() value.Value { return str("a").Get("trimRight").Get("name") }},
	{`[].push.length`, func() value.Value { return arr().Get("push").Get("length") }},
	{`"abc".slice.length`, func() value.Value { return str("abc").Get("slice").Get("length") }},
	{`(1).toString.length`, func() value.Value { return num(1).Get("toString").Get("length") }},
	{`[].with.name`, func() value.Value { return arr().Get("with").Get("name") }},

	// an index read by its text
	{`[1, 2]["0"]`, func() value.Value { return arr(num(1), num(2)).Get("0") }},
	{`[1, 2]["1"]`, func() value.Value { return arr(num(1), num(2)).Get("1") }},
	{`[1, 2]["2"]`, func() value.Value { return arr(num(1), num(2)).Get("2") }},
	{`[1, 2]["01"]`, func() value.Value { return arr(num(1), num(2)).Get("01") }},
	{`[1, 2]["1.0"]`, func() value.Value { return arr(num(1), num(2)).Get("1.0") }},
	{`"abc"["1"]`, func() value.Value { return str("abc").Get("1") }},
	{`"abc"["-1"]`, func() value.Value { return str("abc").Get("-1") }},
	{`"😀"["0"].charCodeAt(0)`, func() value.Value { return str("😀").Get("0").CharCodeAt(num(0)) }},

	// a function's length and name
	{`((x) => x).length`, func() value.Value {
		return value.Closure(&value.Lambda{Name: "<anonymous>", Params: []string{"x"}}).Get("length")
	}},
	{`((a, b) => a).length`, func() value.Value {
		return value.Closure(&value.Lambda{Name: "<anonymous>", Params: []string{"a", "b"}}).Get("length")
	}},
	{`((x) => x).name`, func() value.Value {
		return value.Closure(&value.Lambda{Name: "<anonymous>", Params: []string{"x"}}).Get("name")
	}},
	{`(() => { const sum = (a, b) => a + b; return sum.name })()`, func() value.Value {
		return value.Closure(&value.Lambda{Name: "sum", Params: []string{"a", "b"}}).Get("name")
	}},
	{`(() => 1).length`, func() value.Value { return fn1.Get("length") }},
	{`(() => 1).name`, func() value.Value { return fn1.Get("name") }},
	{`Math.max.name`, func() value.Value {
		return value.NamedFunction("max", 2, func(...value.Value) value.Value { return num(0) }).Get("name")
	}},
	{`Math.max.length`, func() value.Value {
		return value.NamedFunction("max", 2, func(...value.Value) value.Value { return num(0) }).Get("length")
	}},

	// substr, codePointAt, isWellFormed, toWellFormed
	{`"abc".substr(-2, 1)`, func() value.Value { return str("abc").Substr(num(-2), num(1)) }},
	{`"abc".substr(1)`, func() value.Value { return str("abc").Substr(num(1)) }},
	{`"abc".substr(1, -1)`, func() value.Value { return str("abc").Substr(num(1), num(-1)) }},
	{`"abc".substr(-5)`, func() value.Value { return str("abc").Substr(num(-5)) }},
	{`"abc".substr()`, func() value.Value { return str("abc").Substr() }},
	{`"abc".substr(NaN, 2)`, func() value.Value { return str("abc").Substr(nan, num(2)) }},
	{`"abc".substr(1, Infinity)`, func() value.Value { return str("abc").Substr(num(1), num(math.Inf(1))) }},
	{`"a-B-a".substr("a", 1)`, func() value.Value { return str("a-B-a").Substr(str("a"), num(1)) }},
	{`"a-B-a".substr("a", "x")`, func() value.Value { return str("a-B-a").Substr(str("a"), str("x")) }},
	{`"Phường".substr(2, 3)`, func() value.Value { return str("Phường").Substr(num(2), num(3)) }},
	{`"😀x".substr(1).length`, func() value.Value { return str("😀x").Substr(num(1)).Get("length") }},
	{`"abc".codePointAt(0)`, func() value.Value { return str("abc").CodePointAt(num(0)) }},
	{`"abc".codePointAt()`, func() value.Value { return str("abc").CodePointAt() }},
	{`"abc".codePointAt(5)`, func() value.Value { return str("abc").CodePointAt(num(5)) }},
	{`"abc".codePointAt(-1)`, func() value.Value { return str("abc").CodePointAt(num(-1)) }},
	{`"😀".codePointAt(0)`, func() value.Value { return str("😀").CodePointAt(num(0)) }},
	{`"😀".codePointAt(1)`, func() value.Value { return str("😀").CodePointAt(num(1)) }},
	{`"x😀".codePointAt(1.9)`, func() value.Value { return str("x😀").CodePointAt(num(1.9)) }},
	{`"Phường".codePointAt(2)`, func() value.Value { return str("Phường").CodePointAt(num(2)) }},
	{`"😀".slice(0, 1).codePointAt(0)`, func() value.Value { return str("😀").Slice(num(0), num(1)).CodePointAt(num(0)) }},
	{`"a".isWellFormed()`, func() value.Value { return str("a").IsWellFormed() }},
	{`"😀".isWellFormed()`, func() value.Value { return str("😀").IsWellFormed() }},
	{`"😀".slice(0, 1).isWellFormed()`, func() value.Value { return str("😀").Slice(num(0), num(1)).IsWellFormed() }},
	{`"한글".isWellFormed()`, func() value.Value { return str("한글").IsWellFormed() }},
	{`("a" + "😀".slice(0, 1)).toWellFormed()`, func() value.Value { return str("a").Add(str("😀").Slice(num(0), num(1))).ToWellFormed() }},
	{`("😀".slice(1) + "😀".slice(0, 1)).toWellFormed().length`, func() value.Value {
		return str("😀").Slice(num(1)).Add(str("😀").Slice(num(0), num(1))).ToWellFormed().Get("length")
	}},
	{`("😀".slice(1) + "😀".slice(0, 1)).toWellFormed().charCodeAt(1)`, func() value.Value {
		return str("😀").Slice(num(1)).Add(str("😀").Slice(num(0), num(1))).ToWellFormed().CharCodeAt(num(1))
	}},
	{`"😀x".toWellFormed()`, func() value.Value { return str("😀x").ToWellFormed() }},

	// a needle that begins with 0xED and is no surrogate (U+D000–U+D7FF) once never returned
	{`"abc".indexOf("한")`, func() value.Value { return str("abc").IndexOf(str("한")) }},
	{`"한글".includes("글")`, func() value.Value { return str("한글").Includes(str("글")) }},
	{`"x한".startsWith("한", 1)`, func() value.Value { return str("x한").StartsWith(str("한"), num(1)) }},
	{`"한x한".lastIndexOf("한")`, func() value.Value { return str("한x한").LastIndexOf(str("한")) }},

	// toPrecision, toExponential
	{`(12.5).toPrecision()`, func() value.Value { return num(12.5).ToPrecision() }},
	{`(12.5).toPrecision(1)`, func() value.Value { return num(12.5).ToPrecision(num(1)) }},
	{`(12.5).toPrecision(2)`, func() value.Value { return num(12.5).ToPrecision(num(2)) }},
	{`(12.5).toPrecision(3)`, func() value.Value { return num(12.5).ToPrecision(num(3)) }},
	{`(12.5).toPrecision(16)`, func() value.Value { return num(12.5).ToPrecision(num(16)) }},
	{`(123456).toPrecision(2)`, func() value.Value { return num(123456).ToPrecision(num(2)) }},
	{`(123.456).toPrecision(4)`, func() value.Value { return num(123.456).ToPrecision(num(4)) }},
	{`(0.000123).toPrecision(2)`, func() value.Value { return num(0.000123).ToPrecision(num(2)) }},
	{`(1e21).toPrecision(3)`, func() value.Value { return num(1e21).ToPrecision(num(3)) }},
	{`(1.005).toPrecision(3)`, func() value.Value { return num(1.005).ToPrecision(num(3)) }},
	{`(0).toPrecision(5)`, func() value.Value { return num(0).ToPrecision(num(5)) }},
	{`(0).toPrecision(1)`, func() value.Value { return num(0).ToPrecision(num(1)) }},
	{`(-0).toPrecision(2)`, func() value.Value { return num(math.Copysign(0, -1)).ToPrecision(num(2)) }},
	{`(-1.5).toPrecision(1)`, func() value.Value { return num(-1.5).ToPrecision(num(1)) }},
	{`(0.1).toPrecision(21)`, func() value.Value { return num(0.1).ToPrecision(num(21)) }},
	{`(1e-7).toPrecision(1)`, func() value.Value { return num(1e-7).ToPrecision(num(1)) }},
	{`(0.000001).toPrecision(1)`, func() value.Value { return num(0.000001).ToPrecision(num(1)) }},
	{`(1e-7).toPrecision(17)`, func() value.Value { return num(1e-7).ToPrecision(num(17)) }},
	{`(25).toPrecision(1)`, func() value.Value { return num(25).ToPrecision(num(1)) }},
	{`(35).toPrecision(1)`, func() value.Value { return num(35).ToPrecision(num(1)) }},
	{`(99.99).toPrecision(3)`, func() value.Value { return num(99.99).ToPrecision(num(3)) }},
	{`(99.96).toPrecision(3)`, func() value.Value { return num(99.96).ToPrecision(num(3)) }},
	{`(5e-324).toPrecision(1)`, func() value.Value { return num(5e-324).ToPrecision(num(1)) }},
	{`(2.5e-323).toPrecision(2)`, func() value.Value { return num(2.5e-323).ToPrecision(num(2)) }},
	{`(1e23).toPrecision(17)`, func() value.Value { return num(1e23).ToPrecision(num(17)) }},
	{`(999.9999999999998).toPrecision(16)`, func() value.Value { return num(999.9999999999998).ToPrecision(num(16)) }},
	{`(9.999999999999999e-5).toPrecision(16)`, func() value.Value { return num(9.999999999999999e-5).ToPrecision(num(16)) }},
	{`(1.7976931348623157e308).toPrecision(100).length`, func() value.Value {
		return num(1.7976931348623157e308).ToPrecision(num(100)).Get("length")
	}},
	{`(12.5).toPrecision("2")`, func() value.Value { return num(12.5).ToPrecision(str("2")) }},
	{`(12.5).toPrecision(2.9)`, func() value.Value { return num(12.5).ToPrecision(num(2.9)) }},
	{`(1).toPrecision(0)`, func() value.Value { return num(1).ToPrecision(num(0)) }},
	{`(1).toPrecision(101)`, func() value.Value { return num(1).ToPrecision(num(101)) }},
	{`(NaN).toPrecision(200)`, func() value.Value { return nan.ToPrecision(num(200)) }},
	{`(-Infinity).toPrecision(2)`, func() value.Value { return num(math.Inf(-1)).ToPrecision(num(2)) }},
	{`(12.5).toExponential()`, func() value.Value { return num(12.5).ToExponential() }},
	{`(12.5).toExponential(0)`, func() value.Value { return num(12.5).ToExponential(num(0)) }},
	{`(12.5).toExponential(2)`, func() value.Value { return num(12.5).ToExponential(num(2)) }},
	{`(12.5).toExponential(16)`, func() value.Value { return num(12.5).ToExponential(num(16)) }},
	{`(-12.5).toExponential(1)`, func() value.Value { return num(-12.5).ToExponential(num(1)) }},
	{`(0).toExponential(2)`, func() value.Value { return num(0).ToExponential(num(2)) }},
	{`(0).toExponential()`, func() value.Value { return num(0).ToExponential() }},
	{`(-0).toExponential()`, func() value.Value { return num(math.Copysign(0, -1)).ToExponential() }},
	{`(1.25).toExponential(1)`, func() value.Value { return num(1.25).ToExponential(num(1)) }},
	{`(1.35).toExponential(1)`, func() value.Value { return num(1.35).ToExponential(num(1)) }},
	{`(0.00015).toExponential(1)`, func() value.Value { return num(0.00015).ToExponential(num(1)) }},
	{`(1.5e300).toExponential(3)`, func() value.Value { return num(1.5e300).ToExponential(num(3)) }},
	{`(5e-324).toExponential(3)`, func() value.Value { return num(5e-324).ToExponential(num(3)) }},
	{`(5e-324).toExponential()`, func() value.Value { return num(5e-324).ToExponential() }},
	{`(4e-320).toExponential(0)`, func() value.Value { return num(4e-320).ToExponential(num(0)) }},
	{`(123.456).toExponential(20)`, func() value.Value { return num(123.456).ToExponential(num(20)) }},
	{`(0.1).toExponential(20)`, func() value.Value { return num(0.1).ToExponential(num(20)) }},
	{`(99.99).toExponential(1)`, func() value.Value { return num(99.99).ToExponential(num(1)) }},
	{`(1e21).toExponential()`, func() value.Value { return num(1e21).ToExponential() }},
	{`(999999999999999900000).toExponential(15)`, func() value.Value { return num(999999999999999900000).ToExponential(num(15)) }},
	{`(1.7976931348623157e308).toExponential(16)`, func() value.Value { return num(1.7976931348623157e308).ToExponential(num(16)) }},
	{`(1).toExponential(-1)`, func() value.Value { return num(1).ToExponential(num(-1)) }},
	{`(1).toExponential(101)`, func() value.Value { return num(1).ToExponential(num(101)) }},
	{`(NaN).toExponential(-1)`, func() value.Value { return nan.ToExponential(num(-1)) }},
	{`(Infinity).toExponential()`, func() value.Value { return num(math.Inf(1)).ToExponential() }},

	// valueOf, hasOwnProperty
	{`(12.5).valueOf()`, func() value.Value { return num(12.5).ValueOf() }},
	{`"abc".valueOf()`, func() value.Value { return str("abc").ValueOf() }},
	{`true.valueOf()`, func() value.Value { return yes.ValueOf() }},
	{`[1, 2].valueOf()`, func() value.Value { return arr(num(1), num(2)).ValueOf() }},
	{`({a: 1}).valueOf()`, func() value.Value { return value.Object().Set("a", num(1)).ValueOf() }},
	{`(() => { const o = {a: 1}; return o.valueOf() === o })()`, func() value.Value {
		o := value.Object().Set("a", num(1))
		return value.Boolean(o.ValueOf().Equal(o))
	}},
	{`(() => { const f = () => 1; return f.valueOf() === f })()`, func() value.Value { return value.Boolean(fn1.ValueOf().Equal(fn1)) }},
	{`new Date(1700000000123).valueOf()`, func() value.Value { return value.Time(time.UnixMilli(1700000000123)).ValueOf() }},
	{`({a: 1}).hasOwnProperty("a")`, func() value.Value { return value.Object().Set("a", num(1)).HasOwnProperty(str("a")) }},
	{`({a: 1}).hasOwnProperty("toString")`, func() value.Value { return value.Object().Set("a", num(1)).HasOwnProperty(str("toString")) }},
	{`({a: 1}).hasOwnProperty()`, func() value.Value { return value.Object().Set("a", num(1)).HasOwnProperty() }},
	{`({undefined: 1}).hasOwnProperty()`, func() value.Value { return value.Object().Set("undefined", num(1)).HasOwnProperty() }},
	{`({7: 1}).hasOwnProperty(7)`, func() value.Value { return value.Object().Set("7", num(1)).HasOwnProperty(num(7)) }},
	{`[1, 2].hasOwnProperty("1")`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(str("1")) }},
	{`[1, 2].hasOwnProperty(1)`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(num(1)) }},
	{`[1, 2].hasOwnProperty(2)`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(num(2)) }},
	{`[1, 2].hasOwnProperty("01")`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(str("01")) }},
	{`[1, 2].hasOwnProperty("length")`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(str("length")) }},
	{`[1, 2].hasOwnProperty("map")`, func() value.Value { return arr(num(1), num(2)).HasOwnProperty(str("map")) }},
	{`"abc".hasOwnProperty(0)`, func() value.Value { return str("abc").HasOwnProperty(num(0)) }},
	{`"abc".hasOwnProperty(3)`, func() value.Value { return str("abc").HasOwnProperty(num(3)) }},
	{`"abc".hasOwnProperty("length")`, func() value.Value { return str("abc").HasOwnProperty(str("length")) }},
	{`"😀".hasOwnProperty(1)`, func() value.Value { return str("😀").HasOwnProperty(num(1)) }},
	{`(5).hasOwnProperty("x")`, func() value.Value { return num(5).HasOwnProperty(str("x")) }},
	{`true.hasOwnProperty("valueOf")`, func() value.Value { return yes.HasOwnProperty(str("valueOf")) }},
	{`((x) => x).hasOwnProperty("length")`, func() value.Value { return fn1.HasOwnProperty(str("length")) }},
	{`((x) => x).hasOwnProperty("name")`, func() value.Value { return fn1.HasOwnProperty(str("name")) }},
	{`((x) => x).hasOwnProperty("prototype")`, func() value.Value { return fn1.HasOwnProperty(str("prototype")) }},
	{`((x) => x).hasOwnProperty("toString")`, func() value.Value { return fn1.HasOwnProperty(str("toString")) }},

	// findLast, findLastIndex, reduceRight: greaterThanOne is x => x > 1, backwards is (a, b) => b - a
	{`[3, 1, 2].findLast((x) => x > 1)`, func() value.Value { return arr(num(3), num(1), num(2)).FindLast(greaterThanOne) }},
	{`[3, 1, 2].findLastIndex((x) => x > 1)`, func() value.Value { return arr(num(3), num(1), num(2)).FindLastIndex(greaterThanOne) }},
	{`[3, 1, 2].findLast((a, b) => b - a)`, func() value.Value { return arr(num(3), num(1), num(2)).FindLast(backwards) }},
	{`[3, 1, 2].findLastIndex((a, b) => b - a)`, func() value.Value { return arr(num(3), num(1), num(2)).FindLastIndex(backwards) }},
	{`[1, 0].findLast((x) => x > 1)`, func() value.Value { return arr(num(1), num(0)).FindLast(greaterThanOne) }},
	{`[1, 0].findLastIndex((x) => x > 1)`, func() value.Value { return arr(num(1), num(0)).FindLastIndex(greaterThanOne) }},
	{`[].findLastIndex((x) => x > 1)`, func() value.Value { return arr().FindLastIndex(greaterThanOne) }},
	{`[1].findLast(5)`, func() value.Value { return arr(num(1)).FindLast(num(5)) }},
	{`[].findLastIndex()`, func() value.Value { return arr().FindLastIndex() }},
	{`[1, 2, 3].reduceRight((a, b) => a + b)`, func() value.Value { return arr(num(1), num(2), num(3)).ReduceRight(plus) }},
	{`["a", "b", "c"].reduceRight((a, b) => a + b)`, func() value.Value { return arr(str("a"), str("b"), str("c")).ReduceRight(plus) }},
	{`[1, 2, 3].reduceRight((a, b) => a + b, "")`, func() value.Value { return arr(num(1), num(2), num(3)).ReduceRight(plus, str("")) }},
	{`[3, 1, 2].reduceRight((a, b) => b - a)`, func() value.Value { return arr(num(3), num(1), num(2)).ReduceRight(backwards) }},
	{`[].reduceRight((a, b) => a + b, "x")`, func() value.Value { return arr().ReduceRight(plus, str("x")) }},
	{`[].reduceRight((a, b) => a + b)`, func() value.Value { return arr().ReduceRight(plus) }},
	{`[1].reduceRight(5)`, func() value.Value { return arr(num(1)).ReduceRight(num(5)) }},

	// the copying methods leave the array as it was
	{`[10, 9, 1, 100].toSorted().join()`, func() value.Value { return arr(num(10), num(9), num(1), num(100)).ToSorted().Join() }},
	{`[10, 9, 1, 100].toSorted((a, b) => a - b).join()`, func() value.Value { return arr(num(10), num(9), num(1), num(100)).ToSorted(minus).Join() }},
	{`[3, 1, 2].toSorted((a, b) => b - a).join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSorted(backwards).Join() }},
	{`["b", undefined, "a"].toSorted().join()`, func() value.Value { return arr(str("b"), value.Undefined(), str("a")).ToSorted().Join() }},
	{`(() => { const a = [3, 1, 2]; a.toSorted(); return a.join() })()`, func() value.Value {
		a := arr(num(3), num(1), num(2))
		a.ToSorted()
		return a.Join()
	}},
	{`[3, 1, 2].toSorted(5)`, func() value.Value { return arr(num(3), num(1), num(2)).ToSorted(num(5)) }},
	{`[].toSorted().length`, func() value.Value { return arr().ToSorted().Get("length") }},
	{`[1, 2, 3].toReversed().join()`, func() value.Value { return arr(num(1), num(2), num(3)).ToReversed().Join() }},
	{`(() => { const a = [1, 2, 3]; a.toReversed(); return a.join() })()`, func() value.Value {
		a := arr(num(1), num(2), num(3))
		a.ToReversed()
		return a.Join()
	}},
	{`[].toReversed().length`, func() value.Value { return arr().ToReversed().Get("length") }},
	{`[3, 1, 2].toSpliced().join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced().Join() }},
	{`[3, 1, 2].toSpliced(1).join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(1)).Join() }},
	{`[3, 1, 2].toSpliced(1, 2).join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(1), num(2)).Join() }},
	{`[3, 1, 2].toSpliced("-").length`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(str("-")).Get("length") }},
	{`[3, 1, 2].toSpliced(0, 1, "x", "y").join()`, func() value.Value {
		return arr(num(3), num(1), num(2)).ToSpliced(num(0), num(1), str("x"), str("y")).Join()
	}},
	{`[3, 1, 2].toSpliced(-1, 0, 9).join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(-1), num(0), num(9)).Join() }},
	{`[3, 1, 2].toSpliced(1, -5, "z").join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(1), num(-5), str("z")).Join() }},
	{`[3, 1, 2].toSpliced(5, 1, "z").join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(5), num(1), str("z")).Join() }},
	{`[3, 1, 2].toSpliced(1, undefined).join()`, func() value.Value { return arr(num(3), num(1), num(2)).ToSpliced(num(1), value.Undefined()).Join() }},
	{`(() => { const a = [3, 1, 2]; a.toSpliced(0, 2); return a.join() })()`, func() value.Value {
		a := arr(num(3), num(1), num(2))
		a.ToSpliced(num(0), num(2))
		return a.Join()
	}},
	{`[3, 1, 2].with(1, 9).join()`, func() value.Value { return arr(num(3), num(1), num(2)).With(num(1), num(9)).Join() }},
	{`[3, 1, 2].with(-1, 9).join()`, func() value.Value { return arr(num(3), num(1), num(2)).With(num(-1), num(9)).Join() }},
	{`[3, 1, 2].with().join()`, func() value.Value { return arr(num(3), num(1), num(2)).With().Join() }},
	{`[3, 1, 2].with(3, 9)`, func() value.Value { return arr(num(3), num(1), num(2)).With(num(3), num(9)) }},
	{`[3, 1, 2].with(-4, 9)`, func() value.Value { return arr(num(3), num(1), num(2)).With(num(-4), num(9)) }},
	{`[].with(0, 1)`, func() value.Value { return arr().With(num(0), num(1)) }},
	{`(() => { const a = [3, 1, 2]; a.with(0, 7); return a.join() })()`, func() value.Value {
		a := arr(num(3), num(1), num(2))
		a.With(num(0), num(7))
		return a.Join()
	}},
	{`[3, 1, 2].copyWithin().join()`, func() value.Value { return arr(num(3), num(1), num(2)).CopyWithin().Join() }},
	{`[3, 1, 2].copyWithin(1).join()`, func() value.Value { return arr(num(3), num(1), num(2)).CopyWithin(num(1)).Join() }},
	{`[3, 1, 2].copyWithin(1, 2).join()`, func() value.Value { return arr(num(3), num(1), num(2)).CopyWithin(num(1), num(2)).Join() }},
	{`[1, 2, 3, 4, 5].copyWithin(0, 3).join()`, func() value.Value {
		return arr(num(1), num(2), num(3), num(4), num(5)).CopyWithin(num(0), num(3)).Join()
	}},
	{`[1, 2, 3, 4, 5].copyWithin(-2, -3, -1).join()`, func() value.Value {
		return arr(num(1), num(2), num(3), num(4), num(5)).CopyWithin(num(-2), num(-3), num(-1)).Join()
	}},
	{`[1, 2, 3, 4, 5].copyWithin(2, 0).join()`, func() value.Value {
		return arr(num(1), num(2), num(3), num(4), num(5)).CopyWithin(num(2), num(0)).Join()
	}},
	{`[1, 2, 3, 4, 5].copyWithin(0, 3, 2).join()`, func() value.Value {
		return arr(num(1), num(2), num(3), num(4), num(5)).CopyWithin(num(0), num(3), num(2)).Join()
	}},
	{`(() => { const a = [1, 2, 3]; return a.copyWithin(0, 1) === a })()`, func() value.Value {
		a := arr(num(1), num(2), num(3))
		return value.Boolean(a.CopyWithin(num(0), num(1)).Equal(a))
	}},
}

var (
	greaterThanOne = value.Function(func(a ...value.Value) value.Value { return value.Boolean(a[0].Greater(num(1))) })
	backwards      = value.Function(func(a ...value.Value) value.Value { return a[1].Subtract(a[0]) })
)

// ownValueOf is { valueOf: () => result }; ownToString is { toString: () => result }.
func ownValueOf(result value.Value) value.Value {
	return value.Object().Set("valueOf", value.Function(func(...value.Value) value.Value { return result }))
}

func ownToString(result value.Value) value.Value {
	return value.Object().Set("toString", value.Function(func(...value.Value) value.Value { return result }))
}

func keysOf(o value.Value) value.Value {
	keys := []value.Value{}
	for _, k := range o.Keys() {
		keys = append(keys, str(k))
	}
	return arr(keys...).Join()
}

// TestParityWithNode runs every case's JavaScript in node and compares text and typeof. A failure
// is compared as the error class node throws: TYPE is TypeError, RANGE RangeError, INVALID_INPUT
// SyntaxError.
func TestParityWithNode(t *testing.T) { agreeWithNode(t, cases) }

// TestMethodReadsAgreeWithNode reads every method a JavaScript-shaped kind lists, without calling it,
// and holds its typeof, name and length to node's: the lists in javascript.go are data, so a length
// or a name copied wrong there shows here.
func TestMethodReadsAgreeWithNode(t *testing.T) {
	samples := []struct {
		js     string
		sample value.Value
	}{
		{`"ab"`, str("ab")}, {`(1.5)`, num(1.5)}, {`true`, yes}, {`[1]`, arr(num(1))}, {`({})`, value.Object()}, {`(() => 1)`, fn1},
	}
	var reads []parity
	for _, s := range samples {
		for _, name := range s.sample.MethodNames() {
			read := s.sample.Get(name)
			reads = append(reads,
				parity{`typeof ` + s.js + `.` + name, func() value.Value { return read.Typeof() }},
				parity{s.js + `.` + name + `.name`, func() value.Value { return read.Get("name") }},
				parity{s.js + `.` + name + `.length`, func() value.Value { return read.Get("length") }},
			)
		}
	}
	agreeWithNode(t, reads)
}

func agreeWithNode(t *testing.T, cases []parity) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	sources := make([]string, len(cases))
	for i, c := range cases {
		sources[i] = c.js
	}
	script := `
const sources = JSON.parse(process.argv[1]);
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
	raw, err := exec.Command("node", "-e", script, string(encoded)).Output()
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
			gotText, gotType = "throws "+strings.SplitN(got.Failure(), ":", 2)[0], "throws"
			if class, ok := classes[got.Code()]; ok {
				gotText = "throws " + class
			}
		}
		if gotText != expected[i].Text || gotType != expected[i].Type {
			failed++
			t.Errorf("%-40s node: %q (%s)   kitwork: %q (%s)", c.js, expected[i].Text, expected[i].Type, gotText, gotType)
		}
	}
	t.Logf("%d of %d cases agree with node", len(cases)-failed, len(cases))
}
