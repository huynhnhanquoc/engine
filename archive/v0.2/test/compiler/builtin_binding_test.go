package compiler_test

import (
	"math"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// Builtins are bound when the program compiles (04/11/2026): a name nothing declares that is a builtin's
// is read from its slot (BUILTIN n), never by name, and the VM fills the slots the host left empty from
// the frozen shared set. A number or a string a builtin holds is a constant, and a call of a member a
// builtin does not have is a compile error.

// listing lists the program's instructions as NAME operand-or-constant, in order.
func listing(t *testing.T, source string) []string {
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
		text := instruction.Name
		switch {
		case instruction.ConstantPreview != "":
			text += " " + instruction.ConstantPreview
		case instruction.Variable != "":
			text += " " + instruction.Variable
		case len(instruction.Operands) == 1 && instruction.Name == "BUILTIN":
			text += " " + value.Number(float64(instruction.Operands[0])).ToString().Text()
		}
		out = append(out, text)
	}
	return out
}

// runBare runs a program on a VM whose globals hold nothing: what it reads of a builtin comes from the
// slots alone.
func runBare(t *testing.T, source string, slots []value.Value) *runtime.VM {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 10_000_000
	vm.FastReset(bytecode.Program, map[string]value.Value{})
	vm.Builtins = slots
	if result := vm.Run(); result.IsInvalid() {
		t.Fatalf("%s: %s", source, result.Failure())
	}
	return vm
}

func slotOf(t *testing.T, name string) int {
	t.Helper()
	slot, ok := builtins.Slot(name)
	if !ok {
		t.Fatalf("%s has no slot", name)
	}
	return slot
}

func TestABuiltinIsReadFromItsSlot(t *testing.T) {
	code := strings.Join(listing(t, `r = Math.max(parseInt("4"), JSON.parse("2"));`), " | ")
	for _, name := range []string{"Math", "parseInt", "JSON"} {
		if want := "BUILTIN " + value.Number(float64(slotOf(t, name))).ToString().Text(); !strings.Contains(code, want) {
			t.Errorf("%s is not read from its slot (%s): %s", name, want, code)
		}
		if strings.Contains(code, `LOAD "`+name+`"`) {
			t.Errorf("%s is still read by name: %s", name, code)
		}
	}

	// no globals at all: every builtin comes from the shared set, inside functions and closures too
	vm := runBare(t, `
const f = (x) => [Math.max(x, 1), typeof JSON.stringify, Number.isInteger(x), [1, 2].map((y) => String(y + x)).join()];
r = JSON.stringify(f(3)) + " " + Object.keys({ a: 1 }).length + " " + isNaN(NaN);
`, nil)
	if got, want := vm.Vars["r"].Text(), `[3,"function",true,"4,5"] 1 true`; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
}

// A host lays its own builtin over the shared one through the VM's slots (a site's console, its Date),
// and a host's global of a builtin's name is no longer what the program reads.
func TestAHostLaysABuiltinOverItsSlotNotItsGlobal(t *testing.T) {
	var lines []string
	console := value.Object().Set("log", value.Function(func(arguments ...value.Value) value.Value {
		lines = append(lines, arguments[0].ToString().Text())
		return value.Undefined()
	}))
	slots := builtins.Slots()
	slots[slotOf(t, "console")] = console
	vm := runBare(t, `console.log("hello"); r = Math.max(1, 2);`, slots)
	if len(lines) != 1 || lines[0] != "hello" || vm.Vars["r"].Number() != 2 {
		t.Errorf("the site's console wrote %q and Math.max gave %s", lines, vm.Vars["r"].ToString().Text())
	}

	bytecode, err := compiler.CompileSource(`r = Math.max(1, 2);`)
	if err != nil {
		t.Fatal(err)
	}
	host := runtime.New(bytecode.Program)
	globals := map[string]value.Value{}
	builtins.Install(globals)
	globals["Math"] = value.Object().Set("max", value.Function(func(...value.Value) value.Value { return value.Number(99) }))
	host.FastReset(bytecode.Program, globals)
	if result := host.Run(); result.IsInvalid() {
		t.Fatal(result.Failure())
	}
	if got := host.Vars["r"].Number(); got != 2 {
		t.Errorf("a host's global Math was read: Math.max(1, 2) = %v", got)
	}
}

