package host

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// safeBuffer is a log the boot and the test read and write at once.
type safeBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

// panicHost reads a host serving one app, apps/one with the site shop.test, and gives it with its log.
func panicHost(t *testing.T) (*Host, *safeBuffer) {
	t.Helper()
	folder := t.TempDir()
	files := map[string]string{
		"application.work":               "import * as app from \"kitwork/application\";\n\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/one/shop.test/router.work": "import * as router from \"kitwork/router\";\nrouter.get((context) => context.text(\"a\"));\n",
		"apps/one/module.work":           "import * as module from \"kitwork/module\";\nmodule.routes((site) => site.domain);\n",
	}
	for name, text := range files {
		file := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := &safeBuffer{}
	h, err := ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h, log
}

// A panic of the engine as an app boots is that boot's error, said in the log with its stack: the app is closed as an
// app whose boot failed is, its work let go, and the host goes on (13/11/2026: nothing recovered at boot or at a
// reload, so one app ended the host and every other app). Here the map of the other apps' sites holds a nil where the
// boot reads a site, a stand-in for a fault of the engine.
func TestAPanicAsAnAppBootsIsThatBootsError(t *testing.T) {
	h, log := panicHost(t)
	run := &appRun{identity: "one", root: filepath.Join(filepath.Dir(h.File), "apps", "one")}
	opened, err := h.bootWholeApp(run, map[string]*Site{"shop.test": nil})
	if err == nil || !strings.Contains(err.Error(), "boot: a panic of the engine ended it, not the app's code: runtime error: invalid memory address") {
		t.Fatalf("err = %v, want the boot's error saying the panic", err)
	}
	if opened != nil || run.back != nil {
		t.Errorf("a boot that panicked opened %d sites and kept its work (%v)", len(opened), run.back != nil)
	}
	if text := log.String(); !strings.Contains(text, "[one] boot: panic: runtime error") || !strings.Contains(text, "goroutine ") {
		t.Errorf("the log says:\n%s\nwant the panic with its stack", text)
	}
}

// A panic of the engine in a look of hot reload is said with its stack, and the watcher looks again at its next tick.
func TestAPanicInALookOfHotReloadIsSaid(t *testing.T) {
	h, log := panicHost(t)
	h.apps = nil // a look writes to the map of the apps: nil, a stand-in for a fault of the engine
	var manifest map[string]mark
	h.reloadChanged(&manifest) // a new app, seen once: noted as pending
	h.reloadChanged(&manifest) // seen twice: it boots, and the look writes it in h.apps
	if text := log.String(); !strings.Contains(text, "[app] hot reload: panic: assignment to entry in nil map") || !strings.Contains(text, "goroutine ") {
		t.Errorf("the log says:\n%s\nwant the panic with its stack", text)
	}
	if !h.reloading.TryLock() {
		t.Fatal("the look that panicked kept the reload's lock")
	}
	h.reloading.Unlock()
}
