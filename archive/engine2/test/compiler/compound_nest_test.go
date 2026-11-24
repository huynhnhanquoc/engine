package compiler_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/compiler"
)

// compoundNest is x[x[…x[0] += 1…] += 1], depth levels deep.
func compoundNest(depth int) string {
	expression := "x[0]"
	for i := 0; i < depth; i++ {
		expression = "x[" + expression + " += 1]"
	}
	return expression
}

// perCompile is the best time of one compile of source, over rounds that take at least 10 ms.
func perCompile(t *testing.T, source string) float64 {
	t.Helper()
	best := math.MaxFloat64
	for trial := 0; trial < 3; trial++ {
		rounds := 0
		start := time.Now()
		for time.Since(start) < 10*time.Millisecond {
			if _, err := compiler.CompileSource(source); err != nil {
				t.Fatal(err)
			}
			rounds++
		}
		best = min(best, float64(time.Since(start))/float64(rounds))
	}
	return best
}

// A nest of compound assignments, x[x[…] += 1] += 1, compiles in time and memory that grow with its
// source, and the error of one that cannot be assigned is about as long as the source (05/11/2026). The
// parser lowers x += 1 to x = x + 1 with one node for both x; the nesting check, the resolver, the name a
// function is given and an error's text each read that node twice, so every level doubled the cost: a
// 286-byte file of depth 22 took 3.5 s and 5.3 GB to compile, and its error was 92 MB.
func TestANestOfCompoundAssignmentsCostsWhatItsSourceDoes(t *testing.T) {
	valid := func(depth int) string {
		return "const x = [0];\nconst h = (context) => context.text(\"\" + (" + compoundNest(depth) + " += 1));\n"
	}
	invalid := func(depth int) string {
		return "const x = [0];\nconst h = (context) => context.text(\"\" + " + compoundNest(depth) + " += 1);\n"
	}

	// six more levels cost about six levels more, not 64 times as much
	shallow, deep := compileAllocations(t, valid(12)), compileAllocations(t, valid(18))
	if deep > 2*shallow {
		t.Fatalf("depth 18 makes %.0f allocations against %.0f at depth 12, want about as many more as its source is longer", deep, shallow)
	}
	// and time: one walk of the tree that still read the target twice is 2^depth visits
	if testing.CoverMode() == "" {
		if ratio := perCompile(t, valid(20)) / perCompile(t, valid(10)); ratio > 8 {
			t.Fatalf("depth 20 takes %.0f times as long as depth 10 to compile, want about twice", ratio)
		}
	}
	_, err := compiler.CompileSource(invalid(12))
	if err == nil {
		t.Fatal("an infix as the target of += compiled")
	}
	if len(err.Error()) > 1024 {
		t.Fatalf("the error of a 12-deep nest is %d bytes, want about the source's length", len(err.Error()))
	}

	began := time.Now()
	if _, err := compiler.CompileSource(valid(30)); err != nil {
		t.Fatalf("depth 30: %v", err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("depth 30 compiled in %v, want well under a second", took)
	}
	_, err = compiler.CompileSource(invalid(30))
	if err == nil {
		t.Fatal("an infix as the target of += compiled at depth 30")
	}
	text := err.Error()
	if len(text) > 2048 {
		t.Errorf("the error of a 30-deep nest is %d bytes, want about the source's length", len(text))
	}
	// the target is named as the author wrote it, += and all
	if !strings.Contains(text, "không gán được vào") || !strings.Contains(text, "(x[0]) += 1)") {
		t.Errorf("the error names the target %q", text)
	}
}

// x += y and x++ are named as written wherever the compiler shows them, an update of an update included.
func TestACompoundAssignmentIsShownAsWritten(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`let a = 1; (a += 2) = 3;`, "không gán được vào (a += 2)"},
		{`let a = [1]; (a[0] *= 2) = 3;`, "không gán được vào ((a[0]) *= 2)"},
		{`let a = 1; (a++) = 3;`, "không gán được vào (a++)"},
		{`let a = 1; (--a) = 3;`, "không gán được vào (--a)"},
	} {
		_, err := compiler.CompileSource(c.source)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want it to say %q", c.source, err, c.want)
		}
	}
}

// A function written in the target of x op= y or x++ is one function: resolved again through the value's
// left side, its parameter or variable was declared twice and the program was the compile error "tên k
// trỏ tới hai biến" (05/11/2026). node gives these answers.
func TestAFunctionInTheTargetOfACompoundAssignmentIsOneFunction(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`(() => { const o = { a: 1 }; o[((k) => k)('a')] += 1; return o })()`, `{"a":2}`},
		{`(() => { const o = { a: 1 }; o[(() => { const k = 'a'; return k })()]++; return o })()`, `{"a":2}`},
		{`(() => { const o = { a: { b: 1 } }; o[((k) => k)('a')].b *= 5; return o })()`, `{"a":{"b":5}}`},
		{`(() => { const o = { n: 1 }; const r = ++o[((k) => k)('n')]; return [r, o.n] })()`, `[2,2]`},
	} {
		got := runParity(c.source)
		if got.got != c.want {
			t.Errorf("%s: %s %s, want %s", c.source, got.got, got.detail, c.want)
		}
	}
}
