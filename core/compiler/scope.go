package compiler

import (
	"fmt"

	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Phân giải tên: một lượt đi trước khi sinh mã. Mỗi khai báo (const, let, tham số, import) là một binding; mỗi tên được
// đọc hay ghi trỏ tới binding của nó. let và const được nâng lên đầu tầng của chúng như JavaScript, nên một hàm gọi
// được một hàm khai sau nó; đọc một tên trước khai báo của nó trong cùng hàm là lỗi biên dịch (JavaScript ném lúc
// chạy). Một binding được đọc từ một hàm con là "bị bắt": nó sống trong một ô (cell) thay vì một slot, để closure và
// khung cùng thấy một giá trị.

// bindingKind là binding ở đâu.
type bindingKind int

const (
	localBinding    bindingKind = iota // một slot, hay một ô khi bị bắt
	constantBinding                    // một import hay một tên toàn cục: một hằng biết lúc biên dịch
)

type binding struct {
	name        string
	kind        bindingKind
	constant    bool // const, import, tên toàn cục: không gán được
	declared    syntax.Pos
	owner       *function
	slot        int
	cell        int // chỉ số ô khi captured
	captured    bool
	initialized bool // khai báo của nó đã đi qua (trong lượt phân giải)
	value       value.Value
}

// function là phần phân giải của một hàm.
type function struct {
	parent   *function
	blocks   []map[string]*binding
	locals   int
	cells    int
	bindings []*binding // mọi binding cục bộ của hàm, theo thứ tự khai
	captures []*binding // binding của hàm ngoài mà hàm này bắt; chỉ số là chỉ số ô bắt được
	capture  map[*binding]int
	lifted   map[*syntax.Block][]*binding // binding được nâng lên đầu mỗi block
}

// resolution là kết quả của lượt phân giải cho cả file.
type resolution struct {
	file         string
	names        map[*syntax.Identifier]*binding // mỗi tên đọc, hay đích của một phép gán
	declared     map[*syntax.Declaration]*binding
	patterns     map[*syntax.Declaration][]*binding // mỗi tên của một khai báo giải cấu trúc, theo thứ tự Names()
	loopBindings map[syntax.Statement][]*binding    // For: biến đếm; ForOf: danh sách, chỉ số (hai slot liền nhau), rồi các tên của mỗi vòng
	temps        map[*syntax.Update]*binding        // a.b++, a[k]++: slot giữ giá trị cũ
	params       map[*syntax.Arrow][]*binding
	functions    map[syntax.Node]*function // *syntax.Arrow, và *syntax.Program cho thân file
	program      *function
}

type resolver struct {
	*resolution
	globals func(string) (value.Value, bool)
	current *function
}

func (r *resolver) fail(at syntax.Pos, format string, arguments ...any) {
	panic(failure{&syntax.Error{File: r.file, Pos: at, Message: fmt.Sprintf(format, arguments...)}})
}

// failure là lỗi ném lên bằng panic, bắt ở Compile.
type failure struct{ err *syntax.Error }

func (r *resolver) enterFunction(node syntax.Node) *function {
	f := &function{parent: r.current, capture: map[*binding]int{}, lifted: map[*syntax.Block][]*binding{}}
	r.functions[node] = f
	r.current = f
	return f
}

func (r *resolver) leaveFunction() {
	f := r.current
	for _, b := range f.bindings {
		if b.captured {
			b.cell = f.cells
			f.cells++
		}
	}
	r.current = f.parent
}

// declare thêm một binding vào tầng trong cùng của hàm đang phân giải.
func (r *resolver) declare(at syntax.Pos, name string, constant bool) *binding {
	f := r.current
	scope := f.blocks[len(f.blocks)-1]
	if previous, found := scope[name]; found {
		r.fail(at, "%s is declared twice: first at %s", name, previous.declared)
	}
	b := &binding{name: name, kind: localBinding, constant: constant, declared: at, owner: f, slot: f.locals}
	f.locals++
	f.bindings = append(f.bindings, b)
	scope[name] = b
	return b
}

// hidden là một slot của hàm đang phân giải mà chương trình không gọi tên được: danh sách và chỉ số của for…of, giá
// trị cũ của a.b++. Hai lời gọi liền nhau cho hai slot liền nhau.
func (r *resolver) hidden() *binding {
	f := r.current
	b := &binding{name: "#", kind: localBinding, owner: f, slot: f.locals, initialized: true}
	f.locals++
	return b
}

// declarePattern khai mọi tên của pattern ở tầng trong cùng, theo thứ tự Names().
func (r *resolver) declarePattern(pattern *syntax.ObjectPattern, constant bool) []*binding {
	var list []*binding
	for _, p := range pattern.Names() {
		list = append(list, r.declare(p.Pos, p.Name, constant))
	}
	return list
}

// pattern phân giải giá trị mặc định của pattern theo thứ tự viết, và đánh dấu mỗi tên đã khai khi tới nó: mặc định của
// một khoá đọc được các tên trước nó, như JavaScript.
func (r *resolver) pattern(pattern *syntax.ObjectPattern, bindings []*binding, next *int) {
	for _, p := range pattern.Properties {
		if p.Default != nil {
			r.expression(p.Default)
		}
		if p.Pattern != nil {
			r.pattern(p.Pattern, bindings, next)
			continue
		}
		bindings[*next].initialized = true
		*next++
	}
}

// assignable là binding của một tên được gán (=, +=, ++): const, import, tên toàn cục là lỗi.
func (r *resolver) assignable(target *syntax.Identifier) {
	b := r.lookup(target.Pos, target.Name)
	if b == nil || b.constant {
		what := "a const"
		switch {
		case b == nil:
			what = "a value of the language"
		case b.kind == constantBinding:
			what = "an import or a global name"
		}
		r.fail(target.Pos, "cannot assign to %s: it is %s", target.Name, what)
	}
	r.names[target] = b
}

// lookup tìm binding của name từ tầng trong cùng ra ngoài, qua các hàm ngoài, rồi các tên toàn cục.
func (r *resolver) lookup(at syntax.Pos, name string) *binding {
	for f := r.current; f != nil; f = f.parent {
		for i := len(f.blocks) - 1; i >= 0; i-- {
			if b, found := f.blocks[i][name]; found {
				if b.kind == localBinding && b.owner == r.current && !b.initialized {
					r.fail(at, "%s is used before its declaration at %s", name, b.declared)
				}
				if b.kind == localBinding && b.owner != r.current {
					b.captured = true
					r.captureOf(r.current, b)
				}
				return b
			}
		}
	}
	switch name {
	case "NaN", "Infinity":
		return nil // literal, compiler.go
	}
	if v, found := r.globals(name); found {
		return &binding{name: name, kind: constantBinding, constant: true, value: v}
	}
	r.fail(at, "%s is not defined: declare it with const or let, or import it", name)
	return nil
}

// captureOf thêm b vào các ô f bắt được, và vào của mọi hàm giữa f và hàm giữ b.
func (r *resolver) captureOf(f *function, b *binding) int {
	if i, found := f.capture[b]; found {
		return i
	}
	if f.parent != b.owner {
		r.captureOf(f.parent, b)
	}
	f.capture[b] = len(f.captures)
	f.captures = append(f.captures, b)
	return f.capture[b]
}

// block phân giải một dãy câu trong một tầng tên mới: nâng let và const lên đầu trước, rồi đi từng câu.
func (r *resolver) block(owner *syntax.Block, body []syntax.Statement) {
	f := r.current
	f.blocks = append(f.blocks, map[string]*binding{})
	for _, s := range body {
		d, ok := s.(*syntax.Declaration)
		if !ok {
			continue
		}
		var bindings []*binding
		if d.Pattern != nil {
			bindings = r.declarePattern(d.Pattern, d.Const)
			r.patterns[d] = bindings
		} else {
			b := r.declare(d.Pos, d.Name, d.Const)
			r.declared[d] = b
			bindings = []*binding{b}
		}
		if owner != nil {
			f.lifted[owner] = append(f.lifted[owner], bindings...)
		}
	}
	for _, s := range body {
		r.statement(s)
	}
	f.blocks = f.blocks[:len(f.blocks)-1]
}

func (r *resolver) statement(s syntax.Statement) {
	switch s := s.(type) {
	case *syntax.Declaration:
		if s.Init != nil {
			r.expression(s.Init)
		}
		if s.Pattern != nil {
			next := 0
			r.pattern(s.Pattern, r.patterns[s], &next)
		} else {
			r.declared[s].initialized = true
		}
	case *syntax.For:
		f := r.current
		f.blocks = append(f.blocks, map[string]*binding{})
		counter := r.declare(s.Init.Pos, s.Init.Name, false)
		r.expression(s.Init.Init)
		counter.initialized = true
		r.loopBindings[s] = []*binding{counter}
		r.expression(s.Test)
		r.expression(s.Update)
		r.single(s.Body)
		f.blocks = f.blocks[:len(f.blocks)-1]
	case *syntax.ForOf:
		r.expression(s.Iterable)
		f := r.current
		f.blocks = append(f.blocks, map[string]*binding{})
		list, index := r.hidden(), r.hidden()
		var names []*binding
		if s.Pattern != nil {
			names = r.declarePattern(s.Pattern, true)
			next := 0
			r.pattern(s.Pattern, names, &next)
		} else {
			b := r.declare(s.Pos, s.Name, true)
			b.initialized = true
			names = []*binding{b}
		}
		r.loopBindings[s] = append([]*binding{list, index}, names...)
		r.single(s.Body)
		f.blocks = f.blocks[:len(f.blocks)-1]
	case *syntax.ExpressionStatement:
		r.expression(s.Value)
	case *syntax.Return:
		if s.Value != nil {
			r.expression(s.Value)
		}
	case *syntax.If:
		r.expression(s.Condition)
		r.single(s.Then)
		if s.Else != nil {
			r.single(s.Else)
		}
	case *syntax.Block:
		r.block(s, s.Body)
	}
}

// single là câu sau if, else: một block có tầng của nó, hay một câu không khai báo (parser đã từ chối).
func (r *resolver) single(s syntax.Statement) {
	r.statement(s)
}

func (r *resolver) expression(e syntax.Expression) {
	switch e := e.(type) {
	case *syntax.Identifier:
		if b := r.lookup(e.Pos, e.Name); b != nil {
			r.names[e] = b
		}
	case *syntax.Literal:
	case *syntax.Template:
		for _, v := range e.Values {
			r.expression(v)
		}
	case *syntax.Array:
		for _, item := range e.Items {
			r.expression(item)
		}
	case *syntax.Object:
		for _, p := range e.Properties {
			r.expression(p.Value)
		}
	case *syntax.Member:
		r.expression(e.Object)
	case *syntax.Index:
		r.expression(e.Object)
		r.expression(e.Index)
	case *syntax.Call:
		r.expression(e.Callee)
		for _, a := range e.Arguments {
			r.expression(a)
		}
	case *syntax.Arrow:
		r.arrow(e)
	case *syntax.Unary:
		r.expression(e.Operand)
	case *syntax.Binary:
		r.expression(e.Left)
		r.expression(e.Right)
	case *syntax.Logical:
		r.expression(e.Left)
		r.expression(e.Right)
	case *syntax.Conditional:
		r.expression(e.Condition)
		r.expression(e.Then)
		r.expression(e.Otherwise)
	case *syntax.Assign:
		r.expression(e.Value)
		switch target := e.Target.(type) {
		case *syntax.Identifier:
			r.assignable(target)
		default:
			r.expression(e.Target)
		}
	case *syntax.Update:
		switch target := e.Target.(type) {
		case *syntax.Identifier:
			r.assignable(target)
		default:
			r.expression(e.Target)
			if !e.Prefix {
				r.temps[e] = r.hidden()
			}
		}
	case *syntax.Spread:
		r.expression(e.Value)
	}
}

func (r *resolver) arrow(e *syntax.Arrow) {
	f := r.enterFunction(e)
	f.blocks = append(f.blocks, map[string]*binding{})
	var params []*binding
	for _, p := range e.Parameters {
		b := r.declare(p.Pos, p.Name, false)
		params = append(params, b)
	}
	for i, p := range e.Parameters {
		if p.Default != nil {
			r.expression(p.Default)
		}
		params[i].initialized = true
	}
	r.params[e] = params
	switch body := e.Body.(type) {
	case *syntax.Block:
		// thân là tầng của tham số: let x trùng tên một tham số là khai hai lần, như JavaScript
		for _, s := range body.Body {
			if d, ok := s.(*syntax.Declaration); ok {
				for _, p := range params {
					if p.name == d.Name {
						r.fail(d.Pos, "%s is declared twice: first as a parameter at %s", d.Name, p.declared)
					}
				}
			}
		}
		r.block(body, body.Body)
	case syntax.Expression:
		r.expression(body)
	}
	f.blocks = f.blocks[:len(f.blocks)-1]
	r.leaveFunction()
}
