package host_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// app.prewarm(…) (11/11/2026): a site the manifest names is read at its boot and kept for it, its .ui files parsed,
// its pages assembled, its small files held in memory, so no request of it reads the disk; a site it does not name
// reads each page at its first request and each file from the disk at each.

// prewarmFiles are two sites of one app, shop.test and other.test, each a page and a stylesheet; shop.test has a
// robots.txt, a file past the size a site holds, and a file whose name carries its hash. prewarm is the manifest's line.
func prewarmFiles(prewarm string) map[string]string {
	script := "console.log(1);"
	sum := sha256.Sum256([]byte(script))
	return map[string]string{
		"application.work": "import * as app from \"kitwork/application\";\n\napp.root(\"apps\");\n" + prewarm +
			"\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/shop/shop.test/content.ui":                                            `<content><text>Shop</text></content>`,
		"apps/shop/shop.test/assets/site.css":                                       "body { color: red }",
		"apps/shop/shop.test/assets/big.bin":                                        strings.Repeat("x", 300<<10),
		"apps/shop/shop.test/assets/app." + hex.EncodeToString(sum[:])[:12] + ".js": script,
		"apps/shop/shop.test/robots.txt":                                            "User-agent: *",
		"apps/shop/other.test/content.ui":                                           `<content><text>Other</text></content>`,
		"apps/shop/other.test/assets/site.css":                                      "body { color: blue }",
	}
}

// prewarmed boots files, its files checked against the disk every check, and serves them; folder is where they are.
func prewarmed(t *testing.T, files map[string]string, check time.Duration) (b *booted, folder string) {
	t.Helper()
	folder = write(t, files)
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	read.StaticCheck = check
	return serve(t, read, log), folder
}

// remove removes the file name under folder.
func remove(t *testing.T, folder, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(folder, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

// A prewarmed site serves its small files from memory: removed from the disk, each is still served while it is not
// checked again; a file past the size held, and every file of a site the manifest does not name, is read from the disk
// at each request, gone once removed. The boot says what it kept.
//
// CONTROL (11/11/2026): with Static.memory left nil, the removed stylesheet of shop.test answered 404.
func TestAPrewarmedSiteServesItsFilesFromMemory(t *testing.T) {
	b, folder := prewarmed(t, prewarmFiles(`app.prewarm("shop.test");`), time.Hour)
	contains(t, "the boot", b.log.String(), "[shop.test] prewarm: 1 pages, 1 .ui files, 3 files in memory (1 KB)")
	if strings.Contains(b.log.String(), "[other.test] prewarm") {
		t.Errorf("other.test, which the manifest does not name, was warmed:\n%s", b.log)
	}
	for _, name := range []string{"assets/site.css", "assets/big.bin", "robots.txt"} {
		if got := b.get(t, "shop.test", "/"+name); got.status != http.StatusOK {
			t.Fatalf("GET /%s: %d", name, got.status)
		}
		remove(t, folder, "apps/shop/shop.test/"+name)
	}
	if got := b.get(t, "other.test", "/assets/site.css"); got.status != http.StatusOK || got.body != "body { color: blue }" {
		t.Fatalf("other.test: %d %q", got.status, got.body)
	}
	remove(t, folder, "apps/shop/other.test/assets/site.css")

	if got := b.get(t, "shop.test", "/assets/site.css"); got.status != http.StatusOK || got.body != "body { color: red }" ||
		got.header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("the stylesheet held: %d %q %v", got.status, got.body, got.header)
	}
	if got := b.get(t, "shop.test", "/robots.txt"); got.status != http.StatusOK || got.body != "User-agent: *" {
		t.Errorf("robots.txt held: %d %q", got.status, got.body)
	}
	if got := b.get(t, "shop.test", "/assets/big.bin"); got.status != http.StatusNotFound {
		t.Errorf("a file past 256 KB, removed: %d, want it read from the disk, 404", got.status)
	}
	if got := b.get(t, "other.test", "/assets/site.css"); got.status != http.StatusNotFound {
		t.Errorf("a file of a site not prewarmed, removed: %d, want 404", got.status)
	}
}

// A file held answers as one read from the disk does: a range, a HEAD, a 304 for its ETag, the year a browser keeps a
// file whose name carries its hash.
func TestAFileHeldAnswersAsTheDiskDoes(t *testing.T) {
	b, folder := prewarmed(t, prewarmFiles(`app.prewarm("shop.test");`), time.Hour)
	remove(t, folder, "apps/shop/shop.test/assets/site.css") // what follows is memory's
	part := b.request(t, http.MethodGet, "shop.test", "/assets/site.css", nil, "Range", "bytes=0-3")
	if part.status != http.StatusPartialContent || part.body != "body" {
		t.Errorf("a range: %d %q", part.status, part.body)
	}
	head := b.request(t, http.MethodHead, "shop.test", "/assets/site.css", nil)
	if head.status != http.StatusOK || head.body != "" || head.header.Get("Content-Length") != "19" {
		t.Errorf("HEAD: %d %q %v", head.status, head.body, head.header)
	}
	etag := head.header.Get("ETag")
	if again := b.request(t, http.MethodGet, "shop.test", "/assets/site.css", nil, "If-None-Match", etag); etag == "" || again.status != http.StatusNotModified {
		t.Errorf("If-None-Match %q: %d", etag, again.status)
	}
	if head.header.Get("Cache-Control") != "public, max-age=3600" {
		t.Errorf("Cache-Control %q", head.header.Get("Cache-Control"))
	}
	var hashed string
	for name := range prewarmFiles("") {
		if strings.HasSuffix(name, ".js") {
			hashed = strings.TrimPrefix(name, "apps/shop/shop.test")
		}
	}
	remove(t, folder, "apps/shop/shop.test"+hashed)
	if got := b.get(t, "shop.test", hashed); got.status != http.StatusOK || got.header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("GET %s: %d %q", hashed, got.status, got.header.Get("Cache-Control"))
	}
}

