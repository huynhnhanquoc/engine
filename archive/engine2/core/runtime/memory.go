package runtime

import (
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/kitwork/engine2/core/value"
)

// closureBytes is a closure a run makes: its lambda, with the callable inside it.
const closureBytes = uint64(unsafe.Sizeof(value.Lambda{}))

// noByteCeiling is what a VM without a ceiling of bytes shows the VMs nested in its host calls (nesting.shownBytes).
const noByteCeiling = ^uint64(0)

// The bytes a run makes (12/11/2026). Energy counted instructions, and one instruction can make a gigabyte: forty
// iterations of a.push("y".repeat(10_000_000)) held 400 MB for 13,382 units, 0.13% of a request's energy, so one
// request could take a host's memory and every tenant's with it. Bytes counts what the run makes, MaxBytes bounds it,
// and past it the run stops with MEMORY_LIMIT, which nothing catches, as energy's limit: a request holds no more than
// it made, so it holds no more than MaxBytes (Go says nothing of when a value is let go: what a run holds at a time
// cannot be counted, what it makes can).
//
// What is counted (value/footprint.go has the figures):
//   - what an instruction makes: the text of a + or a template (not a side given back whole), an object or an array
//     (MAKE), a function (a closure), the items and keys a write adds (SET, a spread);
//   - what a function of the engine makes: the result as it takes itself (a text, an array's items, an object's
//     keys), unless it is a part of what the call was given (a slice of a text, the array sort gave back), and what
//     the call added to its target and its first arguments (push, Object.assign); the result whole when the
//     function makes it all new (value.Fresh: JSON.parse);
//   - what a module gives: the result of a call of a struct's method or of a function of the host, whole, as a
//     module makes it fresh (the rows of a query, an HTTP answer, a file's text), unless program code ran inside the
//     call, which counted what it made itself.
//
// What Go code of the engine holds while program code it called runs is counted when the run is counted again by what
// it holds: the array map walks and the items it made so far, the tree JSON.parse's reviver walks, the text replace
// writes with a function (value.Kept, 14/11/2026). Unseen, a.map(() => "y".repeat(100000)) held 20 MB under a ceiling
// of 1 MiB, its items let go by each count. What a function of the engine called back gives is counted as a function
// of the host's result is (nativeCallback): a.map(JSON.stringify) made 20 MB counted as 200 items.
//
// Not counted: what a module hands a callback, what the engine makes for itself (a page drawn, a frame, a stack).
// A run nested in another's host call draws on the bytes that run has left and hands back what it made, as energy.

const (
	memoryMessage = "Memory Limit Exceeded: Execution halted"
	// growthTracked is how many of a host call's target and first arguments are watched for what the call adds to them.
	growthTracked = 5
)

// consumeBytes counts amount made; false once there is no room for it (room), which leaves Bytes at the ceiling. The
// common case, room as it is, is small enough to inline; a VM without a ceiling takes the other way. Bytes and amount
// stay far below 2^63 (a ceiling, a result no larger than what is left), so the sum does not wrap.
func (vm *VM) consumeBytes(amount uint64) bool {
	if sum := vm.Bytes + amount; sum <= vm.MaxBytes {
		vm.Bytes = sum
		return true
	}
	return vm.consumeAfterCount(amount)
}

// consumeAfterCount is consumeBytes past the ceiling: the run counted again by what it holds (room). It is kept out
// of line so that its callers' common case inlines.
//
//go:noinline
func (vm *VM) consumeAfterCount(amount uint64) bool {
	if !vm.room(amount) {
		vm.Bytes = vm.MaxBytes
		return false
	}
	vm.Bytes += amount
	return true
}

