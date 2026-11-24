package runtime

import (
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// A write through a value that cannot hold properties is a runtime error (15/10/2026, the owner's L1), as
// JavaScript's TypeError: null.x = 1, o.missing.x = 1 (o.missing is null), and a member or an item of a
// string, a number, a boolean or a function (o.name.x = 1 where o.name is a string, s[0] = "x"). Until
// then SET dropped such a write without a word. JavaScript keeps f.x = 1 on a function, which is an object
// there; Kitwork keeps no member a program writes on any function, an arrow function or one of the
// engine's (Number, Math.max), whose members are the engine's own, so the write is the error rather
// than lost.
//
// The message names the write as the source wrote it and the kind of what it writes through, never a
// value (a key or a value can be a secret a request carried): "không ghi được o.name.x: o.name là chuỗi,
// không phải object hay mảng". Its place is the write's dot or bracket, its code TYPE, so safe(() => …)
// catches it as it catches "a" * 2.
//
// An object and an array take the write as before. A Struct (context, request, a module: a Go value whose
// members are its methods and properties, value/method.go) takes none either, and the write is the same
// error (05/11/2026): until then it was dropped and the program went on as if it had written, so
// `meter.total = 5` then read the struct's own total, and safe(() => { meter.count = 7 }) was ok. A
// struct holds no key a program sets, and its properties are read only, as a getter without a setter
// throws in JavaScript; a write to a builtin, which is frozen, is the same failure. A time, a duration and bytes
// take none either, and the write is the same error (18/11/2026, the owner's yes to docs/language.md mục 8.2): the
// review of 16/10/2026 had taken them back out of the owner's list of 15/10 (null, the primitives and functions), so
// t.x = 1 was dropped and t.x read undefined after it, a value other than JavaScript's, which writes a Date's key
// and a Uint8Array's item. Kitwork's time, duration and bytes are values with no place for a key or a written
// item: the write is refused, said where it stands.

// cannotHoldProperties reports the values a write through is the error: JavaScript's primitives, null
// and undefined, Kitwork's functions, times, durations and bytes, and the host's structs.
func cannotHoldProperties(target value.Value) bool {
	return target.IsNullish() || target.IsNumber() || target.IsBoolean() || target.IsString() || target.IsFunction() ||
		target.IsStruct() || target.IsTime() || target.IsDuration() || target.IsBytes()
}

// writeThroughNonHolder is the error of the SET at ip, whose target cannot hold properties.
func (vm *VM) writeThroughNonHolder(ip int, target value.Value) value.Value {
	write, through := unknownCallee, unknownCallee
	if sites := vm.program.callSites(); sites != nil {
		write, through = sites.written(ip)
	}
	message := "không ghi được " + shortenCallee(write) + ": " + shortenCallee(through) + " là " +
		target.Kind() + ", không phải object hay mảng"
	diagnostic := vm.buildDiagnostic(DiagnosticRuntimeError, message, ip)
	diagnostic.CauseCode = value.TypeCode
	return diagnosticResult(diagnostic)
}

// written names what the SET at ip writes (o.name.x, s[0], grid[1][k]) and the value it writes through
// (o.name, s, grid[1]), as the source wrote them, read back from the bytecode as a failed call's callee
// is; "(…)" stands for a part it cannot name. The kind s.key gives is the word value.Kind() gives for
// the key's constant ("string", "number"), since the kinds themselves do not leave the value package.
func (s *callSites) written(ip int) (string, string) {
	index, ok := s.byIP.at(ip)
	if !ok || s.instructions[index].op != SET {
		return unknownCallee, unknownCallee
	}
	place, ok := s.places.at(ip)
	if !ok {
		return unknownCallee, unknownCallee
	}
	depth := int(place.depth)
	budget := calleeSearchLimit
	target, _ := s.describe(index, depth-3, false, calleeNestingLimit, &budget)
	key, kind := s.key(index, depth-2, &budget)
	switch {
	case kind == stringKey && isPlainName(key):
		return target + "." + key, target
	case kind == stringKey:
		return target + "[" + strconv.Quote(key) + "]", target
	default:
		return target + "[" + key + "]", target
	}
}

// toNumber is value.ToNumber, the number ++ and -- step (STEP): a date made by new Date() steps from its
// time value in milliseconds, as isNaN, Number() and Math read it. No valueOf is called, neither the
// program's (the object is NaN: ++ runs no code of the program) nor one copied from a date into another
// object ({ valueOf: d.valueOf }, { ...d }), which a native valueOf once made a date (15/10/2026). An array is
// made text within the run's budget first, and a text read costs the run's energy (value.NumberWork,
// 15/11/2026): ToNumber made an array's text past what the run may hold, and read a text whole, for the price
// of the instruction.
func (vm *VM) toNumber(v value.Value) value.Value {
	if v.IsArray() {
		if v = vm.arrayText(v); v.IsInvalid() {
			return v
		}
	}
	if failure := vm.read(value.NumberWork(v)); failure.IsInvalid() {
		return failure
	}
	return v.ToNumber()
}
