package host

import (
	"fmt"

	"github.com/huynhnhanquoc/engine/core/syntax"
)

// Phép kiểm môi trường trên cây cú pháp, trước khi biên dịch (luật của docs/KITWORK_ENVIRONMENT.md mục 3):
//   - environment(…) chỉ ở cấp cao nhất của file, dạng const tên = environment("…", …), mỗi đường dẫn là chữ viết sẵn:
//     boot đọc các file trước khi chạy dòng nào;
//   - kết quả chỉ đọc theo tên: env.KEY, env.require("KEY"); ghi, env[…], chuyền nguyên env đi là lỗi, để không đoạn
//     mã nào lấy được cả bộ;
//   - env.require nhận tên viết sẵn: boot kiểm mọi khoá bắt buộc một lần, kể cả trong hàm chỉ chạy lúc request.
// Một tham số hay khai báo cùng tên bên trong một hàm che tên ấy, như JavaScript.

// environmentCall là một const tên = environment(…) ở cấp cao nhất.
type environmentCall struct {
	name  string
	paths []string
	at    syntax.Pos
}

// environmentKey là một env.KEY hay env.require("KEY") trong file.
type environmentKey struct {
	binding, key string
	at           syntax.Pos
}

type environmentCheck struct {
	file      string
	functions map[string]bool // tên cục bộ của environment, import từ "application"
	libraries map[string]bool // tên của import * as … from "application"
	calls     []*environmentCall
	bindings  map[string]*environmentCall
	allowed   map[*syntax.Call]bool
	shadow    map[string]int
	requires  []environmentKey
	reads     []environmentKey
}

func (c *environmentCheck) fail(at syntax.Pos, format string, arguments ...any) {
	panic(failure{&syntax.Error{File: c.file, Pos: at, Message: fmt.Sprintf(format, arguments...)}})
}

type failure struct{ err *syntax.Error }

// checkEnvironment kiểm program và trả các environment(…) cùng các khoá được đọc.
func checkEnvironment(program *syntax.Program) (c *environmentCheck, err error) {
	c = &environmentCheck{
		file: program.File, functions: map[string]bool{}, libraries: map[string]bool{},
		bindings: map[string]*environmentCall{}, allowed: map[*syntax.Call]bool{}, shadow: map[string]int{},
	}
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			c, err = nil, f.err
		}
	}()
	for _, imp := range program.Imports {
		if imp.From != "application" {
			continue
		}
		if imp.Namespace != "" {
			c.libraries[imp.Namespace] = true
		}
		for _, name := range imp.Names {
			if name.Name == "environment" {
				c.functions[name.Local] = true
			}
		}
	}
	for _, s := range program.Body {
		d, ok := s.(*syntax.Declaration)
		if !ok {
			continue
		}
		call, ok := d.Init.(*syntax.Call)
		if !ok || !c.isEnvironment(call.Callee) {
			continue
		}
		if !d.Const {
			c.fail(d.Pos, "write const %s = environment(…): what it reads never changes", d.Name)
		}
		if len(call.Arguments) == 0 {
			c.fail(call.Pos, `environment(…) takes the files it reads: environment("./.env")`)
		}
		ec := &environmentCall{name: d.Name, at: d.Pos}
		for _, a := range call.Arguments {
			l, ok := a.(*syntax.Literal)
			if !ok || l.Kind != syntax.StringLiteral {
				c.fail(a.At(), `environment(…) takes each file as text written in place: environment("./.env")`)
			}
			ec.paths = append(ec.paths, l.Text)
		}
		c.calls = append(c.calls, ec)
		c.bindings[d.Name] = ec
		c.allowed[call] = true
	}
	for _, s := range program.Body {
		c.statement(s)
	}
	return c, nil
}

