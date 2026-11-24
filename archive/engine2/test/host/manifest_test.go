package host_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// A word of the manifest engine2 does not serve yet prints one line and the chain goes on: the words after it
// still declare.
func TestTheManifestNamesWhatItDoesNotServeAndGoesOn(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.gateway({ target: "http://node-a:3000" });
app.desktop({ name: "Shop" }).website({ hostname: "Shop.Test", port: 8123, trustProxy: false });
app.postgresql({ alias: "system", host: "203.0.113.1" }).run();
`,
		"apps/.keep": "",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	manifest := read.Manifest
	if manifest.Website == nil || manifest.Website.Hostname != "shop.test" || manifest.Website.Port != 8123 || !manifest.Run {
		t.Errorf("website %+v, run %v: the words after the one not served were lost", manifest.Website, manifest.Run)
	}
	if want := []string{"desktop"}; !slices.Equal(manifest.Surfaces, want) {
		t.Errorf("surfaces %v, want %v", manifest.Surfaces, want)
	}
	contains(t, "the log", log.String(),
		"[app] app.gateway() is not served by engine2 yet — ignored\n",
		"[app] app.desktop() is not served by engine2 yet — ignored\n",
		"[app] app.website({ trustProxy }) is not served by engine2 yet — ignored\n",
		"[app] app.postgresql() is not served by engine2 yet — ignored\n")
	if err := read.Check(); err != nil {
		t.Errorf("check: %v", err)
	}
}

// A word that is no word of the manifest, a typo or a word of another engine, is the manifest's error at its line,
// saying the nearest word when one is near (15/11/2026, the owner): it was one line and passed over, so
// app.websit({ … }) read as a host with no website.
func TestAWordTheManifestDoesNotHaveIsItsError(t *testing.T) {
	for source, want := range map[string]string{
		"app.websit({ hostname: \"shop.test\", port: 8080 });":    "application.work:2:4: app has no word websit: did you mean app.website?",
		"app.root(\"apps\");\napp.flavour(true);":                 "application.work:3:4: app has no word flavour",
		"app.website(\"shop.test\").limts({ memory: \"64mb\" });": "app has no word limts: did you mean app.limits?",
	} {
		folder := write(t, map[string]string{"application.work": "import * as app from \"kitwork/application\";\n" + source})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want the error %q", source, err, want)
		}
	}
}

// A served word declared twice, a wrong argument, a manifest that declares nothing: each stops at the
// line that says it, with the manifest's name.
func TestTheManifestStopsAtAWrongWord(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{"app.website(\"a.test\");\napp.website(\"b.test\");", "application.work:3:4: app.website(…) is declared twice"},
		{"app.website({ hostname: \"a.test\", port: 70000 });", "app.website({ port }): a whole number from 1 to 65535, got 70000"},
		{"app.website({ hostname: \"a.test\", port: \"8080\" });", "app.website({ port }): a whole number from 1 to 65535, got 8080"},
		{"app.website({ hostname: \"a.test\", allowLocl: true });", "app.website({ allowLocl }): not an option of app.website"},
		{"app.website({ hostname: \"a.test\", canonical: \"apex\" });", "app.website({ canonical }) is gone: a site's www. name and its bare one go to the site with no word"},
		{"app.website(\"https://a.test/\");", "is not a hostname"},
		{"import * as router from \"kitwork/router\";", "kitwork/router is a site's"},
	} {
		folder := write(t, map[string]string{"application.work": "import * as app from \"kitwork/application\";\n" + c.source})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error saying %q", c.source, err, c.want)
		}
	}
	folder := write(t, map[string]string{"application.work": "import * as app from \"kitwork/application\";\napp.logger({});"})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Check(); err == nil || !strings.Contains(err.Error(), "declares no surface") {
		t.Errorf("a manifest with no surface: %v", err)
	}
}

// app.run() is the switch on the manifest's last line (docs/KITWORK_BOOT.md mục 3, luật 5; Quốc, 05/11/2026; engine2
// 12/11/2026): it takes nothing, the port it took is the website's, app.website({ port }), and a word of the manifest
// written after it stops the boot at that word. A website app.run() opens has a port; one a manifest only declares
// needs none.
func TestAppRunIsTheSwitchOnTheLastLine(t *testing.T) {
	read := func(source string) (*host.Host, error) {
		folder := write(t, map[string]string{
			"application.work": "import * as app from \"kitwork/application\";\n" + source,
			"apps/.keep":       "",
		})
		return host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	}
	// the port, read from the website, opened by the switch
	served, err := read("app.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8123 });\napp.run();\n")
	if err != nil {
		t.Fatal(err)
	}
	if served.Manifest.Website.Port != 8123 || !served.Manifest.Run {
		t.Fatalf("website %+v, run %v: want port 8123 and the switch on", served.Manifest.Website, served.Manifest.Run)
	}
	if err := served.Check(); err != nil {
		t.Errorf("check: %v", err)
	}
	if err := served.Boot(); err != nil {
		t.Fatal(err)
	}
	defer served.Close()
	if server, err := served.Server(); err != nil || server.Addr != "127.0.0.1:8123" {
		t.Errorf("server %v (%v): want it on the website's port, for this machine alone beside AutoSSL, 127.0.0.1:8123", server, err)
	}
	// the port written where it was until 12/11/2026, or anything else given to the switch
	for source, want := range map[string]string{
		"app.website({ hostname: \"shop.test\", port: 8080 });\napp.run(8080);":   "application.work:3:4: app.run(8080): the port is the website's, app.website({ hostname: \"kitwork.io\", port: 8080 }), and app.run() takes nothing",
		"app.website({ hostname: \"shop.test\", port: 8080 });\napp.run(null);":   "app.run() takes nothing: it is the switch on the manifest's last line",
		"app.website({ hostname: \"shop.test\", port: 8080 });\napp.run(\"go\");": "app.run() takes nothing",
	} {
		if _, err := read(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", source, err, want)
		}
	}
	// every word of the manifest written after the switch, at its line
	for _, word := range []string{
		`root("apps")`, `website({ hostname: "b.test", port: 8081 })`, `hotReload(true)`, `prewarm("*")`,
		`logger({ logfile: "logs/a.log" })`, `limits({ memory: "64MB" })`, `history({ days: 1 })`,
		`database(".database", { port: 5999, password: "pw" })`, `databases({ jobs: { sqlite: "./jobs.db" } })`,
		`desktop({ name: "Shop" })`, `postgresql({ alias: "system" })`, `shareEnvironment(["A"])`, `flavour(true)`,
		`commands({ hello: (context) => 1 })`,
	} {
		name := word[:strings.Index(word, "(")]
		source := "app.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\napp." + word + ";\n"
		want := "application.work:4:4: app." + name + "(…) is written after app.run(): app.run() is the manifest's last line"
		if name == "postgresql" || name == "shareEnvironment" {
			want = "application.work:4:4: app." + name + "() is written after app.run()"
		}
		if _, err := read(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("app.%s after app.run(): %v, want an error saying %q", word, err, want)
		}
	}
	// a chain ending with the switch is the last line too; a chain going on after it is not
	if _, err := read("app.root(\"apps\").website({ hostname: \"shop.test\", port: 8080 }).run();\n"); err != nil {
		t.Errorf("a chain ending with app.run(): %v", err)
	}
	if _, err := read("app.root(\"apps\").run().website({ hostname: \"shop.test\", port: 8080 });\n"); err == nil || !strings.Contains(err.Error(), "app.website(…) is written after app.run()") {
		t.Errorf("a chain going on after app.run(): %v", err)
	}
	// a website the switch opens has a port; one a manifest only declares needs none
	portless, err := read("app.root(\"apps\");\napp.website(\"shop.test\");\napp.run();\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := portless.Check(); err == nil || !strings.Contains(err.Error(), `app.website(…) has no port, which app.run() opens it on: app.website({ hostname: "shop.test", port: 8080 })`) {
		t.Errorf("app.run() of a website with no port: %v", err)
	}
	declared, err := read("app.root(\"apps\");\napp.website(\"shop.test\");\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := declared.Check(); err != nil {
		t.Errorf("a website declared, nothing run: %v", err)
	}
}

// The manifest reads the host's .env with the system's variables on top, the system winning and saying
// so; an app never sees either (TestTwoSitesKeepTheirOwnConsoleClockAndEnvironment).
func TestTheManifestReadsItsEnvironmentWithTheSystemOnTop(t *testing.T) {
	t.Setenv("PORT", "8299")
	log := &lines{}
	read, err := host.ReadWithLog(testApp, log)
	if err != nil {
		t.Fatal(err)
	}
	if read.Manifest.Website == nil || read.Manifest.Website.Port != 8299 {
		t.Errorf("website %+v: the system's PORT (8299) wins over host.env's (8199)", read.Manifest.Website)
	}
	contains(t, "the log", log.String(), "[app] ./host.env sets PORT, which the operating system sets too: the system's value wins")
	if read.Manifest.Website == nil || !read.Manifest.Website.AllowLocal {
		t.Errorf("website %+v: allowLocal=true of host.env is the boolean true", read.Manifest.Website)
	}
}

// The host's real manifest, D:/project/kitwork/application.work, reads and checks: every word it writes is
// served or named, nothing connects (its postgresql and desktop are named, not opened; its database port is read,
// and only Serve opens it).
func TestTheHostsManifestReads(t *testing.T) {
	file := os.Getenv("KITWORK_HOST_MANIFEST")
	if file == "" {
		file = filepath.Join("..", "..", "..", "application.work") // engine2 sits in the host's repository
	}
	if _, err := os.Stat(file); err != nil {
		t.Skipf("no host manifest at %s (set KITWORK_HOST_MANIFEST)", file)
	}
	log := &lines{}
	read, err := host.ReadWithLog(file, log)
	if err != nil {
		t.Fatalf("read %s: %v\n%s", file, err, log)
	}
	if err := read.Check(); err != nil {
		t.Fatalf("check %s: %v", file, err)
	}
	manifest := read.Manifest
	if manifest.Website == nil || manifest.Website.Hostname == "" || manifest.Website.Port == 0 || !manifest.Run {
		t.Errorf("website %+v, run %v: the host serves a website on a port, app.run() on the last line", manifest.Website, manifest.Run)
	}
	if manifest.Database == nil || manifest.Database.Port == 0 || manifest.Database.Password == "" {
		t.Errorf("app.database = %+v: the host declares its database port", manifest.Database)
	}
	// its search index of shopping and its catalogue are read (07/11/2026)
	if search := manifest.Database.Search; len(search) != 1 || search[0].Database != "shopping" || search[0].Table != "shopping" || len(search[0].Columns) != 2 {
		t.Errorf("app.database({ search }) = %+v, want shopping's index of two columns", search)
	}
	// the demo's jobs are its own database since 17/11/2026 (connection().schema("jobs")), so the host shares none
	if jobs := manifest.Databases["jobs"]; jobs != nil {
		t.Errorf("app.databases jobs = %+v: the demo's jobs are its own, in its .database/", jobs)
	}
	if !manifest.HotReload {
		t.Error("app.hotReload(true) of the host's manifest is not read")
	}
	if !slices.Equal(manifest.Prewarm, []string{"kitwork.io"}) {
		t.Errorf("app.prewarm = %v, want kitwork.io warmed (11/11/2026)", manifest.Prewarm)
	}
	if logger := manifest.Logger; logger == nil || !strings.HasSuffix(filepath.ToSlash(logger.LogFile), "logs/kitwork.log") || logger.Level != "info" {
		t.Errorf("app.logger = %+v, want the host's logs/kitwork.log at info", logger)
	}
	if limit := manifest.Website.RateLimit; limit == nil || limit.Rate != 2000 || limit.IP != 120 || limit.Browser != 240 || limit.Period != time.Second {
		t.Errorf("app.website({ rateLimit }) = %+v, want 2000, 120, 240 a second", limit)
	}
	for _, word := range []string{"postgresql()", "desktop()"} {
		if !slices.Contains(manifest.Ignored, word) {
			t.Errorf("app.%s is not named among the words not served: %v", word, manifest.Ignored)
		}
	}
	// a word engine2's manifest does not have is its read's error (15/11/2026): the read above would have failed
}
