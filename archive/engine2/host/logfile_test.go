package host

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// A file of the log grown past its size is set aside under the time and begun again; MaxBackups of the files set
// aside are kept, the newest, gzipped with Compress, and none older than MaxAge.
func TestALogFileIsSetAsideAndLetGo(t *testing.T) {
	folder := t.TempDir()
	clock := time.Date(2026, 11, 9, 10, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }
	path := filepath.Join(folder, "logs", "kitwork.log")
	// an old file set aside, past max_age
	old := filepath.Join(folder, "logs", "kitwork-"+clock.Add(-40*24*time.Hour).Format(backupStamp)+".log.gz")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(old, []byte("old"), 0o644)
	file, err := openLogFile(path, 1, 2, 28, true, now)
	if err != nil {
		t.Fatal(err)
	}
	file.maxBytes = 100
	line := []byte(strings.Repeat("x", 59) + "\n") // 60 bytes: two lines pass 100
	for i := 0; i < 8; i++ {
		clock = clock.Add(time.Second)
		if _, err := file.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	// eight lines, one a file: seven set aside, two kept (the newest), each gzipped; the old one gone
	if len(names) != 3 || names[2] != "kitwork.log" || !strings.HasSuffix(names[0], ".log.gz") || !strings.HasSuffix(names[1], ".log.gz") {
		t.Fatalf("the folder holds %v", names)
	}
	if names[1] != "kitwork-"+clock.Format(backupStamp)+".log.gz" { // set aside as the eighth line came
		t.Errorf("the newest set aside is %s", names[1])
	}
	current, _ := os.ReadFile(path)
	if string(current) != string(line) {
		t.Errorf("the file begun again holds %q", current)
	}
	zipped, _ := os.Open(filepath.Join(filepath.Dir(path), names[1]))
	reader, err := gzip.NewReader(zipped)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, _ := io.ReadAll(reader)
	zipped.Close()
	if string(unzipped) != string(line) {
		t.Errorf("a file set aside holds %q", unzipped)
	}
}

// A file set aside older than MaxAge goes however few are kept; one younger stays.
func TestALogFileLetsGoOfOldFiles(t *testing.T) {
	folder := t.TempDir()
	clock := time.Date(2026, 11, 9, 10, 0, 0, 0, time.Local)
	path := filepath.Join(folder, "kitwork.log")
	old := filepath.Join(folder, "kitwork-"+clock.Add(-40*24*time.Hour).Format(backupStamp)+".log")
	young := filepath.Join(folder, "kitwork-"+clock.Add(-2*24*time.Hour).Format(backupStamp)+".log")
	os.WriteFile(old, []byte("old"), 0o644)
	os.WriteFile(young, []byte("young"), 0o644)
	file, err := openLogFile(path, 1, 50, 28, false, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	file.maxBytes = 10
	file.Write([]byte("first line\n"))
	file.Write([]byte("second line\n"))
	file.Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a file set aside 40 days ago, past max_age 28, is kept")
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a file set aside 2 days ago is gone: %v", err)
	}
}

// Without Compress, a file set aside stays as written; one that opens on a file already there writes at its end.
func TestALogFileKeepsWhatItHad(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "kitwork.log")
	os.WriteFile(path, []byte("before\n"), 0o644)
	file, err := openLogFile(path, 1, 5, 28, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("after\n"))
	file.maxBytes = 10
	file.Write([]byte("third line\n"))
	file.Close()
	if got, _ := os.ReadFile(path); string(got) != "third line\n" {
		t.Errorf("the file begun again holds %q", got)
	}
	matches, _ := filepath.Glob(filepath.Join(folder, "kitwork-*.log"))
	if len(matches) != 1 {
		t.Fatalf("set aside: %v", matches)
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != "before\nafter\n" {
		t.Errorf("the file set aside holds %q", got)
	}
}
