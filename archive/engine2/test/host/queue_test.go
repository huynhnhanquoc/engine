package host_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/library/database"
	"github.com/kitwork/engine2/library/queue"
)

// kitwork/queue (docs/KITWORK_QUEUE.md): a job declared in the app's core/queue/, sent from a site through the
// handle it imports, stored in kitwork_runs of the app's database, run by the app's worker once the host serves.

const queueImport = "import * as queue from \"kitwork/queue\";\n"

// queueApp is an app whose core/queue/ holds jobs (file name → source), and a site shop.test whose routes send,
// read, cancel and replay them: POST /send/<job>?key=&delay=, GET /runs/<job>, GET /list, POST /cancel?id=,
// POST /replay?id=.
func queueApp(jobs map[string]string) map[string]string {
	files := map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		"apps/shop/shop.test/list/router.work": routerImport + queueImport + `router.get((context) => context.json(queue.list()));
`,
		"apps/shop/shop.test/cancel/router.work": routerImport + queueImport + `router.post((context) => context.json(safe(() => queue.cancel(context.request.query("id")))));
`,
		"apps/shop/shop.test/replay/router.work": routerImport + queueImport + `router.post((context) => context.json(safe(() => queue.replay(context.request.query("id")))));
`,
		"apps/shop/shop.test/failed/router.work": routerImport + queueImport + `router.get((context) => context.json(queue.status("failed").runs()));
`,
	}
	for name, source := range jobs {
		if name == "config.work" {
			name = "rules.work" // the rules of the queue, a plain file: config.work has a role (17/11/2026)
		}
		files["apps/shop/core/queue/"+name] = source
		if name == "rules.work" {
			// the rules are no job: no page sends or lists them (18/11/2026: the check above still said config.work, so a
			// page imported rules, which the file does not export, and read undefined until the import was an error)
			continue
		}
		job := strings.TrimSuffix(name, ".work")
		imported := "import { " + job + " } from \"app/core/queue/" + name + "\";\n"
		files["apps/shop/shop.test/send/"+job+"/router.work"] = routerImport + imported + `router.post((context) => {
  const key = context.request.query("key");
  const delay = context.request.query("delay");
  const options = {};
  if (key) {
    options.key = key;
  }
  if (delay) {
    options.delay = delay;
  }
  return context.json(safe(() => ` + job + `.send({ who: context.request.query("who") ?? "an" }, options)));
});
`
		files["apps/shop/shop.test/runs/"+job+"/router.work"] = routerImport + imported + `router.get((context) => context.json(` + job + `.runs()));
`
	}
	return files
}

// quickQueue makes the worker look every few milliseconds and retry at once, until the test ends.
func quickQueue(t *testing.T) {
	poll, backoff, lease, prune := queue.PollInterval, queue.Backoff, queue.LeaseTTL, queue.PruneEvery
	queue.PollInterval = 5 * time.Millisecond
	queue.Backoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() {
		queue.PollInterval, queue.Backoff, queue.LeaseTTL, queue.PruneEvery = poll, backoff, lease, prune
	})
}

// queueSite boots the app files make and serves it; start runs its background work.
func queueSite(t *testing.T, files map[string]string, start bool, set ...func(*host.Host)) *booted {
	t.Helper()
	quickQueue(t)
	b := boot(t, filepath.Join(write(t, files), "application.work"), set...)
	if start {
		b.host.StartBackground()
	}
	return b
}

// run is a run as runs() gives it.
type run struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"maxAttempts"`
	Data        json.RawMessage `json:"data"`
	Key         *string         `json:"key"`
	Log         string          `json:"log"`
	Error       *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	StartedAt  *string `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
	NextRunAt  *string `json:"nextRunAt"`
	ReplayOf   *string `json:"replayOf"`
}

// sent is what a send answered through safe(…): its receipt, or its error.
type sent struct {
	Value struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Duplicate bool   `json:"duplicate"`
		ReplayOf  string `json:"replayOf"`
	} `json:"value"`
	Error *failure `json:"error"`
}

// failure is an error as safe(…) gives it.
type failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (f *failure) String() string {
	if f == nil {
		return "no error"
	}
	return f.Code + " " + f.Message
}

func (b *booted) send(t *testing.T, job, query string) sent {
	t.Helper()
	return b.safely(t, http.MethodPost, "/send/"+job+query)
}

func (b *booted) safely(t *testing.T, method, path string) sent {
	t.Helper()
	got := b.request(t, method, "shop.test", path, nil)
	if got.status != 200 {
		t.Fatalf("%s %s: %d %s\n%s", method, path, got.status, got.body, b.log)
	}
	var answer sent
	if err := json.Unmarshal([]byte(got.body), &answer); err != nil {
		t.Fatalf("%s %s: %v: %s", method, path, err, got.body)
	}
	return answer
}

func (b *booted) runs(t *testing.T, path string) []run {
	t.Helper()
	got := b.get(t, "shop.test", path)
	if got.status != 200 {
		t.Fatalf("GET %s: %d %s\n%s", path, got.status, got.body, b.log)
	}
	var runs []run
	if err := json.Unmarshal([]byte(got.body), &runs); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, got.body)
	}
	return runs
}

