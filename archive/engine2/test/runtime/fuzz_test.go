package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The verifier and the decoder of a Program read bytes no compiler made: they refuse what is wrong and never panic
// (engine/runtime/verify_test.go and program_binary_test.go of the first engine had these fuzzers; the rewrite had
// none, 13/11/2026). go test -fuzz=FuzzVerify -run=^$ ./test/runtime/ looks for more.

func FuzzVerify(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{byte(runtime.RETURN)})
	f.Add([]byte{byte(runtime.PUSH), 0})
	f.Add([]byte{255, 255, 255})
	if bytecode, err := compiler.CompileSource(`let total = 0; for (let i = 0; i < 3; i++) { total += i; } const f = (a) => a + total;`); err == nil {
		f.Add(bytecode.Program.Instructions())
	} else {
		f.Fatal(err)
	}
	constants := []value.Value{value.String("name"), value.Number(42), value.Closure(&value.Lambda{Address: 0})}
	f.Fuzz(func(t *testing.T, code []byte) {
		_ = runtime.Verify(code, constants)
		_ = runtime.VerifyWithLocals(code, constants, 4)
	})
}

func FuzzUnmarshalProgram(f *testing.F) {
	bytecode, err := compiler.CompileSource(`const answer = 42; const f = (a, b) => a + (b ?? 1) + answer; const r = f(1);`)
	if err != nil {
		f.Fatal(err)
	}
	encoded, err := bytecode.Program.MarshalBinary()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte("KWBC"))
	f.Fuzz(func(t *testing.T, data []byte) {
		program, err := runtime.UnmarshalProgram(data)
		if err != nil {
			return
		}
		if program == nil {
			t.Fatal("decoded without error and without a Program")
		}
		vm := runtime.New(program)
		vm.MaxEnergy = 20_000
		vm.MaxBytes = 16 << 20
		vm.Run()
	})
}
