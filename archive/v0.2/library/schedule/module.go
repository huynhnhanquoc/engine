package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/module"
	"github.com/kitwork/engine2/library/queue"
)

// kitwork/schedule (docs/KITWORK_SCHEDULE.md). A schedule is declared once, in a file of the app's .schedule/, and
// exported; its name is its export:
//
//	// app/.schedule/baoCao.work
//	import * as schedule from "kitwork/schedule";
//	export const baoCaoSang = schedule.daily("09:00").retry(3).timeout("10m").run((context) => {
//	  guiBaoCao(context.job.at);
//	});
//
// schedule.<rule>(…) only declares, as queue.job does: every file that imports baoCao.work runs it again, so
// evaluating it registers nothing and runs nothing. The host's pass over .schedule/ keeps what each file exports as the
// schedules the app's clock runs; a route that imports the file has a handle that reads its runs and runs it now().

// Module is kitwork/schedule as one file imports it.
type Module struct {
	App *App
	// Place is where the call in progress is written (from the app's folder), Written the file writing it, "" at a
	// request; Open says the file still runs as the app boots.
	Place   func() string
	Written func() string
	Open    func() bool
	// Loading is the host's pass over a file of .schedule/: a schedule is kept by the app then.
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

// MissingMember is a word kitwork/schedule does not have, or that the spec replaced.
func (m *Module) MissingMember(name string) value.Value {
	if message, removed := RemovedWord(name); removed {
		return value.Fail(value.InvalidArgumentCode, message)
	}
	return value.Fail(value.TypeCode, UnknownWord(name))
}

// begin is schedule.<word>(…), a rule: a schedule being declared, only in a file of .schedule/ as it is read.
func (m *Module) begin(word string, arguments []value.Value) value.Value {
	if !m.open() || module.RoleFile(m.written()) {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`schedule.%s khai ở cấp file của một file .work thường của app (không phải router.work, config.work, design.work, module.work), lúc app boot: export const baoCaoSang = schedule.daily("09:00").run(fn); file khác import { baoCaoSang } rồi đọc baoCaoSang.runs()`, word))
	}
	chain, err := Begin(word, args(arguments))
	if err != nil {
		return value.Fail(value.InvalidArgumentCode, err.Error())
	}
	d := &declaration{app: m.App, place: m.place(), chain: chain, open: m.Open}
	if m.Loading && m.App != nil {
		m.App.declare(d)
	}
	return value.Struct(&Builder{declaration: d})
}

func (m *Module) Every(arguments ...value.Value) value.Value   { return m.begin("every", arguments) }
func (m *Module) Hourly(arguments ...value.Value) value.Value  { return m.begin("hourly", arguments) }
func (m *Module) Daily(arguments ...value.Value) value.Value   { return m.begin("daily", arguments) }
func (m *Module) Weekly(arguments ...value.Value) value.Value  { return m.begin("weekly", arguments) }
func (m *Module) Monthly(arguments ...value.Value) value.Value { return m.begin("monthly", arguments) }
func (m *Module) Doubleday(arguments ...value.Value) value.Value {
	return m.begin("doubleday", arguments)
}
func (m *Module) Firstday(arguments ...value.Value) value.Value {
	return m.begin("firstday", arguments)
}
func (m *Module) Lastday(arguments ...value.Value) value.Value { return m.begin("lastday", arguments) }
func (m *Module) On(arguments ...value.Value) value.Value      { return m.begin("on", arguments) }
func (m *Module) Monday(arguments ...value.Value) value.Value  { return m.begin("monday", arguments) }
func (m *Module) Tuesday(arguments ...value.Value) value.Value { return m.begin("tuesday", arguments) }
func (m *Module) Wednesday(arguments ...value.Value) value.Value {
	return m.begin("wednesday", arguments)
}
func (m *Module) Thursday(arguments ...value.Value) value.Value {
	return m.begin("thursday", arguments)
}
func (m *Module) Friday(arguments ...value.Value) value.Value { return m.begin("friday", arguments) }
func (m *Module) Saturday(arguments ...value.Value) value.Value {
	return m.begin("saturday", arguments)
}
func (m *Module) Sunday(arguments ...value.Value) value.Value { return m.begin("sunday", arguments) }

