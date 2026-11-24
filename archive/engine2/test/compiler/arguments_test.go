package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// A call takes 255 arguments at most, as CALL and INVOKE write their count in one byte: a call of more is the
// compiler's error at the call, saying so (12/11/2026; it was compiled with its count cut to a byte, which the verifier
// refused as "compiler produced invalid bytecode"). 255 compile.
func TestACallTakesAtMost255Arguments(t *testing.T) {
	arguments := func(n int) string { return strings.TrimSuffix(strings.Repeat("1, ", n), ", ") }
	for _, source := range []string{
		"const f = () => 1; const r = f(" + arguments(255) + ");",
		"const a = []; a.push(" + arguments(255) + ");",
	} {
		if _, err := compiler.CompileSource(source); err != nil {
			t.Errorf("a call of 255 arguments: %v", err)
		}
	}
	for _, source := range []string{
		"const f = () => 1; const r = f(" + arguments(256) + ");",
		"const a = []; a.push(" + arguments(300) + ");",
	} {
		_, err := compiler.CompileSource(source)
		if err == nil || !strings.Contains(err.Error(), "một lời gọi nhận tối đa 255 đối số") || strings.Contains(err.Error(), "invalid bytecode") {
			t.Errorf("a call of more than 255 arguments: %v, want the compiler's error saying so", err)
		}
	}
}
