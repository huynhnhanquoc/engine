package host_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// What a run waits on outside the engine ends with the run (value.WaitContext, 15/11/2026): a call out, a routine's
// wait, a read and a write of the database were given no context of the run, so a call out waited its own 60 seconds,
// a wait and a read as long as they took, past the request's time and past a client gone, and a routine's .timeout()
// cut none of them; a write queued behind a long one was made after its run had ended.

// waitsApp boots an app whose routes wait on held, a server that answers /hold once the test lets it, with set applied
// to the host.
func waitsApp(t *testing.T, held *heldUp, set func(*host.Host)) *booted {
	t.Helper()
	hold := `"` + held.server.URL + `/hold"`
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(busy.Close)
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/core/schema/shop.work": `import { struct, id, string } from "kitwork/schema";
import { connection } from "kitwork/database";
export const notes = struct({ id: id(), text: string() });
export const db = connection().tables({ notes });
`,
		"apps/shop/shop.test/out/router.work": routineImport + `router.get((context) => context.text(http.get(` + hold + `).timeout("30s").send().text));` + "\n",
		"apps/shop/shop.test/timed/router.work": routineImport + `router.get((context) => {
  const [r] = routine.wait(routine.timeout("200ms").go(() => http.get(` + hold + `).timeout("30s").send().text));
  return context.json({ ok: r.ok, code: r.ok ? null : r.error?.code });
});
`,
		"apps/shop/shop.test/wait/router.work": routineImport + `router.get((context) => {
  routine.wait(routine.go(() => http.get(` + hold + `).timeout("30s").send().text));
  return context.text("waited");
});
`,
		"apps/shop/shop.test/busy/router.work": routineImport + `router.get((context) => context.json({ ok: http.get("` + busy.URL + `").retry(5).timeout("30s").send().ok }));` + "\n",
		"apps/shop/shop.test/read/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => context.text("" + db.count((notes) => notes)));
`,
		"apps/shop/shop.test/statement/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
const every = db.where((notes) => notes);
router.get((context) => context.text("" + every.count()));
`,
		"apps/shop/shop.test/hold/router.work": routineImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => {
  routine.go(() => db.transaction((database) => { database.insert((notes) => ({ text: "held" })); http.get(` + hold + `).timeout("30s").send(); return 1; }));
  return context.text("holding");
});
`,
		"apps/shop/shop.test/write/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => { db.insert((notes) => ({ text: "late" })); return context.text("written"); });
`,
		"apps/shop/shop.test/transaction/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => { db.transaction((database) => database.insert((notes) => ({ text: "late" }))); return context.text("written"); });
`,
		"apps/shop/shop.test/late/router.work": routerImport + `import { db } from "app/core/schema/shop.work";
router.get((context) => context.text("" + db.count((notes) => notes.text === "late")));
`,
	})
	if code, said := runMigrate(t, folder, true, ""); code != 0 {
		t.Fatalf("kitwork migrate: %d\n%s", code, said)
	}
	if set == nil {
		set = func(*host.Host) {}
	}
	return boot(t, filepath.Join(folder, "application.work"), set)
}

// within gets path of shop.test and fails the test when no answer comes within limit; else the answer and its time.
func within(t *testing.T, app *booted, path string, limit time.Duration) (reply, time.Duration) {
	t.Helper()
	type got struct {
		reply reply
		took  time.Duration
	}
	done := make(chan got, 1)
	started := time.Now()
	go func() { done <- got{app.get(t, "shop.test", path), time.Since(started)} }()
	select {
	case answer := <-done:
		return answer.reply, answer.took
	case <-time.After(limit):
		t.Fatalf("GET %s: no answer in %v, past its run's time", path, limit)
		return reply{}, 0
	}
}

// A call out ends with the request that sends it: past the request's 300 ms, not at its own 30 s.
func TestACallOutEndsWithItsRun(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, func(h *host.Host) { h.RequestTimeout = 300 * time.Millisecond })
	got, took := within(t, app, "/out", 3*time.Second)
	if got.status != 500 {
		t.Errorf("GET /out: %d %q after %v, want 500", got.status, got.body, took)
	}
	contains(t, "the log", app.log.String(), "GET /out (/out/router.work): 500:", "Execution Cancelled")
}

