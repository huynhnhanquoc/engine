// Package runtime chạy bytecode của core/compiler trên value.Value: một Machine là một lượt chạy (thân application.work
// lúc boot, sau này một request, một job), với năng lượng, trần byte (memory.go) và độ sâu gọi của riêng nó. Luật của JavaScript (phép toán,
// thuộc tính, phương thức) là của core/javascript; chữ của struct engine (website().listen(…)) là words.go.
package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// MaxDepth là số khung gọi lồng nhau tối đa: quá thì RangeError, như JavaScript ("maximum call stack size exceeded").
const MaxDepth = 1000

// Options là trần và nơi của một lượt chạy.
type Options struct {
	// Energy là số lệnh lượt chạy được chạy; hết là lỗi ENERGY_LIMIT ở lệnh ấy. 0 là DefaultEnergy.
	Energy int64
	// Memory là trần byte của lượt chạy (memory.go): giữ quá nửa nó, hay làm ra một thứ không vừa nó, là lỗi
	// MEMORY_LIMIT ở lệnh ấy. 0 là DefaultMemory.
	Memory int64
	// Location là múi giờ của nơi chạy (javascript.Located): Date và t.format đọc nó. nil là múi của máy.
	Location *time.Location
	// Context là context của việc lượt chạy làm cho (request): một câu lệnh database chạy dưới nó, nên dừng khi request
	// dừng. nil là context.Background().
	Context context.Context
}

// DefaultEnergy là năng lượng của một lượt chạy khi Options không nói: đủ cho mọi application.work thật, chặn một
// chương trình chạy mãi.
const DefaultEnergy = 10_000_000

// Machine là một lượt chạy: ngăn xếp chung của mọi khung, năng lượng còn lại, độ sâu gọi, bộ đếm byte. Không dùng
// chung giữa hai goroutine.
type Machine struct {
	stack    []value.Value
	energy   int64
	depth    int
	location *time.Location
	context  context.Context

	memory  uint64            // trần byte
	made    uint64            // byte đã đếm: cái còn giữ ở lần đếm lại trước cộng cái làm ra từ đó
	counted uint64            // made ngay sau lần đếm lại trước: cái làm ra từ đó tốn năng lượng ở lần sau
	kept    []javascript.Kept // cái code Go của engine đang giữ cho lượt chạy (javascript.Meter)
	stopped value.Value       // MEMORY_LIMIT, một khi đã có

	memos    [4]javascript.TextMemo // vài chuỗi dài lượt chạy vừa đọc (javascript.Memos): độ dài, con trỏ; quên khi đếm lại byte
	nextMemo int                    // chỗ thay tiếp theo, vòng quanh
}

// Memo là javascript.Memos: TextMemo của s, cái đã nhớ hay một cái mới thay cái cũ nhất. s.length trong điều kiện
// của một vòng for gặp lại cùng chuỗi mỗi vòng: lần thứ hai là O(1).
func (m *Machine) Memo(s string) *javascript.TextMemo {
	for i := range m.memos {
		if m.memos[i].Of(s) {
			return &m.memos[i]
		}
	}
	slot := &m.memos[m.nextMemo]
	m.nextMemo = (m.nextMemo + 1) % len(m.memos)
	*slot = javascript.Remember(s)
	return slot
}

// New là một lượt chạy mới với options.
func New(options Options) *Machine {
	m := &Machine{stack: make([]value.Value, 0, startStack)}
	m.start(options)
	return m
}

// startStack là chỗ đầu của ngăn xếp một máy mới; keptStack là ngăn xếp lớn nhất một máy trả về kho còn giữ: một
// lượt chạy đệ quy sâu không để lại cho kho một mảng lớn.
const (
	startStack = 256
	keptStack  = 4096
)

// machines là các máy đã xong lượt chạy (Release), chờ lượt sau (25/11/2026): ngăn xếp 8 KB của một máy là hơn
// nửa số byte engine làm ra cho một request qua chuỗi của host (15,9 KB, test/host/cost_test.go), nên mỗi request cấp
// lại nó là việc chính của GC.
var machines sync.Pool

