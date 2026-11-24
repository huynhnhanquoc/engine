package host_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// reloadManifest serves the apps of apps/ with app.hotReload(true).
const reloadManifest = `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
app.hotReload(true);
app.run();
`

// reloading boots the files with reloadManifest and serves them, an app that does not boot answering 503, reading the
// apps' files every 20 ms; folder is where they are.
func reloading(t *testing.T, files map[string]string, set ...func(*host.Host)) (b *booted, folder string) {
	t.Helper()
	if _, ok := files["application.work"]; !ok {
		files["application.work"] = reloadManifest
	}
	folder = write(t, files)
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	for _, each := range set {
		each(read)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	b = serve(t, read, log)
	b.host.ReloadEvery = 20 * time.Millisecond
	b.host.StartWatching()
	return b, folder
}

// rewrite writes text to the file at name under folder.
func rewrite(t *testing.T, folder, name, text string) {
	t.Helper()
	file := filepath.Join(folder, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// removeAll removes the folder at path, trying again for a second: on Windows a folder the watcher is reading at that
// moment cannot be removed.
func removeAll(t *testing.T, path string) {
	t.Helper()
	var err error
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err = os.RemoveAll(path); err == nil {
			return
		}
	}
	t.Fatal(err)
}

// eventually waits up to five seconds for ok.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within five seconds", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHotReloadBootsAnAppAgain(t *testing.T) {
	b, folder := reloading(t, map[string]string{
		"apps/gone/gone.test/router.work":     routerImport + `router.get((context) => context.text("gone"));` + "\n",
		"apps/broken/broken.test/router.work": routerImport + "router.get(1);\n",
		"apps/shop/shop.test/router.work":     routerImport + `router.get((context) => context.text("one"));` + "\n",
		"apps/shop/shop.test/count/router.work": "import { memory } from \"kitwork/storage\";\n" + routerImport + `router.get((context) => {
  const count = (memory("count").get() ?? 0) + 1;
  memory("count").set(count);
  return context.text("" + count);
});
`,
	})
	text := func(hostname, path string) string { return b.get(t, hostname, path).body }
	if got := text("shop.test", "/"); got != "one" {
		t.Fatalf("before: %q", got)
	}
	if got := text("shop.test", "/count"); got != "1" {
		t.Fatalf("count before: %q", got)
	}
	b.host.StartWatching() // it starts once
	time.Sleep(100 * time.Millisecond)
	if said := b.log.String(); strings.Contains(said, "reloaded") || strings.Contains(said, "reload after") {
		t.Fatalf("a reload with no change:\n%s", said)
	}
	if lines := strings.Count(b.log.String(), "hot reload:"); lines != 1 {
		t.Errorf("hot reload started %d times", lines)
	}

	// an app the start closed boots once its file is fixed
	if got := b.get(t, "broken.test", "/").status; got != http.StatusServiceUnavailable {
		t.Fatalf("the broken app: %d", got)
	}
	rewrite(t, folder, "apps/broken/broken.test/router.work", routerImport+`router.get((context) => context.text("fixed"));`+"\n")
	eventually(t, "the app the start closed", func() bool { return text("broken.test", "/") == "fixed" })

	// a file changes: the app boots again, what its storage keeps is kept
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("two!"));`+"\n")
	eventually(t, "the change served", func() bool { return text("shop.test", "/") == "two!" })
	contains(t, "the log", b.log.String(), "[shop] reloaded after shop.test/router.work changed")
	if got := text("shop.test", "/count"); got != "2" {
		t.Errorf("memory() across the reload: %q, want 2", got)
	}

	// a broken file closes the app as a start would; the next change boots it again
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+"router.get(1);\n")
	eventually(t, "the broken boot closing the app", func() bool { return b.get(t, "shop.test", "/").status == http.StatusServiceUnavailable })
	contains(t, "the log", b.log.String(), "[shop] reload after shop.test/router.work changed: the app is closed, its sites answer 503:", "router.get(…) takes the handler")
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("three"));`+"\n")
	eventually(t, "the fixed boot", func() bool { return text("shop.test", "/") == "three" })
	// a change of the same size is a change: the time it was written
	time.Sleep(50 * time.Millisecond)
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("four!"));`+"\n")
	eventually(t, "a change of the same size", func() bool { return text("shop.test", "/") == "four!" })
	// a file that appears, one that goes
	rewrite(t, folder, "apps/shop/shop.test/added/router.work", routerImport+`router.get((context) => context.text("added"));`+"\n")
	eventually(t, "a new route", func() bool { return text("shop.test", "/added") == "added" })
	removeAll(t, filepath.Join(folder, "apps", "shop", "shop.test", "added"))
	eventually(t, "a route gone", func() bool { return b.get(t, "shop.test", "/added").status == http.StatusNotFound })

	// what the app writes as it runs is no change of its code
	reloads := strings.Count(b.log.String(), "reloaded after")
	rewrite(t, folder, "apps/shop/.database/files/note.txt", "written")
	rewrite(t, folder, "apps/shop/.database/storage/x", "written")
	time.Sleep(200 * time.Millisecond)
	if now := strings.Count(b.log.String(), "reloaded after"); now != reloads {
		t.Errorf("a write under .database/ reloaded the app:\n%s", b.log)
	}

	// an app folder that appears boots, a site that goes from it is no more, and so is an app that goes, one the host
	// booted as it started too
	rewrite(t, folder, "apps/other/module.work", routedModule)
	rewrite(t, folder, "apps/other/other.test/router.work", routerImport+`router.get((context) => context.text("other"));`+"\n")
	rewrite(t, folder, "apps/other/more.test/router.work", routerImport+`router.get((context) => context.text("more"));`+"\n")
	rewrite(t, folder, "apps/other/other.test/stream/router.work", "import { memory } from \"kitwork/storage\";\n"+routerImport+
		`router.get((context) => context.sse.subscribe(memory("room")));`+"\n")
	eventually(t, "the new app", func() bool { return text("other.test", "/") == "other" && text("more.test", "/") == "more" })
	contains(t, "the log", b.log.String(), "[other] reloaded after a new app")
	removeAll(t, filepath.Join(folder, "apps", "other", "more.test"))
	eventually(t, "a site gone", func() bool { return b.get(t, "more.test", "/").status == http.StatusNotFound })
	if got := text("other.test", "/"); got != "other" {
		t.Errorf("the site left of the app: %q", got)
	}
	// a stream of the app is open as it goes: it ends, the browser connects again and finds nothing
	request, _ := http.NewRequest(http.MethodGet, b.server.URL+"/stream", nil)
	request.Host = "other.test"
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() {
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		close(ended)
	}()
	removeAll(t, filepath.Join(folder, "apps", "other"))
	eventually(t, "the app gone", func() bool { return b.get(t, "other.test", "/").status == http.StatusNotFound })
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Error("a stream of an app that is gone stays open")
	}
	contains(t, "the log", b.log.String(), "[other] the app's folder is gone")
	removeAll(t, filepath.Join(folder, "apps", "gone"))
	eventually(t, "an app of the start gone", func() bool { return b.get(t, "gone.test", "/").status == http.StatusNotFound })
	if got := text("shop.test", "/"); got != "four!" {
		t.Errorf("another app's change touched shop: %q", got)
	}

	// the manifest is read once: a change is told, not applied
	rewrite(t, folder, "application.work", reloadManifest+"// changed\n")
	eventually(t, "the manifest's line", func() bool { return strings.Contains(b.log.String(), "start it again to apply them") })

	// a closed host reads nothing more
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	said := b.log.String()
	b.host.StartWatching() // nor after Close
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("closed"));`+"\n")
	time.Sleep(200 * time.Millisecond)
	if b.log.String() != said {
		t.Errorf("a closed host reloaded:\n%s", strings.TrimPrefix(b.log.String(), said))
	}
}

