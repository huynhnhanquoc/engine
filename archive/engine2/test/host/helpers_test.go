// Package application_test drives engine2's host from outside: application.work read, every site
// booted, requests answered over HTTP (httptest), as `go run .` will serve them.
package host_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/host"
)

// testApp is the test app of engine2: testdata/application.
var testApp = filepath.Join("..", "..", "testdata", "application", "application.work")

// lines is the host's log as the tests read it: written by the host's goroutines, read by the test's.
type lines struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (l *lines) Write(line []byte) (int, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.buffer.Write(line)
}

func (l *lines) String() string {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.buffer.String()
}

// booted is a host read and booted, served by an httptest server, with its log.
type booted struct {
	host   *host.Host
	server *httptest.Server
	// client reads a redirect as the answer it is, not the page it leads to
	client *http.Client
	log    *lines
}

// boot reads the manifest at file, boots it and serves it until the test ends; each of set is applied to the host
// before it boots (longWork).
func boot(t testing.TB, file string, set ...func(*host.Host)) *booted {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(file, log)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	for _, each := range set {
		each(read)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot %s: %v\nlog:\n%s", file, err, log)
	}
	if err := read.Failed(); err != nil {
		t.Fatalf("boot %s closed an app: %v\nlog:\n%s", file, err, log)
	}
	return serve(t, read, log)
}

// longEnergy is the energy of a run that a test of time gives it, longWork the host's setting: a run's energy, ten
// million units, is some 44 ms of the engine's work (core/runtime, work.go, 15/11/2026), which ends a loop of reverses
// or parses long before a timeout of a test's length. Such a test is of the time, so the energy is put past it, as a
// host whose runs need more than the default would.
const longEnergy = 1 << 40

func longWork(h *host.Host) { h.RunEnergy = longEnergy }

// serve serves a host booted until the test ends.
func serve(t testing.TB, read *host.Host, log *lines) *booted {
	t.Helper()
	server := httptest.NewServer(read)
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = read.Close() }) // the database files its statements opened, before the folder goes
	client := *server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &booted{host: read, server: server, client: &client, log: log}
}

// reply is what a request got: its status, its headers, its body.
type reply struct {
	status int
	header http.Header
	body   string
}

// request sends method path to the site host, with body (nil for none) and headers.
func (b *booted) request(t testing.TB, method, host, path string, body io.Reader, headers ...string) reply {
	t.Helper()
	request, err := http.NewRequest(method, b.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s%s: %v", method, host, path, err)
	}
	defer response.Body.Close()
	read, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{status: response.StatusCode, header: response.Header, body: string(read)}
}

func (b *booted) get(t testing.TB, host, path string) reply {
	t.Helper()
	return b.request(t, http.MethodGet, host, path, nil)
}

// write puts files under a new folder, each by its "/"-separated path, and gives the folder. An app the files make,
// apps/<identity>/…, that writes no module.work gets routedModule: its sites are its folders named by their hostnames, as
// every app was before module.work (17/11/2026); a test of module.work writes its own.
func write(t testing.TB, files map[string]string) string {
	t.Helper()
	folder := t.TempDir()
	files = withModules(files)
	for name, text := range files {
		file := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// routedModule is the module.work of an app whose sites are its folders of hostnames: each name the host gives it is
// served from the folder of that name (docs/module.md).
const routedModule = `import * as module from "kitwork/module";

module.routes((site) => site.domain);
`

// zonedModule is routedModule with the app's zone, module.timezone(zone).
func zonedModule(zone string) string {
	return fmt.Sprintf(`import * as module from "kitwork/module";

module.timezone(%q);
module.routes((site) => site.domain);
`, zone)
}

// withModules is files with routedModule for each app under apps/ that writes no module.work of its own.
func withModules(files map[string]string) map[string]string {
	apps := map[string]bool{}
	for name := range files {
		if parts := strings.Split(name, "/"); len(parts) >= 3 && parts[0] == "apps" && parts[1] != "" {
			apps[parts[1]] = true
		}
	}
	if len(apps) == 0 {
		return files
	}
	out := make(map[string]string, len(files)+len(apps))
	for name, text := range files {
		out[name] = text
	}
	for app := range apps {
		if _, ok := out["apps/"+app+"/module.work"]; !ok {
			out["apps/"+app+"/module.work"] = routedModule
		}
	}
	return out
}

// manifest is a manifest serving the apps of its folder, for the tests that write their own app.
const manifest = `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`

// bootError reads and boots the app files make, and gives the error the boot ends with, or the error of the
// apps it closed ("" when none).
func bootError(t testing.TB, files map[string]string) string {
	t.Helper()
	if _, ok := files["application.work"]; !ok {
		files["application.work"] = manifest
	}
	folder := write(t, files)
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		return err.Error()
	}
	if err := read.Boot(); err != nil {
		return err.Error()
	}
	if err := read.Failed(); err != nil {
		return err.Error()
	}
	return ""
}

func contains(t testing.TB, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not say %q:\n%s", what, want, text)
		}
	}
}
