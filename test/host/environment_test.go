package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// Môi trường của host (owner 23/11/2026): environment("./.env") của "application"; env.KEY và env.require("KEY").

// folder là thư mục của một application.work trong test, với các file của nó.
func folder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const environmentHead = `import { application, environment } from "application";
`

// boot chạy source (sau dòng import) trong thư mục dir, với các biến của hệ điều hành variables.
func boot(dir, source string, variables map[string]string) (host.Booted, error) {
	return host.Program{File: "application.work", Source: environmentHead + source, Folder: dir, System: func(name string) (string, bool) {
		v, found := variables[name]
		return v, found
	}}.Boot()
}

func TestEnvironmentReadsAndCoerces(t *testing.T) {
	dir := folder(t, map[string]string{
		".env": strings.Join([]string{
			"# the host's values",
			"KITDB_PASSWORD=secret",
			"PORT=5445",
			"export DEBUG=TRUE",
			`ZIP="0123"`,
			"VERSION=1.10",
			"PHONE=+84912345678",
			"RATIO=-0.5",
			"EMPTY=",
			"SHARED=from the file",
			"OVERRIDDEN=first",
			`ANSWER="42"`,
			"FLAG='true'",
		}, "\n"),
		"production.env": "OVERRIDDEN=second\n",
	})
	source := `const env = environment("./.env", "./production.env");
const app = application({ timezone: env.TZ ?? "UTC" });
app.database({ listen: env.PORT, password: env.KITDB_PASSWORD }).open(".database");
app.website().routes(() => [env.PORT, env.DEBUG, env.ZIP, env.VERSION, env.PHONE, env.RATIO, env.EMPTY, env.MISSING,
  env.SHARED, env.OVERRIDDEN, env.require("KITDB_PASSWORD"), env.ANSWER, env.FLAG]).listen(443);
return app.run();
`
	booted, err := boot(dir, source, map[string]string{"SHARED": "from the system", "TZ": ""})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := value.As[application.Application](booted.Blueprint)
	if a.Fields().Timezone.String() != "UTC" {
		t.Errorf("an empty variable of the system is no value: %v", a.Fields().Timezone)
	}
	d, _ := value.As[database.Database](a.Fields().Database)
	if d.Fields().Password.String() != "secret" || d.Fields().Listen[0].Number() != 5445 {
		t.Errorf("the blueprint reads the environment: %v", d.Fields().Listen)
	}
	// các giá trị, đọc trong một hàm chạy sau boot, như lúc request
	w, _ := value.As[website.Website](a.Fields().Website)
	got := runtime.New(runtime.Options{}).Call(w.Fields().Routes)
	want := []value.Value{
		value.Number(5445), value.Boolean(true), value.String("0123"), value.String("1.10"), value.String("+84912345678"),
		value.Number(-0.5), value.Null(), value.Null(), value.String("from the system"), value.String("second"),
		value.String("secret"), value.String("42"), value.String("true"), // trong ngoặc: luôn là chuỗi
	}
	if got.Length() != len(want) {
		t.Fatalf("%v", got.Error())
	}
	for i, item := range got.Items() {
		if !javascript.StrictEquals(item, want[i]) {
			t.Errorf("value %d: %s %v, want %s %v", i, item.Typeof(), item, want[i].Typeof(), want[i])
		}
	}
	// EMPTY= rỗng là không có giá trị; TZ của hệ điều hành rỗng cũng vậy
	wantNotes := []string{
		"env.TZ (application.work:3:41) has no value in any file nor in the system's variables: it reads null",
		"env.EMPTY (application.work:5:98) has no value in any file nor in the system's variables: it reads null",
		"env.MISSING (application.work:5:109) has no value in any file nor in the system's variables: it reads null",
	}
	if strings.Join(booted.Notes, "\n") != strings.Join(wantNotes, "\n") {
		t.Errorf("notes:\n%s", strings.Join(booted.Notes, "\n"))
	}
}

