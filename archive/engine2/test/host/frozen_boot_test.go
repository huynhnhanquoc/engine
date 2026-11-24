package host_test

import (
	"path/filepath"
	"testing"
)

// What a site's file made at boot is frozen by the engine, as every request reads it at once (docs/language.md mục
// 1.7): Object.isFrozen of it is the TYPE error, JavaScript leaving it open, where it was true (19/11/2026). What the
// file froze itself with Object.freeze is the program's: true, as in JavaScript.
//
// CONTROL (19/11/2026): with the boot's freeze walk freezing as Object.freeze does (Freeze, not FreezeShared), the
// boot object answered "none", isFrozen true.
func TestObjectIsFrozenOfWhatTheBootFrozeHasNoAnswer(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `const config = { a: 1 };
const own = Object.freeze({ b: 2 });
router.get((context) => context.json({ boot: safe(() => Object.isFrozen(config)).error?.code ?? "none", own: Object.isFrozen(own) }));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.body != `{"boot":"TYPE","own":true}` {
		t.Errorf("GET /: %d %s, want the boot object's isFrozen the TYPE error and the file's own freeze true", got.status, got.body)
	}
}
