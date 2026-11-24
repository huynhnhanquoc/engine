package host_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// storageImport is the imports of a router.work that keeps values.
const storageImport = "import { memory, disk, datastore } from \"kitwork/storage\";\nimport * as http from \"kitwork/http\";\n" + routerImport

// storageFiles are the folders of an app whose site keeps values, U/ the upstream's address; its root's config.work
// sets the zone a calendar expiry is read in.
func storageFiles(up string) map[string]string {
	site := "apps/shop/shop.test/"
	source := func(text string) string { return storageImport + strings.ReplaceAll(text, "U/", up+"/") }
	return map[string]string{
		"application.work":      manifest,
		"apps/shop/module.work": zonedModule("Asia/Ho_Chi_Minh"),
		site + "values/router.work": source(`const users = memory("users");
router.get((context) => {
  users.segment("a:b").set("one");
  users.segment("a", "b").set("two");
  const profile = users.segment(123, "profile");
  profile.set({ name: "Kitwork", tags: ["a", "b"], nested: { z: 1, a: 2 }, nothing: null });
  memory("empty").set(0);
  const read = profile.get() ?? {};
  read.name = "changed";
  return context.json({
    one: users.segment("a:b").get(), two: users.segment("a", "b").get(),
    oneKey: users.segment("a:b").key(), key: profile.key(), keys: profile.keys(), name: profile.name(),
    again: profile.get()?.name, order: Object.keys(profile.get()?.nested ?? {}),
    zeroKept: [memory("empty").get(), memory("empty").exists()], absent: [memory("never").get(), memory("never").exists()],
    ttlNone: profile.ttl(), deleted: [profile.delete(), profile.delete(), profile.get()], places: [disk("users").segment("a:b").get()]
  });
});
`),
		site + "expire/router.work": source(`router.get((context) => {
  memory("short").expire(150).set(1);
  memory("span").expire("2h").set(1);
  memory("days").expire("2d").set(1);
  memory("calendar").expire("daily 00:00").set(1);
  return context.json({ short: memory("short").ttl(), span: memory("span").ttl(), days: memory("days").ttl(), calendar: memory("calendar").ttl() });
});
`),
		site + "expire/later/router.work": source(`router.get((context) => context.json({ short: [memory("short").get(), memory("short").exists(), memory("short").ttl()] }));
`),
		site + "compute/router.work": source(`router.get((context) => {
  const first = memory("quote").expire("1h").get(() => ({ token: Math.random() }));
  const second = memory("quote").expire("1h").get(() => ({ token: Math.random() }));
  const failed = safe(() => memory("bad").get(() => error("no")));
  const self = safe(() => memory("self").get(() => memory("self").get(() => 1)));
  const nothing = [memory("nothing").get(() => null), memory("nothing").exists()];
  return context.json({ same: first.token === second.token, failed: failed.error?.code, kept: memory("bad").exists(),
    self: self.error?.code, selfSays: self.error?.message, nothing: nothing });
});
`),
		site + "flight/router.work": source(`router.get((context) => context.json(memory("slow").get(() => {
  memory("runs").set((memory("runs").get() ?? 0) + 1);
  http.get("U/slow?ms=300").send();
  return { done: true };
})));
`),
		site + "flight/runs/router.work": source(`router.get((context) => context.json({ runs: memory("runs").get() }));
`),
		site + "cross/a/router.work": source(`router.get((context) => {
  const got = safe(() => memory("A").get(() => { http.get("U/slow?ms=300").send(); return memory("B").get(() => "b from a"); }));
  return context.json({ code: got.error === null ? null : got.error?.code, value: got.error === null ? got.value : null });
});
`),
		site + "cross/b/router.work": source(`router.get((context) => {
  const got = safe(() => memory("B").get(() => { http.get("U/slow?ms=300").send(); return memory("A").get(() => "a from b"); }));
  return context.json({ code: got.error === null ? null : got.error?.code, value: got.error === null ? got.value : null });
});
`),
		site + "kept/router.work": source(`router.get((context) => {
  const place = context.query("place") === "disk" ? disk("kept") : datastore("kept");
  if (context.query("write") === "yes") {
    place.segment("n").set({ n: 1, at: "first" });
  }
  return context.json({ value: place.segment("n").get(), never: place.segment("never").get() });
});
`),
		site + "clear/router.work": source(`router.get((context) => {
  const places = { memory: memory, disk: disk, datastore: datastore };
  const out = {};
  for (const name of ["memory", "disk", "datastore"]) {
    const place = places[name];
    place("carts").set("root");
    place("carts").segment(1).set("one");
    place("carts").segment(2, "items").set("two");
    place("cartsX").set("another name");
    place("other").segment("carts").set("another branch");
    const cleared = place("carts").segment(2).clear();
    const all = place("carts").clear();
    out[name] = [cleared, all, place("carts").get(), place("carts").segment(1).get(), place("cartsX").get(),
      place("other").segment("carts").get(), place("nothing").clear()];
  }
  return context.json(out);
});
`),
		site + "send/router.work": source(`const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
const room = memory("rooms").segment("lobby");
router.get((context) => context.json({
  fine: code(() => room.send({ event: "chat", id: "1", data: { text: "hi" } })),
  bare: code(() => room.send({ data: 1 })),
  extra: code(() => room.send({ data: 1, text: "hi" })),
  noData: code(() => room.send({ event: "chat" })),
  eventNumber: code(() => room.send({ event: 1, data: 1 })),
  lineBreak: code(() => room.send({ id: "a\nb", data: 1 })),
  notObject: code(() => room.send("hi")),
  badData: code(() => room.send({ data: () => 1 })),
  observe: code(() => room.observe((message) => message)),
  notObjectSays: safe(() => room.send("hi")).error?.message
}));
`),
		site + "refused/router.work": source(`const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
router.get((context) => context.json({
  fn: code(() => memory("x").set(() => 1)),
  fnSays: safe(() => memory("x").set({ a: [() => 1] })).error?.message,
  request: code(() => memory("x").set(context.request)),
  big: code(() => memory("x").set("x".repeat(1100000))),
  emptyName: code(() => memory("")),
  numberName: code(() => memory(1)),
  emptySegment: code(() => memory("x").segment("")),
  nullSegment: code(() => memory("x").segment(null)),
  objectSegment: code(() => memory("x").segment({})),
  noSegment: code(() => memory("x").segment()),
  longKey: code(() => memory("x").segment("y".repeat(5000))),
  getNumber: code(() => memory("x").get(1)),
  setNothing: code(() => memory("x").set()),
  existsArgument: code(() => memory("x").exists(1)),
  word: code(() => memory("x").nope()),
  zero: code(() => memory("x").expire(0)),
  soon: code(() => memory("x").expire("soon")),
  hour: code(() => memory("x").expire("nextday 25:00")),
  nullValue: code(() => memory("x").set(null)),
  nullSays: safe(() => memory("x").set(null)).error?.message,
  undefinedValue: code(() => memory("x").set(undefined)),
  halfSegment: code(() => memory("x").segment(1.5)),
  fine: code(() => memory("x").expire("nextday 03:00").set(1))
}));
`),
	}
}

