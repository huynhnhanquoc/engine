// Package builtins is the globals every program has without importing anything: Math, JSON,
// Number, String, Boolean, Array, Object, Date, console, parseInt, parseFloat, isNaN, isFinite,
// encodeURIComponent, decodeURIComponent, encodeURI, decodeURI, Infinity, NaN, fail and safe. Each is a
// value of core/value. A program reads each from its slot of the VM (Slots), which the compiler gives the
// name; Install puts them in a map of globals by name, for Go code that reads them so. Everything here
// follows JavaScript, checked against node; what Kitwork does differently (fail, safe, Date as a Time
// value) says so at its definition.
package builtins

import (
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// Install puts every builtin in globals, by name: the values of shared, so it makes nothing. A program
// compiled since 04/11/2026 does not read a builtin from its globals (Slots): a host's global of a
// builtin's name is not what the program reads.
func Install(globals map[string]value.Value) {
	for name, item := range shared {
		globals[name] = item
	}
}

// shared is every builtin, built once when the engine starts and frozen, so every run of every app
// reads the same values at once. A builtin used to be built afresh for each run (22 µs, 481
// allocations, 45 KB, 04/11/2026), because Math, JSON and console were objects a program could write
// (Math.x = 1) and one set shared by two runs would have carried a write of one tenant into another.
// Frozen, Math.x = 1 is a TYPE failure, as TypeScript refuses it, and there is nothing left to carry.
// Within the set each builtin is one value wherever it is reached: Number.parseInt is the global
// parseInt (=== is true), Number.parseFloat the global parseFloat.
var shared = build()

func build() map[string]value.Value {
	globals := make(map[string]value.Value, len(Names()))

	parseInt, parseFloat := ParseInt(), ParseFloat()
	globals["Math"] = Math().MarkLibrary("Math")
	globals["JSON"] = JSON().MarkLibrary("JSON")
	globals["console"] = Console()
	globals["Number"] = number(parseInt, parseFloat)
	globals["String"] = String()
	globals["Boolean"] = Boolean()
	globals["Array"] = markHoles(Array())
	globals["Object"] = Object()
	globals["Date"] = Date()
	globals["parseInt"] = parseInt
	globals["parseFloat"] = parseFloat
	globals["isNaN"] = IsNaN()
	globals["isFinite"] = IsFinite()
	globals["encodeURIComponent"] = EncodeURIComponent()
	globals["decodeURIComponent"] = DecodeURIComponent()
	globals["encodeURI"] = EncodeURI()
	globals["decodeURI"] = DecodeURI()
	globals["Infinity"] = value.Number(math.Inf(1))
	globals["NaN"] = value.Number(math.NaN())
	globals["error"] = Error()
	globals["safe"] = Safe()
	for _, item := range globals {
		freezeAll(item)
	}
	return globals
}

// markHoles is Array as a program calls it: array's call, the array Array(n) of one number marked as holding
// JavaScript's holes (value.WithHoles, 19/11/2026, docs/language.md mục 4.3), where it was n undefined like any other.
// Its members are array's.
func markHoles(array value.Value) value.Value {
	marking := nativeWithin("Array", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		made := array.CallWithin(budget, arguments) // Array(n) asks the run for its items before it makes them
		if len(arguments) == 1 && arguments[0].IsNumber() {
			made.WithHoles()
		}
		return made
	})
	for _, key := range array.Keys() {
		if key != "length" && key != "name" {
			marking.Set(key, array.Get(key))
		}
	}
	return marking
}

// freezeAll freezes a builtin and everything it holds: Math's functions, Number's statics, the length
// and name of each. A value frozen already was reached by another way (Number.parseInt is parseInt),
// so the walk stops there.
func freezeAll(item value.Value) {
	if !(item.IsObject() || item.IsFunction()) || item.IsFrozen() {
		return
	}
	item.FreezeShared() // the engine's: Object.isFrozen has no answer for it (value.FrozenByEngine)
	for _, key := range item.Keys() {
		freezeAll(item.Get(key))
	}
}

// Names lists the builtins Install puts in globals, in the order it puts them, for the compiler to know
// a global it did not declare. A builtin's place in it gives its slot (Slot).
func Names() []string {
	return []string{
		"Math", "JSON", "console", "Number", "String", "Boolean", "Array", "Object", "Date", "parseInt",
		"parseFloat", "isNaN", "isFinite", "encodeURIComponent", "decodeURIComponent", "encodeURI",
		"decodeURI", "Infinity", "NaN", "error", "safe",
	}
}

// Slots (04/11/2026). The compiler reads a builtin a program names, where nothing declares the name and
// the program never writes it, from a slot of the VM (BUILTIN n, vm.Builtins), not from its globals by
// name: slot 0 is kitwork, the host's, and slot 1 + i the builtin Names()[i]. A slot the host leaves
// empty reads the shared builtin, so a host that sets nothing but kitwork has them all.

// slotOf gives each builtin its slot.
var slotOf = func() map[string]int {
	slots := make(map[string]int, len(Names()))
	for i, name := range Names() {
		slots[name] = i + 1
	}
	return slots
}()

// Slot is the slot a program reads the builtin name from; false for a name that is no builtin.
func Slot(name string) (int, bool) {
	slot, ok := slotOf[name]
	return slot, ok
}