// List is schedule.list(): the schedules of the app, by name.
func (m *Module) List(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "schedule"}).List(arguments...)
}

// Runs is schedule.runs(): the runs of every schedule, newest first.
func (m *Module) Runs(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "schedule"}).Runs(arguments...)
}

// Status is schedule.status(s).
func (m *Module) Status(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "schedule"}).Status(arguments...)
}

// Where is schedule.where(fn), not yet.
func (m *Module) Where(arguments ...value.Value) value.Value {
	return (&Query{app: m.App, who: "schedule"}).Where(arguments...)
}

// configWord is a rule of every schedule of the app, schedule.keep, database, session: written at the top level of a
// plain .work file of the app, once in it (17/11/2026; only in .schedule/config.work until then). The host's pass
// that loads the app's work keeps it (Loading); any other run of the file passes over it.
func (m *Module) configWord(word string, set func() value.Value) value.Value {
	if !m.open() || module.RoleFile(m.written()) {
		return value.Fail(value.InvalidArgumentCode, "schedule."+word+` viết ở cấp file của một file .work thường của app, một lần trong app: schedule.keep("7d")`)
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

// Keep is schedule.keep(duration): how long an ended run is kept, from "1h".
func (m *Module) Keep(arguments ...value.Value) value.Value {
	return m.configWord("keep", func() value.Value {
		keep, ok := time.Duration(0), false
		if len(arguments) == 1 && arguments[0].IsString() {
			keep, ok = queue.ParseDuration(arguments[0].Text())
		}
		if !ok || keep < time.Hour {
			return value.Fail(value.InvalidArgumentCode, `schedule.keep nhận một khoảng từ "1h", thời gian giữ lần chạy đã xong: schedule.keep("7d")`)
		}
		m.App.keep = keep
		return value.Undefined()
	})
}

// Session is schedule.session({ … }): the identity a schedule runs with, which engine2 has not yet: read, and said.
func (m *Module) Session(arguments ...value.Value) value.Value {
	return m.configWord("session", func() value.Value {
		if len(arguments) != 1 || !arguments[0].IsObject() {
			return value.Fail(value.InvalidArgumentCode, `schedule.session nhận một object, danh tính dịch vụ của lịch: schedule.session({ role: "system" })`)
		}
		m.App.say("[schedule] " + m.place() + ": schedule.session(…) chưa có trên engine2: lịch chạy không phiên")
		return value.Undefined()
	})
}

// Database is schedule.database(db): the database the runs and the state of the schedules are in, as
// queue.database(db) (docs/KITWORK_SCHEDULE.md mục 2); each kind follows its own call.
func (m *Module) Database(arguments ...value.Value) value.Value {
	return m.configWord("database", func() value.Value {
		path, name, ok := "", "", false
		if len(arguments) == 1 && m.App.Locate != nil {
			path, name, ok = m.App.Locate(arguments[0])
		}
		if !ok {
			return value.Fail(value.InvalidArgumentCode, `schedule.database nhận một database của app đã import, connection(…).tables({ … }) khai ở cấp file của một file .work: import { jobs } from "app/core/jobs.work"; schedule.database(jobs)`)
		}
		m.App.file, m.App.place = queue.Place(m.App.root, m.App.open, queue.KindSchedule, path, name, m.place(), m.App.say)
		m.App.store = queue.NewStore(m.App.file, queue.KindSchedule)
		return value.Undefined()
	})
}

// args are the arguments of a call as the rule reads them.
func args(arguments []value.Value) []Arg {
	out := make([]Arg, len(arguments))
	for index, argument := range arguments {
		switch {
		case argument.IsFunction():
			out[index] = Arg{Kind: Function}
		case argument.IsString():
			out[index] = Arg{Kind: String, Text: argument.Text()}
		case argument.IsNumber():
			out[index] = Arg{Kind: Number, Number: argument.Number()}
		default:
			out[index] = Arg{Kind: Other}
		}
	}
	return out
}

// declaration is one schedule as its file declares it: the chain of its rule, its function, where it is declared. A
// builder and the handle .run(…) or .once(…) gives are two faces of it.
type declaration struct {
	app      *App
	place    string
	chain    *Chain
	function *value.Lambda
	open     func() bool
}

func (d *declaration) running() bool { return d.open != nil && d.open() }

// name is the schedule's name, a failure when no file of the app exports it.
func (d *declaration) name() (string, value.Value) {
	if name := d.app.nameAt(d.place); name != "" {
		return name, value.Value{}
	}
	return "", value.Fail(value.InvalidArgumentCode, `lịch chưa có tên: lịch khai ở cấp file của một file .work của app và export, export const baoCaoSang = schedule.daily("09:00").run(fn); file khác import { baoCaoSang } from "app/core/baoCao.work"`)
}

// Builder is a schedule before .run(…) or .once(…): schedule.daily("09:00").retry(3).
type Builder struct {
	declaration *declaration
}

// Freeze: a builder is chained as its file runs.
func (b *Builder) Freeze() {}

// then chains .word(arguments): the builder again, or the handle after .run and .once.
func (b *Builder) then(word string, arguments []value.Value) value.Value {
	d := b.declaration
	if !d.running() {
		return value.Fail(value.InvalidArgumentCode, "."+word+"(…) nối khi lịch được khai, ở cấp file")
	}
	if err := d.chain.Then(word, args(arguments)); err != nil {
		return value.Fail(value.InvalidArgumentCode, err.Error())
	}
	if !IsEnd(word) {
		return value.Struct(b)
	}
	d.function = arguments[0].Lambda()
	if d.function == nil {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(".%s(handler) nhận một hàm: .%s((context) => { … })", word, word))
	}
	return value.Struct(&Handle{declaration: d})
}

func (b *Builder) At(arguments ...value.Value) value.Value      { return b.then("at", arguments) }
func (b *Builder) First(arguments ...value.Value) value.Value   { return b.then("first", arguments) }
func (b *Builder) Last(arguments ...value.Value) value.Value    { return b.then("last", arguments) }
func (b *Builder) Before(arguments ...value.Value) value.Value  { return b.then("before", arguments) }
func (b *Builder) After(arguments ...value.Value) value.Value   { return b.then("after", arguments) }
func (b *Builder) Retry(arguments ...value.Value) value.Value   { return b.then("retry", arguments) }
func (b *Builder) Timeout(arguments ...value.Value) value.Value { return b.then("timeout", arguments) }
func (b *Builder) Timezone(arguments ...value.Value) value.Value {
	return b.then("timezone", arguments)
}
func (b *Builder) Run(arguments ...value.Value) value.Value  { return b.then("run", arguments) }
func (b *Builder) Once(arguments ...value.Value) value.Value { return b.then("once", arguments) }

// MissingMember is a word chained after a rule that the rule does not have: the chain says which (chain.go).
func (b *Builder) MissingMember(name string) value.Value {
	if err := b.declaration.chain.Then(name, nil); err != nil {
		return value.Fail(value.InvalidArgumentCode, err.Error())
	}
	return value.Fail(value.TypeCode, "."+name+" không có trên lịch")
}

// Handle is what .run(…) and .once(…) give, exported under the schedule's name: it reads the schedule's runs and runs
// it now. Nothing chains after it.
type Handle struct {
	declaration *declaration
}

// Freeze: a handle is never written.
func (h *Handle) Freeze() {}

// MissingMember is a word chained after .run or .once.
func (h *Handle) MissingMember(name string) value.Value {
	ended := h.declaration.chain.Ended()
	if name == "list" {
		return value.Fail(value.InvalidArgumentCode, ".list() không có trên lịch: các lịch đọc bằng schedule.list(), các lần chạy của lịch bằng .runs()")
	}
	if message, removed := RemovedChained(name); removed {
		return value.Fail(value.InvalidArgumentCode, message)
	}
	if IsEnd(name) {
		return value.Fail(value.InvalidArgumentCode, TwoEnds(ended).Error())
	}
	return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("sau .%s(…) không nối gì: handle của lịch có runs, status, where, now", ended))
}

