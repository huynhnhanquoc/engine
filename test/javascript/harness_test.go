package javascript_test

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // múi giờ của test có trên mọi máy
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Bộ đối chiếu với node: mỗi ca viết một lời gọi JavaScript `this.method(đối số)` bằng chữ (literal); node chạy nó và
// tả kết quả cùng this sau lời gọi (một phương thức sửa mảng thì thấy), Go dựng cùng các giá trị bằng parser dưới đây
// rồi gọi javascript.Invoke, và hai bản tả phải giống từng chữ. Số tả bằng bit của float64 (0 và -0 khác nhau), chuỗi
// bằng JSON.stringify (nửa cặp đứng một mình thành \udxxx), lỗi là "throw" và loại của nó: node ghi tên lớp lỗi, Go ghi
// mã của value, thrown đổi tên lớp ra mã (TypeError là TYPE…); lời báo của Kitwork viết theo luật riêng, không so. Hàm gọi lại là những tên trong callbacks, có cả bản node lẫn bản Go.

// testZone là múi giờ địa phương của mọi ca, ở cả hai bên: node đặt nó đầu script (nodePrelude), Go đặt time.Local, mà
// Date của Global đọc. Một múi có chuyển giờ mùa hè, để thử giờ bị bỏ qua và giờ lặp lại.
const testZone = "America/New_York"

func init() {
	location, err := time.LoadLocation(testZone)
	if err != nil {
		panic(err)
	}
	time.Local = location
}

// callback là một hàm gọi lại của ca thử: nguồn node và bản Go.
type callback struct {
	node string
	fn   func(run javascript.Run, arguments []value.Value) value.Value
}

// run là Run của test: gọi bản Go của hàm gọi lại.
type run struct{}

func (run) Call(function value.Value, arguments ...value.Value) value.Value {
	return function.Function().(*callback).fn(run{}, arguments)
}

