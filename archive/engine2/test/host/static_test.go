package host_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// staticSite is a site with a file of each kind a site serves as it is (docs/KITWORK_UI.md mục 5), a route,
// and the files no request may read: its sources, its .env, a note at its root, a dot file in assets/.
// hashed is the name of the file whose name carries the start of its bytes' SHA-256.
func staticSite(t *testing.T, website string) (folder, hashed string) {
	t.Helper()
	script := "console.log(1)\n"
	sum := sha256.Sum256([]byte(script))
	hashed = "app." + hex.EncodeToString(sum[:])[:16] + ".js"
	site := "apps/shop/shop.test/"
	folder = write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website(` + website + `);
`,
		"apps/shop/.env":                           "SECRET_OF_THE_APP=1\n",
		site + "router.work":                       routerImport + "// SECRET-ROUTER\nrouter.get((context) => context.text(\"home\"));\n",
		site + ".env":                              "SECRET_OF_THE_SITE=1\n",
		site + "notes.txt":                         "SECRET-NOTES\n",
		site + "content.ui":                        "<text>SECRET-UI</text>\n",
		site + "robots.txt":                        "User-agent: *\n",
		site + ".well-known/security.txt":          "Contact: mailto:security@shop.test\n",
		site + "assets/logo.svg":                   "<svg xmlns=\"http://www.w3.org/2000/svg\"/>",
		site + "assets/" + hashed:                  script,
		site + "assets/app.0123456789abcdef.js":    "console.log(2)\n",
		site + "assets/logo.20261014.svg":          "<svg/>",
		site + "assets/data.kitworktest":           "bytes",
		site + "assets/x.work":                     "// SECRET-WORK\n",
		site + "assets/x.ui":                       "<text>SECRET-UI-IN-ASSETS</text>\n",
		site + "assets/.hidden":                    "SECRET-HIDDEN\n",
		site + "assets/page.kitwork.html":          "SECRET-OLD-PAGE\n",
		site + "assets/sub/inside.txt":             "inside\n",
		site + "core/x.work":                       "// SECRET-SHARED\n",
		site + "assets/.well-known/security.txt":   "SECRET-NOT-THE-ROOT\n",
		site + "users/router.work":                 routerImport + "router.get((context) => context.text(\"users\"));\n",
		site + "users/assets/logo.svg":             "SECRET-A-BRANCH\n",
		site + "assets/nested/deeper/picture.webp": "webp",
	})
	return folder, hashed
}

// A site serves its assets/ at /assets/…, the catalogue of root files and .well-known/, from disk with no
// VM: each with its type, nosniff, an ETag and Last-Modified that make a second load a 304, Range, HEAD, and
// a year of cache only for a name that carries the hash of its bytes. Its routes still answer every other
// path. Until 05/11/2026 engine2 served no file: /assets/logo.svg was a 404 on every site.
func TestASiteServesItsFilesAsTheyAre(t *testing.T) {
	folder, hashed := staticSite(t, `"shop.test"`)
	b := boot(t, filepath.Join(folder, "application.work"))

	served := []struct {
		path, body, kind, cache string
	}{
		{"/assets/logo.svg", "<svg xmlns=\"http://www.w3.org/2000/svg\"/>", "image/svg+xml", "public, max-age=3600"},
		{"/assets/" + hashed, "console.log(1)\n", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		// a name that looks hashed but whose bytes say otherwise is kept an hour, and so is a date
		{"/assets/app.0123456789abcdef.js", "console.log(2)\n", "text/javascript; charset=utf-8", "public, max-age=3600"},
		{"/assets/logo.20261014.svg", "<svg/>", "image/svg+xml", "public, max-age=3600"},
		{"/assets/data.kitworktest", "bytes", "application/octet-stream", "public, max-age=3600"},
		{"/assets/sub/inside.txt", "inside\n", "text/plain; charset=utf-8", "public, max-age=3600"},
		{"/assets/nested/deeper/picture.webp", "webp", "image/webp", "public, max-age=3600"},
		{"/robots.txt", "User-agent: *\n", "text/plain; charset=utf-8", "public, max-age=3600"},
		{"/.well-known/security.txt", "Contact: mailto:security@shop.test\n", "text/plain; charset=utf-8", "public, max-age=3600"},
	}
	for _, want := range served {
		got := b.get(t, "shop.test", want.path)
		if got.status != http.StatusOK || got.body != want.body {
			t.Errorf("GET %s: %d %q, want 200 %q", want.path, got.status, got.body, want.body)
			continue
		}
		if kind := got.header.Get("Content-Type"); kind != want.kind {
			t.Errorf("GET %s: Content-Type %q, want %q", want.path, kind, want.kind)
		}
		if cache := got.header.Get("Cache-Control"); cache != want.cache {
			t.Errorf("GET %s: Cache-Control %q, want %q", want.path, cache, want.cache)
		}
		if got.header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(got.header.Get("ETag"), `W/"`) || got.header.Get("Last-Modified") == "" {
			t.Errorf("GET %s: nosniff %q, ETag %q, Last-Modified %q", want.path, got.header.Get("X-Content-Type-Options"), got.header.Get("ETag"), got.header.Get("Last-Modified"))
		}
	}

	first := b.get(t, "shop.test", "/assets/logo.svg")
	if again := b.request(t, http.MethodGet, "shop.test", "/assets/logo.svg", nil, "If-None-Match", first.header.Get("ETag")); again.status != http.StatusNotModified || again.body != "" {
		t.Errorf("GET again with its ETag: %d %q, want 304 and no bytes", again.status, again.body)
	}
	if part := b.request(t, http.MethodGet, "shop.test", "/assets/logo.svg", nil, "Range", "bytes=0-3"); part.status != http.StatusPartialContent || part.body != "<svg" {
		t.Errorf("GET a range: %d %q, want 206 \"<svg\"", part.status, part.body)
	}
	if head := b.request(t, http.MethodHead, "shop.test", "/assets/logo.svg", nil); head.status != http.StatusOK || head.body != "" || head.header.Get("Content-Type") != "image/svg+xml" {
		t.Errorf("HEAD: %d %q %q, want 200, no body, image/svg+xml", head.status, head.body, head.header.Get("Content-Type"))
	}
	if home := b.get(t, "shop.test", "/"); home.status != http.StatusOK || home.body != "home" {
		t.Errorf("GET /: %d %q, want the route's 200 \"home\"", home.status, home.body)
	}
	if users := b.get(t, "shop.test", "/users"); users.status != http.StatusOK || users.body != "users" {
		t.Errorf("GET /users: %d %q, want the route's 200 \"users\"", users.status, users.body)
	}
	// a write is never a file's: it goes to the routes, which have no door there
	if post := b.request(t, http.MethodPost, "shop.test", "/assets/logo.svg", nil, "Sec-Fetch-Site", "same-origin"); post.status == http.StatusOK {
		t.Errorf("POST /assets/logo.svg: %d %q, want the routes' answer, not the file", post.status, post.body)
	}
}

