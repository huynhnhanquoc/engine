package host_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	shell "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/host"
)

// File tĩnh của một site (host/static.go; 24/11/2026, lát 4): assets/, .well-known/ và danh mục file ở gốc, nguyên
// dạng, sau chuỗi của host và các luật của thư mục phủ chúng; không file nào khác của site ra ngoài. warm giữ chúng
// trong RAM. Mỗi luật một ca: tắt luật thì ca ấy đỏ.

// staticApplication là application.work của các ca: guard chặn header X-Block, đặt user từ X-User; warm là WARM.
const staticApplication = `import { application } from "application";
return application().website()
  .guard((context) => context.request.header("X-Block") === "yes" ? context.status(403).error("blocked") :
    context.next({ user: context.request.header("X-User") ?? "" }))
  .routes("apps/a/shop.vn")
  .warm(WARM)
  .finally((context) => console.log(context.response.status, context.response.error === null ? "" : context.response.error.code))
  .listen(8080).run();`

const homeRoute = `import { router } from "router";
const route = router();
route.get((context) => context.text("home"));
return route;`

// staticScript là file có tên mang mã băm của nó; hashedName là tên ấy.
const staticScript = "console.log(1)\n"

func hashedName() string {
	sum := sha256.Sum256([]byte(staticScript))
	return "app." + hex.EncodeToString(sum[:])[:16] + ".js"
}

