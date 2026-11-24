package host_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// shopSchema is the app's struct for the tests of kitwork/schema: every kind a form or a request's JSON brings,
// two transforms, one of them a function of the file's top level.
const shopSchema = `import { struct, string, integer, decimal, boolean, date, choice, uuid, array } from "kitwork/schema";

const email = (value) => value.trim().toLowerCase();

export const signup = struct({
  email: string().transform(email).max(254).email(),
  name: string().transform((value) => value.trim()).min(1).max(100),
  age: integer().min(0).nullable(),
  plan: choice("free", "pro").default("free"),
  amount: decimal(2).min(0).nullable(),
  agree: boolean().default(false),
  day: date().nullable(),
  ref: uuid().nullable(),
  tags: array(string().min(1)).max(3).nullable()
});
`

// schemaApp is an app whose / parses a POST's JSON with signup inside safe, /raw without, and /pick with
// signup.pick("name", "age").
func schemaApp(t *testing.T) *booted {
	t.Helper()
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/schema/shop.work": shopSchema,
		"apps/shop/shop.test/router.work": routerImport + `import { signup } from "app/core/schema/shop.work";
router.post((context) => {
  const { value, error } = safe(() => signup.parse(context.request.json()));
  if (error) {
    return context.json({ code: error.code, message: error.message });
  }
  return context.json(value);
});
`,
		"apps/shop/shop.test/raw/router.work": routerImport + `import { signup } from "app/core/schema/shop.work";
router.post((context) => context.json(signup.parse(context.request.json())));
`,
		"apps/shop/shop.test/pick/router.work": routerImport + `import { signup } from "app/core/schema/shop.work";
const editable = signup.pick("name", "age");
router.post((context) => context.json(editable.parse(context.request.json())));
`,
	})
	return boot(t, filepath.Join(folder, "application.work"))
}

func postJSON(t *testing.T, app *booted, path, body string) (int, map[string]any, string) {
	t.Helper()
	got := app.request(t, "POST", "shop.test", path, strings.NewReader(body), "Content-Type", "application/json")
	var read map[string]any
	if got.status == 200 {
		if err := json.Unmarshal([]byte(got.body), &read); err != nil {
			t.Fatalf("POST %s: %d %q is not JSON: %v", path, got.status, got.body, err)
		}
	}
	return got.status, read, got.body
}

// struct.parse reads a request's JSON into the struct's fields (docs/schema.md mục 6): a form's text read as
// its kind ("28" is 28, "on" is true, "35000.50" is 35000.5), the transforms run (one of them a function of
// the schema file's top level, which a request runs only because the VM hands the struct over: value.Keeper),
// a UUID kept lower case, the defaults of the fields left out, a key the struct does not have left out.
//
// CONTROL (06/11/2026): with nativeMethod not handing a Keeper over, the request answers "TYPE: the function
// email was made as the site booted, so a request runs it only by calling it or handing it to a method".
func TestAStructParsesARequestsJSON(t *testing.T) {
	app := schemaApp(t)
	status, got, body := postJSON(t, app, "/", `{"email":"  AN@Example.com ","name":" An ","age":"28","amount":"35000.50","agree":"on",`+
		`"day":"2026-11-06","ref":"0192F3A4-5B6C-7D8E-9FA0-B1C2D3E4F5A6","tags":["a","b"],"extra":"x"}`)
	if status != 200 || got["code"] != nil {
		t.Fatalf("POST /: %d %s", status, body)
	}
	want := map[string]any{
		"email": "an@example.com", "name": "An", "age": 28.0, "plan": "free", "amount": 35000.5, "agree": true,
		"day": "2026-11-06", "ref": "0192f3a4-5b6c-7d8e-9fa0-b1c2d3e4f5a6",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %#v, want %#v (%s)", key, got[key], value, body)
		}
	}
	if tags, _ := got["tags"].([]any); len(tags) != 2 {
		t.Errorf("tags = %#v", got["tags"])
	}
	if _, extra := got["extra"]; extra {
		t.Errorf("a key the struct does not have came through: %s", body)
	}
	if !strings.HasPrefix(body, `{"email":`) || strings.Index(body, `"plan"`) > strings.Index(body, `"tags"`) {
		t.Errorf("the fields are not in the order written: %s", body)
	}

	// requests at once run the same struct's transforms, each on its own VM
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got := app.request(t, "POST", "shop.test", "/", strings.NewReader(`{"email":" B@X.VN ","name":"B"}`), "Content-Type", "application/json")
			if got.status != 200 || !strings.Contains(got.body, `"email":"b@x.vn"`) {
				t.Errorf("a request at once: %d %s", got.status, got.body)
			}
		}()
	}
	wait.Wait()
}