// room reports whether amount more fits under MaxBytes. When it does not, the run is counted again by what it can
// still reach (holding): most of what it made is let go as it goes, s += piece making every shorter text before the
// last, which would stop a run holding little. Bytes is then what it holds, and amount fits or not on that. A run
// holding past half its ceiling has no room: so between two counts it makes at least half its ceiling, and a count,
// which costs no more than what it counts, costs at most what was made since the last.
//
// With the host's pool (pool.go) the room is also what the pool gives: the run takes more when it needs it.
func (vm *VM) room(amount uint64) bool {
	if !vm.fits(amount) {
		vm.spendMade()
		// the room of the buffers of the run's joins is no text a count sees: let it go, the texts stay (value.Joiner)
		if vm.joiner != nil {
			vm.joiner.Forget()
		}
		half := vm.MaxBytes / 2
		held := vm.holding(half)
		if held > half {
			return false
		}
		vm.Bytes = held
		vm.counted = held
		vm.settle()
		if !vm.fits(amount) {
			return false
		}
	}
	return vm.cover(amount)
}

// holding is what the run can still reach, counted up to limit (value.Holding): its stack, the slots and variables of
// its frames and the functions they run and defer, its top-level variables, and what Go code of the engine holds for
// it while program code it called runs (kept). A value of the site's files, frozen as the site booted, is everyone's
// and not counted.
func (vm *VM) holding(limit uint64) uint64 {
	held := value.Holding{Limit: limit}
	for _, kept := range vm.kept {
		held.Add(kept.value)
		if kept.items != nil {
			held.Total += uint64(len(*kept.items)) * value.ItemBytes
			for _, item := range *kept.items {
				held.Add(item)
			}
		}
		if kept.size != nil {
			held.Total += uint64(*kept.size)
		}
	}
	for _, item := range vm.Stack {
		held.Add(item)
	}
	for index := 0; index <= vm.FrameIdx && index < len(vm.Frames) && !held.Past(); index++ {
		frame := &vm.Frames[index]
		for _, item := range frame.Slots {
			held.Add(item)
		}
		for _, item := range frame.Vars {
			held.Add(item)
		}
		if frame.Fn != nil {
			held.AddLambda(frame.Fn)
		}
		for _, deferred := range frame.Defers {
			held.AddLambda(deferred)
		}
	}
	for _, item := range vm.Vars {
		held.Add(item)
	}
	return held.Total
}

// Room is value.Budget's: whether amount more than the run has counted fits within its ceiling, the run counted
// again by what it holds when it must (room), asked by a builder of the engine before it grows (join, a text of an
// array, JSON.stringify, JSON.parse, flat, Array.from, Object.keys). It counts nothing: what the builder made is
// counted once its call returns.
func (vm *VM) Room(amount uint64) bool {
	return vm.MaxBytes == 0 || vm.room(amount)
}

// Exceeded is value.Budget's: the run's memory failure, recorded as its stop, at the instruction running.
func (vm *VM) Exceeded() value.Value {
	ip := -1
	if vm.FrameIdx >= 0 && vm.FrameIdx < len(vm.Frames) {
		ip = vm.Frames[vm.FrameIdx].LastIP
	}
	return vm.memoryLimit(ip)
}

// keptValue is what Go code holds for the run while program code it called runs (value.Kept): a value, the items
// made so far, read as they grow, the length of a text being written. The VM's own methods hold theirs on its stack
// (arrayCallbackMethod).
type keptValue struct {
	value value.Value
	items *[]value.Value
	size  *int
}

// keep adds what Go code holds for the run until letGo with the mark it returns.
func (vm *VM) keep(kept keptValue) int {
	vm.kept = append(vm.kept, kept)
	return len(vm.kept) - 1
}

// letGo forgets what was kept from mark on.
func (vm *VM) letGo(mark int) {
	if mark < len(vm.kept) {
		clear(vm.kept[mark:])
		vm.kept = vm.kept[:mark]
	}
}

// memoryLimit is the failure of the instruction at ip that made past MaxBytes, recorded as the run's stop; or past
// what the host's pool gives, when that refused (pool.go).
func (vm *VM) memoryLimit(ip int) value.Value {
	if vm.hostFull {
		vm.hostFull = false
		return vm.diagnosticValue(DiagnosticHostMemory, hostMemoryMessage, ip)
	}
	return vm.diagnosticValue(DiagnosticMemoryLimit, memoryMessage, ip)
}

