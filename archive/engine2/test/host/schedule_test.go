package host_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/library/schedule"
)

// kitwork/schedule (docs/KITWORK_SCHEDULE.md): the schedules of the app's core/schedule/, each due time a run of
// kitwork_runs stored before it runs, run by the app's clock on the queue's machinery.

const scheduleImport = "import * as schedule from \"kitwork/schedule\";\n"

// scheduleApp is an app whose core/schedule/ holds schedules (file name → source), in the zone Asia/Ho_Chi_Minh when zoned,
// and a site shop.test that reads them: GET /list, GET /runs/<file>/<name>, POST /now/<file>/<name>, GET /failed.
func scheduleApp(schedules map[string]string, zoned bool, names map[string][]string) map[string]string {
	files := map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		"apps/shop/shop.test/list/router.work": routerImport + scheduleImport + `router.get((context) => context.json(schedule.list()));
`,
		"apps/shop/shop.test/failed/router.work": routerImport + scheduleImport + `router.get((context) => context.json(schedule.status("failed").runs()));
`,
	}
	if zoned {
		files["apps/shop/module.work"] = zonedModule("Asia/Ho_Chi_Minh")
	}
	for name, source := range schedules {
		if name == "config.work" {
			name = "rules.work" // the rules of the schedules, a plain file: config.work has a role (17/11/2026)
		}
		files["apps/shop/core/schedule/"+name] = source
		file := strings.TrimSuffix(name, ".work")
		for _, each := range names[name] {
			imported := "import { " + each + " } from \"app/core/schedule/" + name + "\";\n"
			files["apps/shop/shop.test/runs/"+file+"/"+each+"/router.work"] = routerImport + imported + `router.get((context) => context.json(` + each + `.runs()));
`
			files["apps/shop/shop.test/now/"+file+"/"+each+"/router.work"] = routerImport + imported + `router.post((context) => context.json(safe(() => ` + each + `.now())));
`
		}
	}
	return files
}

// clock is the schedules' clock of a test, moved by hand.
type clock struct{ now atomic.Int64 }

func (c *clock) set(at string) {
	parsed, err := time.Parse(time.RFC3339, at)
	if err != nil {
		panic(err)
	}
	c.now.Store(parsed.UnixNano())
}

// handClock makes the schedules read the time from a clock the test moves, from at, until the test ends.
func handClock(t *testing.T, at string) *clock {
	c := &clock{}
	c.set(at)
	previous := schedule.Now
	schedule.Now = func() time.Time { return time.Unix(0, c.now.Load()) }
	t.Cleanup(func() { schedule.Now = previous })
	return c
}

// scheduleRun is a run of a schedule as runs() gives it.
type scheduleRun struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	At          string   `json:"at"`
	Trigger     string   `json:"trigger"`
	Status      string   `json:"status"`
	Attempt     int      `json:"attempt"`
	MaxAttempts int      `json:"maxAttempts"`
	Log         string   `json:"log"`
	Error       *failure `json:"error"`
}

func (b *booted) scheduleRuns(t *testing.T, path string) []scheduleRun {
	t.Helper()
	got := b.get(t, "shop.test", path)
	if got.status != 200 {
		t.Fatalf("GET %s: %d %s\n%s", path, got.status, got.body, b.log)
	}
	var runs []scheduleRun
	if err := json.Unmarshal([]byte(got.body), &runs); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, got.body)
	}
	return runs
}

