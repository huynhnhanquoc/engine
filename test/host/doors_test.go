package host_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/host"
)

// Cách viết của application.work (owner 24/11/2026, docs/KITWORK_APPLICATION_WORK.md): const app = application(…), mỗi
// cửa một câu app.<cửa>(cấu hình).<hàm>….<chữ cuối>(giá trị chính), return app.run(). Các cửa gom về application: thư
// viện chỉ đưa ra application và environment. Ba luật giữ cho không cửa nào có mặt mà người đọc file không thấy; mỗi
// dòng dưới là một cách viết sai và lời báo của nó, kèm chỗ.

const doorsHead = `import { application } from "application";
const https = (context) => context.next();
const app = application();
`

func bootError(source string) string {
	_, err := host.Boot("application.work", source)
	if err == nil {
		return "no error"
	}
	return err.Error()
}

func TestTheSentenceOfADoor(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		// --- Luật 3: một câu phải có chữ cuối ---
		{"a website without listen", doorsHead + "app.website({ certificates: \".certificates\" }).guard(https);\nreturn app.run();",
			"application.work:4:1: the website is not closed: end the statement with .listen(…)"},
		{"a database without open", doorsHead + "app.database({ port: 5445, password: \"x\" });\nreturn app.run();",
			"application.work:4:1: the database is not closed: end the statement with .open(…)"},
		{"a schedule without run", doorsHead + "app.schedule().memory(\"1gb\");\nreturn app.run();",
			"application.work:4:1: the schedule is not closed: end the statement with .run(…)"},
		{"a command without execute", doorsHead + "app.command(\"kitcli\");\nreturn app.run();",
			"application.work:4:1: the command is not closed: end the statement with .execute(…)"},
		{"the door returned, not the application", doorsHead + "return app.website().guard(https);",
			"application.work:4:1: the website is not closed: end the statement with .listen(…)"},

		// --- Chữ cuối đòi giá trị của nó ---
		{"listen with no address", doorsHead + "app.website().listen();\nreturn app.run();",
			"application.work:4:15: website.listen(…) takes the addresses the website listens on: .listen(443, 80)"},
		{"open with no folder", doorsHead + "app.database().open();\nreturn app.run();",
			`application.work:4:16: database.open(…) takes the folder of the databases: .open(".database")`},
		{"run with no folders", doorsHead + "app.schedule().run();\nreturn app.run();",
			`application.work:4:16: schedule.run(…) takes the folders of the schedules: ".schedules", a list, or a function returning a list`},
		{"execute with no command", doorsHead + "app.command(\"kitcli\").execute({});\nreturn app.run();",
			"application.work:4:23: command.execute({ … }) needs at least one command: .execute({ backup: (context) => … })"},

		// --- Giá trị chính chỉ có một chỗ: sau chữ cuối ---
		{"an address where the door opens", doorsHead + "app.website(443).listen(80);\nreturn app.run();",
			"application.work:4:5: app.website(…) takes app.website() or app.website({ … }), and the addresses go to the closing word, .listen(443, 80): got app.website(number)"},
		{"listen as a key", doorsHead + "app.website({ listen: 443 }).listen(80);\nreturn app.run();",
			`application.work:4:5: website has no key "listen": listen is the closing word, end the statement with .listen(…)`},
		{"the folder where the database opens", doorsHead + "app.database(\".database\").open(\".database\");\nreturn app.run();",
			`application.work:4:5: app.database(…) takes app.database() or app.database({ … }), and the folder goes to the closing word, .open(".database"): got app.database(string)`},
		{"the folders where the schedule opens", doorsHead + "app.schedule(\".schedules\").run(\".schedules\");\nreturn app.run();",
			`application.work:4:5: app.schedule(…) takes app.schedule() or app.schedule({ … }), and the folders go to the closing word, .run(".schedules"): got app.schedule(string)`},

		// --- Luật 2: một cửa khai một lần ---
		{"a website twice", doorsHead + "app.website().listen(443);\napp.website().listen(8080);\nreturn app.run();",
			"application.work:5:5: application already has a website, declared at application.work:4:1: a door is declared once, in one statement"},
		{"a database twice, in one sentence", doorsHead + "return app.database().open(\"a\").database().open(\"b\").run();",
			"application.work:4:33: application already has a database, declared at application.work:4:1: a door is declared once, in one statement"},

		// --- Luật 1: cửa chỉ khai ở cấp file ---
		{"a door in a function", doorsHead + "const admin = () => app.website().listen(8080);\nadmin();\nreturn app.run();",
			"application.work:4:21: app is written at the top level of application.work only, one statement for each door, like app.website(…).listen(443): a function or another file gets functions, never app"},
		{"the application handed to a function", doorsHead + "const doors = (it) => it.website().listen(8080);\ndoors(app);\nreturn app.run();",
			"application.work:5:7: app is written at the top level of application.work only, one statement for each door, like app.website(…).listen(443): a function or another file gets functions, never app"},
		{"a door kept in a name", doorsHead + "const web = app.website();\nweb.listen(8080);\nreturn app.run();",
			"application.work:4:13: app is written at the top level of application.work only, one statement for each door, like app.website(…).listen(443): a function or another file gets functions, never app"},
		{"a door under a condition", doorsHead + "if (true) { app.database().open(\".database\"); }\nreturn app.run();",
			"application.work:4:13: app is written at the top level of application.work only, one statement for each door, like app.website(…).listen(443): a function or another file gets functions, never app"},
		{"the application under another name", doorsHead + "const other = app;\nreturn other.run();",
			"application.work:4:15: app is written at the top level of application.work only, one statement for each door, like app.website(…).listen(443): a function or another file gets functions, never app"},
		{"a second application", doorsHead + "const admin = application();\nreturn app.run();",
			"application.work:4:7: application(…) is written once: app is the application of this file"},
		{"an application in a function", `import { application } from "application";` + "\nconst make = () => application();\nreturn make().run();",
			"application.work:2:20: application(…) is written once, at the top level of application.work: const app = application(…)"},
		{"an application in a function, by the library's name", `import * as library from "application";` + "\nconst make = () => library.application();\nreturn make().run();",
			"application.work:2:28: application(…) is written once, at the top level of application.work: const app = application(…)"},
		{"an application that changes", `import { application } from "application";` + "\nlet app = application();\nreturn app.run();",
			"application.work:2:5: write const app = application(…): the application of the file never changes"},

		// --- Các cửa gom về application ---
		{"a door imported", `import { website, application } from "application";` + "\nreturn application().run();",
			`application.work:1:10: "application" has no "website": it exports application and environment, and a door is written app.website(…)`},
		{"a door as a key", `import { application } from "application";` + "\nreturn application({ website: 8080 }).run();",
			`application.work:2:8: application has no key "website": a door is written in its own statement, like app.website(…).listen(443)`},
		{"a word of a door on the application", doorsHead + "app.guard(https);\nreturn app.run();",
			`application.work:4:5: application has no word "guard": guard is a word of the website, write app.website(…).guard(…)`},
		{"a word of two doors on the application", doorsHead + "app.error(https);\nreturn app.run();",
			`application.work:4:5: application has no word "error": error is a word of the website, write app.website(…).error(…)`},
		{"a word no door has, in a door", doorsHead + "app.website().lisen(443);\nreturn app.run();",
			`application.work:4:15: website has no word "lisen": certificates, certs, concurrency, energy, error, finally, guard, listen, memory, port, routes, ssl, success, warm`},

		// --- Chương trình không có cổng của riêng nó (owner 24/11): địa chỉ là của cửa nghe nó ---
		{"a port of the program", `import { application } from "application";` + "\nreturn application(8080).run();",
			"application.work:2:8: application(…) takes application() or application({ … }), and an address is written by the door that listens on it, like app.website(…).listen(443): got application(number)"},
		{"listen as a key of the program", `import { application } from "application";` + "\nreturn application({ listen: 8080 }).run();",
			`application.work:2:8: application has no key "listen": an address is written by the door that listens on it, like app.website(…).listen(443)`},
		{"listen on the application", doorsHead + "app.listen(8080);\nreturn app.run();",
			`application.work:4:5: application has no word "listen": listen is a word of the website, write app.website(…).listen(…)`},
		{"port on the application", doorsHead + "app.port(8080);\nreturn app.run();",
			`application.work:4:5: application has no word "port": port is a word of the website, write app.website(…).port(…)`},

		// --- run và check: không đối số, một trong hai, dòng cuối ---
		{"no verb", doorsHead + "app.website().listen(443);\nreturn app;",
			"application.work: end with return app.run(), or return app.check() to load and check only"},
		{"doors given to run", doorsHead + "return app.run({ web: 1 });",
			"application.work:4:12: app.run() takes nothing: a door is written in its own statement before it, like app.website(…).listen(443), got app.run(object)"},
		{"a door after run", doorsHead + "app.run();\napp.website().listen(443);\nreturn app;",
			"application.work:5:5: app.run() is the last line of application.work: write this before it"},
		{"a value after check", doorsHead + "app.check();\napp.memory(\"1gb\");\nreturn app;",
			"application.work:5:5: app.check() is the last line of application.work: write this before it"},
		{"run and check", doorsHead + "return app.check().run();",
			"application.work:4:20: app.check() is the last line of application.work: write this before it"},
		{"run as a key", `import { application } from "application";` + "\nreturn application({ run: true });",
			`application.work:2:8: application has no key "run": run is a verb, end with return app.run()`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := bootError(c.source); got != c.want {
				t.Errorf("\ngot  %s\nwant %s", got, c.want)
			}
		})
	}
}