// With allowLocal, a file whose name carries no hash is asked again at every load (no-cache), so an edit
// shows at once; a hashed name keeps its year (KITWORK_UI.md mục 5).
func TestAllowLocalAsksAgainForAFileAtEveryLoad(t *testing.T) {
	folder, hashed := staticSite(t, `{ hostname: "shop.test", allowLocal: true }`)
	b := boot(t, filepath.Join(folder, "application.work"))
	if cache := b.get(t, "shop.test", "/assets/logo.svg").header.Get("Cache-Control"); cache != "no-cache" {
		t.Errorf("allowLocal, /assets/logo.svg: Cache-Control %q, want no-cache", cache)
	}
	if cache := b.get(t, "shop.test", "/assets/"+hashed).header.Get("Cache-Control"); cache != "public, max-age=31536000, immutable" {
		t.Errorf("allowLocal, a hashed name: Cache-Control %q, want immutable", cache)
	}
}

// No request reads a file outside the allowlist, a source of the site wherever it lies, a dot file, or a
// file through a spelling the file system reads as another name: .., an encoded ..; on Windows a stream
// (::$DATA), a final dot or space (router.work. opens router.work) and a short name (X~1.WOR). Each such
// path goes to the routes, which answer it without the file's bytes.
func TestASiteNeverServesASourceOrAFileOutsideItsAllowlist(t *testing.T) {
	folder, _ := staticSite(t, `"shop.test"`)
	b := boot(t, filepath.Join(folder, "application.work"))
	refused := []string{
		"/router.work", "/.env", "/notes.txt", "/content.ui", "/core/x.work",
		"/assets/x.work", "/assets/X.WORK", "/assets/x.ui", "/assets/.hidden", "/assets/page.kitwork.html",
		"/assets/.well-known/security.txt", "/users/assets/logo.svg",
		"/assets/../router.work", "/assets/%2e%2e/router.work", "/assets/..%2frouter.work", "/assets/../../.env",
		"/.well-known/../.env", "/assets%5c..%5crouter.work", "/assets/logo.svg::$DATA", "/assets/x.work::$DATA",
		"/assets/x.work.", "/assets/x.work%20", "/assets/x.work...", "/assets/X~1.WOR", "/assets/sub~1/inside.txt",
		"/assets/", "/assets", "/assets/sub", "/assets/sub/", "/assets//logo.svg", "/assets/./logo.svg",
		"/ROBOTS.TXT", "/Assets/logo.svg", "/assets/logo.svg%00",
	}
	for _, target := range refused {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://shop.test"+target, nil)
		b.host.ServeHTTP(recorder, request)
		body := recorder.Body.String()
		if recorder.Code == http.StatusOK || strings.Contains(body, "SECRET") || strings.Contains(body, "svg") {
			t.Errorf("GET %s: %d %q, want the routes' answer without the file", target, recorder.Code, body)
		}
	}
}