func storageSite(t *testing.T, files map[string]string) (*booted, string) {
	t.Helper()
	folder := write(t, files)
	return boot(t, filepath.Join(folder, "application.work")), folder
}

// A handle names a key (mục 3, 4): segment() builds it a segment at a time, "a:b" never meeting "a", "b", a number
// written as text; name, key, keys read it. A value comes back as written, a copy each read, an object's keys in
// order; null kept is not no value (exists); delete says whether there was one; one name in two places is two keys.
func TestStorageKeepsValuesByKey(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/values")
	want := `{"absent":[null,false],"again":"Kitwork","deleted":[true,false,null],"key":"users:123:profile","keys":["123","profile"],` +
		`"name":"users","one":"one","oneKey":"users:a:b","order":["z","a"],"places":[null],"ttlNone":null,"two":"two","zeroKept":[0,true]}`
	if shown(got) != want {
		t.Errorf("values: %s\nwant    %s", shown(got), want)
	}
}

// expire() sets the expiry of the next write (mục 6): a number of milliseconds, a span, a time of the calendar in the
// app's zone (router.timezone of its root); ttl() is what is left, in milliseconds; past it the value is gone.
func TestStorageExpires(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/expire")
	if short := got["short"].(float64); short <= 0 || short > 150 {
		t.Errorf("expire(150) ttl %v", short)
	}
	if span := got["span"].(float64); span <= 7_190_000 || span > 7_200_000 {
		t.Errorf("expire(2h) ttl %v", span)
	}
	if days := got["days"].(float64); days <= 172_790_000 || days > 172_800_000 {
		t.Errorf("expire(2d) ttl %v", days)
	}
	zone, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := time.Now().In(zone)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone).AddDate(0, 0, 1)
	if left := time.Duration(got["calendar"].(float64)) * time.Millisecond; (midnight.Sub(now) - left).Abs() > 5*time.Second {
		t.Errorf("expire(\"daily 00:00\") ttl %v, want %v to midnight in Asia/Ho_Chi_Minh", left, midnight.Sub(now))
	}
	time.Sleep(200 * time.Millisecond)
	if later := fetchJSON(t, b, "/expire/later"); shown(later["short"]) != "[null,false,null]" {
		t.Errorf("past its expiry: %s", shown(later["short"]))
	}
}

