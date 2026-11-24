package compiler

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Options là những gì một chương trình được thấy ngoài chính nó.
type Options struct {
	// Modules là các module import được: "application" → { website, application, database }. Module hay tên không
	// có là lỗi biên dịch nêu những gì có.
	Modules map[string]map[string]value.Value
	// Globals là các tên toàn cục của nơi chạy, trước của JavaScript (core/javascript.Global): một console có đầu dòng
	// của site, chẳng hạn. nil là chỉ các tên của JavaScript.
	Globals map[string]value.Value
	// Module là file được một file khác import: các export const của nó là kết quả (một object), return ở cấp file là
	// lỗi. Không phải module (application.work) thì export là lỗi: file ấy trả bản thiết kế, không export.
	Module bool
}

// Compile biên dịch program thành Function của thân file. Lỗi là *syntax.Error, có file, dòng và cột.
func Compile(program *syntax.Program, options Options) (main *Function, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			main, err = nil, f.err
		}
	}()
	r := &resolver{
		resolution: &resolution{
			file:         program.File,
			names:        map[*syntax.Identifier]*binding{},
			declared:     map[*syntax.Declaration]*binding{},
			patterns:     map[*syntax.Declaration][]*binding{},
			loopBindings: map[syntax.Statement][]*binding{},
			temps:        map[*syntax.Update]*binding{},
			params:       map[*syntax.Arrow][]*binding{},
			functions:    map[syntax.Node]*function{},
		},
		globals: func(name string) (value.Value, bool) {
			if v, found := options.Globals[name]; found {
				return v, true
			}
			return javascript.Global(name)
		},
	}
	top := &syntax.Block{Body: program.Body}
	r.program = r.enterFunction(program)
	imports := map[string]*binding{}
	for _, imp := range program.Imports {
		members, found := options.Modules[imp.From]
		if !found {
			names := make([]string, 0, len(options.Modules))
			for name := range options.Modules {
				names = append(names, `"`+name+`"`)
			}
			slices.Sort(names)
			if strings.HasPrefix(imp.From, "kitwork/") {
				r.fail(imp.Pos, "there is no module %q: libraries are imported by their name, without \"kitwork/\": %s", imp.From, strings.Join(names, ", "))
			}
			r.fail(imp.Pos, "there is no module %q: %s", imp.From, strings.Join(names, ", "))
		}
		add := func(at syntax.Pos, local string, v value.Value) {
			if previous, found := imports[local]; found {
				r.fail(at, "%s is imported twice: first at %s", local, previous.declared)
			}
			imports[local] = &binding{name: local, kind: constantBinding, constant: true, declared: at, value: v}
		}
		if imp.Namespace != "" {
			object := value.Object()
			for _, key := range sortedKeys(members) {
				object.Set(key, members[key])
			}
			add(imp.Pos, imp.Namespace, object.Freeze())
			continue
		}
		for _, name := range imp.Names {
			v, found := members[name.Name]
			if !found {
				r.fail(name.Pos, "the module %q has no %s: %s", imp.From, name.Name, strings.Join(sortedKeys(members), ", "))
			}
			add(name.Pos, name.Local, v)
		}
	}
	for _, s := range program.Body {
		switch s := s.(type) {
		case *syntax.Declaration:
			names := []syntax.PatternProperty{{Pos: s.Pos, Name: s.Name}}
			if s.Pattern != nil {
				names = s.Pattern.Names()
			}
			for _, n := range names {
				if previous, found := imports[n.Name]; found {
					r.fail(n.Pos, "%s is declared twice: first imported at %s", n.Name, previous.declared)
				}
			}
			if s.Export && !options.Module {
				r.fail(s.Pos, "this file exports nothing: it is the program, it returns its blueprint")
			}
		case *syntax.Return:
			if options.Module {
				r.fail(s.Pos, "a file another file imports returns nothing: write export const x = …")
			}
		}
	}
	r.program.blocks = append(r.program.blocks, imports)
	r.block(top, program.Body)
	r.program.blocks = nil
	r.leaveFunction()

	c := &compiler{resolution: r.resolution}
	main = c.function(program, "", nil, func(f *Function) {
		c.lift(top)
		for _, s := range program.Body {
			c.statement(s)
		}
		if options.Module {
			c.exports(program)
		}
	})
	main.File = program.File
	return main, nil
}

