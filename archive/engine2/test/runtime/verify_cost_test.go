package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// Verifying a program makes the same few allocations whatever its size (04/11/2026): its instructions,
// their operands (one block), the table of where each starts and what is proved of each are made once
// at their size. It made a slice of operands for each instruction and kept two maps by address, which
// was most of what a compile allocated in the verifier.
func TestVerifyingAProgramAllocatesTheSameWhateverItsSize(t *testing.T) {
	block := `const f%d = (x) => { let s = 0; for (let i = 0; i < x; i++) { s = s + i * 2; } return s > 10 ? "big" : "small"; };
let r%d = [f%d(3), f%d(20)].join(",");
`
	allocations := map[int]float64{}
	for _, copies := range []int{10, 100} {
		var source strings.Builder
		for k := 0; k < copies; k++ {
			source.WriteString(strings.ReplaceAll(block, "%d", strings.Repeat("x", k%7)+string(rune('a'+k%26))+strings.Repeat("y", k/26)))
		}
		bytecode, err := compiler.CompileSource(source.String())
		if err != nil {
			t.Fatal(err)
		}
		program := bytecode.Program
		code, constants, locals := program.Instructions(), program.Constants(), len(program.Locals())
		if err := runtime.VerifyWithLocals(code, constants, locals); err != nil {
			t.Fatal(err)
		}
		allocations[copies] = testing.AllocsPerRun(20, func() {
			_ = runtime.VerifyWithLocals(code, constants, locals)
		})
	}
	if allocations[100] != allocations[10] || allocations[10] > 16 {
		t.Errorf("verifying 100 copies allocates %.0f times, 10 copies %.0f: want the same few", allocations[100], allocations[10])
	}
}

// Building a program from its parts allocates the same whatever how many strings and numbers it holds
// (04/11/2026): its checksum is written through one buffer to the hash, where each field written straight
// to the hash escaped, a string's bytes and a number's eight bytes three allocations or so a constant.
func TestBuildingAProgramAllocatesTheSameWhateverItsConstants(t *testing.T) {
	allocations := map[int]float64{}
	for _, count := range []int{10, 200} {
		var source strings.Builder
		source.WriteString("r = [")
		for i := 0; i < count; i++ {
			if i > 0 {
				source.WriteString(", ")
			}
			source.WriteString(`"text ` + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + strings.Repeat("z", i/26) + `", ` + strings.Repeat("1", 1+i%9) + "." + string(rune('1'+i%9)))
		}
		source.WriteString("];")
		bytecode, err := compiler.CompileSource(source.String())
		if err != nil {
			t.Fatal(err)
		}
		program := bytecode.Program
		code, constants, debug, locals := program.Instructions(), program.Constants(), program.DebugEntries(), program.Locals()
		if len(constants) < count {
			t.Fatalf("%d constants for %d strings and numbers", len(constants), count)
		}
		rebuilt, err := runtime.NewProgramWithLocals(code, constants, debug, locals)
		if err != nil {
			t.Fatal(err)
		}
		if rebuilt.Checksum() != program.Checksum() {
			t.Fatalf("the same parts give another checksum")
		}
		allocations[count] = testing.AllocsPerRun(20, func() {
			_, _ = runtime.NewProgramWithLocals(code, constants, debug, locals)
		})
	}
	if allocations[200] != allocations[10] {
		t.Errorf("a program of 200 strings and numbers allocates %.0f times as it is built, of 10 %.0f", allocations[200], allocations[10])
	}
}
