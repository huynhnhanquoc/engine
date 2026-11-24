package value

import "strconv"

// list is what an Array value holds: its items, and the mark Freeze sets. An array is shared by
// reference (pushing into one copy is seen through every copy), so the mark lives beside the items,
// where every copy reads it.
type list struct {
	items []Value

	// frozen refuses every write from Freeze on, as Object.freeze does to an array: each writer asks it
	// before it writes, so a frozen array is only read and one can be shared by every run at once
	frozen bool
	// shared says the engine froze it, not the program (FreezeShared)
	shared bool
	// holes says Array(n) made it and no fill has filled it whole: JavaScript's array has holes there, which some of
	// its methods pass over, and Kitwork's holds undefined (WithHoles, HoleSensitive; 19/11/2026)
	holes bool
}

// WithHoles marks v, an array Array(n) made of n > 0, as one JavaScript holds holes in (19/11/2026, docs/language.md
// mục 4.3): Kitwork's array holds undefined there, which reads as JavaScript's holes read (a[i], length, join, JSON,
// for…of, [...a], Array.from) but which the methods that pass over a hole would not pass over. Those are refused on it
// (HoleSensitive); a fill of it whole makes it an array like any other.
func (v Value) WithHoles() Value {
	if l, ok := v.data.(*list); ok && v.kind == array && len(l.items) > 0 {
		l.holes = true
	}
	return v
}

// HasHoles reports an array WithHoles marked and no fill filled whole.
func (v Value) HasHoles() bool {
	l, ok := v.data.(*list)
	return ok && v.kind == array && l.holes
}

// HoleSensitive reports a method of an array that passes over holes, whose result on an array with holes would not be
// JavaScript's: map keeps them, forEach, filter, some, every, reduce and reduceRight skip them, indexOf and lastIndexOf
// do not find them, flat and flatMap drop them, hasOwnProperty says they are not there.
func HoleSensitive(method string) bool {
	switch method {
	case "map", "forEach", "filter", "some", "every", "reduce", "reduceRight", "indexOf", "lastIndexOf", "flat",
		"flatMap", "hasOwnProperty":
		return true
	}
	return false
}

// HolesRefused is the TYPE error of what an array with holes cannot answer as JavaScript does (HoleSensitive, and the
// keys of Object.keys, Object.entries, { ...a }).
func HolesRefused(written string) Value {
	return Fail(TypeCode, written+": mảng tạo bằng Array(n) còn chỗ trống, JavaScript bỏ qua chỗ trống ở đây, mảng của Kitwork không có chỗ trống; điền nó trước, Array(n).fill(x), hay viết Array.from({ length: n }, (_, i) => …)")
}

// The refusals of a writer on a frozen array or object, worded as JavaScript (node 22) words them, the
// holder named as it names it (05/11/2026; the holder was left out): a write to an item or a key it has,
// one it would add, one it would remove, and a write that changes only an array's length (push() of
// nothing, pop() of an empty array), which JavaScript makes a write of "length".

// The holders a refusal names: an array, and every object (a builtin too, "#<Object>" for Math as node
// writes it).
const (
	arrayHolder  = "[object Array]"
	objectHolder = "#<Object>"
)

func readOnlyProperty(key, holder string) Value {
	return Fail(TypeCode, "cannot assign to read only property '"+key+"' of object '"+holder+"'")
}

func notExtensible(key string) Value {
	return Fail(TypeCode, "cannot add property "+key+", object is not extensible")
}

func cannotDelete(key, holder string) Value {
	return Fail(TypeCode, "cannot delete property '"+key+"' of "+holder)
}

func readOnlyItem(i int) Value { return readOnlyProperty(strconv.Itoa(i), arrayHolder) }

func addedItem(i int) Value { return notExtensible(strconv.Itoa(i)) }

func deletedItem(i int) Value { return cannotDelete(strconv.Itoa(i), arrayHolder) }

func readOnlyLength() Value { return readOnlyProperty("length", arrayHolder) }

// refuse is the refusal of a frozen array to `a[key] = v` for a key that is text: an item it has or its
// length is read only, any other key one it cannot add, as JavaScript refuses a["x"], a["-1"] and
// a.length on a frozen array before it looks at what the key is.
func (l *list) refuse(key string) Value {
	if key == "length" {
		return readOnlyLength()
	}
	if i, isIndex := arrayIndex(key); isIndex && int(i) < len(l.items) {
		return readOnlyItem(int(i))
	}
	return notExtensible(key)
}