// chargeMade counts amount, and gives the failure once past the ceiling; a value that is no failure otherwise.
func (vm *VM) chargeMade(amount uint64, ip int) value.Value {
	if vm.MaxBytes == 0 || vm.consumeBytes(amount) {
		return value.Value{}
	}
	return vm.memoryLimit(ip)
}

// spareBytes is what the run may still make before it is counted again by what it holds: the most a buffer of its
// joins is given as room (value.Joiner), so the room never takes the run past its ceiling by itself.
func (vm *VM) spareBytes() uint64 {
	switch {
	case vm.MaxBytes == 0:
		return ^uint64(0)
	case vm.Bytes >= vm.MaxBytes:
		return 0
	}
	return vm.MaxBytes - vm.Bytes
}

// fits reports whether amount more stays within MaxBytes, counting nothing.
func (vm *VM) fits(amount uint64) bool {
	return vm.MaxBytes == 0 || (vm.Bytes <= vm.MaxBytes && amount <= vm.MaxBytes-vm.Bytes)
}

// expectedMade is, for the builtins that make much from little and whose size is known before they make it, the
// least they will make: a text repeated or padded, texts joined or concatenated, arrays concatenated, a text split
// (an item for each piece) or its pattern replaced by a longer text. A call past what is left stops before it
// allocates, which counting its result after could not: "y".repeat(500_000_000) would have made its 500 MB first,
// "x".repeat(500_000).split("") 67 MB for 0.5 MB held. 0 for any other call, counted once it returns. left is what
// the run has left: a size counted by reading the text (split, replaceAll) is counted only when what it could be
// is past it.
func expectedMade(target value.Value, method string, args []value.Value, left uint64) uint64 {
	first := func() float64 {
		if len(args) > 0 && args[0].IsNumber() {
			return args[0].Number()
		}
		return 0
	}
	switch {
	case target.IsString():
		size := uint64(len(target.Text()))
		switch method {
		case "repeat":
			if times := first(); times > 0 {
				return uint64(min(times, 1<<40)) * size
			}
		case "padStart", "padEnd":
			if length := first(); length > float64(size) {
				return uint64(min(length, 1<<40))
			}
		case "concat":
			for _, argument := range args {
				if argument.IsString() {
					size += uint64(len(argument.Text()))
				}
			}
			return size
		case "split":
			return splitMade(target.Text(), args, left)
		case "replaceAll":
			return replacedMade(target.Text(), args, left)
		}
	case target.IsArray():
		items := target.Items()
		switch method {
		case "join":
			size := uint64(0)
			for _, item := range items {
				if item.IsString() {
					size += uint64(len(item.Text()))
				}
			}
			if len(args) > 0 && args[0].IsString() && len(items) > 1 {
				size += uint64(len(items)-1) * uint64(len(args[0].Text()))
			}
			return size
		case "concat":
			count := len(items)
			for _, argument := range args {
				count += max(argument.Count(), 1)
			}
			return uint64(count) * value.ItemBytes
		}
	}
	return 0
}

// splitMade is the least s.split(separator, limit) makes, an item for each piece (each a part of s): a character
// for each piece with "" (a unit each, as many or more), the separator's places plus one otherwise. Counted only
// when as many items as s has bytes would pass left; 0 for a separator that is not a text, counted after.
func splitMade(s string, args []value.Value, left uint64) uint64 {
	if len(args) == 0 || !args[0].IsString() || uint64(len(s)+1)*value.ItemBytes <= left {
		return 0
	}
	separator := args[0].Text()
	pieces := uint64(0)
	if separator == "" {
		pieces = uint64(utf8.RuneCountInString(s))
	} else {
		pieces = uint64(strings.Count(s, separator)) + 1
	}
	if len(args) > 1 && args[1].IsNumber() && args[1].Number() >= 0 {
		pieces = min(pieces, uint64(min(args[1].Number(), 1<<32)))
	}
	return pieces * value.ItemBytes
}

