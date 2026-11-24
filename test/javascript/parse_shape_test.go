package javascript_test

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// JSON.parse cho các object cùng khoá mượn chung khoá (core/javascript/json.go, 24/11/2026): object đầu giữ khoá của
// riêng nó, object thứ hai cùng khoá làm chúng thành một Shape mà mọi object cùng khoá mượn. Giá trị đọc ra so với node
// ở globals_test.go; ở đây là cái node không thấy: ai mượn khoá của ai, và bộ đếm byte.

func parse(t *testing.T, r javascript.Run, text string) value.Value {
	t.Helper()
	json, _ := javascript.Global("JSON")
	v := javascript.Invoke(r, json, "parse", []value.Value{value.String(text)})
	if v.IsInvalid() {
		t.Fatal(v.Error())
	}
	return v
}

// sameKeys là a và b đọc cùng một mảng khoá (mượn chung). Khoá của riêng một object thì mỗi lần đọc là một bản chép,
// nên sameKeys(o, o) là false với nó.
func sameKeys(a, b value.Value) bool {
	x, y := a.ReadKeys(), b.ReadKeys()
	return len(x) > 0 && unsafe.SliceData(x) == unsafe.SliceData(y)
}

// rowsText là một mảng JSON n dòng, mỗi dòng các khoá k0 … k(keys-1).
func rowsText(n, keys int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("{")
		for k := range keys {
			if k > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `"k%d":%d`, k, i*keys+k)
		}
		b.WriteString("}")
	}
	b.WriteString("]")
	return b.String()
}

func row(o value.Value) string {
	var parts []string
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		parts = append(parts, fmt.Sprintf("%s=%v", k, v.Number()))
	}
	return strings.Join(parts, ",")
}

