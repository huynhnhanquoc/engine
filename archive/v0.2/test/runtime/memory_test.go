package runtime_test

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// The bytes a run makes (core/runtime, memory.go): energy counts instructions, and one instruction can make a
// gigabyte, so a run is bounded by what it makes as well, and past its ceiling by what it still holds. Each program
// below holds past a ceiling of 1 MiB what it made in one way only, the rest of it well under, so each stops only when
// that way is counted: MEMORY_LIMIT, which safe(() => ...) does not catch.
const testCeiling = 1 << 20

// madeUnder runs source with a ceiling of bytes: the failure's message, "" when the run ended, and what it counted.
func madeUnder(t *testing.T, source string, ceiling uint64) (string, uint64) {
	t.Helper()
	program := load(t, source)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxBytes = ceiling
	result := program.vm.Run()
	if result.IsInvalid() {
		return result.Message(), program.vm.Bytes
	}
	return "", program.vm.Bytes
}

func TestEachWayOfMakingIsCounted(t *testing.T) {
	for name, source := range map[string]string{
		"a text +":           `const s = "x".repeat(1000); const held = []; for (let i = 0; i < 2000; i++) { held.push(s + i); }`,
		"a template":         "const s = \"x\".repeat(1000); const held = []; for (let i = 0; i < 2000; i++) { held.push(`${s}${i}`); }",
		"repeat":             `const t = "y".repeat(2000000);`,
		"padStart":           `const t = "".padStart(2000000, "x");`,
		"join":               `const a = []; for (let i = 0; i < 60; i++) { a.push("x".repeat(10000)); } const t = a.join("");`,
		"an array's slice":   `const a = []; for (let i = 0; i < 12000; i++) { a.push(0); } const b = a.slice(); const c = a.slice();`,
		"concat":             `const a = []; for (let i = 0; i < 12000; i++) { a.push(0); } const b = a.concat(a);`,
		"map":                `const a = []; for (let i = 0; i < 12000; i++) { a.push(0); } const b = a.map((x) => x); const c = a.map((x) => x);`,
		"push":               `const a = []; for (let i = 0; i < 40000; i++) { a.push(i); }`,
		"an item written":    `const a = []; for (let i = 0; i < 40000; i++) { a[i] = i; }`,
		"a gap":              `const a = []; a[40000] = 1;`,
		"a key written":      `const o = {}; for (let i = 0; i < 20000; i++) { o["k" + i] = i; }`,
		"an object made":     `const held = []; for (let i = 0; i < 20000; i++) { held.push({}); }`,
		"an array made":      `const held = []; for (let i = 0; i < 20000; i++) { held.push([]); }`,
		"Array(n)":           `const a = Array(40000);`,
		"a closure made":     `const held = []; for (let i = 0; i < 6000; i++) { held.push(() => 1); }`,
		"Object.assign":      `const big = {}; for (let i = 0; i < 1000; i++) { big["k" + i] = i; } const held = []; for (let j = 0; j < 20; j++) { held.push(Object.assign({}, big)); }`,
		"a spread":           `const big = {}; for (let i = 0; i < 1000; i++) { big["k" + i] = i; } const held = []; for (let j = 0; j < 20; j++) { held.push({ ...big }); }`,
		"JSON.parse, whole":  `const a = []; for (let i = 0; i < 200; i++) { a.push({ a: i, b: i }); } const text = JSON.stringify(a); const held = []; for (let j = 0; j < 40; j++) { held.push(JSON.parse(text)); }`,
		"safe does not hide": `const r = safe(() => "y".repeat(2000000)); const after = 1;`,
	} {
		t.Run(name, func(t *testing.T) {
			message, made := madeUnder(t, source, testCeiling)
			if !strings.Contains(message, "Memory Limit Exceeded") {
				t.Fatalf("counted %d bytes under a ceiling of %d and ended %q, want the memory limit", made, testCeiling, message)
			}
		})
	}
}

