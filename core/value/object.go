package value

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"unsafe"
)

// Chỉ file này chạm vào bên trong một object. Bên ngoài (website, runtime, …) dùng Get, Set, Delete, Keys, và Freeze,
// IsFrozen (freeze.go): đổi cách lưu (thêm chỉ mục khi object lớn) chỉ sửa file này.

// properties là thứ một object giữ, sau con trỏ: mọi bản chép của Value cùng trỏ tới nó. Hai slice song song, keys[i]
// là khoá của items[i], theo thứ tự thêm vào. Object nhỏ (phần lớn) tìm khoá bằng cách duyệt, không băm, không cấp
// phát thêm. Từ khoá thứ indexFrom có thêm chỉ mục khoá → chỗ (23/11/2026, như engine2): số đo cho thấy object lớn
// chậm theo bình phương, Object.keys, assign, { ...o }, JSON.stringify 2,4 tới 4,7 µs một khoá với 2000 khoá, vì
// mỗi khoá là một lần duyệt. Chỉ mục chỉ dựng khi ghi, không khi đọc: object dùng chung được nhiều lượt chạy đọc cùng
// lúc.
type properties struct {
	keys  []string
	items []Value
	index map[string]int // khoá → chỗ của nó trong keys; nil dưới indexFrom khoá
	// borrowed: keys và index là của một Shape, mượn chung với các object khác (cùng một object literal làm ra, bản chép
	// { ...o }); nil khi chúng là của riêng object này. Không bao giờ sửa tại chỗ: thêm hay xoá một khoá thì chép trước
	// (own).
	borrowed *Shape
	// frozen: Object.freeze, không thêm, sửa, xoá khoá nào nữa (freeze.go)
	frozen bool
	// shared: engine đóng băng nó vì mọi lượt chạy cùng đọc (FreezeShared); frozen cũng true
	shared bool
}

// Object là một object rỗng.
func Object() Value { return Value{kind: object, data: &properties{}} }

// Shape là các khoá của một object, theo thứ tự thêm vào, không trùng nhau (23/11/2026), mà nhiều object mượn chung
// thay vì mỗi object một mảng khoá: một lần cấp phát ít hơn mỗi object, và JSON.stringify đọc khoá không chép
// (ReadKeys). Ba nơi dựng nó: compiler, một lần cho mỗi object literal (một hằng của hàm, NewShape); { ...o } khi o giữ
// khoá của riêng nó (khoá của o thành một Shape, o và bản chép cùng mượn, Assign); FreezeShared, cho mỗi object dùng
// chung. Chỉ đọc, nên nhiều lượt chạy dùng chung được: một object thêm hay xoá khoá thì chép khoá thành của riêng nó
// trước (own).
type Shape struct {
	keys    []string
	index   map[string]int // từ indexFrom khoá, như của một object
	ordered []string       // các khoá theo thứ tự của Keys (chỉ số mảng trước), cho ReadKeys
	// held: các khoá là của một lượt chạy (khoá của riêng một object nó làm ra, thành Shape ở { ...o }): Holding đếm
	// chúng một lần cho Shape. Khoá của một hằng hay của dữ liệu dùng chung là của site, không đếm.
	held bool
}

// NewShape là Shape của keys (không trùng nhau: bên gọi kiểm), giữ chính slice ấy (bên gọi vừa dựng nó, không dùng nó
// nữa): dựng mỗi lần biên dịch một literal, nên không chép thêm.
func NewShape(keys []string) *Shape {
	var index map[string]int
	if len(keys) >= indexFrom {
		index = make(map[string]int, len(keys))
		for i, key := range keys {
			index[key] = i
		}
	}
	return newShape(keys, index, false)
}

// newShape là Shape của keys và chỉ mục của chúng (nil dưới indexFrom khoá), giữ chính hai thứ ấy.
func newShape(keys []string, index map[string]int, held bool) *Shape {
	s := &Shape{keys: slices.Clip(keys), index: index, held: held}
	s.ordered = s.keys
	if slices.ContainsFunc(s.keys, isIndex) { // chỉ số mảng trước: thứ tự của Keys khác thứ tự thêm vào
		s.ordered = orderedKeys(s.keys)
	}
	return s
}

// shape là Shape các khoá của o: Shape o đang mượn, hay khoá của riêng o thành một Shape từ đây (o mượn nó, chép lại
// khi thêm hay xoá khoá). Chỉ gọi khi không lượt chạy nào khác đọc o cùng lúc: không trên object dùng chung, mà
// FreezeShared đã cho mỗi object có khoá một Shape trước khi request nào đọc.
func (o *properties) shape(held bool) *Shape {
	if o.borrowed == nil {
		o.borrowed = newShape(o.keys, o.index, held)
		o.keys = o.borrowed.keys
	}
	return o.borrowed
}

