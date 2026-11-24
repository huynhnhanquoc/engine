package compiler_test

import "testing"

// sort sorts the items the array held when it began, then writes them all back, as JavaScript's
// SortIndexedProperties does: a comparator that removes, adds or changes items loses none and writes none
// twice (05/11/2026; core/value sorted the slice it took while the comparator resliced the array, so a pop
// left [1,2] and a shift left [2,1,4,4], unsorted). Each result is node 22's.
func TestASortKeepsEveryItemItsComparatorMoves(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`(() => { const a = [3, 1, 2]; let n = 0; a.sort((p, q) => { n = n + 1; if (n === 1) { a.pop() } return p - q }); return a })()`, `[1,2,3]`},
		{`(() => { const a = [3, 1, 2, 5, 4]; let n = 0; a.sort((p, q) => { n = n + 1; if (n === 1) { a.shift() } return p - q }); return a })()`, `[1,2,3,4,5]`},
		{`(() => { const a = [3, 1, 2]; let n = 0; a.sort((p, q) => { n = n + 1; if (n === 1) { a.splice(0, 2) } return p - q }); return a })()`, `[1,2,3]`},
		{`(() => { const a = [3, 1, 2]; let n = 0; a.sort((p, q) => { n = n + 1; if (n === 1) { a.push(9) } return p - q }); return a })()`, `[1,2,3,9]`},
		{`(() => { const a = [3, 1, 2]; let n = 0; a.sort((p, q) => { n = n + 1; if (n === 1) { a[0] = 99 } return p - q }); return a })()`, `[1,2,3]`},
		{`(() => { const a = [3, 1, 2]; return [a.sort((p, q) => p - q) === a, a] })()`, `[true,[1,2,3]]`},
	} {
		got := runSource(t, "const result = JSON.stringify("+c.source+");")
		if got.Text() != c.want {
			t.Errorf("%s\n gives %s, want %s", c.source, got.Failure()+got.Text(), c.want)
		}
	}
}
