package host_test

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// Stop (Ctrl+C or SIGTERM, run.go) ends kitwork database as Close does: ServeDatabase returns, the port no longer
// answers, and the app's database written through the port is closed, KitDB's writer last, so no -wal stays beside it.
// Before 12/11/2026 nothing caught the signal and the process died with the files open.
//
// CONTROL (12/11/2026): with ServeDatabase not listening for Stop, "ServeDatabase did not end 10s after Stop".
func TestStopEndsTheDatabaseAlone(t *testing.T) {
	port := freePort(t)
	folder := write(t, map[string]string{
		"application.work":                aloneManifest(port),
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("x"));` + "\n",
	})
	file := filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db")
	writeSQLite(t, file, `CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT NOT NULL)`)
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- read.ServeDatabase() }()
	address := "127.0.0.1:" + strconv.Itoa(port)
	var client *pgClient
	for deadline := time.Now().Add(5 * time.Second); ; {
		if client, err = pgConnect(address, "admin", "mật khẩu", "shop.test/application"); err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, log)
	}
	if _, tag, err := client.query(`INSERT INTO notes (text) VALUES ('một')`); err != nil || tag != "INSERT 0 1" {
		t.Fatalf("INSERT through the port: %q, %v", tag, err)
	}
	client.Close()
	if _, err := os.Stat(file + "-wal"); err != nil {
		t.Fatalf("no -wal while the port writes the file: %v", err)
	}

	read.Stop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("ServeDatabase after Stop: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("ServeDatabase did not end 10s after Stop\n%s", log)
	}
	if info, err := os.Stat(file + "-wal"); err == nil {
		t.Errorf("a -wal of %d bytes stays beside the file after Stop", info.Size())
	}
	if other, err := pgConnect(address, "admin", "mật khẩu", "shop.test/application"); err == nil {
		other.Close()
		t.Error("the port still answers after Stop")
	}
	if !strings.Contains(log.String(), "[app] stopping: the database's files close") {
		t.Errorf("the log says nothing of stopping:\n%s", log)
	}
}

// Stop ends Serve gracefully: an open stream of context.sse closes at once (it never ends by itself, and would hold the
// server to the end of the grace, here 30 s), so Serve returns in a moment; then no request is taken.
//
// CONTROL (12/11/2026): with the streams not closed as the servers shut down, "Serve did not end 10s after Stop" (the
// stream held it); with Serve not listening for Stop, the same.
func TestStopEndsTheServerAndItsStreams(t *testing.T) {
	port := freePort(t)
	folder := write(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: ` + strconv.Itoa(port) + `, allowLocal: true });
app.run();
`,
		"apps/shop/shop.test/router.work":        routerImport + `router.get((context) => context.text("ok"));` + "\n",
		"apps/shop/shop.test/stream/router.work": sseImport + `router.get((context) => context.sse);` + "\n",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	read.StopGrace = 30 * time.Second
	served := make(chan error, 1)
	go func() { served <- read.Serve() }()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	get := func(path string) (*http.Response, error) {
		request, _ := http.NewRequest(http.MethodGet, base+path, nil)
		request.Host = "shop.test"
		return http.DefaultClient.Do(request)
	}
	eventually(t, "the site answering", func() bool {
		response, err := get("/")
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == http.StatusOK
	})
	stream, err := get("/stream")
	if err != nil || stream.StatusCode != http.StatusOK {
		t.Fatalf("the stream: %v %v", stream, err)
	}
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		reader := bufio.NewReader(stream.Body)
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()

	began := time.Now()
	read.Stop()
	select {
	case err := <-served:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Fatalf("Serve after Stop: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Serve did not end 10s after Stop\n%s", log)
	}
	t.Logf("Serve ended %v after Stop", time.Since(began).Round(time.Millisecond))
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream is still open after Serve ended")
	}
	stream.Body.Close()
	if response, err := get("/"); err == nil {
		response.Body.Close()
		t.Error("the site still answers after Stop")
	}
	if !strings.Contains(log.String(), "[app] stopping: the requests going on get 30s") {
		t.Errorf("the log says nothing of stopping:\n%s", log)
	}
}
