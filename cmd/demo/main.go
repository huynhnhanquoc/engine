// Demo của mô hình blueprint thụ động, chạy thật (23/11/2026): mỗi cảnh là một application.work, VM của engine3
// (core/syntax, core/compiler, core/runtime qua host.Boot) biên dịch và chạy nó, gọi đúng các chữ của bản thiết kế. Các
// cảnh viết theo cách viết của owner 24/11/2026 (docs/KITWORK_APPLICATION_WORK.md): const app = application(…), mỗi cửa
// một câu app.<cửa>(cấu hình).<hàm>….<chữ cuối>(giá trị chính), return app.run().
//
//   - Vai VM: host.Boot. Một lỗi biên dịch, một câu sai hình (cảnh 3, 9, 12, 13) hay một lời gọi sai (cảnh 10, 14) dừng
//     chương trình ở dòng và cột của nó.
//   - Vai engine: nạp bản thiết kế (load): quét từng trường, kiểm, gom mọi lỗi theo đường dẫn trường, đóng băng cả cây
//     (value.FreezeShared, qua các closure tới biến chúng bắt), đọc run, in ra thứ nó sẽ mở. Bộ nạp ở đây vẫn là phác
//     thảo cho bộ nạp thật (chưa làm).
//
// Chạy: go run ./cmd/demo
package main

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// hostFolder là thư mục của các application.work của demo: một thư mục tạm có .env, như thư mục của một host.
var hostFolder string

func main() {
	dir, err := os.MkdirTemp("", "kitwork-demo-")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	hostFolder = dir
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KITDB_PASSWORD=s3cret-from-env\nPORT=8443\n"), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("thư mục của host có .env: KITDB_PASSWORD=…, PORT=8443")
	fmt.Println()

	scene("1. application.work đúng: mỗi cửa một câu, chữ cuối mang giá trị chính", goodSource, afterGood)
	scene("2. application.work có giá trị sai: engine gom mọi lỗi, không mở gì", brokenSource, nil)
	scene("3. một câu quên chữ cuối: boot dừng ở chính câu ấy, nêu chữ còn thiếu", unclosedSource, nil)
	scene("4. app.check(): chỉ nạp và kiểm, không mở gì", checkedSource, nil)
	scene("5. một câu nối liền: mỗi chữ cuối trả về app, nên các cửa nối nhau được", chainedSource, nil)
	scene("6. website và database chung một cổng: mỗi cửa giữ địa chỉ của nó, engine lọc lớn chứa nhỏ", twoDoorsSource, nil)
	scene("7. database có giá trị sai: engine báo theo đường dẫn trường", brokenDatabaseSource, nil)
	scene("8. database không cổng: chỉ mở, không cần password", quietDatabaseSource, nil)
	scene("9. một cửa khai trong một hàm: app chỉ đứng ở cấp file", hiddenDoorSource, nil)
	scene("10. viết sai một chữ: VM nói chữ nào có", misspelledWordSource, nil)
	scene("11. env.require thiếu khoá: boot kiểm mọi khoá bắt buộc một lần, kể cả trong hàm chỉ chạy lúc request", requiredSource, nil)
	scene("12. một cửa khai hai lần: lần sau không lặng lẽ đè lần trước", twiceSource, nil)
	scene("13. dựng cửa rời, cách viết trước 24/11: thư viện chỉ còn application và environment", looseDoorSource, nil)
	scene("14. cổng ở mức chương trình, cách viết 23/11: địa chỉ là của cửa nghe nó", programPortSource, nil)
}

func scene(title, source string, afterLoad func(made value.Value)) {
	fmt.Println(strings.Repeat("═", 100))
	fmt.Println(title)
	fmt.Println(strings.Repeat("═", 100))
	fmt.Println(strings.TrimSpace(source))
	fmt.Println(strings.Repeat("─", 100))
	booted, err := host.Program{File: "application.work", Source: strings.TrimLeft(source, "\n"), Folder: hostFolder}.Boot()
	if err != nil {
		fmt.Println("VM: chương trình dừng ở lỗi:", err)
		fmt.Println()
		return
	}
	made := booted.Blueprint
	fmt.Println("VM: chương trình chạy xong, trả về bản thiết kế Application")
	for _, note := range booted.Notes {
		fmt.Println("boot ghi chú:", note)
	}
	if a, ok := value.As[application.Application](made); ok && a.Checking() {
		fmt.Println("app.check() → bản thiết kế đã dựng, mỗi cửa một dòng kèm chỗ khai (Booted.Describe):")
		for _, line := range strings.Split(strings.TrimRight(booted.Describe(), "\n"), "\n") {
			fmt.Println("  ", line)
		}
	}
	plan, errors := load(made)
	if len(errors) > 0 {
		fmt.Printf("engine: %d lỗi trong bản thiết kế, không mở gì:\n", len(errors))
		for _, e := range errors {
			fmt.Println("   ✗", e)
		}
		fmt.Println()
		return
	}
	made.FreezeShared()
	fmt.Println("engine: bản thiết kế hợp lệ, đã đóng băng cả cây (FreezeShared)")
	for _, line := range plan {
		fmt.Println("  ", line)
	}
	if afterLoad != nil {
		afterLoad(made)
	}
	fmt.Println()
}

