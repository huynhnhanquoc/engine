package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A method call leaves its argument slots above the top of the stack as they are: the next push
// overwrites them, and FastReset clears every slot up to the stack's high water, so nothing of one run
// is held for the next. (INVOKE cleared them after every call, a cost of each method call.)
func TestAMethodCallLeavesItsArgumentSlotsToTheNextPush(t *testing.T) {
	program := load(t, `const a = []; const marker = { m: 1 }; a.push(marker);`)
	program.run(t)
	marker := program.variable("marker")
	if !marker.IsObject() {
		t.Fatalf("marker = %s, want the object", marker.Kind())
	}
	stack := program.vm.Stack
	slots := stack[len(stack):cap(stack)]
	held := false
	for _, slot := range slots[:min(len(slots), 8)] {
		if slot.Equal(marker) {
			held = true
		}
	}
	if !held {
		t.Errorf("the slot of push's argument was cleared after the call")
	}

	program.vm.FastReset(program.program, program.globals)
	stack = program.vm.Stack
	for index, slot := range stack[:cap(stack)] {
		if !slot.IsUndefined() {
			t.Fatalf("after FastReset the stack slot %d still holds a %s", index, slot.Kind())
		}
	}
}

// The instructions that skip the failure test never meet a failure: every instruction that can push
// one is tested where it pushes it, so a failure read from a variable, made by a call or by an
// operator ends the run (or reaches safe) there, whatever skipping instruction comes next.
func TestFailuresStopWhereTheyArePushed(t *testing.T) {
	cases := []struct {
		source  string
		message string // in the run's failure; "" for a run that ends without one
		result  string // how r's text ends after a run that ends without a failure
	}{
		{`x = error("call"); y = 1;`, "call", ""},
		{`x = broken; y = 1;`, "kept", ""},
		{`t = !broken;`, "kept", ""},
		{`t = broken === 1;`, "kept", ""},
		{`t = [broken];`, "kept", ""},
		{`if (broken) { y = 1; }`, "kept", ""},
		{`for (let i = 0; i < 3; i++) { y = i + broken; }`, "kept", ""},
		{`r = safe(() => { const y = broken; return 1; }).error === null;`, "", "false"},
		{`r = safe(() => { const y = error("inner"); return y; }).error.message;`, "", "inner"},
		{`r = safe(() => !error("not")).error.message;`, "", "not"},
		{`r = safe(() => [1, 2].map((x) => error("map " + x))).error.message;`, "", "map 1"},
	}
	for _, c := range cases {
		program := load(t, c.source)
		program.globals["broken"] = value.Fail(value.FailCode, "kept")
		program.vm.FastReset(program.program, program.globals)
		result := program.vm.Run()
		switch {
		case c.message != "":
			if !result.IsInvalid() || !strings.Contains(result.Message(), c.message) {
				t.Errorf("%s: %s %q, want a failure with %q", c.source, result.Kind(), result.Message(), c.message)
			}
			if y := program.variable("y"); !y.IsUndefined() {
				t.Errorf("%s: the run went on past the failure (y = %s)", c.source, y.ToString().Text())
			}
		case result.IsInvalid():
			t.Errorf("%s: failed with %q", c.source, result.Message())
		default:
			// a caught failure's message leads with its place (<source>:1:32: inner)
			if got := program.variable("r").ToString().Text(); !strings.HasSuffix(got, c.result) {
				t.Errorf("%s: r = %q, want it to end with %q", c.source, got, c.result)
			}
		}
	}
}
