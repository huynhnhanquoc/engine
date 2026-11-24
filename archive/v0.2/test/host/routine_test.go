package host_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/internal/loopback"
)

// routineImport is the three imports of a router.work that runs routines and calls out.
const routineImport = "import * as routine from \"kitwork/routine\";\nimport * as http from \"kitwork/http\";\n" + routerImport

// long is a function of the file that works for about twenty seconds within its energy: one instruction over a
// large array takes a millisecond (isolation_test.go), so only a stop ends it soon.
const long = `const long = () => {
  const items = "x".repeat(1000000).split("");
  for (let i = 0; i < 20000; i++) { items.reverse(); }
  return "done";
};
`

// slowUp is the other side for routines, on loopback: /slow?ms=N&name=x answers "late" after N ms and counts
// its calls by name.
func slowUp(t *testing.T) (*httptest.Server, func(name string) int) {
	t.Helper()
	var mutex sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		counts[r.URL.Query().Get("name")]++
		mutex.Unlock()
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		w.Write([]byte("late"))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(loopback.Open())
	return server, func(name string) int {
		mutex.Lock()
		defer mutex.Unlock()
		return counts[name]
	}
}

// routineFiles are the folders of a site that runs routines, U/ the upstream's address.
func routineFiles(up string) map[string]string {
	site := "apps/shop/shop.test/"
	source := func(text string) string { return routineImport + long + strings.ReplaceAll(text, "U/", up+"/") }
	return map[string]string{
		"application.work": manifest,
		site + "together/router.work": source(`router.get((context) => {
  const started = Date.now();
  const a = routine.go(() => "a" + http.get("U/slow?ms=300").send().text);
  const b = routine.go(() => "b" + http.get("U/slow?ms=300").send().text);
  const c = routine.go(() => "c" + http.get("U/slow?ms=300").send().text);
  const [x, y, z] = routine.wait(a, b, c);
  const ms = Date.now() - started;
  return context.json({ results: [x.data, y.data, z.data], ms: ms, one: routine.wait(routine.go(() => 7)),
    array: routine.wait([a, b]).map((result) => result.data) });
});
`),
		site + "handle/router.work": source(`router.get((context) => {
  const report = routine.go(() => ({ token: Math.random() }));
  const first = routine.wait(report)[0].data;
  first.extra = 1;
  const again = routine.wait(report)[0].data;
  report.cancel();
  const kept = routine.wait(report)[0];
  return context.json({ same: first.token === again.token, written: again.extra === undefined, kept: kept.ok });
});
`),
		site + "copy/router.work": source(`const answer = () => 42;
const twice = (n) => n * 2;
router.get((context) => {
  let seen = "before";
  const box = { n: 1 };
  const pair = [box, box];
  const child = routine.wait(routine.go(() => { seen = "child"; pair[0].n = 5; return [seen, box.n, pair[1].n]; }))[0].data;
  let later = 1;
  const task = routine.go(() => later);
  later = 2;
  return context.json({ seen: seen, n: box.n, child: child, later: routine.wait(task)[0].data,
    direct: routine.wait(routine.go(answer))[0].data, boot: routine.wait(routine.go(() => twice(21)))[0].data });
});
`),
		site + "attempts/router.work": source(`router.get((context) => {
  const [retried] = routine.wait(routine.retry(2).go(() => JSON.parse(http.get("U/slow?ms=0&name=retried").send().text)));
  const [failing] = routine.wait(routine.retry(1).go(() => { http.get("U/slow?ms=0&name=failing").send(); return error("again"); }));
  const [handled] = routine.wait(routine.retry(2).go(() => {
    http.get("U/slow?ms=0&name=handled").send();
    const inner = safe(() => error("caught"));
    return inner.error?.message;
  }));
  const started = Date.now();
  const [timed] = routine.wait(routine.retry(1).timeout("100ms").go(() => { http.get("U/slow?ms=0&name=timed").send(); return http.get("U/slow?ms=300").send().text; }));
  const timedMs = Date.now() - started;
  const [spent] = routine.wait(routine.retry(2).go(() => {
    http.get("U/slow?ms=0&name=spent").send();
    let x = 0;
    for (let i = 0; i < 100000000; i++) { x = x + 1; }
    return x;
  }));
  return context.json({ retried: retried.error?.code, failing: [failing.ok, failing.data, failing.error?.code, failing.error?.message],
    handled: [handled.ok, handled.data, handled.error], timed: timed.error?.code, timedSays: timed.error?.message, timedMs: timedMs,
    spent: spent.error?.code, spentSays: spent.error?.message });
});
`),
		site + "cancel/router.work": source(`router.get((context) => {
  const one = routine.go(() => long());
  routine.cancel(one);
  const two = routine.go(() => long());
  two.cancel();
  const three = routine.go(() => long());
  routine.cancel([three]);
  const [a, b, c] = routine.wait(one, two, three);
  return context.json({ codes: [a.error?.code, b.error?.code, c.error?.code], says: a.error?.message, data: a.data });
});
`),
		site + "request/router.work": source(`router.get((context) => {
  const query = context.query;
  const request = context.request;
  const response = context.response;
  const [read, method, deep, kept, answered] = routine.wait(
    routine.go(() => context.query("a")),
    routine.go(() => query("a")),
    routine.go(() => context.request.path),
    routine.go(() => request.path),
    routine.go(() => response.status(201))
  );
  const a = context.query("a");
  return context.json({ read: read.error?.code, says: read.error?.message, method: method.error?.code, deep: deep.error?.code,
    kept: kept.error?.code, answered: answered.error?.code, handed: routine.wait(routine.go(() => a))[0].data });
});
`),
		site + "rules/router.work": source(`const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
router.get((context) => {
  const h = routine.go(() => 1);
  return context.json({
    goNothing: code(() => routine.go()),
    goNumber: code(() => routine.go(1)),
    goTwo: code(() => routine.go(() => 1, () => 2)),
    waitNothing: code(() => routine.wait()),
    waitFunction: code(() => routine.wait(() => 1)),
    waitNumber: code(() => routine.wait(1)),
    retried: code(() => routine.retry(1).wait(h)),
    timed: code(() => routine.timeout("1s").wait(h)),
    badRetry: code(() => routine.retry(-1)),
    halfRetry: code(() => routine.retry(1.5)),
    badTimeout: code(() => routine.timeout(100)),
    badDuration: code(() => routine.timeout("soon")),
    readHandle: code(() => h.state),
    badCancel: code(() => routine.cancel(1)),
    cancelStruct: code(() => routine.cancel(routine)),
    word: code(() => routine.run(() => 1)),
    fine: code(() => routine.wait(h)),
    functionBack: routine.wait(routine.go(() => () => 1))[0].error?.code,
    waitFunctionSays: safe(() => routine.wait(() => 1)).error?.message,
    wordSays: safe(() => routine.run(() => 1)).error?.message,
    handleSays: safe(() => h.state).error?.message
  });
});
`),
		site + "several/router.work": source(`router.get((context) => {
  const one = routine.go(() => 1);
  const two = routine.go(() => error("second broke"));
  http.get("U/slow?ms=150").send();
  const [a, b, c] = routine.wait(one, two, routine.go(() => error("third broke")));
  return context.json({ a: [a.ok, a.data, a.error], b: [b.ok, b.data === null, b.error?.code], bSays: b.error?.message, c: c.error?.message });
});
`),
		site + "large/router.work": source(`const count = (list) => routine.wait(routine.go(() => list.length))[0].data;
router.get((context) => {
  const many = "x".repeat(70000).split("").map(() => ({}));
  const few = many.slice(0, 10);
  const held = safe(() => routine.go(() => many.length));
  const [back] = routine.wait(routine.go(() => JSON.parse("[" + "{},".repeat(1050000) + "{}]")));
  return context.json({ held: held.error?.code, heldSays: held.error?.message, back: back.error?.code, backSays: back.error?.message,
    few: count(few) });
});
`),
		site + "log/router.work": source(`router.get((context) => {
  routine.go(() => error("left alone"));
  return context.text("answered");
});
`),
	}
}