// ---- Cảnh 1 ----

const goodSource = `
import { application } from "application";

const seen = {};   // biến cấp file, guard thứ ba ghi vào nó
const https = (context) => context.request.secure ? context.next() : context.redirect("https://" + context.request.hostname);
const notBanned = (context) => context.next();
const confirmed = (hostname) => hostname.endsWith(".vn");

const app = application({ memory: "2gb", timezone: "Asia/Ho_Chi_Minh" });

app.website({ certificates: ".certificates", concurrency: 16 })
  .warm((context) => context.site.hostname === "shop.vn")
  .ssl((context) => confirmed(context.request.hostname))
  .guard(https)
  .guard(notBanned)
  .guard((context) => { seen[context.request.ip] = true; return context.next(); })
  .routes((context) => ` + "`apps/${context.request.tenant.identity}/${context.request.hostname}`" + `)
  .memory((context) => context.request.tenant.memory ?? "64mb")
  .energy(10000000)
  .error((context) => console.log(context.response.error.message))
  .finally((context) => console.log(context.request.path, context.response.status))
  .listen(443, 80);   // chữ cuối: ghi địa chỉ, đóng câu, trả về app; chưa nghe gì

return app.run();
`

// afterGood chạy guard thứ ba bằng chính VM, như một request sẽ chạy nó, sau khi bản thiết kế đã đóng băng.
func afterGood(made value.Value) {
	a, _ := value.As[application.Application](made)
	w, _ := value.As[website.Website](a.Fields().Website)
	next := javascript.NewNative("next", 0, func(javascript.Run, []value.Value) value.Value { return value.String("next") })
	context := value.Object().Set("request", value.Object().Set("ip", value.String("203.0.113.7"))).Set("next", next)
	fmt.Println("một request chạy guard thứ ba (VM chạy thật), nó ghi vào seen, biến cấp file mọi request cùng đọc:")
	fmt.Println("   →", runtime.New(runtime.Options{}).Call(w.Fields().Guards[2], context).Error())
	fmt.Println("một chữ ghi lên website sau khi engine đã nạp:")
	fmt.Println("   →", w.Guard(value.Undefined()).Error())
}

// ---- Cảnh 2 ----

const brokenSource = `
import { application } from "application";

const https = (context) => context.next();

const app = application({ timezone: "Asia/Saigonn" });

app.website()
  .concurrency(-2)
  .memory("64 mb")                                 // có dấu cách
  .ssl((context) => true)                          // ssl mà không có certificates
  .guard(https)
  .guard(5)                                        // không phải hàm
                                                   // quên routes
  .listen(443, 70000, ":443");                     // 70000 không phải cổng; ":443" trùng 443: engine lọc, không lỗi

return app.run();
`

// ---- Cảnh 3 ----

const unclosedSource = `
import { application } from "application";

const app = application({ memory: "2gb" });

app.website().routes("apps/shop");   // quên chữ cuối: .listen(443)

return app.run();
`

// ---- Cảnh 4 ----

const checkedSource = `
import { application } from "application";

const app = application({ memory: "512mb" });

app.website().routes("apps/shop").listen(443);

return app.check();   // nhánh đang viết dở: chỉ nạp và kiểm
`

// ---- Cảnh 5 ----

const chainedSource = `
import { application } from "application";

return application({ timezone: "Asia/Ho_Chi_Minh" })
  .website().routes("apps/shop").listen(443)   // listen trả về app…
  .database().open(".database")                // …nên cửa kế tiếp nối được
  .run();
`

// ---- Cảnh 6 ----

