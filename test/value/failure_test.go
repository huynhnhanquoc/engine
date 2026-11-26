package value_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Lỗi có cấu trúc (core/value/failure.go): mã, lời báo, chỗ; Within gắn chỗ từ trong ra ngoài; Error() vẫn là error
// của Go, errors.Is thấy lỗi gốc.

func TestFail(t *testing.T) {
	failure := value.Fail(value.RangeCode, "the statement ran past %s", "30s")
	if !failure.IsInvalid() || failure.Code() != value.RangeCode || failure.Message() != "the statement ran past 30s" || failure.At() != "" {
		t.Errorf("code %q, message %q, at %q", failure.Code(), failure.Message(), failure.At())
	}
	if failure.Error().Error() != "the statement ran past 30s" {
		t.Errorf("without a place, the Go error is the message: %q", failure.Error())
	}

	// %w giữ lỗi gốc, như Invalid
	wrapped := value.Fail(value.InvalidInputCode, "open the database: %w", os.ErrNotExist)
	if !errors.Is(wrapped.Error(), os.ErrNotExist) || wrapped.Message() != "open the database: "+os.ErrNotExist.Error() {
		t.Errorf("Fail with %%w: %v", wrapped.Error())
	}

	// Errorf: một lỗi Go có mã; bọc thêm bằng fmt.Errorf rồi mới thành Value, mã vẫn còn, lời báo là của lớp ngoài
	inner := value.Errorf(value.TypeCode, "a struct of the engine is not data")
	outer := value.Error(fmt.Errorf("JSON.stringify: %w", inner))
	if outer.Code() != value.TypeCode || outer.Message() != "JSON.stringify: a struct of the engine is not data" || !errors.Is(outer.Error(), inner) {
		t.Errorf("a wrapped code: %q %q", outer.Code(), outer.Message())
	}
	if plain := value.Error(errors.New("disk full")); plain.Code() != "" {
		t.Errorf("an error with no code inside has none: %q", plain.Code())
	}
}

func TestWithin(t *testing.T) {
	// mỗi tầng nói tên của nó, tầng ngoài thêm phần của mình
	inner := value.Fail(value.TypeCode, `takes a time like "30s", got "forever"`).Within("timeout")
	outer := inner.Within("application.database")
	if outer.At() != "application.database.timeout" || outer.Code() != value.TypeCode || outer.Message() != `takes a time like "30s", got "forever"` {
		t.Errorf("at %q, code %q, message %q", outer.At(), outer.Code(), outer.Message())
	}
	if want := `application.database.timeout: takes a time like "30s", got "forever"`; outer.Error().Error() != want {
		t.Errorf("the Go error has the place first: %q", outer.Error())
	}
	if inner.At() != "timeout" {
		t.Errorf("Within makes a new value, the inner one keeps its place: %q", inner.At())
	}

	// một chỉ số dính vào tên trước nó, không có dấu chấm
	if at := value.Invalid("port 70000 is not an integer from 1 to 65535").Within("listen[1]").Within("application.website").At(); at != "application.website.listen[1]" {
		t.Errorf("an index: %q", at)
	}
	// chỉ số gắn riêng (chỗ đọc một phần tử chỉ biết số của nó), rồi tên trường, rồi cửa
	if at := value.Invalid("not an address").Within("[1]").Within("listen").Within("application.website").At(); at != "application.website.listen[1]" {
		t.Errorf("an index on its own: %q", at)
	}

	// lỗi của Invalid và Error có chỗ được, mã thì không, và lỗi gốc vẫn thấy
	plain := value.Error(os.ErrPermission).Within("application.database.open")
	if plain.Code() != "" || plain.At() != "application.database.open" || !errors.Is(plain.Error(), os.ErrPermission) ||
		plain.Message() != os.ErrPermission.Error() {
		t.Errorf("a plain error: code %q, at %q, %v", plain.Code(), plain.At(), plain.Error())
	}

	// không phải lỗi, hay chỗ rỗng: trả nguyên
	if v := value.Number(5).Within("x"); !v.Identical(value.Number(5)) || v.Code() != "" || v.Message() != "" || v.At() != "" {
		t.Error("Within of a value that is no error gives it back")
	}
	if same := outer.Within(""); !same.Identical(outer) {
		t.Error("an empty place gives the error back")
	}
}

