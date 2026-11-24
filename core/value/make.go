package value

import (
	"fmt"
	"math"
	"slices"
	"time"
	"unsafe"
)

// Undefined là Value{}.
func Undefined() Value { return Value{} }

// Null là null.
func Null() Value { return Value{kind: null} }

// Number là một số: float64, NaN và Infinity gồm cả.
func Number(f float64) Value { return Value{kind: number, number: math.Float64bits(f)} }

// Boolean là true hoặc false.
func Boolean(b bool) Value {
	if b {
		return Value{kind: boolean, number: 1}
	}
	return Value{kind: boolean}
}

// Time là một thời điểm, giữ bằng nanosecond từ 1970 UTC: múi giờ và đồng hồ đơn điệu của t không đi theo, đọc lại
// là UTC (múi giờ là việc của application). Ngoài khoảng int64 nanosecond chứa được, năm 1678 tới 2262, là lỗi.
func Time(t time.Time) Value {
	if t.Before(earliest) || t.After(latest) {
		return rangeError("time %s is outside the years 1678 to 2262", t.UTC().Format(time.RFC3339))
	}
	return Value{kind: datetime, number: uint64(t.UnixNano())}
}

// earliest và latest là hai đầu của khoảng Time giữ được.
var earliest, latest = time.Unix(0, math.MinInt64), time.Unix(0, math.MaxInt64)

// Duration là một khoảng thời gian, giữ bằng nanosecond.
func Duration(d time.Duration) Value { return Value{kind: duration, number: uint64(d)} }

// String là một chuỗi. Đặt tên như JavaScript; đọc lại bằng v.String(). Giữ byte đầu và độ dài (value.go): không cấp
// phát, và vẫn giữ byte của s sống như chính s.
func String(s string) Value {
	if s == "" {
		return Value{kind: text, data: (*byte)(nil)}
	}
	return Value{kind: text, number: uint64(len(s)), data: unsafe.StringData(s)}
}

// Bytes là một dãy byte, giữ bản chép của b: sửa b sau đó không sửa Value. Không phép nào ghi vào nó tại chỗ (đọc ra
// cũng là bản chép), nên dùng chung giữa các request mà không cần cờ đóng băng.
func Bytes(b []byte) Value { return Value{kind: bytes, data: slices.Clone(b)} }

// Function là một hàm: callable là thứ runtime chạy; value chỉ mang nó.
func Function(callable any) Value { return Value{kind: function, data: callable} }

// Struct là một struct của engine (Website, Application…), mang bằng con trỏ: chương trình gọi phương thức của nó.
func Struct(pointer any) Value { return Value{kind: structure, data: pointer} }

// Invalid là một lỗi, lời báo tiếng Anh dựng bằng fmt.Errorf. Chuỗi định dạng luôn là hằng: %q cho chữ người viết,
// %s cho Typeof, %d cho số nguyên, %w để bọc một lỗi của Go (value.Invalid("listen: %w", err)).
func Invalid(format string, arguments ...any) Value {
	return Error(fmt.Errorf(format, arguments...))
}

// Error là lỗi err của Go mang nguyên trong một Value, không viết lại lời báo: lỗi của os, net… đi thẳng ra chương
// trình. Cần thêm ngữ cảnh thì dùng Invalid với %w. nil là không có lỗi: Error(nil) là undefined, nên
// return value.Error(err) viết được ngay sau một lời gọi Go mà không cần if err != nil.
func Error(err error) Value {
	if err == nil {
		return Value{}
	}
	return Value{kind: invalid, data: err}
}