func sortedKeys(m map[string]value.Value) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// compiler sinh mã cho từng hàm, đọc kết quả của lượt phân giải.
type compiler struct {
	*resolution
	out   *Function
	scope *function
	at    syntax.Pos
	loops []*loop // vòng lặp quanh chỗ đang sinh mã, trong hàm đang sinh mã
}

// loop là các lệnh nhảy của break, continue trong một vòng lặp, chờ đích của chúng.
type loop struct{ breaks, continues []int }

// exports là kết quả của một module: object các export const của nó, theo thứ tự viết.
func (c *compiler) exports(program *syntax.Program) {
	count := 0
	for _, s := range program.Body {
		d, ok := s.(*syntax.Declaration)
		if !ok || !d.Export {
			continue
		}
		if d.Pattern == nil {
			c.push(value.String(d.Name))
			c.load(c.declared[d])
			count++
			continue
		}
		for i, n := range d.Pattern.Names() {
			c.push(value.String(n.Name))
			c.load(c.patterns[d][i])
			count++
		}
	}
	c.emit(MAKE, count, Object)
	c.emit(RETURN, 0, 0)
}

// function sinh một Function cho node (thân file hay một hàm mũi tên): body viết các lệnh.
func (c *compiler) function(node syntax.Node, name string, parameters []*binding, body func(*Function)) *Function {
	outer, outerScope := c.out, c.scope
	scope := c.functions[node]
	f := &Function{Name: name, File: c.file, Pos: node.At(), Parameters: len(parameters), Locals: scope.locals, Cells: scope.cells}
	for _, b := range scope.captures {
		if b.owner == scope.parent {
			f.Captures = append(f.Captures, Capture{FromCell: true, Index: b.cell, Name: b.name})
		} else {
			f.Captures = append(f.Captures, Capture{Index: scope.parent.capture[b], Name: b.name})
		}
	}
	if arrow, ok := node.(*syntax.Arrow); ok {
		f.Syntax = arrow
	}
	outerLoops := c.loops
	c.out, c.scope, c.loops = f, scope, nil
	body(f)
	c.loops = outerLoops
	c.undefined()
	c.emit(RETURN, 0, 0)
	c.out, c.scope = outer, outerScope
	return f
}

func (c *compiler) emit(op Opcode, a, b int) int {
	c.out.Code = append(c.out.Code, Instruction{Op: op, A: int32(a), B: int32(b)})
	c.out.Positions = append(c.out.Positions, c.at)
	return len(c.out.Code) - 1
}

// patch đặt đích của lệnh nhảy ở jump là lệnh kế tiếp.
func (c *compiler) patch(jump int) { c.out.Code[jump].A = int32(len(c.out.Code)) }

// constant là chỉ số của v trong Constants: một giá trị giống hệt đã có thì dùng lại.
func (c *compiler) constant(v value.Value) int {
	for i, k := range c.out.Constants {
		if k.Identical(v) {
			return i
		}
	}
	c.out.Constants = append(c.out.Constants, v)
	return len(c.out.Constants) - 1
}

// push đẩy một hằng; undefined đẩy undefined.
func (c *compiler) push(v value.Value) { c.emit(PUSH, c.constant(v), 0) }
func (c *compiler) undefined()         { c.emit(PUSH, 0, Undefined) }

// lift tạo ô cho các binding bị bắt mà block khai: trước câu đầu của block, để một closure tạo trước khai báo vẫn
// thấy đúng ô. Đọc ô trước khi khai báo của nó chạy là undefined (JavaScript ném; lát sau).
func (c *compiler) lift(block *syntax.Block) {
	for _, b := range c.scope.lifted[block] {
		if b.captured {
			c.undefined()
			c.emit(STORE, b.cell, FreshCell)
		}
	}
}

// load đẩy giá trị của binding b: LOAD và nơi của nó, hay PUSH một hằng (import, tên toàn cục).
func (c *compiler) load(b *binding) {
	switch {
	case b.kind == constantBinding:
		c.push(b.value)
	case b.owner != c.scope:
		c.emit(LOAD, c.scope.capture[b], Captured)
	case b.captured:
		c.emit(LOAD, b.cell, Cell)
	default:
		c.emit(LOAD, b.slot, Slot)
	}
}

