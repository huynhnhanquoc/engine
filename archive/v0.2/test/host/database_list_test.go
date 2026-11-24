package host_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A list that names no count reads at most 1000 rows: past them it is the failure saying how to write one (15/11/2026,
// the owner, round 3 of the 13/11 review), where it gave the first 1000 and left the rest in silence. A list naming
// its count, a page, or a condition finding 1000 rows or fewer reads as before.
func TestAListPastItsRowsSaysHowToWriteIt(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/core/schema/shop.work": `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";
export const notes = struct({ id: id(), text: string() });
export const db = connection().tables({ notes });
`,
		"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const all = safe(() => db.list((notes) => notes));
  return context.json({
    all: all.error ? [all.error?.code, all.error?.at + ": " + all.error?.message] : null,
    named: db.list((notes) => notes, 1001).length,
    page: db.list((notes) => notes, { page: 2, limit: 1000 }).length,
    narrow: db.list((notes) => notes.text === "even").length,
  });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databaseFile(folder)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		text := "odd"
		if i%2 == 0 {
			text = "even"
		}
		if _, err := tx.Exec(`INSERT INTO notes (text) VALUES (?)`, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	app := boot(t, filepath.Join(folder, "application.work"))
	got := app.get(t, "shop.test", "/")
	want := `{"all":["INVALID_ARGUMENT","router.work:4:28: list() không nêu số dòng đọc tối đa 1000 dòng, mà còn hơn 1000 dòng khớp: viết list(n) với n tới 10000, list({ page, limit }), hay thu hẹp điều kiện"],"named":1001,"page":1,"narrow":501}`
	if got.status != 200 || !strings.Contains(got.body, want) {
		t.Errorf("GET /: %d %s\nwant %s\nlog: %s", got.status, got.body, want, app.log)
	}
}
