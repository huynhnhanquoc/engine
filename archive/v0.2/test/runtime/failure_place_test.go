package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A failure says where it happened apart from what it says (19/11/2026, docs/language.md mục 5.2, decided 14/10/2026):
// safe() gives { code, message, at }, message the text alone, at "file:line:column" of the innermost frame at the
// failing instruction, so a failure deep in a function is at its own line, not at the safe(…) around it. A failure no
// run placed (a function of Go safe calls itself) has at null. A failure a run placed that a function of Go hands back
// as its own (another VM's) stays where it happened, not placed again at the call. message held the place before at,
// "<source>:2:10: không có 7", which a program cut off to show the text.
//
// CONTROL (19/11/2026): with the failure's text the headline again and no place apart, message held the place and at
// was null; with error objects made without at, the keys were code, message; with a placed failure placed again at the
// call, the last at was the call's, <source>:7:20; with WithFields dropping the place, Place() was "".
func TestAFailureSaysWhereApartFromItsMessage(t *testing.T) {
	program := load(t, `const find = (id) => {
  return error("không có " + id);
};
const n = 3;
const shown = (r) => [r.error.code, r.error.message, r.error.at];
result = [shown(safe(() => find(7))), shown(safe(() => n())), shown(safe(JSON.parse)), Object.keys(safe(() => find(1)).error),
  shown(safe(() => placed()))];`)
	program.globals["placed"] = value.Function(func(...value.Value) value.Value {
		return value.FailAt(value.TimeoutCode, "hết giờ", "other.work:9:3")
	})
	program.run(t)
	want := `array [["FAIL","không có 7","<source>:2:10"],["TYPE","n không phải hàm (là number): không gọi được","<source>:6:56"],` +
		`["INVALID_INPUT","JSON: unexpected character 'u' at byte 0",null],["code","message","at"],["TIMEOUT","hết giờ","other.work:9:3"]]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	placed := value.FailAt(value.TypeCode, "x", "a.work:1:2").WithFields(value.Array(nil)).WithProgress(value.Number(1))
	if got := placed.Place(); got != "a.work:1:2" {
		t.Errorf("a failure given fields and progress is at %q, want a.work:1:2", got)
	}
	if got := placed.Failure(); got != "TYPE: a.work:1:2: x" {
		t.Errorf("the failure's line for a log is %q, want its code, its place, its text", got)
	}
	if got := value.Fail(value.TypeCode, "x").Failure(); got != "TYPE: x" {
		t.Errorf("a failure no run placed is %q for a log, want its code and its text", got)
	}
}
