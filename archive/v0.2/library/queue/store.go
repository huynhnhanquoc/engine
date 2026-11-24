package queue

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The runs of an app's background work, kept in kitwork_runs of the app's own database (docs/KITWORK_QUEUE.md mục 2,
// 11): one row per run, written before send() answers, claimed with a lease and a token, settled only by the holder of
// the token (fencing), kept for keep after it ends. Beside it kitwork_definitions holds what each job was declared
// with, and kitwork_schedules the state of each schedule (the rule it was synced with, the due time a once() chose, the
// last due time handled), so a restart neither runs a due time twice nor chooses another "next" for a once().
//
// Both kinds of run share the table, 'queue' and 'schedule' in its kind column (docs/KITWORK_SCHEDULE.md mục 9), so the
// two share the runs, the lease and the retry without sharing a namespace. The tables are the first engine's, column
// for column (utilities/queue), so what it kept in .database/application/kitwork.db reads here. Times are text of one
// width (2026-10-22T08:30:00.000Z), so text order is time order. A write is one writer at a time (KitDB's one writer of
// the file), which makes a read and the write after it one step: a claim, a settle, a cancel and a replay check the
// row and change it with nothing in between.

// Querier reads: a snapshot of the file, or a transaction of it.
type Querier interface {
	Query(query string, arguments ...any) (*sql.Rows, error)
}

// SQL is what a write runs on: one transaction of the file.
type SQL interface {
	Querier
	Exec(query string, arguments ...any) (sql.Result, error)
}

// Database is where the runs are kept. Write runs one write at a time, every other write of the file held back while
// it runs: what one Write reads and then writes is one step.
type Database interface {
	Read(read func(Querier) error) error
	Write(write func(SQL) error) error
}

// row is the first row of a query, as QueryRow gives it: sql.ErrNoRows when there is none.
type row struct {
	rows *sql.Rows
	err  error
}

func queryRow(db Querier, query string, arguments ...any) row {
	rows, err := db.Query(query, arguments...)
	return row{rows: rows, err: err}
}

func (r row) Scan(destination ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := r.rows.Scan(destination...); err != nil {
		return err
	}
	return r.rows.Close()
}

// The statuses of a run (docs/KITWORK_QUEUE.md mục 7).
const (
	Pending    = "pending"
	Running    = "running"
	Completed  = "completed"
	Failed     = "failed"
	Cancelling = "cancelling"
	Cancelled  = "cancelled"
)

// Statuses is every status a run can have, in the order of its life.
var Statuses = []string{Pending, Running, Completed, Failed, Cancelling, Cancelled}

// ErrNotFound is a run this kind has no row of: never sent, pruned by keep, or another job's.
var ErrNotFound = errors.New("queue: run not found")

// StateError is an operation the run's status refuses: cancel of a run that ended, replay of one that has not.
type StateError struct {
	Run Run
}

func (e *StateError) Error() string {
	return fmt.Sprintf("queue: run %s is %s", e.Run.ID, e.Run.Status)
}

// Run is one row of kitwork_runs. Times are the stored text ("" when not set).
type Run struct {
	ID           string
	Name         string
	Key          string
	Status       string
	Attempt      int
	MaxAttempts  int
	TimeoutMS    int
	Data         string // JSON, as sent
	Log          string
	ErrorCode    string
	ErrorMessage string
	CreatedAt    string
	StartedAt    string
	FinishedAt   string
	AvailableAt  string
	ReplayOf     string
}

// Claim is a run this worker won the right to run: Token is the lease it holds, which every settle names.
type Claim struct {
	Run
	Token string
}

// NewRun is what send(), now() and replay() store, due at Available.
type NewRun struct {
	Name        string
	Data        string
	Key         string
	MaxAttempts int
	Timeout     time.Duration
	Available   time.Time
	ReplayOf    string
}

// Definition is one job or schedule as declared: its name, its file, the extra attempts and the time limit written.
type Definition struct {
	Name        string
	Source      string
	Retry       int
	Timeout     string // as written ("30s"), "" when none
	TimeoutMS   int
	ContentHash string
}

// Store is the runs of one kind in one database.
type Store struct {
	database Database
	kind     string
}

// NewStore is the store of kind's runs in database.
func NewStore(database Database, kind string) *Store {
	return &Store{database: database, kind: kind}
}

// stampLayout is how the tables write a time.
const stampLayout = "2006-01-02T15:04:05.000Z"

// Stamp is a time as the tables keep it: UTC to the millisecond, one width.
func Stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

// Schema is the statements that make the tables, each nothing when its table is there.
var Schema = []string{
	`CREATE TABLE IF NOT EXISTS kitwork_runs (
		id TEXT PRIMARY KEY,
		seq INTEGER NOT NULL,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		dedupe_key TEXT,
		status TEXT NOT NULL,
		attempt INTEGER NOT NULL,
		max_attempts INTEGER NOT NULL,
		timeout_ms INTEGER NOT NULL,
		data TEXT,
		log TEXT,
		error_code TEXT,
		error_message TEXT,
		available_at TEXT NOT NULL,
		lease_token TEXT,
		lease_until TEXT,
		created_at TEXT NOT NULL,
		started_at TEXT,
		finished_at TEXT,
		replay_of TEXT,
		UNIQUE (kind, name, dedupe_key))`,
	`CREATE INDEX IF NOT EXISTS kitwork_runs_due ON kitwork_runs (kind, status, available_at)`,
	`CREATE INDEX IF NOT EXISTS kitwork_runs_seq ON kitwork_runs (seq)`,
	`CREATE TABLE IF NOT EXISTS kitwork_definitions (
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		source TEXT NOT NULL,
		retry INTEGER NOT NULL,
		timeout TEXT,
		timeout_ms INTEGER NOT NULL,
		content_hash TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (kind, name))`,
	`CREATE TABLE IF NOT EXISTS kitwork_schedules (
		name TEXT PRIMARY KEY,
		rule TEXT NOT NULL,
		declared_at TEXT NOT NULL,
		once_at TEXT,
		last_at TEXT NOT NULL,
		updated_at TEXT NOT NULL)`,
}

const runColumns = `id, name, dedupe_key, status, attempt, max_attempts, timeout_ms, data, log, error_code,
	error_message, created_at, started_at, finished_at, available_at, replay_of`

type scanner interface {
	Scan(destination ...any) error
}

func scanRun(from scanner, extra ...any) (Run, error) {
	var run Run
	var key, data, log, code, message, started, finished, replayOf sql.NullString
	destination := []any{&run.ID, &run.Name, &key, &run.Status, &run.Attempt, &run.MaxAttempts, &run.TimeoutMS,
		&data, &log, &code, &message, &run.CreatedAt, &started, &finished, &run.AvailableAt, &replayOf}
	if err := from.Scan(append(destination, extra...)...); err != nil {
		return Run{}, err
	}
	run.Key, run.Data, run.Log = key.String, data.String, log.String
	run.ErrorCode, run.ErrorMessage = code.String, message.String
	run.StartedAt, run.FinishedAt, run.ReplayOf = started.String, finished.String, replayOf.String
	return run, nil
}

// nullable is s, or NULL for "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RunID is a new run's id: r_ and 16 letters and digits.
func RunID() string { return "r_" + randomText(10) }

func randomText(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer))
}