// routineSite boots the site of routineFiles against up.
func routineSite(t *testing.T, up string) *booted {
	t.Helper()
	folder := write(t, routineFiles(up))
	return boot(t, filepath.Join(folder, "application.work"))
}

// go() starts a function and gives its task at once; wait(a, b, c) waits for all and gives their results in the
// order given, each { ok, data, error } (engine2/docs/routine.md mục 2, 3); wait([a, b]) takes an array, and one
// task gives an array of one. Three calls of 300 ms each end together, not one after another.
func TestRoutineWaitRunsTogetherInOrder(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/together")
	if shown(got["results"]) != `["alate","blate","clate"]` || shown(got["one"]) != `[{"data":7,"error":null,"ok":true}]` ||
		shown(got["array"]) != `["alate","blate"]` {
		t.Errorf("results %s, one %s, array %s", shown(got["results"]), shown(got["one"]), shown(got["array"]))
	}
	if ms := got["ms"].(float64); ms >= 850 {
		t.Errorf("three calls of 300 ms took %v ms: they ran one after another", ms)
	}
}

// wait(task) gives its result as often as asked, the function run once, each waiter a copy of its own; cancel() of a
// task that has ended keeps its result (mục 3).
func TestRoutineHandlesAreWaitedForAsOftenAsAsked(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	if got := fetchJSON(t, b, "/handle"); shown(got) != `{"kept":true,"same":true,"written":true}` {
		t.Errorf("tasks: %s", shown(got))
	}
}

