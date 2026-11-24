package value_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Dấu lỗ nằm cạnh các phần tử: mọi bản chép của mảng thấy nó; mảng rỗng và thứ không phải mảng không mang nó; mảng
// dựng từ các phần tử của nó thì không.
func TestHolesMarkTheArray(t *testing.T) {
	a := value.Array(value.Undefined(), value.Undefined())
	copied := a
	if a.HasHoles() {
		t.Fatal("an array of items has no holes")
	}
	a.WithHoles()
	if !copied.HasHoles() {
		t.Error("every copy sees the mark")
	}
	if value.Array(a.Items()...).HasHoles() {
		t.Error("an array made of the items is a new array, whole")
	}
	a.Push(value.Number(1))
	a.SetIndex(0, value.Number(2))
	if !a.HasHoles() {
		t.Error("a write keeps the mark: only WithoutHoles takes it off")
	}
	if a.WithoutHoles().HasHoles() {
		t.Error("WithoutHoles")
	}
	if value.Array().WithHoles().HasHoles() {
		t.Error("an empty array has no place for a hole")
	}
	for _, v := range []value.Value{value.Object(), value.Number(1), value.Undefined()} {
		if v.WithHoles().HasHoles() {
			t.Errorf("%s has no holes", v.Typeof())
		}
	}
}