// When Close returns, the database files an old boot opened are let go, as the new boot's are: on Windows a file
// still open cannot be removed.
func TestHotReloadLetsTheOldBootsFilesGo(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a file open is told by its removal on Windows")
	}
	folder := write(t, map[string]string{
		"application.work":                reloadManifest,
		"apps/shop/core/schema/shop.work": fileSchema,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("one"));` + "\n",
		"apps/shop/shop.test/count/router.work": "import { db } from \"app/core/schema/shop.work\";\n" + routerImport +
			`router.get((context) => context.text("" + db.count((users) => users.id > 0)));` + "\n",
	})
	writeFile(t, folder,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, name TEXT NOT NULL, price INTEGER NOT NULL, active INTEGER NOT NULL, createdAt TEXT NOT NULL) STRICT`,
		`CREATE UNIQUE INDEX users_email_unique ON users (email)`,
		`CREATE INDEX users_name ON users (name)`,
		kindsTable,
		`INSERT INTO kitwork_columns VALUES ('users','id','integer',0,0,0), ('users','email','text',0,0,0), ('users','name','text',0,0,0),
		 ('users','price','decimal',15,2,0), ('users','active','bool',0,0,0), ('users','createdAt','datetime',0,0,0)`)
	b := boot(t, filepath.Join(folder, "application.work"))
	b.host.ReloadEvery = 20 * time.Millisecond
	b.host.StartWatching()
	b.host.RequestTimeout = time.Minute // the old boot's time does not end before Close
	if got := b.get(t, "shop.test", "/count"); got.body != "0" {
		t.Fatalf("count: %d %q", got.status, got.body)
	}
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("two!"));`+"\n")
	eventually(t, "the reload", func() bool { return b.get(t, "shop.test", "/").body == "two!" })
	if got := b.get(t, "shop.test", "/count"); got.body != "0" {
		t.Fatalf("count after the reload: %d %q", got.status, got.body)
	}
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(folder, "apps", "shop", ".database", "application", "kitwork.db")
	if err := os.Remove(database); err != nil {
		t.Errorf("the database file is still open after Close: %v", err)
	}
}

// The boot a reload puts aside keeps its routines while a request it answered may still run (RequestTimeout), then
// tells them to stop; Close tells them at once.
func TestHotReloadLetsTheOldBootGo(t *testing.T) {
	files := map[string]string{
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("one"));` + "\n",
		"apps/shop/shop.test/work/router.work": "import * as routine from \"kitwork/routine\";\nimport * as file from \"kitwork/file\";\n" + routerImport + `const big = "[" + "0,".repeat(200000) + "0]";
const work = (name, rounds) => {
  for (let i = 0; i < rounds; i++) {
    JSON.parse(big);
    file.open("progress/").segment(name + ".txt").write("" + i);
  }
  file.open("finished/").segment(name + ".txt").write("done");
};
router.get((context) => {
  const name = context.query("name");
  const rounds = Number(context.query("rounds"));
  if (name === "") {
    const began = Date.now();
    for (let i = 0; i < rounds; i++) {
      JSON.parse(big);
    }
    return context.text("" + (Date.now() - began));
  }
  routine.go(() => work(name, rounds));
  return context.text("started");
});
`,
	}
	b, folder := reloading(t, files, longWork) // the long work within its energy (core/runtime, work.go)
	b.host.RequestTimeout = 800 * time.Millisecond
	finished := func(name string) bool {
		_, err := os.Stat(filepath.Join(folder, "apps", "shop", ".database", "files", "finished", name+".txt"))
		return err == nil
	}
	// rounds of about ms milliseconds of work, measured here
	took, _ := strconv.Atoi(b.get(t, "shop.test", "/work?rounds=5").body)
	each := max(took/5, 1)
	rounds := func(ms int) string { return strconv.Itoa(max(ms/each, 1)) }

	// nothing stops it: the long work ends
	b.get(t, "shop.test", "/work?name=control&rounds="+rounds(2500))
	eventually(t, "the long work alone", func() bool { return finished("control") })

	// a reload: the short work ends within the old boot's time, the long one is told to stop after it
	b.get(t, "shop.test", "/work?name=short&rounds="+rounds(250))
	b.get(t, "shop.test", "/work?name=long&rounds="+rounds(2500))
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("two!"));`+"\n")
	eventually(t, "the reload", func() bool { return b.get(t, "shop.test", "/").body == "two!" })
	time.Sleep(3500 * time.Millisecond)
	if !finished("short") {
		t.Error("the short work of the old boot was stopped before the old boot's time")
	}
	if finished("long") {
		t.Error("the long work of the old boot ran on past the old boot's time")
	}

	// Close stops the routines of an old boot at once: the progress of one stands still after it, however fast the
	// machine is
	b.host.RequestTimeout = time.Minute
	b.get(t, "shop.test", "/work?name=closing&rounds="+rounds(600000))
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("three"));`+"\n")
	eventually(t, "the second reload", func() bool { return b.get(t, "shop.test", "/").body == "three" })
	progress := func() string {
		data, _ := os.ReadFile(filepath.Join(folder, "apps", "shop", ".database", "files", "progress", "closing.txt"))
		return string(data)
	}
	eventually(t, "the routine running", func() bool { return progress() != "" })
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	stopped := progress()
	time.Sleep(time.Duration(max(1500, 8*each)) * time.Millisecond)
	if now := progress(); now != stopped {
		t.Errorf("Close let the routine of an old boot run on: round %s, then %s", stopped, now)
	}
}

