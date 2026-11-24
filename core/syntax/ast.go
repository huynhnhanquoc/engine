package syntax

// Cây cú pháp: mỗi nút nhớ chỗ của nó (At), để compiler và runtime nói lỗi ở đúng dòng và cột.

// Node là một nút của cây.
type Node interface{ At() Pos }

// Program là một file: các import, rồi các câu.
type Program struct {
	File    string
	Imports []*Import
	Body    []Statement
}

// At là đầu file: thân file là một hàm, như một hàm mũi tên.
func (p *Program) At() Pos { return Pos{Line: 1, Column: 1} }

// Import là import { a, b as c } from "application", hay import * as app from "application".
type Import struct {
	Pos       Pos
	From      string
	Names     []ImportName // rỗng khi Namespace
	Namespace string       // * as tên
}

// ImportName là một tên import: Name của module, Local trong file (b as c: Name b, Local c).
type ImportName struct {
	Pos         Pos
	Name, Local string
}

// --- Câu ---

type Statement interface {
	Node
	statement()
}

// Declaration là const x = …, let x = …, let x; hay const { a, b: c } = … (Pattern khác nil, Name rỗng).
type Declaration struct {
	Pos     Pos
	Const   bool
	Export  bool // export const x = …: một tên file khác import được
	Name    string
	Pattern *ObjectPattern // giải cấu trúc; nil khi khai một tên
	Init    Expression     // nil: let x;
}

// ObjectPattern là { a, b: c, d = 1, e: { f } } bên trái một khai báo.
type ObjectPattern struct {
	Pos        Pos
	Properties []PatternProperty
}

// PatternProperty là một khoá của ObjectPattern: khoá đọc, rồi tên nhận nó (Name) hay một mẫu lồng (Pattern), và giá
// trị mặc định khi khoá đọc ra undefined.
type PatternProperty struct {
	Pos     Pos
	Key     string
	Name    string
	Pattern *ObjectPattern
	Default Expression
}

// For là for (let i = 0; i < n; i++) …: dạng đếm, đủ ba phần.
type For struct {
	Pos    Pos
	Init   *Declaration
	Test   Expression
	Update Expression
	Body   Statement
}

// ForOf là for (const x of list) …, hay for (const { a } of list) ….
type ForOf struct {
	Pos      Pos
	Name     string
	Pattern  *ObjectPattern
	Iterable Expression
	Body     Statement
}

// Break là break; Continue là continue; (không nhãn).
type Break struct{ Pos Pos }
type Continue struct{ Pos Pos }

// Names là các tên một mẫu khai, theo thứ tự viết.
func (p *ObjectPattern) Names() []PatternProperty {
	var names []PatternProperty
	for _, property := range p.Properties {
		if property.Pattern != nil {
			names = append(names, property.Pattern.Names()...)
		} else {
			names = append(names, property)
		}
	}
	return names
}

// Return là return …, hay return không giá trị (Value nil).
type Return struct {
	Pos   Pos
	Value Expression
}

// If là if (…) … else ….
type If struct {
	Pos        Pos
	Condition  Expression
	Then, Else Statement // Else nil khi không có
}

// Block là { … }: một tầng tên của let và const.
type Block struct {
	Pos  Pos
	Body []Statement
}

// ExpressionStatement là một biểu thức đứng làm câu: gọi hàm, gán.
type ExpressionStatement struct {
	Pos   Pos
	Value Expression
}

func (s *For) At() Pos                 { return s.Pos }
func (s *ForOf) At() Pos               { return s.Pos }
func (s *Break) At() Pos               { return s.Pos }
func (s *Continue) At() Pos            { return s.Pos }
func (*For) statement()                {}
func (*ForOf) statement()              {}
func (*Break) statement()              {}
func (*Continue) statement()           {}
func (s *Declaration) At() Pos         { return s.Pos }
func (s *Return) At() Pos              { return s.Pos }
func (s *If) At() Pos                  { return s.Pos }
func (s *Block) At() Pos               { return s.Pos }
func (s *ExpressionStatement) At() Pos { return s.Pos }

func (*Declaration) statement()         {}
func (*Return) statement()              {}
func (*If) statement()                  {}
func (*Block) statement()               {}
func (*ExpressionStatement) statement() {}

// --- Biểu thức ---

type Expression interface {
	Node
	expression()
}

// Identifier là một tên.
type Identifier struct {
	Pos  Pos
	Name string
}

