package host_test

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// transformSchema is a table whose columns transform what they keep: a const of the file, a lambda in place.
const transformSchema = `import { struct, id, string, integer } from "kitwork/schema";
import { connection } from "kitwork/database";

const email = (value) => value.trim().toLowerCase();

export const customers = struct({
  id: id(),
  email: string().transform(email).max(254).unique(),
  name: string().transform((value) => value.trim()).min(1),
  code: string().transform((value) => value.toUpperCase()).nullable()
});

export const db = connection().tables({ customers });
`

// The common samples of a table's transform (docs/KITWORK_SCHEMA.md mục 5): for each, what .parse() gives running the
// JavaScript function is what the database keeps, a second run changes nothing, and a condition comparing the column
// with the text as written finds the row, the text going through the transform as the column's values did.
//
// CONTROL (06/11/2026): with toLowerCase taken as ASCII's, "Ế Lê" between Unicode spaces was kept "Ế lê" where
// .parse() gave "ế lê"; with the compared text not transformed, found was false for "  An@X.VN  ".
func TestATablesTransformIsTheParsesOne(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": transformSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { db, customers } from "app/core/schema/shop.work";
router.get((context) => {
  const s = context.request.query("s");
  const parsed = customers.parse({ email: s, name: s, code: s });
  const row = db.insert((customers) => ({ email: s, name: s, code: s }));
  const again = customers.parse(parsed);
  const found = db.first((customers) => customers.email === s);
  db.delete((customers) => customers.id === row.id);
  return context.json({ parsed, row, again, found: found !== null });
});
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	app := boot(t, filepath.Join(folder, "application.work"))
	for _, sample := range []string{"  An@X.VN  ", string(rune(0x00a0)) + "Ế Lê" + string(rune(0x2003)), "\tĐÀ NẴNG\n", "Straße", "İstanbul", string(rune(0xfeff)) + "x" + string(rune(0x3000)), "ΣΑΣ", "ǅungla"} {
		read := answer(t, app, "/?s="+url.QueryEscape(sample))
		parsed, _ := read["parsed"].(map[string]any)
		row, _ := read["row"].(map[string]any)
		again, _ := read["again"].(map[string]any)
		for _, column := range []string{"email", "name", "code"} {
			if parsed[column] != row[column] {
				t.Errorf("%q: .parse() gives %s %q, the database keeps %q", sample, column, parsed[column], row[column])
			}
			if again[column] != parsed[column] {
				t.Errorf("%q: %s through the transform twice is %q, once %q", sample, column, again[column], parsed[column])
			}
		}
		if read["found"] != true {
			t.Errorf("%q: the row is not found comparing email with the text as written", sample)
		}
	}
}

// A table's transform the engine cannot translate stops the boot where the table is declared (docs/KITWORK_SCHEMA.md
// mục 2, rule 1): a method not one of trim, toLowerCase, toUpperCase, a function called, a column not a string.
//
// CONTROL (06/11/2026): with Declared not translating the transforms, every one of these booted.
func TestATransformNotTranslatedStopsTheBoot(t *testing.T) {
	for _, c := range []struct{ name, field, want string }{
		{"replace", `string().transform((value) => value.replace("a", "b"))`, `customers.email: transform chưa dịch được sang SQL: value.replace(a, b): trên bảng, transform chỉ có trim(), toLowerCase(), toUpperCase()`},
		{"a function", `string().transform((value) => slug(value))`, "customers.email: transform chưa dịch được sang SQL"},
		{"not a string", `integer().transform((value) => value.trim())`, "customers.email: transform của bảng đổi chữ (trim, toLowerCase, toUpperCase); email là integer"},
	} {
		schema := strings.Replace(transformSchema, `string().transform(email).max(254).unique()`, c.field, 1)
		schema = strings.Replace(schema, "const email =", "const slug = (value) => value;\nconst email =", 1)
		got := bootError(t, map[string]string{"apps/shop/core/schema/shop.work": schema})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
