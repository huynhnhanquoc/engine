package javascript

import (
	"sync"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Tên phương thức tra sẵn (23/11/2026). Một lời gọi this.name(…) tra name trong bảng phương thức của loại this, rồi
// trong bảng mọi giá trị có, rồi bảng các phương thức không đi qua lỗ: mỗi lần một phép băm chuỗi, khoảng 15% thời
// gian của một lời gọi rẻ ("ab".slice(1), a.push(x)). Tên của một lệnh INVOKE là hằng, nên Name tra nó một lần cho mọi
// loại; runtime giữ Name của mỗi lệnh INVOKE cạnh hàm đã biên dịch (compiler.Function.Resolved), mọi lượt chạy đọc
// chung. Invoke(name) đi cùng đường, qua nameOf.

// Name là một tên phương thức đã tra: phương thức của nó ở mỗi loại (nil khi loại ấy không có), ở mọi giá trị
// (hasOwnProperty, toString, valueOf), và nó có từ chối mảng có lỗ không. Chỉ đọc sau khi dựng.
type Name struct {
	text    string
	methods [kindCount]method
	every   method
	holes   bool
}

// Các loại có bảng phương thức riêng, theo thứ tự methodsOf hỏi.
const (
	kindString = iota
	kindNumber
	kindBoolean
	kindArray
	kindTime
	kindDuration
	kindBytes
	kindFunction
	kindCount
)

// kindOf là loại của this trong các bảng phương thức (methodsOf), −1 khi nó không có bảng (object, null…).
func kindOf(this value.Value) int {
	switch {
	case this.IsString():
		return kindString
	case this.IsNumber():
		return kindNumber
	case this.IsBoolean():
		return kindBoolean
	case this.IsArray():
		return kindArray
	case this.IsTime():
		return kindTime
	case this.IsDuration():
		return kindDuration
	case this.IsBytes():
		return kindBytes
	case this.IsFunction():
		return kindFunction
	}
	return -1
}

// Named tra name trong mọi bảng một lần.
func Named(name string) *Name {
	n := &Name{text: name, every: everyMethods[name], holes: holeSensitive[name]}
	for k, table := range [kindCount]map[string]method{
		stringMethods, numberMethods, booleanMethods, arrayMethods, timeMethods, durationMethods, bytesMethods, functionMethods,
	} {
		n.methods[k] = table[name]
	}
	return n
}

// Text là chữ của tên.
func (n *Name) Text() string { return n.text }

// names là các Name Invoke đã dựng, theo chữ: chỉ tên có ở một bảng (một tên lạ dựng lại mỗi lần, để bảng không lớn
// mãi theo những gì một chương trình gõ).
var names sync.Map

// nameOf là Name của name cho Invoke.
func nameOf(name string) *Name {
	if n, found := names.Load(name); found {
		return n.(*Name)
	}
	n := Named(name)
	if n.known() {
		names.Store(name, n)
	}
	return n
}

// known là n có ở ít nhất một bảng.
func (n *Name) known() bool {
	if n.every != nil || n.holes {
		return true
	}
	for _, m := range n.methods {
		if m != nil {
			return true
		}
	}
	return false
}
