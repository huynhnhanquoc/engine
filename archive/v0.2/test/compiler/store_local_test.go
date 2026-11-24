package compiler_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// accesses lists a program's variable instructions as "OPCODE name", in the order they are written:
// the stores, or (loads) the loads too. A slot instruction is named by the variable it reaches.
func accesses(t *testing.T, source string, loads bool) []string {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	inspection, err := runtime.InspectProgram(bytecode.Program)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, instruction := range inspection.Instructions {
		if !strings.HasPrefix(instruction.Name, "STORE") && !(loads && strings.HasPrefix(instruction.Name, "LOAD")) {
			continue
		}
		name := instruction.Variable
		if name == "" {
			name = strings.Trim(instruction.ConstantPreview, `"`)
		}
		out = append(out, instruction.Name+" "+name)
	}
	return out
}

func stores(t *testing.T, source string) []string {
	return accesses(t, source, false)
}

// A function's own variables, and the top level's block variables, live in slots (bytecode 7): stored
// with STORE_SLOT and read with LOAD_SLOT, by index; a variable of a function around is STORE_OUTER and
// LOAD_OUTER. The top level's own variables and a name nothing declares keep STORE and LOAD by name. A
// parameter assigned while a function around holds its name is written in its own slot, with no copy
// (bytecode 6 copied it into x@1 as the function started).
func TestAFunctionStoresItsOwnVariablesInSlots(t *testing.T) {
	for _, c := range []struct {
		source string
		want   []string
	}{
		{`const f = () => { let sum = 0; for (let i = 0; i < 3; i++) { sum = sum + i; } return sum; };`,
			[]string{"STORE_SLOT sum", "STORE_SLOT i#1", "STORE_SLOT sum", "STORE_SLOT i#1", "STORE f"}},
		{`const make = () => { let n = 0; return () => { n = n + 1; return n; }; };`,
			[]string{"STORE_SLOT n", "STORE_OUTER n", "STORE make"}},
		{`let top = 0; top = 1;`, []string{"STORE top", "STORE top"}},
		{`const x = 1; const f = (x) => { x = 2; return x; };`,
			[]string{"STORE x", "STORE_SLOT x", "STORE f"}},
		{`const f = (o) => { const { a, b } = o; return a + b; };`,
			[]string{"STORE_SLOT a", "STORE_SLOT b", "STORE f"}},
		{`t = 0; for (let i = 0; i < 2; i++) { t = i; }`,
			[]string{"STORE t", "STORE_SLOT i#1", "STORE t", "STORE_SLOT i#1"}},
		{`const f = () => { y = 1; return y; };`, []string{"STORE y", "STORE f"}},
	} {
		if got := stores(t, c.source); strings.Join(got, ", ") != strings.Join(c.want, ", ") {
			t.Errorf("%s\n\tstores: %s\n\twant:   %s", c.source, strings.Join(got, ", "), strings.Join(c.want, ", "))
		}
	}
	// a variable of an iteration a closure reaches lives in the iteration's object, in a slot of its own
	// (#N), which the closure reaches through the function it is made in
	got := strings.Join(accesses(t, `const f = () => { const fs = []; for (let i = 0; i < 2; i++) { fs.push(() => i); } return fs; };`, true), ", ")
	for _, want := range []string{"STORE_SLOT #2", "LOAD_OUTER #2"} {
		if !strings.Contains(got, want) {
			t.Errorf("a closure of a loop's iteration: %s, want %s among them", got, want)
		}
	}
}

