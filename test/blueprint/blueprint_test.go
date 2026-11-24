package blueprint_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Blueprint thụ động: mỗi chữ chỉ ghi đúng giá trị chương trình đưa vào, kể cả giá trị sai (engine kiểm khi nạp);
// danh sách thêm vào cuối theo thứ tự viết, trường một giá trị lần sau đè lần trước; Share đóng băng được cả cây từ
// Application xuống. Các cửa gom về application (owner 24/11/2026): chỉ app dựng được cửa, và câu của một cửa kết thúc
// bằng chữ cuối của nó, chữ trả về app.

// fn là một hàm của chương trình trong test: value chỉ mang nó.
type fn struct{ name string }

// capturing là một closure bắt được vài giá trị, như closure của runtime: một value.Sharer.
type capturing struct {
	held   []value.Value
	shared bool
}

func (c *capturing) Share() []value.Value {
	if c.shared {
		return nil
	}
	c.shared = true
	return c.held
}

func function(name string) value.Value { return value.Function(&fn{name}) }

func websiteOf(t *testing.T, v value.Value) *website.Website {
	t.Helper()
	w, ok := value.As[website.Website](v)
	if !ok {
		t.Fatalf("not a website: %s", v.Typeof())
	}
	return w
}

func TestWebsiteKeepsWhatItIsGiven(t *testing.T) {
	w := websiteOf(t, website.New(alone()))
	w.Listen(value.Number(8080), value.String("not an address"))
	w.Listen(value.Number(70000)) // sai, nhưng blueprint không kiểm: engine báo khi nạp
	w.Memory(value.String("64 mb"))
	w.Memory(value.String("128mb"))
	w.Guard(function("a"))
	w.Guard(function("b"), value.Number(5))
	w.SSL(value.Null())
	w.Warm(value.Boolean(true))
	w.Routes(value.String("apps/x"))
	w.Success(function("ok"))
	w.Error(function("bad"))
	w.Finally(function("end"))
	// mỗi chữ trả lại chính Website, để JS viết nối nhau (app.website().guard(…).routes(…)); chữ cuối thì không
	if returned := w.Routes(value.String("apps/x")); websiteOf(t, returned) != w {
		t.Error("a word gives the Website back")
	}

	f := w.Fields()
	if len(f.Listen) != 3 || f.Listen[0].Number() != 8080 || f.Listen[1].String() != "not an address" || f.Listen[2].Number() != 70000 {
		t.Errorf("listen: %v", len(f.Listen))
	}
	if f.Memory.String() != "128mb" {
		t.Errorf("one value: the later wins, %q", f.Memory.String())
	}
	if len(f.Guards) != 3 || f.Guards[0].Function().(*fn).name != "a" || f.Guards[1].Function().(*fn).name != "b" || !f.Guards[2].IsNumber() {
		t.Error("guards: in the order written, a number kept as it is")
	}
	if !f.SSL.IsNull() || !f.Warm.Boolean() || f.Routes.String() != "apps/x" || len(f.Success) != 1 || len(f.Errors) != 1 || len(f.Finally) != 1 {
		t.Error("the other fields")
	}
	if !f.Certificates.IsUndefined() || !f.Concurrency.IsUndefined() || !f.Energy.IsUndefined() {
		t.Error("a field never written is undefined")
	}
	// Fields không chép (engine chỉ đọc, sau khi đóng băng), nhưng một lần đọc giữ độ dài của nó: chữ viết sau đó không
	// đổi thứ đã đọc
	w.Listen(value.Number(9000))
	if len(f.Listen) != 3 || len(w.Fields().Listen) != 4 {
		t.Errorf("a read keeps its lengths: %d then %d", len(f.Listen), len(w.Fields().Listen))
	}
}

// alone là không có app: Go dựng một cửa một mình, chữ cuối của nó trả undefined về.
func alone() value.Value { return value.Undefined() }

func failure(v value.Value) string {
	if !v.IsInvalid() {
		return "no error"
	}
	return v.Error().Error()
}

