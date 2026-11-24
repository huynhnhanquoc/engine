package value_test

import (
	"math"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/kitwork/engine2/core/value"
)

// The rules that are Kitwork's, not JavaScript's: what a Value is in Go, how a failure travels, how a
// time behaves, how a module's struct is called.

func TestValueIsThirtyTwoBytesAndUndefinedByDefault(t *testing.T) {
	if size := unsafe.Sizeof(value.Value{}); size != 32 {
		t.Fatalf("Value is %d bytes, want 32", size)
	}
	var slots [3]value.Value
	if !slots[1].IsUndefined() || slots[1].Typeof().Text() != "undefined" {
		t.Fatal("an unset Value must be undefined")
	}
	if !value.Undefined().Equal(slots[0]) {
		t.Fatal("undefined === undefined")
	}
}

func TestNumberKeepsEveryFloat(t *testing.T) {
	for _, f := range []float64{0, -0.1, 7.0 / 2, 1 << 53, -1e300, math.SmallestNonzeroFloat64, math.Inf(-1)} {
		if got := value.Number(f).Number(); got != f {
			t.Errorf("Number(%v).Number() = %v", f, got)
		}
	}
	if got := value.Number(math.NaN()).Number(); got == got {
		t.Error("NaN must stay NaN")
	}
	if value.Number(1.5).Integer() != 1 || value.Number(-1.5).Integer() != -1 || value.Number(math.NaN()).Integer() != 0 {
		t.Error("Int drops the fraction toward zero and makes NaN 0")
	}
	if value.Number(1e300).Integer() != math.MaxInt64 {
		t.Error("Int clamps instead of wrapping")
	}
}

func TestTimeIsExactToTheNanosecondAndAddsADuration(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 123456789, time.UTC)
	v := value.Time(at)
	if !v.Time().Equal(at) || v.Time().Nanosecond() != 123456789 {
		t.Fatalf("Time lost precision: %v", v.Time())
	}
	later := v.Add(value.Duration(90 * time.Minute))
	if !later.IsTime() || later.Time().Hour() != 10 || later.Time().Minute() != 30 {
		t.Fatalf("Time + Duration = %s", later.ToString().Text())
	}
	if !value.Time(at).Equal(value.Time(at)) {
		t.Error("two equal times are === (value, not identity)")
	}
	if !v.Less(later) || later.Less(v) || !v.LessEqual(v) {
		t.Error("times order by instant")
	}
	gap := later.Subtract(v)
	if !gap.IsDuration() || gap.Duration() != 90*time.Minute {
		t.Errorf("Time - Time = %s", gap.ToString().Text())
	}
	if value.Duration(time.Second).Multiply(value.Number(1.5)).Duration() != 1500*time.Millisecond {
		t.Error("Duration * number scales")
	}
	if got := value.Duration(90 * time.Minute).Hours().Number(); got != 1.5 {
		t.Errorf("hours() = %v", got)
	}
	if got := v.Format(value.String("2006-01-02 15:04")).Text(); got != "2026-11-03 09:00" {
		t.Errorf("format() = %q", got)
	}
	if got := v.ToISOString().Text(); got != "2026-11-03T09:00:00.123Z" {
		t.Errorf("toISOString() = %q", got)
	}
	if got := v.GetTime().Number(); got != float64(at.UnixMilli()) {
		t.Errorf("getTime() = %v", got)
	}
	if got := v.Typeof().Text(); got != "object" {
		t.Errorf("typeof time = %q", got)
	}
}