// isEnvironment là e chỉ vào environment của "application": tên import, hay app.environment.
func (c *environmentCheck) isEnvironment(e syntax.Expression) bool {
	switch e := e.(type) {
	case *syntax.Identifier:
		return c.functions[e.Name] && c.shadow[e.Name] == 0
	case *syntax.Member:
		object, ok := e.Object.(*syntax.Identifier)
		return ok && e.Name == "environment" && c.libraries[object.Name] && c.shadow[object.Name] == 0
	}
	return false
}

// binding là tên e chỉ vào một kết quả của environment(…) (không bị che).
func (c *environmentCheck) binding(e syntax.Expression) (string, bool) {
	id, ok := e.(*syntax.Identifier)
	if !ok || c.shadow[id.Name] > 0 {
		return "", false
	}
	_, found := c.bindings[id.Name]
	return id.Name, found
}

func (c *environmentCheck) statement(s syntax.Statement) {
	switch s := s.(type) {
	case *syntax.Declaration:
		if s.Init != nil {
			c.expression(s.Init)
		}
		if s.Pattern != nil {
			c.defaults(s.Pattern)
		}
	case *syntax.For:
		c.expression(s.Init.Init)
		c.shadow[s.Init.Name]++
		c.expression(s.Test)
		c.expression(s.Update)
		c.statement(s.Body)
		c.shadow[s.Init.Name]--
	case *syntax.ForOf:
		c.expression(s.Iterable)
		names := []string{s.Name}
		if s.Pattern != nil {
			c.defaults(s.Pattern)
			names = nil
			for _, n := range s.Pattern.Names() {
				names = append(names, n.Name)
			}
		}
		for _, name := range names {
			c.shadow[name]++
		}
		c.statement(s.Body)
		for _, name := range names {
			c.shadow[name]--
		}
	case *syntax.ExpressionStatement:
		c.expression(s.Value)
	case *syntax.Return:
		if s.Value != nil {
			c.expression(s.Value)
		}
	case *syntax.If:
		c.expression(s.Condition)
		c.statement(s.Then)
		if s.Else != nil {
			c.statement(s.Else)
		}
	case *syntax.Block:
		c.block(s.Body, nil)
	}
}

// block đi một khối bên trong một hàm: tên nó khai che tên ngoài, như JavaScript.
func (c *environmentCheck) block(body []syntax.Statement, parameters []syntax.Parameter) {
	var hidden []string
	for _, p := range parameters {
		hidden = append(hidden, p.Name)
	}
	for _, s := range body {
		if d, ok := s.(*syntax.Declaration); ok {
			if d.Pattern != nil {
				for _, n := range d.Pattern.Names() {
					hidden = append(hidden, n.Name)
				}
				continue
			}
			hidden = append(hidden, d.Name)
		}
	}
	for _, name := range hidden {
		c.shadow[name]++
	}
	for _, p := range parameters {
		if p.Default != nil {
			c.expression(p.Default)
		}
	}
	for _, s := range body {
		c.statement(s)
	}
	for _, name := range hidden {
		c.shadow[name]--
	}
}

