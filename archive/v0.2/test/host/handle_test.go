package host_test

import (
	"strings"
	"testing"
)

// kitwork/handle is no library (the owner, 19/11/2026, docs/language.md mục 5): error(…) and safe(…) are builtins, so
// a file that imports it is a boot error saying how to write them, where it said the library was not served yet, as if
// it would be.
//
// CONTROL (19/11/2026): with Handle the unserved library again, the error said "kitwork/handle is not served by engine2
// yet".
func TestKitworkHandleIsNoLibrary(t *testing.T) {
	got := bootError(t, map[string]string{
		"apps/shop/core/errors.work":      "import * as handle from \"kitwork/handle\";\nexport const checked = 1;\n",
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"ok\"));\n",
	})
	if want := "kitwork/handle is no library: error(…) and safe(…) are builtins, written without an import"; !strings.Contains(got, want) {
		t.Errorf("boot error %q, want one saying %q", got, want)
	}
}