// Slots is every builtin by its slot, a new table of the shared values with slot 0 empty: for a host to
// lay its own over (a site's console writing to its log, its Date in its app's zone, built once at boot)
// and hand the VM as vm.Builtins with kitwork in slot 0. A builtin laid over must be the same builtin
// built another way, with the same members: the compiler checks a program's calls of a builtin's members
// and reads its numbers and strings (Math.PI) from the shared set.
func Slots() []value.Value {
	slots := make([]value.Value, len(Names())+1)
	for i, name := range Names() {
		slots[i+1] = shared[name]
	}
	return slots
}

// native is a builtin function as JavaScript has it: f, with the count of arguments it declares as its
// length and its name as its name, set in that order, the order node lists them in.
func native(name string, length int, f func(arguments ...value.Value) value.Value) value.Value {
	return value.Function(f).Set("length", value.Number(float64(length))).Set("name", value.String(name))
}

// nativeWithin is native for a builtin that makes much from little: a run calls it with its budget, which it asks
// before or as it makes (value.FunctionWithin); called without one, its budget is nil.
func nativeWithin(name string, length int, f func(budget value.Budget, arguments ...value.Value) value.Value) value.Value {
	return value.FunctionWithin(f).Set("length", value.Number(float64(length))).Set("name", value.String(name))
}

// ownCount is how many entries Object.keys, values and entries give for v, at least: an object's keys, an array's
// items, a string's characters (its units are as many or more); asked of the budget before they are made.
func ownCount(v value.Value) uint64 {
	if v.IsString() {
		return uint64(utf8.RuneCountInString(v.Text()))
	}
	return uint64(v.Count())
}

// converting is f with its arguments that could run code to convert (an object, an array) converted
// first as hints says (value.ConvertArguments: 't' text, 'n' a number, 'k' kept, '*' the last again),
// the first failure the call's: a builtin that reads an argument as a number or as text read a failed
// conversion as NaN or "" (Math.max(price, 1) was 1 and parseInt(code) NaN for an object whose valueOf
// failed, 05/11/2026), where JavaScript throws.
func converting(hints string, f func(arguments ...value.Value) value.Value) func(arguments ...value.Value) value.Value {
	return func(arguments ...value.Value) value.Value {
		converted, failure := value.ConvertArguments(hints, arguments)
		if failure.IsInvalid() {
			return failure
		}
		return f(converted...)
	}
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// cannotConvert is JavaScript's TypeError for ToObject of null or undefined.
func cannotConvert() value.Value {
	return value.Fail(value.TypeCode, "cannot convert undefined or null to object")
}

// ---- array lengths ----

// maxArrayLength is the most items a builtin makes one array of: 2^24 items are 512 MB of values, as
// much as the longest string, and a length past it is a TOO_LARGE failure before anything is allocated,
// where JavaScript would try (and a length past 2^32 - 1 is JavaScript's own RangeError).
const maxArrayLength = 1 << 24

// arrayLength checks a length a builtin is about to allocate: a RANGE failure past JavaScript's
// 2^32 - 1, a TOO_LARGE failure past maxArrayLength, else undefined.
func arrayLength(length float64) value.Value {
	switch {
	case length > math.MaxUint32:
		return value.Fail(value.RangeCode, "invalid array length")
	case length > maxArrayLength:
		return value.Fail(value.TooLargeCode, "an array of "+strconv.FormatFloat(length, 'f', -1, 64)+" items is past the "+strconv.Itoa(maxArrayLength)+" a builtin makes")
	}
	return value.Undefined()
}

// ---- strings by code point ----

// codePoints splits a string into its code points, as JavaScript's string iterator does (Array.from,
// Object.fromEntries): a surrogate pair is one element, a lone surrogate (kept in its three-byte form,
// core/value text.go) another.
func codePoints(s string) []value.Value {
	out := make([]value.Value, 0, len(s))
	for i := 0; i < len(s); {
		size := 1
		if s[i] >= utf8.RuneSelf {
			if loneSurrogateAt(s, i) {
				size = 3
			} else {
				_, size = utf8.DecodeRuneInString(s[i:])
			}
		}
		out = append(out, value.String(s[i:i+size]))
		i += size
	}
	return out
}

// loneSurrogateAt reports the three-byte form of a surrogate (ED A0..BF xx) at i: half of a pair a
// slice cut, which UTF-8 has no character for.
func loneSurrogateAt(s string, i int) bool {
	return i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80
}

// ---- Boolean, String, Array, Object: the constructors called as functions, with their statics ----

func Boolean() value.Value {
	return native("Boolean", 1, func(arguments ...value.Value) value.Value {
		return value.Boolean(argument(arguments, 0).ToBoolean())
	})
}

func String() value.Value {
	f := nativeWithin("String", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		if len(arguments) == 0 {
			return value.String("")
		}
		return arguments[0].TextWithin(budget) // an array's text, asked of the run as it grows
	})
	f.Set("fromCharCode", native("fromCharCode", 1, converting("n*", func(arguments ...value.Value) value.Value {
		units := make([]uint16, 0, len(arguments))
		for _, a := range arguments {
			units = append(units, uint16(int32(a.ToNumber().Integer())))
		}
		return value.StringFromUnits(units)
	})))
	f.Set("fromCodePoint", native("fromCodePoint", 1, fromCodePoint))
	return f
}

