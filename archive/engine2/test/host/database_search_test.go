package host_test

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// searchSchema is a table of posts searched by their title and body, and one without a .searchable() column.
const searchSchema = `import { struct, id, string, boolean } from "kitwork/schema";
import { connection } from "kitwork/database";

export const posts = struct({
  id: id(),
  title: string().searchable(),
  body: string().searchable(),
  published: boolean(),
  slug: string()
});
export const tags = struct({ id: id(), name: string() });

export const db = connection().tables({ posts, tags });
`

// searchApp boots an app of searchSchema whose posts are written through KitDB's writer, as the engine writes them:
// the index folds in what the writer records.
func searchApp(t *testing.T, routes map[string]string) *booted {
	t.Helper()
	files := map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": searchSchema,
	}
	for path, body := range routes {
		files["apps/shop/shop.test/"+path+"router.work"] = routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
` + body + `
});
`
	}
	folder := write(t, files)
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	ctx := context.Background()
	database, err := kitdb.Open(ctx, databaseFile(folder), kitdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write(ctx, "test", func(tx *kitdb.Transaction) error {
		_, err := tx.Exec(`INSERT INTO posts (title, body, published, slug) VALUES
			('Cà phê sữa đá Sài Gòn', 'Ly cà phê sữa đá buổi sáng', 1, 'a'),
			('Trà đào cam sả', 'Quán không có cà phê', 1, 'b'),
			('Cà phê muối Huế', 'Vị mặn của muối', 0, 'c'),
			('Bánh mì <b>nóng</b>', 'Ổ giòn', 1, 'd')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return boot(t, filepath.Join(folder, "application.work"))
}

func sortedIDs(read any) []float64 {
	got := ids(read, "")
	sort.Float64s(got)
	return got
}

// matches searches the .searchable() columns (docs/KITWORK_DATABASE.md mục 2): by words, without accents; with the
// rest of the condition as SQL; on one column; !matches filtering, alone or beside one; a count on the index alone or
// on the rows; an empty text no row; with an order, every row that matches in that order; each row its score and its
// snippet, the words in <b>, the text HTML escaped.
//
// CONTROL (06/11/2026): with the rows the index found not bound into the statement, titled was [1], countedPublished
// 3, none true and other every post; with !matches not taken from the ranking, fresh was [1 2 3]; with the snippet's
// last text not escaped, bread's was "<b>Bánh</b> <b>mì</b> <b>nóng</b>".
func TestMatchesSearchesTheSearchableColumns(t *testing.T) {
	app := searchApp(t, map[string]string{
		"": `  const q = "ca phe";
  return context.json({
    all: db.list((posts) => posts.matches(q)),
    published: db.list((posts) => posts.matches(q) && posts.published),
    titled: db.list((posts) => posts.title.matches(q)),
    fresh: db.list((posts) => posts.matches(q) && !posts.matches("muối")),
    other: db.list((posts) => !posts.matches("cà phê")),
    counted: db.count((posts) => posts.matches(q)),
    countedPublished: db.count((posts) => posts.matches(q) && posts.published),
    none: db.exists((posts) => posts.matches("không có từ này")),
    empty: db.list((posts) => posts.matches("")),
    ordered: db.where((posts) => posts.matches(q)).descending((posts) => posts.id).list(),
    bread: db.first((posts) => posts.matches("bánh mì"))
  });`,
	})
	read := answer(t, app, "/")
	for key, want := range map[string][]float64{"all": {1, 2, 3}, "published": {1, 2}, "titled": {1, 3}, "fresh": {1, 2}} {
		if got := sortedIDs(read[key]); jsonText(got) != jsonText(want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	expect(t, read, map[string]string{"counted": "3", "countedPublished": "2", "none": "false", "empty": "[]"})
	if got := ids(read["other"], ""); jsonText(got) != "[4]" {
		t.Errorf("other = %v, want [4]", got)
	}
	if got := ids(read["ordered"], ""); jsonText(got) != "[3,2,1]" {
		t.Errorf("ordered = %v, want [3,2,1]", got)
	}
	for _, row := range read["all"].([]any) {
		item := row.(map[string]any)
		if score, _ := item["score"].(float64); score <= 0 {
			t.Errorf("post %v has score %v", item["id"], item["score"])
		}
		if snippet, _ := item["snippet"].(string); !strings.Contains(snippet, "<b>") {
			t.Errorf("post %v has snippet %q, want its words in <b>", item["id"], item["snippet"])
		}
	}
	bread, _ := read["bread"].(map[string]any)
	if snippet, _ := bread["snippet"].(string); !strings.Contains(snippet, "<b>Bánh</b> <b>mì</b>") || !strings.Contains(snippet, "&lt;b&gt;nóng&lt;/b&gt;") {
		t.Errorf("bread's snippet = %q, want its words in <b> and its own <b> escaped", bread["snippet"])
	}
}

// matches written where it cannot be translated stops the boot (docs/KITWORK_DATABASE.md mục 5), and a text of
// another kind is the run's INVALID_ARGUMENT.
//
// CONTROL (06/11/2026): with splitMatches taking a matches from under ||, the boot passed it.
func TestMatchesWrittenWrong(t *testing.T) {
	for _, c := range []struct{ name, statement, want string }{
		{"a column not searchable", `db.list((posts) => posts.slug.matches("x"))`, "posts.slug không có .searchable(): matches tìm trên cột .searchable()"},
		{"a table without searchable", `db.list((tags) => tags.matches("x"))`, "tags không có cột .searchable()"},
		{"under ||", `db.list((posts) => posts.matches("x") || posts.published)`, "matches dưới ||, trong ?: hay sau ! của một điều kiện khác chưa có trong engine2"},
		{"a value", `db.select((posts) => ({ found: posts.matches("x") })).list()`, "matches là một điều kiện, không phải một giá trị"},
	} {
		got := bootError(t, map[string]string{
			"apps/shop/core/schema/shop.work": searchSchema,
			"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => context.json(` + c.statement + `));
`,
		})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
	app := searchApp(t, map[string]string{
		"": `  const n = 5;
  const { error } = safe(() => db.list((posts) => posts.matches(n)));
  return context.json({ code: error?.code, message: error?.message });`,
	})
	if got := answer(t, app, "/"); got["code"] != "INVALID_ARGUMENT" || !strings.Contains(got["message"].(string), "matches nhận một chữ") {
		t.Errorf("a number to matches: %v", got)
	}
}