func (c *environmentCheck) expression(e syntax.Expression) {
	switch e := e.(type) {
	case *syntax.Identifier:
		if name, ok := c.binding(e); ok {
			c.fail(e.Pos, `%s is read by name only: write %s.KEY or %s.require("KEY"), never %s itself`, name, name, name, name)
		}
		if c.isEnvironment(e) {
			c.fail(e.Pos, `environment(…) is written once at the top of the file: const env = environment("./.env")`)
		}
	case *syntax.Member:
		if name, ok := c.binding(e.Object); ok {
			c.reads = append(c.reads, environmentKey{binding: name, key: e.Name, at: e.Pos})
			return
		}
		if c.isEnvironment(e) {
			c.fail(e.Pos, `environment(…) is written once at the top of the file: const env = environment("./.env")`)
		}
		c.expression(e.Object)
	case *syntax.Index:
		if name, ok := c.binding(e.Object); ok {
			c.fail(e.Pos, `%s is read by name only: write %s.KEY, not %s[…]`, name, name, name)
		}
		c.expression(e.Object)
		c.expression(e.Index)
	case *syntax.Call:
		if member, ok := e.Callee.(*syntax.Member); ok {
			if name, ok := c.binding(member.Object); ok && member.Name == "require" {
				l, literal := firstLiteral(e.Arguments)
				if !literal || len(e.Arguments) != 1 {
					c.fail(e.Pos, `%s.require(…) takes the key's name written in place: %s.require("STRIPE_SECRET")`, name, name)
				}
				c.requires = append(c.requires, environmentKey{binding: name, key: l, at: member.Pos})
				return
			}
		}
		if c.isEnvironment(e.Callee) {
			if !c.allowed[e] {
				c.fail(e.Callee.At(), `environment(…) is written once at the top of the file: const env = environment("./.env")`)
			}
			return
		}
		c.expression(e.Callee)
		for _, a := range e.Arguments {
			c.expression(a)
		}
	case *syntax.Assign:
		switch target := e.Target.(type) {
		case *syntax.Member:
			if name, ok := c.binding(target.Object); ok {
				c.fail(target.Pos, "%s is read only: the environment is read once, at boot", name)
			}
			c.expression(target.Object)
		case *syntax.Index:
			if name, ok := c.binding(target.Object); ok {
				c.fail(target.Pos, "%s is read only: the environment is read once, at boot", name)
			}
			c.expression(target.Object)
			c.expression(target.Index)
		case *syntax.Identifier:
			c.expression(target)
		}
		c.expression(e.Value)
	case *syntax.Arrow:
		switch body := e.Body.(type) {
		case *syntax.Block:
			c.block(body.Body, e.Parameters)
		case syntax.Expression:
			for _, p := range e.Parameters {
				c.shadow[p.Name]++
			}
			for _, p := range e.Parameters {
				if p.Default != nil {
					c.expression(p.Default)
				}
			}
			c.expression(body)
			for _, p := range e.Parameters {
				c.shadow[p.Name]--
			}
		}
	case *syntax.Update:
		switch target := e.Target.(type) {
		case *syntax.Member:
			if name, ok := c.binding(target.Object); ok {
				c.fail(target.Pos, "%s is read only: the environment is read once, at boot", name)
			}
		case *syntax.Index:
			if name, ok := c.binding(target.Object); ok {
				c.fail(target.Pos, "%s is read only: the environment is read once, at boot", name)
			}
		}
		c.expression(e.Target)
	case *syntax.Spread:
		c.expression(e.Value)
	case *syntax.Template:
		for _, v := range e.Values {
			c.expression(v)
		}
	case *syntax.Array:
		for _, item := range e.Items {
			c.expression(item)
		}
	case *syntax.Object:
		for _, p := range e.Properties {
			c.expression(p.Value)
		}
	case *syntax.Unary:
		c.expression(e.Operand)
	case *syntax.Binary:
		c.expression(e.Left)
		c.expression(e.Right)
	case *syntax.Logical:
		c.expression(e.Left)
		c.expression(e.Right)
	case *syntax.Conditional:
		c.expression(e.Condition)
		c.expression(e.Then)
		c.expression(e.Otherwise)
	}
}

// defaults đi các giá trị mặc định của một pattern (env.PORT viết được ở đó).
func (c *environmentCheck) defaults(pattern *syntax.ObjectPattern) {
	for _, p := range pattern.Properties {
		if p.Default != nil {
			c.expression(p.Default)
		}
		if p.Pattern != nil {
			c.defaults(p.Pattern)
		}
	}
}

func firstLiteral(arguments []syntax.Expression) (string, bool) {
	if len(arguments) == 0 {
		return "", false
	}
	l, ok := arguments[0].(*syntax.Literal)
	if !ok || l.Kind != syntax.StringLiteral {
		return "", false
	}
	return l.Text, true
}
