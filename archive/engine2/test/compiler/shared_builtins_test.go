package compiler_test

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The builtins are one frozen set every run reads at once (core/builtins, 04/11/2026). These tests hold
// the two things that sharing needs: a program cannot write to a builtin, and reading one writes
// nothing, not even a cache inside a value a program never sees.

// TestAWriteToABuiltinIsRefused: Math.x = 1 is a TYPE failure, as TypeScript refuses it, where
// JavaScript would let it through; the failure names the key, and the next run sees Math as it was.
func TestAWriteToABuiltinIsRefused(t *testing.T) {
	for _, c := range []struct{ source, message string }{
		{`Math.x = 1`, "cannot add property x, object is not extensible"},
		{`Math.PI = 3`, "cannot assign to read only property 'PI'"},
		{`JSON.parse = null`, "cannot assign to read only property 'parse'"},
		{`console.log = 1`, "cannot assign to read only property 'log'"},
		{`Object.assign(Math, { y: 2 })`, "cannot add property y, object is not extensible"},
	} {
		got := runSource(t, c.source)
		if !got.IsInvalid() || !strings.Contains(got.Message(), c.message) {
			t.Errorf("%s: %s, want a failure saying %q", c.source, got.Failure(), c.message)
		}
	}
	// Object.isFrozen(Math) was true, JavaScript's false: a value the engine froze has no answer there since
	// 19/11/2026 (library_data_test.go); a write is what shows it frozen
	got := runSource(t, `const result = [Math.x, Math.y, Math.PI, typeof JSON.parse, typeof console.log]`)
	if got.IsInvalid() {
		t.Fatalf("a read after the refused writes: %s", got.Failure())
	}
	items := got.Items()
	if !items[0].IsUndefined() || !items[1].IsUndefined() || items[2].Number() != math.Pi ||
		items[3].Text() != "function" || items[4].Text() != "function" {
		t.Errorf("after the refused writes the builtins read %s", value.Stringify(got, "").Text())
	}
}

// runSource compiles and runs one program on a fresh VM with the shared builtins; the result is the
// run's failure, or its top-level result when it declares one.
func runSource(t *testing.T, source string) value.Value {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 10_000_000
	builtins.Install(vm.Globals)
	if result := vm.Run(); result.IsInvalid() {
		return result
	}
	return vm.Vars["result"]
}

// TestRunsLeaveTheBuiltinsAsTheyWereBuilt runs the whole corpus, eight runs at a time, on the shared
// builtins, and compares every byte reachable from them (the unexported fields of value too, through
// reflect) before and after. A read that wrote anything (a lazily made map, a cached bound method, a
// key order) would differ; under -race the eight workers also show a write as a data race.
func TestRunsLeaveTheBuiltinsAsTheyWereBuilt(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+7", 7*60*60)
	t.Cleanup(func() { time.Local = local })

	globals := map[string]value.Value{}
	builtins.Install(globals)
	before := fingerprint(globals)

	var corpus struct {
		Scope json.RawMessage `json:"scope"`
		Cases []corpusCase    `json:"cases"`
	}
	readCorpusJSON(t, filepath.Join("testdata", "parity_corpus.json"), &corpus)
	scope := decodeScope(t, "scope", corpus.Scope)
	owns := make([]ordered, len(corpus.Cases))
	for i, c := range corpus.Cases {
		if len(c.Scope) > 0 {
			owns[i] = decodeScope(t, c.Src, c.Scope)
		}
	}

	const workers = 8
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for i := worker; i < len(corpus.Cases); i += workers {
				program, fault := compileCase(corpus.Cases[i].Src)
				if fault != "" || program.err != nil {
					continue
				}
				runCase(program.bytecode, scope, owns[i])
			}
		}(worker)
	}
	group.Wait()

	if after := fingerprint(globals); after != before {
		at := 0
		for at < len(before) && at < len(after) && before[at] == after[at] {
			at++
		}
		from := max(0, at-120)
		t.Fatalf("running the corpus changed the shared builtins near byte %d:\nbefore …%s\nafter  …%s",
			at, before[from:min(len(before), at+120)], after[from:min(len(after), at+120)])
	}
}

// fingerprint writes out everything reachable from the globals: every field of every struct (exported
// or not), every item, every key of every map in sorted order, a function by its code pointer, a
// pointer seen before by the order it was first met.
func fingerprint(globals map[string]value.Value) string {
	var out strings.Builder
	seen := map[uintptr]int{}
	names := make([]string, 0, len(globals))
	for name := range globals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.WriteString(name)
		out.WriteString("=")
		walk(reflect.ValueOf(globals[name]), seen, &out)
		out.WriteString("\n")
	}
	return out.String()
}

func walk(v reflect.Value, seen map[uintptr]int, out *strings.Builder) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			out.WriteString("nil")
			return
		}
		if n, ok := seen[v.Pointer()]; ok {
			fmt.Fprintf(out, "&%d", n)
			return
		}
		seen[v.Pointer()] = len(seen)
		out.WriteString("*")
		walk(v.Elem(), seen, out)
	case reflect.Interface:
		if v.IsNil() {
			out.WriteString("nil")
			return
		}
		fmt.Fprintf(out, "(%s)", v.Elem().Type())
		walk(v.Elem(), seen, out)
	case reflect.Struct:
		out.WriteString("{")
		for i := 0; i < v.NumField(); i++ {
			walk(v.Field(i), seen, out)
			out.WriteString(",")
		}
		out.WriteString("}")
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			out.WriteString("nil")
			return
		}
		fmt.Fprintf(out, "[%d:", v.Len())
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), seen, out)
			out.WriteString(",")
		}
		out.WriteString("]")
	case reflect.Map:
		if v.IsNil() {
			out.WriteString("nil")
			return
		}
		keys := v.MapKeys()
		text := make([]string, len(keys))
		for i, k := range keys {
			var key strings.Builder
			walk(k, map[uintptr]int{}, &key)
			text[i] = key.String()
		}
		order := make([]int, len(keys))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return text[order[a]] < text[order[b]] })
		fmt.Fprintf(out, "map%d{", len(keys))
		for _, i := range order {
			out.WriteString(text[i])
			out.WriteString(":")
			walk(v.MapIndex(keys[i]), seen, out)
			out.WriteString(",")
		}
		out.WriteString("}")
	case reflect.Func:
		if v.IsNil() {
			out.WriteString("nil")
			return
		}
		fmt.Fprintf(out, "func%x", v.Pointer())
	case reflect.String:
		out.WriteString(strconv.Quote(v.String()))
	case reflect.Bool:
		out.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		out.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		out.WriteString(strconv.FormatUint(math.Float64bits(v.Float()), 16))
	default:
		fmt.Fprintf(out, "?%s", v.Kind())
	}
}