const twoDoorsSource = `
import { application, environment } from "application";

const env = environment("./.env");   // .env cạnh application.work, biến của hệ điều hành thắng

const app = application({ memory: "2gb", timezone: env.TIMEZON ?? "UTC" });   // gõ sai TIMEZONE: boot ghi chú, đọc ra null

app.website()
  .routes("apps/shop")
  .listen(443, "0.0.0.0:8080", 8080);   // "0.0.0.0:8080" như ":8080" (một socket cho cả IPv4 và IPv6); 8080 trùng: bỏ

app.database({ listen: "localhost:443", password: env.KITDB_PASSWORD })   // chung 443, chỉ máy này
  .memory("256mb")           // mỗi câu SQL qua cổng: kết quả cộng file tạm
  .timeout("10s")
  .open(".database");

return app.run();
`

// ---- Cảnh 7 ----

const brokenDatabaseSource = `
import { application } from "application";

const app = application();

app.database({ port: 5445 })            // có cổng thì cần password
  .concurrency("many")                  // không phải số
  .timeout("forever")                   // không phải thời gian
  .binding(true)                        // bắt buộc SCRAM-PLUS: cần tls
                                        // quên password
  .open("127.0.0.1:5445");              // thứ được mở là một thư mục, không phải cổng

return app.run();
`

// ---- Cảnh 8 ----

const quietDatabaseSource = `
import { application } from "application";

const app = application();

app.website().routes("apps/shop").listen(443);
app.database().open(".database");   // chỉ mở, không cổng: app của host dùng, không công cụ ngoài nào vào được

return app.run();
`

// ---- Cảnh 9 ----

const hiddenDoorSource = `
import { application } from "application";

const app = application();

const admin = () => app.website().routes("apps/admin").listen(8080);   // một cửa mà người đọc file không thấy ở cấp file
admin();

return app.run();
`

// ---- Cảnh 10 ----

const misspelledWordSource = `
import { application } from "application";

const app = application();

app.website().lisen(80).routes("apps/shop").listen(443);   // lisen: không có chữ ấy

return app.run();
`

// ---- Vai engine: nạp (phác thảo) ----

type loader struct {
	plan   []string
	errors []string
}

func (l *loader) fail(path, format string, arguments ...any) {
	l.errors = append(l.errors, path+": "+fmt.Sprintf(format, arguments...))
}

func load(made value.Value) ([]string, []string) {
	l := &loader{}
	a, ok := value.As[application.Application](made)
	if !ok {
		l.fail("application.work", "must return the application, got %s", made.Typeof())
		return nil, l.errors
	}
	f := a.Fields()

	if !f.Memory.IsUndefined() {
		if n, err := f.Memory.ByteSize(); err != nil {
			l.fail("application.memory", "%v", err)
		} else {
			l.plan = append(l.plan, fmt.Sprintf("trần bộ nhớ chung của mọi lượt chạy: %d MiB", n>>20))
		}
	}
	zone := "UTC"
	if !f.Timezone.IsUndefined() {
		if err := f.Timezone.Scan(&zone); err != nil {
			l.fail("application.timezone", "%v", err)
		} else if _, err := time.LoadLocation(zone); err != nil {
			l.fail("application.timezone", "%q is no time zone: write one like \"Asia/Ho_Chi_Minh\"", zone)
		}
	}
	l.plan = append(l.plan, "múi giờ của lịch và của Date: "+zone)

	// listen: mỗi cửa viết địa chỉ của nó (owner 24/11: chương trình không có cổng riêng), engine đọc và lọc ở đây. Trong
	// một cửa, địa chỉ trùng hay bị địa chỉ lớn hơn chứa thì bỏ (không mất gì: cửa đã có địa chỉ lớn), in ra, không lỗi;
	// giữa các cửa chỉ lọc socket (sockets), mỗi cửa giữ địa chỉ của nó.
	var doors []host.Door
	if !f.Website.IsUndefined() {
		w, ok := value.As[website.Website](f.Website)
		if !ok {
			l.fail("application.website", "is written app.website(…).listen(…), got %s", f.Website.Typeof())
		} else {
			doors = append(doors, host.Door{Name: "website", Addresses: l.website(w.Fields())})
		}
	}
	if !f.Database.IsUndefined() {
		d, ok := value.As[database.Database](f.Database)
		if !ok {
			l.fail("application.database", "is written app.database(…).open(…), got %s", f.Database.Typeof())
		} else {
			doors = append(doors, host.Door{Name: "database", Addresses: l.database(d.Fields())})
		}
	}
	l.sockets(host.Sockets(doors))
	// boot đã từ chối một file không xin run hay check (host.Boot): ở đây chỉ còn hai trường hợp
	if a.Running() {
		l.plan = append([]string{"app.run() → mở cửa:"}, l.plan...)
	} else {
		l.plan = append([]string{"app.check() → chỉ nạp và kiểm, không mở gì; nếu chạy thì sẽ mở:"}, l.plan...)
	}
	return l.plan, l.errors
}

