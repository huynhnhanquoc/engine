package ui

import (
	"html"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/schema"
)

// A control bound to a field of a form (docs/KITWORK_SCHEMA.md mục 7): an <input>, <select> or <textarea> whose
// value is one {{ form.email }}, form a value of s.form(…) the route gave the page. The struct writes what it says of
// the field, so the markup copies no rule: name="email", the type of its kind (email, number, date, checkbox), the
// browser's attributes of its rules (required, minlength, maxlength, min, max, step), its value, aria-invalid when
// the form holds its error. A choice(…) is a <select> whose options are its words when the page writes none. In
// the <field> around it, an empty <label />, <hint /> and <error /> say the field's .label(), .description() and
// error; an empty <error /> with no error is hidden.
//
// One source: an attribute of the field's rules the page writes too is an error, unless it is stricter (a
// maxlength under the struct's .max(), required on a .nullable() field), and then it is the one written. name is
// always the struct's, and so is a type the field's kind gives. A field the engine fills, a .secret() one, one of
// many values (json, array, a struct) has no control.

// formState is what the page's forms are while it is drawn: the selects bound to a field, which their options read.
type formState struct {
	selects map[*Node]*schema.Input
}

// formPart is what a form adds to one element: attributes after the author's, the attributes it writes in place of
// the author's (owns), the element's content and what follows the author's content (a select's options).
type formPart struct {
	attributes []string
	owns       map[string]bool
	content    string
	hasContent bool
	tail       string
}

// binding is the field the control n is bound to, nil for none.
func (r *renderer) binding(n *Node) (*schema.Input, error) {
	if r.field != nil {
		r.readScope(scopeField) // what a scope around gives it: drawn again (frozen.go)
	}
	if r.field != nil && r.field.input == n {
		return r.field.bound, nil // read as the field around it began, which its label needed first
	}
	switch n.Tag {
	case "input", "select", "textarea":
	default:
		return nil, nil
	}
	a, ok := n.Attribute("value")
	if !ok {
		return nil, nil
	}
	source, whole := wrapped(strings.TrimSpace(a.Value))
	if !whole {
		return nil, nil
	}
	steps := strings.Split(source, ".")
	if len(steps) < 2 {
		return nil, nil
	}
	for _, step := range steps {
		if !identifier(strings.TrimSpace(step)) {
			return nil, nil // not a chain of names: an expression whose value the control shows
		}
	}
	dot := strings.LastIndex(source, ".")
	held, err := r.value(n, &a, source[:dot])
	if err != nil {
		return nil, err
	}
	form, ok := held.Host().(*schema.Form)
	if !ok {
		return nil, nil
	}
	in, problem := form.Input(strings.TrimSpace(source[dot+1:]))
	if problem != "" {
		return nil, at(n, &a, "%s", problem)
	}
	return &in, nil
}

// formPart is what the form a control is bound to adds to n: the control, an option of a bound select, the empty
// label, hint and error of a bound field. nil for an element no form is about.
func (r *renderer) formPart(n *Node) (*formPart, error) {
	switch n.Tag {
	case "input", "select", "textarea":
		in, err := r.binding(n)
		if err != nil || in == nil {
			return nil, err
		}
		return r.control(n, in)
	case "option":
		if parent := r.parent(); parent != nil && r.selectOf(parent) != nil {
			return r.option(n, r.selectOf(parent))
		}
	case "label", "hint", "error":
		if r.field != nil {
			r.readScope(scopeField) // what a scope around gives it: drawn again (frozen.go)
		}
		if r.field == nil || r.field.bound == nil || len(n.Children) > 0 {
			return nil, nil
		}
		in := r.field.bound
		part := &formPart{hasContent: true}
		switch n.Tag {
		case "label":
			part.content = in.Label
			if part.content == "" {
				return nil, at(n, nil, "<label /> rỗng là .label() của trường %s, mà trường không có: viết .label(\"…\") trong struct, hay chữ trong <label>", in.Name)
			}
		case "hint":
			part.content = in.Description
			if part.content == "" {
				return nil, at(n, nil, "<hint /> rỗng là .description() của trường %s, mà trường không có: viết .description(\"…\") trong struct, hay chữ trong <hint>", in.Name)
			}
		default:
			part.content = in.Problem
			if in.Problem == "" {
				part.attributes = append(part.attributes, "hidden") // an error with nothing to say
			}
		}
		return part, nil
	}
	return nil, nil
}

