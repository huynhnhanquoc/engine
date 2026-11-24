package runtime_test

import (
	"sync"
	"testing"
)

// A sort that may run program code sorts a private copy of the items, which sorts take from a pool and give
// back cleared (value.Sort, 05/11/2026). A comparator that sorts, another array or the array being sorted,
// and an item's own toString that sorts, each take a copy of their own, and VMs sorting at once on other
// goroutines never share one. The answer is node 22's for the same program.
func TestSortsInsideAComparatorAndAtOnceEachSortTheirOwnCopy(t *testing.T) {
	const source = `const inner = [3, 1, 2];
const outer = [5, 4, 3, 2, 1];
outer.sort((a, b) => { inner.sort((x, y) => y - x); return a - b; });
const same = [3, 1, 2, 5, 4];
let depth = 0;
const seen = [];
same.sort((a, b) => {
  if (depth === 0) {
    depth = 1;
    seen.push(same.sort((x, y) => y - x).join(""));
    depth = 0;
  }
  return a - b;
});
const words = [{ n: "b" }, { n: "a" }, { n: "c" }];
const keys = words.map((w) => ({ toString: () => { [9, 7, 8].sort(); return w.n; } }));
keys.sort();
const many = [];
for (let i = 0; i < 40; i++) { many.push((i * 7) % 40); }
many.sort((a, b) => { [2, 1].sort((x, y) => x - y); return b - a; });
answer = JSON.stringify([outer, inner, same, seen.length > 0, seen[0], keys.map((k) => k.toString()), many.slice(0, 5)]);`
	const node = `[[1,2,3,4,5],[3,2,1],[1,2,3,4,5],true,"54321",["a","b","c"],[39,38,37,36,35]]`

	program := load(t, source)
	program.run(t)
	if got := program.variable("answer").Text(); got != node {
		t.Fatalf("answer = %s, node gives %s", got, node)
	}

	var wrong sync.Map
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		each := load(t, source)
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 50; round++ {
				each.vm.FastReset(each.program, each.globals)
				if result := each.vm.Run(); result.IsInvalid() {
					wrong.Store(result.Message(), true)
					return
				}
				if got := each.variable("answer").Text(); got != node {
					wrong.Store(got, true)
				}
			}
		}()
	}
	group.Wait()
	wrong.Range(func(got, _ any) bool {
		t.Errorf("a sort beside others answered %v, node gives %s", got, node)
		return true
	})
}
