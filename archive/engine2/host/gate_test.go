package host

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Under contention the gate holds no more than its places, and a waiter wakes for each place given back: sixty-four
// goroutines take a gate of four places two hundred times each, every one waiting long enough that a lost wake would
// show as a refusal.
func TestTheGateHoldsItsPlacesUnderContention(t *testing.T) {
	g := newGate(4, 5*time.Second)
	var running, most, refused atomic.Int64
	var group sync.WaitGroup
	done := make(chan struct{})
	for worker := 0; worker < 64; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for turn := 0; turn < 200; turn++ {
				if !g.enter(done) {
					refused.Add(1)
					continue
				}
				now := running.Add(1)
				for {
					seen := most.Load()
					if now <= seen || most.CompareAndSwap(seen, now) {
						break
					}
				}
				if turn%16 == 0 {
					time.Sleep(10 * time.Microsecond)
				}
				running.Add(-1)
				g.leave()
			}
		}()
	}
	group.Wait()
	if most.Load() > 4 {
		t.Errorf("%d requests ran at once through a gate of 4", most.Load())
	}
	if refused.Load() != 0 {
		t.Errorf("%d entries refused though each could wait 5 s: a place given back woke no one", refused.Load())
	}
	if g.running.Load() != 0 || g.waiting.Load() != 0 {
		t.Errorf("the gate ends with %d running, %d waiting", g.running.Load(), g.waiting.Load())
	}
}

// A request that ends while it waits takes no place; a refusal's line is said at most once a second, counting the
// refusals since.
func TestTheGateLetsAWaiterGoAndSaysItsRefusalsOnce(t *testing.T) {
	g := newGate(1, 5*time.Second)
	if !g.enter(nil) {
		t.Fatal("the first entry")
	}
	done := make(chan struct{})
	close(done)
	began := time.Now()
	if g.enter(done) {
		t.Fatal("a request over before a place was free took one")
	}
	if waited := time.Since(began); waited > time.Second {
		t.Errorf("a request over waited %v of its 5 s for a place, want it let go at once", waited)
	}
	if g.running.Load() != 1 {
		t.Errorf("%d running, want the first alone", g.running.Load())
	}
	now := time.Now()
	if line := g.refusal(now); line == "" {
		t.Error("the first refusal said nothing")
	}
	if line := g.refusal(now.Add(100 * time.Millisecond)); line != "" {
		t.Errorf("a refusal within the second said %q", line)
	}
	if line := g.refusal(now.Add(2 * time.Second)); line == "" || line[:7] != "busy: 2" {
		t.Errorf("a refusal two seconds on said %q, want the two since the last line", line)
	}
}