func applicationOf(t *testing.T, v value.Value) *application.Application {
	t.Helper()
	a, ok := value.As[application.Application](v)
	if !ok {
		t.Fatalf("not an application: %s", failure(v))
	}
	return a
}

func TestWebsiteFromAnObject(t *testing.T) {
	options := value.Object().
		Set("certificates", value.String(".certificates")).
		Set("guard", value.Array(function("first"), function("second"))).
		Set("ssl", function("check"))
	f := websiteOf(t, website.New(alone(), options)).Fields()
	if len(f.Listen) != 0 || f.Certificates.String() != ".certificates" || len(f.Guards) != 2 || !f.SSL.IsFunction() {
		t.Errorf("object form: %d listen, %d guards", len(f.Listen), len(f.Guards))
	}

	// app.website(…) nhận không gì hay một object; địa chỉ không đứng ở đây (owner 24/11: nó là giá trị của chữ cuối)
	for name, made := range map[string]value.Value{
		"app.website()":                          website.New(alone()),
		"app.website(undefined), as JS calls it": website.New(alone(), value.Undefined()),
		"app.website({ certs })":                 website.New(alone(), value.Object().Set("certs", value.String(".c"))),
	} {
		if made.IsInvalid() {
			t.Errorf("%s: %v", name, made.Error())
		}
	}
	forms := "app.website(…) takes app.website() or app.website({ … }), and the addresses go to the closing word, .listen(443, 80): got "
	keys := "certificates, concurrency, memory, energy, warm, ssl, guard, routes, success, error, finally"
	for _, c := range []struct {
		name string
		made value.Value
		want string
	}{
		{"a key that is no word", website.New(alone(), value.Object().Set("hostname", value.String("shop.vn"))),
			`website has no key "hostname": ` + keys},
		{"a port", website.New(alone(), value.Number(8080)), forms + "app.website(number)"},
		{"an address", website.New(alone(), value.String(":443")), forms + "app.website(string)"},
		{"an address, then an object", website.New(alone(), value.String(":443"), value.Object()), forms + "app.website(string, object)"},
		{"a boolean", website.New(alone(), value.Boolean(true)), forms + "app.website(boolean)"},
		{"listen as a key", website.New(alone(), value.Object().Set("listen", value.Number(443))),
			`website has no key "listen": listen is the closing word, end the statement with .listen(…)`},
		{"port as a key", website.New(alone(), value.Object().Set("port", value.Number(443))),
			`website has no key "port": port is the closing word, end the statement with .port(…)`},
	} {
		if got := failure(c.made); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, got, c.want)
		}
	}
}