// waitRuns waits for the runs at path to be what done says, and gives them.
func (b *booted) waitRuns(t *testing.T, path string, done func([]scheduleRun) bool) []scheduleRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		runs := b.scheduleRuns(t, path)
		if done(runs) {
			return runs
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never as awaited: %+v\n%s", path, runs, b.log)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ended is true when every run has ended and there are count of them.
func ended(count int) func([]scheduleRun) bool {
	return func(runs []scheduleRun) bool {
		if len(runs) != count {
			return false
		}
		for _, each := range runs {
			if each.Status != "completed" && each.Status != "failed" {
				return false
			}
		}
		return true
	}
}

func TestAScheduleRunsAtItsDueTimes(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"nhip.work": scheduleImport + `export const nhip = schedule.every("1m").timeout("5s").run((context) => {
  context.job.log("nhịp " + context.job.at, context.job.attempt + "/" + context.job.maxAttempts);
});
`,
		// a schedule's file imports another's: the schedule is loaded with its own file
		"zgoi.work": scheduleImport + "import { nhip } from \"app/core/schedule/nhip.work\";\n" + `export const goi = schedule.every("1h").run((context) => {
  nhip.now();
});
`,
	}, true, map[string][]string{"nhip.work": {"nhip"}})), "application.work"))
	b.host.StartBackground()
	// nothing before the sync is missed
	time.Sleep(30 * time.Millisecond)
	if runs := b.scheduleRuns(t, "/runs/nhip/nhip"); len(runs) != 0 {
		t.Fatalf("a schedule just declared: %+v", runs)
	}
	clock.set("2026-11-08T10:01:05+07:00")
	runs := b.waitRuns(t, "/runs/nhip/nhip", ended(1))
	if runs[0].At != "2026-11-08T10:01:00+07:00" || runs[0].Trigger != "schedule" || runs[0].Status != "completed" || runs[0].Log != "nhịp 2026-11-08T10:01:00+07:00 1/1\n" {
		t.Fatalf("the due time 10:01: %+v", runs[0])
	}
	// five due times missed run once, the last of them
	clock.set("2026-11-08T10:06:10+07:00")
	runs = b.waitRuns(t, "/runs/nhip/nhip", ended(2))
	if runs[0].At != "2026-11-08T10:06:00+07:00" {
		t.Fatalf("the due times missed: %+v", runs)
	}
	time.Sleep(30 * time.Millisecond)
	if again := b.scheduleRuns(t, "/runs/nhip/nhip"); len(again) != 2 {
		t.Fatalf("a due time handled is stored once: %+v", again)
	}
	got := b.get(t, "shop.test", "/list").body
	if want := `[{"name":"goi","rule":"every(\"1h\")","mode":"recurring","retry":0,"timeout":null,"timezone":"Asia/Ho_Chi_Minh","next":"2026-11-08T11:00:00+07:00","source":"core/schedule/zgoi.work"},{"name":"nhip","rule":"every(\"1m\")","mode":"recurring","retry":0,"timeout":"5s","timezone":"Asia/Ho_Chi_Minh","next":"2026-11-08T10:07:00+07:00","source":"core/schedule/nhip.work"}]`; got != want {
		t.Fatalf("list: %s, want %s", got, want)
	}
	if !strings.Contains(b.log.String(), "[schedule] worker chạy (kitwork_runs của .database/application/kitwork.db): 2 lịch: goi, nhip") || strings.Contains(b.log.String(), "[queue] worker") {
		t.Fatalf("the clock's line, and no worker of a queue:\n%s", b.log)
	}
}

func TestAScheduleThatFailsIsTriedAgainAndCarriesNoData(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"hong.work": scheduleImport + `export const hong = schedule.every("1m").retry(1).run((context) => {
  return error("hỏng lần " + context.job.attempt);
});

export const data = schedule.every("1m").run((context) => {
  context.data();
});

const seen = [];
export const writes = schedule.every("1m").run((context) => {
  seen.push(1);
});

export const declares = schedule.every("1m").run((context) => {
  schedule.every("1m");
});
`,
	}, true, map[string][]string{"hong.work": {"hong", "data", "writes", "declares"}})), "application.work"))
	b.host.StartBackground()
	clock.set("2026-11-08T10:01:01+07:00")
	runs := b.waitRuns(t, "/runs/hong/hong", ended(1))
	if runs[0].Status != "failed" || runs[0].Attempt != 2 || runs[0].Error == nil || runs[0].Error.Code != "FAIL" || !strings.Contains(runs[0].Error.Message, "hỏng lần 2") {
		t.Fatalf("hong: %+v", runs[0])
	}
	runs = b.waitRuns(t, "/runs/hong/data", ended(1))
	if runs[0].Status != "failed" || runs[0].Error == nil || !strings.Contains(runs[0].Error.Message, "lịch không có context.data()") {
		t.Fatalf("context.data(): %+v", runs[0])
	}
	// a schedule writes nothing of its file, and declares nothing as it runs
	if runs := b.waitRuns(t, "/runs/hong/writes", ended(1)); runs[0].Status != "failed" || runs[0].Error == nil || runs[0].Error.Code != "TYPE" {
		t.Fatalf("a schedule writes its file's array: %+v", runs[0])
	}
	if runs := b.waitRuns(t, "/runs/hong/declares", ended(1)); runs[0].Status != "failed" || runs[0].Error == nil || !strings.Contains(runs[0].Error.Message, "schedule.every khai ở cấp file của một file .work thường của app") {
		t.Fatalf("a schedule declared as a schedule runs: %+v", runs[0])
	}
	if failed := b.scheduleRuns(t, "/failed"); len(failed) != 4 {
		t.Fatalf("schedule.status(\"failed\").runs(): %+v", failed)
	}
}