// A reload stops the app's queue before its next boot claims the same runs, and the next boot's worker runs the
// job's new code. A send while the next boot is made, or from a request of the old boot that ends after it, is
// stored, never refused, and the next worker runs it: the old boot's runs' file stays open until its last request
// has had its time, and then the next worker reads the runs.
func TestHotReloadRestartsTheQueue(t *testing.T) {
	job := func(word string, slow int) string {
		return "import * as queue from \"kitwork/queue\";\nimport { memory } from \"kitwork/storage\";\n" + `const big = "[" + "0,".repeat(200000) + "0]";
for (let i = 0; i < ` + strconv.Itoa(slow) + `; i++) {
  JSON.parse(big);
}
export const tell = queue.job((context) => {
  memory("told").set((memory("told").get() ?? []).concat(["` + word + `"]));
});
`
	}
	b, folder := reloading(t, map[string]string{
		"apps/shop/core/queue/rules.work": "import * as queue from \"kitwork/queue\";\nqueue.parallel(1);\n",
		"apps/shop/core/queue/tell.work":  job("one", 0),
		"apps/shop/shop.test/send/router.work": "import { tell } from \"app/core/queue/tell.work\";\n" + routerImport + `const big = "[" + "0,".repeat(200000) + "0]";
router.get((context) => {
  const began = Date.now();
  for (let i = 0; i < Number(context.query("slow")); i++) {
    JSON.parse(big);
  }
  if (context.query("time") !== "") {
    return context.text("" + (Date.now() - began));
  }
  tell.send({});
  return context.text("sent");
});
`,
		"apps/shop/shop.test/told/router.work": "import { memory } from \"kitwork/storage\";\n" + routerImport + `router.get((context) => context.json(memory("told").get() ?? []));` + "\n",
	}, longWork) // parses that take their time within their energy (core/runtime, work.go)
	b.host.RequestTimeout = 3 * time.Second
	b.host.StartBackground()
	told := func() []string {
		var words []string
		json.Unmarshal([]byte(b.get(t, "shop.test", "/told").body), &words)
		return words
	}
	waitFor := func(what string, count int) []string {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); len(told()) < count; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d of %d: %v\n%s", what, len(told()), count, told(), b.log)
			}
		}
		return told()
	}
	b.get(t, "shop.test", "/send?slow=0")
	waitFor("the first run", 1)

	// the next boot takes a while to make (a loop at the top of the job's file): every send meanwhile is stored
	rewrite(t, folder, "apps/shop/core/queue/tell.work", job("two!", 20))
	sent := 1
	for !strings.Contains(b.log.String(), "reloaded after core/queue/tell.work changed") {
		if got := b.get(t, "shop.test", "/send?slow=0"); got.status != http.StatusOK {
			t.Fatalf("a send as the app reloads: %d %q\n%s", got.status, got.body, b.log)
		}
		sent++
		time.Sleep(50 * time.Millisecond) // a run takes its time: a handful of sends is enough
	}
	for i := 0; i < 4; i++ {
		b.get(t, "shop.test", "/send?slow=0")
		sent++
	}
	words := waitFor("the runs around the reload", sent)
	first := slices.Index(words, "two!")
	if first < 0 || words[len(words)-1] != "two!" || slices.Contains(words[first:], "one") {
		t.Errorf("the runs ran %v: an old worker ran after the new one began", words)
	}

	// a request of the old boot sends after the next boot took its place: the run waits for the old boot's time to end,
	// then the next worker runs it
	took, _ := strconv.Atoi(b.get(t, "shop.test", "/send?slow=5&time=1").body)
	slow := strconv.Itoa(max(1000/max(took/5, 1), 1)) // about a second of work before the send
	late := make(chan reply, 1)
	go func() { late <- b.get(t, "shop.test", "/send?slow="+slow) }()
	time.Sleep(50 * time.Millisecond)
	rewrite(t, folder, "apps/shop/core/queue/tell.work", job("three", 0))
	if got := <-late; got.status != http.StatusOK {
		t.Fatalf("the late send: %d %q\n%s", got.status, got.body, b.log)
	}
	if !strings.Contains(b.log.String(), "[shop] reloaded after core/queue/tell.work changed") || strings.Count(b.log.String(), "reloaded after") < 2 {
		t.Fatalf("the late send ended before the reload:\n%s", b.log)
	}
	words = waitFor("the late run", sent+1)
	if last := words[len(words)-1]; last != "three" {
		t.Errorf("the late run ran %q, want the next boot's three: %v", last, words)
	}
}

