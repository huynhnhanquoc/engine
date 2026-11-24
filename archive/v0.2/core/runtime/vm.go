package runtime

import (
	"github.com/kitwork/engine2/core/value"
)

func (vm *VM) Defer(fn *value.Lambda) {
	if vm.FrameIdx >= 0 {
		f := &vm.Frames[vm.FrameIdx]
		f.Defers = append(f.Defers, fn)
	}
}

// lookupScopeChain tìm biến dọc theo chuỗi closure bao ngoài (lexical scoping).
// Cho phép lambda lồng nhiều cấp đọc biến của mọi hàm bao ngoài, đúng ngữ nghĩa JS.
func lookupScopeChain(fn *value.Lambda, name string) (value.Value, bool) {
	for ; fn != nil; fn = fn.Parent {
		if fn.Scope != nil {
			if v, ok := fn.Scope[name]; ok {
				return v, true
			}
		}
	}
	return value.Undefined(), false
}

// storeScopeChain writes name in the nearest map of the functions around fn that holds it; stored is
// false when none does, and the name is then the frame's own. A map a function of a frozen VM holds (a
// file's top level once the site has booted, Freeze) is shared by every request and is not written:
// refused is true, and the write is a TYPE failure.
func storeScopeChain(fn *value.Lambda, name string, val value.Value) (stored, refused bool) {
	for ; fn != nil; fn = fn.Parent {
		if fn.Scope != nil {
			if _, ok := fn.Scope[name]; ok {
				if madeFrozen(fn) {
					return false, true
				}
				fn.Scope[name] = val
				return true, false
			}
		}
	}
	return false, false
}

// arrayCallback is the function an array method (map, filter, forEach…) calls for each element: a
// script lambda, or a function of the engine (console.log, Boolean, parseInt), called the same way.
type arrayCallback struct {
	lambda *value.Lambda
	native value.Value
}

// newArrayCallback takes an array method's first argument; ok is false when it is not a function.
func newArrayCallback(argument value.Value) (arrayCallback, bool) {
	if lambda := argument.Lambda(); lambda != nil {
		return arrayCallback{lambda: lambda}, true
	}
	if argument.IsFunction() {
		return arrayCallback{native: argument}, true
	}
	return arrayCallback{}, false
}

func (vm *VM) callback3(cb arrayCallback, first, second, third value.Value) value.Value {
	if cb.lambda != nil {
		return vm.executeLambda3(cb.lambda, first, second, third)
	}
	return vm.nativeCallback(cb.native, first, second, third)
}

func (vm *VM) callback4(cb arrayCallback, first, second, third, fourth value.Value) value.Value {
	if cb.lambda != nil {
		return vm.executeLambda4(cb.lambda, first, second, third, fourth)
	}
	return vm.nativeCallback(cb.native, first, second, third, fourth)
}

// nativeCallback calls a function of the engine as CALL calls it. safe is a plain builtin now (core/builtins),
// called like any other function: what it cannot catch has already stopped the run (vm.stop), which the
// instruction loop reads after the INVOKE that ran this callback.
//
// A function another VM made among args (an item of a frozen file's array, handed to safe by map) is
// handed over as a stand-in that runs on this VM (standIn), as passLambdas hands one a host is given
// directly (05/11/2026: tasks.map(safe) ran each task on the frozen boot VM, a TYPE failure).
//
// What it gives is counted as a function of the host's result is (memory.go, chargeCall): whole when it made it,
// nothing when it gave an argument back (Boolean, an item): a.map(JSON.stringify) made 20 MB the meter counted as
// 200 items.
func (vm *VM) nativeCallback(function value.Value, args ...value.Value) value.Value {
	for index, argument := range args {
		if argument.IsFunction() {
			args[index] = vm.standIn(argument)
		}
	}
	instructions := vm.instructions
	result := vm.nativeValue("callback call", func() value.Value {
		return function.CallWithin(vm, args)
	})
	if vm.MaxBytes == 0 || !result.Holds() {
		return result // a number, a boolean (Boolean, parseInt): nothing made
	}
	return vm.chargeCallback(function, args, result, instructions)
}

// chargeCallback counts what a function of the engine called back gave (nativeCallback): nothing when it gave
// one of its arguments back, whole when it made it all (no program code ran in it, or it gives fresh), as itself
// otherwise. The failure once past the ceiling; result otherwise.
func (vm *VM) chargeCallback(function value.Value, args []value.Value, result value.Value, instructions uint64) value.Value {
	for _, argument := range args {
		if result.Shares(argument) {
			return result
		}
	}
	amount := result.Shallow() // a text's whole (String, JSON.stringify), which the walk below would only repeat
	if (result.IsArray() || result.IsObject()) && (function.GivesFresh() || vm.instructions == instructions) {
		amount = result.Footprint(vm.MaxBytes) // up to the whole ceiling, as chargeCall walks (memory.go)
	}
	if vm.consumeBytes(amount) {
		return result
	}
	return vm.memoryLimit(vm.Frames[vm.FrameIdx].LastIP)
}