// Len là số khoá của s.
func (s *Shape) Len() int { return len(s.keys) }

// Keys là các khoá của s theo thứ tự thêm vào, chỉ để đọc: chính slice của s, không chép.
func (s *Shape) Keys() []string { return s.keys }

// ObjectFrom là một object có các khoá của shape (mượn) và items (theo thứ tự khoá, giữ chính slice ấy, không chép:
// bên gọi vừa dựng nó).
func ObjectFrom(shape *Shape, items []Value) Value {
	return Value{kind: object, data: &properties{keys: shape.keys, items: items, index: shape.index, borrowed: shape}}
}

// ObjectOf là một object có keys và items (keys[i] là khoá của items[i]), giữ chính hai slice ấy, không chép (bên gọi
// vừa dựng chúng), với chỉ mục từ indexFrom khoá. false, không dựng gì, khi một khoá có hai lần: bên gọi dựng bằng Set,
// khoá sau thắng ở chỗ của lần đầu.
func ObjectOf(keys []string, items []Value) (Value, bool) {
	var index map[string]int
	if len(keys) >= indexFrom {
		index = make(map[string]int, len(keys))
		for i, key := range keys {
			if _, twice := index[key]; twice {
				return Value{}, false
			}
			index[key] = i
		}
	} else {
		for i, key := range keys {
			if slices.Contains(keys[:i], key) {
				return Value{}, false
			}
		}
	}
	return Value{kind: object, data: &properties{keys: keys, items: items, index: index}}, true
}

// Shape là Shape các khoá của object v, v mượn nó từ đây: Shape v đang mượn, hay khoá của riêng v thành một Shape của
// lượt chạy (Holding đếm nó một lần), như ở { ...v }. Bên gọi bảo đảm không lượt chạy nào khác đọc v cùng lúc. nil khi v
// không phải object, không có khoá, hay là dữ liệu dùng chung (FreezeShared đã cho nó một Shape nếu nó có khoá).
func (v Value) Shape() *Shape {
	o := v.properties()
	switch {
	case o == nil || len(o.keys) == 0:
		return nil
	case o.borrowed != nil:
		return o.borrowed
	case o.shared:
		return nil
	}
	return o.shape(true)
}

// own chép các khoá (và chỉ mục) mượn của một Shape thành của riêng o, trước khi thêm (adding) hay xoá một khoá. Thêm
// thì khoá và giá trị có cùng chỗ cho các khoá sau: chỗ các giá trị đã có ({ ...o, a, b } dựng chúng với chỗ cho a,
// b), hay gấp đôi khi chúng hết chỗ; không vậy thì khoá chép vừa khít, khoá thêm sau đó cấp phát lại lần nữa.
func (o *properties) own(adding bool) {
	if o.borrowed == nil {
		return
	}
	size := len(o.keys)
	if adding {
		if cap(o.items) == len(o.items) {
			o.items = slices.Grow(o.items, max(len(o.items), 4))
		}
		size = cap(o.items)
	}
	keys := make([]string, len(o.keys), size)
	copy(keys, o.keys)
	o.keys = keys
	if o.index != nil {
		o.index = maps.Clone(o.index)
	}
	o.borrowed = nil
}

// properties là thứ object v giữ; nil khi v không phải object.
func (v Value) properties() *properties {
	if o, ok := v.data.(*properties); ok && v.kind == object {
		return o
	}
	return nil
}

// indexFrom là số khoá từ đó object có chỉ mục (engine2: từ khoá thứ 9).
const indexFrom = 9

// find là vị trí của key trong o; -1 khi không có.
func (o *properties) find(key string) int {
	if o.index != nil {
		if i, found := o.index[key]; found {
			return i
		}
		return -1
	}
	return slices.Index(o.keys, key)
}

// reindex dựng lại chỉ mục từ chỗ from trở đi (sau khi các khoá dời chỗ), hay dựng nó lần đầu khi object vừa đủ khoá.
func (o *properties) reindex(from int) {
	if o.index == nil {
		if len(o.keys) < indexFrom {
			return
		}
		o.index, from = make(map[string]int, len(o.keys)*2), 0
	}
	for i := from; i < len(o.keys); i++ {
		o.index[o.keys[i]] = i
	}
}

