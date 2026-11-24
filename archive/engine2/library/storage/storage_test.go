package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/database"
)

// at moves the storage's clock to the time given until the test ends.
func at(t *testing.T, now *time.Time) {
	t.Helper()
	clock = func() time.Time { return *now }
	t.Cleanup(func() { clock = time.Now })
}

func encoded(t *testing.T, text string) []byte {
	t.Helper()
	data, err := Encode(value.String(text), valueLimit)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// memory() full pushes out the key used least recently, and what has expired at the cold end first (mục 9).
func TestMemoryPushesOutTheKeyUsedLeastRecently(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at(t, &now)
	m := newMemory(2, 1<<20)
	_ = m.set("a", encoded(t, "a"), time.Time{})
	_ = m.set("b", encoded(t, "b"), time.Time{})
	if _, found, _ := m.get("a"); !found { // a is used: b is the coldest now
		t.Fatal("a is not kept")
	}
	_ = m.set("c", encoded(t, "c"), time.Time{})
	if _, found, _ := m.get("b"); found {
		t.Error("b, used least recently, is still kept past two keys")
	}
	if _, found, _ := m.get("a"); !found {
		t.Error("a, used recently, was pushed out")
	}
	_ = m.set("short", encoded(t, "s"), now.Add(time.Second))
	now = now.Add(2 * time.Second)
	if _, found, _ := m.get("short"); found {
		t.Error("an expired value is still read")
	}
	small := newMemory(100, 40)
	_ = small.set("one", encoded(t, strings.Repeat("x", 20)), time.Time{})
	_ = small.set("two", encoded(t, strings.Repeat("y", 20)), time.Time{})
	if _, found, _ := small.get("one"); found {
		t.Error("past its bytes, memory() kept the coldest key")
	}
}

// disk() and datastore() full refuse the write, TOO_LARGE, and remove nothing alive; what has expired is swept first
// (mục 9).
func TestDiskAndDatastoreFullRefuseTheWrite(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at(t, &now)
	for _, made := range []struct {
		name string
		make func(root string) place
	}{
		{"disk", func(root string) place { return &diskPlace{app: root, maxKeys: 2, maxBytes: 1 << 20} }},
		{"datastore", func(root string) place {
			return &datastorePlace{app: root, open: database.Acquire, maxKeys: 2, maxBytes: 1 << 20}
		}},
	} {
		t.Run(made.name, func(t *testing.T) {
			kept := made.make(t.TempDir())
			t.Cleanup(kept.close)
			if err := kept.set("a", encoded(t, "a"), time.Time{}); err != nil {
				t.Fatal(err)
			}
			if err := kept.set("b", encoded(t, "b"), now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			err := kept.set("c", encoded(t, "c"), time.Time{})
			var known *placeError
			if err == nil || !strings.Contains(err.Error(), "at its ceiling of 2 keys") || !asPlace(err, &known) || !known.full {
				t.Fatalf("a third key past two: %v", err)
			}
			if err := kept.set("a", encoded(t, "again"), time.Time{}); err != nil {
				t.Errorf("a key already kept is written again at the ceiling: %v", err)
			}
			if _, found, _ := kept.get("b"); !found {
				t.Error("a refused write removed a value alive")
			}
			now = now.Add(2 * time.Second) // b has expired: the write sweeps it and goes in
			if err := kept.set("c", encoded(t, "c"), time.Time{}); err != nil {
				t.Errorf("a write once a value expired: %v", err)
			}
			for _, key := range []string{"a", "c"} {
				if _, found, _ := kept.get(key); !found {
					t.Errorf("%s is not kept", key)
				}
			}
		})
	}
}

func asPlace(err error, target **placeError) bool {
	known, ok := err.(*placeError)
	*target = known
	return ok
}

// A read of a disk() or datastore() never written makes no folder, no file (mục 11).
func TestAReadMakesNothing(t *testing.T) {
	root := t.TempDir()
	disk := &diskPlace{app: root, maxKeys: 10, maxBytes: 1 << 20}
	store := &datastorePlace{app: root, open: database.Acquire, maxKeys: 10, maxBytes: 1 << 20}
	t.Cleanup(store.close)
	for _, kept := range []place{disk, store} {
		if _, found, err := kept.get("a"); found || err != nil {
			t.Fatalf("a read of nothing: %v %v", found, err)
		}
		if removed, err := kept.remove("a"); removed || err != nil {
			t.Fatalf("a delete of nothing: %v %v", removed, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".database")); !os.IsNotExist(err) {
		t.Errorf(".database was made by reads: %v", err)
	}
}

// The codec keeps every kind a value of JSON has, bytes, a time (to the 256 ns of the format's float, as the first
// engine kept it) and a duration, in the order of an object's keys;
// a function, a handle of the engine, a failure are refused by name; a value past the limit is refused.
func TestTheCodecKeepsWhatJSONWrites(t *testing.T) {
	object := value.Object()
	object.Set("z", value.Number(1.5))
	object.Set("a", value.Array([]value.Value{value.Null(), value.Boolean(true), value.String("cà phê"), value.Bytes([]byte{0, 1, 2})}))
	object.Set("at", value.Time(time.Unix(1_800_000_000, 123)))
	object.Set("for", value.Duration(90*time.Second))
	data, err := Encode(object, valueLimit)
	if err != nil {
		t.Fatal(err)
	}
	read, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(read.Keys(), ","); got != "z,a,at,for" {
		t.Errorf("keys %s", got)
	}
	items := read.Get("a").Items()
	if read.Get("z").Number() != 1.5 || !items[0].IsNull() || !items[1].Boolean() || items[2].Text() != "cà phê" || string(items[3].Bytes()) != "\x00\x01\x02" ||
		read.Get("at").Time().Sub(time.Unix(1_800_000_000, 123)).Abs() > time.Microsecond || read.Get("for").Duration() != 90*time.Second {
		t.Errorf("read back: %v", read)
	}
	for _, refused := range []struct {
		item value.Value
		says string
	}{
		{value.Function(func(...value.Value) value.Value { return value.Null() }), "cannot keep a function"},
		{value.Fail(value.TypeCode, "x"), "cannot keep a failure"},
		{value.Struct(&Module{}), "cannot keep a handle of the engine"},
	} {
		if _, err := Encode(refused.item, valueLimit); err == nil || !strings.Contains(err.Error(), refused.says) {
			t.Errorf("%s: %v", refused.says, err)
		}
	}
	if _, err := Encode(value.String(strings.Repeat("x", 100)), 50); err == nil || !strings.Contains(err.Error(), "past 50 bytes") {
		t.Errorf("past the limit: %v", err)
	}
	self := value.Array([]value.Value{value.Null()})
	self.Items()[0] = self
	if _, err := Encode(self, valueLimit); err == nil || !strings.Contains(err.Error(), "holds itself") {
		t.Errorf("an array that holds itself: %v", err)
	}
}

// The first engine's bytes read here: a value it kept in disk() or datastore() is found and read the same (mục 11).
// golden is what the first engine's storage.Encode wrote for { name: "Kitwork", n: [1, true, null], at: the instant
// 1800000000 } (its keys sorted), and the key is its storage.Key{Datastore, "users", ["123"]}.Encoded(), both made on
// 07/11/2026 with its package.
func TestTheFirstEnginesBytesRead(t *testing.T) {
	key := Key{Place: Datastore, Name: "users", Segments: []string{"123"}}
	if got := key.Encoded(); got != "9:datastore5:users3:123" {
		t.Errorf("key %q", got)
	}
	golden := []byte{0x1, 0x6f, 0x3, 0x73, 0x2, 0x61, 0x74, 0x54, 0x43, 0xb8, 0xfa, 0xe2, 0x76, 0x93, 0xb4, 0x0, 0x73, 0x1,
		0x6e, 0x61, 0x3, 0x64, 0x3f, 0xf0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x74, 0x6e, 0x73, 0x4, 0x6e, 0x61, 0x6d, 0x65, 0x73,
		0x7, 0x4b, 0x69, 0x74, 0x77, 0x6f, 0x72, 0x6b}
	read, err := Decode(golden)
	if err != nil {
		t.Fatal(err)
	}
	items := read.Get("n").Items()
	if read.Get("name").Text() != "Kitwork" || len(items) != 3 || items[0].Number() != 1 || !items[1].Boolean() || !items[2].IsNull() ||
		!read.Get("at").Time().Equal(time.Unix(1_800_000_000, 0)) {
		t.Errorf("read %v", read)
	}
	// written back in the order read, which is the first engine's sorted order: the same bytes
	again, err := Encode(read, valueLimit)
	if err != nil || string(again) != string(golden) {
		t.Errorf("written back %#v, %v", again, err)
	}
}

// A file of disk() holds its key: one of another key found under the name is no value of this one.
func TestDiskReadsOnlyItsKeysFile(t *testing.T) {
	d := &diskPlace{app: t.TempDir(), maxKeys: 10, maxBytes: 1 << 20}
	if err := d.set("other", encoded(t, "other"), time.Time{}); err != nil {
		t.Fatal(err)
	}
	root, err := d.open(false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := root.ReadFile(d.file("other"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(root, d.file("mine"), content); err != nil {
		t.Fatal(err)
	}
	root.Close()
	if _, found, err := d.get("mine"); found || err != nil {
		t.Errorf("another key's file read as this one's: %v %v", found, err)
	}
}

// A wait that ended is forgotten: a caller that waited for a flight of another, and was done, does not count as
// waiting when that other later waits for it.
func TestFlightsForgetAWaitThatEnded(t *testing.T) {
	f := flights{running: map[string]*flight{}, waiting: map[*runtime.VM]*flight{}}
	a, b := &runtime.VM{}, &runtime.VM{}
	ofB, _ := f.begin("b", b)
	if !f.wait(a, ofB) {
		t.Fatal("a cannot wait for b")
	}
	f.waited(a)
	f.end("b", ofB)
	ofA, _ := f.begin("a", a)
	if !f.wait(b, ofA) {
		t.Error("b cannot wait for a once a stopped waiting for b")
	}
}

// clear drops every key under a branch, in each place, and counts only the values alive: one that has expired is
// dropped and not counted.
func TestClearCountsWhatLived(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at(t, &now)
	root := t.TempDir()
	for _, kept := range []place{newMemory(10, 1<<20), &diskPlace{app: root, maxKeys: 10, maxBytes: 1 << 20},
		&datastorePlace{app: root, open: database.Acquire, maxKeys: 10, maxBytes: 1 << 20}} {
		t.Cleanup(kept.close)
		_ = kept.set("branch:1", encoded(t, "alive"), time.Time{})
		_ = kept.set("branch:2", encoded(t, "gone"), now.Add(time.Second))
		_ = kept.set("other", encoded(t, "stays"), time.Time{})
		now = now.Add(2 * time.Second)
		cleared, err := kept.clear("branch:")
		if err != nil || cleared != 1 {
			t.Errorf("%T: cleared %d, %v; want 1, the one alive", kept, cleared, err)
		}
		if _, found, _ := kept.get("branch:1"); found {
			t.Errorf("%T: a value of the branch stays", kept)
		}
		if _, found, _ := kept.get("other"); !found {
			t.Errorf("%T: a value outside the branch went", kept)
		}
	}
}
