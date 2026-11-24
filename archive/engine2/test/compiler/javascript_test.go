package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// What the compiler writes for the browser (round 4 of the 13/11 review, 15/11/2026; library/ui's browser test runs
// it against the server and JavaScript): a name of the page is the scope's, this.title; an arrow's parameter and a
// const in its body are the text's own; a builtin is the browser's; a chain of an expression reads as ?.; a key in
// brackets and a member that leads to code are read through the kernel; what only the server runs, and kitwork, are
// errors. CONTROL: names written bare (no this.), red.
func TestTheBrowsersJavaScriptIsWrittenFromTheSource(t *testing.T) {
	for _, c := range []struct{ source, want string }{
		{"title", "this.title"},
		{"user.name", "this.user?.name"},
		{"items.map((i) => i.t)", "this.items?.map(((i) => { return i?.t; }))"},
		{"items[n]", "kitwork.internal.read(this.items, this.n, true)"},
		{"o.prototype", `kitwork.internal.read(this.o, "prototype", true)`}, // o.constructor is a compile error since 18/11/2026
		{"Math.max(1, n)", "Math?.max((1), this.n)"},
		{"new Date(0)", "new Date((0))"},
		{"`n=${n}`", "`${\"n=\"}${this.n}`"},
		{"typeof x", "(typeof this.x)"},
		{"({ n, ...o })", `({"n": this.n, ...this.o})`},
	} {
		got, err := compiler.ExpressionJavaScript(c.source)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", c.source, got, err, c.want)
		}
	}
	for _, c := range []struct{ source, want string }{
		{"count++", "(this.count++)"},
		{"o.b.c += 1", "((this.o?.b).c += (1))"},
		{"value = value + 1", "(this.value = (this.value + (1)))"},
		{"toggle()", "this.toggle()"},
	} {
		got, err := compiler.HandlerJavaScript(c.source)
		if err != nil || got != c.want {
			t.Errorf("event %s: %q, %v; want %q", c.source, got, err, c.want)
		}
	}
	for _, c := range []struct {
		source  string
		handler bool
		want    string
	}{
		{"kitwork.title", false, "kitwork không đọc được"},
		{"safe(() => n)", false, "safe(…) chỉ chạy ở server"},
		{"n = 1", false, "chỉ đọc"},
		{"Math = 1", true, "Math là builtin"},
		{"f(n = 1)", true, "gán bên trong lời gọi"},
	} {
		var err error
		if c.handler {
			_, err = compiler.HandlerJavaScript(c.source)
		} else {
			_, err = compiler.ExpressionJavaScript(c.source)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want an error saying %q", c.source, err, c.want)
		}
	}
}
