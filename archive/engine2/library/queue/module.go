package queue

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/module"
)

// kitwork/queue (docs/KITWORK_QUEUE.md). A job is declared once, in a file of the app's .shared/queue/, and exported;
// whoever needs it imports the handle and sends through it:
//
//	// app/.shared/queue/camOn.work
//	import * as queue from "kitwork/queue";
//	export const camOn = queue.job((context) => {
//	  const { email, orderId } = context.data();
//	  sendOrderEmail(email, orderId);
//	}).retry(3).timeout("30s");
//
//	// a route
//	import { camOn } from "app/.shared/queue/camOn.work";
//	camOn.send({ email, orderId }, { delay: "5m", key: "order:" + orderId });   // { id, name: "camOn", status: "pending" }
//
// queue.job only declares: every file that imports camOn.work runs it again, so evaluating it registers nothing and
// runs nothing. Only the host's pass over .shared/queue/ (Loading) keeps what each file exports as the jobs the worker
// runs. send stores the run in kitwork_runs of the app's database before it answers; the worker claims it with a lease.

// The bounds of the reads and the rules.
const (
	// runsDefault and runsMaximum bound runs() and list(): how many when the call does not say, the most one reads.
	runsDefault = 50
	runsMaximum = 1000
	// timeoutMaximum is the longest .timeout() of an attempt.
	timeoutMaximum = 24 * time.Hour
)

// Module is kitwork/queue as one file imports it.
type Module struct {
	App *App
	// Place is where the call in progress is written, ".shared/queue/camOn.work:10:22" (from the app's folder);
	// Written the file writing it, "" at a request.
	Place   func() string
	Written func() string
	// Open says the file still runs as the app boots: a job is declared then, nothing is sent.
	Open func() bool
	// Loading is the host's pass over a file of .shared/queue/: queue.job declares a job of the app then.
	Loading bool
}

// Freeze: a Module is never written once made.
func (m *Module) Freeze() {}

func (m *Module) open() bool { return m.Open != nil && m.Open() }

func (m *Module) place() string {
	if m.Place == nil {
		return ""
	}
	return m.Place()
}

func (m *Module) written() string {
	if m.Written == nil {
		return ""
	}
	return m.Written()
}

// MissingMember is a word kitwork/queue does not have, or that the spec replaced.
func (m *Module) MissingMember(name string) value.Value {
	if message, removed := removedWords[name]; removed {
		return value.Fail(value.InvalidArgumentCode, message)
	}
	return value.Fail(value.TypeCode, "queue."+name+" không có: kitwork/queue có job, list, runs, status, where, cancel, replay; luật chung của hàng đợi là queue.parallel, queue.keep, queue.database, mỗi chữ một lần trong app")
}

// removedWords are the words of the module the spec replaced (docs/KITWORK_QUEUE.md mục 14, 16).
var removedWords = map[string]string{
	"handle":   `queue.handle đã bỏ: khai job bằng export const camOn = queue.job((context) => { … }) ở cấp file của một file .work của app`,
	"dispatch": `queue.dispatch đã bỏ: gửi việc qua handle, camOn.send(data)`,
	"send":     `queue.send("tên", …) không có: gửi việc qua handle đã import, camOn.send(data)`,
	"jobs":     `queue.jobs() không có: các job là queue.list(), các lần chạy là queue.runs()`,
	"failed":   `queue.failed() đã bỏ: queue.status("failed").runs()`,
	"resend":   `queue.resend không có: send(data) tạo việc từ dữ liệu người gọi đưa, replay(id) tạo việc từ lịch sử`,
	"retry":    `queue.retry(id) không có: .retry(n) là luật của job (queue.job(fn).retry(3)); chạy lại một lần đã xong là queue.replay(id)`,
}

