package host_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// limitedHost boots a site shop.test whose manifest's website writes rateLimit as written.
func limitedHost(t *testing.T, rateLimit string) *host.Host {
	t.Helper()
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
app.root("apps");
app.website({ hostname: "shop.test", port: 8080, rateLimit: ` + rateLimit + ` });
app.run();
`,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("ok"));` + "\n",
	})
	return boot(t, filepath.Join(folder, "application.work")).host
}

// hit sends GET / to shop.test from address with headers, and gives the answer.
func hit(h *host.Host, address string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "http://shop.test/", nil)
	request.RemoteAddr = net.JoinHostPort(address, "40000")
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

// statuses are the statuses of hits, one by one.
func statuses(h *host.Host, addresses ...string) string {
	var out []string
	for _, address := range addresses {
		out = append(out, strconv.Itoa(hit(h, address).Code))
	}
	return strings.Join(out, " ")
}

func TestTheHostsCeilingsOfRequests(t *testing.T) {
	// one address: its ceiling, then 429 with the seconds to wait; another address is its own; the machine itself and a
	// private address are never counted
	ip := limitedHost(t, `{ ip: 3, period: "1m" }`)
	if got := statuses(ip, "203.0.113.5", "203.0.113.5", "203.0.113.5", "203.0.113.5"); got != "200 200 200 429" {
		t.Errorf("one address: %s", got)
	}
	if wait := hit(ip, "203.0.113.5").Header().Get("Retry-After"); wait != "20" {
		t.Errorf("Retry-After %q, want 20 (a third of a minute)", wait)
	}
	if got := statuses(ip, "203.0.113.6"); got != "200" {
		t.Errorf("another address: %s", got)
	}
	for _, address := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.9"} {
		if got := statuses(ip, address, address, address, address, address); got != "200 200 200 200 200" {
			t.Errorf("%s is counted: %s", address, got)
		}
	}

	// all together
	all := limitedHost(t, `{ rate: 4, period: "1m" }`)
	if got := statuses(all, "203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5"); got != "200 200 200 200 429" {
		t.Errorf("all together: %s", got)
	}

	// a browser on an address, by its User-Agent and Accept-Language, whatever cookie or header of its own it sends;
	// another browser on the address is another, and so is the same browser seen from another address
	browser := limitedHost(t, `{ browser: 2, period: "1m" }`)
	same := []string{"User-Agent", "Shop/1.0", "Accept-Language", "vi"}
	var codes []string
	for _, c := range []struct {
		address string
		headers []string
	}{
		{"203.0.113.1", same},
		{"203.0.113.1", append(same, "Cookie", "__device_id=a")},
		{"203.0.113.1", append(same, "Cookie", "__device_id=b", "X-Device-ID", "c")},
		{"203.0.113.1", []string{"User-Agent", "Shop/2.0", "Accept-Language", "vi"}},
		{"203.0.113.1", []string{"User-Agent", "Shop/1.0", "Accept-Language", "en"}},
		{"203.0.113.2", same},
		{"203.0.113.3", same},
	} {
		codes = append(codes, strconv.Itoa(hit(browser, c.address, c.headers...).Code))
	}
	if got := strings.Join(codes, " "); got != "200 200 429 200 200 200 200" {
		t.Errorf("a browser: %s", got)
	}

	// a flood from many addresses, every request the same browser as millions send, spends no room of that browser
	// elsewhere (14/11/2026: a browser was one bucket for the whole host, and a few addresses at their own ceiling
	// shut every browser like theirs out)
	flooded := limitedHost(t, `{ ip: 3, browser: 5, period: "1m" }`)
	chrome := []string{"User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0", "Accept-Language", "vi-VN,vi;q=0.9"}
	for flooder := 1; flooder <= 20; flooder++ {
		for range 3 {
			hit(flooded, "198.51.100."+strconv.Itoa(flooder), chrome...)
		}
	}
	if code := hit(flooded, "203.0.113.77", chrome...).Code; code != http.StatusOK {
		t.Errorf("a browser on its own address after a flood of browsers like it from twenty others: %d", code)
	}
	if code := hit(flooded, "198.51.100.1", "User-Agent", "Other/1.0").Code; code != http.StatusTooManyRequests {
		t.Errorf("a flooding address under another User-Agent: %d, want its address's ceiling", code)
	}

	// an IPv6 address is counted by its /64, the network one subscriber is given: another address of it is the same
	// client, another network another; an IPv4 address written as IPv6 is that IPv4 address
	six := limitedHost(t, `{ ip: 2, period: "1m" }`)
	if got := statuses(six, "2001:db8:1:2::1", "2001:db8:1:2:ffff:eeee:dddd:9", "2001:db8:1:2::abcd", "2001:db8:1:3::1"); got != "200 200 429 200" {
		t.Errorf("IPv6 by its /64: %s", got)
	}
	if got := statuses(six, "203.0.113.9", "::ffff:203.0.113.9", "203.0.113.9"); got != "200 200 429" {
		t.Errorf("an IPv4 address written as IPv6: %s", got)
	}

	// a request one ceiling refuses takes from none: the refused second of .1 spends no room of all
	both := limitedHost(t, `{ rate: 3, ip: 1, period: "1m" }`)
	if got := statuses(both, "203.0.113.1", "203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4"); got != "200 429 200 200 429" {
		t.Errorf("a refused request: %s", got)
	}

	// a period fills the bucket again
	short := limitedHost(t, `{ ip: 1, period: "100ms" }`)
	if got := statuses(short, "203.0.113.1", "203.0.113.1"); got != "200 429" {
		t.Errorf("before the period: %s", got)
	}
	time.Sleep(150 * time.Millisecond)
	if got := statuses(short, "203.0.113.1"); got != "200" {
		t.Errorf("after the period: %s", got)
	}

	// no period written: a second
	second := limitedHost(t, `{ ip: 2 }`)
	if got := statuses(second, "203.0.113.1", "203.0.113.1", "203.0.113.1"); got != "200 200 429" {
		t.Errorf("a second: %s", got)
	}
	if wait := hit(second, "203.0.113.1").Header().Get("Retry-After"); wait != "1" {
		t.Errorf("Retry-After %q within a second, want 1", wait)
	}

	// none written: nothing counted
	none := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("ok"));` + "\n",
	}), "application.work")).host
	for i := 0; i < 50; i++ {
		if code := hit(none, "203.0.113.1").Code; code != http.StatusOK {
			t.Fatalf("a host with no rateLimit answered %d", code)
		}
	}
}

func TestTheHostsCeilingsAreReadStrictly(t *testing.T) {
	for written, want := range map[string]string{
		`10`:                          "app.website({ rateLimit }) takes an object",
		`{ ips: 10 }`:                 "no key ips",
		`{ user: 10 }`:                "app.website({ rateLimit: { user } }) is not served by engine2 yet",
		`{ rate: 0, ip: 0 }`:          "counts nothing",
		`{ ip: 1.5 }`:                 "a whole number of requests",
		`{ ip: -1 }`:                  "a whole number of requests",
		`{ ip: 10, period: 1 }`:       "a span written as text",
		`{ ip: 10, period: "often" }`: "a span written as text",
	} {
		folder := write(t, map[string]string{
			"application.work": "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080, rateLimit: " + written + " });\napp.run();\n",
			"apps/.keep":       "",
		})
		_, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("rateLimit: %s: %v, want %q", written, err, want)
		}
	}
	folder := write(t, map[string]string{
		"application.work": "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080, rateLimit: { ip: 5, period: \"2d\" } });\napp.run();\n",
		"apps/.keep":       "",
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if limit := read.Manifest.Website.RateLimit; limit.IP != 5 || limit.Period != 48*time.Hour {
		t.Errorf("rateLimit %+v", limit)
	}
}

// loggedHost boots a site shop.test whose manifest writes app.logger as written, logs to its log.
func loggedHost(t *testing.T, logger string) (*booted, string) {
	t.Helper()
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
app.root("apps");
app.logger(` + logger + `);
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => {
  console.debug("a debug line");
  console.log("a log line");
  console.warn("a warn line");
  console.error("an error line");
  return context.text("ok");
});
`,
		"apps/shop/shop.test/broken/router.work":  routerImport + `router.get((context) => error("it broke"));` + "\n",
		"apps/shop/shop.test/refused/router.work": routerImport + `router.get((context) => JSON.parse("{"));` + "\n",
		"apps/shop/shop.test/guarded/config.work": routerImport + `const none = {};
router.guard((context) => none.answer).methods("*");` + "\n",
		"apps/shop/shop.test/guarded/router.work": routerImport + `router.get((context) => context.text("in"));` + "\n",
	})
	return boot(t, filepath.Join(folder, "application.work")), folder
}