// What a run makes and lets go does not stop it: past its ceiling it is counted again by what it still reaches. A text
// built by += makes every shorter text before the last (some 58 MB here, for 40 KB held), a turn that makes a text and
// drops it, a value through JSON and back: each makes many times the ceiling and holds little.
func TestWhatIsLetGoDoesNotStopARun(t *testing.T) {
	for name, source := range map[string]string{
		"a text built by +=":  `let s = ""; for (let i = 0; i < 3000; i++) { s += "piece " + i + " "; }`,
		"a text each turn":    `let t; for (let i = 0; i < 1000; i++) { t = "x".repeat(10000) + i; }`,
		"JSON there and back": `let o; for (let i = 0; i < 500; i++) { o = JSON.parse(JSON.stringify({ a: "x".repeat(5000) + i, n: [1, 2, 3] })); }`,
		"objects each turn":   `let o; for (let i = 0; i < 40000; i++) { o = { i: i, name: "n" + i }; }`,
		// a callback's own texts, let go as it returns: what the engine holds for it is what it gave back
		"what map's callback lets go":        `const a = []; for (let i = 0; i < 200; i++) { a.push(i); } const b = a.map((x) => { const g = "y".repeat(100000); return 1; });`,
		"what forEach's callback lets go":    `const a = []; for (let i = 0; i < 200; i++) { a.push(i); } a.forEach((x) => "y".repeat(100000));`,
		"what Array.from's map lets go":      `const b = Array.from({ length: 200 }, (x, i) => { const g = "y".repeat(100000); return i; });`,
		"what replace's function lets go":    `const t = "x".repeat(200).replaceAll("x", () => { const g = "y".repeat(100000); return "z"; });`,
		"what a reviver lets go":             `const t = JSON.stringify([1, 2, 3]); let r; for (let i = 0; i < 200; i++) { r = JSON.parse(t, (k, v) => { const g = "y".repeat(100000); return v; }); }`,
		"what a function of the engine gave": `const a = []; for (let i = 0; i < 200; i++) { a.push({ s: "x".repeat(100000) }); a.pop(); } let b; for (let i = 0; i < 200; i++) { b = [{ s: "x".repeat(1000) }].map(JSON.stringify); }`,
	} {
		t.Run(name, func(t *testing.T) {
			message, counted := madeUnder(t, source, testCeiling)
			if message != "" {
				t.Fatalf("ended %q having counted %d bytes, want it to end: it holds little", message, counted)
			}
		})
	}
}

// What Go code of the engine holds for a run while program code it called runs is what the run holds (value.Kept,
// 14/11/2026). A run past its ceiling is counted again by what it can reach, its stack and frames, which never
// showed the items map had made so far, the tree a reviver walks, the text replace writes with a function: each
// program below held some 20 MB that way under a ceiling of 1 MiB, its items let go by each count, and went on. What
// a function of the engine called back gives is counted as that function's result is: a.map(JSON.stringify) made
// 20 MB counted as 200 items.
func TestWhatTheEngineHoldsWhileACallbackRunsIsCounted(t *testing.T) {
	const twoHundred = `const a = []; for (let i = 0; i < 200; i++) { a.push(i); } `
	for name, source := range map[string]string{
		"map":                  twoHundred + `const b = a.map((x) => "y".repeat(100000));`,
		"map through safe":     twoHundred + `const b = a.map((x) => safe(() => "y".repeat(100000)));`,
		"flatMap":              twoHundred + `const b = a.flatMap((x) => ["y".repeat(100000)]);`,
		"Array.from's map":     `const b = Array.from({ length: 200 }, (x, i) => "y".repeat(100000));`,
		"JSON.parse's reviver": `const a = []; for (let i = 0; i < 200; i++) { a.push(i); } const b = JSON.parse(JSON.stringify(a), (k, v) => typeof v === "number" ? "y".repeat(100000) : v);`,
		// what the replacer gives holds a text JSON leaves out, a function's: the text written stays small
		"JSON.stringify's replacer":  twoHundred + `const t = JSON.stringify(a, (k, v) => { if (typeof v !== "number") { return v; } const s = "y".repeat(100000); return { f: () => s }; });`,
		"JSON.stringify called back": `const o = { s: "x".repeat(100000) }; const a = []; for (let i = 0; i < 200; i++) { a.push(o); } const b = a.map(JSON.stringify);`,
		"String called back":         `const s = ["x".repeat(100000)]; const a = []; for (let i = 0; i < 200; i++) { a.push(s); } const b = a.map(String);`,
		// JSON.parse's tree is counted whole with a reviver too: Go made it from the text, the reviver only walked it
		"JSON.parse's tree with a reviver": `const text = () => { const rows = []; for (let i = 0; i < 2000; i++) { rows.push({ n: i }); } return JSON.stringify({ rows: rows }); }; const t = text(); const held = []; for (let j = 0; j < 20; j++) { held.push(JSON.parse(t, (k, v) => v)); }`,
	} {
		t.Run(name, func(t *testing.T) {
			message, made := madeUnder(t, source, testCeiling)
			if !strings.Contains(message, "Memory Limit Exceeded") {
				t.Fatalf("counted %d bytes under a ceiling of %d and ended %q, want the memory limit", made, testCeiling, message)
			}
		})
	}
}

