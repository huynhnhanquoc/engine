package runtime_test

import (
	"strings"
	"testing"
	"time"
)

// [...a, x] and f(...args) spread as JavaScript spreads (15/11/2026, round 5 of the 13/11 review; runtime spread.go):
// an array's items, a string's code points (one item for a character outside the basic plane), in order with the
// items written beside them; a call's arguments laid out for a lambda, a builtin, a method of a value, a date made
// with new, through ?. too. They were the compile error "Kitwork chưa có ... ở chỗ này", the array to be joined with
// a.concat(b). CONTROL: SPREAD appending the value itself for a spread, red.
func TestAnArrayAndACallSpread(t *testing.T) {
	program := load(t, `const list = [1, 5, 3];
const add = (a, b, c) => a + b + c;
const none = null;
const pushed = [0];
const length = pushed.push(...list);
const result = [
  [...list, 4], [0, ...[...list]], [..."a😀"], [...[]], [...list, ...list],
  Math.max(...list), add(...[1, 2], 3), add(...list), [1].concat(...[[2], [3]]), pushed, length,
  new Date(...[86400000]).getTime(), none?.max(...list), none?.(...list), String(...["x"])
];`)
	program.run(t)
	want := `array [[1,5,3,4],[0,1,5,3],["a","😀"],[],[1,5,3,1,5,3],5,6,9,[1,2,3],[0,1,5,3],4,86400000,null,null,"x"]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// What is not iterable is the spread's TYPE failure, at its ..., as JavaScript throws "is not iterable": an object (with
// the way to spread its keys, values or entries), null, undefined, a number. A call spreads 65535 arguments at most,
// past them INVALID_ARGUMENT, as JavaScript's engines give a RangeError past theirs.
func TestASpreadOfWhatIsNotIterableIsItsFailure(t *testing.T) {
	program := load(t, `const failed = (f) => { const r = safe(f); return [r.error.code, r.error.at + ": " + r.error.message]; };
const many = Array.from({ length: 70000 }, (_, i) => i);
const result = [failed(() => [...{ a: 1 }]), failed(() => [...null]), failed(() => Math.max(...undefined)), failed(() => [...5]),
  failed(() => Math.max(...many))];`)
	program.run(t)
	got := describe(program.variable("result"))
	for _, want := range []string{
		`["TYPE","<source>:3:31: ...: một object không trải được vào mảng hay lời gọi; trải các khoá, giá trị hay cặp của nó: [...Object.keys(o)], [...Object.values(o)], [...Object.entries(o)]"]`,
		`["TYPE","<source>:3:60: ...: null không trải được: chỉ trải mảng, chuỗi hay bytes"]`,
		`["TYPE","<source>:3:93: ...: undefined không trải được: chỉ trải mảng, chuỗi hay bytes"]`,
		`["TYPE","<source>:3:123: ...: number không trải được: chỉ trải mảng, chuỗi hay bytes"]`,
		`["INVALID_ARGUMENT","<source>:4:20: một lời gọi trải nhiều nhất 65535 đối số"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got  %s\nwant it to hold %s", got, want)
		}
	}
}

// A spread costs the run's energy for each item it reads, as an object's spread of as many keys does, and the items
// it adds are what the run makes: a loop of spreads of a large array stops at its energy within a few times a loop of
// additions; an array of 50,000 items spread twice under a ceiling of 1 MiB stops at the run's memory.
func TestASpreadCostsItsItems(t *testing.T) {
	const forever = "for (let i = 0; i < 100000000; i++) "
	_, _, plain := spentUnder(t, "let x = 0; "+forever+"{ x = x + i; }", requestEnergy, 10*time.Second)
	message, spent, took := spentUnder(t, `const a = Array.from({ length: 20000 }, (_, j) => j); `+forever+`{ const p = [...a, ...a]; }`, requestEnergy, 5*time.Second)
	if message == "" || spent < requestEnergy {
		t.Fatalf("ended %q after %v having spent %d units, want its energy spent", message, took, spent)
	}
	if !underRace && testing.CoverMode() == "" && took > 25*plain+100*time.Millisecond {
		t.Errorf("took %v for its energy, %.0f times a loop of additions (%v)", took, float64(took)/float64(plain), plain)
	}
	program := load(t, `const a = Array.from({ length: 50000 }, (_, j) => j); const p = [...a, ...a];`)
	program.vm.FastReset(program.program, program.globals)
	program.vm.MaxBytes = 4 << 20
	if result := program.vm.Run(); !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit Exceeded") {
		t.Fatalf("ended %v, want the run's memory spent", result)
	}
}
