package ui

import (
	"fmt"
	"html"
	"strings"

	"github.com/kitwork/engine2/library/schema"
)

// A <field> ties what it holds together by ids, as the first engine wrote it: its control (an input, a select, a
// textarea) has the field's id, which its <label> is for, and is described by its <hint> when the field holds one
// directly. A field that holds an <editor> also gives its label and its error ids, the names of what labels and
// describes the editor's writing area (editor.go); its label is then always there, never under an if or a for.
// Inside the editor's link popover no field reaches: the address input is the editor's. The ids count the page's
// fields in the order they are drawn: kitwork-field-1, kitwork-field-1-hint. What a piece of the site holds is not
// looked into yet.

// fieldIDs are the ids a field gives what it holds, "" for one it does not give. A field whose control is bound to
// a form's field (form.go) has it read as the field begins, since its <label /> comes first: input is the control,
// bound the form's field.
type fieldIDs struct {
	control, hint, label, error string
	input                       *Node
	bound                       *schema.Input
}

// fieldOf is the ids of the field n, the next of the page's.
func (r *renderer) fieldOf(n *Node) (*fieldIDs, error) {
	r.fields++
	f := &fieldIDs{control: fmt.Sprintf("kitwork-field-%d", r.fields)}
	for _, child := range n.Elements() {
		if child.Tag == "hint" {
			f.hint = f.control + "-hint"
			break
		}
	}
	if input, near := fieldControl(n, nil); input != nil && near == nil {
		bound, err := r.binding(input)
		if err != nil {
			return nil, err
		}
		f.input, f.bound = input, bound
		if found, _ := fieldFind(n, "error", nil); found != nil && bound != nil {
			f.error = f.control + "-error" // what describes the control while the form holds its error
		}
	}
	if editor, _ := fieldFind(n, "editor", nil); editor == nil {
		return f, nil
	}
	if label, around := fieldFind(n, "label", nil); label != nil {
		if around != nil {
			a, ok := around.Attribute("if")
			if !ok {
				a, _ = around.Attribute("for")
			}
			return nil, at(around, &a, "%s này có thể bỏ <label> ở %s:%d:%d, tên của vùng viết của editor (aria-labelledby trỏ vào nó): <label> của field có editor luôn có mặt",
				a.Name, label.File, label.Line, label.Column)
		}
		f.label = f.control + "-label"
	}
	if found, _ := fieldFind(n, "error", nil); found != nil {
		f.error = f.control + "-error"
	}
	return f, nil
}

// fieldControl is the first input, select or textarea inside n, outside a nested field and a popover, with the
// nearest element around it that writes if or for (nil for none).
func fieldControl(n *Node, around *Node) (found, near *Node) {
	for _, child := range n.Elements() {
		closest := around
		_, kept := child.Attribute("if")
		_, repeated := child.Attribute("for")
		if kept || repeated {
			closest = child
		}
		switch child.Tag {
		case "input", "select", "textarea":
			return child, closest
		case "field", "popover", "editor":
			continue
		}
		if found, near := fieldControl(child, closest); found != nil {
			return found, near
		}
	}
	return nil, nil
}

// fieldFind is the first element of tag inside n, outside a nested field and a popover, with the nearest element
// around it that writes if or for (nil for none).
func fieldFind(n *Node, tag string, around *Node) (found, near *Node) {
	for _, child := range n.Elements() {
		closest := around
		_, kept := child.Attribute("if")
		_, repeated := child.Attribute("for")
		if kept || repeated {
			closest = child
		}
		switch child.Tag {
		case tag:
			return child, closest
		case "field", "popover":
			continue
		}
		if found, near := fieldFind(child, tag, closest); found != nil {
			return found, near
		}
	}
	return nil, nil
}

// fieldAttributes are the ids the field around gives n.
func (r *renderer) fieldAttributes(n *Node) []string {
	f := r.field
	if f == nil {
		return nil
	}
	if f.bound != nil && n == f.input {
		r.readScope(scopeField) // the form's value and error: drawn again (frozen.go)
	}
	var attributes []string
	attribute := func(name, value string) { attributes = append(attributes, name+`="`+html.EscapeString(value)+`"`) }
	// the hint, and the field's error while the form holds one, describe the control bound to it
	described := f.hint
	if f.bound != nil && n == f.input && f.bound.Problem != "" {
		described = strings.TrimSpace(described + " " + f.error)
	}
	switch n.Tag {
	case "input":
		attribute("id", f.control)
		if described != "" {
			attribute("aria-describedby", described)
		}
	case "select":
		// the field around an editor is its writing area's: a select inside it (its format) is not its control
		if r.editor == nil || r.editor.field != f {
			attribute("id", f.control)
			if described != "" {
				attribute("aria-describedby", described)
			}
		}
	case "textarea":
		attribute("id", f.control)
		if described != f.hint {
			attribute("aria-describedby", described)
		}
	case "label":
		attribute("for", f.control)
		if f.label != "" {
			attribute("id", f.label) // what names an editor's writing area
		}
	case "hint":
		if f.hint != "" {
			attribute("id", f.hint)
		}
	case "error":
		if f.error != "" {
			attribute("id", f.error) // what describes an editor's writing area
		}
	}
	return attributes
}
