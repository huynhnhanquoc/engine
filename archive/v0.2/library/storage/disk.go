package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// disk() keeps each value in a file of the app's .database/disk/ (mục 2): the disk of this machine, kept across a
// restart, never served, never deployed. The first engine's layout, file for file, so what it kept reads here: a file
// is named by the SHA-256 of the encoded key, under a folder of its first two hex digits, and holds the key itself,
// so a file of another key is never taken for this one. A write goes to a file beside it renamed over the old one, so
// a read sees the old value or the new one, never half of one. Full, a write fails; nothing kept is pushed out, only
// what has expired is swept first, and only when something kept can have expired since the last sweep.
//
// Every operation opens an os.Root of the app's folder and, through it, of .database/disk, so a name that leads out of
// the app, a link included (a symlink, a junction of Windows), is refused by the system as the operation runs.

// diskMagic begins every file of disk(), with the version of its layout.
var diskMagic = []byte("KWS1")

// diskFolder is disk()'s folder, from the app's folder.
var diskFolder = filepath.Join(".database", "disk")

// diskPlace is disk() of one app. Its lock orders reads before writes in this process (a file renamed while read is
// refused on Windows) and keeps the count of keys and bytes the ceilings are measured on, and when the soonest value
// kept expires.
type diskPlace struct {
	app      string // the app's folder
	maxKeys  int
	maxBytes int64

	mu      sync.RWMutex
	counted bool
	keys    int
	bytes   int64
	// expiryKnown is true once a sweep has read every file; nextExpiry is then when the soonest value kept expires,
	// zero when none does. Before it is past, a sweep would find nothing to remove.
	expiryKnown bool
	nextExpiry  time.Time
}

// errOutside is a path of disk() that leads outside the app's folder.
var errOutside = errors.New(".database/disk/ of the app leads elsewhere through a link: nothing is read or written there (KITWORK_STORAGE.md mục 9)")

// diskError is err of the system as disk() tells it: a name that leads out of the os.Root is errOutside.
func diskError(err error) error {
	if err != nil && strings.Contains(err.Error(), "path escapes from parent") {
		return errOutside
	}
	return err
}

// open opens disk()'s folder through an os.Root of the app's folder. With create false, a folder never made yet is
// nil with no error (it holds nothing); with create true it is made first.
func (d *diskPlace) open(create bool) (*os.Root, error) {
	app, err := os.OpenRoot(d.app)
	if err != nil {
		return nil, unavailable("the app's folder: " + err.Error())
	}
	defer app.Close() // disk()'s root stays open on its own
	root, err := app.OpenRoot(diskFolder)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		if !create {
			return nil, nil
		}
		if err := app.MkdirAll(diskFolder, 0o700); err != nil {
			return nil, diskError(err)
		}
		root, err = app.OpenRoot(diskFolder)
	}
	if err != nil {
		return nil, diskError(err)
	}
	return root, nil
}

// file is where key is kept, from disk()'s folder.
func (d *diskPlace) file(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(name[:2], name)
}

// diskRecord is a file of disk(): its key, when it expires, its value.
type diskRecord struct {
	key      string
	expireAt time.Time
	data     []byte
}

func encodeDiskRecord(record diskRecord) []byte {
	out := make([]byte, 0, len(diskMagic)+8+binary.MaxVarintLen64+len(record.key)+len(record.data))
	out = append(out, diskMagic...)
	var expire int64
	if !record.expireAt.IsZero() {
		expire = math.MaxInt64 // the last instant nanoseconds since the epoch say (year 2262): a later expiry stops there
		if record.expireAt.Before(time.Unix(0, math.MaxInt64)) {
			expire = record.expireAt.UnixNano()
		}
	}
	out = binary.BigEndian.AppendUint64(out, uint64(expire))
	out = binary.AppendUvarint(out, uint64(len(record.key)))
	out = append(out, record.key...)
	return append(out, record.data...)
}

var errDiskRecord = errors.New("a file of disk() is damaged")

func decodeDiskRecord(content []byte) (diskRecord, error) {
	if len(content) < len(diskMagic)+8 || !bytes.Equal(content[:len(diskMagic)], diskMagic) {
		return diskRecord{}, errDiskRecord
	}
	rest := content[len(diskMagic):]
	expire := int64(binary.BigEndian.Uint64(rest))
	rest = rest[8:]
	size, read := binary.Uvarint(rest)
	if read <= 0 || size > uint64(len(rest)-read) {
		return diskRecord{}, errDiskRecord
	}
	record := diskRecord{key: string(rest[read : read+int(size)]), data: rest[read+int(size):]}
	if expire != 0 {
		record.expireAt = time.Unix(0, expire)
	}
	return record, nil
}

// lookup is the record of key read off disk()'s folder already open (nil: never made, so nothing is kept); found is
// false for none, expired true for one that has expired.
func (d *diskPlace) lookup(root *os.Root, key string) (record diskRecord, found, gone bool, err error) {
	if root == nil {
		return diskRecord{}, false, false, nil
	}
	content, err := root.ReadFile(d.file(key))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return diskRecord{}, false, false, nil
	}
	if err != nil {
		return diskRecord{}, false, false, diskError(err)
	}
	record, err = decodeDiskRecord(content)
	if err != nil || record.key != key {
		return diskRecord{}, false, false, nil // another key's file under the same name, or damage: not this key's value
	}
	if expired(record.expireAt, clock()) {
		return record, false, true, nil
	}
	return record, true, false, nil
}

func (d *diskPlace) read(key string) (diskRecord, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	root, err := d.open(false)
	if err != nil {
		return diskRecord{}, false, err
	}
	if root != nil {
		defer root.Close()
	}
	record, found, _, err := d.lookup(root, key)
	return record, found, err
}