func newToken() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

// get reads one run of this kind by id, within whatever the caller holds.
func (s *Store) get(db Querier, id string) (Run, error) {
	run, err := scanRun(queryRow(db, `SELECT `+runColumns+` FROM kitwork_runs WHERE id = ? AND kind = ?`, id, s.kind))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	return run, err
}

// insert stores a run as pending. A key another run of the same job holds stores nothing: the run holding it is given
// back with duplicate true. The UNIQUE (kind, name, dedupe_key) of the table decides, not a read before; a run without
// a key never collides (NULL keys are distinct). seq is the number after the highest of the table, read through its
// index.
func (s *Store) insert(db SQL, run NewRun, now time.Time) (Run, bool, error) {
	var last sql.NullInt64
	if err := queryRow(db, `SELECT seq FROM kitwork_runs ORDER BY seq DESC LIMIT 1`).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, err
	}
	id := RunID()
	result, err := db.Exec(`INSERT INTO kitwork_runs (id, seq, kind, name, dedupe_key, status, attempt, max_attempts,
		timeout_ms, data, available_at, created_at, replay_of)
		VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, name, dedupe_key) DO NOTHING`,
		id, last.Int64+1, s.kind, run.Name, nullable(run.Key), run.MaxAttempts, int(run.Timeout/time.Millisecond),
		run.Data, Stamp(run.Available), Stamp(now), nullable(run.ReplayOf))
	if err != nil {
		return Run{}, false, err
	}
	if inserted, _ := result.RowsAffected(); inserted == 0 {
		held, err := scanRun(queryRow(db, `SELECT `+runColumns+` FROM kitwork_runs WHERE kind = ? AND name = ? AND dedupe_key = ?`,
			s.kind, run.Name, run.Key))
		if err != nil {
			return Run{}, false, fmt.Errorf("kitwork_runs: key %q is held, its run not read: %w", run.Key, err)
		}
		return held, true, nil
	}
	stored, err := s.get(db, id)
	return stored, false, err
}