func (l *loader) failures(errors []error) {
	for _, err := range errors {
		l.errors = append(l.errors, err.Error())
	}
}

// said là lời engine kể mỗi địa chỉ nó bỏ khi lọc (host.Filter): không lỗi, nhưng không ẩn.
func said(dropped []host.Dropped) []string {
	lines := make([]string, len(dropped))
	for i, d := range dropped {
		lines[i] = "  " + d.String()
	}
	return lines
}

// sockets in các socket engine mở (host.Sockets): mỗi cửa giữ địa chỉ của nó, và bộ chia đưa cho một cửa chỉ kết nối
// đi vào địa chỉ của nó (host.Use.Accepts).
func (l *loader) sockets(sockets []host.Socket) {
	if len(sockets) == 0 {
		return
	}
	l.plan = append(l.plan, "socket engine mở:")
	for _, s := range sockets {
		var users []string
		for _, use := range s.Doors {
			if len(use.Only) == 0 {
				users = append(users, use.Door)
				continue
			}
			only := make([]string, len(use.Only))
			for i, a := range use.Only {
				only[i] = a.String()
			}
			users = append(users, use.Door+" chỉ khi kết nối vào "+strings.Join(only, ", "))
		}
		line := "  " + s.Address + ": " + strings.Join(users, "; ")
		if len(users) > 1 {
			line += " (engine đọc byte đầu mỗi kết nối để chia)"
		}
		l.plan = append(l.plan, line)
	}
}

// website nạp cửa website và trả các địa chỉ của nó, đã lọc.
func (l *loader) website(f website.Fields) []string {
	const at = "application.website"
	if len(f.Listen) == 0 {
		l.fail(at+".listen", "a website needs at least one address: write .listen(443)")
	}
	own, problems := host.Addresses(at+".listen", f.Listen)
	l.failures(problems)
	addresses, dropped := host.Filter(own)
	certificates := ""
	if !f.Certificates.IsUndefined() {
		if err := f.Certificates.Scan(&certificates); err != nil {
			l.fail(at+".certificates", "%v", err)
		}
	}
	concurrency := 0
	if !f.Concurrency.IsUndefined() {
		if err := f.Concurrency.Scan(&concurrency); err != nil || concurrency < 0 {
			l.fail(at+".concurrency", "takes an integer from 0 (0 lets the engine choose), got %s", show(f.Concurrency))
		}
	}
	l.sizeOrFunction(at+".memory", f.Memory)
	if !f.Energy.IsUndefined() && !f.Energy.IsFunction() {
		var n int64
		if err := f.Energy.Scan(&n); err != nil || n < 1 {
			l.fail(at+".energy", "takes a positive integer or a function, got %s", show(f.Energy))
		}
	}
	if !f.Warm.IsUndefined() && !f.Warm.IsBoolean() && !f.Warm.IsFunction() {
		l.fail(at+".warm", "takes true, false or a function, got %s", f.Warm.Typeof())
	}
	switch {
	case f.SSL.IsUndefined():
	case !f.SSL.IsFunction():
		l.fail(at+".ssl", "takes a function, got %s", f.SSL.Typeof())
	case certificates == "":
		l.fail(at+".ssl", "decides TLS handshakes, but certificates is not set, so no port takes TLS")
	}
	l.functions(at+".guard", f.Guards)
	switch {
	case f.Routes.IsUndefined():
		l.fail(at+".routes", "a website needs routes: a folder like \"apps/shop\" or a function of the request")
	case !f.Routes.IsString() && !f.Routes.IsFunction():
		l.fail(at+".routes", "takes a folder string or a function, got %s", f.Routes.Typeof())
	}
	l.functions(at+".success", f.Success)
	l.functions(at+".error", f.Errors)
	l.functions(at+".finally", f.Finally)

	tls := "không TLS"
	if certificates != "" {
		tls = "TLS với chứng chỉ ở " + certificates + ", mỗi cổng nhận cả HTTP thường"
	}
	l.plan = append(l.plan, fmt.Sprintf("website: lắng nghe %s (%s)", strings.Join(addresses, ", "), tls))
	l.plan = append(l.plan, said(dropped)...)
	l.plan = append(l.plan,
		fmt.Sprintf("  tối đa %s request cùng lúc mỗi site", map[bool]string{true: "gấp đôi số nhân CPU", false: fmt.Sprint(concurrency)}[concurrency == 0]),
		"  khi bắt tay TLS: gọi ssl "+source(f.SSL),
		"  lúc boot mỗi site: warm "+source(f.Warm),
	)
	for i, guard := range f.Guards {
		l.plan = append(l.plan, fmt.Sprintf("  mỗi request, guard %d: gọi %s", i+1, source(guard)))
	}
	l.plan = append(l.plan,
		"  rồi routes: gọi "+source(f.Routes),
		"  trần của lượt chạy: memory "+source(f.Memory)+", energy "+source(f.Energy),
		fmt.Sprintf("  sau khi gửi: %d success, %d error, %d finally", len(f.Success), len(f.Errors), len(f.Finally)),
	)
	return addresses
}