// A function of three levels reaches each level's variables by depth: the nearest is 1, the next 2.
func TestAClosureReachesEachFunctionAroundByDepth(t *testing.T) {
	source := `const a = (x) => { const b = (y) => { const c = (z) => x + y + z; return c; }; return b; };`
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := runtime.InspectProgram(bytecode.Program)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, instruction := range inspection.Instructions {
		if instruction.Name == "LOAD_OUTER" || instruction.Name == "LOAD_SLOT" {
			depth := ""
			if instruction.Name == "LOAD_OUTER" {
				depth = string(rune('0' + instruction.Operands[0]))
			}
			got = append(got, instruction.Name+depth+" "+instruction.Variable)
		}
	}
	want := "LOAD_OUTER2 x, LOAD_OUTER1 y, LOAD_SLOT z, LOAD_SLOT c, LOAD_SLOT b"
	if strings.Join(got, ", ") != want {
		t.Errorf("loads: %s\nwant:  %s", strings.Join(got, ", "), want)
	}
	// c holds b's frame (y), b holds a's (x): each closure on the way keeps its parent's slots; a keeps
	// nothing of the top level's, which has no slot it reaches
	keeps := map[string]bool{}
	for _, constant := range bytecode.Program.Constants() {
		if lambda := constant.Lambda(); lambda != nil {
			keeps[lambda.Name] = lambda.CapturesSlots
		}
	}
	if !keeps["c"] || !keeps["b"] || keeps["a"] {
		t.Errorf("CapturesSlots: %v, want c and b, not a", keeps)
	}
}

// The verifier holds a slot instruction to the frame of the function whose code it is: an index past
// that frame's slots is refused, as is a wide form whose index fits two bytes and an outer depth of 0.
func TestSlotInstructionsAreVerifiedAgainstTheirFrame(t *testing.T) {
	constants := []value.Value{value.Number(1)}
	good := []byte{byte(runtime.PUSH), 0, 0, byte(runtime.STORE_SLOT), 0, 1, byte(runtime.RETURN)}
	if err := runtime.VerifyWithLocals(good, constants, 2); err != nil {
		t.Errorf("STORE_SLOT 1 of a frame of 2: %v", err)
	}
	if err := runtime.VerifyWithLocals(good, constants, 1); err == nil {
		t.Errorf("STORE_SLOT 1 of a frame of 1 verified, want INVALID_OPERAND")
	}
	if err := runtime.Verify(good, constants); err == nil {
		t.Errorf("STORE_SLOT 1 of a top level with no slots verified, want INVALID_OPERAND")
	}
	wide := []byte{byte(runtime.PUSH), 0, 0, byte(runtime.STORE_SLOT_WIDE), 0, 0, 1, byte(runtime.RETURN)}
	if err := runtime.VerifyWithLocals(wide, constants, 2); err == nil {
		t.Errorf("STORE_SLOT_WIDE of index 1 verified, want INVALID_OPERAND")
	}
	outer := []byte{byte(runtime.LOAD_OUTER), 0, 0, 0, byte(runtime.RETURN)}
	if err := runtime.Verify(outer, constants); err == nil {
		t.Errorf("LOAD_OUTER of depth 0 verified, want INVALID_OPERAND")
	}
	outer[1] = 1
	if err := runtime.Verify(outer, constants); err != nil {
		t.Errorf("LOAD_OUTER of depth 1: %v", err)
	}

	// a lambda's frame is as many slots as its template names
	lambda := &value.Lambda{Address: 4, Params: []string{"a"}, Locals: []string{"a"}}
	code := []byte{
		byte(runtime.PUSH), 0, 1, byte(runtime.RETURN),
		byte(runtime.LOAD_SLOT), 0, 0, byte(runtime.RETURN),
	}
	if err := runtime.Verify(code, []value.Value{value.Number(1), value.Closure(lambda)}); err != nil {
		t.Errorf("LOAD_SLOT 0 of a lambda of one slot: %v", err)
	}
	code[6] = 1
	if err := runtime.Verify(code, []value.Value{value.Number(1), value.Closure(lambda)}); err == nil {
		t.Errorf("LOAD_SLOT 1 of a lambda of one slot verified, want INVALID_OPERAND")
	}
}

