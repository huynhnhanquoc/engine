package file

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A file of the data zone (what the app wrote, an upload under the name its sender chose) served as a type a browser
// runs as a page (HTML, XHTML, SVG, XML) comes with Content-Security-Policy: sandbox: shown, its scripts not run, its
// origin not the site's, so an upload "a.html" or "a.svg" cannot read the viewer's cookies or act as them. An image,
// a text, and a file of the code zone (the author's own) are served as they were.
//
// CONTROL (09/11/2026): with no sandbox, "uploads/a.html as text/html; charset=utf-8: Content-Security-Policy \"\",
// want sandbox".
func TestServeSandboxesWhatRunsFromTheDataZone(t *testing.T) {
	app := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uploads := filepath.Join(app, ".database", "files", "uploads")
	write(filepath.Join(uploads, "a.html"), `<script>document.cookie</script>`)
	write(filepath.Join(uploads, "a.svg"), `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	write(filepath.Join(uploads, "a.xml"), `<a/>`)
	write(filepath.Join(uploads, "a.png"), "\x89PNG")
	write(filepath.Join(uploads, "a.txt"), "chữ")
	write(filepath.Join(app, "page.html"), `<p>của tác giả</p>`)
	files := &File{Root: app}
	for _, check := range []struct {
		path, mediaType string
		sandboxed       bool
	}{
		{"uploads/a.html", "", true},
		{"uploads/a.svg", "", true},
		{"uploads/a.xml", "", true},
		{"uploads/a.png", "", false},
		{"uploads/a.txt", "", false},
		{"uploads/a.txt", "text/html; charset=utf-8", true}, // .type("text/html") on what a user wrote
		{"app/page.html", "", false},
	} {
		handle := files.Open(value.String(check.path)).Host().(*Handle)
		recorder := httptest.NewRecorder()
		handle.Serve(recorder, httptest.NewRequest(http.MethodGet, "/f", nil), check.mediaType)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: %d", check.path, recorder.Code)
		}
		got := recorder.Header().Get("Content-Security-Policy")
		if want := map[bool]string{true: "sandbox"}[check.sandboxed]; got != want {
			t.Errorf("%s as %s: Content-Security-Policy %q, want %q", check.path, recorder.Header().Get("Content-Type"), got, want)
		}
	}
}