// database nạp cửa database và trả các địa chỉ của nó, đã lọc. Thứ quan trọng là open, bắt buộc (owner 23/11): thiếu là
// lỗi, không mặc định. Cổng không bắt buộc: database không nêu cổng thì không mở cổng PostgreSQL; mật khẩu chỉ cần khi
// có cổng. Mật khẩu không bao giờ in ra.
func (l *loader) database(f database.Fields) []string {
	const at = "application.database"
	own, problems := host.Addresses(at+".listen", f.Listen)
	l.failures(problems)
	addresses, dropped := host.Filter(own)
	open := ""
	switch err := f.Open.Scan(&open); {
	case f.Open.IsUndefined():
		l.fail(at+".open", `a database needs what to open: end with .open(".database")`)
	case err != nil:
		l.fail(at+".open", "%v", err)
	case open == "":
		l.fail(at+".open", `takes a folder that is not empty: end with .open(".database")`)
	case looksLikeAnAddress(open):
		l.fail(at+".open", `%q looks like an address, but .open(…) takes what to open, a folder: write the port as app.database({ listen: %q })`, open, open)
	}
	concurrency := 0
	if !f.Concurrency.IsUndefined() {
		if err := f.Concurrency.Scan(&concurrency); err != nil || concurrency < 0 {
			l.fail(at+".concurrency", "takes an integer from 0 (0 lets the port choose), got %s", show(f.Concurrency))
		}
	}
	memory := "mặc định của KitDB"
	if !f.Memory.IsUndefined() {
		if _, err := f.Memory.ByteSize(); err != nil {
			l.fail(at+".memory", "%v", err)
		} else {
			memory = f.Memory.String()
		}
	}
	timeout := 30 * time.Second
	if !f.Timeout.IsUndefined() {
		var err error
		if timeout, err = f.Timeout.TimeSpan(); err != nil {
			l.fail(at+".timeout", "%v", err)
		}
	}
	user := "kitdb"
	if !f.User.IsUndefined() {
		if err := f.User.Scan(&user); err != nil || user == "" {
			l.fail(at+".user", "takes a name, got %s", show(f.User))
		}
	}
	switch {
	case f.Password.IsUndefined() && len(addresses) > 0:
		l.fail(at+".password", "a database with a port needs a password: write .password(env.KITDB_PASSWORD)")
	case f.Password.IsUndefined():
	case !f.Password.IsString() || f.Password.String() == "":
		l.fail(at+".password", "takes a string that is not empty, got %s", f.Password.Typeof()) // không in giá trị
	}
	tls := l.flag(at+".tls", f.TLS)
	binding := l.flag(at+".binding", f.Binding)
	if binding && !tls {
		l.fail(at+".binding", "binds SCRAM to TLS, but tls is not on: write .tls(true)")
	}
	authority := ""
	if !f.Authority.IsUndefined() {
		if err := f.Authority.Scan(&authority); err != nil {
			l.fail(at+".authority", "%v", err)
		} else if !tls {
			l.fail(at+".authority", "asks clients for a certificate over TLS, but tls is not on: write .tls(true)")
		}
	}

	// một địa chỉ ngoài máy này: mọi giao diện, hay một IP không phải loopback
	outside := slices.ContainsFunc(addresses, func(address string) bool {
		h, _, _ := net.SplitHostPort(address)
		ip, err := netip.ParseAddr(h)
		return err != nil || !ip.IsLoopback()
	})
	if len(addresses) == 0 {
		l.plan = append(l.plan, "database: mở "+open+", không mở cổng PostgreSQL (không có listen): chỉ app của host dùng")

		if !f.Memory.IsUndefined() || !f.Timeout.IsUndefined() {
			l.plan = append(l.plan, "  memory, timeout không dùng: chúng là trần của một câu qua cổng, mà database không mở cổng")
		}
		return nil
	}
	l.plan = append(l.plan, fmt.Sprintf("database: mở %s, cổng PostgreSQL %s, tài khoản %s, mật khẩu: có (không in)", open, strings.Join(addresses, ", "), user))
	l.plan = append(l.plan, said(dropped)...)
	l.plan = append(l.plan, fmt.Sprintf("  mỗi câu SQL qua cổng: memory %s, timeout %s", memory, timeout))
	switch {
	case tls:
		l.plan = append(l.plan, fmt.Sprintf("  TLS với chứng chỉ của website (kho AutoSSL); bắt buộc SCRAM-PLUS với client máy khác: %v; chứng chỉ client: %s", binding, map[bool]string{true: authority, false: "không đòi"}[authority != ""]))
	case outside:
		l.plan = append(l.plan, "  không TLS: kết nối từ ngoài máy bị từ chối, chỉ máy này vào được")
	}
	l.plan = append(l.plan, "  MATCH qua cổng: mặc định, trên mọi bảng có chỉ mục toàn văn (khai trong database)")
	return addresses
}

