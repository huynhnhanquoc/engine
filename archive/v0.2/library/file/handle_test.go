package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A handle a run calls is attended by the run's context: clear() of a request that is over stops before it removes
// anything, PARTIAL with progress 0, and the folder keeps what it holds (mục 7).
func TestClearStopsAsTheRequestEnds(t *testing.T) {
	app := t.TempDir()
	folder := filepath.Join(app, ".database", "files", "tmp")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := &File{Root: app}
	opened := files.Open(value.String("tmp/"))
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	attended := opened.AttendedBy(ended).Host().(*Handle)
	cleared := attended.Clear()
	if cleared.Code() != value.PartialCode || !cleared.Progress().IsNumber() || cleared.Progress().Number() != 0 {
		t.Fatalf("clear() of a request that is over: %s, progress %v", cleared.Failure(), cleared.Progress())
	}
	if left, _ := os.ReadDir(folder); len(left) != 2 {
		t.Fatalf("the folder holds %d entries", len(left))
	}
	if all := opened.Host().(*Handle).Clear(); !all.IsNumber() || all.Number() != 2 {
		t.Fatalf("clear() with no request: %v", all.Failure())
	}
}
