package value

import (
	"slices"
	"strconv"
)

// Chỉ file này chạm vào bên trong một mảng. Bên ngoài dùng Length, Index, Items, Push, SetIndex, Splice, dấu lỗ
// (WithHoles, WithoutHoles, HasHoles), và Freeze, IsFrozen (freeze.go). Ba phép ghi ấy đủ cho mọi phương thức sửa mảng
// của JavaScript (pop, shift, sort, reverse, fill…), dựng ở core/javascript; cả ba từ chối một mảng đã đóng băng.

// list là thứ một mảng giữ, sau con trỏ: mọi bản chép của Value cùng trỏ tới nó, nên push vào một bản thấy ở mọi
// bản, và dấu cạnh các phần tử (holes, frozen) cũng vậy.
type list struct {
	items []Value
	// holes: Array(n) dựng mảng này và chưa có fill nào điền kín nó. Mảng JavaScript có lỗ ở đó; mảng Kitwork giữ
	// undefined (WithHoles). Như engine2 (19/11/2026).
	holes bool
	// frozen: Object.freeze, không thêm, sửa, gỡ phần tử nào nữa (freeze.go)
	frozen bool
	// shared: engine đóng băng nó vì mọi lượt chạy cùng đọc (FreezeShared); frozen cũng true
	shared bool
}

// Array là một mảng giữ bản chép của items: sửa items sau đó không sửa mảng.
func Array(items ...Value) Value {
	return Value{kind: array, data: &list{items: slices.Clone(items)}}
}

// ArrayOf là một mảng giữ chính items, không chép: cho một slice bên gọi vừa dựng và không dùng nữa (lệnh MAKE, kết
// quả của map, filter, split…), nơi Array chép một lần thừa. Một slice bên gọi còn giữ (đối số của một hàm có sẵn là
// một khúc ngăn xếp) phải qua Array.
func ArrayOf(items []Value) Value {
	return Value{kind: array, data: &list{items: items}}
}

// list là thứ mảng v giữ; nil khi v không phải mảng.
func (v Value) list() *list {
	if l, ok := v.data.(*list); ok && v.kind == array {
		return l
	}
	return nil
}

// Length là số phần tử của mảng v; 0 khi v không phải mảng.
func (v Value) Length() int {
	l := v.list()
	if l == nil {
		return 0
	}
	return len(l.items)
}

// Index là phần tử thứ i của mảng v; undefined ngoài mảng hay khi v không phải mảng.
func (v Value) Index(i int) Value {
	l := v.list()
	if l == nil || i < 0 || i >= len(l.items) {
		return Value{}
	}
	return l.items[i]
}

// Items là bản chép các phần tử của mảng v: sửa nó không sửa mảng. nil khi v không phải mảng.
func (v Value) Items() []Value {
	l := v.list()
	if l == nil {
		return nil
	}
	return slices.Clone(l.items)
}

// ItemsBetween là bản chép các phần tử [start, end) của mảng v, kẹp vào độ dài: slice chép đúng khúc nó cần, không
// cả mảng.
func (v Value) ItemsBetween(start, end int) []Value {
	l := v.list()
	if l == nil {
		return nil
	}
	end = min(end, len(l.items))
	start = min(max(start, 0), end)
	return slices.Clone(l.items[start:end])
}

// List là các phần tử của v khi v là mảng, hay chính [v] khi v không phải mảng: cho một khoá của object cấu hình nhận
// một giá trị hay một mảng (listen: 8080 hay listen: [443, 80]).
func (v Value) List() []Value {
	if l := v.list(); l != nil {
		return slices.Clone(l.items)
	}
	return []Value{v}
}

// Push thêm items vào cuối mảng v rồi trả v; mọi bản chép của v thấy chúng. Lỗi khi v không phải mảng, và khi v đã
// đóng băng, kể cả khi không có gì để thêm (push() của JavaScript ghi length).
func (v Value) Push(items ...Value) Value {
	l := v.list()
	switch {
	case l == nil:
		return typeError("cannot push to %s", describe(v))
	case l.shared:
		return sharedArrayWrite()
	case l.frozen && len(items) > 0:
		return typeError("cannot add property %q, object is not extensible", strconv.Itoa(len(l.items)))
	case l.frozen:
		return frozenLength()
	}
	l.items = append(l.items, items...)
	return v
}