// Code, Message, At của mọi dạng lỗi một Value mang (26/11/2026: ba hàm đọc thẳng lỗi, không dựng một fault mỗi lần
// đọc): lỗi không mã của Invalid và Error, lỗi bọc một lỗi có mã bằng %w hay errors.Join, lỗi có mã, và mỗi dạng sau
// khi Within gắn chỗ. Đọc một lỗi không cấp phát; riêng Code của một lỗi không phải fault đi tìm mã trong lỗi nó bọc
// (errors.As), một lần cấp phát.
func TestFailureReaders(t *testing.T) {
	coded := value.Errorf(value.TypeCode, "no JSON")
	for _, c := range []struct {
		name                    string
		v                       value.Value
		code, message, at       string
		codeAllocs, readsAllocs float64
	}{
		{"Invalid", value.Invalid("not an address"), "", "not an address", "", 1, 0},
		{"Invalid with a cause", value.Invalid("listen: %w", os.ErrNotExist), "", "listen: " + os.ErrNotExist.Error(), "", 1, 0},
		{"Error", value.Error(errors.New("disk full")), "", "disk full", "", 1, 0},
		{"Error of a wrapped code", value.Error(fmt.Errorf("JSON.stringify: %w", coded)), value.TypeCode, "JSON.stringify: no JSON", "", 1, 0},
		// errors.Join dựng lời báo của nó mỗi lần được hỏi (hai lần cấp phát): chúng là của lỗi, không phải của Message
		{"Error of a joined code", value.Error(errors.Join(errors.New("first"), coded)), value.TypeCode, "first\nno JSON", "", 1, 2},
		{"Fail", value.Fail(value.RangeCode, "past %s", "30s"), value.RangeCode, "past 30s", "", 0, 0},
		{"Fail within", value.Fail(value.RangeCode, "past 30s").Within("timeout").Within("application.database"), value.RangeCode, "past 30s",
			"application.database.timeout", 0, 0},
		{"Invalid within", value.Invalid("not an address").Within("[1]").Within("listen"), "", "not an address", "listen[1]", 0, 0},
		{"a wrapped code within", value.Error(fmt.Errorf("JSON.stringify: %w", coded)).Within("body"), value.TypeCode, "JSON.stringify: no JSON",
			"body", 0, 0},
	} {
		if c.v.Code() != c.code || c.v.Message() != c.message || c.v.At() != c.at {
			t.Errorf("%s: code %q, message %q, at %q; want %q, %q, %q", c.name, c.v.Code(), c.v.Message(), c.v.At(), c.code, c.message, c.at)
		}
		var code, message, at string
		if got := testing.AllocsPerRun(100, func() { code = c.v.Code() }); got > c.codeAllocs {
			t.Errorf("%s: Code allocates %.0f times, want %.0f at most", c.name, got, c.codeAllocs)
		}
		if got := testing.AllocsPerRun(100, func() { message, at = c.v.Message(), c.v.At() }); got > c.readsAllocs {
			t.Errorf("%s: Message and At allocate %.0f times, want %.0f at most", c.name, got, c.readsAllocs)
		}
		_, _, _ = code, message, at
	}
	for _, v := range []value.Value{value.Undefined(), value.Number(1), value.String("TYPE"), value.Error(nil)} {
		if v.Code() != "" || v.Message() != "" || v.At() != "" {
			t.Errorf("a %s is no error: code %q, message %q, at %q", v.Typeof(), v.Code(), v.Message(), v.At())
		}
	}
}
