package host_test

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
)

// searchFiles are a page that runs in the browser with two autocompletes (docs/components/autocomplete.md): one whose
// options the page finds on the server as one types (its input's words a var, keyword; search() a work filling the
// var districts; change calling loadWards()), its choice two-way with the var district; one of options written,
// two-way with the var city, which a button writes.
func searchFiles() map[string]string {
	site := "apps/shop/shop.test/"
	return map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		site + "search/router.work": routerImport + `const DISTRICTS = [{ id: "q1", name: "Quận 1" }, { id: "q3", name: "Quận 3" },
  { id: "bt", name: "Bình Thạnh" }, { id: "bc", name: "Bình Chánh" }];

router.view((context) => {
  var district = "q3";
  var keyword = "";
  var districts = [];
  var city = "hn";
  var wards = "";
  const search = () => {
    districts = keyword === "" ? [] : DISTRICTS.filter((d) => d.name.includes(keyword));
  };
  const loadWards = () => {
    wards = "phường của " + district;
  };
  return context.bind({ district, keyword, districts, city, wards, search, loadWards });
});
`,
		site + "search/content.ui": `<content gap="4">
  <autocomplete name="district" value="{{ district }}" change="loadWards()">
    <input value="{{ keyword }}" input:debounce(100)="search()" placeholder="Tìm quận" />
    <popover place="below-left">
      <option for="item of districts; key: item.id" value="{{ item.id }}"><text>{{ item.name }}</text></option>
      <empty>Không tìm thấy</empty>
    </popover>
  </autocomplete>
  <text id="wards">{{ wards }}</text>
  <autocomplete name="city" value="{{ city }}">
    <input placeholder="Thành phố" />
    <popover><option value="hn">Hà Nội</option><option value="dn">Đà Nẵng</option><option value="hcm">Hồ Chí Minh</option></popover>
  </autocomplete>
  <text id="city">{{ city }}</text>
  <button click="city = 'dn'">Đà Nẵng</button>
</content>
`,
		site + "member/router.work": routerImport + `router.view((context) => {
  var form = { district: "q1" };
  return context.bind({ form });
});
`,
		site + "member/content.ui": `<content><autocomplete value="{{ form.district }}"><input />` +
			`<popover><option value="q1">Quận 1</option></popover></autocomplete></content>
`,
	}
}

// The page draws each autocomplete's hidden input as its var's two-way field, the search's options as a list the
// browser keeps (a template row and none yet), the city's input with the words of the option its var names
// (16/11/2026, round 5 of the 13/11 review, dialog + catalogue, slice 6). CONTROL: the two-way bind left off the
// hidden input, red.
func TestAnAutocompleteOfAPageIsTwoWay(t *testing.T) {
	b := boot(t, filepath.Join(write(t, searchFiles()), "application.work"))
	got := b.get(t, "shop.test", "/search")
	if got.status != http.StatusOK {
		t.Fatalf("GET /search: %d\n%s\n%s", got.status, got.body, b.log.String())
	}
	contains(t, "the page", got.body,
		`<input type="hidden" name="district" value="q3" data-kitwork-bind="value: district">`,
		`value="" placeholder="Tìm quận"`, `data-kitwork-bind="value: keyword; aria-expanded: opened" data-kitwork-event="input.debounce(100): search()"`,
		`data-kitwork-scope="filter: false"`, `data-kitwork-for="item of districts; key: item.id" data-kitwork-template hidden`,
		`data-kitwork-bind="data-value: item.id"`,
		`value="Hà Nội" placeholder="Thành phố"`, `<input type="hidden" name="city" value="hn" data-kitwork-bind="value: city">`,
		`data-kitwork-event="change: loadWards()"`)
	// what the browser keeps that is no var's name cannot be written back: an error saying so
	wrong := b.get(t, "shop.test", "/member")
	if wrong.status != http.StatusInternalServerError || !strings.Contains(wrong.body+b.log.String(), "value của autocomplete đọc form.district, thứ trình duyệt giữ: viết một var của trang") {
		t.Errorf("GET /member: %d\n%s\nlog:\n%s", wrong.status, wrong.body, b.log.String())
	}
}

