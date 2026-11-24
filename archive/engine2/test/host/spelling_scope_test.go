package host_test

import (
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/host"
)

// A router an inner scope declares hides the import inside that scope only, as the compiler reads it: a
// const router in an inner block or an inner function does not make the function around it its own, so
// helper(router) and a door in that function's body are boot errors as without it (05/11/2026: a router
// declared anywhere in a function passed over all of it, and each of these booted and served).
func TestARouterAnInnerScopeDeclaresHidesTheImportThereOnly(t *testing.T) {
	library := "export const declare = (r) => { r.get((context) => context.text(\"declared by the library\")); };\n"
	use := routerImport + "import { declare } from \"app/core/doors.work\";\n"
	for name, c := range map[string]struct{ source, want string }{
		"an inner block": {use + "const open = () => { declare(router); { const router = 0; } };\nopen();\n",
			"router.work:3:30: router is read only as router.<word>(…)"},
		"an inner function": {use + "const open = () => { declare(router); const inner = () => { const router = 1; return router; }; inner(); };\nopen();\n",
			"router.work:3:30: router is read only as router.<word>(…)"},
		"a door beside an inner block": {routerImport + "const open = () => { router.get((context) => context.text(\"door in fn\")); if (1 > 2) { const router = 0; } };\nopen();\n",
			"router.work:2:28: router.get(…) is written at the top level of the file"},
	} {
		got := bootError(t, map[string]string{
			"apps/shop/core/doors.work":       library,
			"apps/shop/shop.test/router.work": c.source,
		})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: boot error %q, want one saying %q", name, got, c.want)
		}
	}
}

// kitwork.router is another spelling of the import, at the top level and in a function's body alike, and
// so is const { router } = kitwork; a top-level get, post, put or patch of router.work is one however it is
// declared, a destructuring's and a let's included, and the error says which keyword (05/11/2026: each of
// these booted, kitwork.router.post from a function's body declaring a door at its call).
func TestKitworkRouterAndADestructuredGetAreBootErrors(t *testing.T) {
	handler := "(context) => context.text(\"k\")"
	for name, c := range map[string]struct{ source, want string }{
		"kitwork.router.get":         {"kitwork.router.get(" + handler + ");\n", "router.work:1:1: the router is imported on one line"},
		"in a function's body":       {"const open = () => { kitwork.router.post(" + handler + "); };\nopen();\n", "router.work:1:22: the router is imported on one line"},
		"kept in a function's const": {"const open = () => { const r = kitwork.router; r.post(" + handler + "); };\nopen();\n", "router.work:1:32: the router is imported on one line"},
		"kitwork[\"router\"]":        {"const open = () => kitwork[\"router\"];\n", "router.work:1:20: the router is imported on one line"},
		"const { router } = kitwork": {"const { router } = kitwork;\nrouter.get(" + handler + ");\n", "router.work:1:9: the router is imported on one line"},
		"const { get }":              {routerImport + "const { get } = { get: 1 };\nrouter.get(" + handler + ");\n", "router.work:2:9: const get at the top level of router.work looks like a door"},
		"const [post]":               {routerImport + "const [post] = [1];\nrouter.get(" + handler + ");\n", "router.work:2:8: const post at the top level of router.work looks like a door"},
		"let put":                    {routerImport + "let put = 1;\nrouter.get(" + handler + ");\n", "router.work:2:1: let put ở cấp cao nhất của file"},
	} {
		got := bootError(t, map[string]string{"apps/shop/shop.test/router.work": c.source})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: boot error %q, want one saying %q", name, got, c.want)
		}
	}
}

// What reads no import boots as JavaScript runs it (node 22 runs each): a key router of an object, a
// for…of variable router, and the import written without spaces (05/11/2026: each was a boot error, the
// key and the loop's variable read as the router, the import split on spaces).
func TestASpellingThatReadsNoRouterBoots(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/key/router.work": routerImport + `router.get((context) => context.json({ router: 1 }));
`,
		"apps/shop/shop.test/setting/router.work": routerImport + `const settings = { router: "x" };
router.get((context) => context.text(settings.router));
`,
		"apps/shop/shop.test/loop/router.work": routerImport + `router.get((context) => { let total = 0; for (const router of [1, 2]) { total += router; } return context.text("" + total); });
`,
		"apps/shop/shop.test/tight/router.work": `import*as router from"kitwork/router";
router.get((context) => context.text("tight"));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	for path, want := range map[string]string{"/key": `{"router":1}`, "/setting": "x", "/loop": "3", "/tight": "tight"} {
		if got := app.get(t, "shop.test", path); got.status != 200 || got.body != want {
			t.Errorf("GET %s: %d %q, want 200 %q", path, got.status, got.body, want)
		}
	}
}

