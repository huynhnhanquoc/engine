package schema

import (
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// Form is s.form(), an empty form of the struct s, and s.form(data, error), the form sent back with what the person
// typed and what was wrong (docs/KITWORK_SCHEMA.md mục 7). It goes into the page with context.bind({ form }):
// form.email is the field's value as the data has it, and an input written value="{{ form.email }}" is bound to the
// field, so the .ui engine writes its name, type and rules, and puts the field's error under it (library/ui, form.go).
// A form is the request's: nothing is kept after the page is drawn.
type Form struct {
	shape *Struct
	data  value.Value
	// problems are the message of each field error.fields names, by the field's name (the first of a field's)
	problems map[string]string
}

// Freeze is what value.Freeze calls on a form a site's file made as it booted (an empty one): nothing writes into it.
func (f *Form) Freeze() {}

// Form is s.form(), s.form(data) and s.form(data, error): data an object (a request's form, a row read), error the
// one safe(() => …) gives ({ code, message, fields }), whose fields go under the inputs.
func (s *Struct) Form(arguments ...value.Value) value.Value {
	if len(arguments) > 2 {
		return value.Fail(value.InvalidArgumentCode, "form nhận dữ liệu và lỗi: form(), form(input), form(input, error)")
	}
	data, failure := argument(arguments, 0), argument(arguments, 1)
	switch {
	case data.IsInvalid():
		return data
	case data.IsNullish():
		data = value.Undefined()
	case !data.IsObject() || data.IsArray():
		return value.Fail(value.InvalidArgumentCode, "form(dữ_liệu, …): dữ liệu là một object, như context.request.form() hay một dòng đọc ra, không phải "+data.Kind())
	}
	form := &Form{shape: s, data: data}
	switch {
	case failure.IsNullish():
	case !failure.IsObject() || failure.IsArray():
		return value.Fail(value.InvalidArgumentCode, "form(…, lỗi): lỗi là error của safe(() => …), { code, message, fields }, không phải "+failure.Kind())
	default:
		fields := failure.Get("fields")
		if !fields.IsArray() {
			break // a failure of no field (a delete others point to): the page says error.message itself
		}
		form.problems = map[string]string{}
		for _, entry := range fields.Items() {
			path, message := entry.Get("path"), entry.Get("message")
			if !path.IsArray() || len(path.Items()) == 0 || !message.IsString() {
				return value.Fail(value.InvalidArgumentCode, "form(…, lỗi): mỗi phần tử của error.fields là { path, rule, message }")
			}
			name := path.Items()[0].ToString().Text()
			if _, said := form.problems[name]; !said {
				form.problems[name] = message.Text()
			}
		}
	}
	return value.Struct(form)
}

// MissingMember is a field's value, form.email: what the data has (undefined when it has none); a name the struct has
// no field of fails, naming those it has.
func (f *Form) MissingMember(name string) value.Value {
	if f.shape.field(name) == nil {
		return value.Fail(value.TypeCode, "form không có trường "+name+"; có: "+f.shape.names())
	}
	if f.data.IsUndefined() {
		return value.Undefined()
	}
	return f.data.Get(name)
}

// Input is what a field says of the control bound to it (library/ui): its name, its type, the rules a browser holds as
// attributes, the words of its <label> and <hint>, its problem, its value. Go's only.
type Input struct {
	Name string
	// Control is the element the field is written with: "input", or "select" for a choice(…)
	Control string
	// Type is the input's type the field gives: email, number, date, checkbox; "" for a text, whose type the page
	// may write (password, tel, url, search)
	Type string
	// Rules are the attributes of the field's rules, in the order written: required, minlength, maxlength, min,
	// max, step; each with its value ("" for required)
	Rules []Rule
	// Choices are a choice(…)'s words, the options of its <select>
	Choices []string
	// Optional is a field the data may leave out: nullable, or with a default; Nullable one that takes null
	Optional, Nullable bool
	Label, Description string
	// Problem is the field's error, "" for none
	Problem string
	// Value is the field's value as the data has it, undefined for none; of an empty form, the field's default
	Value value.Value
}

// Rule is one attribute of a field's rules: required, minlength, maxlength, min, max, step.
type Rule struct {
	Name, Value string
}

// Input is the field name's control, or why no control is bound to it: a field the engine fills, a .secret() one, one
// of many values (json, array, a struct), a time in milliseconds.
func (f *Form) Input(name string) (Input, string) {
	field := f.shape.field(name)
	switch {
	case field == nil:
		return Input{}, "form không có trường " + name + "; có: " + f.shape.names()
	case field.generated():
		return Input{}, name + " là " + field.spelling() + ", engine điền: form không nhận nó"
	case field.table.secret:
		return Input{}, name + " là .secret(): không bao giờ xuống trình duyệt"
	}
	in := Input{Name: name, Control: "input", Optional: field.nullable || field.hasDefault, Nullable: field.nullable, Label: field.label,
		Description: field.description, Problem: f.problems[name], Value: f.MissingMember(name)}
	if f.data.IsUndefined() && field.hasDefault {
		in.Value = field.def // an empty form shows what a field left as it is becomes
	}
	if !in.Optional {
		in.Rules = append(in.Rules, Rule{Name: "required"})
	}
	limits := func(least, most string) {
		if field.hasMin {
			in.Rules = append(in.Rules, Rule{least, number(field.min)})
		}
		if field.hasMax {
			in.Rules = append(in.Rules, Rule{most, number(field.max)})
		}
	}
	switch field.kind {
	case kindString:
		if field.email {
			in.Type = "email"
		}
		limits("minlength", "maxlength")
	case kindUUID:
	case kindChoice:
		in.Control, in.Choices = "select", field.choices
	case kindInteger:
		in.Type = "number"
		limits("min", "max")
	case kindFloat:
		in.Type = "number"
		limits("min", "max")
		in.Rules = append(in.Rules, Rule{"step", "any"}) // a number's default step takes integers only
	case kindDecimal:
		in.Type = "number"
		limits("min", "max")
		if field.scale > 0 {
			in.Rules = append(in.Rules, Rule{"step", "0." + zeros(field.scale-1) + "1"})
		}
	case kindBoolean:
		in.Type = "checkbox"
	case kindDate:
		in.Type = "date"
	case kindTimestamp:
		return Input{}, name + " là timestamp(), số mili giây: chưa có ô nhập cho nó; ô ngày là date()"
	default:
		return Input{}, name + " là " + field.spelling() + ": ô nhập gắn với một trường một giá trị"
	}
	return in, ""
}

func zeros(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "0"
	}
	return out
}

// Text is the value's text in a control: "" for undefined and null, a number as JavaScript writes it.
func (in Input) Text() string {
	if in.Value.IsNullish() {
		return ""
	}
	if in.Value.IsNumber() {
		return strconv.FormatFloat(in.Value.Number(), 'f', -1, 64)
	}
	return in.Value.ToString().Text()
}

// Checked is a checkbox's value ticked: true, or the text a form sends of a ticked one.
func (in Input) Checked() bool {
	if in.Value.IsBoolean() {
		return in.Value.ToBoolean()
	}
	text := in.Text()
	return text == "true" || text == "on"
}
