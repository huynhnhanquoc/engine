package host_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kitwork/engine2/library/ui/kernel"
)

// warningsSite is a site whose frame and home warn: an image without alt in view.ui, every page's, and one in a for
// on the home, drawn for each row.
func warningsSite(t *testing.T, website string) *booted {
	t.Helper()
	site := "apps/shop/shop.test/"
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website(` + website + `);
`,
		site + "view.ui":          "<view>\n<image value=\"/logo.svg\" />\n<content />\n</view>\n",
		site + "content.ui":       "<content>\n<list><item for=\"x of [1, 2, 3]\"><button><icon value=\"x\" /></button></item></list>\n</content>\n",
		site + "about/content.ui": "<content><text>Về chúng tôi</text></content>\n",
		site + "quiet/view.ui":    "<view><content /></view>\n",
		site + "quiet/content.ui": "<content><text>Yên</text></content>\n",
		site + "assets/logo.svg":  "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"8\" height=\"8\"/>",
	})
	return boot(t, filepath.Join(folder, "application.work"))
}

// warningsScript finds the warnings file a page links.
var warningsScript = regexp.MustCompile(`<script data-kitwork-generated="warnings" src="(/_kitwork/warnings\.[0-9a-f]{16}\.js)" defer></script>`)

// While developing (allowLocal), each warning of the site's pages is said in the terminal once while it runs, not
// once a request, a row or a route that shares the frame it stands in, and each page that has warnings links
// /_kitwork/warnings.<hash>.js, which says its own in the browser's console on every load: a file the engine
// serves, kept by its name. A page with nothing to tell links nothing. CONTROL: in production the same pages
// render the same, nothing is said, nothing is linked, and the file's name is a 404.
func TestAPageWarnsWhileDeveloping(t *testing.T) {
	b := warningsSite(t, `{ hostname: "shop.test", allowLocal: true }`)
	var home string
	for i := 0; i < 3; i++ {
		got := b.get(t, "shop.test", "/")
		if got.status != http.StatusOK {
			t.Fatalf("GET /: %d, want the page as written\n%s", got.status, b.log.String())
		}
		home = got.body
	}
	about := b.get(t, "shop.test", "/about").body
	frame := "[shop.test] cảnh báo: view.ui:2:1: image cần alt (alt=\"\" nếu chỉ để trang trí)"
	button := "[shop.test] cảnh báo: content.ui:2:34: <button> chỉ có icon nên chưa có tên để trình đọc màn hình đọc: viết alt trên icon (vd. <icon value=\"x\" alt=\"Đóng\" />)"
	log := b.log.String()
	for _, line := range []string{frame, button} {
		if said := strings.Count(log, line); said != 1 {
			t.Errorf("the terminal says %q %d times, want once:\n%s", line, said, log)
		}
	}
	if said := strings.Count(log, "cảnh báo:"); said != 2 {
		t.Errorf("the terminal says %d warnings, want 2:\n%s", said, log)
	}
	// while developing a page loads the engine's readable JavaScript; in production what was minified at build time
	if !strings.Contains(home, `src="`+kernel.Kernel().Path+`"`) {
		t.Errorf("while developing the page loads the readable kernel %s", kernel.Kernel().Path)
	}
	link := warningsScript.FindStringSubmatch(home)
	if link == nil || !warningsScript.MatchString(about) {
		t.Fatalf("a page with warnings links its warnings file while developing:\n%s", home)
	}
	file := b.get(t, "shop.test", link[1])
	if file.status != http.StatusOK || !strings.HasPrefix(file.header.Get("Content-Type"), "text/javascript") ||
		file.header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s: %d %q %q, want the warnings file, kept by its name", link[1], file.status, file.header.Get("Content-Type"), file.header.Get("Cache-Control"))
	}
	for _, line := range []string{frame, button} {
		quoted, _ := json.Marshal(strings.TrimPrefix(line, "[shop.test] "))
		if !strings.Contains(file.body, "console.warn("+string(quoted)+");\n") {
			t.Errorf("the console of / says %s\ngot:\n%s", quoted, file.body)
		}
	}
	if quiet := b.get(t, "shop.test", "/quiet"); quiet.status != http.StatusOK || strings.Contains(quiet.body, "warnings") {
		t.Errorf("GET /quiet: %d, a page with nothing to tell links no warnings file:\n%s", quiet.status, quiet.body)
	}

	production := warningsSite(t, `"shop.test"`)
	page := production.get(t, "shop.test", "/")
	if minified := kernel.KernelMin(); minified == nil || !strings.Contains(page.body, `src="`+minified.Path+`"`) {
		t.Errorf("in production the page loads the kernel minified at build time")
	}
	if page.status != http.StatusOK || strings.Contains(page.body, "warnings") {
		t.Errorf("production GET /: %d, links no warnings file:\n%s", page.status, page.body)
	}
	if strings.Contains(production.log.String(), "cảnh báo") {
		t.Errorf("production says no warning:\n%s", production.log.String())
	}
	if missing := production.get(t, "shop.test", "/_kitwork/warnings.0123456789abcdef.js"); missing.status != http.StatusNotFound {
		t.Errorf("production, a warnings name the engine did not write: %d, want 404", missing.status)
	}
}