// Job is queue.job(handler): the handle, before its rules. A job of the loader's pass is kept by the app; any other
// evaluation is a handle that sends, nothing more.
func (m *Module) Job(arguments ...value.Value) value.Value {
	if !m.open() || module.RoleFile(m.written()) {
		return value.Fail(value.InvalidArgumentCode, "queue.job khai ở cấp file của một file .work thường của app (không phải router.work, config.work, design.work, module.work), lúc app boot: export const camOn = queue.job((context) => { … }); file khác import { camOn } rồi camOn.send(data)")
	}
	if len(arguments) != 1 || arguments[0].Lambda() == nil {
		return value.Fail(value.InvalidArgumentCode, "queue.job nhận một hàm: queue.job((context) => { … })")
	}
	job := &Job{app: m.App, place: m.place(), function: arguments[0].Lambda(), open: m.Open}
	if m.Loading && m.App != nil {
		m.App.declare(job)
	}
	return value.Struct(job)
}

// List is queue.list(): the jobs the app declares, by name, whether anything was sent or not.
func (m *Module) List(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "queue"}).List(arguments...)
}

// Runs is queue.runs(): the runs of every job, newest first.
func (m *Module) Runs(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "queue"}).Runs(arguments...)
}

// Status is queue.status(s): the runs in one status, read by .runs().
func (m *Module) Status(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "queue"}).Status(arguments...)
}

// Where is queue.where(fn), not yet.
func (m *Module) Where(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "queue"}).Where(arguments...)
}

// Cancel is queue.cancel(id).
func (m *Module) Cancel(arguments ...value.Value) value.Value {
	return cancel(m.App, "queue", "", m.open(), arguments)
}

// Replay is queue.replay(id).
func (m *Module) Replay(arguments ...value.Value) value.Value {
	return replay(m.App, "queue", "", m.open(), arguments)
}

// configWord is a rule of the whole queue, queue.parallel, keep, database: written at the top level of a plain .work
// file of the app, once in it (17/11/2026; only in .shared/queue/config.work until then). The host's pass that loads
// the app's work keeps it (Loading); any other run of the file, one that imports it among them, passes over it.
func (m *Module) configWord(word string, arguments []value.Value, set func() value.Value) value.Value {
	if !m.open() || module.RoleFile(m.written()) {
		return value.Fail(value.InvalidArgumentCode, "queue."+word+" viết ở cấp file của một file .work thường của app, một lần trong app: queue.parallel(4); queue.keep(\"30d\")")
	}
	if !m.Loading || m.App == nil {
		return value.Undefined()
	}
	var failure value.Value
	if err := m.App.configure(word, m.place(), func() { failure = set() }); err != nil {
		return value.Fail(value.InvalidArgumentCode, err.Error())
	}
	return failure
}

// Parallel is queue.parallel(n): how many runs one machine runs at once, 1 to 100.
func (m *Module) Parallel(arguments ...value.Value) value.Value {
	return m.configWord("parallel", arguments, func() value.Value {
		if len(arguments) != 1 || !arguments[0].IsNumber() || arguments[0].Number() < 1 || arguments[0].Number() > 100 || arguments[0].Number() != math.Trunc(arguments[0].Number()) {
			return value.Fail(value.InvalidArgumentCode, "queue.parallel nhận một số nguyên từ 1 tới 100, số lần chạy cùng lúc trên một máy: queue.parallel(4)")
		}
		m.App.parallel = int(arguments[0].Number())
		return value.Undefined()
	})
}

// Keep is queue.keep(duration): how long an ended run is kept, from "1h".
func (m *Module) Keep(arguments ...value.Value) value.Value {
	return m.configWord("keep", arguments, func() value.Value {
		keep, ok := time.Duration(0), false
		if len(arguments) == 1 && arguments[0].IsString() {
			keep, ok = ParseDuration(arguments[0].Text())
		}
		if !ok || keep < time.Hour {
			return value.Fail(value.InvalidArgumentCode, `queue.keep nhận một khoảng từ "1h", thời gian giữ lần chạy đã xong: queue.keep("30d")`)
		}
		m.App.keep = keep
		return value.Undefined()
	})
}

