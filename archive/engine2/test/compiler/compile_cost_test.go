package compiler_test

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// compileAllocations is how many allocations compiling source makes.
func compileAllocations(t *testing.T, source string) float64 {
	t.Helper()
	if _, err := compiler.CompileSource(source); err != nil {
		t.Fatal(err)
	}
	return testing.AllocsPerRun(10, func() {
		_, _ = compiler.CompileSource(source)
	})
}

// Each ${…} of a template is parsed by a parser of its own, which costs what its expression costs and
// little more (04/11/2026): the parse functions of each token kind are one table every parser reads. A
// parser built both tables, two maps of bound methods, 64 allocations and 5-8 µs for each ${…}.
func TestATemplatesInterpolationCostsWhatItsExpressionCosts(t *testing.T) {
	template := func(parts int) string {
		var b strings.Builder
		b.WriteString("const a = 1; t = `")
		for i := 0; i < parts; i++ {
			b.WriteString("x ${a + " + strconv.Itoa(i) + "} ")
		}
		b.WriteString("`;")
		return b.String()
	}
	each := (compileAllocations(t, template(110)) - compileAllocations(t, template(10))) / 100
	t.Logf("%.1f allocations for each ${a + i}", each)
	if each > 30 {
		t.Errorf("each ${a + i} of a template costs %.1f allocations, want at most 30", each)
	}
}

// A long line compiles in time proportional to its length (04/11/2026): the column of a place before the
// last one counted on its line (an operator, placed after its right operand) is counted back from that
// one, not again from the line's start. Columns are characters, so a line of Vietnamese gets the same.
func TestALongLineCompilesInLinearTime(t *testing.T) {
	line := func(terms int) string {
		var b strings.Builder
		b.WriteString(`const a = 1; const b = "Phường"; r = a`)
		for i := 0; i < terms; i++ {
			b.WriteString(" + b.length * " + strconv.Itoa(i))
		}
		b.WriteString(";")
		return b.String()
	}
	short, long := line(90), line(900)
	compile := func(source string) func(int) {
		return func(rounds int) {
			for i := 0; i < rounds; i++ {
				if _, err := compiler.CompileSource(source); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if testing.CoverMode() != "" {
		t.Skip("costs are not compared under coverage counters")
	}
	// five measures, one within is enough: a busy machine slowed every trial of one side once (12/11/2026),
	// a line counted again from its start takes a hundred times, every time
	var ratios []string
	for attempt := 0; ; attempt++ {
		ratio := bestRatio(compile(long), compile(short))
		ratios = append(ratios, strconv.FormatFloat(ratio, 'f', 1, 64))
		if ratio <= 20 {
			t.Logf("a line of 900 terms takes %s times one of 90", strings.Join(ratios, ", then "))
			break
		}
		if attempt == 4 {
			t.Errorf("a line of 900 terms takes %s times one of 90, want about 10", strings.Join(ratios, ", "))
			break
		}
	}
	// a place found by counting back is where the source has it, in characters: the + before Math.sqr
	_, err := compiler.CompileSource(`let b = "Phường"; let r = b.length * 2 + Math.sqr(b.length * 3 + 1);`)
	if err == nil || !strings.Contains(err.Error(), "1:47: Math không có sqr") {
		t.Errorf("the error of Math.sqr on a line of Vietnamese: %v, want it at 1:47", err)
	}
}

// bestRatio is the best time of measured over the best of reference, run alternately for the same
// rounds, which grow until the quicker side takes a few milliseconds.
func bestRatio(measured, reference func(int)) float64 {
	timed := func(f func(int), rounds int) float64 {
		start := time.Now()
		f(rounds)
		return float64(time.Since(start))
	}
	rounds := 1
	for rounds < 1<<20 {
		if min(timed(measured, rounds), timed(reference, rounds)) >= float64(10*time.Millisecond) {
			break
		}
		rounds *= 2
	}
	measuredBest, referenceBest := math.MaxFloat64, math.MaxFloat64
	for trial := 0; trial < 7; trial++ {
		measuredBest = min(measuredBest, timed(measured, rounds))
		referenceBest = min(referenceBest, timed(reference, rounds))
	}
	return measuredBest / referenceBest
}

// Checking how deep a program nests walks its tree with one buffer for the children of every node
// (04/11/2026): a slice for each node was a tenth of what a compile allocated at 1,000 lines.
func TestANodeOfTheTreeCostsNoBufferOfItsOwn(t *testing.T) {
	statements := func(count int) string {
		var b strings.Builder
		b.WriteString("x = 0;\n")
		for i := 0; i < count; i++ {
			b.WriteString("x = (x + 1) * 2 - x / 3;\n")
		}
		return b.String()
	}
	each := (compileAllocations(t, statements(1100)) - compileAllocations(t, statements(100))) / 1000
	t.Logf("%.1f allocations for each statement", each)
	// 19 now (22 under -race), 30 with a slice for each node
	if each > 26 {
		t.Errorf("each x = (x + 1) * 2 - x / 3; costs %.1f allocations, want at most 26", each)
	}
}

// A program past 64 KB of bytecode, whose jumps are three bytes, is emitted once (04/11/2026): its
// jumps are made wide where they stand once its code is written. It was written a second time, every
// jump wide from the start, so the copy of a block that took a program past 64 KB cost the emission of
// the whole program again.
func TestAProgramPast64KBIsEmittedOnce(t *testing.T) {
	program := func(copies int) string {
		var b strings.Builder
		for k := 0; k < copies; k++ {
			n := strconv.Itoa(k)
			b.WriteString("const f" + n + " = (x) => { let s = 0; for (let i = 0; i < x; i++) { if (i % 2 === 0) { s = s + i; } else { s = s - " + n + "; } } return s; };\n")
		}
		return b.String()
	}
	wide := func(copies int) bool {
		bytecode, err := compiler.CompileSource(program(copies))
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := runtime.InspectProgram(bytecode.Program)
		if err != nil {
			t.Fatal(err)
		}
		for _, instruction := range inspection.Instructions {
			if instruction.Name == "JUMP_WIDE" {
				return true
			}
		}
		return false
	}
	// the fewest copies whose program is wide
	low, high := 1, 2
	for !wide(high) {
		low, high = high, high*2
	}
	for high-low > 1 {
		if middle := (low + high) / 2; wide(middle) {
			high = middle
		} else {
			low = middle
		}
	}
	narrowCopy := compileAllocations(t, program(low)) - compileAllocations(t, program(low-1))
	wideCopy := compileAllocations(t, program(high)) - compileAllocations(t, program(low))
	t.Logf("%d copies are wide: the copy that makes the program wide costs %.0f allocations, one before it %.0f", high, wideCopy, narrowCopy)
	if wideCopy > 2*narrowCopy+50 {
		t.Errorf("the copy that takes the program past 64 KB costs %.0f allocations, one before it %.0f: the program is emitted twice", wideCopy, narrowCopy)
	}
}
