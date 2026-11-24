package runtime_test

import (
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/kitwork/engine2/core/value"
)

// The VM reads s[i] and s.length, and walks for (const c of s), through one text cursor (04/11/2026):
// each read of a string goes on from where the last read of the same string stood, so a loop over a
// string that is not ASCII is linear. It walked the string from the start for each s[i] and each
// s.length, twice a turn (4.6 ms for 1,000 units of Vietnamese).

// units is what JavaScript reads of s, unit by unit: each UTF-16 unit as a one-unit string, a lone
// surrogate as its three-byte form (core/value text.go).
func units(s string) []string {
	var out []string
	for _, unit := range utf16.Encode([]rune(s)) {
		out = append(out, string(utf16.Decode([]uint16{unit})))
		if unit >= 0xD800 && unit <= 0xDFFF {
			// one half of a pair, as Kitwork keeps it: the code point written as UTF-8 would write it
			out[len(out)-1] = string([]byte{0xED, byte(0xA0 | (unit>>6)&0x1F), byte(0x80 | unit&0x3F)})
		}
	}
	return out
}

func TestAProgramReadsAStringsUnitsThroughTheCursor(t *testing.T) {
	pile := string(rune(0x1F600)) // an emoji: two units
	texts := []string{
		"Phường Bến Thành",
		"a" + pile + "b" + pile,
		"한국어 텍스트",
		"plain ASCII text",
		"",
	}
	for _, text := range texts {
		program := load(t, `
forward = [];
for (let i = 0; i < s.length; i++) { forward.push(s[i]); }
backward = [];
for (let i = s.length - 1; i >= 0; i--) { backward.push(s[i]); }
walked = [];
for (const c of s) { walked.push(c); }
mixed = [];
for (let i = 0; i < s.length; i++) { mixed.push(s[s.length - 1 - i] + other[i % other.length] + s[i]); }
edges = [s[-1], s[s.length], s[1.5], s.length];
`)
		program.globals["s"] = value.String(text)
		program.globals["other"] = value.String("xyz" + pile)
		program.run(t)
		want := units(text)
		other := units("xyz" + pile)
		check := func(name string, got value.Value, expect []string) {
			t.Helper()
			items := got.Items()
			if len(items) != len(expect) {
				t.Errorf("%q %s: %d units, want %d", text, name, len(items), len(expect))
				return
			}
			for i, item := range items {
				if item.Text() != expect[i] {
					t.Errorf("%q %s[%d] = %q, want %q", text, name, i, item.Text(), expect[i])
				}
			}
		}
		check("forward", program.variable("forward"), want)
		reversed := make([]string, len(want))
		mixed := make([]string, len(want))
		for i := range want {
			reversed[i] = want[len(want)-1-i]
			// joined as the program joins them: two halves of a pair that meet are one character
			mixed[i] = value.String(want[len(want)-1-i]).Add(value.String(other[i%len(other)])).Add(value.String(want[i])).Text()
		}
		check("backward", program.variable("backward"), reversed)
		check("mixed", program.variable("mixed"), mixed)
		edges := program.variable("edges").Items()
		if !edges[0].IsUndefined() || !edges[1].IsUndefined() || !edges[2].IsUndefined() || edges[3].Number() != float64(len(want)) {
			t.Errorf("%q: s[-1], s[length], s[1.5], length read %s", text, value.Stringify(program.variable("edges"), "").Text())
		}
		// for…of walks the code points, as JavaScript's iterator does (16/11/2026: it walked the units, a pair two
		// halves); a lone half of a pair, which the string holds as no character, is one item
		var points []string
		for _, r := range text {
			points = append(points, string(r))
		}
		check("for of", program.variable("walked"), points)
	}
}

// A loop over a string costs in proportion to its length: ten times the units, about ten times the
// time, never the hundred times of a walk from the start for each read.
func TestALoopOverAStringIsLinear(t *testing.T) {
	for _, c := range []struct{ name, loop string }{
		{"s[i] to s.length", `x = ""; for (let i = 0; i < s.length; i++) { x = s[i]; }`},
		{"for of", `x = ""; for (const c of s) { x = c; }`},
	} {
		short := load(t, c.loop)
		short.globals["s"] = value.String(strings.Repeat("Phường Bến", 40))
		long := load(t, c.loop)
		long.globals["s"] = value.String(strings.Repeat("Phường Bến", 400))
		requireCost(t, c.name+" over 4,000 units against 400", long.runs(t), short.runs(t), 25)
	}
}

// Reading an ASCII string's units allocates nothing per unit: each is a string made once.
func TestReadingAnASCIIStringAllocatesNothingPerUnit(t *testing.T) {
	allocations := map[int]float64{}
	for _, size := range []int{100, 1000} {
		program := load(t, `x = ""; for (let i = 0; i < s.length; i++) { x = s[i]; } for (const c of s) { x = c; }`)
		program.globals["s"] = value.String(strings.Repeat("abcdefghij", size/10))
		allocations[size] = program.allocationsPerRun(t)
	}
	if allocations[1000] != allocations[100] {
		t.Errorf("1,000 units make %.0f allocations, 100 make %.0f", allocations[1000], allocations[100])
	}
}

// The cursor forgets the string as the VM resets: a VM back in the pool holds nothing of the last run's
// text, which another app's run would otherwise keep alive.
func TestAResetVMLetsGoOfTheLastString(t *testing.T) {
	program := load(t, `x = s[3] + s.length;`)
	freed := make(chan struct{})
	func() {
		bytes := []byte(strings.Repeat("Phường Bến", 1000))
		goruntime.SetFinalizer(&bytes[0], func(*byte) { close(freed) })
		program.globals["s"] = value.String(unsafe.String(&bytes[0], len(bytes)))
		program.run(t)
		if got := program.variable("x").Text(); got != "ờ"+strconv.Itoa(len(units(strings.Repeat("Phường Bến", 1000)))) {
			t.Fatalf("x = %q", got)
		}
		delete(program.globals, "s")
		program.vm.ResetForPool()
	}()
	// the VM stays alive, as a VM in the pool does: only what it holds can keep the string
	defer goruntime.KeepAlive(program.vm)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		goruntime.GC()
		select {
		case <-freed:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Error("the VM still holds the last run's string after ResetForPool")
}
