package ui

import (
	"fmt"
	"strings"
)

// voidTags are the elements that never hold anything: a later </input> written by habit is accepted and
// dropped.
var voidTags = map[string]bool{"input": true}

// Parse reads a .ui file into its top-level nodes. It is written by hand on the standard library: .ui is a
// markup of its own, close to HTML and not HTML, so an HTML parser would accept what .ui refuses.
//
// What it reads:
//   - an element <name …>…</name>, or written closed, <name … />; a name of lower-case letters, digits, '-'
//     and '.', an upper-case one read as its lower case (Written keeps it for the compiler to name);
//   - an attribute name="value" or name='value', its value always quoted, or bare (required);
//   - text, where {{ … }} is one closed piece, so {{ a < b }} is no tag and a quote inside it is skipped;
//   - <!-- … -->, read and dropped.
//
// A line ends in LF, CRLF or CR alone, each read as one line break, as a browser reads an HTML document, so a
// block of code is the same whatever wrote the file. Every error says where it is, file:line:column.
func Parse(file string, source []byte) ([]*Node, error) {
	text := strings.ReplaceAll(strings.ReplaceAll(string(source), "\r\n", "\n"), "\r", "\n")
	p := &parser{file: file, source: text, line: 1, column: 1}
	return p.document()
}

// parser is one reading of one file: where it is, by byte, line and column.
type parser struct {
	file     string
	source   string
	position int
	line     int
	column   int
}

func (p *parser) document() ([]*Node, error) {
	root := &Node{Tag: "#root"}
	open := []*Node{root}
	for !p.end() {
		parent := open[len(open)-1]
		switch {
		case p.at("<!--"):
			if err := p.comment(); err != nil {
				return nil, err
			}
		case p.at("</"):
			closed, err := p.closing(open)
			if err != nil {
				return nil, err
			}
			if closed {
				open = open[:len(open)-1]
			}
		case p.at("<") && p.nameStartsAt(1):
			node, err := p.element()
			if err != nil {
				return nil, err
			}
			parent.Children = append(parent.Children, node)
			if !node.Closed {
				open = append(open, node)
			}
		default:
			node, err := p.text()
			if err != nil {
				return nil, err
			}
			parent.Children = append(parent.Children, node)
		}
	}
	if len(open) > 1 {
		innermost := open[len(open)-1]
		return nil, p.errorAt(innermost.Line, innermost.Column, "thẻ <"+innermost.Tag+"> chưa được đóng")
	}
	return root.Children, nil
}

func (p *parser) end() bool             { return p.position >= len(p.source) }
func (p *parser) at(s string) bool      { return strings.HasPrefix(p.source[p.position:], s) }
func (p *parser) errorf(m string) error { return p.errorAt(p.line, p.column, m) }

func (p *parser) errorAt(line, column int, message string) error {
	return &Error{File: p.file, Line: line, Column: column, Message: message}
}

// place is a place of the file as an error names another one, file:line:column.
func (p *parser) place(line, column int) string {
	if p.file == "" {
		return fmt.Sprintf("%d:%d", line, column)
	}
	return fmt.Sprintf("%s:%d:%d", p.file, line, column)
}

// advance moves n bytes on, counting lines and columns; a column counts characters, not bytes.
func (p *parser) advance(n int) {
	for i := 0; i < n && p.position < len(p.source); i++ {
		switch c := p.source[p.position]; {
		case c == '\n':
			p.line++
			p.column = 1
		case c&0xC0 != 0x80:
			p.column++
		}
		p.position++
	}
}

// nameStartsAt reports a letter offset bytes on: a tag's name begins there. An upper-case letter starts one
// too, so <Box> is read and the compiler can name it.
func (p *parser) nameStartsAt(offset int) bool {
	i := p.position + offset
	return i < len(p.source) && (p.source[i] >= 'a' && p.source[i] <= 'z' || p.source[i] >= 'A' && p.source[i] <= 'Z')
}

func (p *parser) skipSpace() {
	for !p.end() {
		switch p.source[p.position] {
		case ' ', '\t', '\n':
			p.advance(1)
		default:
			return
		}
	}
}

func (p *parser) comment() error {
	line, column := p.line, p.column
	end := strings.Index(p.source[p.position+4:], "-->")
	if end < 0 {
		return p.errorAt(line, column, "chú thích <!-- chưa được đóng bằng -->")
	}
	p.advance(4 + end + 3)
	return nil
}

// name reads a tag's name: letters, digits, '-' and '.'.
func (p *parser) name() string {
	start := p.position
	for !p.end() {
		c := p.source[p.position]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' {
			p.advance(1)
			continue
		}
		break
	}
	return p.source[start:p.position]
}

