package value_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Shape (core/value/object.go, 23/11/2026): các object của một literal mượn chung mảng khoá; thêm hay xoá một khoá thì
// object chép khoá thành của riêng nó trước, nên không object nào khác thấy.

func shaped(shape *value.Shape, n float64) value.Value {
	items := make([]value.Value, shape.Len())
	for i := range items {
		items[i] = value.Number(n)
	}
	return value.ObjectFrom(shape, items)
}

func joined(o value.Value) string {
	var parts []string
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		parts = append(parts, fmt.Sprintf("%s=%v", k, v.Number()))
	}
	return strings.Join(parts, ",")
}

func TestShapeCopiesOnWrite(t *testing.T) {
	for _, size := range []int{2, 3, 8, 9, 12} { // dưới và từ chỗ có chỉ mục
		keys := make([]string, size)
		for i := range keys {
			keys[i] = fmt.Sprintf("k%d", i)
		}
		shape := value.NewShape(keys)
		a, b, c := shaped(shape, 1), shaped(shape, 2), shaped(shape, 3)
		before := joined(c)

		a.Set("k0", value.Number(10))    // khoá đã có: chỉ giá trị của a
		a.Set("extra", value.Number(99)) // khoá mới: a chép khoá trước
		b.Delete("k0")                   // xoá: b chép khoá trước
		b.Set("k1", value.Number(20))

		if got := joined(c); got != before {
			t.Fatalf("size %d: an object of the same literal changed: %s, was %s", size, got, before)
		}
		if v, _ := a.Get("extra"); v.Number() != 99 || a.Count() != size+1 {
			t.Errorf("size %d: a has %d keys, extra %v", size, a.Count(), v.Number())
		}
		if _, found := b.Get("k0"); found || b.Count() != size-1 {
			t.Errorf("size %d: b still has the deleted key, %d keys", size, b.Count())
		}
		if v, _ := b.Get("k1"); v.Number() != 20 {
			t.Errorf("size %d: b.k1 is %v", size, v.Number())
		}
		if _, found := c.Get("extra"); found {
			t.Errorf("size %d: c sees a's new key", size)
		}
		// mọi khoá còn lại của b vẫn tìm thấy đúng giá trị (chỉ mục chép rồi dựng lại sau khi xoá)
		for _, k := range b.Keys() {
			if v, found := b.Get(k); !found || (k != "k1" && v.Number() != 2) {
				t.Errorf("size %d: b.%s is %v %v", size, k, v.Number(), found)
			}
		}
	}
}

// ReadKeys của một object mượn khoá không chép, và không đổi khi object thêm hay xoá khoá sau đó (JSON.stringify đọc
// các khoá lúc bắt đầu, một toJSON có thể sửa object giữa chừng).
func TestShapeReadKeysStay(t *testing.T) {
	shape := value.NewShape([]string{"b", "1", "a", "0"})
	o := shaped(shape, 1)
	read := o.ReadKeys()
	if got := strings.Join(read, ","); got != "0,1,b,a" {
		t.Fatalf("keys in JavaScript's order: %s", got)
	}
	o.Delete("b")
	o.Set("z", value.Number(2))
	if got := strings.Join(read, ","); got != "0,1,b,a" {
		t.Errorf("the keys read before changed: %s", got)
	}
	if got := strings.Join(o.ReadKeys(), ","); got != "0,1,a,z" {
		t.Errorf("the keys now: %s", got)
	}
}

// Object đóng băng mượn khoá: ghi là lỗi, khoá không đổi.
func TestShapeFrozen(t *testing.T) {
	shape := value.NewShape([]string{"a", "b"})
	o := shaped(shape, 1).Freeze()
	if r := o.Set("c", value.Number(1)); !r.IsInvalid() {
		t.Error("a frozen object took a new key")
	}
	if r := o.Delete("a"); !r.IsInvalid() {
		t.Error("a frozen object let a key go")
	}
	if o.Count() != 2 || shaped(shape, 2).Count() != 2 {
		t.Error("the keys changed")
	}
}

// sameKeys là a và b đọc cùng một mảng khoá (mượn chung), không phải hai bản chép.
func sameKeys(a, b value.Value) bool {
	x, y := a.ReadKeys(), b.ReadKeys()
	return len(x) > 0 && unsafe.SliceData(x) == unsafe.SliceData(y)
}

