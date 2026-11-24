package value

import (
	"cmp"
	"slices"
	"strconv"
)

// properties is what an Object value holds, with its keys in the order JavaScript lists them: array
// indexes ("0", "1", "42") first in numeric order, then every other key in the order it was set.
// keys() and JSON walk the slice, so output has that order too. Most objects hold a few keys (a row,
// an options bag), and comparing a few keys in order is quicker than hashing one and costs no map:
// the index map is built only once the object holds more than linearKeys keys.
//
// An object with its index map is written in O(1) and put in order as it is read in order (order): a new key goes
// at the end, a deleted one leaves a dead slot, and an array index set out of its place or a delete marks the
// object unordered. Until 13/11/2026 each write put the keys in order at once: an array index set before the others
// moved every key after it and wrote each one's position again, so n of them in descending order cost O(n^2)
// (JSON.parse of 20,000, 6.1 s), and so did n deletes from the front. A program reads its own objects on one
// goroutine (an object reaches another run copied, core/runtime/routine.go), so a read may put one in order; an
// object every run reads at once is frozen, and Freeze puts it in order first, so no read ever writes it.
type properties struct {
	keys    []string
	items   []Value
	index   map[string]int // the position of each live key; nil while the object has held linearKeys or fewer
	indexed int            // while in order, how many leading keys are array indexes
	last    uint32         // while in order with an index map and indexed > 0: the greatest array index
	// dead is how many slots of deleted keys keys and items still hold: each is "" and a zero Value, and no key of
	// the index map gives its position. (An int32 beside last, and the two marks after, keep the struct in the
	// size class it had: 80 bytes.)
	dead int32

	// unordered says keys holds a dead slot or an array index out of its place; order puts them right before
	// they are read in order. Only an object with its index map is ever unordered.
	unordered bool
	// frozen refuses every write from Freeze on, as Object.freeze does: a frozen object is only read,
	// so one value can be shared by every run at once (the builtins are, core/builtins)
	frozen bool
	// shared says the engine froze it (FreezeShared: a builtin, what a site's boot made), not the program: JavaScript
	// leaves such a value open, so Object.isFrozen has no answer to give for it (19/11/2026)
	shared bool
	// library is which of JavaScript's libraries it is (Math, JSON, console; MarkLibrary), 0 for any other object: a
	// library is no data, its keys are not JavaScript's to list (19/11/2026)
	library uint8
}

// libraryNames are the libraries MarkLibrary marks, by their mark.
var libraryNames = [...]string{1: "Math", 2: "JSON", 3: "console"}

// linearKeys is how many keys an object holds before it builds its index map.
const linearKeys = 8

// firstCapacity is the room keys and items are made with on the first set: a small object then
// grows no further, where append would have grown them 1, 2, 4.
const firstCapacity = 4

func newProperties() *properties { return &properties{} }

// find is the position of key, or -1.
func (o *properties) find(key string) int {
	if o.index != nil {
		if i, ok := o.index[key]; ok {
			return i
		}
		return -1
	}
	for i, k := range o.keys {
		if k == key {
			return i
		}
	}
	return -1
}

// live says slot i of an object with its index map holds a key, not one deleted.
func (o *properties) live(i int) bool {
	j, ok := o.index[o.keys[i]]
	return ok && j == i
}

func (o *properties) get(key string) (Value, bool) {
	if i := o.find(key); i >= 0 {
		return o.items[i], true
	}
	return Value{}, false
}

func (o *properties) set(key string, item Value) {
	if i := o.find(key); i >= 0 {
		o.items[i] = item
		return
	}
	if o.keys == nil {
		o.keys, o.items = make([]string, 0, firstCapacity), make([]Value, 0, firstCapacity)
	}
	if o.index == nil {
		if len(o.keys) < linearKeys {
			o.place(key, item)
			return
		}
		o.index = make(map[string]int, 2*len(o.keys))
		for i, k := range o.keys {
			o.index[k] = i
		}
		if o.indexed > 0 {
			o.last, _ = arrayIndex(o.keys[o.indexed-1])
		}
	}
	if n, isIndex := arrayIndex(key); isIndex && !o.unordered {
		if o.indexed == len(o.keys) && (o.indexed == 0 || n > o.last) {
			o.indexed, o.last = o.indexed+1, n // after the indexes before it, before any other key: in order
		} else {
			o.unordered = true
		}
	}
	o.index[key] = len(o.keys)
	o.keys = append(o.keys, key)
	o.items = append(o.items, item)
}

