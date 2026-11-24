// Package database là database của code engine3 chạy (24/11/2026, lát 3 của "engine3 trả lời request thật"): thư viện
// "schema" khai bảng (struct và các kiểu cột), thư viện "database" mở chúng (connection().tables({ … })) và đọc bằng câu
// lệnh có điều kiện viết bằng JavaScript ((orders) => orders.status === status), dịch sang SQL. Lưu trên sqlitewire
// (một người ghi mỗi file, các bản chụp đọc). Luật là docs/KITWORK_DATABASE.md và docs/KITWORK_SCHEMA.md; cách lưu
// từng kiểu là cách engine2 lưu, để đọc được cùng một file. Lát này chỉ đọc: ghi, migrate, join, includes, matches tới
// sau, mỗi thứ một lỗi nói vậy. blueprint/README.md "Database, lát 3" kể đủ.
package database

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Field là một cột struct khai: loại, và các luật. Luật được ghi lại; lát này chỉ đọc nên chỉ dùng loại, số chữ số lẻ
// và nullable (email, max, index… là việc của ghi và migrate).
type Field struct {
	kind     string // "id", "string", "integer", "float", "decimal", "timestamp", "date", "boolean", "choice"
	key      string // id(): "" (số tăng dần), "uuid", "kitwork"
	scale    int    // decimal(scale)
	nullable bool
	choices  []string
	rules    []string
}

// fieldRules là các luật không đối số một cột nhận; fieldValues là các luật có một đối số.
var (
	fieldRules  = []string{"nullable", "index", "unique", "email", "created", "updated", "readonly", "secret"}
	fieldValues = []string{"max", "min", "default", "label"}
)

func (f *Field) Key(name string) value.Value {
	if slices.Contains(fieldRules, name) || slices.Contains(fieldValues, name) {
		return javascript.NewNative(name, 0, func(run javascript.Run, arguments []value.Value) value.Value {
			return f.Word(run, name, arguments)
		})
	}
	return value.Fail(value.TypeCode, "%s() has no rule %q: %s, %s", f.kind, name, strings.Join(fieldRules, ", "), strings.Join(fieldValues, ", "))
}

// Word là một luật của cột: .nullable(), .max(253)… Trả một cột mới có thêm luật ấy, để viết nối nhau; cột cũ không
// đổi (một cột export từ một file là của mọi request cùng lúc).
func (f *Field) Word(_ javascript.Run, name string, arguments []value.Value) value.Value {
	switch {
	case slices.Contains(fieldRules, name):
		if len(arguments) > 0 {
			return value.Fail(value.TypeCode, ".%s() takes nothing", name)
		}
	case slices.Contains(fieldValues, name):
		if len(arguments) != 1 {
			return value.Fail(value.TypeCode, ".%s(value) takes one value", name)
		}
	default:
		return f.Key(name)
	}
	next := *f
	next.rules = append(slices.Clip(f.rules), name)
	if name == "nullable" {
		next.nullable = true
	}
	return value.Struct(&next)
}

// Struct là struct({ … }): các cột theo thứ tự viết. Trong connection().tables({ … }) nó là một bảng.
type Struct struct {
	names  []string
	fields []*Field
}

func (s *Struct) Key(name string) value.Value {
	return value.Fail(value.TypeCode, "a struct has no key %q: it declares a table in connection().tables({ … })", name)
}

func (s *Struct) Word(_ javascript.Run, name string, _ []value.Value) value.Value { return s.Key(name) }

// SchemaLibrary là thư viện "schema": struct và các kiểu cột.
func SchemaLibrary() map[string]value.Value {
	kind := func(name string) value.Value {
		return javascript.NewNative(name, 0, func(_ javascript.Run, arguments []value.Value) value.Value {
			if len(arguments) > 0 {
				return value.Fail(value.TypeCode, "%s() takes nothing", name)
			}
			return value.Struct(&Field{kind: name})
		})
	}
	library := map[string]value.Value{}
	for _, name := range []string{"string", "integer", "float", "timestamp", "date", "boolean"} {
		library[name] = kind(name)
	}
	library["id"] = javascript.NewNative("id", 1, func(_ javascript.Run, arguments []value.Value) value.Value {
		f := &Field{kind: "id"}
		if len(arguments) > 0 {
			if a := arguments[0]; !a.IsString() || (a.String() != "uuid" && a.String() != "kitwork") {
				return value.Fail(value.TypeCode, `id() is a number counting up; id("uuid") and id("kitwork") are the other keys, got %s`, describe(a))
			}
			f.key = arguments[0].String()
		}
		return value.Struct(f)
	})
	library["decimal"] = javascript.NewNative("decimal", 1, func(_ javascript.Run, arguments []value.Value) value.Value {
		if len(arguments) != 1 || !arguments[0].IsNumber() || arguments[0].Number() < 0 || arguments[0].Number() > 15 ||
			arguments[0].Number() != float64(int(arguments[0].Number())) {
			return value.Fail(value.TypeCode, "decimal(scale) takes the digits after the point, from 0 to 15: decimal(2)")
		}
		return value.Struct(&Field{kind: "decimal", scale: int(arguments[0].Number())})
	})
	library["choice"] = javascript.NewNative("choice", 2, func(_ javascript.Run, arguments []value.Value) value.Value {
		f := &Field{kind: "choice"}
		for _, a := range arguments {
			if !a.IsString() || a.String() == "" {
				return value.Fail(value.TypeCode, `choice("a", "b") takes the choices as strings, got %s`, describe(a))
			}
			f.choices = append(f.choices, a.String())
		}
		if len(f.choices) < 2 {
			return value.Fail(value.TypeCode, `choice("a", "b") takes two choices or more`)
		}
		return value.Struct(f)
	})
	library["struct"] = javascript.NewNative("struct", 1, func(_ javascript.Run, arguments []value.Value) value.Value {
		if len(arguments) != 1 || !arguments[0].IsObject() {
			return value.Fail(value.TypeCode, "struct({ … }) takes an object of columns: struct({ id: id(), email: string() })")
		}
		s := &Struct{}
		object := arguments[0]
		for _, name := range object.Keys() {
			item, _ := object.Get(name)
			f, ok := value.As[Field](item)
			if !ok {
				return value.Fail(value.TypeCode, "struct({ %s }): a column is string(), integer(), decimal(2)…, got %s", name, describe(item))
			}
			s.names, s.fields = append(s.names, name), append(s.fields, f)
		}
		if len(s.names) == 0 {
			return value.Fail(value.TypeCode, "struct({ … }) declares one column or more")
		}
		return value.Struct(s)
	})
	return library
}

// describe là một giá trị cho lời báo.
func describe(v value.Value) string {
	switch {
	case v.IsString():
		return strconv.Quote(v.String())
	case v.IsStruct():
		return fmt.Sprintf("a struct of the engine (%T)", v.Struct())
	}
	return v.Typeof()
}
