package schema

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// read is one field's value from outside at path: left out (undefined, or "" from a form for any kind but
// string) it is the default, or null when nullable, or a problem; else it is read as its kind (a form's
// text "28" is 28 to integer()), goes through the transforms, then the rules. A problem is added to
// found and the value is undefined; the failure is a transform's own, which ends parse.
func (f *Field) read(raw value.Value, path []value.Value, found *problems) (value.Value, value.Value) {
	if raw.IsInvalid() {
		return value.Value{}, raw
	}
	if raw.IsUndefined() || raw.IsString() && raw.Text() == "" && f.kind != kindString {
		switch {
		case f.hasDefault:
			return f.def, value.Value{}
		case f.nullable:
			return value.Null(), value.Value{}
		}
		found.add(path, RuleRequired, "bắt buộc")
		return value.Undefined(), value.Value{}
	}
	if raw.IsNull() {
		if f.nullable {
			return value.Null(), value.Value{}
		}
		found.add(path, RuleRequired, "không nhận null")
		return value.Undefined(), value.Value{}
	}
	before := found.count
	got, failure := f.coerce(raw, path, found)
	if failure.IsInvalid() {
		return value.Value{}, failure
	}
	if found.count > before {
		return value.Undefined(), value.Value{}
	}
	for _, fn := range f.transforms {
		out := fn.Call([]value.Value{got})
		if out.IsInvalid() {
			return value.Value{}, out
		}
		if !f.fits(out) {
			return value.Value{}, value.Fail(value.TypeCode, "transform của "+f.spelling()+" ở "+written(path)+" phải trả "+f.word()+", không phải "+out.Kind())
		}
		got = out
	}
	checked, rule, problem := f.check(got)
	if problem != "" {
		found.add(path, rule, problem)
		return value.Undefined(), value.Value{}
	}
	return checked, value.Value{}
}

// coerce reads raw as the field's kind: a form's text as a number or a boolean, a nested struct or array
// item by item. What cannot be read is a problem.
func (f *Field) coerce(raw value.Value, path []value.Value, found *problems) (value.Value, value.Value) {
	switch f.kind {
	case kindInteger, kindFloat, kindDecimal, kindTimestamp:
		if raw.IsNumber() {
			return raw, value.Value{}
		}
		if raw.IsTime() && f.kind == kindTimestamp {
			return value.Number(float64(raw.Time().UnixMilli())), value.Value{}
		}
		if raw.IsString() && plainNumber(raw.Text(), f.kind == kindInteger || f.kind == kindTimestamp) {
			n, _ := strconv.ParseFloat(raw.Text(), 64)
			return value.Number(n), value.Value{}
		}
		found.add(path, RuleType, "cần "+f.word())
	case kindBoolean:
		if raw.IsBoolean() {
			return raw, value.Value{}
		}
		if raw.IsString() {
			switch raw.Text() {
			case "true", "on": // "on" is what a checked checkbox sends
				return value.Boolean(true), value.Value{}
			case "false":
				return value.Boolean(false), value.Value{}
			}
		}
		found.add(path, RuleType, "cần true hay false")
	case kindJSON:
		if f.shape != nil {
			return f.shape.read(raw, path, found)
		}
		if text := value.Stringify(raw, ""); text.IsString() {
			return raw, value.Value{}
		}
		found.add(path, RuleType, "cần giá trị JSON")
	case kindArray:
		if !raw.IsArray() {
			found.add(path, RuleType, "cần mảng")
			return value.Undefined(), value.Value{}
		}
		items := raw.Items()
		out := make([]value.Value, len(items))
		for i, item := range items {
			got, failure := f.item.read(item, step(path, value.Number(float64(i))), found)
			if failure.IsInvalid() {
				return value.Value{}, failure
			}
			out[i] = got
		}
		return value.Array(out), value.Value{}
	case kindStruct:
		return f.shape.read(raw, path, found)
	default: // string, uuid, date, choice: text as it is
		if raw.IsString() {
			return raw, value.Value{}
		}
		found.add(path, RuleType, "cần chữ")
	}
	return value.Undefined(), value.Value{}
}

// plainNumber is a number written as a person writes one in a form, the only text read as a number:
// -12, 0, 35000.50, never 1e3, 0x10, 012, +1 or 1. ; whole is integer()'s, without a point.
func plainNumber(text string, whole bool) bool {
	if strings.HasPrefix(text, "-") {
		text = text[1:]
	}
	digits, fraction, pointed := strings.Cut(text, ".")
	if digits == "" || len(digits) > 1 && digits[0] == '0' || !allDigits(digits) {
		return false
	}
	if pointed {
		return !whole && fraction != "" && allDigits(fraction)
	}
	return true
}

func allDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// fits reports v as the field's kind of JavaScript value: what a transform must give back.
func (f *Field) fits(v value.Value) bool {
	switch f.kind {
	case kindString, kindUUID, kindDate, kindChoice:
		return v.IsString()
	case kindInteger, kindFloat, kindDecimal, kindTimestamp:
		return v.IsNumber()
	case kindBoolean:
		return v.IsBoolean()
	case kindArray:
		return v.IsArray()
	case kindStruct:
		return v.IsObject()
	}
	return !v.IsUndefined() && !v.IsFunction() && !v.IsStruct()
}

// word is the field's kind of value as a person reads it, in a problem.
func (f *Field) word() string {
	switch f.kind {
	case kindString, kindUUID, kindDate, kindChoice:
		return "chữ"
	case kindInteger:
		return "số nguyên"
	case kindTimestamp:
		return "số mili giây"
	case kindFloat, kindDecimal:
		return "số"
	case kindBoolean:
		return "true hay false"
	case kindArray:
		return "mảng"
	case kindStruct:
		return "object"
	}
	return "giá trị JSON"
}

// check is v, a value of the field's kind of JavaScript value, held to the field's rules: the kind's own
// (a valid date, a UUID, an integer a number holds exactly) and min, max, email. It gives the value kept
// (a UUID in lower case) or the rule it broke and the problem. A default goes through it when the struct is made.
func (f *Field) check(v value.Value) (value.Value, string, string) {
	if !f.fits(v) {
		return v, RuleType, "cần " + f.word()
	}
	switch f.kind {
	case kindString:
		text := v.Text()
		if !utf8.ValidString(text) {
			return v, RuleType, "không phải chuỗi UTF-8"
		}
		if strings.IndexByte(text, 0) >= 0 {
			return v, RuleType, "chứa ký tự NUL (byte 0)"
		}
		count := float64(utf8.RuneCountInString(text))
		if f.hasMin && count < f.min {
			return v, RuleMin, "cần ít nhất " + number(f.min) + " ký tự"
		}
		if f.hasMax && count > f.max {
			return v, RuleMax, "tối đa " + number(f.max) + " ký tự"
		}
		if f.email && !emailAddress.MatchString(text) {
			return v, RuleEmail, "không phải địa chỉ email"
		}
	case kindUUID:
		if !validUUID(v.Text()) {
			return v, RuleType, "cần UUID (8-4-4-4-12 chữ số hệ 16)"
		}
		return value.String(strings.ToLower(v.Text())), "", ""
	case kindDate:
		day, err := time.Parse("2006-01-02", v.Text())
		if err != nil || day.Format("2006-01-02") != v.Text() {
			return v, RuleType, "cần ngày YYYY-MM-DD"
		}
	case kindChoice:
		for _, word := range f.choices {
			if word == v.Text() {
				return v, "", ""
			}
		}
		return v, RuleChoice, "cần một trong " + joinWords(f.choices)
	case kindInteger, kindTimestamp:
		n := v.Number()
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			return v, RuleType, "cần " + f.word()
		}
		if math.Abs(n) > maxSafe {
			return v, RuleType, "ngoài ±(2^53−1)"
		}
		rule, problem := f.bounds(n)
		return v, rule, problem
	case kindFloat:
		n := v.Number()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return v, RuleType, "cần số"
		}
		rule, problem := f.bounds(n)
		return v, rule, problem
	case kindDecimal:
		n := v.Number()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return v, RuleType, "cần số"
		}
		whole, fraction, _ := strings.Cut(strconv.FormatFloat(math.Abs(n), 'f', -1, 64), ".")
		if len(fraction) > f.scale {
			return v, RuleType, "quá " + strconv.Itoa(f.scale) + " chữ số sau dấu phẩy"
		}
		if whole == "0" {
			whole = ""
		}
		if len(whole) > maxDigits-f.scale {
			return v, RuleType, "quá " + strconv.Itoa(maxDigits-f.scale) + " chữ số phần nguyên"
		}
		rule, problem := f.bounds(n)
		return v, rule, problem
	case kindArray:
		count := float64(len(v.Items()))
		if f.hasMin && count < f.min {
			return v, RuleMin, "cần ít nhất " + number(f.min) + " phần tử"
		}
		if f.hasMax && count > f.max {
			return v, RuleMax, "tối đa " + number(f.max) + " phần tử"
		}
	}
	return v, "", ""
}

// bounds is a number's min and max: the rule it broke and the problem, or two "".
func (f *Field) bounds(n float64) (string, string) {
	if f.hasMin && n < f.min {
		return RuleMin, "nhỏ nhất là " + number(f.min)
	}
	if f.hasMax && n > f.max {
		return RuleMax, "lớn nhất là " + number(f.max)
	}
	return "", ""
}

// emailAddress is a valid email address as HTML defines it for <input type="email">, so the server takes
// what the browser takes (WHATWG HTML, "valid email address").
var emailAddress = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

// validUUID is a UUID as PostgreSQL's uuid reads it in its usual form: 32 hexadecimal digits in groups of 8,
// 4, 4, 4 and 12, any case (kept lower case).
func validUUID(text string) bool {
	if len(text) != 36 {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return false
			}
		}
	}
	return true
}
