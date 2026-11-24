package runtime_test

import (
	"strings"
	"testing"
)

// Array(n) makes JavaScript's n holes (19/11/2026, docs/language.md mục 4.3). Kitwork's array holds undefined there,
// which reads as a hole reads (held to node in parity_test.go); what passes over a hole is the TYPE error on it, where it
// was a value other than JavaScript's: Array(3).every((x) => false) was false (JavaScript's true), .filter(() =>
// true).length 3 (0), .indexOf(undefined) 0 (-1), Object.keys(Array(2)) ["0","1"] ([]). slice, concat and splice keep
// the holes as JavaScript's do; a fill of the array whole fills them, a fill of part does not.
//
// CONTROL (19/11/2026): with Array(n) not marked, every refused case gave a value; with the mark not kept by slice and
// concat, their map gave a value; with a whole fill not clearing it, Array(3).fill(1).map(…) was refused.
func TestWhatPassesOverAHoleIsRefusedOnArrayN(t *testing.T) {
	program := load(t, `const refused = (f) => { const r = safe(f); return r.error === null ? "gave " + JSON.stringify(r.value) : r.error.code; };
result = [refused(() => Array(3).map((x) => 1)), refused(() => Array(3).forEach((x) => x)), refused(() => Array(3).filter((x) => true)),
  refused(() => Array(3).some((x) => true)), refused(() => Array(3).every((x) => false)), refused(() => Array(2).reduce((a, b) => a, 0)),
  refused(() => Array(3).indexOf(undefined)), refused(() => Array(3).lastIndexOf(undefined)), refused(() => Object.keys(Array(2))),
  refused(() => Object.entries(Array(2))), refused(() => Object.values(Array(2))), refused(() => ({ ...Array(2) })),
  refused(() => Array(2).hasOwnProperty(0)), refused(() => Object.hasOwn(Array(2), 0)), refused(() => Object.getOwnPropertyNames(Array(2))),
  refused(() => Object.assign({}, Array(2))), refused(() => [Array(2)].flat()), refused(() => [1].flatMap(() => Array(2))),
  refused(() => Array(3).slice(1).map((x) => 1)), refused(() => [1].concat(Array(2)).map((x) => 1)),
  refused(() => Array(3).splice(0, 2).map((x) => 1)), refused(() => Array(3).fill(0, 1).map((x) => 1)),
  refused(() => Array(3).fill(1).map((x) => x * 2)), refused(() => Array(0).map((x) => 1)), refused(() => [...Array(2)].map((x) => 1))];`)
	program.run(t)
	got := describe(program.variable("result"))
	want := `array [` + strings.Repeat(`"TYPE",`, 22) + `"gave [2,2,2]","gave []","gave [1,1]"]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	message := load(t, `result = safe(() => Array(3).map((x) => 1)).error.message;`)
	message.run(t)
	if text := message.variable("result").Text(); !strings.HasPrefix(text, "a.map(…): mảng tạo bằng Array(n) còn chỗ trống") {
		t.Errorf("the message is %q", text)
	}
}