// Get là giá trị ở khoá key của object v, và có khoá ấy hay không; undefined, false khi v không phải object.
func (v Value) Get(key string) (Value, bool) {
	o := v.properties()
	if o == nil {
		return Value{}, false
	}
	if i := o.find(key); i >= 0 {
		return o.items[i], true
	}
	return Value{}, false
}

// Set đặt item vào khoá key của object v rồi trả v: khoá đã có thì thay giá trị và giữ chỗ cũ, khoá mới thì thêm vào
// cuối. Lỗi khi v không phải object, và khi v đã đóng băng, kể cả khi giá trị không đổi (JavaScript cũng ném).
func (v Value) Set(key string, item Value) Value {
	o := v.properties()
	if o == nil {
		return typeError("cannot set property %q of %s", key, describe(v))
	}
	i := o.find(key)
	switch {
	case o.shared:
		return sharedObjectWrite("write", key)
	case o.frozen && i >= 0:
		return typeError("cannot assign to read only property %q of a frozen object", key)
	case o.frozen:
		return typeError("cannot add property %q, object is not extensible", key)
	case i >= 0:
		o.items[i] = item
		return v
	}
	o.own(true)
	if o.keys == nil {
		// khoá đầu tiên: chỗ cho 4 khoá ngay, vì phần lớn object nhỏ. Để append nới dần (1, 2, 4) thì 4 khoá tốn ba lần
		// cấp phát cho mỗi slice.
		o.keys = make([]string, 0, 4)
		o.items = make([]Value, 0, 4)
	}
	o.keys = append(o.keys, key)
	o.items = append(o.items, item)
	o.reindex(len(o.keys) - 1)
	return v
}

// Delete gỡ khoá key khỏi object v rồi trả v; các khoá còn lại giữ thứ tự. Không có khoá ấy thì không làm gì, như
// delete của JavaScript, kể cả trên object đã đóng băng. Lỗi khi v không phải object, và khi gỡ một khoá của object đã
// đóng băng.
func (v Value) Delete(key string) Value {
	o := v.properties()
	if o == nil {
		return typeError("cannot delete property %q of %s", key, describe(v))
	}
	if i := o.find(key); i >= 0 {
		if o.shared {
			return sharedObjectWrite("delete", key)
		}
		if o.frozen {
			return typeError("cannot delete property %q of a frozen object", key)
		}
		o.own(false)
		o.keys = slices.Delete(o.keys, i, i+1)
		o.items = slices.Delete(o.items, i, i+1)
		if o.index != nil {
			delete(o.index, key)
			o.reindex(i) // các khoá sau i dời lên một chỗ
		}
	}
	return v
}

// Assign viết các khoá của object source vào object v rồi trả v, như Object.assign(v, source) và { ...source } của
// JavaScript: theo thứ tự của Keys, khoá đã có thì thay giá trị. v chưa có khoá nào (object literal bắt đầu bằng
// ...source, Object.assign({}, source)) thì mượn khoá của source thay vì chép từng khoá: Shape source đang mượn, hay
// khoá của riêng source thành một Shape cả hai cùng mượn (source chép lại khi nó thêm hay xoá khoá); chỉ các giá trị
// được chép, với chỗ cho room giá trị nữa (các khoá literal viết sau ...source). source không phải object thì không
// làm gì. Lỗi của Set khi v đóng băng hay dùng chung mà source có khoá, và khi v không phải object.
func (v Value) Assign(source Value, room int) Value {
	o := v.properties()
	if o == nil {
		return typeError("cannot assign properties to %s", describe(v))
	}
	from := source.properties()
	if from == nil || len(from.keys) == 0 {
		return v
	}
	if len(o.keys) == 0 && !o.frozen && (from.borrowed != nil || !from.shared) {
		s := from.shape(true)
		o.keys, o.index, o.borrowed = s.keys, s.index, s
		o.items = make([]Value, len(from.items), len(from.items)+max(room, 0))
		copy(o.items, from.items)
		return v
	}
	keys := from.keys
	if from.borrowed != nil {
		keys = from.borrowed.ordered
	} else if slices.ContainsFunc(keys, isIndex) {
		keys = orderedKeys(keys)
	}
	// không khoá nào là chỉ số mảng: thứ tự của Keys là thứ tự của items, đi thẳng không tra khoá. Set lên v không
	// đổi gì của source: v là source thì mọi khoá đã có (chỉ thay giá trị), khác thì khoá mượn chung được chép trước.
	aligned := unsafe.SliceData(keys) == unsafe.SliceData(from.keys)
	for i, key := range keys {
		var item Value
		if aligned {
			item = from.items[i]
		} else {
			item, _ = source.Get(key)
		}
		if r := v.Set(key, item); r.IsInvalid() {
			return r
		}
	}
	return v
}