// Literal là undefined, null, true, false, một số hay một chuỗi.
type Literal struct {
	Pos   Pos
	Kind  LiteralKind
	Num   float64
	Text  string
	Truth bool
}

// LiteralKind là loại của một Literal.
type LiteralKind int

const (
	UndefinedLiteral LiteralKind = iota
	NullLiteral
	BooleanLiteral
	NumberLiteral
	StringLiteral
)

// Template là `a${b}c`: Parts có một phần hơn Values (Parts[0] Values[0] Parts[1] …).
type Template struct {
	Pos    Pos
	Parts  []string
	Values []Expression
}

// Array là [a, b].
type Array struct {
	Pos   Pos
	Items []Expression
}

// Object là { a: 1, b, "c": 2 }.
type Object struct {
	Pos        Pos
	Properties []Property
}

// Property là một khoá của Object: key: value, hay key (Shorthand, value là tên cùng chữ).
type Property struct {
	Pos       Pos
	Key       string
	Value     Expression
	Shorthand bool
}

// Member là a.b.
type Member struct {
	Pos    Pos
	Object Expression
	Name   string
}

// Index là a[b].
type Index struct {
	Pos    Pos
	Object Expression
	Index  Expression
}

// Call là f(…), hay a.f(…) khi Callee là một Member (gọi phương thức: this là a).
type Call struct {
	Pos       Pos
	Callee    Expression
	Arguments []Expression
}

// Arrow là (a, b) => … : Body là một Expression, hay một *Block.
type Arrow struct {
	Pos        Pos
	Parameters []Parameter
	Body       Node
}

// Parameter là một tham số: tên, và giá trị mặc định (nil khi không có).
type Parameter struct {
	Pos     Pos
	Name    string
	Default Expression
}

// Unary là !a, -a, +a, typeof a.
type Unary struct {
	Pos     Pos
	Op      string
	Operand Expression
}

// Binary là a + b, a === b, a < b…; Logical là a && b, a || b, a ?? b (vế phải chỉ chạy khi cần).
type Binary struct {
	Pos         Pos
	Op          string
	Left, Right Expression
}

type Logical struct {
	Pos         Pos
	Op          string
	Left, Right Expression
}

// Conditional là a ? b : c.
type Conditional struct {
	Pos                        Pos
	Condition, Then, Otherwise Expression
}

// Update là a++, a--, ++a, --a (Target là *Identifier, *Member, *Index).
type Update struct {
	Pos    Pos
	Op     string // "++" hay "--"
	Prefix bool
	Target Expression
}

// Spread là ...x trong một mảng ([...a, b]) hay một object ({ ...a, b }).
type Spread struct {
	Pos   Pos
	Value Expression
}

// Assign là a = b, a.b = c, a[b] = c, và a += b… (Op là "=" hay "+=", "-="…).
type Assign struct {
	Pos    Pos
	Op     string
	Target Expression // *Identifier, *Member, *Index
	Value  Expression
}

func (e *Identifier) At() Pos  { return e.Pos }
func (e *Literal) At() Pos     { return e.Pos }
func (e *Template) At() Pos    { return e.Pos }
func (e *Array) At() Pos       { return e.Pos }
func (e *Object) At() Pos      { return e.Pos }
func (e *Member) At() Pos      { return e.Pos }
func (e *Index) At() Pos       { return e.Pos }
func (e *Call) At() Pos        { return e.Pos }
func (e *Arrow) At() Pos       { return e.Pos }
func (e *Unary) At() Pos       { return e.Pos }
func (e *Binary) At() Pos      { return e.Pos }
func (e *Logical) At() Pos     { return e.Pos }
func (e *Conditional) At() Pos { return e.Pos }
func (e *Assign) At() Pos      { return e.Pos }
func (e *Update) At() Pos      { return e.Pos }
func (e *Spread) At() Pos      { return e.Pos }
func (*Update) expression()    {}
func (*Spread) expression()    {}

func (*Identifier) expression()  {}
func (*Literal) expression()     {}
func (*Template) expression()    {}
func (*Array) expression()       {}
func (*Object) expression()      {}
func (*Member) expression()      {}
func (*Index) expression()       {}
func (*Call) expression()        {}
func (*Arrow) expression()       {}
func (*Unary) expression()       {}
func (*Binary) expression()      {}
func (*Logical) expression()     {}
func (*Conditional) expression() {}
func (*Assign) expression()      {}
