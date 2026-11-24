package compiler_test

import (
	"strings"
	"testing"
)

// A /* … */ comment is read as JavaScript reads one (19/11/2026, the owner; it was a compile error, / then *): it ends
// at the first */ after its /*, so /*/ does not close it and comments do not nest; inside a string it is text. One
// that holds a line break is one between the tokens around it, so two statements it parts need no ;, and a return
// before it returns undefined, as in JavaScript. It holds any text but the line breaks Kitwork refuses outside one
// (a CR alone, U+2028), so a line is counted alike everywhere; a /* the file ends inside is a compile error at it.
//
// CONTROL (19/11/2026): with the lexer not reading /*, every case to run failed to compile; with the * of /* closing
// it, 6 /*/ 2 */ was refused and node's parity 300 of 301; with the CR not checked, the CR alone compiled; with a /*
// the file ends inside taken as closed, it compiled.
func TestABlockCommentIsReadAsJavaScriptReadsIt(t *testing.T) {
	for source, want := range map[string]string{
		"const result = 1 /* a */ + 2":                               "3",
		"/* header\n   two lines */\nconst result = 5":               "5",
		"const result = /**/ 7":                                      "7",
		"const result = 4 /* a */ /* b */ * 2":                       "8",
		"const result = \"/* not a comment */\"":                     "/* not a comment */",
		"const result = 6 /*/ 2 */":                                  "6",
		"const result = `${/* c */ 1}`":                              "1",
		"const a = 1 /* line\n */ const b = 2\nconst result = a + b": "3",
		"const f = () => /* c */ 9\nconst result = f()":              "9",
		"const result = 1 /* \x00 é */":                              "1",
		"const result = 2 /* a\r\nb */":                              "2",
		"const result = 3 // a /* b\n":                               "3",
		"/* c */ const result = 1 /* d */":                           "1",
		"const f = () => { return /* c\n */ 1 }\nconst result = f()": "undefined",
	} {
		got, err := runFileResult(t, source)
		if err != nil || shownResult(got) != want {
			t.Errorf("%q: %v %s, want %s", source, err, shownResult(got), want)
		}
	}
	for source, want := range map[string]string{
		"const a = 1 /* x */ const b = 2":       "router.work:1:21: cần ; hay xuống dòng",
		"/* never closed\nconst result = 1":     "router.work:1:1: chú thích /* chưa đóng: file hết trước */",
		"const result = 1 /* a\rb */":           "router.work:1:22: CR đứng riêng làm dòng mới",
		"const result = 1 /* a\xe2\x80\xa8b */": "router.work:1:22: ký tự dòng mới Unicode U+2028",
		"const result = 1 /* a /* b */ c */":    "router.work:1:31:",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
}