// The spelling is read once, each node once: its cost is about a parse, not the product of how deep the
// file nests and how long it is (05/11/2026: a router.work of 300 functions nested around an array of
// 20,000 items booted in 4.3 s, 70 times its compile, each function's body walked again for every function
// around it; x[x[…] += 1] += 1, 16 deep, booted in 0.8 s, 30 times its compile, the subtree x[…] += 1 shares
// walked once for each of its two places; a file of 60,000 problems made a boot error of 10 MB).
func TestTheRoutersSpellingCostsAboutAParse(t *testing.T) {
	nested := func(depth int) string {
		var b strings.Builder
		b.WriteString(routerImport)
		for i := 0; i < depth; i++ {
			b.WriteString("const f" + strconv.Itoa(i) + " = () => {\n")
		}
		b.WriteString("return [")
		for i := 0; i < 20_000; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("12345")
		}
		b.WriteString("];\n")
		for i := depth - 1; i >= 1; i-- {
			b.WriteString("};\nreturn f" + strconv.Itoa(i) + ";\n")
		}
		b.WriteString("};\nrouter.get((context) => context.text(\"ok\"));\n")
		return b.String()
	}
	// 200 of them: the compiler reads the nest once too since 05/11/2026, so one compiles in 50 µs, and
	// the boot's own cost, the manifest and the folders, would be all one measured
	compound := func(depth, count int) string {
		expression := "x[0]"
		for i := 0; i < depth; i++ {
			expression = "x[" + expression + " += 1]"
		}
		var b strings.Builder
		b.WriteString(routerImport + "const x = [0];\n")
		for i := 0; i < count; i++ {
			b.WriteString("const h" + strconv.Itoa(i) + " = (context) => context.text(\"\" + (" + expression + " += 1));\n")
		}
		b.WriteString("router.get((context) => context.text(\"ok\"));\n")
		return b.String()
	}
	for name, source := range map[string]string{"300 functions nested": nested(300), "x[…] += 1, 16 deep, 200 times": compound(16, 200)} {
		folder := write(t, map[string]string{"application.work": manifest, "apps/shop/shop.test/router.work": source})
		booting := fastest(func() {
			read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
			if err == nil {
				err = read.Boot()
			}
			if err == nil {
				err = read.Failed()
			}
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
		compiling := fastest(func() {
			if _, err := compiler.CompileSource(source); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
		ratio := float64(booting) / float64(compiling)
		t.Logf("%s: boot %v, compile %v, %.1f times", name, booting, compiling, ratio)
		if ratio > 8 {
			t.Errorf("%s: the boot took %v, %.1f times the compile's %v, want the spelling to cost about a parse", name, booting, ratio, compiling)
		}
	}
	// a router written once is one problem, however often the parser shares the node it is in
	shared := "x[router]"
	for i := 0; i < 12; i++ {
		shared = "x[" + shared + " += 1]"
	}
	got := bootError(t, map[string]string{"apps/shop/shop.test/router.work": routerImport + "const x = [0];\nconst h = () => " + shared + " += 1;\n"})
	if strings.Count(got, "router is read only") != 1 || strings.Contains(got, " more") {
		t.Errorf("a router written once in x[x[…] += 1] += 1: boot error %q, want one problem", got[:min(len(got), 300)])
	}
	var many strings.Builder
	many.WriteString(routerImport)
	for i := 0; i < 1000; i++ {
		many.WriteString("helper(router);\n")
	}
	got = bootError(t, map[string]string{"apps/shop/shop.test/router.work": "const helper = (r) => r;\n" + many.String()})
	if !strings.Contains(got, "; and 980 more") || len(got) > 8<<10 {
		t.Errorf("a file of 1,000 problems: a boot error of %d bytes ending %q, want 20 problems and the count of the rest", len(got), got[max(0, len(got)-80):])
	}
}

// fastest is the shortest of three runs of work.
func fastest(work func()) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		began := time.Now()
		work()
		best = min(best, time.Since(began))
	}
	return best
}
