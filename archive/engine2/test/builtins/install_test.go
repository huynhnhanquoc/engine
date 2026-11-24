package builtins_test

import (
	"math"
	"slices"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

// TestInstallSharesOneFrozenSet: every Install gives the same values, built once and frozen, so two
// runs read one Math at once and neither can write to it: Math.x = 1 is a TYPE failure, as is a key
// set on a builtin function, and the other run sees nothing of it. Within the set Number.parseInt is
// the global parseInt.
func TestInstallSharesOneFrozenSet(t *testing.T) {
	first, second := map[string]value.Value{}, map[string]value.Value{}
	builtins.Install(first)
	builtins.Install(second)
	if !first["Number"].Get("parseInt").Equal(first["parseInt"]) || !first["Number"].Get("parseFloat").Equal(first["parseFloat"]) {
		t.Error("Number.parseInt and Number.parseFloat are the globals parseInt and parseFloat")
	}
	for _, name := range builtins.Names() {
		a, b := first[name], second[name]
		if (a.IsObject() || a.IsFunction()) && !a.Equal(b) {
			t.Errorf("%s is two values across two Installs", name)
		}
	}
	if got := first["Math"].Set("x", value.Number(1)); got.Code() != value.TypeCode {
		t.Errorf("Math.x = 1 is a TYPE failure, got %s", got.Failure())
	}
	if got := first["Math"].Get("max").Set("x", value.Number(1)); got.Code() != value.TypeCode {
		t.Errorf("a key set on Math.max is a TYPE failure, got %s", got.Failure())
	}
	if first["Math"].Delete("PI") || first["Math"].Get("PI").Number() != math.Pi {
		t.Error("Math.PI is deleted from a frozen Math")
	}
	if !second["Math"].Get("x").IsUndefined() || !second["Math"].Get("max").Get("x").IsUndefined() {
		t.Error("a refused write is seen by another run")
	}
}

// TestEveryBuiltinIsFrozenAllTheWayDown: what a program can reach from a builtin, an object or a
// function, is frozen (Math, Math.max, Math.max's own keys), and no builtin holds an array, which has
// no frozen mark yet.
func TestEveryBuiltinIsFrozenAllTheWayDown(t *testing.T) {
	globals := map[string]value.Value{}
	builtins.Install(globals)
	var visit func(path string, v value.Value, depth int)
	visit = func(path string, v value.Value, depth int) {
		switch {
		case v.IsArray():
			t.Errorf("%s is an array, which cannot be frozen", path)
		case v.IsObject() || v.IsFunction():
			if !v.IsFrozen() {
				t.Errorf("%s is not frozen", path)
			}
			if depth < 4 {
				for _, key := range v.Keys() {
					visit(path+"."+key, v.Get(key), depth+1)
				}
			}
		}
	}
	for _, name := range builtins.Names() {
		visit(name, globals[name], 0)
	}
}

// TestInstallMakesNothing: Install copies the shared values into the run's globals; it makes no
// value of its own (it made 481 allocations, 45 KB, before the set was shared).
func TestInstallMakesNothing(t *testing.T) {
	globals := make(map[string]value.Value, 64)
	if allocations := testing.AllocsPerRun(100, func() { builtins.Install(globals) }); allocations != 0 {
		t.Errorf("Install makes %v allocations, want 0", allocations)
	}
}

// TestNamesListsWhatInstallPuts: Names is every key Install sets, no more, each once.
func TestNamesListsWhatInstallPuts(t *testing.T) {
	globals := map[string]value.Value{}
	builtins.Install(globals)
	names := builtins.Names()
	installed := make([]string, 0, len(globals))
	for name := range globals {
		installed = append(installed, name)
	}
	slices.Sort(installed)
	listed := slices.Clone(names)
	slices.Sort(listed)
	if !slices.Equal(installed, listed) {
		t.Errorf("Install sets %v, Names lists %v", installed, listed)
	}
	if len(slices.Compact(listed)) != len(names) {
		t.Errorf("Names lists a builtin twice: %v", names)
	}
}

// TestBuiltinKeyOrderIsStable: Math, JSON, console and the statics of every constructor keep one order
// of keys on every Install (it was a Go map's once, and changed from run to run).
func TestBuiltinKeyOrderIsStable(t *testing.T) {
	order := func() map[string][]string {
		globals := map[string]value.Value{}
		builtins.Install(globals)
		keys := map[string][]string{}
		for name, v := range globals {
			keys[name] = slices.Clone(v.Keys())
		}
		return keys
	}
	want := order()
	for run := 0; run < 20; run++ {
		got := order()
		for name, keys := range want {
			if !slices.Equal(got[name], keys) {
				t.Fatalf("run %d: %s's keys %v, before %v", run, name, got[name], keys)
			}
		}
	}
	if got := want["Math"]; got[0] != "abs" || got[len(got)-1] != "SQRT2" {
		t.Errorf("Math's keys follow node's order, abs first and SQRT2 last: %v", got)
	}
}

// TestReplacerFunctionOrArrayIsUnsupported: a replacer JSON.stringify would have to read is a failure
// that says so, rather than an output that quietly differs from JavaScript's.
// TestArrayLengthCeiling: a builtin never allocates an array past 2^24 items; past JavaScript's own
// 2^32 - 1 the failure is JavaScript's RangeError, below it TOO_LARGE.
func TestArrayLengthCeiling(t *testing.T) {
	for _, c := range []struct {
		name string
		got  value.Value
		code string
	}{
		{"Array(2 ** 24 + 1)", call("Array", "", num(1<<24+1)), value.TooLargeCode},
		{"Array.from({ length: 2 ** 24 + 1 })", call("Array", "from", value.Object().Set("length", num(1<<24+1))), value.TooLargeCode},
		{"Array(2 ** 32)", call("Array", "", num(1<<32)), value.RangeCode},
		{"Array.from({ length: 2 ** 53 })", call("Array", "from", value.Object().Set("length", num(1<<53))), value.RangeCode},
	} {
		if c.got.Code() != c.code {
			t.Errorf("%s is %s, got %s", c.name, c.code, c.got.ToString().Text())
		}
	}
	if got := call("Array", "", num(1<<10)).Get("length").Number(); got != 1<<10 {
		t.Errorf("Array(1024) has %v items", got)
	}
}

// TestURIFailuresAreInvalidInput: JavaScript's URIError has no code of its own; a text that is no URI
// is input that cannot be read.
func TestURIFailuresAreInvalidInput(t *testing.T) {
	for _, got := range []value.Value{
		call("decodeURIComponent", "", str("%")),
		call("decodeURI", "", str("%E2%82")),
		call("encodeURIComponent", "", call("String", "fromCharCode", num(0xDC00))),
	} {
		if got.Code() != value.InvalidInputCode {
			t.Errorf("a malformed URI is INVALID_INPUT: %s", got.ToString().Text())
		}
	}
}

// BenchmarkInstall is what a run pays for its own builtins.
func BenchmarkInstall(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		builtins.Install(map[string]value.Value{})
	}
}
