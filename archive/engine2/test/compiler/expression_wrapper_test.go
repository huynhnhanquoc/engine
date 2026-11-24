package compiler_test

import (
	"slices"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// An expression of a .ui file reads value as any name of its page (15/11/2026): the text it is compiled from is
// const value = (…), and value read that variable of its own, so reads left it out and a kept part of a page showed
// the value of the request before (test/ui, TestAPartReadsAPageNamedValue). The variable is ExpressionValue now, a
// name no source writes. CONTROL: oneValue not renaming it, red.
func TestAnExpressionReadsAPageNamedValue(t *testing.T) {
	for _, source := range []string{"value", "value + 1", "items.map((x) => x + value)"} {
		_, reads, err := compiler.CompileExpression(source)
		if err != nil || !slices.Contains(reads, "value") {
			t.Errorf("%s: reads %v, %v; want value among them", source, reads, err)
		}
	}
	if _, reads, err := compiler.CompileExpression("items.map((value) => value * 2)"); err != nil || slices.Contains(reads, "value") {
		t.Errorf("an arrow's own value: reads %v, %v", reads, err)
	}
	if names, err := compiler.CompileHandler("value = value + 1"); err != nil || !slices.Equal(names, []string{"value"}) {
		t.Errorf("an event writing value: %v, %v", names, err)
	}
	if compiler.ExpressionValue == "value" {
		t.Error("the expression's own variable is a name a page gives")
	}
}
