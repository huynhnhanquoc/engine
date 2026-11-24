// Demo "gốc theo tầng" (23/11/2026, thử hướng, chưa chốt): mỗi tầng có một bản thiết kế gốc mà máy đọc từ đó xuống.
//
//   - Gốc của máy là Kitwork: không có trong mã, vì mọi dòng .work đã chạy trong nó; nên thư viện gọi bằng tên trần
//     ("host", "module"), không còn "kitwork/…".
//   - Gốc của host: application.work trả app (const app = application(…), mỗi cửa một câu, return app.run(): cách viết
//     của owner 24/11/2026), import từ "host".
//   - Gốc của mỗi app: module.work trả module(…), import từ "module". Mỗi tầng chỉ thấy thư viện của nó: một app
//     không với tới "host".
//   - Gọi lên theo tầng: múi giờ của app là của module, không có thì của application, không có nữa thì UTC.
//
// VM của engine3 chạy thật mọi file; hàm routes của module được VM gọi cho từng tên miền, như host sẽ gọi lúc boot.
// Cửa module ở đây là một struct của demo (moduleRoot), chưa phải gói blueprint/module. Chạy: go run ./cmd/root
package main

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// ---- Các tầng và thư viện của chúng ----

// hostLibraries là thư viện application.work thấy: chỉ "host", và nó chỉ đưa ra application (các cửa gom về
// application: app.website(…), app.database(…)).
var hostLibraries = map[string]map[string]value.Value{
	"host": {
		"application": native("application", application.New),
	},
}

// appLibraries là thư viện module.work của một app thấy: chỉ "module".
var appLibraries = map[string]map[string]value.Value{
	"module": {"module": native("module", newModule)},
}

func native(name string, build func(...value.Value) value.Value) value.Value {
	return javascript.NewNative(name, 1, func(_ javascript.Run, arguments []value.Value) value.Value { return build(arguments...) })
}

// ---- Các file ----

const applicationWork = `
import { application } from "host";   // thư viện gọi bằng tên: máy là gốc, không cần nói "kitwork/"

const app = application({ timezone: "Asia/Ho_Chi_Minh" });

app.website({ certificates: ".certificates" })
  .routes((context) => ` + "`apps/${context.request.tenant.identity}`" + `)
  .listen(443);

return app.run();
`

// app là một thư mục dưới apps/: module.work của nó, và các tên miền bảng domain giao cho nó.
type app struct {
	identity string
	source   string
	domains  []string
}

var apps = []app{
	{"shop", `
import { module } from "module";

// không khai timezone: gọi lên gốc của chương trình
return module().routes((site) => site.domain.endsWith(".vn") ? "sites/vn" : "global");
`, []string{"shop.vn", "shop.com"}},

	{"blog", `
import { module } from "module";

return module({ timezone: "Europe/Paris", routes: (site) => site.domain });   // module thắng application
`, []string{"blog.dev", "blog.fr"}},

	{"reach", `
import { application } from "host";   // một app với tới thư viện của host
return application().website().listen(80).run();
`, []string{"reach.vn"}},

	{"typo", `
import { module } from "module";

return module({ timezone: "Asia/Saigonn", routes: "global" });
`, []string{"typo.vn"}},

	{"oldprefix", `
import { module } from "kitwork/module";   // tiền tố cũ
return module();
`, []string{"old.vn"}},
}

// ---- Chạy ----

func main() {
	line("═")
	fmt.Println("gốc của máy: Kitwork (không có trong mã: mọi dòng .work đã chạy trong nó)")
	line("═")
	fmt.Print("application.work" + applicationWork)
	made, err := run("application.work", applicationWork, hostLibraries)
	if err != nil {
		fmt.Println("✗", err)
		return
	}
	a, ok := value.As[application.Application](made)
	if !ok {
		fmt.Println("✗ application.work: must return the application: end with return app.run()")
		return
	}
	zone, problems := timezone("application.timezone", a.Fields().Timezone, "UTC")
	report(problems)
	made.FreezeShared()
	fmt.Println("gốc của chương trình: application")
	fmt.Println("   timezone:", zone)
	if w, ok := value.As[website.Website](a.Fields().Website); ok {
		fmt.Println("   website: listen", len(w.Fields().Listen), "địa chỉ, routes là", source(w.Fields().Routes))
	}
	fmt.Println()

	for _, x := range apps {
		file := "apps/" + x.identity + "/module.work"
		line("─")
		fmt.Print(file + x.source)
		loaded, err := run(file, x.source, appLibraries)
		if err != nil {
			fmt.Println("   ✗", err)
			fmt.Println()
			continue
		}
		m, ok := value.As[moduleRoot](loaded)
		if !ok {
			fmt.Println("   ✗", file+": must return module(…), got", loaded.Typeof())
			fmt.Println()
			continue
		}
		at := "apps/" + x.identity + " › module"
		appZone, problems := timezone(at+".timezone", m.fields.Timezone, zone)
		var sites []string
		switch routes := m.fields.Routes; {
		case routes.IsUndefined():
			sites = append(sites, "mọi tên miền → gốc app (không có routes)")
		case !routes.IsFunction():
			problems = append(problems, fmt.Sprintf("%s.routes: takes a function of the site, (site) => …, got %s", at, routes.Typeof()))
		default:
			for _, domain := range x.domains {
				// VM gọi hàm của module cho từng tên, trong trần của một lượt chạy
				folder := runtime.New(runtime.Options{Energy: 100_000}).Call(routes, value.Object().Set("domain", value.String(domain)))
				switch {
				case folder.IsInvalid():
					problems = append(problems, fmt.Sprintf("%s.routes(%s): %v", at, domain, folder.Error()))
				case !folder.IsString():
					problems = append(problems, fmt.Sprintf("%s.routes(%s): returns a folder, a string, got %s", at, domain, folder.Typeof()))
				default:
					sites = append(sites, domain+" → apps/"+x.identity+"/"+folder.String())
				}
			}
		}
		if len(problems) > 0 {
			report(problems)
			fmt.Println()
			continue
		}
		loaded.FreezeShared()
		fmt.Println("   gốc của app:", x.identity)
		origin := "của module"
		if m.fields.Timezone.IsUndefined() {
			origin = "gọi lên application"
		}
		fmt.Printf("   timezone: %s (%s)\n", appZone, origin)
		for _, s := range sites {
			fmt.Println("   site:", s)
		}
		fmt.Println()
	}
}

