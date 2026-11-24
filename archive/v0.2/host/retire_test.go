package host

import (
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
	"weak"
)

// A boot a reload put aside is let go once each request it may still answer has had its time (RequestTimeout): the
// host keeps none of them past it (15/11/2026: retired kept every one, so each reload held a whole app's memory for as
// long as the host ran). Five reloads of one app leave none after their time, the first boot's site is collected, and
// the app's current boot serves.
func TestABootAReloadPutAsideIsLetGo(t *testing.T) {
	h, _ := panicHost(t)
	h.RequestTimeout = 20 * time.Millisecond
	if err := h.Boot(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(h.File), "apps", "one")
	first := weak.Make(h.apps["one"].sites[0])
	// the five reloads and the count under one hold of reloading: a boot is let go by a timer that takes reloading to
	// drop it from retired, so none is dropped before the count however long the reloads take (16/11/2026: the lock
	// let go between reloads, five reloads past RequestTimeout on a busy machine let the first go before the count,
	// "put 1 boots aside")
	h.reloading.Lock()
	for range 5 {
		h.reloadApps(reboot{old: h.apps["one"], identity: "one", root: root, marks: marksOf(root), why: "a test"})
	}
	put := len(h.retired)
	h.reloading.Unlock()
	if put != 5 {
		t.Fatalf("five reloads put %d boots aside, want 5", put)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.reloading.Lock()
		left := len(h.retired)
		h.reloading.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d boots a reload put aside are still held after their time", left)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for range 5 {
		goruntime.GC()
	}
	if first.Value() != nil {
		t.Error("the first boot's site is still held after its boot was let go")
	}
	if site := h.current()["shop.test"]; site == nil || site != h.apps["one"].sites[0] {
		t.Fatal("shop.test is not the app's current boot's site")
	}
}
