package javascript

import (
	"strings"
	"unicode/utf8"
)

// Chuỗi của chương trình là một dãy đơn vị UTF-16, như của JavaScript: "Phường".length là 6, "😀".length là 2. Thứ
// value.String mang là một string của Go viết UTF-8, thêm một điều: một nửa cặp đứng một mình (nửa emoji mà slice cắt
// ra) giữ ở dạng ba byte của nó (WTF-8), nên "😀".slice(0, 1) giữ đơn vị 0xD83D và nối lại thành "😀" khi nửa kia
// theo sau. Chuỗi ASCII đi đường byte; đường đơn vị không cấp phát để đếm hay so, chỉ để dựng chuỗi mới. Chép từ
// engine2 (text.go), nơi từng luật đã đối chiếu với node.

// ---- đọc đơn vị ----

// unitReader đi qua một chuỗi từng đơn vị UTF-16, không cấp phát.
type unitReader struct {
	text string
	at   int
	low  uint16 // đơn vị thứ hai của một cặp, đang chờ
}

func (r *unitReader) next() (uint16, bool) {
	if r.low != 0 {
		unit := r.low
		r.low = 0
		return unit, true
	}
	if r.at >= len(r.text) {
		return 0, false
	}
	b := r.text[r.at]
	if b < utf8.RuneSelf {
		r.at++
		return uint16(b), true
	}
	if unit, ok := loneSurrogateAt(r.text, r.at); ok {
		r.at += 3
		return unit, true
	}
	c, size := utf8.DecodeRuneInString(r.text[r.at:])
	r.at += size
	if c < 0x10000 {
		return uint16(c), true // RuneError cho một byte hỏng, như JavaScript hiện U+FFFD
	}
	c -= 0x10000
	r.low = uint16(0xDC00 + c&0x3FF)
	return uint16(0xD800 + c>>10), true
}

// loneSurrogateAt đọc dạng WTF-8 của một nửa cặp (ED A0..BF xx) ở i.
func loneSurrogateAt(s string, i int) (uint16, bool) {
	if i+3 > len(s) || s[i] != 0xED || s[i+1] < 0xA0 || s[i+1] > 0xBF || s[i+2]&0xC0 != 0x80 {
		return 0, false
	}
	return 0xD000 | uint16(s[i+1]&0x3F)<<6 | uint16(s[i+2]&0x3F), true
}

// isASCII đọc tám byte một bước, như unicode/utf8.
func isASCII(s string) bool {
	for len(s) >= 8 {
		first := uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
		second := uint32(s[4]) | uint32(s[5])<<8 | uint32(s[6])<<16 | uint32(s[7])<<24
		if (first|second)&0x80808080 != 0 {
			return false
		}
		s = s[8:]
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// units là chuỗi thành các đơn vị UTF-16.
func units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	r := unitReader{text: s}
	for {
		unit, ok := r.next()
		if !ok {
			return out
		}
		out = append(out, unit)
	}
}

// unitLength là độ dài của chuỗi như chương trình thấy, đếm không cấp phát.
func unitLength(s string) int {
	if isASCII(s) {
		return len(s)
	}
	n := 0
	r := unitReader{text: s}
	for {
		if _, ok := r.next(); !ok {
			return n
		}
		n++
	}
}

// ---- viết đơn vị ----

// appendUnit viết một đơn vị: ASCII và mặt phẳng cơ bản bằng UTF-8, một nửa cặp bằng dạng ba byte.
func appendUnit(b []byte, unit uint16) []byte {
	switch {
	case unit < utf8.RuneSelf:
		return append(b, byte(unit))
	case unit >= 0xD800 && unit <= 0xDFFF:
		return append(b, 0xED, 0x80|byte(unit>>6)&0x3F, 0x80|byte(unit)&0x3F)
	}
	return utf8.AppendRune(b, rune(unit))
}

// encodeUnits viết các đơn vị lại thành chuỗi, ghép nửa cao và nửa thấp đứng cạnh nhau thành một ký tự.
func encodeUnits(u []uint16) string {
	b := make([]byte, 0, len(u)*3)
	for i := 0; i < len(u); i++ {
		if u[i] >= 0xD800 && u[i] < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			b = utf8.AppendRune(b, 0x10000+(rune(u[i])-0xD800)<<10+(rune(u[i+1])-0xDC00))
			i++
			continue
		}
		b = appendUnit(b, u[i])
	}
	return string(b)
}