func arg(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

func number(v value.Value) float64 { return javascript.ToNumber(nil, v).Number() }

var callbacks = map[string]*callback{
	"double": {"x => x * 2", func(_ javascript.Run, a []value.Value) value.Value { return value.Number(number(arg(a, 0)) * 2) }},
	"isEven": {"x => x % 2 === 0", func(_ javascript.Run, a []value.Value) value.Value {
		return value.Boolean(math.Mod(number(arg(a, 0)), 2) == 0)
	}},
	"isBig": {"x => x > 2", func(_ javascript.Run, a []value.Value) value.Value { return value.Boolean(number(arg(a, 0)) > 2) }},
	"ascending": {"(a, b) => a - b", func(_ javascript.Run, a []value.Value) value.Value {
		return value.Number(number(arg(a, 0)) - number(arg(a, 1)))
	}},
	"descending": {"(a, b) => b - a", func(_ javascript.Run, a []value.Value) value.Value {
		return value.Number(number(arg(a, 1)) - number(arg(a, 0)))
	}},
	"sum": {"(a, b) => a + b", func(_ javascript.Run, a []value.Value) value.Value {
		return value.Number(number(arg(a, 0)) + number(arg(a, 1)))
	}},
	"concat": {"(a, b) => String(a) + String(b)", func(r javascript.Run, a []value.Value) value.Value {
		return javascript.Invoke(r, javascript.ToString(r, arg(a, 0)), "concat", []value.Value{arg(a, 1)})
	}},
	"pair":     {"x => [x, x]", func(_ javascript.Run, a []value.Value) value.Value { return value.Array(arg(a, 0), arg(a, 0)) }},
	"index":    {"(x, i) => i", func(_ javascript.Run, a []value.Value) value.Value { return arg(a, 1) }},
	"identity": {"x => x", func(_ javascript.Run, a []value.Value) value.Value { return arg(a, 0) }},
	"upper": {"m => m.toUpperCase()", func(r javascript.Run, a []value.Value) value.Value {
		return javascript.Invoke(r, arg(a, 0), "toUpperCase", nil)
	}},
	"position": {"(m, p) => p", func(_ javascript.Run, a []value.Value) value.Value { return arg(a, 1) }},
	"boom":     {`() => { throw new Error("boom") }`, func(_ javascript.Run, _ []value.Value) value.Value { return value.Invalid("boom") }},
	"seven":    {"() => 7", func(_ javascript.Run, _ []value.Value) value.Value { return value.Number(7) }},
	"text":     {`() => "t"`, func(_ javascript.Run, _ []value.Value) value.Value { return value.String("t") }},
	"empty":    {"() => ({})", func(_ javascript.Run, _ []value.Value) value.Value { return value.Object() }},
	"nan":      {"() => NaN", func(_ javascript.Run, _ []value.Value) value.Value { return value.Number(math.NaN()) }},
	"shrink": {"(x, i, a) => { a.pop(); return true; }", func(r javascript.Run, a []value.Value) value.Value {
		if popped := javascript.Invoke(r, arg(a, 2), "pop", nil); popped.IsInvalid() {
			return popped // a không phải mảng (Object.groupBy không đưa nó): node ném
		}
		return value.Boolean(true)
	}},
	"byK": {"(a, b) => a.k - b.k", func(_ javascript.Run, a []value.Value) value.Value {
		x, _ := arg(a, 0).Get("k")
		y, _ := arg(a, 1).Get("k")
		return value.Number(number(x) - number(y))
	}},
	// reviver và hàm thay của JSON (khoá, giá trị); toJSON (khoá)
	"tag": {`(k, v) => v !== null && typeof v === "object" ? v : k + "=" + String(v)`, func(r javascript.Run, a []value.Value) value.Value {
		if v := arg(a, 1); v.IsObject() || v.IsArray() {
			return v
		}
		return value.String(arg(a, 0).String() + "=" + javascript.ToString(r, arg(a, 1)).String())
	}},
	"dropNumbers": {`(k, v) => typeof v === "number" ? undefined : v`, func(_ javascript.Run, a []value.Value) value.Value {
		if arg(a, 1).IsNumber() {
			return value.Undefined()
		}
		return arg(a, 1)
	}},
	"doubleNumbers": {`(k, v) => typeof v === "number" ? v * 2 : v`, func(_ javascript.Run, a []value.Value) value.Value {
		if arg(a, 1).IsNumber() {
			return value.Number(arg(a, 1).Number() * 2)
		}
		return arg(a, 1)
	}},
	"dropA": {`(k, v) => k === "a" ? undefined : v`, func(_ javascript.Run, a []value.Value) value.Value {
		if arg(a, 0).String() == "a" {
			return value.Undefined()
		}
		return arg(a, 1)
	}},
	"keyed": {`(k) => "key:" + k`, func(_ javascript.Run, a []value.Value) value.Value {
		return value.String("key:" + arg(a, 0).String())
	}},
	"date0": {`() => new Date(0)`, func(_ javascript.Run, _ []value.Value) value.Value { return value.Time(time.UnixMilli(0)) }},
}

// testCase là một lời gọi đã tách: nguồn của this và của các đối số (cho node), và các giá trị (cho Go).
type testCase struct {
	source, this, method, arguments string
}

// split tách "this.method(đối số)" ở dấu chấm sau this và ngoặc của lời gọi.
// Lời gọi là .method( cuối cùng ở tầng ngoài cùng, và nó phải kết thúc ca: this là mọi thứ trước dấu chấm ấy, nên this
// tự nó có thể là một lời gọi (Object.freeze([1]).push(2), new Date(0).getTime()).
func split(t *testing.T, source string) testCase {
	p := &parser{t: t, s: source}
	depth, dot, open := 0, -1, -1
	method := ""
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == '"' || c == '\'':
			p.str()
		case c == '(' || c == '[' || c == '{':
			depth++
			p.i++
		case c == ')' || c == ']' || c == '}':
			depth--
			p.i++
		case c == '.' && depth == 0:
			at := p.i
			p.i++
			if name := p.identifier(); name != "" && p.i < len(p.s) && p.s[p.i] == '(' {
				dot, method, open = at, name, p.i
			}
		default:
			p.i++
		}
	}
	if dot < 0 {
		t.Fatalf("%s: expected a call this.method(…)", source)
	}
	// ngoặc mở của lời gọi phải đóng ở chữ cuối
	p.i, depth = open, 0
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' || c == '\'' {
			p.str()
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
		} else if c == ')' || c == ']' || c == '}' {
			depth--
		}
		if depth == 0 {
			break
		}
		p.i++
	}
	if p.i != len(p.s)-1 {
		t.Fatalf("%s: expected the call to end the case", source)
	}
	return testCase{source, source[:dot], method, source[open+1 : p.i]}
}

// values dựng this và các đối số của ca thành value.Value.
func (c testCase) values(t *testing.T) (value.Value, []value.Value) {
	this := (&parser{t: t, s: c.this}).whole()
	var arguments []value.Value
	p := &parser{t: t, s: c.arguments}
	for p.space(); p.i < len(p.s); p.space() {
		arguments = append(arguments, p.literal())
		p.space()
		if p.i < len(p.s) {
			p.expect(',')
		}
	}
	return this, arguments
}

