package runtime

// Opcode đại diện cho một chỉ thị bytecode trong Kitwork Engine (stack-based VM).
type Opcode uint8

const (
	// =====================
	// DATA FLOW
	// =====================

	PUSH    Opcode = iota // Đẩy literal/hằng số lên Stack
	POP                   // Loại bỏ giá trị trên đỉnh Stack
	LOAD                  // Tải giá trị từ Vars/Context lên Stack (theo index)
	STORE                 // Lưu giá trị từ Stack vào Vars/Context (theo index)
	GET                   // Lấy thuộc tính/phần tử (obj.prop)
	DUP                   // Sao chép giá trị trên cùng của stack
	BUILTIN               // Tải hàm hệ thống theo index (Siêu nhanh)

	// =====================
	// ARITHMETIC
	// =====================

	ADD // Cộng (+)
	SUB // Trừ (-)
	MUL // Nhân (*)
	DIV // Chia (/)

	// =====================
	// LOGIC
	// =====================

	AND // Logic AND: chọn toán hạng khi hai vế đã tính (compiler chỉ phát khi vế phải là hằng hay tên; && đoản mạch bằng DUP/FALSE/POP)
	OR  // Logic OR: như AND (|| đoản mạch bằng DUP/TRUE/POP)
	NOT // Logic NOT (Đảo ngược boolean)

	// =====================
	// CONTROL FLOW
	// =====================

	COMPARE // So sánh (==, !=, <, >, ...) - hành vi định nghĩa qua operand
	JUMP    // Nhảy không điều kiện đến Instruction Pointer
	TRUE    // Nhảy nếu đỉnh Stack là True
	FALSE   // Nhảy nếu đỉnh Stack là False
	ITER    // Vòng lặp tối ưu: Kiểm tra + Lấy phần tử + Nhảy
	HALT    // Dừng thực thi ngay lập tức (Cưỡng bức)
	YIELD   // Tạm dừng thực thi (Nhường tài nguyên/Hồi phục)

	// =====================
	// STRUCTURE
	// =====================

	MAKE  // Tạo cấu trúc dữ liệu mới (Map, Array từ Pool)
	SET   // Gán giá trị vào cấu trúc (target, key, value → value: giá trị vừa gán, như o.x = v của JS)
	MERGE // Gộp các phần tử từ object nguồn vào object đích (Spread)

	// =====================
	// EXECUTION
	// =====================

	CALL   // Gọi hàm host (Go) / Builtin
	INVOKE // Gọi phương thức đối tượng (obj.method)
	LAMBDA // Khởi tạo/Kích hoạt hàm ẩn danh
	RETURN // Kết thúc hàm, dọn dép stack frame
	DEFER  // Đảm bảo dọn dẹp/hoàn trả năng lượng khi kết thúc
	SPAWN  // Chạy lambda trong Goroutine riêng (Fire & Forget)

	// Appended opcodes keep the numeric values of the original instruction set stable.
	MOD     // Remainder (%)
	_COMMIT // Reserved former COMMIT slot (removed in bytecode 6: nothing emitted it); never execute or reuse it.

	_RESERVED // Reserved former POPFINSOFT numeric slot; never execute or reuse it.

	// Wide forms (bytecode 3, 14/10/2026): the same instruction with a three-byte operand, for a
	// constant index above 65,535 or a jump in a program longer than 65,535 bytes. A program that fits
	// two-byte operands never holds one (the verifier refuses it), so it is written as in bytecode 2.
	PUSH_WIDE
	LOAD_WIDE
	STORE_WIDE
	JUMP_WIDE
	TRUE_WIDE
	FALSE_WIDE
	ITER_WIDE

	// STEP (bytecode 4, 15/10/2026): ++ and -- make a number of what they step, as JavaScript's
	// ToNumber does ("5" is 5, null is 0, "a" is NaN), then step it. Its one-byte operand is the step:
	// StepNumber, StepIncrement, StepDecrement or (bytecode 6) StepNegate. x ± 1 joined a string ("5"++ was "51") and refused
	// null, a boolean or a date for --.
	STEP

	// Reserved former STORE_LOCAL and STORE_LOCAL_WIDE (bytecode 6: STORE of a function's own variable
	// into its map, by name); bytecode 7 keeps a function's own variables in slots (LOAD_SLOT), so
	// nothing emits them. Never execute or reuse them.
	_STORE_LOCAL
	_STORE_LOCAL_WIDE

	// TYPEOF (bytecode 6) is typeof x: the word JavaScript's typeof gives for the value on top of the
	// stack (value.Typeof). It compiled to x; PUSH "#typeof"; INVOKE 0, which INVOKE answered before any
	// key or method of x.
	TYPEOF

	// Slots (bytecode 7, 04/11/2026). A function's own variables (its parameters, the let and const of
	// its body and blocks, the objects its loops make for each iteration, the compiler's temporaries) and
	// the top level's block variables live in its frame's slots, at an index the compiler gives each: no
	// name is looked up. LOAD_SLOT and STORE_SLOT read and write a slot of the frame running; LOAD_OUTER and
	// STORE_OUTER one of a function around, through the closure that holds its frame's slots: a one-byte
	// depth (1 is the function the running one was made in) then the index. Each has a wide form with a
	// three-byte index. The top level's own variables and the names nothing declares keep LOAD and STORE.
	LOAD_SLOT
	LOAD_SLOT_WIDE
	STORE_SLOT
	STORE_SLOT_WIDE
	LOAD_OUTER
	LOAD_OUTER_WIDE
	STORE_OUTER
	STORE_OUTER_WIDE

	// TEMPLATE (bytecode 8, 05/11/2026) is a template literal's ${…}: the text below it joined with the
	// value on top made text by ToString, the string hint (value.Template): an object's own toString runs
	// before its valueOf, as JavaScript's template does. A template compiled to ADD, whose default hint
	// runs valueOf first, so `${{ toString: () => "T", valueOf: () => 7 }}` was "7".
	TEMPLATE

	// SPREAD, CALL_SPREAD and INVOKE_SPREAD (bytecode 9, 15/11/2026, spread.go) are the spread of an array and of a
	// call: SPREAD's one-byte operand appends to the array under the top the items of an iterable value
	// (SpreadItems) or the value itself (SpreadItem); CALL_SPREAD calls the callee under an array of arguments,
	// INVOKE_SPREAD the method named on top of the target under one, as CALL and INVOKE call.
	SPREAD
	CALL_SPREAD
	INVOKE_SPREAD
	// REST (bytecode 9, spread.go) is the rest of a pattern, { a, ...rest } and [first, ...others]: a one-byte kind
	// (RestObject, RestArray) and a two-byte constant (the keys read before it, or the index it starts at); it keeps
	// the value destructured and pushes the rest above it.
	REST

	// ITEMS (bytecode 10, 18/11/2026, spread.go) is what an array pattern reads, [a, b] = v: the value on top made the
	// items its iteration gives, as JavaScript's array pattern iterates it: an array itself, a string's code points,
	// bytes' numbers; anything else the TYPE failure. The pattern read v by index, so a string gave half of a
	// character outside the basic plane, and 5 or { 0: "z", length: 1 } gave undefined and "z" where JavaScript
	// throws.
	ITEMS
	// DELETE (bytecode 10, 18/11/2026, delete.go) is delete o.k and delete o[k]: the key on top, the value under it,
	// left as true once the key is not the value's own, or the failure strict JavaScript throws.
	DELETE
	// UNSET_SLOT and UNSET_NAME (bytecode 10, 18/11/2026, unset.go) mark a let or a const not yet made, as its scope
	// begins: a slot of the frame running (its index, three bytes, then its name's constant, three bytes), or a name
	// of the top level (its constant, three bytes). A function that reads or writes it before its line has run meets
	// the failure NOT_INITIALIZED there, JavaScript's ReferenceError; it read undefined.
	UNSET_SLOT
	UNSET_NAME

	_LIMIT // One past the final assigned opcode slot; never execute it.
)