// get(() => …) reads, or runs the function once and keeps its result (mục 6): a second read finds it; a function that
// fails keeps nothing and its failure rises; the function reading its own key with get(fn) would wait for itself.
func TestStorageGetOrCompute(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/compute")
	if got["same"] != true || got["failed"] != "FAIL" || got["kept"] != false || got["self"] != "INVALID_ARGUMENT" || shown(got["nothing"]) != "[null,false]" {
		t.Errorf("compute: %s", shown(got))
	}
	contains(t, "the wait for itself", got["selfSays"].(string), "would wait for itself")
}

// Requests that miss a key at once run its function once (mục 6): the others wait and read what it kept.
func TestStorageGetOrComputeRunsOnceForAll(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if got := b.get(t, "shop.test", "/flight"); got.body != `{"done":true}` {
				t.Errorf("/flight: %d %s", got.status, got.body)
			}
		}()
	}
	group.Wait()
	if runs := fetchJSON(t, b, "/flight/runs"); runs["runs"] != float64(1) {
		t.Errorf("four requests at once ran the function %v times", runs["runs"])
	}
}

// Two get(fn) nested in opposite orders in two requests do not wait for each other for ever: the one that would close
// the circle fails CONFLICT, the other goes on (mục 11).
func TestStorageCrossedComputesDoNotWaitForEver(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	results := make([]map[string]any, 2)
	var group sync.WaitGroup
	for i, path := range []string{"/cross/a", "/cross/b"} {
		group.Add(1)
		go func() {
			defer group.Done()
			results[i] = fetchJSON(t, b, path)
		}()
	}
	started := time.Now()
	group.Wait()
	conflicts := 0
	for _, result := range results {
		if result["code"] == "CONFLICT" {
			conflicts++
		}
	}
	if conflicts != 1 || time.Since(started) > 5*time.Second {
		t.Errorf("crossed: %s after %v, want one CONFLICT soon", shown(results), time.Since(started))
	}
}

// disk() and datastore() live across a restart, each in the app's .database/ (mục 2): the first engine's layout, a
// file a key under .database/disk/, a row of kitwork_storage in .database/storage/kitwork.db; a read of a place never
// written makes nothing.
func TestStorageDiskAndDatastoreLiveAcrossARestart(t *testing.T) {
	up, _ := slowUp(t)
	files := storageFiles(up.URL)
	folder := write(t, files)
	app := filepath.Join(folder, "apps", "shop")
	first := boot(t, filepath.Join(folder, "application.work"))
	for _, place := range []string{"disk", "datastore"} {
		if got := fetchJSON(t, first, "/kept?place="+place); shown(got) != `{"never":null,"value":null}` {
			t.Errorf("%s before a write: %s", place, shown(got))
		}
	}
	if _, err := os.Stat(filepath.Join(app, ".database")); !os.IsNotExist(err) {
		t.Errorf("reads made .database: %v", err)
	}
	for _, place := range []string{"disk", "datastore"} {
		fetchJSON(t, first, "/kept?place="+place+"&write=yes")
	}
	if err := first.host.Close(); err != nil {
		t.Fatal(err)
	}
	// Close lets the file go: on Windows a folder holding an open file does not move
	stored := filepath.Join(app, ".database", "storage")
	if err := os.Rename(stored, stored+"-moved"); err != nil {
		t.Errorf("the datastore() file is still held after Close: %v", err)
	} else if err := os.Rename(stored+"-moved", stored); err != nil {
		t.Fatal(err)
	}
	disk, _ := filepath.Glob(filepath.Join(app, ".database", "disk", "*", "*"))
	if len(disk) != 1 {
		t.Errorf("disk() files %v", disk)
	}
	if _, err := os.Stat(filepath.Join(app, ".database", "storage", "kitwork.db")); err != nil {
		t.Errorf("datastore() file: %v", err)
	}
	again := boot(t, filepath.Join(folder, "application.work"))
	for _, place := range []string{"disk", "datastore"} {
		if got := fetchJSON(t, again, "/kept?place="+place); shown(got["value"]) != `{"at":"first","n":1}` && shown(got["value"]) != `{"n":1,"at":"first"}` {
			t.Errorf("%s after a restart: %s", place, shown(got))
		}
	}
}

