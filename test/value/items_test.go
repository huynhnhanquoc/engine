package value_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// ReadItems (core/value/array.go, 26/11/2026) là chính các phần tử của mảng, không phải bản chép: một phép gán vào mảng
// thấy được qua slice đã lấy, còn Items thì không. Vì vậy nó chỉ dành cho code Go chép các phần tử đi ngay.
func TestReadItemsAreTheArraysOwn(t *testing.T) {
	a := value.Array(value.Number(1), value.Number(2), value.Number(3))
	read, copied := a.ReadItems(), a.Items()
	if len(read) != 3 || read[2].Number() != 3 {
		t.Fatalf("ReadItems: %v", read)
	}
	if r := a.SetIndex(0, value.Number(10)); r.IsInvalid() {
		t.Fatal(r.Error())
	}
	if read[0].Number() != 10 {
		t.Errorf("ReadItems gave a copy: after a[0] = 10 it reads %v", read[0].Number())
	}
	if copied[0].Number() != 1 {
		t.Errorf("Items did not give a copy: after a[0] = 10 it reads %v", copied[0].Number())
	}
	if got := testing.AllocsPerRun(100, func() { read = a.ReadItems() }); got != 0 {
		t.Errorf("ReadItems allocates %.0f times", got)
	}
	if empty := value.Array().ReadItems(); len(empty) != 0 {
		t.Errorf("ReadItems of an empty array: %v", empty)
	}
	for _, v := range []value.Value{value.Undefined(), value.String("abc"), value.Object(), value.Number(3)} {
		if v.ReadItems() != nil {
			t.Errorf("ReadItems of a %s is not nil", v.Typeof())
		}
	}
}
