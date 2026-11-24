package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// signupForm is the struct the form tests draw: a field of each kind an input takes, the words of a label and a hint.
const signupForm = `import { struct, string, integer, decimal, boolean, choice, date, float } from "kitwork/schema";

const email = (value) => value.trim().toLowerCase();

export const signup = struct({
  email: string().transform(email).max(254).email().label("Email"),
  name: string().min(1).max(100).label("Họ tên").description("Tên in trên hoá đơn"),
  age: integer().min(0).max(150).nullable(),
  plan: choice("free", "pro").default("free"),
  amount: decimal(2).min(0),
  ratio: float(),
  agree: boolean().default(false),
  day: date().nullable(),
  password: string().min(8),
  note: string().max(2000).nullable()
});
`

const signupPage = `<content>
<form method="post">
  <field><label /><input value="{{ form.email }}"></input><error /></field>
  <field><label /><input value="{{ form.name }}" maxlength="50"></input><hint /><error /></field>
  <field><label>Tuổi</label><input value="{{ form.age }}" required></input></field>
  <field><label>Gói</label><select value="{{ form.plan }}"></select></field>
  <input value="{{ form.amount }}"></input>
  <input value="{{ form.ratio }}"></input>
  <input value="{{ form.agree }}"></input>
  <input value="{{ form.day }}"></input>
  <input type="password" value="{{ form.password }}"></input>
  <textarea value="{{ form.note }}"></textarea>
</form>
</content>`

const signupRoute = `import { signup } from "app/core/schema/shop.work";
router.get((context) => context.view({ form: signup.form() }));
router.post((context) => {
  const input = context.request.form();
  const { value, error } = safe(() => signup.parse(input));
  if (error) {
    return context.view({ form: signup.form(input, error) });
  }
  return context.text("ok " + value.email);
});
`

// An input bound to a form's field, value="{{ form.email }}", has what the struct says of the field written by the
// engine (docs/KITWORK_SCHEMA.md mục 7): its name, the type of its kind, required, its lengths and bounds, a decimal's
// step, a float's step any, its value (an empty form's, the field's default); a choice(…) is a select of its words; a checkbox is ticked by its value; a
// password is never written back. In its <field>, an empty <label /> and <hint /> say the field's .label() and
// .description(), an empty <error /> its error, hidden when it has none; the control is aria-invalid while the form
// holds its error, described by it. A rule the page writes stricter than the struct's is the one written.
//
// CONTROL (07/11/2026): with opening not taking the form's part (render.go), the inputs had their value only, no
// name; with fieldOf not reading the binding before the label, the labels were empty and the error had no id.
func TestAnInputBoundToAFormsFieldWritesTheStructsRules(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": signupForm,
		"apps/shop/shop.test/router.work": routerImport + signupRoute,
		"apps/shop/shop.test/content.ui":  signupPage,
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	empty := b.get(t, "shop.test", "/")
	if empty.status != http.StatusOK {
		t.Fatalf("GET /: %d %s\n%s", empty.status, empty.body, b.log.String())
	}
	holds(t, "GET /", empty.body,
		`<label for="kitwork-field-1">Email</label>`,
		`<input id="kitwork-field-1" name="email" type="email" required maxlength="254" value="">`,
		`id="kitwork-field-1-error" hidden></p>`,
		`<label for="kitwork-field-2">Họ tên</label>`,
		`<input id="kitwork-field-2" aria-describedby="kitwork-field-2-hint" name="name" required minlength="1" maxlength="50" value="">`,
		`id="kitwork-field-2-hint">Tên in trên hoá đơn</p>`,
		`<input id="kitwork-field-3" name="age" type="number" required min="0" max="150" value="">`,
		`<select class="flex-column" id="kitwork-field-4" name="plan"><option value="free" selected>free</option><option value="pro">pro</option></select>`,
		`<input name="amount" type="number" required min="0" step="0.01" value="">`,
		`<input name="ratio" type="number" required step="any" value="">`,
		`<input name="agree" type="checkbox" value="true">`,
		`<input name="day" type="date" value="">`,
		`<input type="password" name="password" required minlength="8">`,
		`name="note" maxlength="2000"></textarea>`,
	)
	sent := b.request(t, http.MethodPost, "shop.test", "/", strings.NewReader(
		"email=not-an-email&name=An&plan=pro&amount=1.234&ratio=0.5&agree=on&password=secret12&note=Gi%E1%BB%AF+ch%E1%BB%97"),
		"Content-Type", "application/x-www-form-urlencoded")
	if sent.status != http.StatusOK {
		t.Fatalf("POST /: %d %s\n%s", sent.status, sent.body, b.log.String())
	}
	holds(t, "POST / sent back", sent.body,
		`<input id="kitwork-field-1" aria-describedby="kitwork-field-1-error" name="email" type="email" required maxlength="254" value="not-an-email" aria-invalid="true">`,
		`id="kitwork-field-1-error">không phải địa chỉ email</p>`,
		`name="name" required minlength="1" maxlength="50" value="An">`,
		`<option value="free">free</option><option value="pro" selected>pro</option>`,
		`<input name="amount" type="number" required min="0" step="0.01" value="1.234" aria-invalid="true">`,
		`<input name="agree" type="checkbox" value="true" checked>`,
		`<input type="password" name="password" required minlength="8">`,
		`name="note" maxlength="2000">Giữ chỗ</textarea>`,
	)
	if strings.Contains(sent.body, "secret12") {
		t.Errorf("the password was written back into the page")
	}
	right := b.request(t, http.MethodPost, "shop.test", "/", strings.NewReader("email=An@X.vn&name=An&amount=1&ratio=1&password=secret12"),
		"Content-Type", "application/x-www-form-urlencoded")
	if right.body != "ok an@x.vn" {
		t.Errorf("POST / right: %d %s", right.status, right.body)
	}
}