// staticFiles là một site có mỗi loại file nó phục vụ nguyên dạng, và các file không request nào đọc được: nguồn của
// nó, .env, một ghi chú ở gốc, một file chấm trong assets/.
func staticFiles(warm string) map[string]string {
	site := "apps/a/shop.vn/"
	return map[string]string{
		"application.work":                         strings.Replace(staticApplication, "WARM", warm, 1),
		site + "router.work":                       homeRoute + "\n// SECRET-ROUTER\n",
		site + ".env":                              "SECRET_OF_THE_SITE=1\n",
		site + "notes.txt":                         "SECRET-NOTES\n",
		site + "content.ui":                        "<text>SECRET-UI</text>\n",
		site + "robots.txt":                        "User-agent: *\n",
		site + ".well-known/security.txt":          "Contact: mailto:security@shop.vn\n",
		site + "assets/logo.svg":                   `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		site + "assets/" + hashedName():            staticScript,
		site + "assets/app.0123456789abcdef.js":    "console.log(2)\n",
		site + "assets/logo.20261014.svg":          "<svg/>",
		site + "assets/data.kitworktest":           "bytes",
		site + "assets/x.work":                     "// SECRET-WORK\n",
		site + "assets/x.ui":                       "<text>SECRET-UI-IN-ASSETS</text>\n",
		site + "assets/.hidden":                    "SECRET-HIDDEN\n",
		site + "assets/page.kitwork.html":          "SECRET-OLD-PAGE\n",
		site + "assets/sub/inside.txt":             "inside\n",
		site + "assets/nested/deeper/picture.webp": "webp",
		site + "assets/.well-known/security.txt":   "SECRET-NOT-THE-ROOT\n",
		site + "core/x.work":                       "// SECRET-SHARED\n",
		site + "users/router.work":                 homeRoute,
		site + "users/assets/logo.svg":             "SECRET-A-BRANCH\n",
	}
}

// get là một request tới shop.vn với các header ấy (tên, giá trị, …).
func get(w http.Handler, method, path string, headers ...string) *http.Response {
	r := newRequest(method, "https://shop.vn"+path)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return record(w, r).Result()
}

func body(res *http.Response) string {
	var b strings.Builder
	buffer := make([]byte, 512)
	for {
		n, err := res.Body.Read(buffer)
		b.Write(buffer[:n])
		if err != nil {
			return b.String()
		}
	}
}

// TestASiteServesItsFilesAsTheyAre: mỗi file với loại của nó, nosniff, ETag; lần tải thứ hai là 304 không có byte;
// Range; HEAD; một năm cache chỉ cho tên mang mã băm của byte của nó. Như nhau khi site giữ ấm và khi không.
func TestASiteServesItsFilesAsTheyAre(t *testing.T) {
	for _, warm := range []string{"false", "true"} {
		t.Run("warm "+warm, func(t *testing.T) {
			w, console, _ := site(t, staticFiles(warm))
			for _, c := range []struct {
				path, body, kind, cache string
			}{
				{"/assets/logo.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`, "image/svg+xml", "public, max-age=3600"},
				{"/assets/" + hashedName(), staticScript, "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
				{"/assets/app.0123456789abcdef.js", "console.log(2)\n", "text/javascript; charset=utf-8", "public, max-age=3600"},
				{"/assets/logo.20261014.svg", "<svg/>", "image/svg+xml", "public, max-age=3600"},
				{"/assets/data.kitworktest", "bytes", "application/octet-stream", "public, max-age=3600"},
				{"/assets/sub/inside.txt", "inside\n", "text/plain; charset=utf-8", "public, max-age=3600"},
				{"/assets/nested/deeper/picture.webp", "webp", "image/webp", "public, max-age=3600"},
				{"/robots.txt", "User-agent: *\n", "text/plain; charset=utf-8", "public, max-age=3600"},
				{"/.well-known/security.txt", "Contact: mailto:security@shop.vn\n", "text/plain; charset=utf-8", "public, max-age=3600"},
			} {
				res := get(w, "GET", c.path)
				if got := body(res); res.StatusCode != 200 || got != c.body {
					t.Fatalf("%s: %d %q", c.path, res.StatusCode, got)
				}
				h := res.Header
				if h.Get("Content-Type") != c.kind || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != c.cache {
					t.Errorf("%s: Content-Type %q, nosniff %q, Cache-Control %q", c.path, h.Get("Content-Type"),
						h.Get("X-Content-Type-Options"), h.Get("Cache-Control"))
				}
				etag := h.Get("ETag")
				if !strings.HasPrefix(etag, `W/"`) || h.Get("Last-Modified") == "" {
					t.Errorf("%s: ETag %q, Last-Modified %q", c.path, etag, h.Get("Last-Modified"))
				}
				if again := get(w, "GET", c.path, "If-None-Match", etag); again.StatusCode != 304 || body(again) != "" {
					t.Errorf("%s: a second load with its ETag: %d", c.path, again.StatusCode)
				}
			}
			if got := console.take(); strings.Count(got, "[console.log] 200 \n") != 9 || strings.Count(got, "[console.log] 304 \n") != 9 {
				t.Errorf("finally read %q", got)
			}
			part := get(w, "GET", "/assets/logo.svg", "Range", "bytes=1-3")
			if got := body(part); part.StatusCode != 206 || got != "svg" {
				t.Errorf("a range: %d %q", part.StatusCode, got)
			}
			head := get(w, "HEAD", "/assets/logo.svg")
			if got := body(head); head.StatusCode != 200 || got != "" || head.Header.Get("Content-Type") != "image/svg+xml" {
				t.Errorf("HEAD: %d %q %q", head.StatusCode, got, head.Header.Get("Content-Type"))
			}
			if got := console.take(); got != "[console.log] 206 \n[console.log] 200 \n" {
				t.Errorf("finally of the range and the HEAD: %q", got)
			}
		})
	}
}

// TestASiteNeverServesASourceOrAFileOutsideItsAllowlist: không file nguồn, file chấm, file ngoài danh sách nào ra
// ngoài, dù viết đường dẫn thế nào; các cửa của site trả lời các đường ấy (404).
func TestASiteNeverServesASourceOrAFileOutsideItsAllowlist(t *testing.T) {
	for _, warm := range []string{"false", "true"} {
		w, _, _ := site(t, staticFiles(warm))
		for _, path := range []string{
			"/router.work", "/.env", "/notes.txt", "/content.ui", "/core/x.work",
			"/assets/x.work", "/assets/X.WORK", "/assets/x.ui", "/assets/.hidden", "/assets/page.kitwork.html",
			"/assets/.well-known/security.txt", "/users/assets/logo.svg", "/assets/../router.work", "/assets/./x.work",
			"/assets/x.work.", "/assets/x.work%20", "/assets/x.work::$DATA", "/assets/X~1.WOR", "/ROUTER~1.WOR",
			"/assets//logo.svg", "/assets/sub/../../router.work", "/assets\\x.work", "/assets/sub\\..\\..\\.env",
		} {
			res := get(w, "GET", path)
			if got := body(res); strings.Contains(got, "SECRET") || res.StatusCode == 200 {
				t.Errorf("warm %s, %s: %d %q", warm, path, res.StatusCode, got)
			}
		}
	}
}

