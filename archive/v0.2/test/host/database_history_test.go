package host_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// declareHistory turns on the history of tables in the app's database, as an admin's ALTER TABLE … SET (history = on)
// through the port does: a table kitwork migrate makes has none (KITWORK_KITDB.md câu 13.11 (d)).
func declareHistory(t *testing.T, folder string, tables ...string) {
	t.Helper()
	ctx := context.Background()
	database, err := kitdb.Open(ctx, databaseFile(folder), kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, table := range tables {
		if _, err := database.SetHistory(ctx, "admin", table, true); err != nil {
			t.Fatal(err)
		}
	}
}

// writeAppWithHistory is writeApp whose database declares the history of tables between kitwork migrate and the boot.
func writeAppWithHistory(t *testing.T, files map[string]string, tables ...string) *booted {
	t.Helper()
	files["application.work"] = manifest
	files["apps/shop/core/schema/shop.work"] = writeSchema
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	declareHistory(t, folder, tables...)
	return boot(t, filepath.Join(folder, "application.work"))
}

// The history and the past of a table (docs/KITWORK_DATABASE.md mục 2, "Thời điểm và lịch sử"): history() gives the
// changes of the rows its condition keeps, a change kept when the row before or after it meets the condition, each
// { at, before, after, by }, a .secret() column never; at(moment) reads the table as it was then, the rows changed
// since put back, the rows inserted since absent, the rows deleted since there. Both read a table whose history the
// database declares (KITWORK_KITDB.md câu 13.11): on a table without, each is the statement's failure saying how to
// turn it on.
//
// CONTROL (06/11/2026): with the images' condition not run (every change kept), edited held all four changes of notes;
// with at() reading the live table, then was ["một sửa"] and thenEdited 1. (07/11/2026) With history() taking no
// notice of a table without history, lines was [] and no failure.
func TestTheHistoryAndThePastOfATable(t *testing.T) {
	app := writeAppWithHistory(t, map[string]string{
		"apps/shop/shop.test/one/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const a = db.insert((notes) => ({ text: "một" }));
  const b = db.insert((notes) => ({ text: "hai" }));
  db.insert((users) => ({ email: "an@x.vn", username: "an", name: "An", passwordHash: "bí mật" }));
  return context.json({ a: a.id, b: b.id, now: Date.now() });
});
`,
		"apps/shop/shop.test/two/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const a = context.request.query("a");
  const b = context.request.query("b");
  db.where((notes) => notes.id === a).update((notes) => ({ text: "một sửa" }));
  db.delete((notes) => notes.id === b);
  return context.json({ ok: true });
});
`,
		"apps/shop/shop.test/read/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const a = context.request.query("a");
  const b = context.request.query("b");
  const then = Number(context.request.query("then"));
  const late = safe(() => db.at("hôm qua").list((notes) => notes));
  return context.json({
    edited: db.history((notes) => notes.text === "một sửa"),
    ofA: db.history((notes) => notes.id === a),
    ofB: db.where((notes) => notes.id === b).history(),
    two: db.where((notes) => notes.text === "hai").history(),
    users: db.history((users) => users),
    then: db.at(then).list((notes) => notes).map((note) => note.text),
    thenEdited: db.at(then).count((notes) => notes.text === "một sửa"),
    now: db.list((notes) => notes).map((note) => note.text),
    late: late.error,
    lines: safe(() => db.history((lines) => lines)).error,
    linesThen: safe(() => db.at(then).list((lines) => lines)).error
  });
});
`,
	}, "notes", "users")
	one := answer(t, app, "/one")
	a, b := one["a"].(string), one["b"].(string)
	then := strconv.FormatFloat(one["now"].(float64), 'f', 0, 64)
	time.Sleep(20 * time.Millisecond) // the next commits at a later millisecond than then
	answer(t, app, "/two?a="+a+"&b="+b)
	read := answer(t, app, "/read?a="+a+"&b="+b+"&then="+then)

	texts := func(key string) string {
		var out []string
		changes, _ := read[key].([]any)
		for _, change := range changes {
			item := change.(map[string]any)
			side := func(name string) string {
				row, _ := item[name].(map[string]any)
				if row == nil {
					return "null"
				}
				return row["text"].(string)
			}
			if at, _ := item["at"].(float64); at < 1.7e12 || item["by"] != nil {
				t.Errorf("%s: a change's at %v, by %v; want milliseconds and null", key, item["at"], item["by"])
			}
			out = append(out, side("before")+"→"+side("after"))
		}
		return strings.Join(out, ", ")
	}
	for key, want := range map[string]string{
		"edited": "một→một sửa",
		"ofA":    "null→một, một→một sửa",
		"ofB":    "null→hai, hai→null",
		"two":    "null→hai, hai→null",
	} {
		if got := texts(key); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if users, _ := read["users"].([]any); len(users) != 1 || users[0].(map[string]any)["after"].(map[string]any)["passwordHash"] != nil || users[0].(map[string]any)["after"].(map[string]any)["email"] != "an@x.vn" {
		t.Errorf("users' history = %v, want the insert, its email, never its passwordHash", read["users"])
	}
	expect(t, read, map[string]string{"then": `["một","hai"]`, "thenEdited": "0", "now": `["một sửa"]`})
	if late, _ := read["late"].(map[string]any); late == nil || late["code"] != "INVALID_ARGUMENT" || !strings.Contains(late["message"].(string), "at nhận chữ ISO có múi giờ") {
		t.Errorf("at(\"hôm qua\") = %v, want INVALID_ARGUMENT saying what a moment is", read["late"])
	}
	// lines, a table kitwork migrate made and no one declared the history of
	for _, key := range []string{"lines", "linesThen"} {
		failure, _ := read[key].(map[string]any)
		if message, _ := failure["message"].(string); failure["code"] != "DATABASE" || !strings.Contains(message, "của lines: bảng chưa bật lịch sử; bật bằng ALTER TABLE lines SET (history = on)") {
			t.Errorf("%s on a table without history = %v, want DATABASE saying how to turn it on", key, read[key])
		}
	}
}

// A moment is a millisecond: at() of the at history() gives a change reads the table with that change made, every
// commit of that millisecond with it (12/11/2026). A commit is timed to the nanosecond and history() gives its
// millisecond, cut down; at() read the table at the start of that millisecond, before the change, so
// TestTheHistoryAndThePastOfATable, whose then is a Date.now() taken just after two writes, saw the first and not the
// second 3 times in 40. Here it is every time: each change of three is in the table read at its own at.
func TestAtOfAChangesMomentShowsTheChange(t *testing.T) {
	app := writeAppWithHistory(t, map[string]string{
		"apps/shop/shop.test/write/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  db.insert((notes) => ({ text: "một" }));
  db.insert((notes) => ({ text: "hai" }));
  db.insert((notes) => ({ text: "ba" }));
  return context.json({ ok: true });
});
`,
		"apps/shop/shop.test/read/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const changes = db.history((notes) => notes);
  return context.json(changes.map((change) => ({ text: change.after.text, then: db.at(change.at).list((notes) => notes).map((note) => note.text) })));
});
`,
	}, "notes")
	answer(t, app, "/write")
	got := app.get(t, "shop.test", "/read")
	if got.status != 200 {
		t.Fatalf("GET /read: %d %s", got.status, got.body)
	}
	var changes []struct {
		Text string   `json:"text"`
		Then []string `json:"then"`
	}
	if err := json.Unmarshal([]byte(got.body), &changes); err != nil || len(changes) != 3 {
		t.Fatalf("the history: %s (%v)", got.body, err)
	}
	for _, change := range changes {
		if !slices.Contains(change.Then, change.Text) {
			t.Errorf("at() of the moment of the insert of %q read %v, without it", change.Text, change.Then)
		}
	}
}

// at() of a moment before the history of a table began is the statement's failure, saying from when the history has
// it: rows changed then with no trace, so the table as it was is not known (KITWORK_KITDB.md câu 13.11). A moment
// after reads the table as it was then, the rows written before the history began with it.
//
// CONTROL (07/11/2026): with past() not comparing the moment with where the table's history begins, before failed
// saying "notes lúc đó không đọc lại được" (KitDB's RestoreTable refusing the moment too); with that and Restore in
// place of RestoreTable, before was no failure: the table as the anchor held it, without the row "một" it had then.
func TestThePastOfATableBeforeItsHistory(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": writeSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const before = Number(context.request.query("before"));
  const after = Number(context.request.query("after"));
  return context.json({
    before: safe(() => db.at(before).list((notes) => notes)).error,
    after: db.at(after).list((notes) => notes).map((note) => note.text)
  });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	ctx := context.Background()
	database, err := kitdb.Open(ctx, databaseFile(folder), kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id, text string) {
		t.Helper()
		if _, err := database.Write(ctx, "t", func(transaction *kitdb.Transaction) error {
			_, err := transaction.Exec(`INSERT INTO notes (id, text) VALUES (?, ?)`, id, text)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	moment := func() string {
		defer time.Sleep(5 * time.Millisecond)
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	insert("n1", "một") // no trace: notes has no history yet
	before := moment()
	insert("n2", "hai")
	if _, err := database.SetHistory(ctx, "admin", "notes", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	insert("n3", "ba")
	after := moment()
	insert("n4", "bốn")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	app := boot(t, filepath.Join(folder, "application.work"))
	read := answer(t, app, "/?before="+before+"&after="+after)
	failure, _ := read["before"].(map[string]any)
	if message, _ := failure["message"].(string); failure["code"] != "INVALID_ARGUMENT" || !strings.Contains(message, "lịch sử của notes chỉ có từ") {
		t.Errorf("at() before the history of notes began = %v, want INVALID_ARGUMENT saying from when", read["before"])
	}
	if got := jsonText(read["after"]); got != `["một","hai","ba"]` {
		t.Errorf("at() after the history of notes began = %s, want the rows then", got)
	}
}

// at() of a moment the history reaches, which going back from now cannot reach (the schema changed since) and no anchor
// is old enough to restore from, is the statement's failure saying so in Vietnamese and from when an anchor is: here the
// anchors were taken away, as when the first anchor of a table is still being written or the anchors are gone.
//
// CONTROL (09/11/2026): with ErrNoAnchor left to the general failure, before was "at(…): kitdb: no anchor at or before
// the moment".
func TestThePastOfATableWithNoAnchorThen(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": writeSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const then = Number(context.request.query("then"));
  return context.json({ then: safe(() => db.at(then).list((notes) => notes)).error });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	ctx := context.Background()
	path := databaseFile(folder)
	database, err := kitdb.Open(ctx, path, kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SetHistory(ctx, "admin", "notes", true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write(ctx, "t", func(transaction *kitdb.Transaction) error {
		_, err := transaction.Exec(`INSERT INTO notes (id, text) VALUES ('n1', 'một')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	then := strconv.FormatInt(time.Now().UnixMilli(), 10)
	time.Sleep(5 * time.Millisecond)
	// the schema changed after the moment: going back from now stops there
	if _, err := database.Migrate(ctx, "admin", `ALTER TABLE notes ADD COLUMN extra TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	anchors, err := filepath.Glob(path + ".history/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, anchor := range anchors {
		if strings.HasSuffix(anchor, ".base") || strings.HasSuffix(anchor, ".pages") {
			if err := os.Remove(anchor); err != nil {
				t.Fatal(err)
			}
		}
	}

	app := boot(t, filepath.Join(folder, "application.work"))
	read := answer(t, app, "/?then="+then)
	failure, _ := read["then"].(map[string]any)
	message, _ := failure["message"].(string)
	if failure["code"] != "DATABASE" || !strings.Contains(message, "notes lúc đó không đọc lại được") ||
		!strings.Contains(message, "không có mốc sao lưu nào trước lúc đó") || strings.Contains(message, "no anchor") {
		t.Errorf("at() with no anchor then = %v, want DATABASE saying so in Vietnamese", read["then"])
	}
	t.Log(message)
}
