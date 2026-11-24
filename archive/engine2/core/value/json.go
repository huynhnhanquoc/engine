package value

import (
	"strconv"
	"unicode/utf8"
)

// JSON both ways, as JSON.stringify and JSON.parse do: a number that is not finite writes null,
// undefined and a function are left out of an object and write null in an array, keys keep the
// object's order, a value that holds itself is a TYPE failure, and a text that is not JSON is an
// INVALID_INPUT failure that says where. A time writes as toISOString, a duration as its text, bytes
// as base64, a struct as {}.

// Stringify is JSON.stringify(v, null, indent): the JSON text as a String, undefined for a value that
// has no JSON (undefined, a function), a failure for a cycle. No run counts what it writes, so it writes
// at most UnbudgetedLength (a module writing a value as JSON); a run's JSON.stringify is StringifyWithin.
func Stringify(v Value, indent string) Value {
	return StringifyWithin(v, indent, nil)
}

// StringifyWithin is Stringify by a run whose budget is budget, asked as the text grows: an array holding one
// text a thousand times is that text a thousand times in JSON, made from what the run holds once.
func StringifyWithin(v Value, indent string, budget Budget) Value {
	return stringify(v, indent, budget, jsonPlain)
}

// StringifyCalling is a program's JSON.stringify(v, null, indent) (18/11/2026): StringifyWithin that calls an
// object's toJSON with its key ("" for v, an index's text in an array) and writes what it gives, as JavaScript's
// SerializeJSONProperty does, once a value (what toJSON gives is written as it is, its own toJSON not called;
// the values inside it are). An object with one was written as its keys, the function left out ({ toJSON: () => 5 }
// was "{}"). Go code that writes a value as JSON calls nothing of a program.
//
// The value is written in place first, as StringifyWithin writes it, nothing of a program running: the writer passes
// over a function anyway, and when one stands under the key toJSON it stops, and the value is written again calling
// each toJSON, each object's keys from a copy read one by one as JavaScript reads them (a toJSON may write to an
// object being written). A value without one costs what it did; writing every value the second way made the text
// twice as slow, and walking it first to ask cost a twentieth. A failure of the first writing is written the second
// way too, so a toJSON further on still has its turn.
func StringifyCalling(v Value, indent string, budget Budget) Value {
	if written := stringify(v, indent, budget, jsonProbe); written.kind != invalid {
		return written
	}
	// a toJSON met, or a failure a toJSON not yet met may have spared (one that cuts a value holding itself)
	return stringify(v, indent, budget, jsonCalling)
}

// jsonMode is how the JSON writer meets a toJSON: jsonPlain writes past it as any function, jsonProbe stops at it
// (metToJSON), jsonCalling calls it.
type jsonMode uint8

const (
	jsonPlain jsonMode = iota
	jsonProbe
	jsonCalling
)

// metToJSON is what the writer stops with in jsonProbe when it meets a toJSON, never a program's failure.
var metToJSON = Fail(TypeCode, "JSON: a toJSON met")

func stringify(v Value, indent string, budget Budget, mode jsonMode) Value {
	if v.kind == invalid {
		return v
	}
	if mode == jsonCalling {
		if v = jsonValue(v, ""); v.kind == invalid {
			return v
		}
	}
	if v.kind == 0 || v.kind == function {
		return Value{}
	}
	lim := limit{budget: budget, work: 32} // a byte written a unit of energy (work.go)
	b, bad := appendJSON(make([]byte, 0, 64), v, indent, "", nil, &lim, mode)
	if bad.kind == invalid {
		return bad
	}
	// each text is checked before it is written and each item after; what closes the last (a bracket, a
	// newline) is checked here
	if bad := lim.allow(len(b)); bad.kind == invalid {
		return bad
	}
	if spent := lim.finish(len(b)); spent.kind == invalid {
		return spent
	}
	return String(string(b))
}

// jsonValue is what JSON writes of v under key in jsonCalling: what its toJSON gives when v is an object whose
// toJSON is a function, v itself otherwise.
func jsonValue(v Value, key string) Value {
	if v.kind != object {
		return v
	}
	if method := v.Get("toJSON"); method.kind == function {
		return method.Call([]Value{String(key)})
	}
	return v
}

