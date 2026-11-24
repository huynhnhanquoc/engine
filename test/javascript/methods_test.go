package javascript_test

import "testing"

// Mỗi ca là một lời gọi; node cho đáp án. Nhóm theo loại, trong nhóm theo tên phương thức.

func TestStringMethodsAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`"abc".at(0)`, `"abc".at(-1)`, `"abc".at(3)`, `"abc".at(-4)`, `"abc".at("1")`, `"abc".at(1.7)`, `"abc".at(NaN)`,
		`"😀a".at(0)`, `"Phường".at(-1)`, `"abc".at(true)`, `"abc".at(null)`, `"abc".at("  2  ")`, `"abc".at("0x1")`,
		`"abc".at("1e0")`, `"abc".at("1_0")`, `"abc".at([1])`, `"abc".at([1, 2])`, `"abc".at({valueOf: seven})`,
		`"abcdefgh".at({valueOf: seven})`, `"abc".at({toString: text})`, `"abc".at({valueOf: empty, toString: seven})`,
		`"abc".at({valueOf: empty, toString: empty})`, `"abc".at({valueOf: boom})`,
		`"abc".charAt(1)`, `"abc".charAt(5)`, `"abc".charAt(-1)`, `"Phường".charAt(3)`,
		`"😀".charCodeAt(0)`, `"😀".charCodeAt(1)`, `"😀".charCodeAt(2)`, `"ẹ".charCodeAt()`,
		`"😀".codePointAt(0)`, `"😀".codePointAt(1)`, `"a😀".codePointAt(1)`, `"a".codePointAt(1)`,
		`"abc".concat("d", 1, null, undefined, [1, 2])`, `"\uD83D".concat("\uDE00")`, `"".concat(1e21, -0, [1, [2, [3]]], {a: 1}, true)`,
		`"abc".endsWith("bc")`, `"abc".endsWith("b", 2)`, `"abc".endsWith("", 0)`, `"abc".endsWith("abc", -1)`, `"Phường".endsWith("ờng")`,
		`"😀".endsWith("\uDE00")`,
		`"abc".includes("b")`, `"abc".includes("b", 2)`, `"abc".includes("", 10)`, `"😀".includes("\uDE00")`, `"abc".includes()`,
		`"abcabc".indexOf("c")`, `"abcabc".indexOf("c", 3)`, `"abc".indexOf("", 10)`, `"abc".indexOf("x")`, `"ẹ😀ẹ".indexOf("ẹ", 1)`,
		`"😀😀".indexOf("\uDE00")`, `"한국한".indexOf("국")`, `"abc".indexOf({toString: text})`, `"undefined".indexOf()`,
		`"abcabc".lastIndexOf("c")`, `"abcabc".lastIndexOf("c", 4)`, `"abcabc".lastIndexOf("c", -5)`, `"abc".lastIndexOf("", 2)`,
		`"abcabc".lastIndexOf("c", NaN)`, `"ẹ😀ẹ".lastIndexOf("ẹ")`, `"ẹ😀ẹ".lastIndexOf("ẹ", 2)`, `"😀😀".lastIndexOf("\uD83D")`,
		`"a\uD83Db".isWellFormed()`, `"😀".isWellFormed()`, `"a\uD83Db".toWellFormed()`, `"\uDE00😀\uD83D".toWellFormed()`,
		`"abc".padStart(6)`, `"abc".padStart(6, "12")`, `"abc".padStart(2, "x")`, `"abc".padStart(6, "")`, `"abc".padStart(5, "😀")`,
		`"abc".padStart(4, "😀")`, `"abc".padEnd(7, "xy")`, `"abc".padEnd(NaN, "x")`, `"Phường".padEnd(8, ".")`,
		`"ab".repeat(3)`, `"ab".repeat(0)`, `"ab".repeat(-1)`, `"ab".repeat(Infinity)`, `"".repeat(Infinity)`, `"ab".repeat(2.9)`,
		`"\uDE00\uD83D".repeat(2)`,
		`"aXbXc".replace("X", "-")`, `"aXbXc".replace("X", "$&$&")`, "\"aXbXc\".replace(\"X\", \"[$`|$']\")", `"aXbXc".replace("X", "$$")`,
		`"aXbXc".replace("X", "$1$<x>$")`, `"aXbXc".replace("Y", "-")`, `"abc".replace("", "-")`, `"aXbXc".replace("X", upper)`,
		`"aXbXc".replace("X", position)`, `"aXbXc".replace("X", boom)`, `"aXb".replace("X")`, `"ẹXẹ".replace("X", "$'")`,
		`"aXbXc".replaceAll("X", "-")`, `"abc".replaceAll("", "-")`, `"aXbXc".replaceAll("X", position)`, `"xx".replaceAll("x", upper)`,
		`"😀😀".replaceAll("\uDE00", "x")`, `"Phường Phường".replaceAll("ờ", "o")`, `"aaa".replaceAll("aa", "b")`, `"ẹẹ".replaceAll("", "|")`,
		`"abcdef".slice(1, 3)`, `"abcdef".slice(-2)`, `"abcdef".slice(2, 1)`, `"abcdef".slice()`, `"abcdef".slice(-10, 100)`,
		`"abcdef".slice(NaN)`, `"😀".slice(0, 1)`, `"😀x".slice(1)`, `"Phường".slice(2, -1)`,
		`"a,b,,c".split(",")`, `"a,b,,c".split(",", 2)`, `"abc".split("")`, `"abc".split()`, `"abc".split("", 2)`, `"abc".split("x")`,
		`"".split(",")`, `"".split("")`, `"a,b".split(",", 0)`, `"a,b".split(",", -1)`, `"😀a".split("")`, `"Phường".split("ờ")`,
		`"a,b".split(undefined, 0)`, `"a1b1c".split(1)`, `"abc".split("abc")`,
		`"abc".startsWith("ab")`, `"abc".startsWith("b", 1)`, `"abc".startsWith("", 10)`, `"abc".startsWith("abcd")`, `"😀".startsWith("\uD83D")`,
		`"abcdef".substr(1, 3)`, `"abcdef".substr(-3)`, `"abcdef".substr(-3, 2)`, `"abcdef".substr(2)`, `"abcdef".substr(1, -1)`,
		`"abcdef".substr(10)`, `"abcdef".substr(-10, 2)`,
		`"abcdef".substring(4, 1)`, `"abcdef".substring(-1, 2)`, `"abcdef".substring(2)`, `"abcdef".substring(NaN, 3)`, `"😀".substring(1)`,
		`"Straße".toUpperCase()`, `"ΑΣ".toLowerCase()`, `"ΑΣ Α".toLowerCase()`, `"İ".toLowerCase()`, `"Phường".toUpperCase()`,
		`"ﬁ".toUpperCase()`, `"ŉ".toUpperCase()`, `"ABC".toLowerCase()`, `"\uD83Da".toUpperCase()`,
		`"  \t abc \n ".trim()`, `"  abc  ".trimStart()`, `"  abc  ".trimEnd()`, `"  abc  ".trimLeft()`, `"  abc  ".trimRight()`,
		`"\uFEFF\u00A0x\u2028\u3000".trim()`, `"\u0085x\u0085".trim()`, `"\u200Bx".trim()`,
		`"abc".toString()`, `"abc".valueOf()`,
		`"abc".hasOwnProperty("1")`, `"abc".hasOwnProperty("length")`, `"abc".hasOwnProperty("3")`, `"abc".hasOwnProperty("01")`,
		`"abc".hasOwnProperty(1)`,
		`"abc".slice({valueOf: boom})`, `"abc".map(double)`, `"abc".foo()`,
	})
}

func TestNumberAndBooleanMethodsAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`(0.1).toString()`, `(1e21).toString()`, `(1e-7).toString()`, `(123.456).toString()`, `(-0).toString()`, `(5e-324).toString()`,
		`(1.7976931348623157e308).toString()`, `(0.000001).toString()`, `(1234567.5).toString()`, `(100).toString()`, `(1e20).toString()`,
		`(-1.5e-10).toString()`, `(0.30000000000000004).toString()`, `(NaN).toString()`, `(-Infinity).toString()`,
		`(255).toString(16)`, `(255).toString(2)`, `(0.5).toString(2)`, `(-255).toString(36)`, `(0.1).toString(3)`, `(1e21).toString(16)`,
		`(255).toString(1)`, `(255).toString(37)`, `(Infinity).toString(2)`, `(NaN).toString(16)`, `(3.75).toString(8)`, `(255).toString(10.5)`,
		`(1.005).toFixed(2)`, `(1.25).toFixed(1)`, `(2.5).toFixed(0)`, `(0.5).toFixed(0)`, `(-1.5).toFixed(0)`, `(1e21).toFixed(2)`,
		`(123.456).toFixed()`, `(0).toFixed(2)`, `(-0).toFixed(2)`, `(1.45).toFixed(1)`, `(NaN).toFixed(2)`, `(1).toFixed(101)`,
		`(1).toFixed(-1)`, `(0.000001).toFixed(7)`, `(-0.0000001).toFixed(2)`, `(1e20).toFixed(2)`,
		`(123.456).toExponential(2)`, `(123.456).toExponential()`, `(0).toExponential()`, `(0).toExponential(2)`,
		`(-1.5e-10).toExponential(3)`, `(Infinity).toExponential(200)`, `(1).toExponential(101)`, `(5e-324).toExponential()`,
		`(1.25).toExponential(1)`,
		`(123.456).toPrecision(4)`, `(123.456).toPrecision()`, `(0.00012345).toPrecision(2)`, `(123456).toPrecision(2)`,
		`(1e-7).toPrecision(1)`, `(100).toPrecision(3)`, `(1).toPrecision(0)`, `(NaN).toPrecision(0)`, `(2.5).toPrecision(1)`,
		`(0).toPrecision(3)`, `(-0.000001).toPrecision(2)`,
		`(5).valueOf()`, `(5).hasOwnProperty("x")`, `(5).foo()`,
		`true.toString()`, `false.valueOf()`, `true.hasOwnProperty("x")`,
	})
}

func TestArrayMethodsAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`[1, 2, 3].at(-1)`, `[1, 2, 3].at(5)`, `[1, 2, 3].at(-4)`, `[1, 2, 3].at("1")`,
		`[1, 2].concat([3, 4], 5, [[6]])`, `[].concat()`,
		`[1, 2, 3, 4, 5].copyWithin(0, 3)`, `[1, 2, 3, 4, 5].copyWithin(1, 0, 3)`, `[1, 2, 3, 4, 5].copyWithin(-2)`,
		`[1, 2, 3, 4, 5].copyWithin(2, 0)`, `[1, 2, 3, 4, 5].copyWithin(0, -2, -1)`,
		`[1, 2, 3].every(isBig)`, `[3, 4].every(isBig)`, `[].every(isBig)`, `[1].every(5)`,
		`[1, 2, 3].fill(0)`, `[1, 2, 3].fill(0, 1)`, `[1, 2, 3].fill(9, -1)`, `[1, 2, 3].fill(7, 1, 2)`, `[1, 2, 3].fill()`,
		`[1, 2, 3, 4].filter(isEven)`, `[1, 2, 3].filter(identity)`, `[0, "", null, "a"].filter(identity)`,
		`[1, 2, 3, 4].find(isEven)`, `[1, 2, 3, 4].findIndex(isEven)`, `[1, 2, 3, 4].findLast(isEven)`, `[1, 2, 3, 4].findLastIndex(isEven)`,
		`[1, 2].find(isBig)`, `[1, 2].findIndex(isBig)`, `[1, 2].findLast(isBig)`, `[1, 2].findLastIndex(isBig)`,
		`[1, [2, [3, [4]]]].flat()`, `[1, [2, [3, [4]]]].flat(2)`, `[1, [2, [3, [4]]]].flat(Infinity)`, `[1, [2]].flat(0)`, `[1, [2]].flat(-1)`,
		`[1, 2].flatMap(pair)`, `[1, 2].flatMap(identity)`,
		`[1, 2, 3].forEach(double)`, `[1, 2, 3].forEach(shrink)`,
		`[1, 2, NaN].includes(NaN)`, `[1, 2, NaN].indexOf(NaN)`, `[1, 2, 3].includes(2, 2)`, `[1, 2, 3].includes(3, -1)`,
		`[1, 2, 3].includes(1, -100)`, `[].includes(undefined)`, `[0].includes(-0)`, `[-0].indexOf(0)`, `[1].includes(1, Infinity)`,
		`[1, 2, 3, 2].indexOf(2)`, `[1, 2, 3, 2].indexOf(2, 2)`, `[1, 2, 3, 2].indexOf(2, -1)`, `[1, 2].indexOf("2")`,
		`[1, 2, 3, 2].lastIndexOf(2)`, `[1, 2, 3, 2].lastIndexOf(2, 2)`, `[1, 2, 3, 2].lastIndexOf(2, -3)`, `[1, 2, 3, 2].lastIndexOf(2, undefined)`,
		`[1, 2, 3, 2].lastIndexOf(2, -10)`, `[2].lastIndexOf(2, -Infinity)`, `[].lastIndexOf(1)`,
		`[1, null, undefined, [2, 3], "a"].join()`, `[1, 2].join("-")`, `[1, 2].join(undefined)`, `[1, 2].join("")`, `[[1, [2]], []].join(";")`,
		`[1, 2, 3].map(double)`, `[1, 2, 3].map(index)`, `[1, 2, 3].map(shrink)`, `[1, 2].map(boom)`, `[1, 2].map(5)`,
		`[1, 2, 3].pop()`, `[].pop()`,
		`[1, 2].push(3, 4)`, `[1].push()`,
		`[1, 2, 3, 4].reduce(sum)`, `[1, 2, 3, 4].reduce(sum, 10)`, `[].reduce(sum)`, `[].reduce(sum, 1)`, `["a", "b", "c"].reduceRight(concat)`,
		`["a", "b", "c"].reduce(concat, "")`, `[1].reduce(boom)`, `[1, 2].reduce(boom)`,
		`[1, 2, 3].reverse()`, `[].reverse()`,
		`[1, 2, 3].shift()`, `[].shift()`,
		`[1, 2, 3, 4, 5].slice(1, 3)`, `[1, 2, 3, 4, 5].slice(-2)`, `[1, 2, 3, 4, 5].slice(3, 1)`, `[1, 2, 3].slice()`,
		`[1, 2, 3].some(isEven)`, `[1, 3].some(isEven)`,
		`[3, 1, 10, 2].sort()`, `[3, 1, 10, 2].sort(ascending)`, `[3, 1, 10, 2].sort(descending)`, `[3, undefined, 1].sort()`,
		`[3, undefined, 1].sort(descending)`, `["b", "a", "B", "ä", "😀", "\uFFFF", "\uD83D"].sort()`, `[1, 2, 3].sort(nan)`,
		`[2, 1].sort(boom)`, `[1, 2].sort(5)`, `[[2], [1, 0]].sort()`, `[{k: 1, v: "a"}, {k: 0, v: "b"}, {k: 1, v: "c"}].sort(byK)`,
		`[true, false, null, "null"].sort()`,
		`[1, 2, 3, 4, 5].splice(1, 2)`, `[1, 2, 3, 4, 5].splice(1, 2, "a", "b", "c")`, `[1, 2, 3, 4, 5].splice(-2)`, `[1, 2, 3].splice()`,
		`[1, 2, 3].splice(1)`, `[1, 2, 3].splice(1, -1)`, `[1, 2, 3].splice(10, 1, "z")`, `[1, 2, 3].splice(undefined)`,
		`[1, 2, 3].toReversed()`, `[3, 1, 2].toSorted(descending)`, `[3, 1, 2].toSorted()`, `[1, 2, 3].toSpliced(1, 1, "x")`,
		`[1, 2, 3].with(1, "x")`, `[1, 2, 3].with(-1, "x")`, `[1, 2, 3].with(3, "x")`, `[1, 2, 3].with(-4, "x")`,
		`[1, 2].toString()`, `[1, [2, 3]].toString()`, `[null].toString()`,
		`[1, 2, 3].unshift(0, -1)`,
		`[1, 2].hasOwnProperty("1")`, `[1, 2].hasOwnProperty("2")`, `[1, 2].hasOwnProperty("length")`, `[1].valueOf()`,
	})
}

func TestObjectAndOtherMethodsAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`({a: 1}).hasOwnProperty("a")`, `({a: 1}).hasOwnProperty("b")`, `({a: 1}).toString()`, `({a: 1}).valueOf()`,
		`({f: double}).f(4)`, `({f: 5}).f()`, `({toString: text}).toString()`, `({a: 1}).foo()`,
		`null.toString()`, `undefined.foo()`,
		`date(0).toISOString()`, `date(1795392000123).getTime()`, `date(1795392000123).valueOf()`, `date(1795392000123).toISOString()`,
		`date(-1).toISOString()`,
	})
}
