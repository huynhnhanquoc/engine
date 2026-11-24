package runtime_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A function's variables, and the top level's block variables, live in slots (bytecode 7). What a
// slot held must never show where the program reads it before it writes it: not the last call's, not
// the last run's, not another app's that ran on the same VM before.

func compiled(t *testing.T, source string) *runtime.Program {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	return bytecode.Program
}

func installed(extra map[string]value.Value) map[string]value.Value {
	globals := map[string]value.Value{}
	builtins.Install(globals)
	for name, item := range extra {
		globals[name] = item
	}
	return globals
}

func runOn(t *testing.T, vm *runtime.VM, program *runtime.Program, globals map[string]value.Value) {
	t.Helper()
	vm.FastReset(program, globals)
	if result := vm.Run(); result.IsInvalid() {
		t.Fatalf("run: %s", result.Message())
	}
}

// One VM runs another app's program, which writes a secret into the top level's first block slot and a
// function's second slot, then a program that reads the same slots before it writes them, twice: each
// read is the failure NOT_INITIALIZED, never what a slot held. Within one run, a function called again reads its
// slot before writing it as NOT_INITIALIZED too, not as the last call left it. A read before its line in the
// function that declares the name is a compile error since 15/11/2026 (JavaScript's temporal dead zone), so each
// read is made by a function called before the line, which the compiler cannot see; since 18/11/2026 the scope
// marks such a variable as it begins (UNSET_SLOT), and the read, which was undefined, is JavaScript's
// ReferenceError, caught here by safe.
func TestASlotReadBeforeItsWriteNeverShowsTheLastRun(t *testing.T) {
	writer := compiled(t, `{ let secret = "tenant A secret"; } const f = () => { let a = 1; let secret = "tenant A secret"; return a; }; f();`)
	reader := compiled(t, `
root = "unset";
local = "unset";
again = "unset";
{ const read = () => "" + later; root = safe(read).error.code; let later = "this run's"; }
const g = () => { const read = () => "" + hidden; const shown = safe(read).error.code; let hidden = "this run's"; return shown; };
local = g();
const h = (first) => { const read = () => "" + kept; const shown = first ? "" : safe(read).error.code; let kept = "the first call's"; return shown; };
h(true);
again = h(false);
`)
	vm := runtime.New(writer)
	vm.MaxEnergy = 1_000_000
	globals := installed(nil)
	runOn(t, vm, writer, globals)
	for run := 1; run <= 2; run++ {
		runOn(t, vm, reader, globals)
		for _, name := range []string{"root", "local", "again"} {
			if got := vm.Vars[name].ToString().Text(); got != "NOT_INITIALIZED" {
				t.Errorf("run %d: %s read %q before its write, want NOT_INITIALIZED", run, name, got)
			}
		}
	}
}

// A frame whose slots a closure holds is detached, not cleared, when the VM is reset and when its frame
// runs another call: closures of an earlier run keep the values they captured, the top level's (a block
// variable) and a function's, while the next run makes its own.
func TestACapturedFrameIsDetachedNotCleared(t *testing.T) {
	program := compiled(t, `
const make = () => { let n = seed; return () => n; };
keep = make();
{ let m = seed * 10; keepRoot = () => m; }
`)
	vm := runtime.New(program)
	vm.MaxEnergy = 1_000_000
	runOn(t, vm, program, installed(map[string]value.Value{"seed": value.Number(1)}))
	firstKeep, firstRoot := vm.Vars["keep"], vm.Vars["keepRoot"]
	runOn(t, vm, program, installed(map[string]value.Value{"seed": value.Number(2)}))
	secondKeep, secondRoot := vm.Vars["keep"], vm.Vars["keepRoot"]

	for _, c := range []struct {
		name    string
		closure value.Value
		want    float64
	}{
		{"the first run's function closure", firstKeep, 1},
		{"the first run's top-level closure", firstRoot, 10},
		{"the second run's function closure", secondKeep, 2},
		{"the second run's top-level closure", secondRoot, 20},
	} {
		got := c.closure.Call(nil)
		if got.IsInvalid() || got.Number() != c.want {
			t.Errorf("%s gives %s, want %v", c.name, got.ToString().Text(), c.want)
		}
	}
}