// A link in assets/ that points out of the site's folder is not followed: os.OpenInRoot refuses it, so a
// file of the app (its .env) never goes out under a name of assets/. Skipped where the system lets no
// test make a link (Windows without the right).
func TestALinkOutOfTheSiteIsNotFollowed(t *testing.T) {
	folder, _ := staticSite(t, `"shop.test"`)
	outside := filepath.Join(folder, "apps", "shop", ".env")
	link := filepath.Join(folder, "apps", "shop", "shop.test", "assets", "out.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no link made: %v", err)
	}
	b := boot(t, filepath.Join(folder, "application.work"))
	if got := b.get(t, "shop.test", "/assets/out.txt"); got.status == http.StatusOK || strings.Contains(got.body, "SECRET") {
		t.Errorf("GET /assets/out.txt, a link to the app's .env: %d %q, want no file", got.status, got.body)
	}
}

// A junction in assets/ to a folder out of the site (Windows makes one with no right, unlike a link) is not
// followed either: /assets/out/secret.txt does not read the app's file.
func TestAJunctionOutOfTheSiteIsNotFollowed(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a junction is Windows'")
	}
	folder, _ := staticSite(t, `"shop.test"`)
	app := filepath.Join(folder, "apps", "shop")
	if err := os.WriteFile(filepath.Join(app, "secret.txt"), []byte("SECRET-OF-THE-APP\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(app, "shop.test", "assets", "out")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, app).CombinedOutput(); err != nil {
		t.Skipf("no junction made: %v %s", err, output)
	}
	t.Cleanup(func() { os.Remove(junction) }) // the junction alone, never what it points at
	b := boot(t, filepath.Join(folder, "application.work"))
	if got := b.get(t, "shop.test", "/assets/out/secret.txt"); got.status == http.StatusOK || strings.Contains(got.body, "SECRET") {
		t.Errorf("GET /assets/out/secret.txt through a junction to the app: %d %q, want no file", got.status, got.body)
	}
}