func appendJSON(b []byte, v Value, indent, prefix string, active []any, lim *limit, mode jsonMode) ([]byte, Value) {
	switch v.kind {
	case invalid:
		return b, v
	case 0, null, function:
		return append(b, "null"...), Value{}
	case boolean:
		if v.number == 1 {
			return append(b, "true"...), Value{}
		}
		return append(b, "false"...), Value{}
	case number:
		f := v.Number()
		if f != f || f > 1.7976931348623157e308 || f < -1.7976931348623157e308 {
			return append(b, "null"...), Value{}
		}
		return appendNumber(b, f), Value{}
	case text:
		// before the text is written, as the items of an array and an object are checked after theirs
		if bad := lim.allow(jsonStringNeed(len(b), v.Text(), lim.granted)); bad.kind == invalid {
			return b, bad
		}
		return appendJSONString(b, v.Text()), Value{}
	case instant:
		return appendJSONString(b, v.ToISOString().Text()), Value{}
	case duration:
		return appendJSONString(b, v.Duration().String()), Value{}
	case bytes:
		if bad := lim.allow(len(b) + (len(v.Bytes())+2)/3*4 + 2); bad.kind == invalid {
			return b, bad
		}
		return appendJSONString(b, base64Text(v.Bytes())), Value{}
	case host:
		return append(b, "{}"...), Value{}
	case array:
		items := v.data.(*list)
		for _, seen := range active {
			if seen == any(items) {
				return b, Fail(TypeCode, "JSON: the array holds itself")
			}
		}
		active = append(active, any(items))
		if len(items.items) == 0 {
			return append(b, "[]"...), Value{}
		}
		inner := prefix + indent
		b = append(b, '[')
		for i, item := range items.items {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendNewline(b, indent, inner)
			if mode == jsonCalling {
				if item = jsonValue(item, strconv.Itoa(i)); item.kind == invalid {
					return b, item
				}
			}
			var bad Value
			if b, bad = appendJSON(b, item, indent, inner, active, lim, mode); bad.kind == invalid {
				return b, bad
			}
			if bad = lim.allow(len(b)); bad.kind == invalid {
				return b, bad
			}
		}
		b = appendNewline(b, indent, prefix)
		return append(b, ']'), Value{}
	case object:
		if library := v.Library(); library != "" {
			return b, LibraryAsData(library, "JSON.stringify("+library+")")
		}
		o := v.properties()
		o.order()
		for _, seen := range active {
			if seen == any(o) {
				return b, Fail(TypeCode, "JSON: the object holds itself")
			}
		}
		active = append(active, any(o))
		inner := prefix + indent
		b = append(b, '{')
		written := 0
		keys := o.keys
		if mode == jsonCalling && !o.frozen {
			// a toJSON is program code that may write to this object while it is written: its keys as they were, each
			// read when its turn comes, as JavaScript reads them
			keys = append([]string(nil), o.keys...)
		}
		for i, key := range keys {
			var item Value
			if mode == jsonCalling {
				if item = jsonValue(v.Get(key), key); item.kind == invalid {
					return b, item
				}
			} else {
				item = o.items[i]
			}
			if item.kind == function && mode == jsonProbe && key == "toJSON" {
				return b, metToJSON // written again, calling it (StringifyCalling)
			}
			if item.kind == 0 || item.kind == function {
				continue
			}
			if written > 0 {
				b = append(b, ',')
			}
			written++
			b = appendNewline(b, indent, inner)
			if bad := lim.allow(jsonStringNeed(len(b), key, lim.granted)); bad.kind == invalid {
				return b, bad
			}
			b = appendJSONString(b, key)
			b = append(b, ':')
			if indent != "" {
				b = append(b, ' ')
			}
			var bad Value
			if b, bad = appendJSON(b, item, indent, inner, active, lim, mode); bad.kind == invalid {
				return b, bad
			}
			if bad = lim.allow(len(b)); bad.kind == invalid {
				return b, bad
			}
		}
		if written > 0 {
			b = appendNewline(b, indent, prefix)
		}
		return append(b, '}'), Value{}
	}
	return append(b, "null"...), Value{}
}

func appendNewline(b []byte, indent, prefix string) []byte {
	if indent == "" {
		return b
	}
	b = append(b, '\n')
	return append(b, prefix...)
}

const hexLower = "0123456789abcdef"

// jsonStringNeed is how long a JSON text is once s is written after written bytes of it, quoted as
// appendJSONString writes it, for a limit that allowed granted. An escape writes up to six bytes for one (a
// control character's), so a text of a hundred million of them made a JSON text of six hundred million bytes,
// four gigabytes of growth for one instruction, though the text was checked before it was written: it was
// checked as it was, not as it is written (05/11/2026). The escaped length is counted only for a text that
// could pass what is allowed once escaped.
func jsonStringNeed(written int, s string, granted int) int {
	if written+6*len(s)+2 <= granted {
		return written + len(s) + 2
	}
	return written + jsonStringLength(s)
}

