package javascript

import (
	"unicode/utf8"
	"unsafe"
)

// Con trỏ chữ (23/11/2026; bài học TextCursor của engine2). Chỉ số của một chuỗi là đơn vị UTF-16 (text.go), còn
// string của Go là byte: s.length, s[i], s.slice(a, b), s.startsWith(x) phải đi từ đầu để biết đơn vị i ở byte nào.
// Trước lát này mỗi phép đi cả chuỗi, có khi ba bốn lần (isASCII cả chuỗi, đếm độ dài, đổi cả chuỗi thành đơn vị),
// nên for (let i = 0; i < s.length; i++) { s[i] } trên 100 KB là hàng trăm mili giây. Bây giờ:
//   - mỗi phép chỉ đi tới chỗ nó cần (slice(1) không đếm độ dài, startsWith(x) đi bằng độ dài của x), đoạn ASCII
//     tám byte một bước;
//   - một lượt chạy nhớ vài chuỗi dài nó vừa đọc (TextMemo, Memos): độ dài, có phải ASCII không, và chỗ con trỏ dừng,
//     nên s.length lần thứ hai là O(1), s[i] lần lượt đi tiếp từ chỗ trước, chuỗi ASCII đọc thẳng byte;
//   - năng lượng tính theo đúng phần đã đi (work.go): nhớ rồi thì không tốn gì.
// Luật đơn vị giữ đúng như unitReader: dạng ba byte của một nửa cặp là một đơn vị, một byte hỏng là một đơn vị
// U+FFFD, một ký tự bốn byte là hai.

// TextMemo là cái một lượt chạy biết về một chuỗi nó đọc: độ dài (−1 khi chưa biết), có phải ASCII không, và con trỏ:
// đơn vị thứ unit bắt đầu ở byte at, mọi byte trước at là ASCII hay không (prefixASCII). Nó giữ chính chuỗi ấy (text):
// khi còn trong bộ nhớ của lượt chạy, byte của nó không thể thuộc chuỗi nào khác, nên so địa chỉ là đủ.
type TextMemo struct {
	text        string
	length      int
	ascii       bool // chỉ có nghĩa khi length ≥ 0
	unit, at    int
	prefixASCII bool
	work        uint64 // năng lượng của phần đã đi mà chưa tính (take)
}

// Memos là phần của Run nhớ chuỗi dài nó đọc nhiều lần (runtime cài nó). Memo trả TextMemo của s: cái đã nhớ, hay một
// cái mới (Remember) thay cái cũ nhất. Một lần đếm lại byte của lượt chạy quên mọi chỗ nhớ (chúng là cache, và giữ
// chuỗi chương trình đã bỏ), nên con trỏ Memo trả chỉ dùng được tới lần đếm byte hay lời gọi chương trình tiếp theo:
// một hàm đổi đối số của nó (valueOf có thể chạy) trước, rồi mới lấy con trỏ.
type Memos interface {
	Memo(s string) *TextMemo
}

// Remember là TextMemo mới của s, chưa đi byte nào.
func Remember(s string) TextMemo { return TextMemo{text: s, length: -1, prefixASCII: true} }

// Of là t nhớ đúng chuỗi s: cùng chỗ, cùng độ dài.
func (t *TextMemo) Of(s string) bool {
	return len(t.text) == len(s) && unsafe.StringData(t.text) == unsafe.StringData(s)
}

// Text là chuỗi t giữ (bộ đếm byte đếm nó như cái lượt chạy giữ).
func (t *TextMemo) Text() string { return t.text }

// textOf là TextMemo của s cho một phép: của run khi run nhớ và s dài (từ smallText), không thì local, mới.
func textOf(run Run, s string, local *TextMemo) *TextMemo {
	if len(s) >= smallText {
		if memos, ok := run.(Memos); ok {
			return memos.Memo(s)
		}
	}
	*local = Remember(s)
	return local
}