// store lấy đỉnh ghi vào binding b.
func (c *compiler) store(b *binding) {
	switch {
	case b.owner != c.scope:
		c.emit(STORE, c.scope.capture[b], Captured)
	case b.captured:
		c.emit(STORE, b.cell, Cell)
	default:
		c.emit(STORE, b.slot, Slot)
	}
}

func (c *compiler) statement(s syntax.Statement) {
	c.at = s.At()
	switch s := s.(type) {
	case *syntax.Declaration:
		if s.Pattern != nil {
			c.expression(s.Init)
			next := 0
			c.destructure(s.Pattern, c.patterns[s], &next, false)
			c.emit(POP, 0, 0)
			return
		}
		if s.Init != nil {
			c.expressionNamed(s.Init, s.Name)
		} else {
			c.undefined()
		}
		c.at = s.Pos
		c.store(c.declared[s])
	case *syntax.For:
		counter := c.loopBindings[s][0]
		c.expression(s.Init.Init)
		c.at = s.Pos
		c.declare(counter)
		start := len(c.out.Code)
		c.expression(s.Test)
		exit := c.emit(FALSE, 0, Pop)
		l := c.enter()
		c.statement(s.Body)
		for _, j := range l.continues {
			c.patch(j)
		}
		if counter.captured {
			// như JavaScript: mỗi vòng một bản của biến đếm, chép trước khi tăng, để closure của vòng trước giữ giá trị của nó
			c.emit(LOAD, counter.cell, Cell)
			c.emit(STORE, counter.cell, FreshCell)
		}
		c.expression(s.Update)
		c.emit(POP, 0, 0)
		c.emit(JUMP, start, 0)
		c.leave(l, exit)
	case *syntax.ForOf:
		names := c.loopBindings[s]
		list := names[0]
		c.expression(s.Iterable)
		c.at = s.Pos
		c.emit(STORE, list.slot, Slot)
		c.push(value.Number(0))
		c.emit(STORE, names[1].slot, Slot)
		start := c.emit(ITER, 0, list.slot)
		if s.Pattern != nil {
			next := 0
			c.destructure(s.Pattern, names[2:], &next, true)
			c.emit(POP, 0, 0)
		} else {
			c.declare(names[2])
		}
		l := c.enter()
		c.statement(s.Body)
		for _, j := range l.continues {
			c.out.Code[j].A = int32(start)
		}
		c.emit(JUMP, start, 0)
		c.leave(l, start)
	case *syntax.Break:
		l := c.loops[len(c.loops)-1]
		l.breaks = append(l.breaks, c.emit(JUMP, 0, 0))
	case *syntax.Continue:
		l := c.loops[len(c.loops)-1]
		l.continues = append(l.continues, c.emit(JUMP, 0, 0))
	case *syntax.ExpressionStatement:
		c.expression(s.Value)
		c.emit(POP, 0, 0)
	case *syntax.Return:
		if s.Value != nil {
			c.expression(s.Value)
		} else {
			c.undefined()
		}
		c.at = s.Pos
		c.emit(RETURN, 0, 0)
	case *syntax.If:
		c.expression(s.Condition)
		c.at = s.Pos
		otherwise := c.emit(FALSE, 0, 0)
		c.statement(s.Then)
		if s.Else == nil {
			c.patch(otherwise)
			return
		}
		end := c.emit(JUMP, 0, 0)
		c.patch(otherwise)
		c.statement(s.Else)
		c.patch(end)
	case *syntax.Block:
		c.lift(s)
		for _, inner := range s.Body {
			c.statement(inner)
		}
	}
}

// enter mở một vòng lặp cho break, continue; leave đặt đích của chúng: break và lệnh thoát exit nhảy tới sau vòng lặp.
func (c *compiler) enter() *loop {
	l := &loop{}
	c.loops = append(c.loops, l)
	return l
}

func (c *compiler) leave(l *loop, exit int) {
	c.loops = c.loops[:len(c.loops)-1]
	c.patch(exit)
	for _, j := range l.breaks {
		c.patch(j)
	}
}