// What a task holds is copied as go() starts it (mục 4): its writes are its copy's, the caller sees none, a value it
// holds twice is one value in the copy, a variable written after go() is not seen; a function of the file is the
// boot's, run as it is, handed directly or called.
func TestRoutineRunsOnACopyMadeAsItStarts(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/copy")
	want := `{"boot":42,"child":["child",5,5],"direct":42,"later":1,"n":1,"seen":"before"}`
	if shown(got) != want {
		t.Errorf("copy: %s\nwant %s", shown(got), want)
	}
}

// .retry(n) is n runs after the first when a run fails, by a failure or by return error(…); a failure the function
// caught with safe is no failure; .timeout() bounds each run, TIMEOUT past it (mục 3.1); a task that spends its
// energy is not run again. Every failure is in the result, none rises.
func TestRoutineRetriesAndTimesOutEachAttempt(t *testing.T) {
	up, count := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/attempts")
	if got["retried"] != "INVALID_INPUT" || count("retried") != 3 {
		t.Errorf("retry(2) of a failing function: %v after %d runs, want INVALID_INPUT after 3", got["retried"], count("retried"))
	}
	failing := got["failing"].([]any)
	if failing[0] != false || failing[1] != nil || failing[2] != "FAIL" || !strings.Contains(failing[3].(string), "again") || count("failing") != 2 {
		t.Errorf("retry(1) of return error(…): %s after %d runs", shown(failing), count("failing"))
	}
	handled := got["handled"].([]any)
	if handled[0] != true || !strings.HasSuffix(handled[1].(string), "caught") || handled[2] != nil || count("handled") != 1 {
		t.Errorf("a failure caught by safe: %s after %d runs, want one run", shown(got["handled"]), count("handled"))
	}
	if got["timed"] != "TIMEOUT" || count("timed") != 2 || got["timedMs"].(float64) > 2000 {
		t.Errorf("retry(1).timeout(100ms): %v after %d runs in %v ms, want TIMEOUT after 2 soon", got["timed"], count("timed"), got["timedMs"])
	}
	contains(t, "the timeout", got["timedSays"].(string), "took longer than 100ms")
	if got["spent"] == nil || count("spent") != 1 {
		t.Errorf("a task past its energy: %v after %d runs, want a failure after 1", got["spent"], count("spent"))
	}
	contains(t, "the energy", strings.ToLower(got["spentSays"].(string)), "energy")
}