// parser đọc chữ của JavaScript: số (cả -0, NaN, Infinity), chuỗi với các thoát, mảng, object, true, false, null,
// undefined, date(ms), và tên một hàm gọi lại.
type parser struct {
	t *testing.T
	s string
	i int
}

func (p *parser) whole() value.Value {
	v := p.literal()
	p.space()
	if p.i != len(p.s) {
		p.t.Fatalf("%q: unexpected %q", p.s, p.s[p.i:])
	}
	return v
}

func (p *parser) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\n') {
		p.i++
	}
}

func (p *parser) expect(c byte) {
	p.space()
	if p.i >= len(p.s) || p.s[p.i] != c {
		p.t.Fatalf("%q at %d: expected %q", p.s, p.i, c)
	}
	p.i++
}

func (p *parser) identifier() string {
	p.space()
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '_' || p.s[p.i] >= 'a' && p.s[p.i] <= 'z' || p.s[p.i] >= 'A' && p.s[p.i] <= 'Z' ||
		p.i > start && p.s[p.i] >= '0' && p.s[p.i] <= '9') {
		p.i++
	}
	return p.s[start:p.i]
}

func (p *parser) literal() value.Value {
	p.space()
	if p.i >= len(p.s) {
		p.t.Fatalf("%q: expected a value", p.s)
	}
	switch c := p.s[p.i]; {
	case c == '(':
		p.i++
		v := p.literal()
		p.expect(')')
		return v
	case c == '[':
		p.i++
		var items []value.Value
		for p.space(); p.s[p.i] != ']'; p.space() {
			items = append(items, p.literal())
			p.space()
			if p.s[p.i] == ',' {
				p.i++
			}
		}
		p.i++
		return value.Array(items...)
	case c == '{':
		p.i++
		o := value.Object()
		for p.space(); p.s[p.i] != '}'; p.space() {
			var key string
			if p.s[p.i] == '"' || p.s[p.i] == '\'' {
				key = p.str().String()
			} else if c := p.s[p.i]; c >= '0' && c <= '9' {
				key = strconv.FormatFloat(p.number().Number(), 'f', -1, 64) // {1: "x"} là khoá "1"
			} else {
				key = p.identifier()
			}
			p.expect(':')
			o.Set(key, p.literal())
			p.space()
			if p.s[p.i] == ',' {
				p.i++
			}
		}
		p.i++
		return o
	case c == '"' || c == '\'':
		return p.str()
	case c == '-' || c == '.' || c >= '0' && c <= '9':
		return p.number()
	}
	name := p.identifier()
	switch name {
	case "new": // new Date(…) là Date(…): node chạy new, Go gọi hàm
		return p.literal()
	case "true", "false":
		return value.Boolean(name == "true")
	case "null":
		return value.Null()
	case "undefined":
		return value.Undefined()
	case "NaN":
		return value.Number(math.NaN())
	case "Infinity":
		return value.Number(math.Inf(1))
	case "date":
		p.expect('(')
		ms := p.number().Number()
		p.expect(')')
		return value.Time(time.UnixMilli(int64(ms)))
	}
	if f, ok := callbacks[name]; ok {
		return value.Function(f)
	}
	if g, ok := javascript.Global(name); ok {
		// Array(3), Object.freeze([1]): gọi ngay ở đây, như node làm khi tính this hay một đối số; lỗi của lời gọi là giá
		// trị, nên ca là throw
		if g.IsFunction() && p.i < len(p.s) && p.s[p.i] == '(' {
			return g.Function().(*javascript.Native).Call(run{}, p.arguments()...)
		}
		// Math.abs là hàm của Math
		if p.i >= len(p.s) || p.s[p.i] != '.' {
			return g
		}
		p.i++
		key := p.identifier()
		ns := javascript.Members(g)
		if ns == nil {
			p.t.Fatalf("%q: %s has no members", p.s, name)
		}
		item, found := ns.Get(key)
		if !found {
			p.t.Fatalf("%q: %s has no %q", p.s, name, key)
		}
		if p.i < len(p.s) && p.s[p.i] == '(' {
			return item.Function().(*javascript.Native).Call(run{}, p.arguments()...)
		}
		return item
	}
	p.t.Fatalf("%q: unknown name %q", p.s, name)
	return value.Value{}
}