// The array a method walks is held while its callbacks run, named by a variable or not: it is off the stack once the
// call begins. Each program holds 600 KB of a ceiling of 1 MiB while its callback makes texts it lets go; a run that
// holds past half its ceiling has no room, with the array in a variable or made for the call.
func TestTheArrayAMethodWalksIsHeld(t *testing.T) {
	const make = `const make = () => { const a = []; for (let i = 0; i < 25000; i++) { a.push(0); } return a; }; `
	for name, source := range map[string]string{
		"in a variable":        make + `const a = make(); const b = a.filter((x) => { const g = "y".repeat(1000); return false; });`,
		"made for the call":    make + `const b = make().filter((x) => { const g = "y".repeat(1000); return false; });`,
		"forEach, made for it": make + `make().forEach((x) => { const g = "y".repeat(1000); });`,
	} {
		t.Run(name, func(t *testing.T) {
			message, made := madeUnder(t, source, testCeiling)
			if !strings.Contains(message, "Memory Limit Exceeded") {
				t.Fatalf("counted %d bytes and ended %q, want the memory limit: the run holds past half its ceiling", made, message)
			}
		})
	}
}

// What only reads what the run holds makes nothing: a slice of a text, an array's item, the array sort gives back, a
// text joined to "", what Object.values hands back, reduce's accumulator. Counted, each would cross the ceiling many
// times over.
func TestWhatIsOnlyReadIsNotCounted(t *testing.T) {
	for name, source := range map[string]string{
		"a slice of a text":     `const s = "x".repeat(500000); let r = s; for (let i = 0; i < 2000; i++) { r = r.slice(1); }`,
		"an array's item":       `const big = "x".repeat(500000); const a = [big]; let x; for (let i = 0; i < 100; i++) { x = a.find((v) => true); x = a.at(0); x = a.pop(); a.push(x); }`,
		"sort and reverse":      `const a = []; for (let i = 0; i < 1000; i++) { a.push(i); } for (let j = 0; j < 100; j++) { a.sort(); a.reverse(); }`,
		"a text joined to \"\"": `const s = "x".repeat(500000); let t; for (let i = 0; i < 100; i++) { t = s + ""; t = "" + s; }`,
		"Object.values":         `const o = { a: "x".repeat(500000) }; let v; for (let i = 0; i < 100; i++) { v = Object.values(o); }`,
		"reduce":                `const a = ["x".repeat(400000)]; let r; for (let i = 0; i < 100; i++) { r = a.reduce((acc, v) => v, ""); }`,
	} {
		t.Run(name, func(t *testing.T) {
			message, made := madeUnder(t, source, testCeiling)
			if message != "" {
				t.Fatalf("made %d bytes and ended %q, want it to end", made, message)
			}
			if made > testCeiling/2+100_000 {
				t.Fatalf("made %d bytes, want about what its one text holds", made)
			}
		})
	}
}

