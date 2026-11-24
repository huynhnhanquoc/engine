package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// The rules of kitwork_runs one process cannot show over HTTP: a lease taken from an attempt, a lease another process
// left, the state of a schedule. Each on a file of its own.

func openStore(t *testing.T, kind string) *Store {
	t.Helper()
	open := func(path string) (*kitdb.Database, func(), error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, err
		}
		database, err := kitdb.Open(context.Background(), path, kitdb.Options{})
		if err != nil {
			return nil, nil, err
		}
		return database, func() { _ = database.Close() }, nil
	}
	file := NewFile(t.TempDir(), open, "test")
	t.Cleanup(file.Close)
	return NewStore(file, kind)
}

var start = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

func enqueue(t *testing.T, store *Store, name string, attempts int, at time.Time) Run {
	t.Helper()
	run, duplicate, err := store.Enqueue(NewRun{Name: name, Data: "{}", MaxAttempts: attempts, Available: at}, at)
	if err != nil || duplicate {
		t.Fatalf("enqueue %s: %v %v", name, err, duplicate)
	}
	return run
}

func claim(t *testing.T, store *Store, at time.Time, lease time.Duration, limit int) []Claim {
	t.Helper()
	claims, _, err := store.Claim(at, lease, limit)
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

// status reads one run.
func status(t *testing.T, store *Store, id string) Run {
	t.Helper()
	var run Run
	if err := store.database.Read(func(db Querier) error {
		var err error
		run, err = store.get(db, id)
		return err
	}); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return run
}

func TestATokenThatLostItsRunSettlesNothing(t *testing.T) {
	store := openStore(t, KindQueue)
	enqueue(t, store, "thanks", 2, start)
	claimed := claim(t, store, start, time.Minute, 1)
	if len(claimed) != 1 || claimed[0].Attempt != 1 || claimed[0].Status != Running {
		t.Fatalf("claim: %+v", claimed)
	}
	id := claimed[0].ID
	if won, err := store.Complete(id, "another", "", start); err != nil || won {
		t.Fatalf("complete with another token: %v %v", won, err)
	}
	if took, err := store.FailAttempt(id, "another", "FAIL", "x", "", start, start); err != nil || took != "" {
		t.Fatalf("fail with another token: %q %v", took, err)
	}
	if _, holds, err := store.Extend(id, "another", start.Add(time.Hour)); err != nil || holds {
		t.Fatalf("extend with another token: %v %v", holds, err)
	}
	if run := status(t, store, id); run.Status != Running {
		t.Fatalf("the run, untouched: %+v", run)
	}
	// the heartbeat moves the lease: a reclaim past the claim's lease takes nothing
	if _, holds, err := store.Extend(id, claimed[0].Token, start.Add(time.Hour)); err != nil || !holds {
		t.Fatalf("extend: %v %v", holds, err)
	}
	if count, next, err := store.Reclaim(start.Add(2*time.Minute), nil); err != nil || count != 0 || !next.Equal(start.Add(time.Hour)) {
		t.Fatalf("a lease moved: %d %s %v", count, next, err)
	}
	if won, err := store.Complete(id, claimed[0].Token, "done\n", start); err != nil || !won {
		t.Fatalf("complete with its token: %v %v", won, err)
	}
	if won, _ := store.Complete(id, claimed[0].Token, "", start); won {
		t.Fatal("a token settles its run once")
	}
	if run := status(t, store, id); run.Status != Completed || run.Log != "done\n" || run.FinishedAt == "" {
		t.Fatalf("completed: %+v", run)
	}
}

func TestAFailedAttemptIsRetriedFailedOrCancelled(t *testing.T) {
	store := openStore(t, KindQueue)
	again, last, cancelled := enqueue(t, store, "a", 2, start), enqueue(t, store, "b", 1, start), enqueue(t, store, "c", 3, start)
	claims := map[string]Claim{}
	for _, each := range claim(t, store, start, time.Minute, 10) {
		claims[each.ID] = each
	}
	if _, err := store.Cancel(cancelled.ID, "", start); err != nil {
		t.Fatal(err)
	}
	retry := start.Add(time.Second)
	for id, want := range map[string]string{again.ID: Pending, last.ID: Failed, cancelled.ID: Cancelled} {
		if took, err := store.FailAttempt(id, claims[id].Token, "FAIL", "hỏng", "lần thử 1\n", retry, start); err != nil || took != want {
			t.Errorf("%s: %q %v, want %s", id, took, err, want)
		}
	}
	if run := status(t, store, again.ID); run.AvailableAt != Stamp(retry) || run.ErrorCode != "FAIL" || run.Log != "lần thử 1\n" || run.FinishedAt != "" {
		t.Fatalf("retried: %+v", run)
	}
	// a failure no attempt mends ends the run, whatever attempts it has left
	enqueue(t, store, "d", 5, start)
	held := claim(t, store, start.Add(2*time.Second), time.Minute, 10)
	for _, each := range held {
		if each.Name != "d" {
			continue
		}
		if took, _ := store.FailAttempt(each.ID, each.Token, "NOT_FOUND", "không có job d", "", time.Time{}, start); took != Failed {
			t.Fatalf("a failure with no retry: %q", took)
		}
	}
}

func TestALeaseThatRanOutComesBack(t *testing.T) {
	store := openStore(t, KindQueue)
	left, spent, cancelled, held := enqueue(t, store, "a", 2, start), enqueue(t, store, "b", 1, start), enqueue(t, store, "c", 1, start), enqueue(t, store, "d", 1, start)
	claims := map[string]Claim{}
	for _, each := range claim(t, store, start, time.Second, 10) {
		claims[each.ID] = each
	}
	if _, err := store.Cancel(cancelled.ID, "", start); err != nil {
		t.Fatal(err)
	}
	// the leases have not run out yet: nothing comes back, and the first to run out is said
	count, next, err := store.Reclaim(start.Add(500*time.Millisecond), nil)
	if err != nil || count != 0 || !next.Equal(start.Add(time.Second)) {
		t.Fatalf("before the leases run out: %d %s %v", count, next, err)
	}
	count, next, err = store.Reclaim(start.Add(2*time.Second), map[string]bool{held.ID: true})
	if err != nil || count != 3 || !next.IsZero() {
		t.Fatalf("reclaim: %d %s %v", count, next, err)
	}
	if run := status(t, store, left.ID); run.Status != Pending || run.ErrorCode != "TIMEOUT" || run.AvailableAt != Stamp(start.Add(2*time.Second)) {
		t.Fatalf("a run with attempts left: %+v", run)
	}
	if run := status(t, store, spent.ID); run.Status != Failed || run.ErrorCode != "TIMEOUT" || run.FinishedAt == "" {
		t.Fatalf("a run without: %+v", run)
	}
	if run := status(t, store, cancelled.ID); run.Status != Cancelled {
		t.Fatalf("a run being cancelled: %+v", run)
	}
	if run := status(t, store, held.ID); run.Status != Running {
		t.Fatalf("an attempt this process runs: %+v", run)
	}
	// the attempt that lost its lease counts
	again := claim(t, store, start.Add(3*time.Second), time.Second, 10)
	if len(again) != 1 || again[0].ID != left.ID || again[0].Attempt != 2 {
		t.Fatalf("claimed again: %+v", again)
	}
}

func TestACancelFollowsTheStatus(t *testing.T) {
	store := openStore(t, KindQueue)
	pending, running, ended := enqueue(t, store, "a", 1, start.Add(time.Hour)), enqueue(t, store, "a", 1, start), enqueue(t, store, "b", 1, start)
	claims := map[string]Claim{}
	for _, each := range claim(t, store, start, time.Minute, 10) {
		claims[each.ID] = each
	}
	if _, err := store.Complete(ended.ID, claims[ended.ID].Token, "", start); err != nil {
		t.Fatal(err)
	}
	if run, err := store.Cancel(pending.ID, "a", start); err != nil || run.Status != Cancelled {
		t.Fatalf("a pending run: %+v %v", run, err)
	}
	if run, err := store.Cancel(running.ID, "", start); err != nil || run.Status != Cancelling {
		t.Fatalf("a running run: %+v %v", run, err)
	}
	if run, err := store.Cancel(running.ID, "", start); err != nil || run.Status != Cancelling {
		t.Fatalf("a run being cancelled: %+v %v", run, err)
	}
	var state *StateError
	if _, err := store.Cancel(ended.ID, "", start); !errors.As(err, &state) || state.Run.Status != Completed {
		t.Fatalf("a run that ended: %v", err)
	}
	if _, err := store.Cancel(running.ID, "b", start); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another job's run: %v", err)
	}
	if _, err := store.Cancel("r_none", "", start); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no run: %v", err)
	}
	if status, holds, err := store.Extend(running.ID, claims[running.ID].Token, start.Add(time.Hour)); err != nil || !holds || status != Cancelling {
		t.Fatalf("the heartbeat hears the cancel: %s %v %v", status, holds, err)
	}
}