// arguments đọc các đối số của một lời gọi, từ ngoặc mở tới ngoặc đóng.
func (p *parser) arguments() []value.Value {
	p.expect('(')
	var arguments []value.Value
	for p.space(); p.s[p.i] != ')'; p.space() {
		arguments = append(arguments, p.literal())
		p.space()
		if p.s[p.i] == ',' {
			p.i++
		}
	}
	p.i++
	return arguments
}

func (p *parser) number() value.Value {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
		if strings.HasPrefix(p.s[p.i:], "Infinity") {
			p.i += len("Infinity")
			return value.Number(math.Inf(-1))
		}
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		sign := (c == '+' || c == '-') && (p.s[p.i-1] == 'e' || p.s[p.i-1] == 'E')
		if !(c >= '0' && c <= '9' || c == '.' || c == 'e' || c == 'E' || sign) {
			break
		}
		p.i++
	}
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		p.t.Fatalf("%q: bad number %q", p.s, p.s[start:p.i])
	}
	return value.Number(n)
}

// str đọc một chuỗi trong nháy, các thoát của JavaScript thành đơn vị UTF-16, rồi viết lại như Kitwork giữ: UTF-8,
// nửa cặp đứng một mình ở dạng ba byte.
func (p *parser) str() value.Value {
	quote := p.s[p.i]
	p.i++
	var units []uint16
	for p.s[p.i] != quote {
		c := p.s[p.i]
		if c != '\\' {
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			p.i += size
			units = appendRune(units, r)
			continue
		}
		p.i++
		e := p.s[p.i]
		p.i++
		switch e {
		case 'n':
			units = append(units, '\n')
		case 't':
			units = append(units, '\t')
		case 'r':
			units = append(units, '\r')
		case 'b':
			units = append(units, '\b')
		case 'f':
			units = append(units, '\f')
		case 'v':
			units = append(units, '\v')
		case '0':
			units = append(units, 0)
		case 'u':
			if p.s[p.i] == '{' {
				end := strings.IndexByte(p.s[p.i:], '}')
				r, _ := strconv.ParseUint(p.s[p.i+1:p.i+end], 16, 32)
				units = appendRune(units, rune(r))
				p.i += end + 1
			} else {
				u, _ := strconv.ParseUint(p.s[p.i:p.i+4], 16, 16)
				units = append(units, uint16(u))
				p.i += 4
			}
		default:
			units = append(units, uint16(e))
		}
	}
	p.i++
	return value.String(fromUnits(units))
}

func appendRune(units []uint16, r rune) []uint16 {
	if r < 0x10000 {
		return append(units, uint16(r))
	}
	r -= 0x10000
	return append(units, uint16(0xD800+r>>10), uint16(0xDC00+r&0x3FF))
}

// fromUnits viết đơn vị thành chuỗi như Kitwork giữ: một cặp thành ký tự bốn byte, nửa đứng một mình thành ba byte.
func fromUnits(u []uint16) string {
	var b []byte
	for i := 0; i < len(u); i++ {
		switch {
		case u[i] >= 0xD800 && u[i] < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b = utf8.AppendRune(b, 0x10000+(rune(u[i])-0xD800)<<10+(rune(u[i+1])-0xDC00))
			i++
		case u[i] >= 0xD800 && u[i] < 0xE000:
			b = append(b, 0xED, 0x80|byte(u[i]>>6)&0x3F, 0x80|byte(u[i])&0x3F)
		default:
			b = utf8.AppendRune(b, rune(u[i]))
		}
	}
	return string(b)
}

// toUnits đọc một chuỗi Kitwork thành đơn vị, độc lập với core/javascript (để bản tả không dựa vào thứ đang thử).
func toUnits(s string) []uint16 {
	var out []uint16
	for i := 0; i < len(s); {
		if i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			out = append(out, 0xD000|uint16(s[i+1]&0x3F)<<6|uint16(s[i+2]&0x3F))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		out = appendRune(out, r)
		i += size
	}
	return out
}

// describe tả v đúng như describe của node trong script: số bằng bit, chuỗi bằng JSON.stringify.
// thrown đổi lỗi node tả ("throw TypeError") ra mã của value ("throw TYPE"): SyntaxError của JSON.parse và URIError đều
// là INVALID_INPUT; Error thường (một hàm gọi lại ném) là lỗi không mã. Kết quả không phải lỗi giữ nguyên.
func thrown(described string) string {
	name, ok := strings.CutPrefix(described, "throw ")
	if !ok {
		return described
	}
	switch name {
	case "TypeError":
		return "throw " + value.TypeCode
	case "RangeError":
		return "throw " + value.RangeCode
	case "SyntaxError", "URIError":
		return "throw " + value.InvalidInputCode
	case "Error":
		return "throw "
	}
	return "throw ? " + name
}

