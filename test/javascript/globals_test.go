package javascript_test

import "testing"

// Math, JSON, Object đối chiếu với node: mỗi ca là một lời gọi Tên.hàm(đối số), chỉ kết quả được tả. Hàm có sẵn cũng
// đi làm hàm gọi lại ([-1].map(Math.abs)).

func TestMathIsJavaScripts(t *testing.T) {
	compare(t, []string{
		`Math.abs(-2)`, `Math.abs("-3.5")`, `Math.abs(-0)`, `Math.abs()`, `Math.abs(null)`, `Math.abs([-4])`, `Math.abs([1, 2])`,
		`Math.abs({valueOf: seven})`, `Math.abs({valueOf: boom})`, `Math.abs(-Infinity)`, `Math.abs("")`, `Math.abs(" 0x10 ")`,
		`Math.acos(0.5)`, `Math.acos(2)`, `Math.acos(-1)`, `Math.acosh(2)`, `Math.acosh(0.5)`, `Math.asin(0.3)`, `Math.asin(-0)`,
		`Math.asinh(1)`, `Math.asinh(-1e-300)`, `Math.atan(1)`, `Math.atan(-Infinity)`, `Math.atanh(0.5)`, `Math.atanh(1)`,
		`Math.atan2(1, 2)`, `Math.atan2(-0, -1)`, `Math.atan2(0, -0)`, `Math.atan2(-0, 0)`, `Math.atan2(Infinity, -Infinity)`,
		`Math.atan2(1)`, `Math.atan2({valueOf: boom}, {valueOf: seven})`,
		`Math.ceil(1.2)`, `Math.ceil(-0.5)`, `Math.ceil(-0)`, `Math.cbrt(27)`, `Math.cbrt(-2)`, `Math.cbrt(1e-310)`,
		`Math.expm1(1e-10)`, `Math.expm1(1)`, `Math.expm1(-Infinity)`,
		`Math.clz32(1)`, `Math.clz32(0)`, `Math.clz32(-1)`, `Math.clz32(4294967296)`, `Math.clz32(0.5)`, `Math.clz32(NaN)`,
		`Math.cos(2)`, `Math.cos(1e10)`, `Math.cos(0.7853981633974483)`, `Math.cos(Infinity)`, `Math.cosh(1)`, `Math.cosh(-710)`,
		`Math.exp(1)`, `Math.exp(-745.2)`, `Math.exp(709.8)`, `Math.exp(0.5)`,
		`Math.floor(-1.5)`, `Math.floor("x")`, `Math.floor(-0)`, `Math.fround(5.5)`, `Math.fround(5.05)`, `Math.fround(1e40)`,
		`Math.hypot()`, `Math.hypot(3, 4)`, `Math.hypot(3, 4, 12)`, `Math.hypot(NaN, Infinity)`, `Math.hypot(NaN, 1)`,
		`Math.hypot(1e200, 1e200)`, `Math.hypot(-0)`, `Math.hypot(0.1, 0.2, 0.3)`, `Math.hypot({valueOf: boom}, Infinity)`,
		`Math.hypot("3", [4])`, `Math.hypot(-Infinity, NaN)`,
		`Math.imul(3, 4)`, `Math.imul(4294967295, 5)`, `Math.imul(2147483647, 2)`, `Math.imul(-5, 12.9)`, `Math.imul()`,
		`Math.log(10)`, `Math.log(-1)`, `Math.log(0)`, `Math.log(2.718281828459045)`, `Math.log1p(1e-10)`, `Math.log1p(-1)`,
		`Math.log2(8)`, `Math.log2(3)`, `Math.log2(1e-320)`, `Math.log10(1000)`, `Math.log10(2)`, `Math.log10(1e-5)`,
		`Math.max()`, `Math.max(1, 3, 2)`, `Math.max(1, NaN, 3)`, `Math.max(-0, 0)`, `Math.max(0, -0)`, `Math.max(-0, -0)`,
		`Math.max("7", [8])`, `Math.max(NaN, {valueOf: boom})`, `Math.max(1, undefined)`, `Math.max(-Infinity)`, `Math.max(null, -1)`,
		`Math.min()`, `Math.min(1, -3, 2)`, `Math.min(0, -0)`, `Math.min(-0, 0)`, `Math.min(null, 1)`, `Math.min(1, "x")`,
		`Math.pow(2, 10)`, `Math.pow(2, 0.5)`, `Math.pow(1, Infinity)`, `Math.pow(-1, -Infinity)`, `Math.pow(NaN, 0)`,
		`Math.pow(1, NaN)`, `Math.pow(-8, 0.3333333333333333)`, `Math.pow(1.1, 300)`, `Math.pow(-0, -3)`, `Math.pow(-0, -2)`,
		`Math.pow(10, -5)`, `Math.pow(2, -1074)`, `Math.pow(2, 1024)`, `Math.pow(0.5, 0.5)`, `Math.pow(7, 1.5)`,
		`Math.round(0.5)`, `Math.round(-0.5)`, `Math.round(2.5)`, `Math.round(-2.5)`, `Math.round(0.49999999999999994)`,
		`Math.round(-0.2)`, `Math.round(4503599627370497)`, `Math.round("1.5")`, `Math.round(-0)`, `Math.round(NaN)`,
		`Math.sign(-3)`, `Math.sign(-0)`, `Math.sign("x")`, `Math.sign(0.001)`,
		`Math.sin(2)`, `Math.sin(-0)`, `Math.sin(1e300)`, `Math.sin(3.141592653589793)`, `Math.sinh(1)`, `Math.sinh(-0)`,
		`Math.sqrt(2)`, `Math.sqrt(-1)`, `Math.sqrt(-0)`, `Math.sqrt(Infinity)`,
		`Math.tan(1)`, `Math.tan(1.5707963267948966)`, `Math.tan(-1e-8)`, `Math.tanh(0.5)`, `Math.tanh(-Infinity)`, `Math.tanh(30)`,
		`Math.trunc(-4.7)`, `Math.trunc(-0.2)`, `Math.trunc("12.9")`,
		`Math.foo(1)`, `Math.PI(1)`,
		`[-1, 2.5, "-3"].map(Math.abs)`, `[1, 2, 3].reduce(Math.max)`, `["1.5", "2.5", "-2.5"].map(Math.round)`, `[4, 9].map(Math.sqrt)`,
	})
}

