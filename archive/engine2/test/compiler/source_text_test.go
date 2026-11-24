package compiler_test

import (
	"strings"
	"testing"
)

// The source text is read as JavaScript reads it, or refused with a compile error that names what it met
// (18/11/2026, docs/language.md mục 3.2). A // comment ends at a line terminator, LF, CR, U+2028 or U+2029: it ran
// past a CR alone and a U+2028 to the next LF, so the code after them was silently a comment. Outside strings a CR
// alone, a NUL (which ended the source without a word), a control byte (white space until then), a BOM, a Unicode
// space or line separator, a character that is no ASCII and a single & or | are refused, each saying what it is: they
// all said "ILLEGAL", or nothing.
//
// CONTROL (18/11/2026): with every byte up to 0x20 white space again, the control byte compiled and the two CRs
// failed only as two statements on one line; with the comment running to the next LF, the CR and the U+2028 after a
// comment compiled, the line after them gone.
func TestTheSourceTextIsReadAsJavaScriptReadsItOrRefused(t *testing.T) {
	for source, want := range map[string]string{
		"const result = 1 // c\rconst z = 2":           "router.work:1:22: CR đứng riêng làm dòng mới",
		"const a = 1\rconst result = 2":                "router.work:1:12: CR đứng riêng làm dòng mới",
		"const result = 1 // x\xe2\x80\xa8const z = 2": "router.work:1:22: ký tự dòng mới Unicode U+2028 ngoài chuỗi",
		"const result = 1\x00 garbage":                 "router.work:1:17: byte NUL (0x00) ngoài chuỗi",
		"\xef\xbb\xbfconst result = 1":                 "router.work:1:1: file bắt đầu bằng BOM (U+FEFF)",
		"const\x01result = 1":                          "router.work:1:6: ký tự điều khiển 0x01 ngoài chuỗi",
		"const result = 1 \xc2\xa0":                    "router.work:1:18: khoảng trắng Unicode U+00A0 ngoài chuỗi",
		"const caf\xc3\xa9 = 1":                        "router.work:1:10: ký tự 'é' ngoài chuỗi và chú thích",
		"const result = 5 & 3":                         "router.work:1:18: Kitwork không có toán tử bit (&)",
		"const result = 5 | 3":                         "router.work:1:18: Kitwork không có toán tử bit (|)",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for source, want := range map[string]string{
		"const result = String(1)\r\nconst z = 2":                    "1",
		"const result = \"t\xc3\xaan \xc2\xa0\"":                     "t\xc3\xaan \xc2\xa0",
		"// ch\xc3\xa0o \xe2\x80\x94 \x00\nconst result = String(3)": "3",
		"const result = String(1)\t\x0b\x0c+ \"\"":                   "1",
	} {
		got, err := runFileResult(t, source)
		if err != nil || !got.IsString() || got.Text() != want {
			t.Errorf("%q: %v %s, want %q", source, err, shownResult(got), want)
		}
	}
}
