package runtime_test

import (
	"strings"
	"testing"
)

// A text a run builds by + and templates is written in place after itself (19/11/2026, value/join.go): what it costs
// grows with what it holds, not with its square. s = s + "x" copied s whole at every turn, so 30,000 turns of it, a
// text of 30 KB, made 450 MB and stopped at a request's energy (10,000,000 units), as did 100,000 turns of a template
// or of s += "<li>" + i + "</li>"; each now runs within it, having made a few times what it holds. Every text made on
// the way keeps its bytes (held to node in parity_test.go).
//
// CONTROL (19/11/2026): with Join writing every text anew (no buffer found), each case stopped with ENERGY_LIMIT.
func TestATextBuiltPieceByPieceCostsWhatItHolds(t *testing.T) {
	for name, each := range map[string]struct {
		source string
		length int
	}{
		"s = s + x":    {`let s = ""; for (let i = 0; i < 100000; i++) { s = s + "x"; } result = s.length;`, 100000},
		"s += x":       {`let s = ""; for (let i = 0; i < 100000; i++) { s += "x"; } result = s.length;`, 100000},
		"a template":   {"let s = \"\"; for (let i = 0; i < 100000; i++) { s = `${s}x`; } result = s.length;", 100000},
		"pieces":       {`let s = ""; for (let i = 0; i < 20000; i++) { s += "<li>" + i + "</li>"; } result = s.length;`, 20000*9 + 88890},
		"two at once":  {`let a = ""; let b = ""; for (let i = 0; i < 50000; i++) { a += "x"; b = b + "yy"; } result = a.length + b.length;`, 150000},
		"a long piece": {`const piece = "z".repeat(1000); let s = ""; for (let i = 0; i < 5000; i++) { s = s + piece; } result = s.length;`, 5000000},
	} {
		t.Run(name, func(t *testing.T) {
			program := load(t, each.source)
			program.vm.MaxEnergy = 10_000_000
			program.vm.MaxBytes = 64 << 20
			program.vm.FastReset(program.program, program.globals)
			if result := program.vm.Run(); result.IsInvalid() {
				t.Fatalf("%s: %s", result.Code(), result.Message())
			}
			if got := program.variable("result").Number(); got != float64(each.length) {
				t.Fatalf("the text is %v long, want %d", got, each.length)
			}
			// what the run made: the texts of the pieces, and each buffer the text outgrew, twice what it holds at most
			if made := program.vm.Bytes; made > uint64(8*each.length+(1<<20)) {
				t.Errorf("the run made %d bytes for a text of %d", made, each.length)
			}
		})
	}
}

// The room of a text built in place is counted as the run makes it, and let go as the run is counted by what it holds:
// a text past the run's ceiling stops it with MEMORY_LIMIT, as it did, and texts built and let go one after another
// never do, however many.
//
// CONTROL (19/11/2026): with the room not asked of what the run may make (Spare), the second case stopped with its
// memory limit. With the buffers kept as the run is counted again, both cases still passed: what letting them go
// guards, buffers Go holds that the count cannot see once their texts are gone, shows in no program's result, and
// is argued in value/join.go and runtime memory.go, not tested.
func TestTheRoomOfABuiltTextIsWithinTheCeiling(t *testing.T) {
	program := load(t, `const piece = "x".repeat(1000); let s = ""; for (let i = 0; i < 2000; i++) { s = s + piece; } result = s.length;`)
	program.vm.MaxBytes = 1 << 20
	program.vm.FastReset(program.program, program.globals)
	if result := program.vm.Run(); !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit") {
		t.Errorf("a text of 2 MB built under a ceiling of 1 MiB: %s %s, want its memory limit", result.Code(), result.Message())
	}
	program = load(t, `const piece = "x".repeat(1000); let n = 0;
for (let k = 0; k < 30; k++) { let s = ""; for (let i = 0; i < 400; i++) { s = s + piece; } n = n + s.length; }
result = n;`)
	program.vm.MaxBytes = 1 << 20
	program.vm.FastReset(program.program, program.globals)
	if result := program.vm.Run(); result.IsInvalid() {
		t.Fatalf("thirty texts of 400 KB built one after another under a ceiling of 1 MiB: %s", result.Message())
	}
	if got := program.variable("result").Number(); got != 30*400*1000 {
		t.Errorf("they hold %v bytes together, want %d", got, 30*400*1000)
	}
}

// A text a run built and left to another (a value of the site's boot, kept in storage) is never written by a later
// run of the same VM: the buffers are let go as a run begins, and a join writes only after the whole of a buffer.
func TestATextOfAnEarlierRunIsNeverWritten(t *testing.T) {
	first := load(t, `let s = "q".repeat(100); s = s + "1"; s = s + "2"; result = s;`)
	first.run(t)
	shared := first.variable("result")
	if !shared.IsString() || !strings.HasSuffix(shared.Text(), "12") {
		t.Fatalf("the first run built %s", describe(shared))
	}
	second := load(t, `result = [shared + "3", shared + "4", shared.slice(-2), shared.length];`)
	second.globals["shared"] = shared
	second.vm = first.vm // the same VM, as a site's pool lends it
	second.run(t)
	if result := describe(second.variable("result")); result != `array ["`+strings.Repeat("q", 100)+`123","`+strings.Repeat("q", 100)+`124","12",102]` {
		t.Errorf("the second run gave %s", result)
	}
	if text := shared.Text(); text != strings.Repeat("q", 100)+"12" {
		t.Errorf("the first run's text is now %q", text)
	}
}