// Closures keep JavaScript's capture by reference with slots: a variable is shared by the function and
// every closure of it, a loop's let is one per iteration, a closure reaches every function around it, a
// recursive function's closures keep their own call's variables, a closure outlives its frame, and a
// closure made in a callback (the runtime's map and forEach, core/value's findLast and sort) keeps its
// own call's. Each result is what node gives.
func TestClosuresCaptureByReference(t *testing.T) {
	for _, c := range []struct {
		name, source, want string
	}{
		{"shared by two closures and the function",
			`const make = () => { let n = 0; const inc = () => { n = n + 1; }; const get = () => n; inc(); inc(); return [get(), n]; }; r = JSON.stringify(make());`,
			`[2,2]`},
		{"a write from a closure is seen by the function",
			`const f = () => { let n = 1; const set = () => { n = 5; }; set(); return n; }; r = "" + f();`, `5`},
		{"each call has its own",
			`const make = () => { let n = 0; return [() => { n = n + 1; return n; }, () => n]; }; const a = make(); const b = make(); a[0](); a[0](); b[0](); r = JSON.stringify([a[1](), b[1]()]);`,
			`[2,1]`},
		{"a parameter assigned after the closure is made",
			`const f = (x) => { const g = () => x; x = 9; return g(); }; r = "" + f(1);`, `9`},
		{"a loop's let, one per iteration",
			`const f = () => { const fs = []; for (let i = 0; i < 3; i++) { fs.push(() => i); } return fs.map((g) => g()); }; r = JSON.stringify(f());`,
			`[0,1,2]`},
		{"an iteration's variable written by its closure",
			`const f = () => { const fs = []; for (let i = 0; i < 3; i++) { fs.push(() => { i = i + 10; return i; }); } return [fs[0](), fs[0](), fs[1](), fs[2]()]; }; r = JSON.stringify(f());`,
			`[10,20,11,12]`},
		{"nested loops' iterations",
			`const fs = []; for (let i = 0; i < 2; i++) { for (const x of [5, 6]) { fs.push(() => () => i * 10 + x); } } r = JSON.stringify(fs.map((g) => g()()));`,
			`[5,6,15,16]`},
		{"three functions around",
			`const a = (x) => (y) => (z) => { x = x + 100; return x + y + z; }; const c = a(1)(2); c(3); r = "" + c(3);`, `206`},
		{"recursion, each call's closure its own",
			`const f = (n) => { const get = () => n; if (n === 0) { return [get]; } const rest = f(n - 1); rest.push(get); return rest; }; const first = f(3); const second = f(2); r = JSON.stringify([first.map((g) => g()), second.map((g) => g())]);`,
			`[[0,1,2,3],[0,1,2]]`},
		{"a recursive local function",
			`const outer = () => { const fact = (n) => n <= 1 ? 1 : n * fact(n - 1); return fact(10); }; r = "" + outer();`, `3628800`},
		{"closures outliving their frame",
			`const make = (v) => { let w = v * 2; return () => w; }; const a = make(1); const b = make(2); const c = make(3); r = JSON.stringify([a(), b(), c()]);`,
			`[2,4,6]`},
		{"made in the runtime's map and forEach",
			`const fs = [10, 20, 30].map((x) => () => x); const gs = []; [1, 2].forEach((x) => { gs.push(() => x); }); r = JSON.stringify([fs.map((g) => g()), gs.map((g) => g())]);`,
			`[[10,20,30],[1,2]]`},
		{"made in core/value's findLast and sort",
			`const fs = []; [1, 2, 3].findLast((x) => { fs.push(() => x); return false; }); const seen = []; const direct = []; [3, 1, 2].sort((a, b) => { seen.push(() => a * 10 + b); direct.push(a * 10 + b); return a - b; }); r = JSON.stringify([fs.map((g) => g()), JSON.stringify(seen.map((g) => g())) === JSON.stringify(direct)]);`,
			`[[3,2,1],true]`},
		{"a top-level block's variable",
			`const out = []; { let a = 1; const f = () => a; a = 2; out.push(f()); } if (true) { const b = 3; out.push((() => () => b)()()); } r = JSON.stringify(out);`,
			`[2,3]`},
		{"a name nothing declares, written after the closure is made",
			`const f = () => { const get = () => total; total = 5; return get(); }; r = "" + f();`, `5`},
	} {
		program := load(t, c.source)
		program.run(t)
		if got := program.variable("r").ToString().Text(); got != c.want {
			t.Errorf("%s: r = %s, want %s", c.name, got, c.want)
		}
	}
}