// Take là New, với một máy đã trả về kho khi có: cùng trần, cùng nơi, ngăn xếp rỗng, không gì của lượt chạy trước.
func Take(options Options) *Machine {
	m, _ := machines.Get().(*Machine)
	if m == nil {
		return New(options)
	}
	m.start(options)
	return m
}

// Release trả m về kho cho một lượt chạy sau (Take). Chỉ gọi khi lượt chạy đã xong hẳn và không ai còn giữ m: code Go
// của engine dùng javascript.Run trong lời gọi, không giữ nó, và không giữ một khúc ngăn xếp (đối số của một hàm có
// sẵn) sau lời gọi. Mọi ô của ngăn xếp được xoá: lượt chạy sau không thấy, và GC không giữ, giá trị nào của lượt này.
func (m *Machine) Release() {
	if cap(m.stack) > keptStack {
		return
	}
	stack := m.stack[:cap(m.stack)]
	clear(stack)
	kept := m.kept[:cap(m.kept)]
	clear(kept)
	*m = Machine{stack: stack[:0], kept: kept[:0]}
	machines.Put(m)
}

// start đặt trần và nơi của lượt chạy, mặc định khi options không nói.
func (m *Machine) start(options Options) {
	energy := options.Energy
	if energy <= 0 {
		energy = DefaultEnergy
	}
	memory := options.Memory
	if memory <= 0 {
		memory = DefaultMemory
	}
	m.energy, m.memory, m.location, m.context = energy, uint64(memory), options.Location, options.Context
}

// Energy là năng lượng còn lại.
func (m *Machine) Energy() int64 { return m.energy }

// Spend là javascript.Meter: units năng lượng cho việc code Go của engine làm cho lượt chạy (core/javascript/work.go).
// Hết thì ENERGY_LIMIT ngay, không đợi lệnh sau: một sort lớn dừng trước khi bắt đầu.
func (m *Machine) Spend(units uint64) value.Value {
	if units == 0 || units < uint64(max(m.energy, 0)) {
		m.energy -= int64(units)
		return value.Value{}
	}
	m.energy = 0
	if m.stopped.IsInvalid() {
		return m.stopped
	}
	return energyFailure()
}

// energyFailure là lỗi của một lượt chạy hết năng lượng.
func energyFailure() value.Value { return value.Fail(value.EnergyCode, "the run used all its energy") }

// Location là javascript.Located: múi giờ của nơi chạy.
func (m *Machine) Location() *time.Location { return m.location }

// Context là context của lượt chạy (Options.Context), context.Background() khi không có.
func (m *Machine) Context() context.Context {
	if m.context == nil {
		return context.Background()
	}
	return m.context
}

// Run chạy thân file main (đã biên dịch) và trả giá trị return của nó, hay lỗi có chỗ (file:dòng:cột).
func (m *Machine) Run(main *compiler.Function) value.Value {
	return m.execute(&Closure{function: main}, nil, -1)
}

// Call là javascript.Run: gọi một hàm của chương trình (closure), hay một hàm có sẵn, với các đối số.
func (m *Machine) Call(function value.Value, arguments ...value.Value) value.Value {
	return m.call(function, arguments)
}

// Các đối số Go đưa vào không thoát khỏi lời gọi (25/11/2026): một closure chép chúng lên ngăn xếp khi vào khung; một
// hàm có sẵn nhận bản chép của chúng trên ngăn xếp, như đối số của INVOKE. Nhờ vậy m.Call(guard, this) của host không
// cấp một mảng đối số mỗi lời gọi (chín lần mỗi request qua chuỗi của host).
func (m *Machine) call(function value.Value, arguments []value.Value) value.Value {
	switch f := function.Function().(type) {
	case *Closure:
		return m.execute(f, arguments, -1)
	case *javascript.Native:
		n := len(m.stack)
		m.stack = append(m.stack, arguments...)
		r := f.Call(m, m.stack[n:len(m.stack):len(m.stack)]...)
		m.stack = m.stack[:n]
		return r
	}
	return value.Fail(value.TypeCode, "%s is not a function", kindName(function))
}

// Keyed là một struct của engine đọc được theo tên khoá: env.PORT. Tên nào không phải một chữ của struct là một khoá;
// chữ thắng khoá cùng tên.
type Keyed interface {
	Key(name string) value.Value
}