func TestAScheduleStillRunningPassesItsNextDueTimeOver(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"lau.work": scheduleImport + `export const lau = schedule.every("1m").run((context) => {
  let total = 0;
  for (let i = 0; i < 400; i++) {
    total = total + JSON.stringify({ text: "x".repeat(200000) }).length;
  }
});
`,
	}, true, map[string][]string{"lau.work": {"lau"}})), "application.work"))
	b.host.StartBackground()
	clock.set("2026-11-08T10:01:01+07:00")
	b.waitRuns(t, "/runs/lau/lau", func(runs []scheduleRun) bool { return len(runs) == 1 && runs[0].Status == "running" })
	clock.set("2026-11-08T10:02:01+07:00")
	b.waitRuns(t, "/runs/lau/lau", ended(1))
	time.Sleep(30 * time.Millisecond)
	if runs := b.scheduleRuns(t, "/runs/lau/lau"); len(runs) != 1 {
		t.Fatalf("10:02 came while 10:01 ran, and is passed over: %+v", runs)
	}
}

func TestAOnceRunsOnceAndNowRunsAtOnce(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-30T23:00:00+07:00")
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"raMat.work": scheduleImport + `export const raMat = schedule.on("2026-12-01T09:00:00+07:00").once((context) => {
  context.job.log("mở bán " + context.job.at);
});
`,
	}, false, map[string][]string{"raMat.work": {"raMat"}})), "application.work"))
	b.host.StartBackground()
	now := b.safely(t, http.MethodPost, "/now/raMat/raMat")
	if now.Error != nil || now.Value.Name != "raMat" || now.Value.Status != "pending" {
		t.Fatalf("now(): %+v %s", now.Value, now.Error)
	}
	runs := b.waitRuns(t, "/runs/raMat/raMat", ended(1))
	if runs[0].Trigger != "now" || runs[0].At != "2026-11-30T23:00:00+07:00" || runs[0].Log != "mở bán 2026-11-30T23:00:00+07:00\n" {
		t.Fatalf("the run of now(): %+v", runs[0])
	}
	clock.set("2026-12-01T09:00:02+07:00")
	runs = b.waitRuns(t, "/runs/raMat/raMat", ended(2))
	if runs[0].Trigger != "schedule" || runs[0].At != "2026-12-01T09:00:00+07:00" {
		t.Fatalf("the instant: %+v", runs[0])
	}
	clock.set("2026-12-02T09:00:02+07:00")
	time.Sleep(30 * time.Millisecond)
	if again := b.scheduleRuns(t, "/runs/raMat/raMat"); len(again) != 2 {
		t.Fatalf("once() runs once: %+v", again)
	}
	if got := b.get(t, "shop.test", "/list").body; !strings.Contains(got, `"mode":"once"`) || !strings.Contains(got, `"next":null`) || !strings.Contains(got, `"timezone":"+07:00"`) {
		t.Fatalf("list of a once() that ran: %s", got)
	}
}