// What a task holds is copied as it starts, so it is bounded: past 65,536 objects, arrays and functions go() fails
// TOO_LARGE; what it gives back, copied for each who waits, past 1,048,576 is the task's TOO_LARGE. A function holds
// the frames it closes over whole, so a few values go in through a function of their own. A million objects of
// their own take some 92 MB, past a run's ceiling of 64 MiB, so the site's runs are given 256 MiB: under the ceiling
// a run never holds what it could give back past that (JSON.parse stops as its tree grows, 14/11/2026).
func TestRoutineCopiesAreBounded(t *testing.T) {
	up, _ := slowUp(t)
	files := routineFiles(up.URL)
	files["application.work"] = limitsManifest(`app.limits({ memory: "256mb" });` + "\n")
	b := boot(t, filepath.Join(write(t, files), "application.work"))
	got := fetchJSON(t, b, "/large")
	if got["held"] != "TOO_LARGE" || got["back"] != "TOO_LARGE" || got["few"] != float64(10) {
		t.Errorf("large: %s", shown(got))
	}
	contains(t, "held", got["heldSays"].(string), "what a routine holds is past 65536")
	contains(t, "back", got["backSays"].(string), "what a routine gave back is past 1048576")
}

// routine.cancel(task), task.cancel() and routine.cancel([task]) ask a task to stop; its result is then CANCELLED.
func TestRoutineCancelStopsIt(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	started := time.Now()
	got := fetchJSON(t, b, "/cancel")
	if shown(got["codes"]) != `["CANCELLED","CANCELLED","CANCELLED"]` || got["data"] != nil || time.Since(started) > 5*time.Second {
		t.Errorf("cancel: %s after %v, want CANCELLED soon", shown(got), time.Since(started))
	}
	contains(t, "cancel", got["says"].(string), "the task was cancelled")
}

// The request is no value a task holds (mục 4): its context, request and response, and a method read off them,
// stand in the copy as values whose every read fails TYPE saying to read first; a value read first goes in.
func TestRoutineHoldsNoRequest(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/request?a=x")
	if got["read"] != "TYPE" || got["method"] != "TYPE" || got["deep"] != "TYPE" || got["kept"] != "TYPE" || got["answered"] != "TYPE" ||
		got["handed"] != "x" {
		t.Errorf("the request in a task: %s", shown(got))
	}
	contains(t, "the failure", got["says"].(string), "a routine runs apart from the request", "read query before routine.go")
}

// Each word is checked (mục 3): go takes one function; wait and cancel take tasks, or an array of them, not
// functions; .retry and .timeout go before go(); a whole number of runs and a duration as text; data back, not a
// function; nothing read from a task; a word routine does not have.
func TestRoutineRules(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/rules")
	contains(t, "wait of a function", got["waitFunctionSays"].(string), "routine.wait(…) takes tasks, not functions")
	contains(t, "a word routine does not have", got["wordSays"].(string), "routine has no word run")
	contains(t, "a read of a task", got["handleSays"].(string), "a task of routine.go(…) has no state")
	for _, key := range []string{"waitFunctionSays", "wordSays", "handleSays"} {
		delete(got, key)
	}
	want := map[string]any{
		"goNothing": "INVALID_ARGUMENT", "goNumber": "INVALID_ARGUMENT", "goTwo": "INVALID_ARGUMENT", "waitNothing": "INVALID_ARGUMENT",
		"waitFunction": "INVALID_ARGUMENT", "waitNumber": "INVALID_ARGUMENT", "retried": "INVALID_ARGUMENT", "timed": "INVALID_ARGUMENT",
		"badRetry": "INVALID_ARGUMENT", "halfRetry": "INVALID_ARGUMENT", "badTimeout": "INVALID_ARGUMENT", "badDuration": "INVALID_ARGUMENT",
		"readHandle": "UNSUPPORTED", "badCancel": "INVALID_ARGUMENT", "cancelStruct": "INVALID_ARGUMENT", "word": "TYPE", "fine": nil,
		"functionBack": "TYPE",
	}
	if shown(got) != shown(want) {
		t.Errorf("rules: %s\nwant   %s", shown(got), shown(want))
	}
}

