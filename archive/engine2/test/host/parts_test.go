package host_test

import (
	"net/http"
	"path/filepath"
	"testing"
)

// A page drawn from what earlier pages kept (library/ui frozen.go, 11/11/2026) marks the option of a bound select that
// is the field's value at each request: the option's opening reads the form through the select around it, which is
// the request's whatever values its expressions read, and is never written again from another request's.
func TestABoundSelectMarksEachRequestsValue(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/core/schema/shop.work": `import { struct, choice } from "kitwork/schema";
export const order = struct({ size: choice("s", "m", "l") });
`,
		"apps/shop/shop.test/router.work": routerImport + `import { order } from "app/core/schema/shop.work";
router.get((context) => context.view({ form: order.form({ size: context.query("size") ?? "m" }) }));
`,
		"apps/shop/shop.test/content.ui": `<content><form method="post">
<select value="{{ form.size }}"><option value="s">Nhỏ</option><option value="m">Vừa</option><option value="l">Lớn</option></select>
</form></content>`,
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	want := map[string]string{
		"s": `<option value="s" selected>Nhỏ</option><option value="m">Vừa</option><option value="l">Lớn</option>`,
		"m": `<option value="s">Nhỏ</option><option value="m" selected>Vừa</option><option value="l">Lớn</option>`,
		"l": `<option value="s">Nhỏ</option><option value="m">Vừa</option><option value="l" selected>Lớn</option>`,
	}
	for _, size := range []string{"s", "l", "s", "m", "l"} {
		got := b.get(t, "shop.test", "/?size="+size)
		if got.status != http.StatusOK {
			t.Fatalf("GET /?size=%s: %d %s\n%s", size, got.status, got.body, b.log.String())
		}
		holds(t, "GET /?size="+size, got.body, want[size])
	}
}