func (d *diskPlace) get(key string) ([]byte, bool, error) {
	record, found, err := d.read(key)
	return record.data, found, err
}

func (d *diskPlace) expiry(key string) (time.Time, bool, error) {
	record, found, err := d.read(key)
	return record.expireAt, found, err
}

func (d *diskPlace) set(key string, data []byte, expireAt time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	root, err := d.open(true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := d.count(root); err != nil {
		return err
	}
	content := encodeDiskRecord(diskRecord{key: key, expireAt: expireAt, data: data})
	name := d.file(key)
	measure := func() (int, int64, error) {
		keys, size := d.keys, d.bytes+int64(len(content))
		info, err := root.Stat(name)
		switch {
		case err == nil:
			size -= info.Size()
		case errors.Is(diskError(err), errOutside):
			return 0, 0, errOutside
		default:
			keys++
		}
		return keys, size, nil
	}
	keys, size, err := measure()
	if err != nil {
		return err
	}
	if keys > d.maxKeys || size > d.maxBytes {
		if sweepDue(d.expiryKnown, d.nextExpiry) {
			if err := d.sweep(root); err != nil {
				return err
			}
			if keys, size, err = measure(); err != nil {
				return err
			}
		}
		if keys > d.maxKeys || size > d.maxBytes {
			return full("disk() of the app is at its ceiling of " + strconv.Itoa(d.maxKeys) + " keys and " + strconv.FormatInt(d.maxBytes, 10) + " bytes: the write fails, nothing kept is removed (KITWORK_STORAGE.md mục 9)")
		}
	}
	if err := writeAtomic(root, name, content); err != nil {
		return diskError(err)
	}
	d.keys, d.bytes = keys, size
	d.nextExpiry = sooner(d.expiryKnown, d.nextExpiry, expireAt)
	return nil
}

func (d *diskPlace) remove(key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	root, err := d.open(false)
	if err != nil || root == nil {
		return false, err
	}
	defer root.Close()
	if err := d.count(root); err != nil {
		return false, err
	}
	_, found, gone, err := d.lookup(root, key)
	if err != nil || (!found && !gone) {
		return false, err
	}
	name := d.file(key)
	info, statErr := root.Stat(name)
	if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, diskError(err)
	}
	if statErr == nil {
		d.keys--
		d.bytes -= info.Size()
	}
	return found, nil
}

func (d *diskPlace) clear(branch string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	root, err := d.open(false)
	if err != nil || root == nil {
		return 0, err
	}
	defer root.Close()
	now, cleared := clock(), 0
	keys, size, err := d.walk(root, func(name string, content []byte) bool {
		record, err := decodeDiskRecord(content)
		if err != nil || !strings.HasPrefix(record.key, branch) {
			return false
		}
		if !expired(record.expireAt, now) {
			cleared++
		}
		return root.Remove(name) == nil
	})
	if err != nil {
		return 0, err
	}
	d.keys, d.bytes, d.counted = keys, size, true
	return cleared, nil
}

func (d *diskPlace) close() {}

// count measures what the folder holds, once per process: from then on each write keeps the count. Called with mu
// held for writing.
func (d *diskPlace) count(root *os.Root) error {
	if d.counted {
		return nil
	}
	keys, size, err := d.walk(root, nil)
	if err != nil {
		return err
	}
	d.keys, d.bytes, d.counted = keys, size, true
	return nil
}

// sweep removes the files of values that have expired, counts again, and learns when the soonest value kept
// expires. Called with mu held for writing.
func (d *diskPlace) sweep(root *os.Root) error {
	now := clock()
	var soonest time.Time
	keys, size, err := d.walk(root, func(name string, content []byte) bool {
		record, err := decodeDiskRecord(content)
		if err != nil || record.expireAt.IsZero() {
			return false
		}
		if !expired(record.expireAt, now) {
			soonest = sooner(true, soonest, record.expireAt)
			return false
		}
		return root.Remove(name) == nil
	})
	if err != nil {
		return err
	}
	d.keys, d.bytes, d.counted = keys, size, true
	d.expiryKnown, d.nextExpiry = true, soonest
	return nil
}

// walk counts the files of disk()'s folder and their bytes; drop, when given, is asked of each file and a file it
// removed is not counted. A link in the folder is neither followed nor counted.
func (d *diskPlace) walk(root *os.Root, drop func(name string, content []byte) bool) (int, int64, error) {
	keys, size := 0, int64(0)
	err := fs.WalkDir(root.FS(), ".", func(slashed string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return diskError(err)
		}
		if entry.IsDir() || !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		name := filepath.FromSlash(slashed)
		if drop != nil {
			if content, readErr := root.ReadFile(name); readErr == nil && drop(name, content) {
				return nil
			}
		}
		keys++
		size += info.Size()
		return nil
	})
	return keys, size, err
}

// writeAtomic writes content to name of disk()'s folder through a file beside it renamed over name, the folder it is
// in made first.
func writeAtomic(root *os.Root, name string, content []byte) error {
	folder := filepath.Dir(name)
	if err := root.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	temporary := filepath.Join(folder, ".write-"+hex.EncodeToString(suffix[:]))
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(temporary)
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		_ = root.Remove(temporary)
		return err
	}
	return nil
}

// sweepDue says a full place may hold a value that has expired: not known yet (no sweep since the process began),
// or the soonest expiry known is past.
func sweepDue(known bool, next time.Time) bool {
	return !known || (!next.IsZero() && !clock().Before(next))
}

// sooner is the soonest expiry once a value expiring at expireAt is kept, when the soonest is known.
func sooner(known bool, next, expireAt time.Time) time.Time {
	if !known || expireAt.IsZero() {
		return next
	}
	if next.IsZero() || expireAt.Before(next) {
		return expireAt
	}
	return next
}