// read is the text of the file at name under folder, "" when there is none.
func read(folder, name string) string {
	data, _ := os.ReadFile(filepath.Join(folder, filepath.FromSlash(name)))
	return string(data)
}

func TestTheHostsLogGoesToItsFiles(t *testing.T) {
	b, folder := loggedHost(t, `{ level: "info", logfile: "logs/shop.log" }`)
	b.get(t, "shop.test", "/")
	b.get(t, "shop.test", "/broken")
	b.get(t, "shop.test", "/guarded")
	b.get(t, "shop.test", "/refused") // data from outside that is no JSON: 400
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	general, errorsFile, out := read(folder, "logs/shop.log"), read(folder, "errors/shop.log"), b.log.String()
	stamp := `\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}[+-Z][0-9:]*`
	for _, want := range []string{
		stamp + ` INFO \[shop.test\] \[console.log\] a log line`,
		stamp + ` WARN \[shop.test\] \[console.warn\] a warn line`,
		stamp + ` ERROR \[shop.test\] \[console.error\] an error line`,
		stamp + ` ERROR \[shop.test\] GET /broken .*: 500: `,
		stamp + ` WARN \[shop.test\] GET /guarded .*: 403: `,
		stamp + ` WARN \[shop.test\] GET /refused .*: 400: `,
	} {
		if !regexp.MustCompile(want).MatchString(general) {
			t.Errorf("logs/shop.log has no line %s:\n%s", want, general)
		}
	}
	if strings.Contains(general, "a debug line") || strings.Contains(out, "a debug line") {
		t.Errorf("a debug line was written at level info:\n%s", general)
	}
	// the errorfile is errors/<the logfile's name>, its lines those of level error alone
	if !strings.Contains(errorsFile, "an error line") || !strings.Contains(errorsFile, ": 500: ") ||
		strings.Contains(errorsFile, "a warn line") || strings.Contains(errorsFile, "a log line") || strings.Contains(errorsFile, ": 403: ") {
		t.Errorf("errors/shop.log:\n%s", errorsFile)
	}
	// standard output keeps its lines as they were, without time or level
	if !strings.Contains(out, "[shop.test] [console.log] a log line\n") || strings.Contains(out, " INFO ") {
		t.Errorf("standard output:\n%s", out)
	}
}

