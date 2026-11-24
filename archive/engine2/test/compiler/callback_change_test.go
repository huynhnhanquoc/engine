package compiler_test

import "testing"

// An array method walks the length the array had when it began and reads each item as it reaches it,
// as JavaScript does: an item its callback removes is passed over (read as undefined by find and
// findIndex), one it changes is read as it is now, one it adds is not visited (05/11/2026; the runtime's
// own map, filter, find, forEach, some, every, findIndex and reduce walked the items they found at the
// start, and core/value's flatMap flattened undefined for an item removed). Each result is node 22's.
func TestAMethodReadsTheArrayItsCallbackChanges(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{`(() => { const a = [1, 2, 3, 4]; const r = []; a.forEach((x) => { r.push(x); a.shift() }); return [r, a] })()`, `[[1,3],[3,4]]`},
		{`(() => { const a = [1, 2, 3, 4]; const r = []; r.push(a.find((x) => { r.push(x); a.splice(0, a.length); return false })); return [r, a] })()`, `[[1,null,null,null,null],[]]`},
		{`(() => { const a = [1, 2, 3, 4]; const r = []; r.push(a.reduce((s, x) => { a.pop(); return s + x }, 0)); return [r, a] })()`, `[[3],[1,2]]`},
		{`(() => { const a = [1, 2, 3, 4]; return [a.map((x) => { a.pop(); return x * 10 }).join("|"), a.length] })()`, `["10|20||",2]`},
		{`(() => { const a = [1, 2, 3, 4]; return [a.filter((x) => { a.pop(); return true }), a] })()`, `[[1,2],[1,2]]`},
		{`(() => { const a = [1, 2, 3, 4]; let n = 0; const s = a.some((x) => { n = n + 1; a.pop(); return false }); return [s, n] })()`, `[false,2]`},
		{`(() => { const a = [1, 2, 3, 4]; let n = 0; const e = a.every((x) => { n = n + 1; a.pop(); return true }); return [e, n] })()`, `[true,2]`},
		{`(() => { const a = [1, 2, 3]; const seen = []; const i = a.findIndex((x) => { seen.push(x); a.pop(); return false }); return [i, seen.join("|")] })()`, `[-1,"1|2|"]`},
		{`(() => { const a = [1, 2, 3]; const r = []; a.forEach((x) => { r.push(x); if (a.length < 6) { a.push(x * 10) } }); return [r, a] })()`, `[[1,2,3],[1,2,3,10,20,30]]`},
		{`(() => { const a = [1, 2, 3]; const r = []; a.forEach((x, i) => { r.push(x); if (i === 0) { a[2] = 9 } }); return r })()`, `[1,2,9]`},
		{`(() => { const a = [1, 2, 3, 4]; const r = a.flatMap((x) => { a.pop(); return [x, x] }); return [r, a] })()`, `[[1,1,2,2],[1,2]]`},
	} {
		got := runSource(t, "const result = JSON.stringify("+c.source+");")
		if got.Text() != c.want {
			t.Errorf("%s\n gives %s, want %s", c.source, got.Failure()+got.Text(), c.want)
		}
	}
}