// A control bound to a field written wrong fails the page where it is written: a name the struct gives, a type its
// kind gives, a rule as loose as the struct's or looser, required on a field already required, a choice(…) on an
// input, an option none of its words, a select of no option for a field not a choice, an empty <label /> of a field
// with no .label(), a field of many values, a field the form has not.
func TestAControlBoundWrongFailsThePage(t *testing.T) {
	for _, c := range []struct{ name, page, says string }{
		{"name", `<input name="x" value="{{ form.email }}"></input>`, "name của ô nhập là tên trường form gắn (email): bỏ name"},
		{"type", `<input type="text" value="{{ form.email }}"></input>`, "type của trường email là email, struct đã cho: bỏ type"},
		{"loose", `<input value="{{ form.name }}" maxlength="300"></input>`, `maxlength="300" của trường name không chặt hơn maxlength="100" struct đã cho`},
		{"same", `<input value="{{ form.name }}" minlength="1"></input>`, `minlength="1" của trường name không chặt hơn minlength="1"`},
		{"required", `<input value="{{ form.email }}" required></input>`, "trường email đã bắt buộc (không .nullable(), không .default()): bỏ required"},
		{"step", `<input value="{{ form.amount }}" step="1"></input>`, "step của trường amount là 0.01, kiểu của trường đã cho: bỏ step"},
		{"choice", `<input value="{{ form.plan }}"></input>`, "trường plan là choice(…): viết <select"},
		{"option", `<select value="{{ form.plan }}"><option value="gold">Vàng</option></select>`, `"gold" không là lựa chọn của trường plan: choice("free", "pro")`},
		{"options", `<select value="{{ form.name }}"></select>`, "<select> của trường name cần <option>"},
		{"label", `<field><label /><input value="{{ form.age }}"></input></field>`, "<label /> rỗng là .label() của trường age, mà trường không có"},
		{"hint", `<field><label>Tuổi</label><input value="{{ form.age }}"></input><hint /></field>`, "<hint /> rỗng là .description() của trường age"},
		{"checked", `<input value="{{ form.agree }}" checked></input>`, "checked theo giá trị của trường agree: bỏ checked"},
		{"missing", `<input value="{{ form.nope }}"></input>`, "form không có trường nope; có: email, name, age"},
	} {
		t.Run(c.name, func(t *testing.T) {
			folder := write(t, map[string]string{
				"application.work":                manifest,
				"apps/shop/core/schema/shop.work": signupForm,
				"apps/shop/shop.test/router.work": routerImport + signupRoute,
				"apps/shop/shop.test/content.ui":  "<content><form method=\"post\">" + c.page + "</form></content>",
			})
			b := boot(t, filepath.Join(folder, "application.work"))
			got := b.get(t, "shop.test", "/")
			if got.status == http.StatusOK || !strings.Contains(b.log.String(), c.says) {
				t.Errorf("GET /: %d, the log says %q, want %q", got.status, b.log.String(), c.says)
			}
		})
	}
}

// A select bound to a field whose options the page writes, from data (the rows of a foreign key) or by hand, marks
// the one that is the field's value; an option's value is written as the page writes it.
//
// CONTROL (07/11/2026): with option not reading the select around it, no option was selected.
func TestTheOptionsOfABoundSelectAreThePages(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/core/schema/shop.work": `import { struct, string, integer, choice } from "kitwork/schema";
export const order = struct({ shopId: integer(), size: choice("s", "m", "l") });
`,
		"apps/shop/shop.test/router.work": routerImport + `import { order } from "app/core/schema/shop.work";
router.get((context) => context.view({ form: order.form({ shopId: 2, size: "m" }), shops: [{ id: 1, name: "Một" }, { id: 2, name: "Hai" }] }));
`,
		"apps/shop/shop.test/content.ui": `<content><form method="post">
<select value="{{ form.shopId }}"><option for="shop of shops" value="{{ shop.id }}">{{ shop.name }}</option></select>
<select value="{{ form.size }}"><option value="s">Nhỏ</option><option value="m">Vừa</option><option value="l">Lớn</option></select>
</form></content>`,
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	got := b.get(t, "shop.test", "/")
	if got.status != http.StatusOK {
		t.Fatalf("GET /: %d %s\n%s", got.status, got.body, b.log.String())
	}
	holds(t, "GET /", got.body,
		`name="shopId" required><option value="1">Một</option><option value="2" selected>Hai</option></select>`,
		`name="size" required><option value="s">Nhỏ</option><option value="m" selected>Vừa</option><option value="l">Lớn</option></select>`,
	)
}

// holds fails for each piece the body does not hold.
func holds(t *testing.T, label, body string, pieces ...string) {
	t.Helper()
	for _, piece := range pieces {
		if !strings.Contains(body, piece) {
			start := strings.Index(body, "<form")
			if start < 0 {
				start = 0
			}
			t.Errorf("%s does not hold %s\n%s", label, piece, body[start:])
			return
		}
	}
}
