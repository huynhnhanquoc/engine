package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The app's code declares itself (slice 2 of root docs/KITWORK_MODULE_WORK.md, 17/11/2026): the boot reads every plain
// .work file of the app, wherever it is, and what its top level writes is declared: a table by connection().tables(…), a
// job by export const x = queue.job(…), a schedule by export const x = schedule.<rule>(…).run(…), a rule of the queue or
// the schedules once in the app. Until then each had a folder of its own (core/schema/, core/queue/, core/schedule/).

const declareHead = "import { struct, id, string } from \"kitwork/schema\";\nimport { connection } from \"kitwork/database\";\n"

// A table, a job and a schedule are declared in files of any folder, a site's among them and a folder below another;
// a draft (an underscore folder) and a dot folder other than core/ and core/schedule/ at the root are not read.
func TestTablesJobsAndSchedulesAreDeclaredInAnyFile(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                  manifest,
		"apps/shop/core/database.work":      declareHead + "const notes = struct({ id: id(), body: string() });\nexport const db = connection().tables({ notes });\n",
		"apps/shop/core/mail/thanks.work":   queueImport + "export const thanks = queue.job((context) => {});\n",
		"apps/shop/core/clock.work":         scheduleImport + "export const nhip = schedule.every(\"1m\").run((context) => {});\n",
		"apps/shop/core/rules.work":         queueImport + scheduleImport + "queue.parallel(2);\nschedule.keep(\"2h\");\n",
		"apps/shop/shop.test/helper.work":   queueImport + "export const later = queue.job((context) => {});\n",
		"apps/shop/_drafts/old.work":        queueImport + "export const old = queue.job((context) => {});\n",
		"apps/shop/shop.test/.notes/x.work": queueImport + "export const hidden = queue.job((context) => {});\n",
		"apps/shop/shop.test/router.work": routerImport + queueImport + scheduleImport +
			"router.get((context) => context.json({ jobs: queue.list(), schedules: schedule.list() }));\n",
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	body := b.get(t, "shop.test", "/").body
	for _, want := range []string{
		`"name":"later"`, `"source":"shop.test/helper.work"`, `"name":"thanks"`, `"source":"core/mail/thanks.work"`, `"name":"nhip"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the app's work lacks %s:\n%s", want, body)
		}
	}
	for _, unread := range []string{`"name":"old"`, `"name":"hidden"`} {
		if strings.Contains(body, unread) {
			t.Errorf("a file of a draft or a dot folder is read: %s\n%s", unread, body)
		}
	}
	// the table of core/database.work is declared: the database it names is not made yet, which the boot says
	contains(t, "the log", b.log.String(), "[shop] [database] .database/application/kitwork.db chưa có: kitwork migrate tạo nó từ khai báo của app")
}

// A file that imports another runs that one's declarations again: its tables are found by their place, its rules are the
// same call, so neither is declared twice; a job of an imported file is the job of the file that writes it.
func TestAFileImportedRunsItsDeclarationsAgain(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":             manifest,
		"apps/shop/core/database.work": declareHead + "const notes = struct({ id: id(), body: string().unique() });\nexport const db = connection().tables({ notes });\n",
		"apps/shop/core/rules.work":    queueImport + "queue.parallel(2);\nexport const limit = 2;\n",
		"apps/shop/core/thanks.work": queueImport + "import { db } from \"app/core/database.work\";\nimport { limit } from \"app/core/rules.work\";\n" +
			"export const thanks = queue.job((context) => db.count((notes) => notes.id > limit));\n",
		"apps/shop/core/report.work": "import { thanks } from \"app/core/thanks.work\";\nexport const report = thanks;\n",
		"apps/shop/shop.test/router.work": routerImport + queueImport + "import { db } from \"app/core/database.work\";\n" +
			"router.get((context) => context.json(queue.list()));\n",
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	body := b.get(t, "shop.test", "/").body
	if strings.Count(body, `"name":`) != 1 || !strings.Contains(body, `"name":"thanks","retry":0,"timeout":null,"source":"core/thanks.work"`) {
		t.Errorf("one job, thanks of core/thanks.work: %s", body)
	}
}

// kitwork migrate reads the app's files as its boot does: a table declared in core/ is a table it makes.
func TestMigrateReadsEveryFileOfTheApp(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work":                manifest,
		"apps/shop/core/database.work":    declareHead + "const notes = struct({ id: id(), body: string() });\nexport const db = connection().tables({ notes });\n",
		"apps/shop/shop.test/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
	})
	code, output := runMigrate(t, folder, true, "")
	if code != 0 || !strings.Contains(output, "notes") {
		t.Fatalf("migrate: %d\n%s", code, output)
	}
	if _, err := os.Stat(databaseFile(folder)); err != nil {
		t.Fatalf("the file of connection(): %v", err)
	}
}

// The app's .shared/ and .schedule/, the places of its code until 17/11/2026, are the boot's error naming the first file
// and where it goes (slice 2b): each file declares itself wherever it is, a dot folder is not read, so code left there
// would be lost unsaid. .shared/components/ too, since slice 7; config.work has a role, wherever it is. kitwork
// migrate refuses an app the same way, rather than plan without its tables.
func TestTheOldFoldersOfCodeAreErrors(t *testing.T) {
	site := map[string]string{"apps/shop/shop.test/router.work": says("shop")}
	with := func(files map[string]string) map[string]string {
		for name, text := range site {
			files[name] = text
		}
		return files
	}
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"a table":                         {with(map[string]string{"apps/shop/.shared/schema/shop.work": declareHead + "const notes = struct({ id: id() });\nexport const db = connection().tables({ notes });\n"}), `shop/.shared/schema/shop.work: .shared/ đã bỏ (17/11/2026): code của app nằm trong thư mục app tự đặt tên, như core/, mỗi file tự khai ở cấp file (bảng, job, lịch, component, luật chung của hàng đợi và lịch); giao diện dùng lại <tên>.ui của component đặt cạnh file khai nó; dời file này, chẳng hạn core/shop.work, import bằng "app/core/shop.work"`},
		"the queue's rules":               {with(map[string]string{"apps/shop/.shared/queue/config.work": queueImport + "queue.parallel(2);\n"}), `shop/.shared/queue/config.work: .shared/ đã bỏ (17/11/2026)`},
		"a schedule":                      {with(map[string]string{"apps/shop/.schedule/nhip.work": scheduleImport + "export const nhip = schedule.every(\"1m\").run((context) => {});\n"}), `shop/.schedule/nhip.work: .schedule/ đã bỏ (17/11/2026): lịch tự khai ở cấp file của một file .work thường, như core/`},
		"the rules of the schedules":      {with(map[string]string{"apps/shop/.schedule/config.work": scheduleImport + "schedule.keep(\"2h\");\n"}), `chẳng hạn core/schedule.work, import bằng "app/core/schedule.work"`},
		"code shared":                     {with(map[string]string{"apps/shop/.shared/greeting.work": "export const hello = 1;\n"}), `shop/.shared/greeting.work: .shared/ đã bỏ (17/11/2026)`},
		"a config.work of rules in core/": {with(map[string]string{"apps/shop/core/config.work": queueImport + "queue.parallel(2);\n"}), `shop/core/config.work: luật chung của hàng đợi và lịch (queue.parallel, keep, database; schedule.keep, database, session) viết ở cấp file của một file .work thường, không trong config.work`},
	} {
		if got := bootError(t, c.files); !strings.Contains(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
	}
	// .shared/components/ went the same way, with slice 7: a component declares itself in any plain file, its interface
	// beside it
	for file, want := range map[string]string{
		"apps/shop/.shared/components/app-panel.ui": "shop/.shared/components/app-panel.ui: .shared/ đã bỏ (17/11/2026)",
		"apps/shop/.shared/components/shop.work":    "shop/.shared/components/shop.work: .shared/ đã bỏ (17/11/2026)",
	} {
		if got := bootError(t, with(map[string]string{file: "<box><text>panel</text></box>"})); !strings.Contains(got, want) || !strings.Contains(got, "giao diện dùng lại <tên>.ui của component đặt cạnh file khai nó") {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
	folder := write(t, with(map[string]string{
		"application.work":                   manifest,
		"apps/shop/.shared/schema/shop.work": declareHead + "const notes = struct({ id: id() });\nexport const db = connection().tables({ notes });\n",
	}))
	if code, output := runMigrate(t, folder, true, ""); code == 0 || !strings.Contains(output, ".shared/ đã bỏ (17/11/2026)") {
		t.Errorf("migrate: %d\n%s", code, output)
	}
}