// Chữ cuối của mỗi cửa: ghi giá trị chính, đóng câu, trả về app (không trả cửa); không có giá trị thì từ chối và câu
// vẫn chưa đóng. Chỉ application dựng được cửa, nên bài này đi từ app.
func TestTheClosingWordGivesTheApplicationBack(t *testing.T) {
	made := application.New()
	a := applicationOf(t, made)

	web := a.Website(value.Object().Set("certificates", value.String(".certificates")))
	w := websiteOf(t, web)
	if !a.Fields().Website.Identical(web) || w.Closed() {
		t.Error("app.website(…) builds the door, holds it at once, and gives the door back, not closed yet")
	}
	if d := a.Dangling(); len(d) != 1 || d[0].Door != "website" || d[0].Word != "listen" {
		t.Errorf("a website with no closing word dangles: %v", d)
	}
	if got := failure(w.Listen()); got != "website.listen(…) takes the addresses the website listens on: .listen(443, 80)" || w.Closed() {
		t.Errorf("listen() with no address: %s, closed %v", got, w.Closed())
	}
	if got := failure(w.Port(value.Undefined())); got != "website.port(…) takes the addresses the website listens on: .port(443, 80)" {
		t.Errorf("port(undefined): %s", got)
	}
	if back := w.Guard(function("https")); websiteOf(t, back) != w {
		t.Error("a word of the door gives the door back")
	}
	if back := w.Listen(value.Number(443), value.Number(80)); !back.Identical(made) || !w.Closed() {
		t.Error("listen(443, 80) closes the sentence and gives the application back")
	}
	if f := w.Fields(); len(f.Listen) != 2 || f.Listen[0].Number() != 443 || f.Listen[1].Number() != 80 {
		t.Errorf("listen writes the addresses: %d", len(f.Listen))
	}

	db := a.Database(value.Object().Set("port", value.Number(5445)))
	d, _ := value.As[database.Database](db)
	if got := failure(d.Open(value.Undefined())); got != `database.open(…) takes the folder of the databases: .open(".database")` || d.Closed() {
		t.Errorf("open() with no folder: %s", got)
	}
	if back := d.Open(value.String(".database")); !back.Identical(made) || !d.Closed() || d.Fields().Open.String() != ".database" {
		t.Error(`open(".database") closes the sentence and gives the application back`)
	}

	cron := a.Schedule()
	s, _ := value.As[schedule.Schedule](cron)
	if got := failure(s.Run(value.Undefined())); got != `schedule.run(…) takes the folders of the schedules: ".schedules", a list, or a function returning a list` || s.Closed() {
		t.Errorf("run() with no folders: %s", got)
	}
	folders := value.Array(value.String(".schedules"))
	if back := s.Run(folders); !back.Identical(made) || !s.Closed() || !s.Fields().Folders.Identical(folders) {
		t.Error("run(folders) closes the sentence and gives the application back")
	}

	cli := a.Command(value.String("kitcli"))
	c, _ := value.As[command.Command](cli)
	if got := failure(c.Execute()); got != "command.execute(…) takes the commands: .execute({ backup: (context) => … })" || c.Closed() {
		t.Errorf("execute() with nothing: %s", got)
	}
	if got := failure(c.Execute(value.Object())); got != "command.execute({ … }) needs at least one command: .execute({ backup: (context) => … })" || c.Closed() {
		t.Errorf("execute({}): %s", got)
	}
	commands := value.Object().Set("backup", function("backup"))
	if back := c.Execute(commands); !back.Identical(made) || !c.Closed() || c.Fields().Name.String() != "kitcli" ||
		len(c.Fields().Handle) != 1 || !c.Fields().Handle[0].Identical(commands) {
		t.Error("execute({ backup }) closes the sentence and gives the application back")
	}
	if d := a.Dangling(); len(d) != 0 {
		t.Errorf("every door closed: %v", d)
	}

	// mỗi cửa còn mở được nêu, kèm chữ cuối của nó, theo thứ tự website, database, schedule, command
	open := applicationOf(t, application.New())
	open.Command()
	open.Schedule()
	open.Database()
	open.Website()
	var names []string
	for _, d := range open.Dangling() {
		names = append(names, d.Door+"."+d.Word)
	}
	if got := strings.Join(names, " "); got != "website.listen database.open schedule.run command.execute" {
		t.Errorf("the doors left open: %s", got)
	}
}

