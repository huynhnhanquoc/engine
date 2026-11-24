package runtime_test

import (
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// A write through a time, a duration or bytes is the TYPE error a write through a string is, said where it stands
// (18/11/2026, docs/language.md mục 8.4): it was dropped without a word since the review of 16/10/2026, so t.x = 1
// went on and t.x read undefined, a value other than JavaScript's, which writes a Date's key and a Uint8Array's item.
// Kitwork's time, duration and bytes hold no key and no written item; an object still takes the write.
//
// CONTROL (18/11/2026): with the three kinds out of cannotHoldProperties, the first four were "written".
func TestAWriteThroughATimeADurationOrBytesIsTheTypeError(t *testing.T) {
	program := load(t, `const failed = (f) => { const r = safe(f); return r.error === null ? "written" : r.error.code + " " + r.error.message; };
result = [failed(() => { at.x = 1; }), failed(() => { gap.x = 1; }), failed(() => { data[0] = 5; }), failed(() => { data.x = 1; }),
  failed(() => { const o = {}; o.x = 1; })];`)
	program.globals["at"] = value.Time(time.Unix(0, 0).UTC())
	program.globals["gap"] = value.Duration(time.Minute)
	program.globals["data"] = value.Bytes([]byte{1, 2})
	program.run(t)
	want := `array ["TYPE không ghi được at.x: at là time, không phải object hay mảng","TYPE không ghi được gap.x: gap là duration, không phải object hay mảng","TYPE không ghi được data[0]: data là bytes, không phải object hay mảng","TYPE không ghi được data.x: data là bytes, không phải object hay mảng","written"]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
