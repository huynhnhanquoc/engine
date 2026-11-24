package runtime_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// runPooled runs a program loaded on its own VM with the host's pool and a ceiling of its own: its result.
func runPooled(t *testing.T, program *loaded, pool *runtime.Pool) value.Value {
	t.Helper()
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxBytes = 64 << 20
	program.vm.Pool = pool
	return program.vm.Run()
}

// A run takes from the host's pool what it holds as it goes, and gives it all back as it ends (runtime/pool.go, the
// third slice of the tenants' limits).
func TestARunTakesFromTheHostsPoolAndGivesItBack(t *testing.T) {
	pool := runtime.NewPool(16 << 20)
	during := uint64(0)
	program := load(t, `const held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); } look();`)
	program.globals["look"] = value.Function(func(arguments ...value.Value) value.Value {
		during = pool.Held()
		return value.Undefined()
	})
	if result := runPooled(t, program, pool); result.IsInvalid() {
		t.Fatalf("a run holding 3 MB of a pool of 16: %s", result.Message())
	}
	if during < 3<<20 || during > 8<<20 {
		t.Errorf("the run held %d bytes of the pool while it held 3 MB, want about that, in steps of 1 MiB", during)
	}
	if held := pool.Held(); held != 0 {
		t.Errorf("the pool holds %d bytes once the run ended, want 0", held)
	}
}

// A run that makes with + and push alone, which the meter counts on its fast way, takes from the pool at its
// checkpoint, every 64 instructions.
func TestARunTakesFromThePoolAtItsCheckpoint(t *testing.T) {
	pool := runtime.NewPool(64 << 20)
	during := uint64(0)
	program := load(t, `const held = []; for (let i = 0; i < 100000; i++) { held.push("item " + i); } look();`)
	program.globals["look"] = value.Function(func(arguments ...value.Value) value.Value {
		during = pool.Held()
		return value.Undefined()
	})
	if result := runPooled(t, program, pool); result.IsInvalid() {
		t.Fatalf("a run of + and push: %s", result.Message())
	}
	if during < 3<<20 {
		t.Errorf("the run held %d bytes of the pool while it held some 4 MB, want them taken", during)
	}
}

// One builtin that would make past what the host gives is refused before it allocates, as one past the run's own
// ceiling is: a run holding 3 MB of a pool of 4 MiB, another asking "y".repeat(3_000_000) at once.
func TestOneMakingPastThePoolIsRefusedBeforeIt(t *testing.T) {
	pool := runtime.NewPool(4 << 20)
	second := load(t, `const t = "y".repeat(3000000);`)
	var refused value.Value
	first := load(t, `const held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); } other();`)
	first.globals["other"] = value.Function(func(arguments ...value.Value) value.Value {
		refused = runPooled(t, second, pool)
		return value.Undefined()
	})
	if result := runPooled(t, first, pool); result.IsInvalid() {
		t.Fatalf("the run holding the pool: %s", result.Message())
	}
	if !runtime.HostBusy(refused) {
		t.Errorf("a repeat past what the host gives ended %q, want the host's memory limit", refused.Message())
	}
}

// A run that would hold past what the host gives stops with HOST_MEMORY_LIMIT, which safe(() => …) does not catch and
// which the host reads as busy (HostBusy: 503, not 500); the run holding the pool goes on, and once it ends the same
// program runs.
func TestARunPastWhatTheHostGivesStops(t *testing.T) {
	pool := runtime.NewPool(4 << 20)
	second := load(t, `const r = safe(() => { const held = []; for (let i = 0; i < 2000; i++) { held.push("y".repeat(1000) + i); } return held.length; }); const after = 1;`)
	var refused value.Value
	first := load(t, `const held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); } other(); const done = held.length;`)
	first.globals["other"] = value.Function(func(arguments ...value.Value) value.Value {
		refused = runPooled(t, second, pool)
		return value.Undefined()
	})
	if result := runPooled(t, first, pool); result.IsInvalid() {
		t.Fatalf("the run holding the pool: %s", result.Message())
	}
	if !runtime.HostBusy(refused) || !strings.Contains(refused.Message(), "Host Memory Limit Exceeded") {
		t.Errorf("a run past what the host gives ended %q (code %s), want the host's memory limit, busy", refused.Message(), refused.Code())
	}
	if result := runPooled(t, second, pool); result.IsInvalid() {
		t.Errorf("the same run once the pool was free: %s", result.Message())
	}
	if held := pool.Held(); held != 0 {
		t.Errorf("the pool holds %d bytes once both ended, want 0", held)
	}
}

