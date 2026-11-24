package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// datastore() is the app's own store at .database/storage/ (mục 2): a database of its own, not a table of the app's
// database, so db never reads it and it is never part of a transaction of db. It is the SQLite file
// .database/storage/kitwork.db, written by the file's one writer of the process (KitDB, library/database Acquire,
// which keeps its history), one row of kitwork_storage a value: its key, its encoded value and when it expires, in
// milliseconds since the epoch (NULL for never). The first engine's table, row for row, so what it kept reads here. An
// expired value is gone for every read, and is removed when the store would be full and something kept has
// expired; full, a write fails and nothing kept is pushed out. A read of a store never written makes no file.

// Opener opens the file at path through its one writer of the process, to be let go by the function it gives
// (library/database Acquire): datastore() writes its file as the app's databases are written.
type Opener func(path string) (*kitdb.Database, func(), error)

// datastoreFile is the file of datastore(), from the app's folder.
var datastoreFile = filepath.Join(".database", "storage", "kitwork.db")

// datastoreSchema makes the table of datastore(), with the index a sweep finds what has expired by: a plain rowid
// table, no trigger, what the writer keeps the history of.
var datastoreSchema = []string{
	`CREATE TABLE IF NOT EXISTS kitwork_storage (key BLOB NOT NULL PRIMARY KEY, value BLOB NOT NULL, expire INTEGER)`,
	`CREATE INDEX IF NOT EXISTS kitwork_storage_expire ON kitwork_storage (expire) WHERE expire IS NOT NULL`,
}

// writer names the writes of datastore() in the history.
const writer = "kitwork/storage"

// datastorePlace is datastore() of one app: the file's writer once opened, whether its table is there, and the
// count of its keys and bytes the ceilings are measured on, kept by its writes in this process (the file has one
// writer, the process's). Expired values are counted as kept until a sweep removes them.
type datastorePlace struct {
	app      string
	open     Opener
	maxKeys  int
	maxBytes int64

	mu       sync.Mutex
	database *kitdb.Database
	release  func()
	ready    bool
	counted  bool
	keys     int
	bytes    int64
	closed   bool
}

// path is the file, refused when .database or .database/storage of the app is a link: KitDB opens by path, and a
// link would lead into another app's folder.
func (d *datastorePlace) path() (string, error) {
	folder := d.app
	for _, part := range []string{".database", "storage"} {
		folder = filepath.Join(folder, part)
		info, err := os.Lstat(folder)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", unavailable(err.Error())
		}
		if !info.IsDir() { // Lstat follows no link: a symlink, a junction of Windows, is no folder here
			return "", errOutsideStore
		}
	}
	return filepath.Join(d.app, datastoreFile), nil
}

var errOutsideStore = errors.New(".database/storage/ of the app is a link or not a folder: nothing is read or written there (KITWORK_STORAGE.md mục 9)")

// writerOf is the file's writer, opened on first use. With create false, a file never made, or one without the
// table, is nil with no error (it holds nothing) and nothing is made; with create true the table is made first.
// Called with mu held.
func (d *datastorePlace) writerOf(create bool) (*kitdb.Database, error) {
	if d.closed {
		return nil, unavailable("the app is stopping")
	}
	if d.database == nil {
		path, err := d.path()
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) && !create {
			return nil, nil
		}
		database, release, err := d.open(path)
		if err != nil {
			return nil, unavailable("cannot open .database/storage/ of the app: " + err.Error())
		}
		d.database, d.release = database, release
	}
	if d.ready {
		return d.database, nil
	}
	var held int
	err := d.database.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		rows, err := snapshot.Query(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'kitwork_storage'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&held); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if held == 0 {
		if !create {
			return nil, nil
		}
		if _, err := d.database.Migrate(context.Background(), writer, datastoreSchema...); err != nil {
			return nil, err
		}
	}
	d.ready = true
	return d.database, nil
}

// row is the kept row of key: its value, when it expires (zero for never), whether there is one.
func (d *datastorePlace) row(key string) ([]byte, time.Time, bool, error) {
	d.mu.Lock()
	database, err := d.writerOf(false)
	d.mu.Unlock()
	if err != nil || database == nil {
		return nil, time.Time{}, false, err
	}
	var data []byte
	var expire sql.NullInt64
	found := false
	err = database.Read(context.Background(), func(snapshot *kitdb.Snapshot) error {
		rows, err := snapshot.Query(`SELECT value, expire FROM kitwork_storage WHERE key = ?`, []byte(key))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&data, &expire); err != nil {
				return err
			}
			found = true
		}
		return rows.Err()
	})
	if err != nil || !found {
		return nil, time.Time{}, false, err
	}
	var expireAt time.Time
	if expire.Valid {
		expireAt = time.UnixMilli(expire.Int64)
	}
	if expired(expireAt, clock()) {
		return nil, time.Time{}, false, nil
	}
	return data, expireAt, true, nil
}

func (d *datastorePlace) get(key string) ([]byte, bool, error) {
	data, _, found, err := d.row(key)
	return data, found, err
}