// built là một object dựng từng khoá (khoá của riêng nó, như JSON.parse hay o.k = v dựng): size khoá k0, k1…
func built(size int) value.Value {
	o := value.Object()
	for i := range size {
		o.Set(fmt.Sprintf("k%d", i), value.Number(float64(i)))
	}
	return o
}

// { ...o } (Assign vào một object rỗng, 24/11/2026) mượn khoá của o: Shape o mượn, hay khoá của riêng o thành một Shape
// cả hai cùng mượn. Từ đó mỗi bên thêm hay xoá khoá thì chép trước, bên kia không thấy.
func TestAssignBorrows(t *testing.T) {
	for _, size := range []int{2, 8, 9, 12} { // dưới và từ chỗ có chỉ mục
		for _, source := range []string{"literal", "built"} {
			o := built(size)
			if source == "literal" {
				keys := make([]string, size)
				for i := range keys {
					keys[i] = fmt.Sprintf("k%d", i)
				}
				o = shaped(value.NewShape(keys), 1)
			}
			before := joined(o)
			c := value.Object().Assign(o, 2)
			if !sameKeys(o, c) || joined(c) != before {
				t.Fatalf("%s %d: the copy does not borrow the keys: %s", source, size, joined(c))
			}
			e := value.Object().Assign(o, 0)
			e.Delete("k0") // xoá trên bản chép còn mượn: e chép khoá trước, o và c không thấy
			if got := joined(o); got != before || !sameKeys(o, c) || e.Count() != size-1 {
				t.Fatalf("%s %d: a copy's delete reached the source: %s", source, size, got)
			}
			c.Set("k0", value.Number(-1)) // khoá đã có: chỉ giá trị của c
			o.Set("own", value.Number(7)) // o thêm khoá: o chép trước
			c.Set("extra", value.Number(99))
			c.Delete("k1")
			d := value.Object().Assign(o, 0) // bản chép thứ hai: khoá mới của o
			if got := joined(o); got != before+",own=7" {
				t.Errorf("%s %d: the source changed with its copy: %s", source, size, got)
			}
			if v, _ := c.Get("k0"); v.Number() != -1 || c.Count() != size {
				t.Errorf("%s %d: the copy has %d keys, k0 %v", source, size, c.Count(), v.Number())
			}
			if _, found := c.Get("own"); found {
				t.Errorf("%s %d: the copy sees the source's new key", source, size)
			}
			if !sameKeys(o, d) || joined(d) != joined(o) {
				t.Errorf("%s %d: the second copy %s, the source %s", source, size, joined(d), joined(o))
			}
			for _, k := range c.Keys() { // chỉ mục chép rồi dựng lại sau khi xoá
				if _, found := c.Get(k); !found {
					t.Errorf("%s %d: c.%s is lost", source, size, k)
				}
			}
		}
	}
}

// Assign vào một object đã có khoá viết từng khoá theo thứ tự của Keys (chỉ số mảng trước), khoá sau thắng; vào một
// object đóng băng hay dùng chung mà nguồn có khoá là lỗi, như Object.assign của JavaScript.
func TestAssignIntoKeys(t *testing.T) {
	target := value.Object().Set("a", value.Number(1))
	source := value.Object().Set("b", value.Number(2)).Set("1", value.Number(3)).Set("a", value.Number(4))
	if got := strings.Join(target.Assign(source, 0).Keys(), ","); got != "1,a,b" || joined(target) != "1=3,a=4,b=2" {
		t.Errorf("keys %s, values %s", got, joined(target))
	}
	if sameKeys(target, source) {
		t.Error("a target that had keys borrows the source's")
	}
	if r := value.Object().Freeze().Assign(source, 0); !r.IsInvalid() {
		t.Error("a frozen empty object took keys")
	}
	if r := value.Object().FreezeShared().Assign(source, 0); !r.IsInvalid() {
		t.Error("a shared empty object took keys")
	}
	if r := value.Object().Freeze().Assign(value.Object(), 0); r.IsInvalid() {
		t.Error("nothing to write on a frozen object is not an error")
	}
}