// What a run makes and lets go does not fill the pool: a text built by += holds little, and the run gives back what it
// took past what it holds as it is counted again.
func TestAChurningRunDoesNotFillThePool(t *testing.T) {
	pool := runtime.NewPool(2 << 20)
	program := load(t, `let s = ""; for (let i = 0; i < 3000; i++) { s += "piece " + i + " "; }`)
	if result := runPooled(t, program, pool); result.IsInvalid() {
		t.Fatalf("a text built by += in a pool of 2 MiB: %s", result.Message())
	}
	if held := pool.Held(); held != 0 {
		t.Errorf("the pool holds %d bytes once the run ended, want 0", held)
	}
}

// A run that made much since it was last counted is counted again before it takes more, so what it takes follows what
// it holds: a text built by += makes some 58 MB to hold 40 KB, and holds a step or two of a pool of 64 MiB, not all it
// made.
func TestAChurningRunTakesLittleOfThePool(t *testing.T) {
	pool := runtime.NewPool(64 << 20)
	during := uint64(0)
	program := load(t, `let s = ""; for (let i = 0; i < 3000; i++) { s += "piece " + i + " "; } look();`)
	program.globals["look"] = value.Function(func(arguments ...value.Value) value.Value {
		during = pool.Held()
		return value.Undefined()
	})
	if result := runPooled(t, program, pool); result.IsInvalid() {
		t.Fatalf("a text built by +=: %s", result.Message())
	}
	if during > 3<<20 {
		t.Errorf("a run holding 40 KB held %d bytes of the pool, want a step or two", during)
	}
}

// A run counted again gives the pool back what it took past what it holds, for the other runs: one holds 3 MB, lets it
// go and makes 2 MB it drops, then another run takes 2 MB of a pool of 5 MiB.
func TestARunCountedAgainGivesThePoolBack(t *testing.T) {
	pool := runtime.NewPool(5 << 20)
	second := load(t, `const held = []; for (let i = 0; i < 2000; i++) { held.push("y".repeat(1000) + i); }`)
	var other value.Value
	first := load(t, `let held = []; for (let i = 0; i < 3000; i++) { held.push("x".repeat(1000) + i); } held = []; for (let i = 0; i < 2000; i++) { const t = "z".repeat(1000) + i; } other();`)
	first.globals["other"] = value.Function(func(arguments ...value.Value) value.Value {
		other = runPooled(t, second, pool)
		return value.Undefined()
	})
	if result := runPooled(t, first, pool); result.IsInvalid() {
		t.Fatalf("the first run: %s", result.Message())
	}
	if other.IsInvalid() {
		t.Errorf("a run of 2 MB after one that let its 3 MB go, in a pool of 5 MiB: %s", other.Message())
	}
}

// A run the pool refuses is counted again before it fails: one that held 8 MB, let it go and made 3 MB it drops, which
// is less than it held when last counted, asks a pool of 10 MiB for its 11, is refused, is counted by the little it
// holds, and goes on.
func TestARunRefusedIsCountedAgainFirst(t *testing.T) {
	pool := runtime.NewPool(10 << 20)
	program := load(t, `let held = []; for (let i = 0; i < 8000; i++) { held.push("x".repeat(1000) + i); } held = []; for (let i = 0; i < 3000; i++) { const t = "z".repeat(1000) + i; }`)
	if result := runPooled(t, program, pool); result.IsInvalid() {
		t.Errorf("a run that let its 8 MB go, refused past a pool of 10 MiB: %s", result.Message())
	}
}

// Many runs at once take and give back on one pool: sixteen goroutines run fifty times each a program holding some
// 600 KB in a pool roomy enough for all, and the pool ends empty, none refused.
func TestManyRunsShareThePool(t *testing.T) {
	pool := runtime.NewPool(256 << 20)
	var group sync.WaitGroup
	failures := make(chan string, 16*50)
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			program := load(t, `const held = []; for (let i = 0; i < 500; i++) { held.push("z".repeat(1200) + i); }`)
			for turn := 0; turn < 50; turn++ {
				if result := runPooled(t, program, pool); result.IsInvalid() {
					failures <- result.Message()
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("a run failed: %s", failure)
		break
	}
	if held := pool.Held(); held != 0 {
		t.Errorf("the pool holds %d bytes once every run ended, want 0", held)
	}
}