// Một cửa khai một lần; đối số sai thì không cửa nào được gắn; giá trị chính không đứng ở chỗ mở câu.
func TestADoorIsDeclaredOnce(t *testing.T) {
	a := applicationOf(t, application.New())
	if got := failure(a.Website(value.Number(443))); !strings.HasPrefix(got, "app.website(…) takes app.website() or app.website({ … })") ||
		!a.Fields().Website.IsUndefined() {
		t.Errorf("a refused door is not held: %s", got)
	}
	first := a.Website()
	if got := failure(a.Website()); got != "application already has a website: a door is declared once, in one statement" ||
		!a.Fields().Website.Identical(first) {
		t.Errorf("a second website: %s", got)
	}
	for name, got := range map[string]string{
		"database": failure(a.Database(value.String(".database"))),
		"schedule": failure(a.Schedule(value.String(".schedules"))),
		"command":  failure(a.Command(value.Number(5))),
	} {
		if !strings.HasPrefix(got, "app."+name+"(…) takes ") {
			t.Errorf("app.%s with what belongs to its closing word: %s", name, got)
		}
	}
	f := a.Fields()
	if !f.Schedule.IsUndefined() || !f.Database.IsUndefined() || !f.Command.IsUndefined() || !f.Desktop.IsUndefined() || !f.Mobile.IsUndefined() {
		t.Error("a door never written, or refused, is undefined")
	}
	for _, c := range []struct{ name, got, want string }{
		{"open as a key", failure(a.Database(value.Object().Set("open", value.String(".database")))),
			`database has no key "open": open is the closing word, end the statement with .open(…)`},
		{"run as a key", failure(a.Schedule(value.Object().Set("run", value.String(".schedules")))),
			`schedule has no key "run": run is the closing word, end the statement with .run(…)`},
		{"folders is no word any more", failure(a.Schedule(value.Object().Set("folders", value.String(".schedules")))),
			`schedule has no key "folders": memory, energy, success, error, finally`},
		{"execute as a key", failure(a.Command(value.Object().Set("execute", value.Object()))),
			`command has no key "execute": execute is the closing word, end the statement with .execute({ … })`},
		{"handle is no word any more", failure(a.Command(value.Object().Set("handle", value.Object()))),
			`command has no key "handle": name, success, error, finally`},
		{"a door as a key of application", failure(application.New(value.Object().Set("website", first))),
			`application has no key "website": a door is written in its own statement, like app.website(…).listen(443)`},
		{"a short name as a key of application", failure(application.New(value.Object().Set("db", first))),
			`application has no key "db": a door is written in its own statement, like app.website(…).listen(443)`},
	} {
		if c.got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, c.got, c.want)
		}
	}
}

func TestApplicationKeepsWhatItIsGiven(t *testing.T) {
	made := application.New(value.Object().
		Set("memory", value.String("2gb")).
		Set("timezone", value.String("Asia/Ho_Chi_Minh")))
	a := applicationOf(t, made)
	if a.Running() || a.Checking() {
		t.Error("no verb called yet")
	}
	web := a.Website()
	websiteOf(t, web).Listen(value.Number(443))
	if result := a.Run(); !result.Identical(made) {
		t.Errorf("run gives the Application back: %s", failure(result))
	}
	f := a.Fields()
	if !a.Running() || a.Checking() || f.Memory.String() != "2gb" || f.Timezone.String() != "Asia/Ho_Chi_Minh" || !f.Website.Identical(web) {
		t.Error("the fields as given, and run")
	}

}

// Chương trình không có cổng của riêng nó (owner 24/11, "Bỏ đi"): application(8080), application(":443", { … }) và
// khoá listen, port của 23/11 là lỗi nói địa chỉ là của cửa nghe nó; Application không còn chữ listen, port.
func TestTheApplicationHasNoPortOfItsOwn(t *testing.T) {
	forms := "application(…) takes application() or application({ … }), and an address is written by the door that listens on it, " +
		"like app.website(…).listen(443): got "
	key := `: an address is written by the door that listens on it, like app.website(…).listen(443)`
	for _, c := range []struct{ name, got, want string }{
		{"a port", failure(application.New(value.Number(8080))), forms + "application(number)"},
		{"an address", failure(application.New(value.String(":443"))), forms + "application(string)"},
		{"an address, then an object", failure(application.New(value.String(":443"), value.Object())), forms + "application(string, object)"},
		{"listen as a key", failure(application.New(value.Object().Set("listen", value.Number(8080)))), `application has no key "listen"` + key},
		{"port as a key", failure(application.New(value.Object().Set("port", value.Number(8080)))), `application has no key "port"` + key},
	} {
		if c.got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, c.got, c.want)
		}
	}
	a := applicationOf(t, application.New(value.Object().Set("memory", value.String("1gb"))))
	for _, word := range []string{"Listen", "Port"} {
		if _, found := reflect.TypeOf(a).MethodByName(word); found {
			t.Errorf("Application has the word %s: the address of a door is the door's", word)
		}
	}
	if _, found := reflect.TypeOf(a.Fields()).FieldByName("Listen"); found {
		t.Error("Application keeps addresses of its own")
	}
}