// fromCodePoint is String.fromCodePoint(…): each argument a whole number from 0 to 0x10FFFF, past
// 0xFFFF written as its surrogate pair; anything else is a RANGE failure, as JavaScript's RangeError.
// Two surrogates given one after the other join into the character they make, as in JavaScript. Each
// argument is converted and checked before the next is read, as JavaScript's are, a failure of the
// conversion kept (05/11/2026: all were converted first, so a later argument's failure hid a RangeError).
func fromCodePoint(arguments ...value.Value) value.Value {
	units := make([]uint16, 0, len(arguments))
	for _, a := range arguments {
		number := a.ToNumber()
		if number.IsInvalid() {
			return number
		}
		n := number.Number()
		if n != math.Trunc(n) || math.IsInf(n, 0) || n < 0 || n > 0x10FFFF {
			shown := a
			if a.ConvertsWithCode() {
				shown = number // its text would run its toString once more
			}
			return value.Fail(value.RangeCode, "invalid code point "+shown.ToString().Text())
		}
		point := rune(n)
		if point < 0x10000 {
			units = append(units, uint16(point))
			continue
		}
		point -= 0x10000
		units = append(units, uint16(0xD800+point>>10), uint16(0xDC00+point&0x3FF))
	}
	return value.StringFromUnits(units)
}

// Array(n) is an array of n undefined; Array(a, b, …) the array of its arguments.
func Array() value.Value {
	f := native("Array", 1, func(arguments ...value.Value) value.Value {
		if len(arguments) == 1 && arguments[0].IsNumber() {
			length := arguments[0].Number()
			if length < 0 || length != length || math.IsInf(length, 0) || length != math.Trunc(length) {
				return value.Fail(value.RangeCode, "invalid array length")
			}
			if bad := arrayLength(length); bad.IsInvalid() {
				return bad
			}
			return value.Array(make([]value.Value, int(length)))
		}
		return value.Array(append([]value.Value(nil), arguments...))
	})
	f.Set("isArray", native("isArray", 1, func(arguments ...value.Value) value.Value {
		return value.Boolean(argument(arguments, 0).IsArray())
	}))
	f.Set("from", nativeWithin("from", 1, arrayFrom))
	f.Set("of", native("of", 0, func(arguments ...value.Value) value.Value {
		return value.Array(append([]value.Value(nil), arguments...))
	}))
	return f
}

// arrayFrom is Array.from(items, map): an array's items, a string's code points, or for anything else
// the items 0 to length - 1 of an array-like ({ length: 2 } is [undefined, undefined]; a number, a
// boolean, a date and a function have no length and give []), each passed through map(item, index)
// when map is given. null and undefined are a TYPE failure, as JavaScript finds no iterator on them.
//
// The items are asked of the run's budget before they are made: Array.from({ length: 3000000 }) made 72 MB of
// items from a few bytes.
func arrayFrom(budget value.Budget, arguments ...value.Value) value.Value {
	items, mapper := argument(arguments, 0), argument(arguments, 1)
	if items.IsNullish() {
		return value.Fail(value.TypeCode, items.Kind()+" is not iterable")
	}
	if !mapper.IsUndefined() && !mapper.IsFunction() {
		return value.Fail(value.TypeCode, "Array.from: the map function is not a function")
	}
	var list []value.Value
	switch {
	case items.IsArray():
		if bad := value.Afford(budget, uint64(items.Count())*value.ItemBytes); bad.IsInvalid() {
			return bad
		}
		list = append([]value.Value(nil), items.Items()...)
	case items.IsString():
		if bad := value.Afford(budget, ownCount(items)*value.ItemBytes); bad.IsInvalid() {
			return bad
		}
		list = codePoints(items.Text())
	default:
		size := items.Get("length").ToNumber()
		if size.IsInvalid() {
			return size
		}
		length := toLength(size.Number())
		if bad := arrayLength(length); bad.IsInvalid() {
			return bad
		}
		if bad := value.Afford(budget, uint64(length)*value.ItemBytes); bad.IsInvalid() {
			return bad
		}
		list = make([]value.Value, int(length))
		for i := range list {
			list[i] = element(items, i)
		}
	}
	if mapper.IsFunction() {
		// the items made so far, held here while map runs, are counted by its run (value.Kept)
		defer mapper.Keep(value.Kept{Items: &list}).Release()
		for i, item := range list {
			mapped := mapper.Call([]value.Value{item, value.Number(float64(i))})
			if mapped.IsInvalid() {
				return mapped
			}
			list[i] = mapped
		}
	}
	return value.Array(list)
}

// toLength is JavaScript's ToLength: the integer part, 0 for NaN and anything below, at most 2^53 - 1.
func toLength(n float64) float64 {
	n = math.Trunc(n)
	if !(n > 0) {
		return 0
	}
	return math.Min(n, 1<<53-1)
}

// element is v[i] on an array-like: an array's or bytes' item, a string's unit, an object's key "i".
func element(v value.Value, i int) value.Value {
	if v.IsArray() || v.IsBytes() || v.IsString() {
		return v.Index(i)
	}
	return v.Get(strconv.Itoa(i))
}

