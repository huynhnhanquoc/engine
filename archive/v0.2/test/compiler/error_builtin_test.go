package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// fail(…) is error(…) since 07/11/2026 (docs/language.md mục 5): a call of fail nothing declares is a compile error
// naming the new word, never "fail is not a function" at run time; a program's own fail is its own.
func TestACallOfFailNamesError(t *testing.T) {
	for _, source := range []string{`fail("x");`, `const f = () => fail("x");`, `const r = safe(() => fail("x"));`} {
		_, err := compiler.CompileSource(source)
		if err == nil || !strings.Contains(err.Error(), "fail(…) đã đổi tên thành error(…)") || !strings.Contains(err.Error(), "1:") {
			t.Errorf("%s: %v", source, err)
		}
	}
	for _, source := range []string{`const fail = (x) => x; fail(1);`, `const f = (fail) => fail(1);`, `const r = error("x");`} {
		if _, err := compiler.CompileSource(source); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
