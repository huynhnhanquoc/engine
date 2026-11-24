package queue

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// Opener opens the file at path through its one writer of the process, to be let go by the function it gives
// (library/database Acquire): the runs are written as the app's own tables are, by the same writer.
type Opener func(path string) (*kitdb.Database, func(), error)

// applicationFile is the app's database, from the app's folder (docs/KITWORK_QUEUE.md mục 2, 16): the file
// connection().tables({ … }) of .shared/schema/ declares the app's tables in, so a run and a row of the app are one
// file.
var applicationFile = filepath.Join(".database", "application", "kitwork.db")

// ErrClosed is a write after the app closed its runs' file.
var ErrClosed = errors.New("kitwork_runs: the app's database is closed")

// errOutside is a .database or .database/application that is a link: KitDB opens by path, and a link would lead into
// another app's folder.
var errOutside = errors.New("kitwork_runs: .database/application of the app is a link, not a folder")

// File is kitwork_runs of one database, opened the first time a run is read or written, its tables made then; writer
// names its writes in the file's history (kitwork/queue, kitwork/schedule).
type File struct {
	path string
	// app is the app whose own database the file is, whose .database/application must hold no link; "" for a file of
	// the host's catalogue, whose path the manifest wrote
	app    string
	open   Opener
	writer string

	mu       sync.Mutex
	database *kitdb.Database
	release  func()
	closed   bool
}

// NewFile is kitwork_runs of the app's own database, the app at app, opened by open, writing as writer.
func NewFile(app string, open Opener, writer string) *File {
	return &File{path: filepath.Join(app, applicationFile), app: app, open: open, writer: writer}
}

// NewFileAt is kitwork_runs of the database at path, one of the host's catalogue (queue.database(db)).
func NewFileAt(path string, open Opener, writer string) *File {
	return &File{path: path, open: open, writer: writer}
}

// Path is the file the runs are in.
func (f *File) Path() string { return f.path }

// handle is the file's writer, opened and its tables made the first time.
func (f *File) handle() (*kitdb.Database, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, ErrClosed
	}
	if f.database != nil {
		return f.database, nil
	}
	folder := f.app
	for _, part := range []string{".database", "application"} {
		if f.app == "" {
			break
		}
		folder = filepath.Join(folder, part)
		info, err := os.Lstat(folder)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() { // Lstat follows no link: a symlink, a junction of Windows, is no folder here
			return nil, errOutside
		}
	}
	database, release, err := f.open(f.path)
	if err != nil {
		return nil, err
	}
	if _, err := database.Migrate(context.Background(), f.writer, Schema...); err != nil {
		release()
		return nil, err
	}
	f.database, f.release = database, release
	return database, nil
}

// Read reads a snapshot of the file.
func (f *File) Read(read func(Querier) error) error {
	database, err := f.handle()
	if err != nil {
		return err
	}
	return database.Read(context.Background(), func(snapshot *kitdb.Snapshot) error { return read(snapshot) })
}

// Write runs write as one transaction of the file's one writer.
func (f *File) Write(write func(SQL) error) error {
	database, err := f.handle()
	if err != nil {
		return err
	}
	_, err = database.Write(context.Background(), f.writer, func(transaction *kitdb.Transaction) error { return write(transaction) })
	return err
}

// Close lets go of the file's writer; a read or a write after it is ErrClosed.
func (f *File) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.release != nil {
		f.release()
		f.database, f.release = nil, nil
	}
}

// owners are the apps that keep their runs in each file of the host's catalogue, by the file: kitwork_runs carries no
// app, so the worker of a second app on one file would claim the first app's runs and fail them as runs of no job. One
// app a file, its queue and its schedules may share it (the first engine's runs_database.go).
var owners = struct {
	sync.Mutex
	files map[string]string
}{files: map[string]string{}}

// sameFile reports two paths of one file, as the paths are written (a link, two spellings Windows reads as one, are
// two).
func sameFile(a, b string) bool {
	first, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	second, err := filepath.Abs(b)
	return err == nil && filepath.Clean(first) == filepath.Clean(second)
}

// ClaimFile makes app (its folder) the app that keeps its runs in the file at path: "" when it is, the app that does
// otherwise.
func ClaimFile(path, app string) string {
	owners.Lock()
	defer owners.Unlock()
	for file, owner := range owners.files {
		if sameFile(file, path) {
			if owner != app {
				return owner
			}
			return ""
		}
	}
	owners.files[path] = app
	return ""
}

// ReleaseFiles gives back every file app keeps its runs in: the app closed.
func ReleaseFiles(app string) {
	owners.Lock()
	defer owners.Unlock()
	for file, owner := range owners.files {
		if owner == app {
			delete(owners.files, file)
		}
	}
}

// Unended counts the runs of kind of the app's own database (the app at app) that have not ended, reading the file
// without writing: a file that does not exist counts none and is not made, one without kitwork_runs counts none and is
// not given the table. What runs stay behind when queue.database(db) puts the runs in another database.
func Unended(app string, open Opener, kind string) (int, error) {
	path := filepath.Join(app, applicationFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	database, release, err := open(path)
	if err != nil {
		return 0, err
	}
	defer release()
	count := 0
	err = database.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		var tables int
		if err := queryRow(snapshot, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'kitwork_runs'`).Scan(&tables); err != nil || tables == 0 {
			return err
		}
		return queryRow(snapshot, `SELECT count(*) FROM kitwork_runs WHERE kind = ? AND status IN ('pending', 'running', 'cancelling')`, kind).Scan(&count)
	})
	return count, err
}
