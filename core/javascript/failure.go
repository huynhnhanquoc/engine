package javascript

import "github.com/huynhnhanquoc/engine/core/value"

// Lỗi của các luật JavaScript, mỗi loại một mã của value (core/value/failure.go): TypeError là TYPE, RangeError là
// RANGE, SyntaxError của JSON.parse và URIError là INVALID_INPUT. Bộ đối chiếu với node (test/javascript) so mã ấy với
// loại lỗi node ném. Code trả error của Go thì dùng value.Errorf cùng mã.

func typeError(format string, arguments ...any) value.Value {
	return value.Fail(value.TypeCode, format, arguments...)
}

func rangeError(format string, arguments ...any) value.Value {
	return value.Fail(value.RangeCode, format, arguments...)
}