// A file held is checked against the disk at most once every StaticCheck: edited in place, it is served as it is once
// checked; gone, it is gone once a check finds it gone. The clock of Windows moves by about half a millisecond: a
// request within the tick of the last check is not checked, so the test asks until one is (11/11/2026: asked at once,
// 16 runs of 30 got the file held; 2 ms later, none).
//
// CONTROL (11/11/2026): with the check left out of keptFiles.serve, the edited stylesheet was served as it was.
func TestAFileHeldIsCheckedAgainstTheDisk(t *testing.T) {
	b, folder := prewarmed(t, prewarmFiles(`app.prewarm("shop.test");`), time.Hour)
	rewrite(t, folder, "apps/shop/shop.test/assets/site.css", "body { color: green; margin: 0 }")
	if got := b.get(t, "shop.test", "/assets/site.css"); got.body != "body { color: red }" {
		t.Errorf("checked within the hour: %q", got.body)
	}

	b, folder = prewarmed(t, prewarmFiles(`app.prewarm("shop.test");`), time.Nanosecond)
	rewrite(t, folder, "apps/shop/shop.test/assets/site.css", "body { color: green; margin: 0 }")
	if got := b.get(t, "shop.test", "/assets/site.css"); got.status != http.StatusOK || got.body != "body { color: green; margin: 0 }" {
		t.Errorf("edited, then checked: %d %q", got.status, got.body)
	}
	if got := b.get(t, "shop.test", "/assets/site.css"); got.body != "body { color: green; margin: 0 }" {
		t.Errorf("the edited file, held again: %q", got.body)
	}
	remove(t, folder, "apps/shop/shop.test/assets/site.css")
	eventually(t, "the removed file gone", func() bool {
		return b.get(t, "shop.test", "/assets/site.css").status == http.StatusNotFound
	})

	// the file read again once checked is held again, and so is a file written after the boot: removed, both are
	// still served until the next check
	b, folder = prewarmed(t, prewarmFiles(`app.prewarm("shop.test");`), 300*time.Millisecond)
	rewrite(t, folder, "apps/shop/shop.test/assets/site.css", "body { color: green; margin: 0 }")
	rewrite(t, folder, "apps/shop/shop.test/assets/new.css", "p { margin: 0 }")
	time.Sleep(350 * time.Millisecond)
	for _, name := range []string{"site.css", "new.css"} {
		if got := b.get(t, "shop.test", "/assets/"+name); got.status != http.StatusOK {
			t.Fatalf("GET /assets/%s: %d", name, got.status)
		}
		remove(t, folder, "apps/shop/shop.test/assets/"+name)
		if got := b.get(t, "shop.test", "/assets/"+name); got.status != http.StatusOK {
			t.Errorf("/assets/%s read since the boot, removed before the next check: %d, want it held", name, got.status)
		}
	}
}

// A prewarmed site's pages are read at its boot: its content.ui and its notfound.ui removed from the disk after it,
// each page is drawn as the boot read it; a site the manifest does not name reads its page at the first request, and
// finds none.
//
// CONTROL (11/11/2026): with ui.Site.Prewarm not called, the page of shop.test was not drawn either.
func TestAPrewarmedSiteReadsItsPagesAtItsBoot(t *testing.T) {
	files := prewarmFiles(`app.prewarm("shop.test");`)
	files["apps/shop/shop.test/notfound.ui"] = `<content><text>Gone fishing</text></content>`
	b, folder := prewarmed(t, files, time.Hour)
	contains(t, "the boot", b.log.String(), "[shop.test] prewarm: 2 pages, 2 .ui files")
	remove(t, folder, "apps/shop/shop.test/content.ui")
	remove(t, folder, "apps/shop/shop.test/notfound.ui")
	remove(t, folder, "apps/shop/other.test/content.ui")
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusOK || !strings.Contains(got.body, "Shop") {
		t.Errorf("the page read at boot: %d\n%.300s", got.status, got.body)
	}
	if got := b.get(t, "shop.test", "/nowhere"); got.status != http.StatusNotFound || !strings.Contains(got.body, "Gone fishing") {
		t.Errorf("the not-found page read at boot: %d\n%.300s", got.status, got.body)
	}
	if got := b.get(t, "other.test", "/"); got.status == http.StatusOK || strings.Contains(got.body, "Other") {
		t.Errorf("the page of a site not prewarmed, removed before its first request: %d\n%.300s", got.status, got.body)
	}
}

