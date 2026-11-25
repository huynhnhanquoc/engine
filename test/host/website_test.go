package host_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/host"
)

// Cửa website, lát 1 (host/website.go, host/request.go; 24/11/2026): chuỗi guard của host cho mỗi request, câu trả lời
// nó quyết định, và các hàm sau khi gửi. Mỗi luật một ca: tắt luật thì ca ấy đỏ.

// lines là một io.Writer giữ chữ được viết, an toàn khi nhiều request viết cùng lúc.
type lines struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *lines) take() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.b.String()
	l.b.Reset()
	return s
}

// door boot source (một application.work trong một thư mục tạm, hay folder) và nạp cửa website của nó; console là chữ
// console.log viết, log là chữ engine viết. Các file database lần boot mở được đóng khi test xong.
func door(t testing.TB, folder, source string) (w *host.Website, console, log *lines) {
	t.Helper()
	if folder == "" {
		folder = t.TempDir()
	}
	console, log = &lines{}, &lines{}
	booted, err := host.Program{File: "application.work", Source: source, Folder: folder, Console: console,
		System: func(string) (string, bool) { return "", false }}.Boot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { booted.Close() })
	w, err = host.LoadWebsite(booted)
	if err != nil {
		t.Fatal(err)
	}
	w.Log = log
	return w, console, log
}

