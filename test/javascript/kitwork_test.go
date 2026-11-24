package javascript_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Những gì node không kiểm thay được: luật riêng của Kitwork (thời điểm và hàm không có chữ, khoảng thời gian, bytes),
// trần của chuỗi, mảng chứa chính nó, và các phép so bằng, đúng sai gọi thẳng từ Go.

func failure(v value.Value) string {
	if !v.IsInvalid() {
		return "<no error: " + describe(v) + ">"
	}
	return v.Error().Error()
}

func TestKitworksOwnKinds(t *testing.T) {
	moment := value.Time(time.Date(2026, 11, 22, 16, 0, 0, 0, time.UTC))
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"time has no text", failure(javascript.ToString(nil, moment)),
			"a time has no text of its own: JavaScript writes it in the zone of the machine; write t.toISOString()"},
		{"time toString", failure(javascript.Invoke(nil, moment, "toString", nil)),
			"a time has no text of its own: JavaScript writes it in the zone of the machine; write t.toISOString()"},
		{"function has no text", failure(javascript.ToString(nil, value.Function(callbacks["double"]))),
			"a function has no text: JavaScript writes its source, which Kitwork does not keep"},
		{"duration text", javascript.ToString(nil, value.Duration(90*time.Minute)).String(), "1h30m0s"},
		{"duration hours", describe(javascript.Invoke(nil, value.Duration(90*time.Minute), "hours", nil)), describe(value.Number(1.5))},
		{"duration milliseconds", describe(javascript.Invoke(nil, value.Duration(1500*time.Microsecond), "milliseconds", nil)),
			describe(value.Number(1.5))},
		{"duration as number", describe(javascript.ToNumber(nil, value.Duration(2*time.Second))), describe(value.Number(2000))},
		{"time as number", describe(javascript.ToNumber(nil, moment)), describe(value.Number(1795363200000))},
		{"bytes text", javascript.ToString(nil, value.Bytes([]byte("abc"))).String(), "abc"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestAStringPastItsLimitIsAnError(t *testing.T) {
	for name, result := range map[string]value.Value{
		"repeat":   javascript.Invoke(nil, value.String("ab"), "repeat", []value.Value{value.Number(1 << 29)}),
		"padStart": javascript.Invoke(nil, value.String("ab"), "padStart", []value.Value{value.Number(1 << 30)}),
		"padEnd":   javascript.Invoke(nil, value.String("ab"), "padEnd", []value.Value{value.Number(math.Inf(1)), value.String("x")}),
	} {
		if got := failure(result); got != "invalid string length" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// Một mảng chứa chính nó: join viết "" ở chỗ ấy như V8; flat(Infinity) là lỗi thay cho vòng lặp mãi.
func TestAnArrayHoldingItself(t *testing.T) {
	a := value.Array(value.Number(1))
	a.Push(a)
	if got := javascript.ToString(nil, a).String(); got != "1," {
		t.Errorf("join: %q", got)
	}
	if got := failure(javascript.Invoke(nil, a, "flat", []value.Value{value.Number(math.Inf(1))})); got != "maximum call stack size exceeded" {
		t.Errorf("flat: %q", got)
	}
}

func TestCallsNeedARunAndCarryFailures(t *testing.T) {
	double := value.Function(callbacks["double"])
	if got := failure(javascript.Invoke(nil, value.Array(value.Number(1)), "map", []value.Value{double})); got != "cannot call a function here: no run" {
		t.Errorf("no run: %q", got)
	}
	thrown := value.Invalid("thrown")
	if got := javascript.Invoke(run{}, value.String("abc"), "at", []value.Value{thrown}); !got.Identical(thrown) {
		t.Errorf("a failed argument must be the result: %s", describe(got))
	}
	if !javascript.HasMethod(value.String("a"), "at") || javascript.HasMethod(value.String("a"), "map") ||
		!javascript.HasMethod(value.Number(1), "hasOwnProperty") || javascript.HasMethod(value.Null(), "toString") {
		t.Error("HasMethod")
	}
}

func TestEqualityAndTruth(t *testing.T) {
	nan, zero, negative := value.Number(math.NaN()), value.Number(0), value.Number(math.Copysign(0, -1))
	o := value.Object()
	cases := []struct {
		name                         string
		a, b                         value.Value
		strict, sameValueZero, equal bool
	}{
		{"NaN", nan, nan, false, true, true},
		{"0 and -0", zero, negative, true, true, false},
		{"same object", o, o, true, true, true},
		{"two objects", value.Object(), value.Object(), false, false, false},
		{"strings", value.String("a"), value.String("a"), true, true, true},
		{"1 and \"1\"", value.Number(1), value.String("1"), false, false, false},
		{"null and undefined", value.Null(), value.Undefined(), false, false, false},
	}
	for _, c := range cases {
		if javascript.StrictEquals(c.a, c.b) != c.strict || javascript.SameValueZero(c.a, c.b) != c.sameValueZero ||
			javascript.SameValue(c.a, c.b) != c.equal {
			t.Errorf("%s: ===, SameValueZero, SameValue want %v %v %v", c.name, c.strict, c.sameValueZero, c.equal)
		}
	}
	for v, want := range map[*value.Value]bool{
		{}: false, &nan: false, &zero: false, &negative: false,
	} {
		if javascript.ToBoolean(*v) != want {
			t.Errorf("ToBoolean(%s)", describe(*v))
		}
	}
	for _, truthy := range []value.Value{value.String("0"), value.Array(), value.Object(), value.Number(-1), value.Duration(0)} {
		if !javascript.ToBoolean(truthy) {
			t.Errorf("ToBoolean(%s) must be true", describe(truthy))
		}
	}
	if javascript.ToBoolean(value.String("")) || javascript.ToBoolean(value.Null()) || javascript.ToBoolean(value.Boolean(false)) {
		t.Error(`"", null and false must be false`)
	}
}

// Math, JSON, Object là thư viện, không phải dữ liệu; loại của Kitwork viết thành JSON; vòng lặp, độ lồng.
func TestGlobals(t *testing.T) {
	math, _ := javascript.Global("Math")
	json, _ := javascript.Global("JSON")
	object, _ := javascript.Global("Object")
	stringify := func(arguments ...value.Value) value.Value {
		return javascript.Invoke(run{}, json, "stringify", arguments)
	}
	text := func(v value.Value) string {
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return v.String()
	}
	if names := strings.Join(javascript.GlobalNames(), " "); names != "Array Boolean Date JSON Math Number Object String console decodeURI decodeURIComponent encodeURI encodeURIComponent isFinite isNaN parseFloat parseInt" {
		t.Errorf("GlobalNames: %v", names)
	}
	if _, found := javascript.Global("Nope"); found {
		t.Error("Global(Nope)")
	}

	// một hàm có sẵn là một Native: tên và số đối số như JavaScript; gọi được không cần run khi không gọi lại gì
	ns, _ := value.As[javascript.Namespace](math)
	max, _ := ns.Get("max")
	if native := max.Function().(*javascript.Native); native.Name() != "max" || native.Length() != 2 {
		t.Errorf("Math.max: %q %d", native.Name(), native.Length())
	}
	if got := javascript.Invoke(nil, math, "max", []value.Value{value.Number(1), value.Number(5)}); got.Number() != 5 {
		t.Errorf("Math.max without a run: %s", describe(got))
	}
	keys := ns.Keys()
	keys[0] = "changed"
	if ns.Keys()[0] != "abs" {
		t.Error("Keys must be a copy")
	}
	for range 1000 {
		if r := javascript.Invoke(nil, math, "random", nil).Number(); r < 0 || r >= 1 {
			t.Fatalf("Math.random: %v", r)
		}
	}
	if !javascript.HasMethod(math, "max") || javascript.HasMethod(math, "PI") || javascript.HasMethod(math, "foo") {
		t.Error("HasMethod of Math")
	}

	cycle := value.Object()
	cycle.Set("self", cycle)
	looped := value.Array(value.Number(1))
	looped.Push(looped)
	shared := value.Object().Set("a", value.Number(1))
	moment := value.Time(time.Date(2026, 11, 23, 8, 30, 0, 123456789, time.UTC))
	toTime := value.Function(&callback{fn: func(_ javascript.Run, a []value.Value) value.Value {
		if arg(a, 0).String() == "t" {
			return moment
		}
		return arg(a, 1)
	}})
	cases := []struct {
		name string
		got  value.Value
		want string
	}{
		{"stringify Math", stringify(math), "error: JSON.stringify: Math is a library, not data"},
		{"stringify a struct", stringify(value.Object().Set("site", value.Struct(&struct{}{}))),
			"error: JSON.stringify: a struct of the engine is not data"},
		{"keys of Math", javascript.Invoke(nil, object, "keys", []value.Value{math}), "error: Math is a library, not data"},
		{"assign from Math", javascript.Invoke(nil, object, "assign", []value.Value{value.Object(), math}),
			"error: Math is a library, not data"},
		{"Math.PI is no function", javascript.Invoke(nil, math, "PI", nil), "error: Math.PI is not a function"},
		{"Math.foo", javascript.Invoke(nil, math, "foo", nil), "error: Math.foo is not a function"},
		{"time", stringify(moment), `"2026-11-23T08:30:00.123Z"`},
		{"duration", stringify(value.Duration(90 * time.Minute)), `"1h30m0s"`},
		{"bytes", stringify(value.Bytes([]byte("abc"))), `"YWJj"`},
		{"a replacer giving a time", stringify(value.Object().Set("t", value.Number(1)), toTime), `{"t":"2026-11-23T08:30:00.123Z"}`},
		{"an object holding itself", stringify(cycle), "error: converting circular structure to JSON"},
		{"an array holding itself", stringify(looped), "error: converting circular structure to JSON"},
		{"one object twice is no cycle", stringify(value.Array(shared, shared)), `[{"a":1},{"a":1}]`},
		{"assign onto a number", javascript.Invoke(nil, object, "assign", []value.Value{value.Number(5), value.Object()}),
			"error: Object.assign cannot write keys on a number: Kitwork has no object wrapping one"},
		{"assign onto an array", javascript.Invoke(nil, object, "assign", []value.Value{value.Array(), value.Object().Set("a", value.Null())}),
			"error: Object.assign cannot write keys on an array: only an object holds keys"},
		{"a time as a key", javascript.Invoke(nil, object, "fromEntries", []value.Value{value.Array(value.Array(moment, value.Number(1)))}),
			"error: a time has no text of its own: JavaScript writes it in the zone of the machine; write t.toISOString()"},
		{"where parse stops", javascript.Invoke(nil, json, "parse", []value.Value{value.String("[1,]")}),
			`error: JSON.parse: unexpected character ']' at byte 3`},
		{"512 deep", javascript.Invoke(nil, json, "stringify", []value.Value{javascript.Invoke(nil, json, "parse",
			[]value.Value{value.String(strings.Repeat("[", 512) + strings.Repeat("]", 512))})}), strings.Repeat("[", 512) + strings.Repeat("]", 512)},
		{"513 deep", javascript.Invoke(nil, json, "parse", []value.Value{value.String(strings.Repeat("[", 513) + strings.Repeat("]", 513))}),
			"error: JSON.parse: nesting deeper than 512 at byte 512"},
	}
	for _, c := range cases {
		if got := text(c.got); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if hasPI := javascript.Invoke(nil, object, "hasOwn", []value.Value{math, value.String("PI")}); !hasPI.Boolean() {
		t.Error("Object.hasOwn(Math, \"PI\")")
	}
	array := value.Array(value.Number(1))
	if got := javascript.Invoke(nil, object, "assign", []value.Value{array, value.Object()}); !got.Identical(array) {
		t.Error("Object.assign(array, {}) gives the array back, as JavaScript")
	}
}

// Number, String, Array, parseInt: chỗ Kitwork khác JavaScript, các trần, và một hàm có sẵn là một giá trị.
func TestConstructors(t *testing.T) {
	global := func(name string) value.Value {
		g, _ := javascript.Global(name)
		return g
	}
	number, array, parseInt := global("Number"), global("Array"), global("parseInt")
	invoke := func(this value.Value, name string, arguments ...value.Value) string {
		v := javascript.Invoke(nil, this, name, arguments)
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	call := func(f value.Value, arguments ...value.Value) string {
		v := f.Function().(*javascript.Native).Call(nil, arguments...)
		if v.IsInvalid() {
			return "error: " + v.Error().Error()
		}
		return describe(v)
	}
	numberParseInt, _ := javascript.Members(number).Get("parseInt")
	if !numberParseInt.Identical(parseInt) {
		t.Error("Number.parseInt must be parseInt itself, as Number.parseInt === parseInt in JavaScript")
	}
	for name, length := range map[string]int{"Number": 1, "String": 1, "Array": 1, "Boolean": 1, "parseInt": 2, "parseFloat": 1} {
		if n := global(name).Function().(*javascript.Native); n.Name() != name || n.Length() != length || global(name).Typeof() != "function" {
			t.Errorf("%s: %q %d %s", name, n.Name(), n.Length(), global(name).Typeof())
		}
	}
	if !javascript.HasMethod(number, "isInteger") || !javascript.HasMethod(number, "call") || javascript.HasMethod(number, "MAX_VALUE") {
		t.Error("HasMethod of Number")
	}
	math := global("Math")
	if javascript.HasMethod(math, "call") || javascript.Members(parseInt) != nil {
		t.Error("Math is no function; parseInt has no members")
	}
	fn := value.Function(callbacks["double"])
	cases := []struct{ name, got, want string }{
		{"Array(3)", call(array, value.Number(3)), "[undefined,undefined,undefined]"},
		{"Array(2^24 + 1)", call(array, value.Number(1<<24+1)), "error: an array of 16777217 items is more than the 16777216 Kitwork makes at once"},
		{"Array(2^32)", call(array, value.Number(1<<32)), "error: invalid array length"},
		{"Array(0)", call(array, value.Number(0)), "[]"},
		{"String(a time)", call(global("String"), value.Time(time.UnixMilli(0))),
			"error: a time has no text of its own: JavaScript writes it in the zone of the machine; write t.toISOString()"},
		{"String(a function)", call(global("String"), fn), "error: a function has no text: JavaScript writes its source, which Kitwork does not keep"},
		{"parseInt past 2^53 in base 36", call(parseInt, value.String("zzzzzzzzzzzz"), value.Number(36)),
			"error: parseInt: a number past 2^53 in base 36 is not exact, JavaScript leaves its last digits to each engine: read it in base 10 or a power of 2"},
		{"parseInt below 2^53 in base 36", call(parseInt, value.String("zzzzzzzzzz"), value.Number(36)), describe(value.Number(3656158440062975))},
		{"Array.from of 2^24 + 1", invoke(array, "from", value.Object().Set("length", value.Number(1<<24+1))),
			"error: an array of 16777217 items is more than the 16777216 Kitwork makes at once"},
		{"Array.from of 2^32", invoke(array, "from", value.Object().Set("length", value.Number(1<<32))), "error: invalid array length"},
		{"Array.from of a function", invoke(array, "from", fn), "error: Array.from cannot read a function as a list of items"},
		{"Array.from of bytes", invoke(array, "from", value.Bytes([]byte{1, 255})), describe(value.Array(value.Number(1), value.Number(255)))},
		{"Array.from of Math", invoke(array, "from", math), "error: Math is a library, not data"},
		{"Math.call", invoke(math, "call"), "error: Math.call is not a function"},
		{"Object.hasOwn(Number, parseInt)", invoke(global("Object"), "hasOwn", number, value.String("parseInt")), "true"},
		{"Object.keys(Number)", invoke(global("Object"), "keys", number), "[]"},
		{"JSON.stringify(Number)", invoke(global("JSON"), "stringify", number), "undefined"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %s, want %s", c.name, c.got, c.want)
		}
	}
}
