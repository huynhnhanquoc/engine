package runtime

import (
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// delete o.k and delete o[k] (DELETE, bytecode 10, 18/11/2026; decided 27/10/2026, it was the compile error
// "Kitwork chưa có delete"), as strict JavaScript has it: true once the key is not the value's own, the key of an
// object removed; the TYPE failure where strict mode throws: of null and undefined, a frozen object's own key (what a
// site's boot made is frozen, docs/language.md mục 1.7), an array's length, a string's or bytes' index or length, a
// function's length and name (which Kitwork's functions keep). An array's item is UNSUPPORTED: JavaScript leaves a
// hole there, which a Kitwork array never has.

// deleteKey is delete target[key]: true, or the failure.
func (vm *VM) deleteKey(target, key value.Value) value.Value {
	if target.IsNullish() {
		return value.Fail(value.TypeCode, "cannot convert "+target.Kind()+" to object: delete đọc khoá của một object")
	}
	name, failure := vm.deletedName(key)
	if failure.IsInvalid() {
		return failure
	}
	switch {
	case target.IsObject():
		if !target.Has(name) {
			return value.Boolean(true)
		}
		if target.IsFrozen() {
			return value.Fail(value.TypeCode, "cannot delete property '"+name+"' of #<Object>: object đã đóng băng (Object.freeze, hay dữ liệu file làm lúc boot)")
		}
		target.Delete(name)
		return value.Boolean(true)
	case target.IsFunction():
		if name == "length" || name == "name" {
			return value.Fail(value.TypeCode, "cannot delete property '"+name+"' of function: hàm của Kitwork giữ length và name")
		}
		if target.Has(name) {
			return value.Fail(value.TypeCode, "cannot delete property '"+name+"' of function")
		}
		return value.Boolean(true)
	case target.IsArray(), target.IsString(), target.IsBytes():
		var cursor value.TextCursor
		index, isIndex := deletedIndex(key, name)
		holder := target.Kind()
		if name == "length" {
			return value.Fail(value.TypeCode, "cannot delete property 'length' of "+holder)
		}
		if !isIndex || index >= cursor.Length(target) {
			return value.Boolean(true)
		}
		if target.IsArray() {
			return value.Fail(value.UnsupportedCode, "delete a["+name+"]: mảng của Kitwork không có chỗ trống; bỏ phần tử bằng a.splice("+name+", 1), hay đặt a["+name+"] = undefined")
		}
		return value.Fail(value.TypeCode, "cannot delete property '"+name+"' of "+holder)
	}
	return value.Boolean(true)
}

// deletedName is the key's text, as a write makes it (setObject).
func (vm *VM) deletedName(key value.Value) (string, value.Value) {
	if key.IsString() {
		return key.Text(), value.Value{}
	}
	if key.IsArray() {
		if key = vm.arrayText(key); key.IsInvalid() {
			return "", key
		}
	}
	text := key.ToString()
	if text.IsInvalid() {
		return "", text
	}
	return text.Text(), value.Value{}
}

// deletedIndex is the index a key names in an array, a string or bytes: a whole number from 0, or its canonical text.
func deletedIndex(key value.Value, name string) (int, bool) {
	if key.IsNumber() {
		n := key.Number()
		if n >= 0 && n == float64(int(n)) && n < 1<<31 {
			return int(n), true
		}
		return 0, false
	}
	index, err := strconv.Atoi(name)
	if err != nil || index < 0 || strconv.Itoa(index) != name {
		return 0, false
	}
	return index, true
}
