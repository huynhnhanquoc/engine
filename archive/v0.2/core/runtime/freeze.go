package runtime

import (
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// A site's file runs once, as the site boots, on a VM of its own; the functions it makes (the handlers)
// then run at every request, many at once, each on a request's VM, and read what the file's top level
// holds. Before 05/11/2026 nothing stopped them writing it: a top-level let assigned by a handler wrote
// the boot VM's map from every request at once, which ends the Go process ("concurrent map writes",
// which no recover catches), a top-level array pushed by every request lost items, and an object's own
// valueOf ran on the boot VM itself, from every request's goroutine. Freeze is what the host calls once
// the file has run, so that what the run made is shared and only read.

// Freeze ends vm's life as a runner and shares what its run made:
//
//   - every object, array and function reachable from the program's top level (its names and its block
//     variables), from roots (what the host kept of the run: the handlers it was handed) and from the
//     variables each such function holds is frozen (value.Freeze): a write to one is a TYPE failure, as
//     a write to a frozen object is;
//   - a variable a function vm made holds (a name of the top level, a let of a function around it) is
//     not written by any run: the write is a TYPE failure (store, STORE_OUTER);
//   - vm runs nothing more. A function it made runs where it is called: a call of the program runs it on
//     the calling VM, as always, and a host call it is handed runs it on the VM that handed it
//     (passLambdas), as does one an object or an array of a request holds (SET, a spread) and one a Go
//     function handed to a host receives (safe given to map: standIn). A function no VM handed over that
//     a host runs (the own valueOf or toString of an object vm made, which value calls itself) is a TYPE
//     failure (runClosure), whatever converts the object: the VM calling cannot be told from inside
//     value, and running it on vm would run one VM from every request at once.
//
// Call it once the run has ended and before any other goroutine reads what it made.
func (vm *VM) Freeze(roots ...value.Value) {
	if vm == nil {
		return
	}
	vm.frozen = true
	walk := &freezeWalk{
		values:  map[value.Value]struct{}{},
		lambdas: map[*value.Lambda]struct{}{},
	}
	for _, item := range vm.Vars {
		walk.value(item)
	}
	for _, item := range vm.Frames[0].Slots {
		walk.value(item)
	}
	for _, item := range roots {
		walk.value(item)
	}
	walk.drain()
}

// Frozen reports whether Freeze has shared what vm's run made.
func (vm *VM) Frozen() bool { return vm != nil && vm.frozen }

// freezeWalk freezes what a frozen VM's run left reachable, each value and each function's captured
// variables once (a structure may hold itself).
type freezeWalk struct {
	values  map[value.Value]struct{}
	lambdas map[*value.Lambda]struct{}
	pending []value.Value
}

// value takes item when it can hold others: an object, an array, a function. A struct is the host's
// and guards itself: value.Freeze tells it it is shared (napas's chain then copies before a step). Every
// other kind is written by no program (a string, a number).
func (walk *freezeWalk) value(item value.Value) {
	if item.IsStruct() {
		item.Freeze()
		return
	}
	if !(item.IsObject() || item.IsArray() || item.IsFunction()) {
		return
	}
	if _, seen := walk.values[item]; seen {
		return
	}
	walk.values[item] = struct{}{}
	walk.pending = append(walk.pending, item)
}

// lambda takes the variables a function holds: the slots and the map of the frame it was made in, and
// those of every function around it.
func (walk *freezeWalk) lambda(lambda *value.Lambda) {
	for ; lambda != nil; lambda = lambda.Parent {
		if _, seen := walk.lambdas[lambda]; seen {
			return
		}
		walk.lambdas[lambda] = struct{}{}
		for _, item := range lambda.Slots {
			walk.value(item)
		}
		for _, item := range lambda.Scope {
			walk.value(item)
		}
	}
}

// drain freezes every value taken and takes what each holds, until none is left.
func (walk *freezeWalk) drain() {
	for len(walk.pending) > 0 {
		item := walk.pending[len(walk.pending)-1]
		walk.pending = walk.pending[:len(walk.pending)-1]
		item.FreezeShared() // the engine's: Object.isFrozen has no answer for it (value.FrozenByEngine)
		if item.IsArray() {
			for _, inner := range item.Items() {
				walk.value(inner)
			}
			continue
		}
		for _, key := range item.Keys() {
			walk.value(item.Get(key))
		}
		if lambda := item.Lambda(); lambda != nil {
			walk.lambda(lambda)
		}
	}
}

// madeFrozen reports a function a frozen VM made: the variables it holds are shared, and only read.
func madeFrozen(lambda *value.Lambda) bool {
	made := maker(lambda)
	return made != nil && made.frozen
}

// frozenOuterWrite is frozenWrite of slot index of the frame owner was made in. It and frozenWrite stay
// out of the instruction loop that calls them: made inline there, a path no run takes but on a refused
// write made the loop larger, and a call of a method of an object 3-5% slower (05/11/2026).
//
//go:noinline
func frozenOuterWrite(owner *value.Lambda, index int) value.Value {
	return frozenWrite(outerSlotName(owner, index))
}

// frozenWrite is the failure of a write to a variable a frozen VM's function holds.
//
//go:noinline
func frozenWrite(name string) value.Value {
	return value.Fail(value.TypeCode, "cannot assign to "+name+": it belongs to the code that ran as the site booted, which every request shares and only reads")
}

// frozenIterationWrite is refused, the failure of `target[key] = …` (SET at ip) on a frozen object, or the
// failure of a write to a variable a frozen VM's function holds when target is the object a loop's
// iteration made for its variables, which a closure the file made writes into (05/11/2026: the refusal
// named the object's key, "cannot assign to read only property 'i#1' of object '#<Object>'", where the
// source wrote i = …). The bytecode tells which, read back on this path only.
//
//go:noinline
func (vm *VM) frozenIterationWrite(refused, target value.Value, key string, ip int) value.Value {
	if !target.IsFrozen() || !strings.ContainsRune(key, SlotBlockMark) {
		return refused
	}
	if sites := vm.program.callSites(); sites != nil {
		if variable, ok := sites.iterationWrite(ip); ok {
			return frozenWrite(variable)
		}
	}
	return refused
}

// frozenRun is the failure of a frozen VM asked to run lambda (runClosure), out of runClosure, which every
// callback of core/value's methods goes through.
func frozenRun(lambda *value.Lambda) value.Value {
	function := "a function"
	if lambda.Name != "" {
		function = "the function " + lambda.Name
	}
	return value.Fail(value.TypeCode, function+" was made as the site booted, so a request runs it only by calling it or handing it to a method, not as an object's own valueOf or toString: make the object in the handler")
}

// endedRun is the failure of a stand-in run after the request that made it ended (runStandIn): a host kept
// the function, or an object holding it, past the host call, and its VM serves another request now.
func endedRun(lambda *value.Lambda) value.Value {
	function := "a function"
	if lambda.Name != "" {
		function = "the function " + lambda.Name
	}
	return value.Fail(value.TypeCode, function+" was handed over by a request that has ended, so it runs nothing more: a host calls what a request hands it while that request runs")
}

// outerSlotName is the name of slot index of the frame owner was made in, for frozenWrite: the slots
// of the function running then (owner.Parent), or of the top level.
func outerSlotName(owner *value.Lambda, index int) string {
	var locals []string
	if owner.Parent != nil {
		locals = owner.Parent.Locals
	} else if program, ok := ProgramFromRef(owner.Program); ok {
		locals = program.locals
	}
	if index < len(locals) {
		return SlotName(locals[index])
	}
	return "a variable"
}