// replacedMade is the least s.replaceAll(pattern, replacement) makes with a text replacement longer than the
// pattern: s with each place of the pattern grown by the difference, a "$" of the replacement (a pattern of its
// own: $&, $$) counted as nothing. Counted only when the most it could be would pass left; 0 otherwise, counted
// after, as a replacement by a function is (value.Kept).
func replacedMade(s string, args []value.Value, left uint64) uint64 {
	if len(args) < 2 || !args[0].IsString() || !args[1].IsString() {
		return 0
	}
	pattern, replacement := args[0].Text(), args[1].Text()
	grows := len(replacement) - 2*strings.Count(replacement, "$") - len(pattern)
	if grows <= 0 {
		return 0
	}
	most := uint64(len(s)) + uint64(len(s)/max(len(pattern), 1)+1)*uint64(grows)
	if most <= left {
		return 0
	}
	places := len(s) + 1 // "" is every place between two units: as many or more
	if pattern != "" {
		places = strings.Count(s, pattern)
	}
	return uint64(len(s)) + uint64(places)*uint64(grows)
}

// resulting is how a host call's result is counted.
type resulting uint8

const (
	// resultAsItself counts the result as it takes itself: a function of the engine (a text, an array's items)
	resultAsItself resulting = iota
	// resultWhole counts it with everything inside it: a module's, value.Fresh's, all new
	resultWhole
	// resultHeld counts nothing of it: one of the array's items, or what a callback gave (returnsItem)
	resultHeld
	// resultFresh counts it whole even when program code ran in the call: JSON.parse's tree, which Go made from the
	// text and a reviver then ran over (value.Fresh); what the reviver made is counted twice, never left out
	resultFresh
)

// returnsItem is an array's methods whose result is one of its items, or what the callback gave (reduce), never made
// by the call: counting it would count again, at each call, what the run already holds.
func returnsItem(method string) bool {
	switch method {
	case "pop", "shift", "at", "find", "findLast", "reduce", "reduceRight":
		return true
	}
	return false
}

// counts is what a host call's target and first arguments held before it (their items, their keys), and the run's
// instructions then: what the call added is read from the same values as it returns, before their slots are cleared.
type counts struct {
	held         [growthTracked]int
	instructions uint64
}

// countBefore notes what target (undefined for a function's call) and the first arguments hold.
func (vm *VM) countBefore(c *counts, target value.Value, args []value.Value) {
	c.instructions = vm.instructions
	c.held[0] = target.Count()
	for index := 0; index < len(args) && index+1 < growthTracked; index++ {
		c.held[index+1] = args[index].Count()
	}
}

// grown is what v gained since it held before: an item for each item an array has more, a key for each key an object.
func grown(v value.Value, before int) uint64 {
	now := v.Count()
	if now <= before {
		return 0
	}
	if v.IsObject() {
		return uint64(now-before) * value.KeyBytes
	}
	return uint64(now-before) * value.ItemBytes
}

// chargeCall counts what the host call made: what it added to target and its first arguments, and its result as
// counting says, unless the result is one of them or a part of one. A result counted whole is counted as it takes
// itself when program code ran in the call, which counted what it made itself. The failure once past the ceiling;
// result otherwise.
func (vm *VM) chargeCall(c *counts, target value.Value, args []value.Value, result value.Value, counting resulting, ip int) value.Value {
	if vm.MaxBytes == 0 || result.IsInvalid() {
		return result
	}
	amount := grown(target, c.held[0])
	for index := 0; index < len(args) && index+1 < growthTracked; index++ {
		amount += grown(args[index], c.held[index+1])
	}
	if counting != resultHeld && result.Holds() {
		made := !result.Shares(target)
		for index := 0; made && index < len(args) && index+1 < growthTracked; index++ {
			made = !result.Shares(args[index])
		}
		if made {
			if counting == resultFresh || counting == resultWhole && vm.instructions == c.instructions {
				// walked up to the whole ceiling, not what is left: counted again by what it holds, a run has more
				// left, and a figure cut at what was left before fitted what was far past it (14/11/2026)
				amount += result.Footprint(vm.MaxBytes)
			} else {
				amount += result.Shallow()
			}
		}
	}
	if amount == 0 || vm.consumeBytes(amount) {
		return result
	}
	return vm.memoryLimit(ip)
}
