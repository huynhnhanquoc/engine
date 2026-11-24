package value_test

import (
	"errors"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Typeof là chữ typeof của JavaScript; loại JavaScript không có (thời điểm, khoảng thời gian, bytes, struct, lỗi) là
// "object", như một Date. Value{} là undefined.
func TestTypeofIsJavaScripts(t *testing.T) {
	cases := []struct {
		name   string
		value  value.Value
		typeof string
	}{
		{"zero", value.Value{}, "undefined"},
		{"undefined", value.Undefined(), "undefined"},
		{"null", value.Null(), "object"},
		{"number", value.Number(1), "number"},
		{"boolean", value.Boolean(true), "boolean"},
		{"string", value.String("a"), "string"},
		{"function", value.Function("callable"), "function"},
		{"object", value.Object(), "object"},
		{"array", value.Array(), "object"},
		{"time", value.Time(time.Unix(0, 0)), "object"},
		{"duration", value.Duration(time.Second), "object"},
		{"bytes", value.Bytes([]byte("a")), "object"},
		{"struct", value.Struct(&struct{}{}), "object"},
		{"invalid", value.Invalid("x"), "object"},
	}
	for _, c := range cases {
		if got := c.value.Typeof(); got != c.typeof {
			t.Errorf("%s: typeof is %q, want %q", c.name, got, c.typeof)
		}
	}
	if !value.Undefined().IsNullish() || !value.Null().IsNullish() || value.Number(0).IsNullish() {
		t.Error("IsNullish must be true for undefined and null only")
	}
}

// Mỗi hàm đọc trả lại đúng thứ hàm tạo cùng tên đã nhận.
func TestReadersGiveBackWhatWasMade(t *testing.T) {
	if got := value.Number(1.5).Number(); got != 1.5 {
		t.Errorf("Number: %v", got)
	}
	if got := value.Number(math.NaN()).Number(); !math.IsNaN(got) {
		t.Errorf("Number(NaN): %v", got)
	}
	if !value.Boolean(true).Boolean() || value.Boolean(false).Boolean() {
		t.Error("Boolean does not round-trip")
	}
	if got := value.String("chữ").String(); got != "chữ" {
		t.Errorf("String: %q", got)
	}
	callable := &struct{ name string }{"f"}
	if got := value.Function(callable).Function(); got != callable {
		t.Errorf("Function: %v", got)
	}
	pointer := &struct{ port int }{8080}
	if got := value.Struct(pointer).Struct(); got != pointer {
		t.Errorf("Struct: %v", got)
	}
	if got, ok := value.As[struct{ port int }](value.Struct(pointer)); !ok || got != pointer {
		t.Errorf("As[T]: %v, %v", got, ok)
	}
	if _, ok := value.As[struct{ name string }](value.Struct(pointer)); ok {
		t.Error("As[other] must be false for a struct of another type")
	}
	if _, ok := value.As[struct{ port int }](value.Number(1)); ok {
		t.Error("As[T] must be false for a non-struct")
	}
	saigon := time.FixedZone("ICT", 7*3600)
	moment := time.Date(2026, 11, 22, 23, 0, 0, 123456789, saigon)
	if got := value.Time(moment).Time(); !got.Equal(moment) || got.Location() != time.UTC {
		t.Errorf("Time: %v, want %v in UTC", got, moment)
	}
	if got := value.Duration(90 * time.Minute).Duration(); got != 90*time.Minute {
		t.Errorf("Duration: %v", got)
	}
	if got := value.Bytes([]byte("abc")).Bytes(); string(got) != "abc" {
		t.Errorf("Bytes: %q", got)
	}
}

// Một hàm đọc trên loại khác trả giá trị rỗng của Go, không panic.
func TestReadersOfAnotherKindGiveZero(t *testing.T) {
	n := value.Number(1)
	if value.String("1").Number() != 0 || n.String() != "" || n.Boolean() || !n.Time().IsZero() || n.Duration() != 0 ||
		n.Bytes() != nil || n.Function() != nil || n.Struct() != nil || n.Error() != nil {
		t.Error("a reader of another kind must give the zero value")
	}
	if value.Object().Length() != 0 || !value.Object().Index(0).IsUndefined() || value.Object().Items() != nil {
		t.Error("array readers on an object must give zero")
	}
	if _, found := value.Array().Get("x"); found || value.Array().Keys() != nil {
		t.Error("object readers on an array must give zero")
	}
}

// Time giữ được năm 1678 tới 2262 (nanosecond trong int64); ngoài khoảng ấy là lỗi, kể cả time.Time{}.
func TestTimeOutsideItsRangeIsAnError(t *testing.T) {
	inside := time.Date(2262, 1, 1, 0, 0, 0, 0, time.UTC)
	if value.Time(inside).IsInvalid() {
		t.Errorf("%v must fit", inside)
	}
	for _, outside := range []time.Time{{}, time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)} {
		v := value.Time(outside)
		if !v.IsInvalid() {
			t.Errorf("%v must be an error", outside)
			continue
		}
		want := "time " + outside.Format(time.RFC3339) + " is outside the years 1678 to 2262"
		if got := v.Error().Error(); got != want {
			t.Errorf("message %q, want %q", got, want)
		}
	}
}