// SetIndex đặt item vào chỗ i của mảng v rồi trả v; i bằng độ dài là thêm vào cuối. Một i xa hơn sẽ để lỗ ở giữa, mà
// mảng của Kitwork không có lỗ: lỗi, như i âm và khi v không phải mảng.
func (v Value) SetIndex(i int, item Value) Value {
	l := v.list()
	switch {
	case l == nil:
		return typeError("cannot set index %d of %s", i, describe(v))
	case i < 0 || i > len(l.items):
		return rangeError("index %d is outside an array of length %d", i, len(l.items))
	case l.shared:
		return sharedArrayWrite()
	case l.frozen && i == len(l.items):
		return typeError("cannot add property %q, object is not extensible", strconv.Itoa(i))
	case l.frozen:
		return typeError("cannot assign to read only property %q of a frozen array", strconv.Itoa(i))
	case i == len(l.items):
		l.items = append(l.items, item)
	default:
		l.items[i] = item
	}
	return v
}

// WithHoles đánh dấu mảng v (không rỗng) là mảng JavaScript có lỗ, như Array(n) của n > 0 dựng, rồi trả v. Mảng
// Kitwork giữ undefined ở đó, đọc ra như JavaScript đọc một lỗ (a[i], length, join, JSON, for…of, [...a],
// Array.from, find, includes); thứ JavaScript bỏ qua lỗ (map, forEach, filter, every, indexOf, Object.keys, { ...a }…)
// thì không bỏ qua undefined, nên core/javascript và runtime từ chối chúng trên mảng có dấu này. Dấu không gỡ khi một
// chỗ được ghi (a[i] = x): chỉ fill cả mảng gỡ nó (WithoutHoles). Một lời từ chối thừa còn hơn một kết quả khác
// JavaScript.
func (v Value) WithHoles() Value {
	if l := v.list(); l != nil && len(l.items) > 0 {
		l.holes = true
	}
	return v
}

// WithoutHoles gỡ dấu lỗ của mảng v: nó vừa được điền kín (Array(n).fill(x)). Trả v.
func (v Value) WithoutHoles() Value {
	if l := v.list(); l != nil {
		l.holes = false
	}
	return v
}

// HasHoles là v là mảng mang dấu lỗ (WithHoles).
func (v Value) HasHoles() bool {
	l := v.list()
	return l != nil && l.holes
}

// Splice gỡ count phần tử từ chỗ start của mảng v, đặt items vào chỗ ấy, và trả một mảng mới các phần tử đã gỡ. start
// và count được kẹp vào trong mảng. Lỗi khi v không phải mảng, và khi v đã đóng băng, kể cả khi không gỡ hay đặt gì:
// pop, shift, unshift, splice của JavaScript ghi length, nên luôn ném trên một mảng đã đóng băng.
func (v Value) Splice(start, count int, items ...Value) Value {
	l := v.list()
	switch {
	case l == nil:
		return typeError("cannot splice %s", describe(v))
	case l.shared:
		return sharedArrayWrite()
	case l.frozen && count == len(items) && count > 0:
		// thay đúng chừng ấy phần tử (reverse, sort): JavaScript ghi chỗ start, không ghi length
		return typeError("cannot assign to read only property %q of a frozen array", strconv.Itoa(max(0, start)))
	case l.frozen:
		return frozenLength()
	}
	start = max(0, min(start, len(l.items)))
	count = max(0, min(count, len(l.items)-start))
	removed := slices.Clone(l.items[start : start+count])
	l.items = slices.Replace(l.items, start, start+count, items...)
	return Value{kind: array, data: &list{items: removed}}
}

// frozenLength là lỗi của một phép ghi length lên mảng đã đóng băng.
func frozenLength() Value {
	return typeError(`cannot assign to read only property "length" of a frozen array`)
}

// freezeArray đóng băng mảng v (freeze.go); đã đóng băng thì không ghi lại dấu.
func (v Value) freezeArray() {
	if l := v.list(); l != nil && !l.frozen {
		l.frozen = true
	}
}

// arrayFrozen là v là mảng đã đóng băng.
func (v Value) arrayFrozen() bool {
	l := v.list()
	return l != nil && l.frozen
}

// shareArray đánh dấu mảng v là dữ liệu dùng chung (FreezeShared) rồi thêm các phần tử của nó vào pending; đã dùng
// chung thì không ghi, không thêm gì.
func (v Value) shareArray(pending []Value) []Value {
	l := v.list()
	if l == nil || l.shared {
		return pending
	}
	l.frozen, l.shared = true, true
	return append(pending, l.items...)
}

// arrayShared là v là mảng dùng chung.
func (v Value) arrayShared() bool {
	l := v.list()
	return l != nil && l.shared
}

// sharedArrayWrite là lỗi của một phép ghi lên mảng dùng chung (sharedObjectWrite).
func sharedArrayWrite() Value {
	return typeError("cannot change the array: it is shared by every run (made as the site booted) and only read; " +
		"change a copy, [...a]")
}