// Direct là một struct của engine tự trả lời các khoá và chữ của nó, không qua reflect (24/11/2026): context của một
// request (host/request.go), đọc và gọi nhiều lần mỗi request, nơi một lời gọi qua reflect tốn gần một micro giây.
// Key là this.name; Word là this.name(arguments…), arguments chỉ đọc trong lời gọi (một khúc ngăn xếp: giữ lại thì
// chép). Tên không có là lỗi do chính struct nói.
type Direct interface {
	Key(name string) value.Value
	Word(run javascript.Run, name string, arguments []value.Value) value.Value
}

// method là this.name(arguments…): chữ của struct engine, hay luật của JavaScript (cả Math, JSON, console: struct của
// core/javascript, có Members).
func (m *Machine) method(this value.Value, n *javascript.Name, arguments []value.Value) value.Value {
	if engineStruct(this) {
		if direct, ok := this.Struct().(Direct); ok {
			return direct.Word(m, n.Text(), arguments)
		}
		name := n.Text()
		if _, keyed := this.Struct().(Keyed); keyed && !hasWord(this, name) {
			return value.Fail(value.TypeCode, "%s is a key of the %s, a value, not a function", name, doorOf(this))
		}
		return callWord(this, name, arguments)
	}
	return javascript.InvokeNamed(m, m, this, n, arguments) // m là Meter của chính nó: không ép kiểu mỗi lời gọi
}

// make là lệnh MAKE: mảng, object, [...a], { ...a }, template từ các phần trên đỉnh ngăn xếp; ngăn xếp bỏ các phần ấy,
// kết quả (hay lỗi MEMORY_LIMIT) trả về cho execute. Nằm ngoài execute như invoke: vòng lệnh gọn thì mọi lệnh khác
// nhanh hơn (giữ nó trong vòng lệnh, fib và guard chậm 3–4%).
//
//go:noinline
func (m *Machine) make(f *compiler.Function, in compiler.Instruction) value.Value {
	n := len(m.stack)
	var v value.Value
	switch in.B {
	case compiler.Shaped: // các khoá là một Shape của hàm: object mượn chúng, chỉ các giá trị là của nó
		shape, _ := f.Constants[in.A].Struct().(*value.Shape)
		count := shape.Len()
		if !m.charge(value.MadeBytes + uint64(count)*value.ItemBytes) {
			return m.stopped
		}
		items := make([]value.Value, count)
		copy(items, m.stack[n-count:])
		m.stack = m.stack[:n-count]
		return value.ObjectFrom(shape, items)
	case compiler.Array:
		if !m.charge(value.MadeBytes + uint64(in.A)*value.ItemBytes) {
			return m.stopped
		}
		items := make([]value.Value, in.A)
		copy(items, m.stack[n-int(in.A):])
		m.stack = m.stack[:n-int(in.A)]
		return value.ArrayOf(items)
	case compiler.Object:
		if !m.charge(value.MadeBytes + uint64(in.A)*value.KeyBytes) {
			return m.stopped
		}
		pairs := m.stack[n-2*int(in.A):]
		v = value.Object()
		for i := 0; i < len(pairs); i += 2 {
			v.Set(pairs[i].String(), pairs[i+1])
		}
		m.stack = m.stack[:n-2*int(in.A)]
		return v
	case compiler.Copy, compiler.Merge, compiler.Extend:
		return m.spread(f, in)
	case compiler.Join:
		v = m.join(m.stack[n-int(in.A):])
	default:
		v = m.template(m.stack[n-int(in.A):])
	}
	m.stack = m.stack[:n-int(in.A)]
	return v
}