// Enqueue stores a run, or gives back the run holding its key (duplicate true).
func (s *Store) Enqueue(run NewRun, now time.Time) (Run, bool, error) {
	var stored Run
	var duplicate bool
	err := s.database.Write(func(db SQL) error {
		var err error
		stored, duplicate, err = s.insert(db, run, now)
		return err
	})
	return stored, duplicate, err
}

// Claim takes up to limit runs that are due, oldest due first, each with a new lease token held until now+lease. It
// also gives when the first pending run it left is due, the zero time when it left none: the worker does not look
// again before then, unless a send, a replay or a retry tells it of an earlier one. The reads and the updates are one
// step of the file's one writer.
func (s *Store) Claim(now time.Time, lease time.Duration, limit int) ([]Claim, time.Time, error) {
	if limit < 1 {
		return nil, now, nil
	}
	var claims []Claim
	var next time.Time
	err := s.database.Write(func(db SQL) error {
		rows, err := db.Query(fmt.Sprintf(`SELECT `+runColumns+` FROM kitwork_runs
			WHERE kind = ? AND status = 'pending' AND available_at <= ? ORDER BY available_at, seq LIMIT %d`, limit),
			s.kind, Stamp(now))
		if err != nil {
			return err
		}
		var due []Run
		for rows.Next() {
			run, err := scanRun(rows)
			if err != nil {
				rows.Close()
				return err
			}
			due = append(due, run)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, run := range due {
			token := newToken()
			if _, err := db.Exec(`UPDATE kitwork_runs SET status = 'running', attempt = ?, lease_token = ?,
				lease_until = ?, started_at = ? WHERE id = ?`,
				run.Attempt+1, token, Stamp(now.Add(lease)), Stamp(now), run.ID); err != nil {
				return err
			}
			run.Status, run.Attempt, run.StartedAt = Running, run.Attempt+1, Stamp(now)
			claims = append(claims, Claim{Run: run, Token: token})
		}
		var first string
		err = queryRow(db, `SELECT available_at FROM kitwork_runs WHERE kind = ? AND status = 'pending'
			ORDER BY available_at LIMIT 1`, s.kind).Scan(&first)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		next, err = time.Parse(stampLayout, first)
		return err
	})
	return claims, next, err
}

// held reads a run and reports whether token still holds it: the run is running or cancelling under that lease (every
// settle and every reclaim lets the lease go). A run reclaimed, settled or cancelled since is held by no one, or by
// another token.
func (s *Store) held(db Querier, id, token string) (Run, bool, error) {
	var current sql.NullString
	run, err := scanRun(queryRow(db, `SELECT `+runColumns+`, lease_token FROM kitwork_runs WHERE id = ? AND kind = ?`, id, s.kind), &current)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	if !current.Valid || current.String != token {
		return run, false, nil
	}
	return run, true, nil
}

// Complete records a run that ran to its end, as the holder of token; the run's log is replaced by log. It reports
// whether token still held the run: an attempt whose lease was taken from it (timed out, reclaimed) never reports
// success. A run being cancelled that finished first is completed (mục 7).
func (s *Store) Complete(id, token, log string, now time.Time) (bool, error) {
	won := false
	err := s.database.Write(func(db SQL) error {
		_, held, err := s.held(db, id, token)
		if err != nil || !held {
			return err
		}
		if _, err := db.Exec(`UPDATE kitwork_runs SET status = 'completed', finished_at = ?, log = ?,
			error_code = NULL, error_message = NULL, lease_token = NULL, lease_until = NULL WHERE id = ?`, Stamp(now), log, id); err != nil {
			return err
		}
		won = true
		return nil
	})
	return won, err
}

// FailAttempt records an attempt that failed, as the holder of token. A run with attempts left goes back to pending until
// retryAt; one without, or any when retryAt is the zero time (a failure no attempt mends), ends failed (the dead
// letter: the row stays, with its data and its error); one being cancelled ends cancelled, never retried. code and
// message "" store no error. It returns the status the run took, "" when token no longer held it.
func (s *Store) FailAttempt(id, token, code, message, log string, retryAt, now time.Time) (string, error) {
	status := ""
	err := s.database.Write(func(db SQL) error {
		run, held, err := s.held(db, id, token)
		if err != nil || !held {
			return err
		}
		switch {
		case run.Status == Cancelling:
			status = Cancelled
		case run.Attempt < run.MaxAttempts && !retryAt.IsZero():
			status = Pending
		default:
			status = Failed
		}
		if status == Pending {
			_, err = db.Exec(`UPDATE kitwork_runs SET status = 'pending', available_at = ?, log = ?,
				error_code = ?, error_message = ?, lease_token = NULL, lease_until = NULL WHERE id = ?`,
				Stamp(retryAt), log, nullable(code), nullable(message), id)
		} else {
			_, err = db.Exec(`UPDATE kitwork_runs SET status = ?, finished_at = ?, log = ?,
				error_code = ?, error_message = ?, lease_token = NULL, lease_until = NULL WHERE id = ?`,
				status, Stamp(now), log, nullable(code), nullable(message), id)
		}
		if err != nil {
			status = ""
		}
		return err
	})
	return status, err
}

// Extend moves the lease of a run this worker still holds to until. It reports whether token still holds it, and the
// run's status (cancelling tells the worker to stop the attempt).
func (s *Store) Extend(id, token string, until time.Time) (string, bool, error) {
	status, holds := "", false
	err := s.database.Write(func(db SQL) error {
		run, held, err := s.held(db, id, token)
		if err != nil || !held {
			return err
		}
		if _, err := db.Exec(`UPDATE kitwork_runs SET lease_until = ? WHERE id = ?`, Stamp(until), id); err != nil {
			return err
		}
		status, holds = run.Status, true
		return nil
	})
	return status, holds, err
}

// Reclaim gives back the runs whose lease ran out, leaving those in skip (the attempts this process still runs and
// extends). A run with attempts left is due again now; one without ends failed; one being cancelled ends cancelled.
// The attempt that lost its lease counts: it ran, and a run that stops its process every time must not run for ever.
// It returns how many runs it gave back, and when the first lease still held by no attempt in skip runs out (the zero
// time when none is held): a process that stopped while it ran an attempt leaves a lease that has not run out yet when
// the next one boots.
func (s *Store) Reclaim(now time.Time, skip map[string]bool) (int, time.Time, error) {
	count := 0
	var next time.Time
	err := s.database.Write(func(db SQL) error {
		rows, err := db.Query(`SELECT `+runColumns+`, lease_until FROM kitwork_runs
			WHERE kind = ? AND status IN ('running', 'cancelling')`, s.kind)
		if err != nil {
			return err
		}
		var lost []Run
		for rows.Next() {
			var until sql.NullString
			run, err := scanRun(rows, &until)
			if err != nil {
				rows.Close()
				return err
			}
			if skip[run.ID] {
				continue
			}
			if until.Valid && until.String >= Stamp(now) {
				if expires, err := time.Parse(stampLayout, until.String); err == nil && (next.IsZero() || expires.Before(next)) {
					next = expires
				}
				continue
			}
			lost = append(lost, run)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, run := range lost {
			message := fmt.Sprintf("lần thử %d mất lease (engine dừng hay treo khi đang chạy)", run.Attempt)
			switch {
			case run.Status == Cancelling:
				_, err = db.Exec(`UPDATE kitwork_runs SET status = 'cancelled', finished_at = ?,
					lease_token = NULL, lease_until = NULL WHERE id = ?`, Stamp(now), run.ID)
			case run.Attempt < run.MaxAttempts:
				_, err = db.Exec(`UPDATE kitwork_runs SET status = 'pending', available_at = ?,
					error_code = 'TIMEOUT', error_message = ?, lease_token = NULL, lease_until = NULL WHERE id = ?`, Stamp(now), message, run.ID)
			default:
				_, err = db.Exec(`UPDATE kitwork_runs SET status = 'failed', finished_at = ?,
					error_code = 'TIMEOUT', error_message = ?, lease_token = NULL, lease_until = NULL WHERE id = ?`, Stamp(now), message, run.ID)
			}
			if err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, next, err
}

// Cancel cancels a run (mục 9). A pending run ends cancelled at once; a running one becomes cancelling, for its worker
// to stop; a cancelling or cancelled one is given back as it is. A run that ended (completed, failed) is a
// *StateError. name, when not "", is the job the run must belong to: another job's run is ErrNotFound, as an id that
// does not exist.
func (s *Store) Cancel(id, name string, now time.Time) (Run, error) {
	var run Run
	err := s.database.Write(func(db SQL) error {
		var err error
		run, err = s.get(db, id)
		if err != nil {
			return err
		}
		if name != "" && run.Name != name {
			return ErrNotFound
		}
		switch run.Status {
		case Pending:
			if _, err := db.Exec(`UPDATE kitwork_runs SET status = 'cancelled', finished_at = ? WHERE id = ?`, Stamp(now), id); err != nil {
				return err
			}
		case Running:
			if _, err := db.Exec(`UPDATE kitwork_runs SET status = 'cancelling' WHERE id = ?`, id); err != nil {
				return err
			}
		case Cancelling, Cancelled:
			return nil
		default:
			return &StateError{Run: run}
		}
		run, err = s.get(db, id)
		return err
	})
	return run, err
}

// ErrNoDefinition is a job no file declares any more: its runs cannot be replayed.
var ErrNoDefinition = errors.New("queue: job no longer declared")

// Replay makes a new run from the data of an ended one (failed, cancelled, completed), keeping the old one: its own
// id, attempts and retries, from the job's definition now, which declared gives (false when the app declares the job
// no more: ErrNoDefinition). A run not ended is a *StateError. name, when not "", is the job the run must belong to.
func (s *Store) Replay(id, name string, now time.Time, declared func(name string) (NewRun, bool)) (Run, string, error) {
	var replayed Run
	of := ""
	err := s.database.Write(func(db SQL) error {
		run, err := s.get(db, id)
		if err != nil {
			return err
		}
		of = run.Name
		if name != "" && run.Name != name {
			return ErrNotFound
		}
		if run.Status != Failed && run.Status != Cancelled && run.Status != Completed {
			return &StateError{Run: run}
		}
		again, ok := declared(run.Name)
		if !ok {
			return ErrNoDefinition
		}
		again.Name, again.Data, again.ReplayOf, again.Available = run.Name, run.Data, run.ID, now
		replayed, _, err = s.insert(db, again, now)
		return err
	})
	return replayed, of, err
}

// Filter chooses runs: of one job, in one status ("" for any).
type Filter struct {
	Name   string
	Status string
}

// Runs reads runs newest first, limit of them after skipping offset.
func (s *Store) Runs(filter Filter, limit, offset int) ([]Run, error) {
	var runs []Run
	err := s.database.Read(func(db Querier) error {
		where := []string{"kind = ?"}
		arguments := []any{s.kind}
		if filter.Name != "" {
			where, arguments = append(where, "name = ?"), append(arguments, filter.Name)
		}
		if filter.Status != "" {
			where, arguments = append(where, "status = ?"), append(arguments, filter.Status)
		}
		rows, err := db.Query(fmt.Sprintf(`SELECT `+runColumns+` FROM kitwork_runs WHERE %s ORDER BY seq DESC LIMIT %d OFFSET %d`,
			strings.Join(where, " AND "), limit, offset), arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			run, err := scanRun(rows)
			if err != nil {
				return err
			}
			runs = append(runs, run)
		}
		return rows.Err()
	})
	return runs, err
}

// Prune deletes the runs that ended before before (keep; a run has finished_at once it ended, and only then): their keys
// are free again, and replay of them is refused.
// It returns how many it deleted.
func (s *Store) Prune(before time.Time) (int, error) {
	count := 0
	err := s.database.Write(func(db SQL) error {
		result, err := db.Exec(`DELETE FROM kitwork_runs WHERE kind = ? AND finished_at < ?`, s.kind, Stamp(before))
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		count = int(affected)
		return nil
	})
	return count, err
}

// Sync makes kitwork_definitions what the files declare: a definition whose content hash is unchanged is not written
// again, one no file declares any more is deleted. Runs are never touched: a pending run of a job that is gone stays,
// and fails when claimed, saying so.
func (s *Store) Sync(definitions []Definition, now time.Time) error {
	return s.database.Write(func(db SQL) error {
		stored := map[string]string{}
		rows, err := db.Query(`SELECT name, content_hash FROM kitwork_definitions WHERE kind = ?`, s.kind)
		if err != nil {
			return err
		}
		for rows.Next() {
			var name, hash string
			if err := rows.Scan(&name, &hash); err != nil {
				rows.Close()
				return err
			}
			stored[name] = hash
		}
		if err := rows.Close(); err != nil {
			return err
		}
		declared := map[string]bool{}
		for _, definition := range definitions {
			declared[definition.Name] = true
			if hash, found := stored[definition.Name]; found && hash == definition.ContentHash {
				continue
			}
			if _, err := db.Exec(`INSERT INTO kitwork_definitions (kind, name, source, retry, timeout, timeout_ms,
				content_hash, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (kind, name) DO UPDATE SET source = excluded.source, retry = excluded.retry,
				timeout = excluded.timeout, timeout_ms = excluded.timeout_ms, content_hash = excluded.content_hash,
				updated_at = excluded.updated_at`,
				s.kind, definition.Name, definition.Source, definition.Retry, nullable(definition.Timeout), definition.TimeoutMS,
				definition.ContentHash, Stamp(now)); err != nil {
				return fmt.Errorf("kitwork_definitions %s: %w", definition.Name, err)
			}
		}
		gone := make([]string, 0, len(stored))
		for name := range stored {
			if !declared[name] {
				gone = append(gone, name)
			}
		}
		sort.Strings(gone)
		for _, name := range gone {
			if _, err := db.Exec(`DELETE FROM kitwork_definitions WHERE kind = ? AND name = ?`, s.kind, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// ScheduleState is one schedule's row of kitwork_schedules. Times are UTC; OnceAt is zero for a schedule that is not a
// once().
type ScheduleState struct {
	Name string
	// Rule is the rule as written, its zone with it; another rule is another schedule's state.
	Rule       string
	DeclaredAt time.Time
	OnceAt     time.Time
	// LastAt is the last due time handled: stored as a run, or passed over because a run of the schedule had not
	// ended. A due time at or before it is never stored again.
	LastAt time.Time
}

// ScheduleStates reads the state of every schedule, by name.
func (s *Store) ScheduleStates() (map[string]ScheduleState, error) {
	states := map[string]ScheduleState{}
	err := s.database.Read(func(db Querier) error {
		rows, err := db.Query(`SELECT name, rule, declared_at, once_at, last_at FROM kitwork_schedules`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var state ScheduleState
			var declared, last string
			var once sql.NullString
			if err := rows.Scan(&state.Name, &state.Rule, &declared, &once, &last); err != nil {
				return err
			}
			if state.DeclaredAt, err = time.Parse(stampLayout, declared); err != nil {
				return err
			}
			if state.LastAt, err = time.Parse(stampLayout, last); err != nil {
				return err
			}
			if once.Valid {
				if state.OnceAt, err = time.Parse(stampLayout, once.String); err != nil {
					return err
				}
			}
			states[state.Name] = state
		}
		return rows.Err()
	})
	return states, err
}

// SyncSchedules writes the states in put (a schedule declared for the first time, or with another rule) and deletes
// those of the names in remove (no file declares them any more), in one write.
func (s *Store) SyncSchedules(put []ScheduleState, remove []string, now time.Time) error {
	if len(put) == 0 && len(remove) == 0 {
		return nil
	}
	return s.database.Write(func(db SQL) error {
		for _, state := range put {
			var once any
			if !state.OnceAt.IsZero() {
				once = Stamp(state.OnceAt)
			}
			if _, err := db.Exec(`INSERT INTO kitwork_schedules (name, rule, declared_at, once_at, last_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (name) DO UPDATE SET rule = excluded.rule, declared_at = excluded.declared_at,
				once_at = excluded.once_at, last_at = excluded.last_at, updated_at = excluded.updated_at`,
				state.Name, state.Rule, Stamp(state.DeclaredAt), once, Stamp(state.LastAt), Stamp(now)); err != nil {
				return fmt.Errorf("kitwork_schedules %s: %w", state.Name, err)
			}
		}
		sorted := append([]string(nil), remove...)
		sort.Strings(sorted)
		for _, name := range sorted {
			if _, err := db.Exec(`DELETE FROM kitwork_schedules WHERE name = ?`, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// The outcomes of Dispatch.
const (
	// Dispatched: the run of the due time is stored.
	Dispatched = "dispatched"
	// Busy: a run of the schedule has not ended (pending, running): the due time is passed over (KITWORK_SCHEDULE.md
	// mục 13, 12.4: one run of a schedule at a time).
	Busy = "busy"
	// Held: the due time has a run already (another process stored it, or a restart in the same due time).
	Held = "held"
)

// ErrNoSchedule is a schedule kitwork_schedules has no row of: its state was not synced.
var ErrNoSchedule = errors.New("queue: schedule not synced")

// Dispatch stores the run of one due time of a schedule, run.Key the due time, and moves the schedule's LastAt to at,
// in one write. A due time at or before LastAt was handled and stores nothing (Held); so does a key held. When
// skipBusy, a run of the schedule that has not ended passes the due time over (Busy), its LastAt moved all the same:
// a schedule that runs longer than its interval does not pile up runs.
func (s *Store) Dispatch(at time.Time, run NewRun, skipBusy bool, now time.Time) (Run, string, error) {
	var stored Run
	outcome := ""
	err := s.database.Write(func(db SQL) error {
		var last string
		err := queryRow(db, `SELECT last_at FROM kitwork_schedules WHERE name = ?`, run.Name).Scan(&last)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoSchedule
		}
		if err != nil {
			return err
		}
		if last >= Stamp(at) {
			outcome = Held
			return nil
		}
		if skipBusy {
			var id string
			err := queryRow(db, `SELECT id FROM kitwork_runs WHERE kind = ? AND name = ?
				AND status IN ('pending', 'running', 'cancelling') LIMIT 1`, s.kind, run.Name).Scan(&id)
			switch {
			case err == nil:
				outcome = Busy
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
		}
		if outcome == "" {
			var duplicate bool
			stored, duplicate, err = s.insert(db, run, now)
			if err != nil {
				return err
			}
			outcome = Dispatched
			if duplicate {
				outcome = Held
			}
		}
		_, err = db.Exec(`UPDATE kitwork_schedules SET last_at = ?, updated_at = ? WHERE name = ?`, Stamp(at), Stamp(now), run.Name)
		return err
	})
	return stored, outcome, err
}