// A change boots its app once two reads in a row agree on it: a file written again and again faster than the reads
// boots nothing while it is written, and once, after.
func TestHotReloadWaitsForAFileToRest(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                reloadManifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("zero"));` + "\n",
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	b.host.ReloadEvery = 200 * time.Millisecond
	b.host.StartWatching()
	for i := 0; i < 40; i++ { // 800 ms of writes, one every 20 ms: no two reads 200 ms apart agree
		rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("`+strings.Repeat("x", i)+`"));`+"\n")
		time.Sleep(20 * time.Millisecond)
	}
	if reloads := strings.Count(b.log.String(), "reloaded after"); reloads != 0 {
		t.Errorf("a file still being written booted its app %d times", reloads)
	}
	eventually(t, "the boot once the file rests", func() bool { return strings.Count(b.log.String(), "reloaded after") == 1 })
	time.Sleep(500 * time.Millisecond)
	if reloads := strings.Count(b.log.String(), "reloaded after"); reloads != 1 {
		t.Errorf("a file at rest booted its app %d times", reloads)
	}
	if got := b.get(t, "shop.test", "/").body; got != strings.Repeat("x", 39) {
		t.Errorf("the boot read %q, not the file at rest", got)
	}
}

// Without app.hotReload(true), as with app.hotReload(false), the apps are read once; a manifest of no website has no
// app to boot again.
func TestHotReloadIsOffUnlessWritten(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                "import * as app from \"kitwork/application\";\napp.root(\"apps\");\napp.desktop({ name: \"Shop\" });\napp.hotReload(true);\n",
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("one"));` + "\n",
	})
	log := &lines{}
	desktop, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := desktop.Boot(); err != nil {
		t.Fatal(err)
	}
	desktop.ReloadEvery = 20 * time.Millisecond
	desktop.StartWatching()
	time.Sleep(150 * time.Millisecond)
	desktop.Close()
	if strings.Contains(log.String(), "reload") {
		t.Errorf("a manifest of no website reloaded:\n%s", log)
	}

	for _, manifest := range []string{manifest, strings.Replace(reloadManifest, "app.hotReload(true)", "app.hotReload(false)", 1)} {
		b, folder := reloading(t, map[string]string{
			"application.work":                manifest,
			"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("one"));` + "\n",
		})
		rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("two!"));`+"\n")
		time.Sleep(200 * time.Millisecond)
		if got := b.get(t, "shop.test", "/").body; got != "one" {
			t.Errorf("a host without hot reload reloaded: %q", got)
		}
		if strings.Contains(b.log.String(), "hot reload") {
			t.Errorf("a host without hot reload said it watches:\n%s", b.log)
		}
	}
}

// Serve, once the sites listen, reads the apps' files.
func TestServeWatchesTheApps(t *testing.T) {
	port := freePort(t)
	folder := write(t, map[string]string{
		// allowLocal: no AutoSSL, whose ports 80 and 443 another test may hold
		"application.work": strings.Replace(reloadManifest, `app.website({ hostname: "shop.test", port: 8080 });`,
			`app.website({ hostname: "shop.test", port: `+strconv.Itoa(port)+`, allowLocal: true });`, 1),
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("one"));` + "\n",
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	read.ReloadEvery = 20 * time.Millisecond
	t.Cleanup(func() { _ = read.Close() })
	go read.Serve()
	eventually(t, "Serve watching", func() bool { return strings.Contains(log.String(), "hot reload:") })
	rewrite(t, folder, "apps/shop/shop.test/router.work", routerImport+`router.get((context) => context.text("two!"));`+"\n")
	eventually(t, "a reload under Serve", func() bool {
		return strings.Contains(log.String(), "[shop] reloaded after shop.test/router.work changed")
	})
}

