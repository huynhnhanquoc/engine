package host_test

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2"
	"github.com/kitwork/engine2/host"
)

// bootClosing reads and boots the apps files make, the boot itself succeeding, and serves them with the
// errors of the apps it closed.
func bootClosing(t *testing.T, files map[string]string) (*booted, string) {
	t.Helper()
	files["application.work"] = manifest
	folder := write(t, files)
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("one app's error stopped the host: %v", err)
	}
	failed := ""
	if err := read.Failed(); err != nil {
		failed = err.Error()
	}
	return serve(t, read, log), failed
}

// An app that cannot boot is closed alone (06/11/2026): every site of it answers 503 and tells the visitor
// nothing, its error goes to the log once, and every other app serves.
func TestAnAppThatCannotBootIsClosedAlone(t *testing.T) {
	b, failed := bootClosing(t, map[string]string{
		"apps/good/good.test/router.work":    routerImport + "router.get((context) => context.text(\"good\"));\n",
		"apps/bad/bad.test/router.work":      routerImport + "router.get(\"hello\");\n",
		"apps/bad/another.test/router.work":  routerImport + "router.get((context) => context.text(\"other\"));\n",
		"apps/bad/another.test/assets/a.css": "body{}",
	})
	if good := b.get(t, "good.test", "/"); good.status != http.StatusOK || good.body != "good" {
		t.Errorf("GET good.test/: %d %q, want the app that booted to serve", good.status, good.body)
	}
	// a site of the closed app is closed whole, its files with it, even one with nothing wrong of its own
	for _, path := range []string{"bad.test/", "another.test/", "another.test/assets/a.css"} {
		host, rest, _ := strings.Cut(path, "/")
		got := b.get(t, host, "/"+rest)
		if got.status != http.StatusServiceUnavailable || strings.TrimSpace(got.body) != "Service Unavailable" {
			t.Errorf("GET %s: %d %q, want 503 Service Unavailable and nothing more", path, got.status, got.body)
		}
	}
	reason := "router.get(…) takes the handler, a function of the program"
	if !strings.Contains(failed, reason) {
		t.Errorf("Failed is %q, want it to say %q", failed, reason)
	}
	log := b.log.String()
	if said := strings.Count(log, "bad is closed, its sites answer 503: "); said != 1 || !strings.Contains(log, reason) {
		t.Errorf("the log says the app is closed %d times, want once with its reason:\n%s", said, log)
	}
	if nowhere := b.get(t, "nowhere.test", "/"); nowhere.status != http.StatusNotFound {
		t.Errorf("GET nowhere.test/: %d, want a host no folder names to stay a 404", nowhere.status)
	}
}

// Two apps that name one site: the later app is closed and the hostname stays the first's, so one tenant
// never takes another's site, nor the host, down.
func TestTheLaterOfTwoAppsNamingOneSiteIsClosed(t *testing.T) {
	b, failed := bootClosing(t, map[string]string{
		"apps/one/shop.test/router.work": routerImport + "router.get((context) => context.text(\"one\"));\n",
		"apps/two/Shop.Test/router.work": routerImport + "router.get((context) => context.text(\"two\"));\n",
		"apps/two/two.test/router.work":  routerImport + "router.get((context) => context.text(\"two\"));\n",
	})
	if shop := b.get(t, "shop.test", "/"); shop.status != http.StatusOK || shop.body != "one" {
		t.Errorf("GET shop.test/: %d %q, want the first app's site", shop.status, shop.body)
	}
	if two := b.get(t, "two.test", "/"); two.status != http.StatusServiceUnavailable {
		t.Errorf("GET two.test/: %d, want the later app closed", two.status)
	}
	if !strings.Contains(failed, "the site shop.test is in two apps") {
		t.Errorf("Failed is %q, want it to name the site in two apps", failed)
	}
}

// A key env.require misses closes the app that requires it, named with its place; an app that requires
// nothing missing serves.
func TestAMissingRequiredKeyClosesOnlyItsApp(t *testing.T) {
	b, failed := bootClosing(t, map[string]string{
		"apps/needs/needs.test/router.work": "import * as file from \"kitwork/file\";\nconst env = file.env(\"app/app.env\");\nconst key = env.require(\"STRIPE_SECRET\");\n",
		"apps/fine/fine.test/router.work":   routerImport + "router.get((context) => context.text(\"fine\"));\n",
	})
	if fine := b.get(t, "fine.test", "/"); fine.status != http.StatusOK || fine.body != "fine" {
		t.Errorf("GET fine.test/: %d %q, want the app with its keys to serve", fine.status, fine.body)
	}
	if needs := b.get(t, "needs.test", "/"); needs.status != http.StatusServiceUnavailable {
		t.Errorf("GET needs.test/: %d, want the app missing a key closed", needs.status)
	}
	if want := "needs: env.require: no file sets STRIPE_SECRET (needs.test/router.work:3:"; !strings.Contains(failed, want) {
		t.Errorf("Failed is %q, want it to say %q", failed, want)
	}
}

// engine2.Check names every app Run would close, each error once, and is nil when every app boots.
func TestCheckNamesEveryAppRunWouldClose(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/good/good.test/router.work": routerImport + "router.get((context) => context.text(\"good\"));\n",
		"apps/a/a.test/router.work":       routerImport + "router.get(\"a\");\n",
		"apps/b/b.test/config.work":       routerImport + "router.colours({ brand: \"red\" });\n",
	})
	err := engine2.Check(filepath.Join(folder, "application.work"))
	if err == nil {
		t.Fatal("Check of two apps that cannot boot: nil")
	}
	for _, want := range []string{"router.get(…) takes the handler", "router has no word colours"} {
		if strings.Count(err.Error(), want) != 1 {
			t.Errorf("Check: %v\nwant it to say %q once", err, want)
		}
	}
	healthy := write(t, map[string]string{
		"application.work":                manifest,
		"apps/good/good.test/router.work": routerImport + "router.get((context) => context.text(\"good\"));\n",
	})
	if err := engine2.Check(filepath.Join(healthy, "application.work")); err != nil {
		t.Errorf("Check of an app that boots: %v", err)
	}
}

// What is the host's own still stops it: a folder of the apps that is not there closes no app, it is the
// host that cannot serve.
func TestAHostErrorStillStopsTheBoot(t *testing.T) {
	folder := write(t, map[string]string{"application.work": manifest})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err == nil || !strings.Contains(err.Error(), "app.root") {
		t.Errorf("a host with no folder of apps booted: %v", err)
	}
}