// freezeObject đóng băng object v (freeze.go); đã đóng băng thì không ghi lại dấu.
func (v Value) freezeObject() {
	if o := v.properties(); o != nil && !o.frozen {
		o.frozen = true
	}
}

// objectFrozen là v là object đã đóng băng.
func (v Value) objectFrozen() bool {
	o := v.properties()
	return o != nil && o.frozen
}

// shareObject đánh dấu object v là dữ liệu dùng chung (FreezeShared) rồi thêm các giá trị nó giữ vào pending, để đóng
// băng tiếp; đã dùng chung thì không ghi, không thêm gì: một object chứa chính nó dừng ở đó.
func (v Value) shareObject(pending []Value) []Value {
	o := v.properties()
	if o == nil || o.shared {
		return pending
	}
	o.frozen, o.shared = true, true
	if len(o.keys) > 0 {
		// khoá của nó thành một Shape ngay lúc này, khi chưa request nào đọc: { ...o } của mọi lượt chạy mượn nó mà
		// không ghi gì lên o. Shape là của site từ đây, như o: không lượt chạy nào đếm khoá của nó.
		o.shape(false).held = false
	}
	return append(pending, o.items...)
}

// objectShared là v là object dùng chung.
func (v Value) objectShared() bool {
	o := v.properties()
	return o != nil && o.shared
}

// sharedObjectWrite là lỗi của một phép ghi lên object dùng chung. Không phải lời của JavaScript: ở JavaScript object
// ấy mở, nên lời báo nói vì sao nó đóng và cách viết thay.
func sharedObjectWrite(verb, key string) Value {
	return typeError("cannot %s property %q: the object is shared by every run (made as the site booted) and only read; "+
		"write to a copy, { ...o }", verb, key)
}

// Keys là các khoá của object v theo thứ tự của JavaScript: khoá là chỉ số mảng ("0", "7") trước, tăng dần, rồi các
// khoá khác theo thứ tự thêm vào. Bản chép: sửa nó không sửa v. nil khi v không phải object.
func (v Value) Keys() []string {
	o := v.properties()
	if o == nil {
		return nil
	}
	return orderedKeys(o.keys)
}

// ReadKeys là các khoá của object v theo thứ tự của Keys, chỉ để đọc, không sửa: các khoá mượn của một Shape thì
// không chép (chúng không bao giờ bị sửa tại chỗ, kể cả khi object thêm hay xoá khoá trong lúc bên gọi đọc), của riêng
// object thì chép như Keys. nil khi v không phải object.
func (v Value) ReadKeys() []string {
	o := v.properties()
	if o == nil {
		return nil
	}
	if o.borrowed != nil {
		return o.borrowed.ordered
	}
	return orderedKeys(o.keys)
}

// orderedKeys là bản chép của keys theo thứ tự của JavaScript: chỉ số mảng trước, tăng dần, rồi các khoá khác theo
// thứ tự thêm vào.
func orderedKeys(keys []string) []string {
	if !slices.ContainsFunc(keys, isIndex) {
		return slices.Clone(keys)
	}
	var indexes, others []string
	for _, key := range keys {
		if isIndex(key) {
			indexes = append(indexes, key)
		} else {
			others = append(others, key)
		}
	}
	slices.SortFunc(indexes, func(a, b string) int {
		x, _ := strconv.ParseUint(a, 10, 32)
		y, _ := strconv.ParseUint(b, 10, 32)
		return cmp.Compare(x, y)
	})
	return append(indexes, others...)
}

// isIndex là khoá viết đúng một chỉ số mảng của JavaScript: số nguyên thập phân từ 0 tới 2^32−2, không số 0 đứng đầu.
func isIndex(key string) bool {
	if key == "" || len(key) > 10 || (key[0] == '0' && len(key) > 1) {
		return false
	}
	return digitsBelow(key, 1<<32-1)
}

// digitsBelow là key toàn chữ số thập phân và số ấy nhỏ hơn limit (key ngắn, không rỗng). Không qua strconv: ParseUint
// cấp một lỗi cho mỗi khoá không phải số ("id", "name"), và Keys hỏi mọi khoá của mọi object JSON.stringify viết.
func digitsBelow(key string, limit uint64) bool {
	n := uint64(0)
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + uint64(c-'0')
	}
	return n < limit
}