// control is what a bound input, select or textarea writes.
func (r *renderer) control(n *Node, in *schema.Input) (*formPart, error) {
	part := &formPart{owns: map[string]bool{"name": true, "value": true}}
	attribute := func(name, value string) {
		part.attributes = append(part.attributes, name+`="`+html.EscapeString(value)+`"`)
	}
	switch {
	case in.Control == "select" && n.Tag != "select":
		return nil, at(n, nil, "trường %s là choice(…): viết <select value=\"{{ … }}\">, lựa chọn là các chữ của nó", in.Name)
	case n.Tag == "textarea" && in.Type != "":
		return nil, at(n, nil, "<textarea> cho trường chữ; %s là ô %s", in.Name, in.Type)
	case n.Tag == "select" && in.Control != "select" && len(n.Elements()) == 0:
		return nil, at(n, nil, "<select> của trường %s cần <option>: chỉ choice(…) tự có lựa chọn", in.Name)
	}
	if a, ok := n.Attribute("name"); ok {
		return nil, at(n, &a, "name của ô nhập là tên trường form gắn (%s): bỏ name", in.Name)
	}
	written, hasType := n.Attribute("type")
	if hasType && in.Type != "" {
		return nil, at(n, &written, "type của trường %s là %s, struct đã cho: bỏ type", in.Name, in.Type)
	}
	if a, ok := n.Attribute("checked"); ok && in.Type == "checkbox" {
		return nil, at(n, &a, "checked theo giá trị của trường %s: bỏ checked", in.Name)
	}
	rules, err := stricter(n, in)
	if err != nil {
		return nil, err
	}
	attribute("name", in.Name)
	if in.Type != "" && n.Tag == "input" {
		attribute("type", in.Type)
	}
	for _, rule := range rules {
		part.owns[rule.Name] = true
		if rule.Name == "required" {
			part.attributes = append(part.attributes, "required")
			continue
		}
		if n.Tag == "select" || n.Tag == "textarea" && rule.Name != "minlength" && rule.Name != "maxlength" {
			continue // a select holds no length or bound, a textarea no number's
		}
		attribute(rule.Name, rule.Value)
	}
	switch {
	case n.Tag == "textarea":
		part.content, part.hasContent = in.Text(), true
	case n.Tag == "select" && len(n.Elements()) == 0:
		// a choice's words, the empty one first when the field takes null or has no value yet: a required select
		// whose first option is empty asks for a choice
		var options strings.Builder
		if in.Nullable || in.Text() == "" {
			options.WriteString(`<option value=""></option>`)
		}
		for _, word := range in.Choices {
			selected := ""
			if word == in.Text() {
				selected = " selected"
			}
			options.WriteString(`<option value="` + html.EscapeString(word) + `"` + selected + `>` + html.EscapeString(word) + `</option>`)
		}
		part.tail = options.String()
	case n.Tag == "select":
		if r.form == nil {
			r.form = &formState{selects: map[*Node]*schema.Input{}}
		}
		r.form.selects[n] = in // its options are the page's: each says whether it is the one
	case in.Type == "checkbox":
		part.owns["checked"] = true
		attribute("value", "true") // what parse reads as true; an unticked box sends nothing
		if in.Checked() {
			part.attributes = append(part.attributes, "checked")
		}
	case hasType && strings.TrimSpace(written.Value) == "password":
		// a password is never written back into the page
	default:
		attribute("value", in.Text())
	}
	if in.Problem != "" {
		attribute("aria-invalid", "true")
	}
	return part, nil
}

// option is what an option of a bound select writes: selected when it is the field's value. Of a choice(…), an
// option that is none of its words is an error.
func (r *renderer) option(n *Node, in *schema.Input) (*formPart, error) {
	a, ok := n.Attribute("value")
	if !ok {
		return nil, at(n, nil, "<option> của select gắn trường %s cần value", in.Name)
	}
	if _, written := n.Attribute("selected"); written {
		s, _ := n.Attribute("selected")
		return nil, at(n, &s, "selected theo giá trị của trường %s: bỏ selected", in.Name)
	}
	text, err := r.interpolate(n, &a, a.Value)
	if err != nil {
		return nil, err
	}
	if in.Choices != nil && text != "" && !containsWord(in.Choices, text) {
		return nil, at(n, &a, "%q không là lựa chọn của trường %s: choice(%s)", text, in.Name, quoted(in.Choices))
	}
	part := &formPart{owns: map[string]bool{"selected": true}}
	if text == in.Text() {
		part.attributes = append(part.attributes, "selected")
	}
	return part, nil
}

// stricter are the field's rules, each the page writes too taken when it is stricter than the struct's, and one the
// struct does not give taken as written; one as loose as the struct's, or looser, is an error.
func stricter(n *Node, in *schema.Input) ([]schema.Rule, error) {
	rules := append([]schema.Rule(nil), in.Rules...)
	given := map[string]int{}
	for i, rule := range rules {
		given[rule.Name] = i
	}
	for _, name := range []string{"required", "minlength", "maxlength", "min", "max", "step"} {
		a, ok := n.Attribute(name)
		if !ok {
			continue
		}
		if strings.Contains(a.Value, "{{") {
			return nil, at(n, &a, "%s của ô gắn trường %s viết thẳng: luật của trường không đổi theo dữ liệu", name, in.Name)
		}
		i, has := given[name]
		if name == "step" && has {
			return nil, at(n, &a, "step của trường %s là %s, kiểu của trường đã cho: bỏ step", in.Name, rules[i].Value)
		}
		if name == "required" {
			if has {
				return nil, at(n, &a, "trường %s đã bắt buộc (không .nullable(), không .default()): bỏ required", in.Name)
			}
			if !a.Bare && a.Value != "true" {
				return nil, at(n, &a, "required viết không giá trị, hay \"true\"")
			}
			rules = append([]schema.Rule{{Name: "required"}}, rules...)
			given = map[string]int{}
			for j, rule := range rules {
				given[rule.Name] = j
			}
			continue
		}
		written := strings.TrimSpace(a.Value)
		limit, err := strconv.ParseFloat(written, 64)
		if err != nil {
			return nil, at(n, &a, "%s là một số: %q", name, written)
		}
		if !has {
			rules = append(rules, schema.Rule{Name: name, Value: written})
			given[name] = len(rules) - 1
			continue
		}
		struct_, _ := strconv.ParseFloat(rules[i].Value, 64)
		tighter := false
		switch name {
		case "minlength", "min":
			tighter = limit > struct_
		case "maxlength", "max":
			tighter = limit < struct_
		}
		if !tighter {
			return nil, at(n, &a, "%s=\"%s\" của trường %s không chặt hơn %s=\"%s\" struct đã cho: bỏ nó, hay viết chặt hơn", name, written, in.Name, name, rules[i].Value)
		}
		rules[i].Value = written
	}
	return rules, nil
}

func containsWord(words []string, word string) bool {
	for _, w := range words {
		if w == word {
			return true
		}
	}
	return false
}

func quoted(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strconv.Quote(w)
	}
	return strings.Join(out, ", ")
}