// take là năng lượng của phần t đã đi từ lần take trước, và quên nó; 0 với chuỗi ngắn (giá của lệnh).
func (t *TextMemo) take() uint64 {
	work := t.work
	t.work = 0
	if len(t.text) < smallText {
		return 0
	}
	return work
}

// walked thêm năng lượng của các byte đã đi: ASCII một lần quét (equalBytesPerUnit), chữ khác từng ký tự
// (unitsBytesPerUnit).
func (t *TextMemo) walked(ascii, other int) {
	t.work += uint64(ascii)/equalBytesPerUnit + uint64(other)/unitsBytesPerUnit
}

// Length là số đơn vị UTF-16 của chuỗi: đếm một lần, từ chỗ con trỏ.
func (t *TextMemo) Length() int {
	if t.length < 0 {
		units, ascii, asciiBytes, other := countUnits(t.text, t.at)
		t.walked(asciiBytes, other)
		t.length = t.unit + units
		t.ascii = t.prefixASCII && ascii
	}
	return t.length
}

// offset là byte bắt đầu đơn vị i (i ≥ 0) và số đơn vị đi được tới đó (reached < i khi chuỗi ngắn hơn i: offset là
// cuối chuỗi). midPair: i rơi vào đơn vị thứ hai của một ký tự bốn byte, offset là chỗ ký tự ấy bắt đầu. Chuỗi đã
// biết là ASCII thì không đi; không thì đi từ con trỏ (hay từ đầu, khi i ở trước nó) và dời con trỏ tới đó.
func (t *TextMemo) offset(i int) (at, reached int, midPair bool) {
	if t.length >= 0 && t.ascii {
		i = min(i, t.length)
		return i, i, false
	}
	if i < t.unit {
		t.unit, t.at, t.prefixASCII = 0, 0, true
	}
	to, done, mid, ascii, other := advance(t.text, t.at, i-t.unit)
	t.walked(ascii, other)
	t.prefixASCII = t.prefixASCII && other == 0
	t.unit, t.at = t.unit+done, to
	if t.at == len(t.text) && t.length < 0 && !mid {
		t.length, t.ascii = t.unit, t.prefixASCII
	}
	return t.at, t.unit + boolInt(mid), mid
}

// unitAtNumber là đơn vị ở chỉ số n (một số nguyên đã đổi, có thể âm hay vô cực), chỉ số ấy, và nó có không.
func (t *TextMemo) unitAtNumber(n float64) (uint16, int, bool) {
	if n < 0 || n >= float64(len(t.text)) {
		return 0, 0, false
	}
	unit, ok := t.unitAt(int(n))
	return unit, int(n), ok
}

// unitAt là đơn vị thứ i và nó có không (i ≥ 0).
func (t *TextMemo) unitAt(i int) (uint16, bool) {
	at, reached, mid := t.offset(i)
	if reached < i || at >= len(t.text) {
		return 0, false
	}
	r := unitReader{text: t.text, at: at}
	unit, _ := r.next()
	if mid {
		unit, _ = r.next()
	}
	return unit, true
}

// slice là các đơn vị [start, end) (0 ≤ start, kẹp ở độ dài): byte của chuỗi khi hai đầu rơi đúng ranh giới ký tự,
// không thì viết lại các đơn vị (cắt giữa một cặp, hiếm), như unitSlice. Con trỏ dừng ở start, không ở end: đoạn
// start..end đi riêng, nên s.slice(-3) hay s.endsWith(x) lần sau bắt đầu ngay ở chỗ con trỏ, không đi lại từ đầu.
func (t *TextMemo) slice(start, end int) string {
	if start >= end {
		return ""
	}
	from, reached, cut := t.offset(start)
	switch {
	case reached < start:
		return ""
	case cut:
		return encodeUnits(units(t.text)[start:min(end, t.Length())])
	case t.length >= 0 && t.ascii:
		return t.text[from:min(end, t.length)]
	}
	to, _, cut, ascii, other := advance(t.text, from, end-start)
	t.walked(ascii, other)
	if cut {
		return encodeUnits(units(t.text)[start:min(end, t.Length())])
	}
	return t.text[from:to]
}

