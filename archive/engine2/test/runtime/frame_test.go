package runtime_test

import (
	"strconv"
	"strings"
	"testing"
)

// manyLocals is a function of n variables of its own, so the frame that runs it grows a map of n keys.
func manyLocals(n int) string {
	var b strings.Builder
	b.WriteString("const big = () => { ")
	for i := 0; i < n; i++ {
		b.WriteString("let v" + strconv.Itoa(i) + " = 0; ")
	}
	b.WriteString("return 0; };\n")
	return b.String()
}

// A frame that last ran the same function and holds only its parameters is not cleared: the call's
// binding overwrites each of them. A function of 1,000 variables runs once, then a function of one
// parameter a thousand times, on the frame the big one grew (measured) or on another one (reference,
// where big runs a frame deeper). Without the skip each of the thousand calls clears a map sized for
// the 1,000 (1.6 to 1.8 times the reference when measured); with it only the first does (1.0 to 1.1).
func TestARepeatedCallSkipsTheClear(t *testing.T) {
	loop := "const one = (n) => n; t = 0; for (let i = 0; i < 1000; i++) { t = one(i); }"
	grown := load(t, manyLocals(1000)+"big(); "+loop)
	apart := load(t, manyLocals(1000)+"const deeper = () => big(); deeper(); "+loop)
	grown.run(t)
	if got := grown.variable("t").Number(); got != 999 {
		t.Fatalf("t = %v, want 999", got)
	}
	requireCost(t, "a thousand calls on a frame grown by 1,000 variables", grown.runs(t), apart.runs(t), 1.4)
}

// The skip keeps what each call sees: a parameter left out is undefined however the last call bound it,
// and a function whose frame held a variable of its own starts without it.
func TestAReusedFrameKeepsNothingOfTheLastCall(t *testing.T) {
	program := load(t, `
const pair = (a, b) => [a, b];
const first = pair(1, 2);
const second = pair(3);
const local = (n) => { if (n > 0) { const kept = n; return kept; } return typeof kept; };
const third = local(5);
const fourth = local(0);
r = JSON.stringify([first, second]) + " " + third + " " + fourth;
`)
	program.run(t)
	if got, want := program.variable("r").Text(), "[[1,2],[3,null]] 5 undefined"; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
}
