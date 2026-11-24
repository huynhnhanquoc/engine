package value_test

import (
	"testing"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

// A method converts an argument that could run code to convert (an object, an array) before it runs, as
// JavaScript does (conversion.go), and only a call that converts one pays for it: the hints of a method
// are found with the method, so a.push(o) looks at no argument; a.indexOf(o), which keeps o as it is, and
// Object.hasOwn(o, "k") allocate nothing (05/11/2026: the hints were looked up in two maps whenever an
// argument was an object, so a.push(o) cost 65% more than a.push(1), and a kept object cost a slice it
// was copied into).
func TestAMethodPaysForTheArgumentsItConverts(t *testing.T) {
	o := value.Object().Set("k", value.Number(1))
	items := value.Array([]value.Value{value.Number(1), value.Number(2), o})
	object := []value.Value{o}
	globals := map[string]value.Value{}
	builtins.Install(globals)
	hasOwn := globals["Object"].Get("hasOwn")
	key := []value.Value{o, value.String("k")}
	if allocations := testing.AllocsPerRun(100, func() {
		sinkValue = items.Invoke("indexOf", object)
		sinkValue = items.Invoke("includes", object)
		sinkValue = hasOwn.Call(key)
	}); allocations != 0 {
		t.Errorf("a.indexOf(o), a.includes(o) and Object.hasOwn(o, \"k\") allocate %.0f times, want none: o is kept as it is", allocations)
	}
	if got := items.Invoke("indexOf", object); got.Number() != 2 {
		t.Errorf("a.indexOf(o): %s, want 2", got.Failure()+got.Kind())
	}
	if got := hasOwn.Call(key); !got.Boolean() {
		t.Errorf("Object.hasOwn(o, \"k\"): %s, want true", got.Failure()+got.Kind())
	}
	one := []value.Value{value.Number(1)}
	pushing := func(arguments []value.Value) func(int) {
		return func(rounds int) {
			list := value.Array(make([]value.Value, 0, 64))
			for i := 0; i < rounds; i++ {
				if i%64 == 0 {
					list = value.Array(make([]value.Value, 0, 64))
				}
				sinkValue = list.Invoke("push", arguments)
			}
		}
	}
	requireCost(t, "a.push(o) against a.push(1)", pushing(object), pushing(one), 1.3)
}