// sliceFrom là các đơn vị từ start tới cuối (0 ≤ start): không cần độ dài.
func (t *TextMemo) sliceFrom(start int) string {
	from, reached, cut := t.offset(start)
	switch {
	case reached < start:
		return ""
	case cut:
		return encodeUnits(units(t.text)[start:])
	}
	return t.text[from:]
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- đi byte ----

// asciiRun là số byte ASCII liền nhau của s từ at, nhiều nhất limit: tám byte một bước.
func asciiRun(s string, at, limit int) int {
	end := min(len(s), at+limit)
	i := at
	for i+8 <= end {
		chunk := uint64(s[i]) | uint64(s[i+1])<<8 | uint64(s[i+2])<<16 | uint64(s[i+3])<<24 |
			uint64(s[i+4])<<32 | uint64(s[i+5])<<40 | uint64(s[i+6])<<48 | uint64(s[i+7])<<56
		if chunk&0x8080808080808080 != 0 {
			break
		}
		i += 8
	}
	for i < end && s[i] < utf8.RuneSelf {
		i++
	}
	return i - at
}

// step là một bước của unitReader ở byte at (s[at] không phải ASCII): bao nhiêu byte, bao nhiêu đơn vị. Hai và ba
// byte đúng luật (phần lớn chữ có dấu, chữ Hán) đi thẳng; còn lại như unitReader: nửa cặp ba byte, rồi DecodeRune
// (byte hỏng là một byte một đơn vị, ký tự bốn byte là hai đơn vị).
func step(s string, at int) (size, units int) {
	b := s[at]
	switch {
	case b >= 0xC2 && b <= 0xDF:
		if at+1 < len(s) && s[at+1]&0xC0 == 0x80 {
			return 2, 1
		}
	case b >= 0xE1 && b <= 0xEF: // ED với hai byte tiếp: một ký tự (80..9F) hay một nửa cặp (A0..BF), cả hai ba byte
		if at+2 < len(s) && s[at+1]&0xC0 == 0x80 && s[at+2]&0xC0 == 0x80 {
			return 3, 1
		}
	}
	if _, ok := loneSurrogateAt(s, at); ok {
		return 3, 1
	}
	c, size := utf8.DecodeRuneInString(s[at:])
	if c >= 0x10000 {
		return size, 2
	}
	return size, 1
}

// advance đi n đơn vị từ byte at: chỗ dừng, số đơn vị đã đi (ít hơn n khi hết chuỗi), và mid khi đơn vị thứ n là
// nửa sau của một ký tự bốn byte (to là chỗ ký tự ấy bắt đầu, done là n−1); ascii, other là số byte đã đi mỗi loại.
func advance(s string, at, n int) (to, done int, mid bool, ascii, other int) {
	for done < n && at < len(s) {
		if s[at] < utf8.RuneSelf {
			k := asciiRun(s, at, n-done)
			at, done, ascii = at+k, done+k, ascii+k
			continue
		}
		size, units := step(s, at)
		if done+units > n {
			return at, done, true, ascii, other
		}
		at, done, other = at+size, done+units, other+size
	}
	return at, done, false, ascii, other
}

// countUnits là số đơn vị từ byte at tới cuối, các byte ấy có toàn ASCII không, và số byte đã đi mỗi loại.
func countUnits(s string, at int) (units int, allASCII bool, ascii, other int) {
	for at < len(s) {
		if s[at] < utf8.RuneSelf {
			k := asciiRun(s, at, len(s))
			at, units, ascii = at+k, units+k, ascii+k
			continue
		}
		size, n := step(s, at)
		at, units, other = at+size, units+n, other+size
	}
	return units, other == 0, ascii, other
}