// A routine's .timeout() ends the call out its task waits on: the task is TIMEOUT in some 200 ms, not when the call is
// answered.
func TestARoutinesTimeoutEndsItsCallOut(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, nil)
	got, took := within(t, app, "/timed", 3*time.Second)
	if got.status != 200 || got.body != `{"ok":false,"code":"TIMEOUT"}` {
		t.Errorf("GET /timed: %d %q after %v, want the task TIMEOUT", got.status, got.body, took)
	}
}

// routine.wait(…) ends with the run that waits: past the request's 300 ms, its task going on.
func TestARoutineWaitEndsWithItsRun(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, func(h *host.Host) { h.RequestTimeout = 300 * time.Millisecond })
	got, took := within(t, app, "/wait", 3*time.Second)
	if got.status != 500 {
		t.Errorf("GET /wait: %d %q after %v, want 500", got.status, got.body, took)
	}
	contains(t, "the log", app.log.String(), "GET /wait (/wait/router.work): 500:", "routine.wait(…) waited past the end of its run")
}

// A call out's retries end with its run: five of them pause 1.55 s at least between them, past a request of half a
// second, which answers within it that the call failed.
func TestACallOutsRetriesEndWithItsRun(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, func(h *host.Host) { h.RequestTimeout = 500 * time.Millisecond })
	got, took := within(t, app, "/busy", 1300*time.Millisecond)
	if got.status != 200 || got.body != `{"ok":false}` {
		t.Errorf("GET /busy: %d %q after %v, want the call failed within its run; the log:\n%s", got.status, got.body, took, app.log)
	}
}

// A read of the database is made within its run: a request whose client is gone before it reads reads nothing, where
// the read was made and answered; so does a statement a site's file wrote as it booted.
func TestAReadIsMadeWithinItsRun(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, nil)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"/read", "/statement"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(gone)
		request.Host = "shop.test"
		app.host.ServeHTTP(recorder, request)
		if recorder.Code == 200 {
			t.Errorf("GET %s of a client gone: %d %q, want no read", path, recorder.Code, recorder.Body.String())
		}
		if got := app.get(t, "shop.test", path); got.status != 200 || got.body != "0" {
			t.Errorf("GET %s: %d %q, want 200 0", path, got.status, got.body)
		}
	}
}

// A write whose run ended before the writer came to it is not made: queued behind a transaction a routine holds, a
// write and a transaction of requests of 300 ms wait for the writer and are refused whole when it is free, where they
// were made after their request had ended.
func TestAWriteWhoseRunEndedIsNotMade(t *testing.T) {
	held := newHeldUp(t)
	app := waitsApp(t, held, func(h *host.Host) { h.RequestTimeout = 300 * time.Millisecond })
	if got := app.get(t, "shop.test", "/hold"); got.status != 200 {
		t.Fatalf("GET /hold: %d %q", got.status, got.body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for held.at.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if held.at.Load() < 1 {
		t.Fatal("the routine's transaction never held the writer")
	}
	answers := make(chan reply, 2)
	for _, path := range []string{"/write", "/transaction"} {
		go func() { answers <- app.get(t, "shop.test", path) }()
	}
	time.Sleep(time.Second) // both queued, past their request's time
	close(held.release)
	for range 2 {
		select {
		case got := <-answers:
			if got.status == 200 {
				t.Errorf("a write past its run: %d %q, want refused", got.status, got.body)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a write queued behind the held writer never answered")
		}
	}
	if got := app.get(t, "shop.test", "/late"); got.body != "0" {
		t.Errorf("rows written after their run ended: %q, want 0", got.body)
	}
	if !strings.Contains(app.log.String(), "GET /write (/write/router.work): 500:") {
		t.Errorf("the log says:\n%s\nwant the write's 500", app.log)
	}
}