func TestAScheduleCountsInTheAppsZoneOrSaysUTC(t *testing.T) {
	handClock(t, "2026-11-08T10:00:30+07:00")
	source := map[string]string{"nhip.work": scheduleImport + `export const nhip = schedule.daily("09:00").run((context) => {});
export const paris = schedule.daily("09:00").timezone("Europe/Paris").run((context) => {});
export const raMat = schedule.on("2026-12-01T09:00:00+07:00").once((context) => {});
`}
	zoned := boot(t, filepath.Join(write(t, scheduleApp(source, true, nil)), "application.work"))
	if got := zoned.get(t, "shop.test", "/list").body; !strings.Contains(got, `"name":"nhip","rule":"daily(\"09:00\")","mode":"recurring","retry":0,"timeout":null,"timezone":"Asia/Ho_Chi_Minh","next":"2026-11-09T09:00:00+07:00"`) ||
		!strings.Contains(got, `"timezone":"Europe/Paris","next":"2026-11-08T09:00:00+01:00"`) ||
		!strings.Contains(got, `"name":"raMat","rule":"on(\"2026-12-01T09:00:00+07:00\")","mode":"once","retry":0,"timeout":null,"timezone":"+07:00","next":"2026-12-01T09:00:00+07:00"`) {
		t.Fatalf("zones: %s", got)
	}
	if strings.Contains(zoned.log.String(), "đếm theo UTC") {
		t.Fatalf("an app with a zone says nothing:\n%s", zoned.log)
	}
	plain := boot(t, filepath.Join(write(t, scheduleApp(source, false, nil)), "application.work"))
	if !strings.Contains(plain.log.String(), "core/schedule/nhip.work:2:29: lịch nhip không viết .timezone(…), app không khai module.timezone(…), host không khai app.timezone(…): đếm theo UTC") ||
		strings.Contains(plain.log.String(), "lịch paris không viết") {
		t.Fatalf("an app with no zone:\n%s", plain.log)
	}
}