// Cách viết đúng, ở các dạng của nó: app.run() đứng riêng rồi return app (bản owner đưa 24/11) cũng là nó; một cửa
// không cổng; một file không cửa nào chỉ để kiểm.
func TestTheSentencesThatBoot(t *testing.T) {
	for name, source := range map[string]string{
		"run on its own line":                doorsHead + "app.website().listen(443);\napp.run();\nreturn app;",
		"check":                              doorsHead + "app.website().listen(443);\nreturn app.check();",
		"no door at all":                     doorsHead + "return app.check();",
		"a database with no port":            doorsHead + "app.database().open(\".database\");\nreturn app.run();",
		"port closes a website":              doorsHead + "app.website().guard(https).port(8080);\nreturn app.run();",
		"folders as a list":                  doorsHead + "app.schedule({ memory: \"1gb\" }).run([\".schedules\"]);\nreturn app.run();",
		"a name of one's own":                `import { application as host } from "application";` + "\nconst server = host();\nserver.website().listen(443);\nreturn server.run();",
		"a guard may name its parameter app": doorsHead + "app.website().guard((app) => app.next()).listen(443);\nreturn app.run();",
		"the whole library":                  `import * as library from "application";` + "\nconst app = library.application();\nreturn app.check();",
	} {
		if got := bootError(source); got != "no error" {
			t.Errorf("%s: %s", name, got)
		}
	}
	if got := bootError(doorsHead + "return app.run();\n"); got != "no error" {
		t.Errorf("an application with no door runs: %s", got)
	}
}