func TestTheHostsLogAtItsLevelAndFormat(t *testing.T) {
	// warn: nothing of info, anywhere
	b, folder := loggedHost(t, `{ level: "warn", logfile: "logs/shop.log", errorfile: "logs/broken.log" }`)
	b.get(t, "shop.test", "/")
	b.host.Close()
	general, out := read(folder, "logs/shop.log"), b.log.String()
	if strings.Contains(general, "a log line") || strings.Contains(out, "a log line") || strings.Contains(out, "serving") {
		t.Errorf("a line of info was written at level warn:\n%s\n%s", general, out)
	}
	if !strings.Contains(general, "a warn line") || !strings.Contains(read(folder, "logs/broken.log"), "an error line") {
		t.Errorf("level warn:\n%s", general)
	}

	// debug: every line
	b, folder = loggedHost(t, `{ level: "debug", logfile: "logs/shop.log" }`)
	b.get(t, "shop.test", "/")
	b.host.Close()
	if !strings.Contains(read(folder, "logs/shop.log"), "DEBUG [shop.test] [console.debug] a debug line") {
		t.Errorf("level debug:\n%s", read(folder, "logs/shop.log"))
	}

	// json: an object a line; console: false, nothing to standard output
	b, folder = loggedHost(t, `{ format: "json", logfile: "logs/shop.log", console: false }`)
	b.get(t, "shop.test", "/")
	b.host.Close()
	if out := b.log.String(); out != "" {
		t.Errorf("console: false wrote to standard output:\n%s", out)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(read(folder, "logs/shop.log")), "\n") {
		var entry struct{ Time, Level, Msg string }
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Time == "" || entry.Level == "" {
			t.Fatalf("not a line of JSON: %q (%v)", line, err)
		}
		if entry.Level == "WARN" && entry.Msg == "[shop.test] [console.warn] a warn line" {
			found = true
		}
	}
	if !found {
		t.Errorf("json: no warn line:\n%s", read(folder, "logs/shop.log"))
	}
}

