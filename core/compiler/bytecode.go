// Package compiler biến cây cú pháp (core/syntax) thành bytecode cho VM (core/runtime). Mỗi tên được phân giải thành một
// chỗ lúc biên dịch (scope.go), không tìm theo chuỗi lúc chạy: bài học của engine2, nơi LOAD/STORE theo tên chiếm
// khoảng 35% CPU (đo 03/11/2026). Tên chưa khai, gán vào const, dùng trước khai báo là lỗi biên dịch có dòng và cột.
package compiler

import (
	"sync/atomic"

	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Opcode là một lệnh của VM: tên một chữ, như bộ lệnh gốc của engine đầu tiên (engine/runtime/opcode.go, nửa trên):
// biến thể của một việc nằm ở toán hạng A, B của Instruction, không ở tên lệnh (owner 23/11/2026, sau khi bộ lệnh đầu
// của engine3 có 45 lệnh). Toán hạng 32 bit cố định, nên không có bản "rộng" (_WIDE) của lệnh nào.
type Opcode uint8

const (
	// --- Dữ liệu ---
	PUSH  Opcode = iota // B = Constant: đẩy Constants[A]; B = Undefined: đẩy undefined, không cần hằng
	POP                 // bỏ đỉnh
	DUP                 // chép A phần tử đỉnh (A = 1: a → a a; A = 2: a b → a b a b)
	LOAD                // đẩy biến A ở nơi B (Slot, Cell, Captured)
	STORE               // lấy đỉnh ghi vào biến A ở nơi B (FreshCell: một ô mới mang giá trị ấy)

	// --- Cấu trúc ---
	GET  // B = Named: obj → obj[Constants[A]]; B = Keyed: obj key → obj[key]
	SET  // B = Named: obj v → v, sau khi obj[Constants[A]] = v; B = Keyed: obj key v → v
	MAKE // A phần tử trên ngăn xếp → loại B: Array, Object (A cặp khoá, giá trị), Template (A phần nối thành chữ), Join (A mảng hay giá trị trải ra, nối thành một mảng: [...a, b]), Shaped (A là hằng của một value.Shape: các giá trị của các khoá của nó trên ngăn xếp, theo thứ tự, thành một object mượn khoá của Shape). Object có ... dựng dần theo thứ tự viết: Copy (o → một object mới có các khoá riêng của o, mượn khoá của nó, chỗ cho A khoá viết sau: { ...o }), Merge (target o → target, các khoá riêng của o viết vào: { a, ...o }), Extend (target và các giá trị của các khoá của Shape ở hằng A → target, các khoá ấy viết vào: { ...o, a, b })

	// --- Phép toán ---
	ADD     // a + b
	SUB     // a - b
	MUL     // a * b
	DIV     // a / b
	MOD     // a % b
	POW     // a ** b
	NOT     // !a
	UNARY   // A: Negate (-a), Plus (+a), Typeof (typeof a)
	COMPARE // a b → boolean, A: Less, Greater, LessEqual, GreaterEqual, Same (===), Different (!==)

	// --- Nhảy ---
	JUMP  // nhảy tới A
	TRUE  // B = Pop: lấy đỉnh, đúng thì nhảy tới A; B = Keep: đỉnh đúng thì nhảy, giữ nó, không thì bỏ nó (||); B = Present: đỉnh không phải undefined, null thì nhảy, giữ nó, không thì bỏ nó (??)
	ITER  // for…of: slot B là danh sách, slot B+1 là chỉ số; còn phần tử thì đẩy nó và tăng chỉ số, hết thì nhảy tới A. Danh sách không phải mảng là TypeError
	FALSE // B = Pop: lấy đỉnh, sai thì nhảy tới A; B = Keep: đỉnh sai thì nhảy, giữ nó, không thì bỏ nó (&&)

	// --- Thực thi ---
	CALL   // f và A đối số → kết quả
	INVOKE // this và A đối số → this[Constants[B]](…)
	LAMBDA // đẩy closure của Functions[A]
	RETURN // trả đỉnh
)

// Toán hạng B của PUSH: undefined không chiếm một hằng của mỗi hàm (mọi hàm kết thúc bằng PUSH undefined, RETURN).
const (
	Constant  = 0
	Undefined = 1
)

// Toán hạng B của TRUE, FALSE: && || ?? là một lệnh nhảy giữ vế trái. Đo 23/11/2026 trên một guard kiểu thật: nhanh
// hơn ~25% so với ghép từ DUP, FALSE, POP như bộ lệnh gốc. Mỗi cặp (lệnh, B) có một hiệu ứng ngăn xếp cố định.
const (
	Pop     = 0
	Keep    = 1
	Present = 2 // chỉ TRUE
)

// Nơi của một biến (toán hạng B của LOAD, STORE).
const (
	Slot      = 0 // slot của khung
	Cell      = 1 // ô của khung: biến một hàm con bắt
	Captured  = 2 // ô closure bắt từ khung cha
	FreshCell = 3 // chỉ STORE: tạo ô mới cho khung, mang giá trị đỉnh (đầu một block có biến bị bắt)
)

// Cách lấy khoá của GET, SET (toán hạng B).
const (
	Named = 0 // khoá là Constants[A]: obj.name
	Keyed = 1 // khoá trên ngăn xếp: obj[key]
)

// Loại của MAKE (toán hạng B).
const (
	Array    = 0
	Object   = 1
	Template = 2
	Join     = 3
	Merge    = 4
	Shaped   = 5
	Copy     = 6
	Extend   = 7
)

// Toán hạng A của UNARY.
const (
	Negate = 0
	Plus   = 1
	Typeof = 2
)

// Toán hạng A của COMPARE.
const (
	Less         = 0
	Greater      = 1
	LessEqual    = 2
	GreaterEqual = 3
	Same         = 4 // ===
	Different    = 5 // !==
)

var opcodeNames = [...]string{
	"PUSH", "POP", "DUP", "LOAD", "STORE", "GET", "SET", "MAKE", "ADD", "SUB", "MUL", "DIV", "MOD", "POW", "NOT", "UNARY",
	"COMPARE", "JUMP", "TRUE", "ITER", "FALSE", "CALL", "INVOKE", "LAMBDA", "RETURN",
}

func (op Opcode) String() string {
	if int(op) < len(opcodeNames) {
		return opcodeNames[op]
	}
	return "?"
}

// Instruction là một lệnh và hai toán hạng.
type Instruction struct {
	Op   Opcode
	A, B int32
}

// Function là một hàm đã biên dịch: thân của file (application.work) là một Function không tham số.
type Function struct {
	Name       string // tên của biến giữ nó (const f = …), "" khi không có
	File       string
	Pos        syntax.Pos // chỗ của hàm trong file: dấu ( hay tham số đầu của một hàm mũi tên; 1:1 cho thân file
	Code       []Instruction
	Positions  []syntax.Pos // chỗ của mỗi lệnh, cho lỗi lúc chạy
	Constants  []value.Value
	Functions  []*Function // các hàm con, LAMBDA A
	Parameters int         // số tham số: slot 0 … Parameters-1
	Locals     int         // số slot của khung, tham số gồm cả
	Cells      int         // số ô của khung (biến bị hàm con bắt)
	Captures   []Capture   // mỗi ô closure của hàm này bắt, lấy từ khung cha khi LAMBDA chạy
	// Syntax là cây cú pháp của một hàm mũi tên, nil cho thân file: thứ đọc được thân hàm mà không chạy nó (điều kiện
	// của một câu lệnh database dịch sang SQL, host/database) đọc nó. VM không dùng.
	Syntax *syntax.Arrow

	// Resolved là thứ runtime tra sẵn cho hàm này (tên phương thức của các lệnh INVOKE, core/runtime): dựng một lần khi
	// hàm gọi phương thức lần đầu, rồi mọi lượt chạy đọc chung (atomic: các request của một site chạy cùng lúc). Compiler
	// không đặt, không đọc nó.
	Resolved atomic.Value
}

// Capture là một ô hàm con bắt từ khung cha (khi LAMBDA chạy): ô Index của khung cha (FromCell), hay ô bắt được Index của closure cha.
type Capture struct {
	FromCell bool
	Index    int
	Name     string // tên của biến trong code: thứ đọc thân hàm (Syntax) tìm giá trị của một tên bắt được
}