// declare lấy đỉnh ghi vào một binding vừa khai: biến bị bắt nhận một ô mới, để mỗi vòng của một vòng lặp có ô riêng.
func (c *compiler) declare(b *binding) {
	if b.captured {
		c.emit(STORE, b.cell, FreshCell)
		return
	}
	c.store(b)
}

// destructure đọc pattern từ object trên đỉnh (để nó lại): mỗi khoá GET, undefined thì giá trị mặc định, rồi ghi vào
// tên của nó hay đọc tiếp một pattern lồng. fresh: các tên là của một vòng for…of, mỗi vòng một ô.
func (c *compiler) destructure(pattern *syntax.ObjectPattern, bindings []*binding, next *int, fresh bool) {
	for _, p := range pattern.Properties {
		c.at = p.Pos
		c.emit(DUP, 1, 0)
		c.emit(GET, c.constant(value.String(p.Key)), Named)
		if p.Default != nil {
			c.emit(DUP, 1, 0)
			c.undefined()
			c.emit(COMPARE, Same, 0)
			keep := c.emit(FALSE, 0, Pop)
			c.emit(POP, 0, 0)
			c.expression(p.Default)
			c.patch(keep)
		}
		if p.Pattern != nil {
			c.destructure(p.Pattern, bindings, next, fresh)
			c.emit(POP, 0, 0)
			continue
		}
		b := bindings[*next]
		*next++
		if fresh {
			c.declare(b)
		} else {
			c.store(b)
		}
	}
}

// expressionNamed là expression, đặt tên cho một hàm mũi tên được khai thẳng (const f = (…) => …), như JavaScript.
// plainObject là một object của các khoá viết thẳng. Khoá không trùng nhau (gần như luôn vậy): các khoá là một
// value.Shape trong hằng, dựng một lần, mọi object literal này làm ra mượn chung (MAKE Shaped), chỉ các giá trị lên ngăn
// xếp. Một khoá viết hai lần ({ a: 1, a: 2 }: giá trị sau, chỗ của lần đầu) đi đường cặp khoá, giá trị (MAKE Object).
func (c *compiler) plainObject(properties []syntax.Property, at syntax.Pos) {
	if keys := c.shapeValues(properties); keys != nil {
		c.at = at
		c.emit(MAKE, c.constant(value.Struct(value.NewShape(keys))), Shaped)
		return
	}
	for _, p := range properties {
		c.push(value.String(p.Key))
		c.expressionNamed(p.Value, p.Key)
	}
	c.at = at
	c.emit(MAKE, len(properties), Object)
}

// shapeValues viết các giá trị của properties lên ngăn xếp theo thứ tự và trả các khoá của chúng, cho một Shape; nil,
// không viết gì, khi không có khoá nào hay có khoá trùng (khoá sau thắng: một object dựng từng cặp).
func (c *compiler) shapeValues(properties []syntax.Property) []string {
	keys := make([]string, len(properties))
	for i, p := range properties {
		keys[i] = p.Key
		if slices.Contains(keys[:i], p.Key) {
			return nil
		}
	}
	if len(properties) == 0 {
		return nil
	}
	for i, p := range properties {
		c.expressionNamed(p.Value, p.Key)
		// khoá là chính chuỗi của hằng cùng chữ, như tên của một lệnh GET (o.name): hai chuỗi cùng chỗ thì so bằng nhau
		// ngay, không so từng byte; khoá lấy từ cây cú pháp thì mỗi lần đọc thuộc tính so từng byte (guard chậm 4%)
		keys[i] = c.out.Constants[c.constant(value.String(p.Key))].String()
	}
	return keys
}

