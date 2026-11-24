// Package schema is "kitwork/schema": struct({ … }), the one word for a shape of data, and the kinds and
// rules of its fields (docs/schema.md, docs/KITWORK_SCHEMA.md of the root).
//
//	import { struct, string, integer } from "kitwork/schema";
//
//	export const signup = struct({
//	  email: string().transform((value) => value.trim().toLowerCase()).max(254).email(),
//	  name: string().min(1).max(100),
//	  age: integer().min(0).nullable()
//	});
//
// A struct reads data from outside with parse (a request's JSON, a form's text) and gives a new object of
// its fields, transformed and checked, or an INVALID_INPUT failure naming each field that is wrong; pick
// gives a struct of some of its fields. A struct inside connection().tables({ … }) is a table: that part
// waits for kitwork/database, which engine2 does not serve yet, so the words that mean something only in a
// table (id(), foreign(), .unique(), .index(), …) fail where they are written instead of being ignored.
//
// Every value of the package is immutable: a modifier returns a new field and leaves the one it was called
// on as it was, so `const email = string().max(254)` is a template two structs use, each its own, and a
// struct a site's file makes as it boots is read by every request at once. Nothing registers anything.
package schema

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Schema is what the program imports: the builders of fields and struct itself.
type Schema struct {
	// Made is told of each struct made, as the app's .shared/schema/ is declared: kitwork/database refuses a
	// struct written with a word of a table that no tables({ … }) takes. nil in any other run. Go's only.
	Made func(made *Struct)
}

// Freeze is what value.Freeze calls on a struct a site's file made as it booted. A Schema keeps no state of
// a run: Made is the host's.
func (s *Schema) Freeze() {}

// Struct is struct({ name: field, … }): the fields in the order written. A field is a builder's value, or
// another struct, which nests (an object of that shape).
func (s *Schema) Struct(arguments ...value.Value) value.Value {
	written := argument(arguments, 0)
	if len(arguments) != 1 || !written.IsObject() {
		return value.Fail(value.InvalidArgumentCode, "struct nhận một object trường: struct({ name: string(), age: integer() })")
	}
	built := &Struct{}
	for _, name := range written.Keys() {
		if reserved[name] {
			return value.Fail(value.InvalidArgumentCode, "trường "+name+" trùng tên phương thức của struct ("+reservedList+"): đặt tên khác")
		}
		field, failure := fieldOf(name, written.Get(name))
		if failure.IsInvalid() {
			return failure
		}
		built.fields = append(built.fields, named{name: name, field: field})
	}
	if len(built.fields) == 0 {
		return value.Fail(value.InvalidArgumentCode, "struct cần ít nhất một trường: struct({ name: string() })")
	}
	for _, f := range built.fields {
		if failure := f.field.checkDefault(f.name); failure.IsInvalid() {
			return failure
		}
	}
	if keys := built.keys(); len(keys) > 1 {
		return value.Fail(value.InvalidArgumentCode, "struct có hai khoá, "+keys[0]+" và "+keys[1]+": một bảng một khoá, id(…) hay .key(); nhiều cột không được trùng là .unique(\"tên\")")
	}
	built.keeps = built.keepsFunctions()
	if s.Made != nil {
		s.Made(built)
	}
	return value.Struct(built)
}

// reserved are the names of a struct's own methods: a field of that name would be hidden behind the method
// once a field is read off its struct (foreign(users.id)), so it is refused where it is written.
var reserved = map[string]bool{"parse": true, "pick": true, "form": true, "toJSONSchema": true}

const reservedList = "parse, pick, form, toJSONSchema"

// fieldOf is the field a struct's key holds: a builder's field, or a struct, which nests.
func fieldOf(name string, written value.Value) (*Field, value.Value) {
	if written.IsInvalid() {
		return nil, written
	}
	switch built := written.Host().(type) {
	case *Field:
		return built, value.Value{}
	case *Struct:
		return &Field{kind: kindStruct, shape: built}, value.Value{}
	}
	return nil, value.Fail(value.InvalidArgumentCode, "trường "+name+" viết bằng một kiểu của kitwork/schema: "+builders)
}

const builders = "string(), uuid(), integer(), float(), decimal(2), boolean(), date(), timestamp(), json(), choice(\"a\", \"b\"), array(string()), hay một struct"

// The builders: each a new field of its kind, taking no argument but decimal's scale, json's struct,
// choice's words and array's item.