// A program refuses a lambda template with fewer slots than parameters: they are bound into its first
// slots.
func TestALambdaHasASlotForEveryParameter(t *testing.T) {
	lambda := &value.Lambda{Address: 4, Params: []string{"a", "b"}, Locals: []string{"a"}}
	code := []byte{byte(runtime.PUSH), 0, 0, byte(runtime.RETURN), byte(runtime.RETURN)}
	if _, err := runtime.NewProgram(code, []value.Value{value.Closure(lambda)}, nil); err == nil ||
		!strings.Contains(err.Error(), "1 slots for 2 parameters") {
		t.Errorf("a lambda of two parameters and one slot: %v, want it refused", err)
	}
}

// A frame of more slots than two bytes index is written with the wide forms: STORE_SLOT_WIDE and
// LOAD_SLOT_WIDE in the frame running, LOAD_OUTER_WIDE and STORE_OUTER_WIDE from a closure, and the
// program runs as it reads. 65,540 block variables of the top level, the last one read and written.
func TestAFrameOfManySlotsUsesTheWideForms(t *testing.T) {
	const count = 65_540
	var source strings.Builder
	source.WriteString("{ ")
	for i := 0; i < count; i++ {
		source.WriteString("let v" + strconv.Itoa(i) + " = " + strconv.Itoa(i%7) + "; ")
	}
	last := "v" + strconv.Itoa(count-1)
	source.WriteString("first = " + last + " + 1; const f = () => { " + last + " = " + last + " + 10; return " + last + "; }; outer = f(); after = " + last + "; }")
	bytecode, err := compiler.CompileSource(source.String())
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := runtime.InspectProgram(bytecode.Program)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, instruction := range inspection.Instructions {
		if strings.HasSuffix(instruction.Name, "_WIDE") && instruction.Variable == last+"#"+strconv.Itoa(count) {
			seen[instruction.Name] = true
		}
	}
	for _, name := range []string{"STORE_SLOT_WIDE", "LOAD_SLOT_WIDE", "LOAD_OUTER_WIDE", "STORE_OUTER_WIDE"} {
		if !seen[name] {
			t.Errorf("no %s of %s among %v", name, last, seen)
		}
	}
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 100_000_000
	builtins.Install(vm.Globals)
	if result := vm.Run(); result.IsInvalid() {
		t.Fatal(result.Message())
	}
	// v65539 is 65,539 % 7 = 5
	for name, want := range map[string]float64{"first": 6, "outer": 15, "after": 15} {
		if got := vm.Vars[name].Number(); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// The compiler tells the runtime what a closure keeps of the frame it is made in: its slots when it or a
// function inside it reaches a variable of the function around (CapturesSlots), the frame's map by name
// when it or a function inside it reads or writes a variable by name (CapturesNames), and whether the
// function writes a name nothing declares, so its frame makes a map for such names (WritesNames). A
// builtin is read from its slot, not by name (04/11/2026), so a closure that reads only builtins keeps
// nothing.
func TestTheCompilerTellsWhatAClosureKeeps(t *testing.T) {
	bytecode, err := compiler.CompileSource(`
const plain = (x) => x + 1;
const global = () => hostValue + 1;
const builtin = () => Math.max(1, JSON.parse("2"), Infinity);
const writes = () => { undeclared = 1; return undeclared; };
const outer = () => { const inner = () => hostValue; return inner; };
const local = () => { let n = 0; const count = () => { n = n + 1; return n; }; return count; };
`)
	if err != nil {
		t.Fatal(err)
	}
	type keeps struct{ slots, names, writes bool }
	got := map[string]keeps{}
	for _, constant := range bytecode.Program.Constants() {
		if lambda := constant.Lambda(); lambda != nil {
			got[lambda.Name] = keeps{lambda.CapturesSlots, lambda.CapturesNames, lambda.WritesNames}
		}
	}
	for name, want := range map[string]keeps{
		"plain":   {},
		"global":  {names: true},
		"builtin": {},
		"writes":  {names: true, writes: true},
		"outer":   {names: true},
		"inner":   {names: true},
		"local":   {},
		"count":   {slots: true},
	} {
		if got[name] != want {
			t.Errorf("%s keeps %+v, want %+v", name, got[name], want)
		}
	}
}