// The builtins that make much from little, whose size is known before they make it, stop before they allocate: a
// count after the call would let one call make its hundreds of megabytes first (a string is capped at 512 MB, an
// array a builtin makes at 2^24 items). Go's own count of what the run allocated stays far below.
func TestTheLargestStopBeforeTheyAllocate(t *testing.T) {
	for name, source := range map[string]string{
		"repeat":            `const t = "y".repeat(400000000);`,
		"padStart":          `const t = "".padStart(400000000, "x");`,
		"padEnd":            `const t = "x".padEnd(400000000);`,
		"join":              `const s = "x".repeat(100000); const a = []; for (let i = 0; i < 4000; i++) { a.push(s); } const t = a.join("");`,
		"a text's concat":   `const s = "x".repeat(300000); const t = s.concat(` + strings.Repeat("s, ", 119) + `s);`,
		"an array's concat": `const a = []; for (let i = 0; i < 20000; i++) { a.push(0); } const b = a.concat(` + strings.Repeat("a, ", 199) + `a);`,
		"a gap":             `const a = []; a[1000000] = 1;`,
		// what a function gives is written into replace's text, held as it grows (value.Kept)
		"replace with a function": `const t = "x".repeat(2000).replaceAll("x", () => "y".repeat(100000));`,
	} {
		t.Run(name, func(t *testing.T) {
			goruntime.GC()
			var before, after goruntime.MemStats
			goruntime.ReadMemStats(&before)
			message, _ := madeUnder(t, source, testCeiling)
			goruntime.ReadMemStats(&after)
			if !strings.Contains(message, "Memory Limit Exceeded") {
				t.Fatalf("ended %q, want the memory limit", message)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
				t.Fatalf("allocated %d MB before it stopped, want it stopped before", allocated>>20)
			}
		})
	}
}

// What a function of the host gives is counted whole, walked up to the run's whole ceiling (14/11/2026): it was walked
// up to what the run had left, so a run that had made much and held little had its count cut short, and then, counted
// again by what it held, had room for the cut figure. A tree of some 8 MB came in under a ceiling of 1 MiB that way.
func TestWhatTheHostGivesIsCountedPastWhatIsLeft(t *testing.T) {
	program := load(t, `for (let i = 0; i < 9; i++) { const t = "y".repeat(100000); } const b = big();`)
	program.globals["big"] = value.Function(func(...value.Value) value.Value {
		groups := make([]value.Value, 250)
		for g := range groups {
			items := make([]value.Value, 200)
			for i := range items {
				items[i] = value.Object().Set("n", value.Number(float64(i)))
			}
			groups[g] = value.Array(items)
		}
		return value.Array(groups) // 250 arrays of 200 objects, the outer array 6 KB of its own
	})
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxBytes = testCeiling
	if result := program.vm.Run(); !strings.Contains(result.Message(), "Memory Limit Exceeded") {
		t.Fatalf("ended %q holding a tree of some 8 MB under a ceiling of 1 MiB, want the memory limit", result.Message())
	}
}

// What grows from little stops as it grows (value.Budget, 14/11/2026): a builder of the engine asks the run's budget
// before it grows, where counting its result after the call let one call make gigabytes first. Under a ceiling of 1
// MiB, from what the run held: JSON.stringify of one text held a thousand times made 2.6 GB, the array made text by
// +, a template, String() or toString 2 GB, a nested join 2.4 GB, flat 168 MB, Array.from({ length }) 117 MB,
// split("") 67 MB. Each now stops having allocated a few MB, Go's own count; the text of an array a comparison, a key
// or an argument makes too.
func TestWhatGrowsFromLittleStopsAsItGrows(t *testing.T) {
	const refs = `const s = "x".repeat(400000); const a = []; for (let i = 0; i < 1000; i++) { a.push(s); } `
	for name, source := range map[string]string{
		"JSON.stringify":            refs + `const t = JSON.stringify(a);`,
		"an array + a text":         refs + `const t = a + "";`,
		"an array in a template":    refs + "const t = `${a}`;",
		"String(array)":             refs + `const t = String(a);`,
		"toString":                  refs + `const t = a.toString();`,
		"a nested join":             `const s = "x".repeat(400000); const a = []; for (let i = 0; i < 1000; i++) { a.push([s]); } const t = a.join("");`,
		"an array as an argument":   refs + `const t = "y".includes(a);`,
		"an array compared":         refs + `const t = a < "z";`,
		"an array ==":               refs + `const t = a == "z";`,
		"an array as a key":         refs + `const o = {}; const t = o[a];`,
		"an array as a key written": refs + `const o = {}; o[a] = 1;`,
		"flat":                      `const a = []; for (let i = 0; i < 1000; i++) { a.push(0); } const b = []; for (let i = 0; i < 1000; i++) { b.push(a); } const c = b.flat();`,
		"Array.from({ length })":    `const b = Array.from({ length: 3000000 });`,
		"split":                     `const t = "x".repeat(500000).split("");`,
		"split by a separator":      `const t = "a,".repeat(300000).split(",");`,
		"replaceAll, longer":        `const t = "x".repeat(300000).replaceAll("x", "x".repeat(40));`,
		"Object.keys of a text":     `const k = Object.keys("x".repeat(500000));`,
		"Object.entries of a text":  `const e = Object.entries("x".repeat(300000));`,
		"JSON.parse":                `const t = "[" + "{},".repeat(150000) + "{}]"; const b = JSON.parse(t);`,
	} {
		t.Run(name, func(t *testing.T) {
			goruntime.GC()
			var before, after goruntime.MemStats
			goruntime.ReadMemStats(&before)
			message, _ := madeUnder(t, source, testCeiling)
			goruntime.ReadMemStats(&after)
			if !strings.Contains(message, "Memory Limit Exceeded") {
				t.Fatalf("ended %q, want the memory limit", message)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("allocated %d MB before it stopped, want it stopped as it grew", allocated>>20)
			}
		})
	}
}