// Bytes không ghi được tại chỗ: hàm tạo và hàm đọc đều chép.
func TestBytesAreCopiedInAndOut(t *testing.T) {
	source := []byte("abc")
	v := value.Bytes(source)
	source[0] = 'x'
	out := v.Bytes()
	out[1] = 'y'
	if got := string(v.Bytes()); got != "abc" {
		t.Errorf("bytes changed from outside: %q", got)
	}
}

// Invalid và Error mang một error của Go; Error() đọc lại đúng lỗi ấy, lỗi bọc bằng %w vẫn nhận ra được.
func TestErrorsCarryGoErrors(t *testing.T) {
	if v := value.Error(nil); !v.IsUndefined() {
		t.Error("Error(nil) must be undefined")
	}
	_, err := os.Open("no-such-file-in-engine3-tests")
	wrapped := value.Invalid("certificates: %w", err)
	if !wrapped.IsInvalid() || !errors.Is(wrapped.Error(), os.ErrNotExist) {
		t.Errorf("Invalid with %%w must keep the Go error: %v", wrapped.Error())
	}
	if got := value.Invalid("%d%% of %q", 5, "listen").Error().Error(); got != `5% of "listen"` {
		t.Errorf("message %q", got)
	}
	var asAny any = value.Number(1)
	if _, isError := asAny.(error); isError {
		t.Error("Value must not be a Go error")
	}
}

// Keys theo thứ tự JavaScript: khoá là chỉ số mảng trước, tăng dần, rồi các khoá khác theo thứ tự thêm vào; Set một
// khoá đã có giữ chỗ cũ; Delete giữ thứ tự các khoá còn lại.
func TestObjectKeepsJavaScriptsOrder(t *testing.T) {
	o := value.Object()
	o.Set("b", value.Number(1)).Set("a", value.Number(2)).Set("7", value.Number(3)).Set("0", value.Number(4)).
		Set("01", value.Number(5))
	if got, want := o.Keys(), []string{"0", "7", "b", "a", "01"}; !slices.Equal(got, want) {
		t.Errorf("Keys %q, want %q", got, want)
	}
	o.Set("b", value.Number(9))
	if b, _ := o.Get("b"); b.Number() != 9 || o.Keys()[2] != "b" {
		t.Errorf("Set of a present key must replace in place: %q", o.Keys())
	}
	o.Delete("a").Delete("missing")
	if got, want := o.Keys(), []string{"0", "7", "b", "01"}; !slices.Equal(got, want) {
		t.Errorf("after Delete %q, want %q", got, want)
	}
	if _, found := o.Get("a"); found {
		t.Error("a deleted key must not be found")
	}
}

// Một object nằm sau con trỏ: mọi bản chép cùng thấy một chỗ; Keys() là bản chép.
func TestObjectIsSharedByItsCopies(t *testing.T) {
	o := value.Object()
	p := o
	p.Set("x", value.Number(1))
	if x, found := o.Get("x"); !found || x.Number() != 1 {
		t.Error("a Set through a copy must be seen by the original")
	}
	keys := o.Keys()
	keys[0] = "changed"
	if o.Keys()[0] != "x" {
		t.Error("changing Keys() must not change the object")
	}
}

// Một mảng nằm sau con trỏ: Push qua bản chép thấy ở mọi bản; Array và Items chép phần tử.
func TestArrayIsSharedAndCopiesAtItsEdges(t *testing.T) {
	source := []value.Value{value.Number(1), value.Number(2)}
	a := value.Array(source...)
	source[0] = value.Number(99)
	if a.Index(0).Number() != 1 {
		t.Error("Array must copy its items")
	}
	b := a
	b.Push(value.Number(3), value.Number(4))
	if a.Length() != 4 || a.Index(3).Number() != 4 {
		t.Errorf("a Push through a copy must be seen by the original: length %d", a.Length())
	}
	items := a.Items()
	items[0] = value.Number(77)
	if a.Index(0).Number() != 1 {
		t.Error("changing Items() must not change the array")
	}
	if !a.Index(-1).IsUndefined() || !a.Index(4).IsUndefined() {
		t.Error("an index outside the array must be undefined")
	}
}

