package runtime

import (
	"strconv"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// Spread in an array and in a call (bytecode 9, 15/11/2026, round 5 of the 13/11 review; the owner left which
// syntax to open to the work, the rule of 27/10 being JavaScript's meaning where it has no trap): [...a, x],
// f(...args), o.m(...args), new Date(...parts). They were the compile error "Kitwork chưa có ... ở chỗ này", the
// array to be joined with a.concat(b).
//
// An array that spreads is made by SPREAD: SPREAD 0 appends the items of an iterable value, as JavaScript's
// spread reads them (an array's items, a string's code points, bytes' numbers), SPREAD 1 appends the value
// itself; what is not iterable is the TYPE failure for…of gives. A call that spreads gathers its arguments into
// such an array, and CALL_SPREAD and INVOKE_SPREAD lay them on the stack and call as CALL and INVOKE do.

// The operand of SPREAD.
const (
	SpreadItems = 0 // the items of an iterable value, appended
	SpreadItem  = 1 // the value itself, appended
)

// maxSpreadArguments bounds the arguments a call spreads: past it the call is the INVALID_ARGUMENT failure, as
// JavaScript's engines give a RangeError past theirs.
const maxSpreadArguments = 65535

// spreadInto appends to target, the array SPREAD makes, the items of source (SpreadItems) or source itself
// (SpreadItem), and gives target, or the failure: of what is not iterable, of the run's energy or of its bytes.
func (vm *VM) spreadInto(target, source value.Value, mode byte, ip int) value.Value {
	if !target.IsArray() {
		return value.Fail(value.TypeCode, "SPREAD: the compiler's array is no array")
	}
	var items []value.Value
	if mode == SpreadItem {
		items = []value.Value{source}
	} else {
		var failure value.Value
		if items, failure = vm.spreadItems(source); failure.IsInvalid() {
			return failure
		}
	}
	for i, item := range items {
		if lambda := item.Lambda(); lambda != nil && !vm.made(lambda) {
			items[i] = vm.standIn(item) // as SET keeps it
		}
	}
	if len(items) == 0 {
		return target
	}
	if pushed := target.Push(items...); pushed.IsInvalid() {
		return pushed
	}
	// the items added, counted by what the run makes (memory.go)
	if !vm.consumeBytes(uint64(len(items)) * value.ItemBytes) {
		return vm.memoryLimit(ip)
	}
	return target
}

// spreadItems are the items JavaScript's spread reads of source: an array's items, a string's code points (one
// item for a character outside the basic plane, where an index reads its two halves), bytes' numbers; any other
// value is the TYPE failure for…of gives (notIterable). Reading them costs the run what spreading as many keys
// costs (value.IndexedSpreadWork).
func (vm *VM) spreadItems(source value.Value) ([]value.Value, value.Value) {
	switch {
	case source.IsArray():
		items := source.Items()
		if failure := vm.read(value.IndexedSpreadWork(len(items))); failure.IsInvalid() {
			return nil, failure
		}
		return append([]value.Value(nil), items...), value.Value{}
	case source.IsString():
		text := source.Text()
		if failure := vm.read(value.IndexedSpreadWork(utf8.RuneCountInString(text))); failure.IsInvalid() {
			return nil, failure
		}
		items := make([]value.Value, 0, len(text))
		for at := 0; at < len(text); {
			_, size := utf8.DecodeRuneInString(text[at:])
			items = append(items, value.String(text[at:at+size]))
			at += size
		}
		return items, value.Value{}
	case source.IsBytes():
		data := source.Bytes()
		if failure := vm.read(value.IndexedSpreadWork(len(data))); failure.IsInvalid() {
			return nil, failure
		}
		items := make([]value.Value, len(data))
		for i, b := range data {
			items[i] = value.Number(float64(b))
		}
		return items, value.Value{}
	}
	if source.IsObject() {
		return nil, value.Fail(value.TypeCode, "...: một object không trải được vào mảng hay lời gọi; trải các khoá, giá trị hay cặp của nó: [...Object.keys(o)], [...Object.values(o)], [...Object.entries(o)]")
	}
	return nil, value.Fail(value.TypeCode, "...: "+source.Kind()+" không trải được: chỉ trải mảng, chuỗi hay bytes")
}

// itemsOf is what an array pattern reads of source (ITEMS, bytecode 10): an array itself, never copied; a string's
// code points and bytes' numbers, an array made of them as a spread makes it (spreadItems); anything else the TYPE
// failure JavaScript throws, as source is not iterable.
func (vm *VM) itemsOf(source value.Value, ip int) value.Value {
	if source.IsArray() {
		return source
	}
	if !source.IsString() && !source.IsBytes() {
		return value.Fail(value.TypeCode, "mẫu mảng [ … ] tách theo phép lặp: "+source.Kind()+" không lặp được; chỉ mảng, chuỗi hay bytes")
	}
	items, failure := vm.spreadItems(source)
	if failure.IsInvalid() {
		return failure
	}
	if !vm.consumeBytes(value.MadeBytes + uint64(len(items))*value.ItemBytes) {
		return vm.memoryLimit(ip)
	}
	return value.Array(items)
}

// spreadArguments lays the items of the array arguments on the stack, for a call of them: false, with the failure
// placed where the call's result goes, when there are more than a call takes.
func (vm *VM) spreadArguments(arguments value.Value) (int, value.Value) {
	items := arguments.Items()
	if len(items) > maxSpreadArguments {
		return 0, value.Fail(value.InvalidArgumentCode, "một lời gọi trải nhiều nhất 65535 đối số")
	}
	for _, item := range items {
		vm.push(item)
	}
	return len(items), value.Value{}
}

// The rest of a pattern (bytecode 9, 15/11/2026, round 5 of the 13/11 review): const { a, ...rest } = o and const
// [first, ...others] = list. REST's operands are its kind and a count: for RestObject the keys the pattern read
// before it, as many strings above the value destructured; for RestArray the index it starts at. It leaves the value
// destructured where it is and pushes the rest above it.
const (
	RestObject = 0 // an object of the value's own keys but those read
	RestArray  = 1 // an array of the value's items past those read
)

// restOf is the rest of a pattern of kind over source: an object of its own keys but excluded (an array's, a
// string's or bytes' indexes as keys, as { ...v } writes them), or an array of its items from start on (a string's
// code points, as a spread reads it); null and undefined have none to give, and what is not iterable no items, the
// TYPE failure JavaScript throws. It costs the run's energy as a spread of as many keys and counts what it makes.
func (vm *VM) restOf(source value.Value, kind byte, start int, read []value.Value, ip int) value.Value {
	if source.IsNullish() {
		return value.Fail(value.TypeCode, "...: không tách được phần còn lại của "+source.Kind())
	}
	if kind == RestArray {
		items, failure := vm.spreadItems(source)
		if failure.IsInvalid() {
			return failure
		}
		rest := items[min(start, len(items)):]
		if !vm.consumeBytes(value.MadeBytes + uint64(len(rest))*value.ItemBytes) {
			return vm.memoryLimit(ip)
		}
		return value.Array(append([]value.Value(nil), rest...))
	}
	excluded := make(map[string]bool, len(read))
	for _, key := range read {
		excluded[key.Text()] = true
	}
	rest := value.Object()
	if !vm.consumeBytes(value.MadeBytes) {
		return vm.memoryLimit(ip)
	}
	var keys []string
	var items []value.Value
	switch {
	case source.IsObject():
		keys = source.Keys()
		items = make([]value.Value, len(keys))
		for i, key := range keys {
			items[i] = source.Get(key)
		}
	case source.IsArray() || source.IsString() || source.IsBytes():
		var cursor value.TextCursor
		n := cursor.Length(source)
		for i := 0; i < n; i++ {
			keys = append(keys, strconv.Itoa(i))
			items = append(items, cursor.Index(source, i))
		}
	}
	if failure := vm.read(value.IndexedSpreadWork(len(keys))); failure.IsInvalid() {
		return failure
	}
	added := 0
	for i, key := range keys {
		if excluded[key] {
			continue
		}
		item := items[i]
		if item.IsFunction() {
			item = vm.standIn(item) // as SET keeps it
		}
		rest.Set(key, item)
		added++
	}
	if added > 0 && !vm.consumeBytes(uint64(added)*value.KeyBytes) {
		return vm.memoryLimit(ip)
	}
	return rest
}
