package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// A file has one writer in the process (KitDB's writer locks the file: a second Open of it is ErrLocked): the
// statements of an app and the host's database port write it through the same one, held by whoever uses it and
// closed when the last lets it go. kitwork migrate, run with the server stopped, opens its own.
//
// The database is the root (docs/KITWORK_DATABASE.md, "Database là gốc"): beside each file its structure, <file>.sql,
// written by the writer whenever the schema changes; a folder holding the .sql and not the file (a clone of the
// repository) gets the file made from it when the writer first opens it, the tables and indexes only.
//
// The history keeps what app.history({ days }) of the manifest says, HistoryDaysDefault days when it says nothing
// (docs/KITWORK_KITDB.md câu 13.6): each writer anchors its file once a day of commits is in no anchor, and takes out
// what no moment of the days kept needs (older anchors, older commits), when it opens and every RetainEvery after.

// HistoryDaysDefault is the days the history keeps when the manifest says nothing.
const HistoryDaysDefault = 30

var historyDays atomic.Int64

// RetainEvery is how often an open writer applies the retention.
var RetainEvery = time.Hour

// Now is the clock the retention reads: a test moves it days ahead to see what the history lets go.
var Now = time.Now

// SetHistoryDays is app.history({ days }) of the host's manifest, set at boot; 0 keeps the default.
func SetHistoryDays(days int) { historyDays.Store(int64(days)) }

// HistoryDays is how many days the history of the databases keeps on this host.
func HistoryDays() int {
	if days := historyDays.Load(); days > 0 {
		return int(days)
	}
	return HistoryDaysDefault
}

func retention() kitdb.Retention {
	return kitdb.Retention{Keep: time.Duration(HistoryDays()) * 24 * time.Hour, Every: 24 * time.Hour}
}

var writers = struct {
	sync.Mutex
	files map[string]*sharedWriter
}{files: map[string]*sharedWriter{}}

type sharedWriter struct {
	database *kitdb.Database
	users    int
	// stop ends the retention of the file, done says it ended
	stop context.CancelFunc
	done chan struct{}
}

// Acquire is the writer of the file at path, opened or shared, until release; a file not there whose .sql is gets
// made from it.
func Acquire(path string) (*kitdb.Database, func(), error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	absolute = filepath.Clean(absolute)
	writers.Lock()
	defer writers.Unlock()
	shared := writers.files[absolute]
	if shared == nil {
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return nil, nil, err
		}
		structure, made := structureToMake(absolute)
		// the history keeps the tables the database declares it of (WITH (history = on), KITWORK_KITDB.md câu 13.11): a
		// file of before keeps every table's, a table made since has none until one is declared
		database, err := kitdb.Open(context.Background(), absolute, kitdb.Options{Structure: structureWriter(absolute)})
		if err != nil {
			return nil, nil, err
		}
		if made {
			if err := loadStructure(database, structure); err != nil {
				_ = database.Close()
				removeMadeFile(absolute) // nothing is in it but what the structure made: the next open tries again
				return nil, nil, fmt.Errorf("%s: tạo database từ %s.sql: %w", filepath.Base(absolute), filepath.Base(absolute), err)
			}
		}
		ctx, stop := context.WithCancel(context.Background())
		shared = &sharedWriter{database: database, stop: stop, done: make(chan struct{})}
		writers.files[absolute] = shared
		go shared.keep(ctx)
	}
	shared.users++
	var once sync.Once
	release := func() {
		once.Do(func() {
			writers.Lock()
			shared.users--
			last := shared.users == 0
			if last {
				delete(writers.files, absolute)
			}
			writers.Unlock()
			if last {
				shared.stop()
				<-shared.done
				_ = shared.database.Close()
			}
		})
	}
	return shared.database, release, nil
}

// MakeFromStructure makes the file at path from its .sql when the file is not there and the .sql is, and says
// whether it did: the boot and kitwork migrate read the database the repository holds the text of.
func MakeFromStructure(path string) (bool, error) {
	if _, made := structureToMake(path); !made {
		return false, nil
	}
	_, release, err := Acquire(path)
	if err != nil {
		return false, err
	}
	release()
	return true, nil
}

// keep applies the retention now and every RetainEvery, until stop.
func (shared *sharedWriter) keep(ctx context.Context) {
	defer close(shared.done)
	ticker := time.NewTicker(RetainEvery)
	defer ticker.Stop()
	for {
		if _, err := shared.database.Retain(ctx, Now(), retention()); err != nil && ctx.Err() == nil && !errors.Is(err, kitdb.ErrClosed) {
			fmt.Printf("[database] the history of %s: %v\n", filepath.Base(shared.database.Path()), err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// structureToMake is the structure file of path when path does not exist yet and its .sql does: the file to make.
func structureToMake(path string) (string, bool) {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	content, err := os.ReadFile(path + ".sql")
	if err != nil {
		return "", false
	}
	return string(content), true
}

// removeMadeFile takes away a file made from its structure that could not be finished, with what SQLite and KitDB
// made beside it; the .sql stays.
func removeMadeFile(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", ".lock"} {
		_ = os.Remove(path + suffix)
	}
	_ = os.RemoveAll(path + ".history")
}