// Ghi vào loại khác là lỗi, nói đúng loại: null và mảng không thành "object".
func TestWritesOnAnotherKindAreErrors(t *testing.T) {
	cases := []struct {
		result value.Value
		want   string
	}{
		{value.Null().Set("x", value.Number(1)), `cannot set property "x" of null`},
		{value.Array().Set("x", value.Number(1)), `cannot set property "x" of array`},
		{value.Number(5).Set("x", value.Number(1)), `cannot set property "x" of number`},
		{value.String("s").Delete("x"), `cannot delete property "x" of string`},
		{value.Object().Push(value.Number(1)), "cannot push to object"},
		{value.Undefined().Push(value.Number(1)), "cannot push to undefined"},
	}
	for _, c := range cases {
		if !c.result.IsInvalid() {
			t.Errorf("%q: not an error", c.want)
			continue
		}
		if got := c.result.Error().Error(); got != c.want {
			t.Errorf("message %q, want %q", got, c.want)
		}
	}
}

// HostPort đọc một cổng hay một địa chỉ lắng nghe, viết lại cổng không số 0 đứng đầu.
func TestHostPort(t *testing.T) {
	cases := []struct {
		value   value.Value
		address string
		failure string
	}{
		{value.Number(8080), ":8080", ""},
		{value.String(":443"), ":443", ""},
		{value.String("127.0.0.1:8080"), "127.0.0.1:8080", ""},
		{value.String("[::1]:8080"), "[::1]:8080", ""},
		{value.String("localhost:080"), "localhost:80", ""},
		{value.Number(70000), "", "port 70000 is not an integer from 1 to 65535"},
		{value.Number(1.5), "", "port 1.5 is not an integer from 1 to 65535"},
		{value.Number(math.NaN()), "", "port NaN is not an integer from 1 to 65535"},
		{value.String(":0"), "", `port of ":0" is not an integer from 1 to 65535`},
		{value.String(":http"), "", `port of ":http" is not an integer from 1 to 65535`},
		{value.String("8080"), "", `"8080" is not an address: write 8080, ":443" or "127.0.0.1:8080"`},
		{value.Boolean(true), "", `an address is a port 8080 or a string ":443", got boolean`},
	}
	for _, c := range cases {
		address, err := c.value.HostPort()
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		if address != c.address || failure != c.failure {
			t.Errorf("HostPort: %q, %q; want %q, %q", address, failure, c.address, c.failure)
		}
	}
}

// ByteSize đọc một kích thước bộ nhớ ("512kb", "64mb", "2gb") ra số byte.
func TestByteSize(t *testing.T) {
	cases := []struct {
		value   value.Value
		bytes   int64
		failure string
	}{
		{value.String("512kb"), 512 << 10, ""},
		{value.String("64mb"), 64 << 20, ""},
		{value.String("2gb"), 2 << 30, ""},
		{value.String("0mb"), 0, `"0mb" is not a size: write "512kb", "64mb" or "2gb"`},
		{value.String("64 mb"), 0, `"64 mb" is not a size: write "512kb", "64mb" or "2gb"`},
		{value.String("64MB"), 0, `"64MB" is not a size: write "512kb", "64mb" or "2gb"`},
		{value.String("10000000000gb"), 0, `"10000000000gb" is not a size: write "512kb", "64mb" or "2gb"`},
		{value.Number(64), 0, `a size is a string like "512kb", "64mb" or "2gb", got number`},
	}
	for _, c := range cases {
		bytes, err := c.value.ByteSize()
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		if bytes != c.bytes || failure != c.failure {
			t.Errorf("ByteSize: %d, %q; want %d, %q", bytes, failure, c.bytes, c.failure)
		}
	}
}

func TestScan(t *testing.T) {
	var s string
	var b bool
	var n int
	if err := value.String("shop.vn").Scan(&s); err != nil || s != "shop.vn" {
		t.Errorf("Scan string: %q, %v", s, err)
	}
	if err := value.Boolean(true).Scan(&b); err != nil || !b {
		t.Errorf("Scan bool: %v, %v", b, err)
	}
	if err := value.Number(16).Scan(&n); err != nil || n != 16 {
		t.Errorf("Scan int: %d, %v", n, err)
	}
	if err := value.Number(1.5).Scan(&n); err == nil || err.Error() != "1.5 is not an integer" {
		t.Errorf("Scan non-integer: %v", err)
	}
	if err := value.Number(1).Scan(&s); err == nil || err.Error() != "takes a string, got number" {
		t.Errorf("Scan wrong kind: %v", err)
	}
	if got := value.Number(80).List(); len(got) != 1 || got[0].Number() != 80 {
		t.Errorf("List of scalar: %v", got)
	}
	if got := value.Array(value.Number(80), value.Number(443)).List(); len(got) != 2 || got[1].Number() != 443 {
		t.Errorf("List of array: %v", got)
	}
}
