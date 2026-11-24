package host_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/library/file"
	"github.com/kitwork/engine2/library/router"
)

// fileImport is the imports of a router.work that reads and writes the app's files.
const fileImport = "import * as file from \"kitwork/file\";\n" + routerImport

// filesApp boots an app "shop" whose site is shop.test, with files written at their paths (each relative to the
// app's folder, apps/shop/), and gives it served with the app's folder.
func filesApp(t *testing.T, files map[string]string, set ...func(*host.Host)) (*booted, string) {
	t.Helper()
	all := map[string]string{"application.work": manifest}
	for name, text := range files {
		all["apps/shop/"+name] = text
	}
	folder := write(t, all)
	return boot(t, filepath.Join(folder, "application.work"), set...), filepath.Join(folder, "apps", "shop")
}

// answered is the JSON a route answered, read into a map.
func answered(t *testing.T, got reply) map[string]any {
	t.Helper()
	if got.status != http.StatusOK {
		t.Fatalf("status %d: %s", got.status, got.body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got.body), &out); err != nil {
		t.Fatalf("%v: %s", err, got.body)
	}
	return out
}

// same checks a field of an answer against what it should be, written as JSON.
func same(t *testing.T, out map[string]any, field, want string) {
	t.Helper()
	got, _ := json.Marshal(out[field])
	if string(got) != want {
		t.Errorf("%s = %s, want %s", field, got, want)
	}
}

func TestFileReadsBothZones(t *testing.T) {
	b, app := filesApp(t, map[string]string{
		".env":                           "SECRET=1\n",
		"shop.test/data/menu.json":       `[{ "name": "Cà phê" }]`,
		"shop.test/data/.env.local":      "X=1\n",
		".database/files/settings.json":  `{ "theme": "dark" }`,
		".database/files/notes.md":       "xin chào",
		".database/files/broken.json":    `{ "a": `,
		".database/files/latin.txt":      string([]byte{0x41, 0xff, 0xfe}),
		".database/files/photos/a.png":   "PNG",
		".database/files/photos/b/c.txt": "deep",
		"shop.test/read/router.work": fileImport + `router.get((context) => {
  const code = (read) => { const out = safe(read); return out.error ? out.error?.code : "ok"; };
  return context.json({
    theme: file.read("settings.json")?.theme, raw: file.text("settings.json"), note: file.read("notes.md"),
    jsonOfJson: file.json("settings.json")?.theme, menu: file.json("app/shop.test/data/menu.json")?.[0].name,
    missing: [file.read("nope.json"), file.text("nope.txt"), file.bytes("nope.png"), file.json("nope.json"), file.info("nope.txt"), file.exists("nope.txt")],
    there: [file.exists("notes.md"), file.exists("photos/"), file.info("photos/")?.type, file.info("notes.md")?.size, file.info("notes.md")?.extension],
    listed: file.list("photos/").map((entry) => entry.path),
    codeListed: file.list("app/shop.test/data/").map((entry) => entry.name),
    denied: [code(() => file.text("app/.env")), code(() => file.text("app/.ENV")), code(() => file.json("app/.database/files/settings.json")), code(() => file.text("app/shop.test/data/.env.local"))],
    invalid: [code(() => file.text("/notes.md")), code(() => file.text("../notes.md")), code(() => file.text("a//b.txt")), code(() => file.text("App/x.txt")), code(() => file.text("")), code(() => file.text(1))],
    input: [code(() => file.json("broken.json")), code(() => file.text("latin.txt"))],
    conflict: [code(() => file.list("notes.md")), code(() => file.text("photos/"))],
    old: [code(() => file.write("a.txt", "x")), code(() => file.save("a.txt", "x")), code(() => file.base64("a.png"))],
    oldSays: safe(() => file.copy("a", "b")).error?.message, numberSays: safe(() => file.text(1)).error?.message, removeSays: safe(() => file.remove("tmp/")).error?.message,
    pngIsText: typeof file.read("photos/a.png") === "string", brokenSays: safe(() => file.json("broken.json")).error?.message,
    times: [file.info("notes.md")?.createdAt !== null, file.info("notes.md")?.modifiedAt.endsWith("Z")]
  });
});
`,
	})
	if _, err := os.Stat(filepath.Join(app, ".database", "files", "nope.txt")); !os.IsNotExist(err) {
		t.Fatal("a read made a file")
	}
	out := answered(t, b.get(t, "shop.test", "/read"))
	same(t, out, "theme", `"dark"`)
	same(t, out, "raw", `"{ \"theme\": \"dark\" }"`)
	same(t, out, "note", `"xin chào"`)
	same(t, out, "jsonOfJson", `"dark"`)
	same(t, out, "menu", `"Cà phê"`)
	same(t, out, "missing", `[null,null,null,null,null,false]`)
	same(t, out, "there", `[true,true,"directory",9,".md"]`)
	same(t, out, "listed", `["photos/a.png","photos/b/"]`)
	same(t, out, "codeListed", `["menu.json"]`)
	same(t, out, "denied", `["DENIED","DENIED","DENIED","DENIED"]`)
	same(t, out, "invalid", `["INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT"]`)
	same(t, out, "input", `["INVALID_INPUT","INVALID_INPUT"]`)
	same(t, out, "conflict", `["CONFLICT","CONFLICT"]`)
	same(t, out, "old", `["TYPE","TYPE","TYPE"]`)
	contains(t, "file.copy's failure", out["oldSays"].(string), `file.open("notes.txt").copy(đích)`)
	contains(t, "file.remove's failure", out["removeSays"].(string), "clear() rồi delete()")
	contains(t, "file.text(1)'s failure", out["numberSays"].(string), "nhận một đường dẫn là chuỗi")
	same(t, out, "pngIsText", `false`)
	contains(t, "broken.json's failure", out["brokenSays"].(string), "broken.json không phải JSON")
	// a time of creation is the system's: Windows keeps one, Linux and macOS give null (mục 16)
	if runtime.GOOS == "windows" {
		same(t, out, "times", `[true,true]`)
	} else {
		same(t, out, "times", `[false,true]`)
	}
}