// The operand of STEP.
const (
	StepNumber    = 0 // the value made a number, unstepped: what x++ and x-- give as a value
	StepIncrement = 1 // the value made a number, plus one: what ++ writes
	StepDecrement = 2 // the value made a number, minus one: what -- writes
	// StepNegate is -x: value.Negate, the value made a number and negated, a duration negated as a
	// duration (bytecode 6). It compiled to x; PUSH -1; MUL.
	StepNegate = 3
)

// Wide is the three-byte form of PUSH, LOAD, STORE, the slot instructions, JUMP, TRUE, FALSE and ITER;
// any other opcode is its own.
func (op Opcode) Wide() Opcode {
	switch op {
	case PUSH:
		return PUSH_WIDE
	case LOAD:
		return LOAD_WIDE
	case STORE:
		return STORE_WIDE
	case LOAD_SLOT:
		return LOAD_SLOT_WIDE
	case STORE_SLOT:
		return STORE_SLOT_WIDE
	case LOAD_OUTER:
		return LOAD_OUTER_WIDE
	case STORE_OUTER:
		return STORE_OUTER_WIDE
	case JUMP:
		return JUMP_WIDE
	case TRUE:
		return TRUE_WIDE
	case FALSE:
		return FALSE_WIDE
	case ITER:
		return ITER_WIDE
	}
	return op
}

// Narrow is the instruction a wide form is: PUSH for PUSH_WIDE. Any other opcode is its own.
func (op Opcode) Narrow() Opcode {
	switch op {
	case PUSH_WIDE:
		return PUSH
	case LOAD_WIDE:
		return LOAD
	case STORE_WIDE:
		return STORE
	case LOAD_SLOT_WIDE:
		return LOAD_SLOT
	case STORE_SLOT_WIDE:
		return STORE_SLOT
	case LOAD_OUTER_WIDE:
		return LOAD_OUTER
	case STORE_OUTER_WIDE:
		return STORE_OUTER
	case JUMP_WIDE:
		return JUMP
	case TRUE_WIDE:
		return TRUE
	case FALSE_WIDE:
		return FALSE
	case ITER_WIDE:
		return ITER
	}
	return op
}

// IsWide reports a wide form.
func (op Opcode) IsWide() bool {
	return op != op.Narrow()
}