// A variable or a parameter of a builtin's name is that variable, and a program that assigns the name
// reads it by name: neither is a builtin, nor folded, nor checked.
func TestANameTheProgramDeclaresOrAssignsIsNotTheBuiltin(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`const f = (Math) => Math.sqr(2); r = f({ sqr: (x) => x * x });`, "4"},
		{`const Math = { PI: 3, sqr: (x) => x + 1 }; r = Math.PI + Math.sqr(1);`, "5"},
		{`const f = () => { const JSON = { parse: () => "mine" }; return JSON.parse("1"); }; r = f();`, "mine"},
		{`JSON = { parse: () => "assigned" }; r = JSON.parse("1");`, "assigned"},
		{`const g = () => { NaN = 7; return NaN; }; r = g();`, "7"},
		{`const g = () => { Infinity = 7; const h = () => Infinity; return h(); }; r = g();`, "7"},
		// a function that does not assign the name, nor any around it, reads the builtin
		{`const g = () => { NaN = 7; }; g(); const h = () => NaN; r = [typeof NaN, NaN === NaN, h() === h()].join();`, "number,false,false"},
		{`r = [1, 2].map((parseInt) => parseInt * 2).join();`, "2,4"},
	} {
		vm := runBare(t, c.source, nil)
		if got := vm.Vars["r"].ToString().Text(); got != c.want {
			t.Errorf("%s: r = %q, want %q", c.source, got, c.want)
		}
	}
}

// A number or a string a builtin holds is a constant: no slot read and no GET is left of it.
func TestABuiltinsNumberOrStringIsAConstant(t *testing.T) {
	for _, c := range []struct {
		source string
		want   value.Value
	}{
		{`r = Math.PI;`, value.Number(math.Pi)},
		{`r = Number.MAX_SAFE_INTEGER;`, value.Number(1<<53 - 1)},
		{`r = Infinity;`, value.Number(math.Inf(1))},
		{`r = Math.max.length;`, value.Number(2)},
		{`r = parseInt.name;`, value.String("parseInt")},
		{`r = Number.parseFloat.name;`, value.String("parseFloat")},
	} {
		// var r = … is var; r = …, the language has no var keyword yet: LOAD "var"; POP, then the constant
		code := strings.Join(listing(t, c.source), " | ")
		if strings.Contains(code, "GET") || strings.Contains(code, "BUILTIN") || !strings.Contains(code, "PUSH") {
			t.Errorf("%s compiles to %s, want the constant pushed", c.source, code)
		}
		if got := runBare(t, c.source, nil).Vars["r"]; !got.Equal(c.want) {
			t.Errorf("%s: r = %s, want %s", c.source, got.ToString().Text(), c.want.ToString().Text())
		}
	}
	if got := runBare(t, `r = NaN;`, nil).Vars["r"].Number(); !math.IsNaN(got) {
		t.Errorf("NaN reads %v", got)
	}
	// a member a builtin does not hold, an object or a function it holds, is read as it was
	for _, source := range []string{`r = Math.sqr;`, `r = Math.max;`, `r = Number.parseInt;`} {
		if code := strings.Join(listing(t, source), " | "); !strings.Contains(code, "GET") {
			t.Errorf("%s: %s, want a GET", source, code)
		}
	}
}

// A call of a member a builtin does not have is a compile error naming it, with the nearest member it
// does have; a read of one is undefined, and a call through ?. calls nothing.
func TestACallOfAMemberABuiltinDoesNotHaveIsACompileError(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`let r = Math.sqr(2);`, "1:14: Math không có sqr, nên Math.sqr(…) không gọi được: có phải Math.sqrt?"},
		{`let r = JSON.Parse("1");`, "1:14: JSON không có Parse, nên JSON.Parse(…) không gọi được: có phải JSON.parse?"},
		{`const f = () => Number.isInteger.call(null, 1);`, "1:34: Number.isInteger không có call, nên Number.isInteger.call(…) không gọi được"},
		{`let r = Object.seal({});`, "1:16: Object không có seal, nên Object.seal(…) không gọi được"},
		{`r = Math.PI.toFixd(2);`, "Math.PI không có toFixd, nên Math.PI.toFixd(…) không gọi được: có phải Math.PI.toFixed?"},
	} {
		_, err := compiler.CompileSource(c.source)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error containing %q", c.source, err, c.want)
		}
	}
	// Math.toString() was "[object Object]" here, JavaScript's "[object Math]": a library read as data is the TYPE
	// error since 19/11/2026 (library_data_test.go)
	vm := runBare(t, `r = [typeof Math.sqr, Math.sqr?.(2), Math.hasOwnProperty("PI"), Math.PI.toFixed(2)].join();`, nil)
	if got, want := vm.Vars["r"].Text(), "undefined,,true,3.14"; got != want {
		t.Errorf("r = %q, want %q", got, want)
	}
}

// A failure of a call names the builtin the program called as the source wrote it, not the slot it is
// read from: Math() is "Math is not a function", kitwork() still kitwork.
func TestAFailedCallNamesTheBuiltin(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`r = safe(() => Math()).error.message;`, "Math không phải hàm (là object)"},
		{`const f = () => console(); r = safe(f).error.message;`, "console không phải hàm (là object)"},
	} {
		vm := runBare(t, c.source, nil)
		if got := vm.Vars["r"].Text(); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want it to name %q", c.source, got, c.want)
		}
	}
}