// jsonStringLength is how many bytes appendJSONString writes for s, its quotes included.
func jsonStringLength(s string) int {
	length := 2
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
			length += 2
		case c < 0x20:
			length += 6
		default:
			if _, ok := loneSurrogateAt(s, i); ok {
				// the three bytes of its WTF-8 form are the six of its escape
				length += 6
				i += 2
				continue
			}
			length++
		}
	}
	return length
}

// appendJSONString quotes s as JSON.stringify does: the short escapes, \u00XX for other control
// characters, and \udXXX for a lone surrogate so the output is well-formed JSON.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
			i++
		case c == '\n':
			b = append(b, '\\', 'n')
			i++
		case c == '\r':
			b = append(b, '\\', 'r')
			i++
		case c == '\t':
			b = append(b, '\\', 't')
			i++
		case c == '\b':
			b = append(b, '\\', 'b')
			i++
		case c == '\f':
			b = append(b, '\\', 'f')
			i++
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hexLower[c>>4], hexLower[c&0xF])
			i++
		case c < utf8.RuneSelf:
			b = append(b, c)
			i++
		default:
			if unit, ok := loneSurrogateAt(s, i); ok {
				b = append(b, '\\', 'u', hexLower[unit>>12], hexLower[unit>>8&0xF], hexLower[unit>>4&0xF], hexLower[unit&0xF])
				i += 3
				continue
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			b = append(b, s[i:i+size]...)
			i += size
		}
	}
	return append(b, '"')
}

const base64Digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func base64Text(data []byte) string {
	out := make([]byte, 0, (len(data)+2)/3*4)
	for i := 0; i < len(data); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], data[i:])
		out = append(out, base64Digits[chunk[0]>>2], base64Digits[chunk[0]&3<<4|chunk[1]>>4])
		if n > 1 {
			out = append(out, base64Digits[chunk[1]&15<<2|chunk[2]>>6])
		} else {
			out = append(out, '=')
		}
		if n > 2 {
			out = append(out, base64Digits[chunk[2]&63])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}

// ---- parse ----

// maxJSONDepth bounds nesting so a text of ten thousand "[" is a failure, not a stack overflow.
const maxJSONDepth = 512

// ParseJSON is JSON.parse(text): the value, or an INVALID_INPUT failure naming the byte it stopped at.
func ParseJSON(text string) Value {
	return ParseJSONWithin(text, nil)
}

// ParseJSONWithin is ParseJSON by a run whose budget is budget, asked as the tree grows: a text of "[]," made
// an array of some 80 bytes for each 3 of its own, three hundred megabytes from a text a run held as ten.
//
// What it reads costs the run's energy before it begins, jsonParseUnits a byte (work.go).
func ParseJSONWithin(text string, budget Budget) Value {
	if spent := Spend(budget, uint64(len(text))*jsonParseUnits); spent.kind == invalid {
		return spent
	}
	p := jsonParser{text: text, lim: limit{budget: budget, items: true}}
	p.skipSpace()
	v := p.value(0)
	if v.kind == invalid {
		return v
	}
	p.skipSpace()
	if p.at < len(p.text) {
		return p.fail("unexpected text after the value")
	}
	return v
}

type jsonParser struct {
	text string
	at   int
	// made is what the tree takes so far, as the runtime counts it once parse returns (Footprint), which lim
	// allows as it grows
	made int
	lim  limit
}

// grew counts n bytes more of the tree; the failure once past what the limit allows.
func (p *jsonParser) grew(n int) Value {
	p.made += n
	return p.lim.allow(p.made)
}

func (p *jsonParser) fail(what string) Value {
	return Fail(InvalidInputCode, "JSON: "+what+" at byte "+strconv.Itoa(p.at))
}