// closing reads </name> and reports whether it closes the innermost open element; a </input> after an
// <input>, which closes nothing, is read and dropped.
func (p *parser) closing(open []*Node) (bool, error) {
	line, column := p.line, p.column
	p.advance(2)
	written := p.name()
	p.skipSpace()
	if !p.at(">") {
		return false, p.errorAt(line, column, "thẻ đóng </"+written+" thiếu dấu >")
	}
	p.advance(1)
	tag := strings.ToLower(written)
	innermost := open[len(open)-1]
	if voidTags[tag] && innermost.Tag != tag {
		return false, nil
	}
	if innermost.Tag != tag {
		if innermost.Tag == "#root" {
			return false, p.errorAt(line, column, "thẻ đóng </"+written+"> không có thẻ mở")
		}
		return false, p.errorAt(line, column, fmt.Sprintf("thẻ đóng </%s> không khớp thẻ mở <%s> ở dòng %d", written, innermost.Tag, innermost.Line))
	}
	return true, nil
}

// attributeNameByte is a byte an attribute's name may hold: anything but space, '=', '>', '/' and the
// quotes, so the modifiers of an event (input:debounce(300)) and a prefix (medium:gap) stay in it.
func attributeNameByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '=', '>', '/', '"', '\'':
		return false
	}
	return true
}

// element reads <name attribute…> or <name attribute… />.
func (p *parser) element() (*Node, error) {
	line, column := p.line, p.column
	p.advance(1)
	written := p.name()
	node := &Node{Tag: written, File: p.file, Line: line, Column: column}
	if lower := strings.ToLower(written); lower != written {
		node.Tag, node.Written = lower, written
	}
	for {
		p.skipSpace()
		if p.end() {
			return nil, p.errorAt(line, column, "thẻ <"+written+"> chưa được đóng bằng >")
		}
		if p.at("/>") {
			p.advance(2)
			node.Closed = true
			return node, nil
		}
		if p.at(">") {
			p.advance(1)
			node.Closed = voidTags[node.Tag]
			return node, nil
		}
		attribute, err := p.attribute(written)
		if err != nil {
			return nil, err
		}
		for _, earlier := range node.Attributes {
			if earlier.Name == attribute.Name {
				return nil, p.errorAt(attribute.Line, attribute.Column, fmt.Sprintf("%s viết hai lần trên <%s> (lần đầu ở %s): giữ một", attribute.Name, node.Tag, p.place(earlier.Line, earlier.Column)))
			}
		}
		node.Attributes = append(node.Attributes, attribute)
	}
}

// attribute reads name="value", name='value' or a bare name.
func (p *parser) attribute(tag string) (Attribute, error) {
	line, column := p.line, p.column
	start := p.position
	for !p.end() && attributeNameByte(p.source[p.position]) {
		p.advance(1)
	}
	name := p.source[start:p.position]
	if name == "" {
		return Attribute{}, p.errorAt(line, column, fmt.Sprintf("ký tự không hợp lệ %q trong thẻ <%s>", p.source[p.position], tag))
	}
	p.skipSpace()
	if !p.at("=") {
		return Attribute{Name: name, Bare: true, Line: line, Column: column}, nil
	}
	p.advance(1)
	p.skipSpace()
	if p.end() || p.source[p.position] != '"' && p.source[p.position] != '\'' {
		return Attribute{}, p.errorf("giá trị của " + name + " phải đặt trong dấu nháy")
	}
	quote := p.source[p.position]
	p.advance(1)
	valueStart := p.position
	for !p.end() && p.source[p.position] != quote {
		p.advance(1)
	}
	if p.end() {
		return Attribute{}, p.errorAt(line, column, "giá trị của "+name+" thiếu dấu nháy đóng")
	}
	value := p.source[valueStart:p.position]
	p.advance(1)
	return Attribute{Name: name, Value: value, Line: line, Column: column}, nil
}

// text reads up to the next tag, a {{ … }} kept whole, so a '<' inside it ({{ a < b }}) starts no tag.
func (p *parser) text() (*Node, error) {
	line, column := p.line, p.column
	var b strings.Builder
	for !p.end() {
		if p.at("{{") {
			end := closingBraces(p.source[p.position+2:])
			if end < 0 {
				return nil, p.errorf("biểu thức {{ chưa được đóng bằng }}")
			}
			b.WriteString(p.source[p.position : p.position+2+end+2])
			p.advance(2 + end + 2)
			continue
		}
		if p.at("<!--") || p.at("</") || p.at("<") && p.nameStartsAt(1) {
			break
		}
		b.WriteByte(p.source[p.position])
		p.advance(1)
	}
	return &Node{Text: b.String(), File: p.file, Line: line, Column: column}, nil
}

// closingBraces is the offset of the "}}" that closes an expression in s, quoted text skipped; -1 for none.
func closingBraces(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '}':
			if i+1 < len(s) && s[i+1] == '}' {
				return i
			}
		}
	}
	return -1
}
