package runtime_test

import (
	"strings"
	"testing"
	"time"
)

// for…of walks an array, a string or bytes; anything else is its TYPE failure, as JavaScript throws "is not
// iterable" (15/11/2026, round 3 of the 13/11 review): an object walked as many undefined as it had keys, and null,
// undefined or a number did not walk at all, in silence. safe() reads it as any runtime error.
func TestForOfWalksWhatIsIterable(t *testing.T) {
	program := load(t, `const walked = (x) => { const out = []; for (const item of x) { out.push(item); } return out; };
const failed = (x) => { const r = safe(() => walked(x)); return [r.error.code, r.error.at + ": " + r.error.message]; };
const result = [walked([1, 2]), walked("aệ"), failed({ a: 1 }), failed(null), failed(undefined), failed(5)];`)
	program.run(t)
	want := `array [[1,2],["a","ệ"],["TYPE","<source>:1:60: for…of: một object không lặp được; lặp các khoá, giá trị hay cặp của nó: for (const key of Object.keys(o)), Object.values(o), for (const [key, value] of Object.entries(o))"],["TYPE","<source>:1:60: for…of: null không lặp được: chỉ lặp mảng, chuỗi hay bytes"],["TYPE","<source>:1:60: for…of: undefined không lặp được: chỉ lặp mảng, chuỗi hay bytes"],["TYPE","<source>:1:60: for…of: number không lặp được: chỉ lặp mảng, chuỗi hay bytes"]]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// { ...v } of an array, a string or bytes writes each item, each UTF-16 unit, each byte under its index, as
// JavaScript spreads them, where it gave {}; null, undefined, a number, a boolean give nothing, as in JavaScript.
// Indexes come first, as an object lists them.
func TestASpreadOfAnArrayOrAStringWritesItsItems(t *testing.T) {
	program := load(t, `const result = [{ ...[1, 2] }, { ..."xệ" }, { a: 0, ...[9] }, { ...null, ...undefined, ...5, ...true }, { ...[1, 2], ...[3] }];`)
	program.run(t)
	want := `array [{"0":1,"1":2},{"0":"x","1":"ệ"},{"0":9,"a":0},{},{"0":3,"1":2}]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A spread of a large array costs the run's energy as an object's spread of as many keys does: a loop of them stops
// at its energy, within a few times a loop of additions.
func TestASpreadOfALargeArrayCostsItsKeys(t *testing.T) {
	const forever = "for (let i = 0; i < 100000000; i++) "
	_, _, plain := spentUnder(t, "let x = 0; "+forever+"{ x = x + i; }", requestEnergy, 10*time.Second)
	message, spent, took := spentUnder(t, `const a = Array.from({ length: 20000 }, (_, j) => j); `+forever+`{ const p = { ...a, ...a, ...a, ...a }; }`, requestEnergy, 5*time.Second)
	if message == "" || spent < requestEnergy {
		t.Fatalf("ended %q after %v having spent %d units, want its energy spent", message, took, spent)
	}
	if !underRace && testing.CoverMode() == "" && took > 25*plain+100*time.Millisecond {
		t.Errorf("took %v for its energy, %.0f times a loop of additions (%v)", took, float64(took)/float64(plain), plain)
	}
}

// The keys a spread of an array adds are what the run makes: under a ceiling of 4 MiB, an array of 50,000 items
// (1.6 MB) spread into an object (3.2 MB of keys more) stops at the run's memory.
func TestASpreadOfAnArrayCountsTheKeysItAdds(t *testing.T) {
	program := load(t, `const a = Array.from({ length: 50000 }, (_, j) => j); const p = { ...a };`)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxBytes = 4 << 20
	if result := program.vm.Run(); !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit Exceeded") {
		t.Fatalf("ended %v, want the run's memory spent", result)
	}
}