func TestAScheduleIsDeclaredRightOrTheAppDoesNotBoot(t *testing.T) {
	handClock(t, "2026-11-08T10:00:30+07:00")
	declare := func(source string) map[string]string {
		return scheduleApp(map[string]string{"lich.work": scheduleImport + source}, true, nil)
	}
	run := `.run((context) => {})`
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"not exported":    {declare(`const nhip = schedule.every("1m")` + run + ";\n"), "shop/core/schedule/lich.work:2:22: lịch không export"},
		"not ended":       {declare(`export const nhip = schedule.every("1m").retry(1);` + "\n"), "lịch nhip chưa kết thúc: nối .run(fn)"},
		"a wrong clock":   {declare(`export const nhip = schedule.daily("25:00")` + run + ";\n"), `giờ "25:00" sai`},
		"weekly no at":    {declare(`export const nhip = schedule.weekly("monday")` + run + ";\n"), `weekly(…) thiếu .at(…)`},
		"daily at":        {declare(`export const nhip = schedule.daily("09:00").at("10:00")` + run + ";\n"), "giờ đã là tham số của daily"},
		"first and last":  {declare(`export const nhip = schedule.monday("09:00").first("month").last("month")` + run + ";\n"), "first và last không đi cùng nhau"},
		"on with run":     {declare(`export const nhip = schedule.on("2026-12-01T09:00:00+07:00")` + run + ";\n"), "chỉ đi với .once(handler)"},
		"two ends":        {declare(`export const nhip = schedule.daily("09:00").once((c) => {}).run((c) => {});` + "\n"), ".run và .once loại trừ nhau và đứng cuối"},
		"after the end":   {declare(`export const nhip = schedule.daily("09:00")` + run + ".retry(1);\n"), "sau .run(…) không nối gì"},
		"a past instant":  {declare(`export const nhip = schedule.on("2026-01-01T09:00:00+07:00").once((c) => {});` + "\n"), `schedule.on("2026-01-01T09:00:00+07:00") đã qua lúc khai`},
		"a removed word":  {declare(`export const nhip = schedule.cron("* * * * *")` + run + ";\n"), "schedule.cron đã bỏ"},
		"a zone not IANA": {declare(`export const nhip = schedule.daily("09:00").timezone("Local")` + run + ";\n"), "không theo đồng hồ của máy chủ"},
		"two names":       {declare(`export const nhip = schedule.every("1m")` + run + ";\nexport const tim = nhip;\n"), "nhip export thêm dưới tên tim"},
		// below a millisecond the store wrote 0, no timeout at all (15/11/2026)
		"a timeout below 1ms": {declare(`export const nhip = schedule.every("1m").timeout("500us")` + run + ";\n"), `.timeout("500us"): hạn của một lần thử là một khoảng từ 1ms tới 24h`},
	}
	two := scheduleApp(map[string]string{
		"a.work": scheduleImport + `export const nhip = schedule.every("1m")` + run + ";\n",
		"b.work": scheduleImport + `export const nhip = schedule.every("2m")` + run + ";\n",
	}, true, nil)
	cases["one name in two files"] = struct {
		files map[string]string
		want  string
	}{two, "lịch nhip đã khai ở core/schedule/a.work:2:29; tên lịch duy nhất trong app"}
	outside := scheduleApp(nil, true, nil)
	outside["apps/shop/shop.test/router.work"] = routerImport + scheduleImport + `const nhip = schedule.every("1m")` + run + ";\n"
	cases["in router.work"] = struct {
		files map[string]string
		want  string
	}{outside, "schedule.every khai ở cấp file của một file .work thường của app (không phải router.work"}
	inSite := scheduleApp(nil, true, nil)
	inSite["apps/shop/shop.test/.schedule/nhip.work"] = scheduleImport + `export const nhip = schedule.every("1m")` + run + ";\n"
	cases["a site's .schedule"] = struct {
		files map[string]string
		want  string
	}{inSite, "shop/shop.test: .schedule/ đã bỏ (17/11/2026)"}
	for _, config := range []struct{ name, source, want string }{
		{"keep twice", "schedule.keep(\"2h\");\nschedule.keep(\"3h\");\n", "schedule.keep gọi hai lần, ở core/schedule/rules.work:2:9 và core/schedule/rules.work:3:9"},
		{"keep wrong", "schedule.keep(\"1m\");\n", `schedule.keep nhận một khoảng từ "1h"`},
		{"session wrong", "schedule.session(\"system\");\n", "schedule.session nhận một object"},
		{"database wrong", "schedule.database(\"jobs\");\n", "schedule.database nhận một database của app đã import"},
	} {
		files := scheduleApp(map[string]string{"config.work": scheduleImport + config.source}, true, nil)
		cases["config: "+config.name] = struct {
			files map[string]string
			want  string
		}{files, config.want}
	}
	inConfig := scheduleApp(nil, true, nil)
	inConfig["apps/shop/shop.test/config.work"] = scheduleImport + `const nhip = schedule.every("1m")` + run + ";\n"
	cases["a rule in a site's config.work"] = struct {
		files map[string]string
		want  string
	}{inConfig, "schedule.every khai ở cấp file của một file .work thường của app (không phải router.work, config.work"}
	booting := declare(`export const nhip = schedule.every("1m")` + run + ";\n")
	booting["apps/shop/shop.test/router.work"] = routerImport + "import { nhip } from \"app/core/schedule/lich.work\";\nnhip.now();\n"
	cases["now() as a file is read"] = struct {
		files map[string]string
		want  string
	}{booting, "nhip.now() chạy lúc một request chạy, không lúc file được đọc"}
	library := scheduleApp(map[string]string{"config.work": scheduleImport + "schedule.keep(\"2h\");\n"}, true, nil)
	library["apps/shop/core/limits.work"] = scheduleImport + "schedule.keep(\"3h\");\nexport const x = 1;\n"
	cases["a config word in two files"] = struct {
		files map[string]string
		want  string
	}{library, "schedule.keep gọi hai lần, ở core/limits.work:2:9 và core/schedule/rules.work:2:9: mỗi chữ một lần trong app"}
	elsewhere := scheduleApp(nil, true, nil)
	elsewhere["apps/shop/shop.test/router.work"] = routerImport + scheduleImport + "schedule.keep(\"2h\");\nrouter.get((context) => context.text(\"a\"));\n"
	cases["config word in router.work"] = struct {
		files map[string]string
		want  string
	}{elsewhere, "schedule.keep viết ở cấp file của một file .work thường của app"}
	for name, each := range cases {
		if got := bootError(t, each.files); !strings.Contains(got, each.want) {
			t.Errorf("%s: %q, want %q", name, got, each.want)
		}
	}
}

func TestAClosedClockRunsNothing(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"nhip.work": scheduleImport + `export const nhip = schedule.every("1m").run((context) => {});
`,
	}, true, nil)), "application.work"))
	b.host.StartBackground()
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	clock.set("2026-11-08T10:05:00+07:00")
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(b.log.String(), "[schedule] nhip, tới giờ") {
		t.Fatalf("the clock of an app closed still looks:\n%s", b.log)
	}
}