// Tasks that fail give their failure in their result, each its own, none rising (mục 3.2); a failure waited for is
// no line in the log.
func TestRoutineFailuresOfSeveral(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	got := fetchJSON(t, b, "/several")
	if shown(got["a"]) != `[true,1,null]` || shown(got["b"]) != `[false,true,"FAIL"]` {
		t.Errorf("several: %s", shown(got))
	}
	contains(t, "the second", got["bSays"].(string), "second broke")
	contains(t, "the third", got["c"].(string), "third broke")
	time.Sleep(200 * time.Millisecond)
	if strings.Contains(b.log.String(), "second broke") {
		t.Errorf("a failure waited for is in the log:\n%s", b.log)
	}
}

// A task that fails with nobody waiting is a line in the log once the call that started it has ended: a failure is
// never lost.
func TestRoutineFailureNobodyWaitsForIsLogged(t *testing.T) {
	up, _ := slowUp(t)
	b := routineSite(t, up.URL)
	if got := b.get(t, "shop.test", "/log"); got.body != "answered" {
		t.Fatalf("/log: %d %s", got.status, got.body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(b.log.String(), "left alone") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	contains(t, "the log", b.log.String(), "routine.go(…) failed and nothing waited for it", "left alone")
}

// A file declares as the site boots: go() or wait() at its top level is a boot error; the manifest runs nothing, so it
// has no routine.
func TestRoutineAtTheTopLevelIsABootError(t *testing.T) {
	said := bootError(t, map[string]string{
		"apps/shop/shop.test/router.work": routineImport + "routine.go(() => 1);\nrouter.get((context) => context.text(\"x\"));\n",
	})
	contains(t, "the boot", said, "routine.go(…) at the top level of a file")
	said = bootError(t, map[string]string{
		"application.work": "import * as routine from \"kitwork/routine\";\n" + manifest,
	})
	contains(t, "the manifest", said, "kitwork/routine is an app's")
}

// Tasks live past the request and stop with the host (mục 4): Close tells them to stop, and each ends and says so, one
// waiting on a call out too, which ends with its task (value.WaitContext, 15/11/2026: the call out heard nothing, so
// Close waited the grace of two seconds for its task and said it ran on). The grace stays for a call of the host
// that does not hear the stop.
func TestRoutinesStopWithTheHost(t *testing.T) {
	up, _ := slowUp(t)
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routineImport + long + strings.ReplaceAll(`router.get((context) => {
  routine.go(() => long());
  routine.go(() => http.get("U/slow?ms=2600").send().text);
  return context.text("started");
});
`, "U/", up.URL+"/"),
	})
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	read.RunEnergy = longEnergy // long() within its energy (core/runtime, work.go)
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "shop.test"
	read.ServeHTTP(recorder, request)
	if recorder.Body.String() != "started" {
		t.Fatalf("/: %d %s\n%s", recorder.Code, recorder.Body, log)
	}
	time.Sleep(100 * time.Millisecond)
	started := time.Now()
	_ = read.Close()
	took := time.Since(started)
	if took > time.Second {
		t.Errorf("Close took %v, want both tasks stopped at once", took)
	}
	if said := strings.Count(log.String(), "routine.go(…) stopped unfinished as the site closed"); said != 2 || strings.Contains(log.String(), "still run as the host closes") {
		t.Errorf("the log says:\n%s\nwant both tasks stopped unfinished, none running on", log)
	}
	if strings.Contains(log.String(), "done") {
		t.Errorf("the long task finished:\n%s", log)
	}
}