func TestADueTimeIsStoredOnce(t *testing.T) {
	store := openStore(t, KindSchedule)
	if err := store.SyncSchedules([]ScheduleState{{Name: "nhip", Rule: "every 1m", DeclaredAt: start, LastAt: start}}, nil, start); err != nil {
		t.Fatal(err)
	}
	due := func(at time.Time, skipBusy bool) (Run, string) {
		t.Helper()
		run, outcome, err := store.Dispatch(at, NewRun{Name: "nhip", Data: "{}", Key: Stamp(at), MaxAttempts: 1, Available: at}, skipBusy, at)
		if err != nil {
			t.Fatal(err)
		}
		return run, outcome
	}
	if _, outcome := due(start, true); outcome != Held {
		t.Fatalf("a due time at the last handled: %s", outcome)
	}
	first, outcome := due(start.Add(time.Minute), true)
	if outcome != Dispatched || first.Status != Pending || first.Key != Stamp(start.Add(time.Minute)) {
		t.Fatalf("the next due time: %s %+v", outcome, first)
	}
	if _, outcome := due(start.Add(2*time.Minute), true); outcome != Busy {
		t.Fatalf("a due time while a run has not ended: %s", outcome)
	}
	if _, outcome := due(start.Add(2*time.Minute), true); outcome != Held {
		t.Fatalf("a due time passed over is handled: %s", outcome)
	}
	if _, outcome := due(start.Add(3*time.Minute), false); outcome != Dispatched {
		t.Fatalf("a once() is never passed over: %s", outcome)
	}
	states, err := store.ScheduleStates()
	if err != nil || !states["nhip"].LastAt.Equal(start.Add(3*time.Minute)) || !states["nhip"].DeclaredAt.Equal(start) {
		t.Fatalf("the state: %+v %v", states, err)
	}
	if err := store.SyncSchedules([]ScheduleState{{Name: "raMat", Rule: "once", DeclaredAt: start, OnceAt: start.Add(time.Hour), LastAt: start}}, nil, start); err != nil {
		t.Fatal(err)
	}
	if states, _ := store.ScheduleStates(); !states["raMat"].OnceAt.Equal(start.Add(time.Hour)) || !states["nhip"].OnceAt.IsZero() {
		t.Fatalf("the due time a once() chose: %+v", states)
	}
	if _, _, err := store.Dispatch(start, NewRun{Name: "gone", Available: start}, true, start); !errors.Is(err, ErrNoSchedule) {
		t.Fatalf("a schedule not synced: %v", err)
	}
	if err := store.SyncSchedules(nil, []string{"nhip"}, start); err != nil {
		t.Fatal(err)
	}
	if states, _ := store.ScheduleStates(); len(states) != 1 || states["raMat"].Name != "raMat" {
		t.Fatalf("a schedule removed, the other kept: %+v", states)
	}
}