// A run nested in another's host call makes no more than that run has left, and what it made goes back to it: the
// outer run makes 600 KB, the inner one is lent the rest of 1 MiB and stops at it, and the outer one stops with it.
// Within its ceiling, what the inner run made is the outer one's: two of 400 KB each make it stop at the third.
func TestANestedRunDrawsOnTheBytesOfTheRunItIsIn(t *testing.T) {
	nest := func(outerSource, innerSource string) (string, string) {
		inner := load(t, innerSource)
		outer := load(t, outerSource)
		innerSays := ""
		outer.globals["inner"] = value.Function(func(arguments ...value.Value) value.Value {
			inner.vm.FastReset(inner.program, inner.globals)
			inner.vm.MaxBytes = 64 << 20
			inner.vm.NestIn(outer.vm)
			result := inner.vm.Run()
			inner.vm.Unnest(result)
			if result.IsInvalid() {
				innerSays = result.Message()
			}
			return value.Undefined()
		})
		outer.vm.FastReset(outer.program, outer.globals)
		outer.vm.MaxBytes = testCeiling
		if result := outer.vm.Run(); result.IsInvalid() {
			return result.Message(), innerSays
		}
		return "", innerSays
	}
	outerSays, innerSays := nest(`const s = "x".repeat(600000); inner(); const after = 1;`, `const t = "y".repeat(600000);`)
	if !strings.Contains(innerSays, "Memory Limit Exceeded") || !strings.Contains(outerSays, "Memory Limit Exceeded") {
		t.Errorf("inner %q, outer %q: want the inner run stopped at what the outer one had left, and the outer with it", innerSays, outerSays)
	}
	outerSays, innerSays = nest(`inner(); inner(); inner(); const after = 1;`, `const t = "y".repeat(400000);`)
	if innerSays != "" && !strings.Contains(innerSays, "Memory Limit Exceeded") || !strings.Contains(outerSays, "Memory Limit Exceeded") {
		t.Errorf("inner %q, outer %q: want what the inner runs made counted as the outer one's", innerSays, outerSays)
	}

	// a run nested before the outer one showed what it has left is lent nothing; what it hands back past the outer
	// run's ceiling stops that run at its next check
	inner := load(t, `const t = "y".repeat(2000000);`)
	outer := load(t, `let n = 0; for (let i = 0; i < 1000; i++) { n = n + i; }`)
	outer.vm.FastReset(outer.program, outer.globals)
	outer.vm.MaxBytes = testCeiling
	inner.vm.FastReset(inner.program, inner.globals)
	inner.vm.MaxBytes = 64 << 20
	inner.vm.NestIn(outer.vm)
	inner.vm.Unnest(inner.vm.Run())
	if result := outer.vm.Run(); !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit Exceeded") {
		t.Errorf("the outer run ended %q after 2 MB handed back, want the memory limit", result.Message())
	}
}

// Without a ceiling nothing stops, and what a run made is still counted: the host reads it to give the next run of
// the request what is left.
func TestNoCeilingCountsWithoutStopping(t *testing.T) {
	message, made := madeUnder(t, `const t = "y".repeat(2000000);`, 0)
	if message != "" {
		t.Fatalf("ended %q without a ceiling", message)
	}
	if made != 0 {
		t.Logf("made %d bytes without a ceiling (not counted: a VM without one skips the meter)", made)
	}
}
