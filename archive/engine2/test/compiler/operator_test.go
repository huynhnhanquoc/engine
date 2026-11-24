package compiler_test

import (
	"strings"
	"testing"
)

// An operator of JavaScript Kitwork does not have is a compile error naming it and how to write it (19/11/2026,
// docs/language.md mục 3.3): ** and %= were `không đọc được "*" ở đầu một biểu thức`, of their second character, and a
// shift `không đọc được "<"`. The operators Kitwork has read as before (held to node by parity_test.go).
//
// CONTROL (19/11/2026): with the lexer reading ** as two stars, the first case said `không đọc được "*"`; with shifts
// not refused, 1 << 2 said `không đọc được "<"`.
func TestAnOperatorKitworkDoesNotHaveIsNamed(t *testing.T) {
	in := func(body string) string { return "const f = (a, b) => { " + body + " }" }
	for source, want := range map[string]string{
		"const result = 2 ** 3":   "router.work:1:18: Kitwork không có **: viết Math.pow(a, b)",
		in("a **= b; return a;"):  "router.work:1:25: Kitwork không có **=: viết a = Math.pow(a, b)",
		in("a %= b; return a;"):   "router.work:1:25: Kitwork không có %=: viết a = a % b",
		in("a &&= b; return a;"):  "router.work:1:25: Kitwork không có &&=: viết a = a && b, hay if (a) a = b",
		in("a ||= b; return a;"):  "router.work:1:25: Kitwork không có ||=: viết a = a || b, hay if (!a) a = b",
		in("a ??= b; return a;"):  "router.work:1:25: Kitwork không có ??=: viết a = a ?? b, hay if (a == null) a = b",
		"const result = 1 << 2":   "router.work:1:18: Kitwork không có <<: toán tử bit",
		"const result = 8 >> 1":   "router.work:1:18: Kitwork không có >>: toán tử bit",
		"const result = 8 >>> 1":  "router.work:1:18: Kitwork không có >>>: toán tử bit",
		in("a <<= b; return a;"):  "router.work:1:25: Kitwork không có <<=: toán tử bit",
		in("a >>= b; return a;"):  "router.work:1:25: Kitwork không có >>=: toán tử bit",
		in("a >>>= b; return a;"): "router.work:1:25: Kitwork không có >>>=: toán tử bit",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const result = String([2 * 3, 7 % 4, 1 < 2, 2 <= 2, 3 > 2, 3 >= 4, 0 && 1, 0 || 1, null ?? 5])": "6,3,true,true,true,false,0,1,5",
		"const f = (a) => { a *= 2; a /= 4; return a }\nconst result = String(f(6))":                     "3",
		"const o = null\nconst result = String([o?.a, o ? 1 : 2])":                                       ",2",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
}