// TestALinkOutOfTheSiteIsNotFollowed: một link trong assets/ trỏ ra ngoài thư mục site không được theo.
func TestALinkOutOfTheSiteIsNotFollowed(t *testing.T) {
	files := staticFiles("false")
	files["apps/a/outside.txt"] = "SECRET-OUTSIDE\n"
	folder := t.TempDir()
	for name, source := range files {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(folder, "apps", "a", "shop.vn", "assets", "out.txt")
	if err := os.Symlink(filepath.Join(folder, "apps", "a", "outside.txt"), link); err != nil {
		t.Skipf("no symbolic link on this machine: %v", err)
	}
	w, _, _ := door(t, folder, files["application.work"])
	if res := get(w, "GET", "/assets/out.txt"); strings.Contains(body(res), "SECRET") {
		t.Fatalf("a link out of the site was followed: %d", res.StatusCode)
	}
}

// TestAFileGoesThroughTheHostChainFirst: guard của host chạy trước file tĩnh như trước mọi request.
func TestAFileGoesThroughTheHostChainFirst(t *testing.T) {
	w, console, _ := site(t, staticFiles("true"))
	res := get(w, "GET", "/assets/logo.svg", "X-Block", "yes")
	if got := body(res); res.StatusCode != 403 || got != "blocked" {
		t.Fatalf("a blocked request for a file: %d %q", res.StatusCode, got)
	}
	if got := console.take(); got != "[console.log] 403 REFUSED\n" {
		t.Errorf("finally: %q", got)
	}
}

// TestAFileNotThereGoesToTheDoors: file không có, hay một method không phải GET, HEAD, đi tới các cửa của site.
func TestAFileNotThereGoesToTheDoors(t *testing.T) {
	files := staticFiles("true")
	files["apps/a/shop.vn/assets/router.work"] = `import { router } from "router";
const route = router();
route.post((context) => context.text("posted"));
return route;`
	w, _, _ := site(t, files)
	for _, c := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/assets/none.svg", 404, "no route for /assets/none.svg"},
		{"GET", "/favicon.ico", 404, "no route for /favicon.ico"},
		{"POST", "/assets/logo.svg", 404, "no route for /assets/logo.svg"},
		{"POST", "/assets", 200, "posted"},
		{"GET", "/", 200, "home"},
	} {
		res := get(w, c.method, c.path)
		if got := body(res); res.StatusCode != c.status || got != c.body {
			t.Errorf("%s %s: %d %q", c.method, c.path, res.StatusCode, got)
		}
	}
}

// TestFilesFollowTheRulesOfTheirFolders: all(…) của các thư mục cha phủ file của mọi thư mục dưới nó; methods(…) của
// một thư mục chỉ phủ file của chính thư mục ấy; thư mục không biên dịch ("_…") không có luật riêng, all của cha vẫn phủ.
func TestFilesFollowTheRulesOfTheirFolders(t *testing.T) {
	for _, warm := range []string{"false", "true"} {
		files := staticFiles(warm)
		files["apps/a/core/session.work"] = session
		files["apps/a/shop.vn/router.work"] = `import { router } from "router";
import { signedIn } from "app/core/session.work";
const route = router();
route.guard(signedIn).all("get");
route.get((context) => context.text("home"));
return route;`
		files["apps/a/shop.vn/assets/router.work"] = `import { router } from "router";
import { staff } from "app/core/session.work";
const route = router();
route.guard(staff).methods("*");
return route;`
		files["apps/a/shop.vn/assets/_drafts/plan.txt"] = "plan\n"
		w, _, _ := site(t, files)
		for _, c := range []struct {
			path, user string
			status     int
		}{
			{"/robots.txt", "", 401},              // all của gốc
			{"/robots.txt", "an", 200},            //
			{"/assets/logo.svg", "an", 403},       // methods của assets/
			{"/assets/logo.svg", "staff", 200},    //
			{"/assets/sub/inside.txt", "", 401},   // all của gốc đi xuống
			{"/assets/sub/inside.txt", "an", 200}, // methods của assets/ không xuống assets/sub/
			{"/assets/_drafts/plan.txt", "", 401},
			{"/assets/_drafts/plan.txt", "an", 200}, // _drafts không có luật riêng; methods của assets/ không phủ nó
		} {
			var headers []string
			if c.user != "" {
				headers = []string{"X-User", c.user}
			}
			if res := get(w, "GET", c.path, headers...); res.StatusCode != c.status {
				t.Errorf("warm %s, %s as %q: %d, want %d", warm, c.path, c.user, res.StatusCode, c.status)
			}
		}
	}
}