func TestHotReloadIsWrittenOnce(t *testing.T) {
	for written, want := range map[string]string{
		"app.hotReload(true);\napp.hotReload(true);": "app.hotReload(…) is declared twice",
		`app.hotReload("yes");`:                      "app.hotReload(…) takes true or false",
		"app.hotReload();":                           "app.hotReload(…) takes true or false",
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

// A site reads its pages for its boot (10/11/2026, library/ui kept.go): a content.ui changed on the disk is served once
// the app boots again, soon with hot reload, never without it.
func TestAPageChangedIsServedAtTheNextBoot(t *testing.T) {
	files := func(manifest string) map[string]string {
		return map[string]string{
			"application.work":                manifest,
			"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.view({}));\n",
			"apps/shop/shop.test/content.ui":  "<content><text>một</text></content>",
		}
	}
	page := func(b *booted) string { return b.get(t, "shop.test", "/").body }

	b, folder := reloading(t, files(reloadManifest))
	contains(t, "the page", page(b), "một")
	rewrite(t, folder, "apps/shop/shop.test/content.ui", "<content><text>hai</text></content>")
	eventually(t, "the changed page", func() bool { return strings.Contains(page(b), "hai") })

	b, folder = reloading(t, files(strings.Replace(reloadManifest, "app.hotReload(true)", "app.hotReload(false)", 1)))
	contains(t, "the page", page(b), "một")
	rewrite(t, folder, "apps/shop/shop.test/content.ui", "<content><text>hai</text></content>")
	time.Sleep(200 * time.Millisecond)
	if got := page(b); !strings.Contains(got, "một") || strings.Contains(got, "hai") {
		t.Errorf("a host without hot reload served the changed page before its next boot:\n%.400s", got)
	}
}
