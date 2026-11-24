package runtime_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// Object.groupBy (19/11/2026, decided 27/10/2026, docs/language.md mục 4.3) is JavaScript's, held to node case by case
// in parity_test.go. What node cannot hold is Kitwork's: a time or a function as a key has no text, the TYPE error a
// String(t) is; bytes are walked as for…of walks them, their numbers; and what it makes is asked of the run as it grows,
// so grouping 100,000 items each its own key under a ceiling of 4 MiB stops as it grows (12 MB allocated in all).
//
// CONTROL (19/11/2026): with the key made by ToString().Text(), the time and the function were grouped under ""; with
// bytes not walked, they were the TYPE failure "bytes is not iterable"; with each piece asked of the run alone
// (value.Afford), the run allocated 56 MB before its count at the end of the call stopped it.
func TestObjectGroupByIsJavaScriptsWithKitworksKinds(t *testing.T) {
	program := load(t, `const failed = (f) => { const r = safe(f); return r.error === null ? "grouped" : r.error.code + " " + r.error.message; };
result = [failed(() => Object.groupBy([1], (x) => at)), failed(() => Object.groupBy([1], (x) => failed)),
  Object.groupBy(data, (b) => b % 2)];`)
	program.globals["at"] = value.Time(time.Unix(0, 0).UTC())
	program.globals["data"] = value.Bytes([]byte{1, 2, 3})
	program.run(t)
	got := describe(program.variable("result"))
	for _, want := range []string{
		`"TYPE một thời gian không đổi thành chữ`,
		`"TYPE một hàm không đổi thành chữ`,
		`{"0":[2],"1":[1,3]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got  %s\nwant it to hold %s", got, want)
		}
	}

	big := load(t, `const a = Array.from({ length: 100000 }, (_, j) => j); const g = Object.groupBy(a, (x) => x);`)
	big.vm.FastReset(big.program, big.globals)
	big.vm.MaxBytes = 4 << 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result := big.vm.Run()
	runtime.ReadMemStats(&after)
	if !result.IsInvalid() || !strings.Contains(result.Message(), "Memory Limit Exceeded") {
		t.Errorf("100,000 groups under 4 MiB ended %v, want the run's memory spent", result)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 24<<20 {
		t.Errorf("allocated %d MB before it stopped, want it stopped as it grew", allocated>>20)
	}
}
