package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// Booted.Describe (owner 24/11/2026, "Làm đi"): bản thiết kế đã dựng, viết ra để đọc cả chương trình ở một chỗ. Dòng
// đầu là application, rồi mỗi cửa một dòng; ba cột: tên, chỗ khai (file:dòng:cột của câu), và các giá trị đã ghi theo
// thứ tự các trường, giá trị chính của cửa đứng đầu. Owner duyệt hình dạng dưới đây 24/11 ("Được, giữ vậy").

func describe(t *testing.T, folder, source string) string {
	t.Helper()
	booted, err := host.Program{File: "application.work", Source: source, Folder: folder,
		System: func(string) (string, bool) { return "", false }}.Boot()
	if err != nil {
		t.Fatal(err)
	}
	return booted.Describe()
}

func sameLines(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

// Bản thiết kế của mốc nghiệm thu: bốn cửa, năm guard, mật khẩu đọc từ .env.
func TestDescribeTheBlueprintOfDocsHost(t *testing.T) {
	folder := filepath.Join("testdata", "blueprint")
	source, err := os.ReadFile(filepath.Join(folder, "application.work"))
	if err != nil {
		t.Fatal(err)
	}
	got := describe(t, folder, string(source))
	want := strings.Join([]string{
		`application  application.work:15:13  memory "2gb" · timezone "Asia/Ho_Chi_Minh"`,
		`  website    application.work:18:1   listen 443, 80 · certificates ".certificates" · concurrency 16 · memory application.work:79:11 · ` +
			`energy application.work:80:11 · warm application.work:20:9 · ssl application.work:23:8 · 5 guard · routes application.work:75:11 · ` +
			`1 error · 1 finally`,
		`  database   application.work:108:1  open ".database" · listen 5445 · password ••••`,
		`  schedule   application.work:96:1   run application.work:102:8 · memory application.work:97:11 · 1 error`,
		`  command    application.work:111:1  name "kitcli" · execute backup, migrate, tenants · 1 error`,
		"",
	}, "\n")
	sameLines(t, "the blueprint", got, want)
	if strings.Contains(got, "secret") {
		t.Error("the password of the database is printed")
	}
}

func TestDescribe(t *testing.T) {
	head := `import { application } from "application";` + "\n"
	folder := tree(t, map[string]string{
		"host/web.work": "export const https = (context) => context.next();\nexport const pick = (context) => \"apps/shop\";\n",
	})
	for _, c := range []struct{ name, source, want string }{
		// một cửa vắng thì không có dòng; một trường chưa ghi thì không có mặt
		{"an application alone", head + "return application().check();",
			"application  application.work:2:8\n"},
		{"only what was written", head + "const app = application({ timezone: \"UTC\" });\napp.website().listen(8080);\nreturn app.check();",
			"application  application.work:2:13  timezone \"UTC\"\n" +
				"  website    application.work:3:1   listen 8080\n"},
		// một hàm là chỗ của nó, kể cả hàm của file khác; danh sách toàn hàm là số hàm và chữ của nó
		{"functions", head + "import { https, pick } from \"./host/web.work\";\nconst app = application();\n" +
			"app.website().guard(https).guard((context) => context.next()).routes(pick).success(https).listen(\":443\");\nreturn app.run();",
			"application  application.work:3:13\n" +
				"  website    application.work:4:1   listen \":443\" · 2 guard · routes host/web.work:2:21 · 1 success\n"},
		// giá trị viết như người viết viết, kể cả sai: Describe không kiểm gì
		{"values as written", head + "const app = application({ memory: 5 });\n" +
			"app.website({ warm: true, concurrency: -2, ssl: null }).guard(5).listen(70000, \"la.vn:80\");\nreturn app.check();",
			"application  application.work:2:13  memory 5\n" +
				"  website    application.work:3:1   listen 70000, \"la.vn:80\" · concurrency -2 · warm true · ssl null · guard 5\n"},
		// một hàm không phải của file nào (hàm có sẵn) là "function"
		{"a function of the engine", head + "const app = application();\napp.website().routes(console.log).listen(80);\nreturn app.check();",
			"application  application.work:2:13\n" +
				"  website    application.work:3:1   listen 80 · routes function\n"},
		// database: mật khẩu chỉ in là có; không cổng thì không có listen; mọi trường của nó
		{"a database", head + "const app = application();\n" +
			"app.database({ port: [5445, \"127.0.0.1:5446\"], user: \"kitdb\", password: \"hunter2\", tls: true, binding: true, " +
			"authority: \"authority.pem\", concurrency: 64, memory: \"256mb\", timeout: \"10s\" }).open(\".database\");\nreturn app.check();",
			"application  application.work:2:13\n" +
				"  database   application.work:3:1   open \".database\" · listen 5445, \"127.0.0.1:5446\" · concurrency 64 · memory \"256mb\" · " +
				"timeout \"10s\" · user \"kitdb\" · password •••• · tls true · binding true · authority \"authority.pem\"\n"},
		{"a database with no port", head + "const app = application();\napp.database().open(\".database\");\nreturn app.check();",
			"application  application.work:2:13\n" +
				"  database   application.work:3:1   open \".database\"\n"},
		// schedule: các thư mục của chữ cuối run, là chuỗi, mảng hay hàm; command: tên, rồi tên các lệnh
		{"a schedule and a command", head + "const app = application();\n" +
			"app.schedule({ memory: \"512mb\", energy: 1000 }).finally((context) => 1).run([\".schedules\", \"apps/a/.schedules\"]);\n" +
			"app.command(\"kitcli\").success((context) => 1).execute({ backup: (context) => 1, migrate: (context) => 2 });\nreturn app.check();",
			"application  application.work:2:13\n" +
				"  schedule   application.work:3:1   run [\".schedules\", \"apps/a/.schedules\"] · memory \"512mb\" · energy 1000 · 1 finally\n" +
				"  command    application.work:4:1   name \"kitcli\" · execute backup, migrate · 1 success\n"},
		// thứ tự dòng là website, database, schedule, command, dù file viết theo thứ tự khác; desktop, mobile ở cuối
		{"the order of the doors", head + "const app = application();\napp.mobile({ theme: \"dark\" });\napp.command(\"cli\").execute({ a: (context) => 1 });\n" +
			"app.schedule().run(\".schedules\");\napp.database().open(\"data\");\napp.desktop(\"Shop\");\napp.website().listen(80);\nreturn app.check();",
			"application  application.work:2:13\n" +
				"  website    application.work:8:1   listen 80\n" +
				"  database   application.work:6:1   open \"data\"\n" +
				"  schedule   application.work:5:1   run \".schedules\"\n" +
				"  command    application.work:4:1   name \"cli\" · execute a\n" +
				"  desktop    application.work:7:1   \"Shop\"\n" +
				"  mobile     application.work:3:1   { theme }\n"},
		// một câu nối liền: mọi cửa cùng chỗ của câu ấy
		{"one sentence", head + "return application({ memory: \"1gb\" }).website().listen(443).database().open(\".database\").run();",
			"application  application.work:2:8  memory \"1gb\"\n" +
				"  website    application.work:2:1\n" +
				"  database   application.work:2:1\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := describe(t, folder, c.source)
			if c.name == "one sentence" { // các giá trị của hai cửa vẫn có mặt: chỉ so chỗ ở cột hai
				for _, line := range strings.Split(strings.TrimSuffix(c.want, "\n"), "\n") {
					if !strings.Contains(got, line) {
						t.Errorf("want the line %q in:\n%s", line, got)
					}
				}
				return
			}
			sameLines(t, c.name, got, c.want)
			if strings.Contains(got, "hunter2") {
				t.Error("the password of the database is printed")
			}
		})
	}

	// Go dựng bản thiết kế, không qua application.work: không có chỗ khai, các giá trị vẫn in; thứ không phải application
	// thì không có gì để in
	made := application.New(value.Object().Set("memory", value.String("2gb")))
	if got := (host.Booted{Blueprint: made}).Describe(); got != "application    memory \"2gb\"\n" {
		t.Errorf("a blueprint Go built: %q", got)
	}
	if got := (host.Booted{Blueprint: value.Number(5)}).Describe(); got != "" {
		t.Errorf("what is no application: %q", got)
	}
}