func (h *Handle) query(read func(*Query) value.Value) value.Value {
	name, failure := h.declaration.name()
	if failure.IsInvalid() {
		return failure
	}
	return read(&Query{app: h.declaration.app, who: name, name: name})
}

// Runs is baoCaoSang.runs(): the schedule's runs, newest first.
func (h *Handle) Runs(arguments ...value.Value) value.Value {
	return h.query(func(q *Query) value.Value { return q.Runs(arguments...) })
}

// Status is baoCaoSang.status(s).
func (h *Handle) Status(arguments ...value.Value) value.Value {
	return h.query(func(q *Query) value.Value { return q.Status(arguments...) })
}

// Where is baoCaoSang.where(fn), not yet.
func (h *Handle) Where(arguments ...value.Value) value.Value {
	return h.query(func(q *Query) value.Value { return q.Where(arguments...) })
}

// Now is baoCaoSang.now(): one run of the schedule now, outside its rule (mục 6); context.job.at is when it was called.
func (h *Handle) Now(arguments ...value.Value) value.Value {
	name, failure := h.declaration.name()
	if failure.IsInvalid() {
		return failure
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, name+".now() không nhận tham số: chạy lịch ngay một lần, context.job.at là lúc gọi")
	}
	if h.declaration.running() {
		return value.Fail(value.InvalidArgumentCode, name+".now() chạy lúc một request chạy, không lúc file được đọc")
	}
	return h.declaration.app.now(name)
}