var asciiStrings = func() (table [utf8.RuneSelf]string) {
	for i := range table {
		table[i] = string(rune(i))
	}
	return
}()

// unitString là một đơn vị thành chuỗi: tra bảng với ASCII, nên s[i] không cấp phát.
func unitString(unit uint16) string {
	if unit < utf8.RuneSelf {
		return asciiStrings[unit]
	}
	return string(appendUnit(nil, unit))
}

// concatText là a + b của chuỗi: khi a tận cùng bằng một nửa cao và b mở đầu bằng nửa thấp, hai nửa thành lại ký tự
// của chúng, như đơn vị của JavaScript.
func concatText(a, b string) string {
	if len(a) >= 3 && len(b) >= 3 && a[len(a)-3] == 0xED && b[0] == 0xED {
		high, okHigh := loneSurrogateAt(a, len(a)-3)
		low, okLow := loneSurrogateAt(b, 0)
		if okHigh && okLow && high < 0xDC00 && low >= 0xDC00 {
			return a[:len(a)-3] + encodeUnits([]uint16{high, low}) + b[3:]
		}
	}
	return a + b
}

// mergeSurrogates ghép mọi nửa cao có nửa thấp theo sau, trong một chuỗi dựng từ nhiều mảnh (join, concat, repeat),
// thành ký tự của chúng; chuỗi không có dạng ba byte trả nguyên, không chép.
func mergeSurrogates(s string) string {
	if strings.IndexByte(s, 0xED) < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if high, ok := loneSurrogateAt(s, i); ok && high < 0xDC00 {
			if low, ok := loneSurrogateAt(s, i+3); ok && low >= 0xDC00 {
				b = append(b, encodeUnits([]uint16{high, low})...)
				i += 6
				continue
			}
		}
		b = append(b, s[i])
		i++
	}
	return string(b)
}

// ---- vị trí đơn vị ----

// byteOffset là chỗ đơn vị i bắt đầu trong s, và i có rơi vào đơn vị thứ hai của một cặp không (giữa một ký tự bốn
// byte), chỗ không byte nào chỉ được.
func byteOffset(s string, i int) (offset int, midPair bool) {
	if i <= 0 {
		return 0, false
	}
	r := unitReader{text: s}
	for n := 0; n < i; n++ {
		if _, ok := r.next(); !ok {
			return len(s), false
		}
	}
	return r.at, r.low != 0
}

// hasLoneSurrogate là s có dạng ba byte của một nửa cặp: chữ chỉ so từng đơn vị mới khớp được vào giữa một cặp.
// 0xED cũng mở đầu U+D000–U+D7FF (phần lớn âm tiết Hangul), không phải nửa cặp: tìm tiếp qua chúng.
func hasLoneSurrogate(s string) bool {
	for i := strings.IndexByte(s, 0xED); i >= 0; {
		if _, ok := loneSurrogateAt(s, i); ok {
			return true
		}
		next := strings.IndexByte(s[i+1:], 0xED)
		if next < 0 {
			return false
		}
		i += next + 1
	}
	return false
}

// unitSlice là s[start:end] theo đơn vị, hai đầu đã kẹp vào 0..độ dài. Hai đầu rơi đúng ranh giới ký tự thì kết quả
// dùng chung byte của chuỗi; chỉ khi cắt giữa một cặp mới viết lại.
func unitSlice(s string, start, end int) string {
	if start >= end {
		return ""
	}
	if isASCII(s) {
		return s[start:end]
	}
	from, cutStart := byteOffset(s, start)
	to, cutEnd := byteOffset(s, end)
	if !cutStart && !cutEnd {
		return s[from:to]
	}
	return encodeUnits(units(s)[start:end])
}