func TestTheDefinitionsAreWhatTheFilesDeclare(t *testing.T) {
	store := openStore(t, KindQueue)
	type definition struct {
		Definition
		updated string
	}
	read := func() map[string]definition {
		t.Helper()
		out := map[string]definition{}
		if err := store.database.Read(func(db Querier) error {
			rows, err := db.Query(`SELECT name, source, retry, timeout, timeout_ms, content_hash, updated_at FROM kitwork_definitions WHERE kind = ?`, KindQueue)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var each definition
				var timeout *string
				if err := rows.Scan(&each.Name, &each.Source, &each.Retry, &timeout, &each.TimeoutMS, &each.ContentHash, &each.updated); err != nil {
					return err
				}
				if timeout != nil {
					each.Timeout = *timeout
				}
				out[each.Name] = each
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if err := store.Sync([]Definition{{Name: "a", Source: "a.work", Retry: 1, Timeout: "30s", TimeoutMS: 30000, ContentHash: "1"}, {Name: "b", Source: "b.work", ContentHash: "1"}}, start); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 2 || got["a"].Timeout != "30s" || got["a"].Retry != 1 || got["b"].Timeout != "" {
		t.Fatalf("first sync: %+v", got)
	}
	// a definition unchanged is not written again
	if err := store.Sync([]Definition{{Name: "a", Source: "a.work", Retry: 1, Timeout: "30s", TimeoutMS: 30000, ContentHash: "1"}, {Name: "b", Source: "b.work", ContentHash: "1"}}, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := read(); got["a"].updated != Stamp(start) {
		t.Fatalf("a definition unchanged: %+v", got)
	}
	if err := store.Sync([]Definition{{Name: "a", Source: "a.work", Retry: 3, ContentHash: "2"}}, start); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 1 || got["a"].Retry != 3 || got["a"].ContentHash != "2" {
		t.Fatalf("second sync: %+v", got)
	}
}

func TestPruneTakesOnlyEndedRunsPastKeep(t *testing.T) {
	store := openStore(t, KindQueue)
	old, recent, waiting := enqueue(t, store, "a", 1, start), enqueue(t, store, "a", 1, start), enqueue(t, store, "a", 1, start.Add(time.Hour))
	claims := map[string]Claim{}
	for _, each := range claim(t, store, start, time.Minute, 10) {
		claims[each.ID] = each
	}
	if _, err := store.Complete(old.ID, claims[old.ID].Token, "", start); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(recent.ID, claims[recent.ID].Token, "", start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Prune(start.Add(time.Hour)); err != nil || count != 1 {
		t.Fatalf("prune: %d %v", count, err)
	}
	runs, err := store.Runs(Filter{}, 10, 0)
	if err != nil || len(runs) != 2 || runs[0].ID != waiting.ID || runs[1].ID != recent.ID {
		t.Fatalf("what is left, newest first: %+v %v", runs, err)
	}
	if runs, _ := store.Runs(Filter{Status: Pending}, 10, 0); len(runs) != 1 || runs[0].ID != waiting.ID {
		t.Fatalf("by status: %+v", runs)
	}
	if runs, _ := store.Runs(Filter{}, 1, 1); len(runs) != 1 || runs[0].ID != recent.ID {
		t.Fatalf("a page: %+v", runs)
	}
	if runs, _ := store.Runs(Filter{Name: "b"}, 10, 0); len(runs) != 0 {
		t.Fatalf("another job's: %+v", runs)
	}
}