func TestJSONIsJavaScripts(t *testing.T) {
	compare(t, []string{
		// parse
		`JSON.parse("1")`, `JSON.parse("-0")`, `JSON.parse(" \n\t1 \r")`, `JSON.parse("1e400")`, `JSON.parse("-1e-400")`,
		`JSON.parse("0.5")`, `JSON.parse("1E+2")`, `JSON.parse("123456789012345678901234567890")`,
		`JSON.parse("01")`, `JSON.parse("1.")`, `JSON.parse(".5")`, `JSON.parse("+1")`, `JSON.parse("1e")`, `JSON.parse("-")`,
		`JSON.parse("")`, `JSON.parse(" ")`, `JSON.parse("[1,]")`, `JSON.parse("{\"a\":1,}")`, `JSON.parse("[1 2]")`,
		`JSON.parse("nul")`, `JSON.parse("true false")`, `JSON.parse("'a'")`, `JSON.parse("\"a\tb\"")`, `JSON.parse("\"\\x41\"")`,
		`JSON.parse("{a:1}")`, `JSON.parse("[")`, `JSON.parse("\"abc")`, `JSON.parse("NaN")`, `JSON.parse("Infinity")`,
		`JSON.parse("\"\\u00e9\"")`, `JSON.parse("\"\\uD83D\\uDE00\"")`, `JSON.parse("\"\\uDE00\\uD83D\"")`,
		`JSON.parse("\"\\uD83Dx\"")`, `JSON.parse("\"\\uD83D\"")`, `JSON.parse("\"\\u12\"")`, `JSON.parse("\"\\u12G4\"")`,
		`JSON.parse("\"\\/\\b\\f\\n\\r\\t\\\\\\\"\"")`, `JSON.parse("\"Phường 😀\"")`,
		`JSON.parse("[1, 2.5e3, -0, true, null, \"a\", [], {}]")`,
		`JSON.parse("{\"b\":1,\"a\":2,\"1\":3,\"0\":4}")`, `JSON.parse("{\"a\":1,\"b\":{\"c\":[]},\"a\":3}")`,
		`JSON.parse("{\"__proto__\":1,\"\":2}")`, `JSON.parse("{\"a\" : [ 1 , { } ] }")`,
		`JSON.parse(5)`, `JSON.parse(null)`, `JSON.parse(true)`, `JSON.parse(undefined)`, `JSON.parse()`, `JSON.parse({})`,
		`JSON.parse([1])`, `JSON.parse([1, 2])`, `JSON.parse({toString: text})`, `JSON.parse({toString: boom})`,
		`JSON.parse("{\"a\":1,\"b\":[2,\"x\"],\"c\":null}", tag)`, `JSON.parse("[1,{\"a\":2}]", dropNumbers)`,
		`JSON.parse("{\"a\":1,\"b\":2}", dropA)`, `JSON.parse("5", doubleNumbers)`, `JSON.parse("{\"a\":[1,2],\"b\":\"x\"}", doubleNumbers)`,
		`JSON.parse("1", boom)`, `JSON.parse("[1]", 5)`, `JSON.parse("{\"a\":{\"a\":1}}", dropA)`, `JSON.parse("[[1],[2]]", tag)`,
		// các object cùng khoá mượn chung khoá (24/11/2026): hai loại cùng khoá đầu và số khoá xen nhau, khoá trùng trong
		// một loại đã nhớ, khoá dạng chỉ số, năm bộ khoá một loại (quá chỗ nhớ), mười khoá, reviver xoá khoá trên dòng mượn
		`JSON.parse("[{\"id\":1,\"a\":2},{\"id\":3,\"b\":4},{\"id\":5,\"a\":6},{\"id\":7,\"b\":8}]")`,
		`JSON.parse("[{\"a\":1,\"b\":2},{\"a\":1,\"a\":3},{\"a\":4,\"b\":5},{\"a\":1,\"a\":3}]")`,
		`JSON.parse("[{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6,\"g\":7,\"h\":8,\"i\":9,\"a\":10},{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6,\"g\":7,\"h\":8,\"i\":9,\"a\":10}]")`,
		`JSON.parse("[{\"b\":1,\"2\":0},{\"b\":3,\"2\":4}]")`,
		`JSON.parse("[{\"k\":1,\"a\":0},{\"k\":1,\"b\":0},{\"k\":1,\"c\":0},{\"k\":1,\"d\":0},{\"k\":1,\"e\":0},{\"k\":2,\"a\":2},{\"k\":3,\"e\":3}]")`,
		`JSON.parse("{\"rows\":[{\"x\":{\"id\":1,\"n\":\"a\"},\"y\":{\"id\":2,\"n\":\"b\"}},{\"x\":{\"id\":3,\"n\":\"c\"},\"y\":{\"id\":4,\"n\":\"d\"}}]}")`,
		`JSON.parse("[{\"a\":1,\"b\":2},{\"a\":3,\"b\":4},{\"a\":5,\"b\":6}]", dropA)`,
		// mảng dựng đúng kích thước từ cùng ngăn gom (24/11/2026): object trong mảng trong object có khoá trước nó, mảng
		// lồng, mảng rỗng xen giữa, reviver làm lỗ trong các mảng ấy
		`JSON.parse("{\"a\":1,\"b\":[{\"c\":2,\"d\":[3,{\"e\":4}]},5],\"f\":[[6],[]],\"g\":{\"h\":[7,[8,{\"i\":[]}]]}}")`,
		`JSON.parse("[[1,{\"a\":[2,3],\"b\":4},[]],{\"c\":[{\"d\":5},6]},[[[7]]],[],{}]")`,
		`JSON.parse("{\"x\":[1,\"a\",[2,\"b\"]],\"y\":[{\"z\":3}]}", dropNumbers)`,
		`JSON.parse("[{\"a\":1,\"b\":\"x\"},{\"a\":\"y\",\"b\":2},{\"a\":3,\"b\":4}]", dropNumbers)`,
		`JSON.parse("[{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6,\"g\":7,\"h\":8,\"i\":9,\"j\":10},{\"a\":11,\"b\":12,\"c\":13,\"d\":14,\"e\":15,\"f\":16,\"g\":17,\"h\":18,\"i\":19,\"j\":20}]", dropA)`,
		`JSON.parse("[{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6,\"g\":7,\"h\":8,\"i\":9,\"j\":10},{\"a\":11,\"b\":12,\"c\":13,\"d\":14,\"e\":15,\"f\":16,\"g\":17,\"h\":18,\"i\":19,\"j\":20}]", doubleNumbers)`,
		// stringify: giá trị đơn
		`JSON.stringify(1)`, `JSON.stringify(-0)`, `JSON.stringify(NaN)`, `JSON.stringify(-Infinity)`, `JSON.stringify(1e21)`,
		`JSON.stringify(0.1)`, `JSON.stringify(-1e-7)`, `JSON.stringify(5e-324)`, `JSON.stringify(true)`, `JSON.stringify(null)`,
		`JSON.stringify(undefined)`, `JSON.stringify(double)`, `JSON.stringify()`, `JSON.stringify("x", null, 2)`,
		`JSON.stringify("a\"b\\c\n\r\t\b\f\u0001\u001f\u007f")`, `JSON.stringify("\uD83D")`, `JSON.stringify("x\uDE00y")`,
		`JSON.stringify("😀 Phường")`, `JSON.stringify("\u2028\u2029")`, `JSON.stringify("")`, `JSON.stringify("\\u0001")`,
		// mảng, object
		`JSON.stringify([1, "a", null, undefined, double, NaN, [], {}])`,
		`JSON.stringify({a: 1, b: undefined, c: double, d: [undefined], "1": 2, "0": 3})`,
		`JSON.stringify({"a\"b": 1, "\uD83D": 2, "😀": 3})`, `JSON.stringify([[[]]])`, `JSON.stringify({a: {b: {c: {}}}})`,
		// thụt lề
		`JSON.stringify({a: [1, {b: 2}], c: {}, d: []}, null, 2)`, `JSON.stringify([1, [2, [3]]], null, "\t")`,
		`JSON.stringify({a: 1}, null, "-----------x")`, `JSON.stringify({a: 1}, null, 20)`, `JSON.stringify({a: 1}, null, 0)`,
		`JSON.stringify({a: 1}, null, -1)`, `JSON.stringify({a: [1]}, null, 1.9)`, `JSON.stringify({a: 1}, null, "😀😀😀😀😀😀")`,
		`JSON.stringify({a: 1}, null, "a😀😀😀😀😀")`, `JSON.stringify({a: undefined}, null, 2)`, `JSON.stringify([[]], null, 2)`,
		`JSON.stringify({a: [1, 2]}, null, "ab")`, `JSON.stringify([], null, 2)`, `JSON.stringify({a: 1}, null, true)`,
		`JSON.stringify({a: 1}, null, {})`, `JSON.stringify({a: 1}, null, NaN)`, `JSON.stringify({a: 1}, null, Infinity)`,
		// hàm thay
		`JSON.stringify({a: 1, b: [2, "x"], c: {d: null}}, tag)`, `JSON.stringify({a: 1, b: "s", c: [1]}, dropNumbers)`,
		`JSON.stringify(5, dropNumbers)`, `JSON.stringify({a: 1}, boom)`, `JSON.stringify({a: {a: 1}, b: 2}, dropA)`,
		`JSON.stringify([1, {a: 2}], doubleNumbers, 1)`,
		// mảng thay
		`JSON.stringify({a: 1, b: 2, c: {a: 3, d: 4}}, ["a", "c"])`, `JSON.stringify({1: "x", a: 2}, [1, "a", 1, true, null])`,
		`JSON.stringify([{a: 1, b: 2}], ["b"])`, `JSON.stringify({a: 1}, [])`, `JSON.stringify({a: 1}, ["z"])`,
		`JSON.stringify({b: 1, a: 2}, ["a", "b"])`, `JSON.stringify({a: 1, b: 2}, ["b"], 2)`, `JSON.stringify({a: 1}, 5)`,
		// toJSON
		`JSON.stringify({toJSON: seven})`, `JSON.stringify({a: {toJSON: keyed}, b: [{toJSON: keyed}]})`, `JSON.stringify({toJSON: keyed})`,
		`JSON.stringify({toJSON: 5, a: 1})`, `JSON.stringify({a: {toJSON: boom}})`, `JSON.stringify({x: {toJSON: keyed}}, tag)`,
		`JSON.stringify({a: {toJSON: empty}})`, `JSON.stringify([{toJSON: nan}])`,
		// thời điểm: toJSON của Date
		`JSON.stringify(date(0))`, `JSON.stringify({d: date(1700000000123)})`, `JSON.stringify([date(-1)])`,
		`JSON.stringify({d: date(0)}, tag)`, `JSON.stringify({d: date(0)}, ["d"])`,
		// hàm có sẵn làm hàm gọi lại
		`[[1, 2], [3]].map(JSON.stringify)`, `["{\"a\":1}", "[2]"].map(JSON.parse)`,
	})
}