func (p *jsonParser) skipSpace() {
	for p.at < len(p.text) {
		switch p.text[p.at] {
		case ' ', '\t', '\n', '\r':
			p.at++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) Value {
	if depth > maxJSONDepth {
		return p.fail("nested too deep")
	}
	if p.at >= len(p.text) {
		return p.fail("unexpected end")
	}
	switch c := p.text[p.at]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, ok := p.string()
		if !ok {
			return p.fail("bad string")
		}
		if bad := p.grew(len(s)); bad.kind == invalid {
			return bad
		}
		return String(s)
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case p.literal("true"):
		return Boolean(true)
	case p.literal("false"):
		return Boolean(false)
	case p.literal("null"):
		return Null()
	}
	return p.fail("unexpected character " + strconv.QuoteRune(rune(p.text[p.at])))
}

func (p *jsonParser) literal(word string) bool {
	if len(p.text)-p.at >= len(word) && p.text[p.at:p.at+len(word)] == word {
		p.at += len(word)
		return true
	}
	return false
}

func (p *jsonParser) number() Value {
	start := p.at
	if p.text[p.at] == '-' {
		p.at++
	}
	digits := func() int {
		from := p.at
		for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
			p.at++
		}
		return p.at - from
	}
	whole := digits()
	if whole == 0 || (whole > 1 && p.text[start+boolInt(p.text[start] == '-')] == '0') {
		return p.fail("bad number")
	}
	if p.at < len(p.text) && p.text[p.at] == '.' {
		p.at++
		if digits() == 0 {
			return p.fail("bad number")
		}
	}
	if p.at < len(p.text) && (p.text[p.at] == 'e' || p.text[p.at] == 'E') {
		p.at++
		if p.at < len(p.text) && (p.text[p.at] == '+' || p.text[p.at] == '-') {
			p.at++
		}
		if digits() == 0 {
			return p.fail("bad number")
		}
	}
	f, _ := strconv.ParseFloat(p.text[start:p.at], 64)
	return Number(f)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// string reads a quoted string with its escapes; \u escapes that make a pair join into one
// character, a lone one keeps its three-byte form.
func (p *jsonParser) string() (string, bool) {
	p.at++ // the opening quote
	var b []byte
	start := p.at
	for p.at < len(p.text) {
		c := p.text[p.at]
		switch {
		case c == '"':
			if b == nil {
				s := p.text[start:p.at]
				p.at++
				return s, true
			}
			b = append(b, p.text[start:p.at]...)
			p.at++
			return string(b), true
		case c < 0x20:
			return "", false
		case c == '\\':
			b = append(b, p.text[start:p.at]...)
			p.at++
			if p.at >= len(p.text) {
				return "", false
			}
			switch p.text[p.at] {
			case '"', '\\', '/':
				b = append(b, p.text[p.at])
			case 'b':
				b = append(b, '\b')
			case 'f':
				b = append(b, '\f')
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'u':
				unit, ok := p.hex4(p.at + 1)
				if !ok {
					return "", false
				}
				p.at += 4
				if unit >= 0xD800 && unit < 0xDC00 && p.at+6 < len(p.text) && p.text[p.at+1] == '\\' && p.text[p.at+2] == 'u' {
					if low, ok := p.hex4(p.at + 3); ok && low >= 0xDC00 && low < 0xE000 {
						b = append(b, encodeUnits([]uint16{unit, low})...)
						p.at += 6
						break
					}
				}
				b = appendUnit(b, unit)
			default:
				return "", false
			}
			p.at++
			start = p.at
		default:
			p.at++
		}
	}
	return "", false
}

func (p *jsonParser) hex4(at int) (uint16, bool) {
	if at+4 > len(p.text) {
		return 0, false
	}
	var unit uint16
	for i := 0; i < 4; i++ {
		d := digitValue(p.text[at+i])
		if d >= 16 {
			return 0, false
		}
		unit = unit<<4 | uint16(d)
	}
	return unit, true
}

func (p *jsonParser) array(depth int) Value {
	p.at++ // [
	if bad := p.grew(MadeBytes); bad.kind == invalid {
		return bad
	}
	items := []Value{}
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == ']' {
		p.at++
		return Array(items)
	}
	for {
		p.skipSpace()
		item := p.value(depth + 1)
		if item.kind == invalid {
			return item
		}
		if bad := p.grew(int(ItemBytes)); bad.kind == invalid {
			return bad
		}
		items = append(items, item)
		p.skipSpace()
		if p.at >= len(p.text) {
			return p.fail("unexpected end")
		}
		switch p.text[p.at] {
		case ',':
			p.at++
		case ']':
			p.at++
			return Array(items)
		default:
			return p.fail("expected , or ]")
		}
	}
}

func (p *jsonParser) object(depth int) Value {
	p.at++ // {
	if bad := p.grew(MadeBytes); bad.kind == invalid {
		return bad
	}
	o := Object()
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == '}' {
		p.at++
		return o
	}
	for {
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != '"' {
			return p.fail("expected a key")
		}
		key, ok := p.string()
		if !ok {
			return p.fail("bad key")
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ':' {
			return p.fail("expected :")
		}
		p.at++
		p.skipSpace()
		item := p.value(depth + 1)
		if item.kind == invalid {
			return item
		}
		if bad := p.grew(int(KeyBytes) + len(key)); bad.kind == invalid {
			return bad
		}
		o.Set(key, item)
		p.skipSpace()
		if p.at >= len(p.text) {
			return p.fail("unexpected end")
		}
		switch p.text[p.at] {
		case ',':
			p.at++
		case '}':
			p.at++
			return o
		default:
			return p.fail("expected , or }")
		}
	}
}