// place puts a new key of an object with no index map, linearKeys or fewer, in its place at once.
func (o *properties) place(key string, item Value) {
	at := len(o.keys)
	if n, isIndex := arrayIndex(key); isIndex {
		at = o.indexed
		for at > 0 {
			if previous, _ := arrayIndex(o.keys[at-1]); previous < n {
				break
			}
			at--
		}
		o.indexed++
	}
	o.keys = append(o.keys, "")
	o.items = append(o.items, Value{})
	copy(o.keys[at+1:], o.keys[at:])
	copy(o.items[at+1:], o.items[at:])
	o.keys[at], o.items[at] = key, item
}

func (o *properties) delete(key string) bool {
	i := o.find(key)
	if i < 0 {
		return false
	}
	if o.index == nil {
		if i < o.indexed {
			o.indexed--
		}
		o.keys = append(o.keys[:i], o.keys[i+1:]...)
		o.items = append(o.items[:i], o.items[i+1:]...)
		return true
	}
	delete(o.index, key)                // a map once built stays: find reads it whatever the count falls to
	o.keys[i], o.items[i] = "", Value{} // what the slot held is let go
	if last := len(o.keys) - 1; i == last && !o.unordered {
		// the last key of an object in order: nothing after it moves
		o.keys, o.items = o.keys[:last], o.items[:last]
		if i < o.indexed {
			if o.indexed--; o.indexed > 0 {
				o.last, _ = arrayIndex(o.keys[o.indexed-1])
			}
		}
		return true
	}
	o.dead++
	o.unordered = true
	if 2*int(o.dead) > len(o.keys) {
		o.order() // more slots dead than live: closed up, so a delete costs O(1) over time and slots stay few
	}
	return true
}

// order puts an unordered object's keys as JavaScript lists them, its dead slots gone: the array indexes first in
// numeric order, then every other key in the order it was set. O(n log n), once for all the writes that left it
// unordered; into new slices, so a list of keys read before (Keys) keeps what it held.
func (o *properties) order() {
	if !o.unordered {
		return
	}
	type indexKey struct {
		n  uint32
		at int
	}
	var indexes []indexKey
	var named []int
	for i := range o.keys {
		if !o.live(i) {
			continue
		}
		if n, isIndex := arrayIndex(o.keys[i]); isIndex {
			indexes = append(indexes, indexKey{n, i})
		} else {
			named = append(named, i)
		}
	}
	slices.SortFunc(indexes, func(a, b indexKey) int { return cmp.Compare(a.n, b.n) })
	size := len(indexes) + len(named)
	keys, items := make([]string, size), make([]Value, size)
	for to, index := range indexes {
		keys[to], items[to] = o.keys[index.at], o.items[index.at]
	}
	for to, at := range named {
		keys[len(indexes)+to], items[len(indexes)+to] = o.keys[at], o.items[at]
	}
	for i, k := range keys {
		o.index[k] = i
	}
	o.keys, o.items = keys, items
	o.indexed, o.dead, o.unordered = len(indexes), 0, false
	if len(indexes) > 0 {
		o.last = indexes[len(indexes)-1].n
	}
}