// Dữ liệu dùng chung (FreezeShared) có Shape từ lúc boot: { ...o } của nhiều lượt chạy cùng lúc mượn nó, không ghi gì
// lên o (go test -race).
func TestAssignShared(t *testing.T) {
	for _, size := range []int{3, 12} {
		o := built(size)
		o.FreezeShared()
		var wg sync.WaitGroup
		for g := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range 100 {
					c := value.Object().Assign(o, 1)
					if !sameKeys(o, c) {
						t.Error("a copy of shared data does not borrow its keys")
						return
					}
					c.Set("k0", value.Number(float64(g)))
					c.Set(fmt.Sprintf("own%d", i), value.Number(1))
					if v, _ := c.Get("k0"); v.Number() != float64(g) {
						t.Errorf("k0 is %v", v.Number())
						return
					}
				}
			}()
		}
		wg.Wait()
		if o.Count() != size {
			t.Errorf("the shared object has %d keys", o.Count())
		}
	}
}

// Shallow của một object mượn khoá là phần đầu và các giá trị của nó (khoá là của Shape): bộ đếm byte đếm một bản
// chép { ...o } là cái nó làm ra, và khi nó thêm khoá thì cả các khoá nó chép thành của riêng nó.
func TestShallowBorrowed(t *testing.T) {
	o := built(12)
	c := value.Object().Assign(o, 0)
	if c.Shallow() != value.MadeBytes+12*value.ItemBytes || o.Shallow() != c.Shallow() {
		t.Errorf("a borrowing object holds %d bytes, its source %d", c.Shallow(), o.Shallow())
	}
	c.Set("extra", value.Number(1))
	if c.Shallow() != value.MadeBytes+13*value.KeyBytes {
		t.Errorf("after a new key the copy holds %d bytes", c.Shallow())
	}
}

// holding là cái Holding đếm được từ các gốc ấy.
func holding(roots ...value.Value) uint64 {
	h := value.Holding{Limit: 1 << 40}
	for _, v := range roots {
		h.Add(v)
	}
	h.Walk()
	return h.Total
}

// Khoá của riêng một object lượt chạy làm ra, thành Shape ở { ...o }, vẫn là của lượt chạy: Holding đếm chúng một lần
// dù bao nhiêu object mượn (không đếm chúng là một lượt chạy giữ chữ dài trong khoá mà bộ đếm byte không thấy). Khoá của
// dữ liệu dùng chung là của site: không đếm, kể cả khi lúc boot nó đã thành Shape ở một { ...o }.
func TestAssignHolding(t *testing.T) {
	const keys, long = 20, 4000
	o := value.Object()
	for i := range keys {
		o.Set(fmt.Sprintf("%0*d", long, i), value.Number(1)) // khoá dài, tính ra lúc chạy
	}
	text := uint64(keys * long)
	alone := holding(o)
	c := value.Object().Assign(o, 0)
	both := holding(o, c)
	if both < alone || both > alone+text/4 {
		t.Errorf("the source alone %d bytes, with its copy %d: the keys (%d bytes) are not counted once", alone, both, text)
	}
	if copied := holding(c); copied < text {
		t.Errorf("a copy alone holds %d bytes, its keys %d", copied, text)
	}
	o.FreezeShared() // như boot: o và Shape của nó là của site từ đây
	if c2 := value.Object().Assign(o, 0); holding(c2) > text/4 {
		t.Errorf("a copy of shared data counts its keys: %d bytes", holding(c2))
	}
}

// Nhiều lượt chạy dùng chung một Shape (hằng của một hàm của site): mỗi lượt sửa object của nó, không ghi gì của Shape
// (go test -race).
func TestShapeShared(t *testing.T) {
	keys := make([]string, 12)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	shape := value.NewShape(keys)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				o := shaped(shape, float64(g))
				o.Delete("k3") // trước: xoá sửa tại chỗ, trên khoá còn mượn
				o.Set(fmt.Sprintf("own%d", i), value.Number(1))
				o.ReadKeys()
				if v, _ := o.Get("k11"); v.Number() != float64(g) {
					t.Errorf("k11 is %v", v.Number())
					return
				}
			}
		}()
	}
	wg.Wait()
}