// clear() drops the value of a key and of every key under it, in each place (engine2/docs/storage.md mục 2), and
// says how many lived; a key of another name or another branch stays.
func TestStorageClearsABranch(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/clear")
	for _, place := range []string{"memory", "disk", "datastore"} {
		if shown(got[place]) != `[1,2,null,null,"another name","another branch",0]` {
			t.Errorf("%s: %s", place, shown(got[place]))
		}
	}
}

// send(…) takes { event, id, data } and nothing else (mục 7); observe(…) in a request keeps its observer until the
// request ends, which runs its function only while it answers with its stream (sse_test.go).
func TestStorageSendChecksTheEnvelope(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/send")
	contains(t, "a message not an object", got["notObjectSays"].(string), "string here, send({ data: … })")
	delete(got, "notObjectSays")
	want := `{"badData":"INVALID_ARGUMENT","bare":null,"eventNumber":"INVALID_ARGUMENT","extra":"INVALID_ARGUMENT","fine":null,` +
		`"lineBreak":"INVALID_ARGUMENT","noData":"INVALID_ARGUMENT","notObject":"INVALID_ARGUMENT","observe":null}`
	if shown(got) != want {
		t.Errorf("send: %s\nwant  %s", shown(got), want)
	}
}

// What is refused (mục 4, 5, 6): a value JSON does not write, named where it is; a value past 1 MiB; a name or a
// segment that is no string or number, a key past 4096 bytes; an expiry that reads as none; a word a handle lacks.
func TestStorageRefuses(t *testing.T) {
	up, _ := slowUp(t)
	b, _ := storageSite(t, storageFiles(up.URL))
	got := fetchJSON(t, b, "/refused")
	contains(t, "a function in a value", got["fnSays"].(string), "cannot keep a function at .a[0]")
	contains(t, "null", got["nullSays"].(string), "null is no value to keep", ".delete() removes the key")
	delete(got, "fnSays")
	delete(got, "nullSays")
	want := `{"big":"INVALID_ARGUMENT","emptyName":"INVALID_ARGUMENT","emptySegment":"INVALID_ARGUMENT","existsArgument":"INVALID_ARGUMENT",` +
		`"fine":null,"fn":"INVALID_ARGUMENT","getNumber":"INVALID_ARGUMENT","halfSegment":"INVALID_ARGUMENT","hour":"INVALID_ARGUMENT",` +
		`"longKey":"INVALID_ARGUMENT","noSegment":"INVALID_ARGUMENT","nullSegment":"INVALID_ARGUMENT","nullValue":"INVALID_ARGUMENT",` +
		`"numberName":"INVALID_ARGUMENT","objectSegment":"INVALID_ARGUMENT","request":"INVALID_ARGUMENT","setNothing":"INVALID_ARGUMENT",` +
		`"soon":"INVALID_ARGUMENT","undefinedValue":"INVALID_ARGUMENT","word":"TYPE","zero":"INVALID_ARGUMENT"}`
	if shown(got) != want {
		t.Errorf("refused: %s\nwant     %s", shown(got), want)
	}
}

// A file declares as the site boots (mục 9): a handle is made at its top level, a read or a write there is a boot
// error; the manifest keeps nothing.
func TestStorageAtTheTopLevel(t *testing.T) {
	said := bootError(t, map[string]string{
		"apps/shop/shop.test/router.work": storageImport + "const flags = datastore(\"flags\");\nflags.get();\nrouter.get((context) => context.text(\"x\"));\n",
	})
	contains(t, "the boot", said, "datastore(\"flags\").get(…) at the top level of a file")
	said = bootError(t, map[string]string{
		"application.work": "import { memory } from \"kitwork/storage\";\n" + manifest,
	})
	contains(t, "the manifest", said, "kitwork/storage is an app's")
}

// A .database of the app that leads elsewhere through a link is refused, DENIED, not read or written there (mục 9).
func TestStorageRefusesALinkOutOfTheApp(t *testing.T) {
	up, _ := slowUp(t)
	files := storageFiles(up.URL)
	folder := write(t, files)
	elsewhere := t.TempDir()
	junction(t, filepath.Join(folder, "apps", "shop", ".database"), elsewhere)
	b := boot(t, filepath.Join(folder, "application.work"))
	for _, place := range []string{"disk", "datastore"} {
		got := b.get(t, "shop.test", "/kept?place="+place+"&write=yes")
		if got.status != http.StatusInternalServerError || !strings.Contains(b.log.String(), "DENIED") {
			t.Errorf("%s through a link: %d %s\n%s", place, got.status, got.body, b.log)
		}
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("written through the link: %v", entries)
	}
}