// serve là một request tới w: target là URL đầy đủ (https:// là đến qua TLS), headers từng cặp tên, giá trị.
func serve(w http.Handler, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	r.RemoteAddr = "203.0.113.7:5555"
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "RemoteAddr" {
			r.RemoteAddr = headers[i+1]
			continue
		}
		r.Header.Add(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	w.ServeHTTP(rec, r)
	return rec
}

// blueprintDoor là cửa website của bản thiết kế docs/host/blueprint: một bản chép của testdata/blueprint, có các file
// database của nó (blueprintFolder).
func blueprintDoor(t testing.TB) (*host.Website, *lines, *lines) {
	t.Helper()
	folder := blueprintFolder(t)
	source, err := os.ReadFile(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	return door(t, folder, string(source))
}

// Mốc nghiệm thu của lát 1: năm guard của application.work trong docs/host/blueprint trả lời request thật, đúng như
// file viết, và các hàm error, finally của nó in đúng dòng của chúng.
func TestTheBlueprintsGuardsAnswer(t *testing.T) {
	w, console, log := blueprintDoor(t)
	for _, want := range []string{"certificates", "ssl", "concurrency"} {
		if !strings.Contains(strings.Join(w.Notes, "\n"), "application.website."+want+" is not served") {
			t.Errorf("no note for %s: %q", want, w.Notes)
		}
	}
	const agent = "User-Agent"
	for _, c := range []struct {
		name, target   string
		headers        []string
		status         int
		body, location string
		printed        string
	}{
		{"plain HTTP goes to https", "http://shop.vn/don-hang?page=2", []string{agent, "k6"}, 301, "", "https://shop.vn/don-hang?page=2",
			"[console.log] GET shop.vn/don-hang 301\n"},
		{"a banned IP", "https://shop.vn/", []string{agent, "k6", "RemoteAddr", "198.51.100.9:4000"}, 403, "IP 198.51.100.9 bị chặn: spam", "",
			"[console.log] 403 shop.vn/ IP 198.51.100.9 bị chặn: spam\n[console.log] GET shop.vn/ 403\n"},
		{"a name not registered", "https://la.vn/", []string{agent, "k6"}, 404, "Tên miền la.vn chưa đăng ký", "",
			"[console.log] 404 la.vn/ Tên miền la.vn chưa đăng ký\n[console.log] GET la.vn/ 404\n"},
		{"a name not confirmed", "https://moi.vn/", []string{agent, "k6"}, 404, "Tên miền moi.vn chưa xác nhận", "",
			"[console.log] 404 moi.vn/ Tên miền moi.vn chưa xác nhận\n[console.log] GET moi.vn/ 404\n"},
		{"a paused tenant", "https://cu.vn/", []string{agent, "k6"}, 403, "Tenant cu đang tạm dừng", "",
			"[console.log] 403 cu.vn/ Tenant cu đang tạm dừng\n[console.log] GET cu.vn/ 403\n"},
		{"no User-Agent", "https://shop.vn/", nil, 403, "Thiếu User-Agent", "",
			"[console.log] 403 shop.vn/ Thiếu User-Agent\n[console.log] GET shop.vn/ 403\n"},
		{"a scanner's path", "https://shop.vn/wp-login.php", []string{agent, "k6"}, 404, "Không có /wp-login.php", "",
			"[console.log] 404 shop.vn/wp-login.php Không có /wp-login.php\n[console.log] GET shop.vn/wp-login.php 404\n"},
		{"through every guard, to the site", "https://SHOP.vn:443/", []string{agent, "k6"}, 200, "Xin chào từ shop.vn của tenant 03dt", "",
			"[console.log] GET shop.vn/ 200\n"},
		{"a path the site does not have", "https://shop.vn/gio-hang", []string{agent, "k6"}, 404, "no route for /gio-hang", "",
			"[console.log] 404 shop.vn/gio-hang no route for /gio-hang\n[console.log] GET shop.vn/gio-hang 404\n"},
		{"a file of the site's assets, through every guard (lát 4)", "https://shop.vn/assets/app.css", []string{agent, "k6"}, 200,
			"body { margin: 0; }\n", "", "[console.log] GET shop.vn/assets/app.css 200\n"},
		{"a file of the root's catalogue", "https://shop.vn/robots.txt", []string{agent, "k6"}, 200, "User-agent: *\nAllow: /\n", "",
			"[console.log] GET shop.vn/robots.txt 200\n"},
		{"a file, for a banned IP: the guards come first", "https://shop.vn/assets/app.css",
			[]string{agent, "k6", "RemoteAddr", "198.51.100.9:4000"}, 403, "IP 198.51.100.9 bị chặn: spam", "",
			"[console.log] 403 shop.vn/assets/app.css IP 198.51.100.9 bị chặn: spam\n[console.log] GET shop.vn/assets/app.css 403\n"},
	} {
		rec := serve(w, c.target, c.headers...)
		if rec.Code != c.status || rec.Body.String() != c.body || rec.Header().Get("Location") != c.location {
			t.Errorf("%s: %d %q, Location %q", c.name, rec.Code, rec.Body.String(), rec.Header().Get("Location"))
		}
		if got := console.take(); got != c.printed {
			t.Errorf("%s: printed %q, want %q", c.name, got, c.printed)
		}
	}
	// warm của bản thiết kế chọn shop.vn: hai file tĩnh của nó nằm trong RAM từ request đầu
	if got := log.String(); got != "website: apps/03dt/shop.vn: warm: 2 files in memory (1 KB)\n" {
		t.Errorf("the engine logged %q", got)
	}
}

// Trên một socket thật (127.0.0.1, cổng do máy chọn): Open mở các địa chỉ của app.website(…).listen(…), Serve trả lời, Shutdown đóng.
func TestTheWebsiteListens(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0") // một cổng trống: bản thiết kế không nhận cổng 0
	if err != nil {
		t.Fatal(err)
	}
	address := free.Addr().String()
	free.Close()
	w, _, _ := door(t, "", `import { application } from "application";
return application().website().guard((context) => context.status(418).error("hello " + context.request.hostname))
  .listen("`+address+`").run();`)
	listeners, err := w.Open()
	if err != nil || len(listeners) != 1 {
		t.Fatal(listeners, err)
	}
	served := make(chan error, 1)
	go func() { served <- w.Serve(listeners[0]) }()
	response, err := http.Get("http://" + listeners[0].Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 64)
	n, _ := response.Body.Read(body)
	response.Body.Close()
	if response.StatusCode != 418 || string(body[:n]) != "hello 127.0.0.1" {
		t.Errorf("%d %q", response.StatusCode, body[:n])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve returned %v", err)
	}
}

// guarded là một application.work có các guard ấy, và các hàm sau khi gửi in response của chúng.
func guarded(guards ...string) string {
	var b strings.Builder
	b.WriteString("import { application } from \"application\";\nreturn application().website()")
	for _, g := range guards {
		b.WriteString("\n  .guard(" + g + ")")
	}
	b.WriteString(`
  .success((context) => console.log("success", context.response.status, context.response.error))
  .error((context) => { const { status, error } = context.response; console.log("error", status, error.code, error.message, error.at); })
  .finally((context) => console.log("finally", context.response.status))
  .listen(8080).run();`)
	return b.String()
}

// Request mang đúng các khoá của spec: method, url, path, search, hostname (chữ thường, không cổng), ip, secure,
// header(tên) (null khi không có), query(tên) ("" khi không có), cookies; khoá không có là lỗi nêu các khoá.
func TestTheRequestAGuardReads(t *testing.T) {
	w, _, _ := door(t, "", guarded(`(context) => {
    const r = context.request;
    return context.error(JSON.stringify([r.method, r.url, r.path, r.search, r.hostname, r.ip, r.secure, r.header("X-A"),
      r.header("x-none"), r.header("Host"), r.query("q"), r.query("none"), r.cookies]));
  }`))
	rec := serve(w, "https://Shop.VN:8443/a%20b?q=1&q=2", "X-A", "x", "Cookie", "c=v; d=w", "RemoteAddr", "[::ffff:203.0.113.9]:4000")
	want := `["GET","https://Shop.VN:8443/a%20b?q=1&q=2","/a b","?q=1&q=2","shop.vn","203.0.113.9",true,"x",null,"Shop.VN:8443","1","",{"c":"v","d":"w"}]`
	if rec.Code != 403 || rec.Body.String() != want {
		t.Errorf("%d %s\nwant %s", rec.Code, rec.Body.String(), want)
	}
	rec = serve(w, "http://shop.vn/")
	if !strings.Contains(rec.Body.String(), `"http://shop.vn/","/","","shop.vn","203.0.113.7",false`) {
		t.Errorf("plain HTTP: %s", rec.Body.String())
	}
}

// context.next({ khoá }) đặt khoá vào request cho các guard sau; giá trị là một bản chép đã khoá; khoá đã có, giá trị
// là hàm, đặt một khoá hai lần là lỗi (500, nêu lý do ở log và ở response.error).
func TestNextSetsKeys(t *testing.T) {
	w, console, log := door(t, "", guarded(
		`(context) => context.request.path === "/copy" ? context.next({ row: { name: "a", tags: ["x"], inner: { n: 1 } } }) : context.next()`,
		`(context) => { if (context.request.path !== "/copy") { return context.next(); }
    const { row } = context.request;
    if (context.request.query("write") === "inner") { row.inner.n = 2; }
    if (context.request.query("write") === "tags") { row.tags.push("y"); }
    return context.error(row.name + " " + row.inner.n + " " + row.tags.join(","));
  }`,
		`(context) => context.request.path === "/hostname" ? context.next({ hostname: "la.vn" }) : context.next()`,
		`(context) => context.request.path === "/function" ? context.next({ f: () => 1 }) : context.next()`,
		`(context) => context.request.path === "/twice" ? context.next({ a: 1 }) : context.next()`,
		`(context) => context.request.path === "/twice" ? context.next({ a: 2 }) : context.next()`,
		`(context) => context.error("missing " + context.request.tenant)`,
	))
	for _, c := range []struct {
		target, body, logged, printed string
		status                        int
	}{
		{"https://shop.vn/copy", "a 1 x", "", "error 403 REFUSED a 1 x null\nfinally 403\n", 403},
		{"https://shop.vn/copy?write=inner", "Internal Server Error", `cannot assign to read only property "n"`, "error 500 TYPE", 500},
		{"https://shop.vn/copy?write=tags", "Internal Server Error", "object is not extensible", "error 500 TYPE", 500},
		{"https://shop.vn/hostname", "Internal Server Error", "context.next({ hostname }): the request already has the key hostname", "error 500 TYPE", 500},
		{"https://shop.vn/function", "Internal Server Error", "context.next({ f }): f is a function", "error 500 TYPE", 500},
		{"https://shop.vn/twice", "Internal Server Error", "the request already has the key a", "error 500 TYPE", 500},
		{"https://shop.vn/", "Internal Server Error", `request has no key "tenant"`, "error 500 TYPE", 500},
	} {
		rec := serve(w, c.target)
		logged, printed := log.take(), strings.ReplaceAll(console.take(), "[console.log] ", "")
		if rec.Code != c.status || rec.Body.String() != c.body || !strings.Contains(logged, c.logged) || !strings.HasPrefix(printed, c.printed) {
			t.Errorf("%s: %d %q\nlogged %q\nprinted %q", c.target, rec.Code, rec.Body.String(), logged, printed)
		}
		if c.status == 500 && (!strings.Contains(printed, "application.work:") || !strings.HasSuffix(printed, "finally 500\n")) {
			t.Errorf("%s: response.error.at has no place, or finally did not run: %q", c.target, printed)
		}
	}
}

// Bốn khoá từ ba guard (hai khoá trong một next, rồi mỗi guard một khoá): request giữ cả bốn. Chỗ dựng sẵn cho khoá
// đầu của một next và hai khoá đầu của một request (request.go, 25/11/2026) không làm mất khoá nào khi đầy.
func TestNextSetsManyKeys(t *testing.T) {
	w, _, log := door(t, "", guarded(
		`(context) => context.next({ a: "1", b: "2" })`,
		`(context) => context.next({ c: "3" })`,
		`(context) => context.next({ d: "4" })`,
		`(context) => context.error(context.request.a + context.request.b + context.request.c + context.request.d)`,
	))
	for range 2 {
		if rec := serve(w, "https://shop.vn/"); rec.Code != 403 || rec.Body.String() != "1234" {
			t.Errorf("%d %q, log %q", rec.Code, rec.Body.String(), log.take())
		}
	}
}

// Các quyết định: error là 403, redirect là 302 khi không có status; status(3xx).redirect, status(4xx).error; mã và đối
// số sai là lỗi của guard (500), một dòng mới trong địa chỉ chuyển hướng cũng vậy. Chuyển hướng là thành công (success),
// từ chối là error.
func TestTheDecisions(t *testing.T) {
	w, console, log := door(t, "", guarded(`(context) => {
    const { path } = context.request;
    if (path === "/error") { return context.error("no"); }
    if (path === "/redirect") { return context.redirect("/there"); }
    if (path === "/307") { return context.status(307).redirect("https://la.vn/x"); }
    if (path === "/429") { return context.status(429).error("slow down"); }
    if (path === "/500") { return context.status(500).error("down"); }
    if (path === "/301-error") { return context.status(301).error("no"); }
    if (path === "/404-redirect") { return context.status(404).redirect("/"); }
    if (path === "/empty") { return context.error(""); }
    if (path === "/newline") { return context.redirect("/a\r\nSet-Cookie: x=1"); }
    if (path === "/nothing") { context.next(); return; }
    return context.next();
  }`))
	for _, c := range []struct {
		path, location, body, logged, printed string
		status                                int
	}{
		{"/error", "", "no", "", "error 403 REFUSED no null\nfinally 403\n", 403},
		{"/redirect", "/there", "", "", "success 302 null\nfinally 302\n", 302},
		{"/307", "https://la.vn/x", "", "", "success 307 null\nfinally 307\n", 307},
		{"/429", "", "slow down", "", "error 429 REFUSED slow down null\nfinally 429\n", 429},
		{"/500", "", "Internal Server Error", "context.status(500): a guard answers with 3xx", "error 500 RANGE", 500},
		{"/301-error", "", "Internal Server Error", "a refusal is a 4xx", "error 500 RANGE", 500},
		{"/404-redirect", "", "Internal Server Error", "a redirect is a 3xx", "error 500 RANGE", 500},
		{"/empty", "", "Internal Server Error", "got an empty string", "error 500 TYPE", 500},
		{"/newline", "", "Internal Server Error", "control character", "error 500 TYPE", 500},
		{"/nothing", "", "Internal Server Error", "application.website.guard[0] returned undefined (no return?)", "error 500 TYPE", 500},
	} {
		rec := serve(w, "https://shop.vn"+c.path)
		logged, printed := log.take(), strings.ReplaceAll(console.take(), "[console.log] ", "")
		if rec.Code != c.status || rec.Header().Get("Location") != c.location || rec.Body.String() != c.body ||
			!strings.Contains(logged, c.logged) || !strings.HasPrefix(printed, c.printed) {
			t.Errorf("%s: %d %q Location %q\nlogged %q\nprinted %q", c.path, rec.Code, rec.Body.String(), rec.Header().Get("Location"), logged, printed)
		}
	}
	// một guard không trả quyết định: response.error.at là chỗ của chính guard ấy
	serve(w, "https://shop.vn/nothing")
	if printed := console.take(); !strings.Contains(printed, "application.work:3:") {
		t.Errorf("the place of the guard: %q", printed)
	}
}

// Các hàm sau khi gửi không quyết định, đọc được response; một hàm ném thì engine viết lỗi ấy, các hàm sau vẫn chạy. Một
// guard đọc response là lỗi.
func TestTheFunctionsAfterSending(t *testing.T) {
	w, console, log := door(t, "", `import { application } from "application";
return application().website()
  .guard((context) => context.request.path === "/response" ? context.error(String(context.response)) : context.error("no"))
  .error((context) => context.next())
  .error((context) => console.log("second", context.response.status))
  .finally((context) => console.log("finally", context.response.error.code))
  .listen(8080).run();`)
	rec := serve(w, "https://shop.vn/")
	if !rec.Flushed {
		t.Error("the functions after sending ran before the response was sent")
	}
	if rec.Code != 403 || console.take() != "[console.log] second 403\n[console.log] finally REFUSED\n" ||
		!strings.Contains(log.take(), "application.website.error[0]: application.work:4:") {
		t.Errorf("%d", rec.Code)
	}
	rec = serve(w, "https://shop.vn/response")
	if rec.Code != 500 || !strings.Contains(log.take(), "context.response is read after the response is sent") {
		t.Errorf("a guard read the response: %d", rec.Code)
	}
}

// Mỗi request một lượt chạy có trần: một guard chạy mãi dừng ở ENERGY_LIMIT (500), các hàm sau khi gửi vẫn chạy (lượt
// của chúng), và request sau không mang gì của request trước. Một guard gán vào biến nó bắt (mọi request cùng đọc) là
// lỗi, không phải một cuộc đua.
func TestEachRequestRunsAlone(t *testing.T) {
	w, console, log := door(t, "", `import { application } from "application";
let seen = 0;
return application().website()
  .guard((context) => {
    if (context.request.path === "/forever") { let n = 0; for (let i = 0; i < 1000000000; i++) { n = n + 1; } }
    if (context.request.path === "/count") { seen = seen + 1; }
    return context.error("ok");
  })
  .finally((context) => console.log(context.response.status, context.response.error.code))
  .listen(8080).run();`)
	start := time.Now()
	rec := serve(w, "https://shop.vn/forever")
	if rec.Code != 500 || console.take() != "[console.log] 500 ENERGY_LIMIT\n" || !strings.Contains(log.take(), "the run used all its energy") {
		t.Errorf("forever: %d", rec.Code)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the energy of a request stopped it after %v", elapsed)
	}
	if rec := serve(w, "https://shop.vn/"); rec.Code != 403 || console.take() != "[console.log] 403 REFUSED\n" {
		t.Errorf("the next request: %d", rec.Code)
	}
	if rec := serve(w, "https://shop.vn/count"); rec.Code != 500 || !strings.Contains(log.take(), "cannot assign to a variable this function holds") {
		t.Errorf("a write to a captured variable: %d", rec.Code)
	}
}

// Nhiều request cùng lúc qua cùng các guard của bản thiết kế (go test -race): mỗi request đúng câu trả lời của nó.
func TestRequestsAtOnce(t *testing.T) {
	w, _, _ := blueprintDoor(t)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				target, status := "https://shop.vn/", 200
				switch (g + i) % 3 {
				case 1:
					target, status = "https://la.vn/", 404
				case 2:
					target, status = "http://shop.vn/", 301
				}
				if rec := serve(w, target, "User-Agent", "k6"); rec.Code != status {
					t.Errorf("%s: %d, want %d", target, rec.Code, status)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Nạp: mỗi trường sai một lỗi theo đường dẫn của nó; không có website thì không có cửa.
func TestLoadingTheWebsite(t *testing.T) {
	boot := func(source string) error {
		booted, err := host.Program{File: "application.work", Source: source, Folder: t.TempDir(),
			System: func(string) (string, bool) { return "", false }}.Boot()
		if err != nil {
			return err
		}
		w, err := host.LoadWebsite(booted)
		if err == nil && w == nil {
			return errors.New("no door")
		}
		return err
	}
	err := boot(`import { application } from "application";
return application().website().guard(5).finally("x").listen("la.vn:80").run();`)
	for _, want := range []string{
		`application.website.listen[0]: "la.vn" is not an IP`,
		"application.website.guard[0]: a function (context) => …, got number",
		"application.website.finally[0]: a function (context) => …, got string",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if err := boot(`import { application } from "application"; return application({ memory: "1gb" }).run();`); err == nil || err.Error() != "no door" {
		t.Errorf("an application without a website: %v", err)
	}
}