func TestFailureIsAValueThatPassesThroughEveryOperation(t *testing.T) {
	bad := value.Fail("INVALID_ARGUMENT", "bank: BIN must be 6 digits")
	if !bad.IsInvalid() || bad.Code() != "INVALID_ARGUMENT" {
		t.Fatal("Fail makes an Invalid with its code")
	}
	for name, got := range map[string]value.Value{
		"bad + 1":      bad.Add(value.Number(1)),
		"1 + bad":      value.Number(1).Add(bad),
		"bad - 1":      bad.Subtract(value.Number(1)),
		"bad.x":        bad.Get("x"),
		"bad.f()":      bad.Invoke("f", nil),
		"bad()":        bad.Call(nil),
		"-bad":         bad.Negate(),
		"bad.x = 1":    bad.Set("x", value.Number(1)),
		"Number(bad)":  bad.ToNumber(),
		"bad.push(1)":  bad.Push(value.Number(1)),
		"bad.at(0)":    bad.At(value.Number(0)),
		"bad.slice(0)": bad.Slice(value.Number(0)),
	} {
		if !got.IsInvalid() || got.Code() != "INVALID_ARGUMENT" {
			t.Errorf("%s: the failure did not pass: %s", name, got.ToString().Text())
		}
	}
	if !bad.ToBoolean() {
		t.Error("a failure is truthy until safe() reads it, so `if (x)` does not swallow it")
	}
	if got := bad.ToString().Text(); got != "INVALID_ARGUMENT: bank: BIN must be 6 digits" {
		t.Errorf("ToString = %q", got)
	}
}

func TestWrongKindIsATypeFailureNotAPanic(t *testing.T) {
	for name, got := range map[string]value.Value{
		"(5).toUpperCase()": value.Number(5).ToUpperCase(),
		"(5).push(1)":       value.Number(5).Push(value.Number(1)),
		`"a".join()`:        value.String("a").Join(),
		"null.x = 1":        value.Null().Set("x", value.Number(1)),
		"(5)()":             value.Number(5).Call(nil),
		"(5).nope()":        value.Number(5).Invoke("nope", nil),
		"[].format()":       value.Array(nil).Format(),
	} {
		if !got.IsInvalid() || got.Code() != "TYPE" {
			t.Errorf("%s: want a TYPE failure, got %s", name, got.ToString().Text())
		}
	}
	if got := value.Number(5).Invoke("nope", nil).Failure(); got != "TYPE: number has no method nope" {
		t.Errorf("message = %q", got)
	}
}

func TestObjectKeepsKeyOrder(t *testing.T) {
	o := value.Object().Set("zeta", value.Number(1)).Set("alpha", value.Number(2)).Set("zeta", value.Number(3))
	if got := strings.Join(o.Keys(), ","); got != "zeta,alpha" {
		t.Errorf("keys = %q", got)
	}
	if o.Get("zeta").Number() != 3 || !o.Has("alpha") || o.Has("beta") {
		t.Error("set overwrites in place; has sees own keys only")
	}
	if !o.Delete("zeta") || strings.Join(o.Keys(), ",") != "alpha" || o.Get("zeta").IsUndefined() != true {
		t.Error("delete removes the key and closes the gap")
	}
	if !o.Equal(o) || o.Equal(value.Object()) {
		t.Error("objects are === by identity")
	}
}

func TestArrayIsSharedByReference(t *testing.T) {
	a := value.Array(nil)
	b := a
	b.Push(value.Number(1))
	if a.Get("length").Number() != 1 {
		t.Error("a push through one copy is seen through the other")
	}
	a.SetIndex(3, value.String("x"))
	if a.Get("length").Number() != 4 || !a.Index(2).IsUndefined() || a.Index(3).Text() != "x" {
		t.Error("v[3] = x grows the array with holes")
	}
	if got := a.ToString().Text(); got != "1,,,x" {
		t.Errorf("String([1,,,x]) = %q", got)
	}
}

// ---- modules: a struct's uppercase methods with the one signature are the program's ----

type Napas struct{ bin, account string }

func (n *Napas) Bank(a ...value.Value) value.Value {
	bin := a[0].ToString().Text()
	if len(bin) != 6 {
		return value.Fail("INVALID_ARGUMENT", "bank: BIN must be 6 digits")
	}
	n.bin, n.account = bin, a[1].ToString().Text()
	return value.Struct(n)
}

func (n *Napas) Payload(...value.Value) value.Value {
	return value.String("000201" + n.bin + n.account)
}

// Valid is Go's: another signature, so no program sees it.
func (n *Napas) Valid() bool { return n.bin != "" }

