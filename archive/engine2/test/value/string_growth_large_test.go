//go:build !race

package value_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A string that grows as it is written passes the longest string from an input well below it: an escape
// of JSON writes six bytes for a control character, a filler of other than ASCII has more bytes than
// units. Each is counted before anything is written (05/11/2026): JSON.stringify of 90 million U+0001
// built 537 million bytes for a few instructions, allocating 3.4 GB, and 'a'.padEnd(179000000, 'ạ') built
// 537 million bytes too, the target being counted in units. Each case is the RANGE failure, and the
// refusal allocates almost nothing: an input of 90 MB, not the gigabytes the result would take. Left out
// under the race detector, whose shadow memory would multiply the input. JSON.stringify is given a budget
// without a ceiling, a run's without one (budget_test.go).
func TestAStringThatGrowsAsItIsWrittenIsCountedFirst(t *testing.T) {
	controls := value.String(strings.Repeat(string(rune(1)), 89_478_486)) // 6 bytes each, 30 past
	quotes := value.String(strings.Repeat(`"`, 268_435_444))              // 2 bytes each, 2 past
	vietnamese := value.String("a")
	for name, build := range map[string]func() value.Value{
		"JSON.stringify(s) of control characters": func() value.Value { return value.StringifyWithin(controls, "", unlimited{}) },
		"JSON.stringify(s) of quotes":             func() value.Value { return value.StringifyWithin(quotes, "", unlimited{}) },
		"JSON.stringify([s])": func() value.Value {
			return value.StringifyWithin(value.Array([]value.Value{controls}), "", unlimited{})
		},
		"JSON.stringify({ [s]: 1 })": func() value.Value {
			return value.StringifyWithin(value.Object().Set(controls.Text(), value.Number(1)), "", unlimited{})
		},
		"'a'.padEnd(179000000, 'ạ')": func() value.Value {
			return vietnamese.PadEnd(value.Number(179_000_000), value.String("ạ"))
		},
		"'a'.padStart(536870888, '😀')": func() value.Value {
			return vietnamese.PadStart(value.Number(536_870_888), value.String("😀"))
		},
	} {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		got := build()
		runtime.ReadMemStats(&after)
		if got.Code() != value.RangeCode || got.Message() != "invalid string length" {
			t.Errorf("%s: %s of %d bytes, want RANGE: invalid string length", name, got.Failure(), len(got.Text()))
		}
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<20 {
			t.Errorf("%s: %d MB allocated to refuse it, want what it reads only", name, grown>>20)
		}
	}
	// a text whose escapes would not pass it is written: one long enough to be counted, with none
	if got := value.StringifyWithin(value.String(strings.Repeat("x", 100_000_000)), "", unlimited{}); len(got.Text()) != 100_000_002 {
		t.Errorf("JSON.stringify of 100,000,000 letters: %s of %d bytes, want 100,000,002", got.Failure(), len(got.Text()))
	}
}