// statuses are the statuses of a schedule's run (mục 9).
var statuses = []string{queue.Pending, queue.Running, queue.Completed, queue.Failed}

// Query is a read described before its action: schedule.status("failed").runs(10), baoCaoSang.runs().
type Query struct {
	app *App
	// who is how the read is written, schedule or the schedule's name; name the schedule it is limited to ("" all)
	who    string
	name   string
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
		for _, status := range statuses {
			known = known || arguments[0].Text() == status
		}
	}
	if !known {
		return value.Fail(value.InvalidArgumentCode, q.who+".status nhận một trạng thái của lần chạy: "+strings.Join(statuses, ", "))
	}
	next := *q
	next.status = arguments[0].Text()
	return value.Struct(&next)
}

// Where is where(fn): a condition run where the runs are kept, which engine2 has not yet.
func (q *Query) Where(...value.Value) value.Value {
	return value.Fail(value.UnsupportedCode, q.who+`.where(fn) chưa có: điều kiện của where chạy ở tầng lưu trữ, không lọc trong bộ nhớ (KITWORK_SCHEDULE.md mục 8); lọc theo trạng thái là .status("failed")`)
}

// Runs reads the runs, newest first: id, the schedule's name, the due time (at) and what started it (trigger:
// schedule or now), status, attempts, log, error, and its times.
func (q *Query) Runs(arguments ...value.Value) value.Value {
	limit, offset, failure := queue.Page("runs", arguments)
	if failure.IsInvalid() {
		return failure
	}
	if q.app == nil {
		return value.Array(nil)
	}
	runs, err := q.app.store.Runs(queue.Filter{Name: q.name, Status: q.status}, limit, offset)
	if err != nil {
		return value.Fail(value.DatabaseCode, fmt.Sprintf("%s.runs: %v", q.who, err))
	}
	out := make([]value.Value, len(runs))
	for index, run := range runs {
		at, trigger := runOf(run.Data)
		record := value.Object()
		record.Set("id", value.String(run.ID))
		record.Set("name", value.String(run.Name))
		record.Set("at", orNull(at))
		record.Set("trigger", orNull(trigger))
		record.Set("status", value.String(run.Status))
		record.Set("attempt", value.Number(float64(run.Attempt)))
		record.Set("maxAttempts", value.Number(float64(run.MaxAttempts)))
		record.Set("log", value.String(run.Log))
		record.Set("error", queue.RunError(run))
		record.Set("createdAt", value.String(run.CreatedAt))
		record.Set("startedAt", orNull(run.StartedAt))
		record.Set("finishedAt", orNull(run.FinishedAt))
		record.Set("nextRunAt", queue.NextRunAt(run))
		out[index] = record
	}
	return value.Array(out)
}