// app.run() và app.check(): không đối số, một trong hai, và là dòng cuối: sau nó không chữ nào ghi được nữa.
func TestRunAndCheck(t *testing.T) {
	checked := applicationOf(t, application.New())
	checked.Check()
	if !checked.Checking() || checked.Running() {
		t.Error("check sets its own mark")
	}
	ran := applicationOf(t, application.New())
	ran.Run(value.Undefined()) // JS gọi app.run() có thể đưa undefined
	if !ran.Running() || ran.Checking() {
		t.Error("run sets its own mark")
	}

	wrong := applicationOf(t, application.New())
	for _, c := range []struct{ name, got, want string }{
		{"doors given to run, the old way", failure(wrong.Run(value.Object().Set("web", value.Number(1)))),
			"app.run() takes nothing: a door is written in its own statement before it, like app.website(…).listen(443), got app.run(object)"},
		{"the old flag", failure(wrong.Check(value.Boolean(true))),
			"app.check() takes nothing: a door is written in its own statement before it, like app.website(…).listen(443), got app.check(boolean)"},
		{"application({ run: … })", failure(application.New(value.Object().Set("run", value.Boolean(true)))),
			`application has no key "run": run is a verb, end with return app.run()`},
		{"a key that is no word", failure(application.New(value.Object().Set("x", value.Number(5)))),
			`application has no key "x": memory, timezone, desktop, mobile`},
		{"a boolean", failure(application.New(value.Boolean(true))),
			"application(…) takes application() or application({ … }), and an address is written by the door that listens on it, " +
				"like app.website(…).listen(443): got application(boolean)"},
	} {
		if c.got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, c.got, c.want)
		}
	}
	if wrong.Running() || wrong.Checking() {
		t.Error("a refused verb sets no mark")
	}

	last := "app.run() is the last line of application.work: write this before it"
	for name, got := range map[string]string{
		"a door after run":  failure(ran.Website()),
		"a value after run": failure(ran.Memory(value.String("1gb"))),
		"a zone after run":  failure(ran.Timezone(value.String("UTC"))),
		"check after run":   failure(ran.Check()),
		"run twice":         failure(ran.Run()),
	} {
		if got != last {
			t.Errorf("%s: %s", name, got)
		}
	}
	if f := ran.Fields(); !f.Website.IsUndefined() || !f.Memory.IsUndefined() || !f.Timezone.IsUndefined() || ran.Checking() {
		t.Error("a word after run writes nothing")
	}
	if got := failure(checked.Run()); got != "app.check() is the last line of application.work: write this before it" || checked.Running() {
		t.Errorf("run after check: %s", got)
	}
}