func Object() value.Value {
	f := native("Object", 1, func(arguments ...value.Value) value.Value {
		a := argument(arguments, 0)
		switch {
		case a.IsNullish():
			return value.Object()
		case a.IsNumber() || a.IsString() || a.IsBoolean():
			// JavaScript wraps it in a Number, String or Boolean object, which Kitwork has none of (19/11/2026,
			// docs/language.md mục 4.3): Object(5) gave 5 back, typeof "number" where JavaScript's is "object"
			return wrapperRefused("Object(x)", a)
		}
		return a
	})
	// assign and fromEntries write each key a source holds, which costs the run's energy before they begin, as keys,
	// entries and values give each (value/work.go)
	f.Set("assign", nativeWithin("assign", 2, func(budget value.Budget, arguments ...value.Value) value.Value {
		keys := uint64(0)
		for _, source := range arguments[min(1, len(arguments)):] {
			keys += ownCount(source)
		}
		if bad := value.Spend(budget, keys*value.AssignUnits); bad.IsInvalid() {
			return bad
		}
		return objectAssign(arguments...)
	}))
	f.Set("freeze", native("freeze", 1, func(arguments ...value.Value) value.Value {
		return argument(arguments, 0).Freeze()
	}))
	f.Set("isFrozen", native("isFrozen", 1, func(arguments ...value.Value) value.Value {
		// a builtin and what a site's boot made are frozen by the engine, so every run reads them at once; JavaScript
		// leaves them open, so isFrozen has no answer to give (19/11/2026: it was true, JavaScript's false)
		if o := argument(arguments, 0); o.FrozenByEngine() {
			return value.Fail(value.TypeCode, "Object.isFrozen(x): x do Kitwork đóng băng vì mọi request cùng đọc nó (builtin, dữ liệu cấp file sau boot), JavaScript để nó mở; ghi vào nó là lỗi TYPE")
		}
		return value.Boolean(argument(arguments, 0).IsFrozen())
	}))
	f.Set("getOwnPropertyNames", nativeWithin("getOwnPropertyNames", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		o := argument(arguments, 0)
		if o.IsNullish() {
			return cannotConvert()
		}
		if bad := value.Spend(budget, ownCount(o)*value.KeysUnits); bad.IsInvalid() {
			return bad
		}
		if o.HasHoles() {
			return value.HolesRefused("Object.getOwnPropertyNames(a)")
		}
		if missing := o.StaticsMissing(); len(missing) > 0 {
			// JavaScript's lists names Kitwork's does not hold (19/11/2026, docs/language.md mục 4.4)
			return value.Fail(value.TypeCode, "Object.getOwnPropertyNames của builtin này thiếu "+strings.Join(missing, ", ")+
				" so với JavaScript: danh sách sẽ khác, nên đọc nó là lỗi; đọc từng tên của nó")
		}
		keys := ownPropertyNames(o)
		out := make([]value.Value, len(keys))
		for i, k := range keys {
			out[i] = value.String(k)
		}
		return value.Array(out)
	}))
	f.Set("hasOwn", native("hasOwn", 2, func(arguments ...value.Value) value.Value {
		o := argument(arguments, 0)
		if o.IsNullish() {
			return cannotConvert() // ToObject before the key is converted, as JavaScript does
		}
		if o.HasHoles() {
			return value.HolesRefused("Object.hasOwn(a, i)") // a hole is no own key of JavaScript's
		}
		key, failure := argument(arguments, 1).ToText()
		if failure.IsInvalid() {
			return failure
		}
		if static := o.JavaScriptStatic(key); static.IsInvalid() {
			return static // Object.hasOwn(Array, "prototype"): JavaScript's has it, Kitwork's not
		}
		return value.Boolean(hasOwn(o, key))
	}))
	f.Set("is", native("is", 2, func(arguments ...value.Value) value.Value {
		return value.Boolean(sameValue(argument(arguments, 0), argument(arguments, 1)))
	}))
	// what keys, entries and values make is asked of the run's budget before it is made: the keys of a long
	// text are a text and an item for each of its characters; each entry costs the run's energy then too
	f.Set("keys", nativeWithin("keys", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		if bad := value.Afford(budget, ownCount(argument(arguments, 0))*(value.ItemBytes+1)); bad.IsInvalid() {
			return bad
		}
		if bad := value.Spend(budget, ownCount(argument(arguments, 0))*value.KeysUnits); bad.IsInvalid() {
			return bad
		}
		keys, _, bad := ownEntries(argument(arguments, 0))
		if bad.IsInvalid() {
			return bad
		}
		out := make([]value.Value, len(keys))
		for i, k := range keys {
			out[i] = value.String(k)
		}
		return value.Array(out)
	}))
	f.Set("entries", nativeWithin("entries", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		if bad := value.Afford(budget, ownCount(argument(arguments, 0))*(3*value.ItemBytes+value.MadeBytes)); bad.IsInvalid() {
			return bad
		}
		if bad := value.Spend(budget, ownCount(argument(arguments, 0))*value.EntriesUnits); bad.IsInvalid() {
			return bad
		}
		keys, items, bad := ownEntries(argument(arguments, 0))
		if bad.IsInvalid() {
			return bad
		}
		out := make([]value.Value, len(keys))
		for i, k := range keys {
			out[i] = value.Array([]value.Value{value.String(k), items[i]})
		}
		return value.Array(out)
	}))
	f.Set("fromEntries", nativeWithin("fromEntries", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		if bad := value.Spend(budget, ownCount(argument(arguments, 0))*value.AssignUnits); bad.IsInvalid() {
			return bad
		}
		return objectFromEntries(arguments...)
	}))
	f.Set("values", nativeWithin("values", 1, func(budget value.Budget, arguments ...value.Value) value.Value {
		if bad := value.Afford(budget, ownCount(argument(arguments, 0))*value.ItemBytes); bad.IsInvalid() {
			return bad
		}
		if bad := value.Spend(budget, ownCount(argument(arguments, 0))*value.ValuesUnits); bad.IsInvalid() {
			return bad
		}
		_, items, bad := ownEntries(argument(arguments, 0))
		if bad.IsInvalid() {
			return bad
		}
		return value.Array(items)
	}))
	f.Set("groupBy", nativeWithin("groupBy", 2, objectGroupBy))
	return f
}