// In a browser, against the host (16/11/2026): typing "Bình" calls search() on the server after the pause, the
// options come back as the browser's list and show, and typing over the choice the page was built with undoes it
// (district empty, change calling loadWards()); Down and Enter choose Bình Thạnh, which writes district (the server's
// loadWards() answers with its wards) and the keyword the input shows; the button writing city shows Đà Nẵng in the
// city's input; typing "ha" there and choosing writes city "hn". CONTROLS: the component writing no var, red; the
// hidden input's two-way bind left off, red.
func TestBrowserAnAutocompleteSearchesTheServerTwoWay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := hostBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	b := boot(t, filepath.Join(write(t, searchFiles()), "application.work"))
	script := `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var boxes = document.querySelectorAll("[role=combobox]"), search = boxes[0], city = boxes[1], seen = [];
    var rows = function () {
      return Array.prototype.filter.call(search.parentNode.querySelectorAll("[role=option]"), function (o) {
        return !o.hasAttribute("data-kitwork-template") && !o.hidden;
      }).map(function (o) { return o.textContent; }).join(",");
    };
    var look = function (label) {
      seen.push(label + ": " + [search.value, search.nextElementSibling.value, rows(), document.getElementById("wards").textContent,
        city.value, city.nextElementSibling.value, document.getElementById("city").textContent].join(" | "));
    };
    var key = function (box, k) { box.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true })); };
    var type = function (box, text) { box.focus(); box.value = text; box.dispatchEvent(new Event("input", { bubbles: true })); };
    look("built");
    type(search, "Bình");
    setTimeout(function () {
      look("found");
      key(search, "ArrowDown");
      key(search, "Enter");
      setTimeout(function () {
        look("chosen");
        document.querySelector("main > button").click();
        setTimeout(function () {
          look("assigned");
          type(city, "ha");
          key(city, "ArrowDown");
          key(city, "Enter");
          setTimeout(function () {
            look("typed");
            fetch("/report", { method: "POST", body: JSON.stringify(seen) });
          }, 300);
        }, 300);
      }, 1500);
    }, 1500);
  });
});
</script>`
	var seen []string
	if err := json.Unmarshal(runHostPage(t, browser, b, "/search", script), &seen); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"built:  | q3 |  |  | Hà Nội | hn | hn",
		"found: Bình |  | Bình Thạnh,Bình Chánh | phường của  | Hà Nội | hn | hn",
		"chosen: Bình Thạnh | bt | Bình Thạnh,Bình Chánh | phường của bt | Hà Nội | hn | hn",
		"assigned: Bình Thạnh | bt | Bình Thạnh,Bình Chánh | phường của bt | Đà Nẵng | dn | dn",
		"typed: Bình Thạnh | bt | Bình Thạnh,Bình Chánh | phường của bt | Hà Nội | hn | hn",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("the page went\n%s\nwant\n%s\nlog:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"), b.log.String())
	}
}

// runHostPage opens path of the booted host's shop.test in browser through a server of the test's own: the page
// with script before its </body>, every other request the host's (its files, the page's calls), /hold answered after
// a second (a page waits on it for the kernel to have run), /report what the page posts there, which it gives.
func runHostPage(t *testing.T, browser string, b *booted, path, script string) []byte {
	t.Helper()
	var mu sync.Mutex
	var report []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/hold":
			time.Sleep(time.Second)
		case request.URL.Path == "/report":
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			report = body
			mu.Unlock()
		case request.URL.Path == path && request.Method == http.MethodGet && request.URL.RawQuery == "":
			request.Host = "shop.test"
			request.Header.Del("Accept-Encoding") // the page as text, to put the script in
			recorded := httptest.NewRecorder()
			b.host.ServeHTTP(recorded, request)
			for name, values := range recorded.Header() {
				if name != "Content-Length" && name != "Content-Security-Policy" {
					response.Header()[name] = values
				}
			}
			response.WriteHeader(recorded.Code)
			_, _ = io.WriteString(response, strings.Replace(recorded.Body.String(), "</body>", script+"</body>", 1))
		default:
			request.Host = "shop.test"
			b.host.ServeHTTP(response, request)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--disable-background-networking", "--disable-default-apps", "--disable-extensions", "--disable-sync",
		"--metrics-recording-only", "--no-first-run", "--window-size=800,600",
		"--user-data-dir=" + filepath.Join(t.TempDir(), "profile"), "--virtual-time-budget=30000", "--dump-dom", server.URL + path}
	if runtime.GOOS == "windows" {
		args = append([]string{"--disable-background-mode", "--disable-breakpad", "--disable-crashpad-for-testing"}, args...)
	}
	output, _ := exec.CommandContext(ctx, browser, args...).CombinedOutput()
	mu.Lock()
	defer mu.Unlock()
	if report == nil {
		_ = os.WriteFile(filepath.Join(os.TempDir(), "kitwork-autocomplete-dump.html"), output, 0o644)
		t.Fatalf("the page never reported (its DOM in %s)", filepath.Join(os.TempDir(), "kitwork-autocomplete-dump.html"))
	}
	return report
}

// hostBrowser is Chrome, Chromium or Edge, "" when none is installed or KITWORK_BROWSER_TESTS says not to.
func hostBrowser() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KITWORK_BROWSER_TESTS"))) {
	case "0", "false", "off", "no":
		return ""
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if found, err := exec.LookPath(name); err == nil {
			return found
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
