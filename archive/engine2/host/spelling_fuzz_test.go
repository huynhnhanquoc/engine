package host

import "testing"

// FuzzRouterWords: the spelling of a router.work or design.work is read from the tree of a source any app writes, at
// boot, outside the compiler (spelling.go): it says a problem or nothing, and never panics (13/11/2026).
func FuzzRouterWords(f *testing.F) {
	for _, seed := range []string{
		`import * as router from "kitwork/router"; router.get((context) => context.text("ok"));`,
		`import * as router from "kitwork/router"; router.limit("100/m"); router.guard((context) => context.next());`,
		`import * as design from "kitwork/design"; design.colors({ brand: "#635bff" });`,
		`import * as router from "kitwork/router"; const r = router; r.get(() => 1); { const router = 1; router.x(); }`,
		`import { get } from "kitwork/router"; get(() => 1);`,
		`import*as router from"kitwork/router";router.get(()=>1)`,
	} {
		f.Add("router.work", seed)
		f.Add("design.work", seed)
	}
	f.Fuzz(func(t *testing.T, name, source string) {
		if name != "router.work" && name != "design.work" && name != "config.work" {
			name = "router.work"
		}
		routerWords(name, source)
	})
}