// Database is queue.database(db): the database the runs of the jobs are in, one the app's .shared/schema/ declares,
// connection() (the app's own, the default) or connection("tên") of the host's catalogue (mục 2).
func (m *Module) Database(arguments ...value.Value) value.Value {
	return m.configWord("database", arguments, func() value.Value {
		path, name, ok := "", "", false
		if len(arguments) == 1 && m.App.Locate != nil {
			path, name, ok = m.App.Locate(arguments[0])
		}
		if !ok {
			return value.Fail(value.InvalidArgumentCode, `queue.database nhận một database của app đã import, connection(…).tables({ … }) khai ở cấp file của một file .work: import { jobs } from "app/core/jobs.work"; queue.database(jobs), jobs là connection("jobs").tables({ … }) của một tên host khai trong app.databases`)
		}
		m.App.file, m.App.place = Place(m.App.root, m.App.open, KindQueue, path, name, m.place(), m.App.say)
		m.App.store = NewStore(m.App.file, KindQueue)
		return value.Undefined()
	})
}

// Job is the handle queue.job(fn) gives: the function, the rules chained after it, where it is declared, which tells
// its name (the app's, from the loader's pass).
type Job struct {
	app      *App
	place    string
	function *value.Lambda
	open     func() bool

	retry       int
	retrySet    bool
	timeout     time.Duration
	timeoutText string
	timeoutSet  bool
}

// Freeze: the rules are chained as the file runs, and the handle is read only after.
func (j *Job) Freeze() {}

func (j *Job) running() bool { return j.open != nil && j.open() }

// declared is the job as the app keeps it, a failure when no file of the app exports it.
func (j *Job) declared() (*Handler, value.Value) {
	if handler := j.app.jobAt(j.place); handler != nil {
		return handler, value.Value{}
	}
	return nil, value.Fail(value.InvalidArgumentCode, `job chưa có tên: queue.job khai ở cấp file của một file .work của app và export, export const camOn = queue.job(fn); file khác import { camOn } from "app/core/camOn.work" rồi camOn.send(data)`)
}

// MissingMember is a word a handle does not have.
func (j *Job) MissingMember(name string) value.Value {
	switch name {
	case "handle", "success", "error":
		return value.Fail(value.InvalidArgumentCode, "."+name+" đã bỏ: queue.job(fn) nối .retry(n), .timeout(hạn); việc sau khi xong viết ở cuối hàm; các lần chạy thất bại đọc bằng camOn.status(\"failed\").runs()")
	case "list":
		return value.Fail(value.InvalidArgumentCode, ".list() không có trên job: các job là queue.list(), các lần chạy của job là camOn.runs()")
	}
	return value.Fail(value.TypeCode, "."+name+" không có trên job: queue.job(fn) nối .retry(n), .timeout(hạn); handle có send, runs, status, where, cancel, replay")
}

// Retry is .retry(n): n attempts more after the first, .retry(3) four in all.
func (j *Job) Retry(arguments ...value.Value) value.Value {
	if !j.running() {
		return value.Fail(value.InvalidArgumentCode, ".retry(n) là luật khai cùng job, ở cấp file: queue.job(fn).retry(3)")
	}
	if j.retrySet {
		return value.Fail(value.InvalidArgumentCode, ".retry nối hai lần: một job một luật, .retry(3)")
	}
	j.retrySet = true
	if len(arguments) != 1 || !arguments[0].IsNumber() || arguments[0].Number() < 0 || arguments[0].Number() > 100 || arguments[0].Number() != math.Trunc(arguments[0].Number()) {
		return value.Fail(value.InvalidArgumentCode, ".retry(n) nhận số lần thử thêm, số nguyên từ 0 tới 100: .retry(3) là bốn lần tổng cộng")
	}
	j.retry = int(arguments[0].Number())
	return value.Struct(j)
}

