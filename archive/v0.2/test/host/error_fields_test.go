package host_test

import (
	"path/filepath"
	"testing"
)

// A failure of data from outside names each field it is about, as error.fields (docs/schema.md mục 6): { path,
// rule, message }, the path the field's place (["tags", 0] an item), the rule the one the value broke (the word
// the author wrote: min, max, email, choice; required, type), the message what is wrong, never the value. safe()
// hands it beside code and message, whether a transform ran in the call (the failure as it is) or none did (the
// failure told at the call): a form shows each under its input.
//
// CONTROL (07/11/2026): with safe() not handing fields, every key was null; with the runtime not keeping the
// cause's fields on the failure told at the call (diagnostic.Fields), required was null, the others there (no
// transform ran in its call, one did in the others').
func TestAParseFailureNamesItsFields(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": shopSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { signup } from "app/core/schema/shop.work";
router.get((context) => {
  const wrong = safe(() => signup.parse({ email: "not-an-email", name: "   ", age: -1, plan: "gold", amount: "0.123",
    agree: "yes", day: "2026-02-30", ref: "x", tags: ["", "b", "c", "d"] }));
  const required = safe(() => signup.parse({}));
  const tooLong = safe(() => signup.parse({ email: "a@x.vn", name: "An", tags: ["a", "b", "c", "d"] }));
  const right = safe(() => signup.parse({ email: "a@x.vn", name: "An" }));
  const plain = safe(() => JSON.parse("{"));
  return context.json({ wrong: wrong.error?.fields, required: required.error?.fields, tooLong: tooLong.error?.fields,
    right: right.error, plain: plain.error?.fields === undefined });
});
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	read := answer(t, app, "/")
	expect(t, read, map[string]string{
		"wrong": `[
			{"path": ["email"], "rule": "email", "message": "không phải địa chỉ email"},
			{"path": ["name"], "rule": "min", "message": "cần ít nhất 1 ký tự"},
			{"path": ["age"], "rule": "min", "message": "nhỏ nhất là 0"},
			{"path": ["plan"], "rule": "choice", "message": "cần một trong \"free\", \"pro\""},
			{"path": ["amount"], "rule": "type", "message": "quá 2 chữ số sau dấu phẩy"},
			{"path": ["agree"], "rule": "type", "message": "cần true hay false"},
			{"path": ["day"], "rule": "type", "message": "cần ngày YYYY-MM-DD"},
			{"path": ["ref"], "rule": "type", "message": "cần UUID (8-4-4-4-12 chữ số hệ 16)"},
			{"path": ["tags", 0], "rule": "min", "message": "cần ít nhất 1 ký tự"}
		]`,
		"required": `[{"path": ["email"], "rule": "required", "message": "bắt buộc"}, {"path": ["name"], "rule": "required", "message": "bắt buộc"}]`,
		"tooLong":  `[{"path": ["tags"], "rule": "max", "message": "tối đa 3 phần tử"}]`,
		"right":    `null`,
		"plain":    `true`,
	})
}

// A value a write refuses names its column the same way, so one safe around .parse() and the write shows either under
// the input (docs/schema.md mục 6, học unique_constraint của Ecto): a unique column already there, a foreign key to no
// row (the column SQLite does not name, known when the write gives one foreign key), a value the column's rules refuse.
//
// CONTROL (07/11/2026): with writeFailure making no fields, duplicate and orphan were null; with bindWrite's
// invalidColumn a plain failure, negative and empty were null.
func TestAWriteFailureNamesItsColumn(t *testing.T) {
	app := writeApp(t, map[string]string{"apps/shop/shop.test/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  const an = db.insert((users) => ({ email: "an@x.vn", username: "an", name: "An", passwordHash: "h" }));
  const duplicate = safe(() => db.insert((users) => ({ email: "an@x.vn", username: "an2", name: "B", passwordHash: "h" })));
  const empty = safe(() => db.insert((users) => ({ email: "b@x.vn", username: "b", name: "", passwordHash: "h" })));
  const nobody = 99;
  const orphan = safe(() => db.insert((orders) => ({ userId: nobody, item: "x", amount: 1 })));
  const negative = safe(() => db.insert((orders) => ({ userId: an.id, item: "x", amount: -1 })));
  const restricted = safe(() => db.insert((orders) => ({ userId: an.id, item: "x", amount: 1 })) && db.delete((users) => users.id === an.id));
  return context.json({ duplicate: duplicate.error?.fields, empty: empty.error?.fields, orphan: orphan.error?.fields,
    negative: negative.error?.fields, restricted: restricted.error?.fields === undefined });
});
`})
	read := answer(t, app, "/")
	expect(t, read, map[string]string{
		"duplicate":  `[{"path": ["email"], "rule": "unique", "message": "đã có"}]`,
		"empty":      `[{"path": ["name"], "rule": "min", "message": "cần ít nhất 1 ký tự"}]`,
		"orphan":     `[{"path": ["userId"], "rule": "foreign", "message": "dòng được trỏ tới không có"}]`,
		"negative":   `[{"path": ["amount"], "rule": "min", "message": "nhỏ nhất là 0"}]`,
		"restricted": `true`, // a delete others point to is no field of a form
	})
}