// What a prewarmed site cannot read is said at its boot, once, and the boot goes on: a .ui that does not parse, a
// frame two pages stand in that does not parse, so neither assembles. A name no app serves is said too; "*"
// warms every site; app.prewarm(false) none.
func TestPrewarmSaysWhatItCannotRead(t *testing.T) {
	files := prewarmFiles(`app.prewarm("nope.test", "shop.test");`)
	files["apps/shop/shop.test/broken.ui"] = `<box><text>x</box>`
	files["apps/shop/shop.test/guide/view.ui"] = `<view><content /><text>x</view>`
	files["apps/shop/shop.test/guide/a/content.ui"] = `<content><text>A</text></content>`
	files["apps/shop/shop.test/guide/b/content.ui"] = `<content><text>B</text></content>`
	b, _ := prewarmed(t, files, time.Hour)
	contains(t, "the boot", b.log.String(), "[shop.test] prewarm: ", "broken.ui:1:",
		"app.prewarm names nope.test, which no app serves: nothing is warmed for it")
	if count := strings.Count(b.log.String(), "view.ui:1:"); count != 1 {
		t.Errorf("the frame that does not parse said %d times, want once:\n%s", count, b.log)
	}
	if got := b.get(t, "shop.test", "/"); got.status != http.StatusOK {
		t.Errorf("the site after what it could not read: %d", got.status)
	}

	b, _ = prewarmed(t, prewarmFiles(`app.prewarm("*");`), time.Hour)
	contains(t, "every site", b.log.String(), "[shop.test] prewarm: 1 pages", "[other.test] prewarm: 1 pages")
	b, _ = prewarmed(t, prewarmFiles(`app.prewarm(false);`), time.Hour)
	if strings.Contains(b.log.String(), "prewarm") {
		t.Errorf("app.prewarm(false) warmed:\n%s", b.log)
	}
	b, _ = prewarmed(t, prewarmFiles(""), time.Hour)
	if strings.Contains(b.log.String(), "prewarm") {
		t.Errorf("no app.prewarm warmed:\n%s", b.log)
	}
}

// An app booted again by hot reload warms its sites again, the new boot's own.
func TestAReloadedAppIsWarmedAgain(t *testing.T) {
	files := prewarmFiles("app.prewarm(\"shop.test\");\napp.hotReload(true);")
	b, folder := prewarmed(t, files, time.Hour)
	b.host.ReloadEvery = 20 * time.Millisecond
	b.host.StartWatching()
	rewrite(t, folder, "apps/shop/shop.test/assets/site.css", "body { color: green; margin: 0 }")
	eventually(t, "the reload", func() bool { return strings.Count(b.log.String(), "[shop.test] prewarm: ") == 2 })
	remove(t, folder, "apps/shop/shop.test/assets/site.css")
	if got := b.get(t, "shop.test", "/assets/site.css"); got.body != "body { color: green; margin: 0 }" {
		t.Errorf("the file the new boot held: %d %q", got.status, got.body)
	}
}

// app.prewarm(…) names sites, once: a second declaration, true, no name, "*" beside a name, an address, a name twice,
// a number, each stops the read at its line.
func TestPrewarmIsWrittenOnce(t *testing.T) {
	for written, want := range map[string]string{
		"app.prewarm(\"a.test\");\napp.prewarm(\"b.test\");": "app.prewarm(…) is declared twice",
		"app.prewarm(true);":               "app.prewarm(…) names the sites it warms",
		"app.prewarm();":                   "app.prewarm(…) names the sites it warms",
		`app.prewarm("*", "a.test");`:      `app.prewarm("*") warms every site: alone`,
		`app.prewarm("https://a.test");`:   `"https://a.test" is no hostname`,
		`app.prewarm("a.test", "A.test");`: "app.prewarm(…) names a.test twice",
		`app.prewarm(1);`:                  "app.prewarm(…) names the sites it warms",
	} {
		folder := write(t, map[string]string{
			"application.work": "import * as app from \"kitwork/application\";\n" + written + "\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
			"apps/.keep":       "",
		})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", written, err, want)
		}
	}
}