// List reads the schedules of the app, by name.
func (q *Query) List(arguments ...value.Value) value.Value {
	if q.name != "" {
		return value.Fail(value.InvalidArgumentCode, ".list() không có trên lịch: các lịch đọc bằng schedule.list(), các lần chạy của lịch bằng .runs()")
	}
	if q.status != "" {
		return value.Fail(value.InvalidArgumentCode, fmt.Sprintf(`schedule.status("%s").list() không có: trạng thái chỉ có ở lần chạy, viết schedule.status("%s").runs()`, q.status, q.status))
	}
	limit, offset, failure := queue.Page("list", arguments)
	if failure.IsInvalid() {
		return failure
	}
	if q.app == nil {
		return value.Array(nil)
	}
	return q.app.list(limit, offset)
}

// orNull is s, null for "".
func orNull(s string) value.Value {
	if s == "" {
		return value.Null()
	}
	return value.String(s)
}

// Context is the context a schedule's function gets (mục 7): context.job, the run and this attempt. A schedule carries
// no data: context.data() is its error.
type Context struct {
	claim queue.Claim
	log   *queue.Log
}

// scheduleContext is the context of a claimed run of a schedule.
func scheduleContext(claim queue.Claim, log *queue.Log) value.Value {
	return value.Struct(&Context{claim: claim, log: log})
}

// Freeze: a Context is never written.
func (c *Context) Freeze() {}

// noData is the error of context.data() in a schedule's function.
const noData = `lịch không có context.data(): lịch không mang dữ liệu; việc có dữ liệu là một job, lịch gửi nó bằng camOn.send(data, { key: "…" + context.job.at })`

// Data is context.data(): a schedule carries none.
func (c *Context) Data(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, noData)
}

// Job is context.job: id, name, attempt (from 1), maxAttempts, isFinalAttempt, at (the due time), and log(…).
func (c *Context) Job() value.Value {
	at, _ := runOf(c.claim.Data)
	job := value.Object()
	job.Set("id", value.String(c.claim.ID))
	job.Set("name", value.String(c.claim.Name))
	job.Set("attempt", value.Number(float64(c.claim.Attempt)))
	job.Set("maxAttempts", value.Number(float64(c.claim.MaxAttempts)))
	job.Set("isFinalAttempt", value.Boolean(c.claim.Attempt >= c.claim.MaxAttempts))
	job.Set("at", value.String(at))
	job.Set("log", c.log.Function())
	return job
}

// removedContext are the words of the old cron's handler argument, by what the spec writes.
var removedContext = map[string]string{
	"scheduledFor":   "context.job.at",
	"executionId":    "context.job.id",
	"log":            "context.job.log(…)",
	"attempt":        "context.job.attempt",
	"maxAttempts":    "context.job.maxAttempts",
	"isFinalAttempt": "context.job.isFinalAttempt",
	"id":             "context.job.id",
	"name":           "context.job.name",
	"at":             "context.job.at",
}

// MissingMember names, for each word of the old cron's handler argument, what it is now.
func (c *Context) MissingMember(name string) value.Value {
	if now, removed := removedContext[name]; removed {
		return value.Fail(value.InvalidArgumentCode, "context."+name+" không có ở lịch: "+now)
	}
	return value.Fail(value.TypeCode, "context."+name+" không có: context của lịch có job")
}