// spread là ba lệnh MAKE của một object literal có ... (compiler.object), dựng dần theo thứ tự viết: Copy (o → bản
// chép của o, mượn khoá của nó: javascript.Spread vào một object rỗng, chỗ cho A khoá viết sau), Merge (target o →
// target, các khoá của o viết vào), Extend (target và các giá trị trên ngăn xếp → target, các khoá của Shape ở hằng A
// viết vào). target là object chính literal này dựng, chưa ai khác thấy, nên ghi lên nó không bao giờ lỗi. Đếm cái
// object lớn thêm như Shallow thấy: một khoá mới, các giá trị chép, các khoá mượn chép thành của riêng nó khi nó thêm
// khoá.
func (m *Machine) spread(f *compiler.Function, in compiler.Instruction) value.Value {
	n := len(m.stack)
	switch in.B {
	case compiler.Copy:
		target := javascript.Spread(m, value.Object(), m.stack[n-1], int(in.A))
		if !target.IsInvalid() && !m.charge(target.Shallow()) {
			target = m.stopped
		}
		m.stack = m.stack[:n-1]
		return target
	case compiler.Merge:
		target := m.stack[n-2]
		before := target.Shallow()
		if r := javascript.Spread(m, target, m.stack[n-1], 0); r.IsInvalid() {
			return r
		}
		m.stack = m.stack[:n-2]
		if after := target.Shallow(); after > before && !m.charge(after-before) {
			return m.stopped
		}
		return target
	}
	shape, _ := f.Constants[in.A].Struct().(*value.Shape)
	count := shape.Len()
	target, items := m.stack[n-count-1], m.stack[n-count:]
	before := target.Shallow()
	for i, key := range shape.Keys() {
		target.Set(key, items[i])
	}
	m.stack = m.stack[:n-count-1]
	if after := target.Shallow(); after > before && !m.charge(after-before) {
		return m.stopped
	}
	return target
}

// invoke là lệnh INVOKE: this và các đối số trên đỉnh ngăn xếp, tên đã tra (methodNames); ngăn xếp về chỗ của this, kết
// quả trả về cho execute đẩy lên. Đối số là khúc ngăn xếp của chúng, như CALL một hàm có sẵn: this, đối số còn trên
// ngăn xếp tới khi trả (holding thấy). Nằm ngoài execute: vòng lệnh gọn thì mọi lệnh khác nhanh hơn.
//
//go:noinline
func (m *Machine) invoke(f *compiler.Function, in compiler.Instruction) value.Value {
	n := len(m.stack)
	count := int(in.A)
	r := m.method(m.stack[n-count-1], methodNames(f)[in.B], m.stack[n-count:n:n])
	m.stack = m.stack[:n-count-1]
	return r
}

// methodNames là Name của mỗi hằng của f mà một lệnh INVOKE gọi (nil ở hằng khác), theo chỉ số hằng: tra một lần cho
// hàm, giữ ở f.Resolved; hai lượt chạy cùng dựng lần đầu thì dựng cùng một thứ, cái nào lưu sau cũng đúng.
func methodNames(f *compiler.Function) []*javascript.Name {
	if names, ok := f.Resolved.Load().([]*javascript.Name); ok {
		return names
	}
	names := make([]*javascript.Name, len(f.Constants))
	for _, in := range f.Code {
		if in.Op == compiler.INVOKE && names[in.B] == nil {
			names[in.B] = javascript.Named(f.Constants[in.B].String())
		}
	}
	f.Resolved.Store(names)
	return names
}

// get là obj.name.
func (m *Machine) get(object value.Value, name string) value.Value {
	if engineStruct(object) {
		if direct, ok := object.Struct().(Direct); ok {
			return direct.Key(name)
		}
		if keyed, ok := object.Struct().(Keyed); ok && !hasWord(object, name) {
			return keyed.Key(name)
		}
		return readWord(object, name)
	}
	if object.IsString() {
		return javascript.Read(m, object, name) // một chuỗi dài: đọc nó tốn năng lượng (core/javascript/work.go)
	}
	return javascript.Get(object, name)
}

// engineStruct là v là một struct của engine (website, application…), không phải một tên toàn cục của JavaScript.
func engineStruct(v value.Value) bool {
	return v.IsStruct() && javascript.Members(v) == nil
}

// key là tên thuộc tính của một giá trị dùng làm khoá (a[k]): chữ của nó, như JavaScript.
func (m *Machine) key(k value.Value) (string, value.Value) {
	if k.IsString() {
		return k.String(), value.Value{}
	}
	text := javascript.ToString(m, k)
	if text.IsInvalid() {
		return "", text
	}
	return text.String(), value.Value{}
}

func kindName(v value.Value) string {
	switch {
	case v.IsNull():
		return "null"
	case v.IsArray():
		return "an array"
	case v.IsStruct():
		return "a struct of the engine"
	}
	return v.Typeof()
}
