package value_test

import (
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// What a site's boot made is frozen and read by every request at once, and so is every builtin: a
// request that freezes one again (Object.freeze(Math), Object.freeze of a file-level object) writes
// nothing, as freezing a frozen object changes nothing in JavaScript. Until 05/11/2026 each such call
// wrote the mark again, a data race with every other request reading it (go test -race sees it).
func TestFreezingAFrozenValueWritesNothing(t *testing.T) {
	object := value.Object().Set("a", value.Number(1)).Freeze()
	array := value.Array([]value.Value{value.Number(1)}).Freeze()
	function := value.Function(func(...value.Value) value.Value { return value.Undefined() }).Freeze()
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				for _, frozen := range []value.Value{object, array, function} {
					frozen.Freeze()
					if !frozen.IsFrozen() {
						t.Error("a frozen value thawed")
						return
					}
				}
				if !object.Set("a", value.Number(2)).IsInvalid() || !array.Push(value.Number(2)).IsInvalid() {
					t.Error("a frozen value took a write")
					return
				}
			}
		}()
	}
	group.Wait()
}