func TestEnvironmentWithoutAFile(t *testing.T) {
	// production thường không có .env: chỉ còn biến của hệ điều hành, không phải lỗi
	booted, err := boot(t.TempDir(), `const env = environment("./.env");
return application({ timezone: env.require("TZ") }).check();
`, map[string]string{"TZ": "Asia/Ho_Chi_Minh"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := value.As[application.Application](booted.Blueprint)
	if a.Fields().Timezone.String() != "Asia/Ho_Chi_Minh" || len(booted.Notes) != 1 ||
		booted.Notes[0] != `environment("./.env"): no such file, the system's variables only` {
		t.Errorf("timezone %v, notes %q", a.Fields().Timezone, booted.Notes)
	}
}

func TestEnvironmentNotes(t *testing.T) {
	dir := folder(t, map[string]string{".env": "KITDB_PASSWORD=x\nrequire=y\n"})
	booted, err := boot(dir, `const env = environment("./.env");
const app = application();
app.website().guard((context) => env.KITDB_PASWORD ?? context.next()).listen(443);
return app.run();
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`application.work:2:7: a file sets a key named require: env.require is the word, read the key with env.require("require")`,
		`env.KITDB_PASWORD (application.work:4:38) has no value in any file nor in the system's variables: it reads null: did you mean KITDB_PASSWORD?`,
	}
	if strings.Join(booted.Notes, "\n") != strings.Join(want, "\n") {
		t.Errorf("notes:\n%s\nwant:\n%s", strings.Join(booted.Notes, "\n"), strings.Join(want, "\n"))
	}
}

func TestEnvironmentErrors(t *testing.T) {
	dir := folder(t, map[string]string{".env": "PORT=8080\n"})
	if err := os.Mkdir(filepath.Join(dir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, source, want string }{
		// mọi khoá bắt buộc thiếu, trong một lời báo, kể cả trong một hàm chỉ chạy lúc request
		{"every missing required key at once", `const env = environment("./.env");
const app = application({ timezone: env.require("TZ") });
app.website().guard((context) => env.require("STRIPE_SECRET") ? context.next() : null).listen(env.require("PORT"));
return app.run();`,
			"application.work: env.require: no file and no variable of the system sets TZ (application.work:3:41), STRIPE_SECRET (application.work:4:38)"},
		{"not at the top of the file", `const f = () => environment("./.env");
return application();`,
			`application.work:2:17: environment(…) is written once at the top of the file: const env = environment("./.env")`},
		{"let", `let env = environment("./.env");
return application();`,
			"application.work:2:5: write const env = environment(…): what it reads never changes"},
		{"a path not written in place", `const path = "./.env";
const env = environment(path);
return application();`,
			`application.work:3:25: environment(…) takes each file as text written in place: environment("./.env")`},
		{"no file named", `const env = environment();
return application();`,
			`application.work:2:24: environment(…) takes the files it reads: environment("./.env")`},
		{"outside the folder", `const env = environment("../.env");
return application();`,
			`application.work:2:7: environment("../.env"): the path is outside the folder of application.work: write it from there, like "./.env"`},
		{"a folder", `const env = environment("./config");
return application();`,
			`application.work:2:7: environment("./config") is a folder: name a file, like "./.env"`},
		{"env passed whole", `const env = environment("./.env");
const copy = env;
return application();`,
			`application.work:3:14: env is read by name only: write env.KEY or env.require("KEY"), never env itself`},
		{"env in an object", `const env = environment("./.env");
return application().run({ env });`,
			`application.work:3:28: env is read by name only: write env.KEY or env.require("KEY"), never env itself`},
		{"env[…]", `const env = environment("./.env");
const port = env["PORT"];
return application();`,
			`application.work:3:17: env is read by name only: write env.KEY, not env[…]`},
		{"a write", `const env = environment("./.env");
env.PORT = 1;
return application();`,
			"application.work:3:5: env is read only: the environment is read once, at boot"},
		{"a required name not written in place", `const env = environment("./.env");
const key = "PORT";
return application({ timezone: env.require(key) });`,
			`application.work:4:43: env.require(…) takes the key's name written in place: env.require("STRIPE_SECRET")`},
		{"a key called", `const env = environment("./.env");
return application({ timezone: env.PORT() });`,
			"application.work:3:36: PORT is a key of the environment, a value, not a function"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := boot(dir, c.source, nil)
			if err == nil || err.Error() != c.want {
				t.Errorf("got  %v\nwant %s", err, c.want)
			}
		})
	}

	// một tham số cùng tên che env: không phải env của file
	if _, err := boot(dir, `const env = environment("./.env");
const app = application();
app.website().guard((env) => env).listen(443);
return app.run();`, nil); err != nil {
		t.Errorf("a parameter named env hides it: %v", err)
	}
	// tiền tố cũ nói tên mới
	if _, err := host.Boot("application.work", `import { website } from "kitwork/host";`); err == nil ||
		err.Error() != `application.work:1:1: there is no module "kitwork/host": libraries are imported by their name, without "kitwork/": "application"` {
		t.Errorf("the old prefix: %v", err)
	}
}
