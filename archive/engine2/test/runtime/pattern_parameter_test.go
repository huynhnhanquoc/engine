package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// A parameter and a for…of item destructure as JavaScript's do (15/11/2026, round 5 of the 13/11 review): ([x, y]) =>
// …, a function's ({ a }, [b, ...c]), an array method's callback ([a, b]) => …, a default inside, for (const { id, t }
// of list), for (const [k, v] of Object.entries(o)), for (let [first, ...rest] of rows) whose names the body may
// write. They were compile errors (an object parameter had been lowered since 19/10/2026; an array one and a pattern
// in for…of had not), the item to be taken whole and destructured in the body. CONTROL: the iteration not starting
// with its destructuring, red.
func TestAParameterAndAnItemDestructure(t *testing.T) {
	program := load(t, `const pairs = ([x, y]) => x + y;
function g({ a }, [b, ...c]) { return [a, b, c]; }
const out = [];
for (const { id, t } of [{ id: 1, t: "x" }, { id: 2, t: "y" }]) { out.push(id + t); }
for (const [k, v] of Object.entries({ a: 1, b: 2 })) { out.push(k + v); }
for (let [first, ...rest] of [[1, 2, 3]]) { first = first * 10; out.push(first, rest); }
const result = [pairs([2, 3]), g({ a: 1 }, [2, 3, 4]), out, [[1, 2], [3, 4]].map(([a, b]) => a * b), (([a = 5]) => a)([])];`)
	program.run(t)
	want := `array [5,[1,2,[3,4]],["1x","2y","a1","b2",10,[2,3]],[2,12],5]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// What JavaScript refuses stays a compile error: writing a name a const for…of destructured, a pattern in a counted
// for or a for…in.
func TestAnItemsPatternStandsWhereJavaScriptHasOne(t *testing.T) {
	for source, want := range map[string]string{
		`for (const { a } of [{ a: 1 }]) { a = 2; }`: "const",
		`for (let [a] = [0]; a < 1; a++) { }`:        "tách trong for chỉ có ở for…of",
		`for (const { a } in { a: 1 }) { }`:          "tách trong for chỉ có ở for…of",
	} {
		if _, err := compiler.CompileSource(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", source, err, want)
		}
	}
}
