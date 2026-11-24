package javascript

import (
	"strings"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/value"
)

// encodeURIComponent, decodeURIComponent, encodeURI, decodeURI theo Encode và Decode của ECMAScript (19.2.6), đối chiếu
// với node: mọi ký tự ngoài tập một hàm giữ viết thành %XX của từng byte UTF-8 của nó, chữ hex hoa; giải mã đọc các
// %XX lại thành ký tự. Hai cặp chỉ khác tập: cặp component giữ chữ cái, chữ số và -_.!~*'() và giải mã mọi thứ;
// encodeURI giữ thêm ;/?:@&=+$,# và decodeURI để nguyên %XX của các ký tự ấy, để một URI trọn vẹn giữ cấu trúc của nó.
// Một nửa cặp đứng một mình không mã hoá được, và một %XX hỏng không giải mã được: URIError của JavaScript ("URI
// malformed"). Chép từ engine2 (core/builtins/uri.go).

const (
	uriMarks    = "-_.!~*'()"   // ký tự ngoài chữ cái và chữ số mà mọi hàm giữ
	uriReserved = ";/?:@&=+$,#" // ký tự làm nên cấu trúc của URI: encodeURI giữ, decodeURI để nguyên
)

var (
	encodeURIComponentFunction = native("encodeURIComponent", 1, uriFunction(encodeURIText, "", 3))
	encodeURIFunction          = native("encodeURI", 1, uriFunction(encodeURIText, uriReserved, 3))
	decodeURIComponentFunction = native("decodeURIComponent", 1, uriFunction(decodeURIText, "", 1))
	decodeURIFunction          = native("decodeURI", 1, uriFunction(decodeURIText, uriReserved, 1))
)

// uriFunction là một trong bốn hàm: đối số đổi thành chữ trước (encodeURIComponent(12.5) là "12.5"), rồi f với tập set.
// grows là kết quả dài gấp mấy lần chữ, nhiều nhất thường gặp: hỏi chỗ trước khi viết (một byte thành %XX là ba).
func uriFunction(f func(text, set string) (string, error), set string, grows uint64) func(Run, []value.Value) value.Value {
	return func(run Run, arguments []value.Value) value.Value {
		text, err := toText(run, argument(arguments, 0))
		if err != nil {
			return value.Error(err)
		}
		if failure := room(run, grows*uint64(len(text))); failure.IsInvalid() {
			return failure
		}
		if failure := spend(run, uint64(len(text))/uriBytesPerUnit); failure.IsInvalid() {
			return failure
		}
		out, err := f(text, set)
		if err != nil {
			return value.Error(err)
		}
		return value.String(out)
	}
}

func malformedURI(what string) error {
	return value.Errorf(value.InvalidInputCode, "URI malformed: %s", what)
}

// unescaped là c nằm trong tập encode giữ: chữ cái, chữ số, uriMarks, và extra.
func unescaped(c byte, extra string) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		strings.IndexByte(uriMarks, c) >= 0 || strings.IndexByte(extra, c) >= 0
}

const upperHex = "0123456789ABCDEF"

// encodeURIText là Encode của ECMAScript: chữ đã là UTF-8, nên mỗi byte của một ký tự ngoài tập là một %XX; một nửa
// cặp đứng một mình (dạng ba byte của nó) không phải ký tự: lỗi. Một byte hỏng là U+FFFD, như Kitwork đọc nó. Kết quả
// dài quá maxStringLength là lỗi trước khi viết gì: mỗi byte có thể thành ba.
func encodeURIText(text, extra string) (string, error) {
	if len(text) > maxStringLength/3 {
		length := 0
		for i := 0; i < len(text); i++ {
			if c := text[i]; c < utf8.RuneSelf && unescaped(c, extra) {
				length++
			} else {
				length += 9 // một byte hỏng thành ba byte của U+FFFD, mỗi byte ba chữ
			}
		}
		if length > maxStringLength {
			return "", tooLong().Error()
		}
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf && unescaped(c, extra) {
			b.WriteByte(c)
			i++
			continue
		}
		if _, lone := loneSurrogateAt(text, i); lone {
			return "", malformedURI("a lone surrogate cannot be encoded")
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		character := text[i : i+size]
		if r == utf8.RuneError && size == 1 {
			character = string(utf8.RuneError)
		}
		for j := 0; j < len(character); j++ {
			b.WriteByte('%')
			b.WriteByte(upperHex[character[j]>>4])
			b.WriteByte(upperHex[character[j]&0xF])
		}
		i += size
	}
	return b.String(), nil
}

// decodeURIText là Decode của ECMAScript: mỗi %XX là một byte; byte 0xxxxxxx là một ký tự (để nguyên %XX khi ký tự ấy
// trong preserve), byte khác mở một chuỗi UTF-8 hai tới bốn byte, mỗi byte sau là một %XX dạng 10xxxxxx, cả chuỗi phải
// là một ký tự hợp lệ (không dạng dài thừa, không nửa cặp, không quá 0x10FFFF). Thứ không phải % chép nguyên.
func decodeURIText(text, preserve string) (string, error) {
	if strings.IndexByte(text, '%') < 0 {
		return text, nil
	}
	b := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] != '%' {
			b = append(b, text[i])
			i++
			continue
		}
		first, ok := hexOctet(text, i)
		if !ok {
			return "", malformedURI("% is not followed by two hexadecimal digits")
		}
		if first < utf8.RuneSelf {
			if strings.IndexByte(preserve, first) >= 0 {
				b = append(b, text[i:i+3]...)
			} else {
				b = append(b, first)
			}
			i += 3
			continue
		}
		var length int
		switch {
		case first&0xE0 == 0xC0:
			length = 2
		case first&0xF0 == 0xE0:
			length = 3
		case first&0xF8 == 0xF0:
			length = 4
		default:
			return "", malformedURI("a byte that begins no UTF-8 character")
		}
		sequence := []byte{first}
		at := i + 3
		for range length - 1 {
			octet, ok := hexOctet(text, at)
			if !ok || octet&0xC0 != 0x80 {
				return "", malformedURI("a UTF-8 sequence cut short")
			}
			sequence = append(sequence, octet)
			at += 3
		}
		// bộ giải mã của Go từ chối đúng thứ ECMAScript từ chối (dạng dài thừa, nửa cặp, quá 0x10FFFF) bằng RuneError cỡ 1;
		// các byte của chính U+FFFD giải mã trọn
		if r, size := utf8.DecodeRune(sequence); r == utf8.RuneError && size != length {
			return "", malformedURI("an invalid UTF-8 sequence")
		}
		b = append(b, sequence...)
		i = at
	}
	return string(b), nil
}

// hexOctet đọc %XX ở i: byte hai chữ hex viết, và có đủ hai chữ hay không.
func hexOctet(text string, i int) (byte, bool) {
	if i+2 >= len(text) || text[i] != '%' {
		return 0, false
	}
	high, low := digitValue(text[i+1]), digitValue(text[i+2])
	return byte(high<<4 | low), high < 16 && low < 16
}