func TestAScheduleSessionIsReadAndSaid(t *testing.T) {
	b := boot(t, filepath.Join(write(t, scheduleApp(map[string]string{
		"config.work": scheduleImport + "schedule.session({ role: \"system\" });\n",
	}, true, nil)), "application.work"))
	if !strings.Contains(b.log.String(), "core/schedule/rules.work:2:9: schedule.session(…) chưa có trên engine2: lịch chạy không phiên") {
		t.Fatalf("schedule.session is said:\n%s", b.log)
	}
}

func TestAScheduleRefusesWhatItIsNot(t *testing.T) {
	handClock(t, "2026-11-08T10:00:30+07:00")
	files := scheduleApp(map[string]string{"nhip.work": scheduleImport + `export const nhip = schedule.every("1m").run((context) => {});
`}, true, nil)
	files["apps/shop/shop.test/bad/router.work"] = routerImport + scheduleImport + "import { nhip } from \"app/core/schedule/nhip.work\";\n" + `const cases = {
  rule: () => schedule.every("1m"),
  removed: () => schedule.jobs(),
  statusList: () => schedule.status("failed").list(),
  status: () => schedule.status("cancelled"),
  where: () => schedule.where(() => true),
  list: () => nhip.list(),
  now: () => nhip.now(1),
  chained: () => nhip.keep("1d"),
};
router.post((context) => context.json(safe(cases[context.request.query("what")])));
`
	b := boot(t, filepath.Join(write(t, files), "application.work"))
	for what, want := range map[string]string{
		"rule":       "INVALID_ARGUMENT schedule.every khai ở cấp file của một file .work thường của app",
		"removed":    "INVALID_ARGUMENT schedule.jobs() không có",
		"statusList": `INVALID_ARGUMENT schedule.status("failed").list() không có`,
		"status":     "INVALID_ARGUMENT schedule.status nhận một trạng thái của lần chạy: pending, running, completed, failed",
		"where":      "UNSUPPORTED schedule.where(fn) chưa có",
		"list":       "INVALID_ARGUMENT .list() không có trên lịch",
		"now":        "INVALID_ARGUMENT nhip.now() không nhận tham số",
		"chained":    "INVALID_ARGUMENT .keep đã bỏ",
	} {
		got := b.safely(t, http.MethodPost, "/bad?what="+what)
		code, message, _ := strings.Cut(want, " ")
		if got.Error == nil || got.Error.Code != code || !strings.Contains(got.Error.Message, message) {
			t.Errorf("%s: %s, want %q", what, got.Error, want)
		}
	}
}

func TestAScheduleKeepsItsStateAcrossARestart(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-30T23:00:00+07:00")
	names := map[string][]string{"lich.work": {"raMat", "tim", "nhip"}}
	declare := func(nhip string) map[string]string {
		return scheduleApp(map[string]string{"lich.work": scheduleImport + `export const raMat = schedule.on("2026-12-01T09:00:00+07:00").once((context) => {
  context.job.log("mở bán " + context.job.at);
});
export const tim = schedule.every("1m").run((context) => {});
export const nhip = schedule.every("` + nhip + `").run((context) => {});
`}, true, names)
	}
	files := declare("1m")
	root := write(t, files)
	first := boot(t, filepath.Join(root, "application.work"))
	first.host.StartBackground() // the states are written as the clock starts
	if err := first.host.Close(); err != nil {
		t.Fatal(err)
	}
	// the instant has passed while the app was down; nhip is written with another rule
	clock.set("2026-12-01T10:00:30+07:00")
	changed := declare("2m")
	for name, text := range changed {
		if strings.HasPrefix(name, "apps/shop/core/schedule/") {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	second := boot(t, filepath.Join(root, "application.work"))
	second.host.StartBackground()
	runs := second.waitRuns(t, "/runs/lich/raMat", ended(1))
	if runs[0].At != "2026-12-01T09:00:00+07:00" || runs[0].Log != "mở bán 2026-12-01T09:00:00+07:00\n" {
		t.Fatalf("the instant missed while the app was down: %+v", runs[0])
	}
	if runs := second.waitRuns(t, "/runs/lich/tim", ended(1)); runs[0].At != "2026-12-01T10:00:00+07:00" {
		t.Fatalf("a rule kept runs the due times missed once, the last: %+v", runs)
	}
	time.Sleep(30 * time.Millisecond)
	if runs := second.scheduleRuns(t, "/runs/lich/nhip"); len(runs) != 0 {
		t.Fatalf("a rule changed counts from now, nothing missed: %+v", runs)
	}
}