// Timeout is .timeout(duration): the longest one attempt may run, "30s", "5m".
func (j *Job) Timeout(arguments ...value.Value) value.Value {
	if !j.running() {
		return value.Fail(value.InvalidArgumentCode, `.timeout(hạn) là luật khai cùng job, ở cấp file: queue.job(fn).timeout("30s")`)
	}
	if j.timeoutSet {
		return value.Fail(value.InvalidArgumentCode, `.timeout nối hai lần: một job một luật, .timeout("30s")`)
	}
	j.timeoutSet = true
	if len(arguments) != 1 || !arguments[0].IsString() {
		return value.Fail(value.InvalidArgumentCode, `.timeout(hạn) nhận một khoảng viết bằng chuỗi: .timeout("30s"), "5m", "1h"`)
	}
	text := strings.TrimSpace(arguments[0].Text())
	timeout, ok := ParseDuration(text)
	if !ok || timeout < time.Millisecond || timeout > timeoutMaximum {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`.timeout(%q): hạn của một lần thử là một khoảng từ 1ms tới 24h: "30s", "5m", "1h"`, text))
	}
	j.timeout, j.timeoutText = timeout, text
	return value.Struct(j)
}

// Send is .send(data, { delay, key }): one run of the job, stored before the answer; the function never runs in the
// caller's call (mục 4).
func (j *Job) Send(arguments ...value.Value) value.Value {
	handler, failure := j.declared()
	if failure.IsInvalid() {
		return failure
	}
	name := handler.Name
	if j.running() {
		return value.Fail(value.InvalidArgumentCode, name+".send chạy lúc một request, một lịch hay một job chạy, không lúc file được đọc: file chỉ khai")
	}
	if len(arguments) > 2 {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s.send nhận dữ liệu và một object lựa chọn: %s.send(data, { delay, key })", name, name))
	}
	data := "null"
	if len(arguments) > 0 {
		encoded, failure := encodeData(arguments[0])
		if failure.IsInvalid() {
			if failure.Code() == value.TooLargeCode {
				return value.Fail(value.TooLargeCode, name+".send: "+failure.Headline())
			}
			return value.Fail(value.InvalidArgumentCode, name+".send: "+failure.Headline())
		}
		data = encoded
	}
	if len(data) > DataLimit {
		return value.Fail(value.TooLargeCode, fmt.Sprintf("%s.send: dữ liệu %d byte, quá trần %d KiB của một lần chạy; lưu phần lớn vào database rồi gửi id", name, len(data), DataLimit>>10))
	}
	var delay time.Duration
	key := ""
	if len(arguments) > 1 {
		var failure value.Value
		if delay, key, failure = sendOptions(name, arguments[1]); failure.IsInvalid() {
			return failure
		}
	}
	now := time.Now()
	run, duplicate, err := j.app.store.Enqueue(NewRun{Name: name, Data: data, Key: key, MaxAttempts: handler.Retry + 1,
		Timeout: handler.Timeout, Available: now.Add(delay)}, now)
	if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s.send: không lưu được lần chạy: %v", name, err))
	}
	if worker := j.app.running(); worker != nil {
		worker.Due(now.Add(delay))
	}
	receipt := value.Object()
	receipt.Set("id", value.String(run.ID))
	receipt.Set("name", value.String(run.Name))
	receipt.Set("status", value.String(run.Status))
	if duplicate {
		receipt.Set("duplicate", value.Boolean(true))
	}
	return receipt
}

