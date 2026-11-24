package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// evaluate compiles source as a .ui expression and runs it with scope as the names it reads: its value as
// text (undefined and null as "<nothing>"), or its failure's text.
func evaluate(t *testing.T, source string, scope map[string]value.Value) string {
	t.Helper()
	bytecode, _, err := compiler.CompileExpression(source)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	globals := map[string]value.Value{}
	builtins.Install(globals)
	for name, each := range scope {
		globals[name] = each
	}
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 1_000_000
	vm.FastReset(bytecode.Program, globals)
	if result := vm.Run(); result.IsInvalid() {
		return "failure: " + result.Message()
	}
	got := vm.Vars[compiler.ExpressionValue]
	if got.IsNullish() {
		return "<nothing>"
	}
	return got.ToString().Text()
}

// Every link of a chain in a .ui expression reads as ?. does (KITWORK_LANG mục 3): a name, a member, an item
// or a method's object that is null or undefined makes the chain undefined, never a failure; the rest of the
// expression is .work's.
func TestEveryLinkOfAChainReadsAsOptional(t *testing.T) {
	surface := value.Object()
	surface.Set("title", value.String("Web"))
	surface.Set("owner", value.Null())
	scope := map[string]value.Value{
		"surface": surface,
		"items":   value.Array(nil),
		"count":   value.Number(3),
	}
	for _, want := range []struct{ source, value string }{
		{"surface.title + ' ' + count", "Web 3"},
		{"missing.title", "<nothing>"},
		{"surface.owner.name.first", "<nothing>"},
		{"items[0].name", "<nothing>"},
		{"surface.owner.name.toUpperCase()", "<nothing>"},
		{"surface.title.toUpperCase()", "WEB"},
		{"items.length === 0 ? 'none' : items[0]", "none"},
		{"`${surface.owner.name} of ${surface.title}`", "undefined of Web"},
		{"[surface.owner.x, count].length", "2"},
		{"({ a: surface.owner.x }).a", "<nothing>"},
		{"[1, 2, 3].map((n) => n * count).join(',')", "3,6,9"},
		{"[{}].map((row) => row.x.y)[0]", "<nothing>"},
		{"Math.max(count, 7)", "7"},
	} {
		if got := evaluate(t, want.source, scope); got != want.value {
			t.Errorf("{{ %s }}: %s, want %s", want.source, got, want.value)
		}
	}
	// a call of a name is a call: a function the page is not given is a failure, not nothing
	if got := evaluate(t, "money(count)", scope); !strings.HasPrefix(got, "failure:") {
		t.Errorf("{{ money(count) }} with no money: %s, want a failure", got)
	}
}

// A .ui expression reads and never writes, and is one expression: what closes its parenthesis and goes on to
// a second statement is an error.
func TestAnExpressionReadsAndIsOneExpression(t *testing.T) {
	for _, source := range []string{
		"count = 1", "count++", "--count", "count += 2", "items.map((row) => { row.x = 1; return row })",
		"1); evil(); (2", "1), other = (2",
	} {
		if _, _, err := compiler.CompileExpression(source); err == nil {
			t.Errorf("{{ %s }} compiles, want an error", source)
		}
	}
}

// What an expression reads is the names it does not declare and that are no builtin, each once, in the order
// first read: an arrow's parameter is declared inside it, Math is a builtin, and a member's name is no name.
func TestAnExpressionSaysWhatItReads(t *testing.T) {
	for _, want := range []struct {
		source string
		reads  string
	}{
		{"surface.title + count + Math.max(a, b) + items.filter((x) => x.ok && x.id !== a).length", "surface count a b items"},
		{"`${user.name}` + user.email", "user"},
		{"money(order.total, currency)", "money order currency"},
		{"'plain text'", ""},
	} {
		_, reads, err := compiler.CompileExpression(want.source)
		if err != nil {
			t.Errorf("{{ %s }}: %v", want.source, err)
			continue
		}
		if got := strings.Join(reads, " "); got != want.reads {
			t.Errorf("{{ %s }} reads %q, want %q", want.source, got, want.reads)
		}
	}
}