func TestStructExposesOnlyTheOneSignature(t *testing.T) {
	table := value.Expose[Napas]()
	if got := strings.Join(table.Names(), " "); got != "bank payload" {
		t.Fatalf("exposed = %q", got)
	}
	napas := value.Struct(&Napas{})
	qr := napas.Invoke("bank", []value.Value{value.String("970436"), value.String("0123")}).Invoke("payload", nil)
	if qr.Text() != "0002019704360123" {
		t.Errorf("payload = %q", qr.Text())
	}
	if got := napas.Invoke("valid", nil); !got.IsInvalid() || got.Failure() != "TYPE: napas has no method valid" {
		t.Errorf("valid() must not be reachable: %s", got.ToString().Text())
	}
	short := napas.Invoke("bank", []value.Value{value.String("97"), value.String("1")}).Invoke("payload", nil)
	if short.Code() != "INVALID_ARGUMENT" {
		t.Errorf("a failure from bank() reaches payload() unchanged: %s", short.ToString().Text())
	}
	// a struct is an object to the program and its type's name to a person: "struct" is kitwork/schema's word
	// since 06/11/2026, so a VietQR is "napas" in a failure text ("cannot set x on napas")
	if napas.Typeof().Text() != "object" || napas.Kind() != "napas" {
		t.Errorf("a struct is an object to the program, its type's name to a person: %q", napas.Kind())
	}
	if failed := napas.Set("bin", value.String("1")); failed.Failure() != "TYPE: cannot set bin on napas" {
		t.Errorf("a write to a struct says %q", failed.Failure())
	}
}

func TestValueExposesItsJavaScriptMethodsOnly(t *testing.T) {
	names := value.ValueMethods()
	has := func(name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"toString", "toUpperCase", "push", "slice", "format", "at"} {
		if !has(name) {
			t.Errorf("%s must be callable from a program", name)
		}
	}
	for _, name := range []string{"equal", "text", "number", "toNumber", "typeof", "call", "get", "index", "isInvalid", "add"} {
		if has(name) {
			t.Errorf("%s is Go's and must not be callable from a program", name)
		}
	}
	if got := value.String("abc").Invoke("toUpperCase", nil).Text(); got != "ABC" {
		t.Errorf("invoke by name = %q", got)
	}
}

// TestEveryMethodIsListedForTheKindsItAnswers holds the method lists of javascript.go (what a program
// reads without calling: 'abc'.at, [].map) to the methods themselves, both ways: a name a kind lists is
// a program-callable method whose kind guard takes that kind, a method whose guard takes a kind is
// listed for it, and every program-callable method is listed for at least one kind.
func TestEveryMethodIsListedForTheKindsItAnswers(t *testing.T) {
	samples := []func() value.Value{
		func() value.Value { return value.String("ab") },
		func() value.Value { return value.Number(1.5) },
		func() value.Value { return value.Boolean(true) },
		func() value.Value { return value.Array([]value.Value{value.Number(1)}) },
		func() value.Value { return value.Object() },
		func() value.Value { return value.Time(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)) },
		func() value.Value { return value.Duration(time.Minute) },
		func() value.Value { return value.Bytes([]byte("ab")) },
		func() value.Value { return value.Function(func(...value.Value) value.Value { return value.Null() }) },
	}
	callable := map[string]bool{}
	for _, name := range value.ValueMethods() {
		callable[name] = true
	}
	listedSomewhere := map[string]bool{}
	for _, sample := range samples {
		kind := sample().Kind()
		listed := map[string]bool{}
		for _, name := range sample().MethodNames() {
			listed[name], listedSomewhere[name] = true, true
			if !callable[name] {
				t.Errorf("%s lists %s, which no program can call", kind, name)
			}
		}
		for name := range callable {
			// a fresh sample for each call: push, sort, fill … change the one they are called on
			got := sample().Invoke(name, nil)
			answers := !(got.IsInvalid() && got.Message() == kind+" has no method "+name)
			if answers != listed[name] {
				t.Errorf("%s.%s: the method answers %v, the list says %v", kind, name, answers, listed[name])
			}
			if read := sample().Get(name); read.IsFunction() != listed[name] {
				t.Errorf("%s.%s read without a call is %s", kind, name, read.Typeof().Text())
			}
		}
	}
	for name := range callable {
		if !listedSomewhere[name] {
			t.Errorf("%s is program-callable but no kind lists it", name)
		}
	}
	if !value.String("ab").Get("map").IsUndefined() || !value.Number(1).Get("push").IsUndefined() {
		t.Error("a name the kind does not list reads as undefined")
	}
	if names := value.Null().MethodNames(); names != nil {
		t.Errorf("null has no methods, got %v", names)
	}
}