// sendOptions reads send's second argument, { delay, key } and nothing else.
func sendOptions(name string, options value.Value) (time.Duration, string, value.Value) {
	usage := fmt.Sprintf(`%s.send(data, { delay: "5m", key: "order:123" })`, name)
	if options.IsNullish() {
		return 0, "", value.Value{}
	}
	if !options.IsObject() {
		return 0, "", value.Fail(value.InvalidArgumentCode, "lựa chọn của send là một object: "+usage)
	}
	var delay time.Duration
	key := ""
	for _, option := range options.Keys() {
		item := options.Get(option)
		switch option {
		case "delay":
			parsed, ok := time.Duration(0), false
			if item.IsString() {
				parsed, ok = ParseDuration(item.Text())
			}
			if !ok || parsed <= 0 {
				return 0, "", value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`delay %s không đọc được: một khoảng viết bằng chuỗi như "30s", "5m", "1h": %s`, text(item), usage))
			}
			delay = parsed
		case "key":
			if !item.IsString() || strings.TrimSpace(item.Text()) == "" {
				return 0, "", value.Fail(value.InvalidArgumentCode, "key là một chuỗi không rỗng: "+usage)
			}
			key = item.Text()
		default:
			return 0, "", value.Fail(value.InvalidArgumentCode, fmt.Sprintf("send không có lựa chọn %s: chỉ delay và key, %s", option, usage))
		}
	}
	return delay, key, value.Value{}
}

// Runs is camOn.runs(): the job's runs, newest first.
func (j *Job) Runs(arguments ...value.Value) value.Value {
	return j.query(func(q *Query) value.Value { return q.Runs(arguments...) })
}

// Status is camOn.status(s).
func (j *Job) Status(arguments ...value.Value) value.Value {
	return j.query(func(q *Query) value.Value { return q.Status(arguments...) })
}

// Where is camOn.where(fn), not yet.
func (j *Job) Where(arguments ...value.Value) value.Value {
	return j.query(func(q *Query) value.Value { return q.Where(arguments...) })
}

func (j *Job) query(read func(*Query) value.Value) value.Value {
	handler, failure := j.declared()
	if failure.IsInvalid() {
		return failure
	}
	return read(&Query{app: j.app, who: handler.Name, job: handler.Name})
}

// Cancel is camOn.cancel(id): a run of this job only.
func (j *Job) Cancel(arguments ...value.Value) value.Value {
	handler, failure := j.declared()
	if failure.IsInvalid() {
		return failure
	}
	return cancel(j.app, handler.Name, handler.Name, j.running(), arguments)
}

// Replay is camOn.replay(id): a run of this job only.
func (j *Job) Replay(arguments ...value.Value) value.Value {
	handler, failure := j.declared()
	if failure.IsInvalid() {
		return failure
	}
	return replay(j.app, handler.Name, handler.Name, j.running(), arguments)
}

// Query is a read described before its action: queue.status("failed").runs(10), camOn.runs().
type Query struct {
	app *App
	// who is how the read is written, queue or the job's name; job the job it is limited to ("" all)
	who    string
	job    string
	status string
}

// Freeze: a Query is never written once made.
func (q *Query) Freeze() {}

// MissingMember is a word a read does not have.
func (q *Query) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "."+name+" không có ở đây: câu đọc viết .status(…) rồi .runs(…)")
}

// Status chooses the runs in one status.
func (q *Query) Status(arguments ...value.Value) value.Value {
	if q.status != "" {
		return value.Fail(value.InvalidArgumentCode, q.who+".status(…) viết hai lần: một trạng thái mỗi câu đọc")
	}
	known := false
	if len(arguments) == 1 && arguments[0].IsString() {
		for _, status := range Statuses {
			known = known || arguments[0].Text() == status
		}
	}
	if !known {
		return value.Fail(value.InvalidArgumentCode, q.who+".status nhận một trạng thái của lần chạy: "+strings.Join(Statuses, ", "))
	}
	next := *q
	next.status = arguments[0].Text()
	return value.Struct(&next)
}

// Where is where(fn): a condition run where the runs are kept, which engine2 has not yet.
func (q *Query) Where(...value.Value) value.Value {
	return value.Fail(value.UnsupportedCode, q.who+`.where(fn) chưa có: điều kiện của where chạy ở tầng lưu trữ, không lọc trong bộ nhớ (KITWORK_QUEUE.md mục 8); lọc theo trạng thái là .status("failed")`)
}