// object viết một object literal. Không có ...: một lệnh MAKE (plainObject). Có: object dựng dần theo thứ tự viết,
// như JavaScript: đoạn đầu là các khoá viết thẳng (plainObject), hay một ...o (MAKE Copy: bản chép mượn khoá của o, chỗ
// cho mọi khoá viết thẳng sau nó); mỗi ...o sau đó chép vào ngay khi gặp (MAKE Merge), mỗi đoạn khoá viết thẳng ghi
// vào (extend). Nên một giá trị viết sau không đổi được cái một ...o trước nó đã chép: { ...o, a: f() } chép o trước
// khi f chạy, như JavaScript (trước 24/11/2026: mọi đoạn tính xong mới gộp, và một object tạm cho mỗi đoạn khoá).
func (c *compiler) object(e *syntax.Object) {
	properties := e.Properties
	plain := plainRun(properties)
	if plain == len(properties) {
		c.plainObject(properties, e.Pos)
		return
	}
	if plain > 0 {
		c.plainObject(properties[:plain], e.Pos)
	} else {
		spread := properties[0].Value.(*syntax.Spread)
		c.expression(spread.Value)
		written := 0
		for _, p := range properties {
			if _, ok := p.Value.(*syntax.Spread); !ok {
				written++
			}
		}
		c.at = spread.Pos
		c.emit(MAKE, written, Copy)
		plain = 1
	}
	for properties = properties[plain:]; len(properties) > 0; properties = properties[plain:] {
		if spread, ok := properties[0].Value.(*syntax.Spread); ok {
			c.expression(spread.Value)
			c.at = spread.Pos
			c.emit(MAKE, 0, Merge)
			plain = 1
			continue
		}
		plain = plainRun(properties)
		c.extend(properties[:plain], e.Pos)
	}
}

// plainRun là số khoá viết thẳng ở đầu properties, tới ... đầu tiên.
func plainRun(properties []syntax.Property) int {
	for i, p := range properties {
		if _, ok := p.Value.(*syntax.Spread); ok {
			return i
		}
	}
	return len(properties)
}

// extend viết một đoạn khoá viết thẳng vào object đang dựng ở đỉnh ngăn xếp: các giá trị rồi MAKE Extend với Shape của
// các khoá, không object tạm. Khoá trùng trong đoạn: một object tạm (khoá sau thắng) rồi MAKE Merge.
func (c *compiler) extend(properties []syntax.Property, at syntax.Pos) {
	if keys := c.shapeValues(properties); keys != nil {
		c.at = at
		c.emit(MAKE, c.constant(value.Struct(value.NewShape(keys))), Extend)
		return
	}
	c.plainObject(properties, at)
	c.at = at
	c.emit(MAKE, 0, Merge)
}

func (c *compiler) expressionNamed(e syntax.Expression, name string) {
	if arrow, ok := e.(*syntax.Arrow); ok {
		c.arrow(arrow, name)
		return
	}
	c.expression(e)
}

var (
	arithmetic  = map[string]Opcode{"+": ADD, "-": SUB, "*": MUL, "/": DIV, "%": MOD, "**": POW}
	comparisons = map[string]int{"<": Less, ">": Greater, "<=": LessEqual, ">=": GreaterEqual, "===": Same, "!==": Different}
)

// operator viết phép hai ngôi op: một lệnh của phép số, hay COMPARE với loại so sánh làm toán hạng.
func (c *compiler) operator(op string) {
	if o, ok := arithmetic[op]; ok {
		c.emit(o, 0, 0)
		return
	}
	c.emit(COMPARE, comparisons[op], 0)
}