// A method read off its value has no value to work on (18/11/2026, docs/language.md mục 4.2): Kitwork has no `this`, and
// JavaScript's prototype method read off its value throws when called apart, so calling it is the TYPE error that says
// to call it on its value; it was bound to its receiver, so push read then called pushed. It is a function still, and
// two reads of one method are one, as JavaScript's are; a call on its value (Invoke) runs it.
//
// CONTROL (18/11/2026): with the method bound again, push read then called pushed (length 2), and the earlier toString
// read gave "[object Object]".
func TestAMethodReadHasNoValueToWorkOn(t *testing.T) {
	a := value.Array([]value.Value{value.Number(1)})
	push := a.Get("push")
	if got := push.Call([]value.Value{value.Number(2)}); got.Code() != value.TypeCode || !strings.Contains(got.Message(), "method push đọc ra khỏi giá trị rồi gọi riêng") || a.Get("length").Number() != 1 {
		t.Errorf("push read then called: %s, length %v", got.Message(), a.Get("length").Number())
	}
	if !push.IsFunction() || !push.Equal(value.Array(nil).Get("push")) {
		t.Error("a method read is a function, one for every array")
	}
	if got := a.Invoke("push", []value.Value{value.Number(2)}); got.Number() != 2 {
		t.Errorf("push called on its array: %s", got.ToString().Text())
	}
	o := value.Object()
	toString := o.Get("toString")
	o.Set("toString", value.Number(5))
	if got := toString.Call(nil); got.Code() != value.TypeCode {
		t.Errorf("Object.prototype.toString read then called: %s", got.ToString().Text())
	}
	if got := o.Get("toString"); got.Number() != 5 {
		t.Error("an own key comes before the method")
	}
	bad := value.Fail(value.RangeCode, "a failure")
	if got := value.String("abc").Get("at").Call([]value.Value{bad}); got.Code() != value.RangeCode {
		t.Error("a failure handed to a method read comes back out")
	}
	if got := value.Fail(value.RangeCode, "x").Get("at"); got.Code() != value.RangeCode {
		t.Error("a method read on a failure is the failure")
	}
	if got := value.Undefined().Get("at"); got.Code() != value.TypeCode {
		t.Error("a method read on undefined is a TYPE failure, as JavaScript throws")
	}
}

// The findings of the 03/11 review, each kept red until fixed.