func (d *datastorePlace) expiry(key string) (time.Time, bool, error) {
	_, expireAt, found, err := d.row(key)
	return expireAt, found, err
}

func (d *datastorePlace) set(key string, data []byte, expireAt time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	database, err := d.writerOf(true)
	if err != nil {
		return err
	}
	var expire any // NULL for never
	if !expireAt.IsZero() {
		expire = expireAt.UnixMilli()
	}
	keys, size := d.keys, d.bytes
	_, err = database.Write(context.Background(), writer, func(tx *kitdb.Transaction) error {
		if err := d.count(tx); err != nil {
			return err
		}
		measure := func() error {
			previous, err := sizeOf(tx, key)
			if err != nil {
				return err
			}
			keys, size = d.keys, d.bytes+int64(len(key)+len(data))
			if previous < 0 {
				keys++
			} else {
				size -= previous
			}
			return nil
		}
		if err := measure(); err != nil {
			return err
		}
		if keys > d.maxKeys || size > d.maxBytes {
			// what has expired is gone already: removed before refusing
			if _, err := tx.Exec(`DELETE FROM kitwork_storage WHERE expire IS NOT NULL AND expire <= ?`, clock().UnixMilli()); err != nil {
				return err
			}
			d.counted = false
			if err := d.count(tx); err != nil {
				return err
			}
			if err := measure(); err != nil {
				return err
			}
			if keys > d.maxKeys || size > d.maxBytes {
				return full("datastore() of the app is at its ceiling of " + strconv.Itoa(d.maxKeys) + " keys and " + strconv.FormatInt(d.maxBytes, 10) + " bytes: the write fails, nothing kept is removed (KITWORK_STORAGE.md mục 9)")
			}
		}
		_, err := tx.Exec(`INSERT INTO kitwork_storage (key, value, expire) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, expire = excluded.expire`, []byte(key), data, expire)
		return err
	})
	if err != nil {
		d.counted = false // the transaction is gone, and the count it may have read
		return err
	}
	d.keys, d.bytes = keys, size
	return nil
}

// count reads what the table holds, once per process: from then on each write keeps the count.
func (d *datastorePlace) count(tx *kitdb.Transaction) error {
	if d.counted {
		return nil
	}
	rows, err := tx.Query(`SELECT count(*), coalesce(sum(length(key) + length(value)), 0) FROM kitwork_storage`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&d.keys, &d.bytes); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	d.counted = true
	return nil
}

// sizeOf is the bytes the row of key takes, -1 when there is none.
func sizeOf(tx *kitdb.Transaction, key string) (int64, error) {
	rows, err := tx.Query(`SELECT length(key) + length(value) FROM kitwork_storage WHERE key = ?`, []byte(key))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	size := int64(-1)
	for rows.Next() {
		if err := rows.Scan(&size); err != nil {
			return 0, err
		}
	}
	return size, rows.Err()
}

func (d *datastorePlace) remove(key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	database, err := d.writerOf(false)
	if err != nil || database == nil {
		return false, err
	}
	removed := false
	var size int64
	_, err = database.Write(context.Background(), writer, func(tx *kitdb.Transaction) error {
		if err := d.count(tx); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT expire, length(key) + length(value) FROM kitwork_storage WHERE key = ?`, []byte(key))
		if err != nil {
			return err
		}
		var expire sql.NullInt64
		held := false
		for rows.Next() {
			if err := rows.Scan(&expire, &size); err != nil {
				rows.Close()
				return err
			}
			held = true
		}
		rows.Close()
		if !held {
			return nil
		}
		removed = !expire.Valid || !expired(time.UnixMilli(expire.Int64), clock())
		_, err = tx.Exec(`DELETE FROM kitwork_storage WHERE key = ?`, []byte(key))
		if err == nil {
			d.keys, d.bytes = d.keys-1, d.bytes-size
		}
		return err
	})
	if err != nil {
		d.counted = false
		return false, err
	}
	return removed, nil
}

func (d *datastorePlace) clear(branch string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	database, err := d.writerOf(false)
	if err != nil || database == nil {
		return 0, err
	}
	cleared := 0
	_, err = database.Write(context.Background(), writer, func(tx *kitdb.Transaction) error {
		rows, err := tx.Query(`SELECT count(*) FROM kitwork_storage WHERE substr(key, 1, ?) = ? AND (expire IS NULL OR expire > ?)`,
			len(branch), []byte(branch), clock().UnixMilli())
		if err != nil {
			return err
		}
		for rows.Next() {
			if err := rows.Scan(&cleared); err != nil {
				rows.Close()
				return err
			}
		}
		rows.Close()
		_, err = tx.Exec(`DELETE FROM kitwork_storage WHERE substr(key, 1, ?) = ?`, len(branch), []byte(branch))
		return err
	})
	d.counted = false // counted again at the next write
	return cleared, err
}

// close lets the file's writer go.
func (d *datastorePlace) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.release != nil {
		d.release()
		d.release, d.database = nil, nil
	}
}
