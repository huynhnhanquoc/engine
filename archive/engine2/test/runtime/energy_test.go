package runtime_test

import (
	"context"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// Energy by work (core/runtime, work.go; core/value, work.go, 15/11/2026): what the engine's code does for a run costs
// its energy as the run's own instructions do, so a loop of one builtin over a large value stops at the run's energy
// as a loop of additions does, where it ran until the request's time (30 s) on 1% of its energy.

// requestEnergy is a request's energy, the host's default.
const requestEnergy = 10_000_000

// spentUnder runs source with energy units, a ceiling of 64 MiB and a time of within: the failure's message ("" for
// none), what it spent, how long it took.
func spentUnder(t *testing.T, source string, energy uint64, within time.Duration) (string, uint64, time.Duration) {
	t.Helper()
	program := load(t, source)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxEnergy = energy
	program.vm.MaxBytes = 64 << 20
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	program.vm.Context = ctx
	started := time.Now()
	result := program.vm.Run()
	took := time.Since(started)
	if result.IsInvalid() {
		return result.Message(), program.vm.Energy, took
	}
	return "", program.vm.Energy, took
}

// Each program below is a loop whose turn is one call over a large value, for far more turns than a request's energy
// pays for at the price of a call: each stops at its energy, not at its time, within a few times what a loop of
// additions takes for the same energy (a unit is what a plain instruction takes). What it loops over is made by
// builtins, which cost what they make: a loop of a hundred thousand pushes spends 16 million units, past the energy
// before the call tried begins.
func TestEnergyEndsWhatTheEngineDoes(t *testing.T) {
	const forever = "for (let i = 0; i < 100000000; i++) "
	_, _, plain := spentUnder(t, "let x = 0; "+forever+"{ x = x + i; }", requestEnergy, 10*time.Second)
	for name, source := range map[string]string{
		"includes":               `const s = "x".repeat(20000000); ` + forever + `{ s.includes("y"); }`,
		"indexOf":                `const s = "x".repeat(20000000); ` + forever + `{ s.indexOf("y"); }`,
		"a text's slice":         `const s = "x".repeat(20000000); ` + forever + `{ s.slice(0, 1); }`,
		"a slice of Vietnamese":  `const s = "ệ".repeat(3000000); ` + forever + `{ s.slice(0, 1); }`,
		"toUpperCase":            `const s = "X".repeat(20000000); ` + forever + `{ s.toUpperCase(); }`,
		"toLowerCase":            `const s = "x".repeat(20000000); ` + forever + `{ s.toLowerCase(); }`,
		"split":                  `const s = "x".repeat(20000000); ` + forever + `{ s.split("y"); }`,
		"trim":                   `const s = " ".repeat(20000000); ` + forever + `{ s.trim(); }`,
		"charCodeAt":             `const s = "x".repeat(20000000); ` + forever + `{ s.charCodeAt(19999999); }`,
		"an array's includes":    `const a = Array.from({ length: 1000000 }); ` + forever + `{ a.includes(1); }`,
		"a long needle":          `const s = "x".repeat(1000000); const a = []; for (let j = 0; j < 30; j++) { a.push(s); } const n = "x".repeat(999999) + "y"; ` + forever + `{ a.includes(n); }`,
		"reverse":                `const a = Array.from({ length: 1000000 }); ` + forever + `{ a.reverse(); }`,
		"fill":                   `const a = Array.from({ length: 1000000 }); ` + forever + `{ a.fill(0); }`,
		"shift and unshift":      `const a = Array.from({ length: 1000000 }); ` + forever + `{ a.unshift(a.shift()); }`,
		"sort":                   `const a = Array.from({ length: 100000 }, (_, j) => (j * 7919) % 100003); ` + forever + `{ a.sort(); }`,
		"toSorted":               `const a = Array.from({ length: 100000 }, (_, j) => "k" + (j * 7919) % 100003); ` + forever + `{ a.toSorted(); }`,
		"join":                   `const a = Array.from({ length: 100000 }, (_, j) => j); ` + forever + `{ a.join(","); }`,
		"an array made text":     `const a = Array.from({ length: 100000 }, (_, j) => j); ` + forever + `{ String(a); }`,
		"flat":                   `const a = Array.from({ length: 100000 }, (_, j) => [j]); ` + forever + `{ a.flat(); }`,
		"JSON.stringify":         `const s = "x".repeat(5000000); ` + forever + `{ JSON.stringify(s); }`,
		"JSON.parse":             `const s = " ".repeat(20000000) + "1"; ` + forever + `{ JSON.parse(s); }`,
		"Object.keys":            `const o = {}; for (let j = 0; j < 20000; j++) { o["k" + j] = j; } ` + forever + `{ Object.keys(o); }`,
		"Object.entries":         `const o = {}; for (let j = 0; j < 20000; j++) { o["k" + j] = j; } ` + forever + `{ Object.entries(o); }`,
		"Object.assign":          `const o = {}; for (let j = 0; j < 20000; j++) { o["k" + j] = j; } ` + forever + `{ Object.assign({}, o); }`,
		"Object.fromEntries":     `const o = {}; for (let j = 0; j < 20000; j++) { o["k" + j] = j; } const e = Object.entries(o); ` + forever + `{ Object.fromEntries(e); }`,
		"parseInt":               `const s = "1".repeat(20000000); ` + forever + `{ parseInt(s); }`,
		"encodeURIComponent":     `const s = "x".repeat(5000000); ` + forever + `{ encodeURIComponent(s); }`,
		"a text made and let go": `const s = "x".repeat(10000000); ` + forever + `{ const t = s + "y"; }`,
	} {
		t.Run(name, func(t *testing.T) {
			message, spent, took := spentUnder(t, source, requestEnergy, 5*time.Second)
			if !strings.Contains(message, "Energy Limit Exceeded") {
				t.Fatalf("ended %q after %v having spent %d units, want its energy spent", message, took, spent)
			}
			t.Logf("%v for its energy, %.1f times a loop of additions (%v)", took, float64(took)/float64(plain), plain)
			if !underRace && testing.CoverMode() == "" && took > 25*plain+100*time.Millisecond {
				t.Errorf("took %v for its energy, %.0f times a loop of additions (%v): its work is charged too little", took, float64(took)/float64(plain), plain)
			}
		})
	}
}

// What a run spends is counted from the sizes of what it reads and writes, never from a clock: the same run spends the
// same energy each time, and under load.
func TestTheSameRunSpendsTheSameEnergy(t *testing.T) {
	source := `const s = "xy".repeat(100000); const a = []; for (let j = 0; j < 2000; j++) { a.push(j); }
let n = 0; for (let i = 0; i < 20; i++) { n = n + s.indexOf("z") + a.join(",").length + JSON.stringify(a).length + a.toSorted().length; }`
	_, first, _ := spentUnder(t, source, 0, time.Minute)
	for range 3 {
		if _, again, _ := spentUnder(t, source, 0, time.Minute); again != first {
			t.Fatalf("spent %d units, then %d", first, again)
		}
	}
	if first < 1_000_000 {
		t.Fatalf("spent %d units: the work of its builtins is not counted", first)
	}
}

// A sort is counted before it begins: one the run's energy cannot pay for never sorts, so a run past its energy does
// not first spend the seconds a sort of a large array takes.
func TestASortPastTheEnergyNeverBegins(t *testing.T) {
	program := load(t, `const a = Array.from({ length: 200000 }, (_, j) => 200000 - j); a.sort();`)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxEnergy = requestEnergy
	result := program.vm.Run()
	if !result.IsInvalid() || !strings.Contains(result.Message(), "Energy Limit Exceeded") {
		t.Fatalf("ended %v, want its energy spent", result)
	}
	if first := program.variable("a").Items()[0].Number(); first != 200000 {
		t.Fatalf("the array begins with %v: the sort began", first)
	}
}

// A builder spends as it writes: JSON.stringify of a text held fifty times stops near its energy, having made a few
// megabytes, not the fifty it would write.
func TestABuilderStopsAsItWrites(t *testing.T) {
	program := load(t, `const s = "x".repeat(1000000); const a = []; for (let j = 0; j < 50; j++) { a.push(s); } const t = JSON.stringify(a);`)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxEnergy = 3_000_000
	program.vm.MaxBytes = 1 << 30
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	result := program.vm.Run()
	goruntime.ReadMemStats(&after)
	if !result.IsInvalid() || !strings.Contains(result.Message(), "Energy Limit Exceeded") {
		t.Fatalf("ended %v, want its energy spent", result)
	}
	if made := after.TotalAlloc - before.TotalAlloc; made > 20<<20 {
		t.Fatalf("made %d MB before it stopped, want a few", made>>20)
	}
}

// What costs a call is what it reads, not what it is handed: a push of a large text, an array's at, a slice of an
// array, a text's toString cost nothing past their call (what they make is counted as made), so a loop of them on
// large values spends as a loop on small ones.
func TestWhatIsNotReadCostsNothing(t *testing.T) {
	small := `const s = "x"; const a = [1, 2, 3]; let n = 0; for (let i = 0; i < 1000; i++) { a.push(s); a.pop(); n = n + a.at(0) + a.slice(0, 1).length + s.toString().length; }`
	large := `const s = "x".repeat(1000000); const a = Array.from({ length: 1000000 }); let n = 0; for (let i = 0; i < 1000; i++) { a.push(s); a.pop(); n = n + a.at(0) + a.slice(0, 1).length + s.toString().length; }`
	_, onSmall, _ := spentUnder(t, small, 0, time.Minute)
	_, onLarge, _ := spentUnder(t, large, 0, time.Minute)
	// the large program makes its million items first, which a loop of a million pushes spends; the loop is the same
	_, making, _ := spentUnder(t, `const s = "x".repeat(1000000); const a = Array.from({ length: 1000000 });`, 0, time.Minute)
	_, makingSmall, _ := spentUnder(t, `const s = "x"; const a = [1, 2, 3];`, 0, time.Minute)
	if loopLarge, loopSmall := onLarge-making, onSmall-makingSmall; loopLarge > loopSmall+loopSmall/10 {
		t.Fatalf("the loop spent %d units on large values, %d on small ones", loopLarge, loopSmall)
	}
}