// arrayCallbackMethod executes the Array methods that call back once per item (map, filter, find,
// forEach, some, every, findIndex, reduce) on this VM, each callback bound straight into its frame with
// no argument slice. core/value has the same methods, which reuse one argument slice per call; measured
// 04/11/2026 (after slot locals, bindParameters one by one and the unlisted own closure of passLambdas),
// they cost 4-6% more per callback for forEach, reduce and some, 14-16% for map and filter (their
// callback writes its arguments into its slice and the frame takes them again, each write a write
// barrier while a collection runs), and about 90 ns and one allocation (that slice) more per call:
// i_callback, three items a call, 18% slower. So these stay here until a callback of core/value's
// methods can be bound straight into its frame.
// A method of JavaScript hands its callback the item, its index and the array, reduce the accumulator
// before them, as JavaScript does ([5].map((x, i, a) => a) is [[5]]; until 03/11/2026 the array was
// missing).
//
// A method walks the length the array had when it began and reads each item as it reaches it, as
// JavaScript does (05/11/2026; until then it walked the items it found at the start, so a callback that
// removed items still saw them): an item the callback adds is not visited, one it changes is read as it
// is now, and one it removes (an index past the array's length now) is passed over by map, filter,
// forEach, some, every and reduce, and read as undefined by find and findIndex. map leaves undefined in
// the place of an item passed over, JavaScript's hole.
// Trả về (kết quả, true) nếu method được xử lý tại đây; ngược lại (zero, false).
func (vm *VM) arrayCallbackMethod(
	target value.Value,
	m string,
	cb arrayCallback,
	initial value.Value,
	hasInitial bool,
) (value.Value, bool) {
	if !target.IsArray() {
		return value.Undefined(), false
	}
	length := len(target.Items())

	if cb.lambda == nil && !cb.native.IsFunction() {
		return value.Undefined(), false
	}

	switch m {
	case "map":
		// the array it gives, on the stack above the array walked while the callbacks fill it: what it holds
		// is counted when the run is counted again by what it holds (memory.go); the caller takes it off
		result := make([]value.Value, length)
		out := value.Array(result)
		vm.push(out)
		for i := range result {
			items := target.Items()
			if i >= len(items) {
				break // removed, and every item after it: undefined, JavaScript's holes
			}
			result[i] = vm.callback3(cb, items[i], arrayIndex(i), target)
			if result[i].IsInvalid() {
				return result[i], true
			}
		}
		return out, true

	case "filter":
		result := make([]value.Value, 0, length)
		for i := 0; i < length; i++ {
			items := target.Items()
			if i >= len(items) {
				break
			}
			item := items[i]
			matched := vm.callback3(cb, item, arrayIndex(i), target)
			if matched.IsInvalid() {
				return matched, true
			}
			if matched.ToBoolean() {
				result = append(result, item)
			}
		}
		return value.Array(result), true

	case "find":
		for i := 0; i < length; i++ {
			item := itemAt(target, i)
			matched := vm.callback3(cb, item, arrayIndex(i), target)
			if matched.IsInvalid() {
				return matched, true
			}
			if matched.ToBoolean() {
				return item, true
			}
		}
		return value.Undefined(), true

	case "forEach":
		for i := 0; i < length; i++ {
			items := target.Items()
			if i >= len(items) {
				break
			}
			result := vm.callback3(cb, items[i], arrayIndex(i), target)
			if result.IsInvalid() {
				return result, true
			}
		}
		// JS forEach trả về undefined
		return value.Undefined(), true

	case "some":
		for i := 0; i < length; i++ {
			items := target.Items()
			if i >= len(items) {
				break
			}
			result := vm.callback3(cb, items[i], arrayIndex(i), target)
			if result.IsInvalid() {
				return result, true
			}
			if result.ToBoolean() {
				return value.Boolean(true), true
			}
		}
		return value.Boolean(false), true

	case "every":
		for i := 0; i < length; i++ {
			items := target.Items()
			if i >= len(items) {
				break
			}
			result := vm.callback3(cb, items[i], arrayIndex(i), target)
			if result.IsInvalid() {
				return result, true
			}
			if !result.ToBoolean() {
				return value.Boolean(false), true
			}
		}
		return value.Boolean(true), true

	case "findIndex":
		for i := 0; i < length; i++ {
			result := vm.callback3(cb, itemAt(target, i), arrayIndex(i), target)
			if result.IsInvalid() {
				return result, true
			}
			if result.ToBoolean() {
				return arrayIndex(i), true
			}
		}
		return value.Number(-1), true

	case "reduce":
		start := 0
		var acc value.Value
		if hasInitial {
			acc = initial
		} else {
			if length == 0 {
				// JavaScript's TypeError
				return value.Fail(value.TypeCode, "reduce: empty array with no initial value"), true
			}
			acc = target.Items()[0]
			start = 1
		}
		for i := start; i < length; i++ {
			items := target.Items()
			if i >= len(items) {
				break
			}
			acc = vm.callback4(cb, acc, items[i], arrayIndex(i), target)
			if acc.IsInvalid() {
				return acc, true
			}
		}
		return acc, true
	}

	return value.Undefined(), false
}

// callsBack reports the array methods whose first argument is the function they call back: a call of
// one with anything else is the runtime error callbackNotAFunction (callee.go), before the method runs.
// map, filter, find, forEach, some, every, findIndex and reduce run here (arrayCallbackMethod); sort is
// core/value's (04/11/2026). Kitwork's own group, groupBy, sortBy and unique are gone (LANG, 27/10/2026):
// an array has no such method.
func callsBack(method string) bool {
	switch method {
	case "map", "filter", "find", "forEach", "some", "every", "findIndex", "reduce", "sort":
		return true
	}
	return false
}

func arrayIndex(index int) value.Value {
	return value.Number(float64(index))
}

// itemAt is item i of an array as it is now, undefined once a callback removed it: what find and
// findIndex hand their callback for an index past the array's length, as JavaScript reads it.
func itemAt(target value.Value, i int) value.Value {
	if items := target.Items(); i < len(items) {
		return items[i]
	}
	return value.Undefined()
}

func (vm *VM) currentLine(ip int) int32 {
	return vm.currentLocation(ip).Line
}

func (vm *VM) currentLocation(ip int) SourceLocation {
	if vm == nil || vm.program == nil {
		return SourceLocation{}
	}
	return vm.program.SourceAt(ip)
}
