package host_test

import (
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

// Boot chạy application.work thật (23/11/2026): VM gọi đúng các chữ của bản thiết kế, thay cho demo đóng vai VM.

const shop = `import { application } from "application";

const seen = {};                                   // biến cấp file, một guard ghi vào nó
let visits = 0;                                    // một guard gán vào nó
const https = (context) => context.next();

const app = application({ memory: "2gb", timezone: "Asia/Ho_Chi_Minh" });

app.website()
  .certificates(".certificates")
  .guard(https)
  .guard((context) => { seen[context.request.ip] = true; return context.next(); })
  .guard((context) => { visits = visits + 1; return context.next(); })
  .routes((context) => ` + "`apps/${context.request.hostname}`" + `)
  .memory((context) => context.request.tenant.memory ?? "64mb")
  .listen(443, 8443, 80);                          // chữ cuối nhận nhiều địa chỉ, và trả về app

app.database({ listen: "127.0.0.1:5445", password: "secret" }).open(".database");

return app.run();
`

func TestBoot(t *testing.T) {
	made, err := host.Boot("application.work", shop)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := value.As[application.Application](made)
	f := a.Fields()
	w, ok := value.As[website.Website](f.Website)
	if !ok || !a.Running() || f.Memory.String() != "2gb" || f.Timezone.String() != "Asia/Ho_Chi_Minh" {
		t.Fatalf("the application: running %v, memory %v", a.Running(), f.Memory.String())
	}
	if d, ok := value.As[database.Database](f.Database); !ok || d.Fields().Open.String() != ".database" || len(d.Fields().Listen) != 1 {
		t.Error("app.database(…).open(…) writes the database door")
	}
	wf := w.Fields()
	if len(wf.Listen) != 3 || wf.Listen[0].Number() != 443 || wf.Listen[1].Number() != 8443 || wf.Listen[2].Number() != 80 || len(wf.Guards) != 3 ||
		!wf.Routes.IsFunction() || !wf.Memory.IsFunction() || wf.Certificates.String() != ".certificates" {
		t.Fatalf("the website as written: %d listen, %d guards", len(wf.Listen), len(wf.Guards))
	}
	if name := wf.Guards[0].Function().(*runtime.Closure).Name(); name != "https" {
		t.Errorf("a function keeps the name of its const: %q", name)
	}

	// trước khi đóng băng, một guard chạy được và ghi vào biến cấp file
	m := runtime.New(runtime.Options{})
	context := value.Object().
		Set("request", value.Object().Set("ip", value.String("203.0.113.7")).Set("hostname", value.String("shop.vn"))).
		Set("next", javascript.NewNative("next", 0, func(javascript.Run, []value.Value) value.Value { return value.String("next") }))
	if r := m.Call(wf.Guards[1], context); r.IsInvalid() || r.String() != "next" {
		t.Fatalf("the guard runs before the blueprint is loaded: %v", r.Error())
	}
	if r := m.Call(wf.Routes, context); r.String() != "apps/shop.vn" {
		t.Errorf("routes: %v", r)
	}

	// engine nạp xong và đóng băng cả cây, qua các closure tới biến chúng bắt
	made.FreezeShared()
	r := m.Call(wf.Guards[1], context.Set("request", value.Object().Set("ip", value.String("198.51.100.1"))))
	if !r.IsInvalid() || r.Code() != value.TypeCode || !strings.Contains(r.Error().Error(), "shared by every run") ||
		!strings.HasPrefix(r.At(), "application.work:12:") {
		t.Errorf("a guard writing a file-level object after boot: %q at %q", r.Error(), r.At())
	}
	r = m.Call(wf.Guards[2], context)
	if !r.IsInvalid() || r.Code() != value.TypeCode || !strings.Contains(r.Error().Error(), "cannot assign to a variable this function holds") {
		t.Errorf("a guard assigning a file-level let after boot: %v", r.Error())
	}
	if r := w.Guard(value.Undefined()); !r.IsInvalid() {
		t.Error("a word on the website after boot is refused")
	}
}

// Một câu nối liền cũng là bản thiết kế ấy: mỗi chữ cuối trả về app, nên các cửa nối nhau được, và run đứng cuối.
func TestBootInOneSentence(t *testing.T) {
	made, err := host.Boot("application.work", `import { application } from "application";
return application({ memory: "1gb" }).website().routes("apps/shop").listen(8080).database().open(".database").check();`)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := value.As[application.Application](made)
	w, _ := value.As[website.Website](a.Fields().Website)
	d, _ := value.As[database.Database](a.Fields().Database)
	if w == nil || d == nil || !w.Closed() || !d.Closed() || !a.Checking() || a.Running() || len(a.Dangling()) != 0 {
		t.Error("one sentence: a website, a database, both closed, and check")
	}
}

func TestBootErrors(t *testing.T) {
	head := `import { application } from "application";` + "\n"
	for _, c := range []struct{ name, source, want string }{
		{"a key that is no word", head + "return application({ memry: \"2gb\" }).run();",
			`application.work:2:8: application has no key "memry": memory, timezone, desktop, mobile`},
		{"nothing returned", head + "const app = application();",
			"application.work: must return the application, got nothing: end the file with return app.run()"},
		{"something else returned", head + "return 5;",
			"application.work: must return the application, got number"},
		{"an import the module has not", `import { cron } from "application";`,
			`application.work:1:10: the module "application" has no cron: application, environment`},
		{"a name not defined", head + "return application().memory(size).run();",
			"application.work:2:29: size is not defined: declare it with const or let, or import it"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := host.Boot("application.work", c.source)
			if err == nil || err.Error() != c.want {
				t.Errorf("got  %v\nwant %s", err, c.want)
			}
		})
	}
}
