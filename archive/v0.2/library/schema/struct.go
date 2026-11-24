package schema

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Struct is struct({ … }): its fields in the order written.
type Struct struct {
	fields []named
	// keeps is a struct with a transform, its own or a nested one's: it is a value.Keeper, handed over for
	// each call of its methods so a transform written as the site booted runs on the request's VM.
	keeps bool
}

type named struct {
	name  string
	field *Field
}

// Freeze is what value.Freeze calls on a struct a site's file made as it booted: nothing writes into one.
func (s *Struct) Freeze() {}

func (s *Struct) keepsFunctions() bool {
	for _, f := range s.fields {
		if f.field.keeps() {
			return true
		}
	}
	return false
}

// HandOver is the struct one call of its method runs on (value.Keeper): itself when it has no transform, else a
// copy whose transforms went through hand, which runs them on the calling VM.
func (s *Struct) HandOver(hand func(value.Value) value.Value) value.Value {
	return value.Struct(s.handOver(hand))
}

func (s *Struct) handOver(hand func(value.Value) value.Value) *Struct {
	if !s.keeps {
		return s
	}
	next := &Struct{fields: make([]named, len(s.fields)), keeps: true}
	for i, f := range s.fields {
		next.fields[i] = named{name: f.name, field: f.field.handOver(hand)}
	}
	return next
}

// Parse reads data from outside (a request's JSON, a form's text) into a new object of the struct's fields,
// in the order written: each value transformed, then checked; a field left out takes its default, or null
// when nullable; a key the struct does not have stays out. Any field wrong is one INVALID_INPUT failure that
// names each, never their values: "dữ liệu không hợp lệ: email: không phải địa chỉ email; name: cần ít nhất 1
// ký tự", with its fields, each { path, rule, message }, which safe() hands the program as error.fields, so a
// form shows each under its input (docs/schema.md mục 6). A transform that fails is its own failure, as any
// call's.
func (s *Struct) Parse(arguments ...value.Value) value.Value {
	data := argument(arguments, 0)
	if data.IsInvalid() {
		return data
	}
	found := &problems{}
	out, failure := s.read(data, nil, found)
	if failure.IsInvalid() {
		return failure
	}
	if found.count > 0 {
		return found.failure()
	}
	return out
}

// read reads data as an object of s at path (nil at the top), adding what is wrong to found.
func (s *Struct) read(data value.Value, path []value.Value, found *problems) (value.Value, value.Value) {
	if !data.IsObject() {
		found.add(path, RuleType, "cần object")
		return value.Undefined(), value.Value{}
	}
	out := value.Object()
	for _, f := range s.fields {
		if f.field.generated() {
			continue // the engine makes it at insert: parse never takes it (docs/schema.md mục 6)
		}
		got, failure := f.field.read(data.Get(f.name), step(path, value.String(f.name)), found)
		if failure.IsInvalid() {
			return value.Value{}, failure
		}
		out = out.Set(f.name, got)
	}
	return out, value.Value{}
}

// Pick is a struct of the fields named, each with its rules, in the struct's order: the shape a form takes
// (customers.pick("name", "phone")). A name the struct does not have, or named twice, fails.
func (s *Struct) Pick(arguments ...value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, "pick nhận tên các trường: pick(\"name\", \"phone\")")
	}
	wanted := map[string]bool{}
	for _, name := range arguments {
		if !name.IsString() {
			return value.Fail(value.InvalidArgumentCode, "pick nhận tên trường viết bằng chữ: pick(\"name\")")
		}
		if wanted[name.Text()] {
			return value.Fail(value.InvalidArgumentCode, "pick có "+strconv.Quote(name.Text())+" hai lần")
		}
		field := s.field(name.Text())
		if field == nil {
			return value.Fail(value.InvalidArgumentCode, "struct không có trường "+name.Text()+"; có: "+s.names())
		}
		if field.generated() {
			return value.Fail(value.InvalidArgumentCode, "pick(\""+name.Text()+"\"): "+field.spelling()+" do engine điền, .parse() không bao giờ nhận nó")
		}
		wanted[name.Text()] = true
	}
	picked := &Struct{}
	for _, f := range s.fields {
		if wanted[f.name] {
			picked.fields = append(picked.fields, f)
		}
	}
	picked.keeps = picked.keepsFunctions()
	return value.Struct(picked)
}

// MissingMember is a field read off its struct, users.id, which foreign(…) takes (Column); a name the struct
// has no field of fails, naming those it has.
func (s *Struct) MissingMember(name string) value.Value {
	if s.field(name) == nil {
		return value.Fail(value.TypeCode, "struct không có trường "+name+"; có: "+s.names())
	}
	return value.Struct(&Column{owner: s, name: name})
}

func (s *Struct) field(name string) *Field {
	for _, f := range s.fields {
		if f.name == name {
			return f.field
		}
	}
	return nil
}

func (s *Struct) names() string {
	names := make([]string, len(s.fields))
	for i, f := range s.fields {
		names[i] = f.name
	}
	return strings.Join(names, ", ")
}

// step is path one step further, a field's name or an item's place, never sharing path's array: each field
// keeps its own.
func step(path []value.Value, next value.Value) []value.Value {
	return append(path[:len(path):len(path)], next)
}
