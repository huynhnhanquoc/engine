package schema

import (
	"math"
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// The kinds of a field, each the JavaScript value it reads (docs/schema.md mục 4).
const (
	kindString    = "string"
	kindUUID      = "uuid"
	kindInteger   = "integer"
	kindFloat     = "float"
	kindDecimal   = "decimal"
	kindBoolean   = "boolean"
	kindDate      = "date"
	kindTimestamp = "timestamp"
	kindJSON      = "json"
	kindChoice    = "choice"
	kindArray     = "array"
	kindStruct    = "struct" // a struct written as a field: an object of its shape
)

// maxDigits is a decimal's digits in all, the integer part and the scale (docs/schema.md mục 4).
const maxDigits = 15

// maxSafe is ±(2^53−1), the integers a JavaScript number holds exactly.
const maxSafe = 1<<53 - 1

// Field is one field of a struct: its kind, and the rules its value keeps. A modifier returns a copy with
// the rule set (step), so a field is a template any struct may use.
type Field struct {
	kind    string
	scale   int      // decimal's
	choices []string // choice's
	item    *Field   // array's
	shape   *Struct  // a struct's, json(struct)'s

	nullable   bool
	hasDefault bool
	def        value.Value
	hasMin     bool
	min        float64
	hasMax     bool
	max        float64
	email      bool
	// label and description are the field as a person reads it, .label("Họ tên"), .description("…"): what a form
	// says of its input, the title and description of toJSONSchema()
	label       string
	description string
	// transforms run in the order written, before every rule (docs/schema.md mục 5)
	transforms []value.Value
	// table is what the field says of a table it is a column of (table.go)
	table tableWords
}

// Freeze is what value.Freeze calls on a field a site's file made as it booted: no modifier writes into one.
func (f *Field) Freeze() {}

func (f *Field) step() *Field {
	next := *f
	next.transforms = append([]value.Value(nil), f.transforms...)
	return &next
}

// spelling is the field as a person writes it: string(), decimal(2), choice(…).
func (f *Field) spelling() string {
	switch f.kind {
	case kindDecimal:
		return "decimal(" + strconv.Itoa(f.scale) + ")"
	case kindChoice:
		return "choice(" + joinWords(f.choices) + ")"
	case kindArray:
		return "array(" + f.item.spelling() + ")"
	case kindStruct:
		return "struct"
	case kindID:
		if f.table.generate != "number" {
			return "id(\"" + f.table.generate + "\")"
		}
		return "id()"
	case kindJSON:
		if f.shape != nil {
			return "json(struct)"
		}
	}
	return f.kind + "()"
}

// Nullable lets the field be null; a field left out of the data is null too.
func (f *Field) Nullable(arguments ...value.Value) value.Value {
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".nullable() không nhận đối số")
	}
	next := f.step()
	next.nullable = true
	return value.Struct(next)
}

// Default is the value a field left out of the data takes: a string, a number, a boolean or null, which
// the field's own rules must take (checked when the struct is made, checkDefault). Not for a null given.
func (f *Field) Default(arguments ...value.Value) value.Value {
	given := argument(arguments, 0)
	if len(arguments) != 1 || !(given.IsString() || given.IsNumber() || given.IsBoolean() || given.IsNull()) {
		return value.Fail(value.InvalidArgumentCode, ".default nhận một chữ, một số, true, false hay null viết thẳng: .default(\"vi\")")
	}
	next := f.step()
	next.hasDefault, next.def = true, given
	return value.Struct(next)
}

// Min is the least a number may be, the fewest characters of a string, the fewest items of an array.
func (f *Field) Min(arguments ...value.Value) value.Value { return f.bound("min", arguments) }

// Max is the most a number may be, the most characters of a string, the most items of an array.
func (f *Field) Max(arguments ...value.Value) value.Value { return f.bound("max", arguments) }

func (f *Field) bound(word string, arguments []value.Value) value.Value {
	switch f.kind {
	case kindString, kindInteger, kindFloat, kindDecimal, kindArray:
	default:
		return value.Fail(value.InvalidArgumentCode, "."+word+"() chỉ đi sau string(), integer(), float(), decimal() hay array(), không sau "+f.spelling())
	}
	limit := argument(arguments, 0)
	n := limit.Number()
	if len(arguments) != 1 || !limit.IsNumber() || math.IsNaN(n) || math.IsInf(n, 0) {
		return value.Fail(value.InvalidArgumentCode, "."+word+" nhận một số viết thẳng: ."+word+"(1)")
	}
	if counted(f.kind) && (n < 0 || n != math.Trunc(n)) {
		return value.Fail(value.InvalidArgumentCode, "."+word+" của "+f.spelling()+" là một số đếm: số nguyên không âm")
	}
	next := f.step()
	if word == "min" {
		next.hasMin, next.min = true, n
	} else {
		next.hasMax, next.max = true, n
	}
	if next.hasMin && next.hasMax && next.min > next.max {
		return value.Fail(value.InvalidArgumentCode, ".min("+number(next.min)+") lớn hơn .max("+number(next.max)+")")
	}
	return value.Struct(next)
}