func (c *compiler) expression(e syntax.Expression) {
	c.at = e.At()
	switch e := e.(type) {
	case *syntax.Identifier:
		b := c.names[e]
		if b == nil { // NaN, Infinity
			n := math.NaN()
			if e.Name == "Infinity" {
				n = math.Inf(1)
			}
			c.push(value.Number(n))
			return
		}
		c.load(b)
	case *syntax.Literal:
		switch e.Kind {
		case syntax.UndefinedLiteral:
			c.undefined()
		case syntax.NullLiteral:
			c.push(value.Null())
		case syntax.BooleanLiteral:
			c.push(value.Boolean(e.Truth))
		case syntax.NumberLiteral:
			c.push(value.Number(e.Num))
		case syntax.StringLiteral:
			c.push(value.String(e.Text))
		}
	case *syntax.Template:
		count := 0
		for i, part := range e.Parts {
			if part != "" || len(e.Parts) == 1 {
				c.push(value.String(part))
				count++
			}
			if i < len(e.Values) {
				c.expression(e.Values[i])
				count++
			}
		}
		c.at = e.Pos
		c.emit(MAKE, count, Template)
	case *syntax.Array:
		// không có ...: một MAKE Array; có: các đoạn (mảng của phần tử viết thẳng, hay giá trị trải ra) nối bằng MAKE Join
		segments, plain := 0, 0
		flush := func() {
			if plain > 0 {
				c.emit(MAKE, plain, Array)
				segments, plain = segments+1, 0
			}
		}
		for _, item := range e.Items {
			if spread, ok := item.(*syntax.Spread); ok {
				flush()
				c.expression(spread.Value)
				segments++
				continue
			}
			c.expression(item)
			plain++
		}
		c.at = e.Pos
		if segments == 0 {
			c.emit(MAKE, plain, Array)
			return
		}
		flush()
		c.emit(MAKE, segments, Join)
	case *syntax.Object:
		c.object(e)
	case *syntax.Spread:
		c.fail(e.Pos, "... is written in an array or an object: [...a], { ...o }")
	case *syntax.Update:
		c.update(e)
	case *syntax.Member:
		c.expression(e.Object)
		c.at = e.Pos
		c.emit(GET, c.constant(value.String(e.Name)), Named)
	case *syntax.Index:
		c.expression(e.Object)
		c.expression(e.Index)
		c.at = e.Pos
		c.emit(GET, 0, Keyed)
	case *syntax.Call:
		if member, ok := e.Callee.(*syntax.Member); ok {
			c.expression(member.Object)
			for _, a := range e.Arguments {
				c.expression(a)
			}
			c.at = member.Pos
			c.emit(INVOKE, len(e.Arguments), c.constant(value.String(member.Name)))
			return
		}
		c.expression(e.Callee)
		for _, a := range e.Arguments {
			c.expression(a)
		}
		c.at = e.Callee.At() // lỗi của lời gọi chỉ vào hàm được gọi, như lỗi của một phương thức chỉ vào tên nó
		c.emit(CALL, len(e.Arguments), 0)
	case *syntax.Arrow:
		c.arrow(e, "")
	case *syntax.Unary:
		c.expression(e.Operand)
		c.at = e.Pos
		switch e.Op {
		case "!":
			c.emit(NOT, 0, 0)
		case "-":
			c.emit(UNARY, Negate, 0)
		case "+":
			c.emit(UNARY, Plus, 0)
		case "typeof":
			c.emit(UNARY, Typeof, 0)
		}
	case *syntax.Binary:
		c.expression(e.Left)
		c.expression(e.Right)
		c.at = e.Pos
		c.operator(e.Op)
	case *syntax.Logical:
		// đoản mạch: một lệnh nhảy giữ vế trái khi nó quyết định kết quả, bỏ nó khi phải tính vế phải
		c.expression(e.Left)
		c.at = e.Pos
		var end int
		switch e.Op {
		case "&&":
			end = c.emit(FALSE, 0, Keep)
		case "||":
			end = c.emit(TRUE, 0, Keep)
		case "??":
			end = c.emit(TRUE, 0, Present)
		}
		c.expression(e.Right)
		c.patch(end)
	case *syntax.Conditional:
		c.expression(e.Condition)
		c.at = e.Pos
		otherwise := c.emit(FALSE, 0, 0)
		c.expression(e.Then)
		end := c.emit(JUMP, 0, 0)
		c.patch(otherwise)
		c.expression(e.Otherwise)
		c.patch(end)
	case *syntax.Assign:
		c.assign(e)
	default:
		panic(fmt.Sprintf("compiler: no code for %T", e))
	}
}

