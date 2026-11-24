package javascript_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// console viết một dòng mỗi lời gọi, đầu dòng là lead và tên của chữ; ký tự có thể bắt đầu một dòng mới hay vẽ đè
// lên một dòng được thoát, để một site không viết được dòng mang tên site khác; mỗi lời gọi tối đa 64 KB chữ.
func TestConsole(t *testing.T) {
	var out bytes.Buffer
	console := javascript.Console(&out, "[shop.com] ")
	write := func(word string, arguments ...value.Value) string {
		out.Reset()
		r := javascript.Invoke(run{}, console, word, arguments)
		switch {
		case r.IsInvalid():
			return "error: " + r.Error().Error() + " | written: " + out.String()
		case !r.IsUndefined():
			return "not undefined: " + describe(r)
		}
		return out.String()
	}
	math, _ := javascript.Global("Math")
	maxFunction, _ := javascript.Members(math).Get("max")
	object := value.Object().Set("toString", value.Function(callbacks["text"]))
	escaped := func(r rune) string { return string('\\') + fmt.Sprintf("u%04x", r) }
	char := func(r rune) string { return string(r) }

	cases := []struct {
		name, got, want string
	}{
		{"values", write("log", value.String("a"), value.Number(1), value.Number(-0.0), value.Null(), value.Undefined(),
			value.Boolean(true), value.Number(1.5)), "[shop.com] [console.log] a 1 0 null undefined true 1.5\n"},
		{"arrays and objects", write("log", value.Array(value.Number(1), value.Array(value.Number(2), value.Number(3))), value.Object(), object),
			"[shop.com] [console.log] 1,2,3 [object Object] t\n"},
		{"functions", write("log", maxFunction, value.Function(callbacks["double"])),
			"[shop.com] [console.log] [Function: max] [Function (anonymous)]\n"},
		{"Kitwork's kinds", write("log", value.Time(time.Date(2026, 11, 23, 8, 30, 0, 123456789, time.UTC)), value.Duration(90*time.Minute),
			value.Bytes([]byte("abc"))), "[shop.com] [console.log] 2026-11-23T08:30:00.123Z 1h30m0s abc\n"},
		{"nothing", write("log"), "[shop.com] [console.log] \n"},
		{"error", write("error", value.String("x")), "[shop.com] [console.error] x\n"},
		{"warn", write("warn", value.String("x")), "[shop.com] [console.warn] x\n"},
		{"info", write("info", value.String("x")), "[shop.com] [console.info] x\n"},
		{"debug", write("debug", value.String("x")), "[shop.com] [console.debug] x\n"},
		{"every line is led", write("log", value.String("a\nb")), "[shop.com] [console.log] a\n[shop.com] [console.log] b\n"},
		{"control characters", write("log", value.String("\x1b[31mred\r\tok\x7f")), "[shop.com] [console.log] \\x1b[31mred\\x0d\tok\\x7f\n"},
		{"line separators and bidirectional controls", write("log", value.String("a"+char(0x2028)+"b"+char(0x202e)+"c"+char(0x2069)+"d"+char(0x2027))),
			"[shop.com] [console.log] a" + escaped(0x2028) + "b" + escaped(0x202e) + "c" + escaped(0x2069) + "d" + char(0x2027) + "\n"},
		{"C1", write("log", value.String(char(0x85)+char(0x9b)+char(0xa0))), "[shop.com] [console.log] \\x85\\x9b" + char(0xa0) + "\n"},
		{"a lone surrogate and a broken byte", write("log", value.String("\xed\xa0\xbd|\xff")), "[shop.com] [console.log] \\xed\\xa0\\xbd|\\xff\n"},
		{"Vietnamese passes as it is", write("log", value.String("Phường ạ 😀")), "[shop.com] [console.log] Phường ạ 😀\n"},
		{"Math is no text", write("log", value.String("a"), math), "error: console.log: Math is a library, not data | written: "},
		{"a toString that throws", write("log", value.Object().Set("toString", value.Function(callbacks["boom"]))),
			"error: console.log: boom | written: "},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n  got  %q\n  want %q", c.name, c.got, c.want)
		}
	}

	// 64 KB một lời gọi, cắt ở đầu một ký tự; chỉ phần ấy được dựng
	long := write("log", value.String(strings.Repeat("ạ", 30000)))
	if want := "[shop.com] [console.log] " + strings.Repeat("ạ", 21845) + " … (24465 bytes more)\n"; long != want {
		t.Errorf("cut: %d bytes, ends %q", len(long), long[len(long)-40:])
	}
	big := value.String(strings.Repeat("x", 100_000))
	if many := write("log", big, big, big); !strings.HasSuffix(many, " … (234466 bytes more)\n") || len(many) > 70_000 {
		t.Errorf("three long arguments: %d bytes, ends %q", len(many), many[len(many)-40:])
	}

	// console là thư viện, không phải dữ liệu; Global có một cái viết ra stdout
	global, found := javascript.Global("console")
	json, _ := javascript.Global("JSON")
	if !found || javascript.Members(global) == nil {
		t.Fatal(`Global("console")`)
	}
	if got := javascript.Invoke(nil, json, "stringify", []value.Value{global}); !got.IsInvalid() ||
		got.Error().Error() != "JSON.stringify: console is a library, not data" {
		t.Errorf("JSON.stringify(console): %s", describe(got))
	}
	log, _ := javascript.Members(console).Get("log")
	if n := log.Function().(*javascript.Native); n.Name() != "log" || n.Length() != 0 {
		t.Errorf("console.log: %q %d", n.Name(), n.Length())
	}
}

// Một chữ hỏng ở giữa (byte không phải UTF-8) mã hoá thành U+FFFD, như Kitwork đọc nó; node không dựng được chữ ấy.
func TestEncodeURIOfABrokenByte(t *testing.T) {
	encode, _ := javascript.Global("encodeURIComponent")
	got := encode.Function().(*javascript.Native).Call(nil, value.String("a\xffb"))
	if got.String() != "a%EF%BF%BDb" {
		t.Errorf("%s", describe(got))
	}
}