// counted is a kind whose min and max count: a string's characters, an array's items.
func counted(kind string) bool { return kind == kindString || kind == kindArray }

// Email is a string that is an email address as the browser's <input type="email"> reads one, so the
// browser and the server agree on the same text (docs/schema.md mục 3).
func (f *Field) Email(arguments ...value.Value) value.Value {
	if f.kind != kindString {
		return value.Fail(value.InvalidArgumentCode, ".email() chỉ đi sau string(), không sau "+f.spelling())
	}
	if len(arguments) != 0 {
		return value.Fail(value.InvalidArgumentCode, ".email() không nhận đối số")
	}
	next := f.step()
	next.email = true
	return value.Struct(next)
}

// Label is the field's name as a person reads it, .label("Họ tên"): what the empty <label /> of the <field> its
// input is bound to says (library/ui), and the title of toJSONSchema(). A text written, not empty.
func (f *Field) Label(arguments ...value.Value) value.Value { return f.words("label", arguments) }

// Description is what a person is told of the field, .description("Dùng để đăng nhập"): what the empty <hint /> of
// the <field> its input is bound to says, and the description of toJSONSchema().
func (f *Field) Description(arguments ...value.Value) value.Value {
	return f.words("description", arguments)
}

func (f *Field) words(word string, arguments []value.Value) value.Value {
	text := argument(arguments, 0)
	if len(arguments) != 1 || !text.IsString() || text.Text() == "" {
		return value.Fail(value.InvalidArgumentCode, "."+word+" nhận một chữ viết thẳng: ."+word+"(\"Họ tên\")")
	}
	next := f.step()
	if word == "label" {
		next.label = text.Text()
	} else {
		next.description = text.Text()
	}
	return value.Struct(next)
}

// Transform is a function the value goes through before the rules are checked, giving a value of the same
// kind: (value) => value.trim().toLowerCase(). A struct that is not a table runs it on the server, in
// parse; a table's is translated to SQL for the database, which waits for kitwork/database.
func (f *Field) Transform(arguments ...value.Value) value.Value {
	fn := argument(arguments, 0)
	if len(arguments) != 1 || !fn.IsFunction() {
		return value.Fail(value.InvalidArgumentCode, ".transform nhận một hàm: .transform((value) => value.trim())")
	}
	next := f.step()
	next.transforms = append(next.transforms, fn)
	return value.Struct(next)
}

// Normalize is the 05/11 draft's name of transform: normalize is the string method of Unicode, which a
// transform calls itself.
func (f *Field) Normalize(...value.Value) value.Value {
	return value.Fail(value.InvalidArgumentCode, ".normalize(fn) là cách viết cũ: viết .transform(fn); value.normalize() trong hàm là chuẩn hoá Unicode của JavaScript")
}

// checkDefault refuses a default the field's own rules do not take, when the struct is made: the rules are
// all written by then, whatever their order.
func (f *Field) checkDefault(name string) value.Value {
	if !f.hasDefault {
		return value.Value{}
	}
	if f.def.IsNull() {
		if !f.nullable {
			return value.Fail(value.InvalidArgumentCode, "trường "+name+": .default(null) cần .nullable()")
		}
		return value.Value{}
	}
	if _, _, problem := f.check(f.def); problem != "" {
		return value.Fail(value.InvalidArgumentCode, "trường "+name+": .default không hợp lệ: "+problem)
	}
	return value.Value{}
}

// keeps reports a field holding a transform, its own or a nested one's.
func (f *Field) keeps() bool {
	if len(f.transforms) > 0 {
		return true
	}
	if f.item != nil && f.item.keeps() {
		return true
	}
	return f.shape != nil && f.shape.keeps
}

// handOver is the field for one call: its transforms, and its nested ones, through hand (value.Keeper).
func (f *Field) handOver(hand func(value.Value) value.Value) *Field {
	if !f.keeps() {
		return f
	}
	next := f.step()
	for i, fn := range next.transforms {
		next.transforms[i] = hand(fn)
	}
	if f.item != nil {
		next.item = f.item.handOver(hand)
	}
	if f.shape != nil {
		next.shape = f.shape.handOver(hand)
	}
	return next
}

func number(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