// objectGroupBy is Object.groupBy(items, callback) (19/11/2026, decided 27/10/2026, docs/language.md mục 4.3): an
// object whose keys are what callback(item, index) gives for each item, made a key as JavaScript makes one (a number
// its text; a time and a function, which have no text, the TYPE error), each key holding the array of its items in
// order. The items are those for…of walks: an array's, read as it goes, so an item the callback pushes is walked, a
// string's code points, bytes' numbers; anything else is the TYPE failure, as JavaScript finds no iterator on it.
// The keys come in an object's order, index keys first. Each item costs the run's energy as a key assign writes one;
// what the groups hold is asked of the run as it grows (growth), since the run counts it only once the call returns,
// and kept for the run while the callback runs.
func objectGroupBy(budget value.Budget, arguments ...value.Value) value.Value {
	items, callback := argument(arguments, 0), argument(arguments, 1)
	var walked []value.Value // a string's code points; an array and bytes are read as they go
	switch {
	case items.IsArray(), items.IsBytes():
	case items.IsString():
		walked = codePoints(items.Text())
	default:
		return value.Fail(value.TypeCode, "Object.groupBy: "+items.Kind()+" is not iterable: group an array, a string or bytes")
	}
	if !callback.IsFunction() {
		return value.Fail(value.TypeCode, "Object.groupBy: the callback is not a function")
	}
	groups := value.Object()
	defer callback.Keep(value.Kept{Value: groups, Items: &walked}).Release()
	made := growth{budget: budget}
	for i := 0; ; i++ {
		var item value.Value
		switch {
		case items.IsString():
			if i >= len(walked) {
				return groups
			}
			item = walked[i]
		case items.IsArray():
			if i >= items.Count() {
				return groups
			}
			item = items.Index(i)
		default:
			if i >= int(items.Get("length").Number()) {
				return groups
			}
			item = items.Index(i)
		}
		if bad := value.Spend(budget, value.AssignUnits); bad.IsInvalid() {
			return bad
		}
		got := callback.Call([]value.Value{item, value.Number(float64(i))})
		if got.IsInvalid() {
			return got
		}
		key, bad := got.ToText()
		if bad.IsInvalid() {
			return bad
		}
		if group := groups.Get(key); group.IsArray() && groups.Has(key) {
			if bad := made.grow(value.ItemBytes); bad.IsInvalid() {
				return bad
			}
			group.Push(item)
			continue
		}
		if bad := made.grow(value.KeyBytes + uint64(len(key)) + value.MadeBytes + value.ItemBytes); bad.IsInvalid() {
			return bad
		}
		groups.Set(key, value.Array([]value.Value{item}))
	}
}

// growth is what a builtin that calls back the program has made so far, asked of the run as it grows: the run counts
// it only once the call returns, and asking each piece alone (value.Afford) asks whether that piece fits, never all
// of them (19/11/2026: Object.groupBy of 100,000 keys made 56 MB under a ceiling of 4 MiB). It asks ahead, twice what
// it made, so the asks stay few; past a refusal it asks halfway to it, then exactly what it made, the run's memory
// failure once that does not fit. Room counts nothing and records no stop, so a refused ask ahead ends nothing.
type growth struct {
	budget                 value.Budget
	made, granted, refused uint64
}

// grow adds n bytes made: Value{}, or the run's memory failure.
func (g *growth) grow(n uint64) value.Value {
	g.made += n
	if g.budget == nil || g.made <= g.granted {
		return value.Value{}
	}
	ahead := max(2*g.made, 64<<10)
	if g.refused > g.made && ahead >= g.refused {
		ahead = g.made + (g.refused-g.made)/2
	}
	if g.budget.Room(ahead) {
		g.granted = ahead
		return value.Value{}
	}
	g.refused = ahead
	if !g.budget.Room(g.made) {
		return g.budget.Exceeded()
	}
	g.granted = g.made
	return value.Value{}
}

// objectAssign is Object.assign(target, …sources): each source's own keys, in order, written on the
// target, a string's and an array's indexes included; null and undefined sources are skipped. A target
// of null or undefined is a TYPE failure, and so is a number, a string or a boolean, which JavaScript wraps (19/11/2026:
// a wrapper object is not Kitwork's), and any other target but an object that a source writes a key on: an array, a
// time, a function hold no such key. With nothing to write, such a target comes back as it is, as in JavaScript.
func objectAssign(arguments ...value.Value) value.Value {
	target := argument(arguments, 0)
	if target.IsNullish() {
		return cannotConvert()
	}
	if !target.IsObject() {
		// JavaScript writes the keys on the target, wrapped when it is a number, a string or a boolean: an array, a
		// time, a function take keys Kitwork's do not hold (19/11/2026: the target came back as it was, so
		// Object.assign([1, 2], [3]) was [1, 2] where JavaScript's is [3, 2])
		if target.IsNumber() || target.IsString() || target.IsBoolean() {
			return wrapperRefused("Object.assign(x, …)", target)
		}
		for _, source := range arguments[1:] {
			if !source.IsNullish() && ownCount(source) > 0 {
				return value.Fail(value.TypeCode, "Object.assign(x, …) viết khoá vào một object; x là "+target.Kind()+", không giữ khoá: chép một mảng viết [...a], gộp object viết Object.assign({}, …)")
			}
		}
		return target // nothing to write: JavaScript gives the target back as it is
	}
	for _, source := range arguments[1:] {
		if source.IsNullish() {
			continue
		}
		if library := source.Library(); library != "" {
			return value.LibraryAsData(library, "Object.assign(x, "+library+")")
		}
		if source.HasHoles() {
			return value.HolesRefused("Object.assign(x, a)")
		}
		keys, items, _ := ownEntries(source)
		for i, k := range keys {
			if written := target.Set(k, items[i]); written.IsInvalid() {
				return written // a frozen target, as Object.assign throws on one
			}
		}
	}
	return target
}