func TestParseSharesKeys(t *testing.T) {
	for _, keys := range []int{3, 12} { // dưới và từ chỗ có chỉ mục
		rows := parse(t, run{}, rowsText(50, keys))
		for i := 1; i < 50; i++ {
			if !sameKeys(rows.Index(0), rows.Index(i)) {
				t.Fatalf("%d keys: row %d does not borrow the keys of row 0", keys, i)
			}
		}
		before0, before4 := row(rows.Index(0)), row(rows.Index(4))
		rows.Index(1).Set("k0", value.Number(-1)) // ghi ở một dòng chỉ ở dòng ấy
		rows.Index(2).Set("extra", value.Number(1))
		rows.Index(3).Delete("k1")
		if row(rows.Index(0)) != before0 || row(rows.Index(4)) != before4 || !sameKeys(rows.Index(0), rows.Index(4)) {
			t.Errorf("%d keys: a write to one row reached the others: %s / %s", keys, row(rows.Index(0)), row(rows.Index(4)))
		}
		if v, _ := rows.Index(1).Get("k0"); v.Number() != -1 || rows.Index(2).Count() != keys+1 || rows.Index(3).Count() != keys-1 {
			t.Errorf("%d keys: the written rows are %s / %s / %s", keys, row(rows.Index(1)), row(rows.Index(2)), row(rows.Index(3)))
		}
	}

	// hai loại cùng khoá đầu và số khoá xen nhau: mỗi loại mượn chung trong loại của nó
	var b strings.Builder
	b.WriteString("[")
	for i := range 10 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"owner":{"id":%d,"email":"u%d"},"category":{"id":%d,"title":"c%d"}}`, i, i, i, i)
	}
	b.WriteString("]")
	rows := parse(t, run{}, b.String())
	owner := func(i int) value.Value { v, _ := rows.Index(i).Get("owner"); return v }
	category := func(i int) value.Value { v, _ := rows.Index(i).Get("category"); return v }
	for i := 1; i < 10; i++ {
		if !sameKeys(owner(0), owner(i)) || !sameKeys(category(0), category(i)) || !sameKeys(rows.Index(0), rows.Index(i)) {
			t.Fatalf("row %d: two kinds of the same first key do not each share their keys", i)
		}
	}
	if sameKeys(owner(0), category(0)) {
		t.Error("two kinds share one set of keys")
	}

	// object có khoá không lặp lại giữ khoá của riêng nó: lần đọc không làm Shape cho nó
	top := parse(t, run{}, `{"a":1,"list":[{"x":1},{"x":2}]}`)
	if sameKeys(top, top) {
		t.Error("an object whose keys come once got a Shape")
	}
	list, _ := top.Get("list")
	if !sameKeys(list.Index(0), list.Index(1)) {
		t.Error("the objects of the list do not share their keys")
	}
}

// Mảng JSON.parse đọc được dựng một lần, đúng kích thước (24/11/2026): một mảng là hai lần cấp phát (phần đầu, các phần
// tử), không lớn dần qua từng phần tử; và là một mảng bình thường, thêm phần tử được.
func TestParseArraysExact(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := range 100 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `[%d,"x",%d]`, i, i)
	}
	b.WriteString("]")
	text := b.String()
	json, _ := javascript.Global("JSON")
	arguments := []value.Value{value.String(text)}
	allocs := testing.AllocsPerRun(20, func() {
		if v := javascript.Invoke(run{}, json, "parse", arguments); v.Length() != 100 {
			t.Fatal(v, v.Error())
		}
	})
	if allocs > 2*101+20 { // 101 mảng, và cái của lần đọc
		t.Errorf("%.0f allocations for 101 arrays", allocs)
	}
	v := parse(t, run{}, text)
	inner := v.Index(99)
	if r := inner.Push(value.Number(1)); r.IsInvalid() || inner.Length() != 4 || v.Index(98).Length() != 3 {
		t.Errorf("a parsed array takes no item: %v, lengths %d %d", r.Error(), inner.Length(), v.Index(98).Length())
	}
}

// meter là một Run đếm byte JSON.parse làm ra (javascript.Meter), không có trần.
type meter struct {
	run
	made uint64
}

func (m *meter) Room(uint64) value.Value   { return value.Value{} }
func (m *meter) Made(n uint64) value.Value { m.made += n; return value.Value{} }
func (m *meter) Spend(uint64) value.Value  { return value.Value{} }
func (m *meter) Keep(javascript.Kept) int  { return 0 }
func (m *meter) LetGo(int)                 {}

// held là cái Holding đếm được từ v.
func held(v value.Value) uint64 {
	h := value.Holding{Limit: 1 << 40}
	h.Add(v)
	h.Walk()
	return h.Total
}

// Cây JSON.parse đọc ra giữ khoá của các dòng cùng khoá một lần, và bộ đếm byte đếm đúng cái cây giữ: không đếm khoá của
// từng dòng (quá cái cây giữ, một lượt chạy dừng sớm), không quên khoá của một object giữ khoá của riêng nó (dưới cái
// cây giữ: một object 2000 khoá không lặp lại).
func TestParseCountsKeysOnce(t *testing.T) {
	const n, keys = 1000, 12
	shared := uint64(n)*(value.MadeBytes+keys*value.ItemBytes) + value.MadeBytes + n*value.ItemBytes // dòng và mảng
	if h := held(parse(t, run{}, rowsText(n, keys))); h > shared+4096 {
		t.Errorf("the rows hold %d bytes, %d without their keys: the keys are held per row", h, shared)
	}
	// trong một mảng: kết quả của lời gọi (cái mảng) được đếm thêm như nó tự giữ, ít so với cái cây
	for _, text := range []string{rowsText(n, keys), rowsText(1, 2000)} {
		m := &meter{}
		v := parse(t, m, text)
		if h := held(v); m.made < h || m.made > h+h/4 {
			t.Errorf("%d bytes of JSON: JSON.parse counted %d bytes, the tree holds %d", len(text), m.made, h)
		}
	}
}
