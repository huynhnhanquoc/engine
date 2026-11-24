package javascript

import (
	"github.com/huynhnhanquoc/engine/core/value"
)

// Đọc và ghi thuộc tính của JavaScript cho runtime: v.key, v[key], v.key = x. Struct của engine không qua đây: chữ của
// chúng là việc của runtime (core/runtime/words.go).

// NewNative là một hàm có sẵn trong value.Function: host dựng các hàm của một module ("application": website,
// application, database) bằng nó, như Math.abs của Global.
func NewNative(name string, length int, run func(run Run, arguments []value.Value) value.Value) value.Value {
	return native(name, length, run)
}

// Get là v[key]: khoá riêng của object; "length" và chỉ số của mảng và chuỗi (chỉ số của chuỗi là một đơn vị UTF-16,
// như JavaScript); khoá của một tên toàn cục (Math.PI) và khoá tĩnh (Number.MAX_SAFE_INTEGER). Một phương thức đọc như
// một giá trị ("a".toUpperCase không gọi) là một hàm gọi lại nó trên v, như JavaScript cho một hàm. Khoá không có là
// undefined; đọc trên undefined hay null là TypeError.
func Get(v value.Value, key string) value.Value {
	switch {
	case v.IsInvalid():
		return v
	case v.IsNullish():
		return typeError("cannot read properties of %s (reading %q)", kindName(v), key)
	}
	if ns := Members(v); ns != nil {
		if member, found := ns.Get(key); found {
			return member
		}
	}
	switch {
	case v.IsObject():
		if own, found := v.Get(key); found {
			return own
		}
	case v.IsArray():
		if key == "length" {
			return value.Number(float64(v.Length()))
		}
		if i, ok := arrayIndex(key); ok {
			if i < v.Length() {
				return v.Index(i)
			}
			return value.Undefined()
		}
	case v.IsString():
		s := v.String()
		if key == "length" {
			return value.Number(float64(unitLength(s)))
		}
		if i, ok := arrayIndex(key); ok {
			text := newUnitText(s)
			if i < text.length() {
				return value.String(text.slice(i, i+1))
			}
			return value.Undefined()
		}
	}
	if HasMethod(v, key) {
		return native(key, 0, func(run Run, arguments []value.Value) value.Value {
			return Invoke(run, v, key, arguments)
		})
	}
	return value.Undefined()
}

// Set là v[key] = item: một khoá của object; một chỉ số đã có của mảng. Object hay mảng đã đóng băng, hay dùng chung
// từ lúc boot, từ chối như value từ chối. Ghi lên một chuỗi, một số, một tên toàn cục, undefined, null là TypeError,
// như JavaScript trong một module (luôn strict). Đổi length của mảng chưa có.
func Set(v value.Value, key string, item value.Value) value.Value {
	switch {
	case v.IsInvalid():
		return v
	case item.IsInvalid():
		return item
	case v.IsNullish():
		return typeError("cannot set properties of %s (setting %q)", kindName(v), key)
	case Members(v) != nil:
		return typeError("cannot set %q on %s: it is read only", key, kindName(v))
	case v.IsObject():
		return v.Set(key, item)
	case v.IsArray():
		if key == "length" {
			return typeError("cannot set the length of an array: write a.splice(n) to shorten it")
		}
		if i, ok := arrayIndex(key); ok {
			if i == v.Length() {
				return v.Push(item)
			}
			return v.SetIndex(i, item)
		}
		return typeError("cannot set property %q of an array: an array holds items by index", key)
	}
	return typeError("cannot create property %q on %s", key, kindName(v))
}

// arrayIndex là key viết như một chỉ số của mảng: "0", "12", không số 0 đứng đầu, không dấu, nhỏ hơn 2^31.
func arrayIndex(key string) (int, bool) {
	if key == "" || len(key) > 10 || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	n := 0 // không qua strconv: ParseUint cấp một lỗi cho mỗi khoá không phải số
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n >= 1<<31 {
		return 0, false
	}
	return n, true
}