// assign là a = v, a.b = v, a[k] = v, và các phép gộp (a += v): giá trị của phép gán ở lại trên ngăn xếp.
func (c *compiler) assign(e *syntax.Assign) {
	compound := e.Op != "="
	op := strings.TrimSuffix(e.Op, "=")
	switch target := e.Target.(type) {
	case *syntax.Identifier:
		b := c.names[target]
		if compound {
			c.load(b)
			c.expression(e.Value)
			c.at = e.Pos
			c.operator(op)
		} else {
			c.expressionNamed(e.Value, target.Name)
		}
		c.at = e.Pos
		c.emit(DUP, 1, 0)
		c.store(b)
	case *syntax.Member:
		c.expression(target.Object)
		name := c.constant(value.String(target.Name))
		if compound {
			c.emit(DUP, 1, 0)
			c.at = target.Pos
			c.emit(GET, name, Named)
			c.expression(e.Value)
			c.at = e.Pos
			c.operator(op)
		} else {
			c.expression(e.Value)
		}
		c.at = target.Pos
		c.emit(SET, name, Named)
	case *syntax.Index:
		c.expression(target.Object)
		c.expression(target.Index)
		if compound {
			c.emit(DUP, 2, 0)
			c.at = target.Pos
			c.emit(GET, 0, Keyed)
			c.expression(e.Value)
			c.at = e.Pos
			c.operator(op)
		} else {
			c.expression(e.Value)
		}
		c.at = target.Pos
		c.emit(SET, 0, Keyed)
	}
}

// update là a++, a--, ++a, --a: ToNumber của giá trị cũ, cộng hay trừ một, ghi lại; giá trị của biểu thức là số mới
// (dạng trước) hay số cũ (dạng sau), như JavaScript.
func (c *compiler) update(e *syntax.Update) {
	op := ADD
	if e.Op == "--" {
		op = SUB
	}
	step := func() {
		c.push(value.Number(1))
		c.emit(op, 0, 0)
	}
	switch target := e.Target.(type) {
	case *syntax.Identifier:
		b := c.names[target]
		c.load(b)
		c.at = e.Pos
		c.emit(UNARY, Plus, 0)
		if e.Prefix {
			step()
			c.emit(DUP, 1, 0)
		} else {
			c.emit(DUP, 1, 0)
			step()
		}
		c.store(b)
	case *syntax.Member, *syntax.Index:
		key, kind := 0, Keyed
		if member, ok := target.(*syntax.Member); ok {
			c.expression(member.Object)
			key, kind = c.constant(value.String(member.Name)), Named
			c.emit(DUP, 1, 0)
		} else {
			index := target.(*syntax.Index)
			c.expression(index.Object)
			c.expression(index.Index)
			c.emit(DUP, 2, 0)
		}
		c.at = e.Pos
		c.emit(GET, key, kind)
		c.emit(UNARY, Plus, 0)
		old := c.temps[e]
		if !e.Prefix {
			c.emit(DUP, 1, 0)
			c.emit(STORE, old.slot, Slot)
		}
		step()
		c.emit(SET, key, kind)
		if !e.Prefix {
			c.emit(POP, 0, 0)
			c.emit(LOAD, old.slot, Slot)
		}
	}
}

// fail là một lỗi biên dịch ở at.
func (c *compiler) fail(at syntax.Pos, format string, arguments ...any) {
	panic(failure{&syntax.Error{File: c.file, Pos: at, Message: fmt.Sprintf(format, arguments...)}})
}

// arrow sinh hàm con cho một hàm mũi tên rồi đẩy closure của nó (LAMBDA).
func (c *compiler) arrow(e *syntax.Arrow, name string) {
	params := c.params[e]
	child := c.function(e, name, params, func(f *Function) {
		// giá trị mặc định: tham số còn undefined thì tính biểu thức của nó
		for i, p := range e.Parameters {
			if p.Default == nil {
				continue
			}
			c.at = p.Pos
			c.emit(LOAD, params[i].slot, Slot)
			c.undefined()
			c.emit(COMPARE, Same, 0)
			skip := c.emit(FALSE, 0, 0)
			c.expression(p.Default)
			c.emit(STORE, params[i].slot, Slot)
			c.patch(skip)
		}
		// tham số bị bắt chuyển từ slot vào một ô mới của nó
		for _, b := range params {
			if b.captured {
				c.at = b.declared
				c.emit(LOAD, b.slot, Slot)
				c.emit(STORE, b.cell, FreshCell)
			}
		}
		switch body := e.Body.(type) {
		case *syntax.Block:
			c.lift(body)
			for _, s := range body.Body {
				c.statement(s)
			}
		case syntax.Expression:
			c.expression(body)
			c.emit(RETURN, 0, 0)
		}
	})
	c.out.Functions = append(c.out.Functions, child)
	c.at = e.Pos
	c.emit(LAMBDA, len(c.out.Functions)-1, 0)
}
