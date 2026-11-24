package value_test

import (
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// closure là một Sharer như closure của runtime: giữ vài giá trị, đánh dấu mình dùng chung ở lần Share đầu.
type closure struct {
	held   []value.Value
	shares int
	shared bool
}

func (c *closure) Share() []value.Value {
	c.shares++
	if c.shared {
		return nil
	}
	c.shared = true
	return c.held
}

// FreezeShared đóng băng sâu: mọi object, mảng với tới được, kể cả qua một closure và một vòng; thứ chương trình đã
// đóng băng cũng thành dùng chung; một closure với tới theo hai đường chỉ được đi qua một lần.
func TestFreezeSharedIsDeep(t *testing.T) {
	captured := value.Object().Set("count", value.Number(0))
	handler := &closure{held: []value.Value{captured}}
	deep := value.Object()
	frozenByProgram := value.Object().Set("inner", value.Array(value.Number(1))).Freeze()
	root := value.Object().
		Set("list", value.Array(value.Number(1), value.Object().Set("deep", deep))).
		Set("handler", value.Function(handler)).
		Set("again", value.Function(handler)).
		Set("frozen", frozenByProgram)
	root.Set("self", root)

	root.FreezeShared()

	inner, _ := frozenByProgram.Get("inner")
	list, _ := root.Get("list")
	for name, v := range map[string]value.Value{
		"root": root, "list": list, "list[1]": list.Index(1), "deep": deep, "captured": captured,
		"frozen by the program": frozenByProgram, "inside what the program froze": inner,
	} {
		if !v.IsShared() || !v.IsFrozen() {
			t.Errorf("%s must be shared and frozen", name)
		}
	}
	if handler.shares != 2 || !handler.shared {
		t.Errorf("the closure reached twice is shared once: %d calls", handler.shares)
	}

	// mọi phép ghi nói vì sao và cách viết thay; không gì đổi
	failure := func(v value.Value) string {
		if !v.IsInvalid() {
			return "no error"
		}
		return v.Error().Error()
	}
	const object = `: the object is shared by every run (made as the site booted) and only read; write to a copy, { ...o }`
	const array = `cannot change the array: it is shared by every run (made as the site booted) and only read; change a copy, [...a]`
	for _, c := range []struct{ name, got, want string }{
		{"set", failure(deep.Set("a", value.Number(1))), `cannot write property "a"` + object},
		{"set what the program froze", failure(frozenByProgram.Set("inner", value.Null())), `cannot write property "inner"` + object},
		{"delete", failure(root.Delete("list")), `cannot delete property "list"` + object},
		{"delete a key it has not", failure(root.Delete("nope")), "no error"},
		{"push", failure(list.Push(value.Number(2))), array},
		{"set an index", failure(list.SetIndex(0, value.Number(2))), array},
		{"splice", failure(list.Splice(0, 1)), array},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
	if len(deep.Keys()) != 0 || list.Length() != 2 {
		t.Error("a refused write changes nothing")
	}

	// bản chép mở; Object.freeze của chương trình không phải dùng chung; gọi lại không đi lại
	if value.Array(list.Items()...).IsShared() || value.Object().Freeze().IsShared() {
		t.Error("a copy is open, and Object.freeze is not shared")
	}
	root.FreezeShared()
	if handler.shares != 2 {
		t.Error("FreezeShared of what is shared walks nothing")
	}
	for _, v := range []value.Value{value.Number(1), value.String("x"), value.Function("f")} {
		if v.FreezeShared().IsShared() {
			t.Errorf("%s holds nothing to share", v.Typeof())
		}
	}
}

// Một mảng lồng hai trăm nghìn tầng: FreezeShared đi bằng danh sách chờ, không đệ quy.
func TestFreezeSharedOfADeepNesting(t *testing.T) {
	innermost := value.Array()
	nested := innermost
	for range 200_000 {
		nested = value.Array(nested)
	}
	nested.FreezeShared()
	if !innermost.IsShared() {
		t.Error("the innermost array must be shared")
	}
}

// Held gom mọi Value trong các trường xuất ra kiểu Value và []Value, theo thứ tự khai; bỏ trường viết thường và trường
// kiểu khác; nhận struct hay con trỏ tới struct.
func TestHeld(t *testing.T) {
	type door struct {
		Name    value.Value
		Count   int
		Hooks   []value.Value
		hidden  value.Value
		Default value.Value
	}
	d := door{
		Name:    value.String("web"),
		Count:   3,
		Hooks:   []value.Value{value.Number(1), value.Number(2)},
		hidden:  value.String("not held"),
		Default: value.Null(),
	}
	for _, structure := range []any{d, &d} {
		held := value.Held(structure)
		if len(held) != 4 || held[0].String() != "web" || held[1].Number() != 1 || held[2].Number() != 2 || !held[3].IsNull() {
			t.Errorf("%T: %d values", structure, len(held))
		}
	}
	if value.Held(5) != nil || value.Held(nil) != nil {
		t.Error("not a struct holds nothing")
	}
}