// settled waits for the run id of job to take status, and gives it.
func (b *booted) settled(t *testing.T, job, id, status string) run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last []run
	for time.Now().Before(deadline) {
		last = b.runs(t, "/runs/"+job)
		for _, each := range last {
			if each.ID == id && each.Status == status {
				return each
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s of %s never %s: %+v\n%s", id, job, status, last, b.log)
	return run{}
}

func TestAJobRunsWhatItIsSent(t *testing.T) {
	files := queueApp(map[string]string{
		// a place is counted in characters, as the runtime counts it: the comment before the declaration is not ASCII
		"thanks.work": queueImport + `const note = "cảm ơn"; export const thanks = queue.job((context) => {
  const { who } = context.data();
  context.job.log("gửi " + who, "lần " + context.job.attempt + "/" + context.job.maxAttempts, context.job.isFinalAttempt, context.job.name);
}).retry(1).timeout("30s");
`,
	})
	b := queueSite(t, files, false)
	receipt := b.send(t, "thanks", "?who=ana")
	if receipt.Error != nil || receipt.Value.Name != "thanks" || receipt.Value.Status != "pending" || !strings.HasPrefix(receipt.Value.ID, "r_") || receipt.Value.Duplicate {
		t.Fatalf("receipt: %+v %s\n%s", receipt.Value, receipt.Error, b.log)
	}
	// stored before the answer, run by no one until the host serves
	time.Sleep(30 * time.Millisecond)
	if runs := b.runs(t, "/runs/thanks"); len(runs) != 1 || runs[0].Status != "pending" || runs[0].NextRunAt == nil || string(runs[0].Data) != `{"who":"ana"}` {
		t.Fatalf("before the worker: %+v", runs)
	}
	b.host.StartBackground()
	b.host.StartBackground() // the worker starts once
	done := b.settled(t, "thanks", receipt.Value.ID, "completed")
	if done.Attempt != 1 || done.MaxAttempts != 2 || done.Log != "gửi ana lần 1/2 false thanks\n" || done.Error != nil || done.FinishedAt == nil || done.NextRunAt != nil {
		t.Fatalf("completed: %+v", done)
	}
	if got := b.get(t, "shop.test", "/list").body; got != `[{"name":"thanks","retry":1,"timeout":"30s","source":"core/queue/thanks.work"}]` {
		t.Fatalf("list: %s", got)
	}
	log := b.log.String()
	if strings.Count(log, "[queue] worker chạy (kitwork_runs của .database/application/kitwork.db): 1 job: thanks") != 1 || strings.Contains(log, "[schedule] worker") {
		t.Fatalf("the worker's line, once, and no clock:\n%s", b.log)
	}
	// what the job was declared with, for whoever reads the file (the port, a tool)
	if got := definitions(t, b.host.Folder); got != "thanks core/queue/thanks.work 1 30s 30000" {
		t.Fatalf("kitwork_definitions: %q", got)
	}
}

// definitions reads kitwork_definitions of the app at root: name, source, retry, timeout, timeout_ms of each job.
func definitions(t *testing.T, root string) string {
	t.Helper()
	opened, release, err := database.Acquire(filepath.Join(root, "apps", "shop", ".database", "application", "kitwork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var lines []string
	if err := opened.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		rows, err := snapshot.Query(`SELECT name, source, retry, timeout, timeout_ms FROM kitwork_definitions WHERE kind = 'queue' ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, source, timeout string
			var retry, milliseconds int
			if err := rows.Scan(&name, &source, &retry, &timeout, &milliseconds); err != nil {
				return err
			}
			lines = append(lines, fmt.Sprintf("%s %s %d %s %d", name, source, retry, timeout, milliseconds))
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "; ")
}

func TestASendWakesTheWorker(t *testing.T) {
	b := queueSite(t, queueApp(map[string]string{
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
	}), false)
	queue.PollInterval = time.Hour // the worker looks again only when told
	b.host.StartBackground()
	time.Sleep(20 * time.Millisecond)
	sent := b.send(t, "thanks", "")
	b.settled(t, "thanks", sent.Value.ID, "completed")
}

func TestAnAppThatClosesWaitsForItsRuns(t *testing.T) {
	slow := map[string]string{
		"slow.work": queueImport + `export const slow = queue.job((context) => {
  let total = 0;
  for (let i = 0; i < 400; i++) {
    total = total + JSON.stringify({ text: "x".repeat(200000) }).length;
  }
  context.job.log("xong");
});
`,
	}
	// a stringify costs what it writes (core/runtime, work.go): the default energy ends the job before the close
	b := queueSite(t, queueApp(slow), true, longWork)
	sent := b.send(t, "slow", "")
	b.settled(t, "slow", sent.Value.ID, "running")
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	if got := runStatus(t, b.host.Folder, sent.Value.ID); got != "completed" {
		t.Fatalf("a run running as the app closed: %s\n%s", got, b.log)
	}
	b.host.StartBackground() // closed: nothing starts again
	if lines := strings.Count(b.log.String(), "[queue] worker chạy"); lines != 1 {
		t.Fatalf("a worker started after the close:\n%s", b.log)
	}
	// past the grace, the close goes on and says so; the run stays running, its lease to run out
	grace := queue.StopGrace
	queue.StopGrace = time.Millisecond
	t.Cleanup(func() { queue.StopGrace = grace })
	b = queueSite(t, queueApp(slow), true, longWork)
	sent = b.send(t, "slow", "")
	b.settled(t, "slow", sent.Value.ID, "running")
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.log.String(), "[queue] các lần chạy còn đang chạy khi app đóng: không chờ thêm") {
		t.Fatalf("past the grace:\n%s", b.log)
	}
	time.Sleep(300 * time.Millisecond) // the attempt ends, its outcome not written: the file is let go
	if got := runStatus(t, b.host.Folder, sent.Value.ID); got != "running" {
		t.Fatalf("a run left past the grace: %s", got)
	}
}

func TestAnAppClosedStartsNothing(t *testing.T) {
	files := queueApp(map[string]string{"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`})
	files["apps/shop/core/schedule/nhip.work"] = scheduleImport + `export const nhip = schedule.every("1m").run((context) => {});
`
	b := queueSite(t, files, false)
	if err := b.host.Close(); err != nil {
		t.Fatal(err)
	}
	b.host.StartBackground()
	if log := b.log.String(); strings.Contains(log, "[queue] worker") || strings.Contains(log, "[schedule] worker") || strings.Contains(log, "đồng hồ") {
		t.Fatalf("a worker of an app closed: %s", b.log)
	}
}

// runStatus reads the status of the run id in kitwork_runs of the app at root, with its error when it has one.
func runStatus(t *testing.T, root, id string) string {
	t.Helper()
	opened, release, err := database.Acquire(filepath.Join(root, "apps", "shop", ".database", "application", "kitwork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	status := ""
	if err := opened.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		rows, err := snapshot.Query(`SELECT status, coalesce(error_message, '') FROM kitwork_runs WHERE id = ?`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			var message string
			if err := rows.Scan(&status, &message); err != nil {
				return err
			}
			if message != "" {
				status += ": " + message
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestAJobThatFailsIsTriedAgain(t *testing.T) {
	b := queueSite(t, queueApp(map[string]string{
		"late.work": queueImport + `export const late = queue.job((context) => {
  if (context.job.attempt < 3) {
    return error("hỏng lần " + context.job.attempt);
  }
  context.job.log("xong");
}).retry(2);
`,
		"broken.work": queueImport + `export const broken = queue.job((context) => {
  context.job.log("thử " + context.job.attempt);
  return error("luôn hỏng");
}).retry(1);
`,
		"old.work": queueImport + `export const old = queue.job((context) => {
  return context.payload;
});
`,
		// a job declares nothing as it runs, and writes nothing of its file: the file's top level is shared
		"declares.work": queueImport + `export const declares = queue.job((context) => {
  queue.job(() => {});
});
`,
		"writes.work": queueImport + `const seen = [];
export const writes = queue.job((context) => {
  seen.push(1);
});
`,
	}), true)
	late := b.send(t, "late", "")
	done := b.settled(t, "late", late.Value.ID, "completed")
	if done.Attempt != 3 || !strings.Contains(done.Log, "lần thử 1: FAIL") || !strings.Contains(done.Log, "lần thử 2: FAIL") || !strings.HasSuffix(done.Log, "xong\n") || done.Error != nil {
		t.Fatalf("late: %+v", done)
	}
	broken := b.send(t, "broken", "")
	failed := b.settled(t, "broken", broken.Value.ID, "failed")
	if failed.Attempt != 2 || failed.Error == nil || failed.Error.Code != "FAIL" || !strings.Contains(failed.Error.Message, "luôn hỏng") || !strings.Contains(failed.Log, "thử 2\n") || failed.FinishedAt == nil {
		t.Fatalf("broken: %+v", failed)
	}
	if runs := b.runs(t, "/failed"); len(runs) != 1 || runs[0].ID != broken.Value.ID {
		t.Fatalf("queue.status(\"failed\").runs(): %+v", runs)
	}
	old := b.send(t, "old", "")
	if gone := b.settled(t, "old", old.Value.ID, "failed"); gone.Error == nil || !strings.Contains(gone.Error.Message, "context.payload đã bỏ: dữ liệu đã gửi là context.data()") {
		t.Fatalf("a word of the old argument: %+v", gone)
	}
	declares := b.send(t, "declares", "")
	if gone := b.settled(t, "declares", declares.Value.ID, "failed"); gone.Error == nil || !strings.Contains(gone.Error.Message, "queue.job khai ở cấp file của một file .work thường của app") {
		t.Fatalf("a job declared as a job runs: %+v", gone)
	}
	writes := b.send(t, "writes", "")
	if gone := b.settled(t, "writes", writes.Value.ID, "failed"); gone.Error == nil || gone.Error.Code != "TYPE" {
		t.Fatalf("a job writes its file's array: %+v", gone)
	}
}

func TestAnAttemptPastItsTimeoutFails(t *testing.T) {
	files := queueApp(map[string]string{
		"slow.work": queueImport + `export const slow = queue.job((context) => {
  let total = 0;
  for (let i = 0; i < 1500000; i++) {
    total = total + i;
  }
  context.job.log("xong " + total);
}).timeout("1ms").retry(1);
`,
		// one call of a builtin, about a third of a second, that reads no deadline: the attempt is settled at its
		// time all the same, before the call returns
		"parse.work": queueImport + `export const parse = queue.job((context) => {
  const big = JSON.parse("[" + "1,".repeat(3000000) + "1]");
  context.job.log("parsed " + big.length);
}).timeout("20ms");
`,
		// the attempt stops at its time: what it would do after is not done
		"after.work": queueImport + `import { memory } from "kitwork/storage";

export const after = queue.job((context) => {
  let total = 0;
  for (let i = 0; i < 200000; i++) {
    total = total + i;
  }
  memory("after").set(total);
}).timeout("1ms");
`,
	})
	files["apps/shop/shop.test/after/router.work"] = routerImport + "import { memory } from \"kitwork/storage\";\n" + `router.get((context) => context.json({ after: memory("after").get() }));
`
	b := queueSite(t, files, true, longWork) // parse's call costs what it reads (core/runtime, work.go)
	b.settled(t, "after", b.send(t, "after", "").Value.ID, "failed")
	parse := b.send(t, "parse", "")
	slow := b.send(t, "slow", "")
	failed := b.settled(t, "slow", slow.Value.ID, "failed")
	if failed.Attempt != 2 || failed.Error == nil || failed.Error.Code != "TIMEOUT" || !strings.Contains(failed.Error.Message, `hết giờ: lần thử 2 chạy quá 1ms (.timeout("1ms"))`) ||
		!strings.Contains(failed.Log, "lần thử 1: TIMEOUT") || strings.Contains(failed.Log, "xong") {
		t.Fatalf("slow: %+v", failed)
	}
	early := b.settled(t, "parse", parse.Value.ID, "failed")
	started, _ := time.Parse("2006-01-02T15:04:05.000Z", *early.StartedAt)
	finished, _ := time.Parse("2006-01-02T15:04:05.000Z", *early.FinishedAt)
	if early.Error == nil || early.Error.Code != "TIMEOUT" || finished.Sub(started) > 150*time.Millisecond || strings.Contains(early.Log, "parsed") {
		t.Fatalf("an attempt in a long call, settled at its time: %s, %+v", finished.Sub(started), early)
	}
	// a second, read again and again: an attempt that ran on would write it in a few hundredths
	for until := time.Now().Add(time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		if got := b.get(t, "shop.test", "/after").body; got != `{"after":null}` {
			t.Fatalf("what an attempt past its time would do after: %s", got)
		}
	}
}

func TestASendWithAKeyIsOneRunAndADelayWaits(t *testing.T) {
	b := queueSite(t, queueApp(map[string]string{
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
	}), true)
	first := b.send(t, "thanks", "?key=order-1&delay=1h")
	again := b.send(t, "thanks", "?key=order-1")
	if first.Error != nil || again.Error != nil || again.Value.ID != first.Value.ID || !again.Value.Duplicate || first.Value.Duplicate {
		t.Fatalf("a key sent twice: %+v %+v", first, again)
	}
	other := b.send(t, "thanks", "?key=order-2")
	b.settled(t, "thanks", other.Value.ID, "completed")
	runs := b.runs(t, "/runs/thanks")
	if len(runs) != 2 || runs[1].ID != first.Value.ID || runs[1].Status != "pending" || runs[1].Key == nil || *runs[1].Key != "order-1" {
		t.Fatalf("the delayed run waits: %+v", runs)
	}
	due, err := time.Parse("2006-01-02T15:04:05.000Z", *runs[1].NextRunAt)
	if err != nil || time.Until(due) < 59*time.Minute {
		t.Fatalf("nextRunAt %s: %v", *runs[1].NextRunAt, err)
	}
}

func TestASendRefusesWhatIsNotData(t *testing.T) {
	files := queueApp(map[string]string{"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`})
	files["apps/shop/shop.test/bad/router.work"] = routerImport + queueImport + "import { thanks } from \"app/core/queue/thanks.work\";\n" + `const self = {};
self.self = self;
const cases = {
  function: () => thanks.send({ call: () => 1 }),
  item: () => thanks.send({ list: [1, () => 1] }),
  self: () => thanks.send(self),
  large: () => thanks.send({ text: "x".repeat(70000) }),
  wide: () => {
    let wide = [1];
    for (let i = 0; i < 50; i++) {
      wide = [wide, wide];
    }
    return thanks.send({ wide: wide });
  },
  nan: () => thanks.send({ n: 0 / 0 }),
  option: () => thanks.send({}, { wait: "1s" }),
  delay: () => thanks.send({}, { delay: "soon" }),
  key: () => thanks.send({}, { key: " " }),
  three: () => thanks.send({}, {}, 1),
  list: () => thanks.list(),
  declare: () => queue.job(() => {}),
  retry: () => thanks.retry(1),
  timeout: () => thanks.timeout("1s"),
  twice: () => thanks.status("failed").status("pending"),
  status: () => thanks.status("done"),
  statusList: () => queue.status("failed").list(),
  page: () => queue.runs(0),
  where: () => queue.where(() => true),
};
router.post((context) => context.json(safe(cases[context.request.query("what")])));
`
	b := queueSite(t, files, false)
	for what, want := range map[string]string{
		"function":   "INVALID_ARGUMENT thanks.send: data.call là function, không phải dữ liệu",
		"item":       "INVALID_ARGUMENT thanks.send: data.list[1] là function",
		"self":       "INVALID_ARGUMENT thanks.send: data.self",
		"large":      "TOO_LARGE thanks.send: dữ liệu viết thành JSON quá trần 64 KiB của một lần chạy (đã quá ở data.text)",
		"wide":       "TOO_LARGE thanks.send: dữ liệu viết thành JSON quá trần 64 KiB của một lần chạy (đã quá ở data.wide[0]",
		"nan":        "INVALID_ARGUMENT thanks.send: data.n là NaN, JSON không có số này",
		"option":     "INVALID_ARGUMENT send không có lựa chọn wait",
		"delay":      "INVALID_ARGUMENT delay soon không đọc được",
		"key":        "INVALID_ARGUMENT key là một chuỗi không rỗng",
		"three":      "INVALID_ARGUMENT thanks.send nhận dữ liệu và một object lựa chọn",
		"list":       "INVALID_ARGUMENT .list() không có trên job",
		"declare":    "INVALID_ARGUMENT queue.job khai ở cấp file của một file .work thường của app",
		"retry":      "INVALID_ARGUMENT .retry(n) là luật khai cùng job",
		"timeout":    "INVALID_ARGUMENT .timeout(hạn) là luật khai cùng job",
		"twice":      "INVALID_ARGUMENT thanks.status(…) viết hai lần",
		"status":     "INVALID_ARGUMENT thanks.status nhận một trạng thái của lần chạy",
		"statusList": `INVALID_ARGUMENT queue.status("failed").list() không có`,
		"page":       "INVALID_ARGUMENT runs(n): n là số nguyên từ 1 tới 1000",
		"where":      "UNSUPPORTED queue.where(fn) chưa có",
	} {
		got := b.safely(t, http.MethodPost, "/bad?what="+what)
		code, message, _ := strings.Cut(want, " ")
		if got.Error == nil || got.Error.Code != code || !strings.Contains(got.Error.Message, message) {
			t.Errorf("%s: %s, want %q", what, got.Error, want)
		}
	}
	if runs := b.runs(t, "/runs/thanks"); len(runs) != 0 {
		t.Fatalf("a refused send stores nothing: %+v", runs)
	}
}
func TestARunIsCancelledAndReplayed(t *testing.T) {
	b := queueSite(t, queueApp(map[string]string{
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {
  context.job.log("chạy " + context.data().who);
});
`,
		"other.work": queueImport + `export const other = queue.job((context) => {});
`,
	}), true)
	later := b.send(t, "thanks", "?delay=1h&who=bo")
	cancelled := b.safely(t, http.MethodPost, "/cancel?id="+later.Value.ID)
	if cancelled.Error != nil || cancelled.Value.Status != "cancelled" || cancelled.Value.ID != later.Value.ID {
		t.Fatalf("cancel of a pending run: %+v", cancelled)
	}
	if again := b.safely(t, http.MethodPost, "/cancel?id="+later.Value.ID); again.Error != nil || again.Value.Status != "cancelled" {
		t.Fatalf("cancel of a cancelled run: %+v", again)
	}
	replayed := b.safely(t, http.MethodPost, "/replay?id="+later.Value.ID)
	if replayed.Error != nil || replayed.Value.ID == later.Value.ID || replayed.Value.ReplayOf != later.Value.ID || replayed.Value.Status != "pending" {
		t.Fatalf("replay: %+v", replayed)
	}
	done := b.settled(t, "thanks", replayed.Value.ID, "completed")
	if done.Log != "chạy bo\n" || done.ReplayOf == nil || *done.ReplayOf != later.Value.ID {
		t.Fatalf("the replayed run: %+v", done)
	}
	for path, want := range map[string]string{
		"/cancel?id=" + done.ID:                 "CONFLICT",
		"/replay?id=" + replayed.Value.ID + "x": "NOT_FOUND",
		"/cancel?id=thanks":                     "INVALID_ARGUMENT",
		"/replay?id=thanks":                     "INVALID_ARGUMENT",
	} {
		if got := b.safely(t, http.MethodPost, path); got.Error == nil || got.Error.Code != want {
			t.Errorf("%s: %+v, want %s", path, got.Error, want)
		}
	}
	pending := b.send(t, "other", "?delay=1h")
	if got := b.safely(t, http.MethodPost, "/replay?id="+pending.Value.ID); got.Error == nil || got.Error.Code != "CONFLICT" {
		t.Errorf("replay of a pending run: %+v", got.Error)
	}
}

func TestAJobsHandleReachesItsOwnRunsOnly(t *testing.T) {
	files := queueApp(map[string]string{
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
		"other.work": queueImport + `export const other = queue.job((context) => {});
`,
		// a job sends another: a file of core/queue/ imports another's job, which is loaded with its own file
		"relay.work": queueImport + "import { other } from \"app/core/queue/other.work\";\n" + `export const relay = queue.job((context) => {
  other.send({ from: "relay" });
});
`,
	})
	files["apps/shop/shop.test/own/router.work"] = routerImport + "import { thanks } from \"app/core/queue/thanks.work\";\n" +
		`router.post((context) => {
  const id = context.request.query("id");
  if (context.request.query("what") === "replay") {
    return context.json(safe(() => thanks.replay(id)));
  }
  return context.json(safe(() => thanks.cancel(id)));
});
`
	b := queueSite(t, files, false)
	other := b.send(t, "other", "?delay=1h")
	if got := b.safely(t, http.MethodPost, "/own?id="+other.Value.ID); got.Error == nil || got.Error.Code != "NOT_FOUND" {
		t.Fatalf("thanks.cancel of other's run: %+v", got)
	}
	if runs := b.runs(t, "/runs/thanks"); len(runs) != 0 {
		t.Fatalf("thanks.runs() reads only its own: %+v", runs)
	}
	if got := b.safely(t, http.MethodPost, "/cancel?id="+other.Value.ID); got.Error != nil || got.Value.Status != "cancelled" {
		t.Fatalf("queue.cancel of it: %+v %s", got.Value, got.Error)
	}
	if got := b.safely(t, http.MethodPost, "/own?what=replay&id="+other.Value.ID); got.Error == nil || got.Error.Code != "NOT_FOUND" {
		t.Fatalf("thanks.replay of other's run: %+v", got)
	}
	b.host.StartBackground()
	relay := b.send(t, "relay", "")
	b.settled(t, "relay", relay.Value.ID, "completed")
	deadline := time.Now().Add(10 * time.Second)
	for {
		runs := b.runs(t, "/runs/other")
		if len(runs) == 2 && runs[0].Status == "completed" && string(runs[0].Data) == `{"from":"relay"}` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job relay sent: %+v", runs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestParallelBoundsTheRunsAtOnce(t *testing.T) {
	slow := queueImport + `export const slow = queue.job((context) => {
  let total = 0;
  for (let i = 0; i < 300000; i++) {
    total = total + i;
  }
});
`
	overlap := func(parallel string) bool {
		jobs := map[string]string{"slow.work": slow}
		if parallel != "" {
			jobs["config.work"] = queueImport + "queue.parallel(" + parallel + ");\n"
		}
		b := queueSite(t, queueApp(jobs), false)
		first, second := b.send(t, "slow", ""), b.send(t, "slow", "")
		b.host.StartBackground()
		one, two := b.settled(t, "slow", first.Value.ID, "completed"), b.settled(t, "slow", second.Value.ID, "completed")
		if parallel == "1" && *two.StartedAt < *one.FinishedAt {
			t.Fatalf("one at a time, the first sent first: %+v %+v", one, two)
		}
		return *one.StartedAt < *two.FinishedAt && *two.StartedAt < *one.FinishedAt
	}
	if overlap("1") {
		t.Fatal("queue.parallel(1) ran two runs at once")
	}
	if !overlap("") {
		t.Fatal("the default, 4 at once, ran two runs one after the other")
	}
}

func TestARunningRunIsCancelled(t *testing.T) {
	b := queueSite(t, queueApp(map[string]string{
		"slow.work": queueImport + `export const slow = queue.job((context) => {
  context.job.log("bắt đầu");
  let total = 0;
  for (let i = 0; i < 400; i++) {
    total = total + JSON.stringify({ text: "x".repeat(200000) }).length;
  }
  context.job.log("xong");
}).retry(3);
`,
	}), true)
	sent := b.send(t, "slow", "")
	b.settled(t, "slow", sent.Value.ID, "running")
	cancelled := b.safely(t, http.MethodPost, "/cancel?id="+sent.Value.ID)
	if cancelled.Error != nil || cancelled.Value.Status != "cancelling" {
		t.Fatalf("cancel of a running run: %+v %s", cancelled.Value, cancelled.Error)
	}
	done := b.settled(t, "slow", sent.Value.ID, "cancelled")
	if done.Attempt != 1 || !strings.Contains(done.Log, "lần thử 1: dừng vì lần chạy bị huỷ") || strings.Contains(done.Log, "xong") {
		t.Fatalf("the attempt stops, never retried: %+v", done)
	}
}

// seed writes rows into kitwork_runs of the app at root, as another process left them.
func seed(t *testing.T, root string, statements ...string) {
	t.Helper()
	opened, release, err := database.Acquire(filepath.Join(root, "apps", "shop", ".database", "application", "kitwork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := opened.Migrate(context.Background(), "test", queue.Schema...); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Write(context.Background(), "test", func(transaction *kitdb.Transaction) error {
		for _, statement := range statements {
			if _, err := transaction.Exec(statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestARunAProcessLeftComesBack(t *testing.T) {
	quickQueue(t)
	files := queueApp(map[string]string{
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {
  context.job.log("lần " + context.job.attempt, context.data());
}).retry(1);
`,
	})
	root := write(t, files)
	// a run whose lease ran out while its process ran it, one whose lease runs out a little after the boot, another of
	// a job no file declares any more
	soon := time.Now().UTC().Add(300 * time.Millisecond).Format("2006-01-02T15:04:05.000Z")
	seed(t, root,
		`INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, lease_token, lease_until, created_at, started_at)
		 VALUES ('r_soon', 3, 'queue', 'thanks', 'running', 1, 2, 0, 'not json', '2026-01-01T00:00:00.000Z', 'gone', '`+soon+`', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
		`INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, lease_token, lease_until, created_at, started_at)
		 VALUES ('r_left', 1, 'queue', 'thanks', 'running', 1, 2, 0, '{}', '2026-01-01T00:00:00.000Z', 'gone', '2026-01-01T00:00:30.000Z', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
		`INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, created_at)
		 VALUES ('r_gone', 2, 'queue', 'renamed', 'pending', 0, 3, 0, '{}', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`)
	b := boot(t, filepath.Join(root, "application.work"))
	b.host.StartBackground()
	back := b.settled(t, "thanks", "r_left", "completed")
	if back.Attempt != 2 || back.Log != "lần 2 {}\n" {
		t.Fatalf("the run left running: %+v", back)
	}
	if later := b.settled(t, "thanks", "r_soon", "completed"); later.Attempt != 2 || later.Log != "lần 2 null\n" {
		t.Fatalf("the run whose lease ran out after the boot: %+v", later)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		failed := b.runs(t, "/failed")
		if len(failed) == 1 && failed[0].ID == "r_gone" && failed[0].Attempt == 1 && failed[0].Error != nil && failed[0].Error.Code == "NOT_FOUND" &&
			strings.Contains(failed[0].Error.Message, "không có job renamed trong app") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run of a job gone: %+v", failed)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := b.safely(t, http.MethodPost, "/replay?id=r_gone"); got.Error == nil || got.Error.Code != "NOT_FOUND" || !strings.Contains(got.Error.Message, "job renamed không còn trong app") {
		t.Fatalf("replay of a job gone: %s", got.Error)
	}
}
func TestAnEndedRunIsKeptForKeep(t *testing.T) {
	quickQueue(t)
	queue.PruneEvery = 5 * time.Millisecond
	files := queueApp(map[string]string{
		"config.work": queueImport + "queue.keep(\"2h\");\n",
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
	})
	root := write(t, files)
	stamp := time.Now().UTC().Add(-90 * time.Minute).Format("2006-01-02T15:04:05.000Z")
	old := time.Now().UTC().Add(-3 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	seed(t, root,
		`INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, created_at, finished_at)
		 VALUES ('r_recent', 1, 'queue', 'thanks', 'completed', 1, 1, 0, '{}', '`+stamp+`', '`+stamp+`', '`+stamp+`')`,
		`INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, created_at, finished_at)
		 VALUES ('r_old', 2, 'queue', 'thanks', 'failed', 1, 1, 0, '{}', '`+old+`', '`+old+`', '`+old+`')`)
	b := boot(t, filepath.Join(root, "application.work"))
	b.host.StartBackground()
	deadline := time.Now().Add(10 * time.Second)
	for {
		runs := b.runs(t, "/runs/thanks")
		if len(runs) == 1 && runs[0].ID == "r_recent" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("keep(\"2h\") keeps the run of 90 minutes ago, not of 3 hours: %+v", runs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAJobIsDeclaredRightOrTheAppDoesNotBoot(t *testing.T) {
	declare := func(name, source string) map[string]string {
		return queueApp(map[string]string{name: source})
	}
	job := `queue.job((context) => {})`
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"not exported":     {declare("thanks.work", queueImport+"const thanks = "+job+";\n"), "shop/core/queue/thanks.work:2:21: queue.job không export"},
		"retry twice":      {declare("thanks.work", queueImport+"export const thanks = "+job+".retry(1).retry(2);\n"), ".retry nối hai lần"},
		"retry wrong":      {declare("thanks.work", queueImport+"export const thanks = "+job+".retry(-1);\n"), ".retry(n) nhận số lần thử thêm, số nguyên từ 0 tới 100"},
		"timeout wrong":    {declare("thanks.work", queueImport+"export const thanks = "+job+".timeout(\"abc\");\n"), `.timeout("abc"): hạn của một lần thử`},
		"timeout twice":    {declare("thanks.work", queueImport+"export const thanks = "+job+".timeout(\"1s\").timeout(\"2s\");\n"), ".timeout nối hai lần"},
		"not a function":   {declare("thanks.work", queueImport+"export const thanks = queue.job(1);\n"), "queue.job nhận một hàm"},
		"export default":   {declare("thanks.work", queueImport+"export default "+job+";\n"), "export default không có trong core/queue/"},
		"export as":        {declare("thanks.work", queueImport+"const thanks = "+job+";\nexport { thanks as cam };\n"), "export { … as cam } không có trong core/queue/"},
		"two names":        {declare("thanks.work", queueImport+"export const thanks = "+job+";\nexport const cam = thanks;\n"), "thanks export thêm dưới tên cam: một khai báo, một tên"},
		"a word not there": {declare("thanks.work", queueImport+"export const thanks = "+job+".handle(1);\n"), ".handle đã bỏ"},
	}
	twice := queueApp(map[string]string{"a.work": queueImport + "export const thanks = " + job + ";\n"})
	twice["apps/shop/core/queue/b.work"] = queueImport + "export const thanks = " + job + ";\n"
	cases["one name in two files"] = struct {
		files map[string]string
		want  string
	}{twice, "job thanks đã khai ở core/queue/a.work:2:28; tên job duy nhất trong app"}
	outside := queueApp(nil)
	outside["apps/shop/shop.test/router.work"] = routerImport + queueImport + "const thanks = " + job + ";\n"
	cases["in router.work"] = struct {
		files map[string]string
		want  string
	}{outside, "queue.job khai ở cấp file của một file .work thường của app (không phải router.work"}
	booting := queueApp(map[string]string{"thanks.work": queueImport + "export const thanks = " + job + ";\n"})
	booting["apps/shop/shop.test/router.work"] = routerImport + "import { thanks } from \"app/core/queue/thanks.work\";\nthanks.send({});\n"
	cases["send as a file is read"] = struct {
		files map[string]string
		want  string
	}{booting, "thanks.send chạy lúc một request, một lịch hay một job chạy, không lúc file được đọc"}
	inSite := queueApp(nil)
	inSite["apps/shop/shop.test/.shared/queue/thanks.work"] = queueImport + "export const thanks = " + job + ";\n"
	cases["a site's .shared/queue"] = struct {
		files map[string]string
		want  string
	}{inSite, "shop/shop.test: .shared/queue/ đã bỏ (17/11/2026)"}
	for _, config := range []struct{ name, source, want string }{
		{"parallel twice", "queue.parallel(2);\nqueue.parallel(3);\n", "queue.parallel gọi hai lần, ở core/queue/rules.work:2:6 và core/queue/rules.work:3:6"},
		{"parallel wrong", "queue.parallel(0);\n", "queue.parallel nhận một số nguyên từ 1 tới 100"},
		{"keep wrong", "queue.keep(\"10m\");\n", `queue.keep nhận một khoảng từ "1h"`},
		{"database wrong", "queue.database(\"jobs\");\n", "queue.database nhận một database của app đã import"},
	} {
		cases["config: "+config.name] = struct {
			files map[string]string
			want  string
		}{declare("config.work", queueImport+config.source), config.want}
	}
	inConfig := queueApp(nil)
	inConfig["apps/shop/shop.test/config.work"] = queueImport + "const thanks = " + job + ";\n"
	cases["a job in a site's config.work"] = struct {
		files map[string]string
		want  string
	}{inConfig, "queue.job khai ở cấp file của một file .work thường của app (không phải router.work, config.work"}
	cancelling := queueApp(nil)
	cancelling["apps/shop/shop.test/router.work"] = routerImport + queueImport + "queue.cancel(\"r_x\");\n"
	cases["cancel as a file is read"] = struct {
		files map[string]string
		want  string
	}{cancelling, "queue.cancel chạy lúc một request chạy, không lúc file được đọc"}
	library := queueApp(map[string]string{"config.work": queueImport + "queue.parallel(2);\n"})
	library["apps/shop/core/limits.work"] = queueImport + "queue.parallel(3);\nexport const x = 1;\n"
	cases["a config word in two files"] = struct {
		files map[string]string
		want  string
	}{library, "queue.parallel gọi hai lần, ở core/limits.work:2:6 và core/queue/rules.work:2:6: mỗi chữ một lần trong app"}
	elsewhere := queueApp(nil)
	elsewhere["apps/shop/shop.test/router.work"] = routerImport + queueImport + "queue.parallel(2);\nrouter.get((context) => context.text(\"a\"));\n"
	cases["config word in router.work"] = struct {
		files map[string]string
		want  string
	}{elsewhere, "queue.parallel viết ở cấp file của một file .work thường của app"}
	for name, each := range cases {
		if got := bootError(t, each.files); !strings.Contains(got, each.want) {
			t.Errorf("%s: %q, want %q", name, got, each.want)
		}
	}
}

// count reads one number of the database at path (from the folder root), a statement of a test.
func count(t *testing.T, path, query string) int {
	t.Helper()
	opened, release, err := database.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	number := 0
	if err := opened.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		rows, err := snapshot.Query(query)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return rows.Scan(&number)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return number
}

// The runs are in the database the folder's config.work chooses (docs/KITWORK_QUEUE.md mục 2, KITWORK_SCHEDULE.md mục
// 2): queue.database(jobs) puts the jobs' runs in the file of connection("jobs") of the host's catalogue, and
// schedule.database(jobs) the schedules', their state with them. The runs the app's own database still holds unended
// stay there and are said; a second app naming the same file keeps its runs in its own database, said.
func TestTheRunsAreInTheDatabaseTheFolderChooses(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	jobs := `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";

const letters = struct({ id: id(), text: string() });

export const jobs = connection("jobs").tables({ letters });
`
	choose := queueImport + "import { jobs } from \"app/core/schema/jobs.work\";\nqueue.database(jobs);\n"
	files := queueApp(map[string]string{
		"config.work": choose,
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
	})
	files["application.work"] = `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.databases({ jobs: { sqlite: "./data/jobs.db", apps: "*" } });
app.run();
`
	files["apps/shop/module.work"] = zonedModule("Asia/Ho_Chi_Minh")
	files["apps/shop/core/schema/jobs.work"] = jobs
	files["apps/shop/core/schedule/rules.work"] = scheduleImport + "import { jobs } from \"app/core/schema/jobs.work\";\nschedule.database(jobs);\n"
	files["apps/shop/core/schedule/nhip.work"] = scheduleImport + `export const nhip = schedule.every("1m").run((context) => {});
`
	files["apps/zkho/core/schema/jobs.work"] = jobs
	files["apps/zkho/core/queue/rules.work"] = choose
	files["apps/zkho/core/queue/thanks.work"] = queueImport + `export const thanks = queue.job((context) => {});
`
	files["apps/zkho/zkho.test/router.work"] = routerImport + "router.get((context) => context.text(\"kho\"));\n"
	root := write(t, files)
	// a run the app's own database holds from before the call
	seed(t, root, `INSERT INTO kitwork_runs (id, seq, kind, name, status, attempt, max_attempts, timeout_ms, data, available_at, created_at)
		 VALUES ('r_before', 1, 'queue', 'thanks', 'pending', 0, 1, 0, '{}', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`)
	b := boot(t, filepath.Join(root, "application.work"))
	log := b.log.String()
	if !strings.Contains(log, `[queue] .database/application/kitwork.db còn 1 lần chạy chưa xong của job (pending, running, cancelling): chúng ở lại đó và không chạy, vì queue.database(…) đặt các lần chạy ở connection("jobs"); đổi database không dời lần chạy`) {
		t.Fatalf("the runs left behind:\n%s", log)
	}
	if strings.Contains(log, "của lịch (pending") {
		t.Fatalf("the runs left behind are counted by kind:\n%s", log)
	}
	if !strings.Contains(log, `[zkho] [queue] core/queue/rules.work:3:6: connection("jobs") đã giữ các lần chạy của app shop`) {
		t.Fatalf("a second app on the file:\n%s", log)
	}
	b.host.StartBackground()
	log = b.log.String()
	if !strings.Contains(log, `[shop] [queue] worker chạy (kitwork_runs của connection("jobs")): 1 job: thanks`) ||
		!strings.Contains(log, `[shop] [schedule] worker chạy (kitwork_runs của connection("jobs")): 1 lịch: nhip`) ||
		!strings.Contains(log, `[zkho] [queue] worker chạy (kitwork_runs của .database/application/kitwork.db): 1 job: thanks`) {
		t.Fatalf("where each worker's runs are:\n%s", log)
	}
	sent := b.send(t, "thanks", "")
	b.settled(t, "thanks", sent.Value.ID, "completed")
	clock.set("2026-11-08T10:01:05+07:00")
	jobsFile := filepath.Join(root, "data", "jobs.db")
	own := filepath.Join(root, "apps", "shop", ".database", "application", "kitwork.db")
	deadline := time.Now().Add(10 * time.Second)
	for count(t, jobsFile, `SELECT count(*) FROM kitwork_runs WHERE kind = 'schedule' AND status = 'completed'`) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("the schedule's run in jobs.db\n%s", b.log)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := count(t, jobsFile, `SELECT count(*) FROM kitwork_runs WHERE kind = 'queue' AND status = 'completed'`); got != 1 {
		t.Fatalf("the job's run in jobs.db: %d", got)
	}
	if got := count(t, jobsFile, `SELECT count(*) FROM kitwork_schedules WHERE name = 'nhip'`); got != 1 {
		t.Fatalf("the schedule's state in jobs.db: %d", got)
	}
	if got := count(t, own, `SELECT count(*) FROM kitwork_runs`); got != 1 {
		t.Fatalf("the app's own database keeps what it had, and nothing more: %d", got)
	}
}

// An app that does not boot lets go of the file of the catalogue its config.work chose, and the count of the runs left
// behind reads the app's own database without making it, nor a table in it.
func TestAClosedAppLetsGoOfItsDatabase(t *testing.T) {
	jobs := `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";

const letters = struct({ id: id(), text: string() });

export const jobs = connection("jobs").tables({ letters });
`
	choose := queueImport + "import { jobs } from \"app/core/schema/jobs.work\";\nqueue.database(jobs);\n"
	job := queueImport + `export const thanks = queue.job((context) => {});
`
	files := map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.databases({ jobs: { sqlite: "./data/jobs.db", apps: "*" } });
app.run();
`,
		"apps/abroken/core/schema/jobs.work":    jobs,
		"apps/abroken/core/queue/rules.work":    choose,
		"apps/abroken/core/queue/thanks.work":   job,
		"apps/abroken/abroken.test/router.work": routerImport + "router.get(1);\n",
		"apps/bshop/core/schema/jobs.work":      jobs,
		"apps/bshop/core/queue/rules.work":      choose,
		"apps/bshop/core/queue/thanks.work":     job,
		"apps/bshop/bshop.test/router.work":     routerImport + "router.get((context) => context.text(\"shop\"));\n",
		"apps/cshop/core/schema/jobs.work":      jobs,
		"apps/cshop/core/queue/thanks.work":     job,
		"apps/cshop/cshop.test/router.work":     routerImport + "router.get((context) => context.text(\"shop\"));\n",
	}
	root := write(t, files)
	// bshop's own database is there, without kitwork_runs
	other := filepath.Join(root, "apps", "bshop", ".database", "application", "kitwork.db")
	opened, release, err := database.Acquire(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Migrate(context.Background(), "test", "CREATE TABLE notes (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	release()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(root, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	if read.Failed() == nil || strings.Contains(log.String(), "đã giữ các lần chạy") || strings.Contains(log.String(), "không đọc được") {
		t.Fatalf("abroken closed, bshop takes the file, its own database read as it is:\n%s", log)
	}
	if got := count(t, other, `SELECT count(*) FROM sqlite_master WHERE name = 'kitwork_runs'`); got != 0 {
		t.Fatalf("the count of the runs left behind made a table: %d", got)
	}
	if _, err := os.Stat(filepath.Join(root, "apps", "abroken", ".database", "application", "kitwork.db")); err == nil {
		t.Fatal("the count of the runs left behind made the app's own database")
	}
	read.StartBackground()
	if got := log.String(); !strings.Contains(got, `[bshop] [queue] worker chạy (kitwork_runs của connection("jobs"))`) || strings.Contains(got, "[abroken] [queue] worker") {
		t.Fatalf("the workers:\n%s", got)
	}
}

func TestTheAppsOwnDatabaseIsTheDefaultSaidAgain(t *testing.T) {
	files := queueApp(map[string]string{
		"config.work": queueImport + "import { db } from \"app/core/schema/shop.work\";\nqueue.database(db);\n",
		"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`,
	})
	files["apps/shop/core/schema/shop.work"] = `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";

const notes = struct({ id: id(), text: string() });

export const db = connection().tables({ notes });
`
	b := queueSite(t, files, true)
	b.settled(t, "thanks", b.send(t, "thanks", "").Value.ID, "completed")
	if !strings.Contains(b.log.String(), "[queue] worker chạy (kitwork_runs của .database/application/kitwork.db): 1 job: thanks") {
		t.Fatalf("connection() is the app's own:\n%s", b.log)
	}
}

// Serve starts what the apps run in the background, with the sites; Close stops it.
func TestServeStartsTheBackground(t *testing.T) {
	port := freePort(t)
	files := queueApp(map[string]string{"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`})
	files["application.work"] = strings.Replace(files["application.work"], "port: 8080", "port: "+strconv.Itoa(port), 1)
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, files), "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	served := make(chan error, 1)
	go func() { served <- read.Serve() }()
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(log.String(), "[queue] worker chạy"); {
		select {
		case err := <-served:
			t.Fatalf("Serve ended: %v\n%s", err, log)
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("Serve started no worker:\n%s", log)
		}
	}
}

// An app with no site runs its schedules all the same: a clock is no site's.
func TestAnAppWithNoSiteRunsItsSchedules(t *testing.T) {
	quickQueue(t)
	clock := handClock(t, "2026-11-08T10:00:30+07:00")
	files := queueApp(nil)
	files["apps/clock/module.work"] = zonedModule("Asia/Ho_Chi_Minh")
	files["apps/clock/core/schedule/nhip.work"] = scheduleImport + `export const nhip = schedule.every("1m").run((context) => {});
`
	root := write(t, files)
	b := boot(t, filepath.Join(root, "application.work"))
	b.host.StartBackground()
	clock.set("2026-11-08T10:01:05+07:00")
	path := filepath.Join(root, "apps", "clock", ".database", "application", "kitwork.db")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil && count(t, path, `SELECT count(*) FROM kitwork_runs WHERE kind = 'schedule' AND status = 'completed'`) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the schedule of an app with no site:\n%s", b.log)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheRunsFileIsAFolderOfTheApp(t *testing.T) {
	files := queueApp(map[string]string{"thanks.work": queueImport + `export const thanks = queue.job((context) => {});
`})
	files["apps/shop/.database/application"] = "not a folder"
	b := queueSite(t, files, false)
	if got := b.send(t, "thanks", ""); got.Error == nil || got.Error.Code != "DATABASE" || !strings.Contains(got.Error.Message, ".database/application of the app is a link, not a folder") {
		t.Fatalf("a send: %s", got.Error)
	}
}
