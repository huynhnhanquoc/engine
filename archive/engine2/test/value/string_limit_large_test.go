//go:build !race

package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// TestEveryWayToBuildAStringStopsAtTheLongest builds one string of 2^28 + 8 bytes, half the longest and a
// little more, and joins it to itself every way a program can: each is the RANGE failure, refused before
// the result is written. It needs a few hundred megabytes, so it is left out under the race detector,
// whose shadow memory would multiply them. The builders a run gives its budget to are given one without a
// ceiling, as a run without one gives (budget_test.go): without any they stop at UnbudgetedLength.
func TestEveryWayToBuildAStringStopsAtTheLongest(t *testing.T) {
	half := value.String(strings.Repeat("x", 1<<28+8))
	both := value.Array([]value.Value{half, half})
	nested := value.Array([]value.Value{value.Array([]value.Value{half}), value.Array([]value.Value{half})})
	for name, got := range map[string]value.Value{
		"s + s":                      half.Add(half),
		"s.concat(s)":                half.Concat(half),
		"[s, s].join(\"\")":          both.InvokeWithin(unlimited{}, "join", []value.Value{value.String("")}),
		"String([s, s])":             both.TextWithin(unlimited{}),
		"[[s], [s]].join()":          nested.InvokeWithin(unlimited{}, "join", nil),
		"JSON.stringify([s, s])":     value.StringifyWithin(both, "", unlimited{}),
		"s.padEnd(2 * s.length)":     half.PadEnd(value.Number(float64(2 * len(half.Text())))),
		"s.repeat(2)":                half.Repeat(value.Number(2)),
		"s.replace(\"x\", s)":        half.Replace(value.String("x"), half),
		"`${s}${s}` as + is written": value.String("").Add(half).Add(half),
	} {
		if got.Code() != value.RangeCode || got.Message() != "invalid string length" {
			t.Errorf("%s: %s, want RANGE: invalid string length", name, got.Failure())
		}
	}
}
