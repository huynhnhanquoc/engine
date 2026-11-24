package value

// kind là loại của một Value. Nó không ra khỏi gói: chương trình hỏi Typeof, Go hỏi IsNumber.
type kind uint8

const (
	// --- Rỗng (v.kind <= null) ---
	undefined kind = iota // 0: undefined (Value{} mặc định)
	null                  // 1: null

	// --- Nguyên thuỷ 64-bit: giữ trong Value.number, không cấp phát Heap (v.kind <= duration) ---
	number   // 2: float64 (bit IEEE-754 trong Value.number)
	boolean  // 3: bool (0 hoặc 1 trong Value.number)
	datetime // 4: time.Time (nanosecond từ 1970 UTC trong Value.number)
	duration // 5: time.Duration (nanosecond trong Value.number)

	// --- Dữ liệu tham chiếu: giữ trong Value.data ---
	text   // 6: string
	bytes  // 7: []byte
	object // 8: *properties (khoá và giá trị theo thứ tự thêm vào; object.go)
	array  // 9: *list (các phần tử; array.go)

	// --- Thực thể & Hàm của Engine: giữ trong Value.data ---
	structure // 10: con trỏ tới một struct của engine (Application, Website, RequestContext, Decision, Schema, Query…)
	function  // 11: thứ runtime chạy (hàm Go hoặc hàm JS chờ kích hoạt); value chỉ mang nó

	// --- Lỗi: giữ trong Value.data ---
	invalid // 12: error của Go, lời báo tiếng Anh: Invalid tạo bằng fmt.Errorf, Error nhận thẳng một error; Fail thêm mã, Within thêm chỗ (failure.go)
)

// Typeof là chữ typeof của JavaScript cho v. Những loại JavaScript không có (datetime, duration, bytes, structure,
// invalid) là "object", như một Date.
func (v Value) Typeof() string {
	switch v.kind {
	case undefined:
		return "undefined"
	case number:
		return "number"
	case boolean:
		return "boolean"
	case text:
		return "string"
	case function:
		return "function"
	}
	return "object"
}

// describe là loại của v cho lời báo: Typeof, trừ null và mảng, vì typeof của cả hai là "object" và lời báo
// "cannot set property of object" khi v là null thì đọc sai.
func describe(v Value) string {
	switch v.kind {
	case null:
		return "null"
	case array:
		return "array"
	}
	return v.Typeof()
}