// wrapperRefused is the TYPE error of what wraps a number, a string or a boolean in an object, which JavaScript does
// and Kitwork does not: a wrapper object is rare and obscure (docs/language.md, the rule that chooses).
func wrapperRefused(written string, primitive value.Value) value.Value {
	return value.Fail(value.TypeCode, written+" bọc một "+primitive.Kind()+" thành object, Kitwork không có object bọc giá trị nguyên thuỷ: dùng chính giá trị ấy")
}

// objectFromEntries is Object.fromEntries(entries): entries an array (or a string, whose characters are
// no entries), each entry a value with an item 0, the key, and an item 1, the value. Anything else is a
// TYPE failure, as JavaScript's TypeError: not iterable, or an entry that is no object.
func objectFromEntries(arguments ...value.Value) value.Value {
	iterable := argument(arguments, 0)
	var entries []value.Value
	switch {
	case iterable.IsArray():
		entries = iterable.Items()
	case iterable.IsString():
		entries = codePoints(iterable.Text())
	default:
		return value.Fail(value.TypeCode, iterable.Kind()+" is not iterable")
	}
	out := value.Object()
	for _, entry := range entries {
		if !isObjectLike(entry) {
			return value.Fail(value.TypeCode, "iterator value "+entry.ToString().Text()+" is not an entry object")
		}
		key, failure := element(entry, 0).ToText()
		if failure.IsInvalid() {
			return failure
		}
		out.Set(key, element(entry, 1))
	}
	return out
}

// isObjectLike is what JavaScript calls an object: everything but undefined, null, a number, a string
// and a boolean.
func isObjectLike(v value.Value) bool {
	return !v.IsNullish() && !v.IsNumber() && !v.IsString() && !v.IsBoolean()
}

// sameValue is Object.is: === except that NaN is NaN and 0 is not -0.
func sameValue(a, b value.Value) bool {
	if a.IsNumber() && b.IsNumber() {
		x, y := a.Number(), b.Number()
		switch {
		case x != x && y != y:
			return true
		case x == 0 && y == 0:
			return math.Signbit(x) == math.Signbit(y)
		}
		return x == y
	}
	return a.Equal(b)
}

// ownPropertyNames is Object.getOwnPropertyNames: an object's keys, a function's (its length and name
// first, as JavaScript lists them; 18/11/2026: they were left out, so a function had none), an array's or a
// string's indexes and then "length", bytes' indexes; nothing for the rest.
func ownPropertyNames(v value.Value) []string {
	switch {
	case v.IsFunction():
		// a builtin's keys hold its length and name already, a program's function none
		names := []string{"length", "name"}
		for _, key := range v.Keys() {
			if key != "length" && key != "name" {
				names = append(names, key)
			}
		}
		return names
	case v.IsObject():
		return append([]string(nil), v.Keys()...)
	case v.IsArray(), v.IsString(), v.IsBytes():
		n := int(v.Get("length").Number())
		keys := make([]string, n, n+1)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}
		if v.IsBytes() {
			return keys // a typed array's length is its prototype's, not its own
		}
		return append(keys, "length")
	}
	return []string{}
}

// hasOwn is Object.hasOwn(v, key) once v is known not to be null or undefined: a function owns its length and
// name, as f.hasOwnProperty says (18/11/2026: Object.hasOwn said it did not).
func hasOwn(v value.Value, key string) bool {
	switch {
	case v.IsFunction():
		return v.Has(key) || key == "length" || key == "name"
	case v.IsObject():
		return v.Has(key)
	case v.IsArray(), v.IsString(), v.IsBytes():
		if key == "length" {
			return !v.IsBytes()
		}
		i, ok := arrayIndex(key)
		return ok && i < int(v.Get("length").Number())
	}
	return false
}