// arrayIndex reads a key that is a canonical array index: the decimal text of an integer below
// 2^32 - 1 with no leading zero ("0", "42"; not "042", "1.5", "4294967295").
func arrayIndex(key string) (uint32, bool) {
	if key == "" || len(key) > 10 || (key[0] == '0' && len(key) > 1) {
		return 0, false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= 1<<32-1 {
		return 0, false
	}
	return uint32(n), true
}

// properties is an Object's, or a Function's (made on first Set); nil for every other kind.
func (v Value) properties() *properties {
	switch payload := v.data.(type) {
	case *properties:
		return payload
	case *callable:
		if payload.stands != nil {
			// a stand-in's properties are its function's (HandingOver), read and written there: a stand-in
			// made before they existed kept none, so Object.freeze of 'abc'.at, handed over to freeze,
			// froze the stand-in alone and isFrozen read false (05/11/2026)
			return payload.stands.properties
		}
		return payload.properties
	}
	return nil
}

func (v Value) propertiesForWrite() *properties {
	if c, ok := v.data.(*callable); ok {
		if c.stands != nil {
			c = c.stands
		}
		if c.properties == nil {
			c.properties = newProperties()
		}
		return c.properties
	}
	return v.properties()
}

// Keys is the object's keys in order; nil for every other kind. Go reads it; a program uses
// Object.keys, a builtin of the runtime. It is the object's own list, not a copy: a write to the object
// may move the keys inside it, so a walk that runs program code (a callback, a replacer) copies it first.
func (v Value) Keys() []string {
	if o := v.properties(); o != nil {
		o.order()
		return o.keys
	}
	return nil
}

// Set writes key on an Object and returns the object, so a host can chain; on any other kind it is a
// TYPE failure, as `(5).x = 1` throws in strict JavaScript, and on a frozen object or function too, as
// a write to a frozen object throws there. A failure passes through.
func (v Value) Set(key string, item Value) Value {
	o := v.propertiesForWrite()
	if o == nil {
		if v.kind == invalid {
			return v
		}
		if l, ok := v.data.(*list); ok && l.frozen {
			return l.refuse(key)
		}
		return Fail(TypeCode, "cannot set "+key+" on "+v.Kind())
	}
	if o.frozen {
		if o.find(key) >= 0 {
			return readOnlyProperty(key, objectHolder)
		}
		return notExtensible(key)
	}
	o.set(key, item)
	return v
}

// Delete removes key from an Object; true when it was there. A frozen object keeps its keys: false.
func (v Value) Delete(key string) bool {
	if o := v.properties(); o != nil && !o.frozen {
		return o.delete(key)
	}
	return false
}

// Freeze makes an Object, a Function or an Array refuse every write from now on, as Object.freeze does,
// and returns it: Set, SetIndex and every writer of an array (push, sort, …) are a TYPE failure and
// Delete removes nothing. Like Object.freeze it is shallow (what the value holds stays as writable as it
// was) and it cannot be undone. Every other kind is returned as it is, as Object.freeze(5) returns 5.
// (Until 04/11/2026 an array's items had no place for the mark, and freezing one was a failure.)
func (v Value) Freeze() Value {
	// What is frozen already is left unwritten: what a site's boot made, and every builtin, is frozen
	// and read by every request at once, and Object.freeze of one at a request wrote its mark again from
	// each, a data race on a value that only reads (05/11/2026). In JavaScript too, freezing a frozen
	// object changes nothing.
	switch v.kind {
	case object, function:
		if !v.IsFrozen() {
			o := v.propertiesForWrite()
			o.order() // in order before it is shared: a read of a frozen object never writes it
			o.frozen = true
		}
	case array:
		if l := v.data.(*list); !l.frozen {
			l.frozen = true
		}
	case host:
		// a struct guards itself: one whose type keeps state a program can change has a Go-only Freeze
		// (napas's chain), which makes it refuse to change while it is shared
		if freezer, ok := v.data.(interface{ Freeze() }); ok {
			freezer.Freeze()
		}
	}
	return v
}

// FreezeShared is Freeze by the engine: a builtin, what a site's boot made, which every run reads at once. What it
// freezes is marked shared (FrozenByEngine); what the program froze already stays the program's.
func (v Value) FreezeShared() Value {
	switch v.kind {
	case object, function:
		if !v.IsFrozen() {
			o := v.propertiesForWrite()
			o.order()
			o.frozen, o.shared = true, true
		}
	case array:
		if l := v.data.(*list); !l.frozen {
			l.frozen, l.shared = true, true
		}
	default:
		return v.Freeze()
	}
	return v
}

// FrozenByEngine reports an object, a function or an array the engine froze (FreezeShared), not the program.
func (v Value) FrozenByEngine() bool {
	switch v.kind {
	case object, function:
		o := v.properties()
		return o != nil && o.shared
	case array:
		return v.data.(*list).shared
	}
	return false
}

// MarkLibrary marks the object v as the library name, Math, JSON or console, before it is frozen, and returns it.
func (v Value) MarkLibrary(name string) Value {
	for mark, library := range libraryNames {
		if library == name && mark > 0 && v.kind == object {
			v.propertiesForWrite().library = uint8(mark)
		}
	}
	return v
}

// Library is which library v is (MarkLibrary), "" for any other value.
func (v Value) Library() string {
	if o, ok := v.data.(*properties); ok && v.kind == object && o.library > 0 {
		return libraryNames[o.library]
	}
	return ""
}

// LibraryAsData is the TYPE error of a library read as data, written being what was asked of it: Object.keys(Math),
// String(JSON), { ...console }, JSON.stringify(Math). JavaScript lists none of a library's keys and writes its own tag
// ("[object Math]"), which no one writes on purpose (19/11/2026, docs/language.md mục 4.3).
func LibraryAsData(library, written string) Value {
	return Fail(TypeCode, written+": "+library+" là thư viện, không phải dữ liệu; đọc từng tên của nó ("+library+"."+map[string]string{"Math": "PI", "JSON": "parse", "console": "log"}[library]+")")
}

// IsFrozen is Object.isFrozen: true for a frozen Object, Function or Array and for every value that holds
// no keys to write (a number, a string, null), false for an object, a function or an array that can
// still be written.
func (v Value) IsFrozen() bool {
	switch v.kind {
	case object, function:
		o := v.properties()
		return o != nil && o.frozen
	case array:
		return v.data.(*list).frozen
	}
	return true
}

// Has reports whether an Object has key, as `key in o` does.
func (v Value) Has(key string) bool {
	if o := v.properties(); o != nil {
		return o.find(key) >= 0
	}
	return false
}