// Runs reads the runs, newest first.
func (q *Query) Runs(arguments ...value.Value) value.Value {
	limit, offset, failure := Page("runs", arguments)
	if failure.IsInvalid() {
		return failure
	}
	if q.app == nil {
		return value.Array(nil)
	}
	runs, err := q.app.store.Runs(Filter{Name: q.job, Status: q.status}, limit, offset)
	if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s.runs: %v", q.who, err))
	}
	out := make([]value.Value, len(runs))
	for index, run := range runs {
		out[index] = runRecord(run)
	}
	return value.Array(out)
}

// List reads the jobs the app declares, by name: name, retry, timeout, source.
func (q *Query) List(arguments ...value.Value) value.Value {
	if q.job != "" {
		return value.Fail(value.InvalidArgumentCode, ".list() không có trên job: các job là queue.list(), các lần chạy của job là "+q.who+".runs()")
	}
	if q.status != "" {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`queue.status("%s").list() không có: trạng thái chỉ có ở lần chạy, viết queue.status("%s").runs()`, q.status, q.status))
	}
	limit, offset, failure := Page("list", arguments)
	if failure.IsInvalid() {
		return failure
	}
	var handlers []*Handler
	if q.app != nil {
		handlers = q.app.Handlers()
	}
	handlers = handlers[min(offset, len(handlers)):]
	handlers = handlers[:min(limit, len(handlers))]
	out := make([]value.Value, len(handlers))
	for index, handler := range handlers {
		item := value.Object()
		item.Set("name", value.String(handler.Name))
		item.Set("retry", value.Number(float64(handler.Retry)))
		item.Set("timeout", nullableText(handler.TimeoutText))
		item.Set("source", value.String(handler.Source))
		out[index] = item
	}
	return value.Array(out)
}

// Page reads the argument of list() and runs(): nothing, a number (how many), or { page, limit } with pages from 1.
func Page(word string, arguments []value.Value) (limit, offset int, failure value.Value) {
	usage := fmt.Sprintf("%s() nhận không gì, một số (bao nhiêu), hay { page, limit }: %s(10), %s({ page: 2, limit: 20 })", word, word, word)
	whole := func(item value.Value) bool {
		return item.IsNumber() && item.Number() >= 1 && item.Number() == math.Trunc(item.Number())
	}
	switch {
	case len(arguments) == 0:
		return runsDefault, 0, value.Value{}
	case len(arguments) > 1:
		return 0, 0, value.Fail(value.InvalidArgumentCode, usage)
	case arguments[0].IsNumber():
		if !whole(arguments[0]) || arguments[0].Number() > runsMaximum {
			return 0, 0, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s(n): n là số nguyên từ 1 tới %d", word, runsMaximum))
		}
		return int(arguments[0].Number()), 0, value.Value{}
	case arguments[0].IsObject():
		limit, page := runsDefault, 1
		for _, key := range arguments[0].Keys() {
			item := arguments[0].Get(key)
			switch key {
			case "limit":
				if !whole(item) || item.Number() > runsMaximum {
					return 0, 0, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("limit là số nguyên từ 1 tới %d", runsMaximum))
				}
				limit = int(item.Number())
			case "page":
				if !whole(item) || item.Number() > 1_000_000 {
					return 0, 0, value.Fail(value.InvalidArgumentCode, "page là số nguyên từ 1")
				}
				page = int(item.Number())
			case "where":
				return 0, 0, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s({ where }) không có: điều kiện viết trước hành động, .where(fn).%s()", word, word))
			default:
				return 0, 0, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s không nhận %s: %s", word, key, usage))
			}
		}
		return limit, (page - 1) * limit, value.Value{}
	}
	return 0, 0, value.Fail(value.InvalidArgumentCode, usage)
}

