package host_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// junction makes link a junction to target, as a Windows host keeps its data on another disk (mklink /J);
// the test skips where there is none.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows's")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

// An import that stays inside its app boots whatever links the host's own layout has above the app's
// folder: filepath.EvalSymlinks fails below a junction on Windows, and every relative import of an app
// whose apps/ folder was a junction was refused as leaving the app (05/11/2026). A link inside the app that
// cannot be followed is still refused, and the error names it.
func TestAnAppBehindAJunctionImportsItsOwnFiles(t *testing.T) {
	host := t.TempDir()
	real := filepath.Join(host, "data")
	for name, text := range map[string]string{
		"shop/core/mine.work":            "export const mine = \"mine\";\n",
		"shop/shop.test/helper.work":     "export const local = \"local\";\n",
		"shop/shop.test/router.work":     routerImport + "import { mine } from \"../core/mine.work\";\nimport { local } from \"./helper.work\";\nrouter.get((context) => context.text(mine + \" \" + local));\n",
		"shop/shop.test/app/router.work": routerImport + "import { mine } from \"app/core/mine.work\";\nrouter.get((context) => context.text(\"app \" + mine));\n",
		"outside/secret.work":            "export const secret = \"secret\";\n",
		"shop/module.work":               routedModule,
		"outside/module.work":            routedModule,
	} {
		file := filepath.Join(real, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(host, "application.work"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(host, "apps"), real)
	app := boot(t, filepath.Join(host, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 200 || got.body != "mine local" {
		t.Errorf("GET /: %d %q, want the app's own files: mine local", got.status, got.body)
	}
	if got := app.get(t, "shop.test", "/app"); got.status != 200 || got.body != "app mine" {
		t.Errorf("GET /app: %d %q, want app mine", got.status, got.body)
	}

	// a junction inside the app, to a folder outside it, is refused, named
	junction(t, filepath.Join(real, "shop", "shop.test", "out"), filepath.Join(real, "outside"))
	if err := os.WriteFile(filepath.Join(real, "shop", "shop.test", "router.work"), []byte(routerImport+"import { secret } from \"./out/secret.work\";\nrouter.get((context) => context.text(secret));\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := bootError(t, map[string]string{"application.work": strings.ReplaceAll(manifest, `app.root("apps")`, `app.root("`+filepath.ToSlash(filepath.Join(host, "apps"))+`")`)})
	t.Logf("the boot error: %s", got)
	if !strings.Contains(got, `import "./out/secret.work"`) || !strings.Contains(got, "liên kết shop.test/out") {
		t.Errorf("an import through a junction inside the app: %q, want it refused, naming the link", got)
	}
}
