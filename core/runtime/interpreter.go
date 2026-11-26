package runtime

import (
	"fmt"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// execute chạy closure trên ngăn xếp chung: closure ở stack[base-1], slot của khung là stack[base:base+Locals], rồi
// các ô của khung, ngăn xếp của phép tính ở trên đó (closure và các ô nằm đó cho holding, memory.go: không lệnh nào
// đọc chúng ở đó). at ≥ 0: CALL đã đặt closure và các đối số lên ngăn xếp, đối số bắt đầu ở at, và chúng thành slot
// tại chỗ, không chép; at < 0: Go đưa arguments (Run, Call), closure và chúng được đẩy lên. Khi trả, ngăn xếp về
// base-1. Mỗi lệnh tốn một đơn vị năng lượng. Một lỗi dừng khung và đi ra, mang chỗ của lệnh sinh ra nó
// (chỗ trong cùng thắng: lỗi từ một hàm được gọi giữ chỗ trong hàm ấy).
func (m *Machine) execute(closure *Closure, arguments []value.Value, at int) value.Value {
	if m.depth >= MaxDepth {
		return value.Fail(value.RangeCode, "maximum call stack size exceeded")
	}
	m.depth++
	defer func() { m.depth-- }()

	f := closure.function
	base := at
	if at < 0 {
		m.stack = append(m.stack, value.Function(closure))
		base = len(m.stack)
		m.stack = append(m.stack, arguments[:min(len(arguments), f.Parameters)]...)
	} else if len(m.stack) > base+f.Parameters {
		m.stack = m.stack[:base+f.Parameters] // đối số thừa không phải biến nào
	}
	for len(m.stack) < base+f.Locals {
		m.stack = append(m.stack, value.Value{})
	}
	cellsAt := len(m.stack)
	var cells []*cell
	if f.Cells > 0 {
		cells = make([]*cell, f.Cells)
		for range f.Cells {
			m.stack = append(m.stack, value.Value{})
		}
	}
	code := f.Code
	pc := 0
	fail := func(v value.Value) value.Value {
		m.stack = m.stack[:base-1]
		if v.At() == "" {
			p := f.Positions[pc-1]
			v = v.Within(fmt.Sprintf("%s:%d:%d", f.File, p.Line, p.Column))
		}
		return v
	}
	pop := func() value.Value {
		v := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		return v
	}
	push := func(v value.Value) { m.stack = append(m.stack, v) }

	for {
		if m.energy <= 0 {
			pc++
			if m.stopped.IsInvalid() {
				return fail(m.stopped)
			}
			return fail(energyFailure())
		}
		m.energy--
		in := code[pc]
		pc++
		switch in.Op {
		// --- Dữ liệu ---
		case compiler.PUSH:
			if in.B == compiler.Undefined {
				push(value.Value{})
			} else {
				push(f.Constants[in.A])
			}
		case compiler.POP:
			m.stack = m.stack[:len(m.stack)-1]
		case compiler.DUP:
			n := len(m.stack)
			m.stack = append(m.stack, m.stack[n-int(in.A):n]...)
		case compiler.LOAD:
			switch in.B {
			case compiler.Slot:
				push(m.stack[base+int(in.A)])
			case compiler.Cell:
				push(cells[in.A].value)
			default:
				push(closure.cells[in.A].value)
			}
		case compiler.STORE:
			switch in.B {
			case compiler.Slot:
				m.stack[base+int(in.A)] = pop()
			case compiler.Cell:
				cells[in.A].value = pop()
			case compiler.FreshCell:
				if !m.charge(cellBytes) {
					return fail(m.stopped)
				}
				c := &cell{value: pop()}
				cells[in.A] = c
				m.stack[cellsAt+int(in.A)] = value.Function(c)
			default:
				if closure.shared {
					return fail(value.Fail(value.TypeCode, "cannot assign to a variable this function holds: every run shares it "+
						"(made as the site booted) and only reads it; keep what a run writes in the run"))
				}
				closure.cells[in.A].value = pop()
			}

		// --- Cấu trúc ---
		case compiler.GET:
			var v value.Value
			if in.B == compiler.Named {
				v = m.get(pop(), f.Constants[in.A].String())
			} else {
				k := pop()
				v = m.getKeyed(pop(), k)
			}
			if v.IsInvalid() {
				return fail(v)
			}
			push(v)
		case compiler.SET:
			item := pop()
			var key string
			if in.B == compiler.Named {
				key = f.Constants[in.A].String()
			} else {
				var failure value.Value
				if key, failure = m.key(pop()); failure.IsInvalid() {
					return fail(failure)
				}
			}
			target := pop()
			before := target.Count()
			if r := javascript.Set(target, key, item); r.IsInvalid() {
				return fail(r)
			}
			if target.Count() > before { // một khoá hay một phần tử mới
				grown := value.KeyBytes
				if target.IsArray() {
					grown = value.ItemBytes
				}
				if !m.charge(grown) {
					return fail(m.stopped)
				}
			}
			push(item)
		case compiler.MAKE:
			if v := m.make(f, in); v.IsInvalid() {
				return fail(v)
			} else {
				push(v)
			}

		// --- Phép toán ---
		case compiler.ADD:
			b, a := pop(), pop()
			if a.IsNumber() && b.IsNumber() {
				push(value.Number(a.Number() + b.Number()))
				continue
			}
			// hai chuỗi: đếm trước khi chép (s += piece); loại khác đổi thành chữ trước, đếm sau
			both := a.IsString() && b.IsString()
			if both {
				n := len(a.String()) + len(b.String())
				if !m.charge(uint64(n)) {
					return fail(m.stopped)
				}
				if failure := m.Spend(javascript.ConcatWork(n)); failure.IsInvalid() {
					return fail(failure)
				}
			}
			r := javascript.Add(m, a, b)
			if r.IsInvalid() {
				return fail(r)
			}
			if !both && r.IsString() && !m.charge(uint64(len(r.String()))) {
				return fail(m.stopped)
			}
			push(r)
		case compiler.SUB, compiler.MUL, compiler.DIV, compiler.MOD, compiler.POW:
			b, a := pop(), pop()
			if a.IsNumber() && b.IsNumber() {
				switch in.Op {
				case compiler.SUB:
					push(value.Number(a.Number() - b.Number()))
					continue
				case compiler.MUL:
					push(value.Number(a.Number() * b.Number()))
					continue
				case compiler.DIV:
					push(value.Number(a.Number() / b.Number()))
					continue
				}
			}
			r := javascript.Arithmetic(m, arithmetic[in.Op], a, b)
			if r.IsInvalid() {
				return fail(r)
			}
			push(r)
		case compiler.NOT:
			push(value.Boolean(!javascript.ToBoolean(pop())))
		case compiler.UNARY:
			a := pop()
			var r value.Value
			switch in.A {
			case compiler.Negate:
				if a.IsNumber() {
					push(value.Number(-a.Number()))
					continue
				}
				r = javascript.Negate(m, a)
			case compiler.Plus:
				r = javascript.ToNumber(m, a)
			default:
				r = value.String(a.Typeof())
			}
			if r.IsInvalid() {
				return fail(r)
			}
			push(r)
		case compiler.COMPARE:
			b, a := pop(), pop()
			if in.A == compiler.Same || in.A == compiler.Different {
				// hai chuỗi dài cùng độ dài so từng byte: tốn năng lượng (core/javascript/work.go)
				if work := javascript.EqualWork(a, b); work > 0 {
					if failure := m.Spend(work); failure.IsInvalid() {
						return fail(failure)
					}
				}
			}
			switch in.A {
			case compiler.Same:
				push(value.Boolean(javascript.StrictEquals(a, b)))
				continue
			case compiler.Different:
				push(value.Boolean(!javascript.StrictEquals(a, b)))
				continue
			}
			if a.IsNumber() && b.IsNumber() {
				x, y := a.Number(), b.Number()
				switch in.A {
				case compiler.Less:
					push(value.Boolean(x < y))
				case compiler.Greater:
					push(value.Boolean(x > y))
				case compiler.LessEqual:
					push(value.Boolean(x <= y))
				default:
					push(value.Boolean(x >= y))
				}
				continue
			}
			r := javascript.Relation(m, relation[in.A], a, b)
			if r.IsInvalid() {
				return fail(r)
			}
			push(r)

		// --- Nhảy ---
		case compiler.JUMP:
			pc = int(in.A)
		case compiler.TRUE:
			switch in.B {
			case compiler.Pop:
				if javascript.ToBoolean(pop()) {
					pc = int(in.A)
				}
			case compiler.Keep:
				if javascript.ToBoolean(m.stack[len(m.stack)-1]) {
					pc = int(in.A)
				} else {
					m.stack = m.stack[:len(m.stack)-1]
				}
			default: // Present
				if !m.stack[len(m.stack)-1].IsNullish() {
					pc = int(in.A)
				} else {
					m.stack = m.stack[:len(m.stack)-1]
				}
			}
		case compiler.ITER:
			slot := base + int(in.B)
			list := m.stack[slot]
			if list.IsString() { // một chuỗi lặp theo ký tự, như JavaScript: đổi một lần thành mảng
				if list = m.characters(list); list.IsInvalid() {
					return fail(list)
				}
				m.stack[slot] = list
			}
			if !list.IsArray() {
				return fail(value.Fail(value.TypeCode, "%s is not iterable: for…of takes an array or a string", kindName(list)))
			}
			i := int(m.stack[slot+1].Number())
			if i >= list.Length() {
				pc = int(in.A)
				continue
			}
			m.stack[slot+1] = value.Number(float64(i + 1))
			push(list.Index(i))
		case compiler.FALSE:
			if in.B == compiler.Pop {
				if !javascript.ToBoolean(pop()) {
					pc = int(in.A)
				}
			} else if !javascript.ToBoolean(m.stack[len(m.stack)-1]) { // Keep
				pc = int(in.A)
			} else {
				m.stack = m.stack[:len(m.stack)-1]
			}

		// --- Thực thi ---
		case compiler.CALL:
			n := len(m.stack)
			count := int(in.A)
			callee := m.stack[n-count-1]
			var r value.Value
			if child, ok := callee.Function().(*Closure); ok {
				// closure và đối số nằm sẵn trên ngăn xếp: đối số thành slot của khung mới tại chỗ; khi trả ngăn xếp
				// về chỗ của closure
				r = m.execute(child, nil, n-count)
			} else {
				// hàm có sẵn: đối số là chính khúc ngăn xếp của chúng (không cấp mảng; holding thấy chúng khi nó gọi lại
				// chương trình), cap cắt ở đó nên một append của nó không ghi lên ngăn xếp; không hàm có sẵn nào giữ mảng
				// đối số quá lúc nó trả (23/11/2026)
				r = m.call(callee, m.stack[n-count:n:n])
				m.stack = m.stack[:n-count-1]
			}
			if r.IsInvalid() {
				return fail(r)
			}
			push(r)
		case compiler.INVOKE:
			if r := m.invoke(f, in); r.IsInvalid() {
				return fail(r)
			} else {
				push(r)
			}
		case compiler.LAMBDA:
			child := f.Functions[in.A]
			if !m.charge(closureBytes + uint64(len(child.Captures))*8) {
				return fail(m.stopped)
			}
			held := make([]*cell, len(child.Captures))
			for i, c := range child.Captures {
				if c.FromCell {
					held[i] = cells[c.Index]
				} else {
					held[i] = closure.cells[c.Index]
				}
			}
			push(value.Function(&Closure{function: child, cells: held}))
		case compiler.RETURN:
			r := pop()
			m.stack = m.stack[:base-1]
			return r
		default:
			return fail(value.Invalid("runtime: no instruction %s", in.Op))
		}
	}
}

// getKeyed là obj[k]: một chỉ số số nguyên của mảng đọc thẳng, còn lại qua chữ của khoá.
func (m *Machine) getKeyed(object, k value.Value) value.Value {
	if object.IsArray() && k.IsNumber() {
		if n := k.Number(); n >= 0 && n == float64(int(n)) {
			if i := int(n); i < object.Length() {
				return object.Index(i)
			}
			return value.Value{}
		}
	}
	key, failure := m.key(k)
	if failure.IsInvalid() {
		return failure
	}
	return m.get(object, key)
}

// join là [...a, b]: nối các đoạn thành một mảng mới; một mảng trải các phần tử của nó, một chuỗi các ký tự của nó,
// thứ khác là TypeError như JavaScript. Các đoạn đổi thành mảng tại chỗ (chúng nằm trên ngăn xếp, holding thấy), rồi
// mảng mới được đếm trước khi dựng.
func (m *Machine) join(segments []value.Value) value.Value {
	total := 0
	for i, segment := range segments {
		if segment.IsString() {
			if segment = m.characters(segment); segment.IsInvalid() {
				return segment
			}
			segments[i] = segment
		}
		if !segment.IsArray() {
			return value.Fail(value.TypeCode, "%s is not iterable: ... spreads an array or a string", kindName(segment))
		}
		total += segment.Length()
	}
	if !m.charge(value.MadeBytes + uint64(total)*value.ItemBytes) {
		return m.stopped
	}
	items := make([]value.Value, 0, total)
	for _, segment := range segments {
		items = append(items, segment.ReadItems()...) // append chép: không chép thêm một lần trước đó (Items)
	}
	return value.ArrayOf(items)
}

// characters là các ký tự của một chuỗi, theo code point như JavaScript lặp nó: Array.from(s).
func (m *Machine) characters(s value.Value) value.Value {
	array, _ := javascript.Global("Array")
	return javascript.Invoke(m, array, "from", []value.Value{s})
}

// template nối các phần của một template thành chữ: ToString mỗi phần (tại chỗ: các phần nằm trên ngăn xếp), đếm
// chữ sẽ làm ra, rồi nối như JavaScript nối chuỗi.
func (m *Machine) template(parts []value.Value) value.Value {
	length := 0
	for i, part := range parts {
		if !part.IsString() {
			if part = javascript.ToString(m, part); part.IsInvalid() {
				return part
			}
			parts[i] = part
		}
		length += len(part.String())
	}
	if !m.charge(uint64(length)) {
		return m.stopped
	}
	if failure := m.Spend(javascript.ConcatWork(length)); failure.IsInvalid() {
		return failure
	}
	text := value.String("")
	for _, part := range parts {
		if text = javascript.Add(m, text, part); text.IsInvalid() {
			return text
		}
	}
	return text
}

var arithmetic = map[compiler.Opcode]string{
	compiler.SUB: "-", compiler.MUL: "*", compiler.DIV: "/", compiler.MOD: "%", compiler.POW: "**",
}

var relation = map[int32]string{
	compiler.Less: "<", compiler.Greater: ">", compiler.LessEqual: "<=", compiler.GreaterEqual: ">=",
}