// nullableText is s, null for "".
func nullableText(s string) value.Value {
	if s == "" {
		return value.Null()
	}
	return value.String(s)
}

// runRecord is a run as runs() gives it.
func runRecord(run Run) value.Value {
	record := value.Object()
	record.Set("id", value.String(run.ID))
	record.Set("name", value.String(run.Name))
	record.Set("status", value.String(run.Status))
	record.Set("attempt", value.Number(float64(run.Attempt)))
	record.Set("maxAttempts", value.Number(float64(run.MaxAttempts)))
	record.Set("data", decodeData(run.Data))
	record.Set("key", nullableText(run.Key))
	record.Set("log", value.String(run.Log))
	record.Set("error", RunError(run))
	record.Set("createdAt", value.String(run.CreatedAt))
	record.Set("startedAt", nullableText(run.StartedAt))
	record.Set("finishedAt", nullableText(run.FinishedAt))
	record.Set("nextRunAt", NextRunAt(run))
	record.Set("replayOf", nullableText(run.ReplayOf))
	return record
}

// RunError is a run's error as a read gives it: { code, message }, null when it has none.
func RunError(run Run) value.Value {
	if run.ErrorCode == "" && run.ErrorMessage == "" {
		return value.Null()
	}
	failure := value.Object()
	failure.Set("code", value.String(run.ErrorCode))
	failure.Set("message", value.String(run.ErrorMessage))
	return failure
}

// NextRunAt is when a pending run is due, null for a run in any other status.
func NextRunAt(run Run) value.Value {
	if run.Status != Pending {
		return value.Null()
	}
	return value.String(run.AvailableAt)
}

// runID reads the id cancel and replay take: the bare id of a run, never a job's name.
func runID(word string, arguments []value.Value) (string, value.Value) {
	if len(arguments) != 1 || !arguments[0].IsString() || !strings.HasPrefix(arguments[0].Text(), "r_") {
		return "", value.Fail(value.InvalidArgumentCode, word+` nhận id của một lần chạy ("r_…", từ biên nhận của send hay từ runs()), không phải tên job`)
	}
	return arguments[0].Text(), value.Value{}
}

// owner is who has the runs a cancel or a replay looks in: the app, or the job.
func owner(who string) string {
	if who == "queue" {
		return "app"
	}
	return who
}

// cancel is cancel(id) of the queue (job "") or of a job (mục 9).
func cancel(app *App, who, job string, booting bool, arguments []value.Value) value.Value {
	word := who + ".cancel"
	id, failure := runID(word, arguments)
	if failure.IsInvalid() {
		return failure
	}
	if booting || app == nil {
		return value.Fail(value.InvalidArgumentCode, word+" chạy lúc một request chạy, không lúc file được đọc")
	}
	run, err := app.store.Cancel(id, job, time.Now())
	var state *StateError
	switch {
	case errors.Is(err, ErrNotFound):
		return value.Fail(value.NotFoundCode, fmt.Sprintf("%s(%q): %s không có lần chạy này", word, id, owner(who)))
	case errors.As(err, &state):
		return value.Fail(value.ConflictCode, fmt.Sprintf("%s(%q): lần chạy đã %s, không còn gì để huỷ", word, id, state.Run.Status))
	case err != nil:
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s(%q): %v", word, id, err))
	}
	if worker := app.running(); worker != nil && run.Status == Cancelling {
		worker.StopAttempt(run.ID)
	}
	receipt := value.Object()
	receipt.Set("id", value.String(run.ID))
	receipt.Set("name", value.String(run.Name))
	receipt.Set("status", value.String(run.Status))
	return receipt
}