// The files of app.logger open as the host starts, not as its manifest is read.
func TestTheHostsLogFilesOpenAsItStarts(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.logger({ logfile: \"logs/shop.log\" });\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/.keep":       "",
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "logs")); !os.IsNotExist(err) {
		t.Fatal("a read of the manifest made the log's folder")
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	if _, err := os.Stat(filepath.Join(folder, "logs", "shop.log")); err != nil {
		t.Errorf("the boot opened no logfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "errors", "shop.log")); err != nil {
		t.Errorf("the boot opened no errorfile: %v", err)
	}
}

// An app the boot closes is a line of level error; a file of the log that cannot be opened stops the start.
func TestTheHostsLogAtTheBoot(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.logger({ logfile: \"logs/shop.log\" });\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/shop/shop.test/router.work": routerImport + "router.get(1);\n",
	})
	started, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := started.Boot(); err != nil {
		t.Fatal(err)
	}
	started.Close()
	if got := read(folder, "errors/shop.log"); !strings.Contains(got, "ERROR [app] shop is closed, its sites answer 503") {
		t.Errorf("errors/shop.log:\n%s", got)
	}

	folder = write(t, map[string]string{
		"application.work": "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.logger({ logfile: \"taken/shop.log\" });\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"taken":            "a file where the log's folder would be",
		"apps/.keep":       "",
	})
	started, err = host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := started.Boot(); err == nil || !strings.Contains(err.Error(), "app.logger({ logfile })") {
		t.Errorf("a logfile that cannot open: %v", err)
	}
	started.Close()
}

func TestTheHostsLoggerIsReadStrictly(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": "import * as app from \"kitwork/application\";\napp.logger({ level: \"WARN\", format: \"json\", logfile: \"logs/a.log\", errorfile: \"logs/b.log\", max_size: 3, max_backups: 2, max_age: 7, compress: true, console: false });\napp.root(\"apps\");\napp.website({ hostname: \"shop.test\", port: 8080 });\napp.run();\n",
		"apps/.keep":       "",
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	logger := read.Manifest.Logger
	if logger.Level != "warn" || logger.Format != "json" || logger.LogFile != filepath.Join(folder, "logs", "a.log") || logger.ErrorFile != filepath.Join(folder, "logs", "b.log") ||
		logger.MaxSize != 3 || logger.MaxBackups != 2 || logger.MaxAge != 7 || !logger.Compress || logger.Console {
		t.Errorf("app.logger read %+v", logger)
	}

	for written, want := range map[string]string{
		`app.logger({ level: "loud" });`:                                     `app.logger({ level }): "debug", "info", "warn" or "error"`,
		`app.logger({ format: "xml" });`:                                     `app.logger({ format }): "text" or "json"`,
		`app.logger({ logfile: "" });`:                                       "app.logger({ logfile }): the path of a file",
		`app.logger({ maxSize: 10 });`:                                       "no key maxSize",
		`app.logger({ max_size: 1.5 });`:                                     "app.logger({ max_size }): a whole number",
		`app.logger({ compress: "yes" });`:                                   "app.logger({ compress }): true or false",
		`app.logger("logs/a.log");`:                                          "app.logger(…) takes one object",
		"app.logger({ level: \"info\" });\napp.logger({ level: \"info\" });": "app.logger(…) is declared twice",
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
