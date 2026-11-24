package value

import (
	"errors"
	"fmt"
	"strings"
)

// Lỗi có cấu trúc (owner 23/11): ngoài lời báo, một Value loại invalid mang được một mã cho chương trình so
// (error.code: "TIMEOUT", "REFUSED"…) và một chỗ (error.at: đường dẫn trường của bản thiết kế,
// "application.database.timeout"). Không có type lỗi xuất ra: lỗi vẫn là một Value, đọc bằng Code, Message, At;
// Error() trả một error của Go như trước, nên errors.Is, errors.As vẫn thấy lỗi gốc. Invalid và Error không có mã.

// Tập mã đóng (owner 23/11: chỉ các mã engine3 đang sinh, thêm một mã khi một việc thật cần, như TIMEOUT khi http tới;
// tên theo engine đầu tiên). test/value/codes_test.go giữ mọi Fail và Errorf của engine3 trong tập này.
const (
	// TypeCode là một phép trên loại nó không nhận: đọc thuộc tính của undefined, gọi thứ không phải hàm, ghi vào
	// object đã đóng băng. TypeError của JavaScript.
	TypeCode = "TYPE"
	// RangeCode là một số ngoài khoảng một phép nhận: toFixed(101), repeat(-1), độ dài mảng sai, một thời điểm ngoài
	// 1678–2262. RangeError của JavaScript.
	RangeCode = "RANGE"
	// InvalidInputCode là chữ từ ngoài vào không đọc được: JSON.parse của một chuỗi hỏng, decodeURIComponent của một
	// chuỗi hỏng. SyntaxError và URIError của JavaScript.
	InvalidInputCode = "INVALID_INPUT"
	// EnergyCode là một lượt chạy đã dùng hết năng lượng của nó (energy của website mỗi request, trần lúc boot của
	// application.work): lượt chạy dừng ở lệnh ấy. JavaScript không có; tên của engine2 (core/runtime, 23/11 thêm khi
	// runtime của engine3 tới).
	EnergyCode = "ENERGY_LIMIT"
	// MemoryCode là một lượt chạy giữ quá trần byte của nó (memory của website mỗi request, 64 MiB mặc định): lượt
	// chạy dừng ở lệnh ấy, như ENERGY_LIMIT. Tên của engine2 (23/11 thêm khi bộ đếm byte của engine3 tới).
	MemoryCode = "MEMORY_LIMIT"
	// DatabaseCode là một câu lệnh database không chạy được: file chưa có, bảng hay cột struct khai mà file thiếu (migrate
	// thêm), lỗi của SQLite. Tên của engine2 (24/11 thêm khi database của engine3 tới).
	DatabaseCode = "DATABASE"
)

// fault là lỗi Fail tạo và Within gắn chỗ. Lời báo của nó trong Go có chỗ đứng trước ("at: message"); Message thì không.
type fault struct {
	code, message, at string
	cause             error // lỗi Go gốc (%w, hay error của Invalid/Error): errors.Is, errors.As đi tiếp vào nó
}

func (f *fault) Error() string {
	if f.at == "" {
		return f.message
	}
	return f.at + ": " + f.message
}

func (f *fault) Unwrap() error { return f.cause }

// Fail là một lỗi có mã: value.Fail(value.RangeCode, "invalid count value: %s", n). %w trong format giữ lỗi gốc cho
// errors.Is như Invalid.
func Fail(code, format string, arguments ...any) Value {
	return Value{kind: invalid, data: Errorf(code, format, arguments...)}
}

// Errorf là lỗi có mã cho code Go trả error chứ không phải Value (một bước đọc, một hàm chuyển): khi nó thành Value
// bằng Error, kể cả sau khi được bọc bằng %w, mã vẫn còn.
func Errorf(code, format string, arguments ...any) error {
	err := fmt.Errorf(format, arguments...)
	return &fault{code: code, message: err.Error(), cause: errors.Unwrap(err)}
}

// typeError, rangeError là Fail với TypeCode, RangeCode, cho các lỗi của value.
func typeError(format string, arguments ...any) Value  { return Fail(TypeCode, format, arguments...) }
func rangeError(format string, arguments ...any) Value { return Fail(RangeCode, format, arguments...) }

// Within gắn chỗ của lỗi v, đặt trước chỗ nó đã có: lỗi ở "timeout" Within("application.database") là ở
// "application.database.timeout", lỗi ở "listen[1]" Within("application.website") là ở
// "application.website.listen[1]". Mỗi tầng chỉ nói tên của nó, tầng ngoài thêm phần của mình. v không phải lỗi, hay
// path rỗng, thì trả nguyên v; v không bị đổi.
func (v Value) Within(path string) Value {
	if v.kind != invalid || path == "" {
		return v
	}
	f := *v.fault()
	switch {
	case f.at == "":
		f.at = path
	case strings.HasPrefix(f.at, "["):
		f.at = path + f.at
	default:
		f.at = path + "." + f.at
	}
	return Value{kind: invalid, data: &f}
}

// Code là mã của lỗi v: "" khi lỗi không có mã (Invalid, Error), hay v không phải lỗi.
func (v Value) Code() string {
	if v.kind != invalid {
		return ""
	}
	return v.fault().code
}

// Message là lời báo của lỗi v, không có chỗ (error.message); "" khi v không phải lỗi.
func (v Value) Message() string {
	if v.kind != invalid {
		return ""
	}
	return v.fault().message
}

// At là chỗ của lỗi v (error.at): "" khi chưa gắn, hay v không phải lỗi.
func (v Value) At() string {
	if v.kind != invalid {
		return ""
	}
	return v.fault().at
}

// fault là lỗi v mang, ở dạng có cấu trúc: lỗi của Invalid hay Error thành một fault không chỗ, lỗi gốc là chính nó,
// mã là mã của lỗi có mã nó bọc (fmt.Errorf("JSON.stringify: %w", Errorf(TypeCode, …)) giữ TYPE), hay không có.
func (v Value) fault() *fault {
	err := v.data.(error)
	if f, ok := err.(*fault); ok {
		return f
	}
	f := &fault{message: err.Error(), cause: err}
	var inner *fault
	if errors.As(err, &inner) {
		f.code = inner.code
	}
	return f
}