// replay is replay(id) of the queue (job "") or of a job: a new run from an ended one's data (mục 9).
func replay(app *App, who, job string, booting bool, arguments []value.Value) value.Value {
	word := who + ".replay"
	id, failure := runID(word, arguments)
	if failure.IsInvalid() {
		if len(arguments) == 1 && arguments[0].IsString() && !strings.HasPrefix(arguments[0].Text(), "r_") {
			return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`%s(%q): replay nhận id của một lần chạy và tạo một lần chạy mới; chạy lại mọi lần thất bại của một job là đọc %s.status("failed").runs() rồi replay(id) từng lần`, word, arguments[0].Text(), arguments[0].Text()))
		}
		return failure
	}
	if booting || app == nil {
		return value.Fail(value.InvalidArgumentCode, word+" chạy lúc một request chạy, không lúc file được đọc")
	}
	declared := func(name string) (NewRun, bool) {
		handler := app.handler(name)
		if handler == nil {
			return NewRun{}, false
		}
		return NewRun{MaxAttempts: handler.Retry + 1, Timeout: handler.Timeout}, true
	}
	now := time.Now()
	run, of, err := app.store.Replay(id, job, now, declared)
	var state *StateError
	switch {
	case errors.Is(err, ErrNotFound):
		return value.Fail(value.NotFoundCode, fmt.Sprintf("%s(%q): %s không có lần chạy này (chưa từng gửi, hay đã dọn theo keep)", word, id, owner(who)))
	case errors.Is(err, ErrNoDefinition):
		return value.Fail(value.NotFoundCode, fmt.Sprintf("%s(%q): job %s không còn trong app, không chạy lại được", word, id, of))
	case errors.As(err, &state):
		return value.Fail(value.ConflictCode, fmt.Sprintf("%s(%q): lần chạy đang %s; chỉ chạy lại lần chạy đã xong (failed, cancelled, completed)", word, id, state.Run.Status))
	case err != nil:
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s(%q): %v", word, id, err))
	}
	if worker := app.running(); worker != nil {
		worker.Due(now)
	}
	receipt := value.Object()
	receipt.Set("id", value.String(run.ID))
	receipt.Set("name", value.String(run.Name))
	receipt.Set("status", value.String(run.Status))
	receipt.Set("replayOf", value.String(run.ReplayOf))
	return receipt
}

// Context is the context a job's function gets (mục 5): context.data(), the data sent, and context.job, the run and
// this attempt.
type Context struct {
	claim Claim
	log   *Log
}

// jobContext is the context of a claimed run of a job.
func jobContext(claim Claim, log *Log) value.Value {
	return value.Struct(&Context{claim: claim, log: log})
}

// Freeze: a Context is never written.
func (c *Context) Freeze() {}

// Data is context.data(): the data sent, a value of its own each call.
func (c *Context) Data(...value.Value) value.Value { return decodeData(c.claim.Data) }

// Job is context.job: id, name, attempt (from 1), maxAttempts, isFinalAttempt, createdAt, startedAt, and log(…).
func (c *Context) Job() value.Value {
	job := value.Object()
	job.Set("id", value.String(c.claim.ID))
	job.Set("name", value.String(c.claim.Name))
	job.Set("attempt", value.Number(float64(c.claim.Attempt)))
	job.Set("maxAttempts", value.Number(float64(c.claim.MaxAttempts)))
	job.Set("isFinalAttempt", value.Boolean(c.claim.Attempt >= c.claim.MaxAttempts))
	job.Set("createdAt", value.String(c.claim.CreatedAt))
	job.Set("startedAt", value.String(c.claim.StartedAt))
	job.Set("log", c.log.Function())
	return job
}

// MissingMember names the word of the handler's old argument each now is (job.payload, job.log, job.attempt…).
func (c *Context) MissingMember(name string) value.Value {
	switch name {
	case "payload":
		return value.Fail(value.InvalidArgumentCode, "context.payload đã bỏ: dữ liệu đã gửi là context.data()")
	case "log", "attempt", "maxAttempts", "isFinalAttempt", "id", "name":
		return value.Fail(value.InvalidArgumentCode, "context."+name+" không có: context.job."+name)
	}
	return value.Fail(value.TypeCode, "context."+name+" không có: context của job có data() và job")
}
