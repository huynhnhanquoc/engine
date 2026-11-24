package compiler_test

import (
	"strings"
	"testing"
)

// A program freezes an array as it freezes an object (04/11/2026; until then Object.freeze([1, 2]) was an
// UNSUPPORTED failure): every write through it, a[i] = v at an item it has or past its end included, is
// a TYPE failure that ends the run, and reads and copies are as before.
func TestAProgramCannotWriteAFrozenArray(t *testing.T) {
	for _, c := range []struct{ source, message string }{
		{`const a = Object.freeze([1, 2]); a[0] = 5`, "cannot assign to read only property '0'"},
		{`const a = Object.freeze([1, 2]); a[2] = 5`, "cannot add property 2, object is not extensible"},
		{`const a = Object.freeze([1, 2]); a[9] = 5`, "cannot add property 9, object is not extensible"},
		{`const a = Object.freeze([1, 2]); a.push(3)`, "cannot add property 2, object is not extensible"},
		{`const a = Object.freeze([3, 1, 2]); a.sort((x, y) => x - y)`, "cannot assign to read only property '0'"},
	} {
		got := runSource(t, c.source)
		if !got.IsInvalid() || !strings.Contains(got.Message(), c.message) {
			t.Errorf("%s: %s, want a failure saying %q", c.source, got.Failure(), c.message)
		}
	}
	got := runSource(t, `const a = Object.freeze([3, 1, 2]);
const result = [Object.isFrozen(a), a.length, a[0], a.map((x) => x * 2).join(","), a.slice().sort().join(","), safe(() => a.pop()).error.code, a.join(",")].join(" ")`)
	if want := "true 3 3 6,2,4 1,2,3 TYPE 3,1,2"; got.Text() != want {
		t.Errorf("reads of a frozen array: %q, want %q (%s)", got.Text(), want, got.Failure())
	}
}

// A frozen array's refusals are worded as node 22 words them, the holder named: "of object
// '[object Array]'" for an assignment, "of [object Array]" for a removal (05/11/2026; the holder was left
// out). A key that is no index, a negative number, a text and length, is refused as on a frozen object,
// before it is looked at, where an array that is not frozen refuses it as a key of no item.
func TestAFrozenArraysRefusalsAreNodes(t *testing.T) {
	for _, c := range []struct{ source, message string }{
		{`const a = Object.freeze([1, 2]); a[0] = 5`, "cannot assign to read only property '0' of object '[object Array]'"},
		{`const a = Object.freeze([1, 2]); a[2] = 5`, "cannot add property 2, object is not extensible"},
		{`const a = Object.freeze([1, 2]); a.pop()`, "cannot delete property '1' of [object Array]"},
		{`const a = Object.freeze([1, 2]); a.push()`, "cannot assign to read only property 'length' of object '[object Array]'"},
		{`const a = Object.freeze([1, 2]); a[-1] = 5`, "cannot add property -1, object is not extensible"},
		{`const a = Object.freeze([1, 2]); a.x = 1`, "cannot add property x, object is not extensible"},
		{`const a = Object.freeze([1, 2]); a.length = 0`, "cannot assign to read only property 'length' of object '[object Array]'"},
		{`const a = Object.freeze([1, 2]); a[1.5] = 0`, "cannot add property 1.5, object is not extensible"},
		{`const o = Object.freeze({ a: 1 }); o.a = 2`, "cannot assign to read only property 'a' of object '#<Object>'"},
		{`Math.PI = 3`, "cannot assign to read only property 'PI' of object '#<Object>'"},
	} {
		got := runSource(t, c.source)
		if !got.IsInvalid() || !strings.HasSuffix(got.Message(), c.message) {
			t.Errorf("%s: %s, want a failure ending %q", c.source, got.Failure(), c.message)
		}
	}
	if got := runSource(t, `const a = [1, 2]; a[-1] = 5`); !got.IsInvalid() || got.Code() == "" || strings.Contains(got.Message(), "extensible") {
		t.Errorf("an array not frozen refuses a[-1] as a key of no item: %s", got.Failure())
	}
}