// arrayIndex reads a canonical array index: the decimal text of an integer below 2^32 - 1, with no
// leading zero ("0", "42"; not "042", "1.5", "-1").
func arrayIndex(key string) (int, bool) {
	if key == "" || len(key) > 10 || (key[0] == '0' && len(key) > 1) {
		return 0, false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= math.MaxUint32 {
		return 0, false
	}
	return int(n), true
}

// ownEntries is what Object.keys, values and entries walk: an object's keys in order, an array's or a
// string's indexes, nothing for a number, a boolean or a function (whose own keys JavaScript does not
// list); null and undefined are a TYPE failure, as JavaScript throws.
func ownEntries(v value.Value) (keys []string, items []value.Value, bad value.Value) {
	switch {
	case v.IsNullish():
		return nil, nil, cannotConvert()
	case v.Library() != "":
		return nil, nil, value.LibraryAsData(v.Library(), "Object.keys("+v.Library()+"), values, entries")
	case v.HasHoles():
		return nil, nil, value.HolesRefused("Object.keys(a), values, entries")
	case v.IsObject():
		keys = v.Keys()
		items = make([]value.Value, len(keys))
		for i, k := range keys {
			items[i] = v.Get(k)
		}
	case v.IsArray(), v.IsString():
		n := int(v.Get("length").Number())
		keys, items = make([]string, n), make([]value.Value, n)
		for i := 0; i < n; i++ {
			keys[i], items[i] = strconv.Itoa(i), v.Index(i)
		}
	}
	return keys, items, value.Undefined()
}

// ---- error and safe: Kitwork's, not JavaScript's (docs/language.md mục 5, 05/11/2026) ----

// Error is error("…"), written return error("…"): a failure of the program, its code always FAIL (a program never
// sets a code, so the set of codes keeps its meaning). It rises as every failure does: the call that gives it stops,
// and its caller, up to a safe(() => …) or the edge of the run. It was fail("…") until 07/11/2026; fail is no name
// now, not one beside it.
func Error() value.Value {
	return native("error", 1, converting("t", func(arguments ...value.Value) value.Value {
		text := "error"
		if a := argument(arguments, 0); !a.IsUndefined() {
			text = a.ToString().Text()
		}
		return value.Fail(value.FailCode, text)
	}))
}

// Safe is safe(() => …) (docs/language.md mục 5.2): { value, error: null } when the function returns, whatever it
// returns (false, 0, null, undefined); { value: undefined, error: { code, message, at } } when it fails (at where it
// happened, apart from the message, 19/11/2026: value.ErrorObject), with fields when
// the failure names the fields of data it is about (struct.parse, a write a unique column refused: docs/schema.md mục
// 6), with progress when it says how much was done before it stopped (PARTIAL of kitwork/file's clear()). error alone tells which: no ok, no state. Because a failure is a value that comes back out of Call, safe needs
// nothing from the runtime; a stop of the run (energy, time, cancellation) it never catches.
func Safe() value.Value {
	return native("safe", 1, func(arguments ...value.Value) value.Value {
		fn := argument(arguments, 0)
		if !fn.IsFunction() {
			return value.Fail(value.TypeCode, "safe: the argument is not a function")
		}
		out := fn.Call(nil)
		if out.IsInvalid() {
			return value.Object().Set("value", value.Undefined()).Set("error", out.ErrorObject())
		}
		return value.Object().Set("value", out).Set("error", value.Null())
	})
}

// ---- console ----

// Console is console.log, error, warn, info, debug: each writes its arguments' text on one line of
// standard output, led by its own name.
func Console() value.Value {
	return ConsoleTo(os.Stdout, "")
}

// ConsoleTo is console writing each line to w, led by lead and then by its own name: a site's console,
// "[shop.com] [console.log] text", built once when the site boots and laid over the shared set
// (04/11/2026). Each call is one Write, so a w that locks keeps the lines of requests running at once
// whole. It is frozen, as every builtin is.
//
// Every line a call writes is led so (LogText): a text of several lines leads each, and a control
// character is written as its escape, so that a site cannot write a line led by another site's name
// into a log the sites share (05/11/2026: "hi\n[beta.test] [console.log] …" from alpha read as beta's).
// A call writes at most maxConsoleText bytes of text, and says how many more it had.
func ConsoleTo(w io.Writer, lead string) value.Value {
	console := value.Object().MarkLibrary("console")
	for _, name := range []string{"log", "error", "warn", "info", "debug"} {
		prefix := lead + "[console." + name + "] "
		w := w
		if leveled, ok := w.(LevelWriter); ok {
			w = leveled.Level(name) // the host's log: each word at its level
		}
		console.Set(name, native(name, 0, func(arguments ...value.Value) value.Value {
			parts := make([]string, len(arguments))
			for i, a := range arguments {
				if a.IsFunction() {
					// a function as node's console writes it, [Function: name]: a function has no text (value.ToString)
					parts[i] = functionShown(a)
					continue
				}
				if a.IsTime() {
					// a time as node's console writes a Date, its toISOString: a time has no text (value.ToString)
					parts[i] = a.ToISOString().Text()
					continue
				}
				parts[i] = a.ToString().Text()
			}
			w.Write(LogText(prefix, strings.Join(parts, " ")))
			return value.Undefined()
		}))
	}
	freezeAll(console)
	return console
}

// functionShown is a function as node's console writes it: [Function: name], [Function (anonymous)].
func functionShown(f value.Value) string {
	if name := f.Get("name").Text(); name != "" && name != "<anonymous>" {
		return "[Function: " + name + "]"
	}
	return "[Function (anonymous)]"
}

// LevelWriter is a writer of lines at levels, which the console writes each of its words to at that word's level: the
// host's log (app.logger), where console.error is an error and console.debug written only at level debug.
type LevelWriter interface {
	Level(name string) io.Writer
}

// maxConsoleText is how many bytes of text one console call writes; the rest is counted, not written.
const maxConsoleText = 64 << 10

// LogText is text as the lines one writer of a log the sites share writes (a site's console, the failure
// of one of its requests): each led by lead and ended by a newline, and past maxConsoleText bytes cut at
// a character with the count of the bytes left out. What could end a line, or draw over one, for a
// terminal or a reader of the log is written as an escape, so that no line is led by any name but lead:
//
//   - a control character (C0 but the tab, DEL, and C1: U+0085 NEL, U+009B CSI) as its \xNN;
//   - a byte that is not UTF-8, the WTF-8 form of a lone surrogate among them, as its \xNN;
//   - the line and paragraph separators U+2028 and U+2029, which JavaScript's and Python's readers split
//     lines on, and the bidirectional embeddings, overrides and isolates (U+202A to U+202E, U+2066 to
//     U+2069), as the six-character escape JavaScript writes for them.
//
// Until 05/11/2026 only C0 and DEL were escaped, so a U+2028 in alpha's text began a line led by
// "[beta.test] [console.log]" for a reader that splits on Unicode's line breaks.
func LogText(lead, text string) []byte {
	cut := 0
	if len(text) > maxConsoleText {
		end := maxConsoleText
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		cut = len(text) - end
		text = text[:end]
	}
	out := make([]byte, 0, len(lead)+len(text)+32)
	out = append(out, lead...)
	// The text is copied a run at a time, text[start:i], in one pass: an ASCII byte is looked at alone, a
	// character of more bytes is measured as utf8.DecodeRuneInString measures it (utf8Length) and decoded
	// only when its first byte is 0xC2 (C1) or 0xE2 (the separators and the bidirectional controls), the
	// only ones an escape may begin with; a byte that begins no character is escaped alone, as the
	// decoder's RuneError of one byte was (05/11/2026: every character but ASCII was decoded and appended
	// alone, 2.2 times the time of the byte loop before it for a line of Vietnamese; then a check of the
	// whole text with utf8.ValidString before the loop, 1.2 times).
	start := 0
	for i := 0; i < len(text); {
		character := text[i]
		if character < utf8.RuneSelf {
			if character >= 0x20 && character != 0x7f || character == '\t' {
				i++
				continue
			}
			out = append(out, text[start:i]...)
			if character == '\n' {
				out = append(out, '\n')
				out = append(out, lead...)
			} else {
				out = appendByteEscape(out, character)
			}
			i++
			start = i
			continue
		}
		// the common characters, measured here with no call: two bytes led by 0xC3 to 0xDF, three led by
		// 0xE1, 0xE3 to 0xEC, 0xEE or 0xEF, whose second byte may be any continuation byte; none begins an
		// escape (Vietnamese is 0xC3 to 0xC6 and 0xE1)
		if character >= 0xc3 && character <= 0xdf {
			if i+1 < len(text) && text[i+1]&0xc0 == 0x80 {
				i += 2
				continue
			}
		} else if character >= 0xe1 && character <= 0xef && character != 0xe2 && character != 0xed {
			if i+2 < len(text) && text[i+1]&0xc0 == 0x80 && text[i+2]&0xc0 == 0x80 {
				i += 3
				continue
			}
		}
		size := utf8Length(text, i)
		switch {
		case size == 0:
			out = append(out, text[start:i]...)
			out = appendByteEscape(out, character)
			size = 1
		case character == 0xc2 && text[i+1] < 0xa0:
			// U+0080 to U+009F, whose second byte is the code point
			out = append(out, text[start:i]...)
			out = appendByteEscape(out, text[i+1])
		case character == 0xe2:
			r := 0x2000 | rune(text[i+1]&0x3f)<<6 | rune(text[i+2]&0x3f)
			if r != 0x2028 && r != 0x2029 && (r < 0x202a || r > 0x202e) && (r < 0x2066 || r > 0x2069) {
				i += size
				continue
			}
			out = append(out, text[start:i]...)
			out = append(out, '\\', 'u', hexDigits[r>>12], hexDigits[r>>8&15], hexDigits[r>>4&15], hexDigits[r&15])
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	out = append(out, text[start:]...)
	if cut > 0 {
		out = append(out, " … ("+strconv.Itoa(cut)+" bytes more)"...)
	}
	return append(out, '\n')
}

// appendByteEscape writes the \xNN of b.
func appendByteEscape(out []byte, b byte) []byte {
	return append(out, '\\', 'x', hexDigits[b>>4], hexDigits[b&15])
}

// utf8Length is the length of the character of more than one byte that begins at text[i], or 0 when the
// bytes there are not one (a byte that begins none, one cut short, an overlong form, a surrogate, past
// U+10FFFF): the size utf8.DecodeRuneInString gives a character it decodes, by the ranges it accepts for
// the second byte, without decoding it.
func utf8Length(text string, i int) int {
	lead := text[i]
	switch {
	case lead < 0xc2:
		return 0 // a continuation byte, or the first of an overlong form of two bytes
	case lead < 0xe0:
		if i+1 < len(text) && text[i+1]&0xc0 == 0x80 {
			return 2
		}
	case lead < 0xf0:
		if i+2 >= len(text) {
			return 0
		}
		low, high := byte(0x80), byte(0xbf)
		if lead == 0xe0 {
			low = 0xa0 // an overlong form below
		} else if lead == 0xed {
			high = 0x9f // a surrogate above
		}
		if second := text[i+1]; second >= low && second <= high && text[i+2]&0xc0 == 0x80 {
			return 3
		}
	case lead < 0xf5:
		if i+3 >= len(text) {
			return 0
		}
		low, high := byte(0x80), byte(0xbf)
		if lead == 0xf0 {
			low = 0x90 // an overlong form below
		} else if lead == 0xf4 {
			high = 0x8f // past U+10FFFF above
		}
		if second := text[i+1]; second >= low && second <= high && text[i+2]&0xc0 == 0x80 && text[i+3]&0xc0 == 0x80 {
			return 4
		}
	}
	return 0
}

// hexDigits writes a byte's escape.
const hexDigits = "0123456789abcdef"