func TestObjectIsJavaScripts(t *testing.T) {
	compare(t, []string{
		`Object.keys({b: 1, a: 2, 1: 3, 0: 4})`, `Object.keys([5, 6])`, `Object.keys("ab")`, `Object.keys("😀")`, `Object.keys(5)`,
		`Object.keys(true)`, `Object.keys(double)`, `Object.keys(date(0))`, `Object.keys(null)`, `Object.keys()`, `Object.keys({})`,
		`Object.keys([])`, `Object.keys("")`,
		`Object.values({a: 1, b: [2]})`, `Object.values("a😀")`, `Object.values([1, undefined])`, `Object.values(undefined)`,
		`Object.values({1: "x", a: "y", 0: "z"})`,
		`Object.entries({a: 1, 2: "x"})`, `Object.entries(["x"])`, `Object.entries("ab")`, `Object.entries(7)`, `Object.entries(null)`,
		`Object.fromEntries([["a", 1], ["b", 2], ["a", 3]])`, `Object.fromEntries([[1, "x"], [null, 2], [[1, 2], 3]])`,
		`Object.fromEntries([{0: "k", 1: "v"}])`, `Object.fromEntries([["a"]])`, `Object.fromEntries([])`, `Object.fromEntries("")`,
		`Object.fromEntries("ab")`, `Object.fromEntries([1])`, `Object.fromEntries(["ab"])`, `Object.fromEntries(5)`,
		`Object.fromEntries()`, `Object.fromEntries({})`, `Object.fromEntries([[{toString: text}, 1]])`,
		`Object.fromEntries([[{toString: boom}, 1]])`, `Object.fromEntries([[1.5, 1], [-0, 2]])`, `Object.fromEntries([[]])`,
		`Object.assign({a: 1}, {b: 2}, null, {a: 3}, undefined)`, `Object.assign({}, [1, 2], "xy")`, `Object.assign({}, 5, true)`,
		`Object.assign({1: "a"}, {0: "b"})`, `Object.assign(null)`, `Object.assign()`, `Object.assign({})`, `Object.assign([1, 2])`,
		`Object.assign(double)`, `Object.assign([1, 2], null)`, `Object.assign({a: 1}, {a: undefined})`, `Object.assign({}, "😀")`,
		`Object.assign(undefined, {a: 1})`,
		`Object.hasOwn({a: 1}, "a")`, `Object.hasOwn({a: undefined}, "a")`, `Object.hasOwn({a: 1}, "b")`, `Object.hasOwn([1, 2], 1)`,
		`Object.hasOwn([1, 2], "2")`, `Object.hasOwn([1], "length")`, `Object.hasOwn("ab", "1")`, `Object.hasOwn("ab", "length")`,
		`Object.hasOwn(5, "x")`, `Object.hasOwn(null, "a")`, `Object.hasOwn({1: 2}, 1)`, `Object.hasOwn({a: 1}, {toString: boom})`,
		`Object.hasOwn({toString: 1}, "toString")`, `Object.hasOwn({}, "toString")`, `Object.hasOwn([1], "01")`, `Object.hasOwn(undefined)`,
		`Object.is(NaN, NaN)`, `Object.is(0, -0)`, `Object.is(-0, -0)`, `Object.is("a", "a")`, `Object.is({}, {})`, `Object.is(1, "1")`,
		`Object.is()`, `Object.is(null, undefined)`, `Object.is(double, double)`,
		`Object.groupBy([1, 2, 3, 4], isEven)`, `Object.groupBy([5, 6, 7], index)`, `Object.groupBy("a😀a", identity)`,
		`Object.groupBy([], isEven)`, `Object.groupBy([1], 5)`, `Object.groupBy(5, isEven)`, `Object.groupBy(null, isEven)`,
		`Object.groupBy([1, 2], boom)`, `Object.groupBy([1.5, 2], identity)`, `Object.groupBy([1, 2], shrink)`,
		`Object.groupBy([null, undefined], identity)`, `Object.groupBy([{toString: boom}], identity)`, `Object.groupBy("", isEven)`,
		`Object.foo()`,
		`[{a: 1}, [2]].map(Object.keys)`, `[[["a", 1]]].map(Object.fromEntries)`, `[{b: 2}].map(Object.entries)`,
	})
}