// looksLikeAnAddress là s viết như một cổng ("5445") hay một địa chỉ ("127.0.0.1:5445", ":5445"): .open(s) mở s, nên
// đây gần như chắc là người viết muốn một cổng.
func looksLikeAnAddress(s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	_, err = strconv.Atoi(port)
	return err == nil
}

// flag đọc một trường true/false; vắng là false.
func (l *loader) flag(path string, v value.Value) bool {
	switch {
	case v.IsUndefined():
		return false
	case v.IsBoolean():
		return v.Boolean()
	}
	l.fail(path, "takes true or false, got %s", show(v))
	return false
}

func (l *loader) sizeOrFunction(path string, v value.Value) {
	switch {
	case v.IsUndefined(), v.IsFunction():
	case v.IsString():
		if _, err := v.ByteSize(); err != nil {
			l.fail(path, "%v", err)
		}
	default:
		l.fail(path, `takes a size like "64mb" or a function, got %s`, v.Typeof())
	}
}

func (l *loader) functions(path string, list []value.Value) {
	for i, item := range list {
		if !item.IsFunction() {
			l.fail(fmt.Sprintf("%s[%d]", path, i), "takes a function, got %s", show(item))
		}
	}
}

// source là chữ in ra cho một trường: nguồn của closure, hay giá trị.
func source(v value.Value) string {
	if c, ok := v.Function().(*runtime.Closure); ok {
		if c.Name() != "" {
			return "hàm " + c.Name()
		}
		return "hàm mũi tên ở " + c.At()
	}
	if v.IsUndefined() {
		return "(không có)"
	}
	return show(v)
}

func show(v value.Value) string {
	switch {
	case v.IsNumber():
		return strconv.FormatFloat(v.Number(), 'f', -1, 64)
	case v.IsString():
		return fmt.Sprintf("%q", v.String())
	case v.IsBoolean():
		return fmt.Sprint(v.Boolean())
	}
	return v.Typeof()
}

// ---- Cảnh 11 ----

const requiredSource = `
import { application, environment } from "application";

const env = environment("./.env");

const app = application({ timezone: env.require("TIMEZONE") });   // .env không có STRIPE_SECRET, TIMEZONE

app.website()
  .routes("apps/shop")
  .guard((context) => context.request.header("Stripe-Signature") === env.require("STRIPE_SECRET") ? context.next() : null)
  .listen(env.PORT);   // 8443 từ .env

return app.run();
`

// ---- Cảnh 12 ----

const twiceSource = `
import { application } from "application";

const app = application();

app.website().routes("apps/shop").listen(443);
app.website().routes("apps/admin").listen(8080);   // cửa thứ hai cùng tên

return app.run();
`

// ---- Cảnh 13 ----

const looseDoorSource = `
import { website, application } from "application";

const web = website(443).routes("apps/shop");

return application().run({ web });
`

// ---- Cảnh 14 ----

const programPortSource = `
import { application } from "application";

const app = application(8080);   // cổng của chương trình: website nghe ở đâu thì đọc câu của website

app.website().routes("apps/shop").listen(443);

return app.run();
`
