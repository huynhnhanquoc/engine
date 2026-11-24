package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/huynhnhanquoc/sqlitewire/sqlite"
)

// Backup is one copy BackupFolder made of a database.
type Backup struct {
	// Folder is <to>/<name>/<moment>, the copy's folder.
	Folder string
	// Files are what it holds: each schema file, at the revision it was copied at, then its structure.
	Files []BackedUp
	// Bytes are those of every file; Seconds how long the copy took.
	Bytes   int64
	Seconds float64
	// Removed are the older copies keep let go of, their folders.
	Removed []string
}

// BackedUp is one file of a Backup: a schema file (Revision its newest commit in the copy), a structure (.db.sql), or
// the accounts of the cluster (.catalog/kitwork.db).
type BackedUp struct {
	Name      string
	Bytes     int64
	Revision  uint64
	Structure bool
	Accounts  bool
}

// backupMoment is how a copy's folder is named: the moment it was made, in the host's zone, to the second, and -1, -2…
// for a second copy within the same second.
const backupMoment = "2006-01-02T15-04-05"

// backupFolder is a folder BackupFolder made: only those are ever removed by keep.
var backupFolder = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2})(?:-(\d+))?$`)

// BackupFolder copies the database at folder, named name, into <to>/<name>/<moment>/ (docs/KITWORK_COMMANDS.md mục 6):
// each schema file, <schema>.db, by KitDB's sqlite.BackupFile, consistent at its newest commit while its writer goes on
// (the files of one database at moments of their own), and its structure <schema>.db.sql; then the accounts of the
// cluster the database is in, .catalog/kitwork.db beside its folder, by sqlite.CopyFile. What can be built again or
// is the machine's own is not copied: the anchors of .history, the .search indexes, the .analytics copies, the lock,
// the WAL. The copy is made in a folder of its own beside it, .<moment>.partial, renamed when every file is there: a
// copy is whole, or is not. keep, when above 0, then keeps the keep newest copies of name in to and removes the older:
// only the folders named as BackupFolder names them, never anything else there.
func BackupFolder(ctx context.Context, folder, name, to string, keep int, now time.Time) (*Backup, error) {
	started := time.Now()
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("the database %s: %w", name, err)
	}
	var schemas []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".db") {
			schemas = append(schemas, entry.Name())
		}
	}
	if len(schemas) == 0 {
		return nil, fmt.Errorf("the database %s has no file yet, only its structure: nothing to copy", name)
	}
	sort.Strings(schemas)
	if inside(folder, to) {
		return nil, fmt.Errorf("the copies of %s go to %s, inside the database itself: copy it to another folder, best another disk", name, to)
	}
	copies := filepath.Join(to, filepath.FromSlash(name))
	if err := os.MkdirAll(copies, 0o755); err != nil {
		return nil, err
	}
	moment := now.Format(backupMoment)
	for i := 1; ; i++ {
		if _, err := os.Lstat(filepath.Join(copies, moment)); errors.Is(err, os.ErrNotExist) {
			break
		}
		moment = now.Format(backupMoment) + "-" + strconv.Itoa(i)
	}
	partial := filepath.Join(copies, "."+moment+".partial")
	if err := os.Mkdir(partial, 0o755); err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(partial)
		}
	}()
	backup := &Backup{}
	for _, schema := range schemas {
		target := filepath.Join(partial, schema)
		revision, err := sqlite.BackupFile(ctx, filepath.Join(folder, schema), target)
		if err != nil {
			return nil, fmt.Errorf("the database %s, %s: %w", name, schema, err)
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		backup.Files = append(backup.Files, BackedUp{Name: schema, Bytes: info.Size(), Revision: revision})
		structure, err := os.ReadFile(filepath.Join(folder, schema+".sql"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := writeSynced(filepath.Join(partial, schema+".sql"), structure); err != nil {
			return nil, err
		}
		backup.Files = append(backup.Files, BackedUp{Name: schema + ".sql", Bytes: int64(len(structure)), Structure: true})
	}
	// the accounts of the cluster the database is in, its roles and grants (.catalog/kitwork.db beside the database's
	// folder, kitdb/catalog's): without them a copy put back is the data with no one to read it but the host's admin
	catalog := filepath.Join(filepath.Dir(folder), ".catalog", "kitwork.db")
	if info, err := os.Stat(catalog); err == nil && info.Mode().IsRegular() {
		target := filepath.Join(partial, ".catalog", "kitwork.db")
		if err := sqlite.CopyFile(ctx, catalog, target); err != nil {
			return nil, fmt.Errorf("the database %s, the accounts of its cluster: %w", name, err)
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		backup.Files = append(backup.Files, BackedUp{Name: ".catalog/kitwork.db", Bytes: info.Size(), Accounts: true})
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	backup.Folder = filepath.Join(copies, moment)
	if err := os.Rename(partial, backup.Folder); err != nil {
		return nil, err
	}
	done = true
	for _, file := range backup.Files {
		backup.Bytes += file.Bytes
	}
	backup.Seconds = time.Since(started).Seconds()
	if keep > 0 {
		removed, err := keepNewest(copies, keep)
		backup.Removed = removed
		if err != nil {
			return backup, fmt.Errorf("the copy of %s is made, %s, but an older one was not removed: %w", name, backup.Folder, err)
		}
	}
	return backup, nil
}

// keepNewest removes all but the keep newest copies BackupFolder made in copies, oldest first.
func keepNewest(copies string, keep int) ([]string, error) {
	entries, err := os.ReadDir(copies)
	if err != nil {
		return nil, err
	}
	type made struct {
		name, moment string
		number       int
	}
	var all []made
	for _, entry := range entries {
		parts := backupFolder.FindStringSubmatch(entry.Name())
		if !entry.IsDir() || parts == nil {
			continue
		}
		number, _ := strconv.Atoi(parts[2])
		all = append(all, made{name: entry.Name(), moment: parts[1], number: number})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].moment != all[j].moment {
			return all[i].moment < all[j].moment
		}
		return all[i].number < all[j].number
	})
	var removed []string
	for len(all) > keep {
		folder := filepath.Join(copies, all[0].name)
		if err := os.RemoveAll(folder); err != nil {
			return removed, err
		}
		removed = append(removed, folder)
		all = all[1:]
	}
	return removed, nil
}

// inside reports target as folder or a folder within it.
func inside(folder, target string) bool {
	folder, errFolder := filepath.Abs(folder)
	target, errTarget := filepath.Abs(target)
	if errFolder != nil || errTarget != nil {
		return false
	}
	relative, err := filepath.Rel(folder, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// writeSynced writes data to a new file at path and syncs it.
func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
