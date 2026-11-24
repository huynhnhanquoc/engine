package host

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Phép kiểm các cửa trên cây cú pháp của application.work, trước khi biên dịch (owner 24/11/2026,
// docs/KITWORK_APPLICATION_WORK.md mục 4). Cách viết ghi dần vào app, nên có ba chỗ một cửa có thể có mặt mà người đọc
// file không thấy; ba luật đóng ba chỗ ấy, và kiểm ở đây thì lỗi mang đúng dòng viết sai:
//
//  1. cửa chỉ khai ở cấp file: app chỉ đứng đầu một câu của cấp file (app.website(…)….listen(…);) hay sau return; nó
//     không đi vào hàm, không gán cho tên khác, không đưa làm đối số. File khác nhận hàm, không nhận app;
//  2. một cửa khai hai lần là lỗi, nêu chỗ của lần khai trước;
//  3. một câu của cửa phải có chữ cuối: app.website({ … }).guard(https); là lỗi, nêu chữ còn thiếu.
//
// Kèm theo: "application" chỉ đưa ra application và environment (các cửa gom về application), và application(…) viết
// một lần, ở cấp file. Các gói blueprint giữ cùng các luật lúc chạy (application.open, Dangling), cho chỗ cú pháp không
// thấy; ở đây là lời báo có dòng.

// closingWords là chữ cuối của mỗi cửa: chữ đầu là chữ lời báo nêu.
var closingWords = map[string][]string{
	"website":  {"listen", "port"},
	"database": {"open"},
	"schedule": {"run"},
	"command":  {"execute"},
}

// doorOrder là các cửa theo thứ tự lời báo nêu.
var doorOrder = []string{"website", "database", "schedule", "command"}

// doorWords là các chữ JS của mỗi cửa, đọc từ chính struct của nó: app.guard(…) là chữ của website viết nhầm lên app.
var doorWords = map[string]map[string]bool{
	"website":  wordsOf(reflect.TypeFor[*website.Website]()),
	"database": wordsOf(reflect.TypeFor[*database.Database]()),
	"schedule": wordsOf(reflect.TypeFor[*schedule.Schedule]()),
	"command":  wordsOf(reflect.TypeFor[*command.Command]()),
}

// applicationWords là các chữ của app không phải cửa.
var applicationWords = map[string]bool{
	"memory": true, "timezone": true, "desktop": true, "mobile": true, "run": true, "check": true,
}

// wordsOf là tên JS của các chữ của một struct, theo luật của engine: phương thức xuất ra mà mọi tham số và kết quả là
// value.Value (một tham số ...value.Value là một danh sách); tên viết thường chữ đầu, hay cả chữ khi toàn chữ hoa.
func wordsOf(pointer reflect.Type) map[string]bool {
	one, list := reflect.TypeFor[value.Value](), reflect.TypeFor[[]value.Value]()
	words := map[string]bool{}
	for i := range pointer.NumMethod() {
		m := pointer.Method(i)
		if m.Type.NumOut() != 1 || m.Type.Out(0) != one {
			continue
		}
		word := true
		for j := 1; j < m.Type.NumIn(); j++ {
			last := j == m.Type.NumIn()-1
			if m.Type.In(j) != one && !(last && m.Type.IsVariadic() && m.Type.In(j) == list) {
				word = false
			}
		}
		if !word {
			continue
		}
		words[jsName(m.Name)] = true
	}
	return words
}

// jsName là tên JS của một tên Go, theo luật của engine: viết thường chữ đầu (Certificates là certificates), hay cả chữ
// khi toàn chữ hoa (SSL là ssl).
func jsName(name string) string {
	if strings.ToUpper(name) == name {
		return strings.ToLower(name)
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

type doorCheck struct {
	file      string
	functions map[string]bool // tên cục bộ của application, import từ "application"
	libraries map[string]bool // tên của import * as … from "application"
	binding   string          // tên của const … = application(…); "" khi file không khai
	roots     map[*syntax.Call]bool
	written   bool                  // đã gặp một application(…) ở cấp file
	declared  map[string]syntax.Pos // cửa → câu khai nó; "application" → lời gọi application(…)
	shadow    map[string]int
}

func (c *doorCheck) fail(at syntax.Pos, format string, arguments ...any) {
	panic(failure{&syntax.Error{File: c.file, Pos: at, Message: fmt.Sprintf(format, arguments...)}})
}

// step là một chữ của một câu: .tên(…).
type step struct {
	name string
	at   syntax.Pos // chỗ của chữ, như VM nêu khi một chữ báo lỗi
	call *syntax.Call
}

// checkDoors kiểm ba luật của các cửa trên program (application.work), và trả chỗ khai của application và của mỗi cửa
// ("website" → chỗ của câu app.website(…)), cho Booted.Places.
func checkDoors(program *syntax.Program) (places map[string]syntax.Pos, err error) {
	c := &doorCheck{
		file: program.File, functions: map[string]bool{}, libraries: map[string]bool{},
		roots: map[*syntax.Call]bool{}, declared: map[string]syntax.Pos{}, shadow: map[string]int{},
	}
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			places, err = nil, f.err
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
			if _, door := closingWords[name.Name]; door {
				c.fail(name.Pos, `"application" has no %q: it exports application and environment, and a door is written app.%s(…)`, name.Name, name.Name)
			}
			if name.Name == "application" {
				c.functions[name.Local] = true
			}
		}
	}

	// tên của app trước: một hàm khai ở trên const app = application(…) vẫn bắt được tên ấy
	for _, s := range program.Body {
		d, ok := s.(*syntax.Declaration)
		if !ok || d.Init == nil {
			continue
		}
		if _, root := c.chain(d.Init); c.isRoot(root) {
			if c.binding != "" {
				c.fail(d.Pos, "application(…) is written once: %s is the application of this file", c.binding)
			}
			if !d.Const || d.Pattern != nil {
				c.fail(d.Pos, "write const app = application(…): the application of the file never changes")
			}
			c.binding = d.Name
		}
	}

	for _, s := range program.Body {
		var sentence syntax.Expression
		switch s := s.(type) {
		case *syntax.Declaration:
			sentence = s.Init
		case *syntax.ExpressionStatement:
			sentence = s.Value
		case *syntax.Return:
			sentence = s.Value
		}
		if sentence == nil {
			c.statement(s)
			continue
		}
		steps, root := c.chain(sentence)
		if _, declaration := s.(*syntax.Declaration); declaration && !c.isRoot(root) {
			c.statement(s) // const web = app.website(…): app không đứng đầu một câu
			continue
		}
		if !c.isRoot(root) && !c.isBinding(root) {
			c.statement(s)
			continue
		}
		if call, ok := root.(*syntax.Call); ok { // application(…) đứng đầu câu
			if c.written {
				c.fail(call.Callee.At(), "application(…) is written once, at the top level of %s", c.file)
			}
			c.written = true
			c.roots[call] = true
			c.declared["application"] = call.Callee.At()
			for _, a := range call.Arguments {
				c.expression(a)
			}
		}
		c.sentence(s.At(), steps)
		for _, step := range steps {
			for _, a := range step.call.Arguments {
				c.expression(a)
			}
		}
	}
	return c.declared, nil
}

// chain tách e thành gốc và các chữ nối sau nó: app.website({ … }).guard(a).listen(443) là gốc app và ba chữ. Gốc là
// thứ còn lại khi không gỡ được .tên(…) nào nữa: một tên, lời gọi application(…), hay một biểu thức khác.
func (c *doorCheck) chain(e syntax.Expression) (steps []step, root syntax.Expression) {
	for {
		call, ok := e.(*syntax.Call)
		if !ok || c.isApplication(call.Callee) {
			break
		}
		member, ok := call.Callee.(*syntax.Member)
		if !ok {
			break
		}
		steps = append(steps, step{name: member.Name, at: member.Pos, call: call})
		e = member.Object
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return steps, e
}

// sentence đi các chữ của một câu bắt đầu từ app, và kiểm luật 2 và 3: một chữ tên cửa mở câu của cửa ấy, chữ cuối của
// cửa đóng nó và trả về app; hết câu mà cửa còn mở là lỗi.
func (c *doorCheck) sentence(at syntax.Pos, steps []step) {
	door := ""
	for _, step := range steps {
		if door != "" {
			if !doorWords[door][step.name] {
				return // một chữ cửa không có (.lisen(443)): VM nói chữ ấy và các chữ của cửa, rõ hơn "chưa đóng"
			}
			for _, word := range closingWords[door] {
				if step.name == word {
					door = ""
				}
			}
			continue
		}
		if _, opens := closingWords[step.name]; opens {
			if first, found := c.declared[step.name]; found {
				c.fail(step.at, "application already has a %s, declared at %s:%s: a door is declared once, in one statement", step.name, c.file, first)
			}
			c.declared[step.name] = at
			door = step.name
			continue
		}
		if step.name == "desktop" || step.name == "mobile" { // chưa có câu của mình: chỉ nhớ chỗ, cho Describe
			c.declared[step.name] = at
		}
		if !applicationWords[step.name] {
			for _, name := range doorOrder {
				if doorWords[name][step.name] {
					c.fail(step.at, "application has no word %q: %s is a word of the %s, write app.%s(…).%s(…)", step.name, step.name, name, name, step.name)
				}
			}
		}
	}
	if door != "" {
		c.fail(at, "the %s is not closed: end the statement with .%s(…)", door, closingWords[door][0])
	}
}

// isApplication là e chỉ vào application của "application": tên import, hay lib.application.
func (c *doorCheck) isApplication(e syntax.Expression) bool {
	switch e := e.(type) {
	case *syntax.Identifier:
		return c.functions[e.Name] && c.shadow[e.Name] == 0
	case *syntax.Member:
		object, ok := e.Object.(*syntax.Identifier)
		return ok && e.Name == "application" && c.libraries[object.Name] && c.shadow[object.Name] == 0
	}
	return false
}

// isRoot là e là một lời gọi application(…).
func (c *doorCheck) isRoot(e syntax.Expression) bool {
	call, ok := e.(*syntax.Call)
	return ok && c.isApplication(call.Callee)
}

// isBinding là e là tên của app (không bị che).
func (c *doorCheck) isBinding(e syntax.Expression) bool {
	id, ok := e.(*syntax.Identifier)
	return ok && c.binding != "" && id.Name == c.binding && c.shadow[id.Name] == 0
}

// --- Đi phần còn lại của file: app và application(…) không được có mặt ở đó ---

func (c *doorCheck) statement(s syntax.Statement) {
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

// block đi một khối: tên nó khai che tên ngoài, như JavaScript.
func (c *doorCheck) block(body []syntax.Statement, parameters []syntax.Parameter) {
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

func (c *doorCheck) expression(e syntax.Expression) {
	switch e := e.(type) {
	case *syntax.Identifier:
		if c.isBinding(e) {
			c.fail(e.Pos, "%s is written at the top level of %s only, one statement for each door, like %s.website(…).listen(443): "+
				"a function or another file gets functions, never %s", e.Name, c.file, e.Name, e.Name)
		}
		if c.isApplication(e) {
			c.fail(e.Pos, "application(…) is written once, at the top level of %s: const app = application(…)", c.file)
		}
	case *syntax.Member:
		if c.isApplication(e) {
			c.fail(e.Pos, "application(…) is written once, at the top level of %s: const app = application(…)", c.file)
		}
		c.expression(e.Object)
	case *syntax.Index:
		c.expression(e.Object)
		c.expression(e.Index)
	case *syntax.Call:
		if !c.roots[e] { // application(…) đứng đầu một câu của cấp file là chỗ của nó; ở chỗ khác, tên ấy là lỗi (Identifier, Member)
			c.expression(e.Callee)
		}
		for _, a := range e.Arguments {
			c.expression(a)
		}
	case *syntax.Assign:
		c.expression(e.Target)
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

// defaults đi các giá trị mặc định của một pattern.
func (c *doorCheck) defaults(pattern *syntax.ObjectPattern) {
	for _, p := range pattern.Properties {
		if p.Default != nil {
			c.expression(p.Default)
		}
		if p.Pattern != nil {
			c.defaults(p.Pattern)
		}
	}
}
