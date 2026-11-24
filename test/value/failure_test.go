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
