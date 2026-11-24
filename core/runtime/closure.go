package runtime

import (
	"fmt"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/value"
)

// cell là một biến bị hàm con bắt: khung khai nó và mọi closure bắt nó cùng giữ một cell.
type cell struct{ value value.Value }

// Closure là một hàm mũi tên của chương trình: mã của nó và các biến nó bắt. Nó là một value.Sharer: khi engine đóng
// băng bản thiết kế lúc boot (value.FreezeShared), các giá trị nó bắt được đóng băng theo (const seen = {} mà một guard
// đọc), và từ đó gán vào một biến nó bắt là lỗi: biến ấy mọi request cùng đọc.
type Closure struct {
	function *compiler.Function
	cells    []*cell
	shared   bool
}

// Name là tên của hàm (const f = … cho tên f), "" khi không có.
func (c *Closure) Name() string { return c.function.Name }

// At là chỗ của hàm trong file của nó: "application.work:15:9".
func (c *Closure) At() string {
	return fmt.Sprintf("%s:%d:%d", c.function.File, c.function.Pos.Line, c.function.Pos.Column)
}

// Share là value.Sharer: đánh dấu closure dùng chung và trả các giá trị nó bắt; lần sau trả nil.
func (c *Closure) Share() []value.Value {
	if c.shared {
		return nil
	}
	c.shared = true
	held := make([]value.Value, len(c.cells))
	for i, cell := range c.cells {
		held[i] = cell.value
	}
	return held
}

// Shared là closure đã được đóng băng cùng bản thiết kế.
func (c *Closure) Shared() bool { return c.shared }

// Function là hàm đã biên dịch của closure: thứ đọc thân hàm mà không chạy nó (Syntax của nó) dùng, như điều kiện của
// một câu lệnh database dịch sang SQL.
func (c *Closure) Function() *compiler.Function { return c.function }

// Captured là giá trị hiện tại của biến name mà closure bắt (một biến của hàm bao nó, hay của cấp file), và có bắt nó
// hay không.
func (c *Closure) Captured(name string) (value.Value, bool) {
	for i, capture := range c.function.Captures {
		if capture.Name == name && i < len(c.cells) && c.cells[i] != nil {
			return c.cells[i].value, true
		}
	}
	return value.Value{}, false
}