// unitIndex là chỉ số đơn vị của needle đầu tiên trong s từ from trở đi, -1 khi không có. Byte tìm ra đúng những chỗ
// đơn vị tìm ra, trừ một needle là nửa cặp nằm trong một cặp: chỗ ấy đi từng đơn vị.
func unitIndex(s, needle string, from int) int {
	if from > unitLength(s) {
		return -1
	}
	if isASCII(s) && isASCII(needle) {
		i := strings.Index(s[from:], needle)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if hasLoneSurrogate(needle) {
		return unitIndexSlow(s, needle, from)
	}
	offset, midPair := byteOffset(s, from)
	if midPair {
		return unitIndexSlow(s, needle, from)
	}
	i := strings.Index(s[offset:], needle)
	if i < 0 {
		return -1
	}
	return from + unitLength(s[offset:offset+i])
}

func unitIndexSlow(s, needle string, from int) int {
	haystack, pattern := units(s), units(needle)
	for at := from; at+len(pattern) <= len(haystack); at++ {
		if equalUnits(haystack[at:at+len(pattern)], pattern) {
			return at
		}
	}
	return -1
}

// unitLastIndex là chỉ số đơn vị của needle cuối cùng trong s bắt đầu tại from hay trước đó, -1 khi không có.
func unitLastIndex(s, needle string, from int) int {
	if isASCII(s) && isASCII(needle) {
		for at := min(from, len(s)-len(needle)); at >= 0; at-- {
			if s[at:at+len(needle)] == needle {
				return at
			}
		}
		return -1
	}
	if hasLoneSurrogate(needle) {
		return unitLastIndexSlow(s, needle, from)
	}
	limit, midPair := byteOffset(s, from)
	if midPair {
		return unitLastIndexSlow(s, needle, from)
	}
	i := strings.LastIndex(s[:min(len(s), limit+len(needle))], needle)
	if i < 0 {
		return -1
	}
	return unitLength(s[:i])
}

func unitLastIndexSlow(s, needle string, from int) int {
	haystack, pattern := units(s), units(needle)
	for at := min(from, len(haystack)-len(pattern)); at >= 0; at-- {
		if equalUnits(haystack[at:at+len(pattern)], pattern) {
			return at
		}
	}
	return -1
}

func equalUnits(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// compareText xếp hai chuỗi theo đơn vị, như < của JavaScript: -1, 0 hay 1. Thứ tự byte là thứ tự đơn vị với ASCII và
// mặt phẳng cơ bản; một emoji (một cặp) đứng dưới U+E000–U+FFFF.
func compareText(a, b string) int {
	if isASCII(a) || isASCII(b) {
		return strings.Compare(a, b)
	}
	x, y := unitReader{text: a}, unitReader{text: b}
	for {
		u, okU := x.next()
		w, okW := y.next()
		switch {
		case !okU && !okW:
			return 0
		case !okU:
			return -1
		case !okW:
			return 1
		case u < w:
			return -1
		case u > w:
			return 1
		}
	}
}

// ---- khoảng trắng ----

// trimSpace bỏ những gì trim() và StringToNumber của JavaScript bỏ ở hai đầu: WhiteSpace và LineTerminator.
// unicode.IsSpace của Go khác ở hai ký tự: U+0085 và U+FEFF.
func trimSpace(text string) string { return trimSpaceRight(trimSpaceLeft(text)) }

func trimSpaceLeft(text string) string {
	for text != "" {
		c, size := utf8.DecodeRuneInString(text)
		if !isSpace(c) {
			break
		}
		text = text[size:]
	}
	return text
}

func trimSpaceRight(text string) string {
	for text != "" {
		c, size := utf8.DecodeLastRuneInString(text)
		if !isSpace(c) {
			break
		}
		text = text[:len(text)-size]
	}
	return text
}

func isSpace(c rune) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}
