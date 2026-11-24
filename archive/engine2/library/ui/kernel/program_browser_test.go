package kernel_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// The kernel runs only the page's program (round 4 of the 13/11 review, slice 3, 15/11/2026), in a real browser: a
// source the program holds runs as its function, a name no scope holds reading undefined (it read 0); a source no
// program holds runs nothing, its element keeping what the server drew, and is said once in the console however many
// elements write it; data-kitwork-scope's values are read as the literals the engine writes (a number, a string in
// quotes with its escapes, true). CONTROLS: a name no scope holds read 0 again, red; a missing source said each time,
// red.
func TestBrowserTheKernelRunsOnlyThePagesProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := findHeadlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	var program strings.Builder
	program.WriteString("<script>kitwork.expressions({")
	for i, source := range []string{"nope + ''", "n", "label", "flag"} {
		written, err := compiler.ExpressionJavaScript(source)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			program.WriteString(", ")
		}
		quoted, _ := json.Marshal(source)
		program.WriteString(string(quoted) + ": function () { return " + written + "; }")
	}
	program.WriteString("});</script>")
	page := `<!doctype html><html><head><meta charset="utf-8"><script>
window.said = [];
var error = console.error;
console.error = function () { said.push(String(arguments[0] && arguments[0].message || arguments[0])); return error.apply(console, arguments); };
</script></head><body>
<main data-kitwork-scope="n: 41; label: 'it\'s \\ here'; flag: true">
  <p id="unknown" data-kitwork-bind="text: nope + ''">drawn</p>
  <p id="missing" data-kitwork-bind="text: n + 1">first</p>
  <p id="again" data-kitwork-bind="title: n; text: n + 1">second</p>
  <p id="number" data-kitwork-bind="text: n"></p>
  <p id="text" data-kitwork-bind="text: label"></p>
  <p id="flag" data-kitwork-bind="text: flag"></p>
</main>
<script src="/kernel.js"></script>` + program.String() + `
<script>
window.addEventListener("load", function () {
  setTimeout(function () {
    var text = function (id) { return document.getElementById(id).textContent; };
    fetch("/report", { method: "POST", body: JSON.stringify({
      unknown: text("unknown"), missing: text("missing"), again: text("again"), number: text("number"),
      text: text("text"), flag: text("flag"), said: said }) });
  }, 300);
});
</script></body></html>`
	for _, c := range []struct {
		name string
		file func() *kernel.File
	}{{"readable", kernel.Kernel}, {"minified", kernel.KernelMin}} {
		t.Run(c.name, func(t *testing.T) {
			file := c.file()
			if file == nil {
				t.Fatal("the kernel has no minified twin made from its source of today: run tools/minify (cd tools/minify && go run .)")
			}
			var seen struct {
				Unknown, Missing, Again, Number, Text, Flag string
				Said                                        []string
			}
			if err := json.Unmarshal(runKernelPage(t, browser, page, file.Body), &seen); err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct{ what, got, want string }{
				{"a name no scope holds", seen.Unknown, "undefined"},
				{"a source no program holds", seen.Missing, "first"},
				{"the same source again", seen.Again, "second"},
				{"a number of data-kitwork-scope", seen.Number, "41"},
				{"a string of data-kitwork-scope", seen.Text, `it's \ here`},
				{"true of data-kitwork-scope", seen.Flag, "true"},
			} {
				if check.got != check.want {
					t.Errorf("%s: %q, want %q", check.what, check.got, check.want)
				}
			}
			missing := 0
			for _, line := range seen.Said {
				if strings.Contains(line, `"n + 1" is in no program of this page`) {
					missing++
				}
			}
			if missing != 1 {
				t.Errorf("the source no program holds was said %d times, want once:\n%s", missing, strings.Join(seen.Said, "\n"))
			}
		})
	}
}

// runKernelPage serves page with the kernel body at /kernel.js and gives what it posts to /report.
func runKernelPage(t *testing.T, browser, page string, body []byte) []byte {
	t.Helper()
	var mu sync.Mutex
	var report []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, page)
		case "/kernel.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(body)
		case "/report":
			got, _ := io.ReadAll(r.Body)
			mu.Lock()
			report = got
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--disable-background-networking", "--disable-default-apps", "--disable-extensions", "--disable-sync",
		"--metrics-recording-only", "--no-first-run", "--window-size=800,600",
		"--user-data-dir=" + filepath.Join(t.TempDir(), "profile"), "--virtual-time-budget=20000", "--dump-dom", server.URL + "/"}
	if runtime.GOOS == "windows" {
		args = append([]string{"--disable-background-mode", "--disable-breakpad", "--disable-crashpad-for-testing"}, args...)
	}
	output, _ := exec.CommandContext(ctx, browser, args...).CombinedOutput()
	mu.Lock()
	defer mu.Unlock()
	if report == nil {
		t.Fatalf("the page never reported\n%.700s", output)
	}
	return report
}