// Data that is wrong is one INVALID_INPUT failure naming each field and what is wrong with it, never a value
// written (a form's text can be a password); safe() catches it as any failure.
func TestAStructSaysEveryFieldThatIsWrong(t *testing.T) {
	app := schemaApp(t)
	status, got, body := postJSON(t, app, "/", `{"email":"not-an-email","name":"   ","age":-1,"plan":"gold","amount":0.123,`+
		`"day":"2026-02-30","ref":"x","tags":["","b"]}`)
	if status != 200 || got["code"] != "INVALID_INPUT" {
		t.Fatalf("POST /: %d %s", status, body)
	}
	message, _ := got["message"].(string)
	for _, want := range []string{
		"dữ liệu không hợp lệ: email: không phải địa chỉ email",
		"name: cần ít nhất 1 ký tự",
		"age: nhỏ nhất là 0",
		`plan: cần một trong "free", "pro"`,
		"amount: quá 2 chữ số sau dấu phẩy",
		"day: cần ngày YYYY-MM-DD",
		"ref: cần UUID (8-4-4-4-12 chữ số hệ 16)",
		"tags[0]: cần ít nhất 1 ký tự",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the failure says %q, want %q in it", message, want)
		}
	}
	for _, secret := range []string{"not-an-email", "gold", "0.123", "2026-02-30"} {
		if strings.Contains(message, secret) {
			t.Errorf("the failure shows a value written, %q: %q", secret, message)
		}
	}
	// a failure of the host is led by where the call is written when no program code ran in the call
	// ("router.work:4:45: …"), and is as it is when a transform ran (runtime/interpreter.go): the message
	// ends with what is wrong either way
	if status, got, body := postJSON(t, app, "/", `{"name":"An"}`); status != 200 || !strings.HasSuffix(fmt.Sprint(got["message"]), "dữ liệu không hợp lệ: email: bắt buộc") {
		t.Errorf("a required field left out: %d %s", status, body)
	}
	if status, got, body := postJSON(t, app, "/", `["not", "an", "object"]`); status != 200 || !strings.HasSuffix(fmt.Sprint(got["message"]), "dữ liệu không hợp lệ: cần object") {
		t.Errorf("an array: %d %s", status, body)
	}
}

// A failure of data from outside that the handler does not catch is the visitor's to fix: 400, the reason in
// the log and not in the answer (06/11/2026; every failure was a 500 until then).
//
// CONTROL (06/11/2026): without the INVALID_INPUT case of serve, the answer was 500 Internal Server Error.
func TestAnUncaughtParseIsABadRequest(t *testing.T) {
	app := schemaApp(t)
	got := app.request(t, "POST", "shop.test", "/raw", strings.NewReader(`{"email":"x"}`), "Content-Type", "application/json")
	if got.status != 400 || strings.Contains(got.body, "email") {
		t.Errorf("POST /raw: %d %q, want 400 without the reason", got.status, got.body)
	}
	if !strings.Contains(app.log.String(), "400: INVALID_INPUT: dữ liệu không hợp lệ: email: không phải địa chỉ email") {
		t.Errorf("the log does not say why:\n%s", app.log)
	}
}

// pick is a struct of the fields named, with their rules: a form that edits the name and the age takes
// nothing else, whatever the request sends.
func TestPickTakesTheFieldsNamedOnly(t *testing.T) {
	app := schemaApp(t)
	status, got, body := postJSON(t, app, "/pick", `{"name":" An ","email":"evil@x.vn","plan":"pro"}`)
	if status != 200 || got["name"] != "An" || got["age"] != nil || len(got) != 2 {
		t.Errorf("POST /pick: %d %s, want name and age alone", status, body)
	}
}

// What a struct is written with wrong stops the boot where it is written (docs/engine.md mục 1, fail closed):
// a word of a table on a struct no tables({ … }) takes (the app's pass over core/schema/ says where), an older
// word, a rule on a kind it does not fit, a default the field's rules refuse, a field named as a method of
// struct, and kitwork/schema imported by a file with a role.
//
// CONTROL (06/11/2026): with Kitwork.Schema not reading where the import is written, the struct of router.work
// booted; with Catalog.Close not reading TableWord, the shape with .unique() booted.
func TestAStructWrittenWrongStopsTheBoot(t *testing.T) {
	schemaFile := func(text string) map[string]string {
		return map[string]string{
			"apps/shop/core/schema/shop.work": "import { struct, string, integer, id, text, table } from \"kitwork/schema\";\n" + text,
			"apps/shop/shop.test/router.work": routerImport + "import { shape } from \"app/core/schema/shop.work\";\n" +
				"router.get((context) => context.text(\"a\"));\n",
		}
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a word of a table on no table", schemaFile("export const shape = struct({ email: string().unique() });\n"),
			"core/schema/shop.work:2:22: struct có email: .unique() mà không là bảng"},
		{"a key on no table", schemaFile("export const shape = struct({ id: id() });\n"), "struct có id: id() mà không là bảng"},
		{"text()", schemaFile("export const shape = struct({ name: text() });\n"), "text() là cách viết cũ: viết string()"},
		{"table()", schemaFile("export const shape = table({ name: string() });\n"), "table() là cách viết cũ: viết struct({ … })"},
		{"email on a number", schemaFile("export const shape = struct({ age: integer().email() });\n"), ".email() chỉ đi sau string(), không sau integer()"},
		{"a default the rules refuse", schemaFile("export const shape = struct({ name: string().default(\"\").min(1) });\n"),
			"trường name: .default không hợp lệ: cần ít nhất 1 ký tự"},
		{"a field named parse", schemaFile("export const shape = struct({ parse: string() });\n"), "trường parse trùng tên phương thức của struct"},
		{"no field", schemaFile("export const shape = struct({});\n"), "struct cần ít nhất một trường"},
		{"in router.work", map[string]string{
			"apps/shop/shop.test/router.work": "import { struct, string } from \"kitwork/schema\";\n" + routerImport +
				"const shape = struct({ name: string() });\nrouter.get((context) => context.text(\"a\"));\n",
		}, "kitwork/schema is imported by shop.test/router.work, a file with a role: a struct is declared in a plain .work file of the app"},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s: the boot ends with %q, want it to say %q", c.name, got, c.want)
		}
	}
}