func describe(v value.Value) string {
	switch {
	case v.IsUndefined():
		return "undefined"
	case v.IsNull():
		return "null"
	case v.IsBoolean():
		return strconv.FormatBool(v.Boolean())
	case v.IsNumber():
		if n := v.Number(); n != n {
			return "NaN"
		}
		return "n:" + strconv.FormatUint(math.Float64bits(v.Number()), 16)
	case v.IsString():
		return "s:" + quote(toUnits(v.String()))
	case v.IsFunction():
		return "function"
	case v.IsTime():
		return "date:" + strconv.FormatInt(v.Time().UnixMilli(), 10)
	case v.IsArray():
		parts := make([]string, v.Length())
		for i := range parts {
			parts[i] = describe(v.Index(i))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case v.IsObject():
		var parts []string
		for _, key := range v.Keys() {
			item, _ := v.Get(key)
			parts = append(parts, quote(toUnits(key))+":"+describe(item))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return "?" + v.Typeof()
}

// quote là JSON.stringify của một chuỗi: các thoát ngắn, ký tự điều khiển khác là \u00xx, nửa cặp đứng một mình là
// \udxxx, mọi thứ khác viết nguyên.
func quote(u []uint16) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00` + strconv.FormatUint(uint64(c)>>4, 16) + strconv.FormatUint(uint64(c)&15, 16))
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b.WriteRune(0x10000 + (rune(c)-0xD800)<<10 + (rune(u[i+1]) - 0xDC00))
			i++
		case c >= 0xD800 && c < 0xE000:
			b.WriteString(`\u` + strconv.FormatUint(uint64(c), 16))
		default:
			b.WriteRune(rune(c))
		}
	}
	b.WriteByte('"')
	return b.String()
}

const nodePrelude = `
process.env.TZ = "` + testZone + `";
const bits = (x) => { const view = new DataView(new ArrayBuffer(8)); view.setFloat64(0, x); return view.getBigUint64(0).toString(16); };
const describe = (x) => {
  if (x === undefined) return "undefined";
  if (x === null) return "null";
  if (typeof x === "boolean") return String(x);
  if (typeof x === "number") return Number.isNaN(x) ? "NaN" : "n:" + bits(x);
  if (typeof x === "string") return "s:" + JSON.stringify(x);
  if (typeof x === "function") return "function";
  if (x instanceof Date) return "date:" + x.getTime();
  if (Array.isArray(x)) return "[" + Array.from(x, describe).join(",") + "]";
  return "{" + Object.keys(x).map((k) => JSON.stringify(k) + ":" + describe(x[k])).join(",") + "}";
};
const date = (ms) => new Date(ms);
const out = [];
`

// compare chạy mọi ca trong node rồi trong Go, và báo từng ca hai bên tả khác nhau.
func compare(t *testing.T, sources []string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cases := make([]testCase, len(sources))
	var script strings.Builder
	script.WriteString(nodePrelude)
	for name, f := range callbacks {
		script.WriteString("const " + name + " = " + f.node + ";\n")
	}
	for i, source := range sources {
		cases[i] = split(t, source)
		c := cases[i]
		described := "[r, t]"
		if _, global := javascript.Global(c.this); global {
			described = "[r]" // Math, JSON, Object không phải dữ liệu: chỉ tả kết quả
		}
		script.WriteString("try { const t = " + c.this + "; const r = t." + c.method + "(" + c.arguments +
			"); out.push(describe(" + described + ")); } catch (e) { out.push(\"throw \" + e.constructor.name); }\n")
	}
	script.WriteString("process.stdout.write(JSON.stringify(out));\n")
	path := filepath.Join(t.TempDir(), "cases.js")
	if err := os.WriteFile(path, []byte(script.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, path).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var want []string
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("node output: %v", err)
	}
	for i, c := range cases {
		want[i] = thrown(want[i])
		this, arguments := c.values(t)
		result := javascript.Invoke(run{}, this, c.method, arguments)
		got := "throw " + result.Code()
		switch _, global := javascript.Global(c.this); {
		case result.IsInvalid():
		case global:
			got = describe(value.Array(result))
		default:
			got = describe(value.Array(result, this))
		}
		if got != want[i] {
			message := ""
			if result.IsInvalid() {
				message = " (" + result.Error().Error() + ")"
			}
			t.Errorf("%s\n  node:  %s\n  go:    %s%s", c.source, want[i], got, message)
		}
	}
}