// staticDoor là site của staticFiles(warm) với StaticCheck là every, và thư mục của nó.
func staticDoor(t *testing.T, warm string, every time.Duration) (*host.Website, string, *lines) {
	t.Helper()
	files := staticFiles(warm)
	folder := t.TempDir()
	for name, source := range files {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w, _, log := door(t, folder, files["application.work"])
	w.StaticCheck = every
	return w, filepath.Join(folder, "apps", "a", "shop.vn"), log
}

// TestWarmKeepsFilesInMemory: một site giữ ấm phục vụ file từ RAM: sửa trên đĩa không tới khách trước lần so với đĩa
// (StaticCheck), tới sau nó; file xoá thì đi tới các cửa. Site không giữ ấm đọc đĩa ở mỗi request.
func TestWarmKeepsFilesInMemory(t *testing.T) {
	logo := `<svg xmlns="http://www.w3.org/2000/svg"/>`
	edited := `<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`

	w, dir, log := staticDoor(t, "true", time.Hour)
	if got := body(get(w, "GET", "/assets/logo.svg")); got != logo {
		t.Fatalf("first: %q", got)
	}
	if got := log.String(); !strings.Contains(got, "website: apps/a/shop.vn: warm: 9 files in memory") {
		t.Fatalf("log: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.svg"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := body(get(w, "GET", "/assets/logo.svg")); got != logo {
		t.Fatalf("a warm site read the disk before its check: %q", got)
	}

	w, dir, _ = staticDoor(t, "true", time.Nanosecond)
	get(w, "GET", "/assets/logo.svg")
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.svg"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if got := body(get(w, "GET", "/assets/logo.svg")); got != edited {
		t.Fatalf("after its check, the edited file: %q", got)
	}
	if err := os.Remove(filepath.Join(dir, "assets", "logo.svg")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if res := get(w, "GET", "/assets/logo.svg"); res.StatusCode != 404 {
		t.Fatalf("a file removed: %d %q", res.StatusCode, body(res))
	}

	w, dir, log = staticDoor(t, "false", time.Hour)
	get(w, "GET", "/assets/logo.svg")
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.svg"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := body(get(w, "GET", "/assets/logo.svg")); got != edited {
		t.Fatalf("a site not warm: %q", got)
	}
	if strings.Contains(log.String(), "warm") {
		t.Errorf("a site not warm logged %q", log.String())
	}
	// the file read from the disk was closed once sent
	if heldOpen(filepath.Join(dir, "assets", "logo.svg")) {
		t.Fatal("the file sent is still open")
	}
}

// TestAFileRefusedIsClosed: a file a rule of its folder refuses, opened from the disk, is closed all the same.
func TestAFileRefusedIsClosed(t *testing.T) {
	files := staticFiles("false")
	files["apps/a/core/session.work"] = session
	files["apps/a/shop.vn/router.work"] = `import { router } from "router";
import { signedIn } from "app/core/session.work";
const route = router();
route.guard(signedIn).all("*");
route.get((context) => context.text("home"));
return route;`
	folder := t.TempDir()
	for name, source := range files {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w, _, _ := door(t, folder, files["application.work"])
	if res := get(w, "GET", "/robots.txt"); res.StatusCode != 401 {
		t.Fatalf("robots.txt, not signed in: %d", res.StatusCode)
	}
	if heldOpen(filepath.Join(folder, "apps", "a", "shop.vn", "robots.txt")) {
		t.Fatal("the file refused is still open")
	}
}

// TestWarmHoldsSmallFilesOnly: một file quá 256 KB không nằm trong RAM: đọc từ đĩa ở mỗi request.
func TestWarmHoldsSmallFilesOnly(t *testing.T) {
	w, dir, log := staticDoor(t, "true", time.Hour)
	big := strings.Repeat("a", 300<<10)
	if err := os.WriteFile(filepath.Join(dir, "assets", "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := body(get(w, "GET", "/assets/big.txt")); got != big {
		t.Fatalf("big: %d bytes", len(got))
	}
	if !strings.Contains(log.String(), "warm: 9 files in memory") {
		t.Fatalf("log: %q", log.String())
	}
	edited := strings.Repeat("b", 300<<10)
	if err := os.WriteFile(filepath.Join(dir, "assets", "big.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := body(get(w, "GET", "/assets/big.txt")); got != edited {
		t.Fatal("a file past 256 KB was kept in memory")
	}
}

// TestWarmChoosesTheSites: warm là một hàm nhận context.site; ném lỗi hay trả thứ khác boolean thì engine nói lý do và
// site không giữ ấm (vẫn phục vụ từ đĩa); một giá trị không phải true, false hay hàm là lỗi lúc nạp.
func TestWarmChoosesTheSites(t *testing.T) {
	for _, c := range []struct {
		warm, log string
	}{
		{`(context) => context.site.hostname === "shop.vn" && context.site.folder === "apps/a/shop.vn"`, "warm: 9 files in memory"},
		{`(context) => context.site.hostname === "la.vn"`, ""},
		{`(context) => context.request.hostname === "shop.vn"`, `context has no key "request": warm runs as a site is compiled`},
		{`(context) => "yes"`, `application.website.warm returned "yes", not true or false`},
	} {
		w, _, log := site(t, staticFiles(c.warm))
		if res := get(w, "GET", "/assets/logo.svg"); res.StatusCode != 200 {
			t.Errorf("%s: %d", c.warm, res.StatusCode)
		}
		got := log.String()
		if c.log == "" && got != "" || c.log != "" && !strings.Contains(got, c.log) {
			t.Errorf("%s: logged %q, want %q", c.warm, got, c.log)
		}
	}
	booted, err := host.Program{File: "application.work", Source: strings.Replace(staticApplication, "WARM", "5", 1),
		Folder: t.TempDir(), System: func(string) (string, bool) { return "", false }}.Boot()
	if err != nil {
		t.Fatal(err)
	}
	defer booted.Close()
	if _, err := host.LoadWebsite(booted); err == nil || !strings.Contains(err.Error(), "application.website.warm: true, false, or (context) => boolean, got") {
		t.Fatalf("warm(5): %v", err)
	}
}

// TestFilesAtOnce: nhiều request cùng tới các file của một site giữ ấm và một site không giữ ấm (-race).
func TestFilesAtOnce(t *testing.T) {
	for _, warm := range []string{"false", "true"} {
		w, _, _ := staticDoor(t, warm, time.Nanosecond)
		var group sync.WaitGroup
		failures := make(chan string, 64)
		for g := range 16 {
			group.Go(func() {
				for i := range 50 {
					path := []string{"/assets/logo.svg", "/robots.txt", "/assets/sub/inside.txt"}[(g+i)%3]
					if res := get(w, "GET", path); res.StatusCode != 200 {
						failures <- path
						return
					}
				}
			})
		}
		group.Wait()
		close(failures)
		for path := range failures {
			t.Errorf("warm %s: %s failed", warm, path)
		}
	}
}

// TestAJunctionOutOfTheSiteIsNotFollowed: một junction của Windows trong assets/ trỏ ra thư mục app không được theo,
// cả khi site giữ ấm (warm không đi qua nó).
func TestAJunctionOutOfTheSiteIsNotFollowed(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a junction is Windows'")
	}
	for _, warm := range []string{"false", "true"} {
		files := staticFiles(warm)
		files["apps/a/secret.txt"] = "SECRET-OF-THE-APP\n"
		folder := t.TempDir()
		for name, source := range files {
			path := filepath.Join(folder, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		app := filepath.Join(folder, "apps", "a")
		junction := filepath.Join(app, "shop.vn", "assets", "out")
		if output, err := shell.Command("cmd", "/c", "mklink", "/J", junction, app).CombinedOutput(); err != nil {
			t.Skipf("no junction made: %v %s", err, output)
		}
		t.Cleanup(func() { os.Remove(junction) }) // the junction alone, never what it points at
		w, _, _ := door(t, folder, files["application.work"])
		if res := get(w, "GET", "/assets/out/secret.txt"); res.StatusCode == 200 || strings.Contains(body(res), "SECRET") {
			t.Errorf("warm %s: a junction to the app was followed: %d", warm, res.StatusCode)
		}
	}
}