func TestReviewFindingsStayFixed(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	now := value.Time(at)
	if !now.Less(value.Number(2e12)) || !now.Greater(value.Number(1)) || !now.GreaterEqual(value.Number(1)) {
		t.Error("a time orders against a number by its milliseconds, as a Date does")
	}
	if got := value.Function(nil).Call(nil); !got.IsInvalid() {
		t.Error("Function(nil) is a failure, not a nil dereference at call time")
	}
	type Point struct{ x int }
	p := &Point{}
	if !value.Struct(p).Equal(value.Struct(p)) || value.Struct(p).Equal(value.Struct(&Point{})) {
		t.Error("structs are === by pointer")
	}
	if !value.Struct[Point](nil).IsNull() {
		t.Error("a nil pointer is null")
	}
	bad := value.Fail("BOOM", "a failure")
	for name, got := range map[string]value.Value{
		`"abc".slice(bad)`:            value.String("abc").Invoke("slice", []value.Value{bad}),
		`"abc".includes(bad)`:         value.String("abc").Invoke("includes", []value.Value{bad}),
		`[1].push(bad)`:               value.Array(nil).Invoke("push", []value.Value{bad}),
		`f(bad)`:                      value.Function(func(...value.Value) value.Value { return value.Null() }).Call([]value.Value{bad}),
		`!bad`:                        bad.Not(),
		`typeof bad`:                  bad.Typeof(),
		`a[0] = bad`:                  value.Array(nil).SetIndex(0, bad),
		`"abc".slice(bad) via Invoke`: value.String("abc").Invoke("slice", []value.Value{bad}),
	} {
		if !got.IsInvalid() || got.Code() != "BOOM" {
			t.Errorf("%s: the failure did not come back out: %s", name, got.ToString().Text())
		}
	}
	if value.Boolean(true).Number() != 0 || !value.Number(5).Time().IsZero() || value.String("7").Integer() != 0 || value.Number(1).Duration() != 0 {
		t.Error("an accessor on the wrong kind reads as the zero value, never as bits")
	}
	if got := value.Time(time.Time{}); !got.IsInvalid() || got.Code() != "RANGE" {
		t.Error("the zero time is a RANGE failure, not the year 1754")
	}
	if got := value.Time(time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)); !got.IsInvalid() {
		t.Error("a time before 1678 is a RANGE failure")
	}
	b := value.Bytes([]byte("5x"))
	if b.Get("length").Number() != 2 || b.Index(0).Number() != 53 || !b.Index(9).IsUndefined() {
		t.Error("bytes have a length and index as numbers")
	}
	if value.ParseInt("0x1f", 0) != 31 || value.ParseInt("12px", 10) != 12 || !math.IsNaN(value.ParseInt("x", 10)) || value.ParseFloat("1.5e3x") != 1500 {
		t.Error("ParseInt and ParseFloat are JavaScript's, for the runtime's builtins")
	}
	var e any = value.Number(5)
	if _, isError := e.(error); isError {
		t.Error("a Value must not satisfy the error interface")
	}
}

func BenchmarkInvokeStruct(b *testing.B) {
	value.Expose[Napas]()
	napas := &Napas{}
	stack := []value.Value{value.String("970436"), value.String("0123")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		value.Struct(napas).Invoke("bank", stack)
	}
}

func BenchmarkAddNumbers(b *testing.B) {
	x, y := value.Number(1.5), value.Number(2)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		x = x.Add(y)
	}
}

func BenchmarkInvokeString(b *testing.B) {
	s := value.String("hello")
	needle := []value.Value{value.String("ell")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.Invoke("includes", needle)
	}
}

var vietnamese = value.String("Phường Bến Nghé, Quận 1, Thành phố Hồ Chí Minh")

func BenchmarkVietnameseLength(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vietnamese.Get("length")
	}
}

func BenchmarkVietnameseStartsWith(b *testing.B) {
	needle := []value.Value{value.String("Phường")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vietnamese.Invoke("startsWith", needle)
	}
}

func BenchmarkVietnameseSlice(b *testing.B) {
	bounds := []value.Value{value.Number(7), value.Number(15)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vietnamese.Invoke("slice", bounds)
	}
}

func BenchmarkVietnameseIndexOf(b *testing.B) {
	needle := []value.Value{value.String("Quận")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vietnamese.Invoke("indexOf", needle)
	}
}

type Request struct{ url string }

// Url has no parameter, so it is a property: request.url reads it without () (struct_test.go).
func (r *Request) Url() value.Value { return value.String(r.url) }

func TestAStructPropertyIsItsMethodOfNoParameter(t *testing.T) {
	request := value.Struct(&Request{url: "/orders/7"})
	if got := request.Get("url").Text(); got != "/orders/7" {
		t.Errorf("request.url = %q", got)
	}
	if !request.Get("nope").IsUndefined() {
		t.Error("a name the struct has no method for reads as undefined")
	}
	bad := value.Fail(value.DeniedCode, "a file outside the app")
	if bad.Message() != "a file outside the app" || bad.Code() != "DENIED" || value.Number(1).Message() != "" {
		t.Error("Message and Code are the two halves safe() hands the program")
	}
}