func (s *Schema) String(arguments ...value.Value) value.Value  { return plain(kindString, arguments) }
func (s *Schema) Uuid(arguments ...value.Value) value.Value    { return plain(kindUUID, arguments) }
func (s *Schema) Integer(arguments ...value.Value) value.Value { return plain(kindInteger, arguments) }
func (s *Schema) Float(arguments ...value.Value) value.Value   { return plain(kindFloat, arguments) }
func (s *Schema) Boolean(arguments ...value.Value) value.Value { return plain(kindBoolean, arguments) }
func (s *Schema) Date(arguments ...value.Value) value.Value    { return plain(kindDate, arguments) }
func (s *Schema) Timestamp(arguments ...value.Value) value.Value {
	return plain(kindTimestamp, arguments)
}

func plain(kind string, arguments []value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, kind+"() không nhận đối số")
	}
	return value.Struct(&Field{kind: kind})
}

// Decimal is decimal(scale): a number with at most scale digits after the point, 0 to 15, and 15 digits in
// all. decimal(12, 2), SQL's numeric(p, s), is decimal(2): the 15 digits are the precision.
func (s *Schema) Decimal(arguments ...value.Value) value.Value {
	if len(arguments) == 2 {
		return value.Fail(value.InvalidArgumentCode, "decimal nhận một số, số chữ số sau dấu phẩy: decimal(2); cả số tối đa 15 chữ số")
	}
	scale := argument(arguments, 0)
	if len(arguments) != 1 || !scale.IsNumber() || scale.Number() != float64(int(scale.Number())) || scale.Number() < 0 || scale.Number() > maxDigits {
		return value.Fail(value.InvalidArgumentCode, "decimal nhận số chữ số sau dấu phẩy, từ 0 tới 15: decimal(2)")
	}
	return value.Struct(&Field{kind: kindDecimal, scale: int(scale.Number())})
}

// Json is json() or json(struct): any value JSON writes, or one the struct reads.
func (s *Schema) Json(arguments ...value.Value) value.Value {
	switch len(arguments) {
	case 0:
		return value.Struct(&Field{kind: kindJSON})
	case 1:
		if shape, ok := arguments[0].Host().(*Struct); ok {
			return value.Struct(&Field{kind: kindJSON, shape: shape})
		}
	}
	return value.Fail(value.InvalidArgumentCode, "json nhận không gì, hay một struct: json(), json(address)")
}

// Choice is choice("a", "b", …): one of the words written, a closed set as TypeScript's union of strings.
func (s *Schema) Choice(arguments ...value.Value) value.Value {
	if len(arguments) == 0 {
		return value.Fail(value.InvalidArgumentCode, "choice nhận các chữ của tập: choice(\"moi\", \"giao\", \"huy\")")
	}
	words := make([]string, 0, len(arguments))
	seen := map[string]bool{}
	for _, word := range arguments {
		if !word.IsString() || word.Text() == "" {
			return value.Fail(value.InvalidArgumentCode, "choice nhận chữ viết thẳng, không rỗng: choice(\"moi\", \"giao\")")
		}
		if seen[word.Text()] {
			return value.Fail(value.InvalidArgumentCode, "choice có "+strconv.Quote(word.Text())+" hai lần")
		}
		seen[word.Text()] = true
		words = append(words, word.Text())
	}
	return value.Struct(&Field{kind: kindChoice, choices: words})
}

// Array is array(item): a list whose every item the item field reads.
func (s *Schema) Array(arguments ...value.Value) value.Value {
	if len(arguments) == 1 {
		if item, failure := fieldOf("của array", arguments[0]); !failure.IsInvalid() {
			return value.Struct(&Field{kind: kindArray, item: item})
		}
	}
	return value.Fail(value.InvalidArgumentCode, "array nhận kiểu của phần tử: array(string()), array(address)")
}

// The older words, each an error saying what is written now.

func (s *Schema) Table(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "table() là cách viết cũ: viết struct({ … }); struct trong connection().tables({ … }) là bảng (06/11/2026)")
}
func (s *Schema) Text(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "text() là cách viết cũ: viết string() (06/11/2026: string là kiểu giá trị JavaScript nó đọc ra)")
}
func (s *Schema) Number(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "number() không có: số đếm dùng integer(), tiền dùng decimal(2), số đo dùng float()")
}
func (s *Schema) Bigint(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "bigint() không có: .work không có BigInt; số đếm dùng integer(), mã số lớn của hệ khác dùng string()")
}
func (s *Schema) Kitid(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, "kitid() là cách viết cũ: khoá KitID của bảng là id(\"kitwork\"), KitID của hệ khác là string()")
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// joinWords is the words of a choice as a person reads them: "moi", "giao", "huy".
func joinWords(words []string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = strconv.Quote(word)
	}
	return strings.Join(quoted, ", ")
}
