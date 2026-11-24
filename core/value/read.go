package value

import (
	"math"
	"slices"
	"time"
	"unsafe"
)

// Mỗi hàm đọc trả đúng thứ Go mà một loại mang, cùng tên với hàm tạo ra nó (Number và v.Number()), và giá trị rỗng
// của Go khi v là loại khác: hỏi loại trước bằng Is….

// Number là số của v; 0 khi v không phải số.
func (v Value) Number() float64 {
	if v.kind != number {
		return 0
	}
	return math.Float64frombits(v.number)
}

// Boolean là bool của v; false khi v không phải boolean.
func (v Value) Boolean() bool { return v.kind == boolean && v.number == 1 }

// String là chuỗi của v; "" khi v không phải chuỗi. Vì tên là String nên Value là một fmt.Stringer: fmt in một Value
// (%v, %s) bằng hàm này, nên một số in ra "". Trong lời báo, in v.Number() hay v.Typeof(), đừng in thẳng v.
func (v Value) String() string {
	if v.kind != text {
		return ""
	}
	return v.text()
}

// text là chuỗi v giữ (v.kind là text): byte đầu ở data, độ dài ở number. Đọc thẳng word dữ liệu của interface (với
// một con trỏ, word ấy là chính con trỏ), không ép kiểu, không rẽ nhánh: mỗi lần đọc thuộc tính dựng lại tên khoá qua
// đây, và một phép ép kiểu ở đó làm guard chậm 4% (đo 23/11/2026). Chuỗi rỗng là (nil, 0).
func (v Value) text() string {
	return unsafe.String((*byte)((*[2]unsafe.Pointer)(unsafe.Pointer(&v.data))[1]), int(v.number))
}

// Time là thời điểm v mang, ở UTC; time.Time{} khi v không phải thời điểm.
func (v Value) Time() time.Time {
	if v.kind != datetime {
		return time.Time{}
	}
	return time.Unix(0, int64(v.number)).UTC()
}

// Duration là khoảng thời gian v mang; 0 khi v không phải khoảng thời gian.
func (v Value) Duration() time.Duration {
	if v.kind != duration {
		return 0
	}
	return time.Duration(int64(v.number))
}

// Bytes là bản chép dãy byte v mang: sửa nó không sửa v. nil khi v không phải bytes.
func (v Value) Bytes() []byte {
	if v.kind != bytes {
		return nil
	}
	return slices.Clone(v.data.([]byte))
}

// Function là thứ runtime chạy mà v mang, như Function đã nhận; nil khi v không phải hàm.
func (v Value) Function() any {
	if v.kind != function {
		return nil
	}
	return v.data
}

// Struct là con trỏ v mang; nil khi v không phải struct của engine.
func (v Value) Struct() any {
	if v.kind != structure {
		return nil
	}
	return v.data
}

// As là con trỏ *T mà v mang, và v có mang một *T hay không; nil, false khi v không phải struct hoặc là struct loại
// khác (website: db hỏi As[website.Website] được nil, false).
func As[T any](v Value) (*T, bool) {
	if v.kind != structure {
		return nil, false
	}
	pointer, ok := v.data.(*T)
	return pointer, ok
}

// Error là lỗi v mang, đúng error mà Invalid hay Error đã cất (errors.Is, errors.As vẫn thấy lỗi gốc); nil khi v
// không phải lỗi. Trả error chứ không phải string: một phương thức Error() string sẽ biến mọi Value thành một error
// của Go, và fmt sẽ in mọi Value bằng nó.
func (v Value) Error() error {
	if v.kind != invalid {
		return nil
	}
	return v.data.(error)
}
