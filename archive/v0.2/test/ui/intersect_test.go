package ui_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// intersect-enter and intersect-leave run (15/11/2026, round 3 of the 13/11 review): engine2 wrote their pairs and
// gave no page the file that watches the screen (the first engine's jit/js/directives/intersect.js), so no
// intersect event ever ran, and the page said nothing. A page that writes one loads kernel.Intersect after the
// kernel, minified in production; a page that writes none does not.
func TestAPageWithAnIntersectEventLoadsItsObserver(t *testing.T) {
	watching := map[string]string{"content.ui": `<content><dropdown><button intersect-enter:once="toggle()">a</button><popover>p</popover></dropdown></content>`}
	clicking := map[string]string{"content.ui": `<content><dropdown><button click="toggle()">a</button><popover>p</popover></dropdown></content>`}
	for _, c := range []struct {
		name  string
		files map[string]string
		head  ui.Head
		want  []*kernel.File
	}{
		{"developing", watching, ui.Head{}, []*kernel.File{kernel.Kernel(), kernel.Intersect(), component.File("dropdown")}},
		{"in production", watching, ui.Head{Minify: true}, []*kernel.File{kernel.KernelMin(), kernel.IntersectMin(), component.MinFile("dropdown")}},
		{"no intersect event", clicking, ui.Head{}, []*kernel.File{kernel.Kernel(), component.File("dropdown")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			document, _ := drawWarned(t, c.files, value.Value{}, c.head)
			at := 0
			for _, file := range c.want {
				if file == nil {
					t.Fatal("a file has no minified twin: run tools/minify (cd tools/minify && go run .)")
				}
				tag := `<script data-kitwork-generated="` + file.Role + `" src="` + file.Path + `" defer></script>`
				i := strings.Index(document[at:], tag)
				if i < 0 {
					t.Fatalf("the page does not load %s after what comes before it:\n%s", file.Path, document[strings.LastIndex(document, "</style>"):])
				}
				at += i + len(tag)
			}
			if c.name == "no intersect event" && strings.Contains(document, "/_kitwork/directives/intersect.") {
				t.Error("a page with no intersect event loads their observer")
			}
		})
	}
}

// What a page loads for its intersect events is kept with the part that writes them: the part written again from
// what an earlier page kept loads the observer as the page drawn whole does.
func TestAPartKeepsTheObserverItsIntersectEventsLoad(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><box padding="2"><text>{{ title }}</text></box>` +
			`<box padding="2"><dropdown><button intersect-enter="toggle()">a</button><popover>p</popover></dropdown></box></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("title", value.String("one"))},
		{path: "", data: object("title", value.String("two"))},
	})
}

// In a browser (Chrome or Edge, headless), a page drawn by library/ui and given its files by ui.FileAt runs each
// intersect event as the screen says: an element on the screen runs its intersect-enter and not its
// intersect-leave, one far below runs its intersect-leave and not its intersect-enter, once and half read as
// modifiers, and margin(…) grows the screen the observer tests (a far element under margin(6000px) is on it).
// CONTROL: the page not given the observer, every event that should run stays unrun; the test fails on each.
func TestBrowserIntersectEventsRunAsTheScreenSays(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	cases := []struct {
		attribute string
		far, runs bool
	}{
		{`intersect-enter="toggle()"`, false, true},
		{`intersect-enter:once.half="toggle()"`, false, true},
		{`intersect-leave="toggle()"`, false, false},
		{`intersect-enter="toggle()"`, true, false},
		{`intersect-leave="toggle()"`, true, true},
		{`intersect-enter:margin(6000px)="toggle()"`, true, true},
	}
	var content strings.Builder
	content.WriteString("<content>")
	for _, c := range cases {
		content.WriteString(`<dropdown><button ` + c.attribute + `>x</button><popover>p</popover></dropdown>`)
	}
	content.WriteString("</content>")
	files := map[string]string{"content.ui": content.String()}
	for _, head := range []ui.Head{{}, {Minify: true}} {
		name := "readable"
		if head.Minify {
			name = "minified"
		}
		t.Run(name, func(t *testing.T) {
			document, _ := drawWarned(t, files, value.Value{}, head)
			// the fourth dropdown and those after it far below the screen (800×600)
			page := strings.Replace(document, "</head>", "<style>main > div:nth-child(4) { margin-top: 4000px }</style>\n</head>", 1)
			page = strings.Replace(page, "</body>", reportOpen+"</body>", 1)
			report := runInBrowser(t, browser, page)
			var seen struct {
				Open     []bool `json:"open"`
				Watching bool   `json:"watching"`
			}
			if err := json.Unmarshal(report, &seen); err != nil {
				t.Fatalf("report %.300s: %v", report, err)
			}
			if !seen.Watching {
				t.Error("the page has no observer of intersect events (window.kitwork.intersect)")
			}
			if len(seen.Open) != len(cases) {
				t.Fatalf("the browser read %d dropdowns, want %d", len(seen.Open), len(cases))
			}
			for i, c := range cases {
				where := "on the screen"
				if c.far {
					where = "far below the screen"
				}
				if seen.Open[i] != c.runs {
					t.Errorf("%s %s: ran %v, want %v", c.attribute, where, seen.Open[i], c.runs)
				}
			}
		})
	}
}

// reportOpen posts, once the page has been on the screen a while, whether each dropdown of the page is open, and
// whether the page has the observer. The browser's clock is virtual (--virtual-time-budget), and only a request
// pending holds it: /hold, answered a second later, gives the browser the frames an IntersectionObserver reports in.
const reportOpen = `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    setTimeout(function () {
      var open = [];
      document.querySelectorAll("main > div").forEach(function (d) { open.push(!d.querySelector(".popover").hidden); });
      fetch("/report", { method: "POST", body: JSON.stringify({ open: open, watching: !!(window.kitwork && window.kitwork.intersect) }) });
    }, 100);
  });
});
</script>`

// runInBrowser serves page at / and the engine's files under /_kitwork/ (ui.FileAt), opens it in browser, and gives
// what the page posts to /report.
func runInBrowser(t *testing.T, browser, page string) []byte {
	t.Helper()
	var mu sync.Mutex
	var report []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(response, page)
		case "/hold":
			time.Sleep(time.Second)
		case "/report":
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			report = body
			mu.Unlock()
		default:
			file, _ := ui.FileAt(request.URL.Path)
			if file == nil {
				http.NotFound(response, request)
				return
			}
			response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = response.Write(file.Body)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--disable-background-networking", "--disable-default-apps", "--disable-extensions", "--disable-sync",
		"--metrics-recording-only", "--no-first-run", "--window-size=800,600", "--run-all-compositor-stages-before-draw",
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

// headlessBrowser is Chrome, Chromium or Edge, "" when none is installed or KITWORK_BROWSER_TESTS says not to.
func headlessBrowser() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITWORK_BROWSER_TESTS"))) {
	case "0", "false", "off", "no":
		return ""
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, path := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
