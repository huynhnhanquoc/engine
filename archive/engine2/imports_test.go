package engine2

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// KitDB is its own module, ../kitdb (github.com/huynhnhanquoc/sqlitewire), moved out of the first engine with its history on
// 11/11/2026: a database that runs alone, Kitwork's way to it a shortcut. What engine2 takes from it is listed here by
// the file that may hold it; a new import of KitDB is a decision, made by editing this table.
var kitdb = map[string][]string{
	// kitwork migrate writes in the port's words (gateway.TranslateSchema, gateway.Structure) on KitDB's writer, and
	// an app's statements read the file through it (library/database, run.go, slice 3, 06/11/2026).
	"github.com/huynhnhanquoc/sqlitewire/gateway": {"host/port.go", "host/port_search.go", "library/database/migrate.go", "library/database/choices.go"},
	// datastore() of kitwork/storage is the app's file .database/storage/kitwork.db, written by its one writer of the
	// process, KitDB's, as the app's databases are (library/storage, 07/11/2026); kitwork_runs of kitwork/queue and
	// kitwork/schedule is a table of the app's database, written by that writer too (library/queue, 08/11/2026).
	// A command's backup of a database is KitDB's copy of each file without its writer, sqlite.BackupFile (library/database,
	// backup.go, KITWORK_COMMANDS.md mục 6, 13/11/2026).
	"github.com/huynhnhanquoc/sqlitewire/sqlite": {"library/database/backup.go", "library/storage/datastore.go", "library/queue/file.go", "library/queue/store_test.go", "test/host/queue_test.go", "library/database/catalog.go", "library/database/history.go", "library/database/migrate.go", "library/database/run.go", "library/database/search.go", "library/database/transaction.go", "library/database/write.go", "library/database/writers.go", "host/port.go", "test/host/migrate_test.go", "test/host/database_search_test.go", "test/host/database_history_test.go", "test/host/command_backup_test.go"},
	// matches reads the index the database port's MATCH reads, <file>.search, with KitDB's full text (library/database,
	// search.go, slice 3b-4, 06/11/2026): one index of a table, whoever reads it.
	"github.com/huynhnhanquoc/sqlitewire/fulltext": {"library/database/search.go", "host/port_search.go"},
	"github.com/huynhnhanquoc/sqlitewire/search":   {"library/database/search.go", "host/port_search.go"},
	// the host's database port is KitDB's wire, accounts and column copies (host, port.go, slice 6, 06/11/2026).
	"github.com/huynhnhanquoc/sqlitewire/pgwire":    {"host/port.go", "host/port_search.go"},
	"github.com/huynhnhanquoc/sqlitewire/catalog":   {"host/port.go", "test/host/command_backup_test.go"},
	"github.com/huynhnhanquoc/sqlitewire/analytics": {"host/port.go"},
}

// TestNothingImportsTheFirstEngine: engine2 takes nothing from github.com/kitwork/engine since 11/11/2026, KitDB gone
// to its own module, KitID to internal/id and the napas the test matched written into the test as text. A program
// linking both engines would hold two KitDBs, each with its own writers, two writers for one file.
func TestNothingImportsTheFirstEngine(t *testing.T) {
	eachImport(t, func(path, imported string) {
		if imported == "github.com/kitwork/engine" || strings.HasPrefix(imported, "github.com/kitwork/engine/") {
			t.Errorf("%s imports %s: engine2 takes nothing from the first engine", path, imported)
		}
	})
}

func TestOnlyListedFilesImportKitDB(t *testing.T) {
	eachImport(t, func(path, imported string) {
		if imported != "github.com/huynhnhanquoc/sqlitewire" && !strings.HasPrefix(imported, "github.com/huynhnhanquoc/sqlitewire/") {
			return
		}
		allowed := false
		for _, file := range kitdb[imported] {
			allowed = allowed || file == path
		}
		if !allowed {
			t.Errorf("%s imports %s, which the table in imports_test.go does not allow", path, imported)
		}
	})
}

// eachImport calls seen for each import of each Go file of engine2.
func eachImport(t *testing.T, seen func(path, imported string)) {
	t.Helper()
	set := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(set, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			seen(filepath.ToSlash(path), strings.Trim(spec.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