// run biên dịch và chạy một file với thư viện của tầng nó.
func run(file, source string, libraries map[string]map[string]value.Value) (value.Value, error) {
	program, err := syntax.Parse(file, strings.TrimLeft(source, "\n"))
	if err != nil {
		return value.Value{}, err
	}
	main, err := compiler.Compile(program, compiler.Options{Modules: libraries})
	if err != nil {
		return value.Value{}, err
	}
	result := runtime.New(runtime.Options{}).Run(main)
	if result.IsInvalid() {
		return value.Value{}, result.Error()
	}
	return result, nil
}

// timezone đọc một múi giờ; vắng thì là của tầng trên (above).
func timezone(at string, v value.Value, above string) (string, []string) {
	if v.IsUndefined() {
		return above, nil
	}
	if !v.IsString() {
		return above, []string{fmt.Sprintf("%s: takes a zone like \"Asia/Ho_Chi_Minh\", got %s", at, v.Typeof())}
	}
	if _, err := time.LoadLocation(v.String()); err != nil || v.String() == "Local" {
		return above, []string{fmt.Sprintf("%s: %q is no time zone: write one like \"Asia/Ho_Chi_Minh\"", at, v.String())}
	}
	return v.String(), nil
}

func report(problems []string) {
	for _, p := range problems {
		fmt.Println("   ✗", p)
	}
}

func source(v value.Value) string {
	if c, ok := v.Function().(*runtime.Closure); ok {
		return "hàm ở " + c.At()
	}
	return v.Typeof()
}

func line(s string) { fmt.Println(strings.Repeat(s, 100)) }

// ---- Cửa module của demo: thụ động như website, chỉ giữ ----

// moduleRoot là bản thiết kế gốc của một app: module(…) của module.work.
type moduleRoot struct {
	fields moduleFields
	shared bool
}

type moduleFields struct {
	Timezone value.Value // "Europe/Paris": múi giờ của cả app; vắng: của application, rồi UTC
	Routes   value.Value // (site) => "thư mục": thư mục của từng tên miền; vắng: app là một site
}

// newModule là module(), module({ timezone, routes }).
func newModule(options ...value.Value) value.Value {
	m := &moduleRoot{}
	if len(options) == 0 || options[0].IsUndefined() {
		return value.Struct(m)
	}
	if len(options) > 1 || !options[0].IsObject() {
		return value.Fail(value.TypeCode, "module takes module() or module({ timezone, routes })")
	}
	for _, key := range options[0].Keys() {
		item, _ := options[0].Get(key)
		var result value.Value
		switch key {
		case "timezone":
			result = m.Timezone(item)
		case "routes":
			result = m.Routes(item)
		default:
			return value.Fail(value.TypeCode, "module has no key %q: timezone, routes", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(m)
}

func (m *moduleRoot) Timezone(v value.Value) value.Value { return m.set(&m.fields.Timezone, v) }
func (m *moduleRoot) Routes(v value.Value) value.Value   { return m.set(&m.fields.Routes, v) }

func (m *moduleRoot) set(field *value.Value, v value.Value) value.Value {
	if m.shared {
		return value.Fail(value.TypeCode, "module was loaded by the engine and is read only now")
	}
	*field = v
	return value.Struct(m)
}

// Share là value.Sharer: đóng băng cả cây của module, qua hàm routes tới biến nó bắt.
func (m *moduleRoot) Share() []value.Value {
	if m.shared {
		return nil
	}
	m.shared = true
	return value.Held(m.fields)
}