// Database thụ động như Website: giữ đúng giá trị, kể cả sai (engine kiểm khi nạp). Thứ nó mở là giá trị của chữ cuối
// open; cổng viết bằng khoá listen (owner 23/11), port là chữ thứ hai của listen.
func TestDatabaseKeepsWhatItIsGiven(t *testing.T) {
	made := database.New(alone(), value.Object().
		Set("listen", value.String("127.0.0.1:5445")).
		Set("password", value.String("secret")).
		Set("port", value.Array(value.Number(5446))).
		Set("tls", value.Boolean(true)).
		Set("binding", value.String("yes")). // sai, nhưng blueprint không kiểm
		Set("memory", value.String("256mb")).
		Set("timeout", value.String("10s")))
	d, ok := value.As[database.Database](made)
	if !ok {
		t.Fatalf(`app.database({ … }): %v`, made.Error())
	}
	if returned := d.Listen(value.Number(70000)); !returned.Identical(made) {
		t.Error("a word gives the Database back, for JS to chain")
	}
	d.Port(value.String("localhost:5447"))
	// một chuỗi là thứ mở, kể cả khi trông như địa chỉ: engine báo khi nạp
	d.Open(value.String("127.0.0.1:5445"))
	f := d.Fields()
	if f.Open.String() != "127.0.0.1:5445" {
		t.Errorf("open writes what to open: %v", f.Open.Typeof())
	}
	if len(f.Listen) != 4 || f.Listen[0].String() != "127.0.0.1:5445" || f.Listen[1].Number() != 5446 ||
		f.Listen[2].Number() != 70000 || f.Listen[3].String() != "localhost:5447" {
		t.Errorf("listen and port, in the order written: %d", len(f.Listen))
	}
	if f.Password.String() != "secret" || !f.TLS.Boolean() || f.Binding.String() != "yes" ||
		f.Memory.String() != "256mb" || f.Timeout.String() != "10s" ||
		!f.User.IsUndefined() || !f.Authority.IsUndefined() {
		t.Error("the fields as given, the others undefined")
	}

	forms := `app.database(…) takes app.database() or app.database({ … }), and the folder goes to the closing word, .open(".database"): got `
	keys := "listen, concurrency, memory, timeout, user, password, tls, binding, authority"
	for _, c := range []struct {
		name string
		made value.Value
		want string
	}{
		{"a key that is no word", database.New(alone(), value.Object().Set("folder", value.String(".database"))),
			`database has no key "folder": ` + keys},
		// không có công tắc search (owner 23/11): MATCH mặc định, chỉ mục khai trong database
		{"search is no key", database.New(alone(), value.Object().Set("search", value.Boolean(true))),
			`database has no key "search": ` + keys},
		{"what to open, where the door opens", database.New(alone(), value.String(".database")), forms + "app.database(string)"},
		{"a port", database.New(alone(), value.Number(5445)), forms + "app.database(number)"},
		{"what to open, then an object", database.New(alone(), value.String(".database"), value.Object()), forms + "app.database(string, object)"},
	} {
		if got := failure(c.made); got != c.want {
			t.Errorf("%s:\ngot  %s\nwant %s", c.name, got, c.want)
		}
	}
}

// Sau khi nạp, engine đóng băng cả cây từ Application (value.FreezeShared): vào các cửa qua Share, vào các object và
// mảng chương trình đưa vào; Share lần sau trả nil.
func TestFreezeSharedReachesThroughTheBlueprint(t *testing.T) {
	limits := value.Object().Set("default", value.String("64mb"))
	// một mảng đưa cho trường một giá trị thì được giữ nguyên, và đóng băng theo
	folders := value.Array(value.String("apps/x"))
	made := application.New(value.Object().Set("memory", value.String("2gb")))
	a := applicationOf(t, made)
	web := a.Website(value.Object().Set("memory", limits).Set("routes", folders))
	// một guard bắt được một biến cấp file (const seen = {}): trường danh sách, qua closure (một value.Sharer)
	seen := value.Object()
	websiteOf(t, web).Guard(value.Function(&capturing{held: []value.Value{seen}}))
	websiteOf(t, web).Listen(value.Number(443))
	// database: một object đưa cho trường một giá trị được giữ nguyên, và đóng băng theo
	secret := value.Object().Set("from", value.String("KITDB_PASSWORD"))
	db := a.Database(value.Object().Set("listen", value.Number(5445)).Set("password", secret))
	d, _ := value.As[database.Database](db)
	d.Open(value.String(".database"))
	// schedule: mảng các thư mục của chữ cuối run cũng được giữ và đóng băng
	lists := value.Array(value.String(".schedules"))
	s, _ := value.As[schedule.Schedule](a.Schedule())
	s.Run(lists)
	a.Run()

	made.FreezeShared()

	w := websiteOf(t, web)
	if !a.Shared() || !w.Shared() || !d.Shared() || !s.Shared() || !secret.IsShared() {
		t.Error("Application, Website, Database and Schedule are shared")
	}
	if got := d.Password(value.String("changed")); !got.IsInvalid() || got.Error().Error() !=
		"database was loaded by the engine and is read only now: write it in application.work, not while serving" {
		t.Errorf("a word on a loaded database: %v", got.Error())
	}
	if !limits.IsShared() || !folders.IsShared() || !seen.IsShared() || !lists.IsShared() {
		t.Error("what the program gave a field is frozen too")
	}
	if a.Share() != nil || w.Share() != nil {
		t.Error("Share once")
	}
	if got := limits.Set("default", value.String("1gb")); !got.IsInvalid() {
		t.Error("a write to what the blueprint holds is refused")
	}
}
