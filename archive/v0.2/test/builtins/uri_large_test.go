//go:build !race

package builtins_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// encodeURIComponent and encodeURI write three bytes for each byte they escape, so an input of 180 MB
// of "ạ" was a result of 537 MB, past the longest string, two gigabytes allocated for one call. Past it
// they are RANGE before anything is written, as JavaScript throws "Invalid string length" (05/11/2026);
// under it they encode as before. Left out under the race detector, whose shadow memory would multiply the
// input.
func TestEncodingAURIPastTheLongestStringIsRefused(t *testing.T) {
	past := str(strings.Repeat("ạ", 59_652_321)) // 9 bytes each once escaped: 536,870,889
	for _, name := range []string{"encodeURIComponent", "encodeURI"} {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		got := call(name, "", past)
		runtime.ReadMemStats(&after)
		if got.Code() != value.RangeCode || got.Message() != "invalid string length" {
			t.Errorf("%s of 59,652,321 'ạ': %s of %d bytes, want RANGE: invalid string length", name, got.Failure(), len(got.Text()))
		}
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<20 {
			t.Errorf("%s: %d MB allocated to refuse it", name, grown>>20)
		}
	}
	if got := call("encodeURIComponent", "", str(strings.Repeat("a", 200_000_000))); len(got.Text()) != 200_000_000 {
		t.Errorf("encodeURIComponent of 200,000,000 letters, counted and kept: %s of %d bytes", got.Failure(), len(got.Text()))
	}
}
