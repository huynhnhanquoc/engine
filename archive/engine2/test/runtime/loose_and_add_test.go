package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// + converts its right side only once its left side converted: JavaScript's ToPrimitive of the left side
// throws before the right side's runs (05/11/2026: the right side's valueOf ran after the left one failed,
// so the log was [a, b]; - and < already stopped). node 22's answers.
func TestAddStopsAtItsLeftSidesFailure(t *testing.T) {
	program := load(t, `const result = [];
for (const op of ["+", "-"]) {
  const log = [];
  const a = { valueOf: () => { log.push("a"); return error("A") } };
  const b = { valueOf: () => { log.push("b"); return 1 } };
  const r = op === "+" ? safe(() => a + b) : safe(() => a - b);
  result.push([r.error === null, log]);
}
const log = [];
const c = { valueOf: () => { log.push("c"); return "x" } };
const d = { valueOf: () => { log.push("d"); return 2 } };
result.push([c + d, log]);`)
	program.run(t)
	if got := describe(program.variable("result")); got != `array [[false,["a"]],[false,["a"]],["x2",["c","d"]]]` {
		t.Errorf("got %s", got)
	}
}

// == compares what an object's ToPrimitive gave as JavaScript compares two primitives: a null or undefined
// equals no number or text, and a boolean is its number (05/11/2026: the null was read as 0, so
// { valueOf: () => null } == 0 was true, and { valueOf: () => true } == "1" was false). node 22's answers.
func TestLooseEqualityComparesWhatToPrimitiveGaveAsJavaScriptDoes(t *testing.T) {
	program := load(t, `const result = [
  { valueOf: () => null } == 0, { valueOf: () => null } == false, { toString: () => null } == 0,
  { valueOf: () => null } == null, { valueOf: () => null } != 0, { valueOf: () => undefined } == 0,
  { valueOf: () => null } == "", "" == { valueOf: () => undefined }, { valueOf: () => 0 } == 0,
  { valueOf: () => true } == "1", { valueOf: () => true } == 1, { valueOf: () => false } == "",
  "1" == { valueOf: () => true }, { valueOf: () => true } == "true"
];`)
	program.run(t)
	want := `array [false,false,false,false,true,false,false,false,true,true,true,true,true,false]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// LooseEqual, the == of Go, answers as LooseEquals does for an object whose valueOf is a function of Go.
func TestLooseEqualOfGoComparesWhatToPrimitiveGave(t *testing.T) {
	giving := func(primitive value.Value) value.Value {
		o := value.Object()
		o.Set("valueOf", value.Function(func(...value.Value) value.Value { return primitive }))
		return o
	}
	for _, c := range []struct {
		a, b value.Value
		want bool
	}{
		{giving(value.Null()), value.Number(0), false},
		{giving(value.Undefined()), value.String(""), false},
		{giving(value.Boolean(true)), value.String("1"), true},
		{giving(value.Number(0)), value.Number(0), true},
	} {
		if got := c.a.LooseEqual(c.b); got != c.want {
			t.Errorf("valueOf %s == %s: %v, want %v", c.a.Get("valueOf").Call(nil).ToString().Text(), c.b.ToString().Text(), got, c.want)
		}
		if got := c.a.LooseEquals(c.b); got.Boolean() != c.want {
			t.Errorf("LooseEquals: valueOf %s == %s: %v, want %v", c.a.Get("valueOf").Call(nil).ToString().Text(), c.b.ToString().Text(), got.Boolean(), c.want)
		}
	}
}