// A closure that reads no variable by name keeps nothing of the top level's map, so the next run does
// not make the map again: with a hundred top-level names and such a closure, a run costs the closure
// alone. A closure that reads a top-level name keeps the map, and still reads it after the next run
// began, as a closure handed to another VM does.
func TestATopLevelClosureThatReadsNoNameLeavesTheMap(t *testing.T) {
	var names strings.Builder
	for i := 0; i < 100; i++ {
		names.WriteString("const v" + strconv.Itoa(i) + " = " + strconv.Itoa(i) + "; ")
	}
	program := load(t, names.String()+"const f = (x) => x + 1; t = f(v99);")
	program.run(t)
	if got := program.variable("t").Number(); got != 100 {
		t.Fatalf("t = %v, want 100", got)
	}
	if allocations := program.allocationsPerRun(t); allocations > 2 {
		t.Errorf("%.0f allocations a run, want at most 2 (the closure)", allocations)
	} else {
		t.Logf("%.0f allocations a run", allocations)
	}

	reading := compiled(t, "const base = seed; const f = (x) => x + base; t = f(1);")
	vm := runtime.New(reading)
	vm.MaxEnergy = 1_000_000
	runOn(t, vm, reading, installed(map[string]value.Value{"seed": value.Number(100)}))
	f := vm.Vars["f"]
	runOn(t, vm, reading, installed(map[string]value.Value{"seed": value.Number(200)}))
	if got := f.Call([]value.Value{value.Number(1)}); got.Number() != 101 {
		t.Errorf("the last run's closure gives %s, want 101 (its run's base, 100)", got.ToString().Text())
	}
}

// A call of a function of many slots pays one unit of energy for each past the first 256, as its frame
// is made or cleared whole: a function of 300 slots costs 44 more than one of none, whatever it runs.
func TestAFrameOfManySlotsPaysForThem(t *testing.T) {
	var locals strings.Builder
	for i := 0; i < 300; i++ {
		locals.WriteString("let v" + strconv.Itoa(i) + " = 0; ")
	}
	energy := func(body string) uint64 {
		program := load(t, "const f = () => { if (false) { "+body+"} return 1; }; f();")
		program.run(t)
		return program.vm.Stats().Energy
	}
	if many, none := energy(locals.String()), energy(""); many != none+44 {
		t.Errorf("a call of 300 slots spent %d, one of none %d: want 44 more", many, none)
	}
}

// A failed call names what it called as the source wrote it: a variable of the function running, of a
// function around (two out), of a loop's iteration that a closure reaches, and of a top-level block.
// The VM reads slots by index; the names come from the slots' names the program carries.
func TestAFailedCallNamesASlotVariable(t *testing.T) {
	for _, c := range []struct {
		source, want string
	}{
		{`const f = () => { const g = 5; return safe(() => g()).error.message; }; r = f();`, "g không phải hàm"},
		{`const a = (x) => { const b = () => { const c = () => x(); return c(); }; return b(); }; r = safe(() => a(7)).error.message;`, "x không phải hàm"},
		{`const fs = []; for (let i = 0; i < 2; i++) { fs.push(() => i()); } r = safe(fs[0]).error.message;`, "i không phải hàm"},
		{`{ const m = 4; r = safe(() => m()).error.message; }`, "m không phải hàm"},
		{`const f = () => { for (const item of [1]) { return safe(() => item()).error.message; } }; r = f();`, "item không phải hàm"},
	} {
		program := load(t, c.source)
		program.run(t)
		if got := program.variable("r").Text(); !strings.Contains(got, c.want) {
			t.Errorf("%s\n\tmessage %q, want it to name %q", c.source, got, c.want)
		}
	}
}

// Only a closure the VM's own program made runs: a lambda with no program (a template read out of the
// constants, one Go built) is refused as a lambda of another program is, since its frame's size is only
// known for the program's own.
func TestALambdaNoProgramMadeIsRefused(t *testing.T) {
	program := compiled(t, `const f = (x) => x + 1;`)
	var template *value.Lambda
	for _, constant := range program.Constants() {
		if lambda := constant.Lambda(); lambda != nil {
			template = lambda
		}
	}
	vm := runtime.New(program)
	vm.FastReset(program, installed(nil))
	result := vm.ExecuteLambda(template, []value.Value{value.Number(1)})
	if !result.IsInvalid() || !strings.Contains(result.Message(), "different program") {
		t.Errorf("a template run as a closure gave %s, want it refused", result.ToString().Text())
	}
}