func TestFileChangesThroughAHandle(t *testing.T) {
	b, app := filesApp(t, map[string]string{
		"shop.test/data/menu.json": `[1, 2]`,
		"shop.test/latin/router.work": fileImport + `router.post((context) => {
  const body = context.request.text();
  const code = (change) => { const out = safe(change); return out.error ? out.error?.code : "ok"; };
  return context.json({ text: code(() => file.open("l.txt").write(body)), json: code(() => file.open("l.json").write({ body: body })) });
});
`,
		"shop.test/change/router.work": fileImport + `router.post((context) => {
  const code = (change) => { const out = safe(change); return out.error ? out.error?.code : "ok"; };
  const notes = file.open("notes/");
  const untouched = file.open("never/").segment("there.txt");
  const made = [file.open("reports/").create().type, file.open("empty.txt").create().size];
  file.open("kept.txt").write("giữ");
  const again = [file.open("kept.txt").create().size, file.text("kept.txt")];
  const json = file.open("x/y/z.json").write({ a: 1 });
  const text = file.open("a.txt").write("Xin chào");
  const raw = file.open("blob").write(file.bytes("a.txt"));
  const cycle = { a: 1 };
  cycle.self = cycle;
  const wrong = [code(() => file.open("b.txt").write({ a: 1 })), code(() => file.open("b.png").write("PNG")),
    code(() => file.open("b.png").write("data:image/png;base64,AAAA")), code(() => file.open("b.json").write(NaN)),
    code(() => file.open("b.json").write(cycle)), code(() => file.open("b.json").write(file.bytes("a.txt"))),
    code(() => file.open("app/b.png").write("x")), code(() => file.open("notes/").write("x")), code(() => file.open("b.txt").write())];
  let deep = {};
  for (let i = 0; i < 10001; i++) {
    deep = { a: deep };
  }
  let wide = [1];
  for (let i = 0; i < 50; i++) {
    wide = [wide, wide];
  }
  const says = {
    segEmpty: safe(() => notes.segment("")).error?.message, remove: safe(() => notes.remove()).error?.message,
    data: safe(() => file.open("b.png").write("data:image/png;base64,AAAA")).error?.message,
    cycle: safe(() => file.open("b.json").write(cycle)).error?.message, deep: safe(() => file.open("b.json").write(deep)).error?.message,
    bytes: safe(() => file.open("b.json").write(file.bytes("a.txt"))).error?.message,
    wide: safe(() => file.open("b.json").write(wide)).error?.message, wideCode: safe(() => file.open("b.json").write(wide)).error?.code
  };
  notes.segment("1.txt").write("one");
  notes.segment(2, "deep.txt").write("two");
  const listed = notes.list().map((entry) => entry.path);
  const segments = [code(() => notes.segment("a/b")), code(() => notes.segment("..")), code(() => notes.segment("")),
    code(() => notes.segment(1.5)), code(() => file.open("a.txt").segment("x")), code(() => notes.segment())];
  const named = [notes.segment(2, "deep.txt").name(), notes.segment(2, "deep.txt").path(), code(() => notes.name("x"))];
  const read = notes.segment("1.txt").delete;
  const stillThere = file.exists("notes/1.txt");
  const full = code(() => notes.delete());
  const copied = notes.segment("1.txt").copy("backup/old/1.txt");
  const copyAgain = code(() => notes.segment("1.txt").copy("backup/old/1.txt"));
  const toHandle = notes.segment("1.txt").copy(file.open("backup/").segment("h.txt")).path;
  const moveCode = [code(() => file.open("app/shop.test/data/menu.json").move("m.json")), code(() => file.open("kept.txt").move("app/k.txt"))];
  const fromCode = file.open("app/shop.test/data/menu.json").copy("menu.json").size;
  const toCode = code(() => file.open("a.txt").copy("app/a.txt"));
  const source = file.open("a.txt");
  const moved = source.move("archive/a.txt");
  const moveAgain = code(() => file.open("kept.txt").move("archive/a.txt"));
  const missing = code(() => file.open("nope.txt").copy("x.txt"));
  const cleared = notes.clear();
  const deleted = [notes.delete(), notes.delete(), file.open("nope.txt").delete()];
  return context.json({
    untouched: [untouched.exists(), file.exists("never/")], made: made, again: again,
    json: [json.size, json.type, json.extension, file.read("x/y/z.json")?.a], text: [text.size, text.name, text.path], raw: raw.size,
    wrong: wrong, listed: listed, segments: segments, named: named, stillThere: stillThere, read: typeof read,
    full: full, copied: [copied.path, file.text("backup/old/1.txt")], copyAgain: copyAgain, fromCode: fromCode, toCode: toCode,
    moved: [moved.path, source.path(), source.exists(), file.text("archive/a.txt")], moveAgain: moveAgain, missing: missing,
    cleared: cleared, deleted: deleted,
    oldHandle: safe(() => notes.remove()).error?.code, says: says, toHandle: toHandle, moveCode: moveCode
  });
});
`,
	})
	out := answered(t, b.request(t, http.MethodPost, "shop.test", "/change", nil, "Origin", "http://shop.test"))
	same(t, out, "untouched", `[false,false]`)
	same(t, out, "made", `["directory",0]`)
	same(t, out, "again", `[5,"giữ"]`)
	same(t, out, "json", `[7,"file",".json",1]`)
	same(t, out, "text", `[9,"a.txt","a.txt"]`)
	same(t, out, "raw", `9`)
	same(t, out, "wrong", `["INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","DENIED","CONFLICT","INVALID_ARGUMENT"]`)
	same(t, out, "listed", `["notes/1.txt","notes/2/"]`)
	same(t, out, "segments", `["INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT","INVALID_ARGUMENT"]`)
	same(t, out, "named", `["deep.txt","notes/2/deep.txt","INVALID_ARGUMENT"]`)
	same(t, out, "stillThere", `true`)
	same(t, out, "read", `"function"`)
	same(t, out, "full", `"CONFLICT"`)
	same(t, out, "copied", `["backup/old/1.txt","one"]`)
	same(t, out, "copyAgain", `"CONFLICT"`)
	same(t, out, "fromCode", `6`)
	same(t, out, "toCode", `"DENIED"`)
	same(t, out, "moved", `["archive/a.txt","a.txt",false,"Xin chào"]`)
	same(t, out, "moveAgain", `"CONFLICT"`)
	same(t, out, "missing", `"NOT_FOUND"`)
	same(t, out, "cleared", `3`)
	same(t, out, "deleted", `[true,false,false]`)
	same(t, out, "oldHandle", `"TYPE"`)
	same(t, out, "toHandle", `"backup/h.txt"`)
	same(t, out, "moveCode", `["DENIED","DENIED"]`)
	says, _ := out["says"].(map[string]any)
	for field, want := range map[string]string{"segEmpty": "chuỗi rỗng", "remove": "clear() rồi delete()", "data": `"data:…"`,
		"cycle": "chứa chính nó", "deep": "lồng quá 10000 tầng", "bytes": "bytes và upload ghi vào đuôi nhị phân",
		"wide": `"b.json": giá trị viết thành JSON quá 8 MiB, json() không đọc lại được file này`, "wideCode": "TOO_LARGE"} {
		said, _ := says[field].(string)
		contains(t, field, said, want)
	}
	out = answered(t, b.request(t, http.MethodPost, "shop.test", "/latin", strings.NewReader(string([]byte{0x41, 0xff, 0xfe})), "Origin", "http://shop.test"))
	same(t, out, "text", `"INVALID_ARGUMENT"`)
	same(t, out, "json", `"INVALID_ARGUMENT"`)
	if data, err := os.ReadFile(filepath.Join(app, ".database", "files", "x", "y", "z.json")); err != nil || string(data) != `{"a":1}` {
		t.Errorf("z.json on the disk: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(app, "b.txt")); !os.IsNotExist(err) {
		t.Error("a write reached the app's code")
	}
}

func TestFileClearStopsWithItsProgress(t *testing.T) {
	limit := file.TreeLimit
	file.TreeLimit = 3
	defer func() { file.TreeLimit = limit }()
	b, _ := filesApp(t, map[string]string{
		".database/files/tmp/1.txt": "1", ".database/files/tmp/2.txt": "2", ".database/files/tmp/3.txt": "3",
		".database/files/tmp/4.txt": "4", ".database/files/tmp/5.txt": "5",
		"shop.test/clear/router.work": fileImport + `router.post((context) => {
  const first = safe(() => file.open("tmp/").clear());
  return context.json({ code: first.error?.code, progress: first.error?.progress, left: file.list("tmp/").length, then: file.open("tmp/").clear() });
});
`,
	})
	out := answered(t, b.request(t, http.MethodPost, "shop.test", "/clear", nil, "Origin", "http://shop.test"))
	same(t, out, "code", `"PARTIAL"`)
	same(t, out, "progress", `3`)
	same(t, out, "left", `2`)
	same(t, out, "then", `2`)
}

func TestFileChangesNothingAtBoot(t *testing.T) {
	said := bootError(t, map[string]string{
		"apps/shop/shop.test/data/menu.json": `[1]`,
		"apps/shop/shop.test/router.work": fileImport + `const menu = file.json("app/shop.test/data/menu.json");
const later = file.open("later.txt");
file.open("boot.txt").write("x");
router.get((context) => context.json(menu));
`,
	})
	contains(t, "the boot", said, `file.open("boot.txt").write(…) ở cấp file`)
	if said := bootError(t, map[string]string{
		"apps/shop/shop.test/data/menu.json": `[1]`,
		"apps/shop/shop.test/router.work": fileImport + `const menu = file.json("app/shop.test/data/menu.json");
const later = file.open("later.txt");
router.get((context) => context.json(menu));
`,
	}); said != "" {
		t.Fatalf("a read at boot: %s", said)
	}
	said = bootError(t, map[string]string{
		"application.work": `import * as app from "kitwork/application";
import * as file from "kitwork/file";
const settings = file.json("./settings.json");
app.root("apps");
app.website({ hostname: "shop.test", port: 8080 });
app.run();
`,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => context.text("x"));
`,
	})
	contains(t, "the manifest's boot", said, "manifest chỉ đọc biến môi trường")
}

func TestFileServes(t *testing.T) {
	b, _ := filesApp(t, map[string]string{
		".env":                             "SECRET=1\n",
		".database/files/exports/a.csv":    "abcdef",
		".database/files/exports/data.bin": "bytes",
		".database/files/exports/LICENSE":  "MIT",
		".database/files/exports/inner/x":  "x",
		"shop.test/data/menu.json":         `[1]`,
		"shop.test/csv/router.work":        fileImport + `router.get((context) => context.file("exports/a.csv"));` + "\n",
		"shop.test/menu/router.work":       fileImport + `router.get((context) => context.file("app/shop.test/data/menu.json"));` + "\n",
		"shop.test/bin/router.work":        fileImport + `router.get((context) => context.file("exports/data.bin"));` + "\n",
		"shop.test/license/router.work":    fileImport + `router.get((context) => context.file("exports/LICENSE"));` + "\n",
		"shop.test/env/router.work":        fileImport + `router.get((context) => context.file("app/.env"));` + "\n",
		"shop.test/folder/router.work":     fileImport + `router.get((context) => context.file("exports/inner/"));` + "\n",
		"shop.test/named/router.work":      fileImport + `router.get((context) => context.file(file.open("exports/").segment(context.query("name"))));` + "\n",
		"shop.test/typed/router.work":      fileImport + `router.get((context) => context.type("text/plain; charset=utf-8").file(file.bytes("exports/a.csv")));` + "\n",
		"shop.test/raw/router.work":        fileImport + `router.get((context) => context.file(file.bytes("exports/a.csv")));` + "\n",
		"shop.test/typedpath/router.work":  fileImport + `router.get((context) => context.type("text/plain; charset=utf-8").file("exports/a.csv"));` + "\n",
		"shop.test/wrong/router.work": fileImport + `router.get((context) => {
  const outside = safe(() => context.file("../a.csv"));
  const number = safe(() => context.file(1));
  return context.json({ outside: outside.error?.code, number: number.error?.code });
});
`,
	})
	csv := b.get(t, "shop.test", "/csv")
	if csv.status != http.StatusOK || csv.body != "abcdef" || !strings.HasPrefix(csv.header.Get("Content-Type"), "text/csv") ||
		csv.header.Get("X-Content-Type-Options") != "nosniff" || csv.header.Get("ETag") == "" || csv.header.Get("Last-Modified") == "" {
		t.Fatalf("csv: %d %q %v", csv.status, csv.body, csv.header)
	}
	ranged := b.request(t, http.MethodGet, "shop.test", "/csv", nil, "Range", "bytes=1-2")
	if ranged.status != http.StatusPartialContent || ranged.body != "bc" {
		t.Errorf("range: %d %q", ranged.status, ranged.body)
	}
	if again := b.request(t, http.MethodGet, "shop.test", "/csv", nil, "If-None-Match", csv.header.Get("ETag")); again.status != http.StatusNotModified {
		t.Errorf("if-none-match: %d", again.status)
	}
	if head := b.request(t, http.MethodHead, "shop.test", "/csv", nil); head.status != http.StatusOK || head.body != "" {
		t.Errorf("head: %d %q", head.status, head.body)
	}
	if menu := b.get(t, "shop.test", "/menu"); menu.body != `[1]` || !strings.HasPrefix(menu.header.Get("Content-Type"), "application/json") {
		t.Errorf("menu: %q %v", menu.body, menu.header)
	}
	for _, path := range []string{"/bin", "/license"} {
		if got := b.get(t, "shop.test", path); got.header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s: %v", path, got.header)
		}
	}
	for _, path := range []string{"/env", "/folder", "/named?name=nope.csv", "/named?name=inner"} {
		if got := b.get(t, "shop.test", path); got.status != http.StatusNotFound {
			t.Errorf("%s: %d %q", path, got.status, got.body)
		}
	}
	if typed := b.get(t, "shop.test", "/typed"); typed.body != "abcdef" || typed.header.Get("Content-Type") != "text/plain; charset=utf-8" || typed.header.Get("ETag") != "" {
		t.Errorf("typed: %q %v", typed.body, typed.header)
	}
	if raw := b.get(t, "shop.test", "/raw"); raw.header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("raw: %v", raw.header)
	}
	if typed := b.get(t, "shop.test", "/typedpath"); typed.body != "abcdef" || typed.header.Get("Content-Type") != "text/plain; charset=utf-8" || typed.header.Get("ETag") == "" {
		t.Errorf("typed path: %q %v", typed.body, typed.header)
	}
	out := answered(t, b.get(t, "shop.test", "/wrong"))
	same(t, out, "outside", `"INVALID_ARGUMENT"`)
	same(t, out, "number", `"INVALID_ARGUMENT"`)
}

// multipartBody is a multipart body of fields and of one file per field of files (its client name, its bytes).
func multipartBody(t *testing.T, fields map[string]string, files map[string][2]string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, text := range fields {
		writer.WriteField(name, text)
	}
	for field, sent := range files {
		part, err := writer.CreateFormFile(field, sent[0])
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(sent[1]))
	}
	writer.Close()
	return &body, writer.FormDataContentType()
}

func TestFileUploads(t *testing.T) {
	b, app := filesApp(t, map[string]string{
		"shop.test/upload/router.work": "import * as routine from \"kitwork/routine\";\n" + fileImport + `router.post((context) => {
  const avatar = context.request.file("avatar");
  if (avatar == null) return context.text("no avatar");
  const apart = routine.wait(routine.go(() => avatar.name))[0];
  const saved = file.open("uploads/").segment("a.png").write(avatar);
  return context.json({ name: avatar.name, size: avatar.size, type: avatar.type, saved: saved.size,
    title: context.request.form("title"), none: context.request.file("nope"), many: context.request.files("avatar").length,
    noneMany: context.request.files("nope"), text: safe(() => context.request.text()).error?.code,
    intoText: safe(() => file.open("a.txt").write(avatar)).error?.code, word: safe(() => avatar.path).error?.code, apart: apart.ok });
});
`,
		"shop.test/formfirst/router.work": fileImport + `router.post((context) => {
  const title = context.request.form("title");
  const avatar = context.request.file("avatar");
  if (avatar == null) return context.text("no avatar");
  return context.json({ title: title, saved: file.open("b.png").write(avatar).size });
});
`,
		"shop.test/plain/router.work": fileImport + `router.post((context) => context.json({ none: context.request.file("avatar"), many: context.request.files("avatar"), body: context.request.json().a }));` + "\n",
		"shop.test/fields/router.work": fileImport + `router.post((context) => {
  const avatar = context.request.file("avatar");
  if (avatar == null) return context.text("no avatar");
  return context.text(context.request.form("title").length + " " + avatar.size);
});
`,
		"shop.test/kept/router.work": fileImport + `router.post((context) => {
  const kept = safe(() => context.request.file("avatar"));
  return context.json({ code: kept.error ? kept.error?.code : "ok" });
});
`,
	})
	big := strings.Repeat("x", 3<<20) // past BodyLimit: an upload streams, it is never a read of the body
	body, kind := multipartBody(t, map[string]string{"title": "Ảnh"}, map[string][2]string{"avatar": {"../../a.png", big}})
	out := answered(t, b.request(t, http.MethodPost, "shop.test", "/upload", body, "Content-Type", kind, "Origin", "http://shop.test"))
	same(t, out, "name", `"a.png"`)
	same(t, out, "size", `3145728`)
	same(t, out, "type", `"application/octet-stream"`)
	same(t, out, "saved", `3145728`)
	same(t, out, "title", `"Ảnh"`)
	same(t, out, "none", `null`)
	same(t, out, "many", `1`)
	same(t, out, "noneMany", `[]`)
	same(t, out, "text", `"INVALID_INPUT"`)
	same(t, out, "intoText", `"INVALID_ARGUMENT"`)
	same(t, out, "word", `"TYPE"`)
	same(t, out, "apart", `false`)
	if data, err := os.ReadFile(filepath.Join(app, ".database", "files", "uploads", "a.png")); err != nil || len(data) != len(big) {
		t.Errorf("the upload on the disk: %d bytes, %v", len(data), err)
	}
	body, kind = multipartBody(t, map[string]string{"title": "trước"}, map[string][2]string{"avatar": {"b.png", "PNG"}})
	out = answered(t, b.request(t, http.MethodPost, "shop.test", "/formfirst", body, "Content-Type", kind, "Origin", "http://shop.test"))
	same(t, out, "title", `"trước"`)
	same(t, out, "saved", `3`)
	out = answered(t, b.request(t, http.MethodPost, "shop.test", "/plain", strings.NewReader(`{"a":1}`), "Content-Type", "application/json", "Origin", "http://shop.test"))
	same(t, out, "none", `null`)
	same(t, out, "many", `[]`)
	same(t, out, "body", `1`)

	// the fields beside an upload count against BodyLimit, as a form's do
	body, kind = multipartBody(t, map[string]string{"title": strings.Repeat("t", router.BodyLimit+1)}, map[string][2]string{"avatar": {"a.png", "PNG"}})
	if past := b.request(t, http.MethodPost, "shop.test", "/fields", body, "Content-Type", kind, "Origin", "http://shop.test"); past.status != http.StatusRequestEntityTooLarge {
		t.Errorf("fields past BodyLimit: %d %q", past.status, past.body)
	}
	// more parts than Go's parse reads
	many := map[string]string{}
	for i := 0; i < 1001; i++ {
		many["f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+strings.Repeat("y", i/26)] = "1"
	}
	body, kind = multipartBody(t, many, map[string][2]string{"avatar": {"a.png", "PNG"}})
	if past := b.request(t, http.MethodPost, "shop.test", "/fields", body, "Content-Type", kind, "Origin", "http://shop.test"); past.status != http.StatusRequestEntityTooLarge {
		t.Errorf("too many parts: %d %q", past.status, past.body)
	}

	// past uploadMemory a file goes to a file of the system, gone as the request ends; none at all is DATABASE
	temporary := t.TempDir()
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)
	t.Setenv("TMPDIR", temporary)
	body, kind = multipartBody(t, nil, map[string][2]string{"avatar": {"a.png", strings.Repeat("z", 200<<10)}})
	answered(t, b.request(t, http.MethodPost, "shop.test", "/upload", body, "Content-Type", kind, "Origin", "http://shop.test"))
	deadline := time.Now().Add(2 * time.Second)
	for {
		left, _ := os.ReadDir(temporary)
		if len(left) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the upload's file of the system is still there: %v", left[0].Name())
		}
		time.Sleep(10 * time.Millisecond)
	}
	gone := filepath.Join(temporary, "gone")
	t.Setenv("TMP", gone)
	t.Setenv("TEMP", gone)
	t.Setenv("TMPDIR", gone)
	body, kind = multipartBody(t, nil, map[string][2]string{"avatar": {"a.png", strings.Repeat("z", 200<<10)}})
	out = answered(t, b.request(t, http.MethodPost, "shop.test", "/kept", body, "Content-Type", kind, "Origin", "http://shop.test"))
	same(t, out, "code", `"DATABASE"`)
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)
	t.Setenv("TMPDIR", temporary)

	limit := router.UploadLimit
	router.UploadLimit = 1 << 20
	defer func() { router.UploadLimit = limit }()
	body, kind = multipartBody(t, nil, map[string][2]string{"avatar": {"a.png", big}})
	if past := b.request(t, http.MethodPost, "shop.test", "/upload", body, "Content-Type", kind, "Origin", "http://shop.test"); past.status != http.StatusRequestEntityTooLarge {
		t.Errorf("past the ceiling: %d %q", past.status, past.body)
	}
}
