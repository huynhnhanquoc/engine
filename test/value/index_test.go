package value_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Chỉ mục của object lớn (core/value/object.go, 23/11/2026): từ khoá thứ 9 có thêm một map khoá → chỗ. Mọi phép đọc,
// ghi, xoá cho cùng kết quả như duyệt, với mọi số khoá quanh ngưỡng, và thứ tự khoá giữ nguyên.

// model là object viết bằng hai slice, duyệt tìm: cái chỉ mục phải khớp.
type model struct {
	keys  []string
	items []float64
}

func (m *model) set(k string, v float64) {
	if i := slices.Index(m.keys, k); i >= 0 {
		m.items[i] = v
		return
	}
	m.keys, m.items = append(m.keys, k), append(m.items, v)
}

func (m *model) remove(k string) {
	if i := slices.Index(m.keys, k); i >= 0 {
		m.keys, m.items = slices.Delete(m.keys, i, i+1), slices.Delete(m.items, i, i+1)
	}
}

func same(t *testing.T, step string, o value.Value, m *model) {
	t.Helper()
	if got := strings.Join(o.Keys(), ","); got != strings.Join(m.keys, ",") {
		t.Fatalf("%s: keys %s, want %s", step, got, strings.Join(m.keys, ","))
	}
	for i, k := range m.keys {
		if v, found := o.Get(k); !found || v.Number() != m.items[i] {
			t.Fatalf("%s: %s is %v %v, want %v", step, k, v, found, m.items[i])
		}
	}
	for _, k := range []string{"none", "k99"} {
		if slices.Contains(m.keys, k) {
			continue
		}
		if _, found := o.Get(k); found {
			t.Fatalf("%s: %s found", step, k)
		}
	}
}

func TestObjectIndex(t *testing.T) {
	for size := 1; size <= 20; size++ {
		o, m := value.Object(), &model{}
		for i := range size {
			k := fmt.Sprintf("k%d", i)
			o.Set(k, value.Number(float64(i)))
			m.set(k, float64(i))
			same(t, fmt.Sprintf("size %d, set %s", size, k), o, m)
		}
		// ghi lại một khoá đã có giữ chỗ của nó
		o.Set("k0", value.Number(100))
		m.set("k0", 100)
		same(t, fmt.Sprintf("size %d, set again", size), o, m)
		// xoá đầu, giữa, cuối: các khoá sau dời chỗ, chỉ mục theo
		for _, k := range []string{"k0", fmt.Sprintf("k%d", size/2), fmt.Sprintf("k%d", size-1)} {
			o.Delete(k)
			m.remove(k)
			same(t, fmt.Sprintf("size %d, delete %s", size, k), o, m)
			if _, found := o.Get(k); found {
				t.Fatalf("size %d: %s found after delete", size, k)
			}
		}
		// thêm lại sau khi xoá
		o.Set("again", value.Number(7))
		m.set("again", 7)
		same(t, fmt.Sprintf("size %d, add after delete", size), o, m)
	}
}

// Đọc một object lớn không còn duyệt: 20 000 khoá đọc hết dưới 100 ms (duyệt là 2·10⁸ phép so).
func TestObjectIndexIsFast(t *testing.T) {
	o := value.Object()
	const n = 20000
	for i := range n {
		o.Set(fmt.Sprintf("key-%05d", i), value.Number(float64(i)))
	}
	start := time.Now()
	for i := range n {
		if v, _ := o.Get(fmt.Sprintf("key-%05d", i)); v.Number() != float64(i) {
			t.Fatal(i, v)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("reading %d keys took %v", n, elapsed)
	}
}

// Một object dùng chung có chỉ mục dựng sẵn khi ghi: đọc cùng lúc từ nhiều goroutine không ghi gì (go test -race).
func TestObjectIndexSharedReads(t *testing.T) {
	o := value.Object()
	for i := range 50 {
		o.Set(fmt.Sprintf("k%d", i), value.Number(float64(i)))
	}
	o.FreezeShared()
	done := make(chan bool)
	for range 4 {
		go func() {
			for i := range 50 {
				o.Get(fmt.Sprintf("k%d", i))
			}
			done <- true
		}()
	}
	for range 4 {
		<-done
	}
}
