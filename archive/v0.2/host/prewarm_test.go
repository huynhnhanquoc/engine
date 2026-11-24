package host

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A prewarmed site holds files up to what it may hold in all (keptFilesSize, here 25 bytes): a file past it is read from
// the disk; a file held again takes its own place, never twice the room; one let go gives its room back.
//
// CONTROL (11/11/2026): with the sum not checked, the third file was held past the 25 bytes.
func TestKeptFilesHoldNoMoreThanTheirRoom(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := &keptFiles{most: 25}
	keep := func(name string) *keptFile {
		t.Helper()
		file, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		return m.keep(name, file, info, "text/plain", `W/"1"`, "no-cache")
	}
	if keep("a") == nil || keep("b") == nil {
		t.Fatal("two files of 10 bytes in 25 not held")
	}
	if keep("c") != nil || m.size.Load() != 20 {
		t.Errorf("a third held past the room: %d bytes", m.size.Load())
	}
	again := keep("a")
	if again == nil || m.size.Load() != 20 {
		t.Errorf("a file held again: %d bytes, want 20", m.size.Load())
	}
	held, _ := m.files.Load("b")
	m.drop("b", held.(*keptFile))
	if m.size.Load() != 10 || keep("c") == nil || m.size.Load() != 20 {
		t.Errorf("the room given back: %d bytes", m.size.Load())
	}
	if string(again.data) != "0123456789" {
		t.Errorf("held %q", again.data)
	}
}

// Many requests keeping and letting go of the same files at once leave the room counted as what is held, byte for
// byte: each keep counts what its swap let go, each drop what it took away.
func TestKeptFilesCountTheirRoomUnderManyRequests(t *testing.T) {
	folder := t.TempDir()
	names := []string{"a", "b", "c", "d", "e", "f"}
	for i, name := range names {
		if err := os.WriteFile(filepath.Join(folder, name), make([]byte, 100*(i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := &keptFiles{most: 1 << 20}
	var group sync.WaitGroup
	for worker := range 16 {
		group.Go(func() {
			for i := range 400 {
				name := names[(worker+i)%len(names)]
				file, err := os.Open(filepath.Join(folder, name))
				if err != nil {
					t.Error(err)
					return
				}
				if info, err := file.Stat(); err == nil {
					m.keep(name, file, info, "application/octet-stream", `W/"1"`, "no-cache")
				}
				file.Close()
				if i%3 == 0 {
					if held, ok := m.files.Load(name); ok {
						m.drop(name, held.(*keptFile))
					}
				}
			}
		})
	}
	group.Wait()
	var held int64
	m.files.Range(func(_, k any) bool {
		held += int64(len(k.(*keptFile).data))
		return true
	})
	if held != m.size.Load() {
		t.Errorf("the room counted %d bytes, the files held %d", m.size.Load(), held)
	}
}
