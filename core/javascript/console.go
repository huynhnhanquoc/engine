package javascript

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/value"
)

// console của JavaScript: log, error, warn, info, debug. Mỗi lời gọi viết một dòng: chữ của các đối số nối bằng một
// khoảng trắng (object là "[object Object]" như String của nó, không đọc %s), đầu dòng là lead rồi tên của chữ
// ("[shop.com] [console.log] "). Chỗ khác String: một hàm viết như node viết nó, [Function: tên]; một thời điểm viết
// toISOString như node viết một Date. Một đối số không có chữ (struct của engine, toString riêng ném) là lỗi của lời
// gọi, và không gì được viết. Chép từ engine2 (core/builtins/builtins.go, docs/language.md mục 4.3); app.logger bỏ ngày
// 22/11/2026, nên không còn mức log: mọi chữ viết vào cùng một nơi.

// consoleWords là các chữ của console, theo thứ tự.
var consoleWords = []string{"log", "error", "warn", "info", "debug"}

// consoleNamespace là console của Global: viết ra os.Stdout, không lead.
var consoleNamespace = Console(os.Stdout, "")

// Console là một console viết mỗi dòng vào w, đầu dòng là lead: host dựng một cái cho mỗi site lúc boot
// (Console(w, "[shop.com] ")) và đặt nó vào chỗ của tên console, thay cái của Global. Mỗi lời gọi là một lần Write,
// nên một w có khoá giữ trọn dòng của các request chạy cùng lúc. Trả value.Struct của một *Namespace, chỉ đọc.
func Console(w io.Writer, lead string) value.Value {
	members := make([]member, len(consoleWords))
	for i, name := range consoleWords {
		prefix := lead + "[console." + name + "] "
		members[i] = member{name, native(name, 0, func(run Run, arguments []value.Value) value.Value {
			parts := make([]string, len(arguments))
			for j, a := range arguments {
				text, err := consoleText(run, a)
				if err != nil {
					return value.Error(fmt.Errorf("console.%s: %w", name, err))
				}
				parts[j] = text
			}
			w.Write(logLine(prefix, parts))
			return value.Undefined()
		})}
	}
	return value.Struct(newNamespace("console", members...))
}

// consoleText là chữ console viết cho a: hàm như node viết ([Function: tên], [Function (anonymous)]), thời điểm như
// toISOString, struct của engine là lỗi (nó không phải dữ liệu), còn lại là String(a).
func consoleText(run Run, a value.Value) (string, error) {
	switch {
	case a.IsFunction():
		if named, ok := a.Function().(interface{ Name() string }); ok && named.Name() != "" {
			return "[Function: " + named.Name() + "]", nil
		}
		return "[Function (anonymous)]", nil
	case a.IsTime():
		return isoText(a.Time()), nil
	case a.IsStruct():
		return "", notData(a)
	}
	return toText(run, a)
}

// maxConsoleText là số byte chữ nhiều nhất một lời gọi console viết; phần còn lại được đếm, không viết.
const maxConsoleText = 64 << 10

// logLine là dòng console viết cho các phần chữ parts nối bằng khoảng trắng: mỗi dòng của chữ mở đầu bằng lead và
// kết thúc bằng xuống dòng; quá maxConsoleText byte thì cắt ở một ký tự và nói còn bao nhiêu byte. Thứ có thể kết
// thúc một dòng hay vẽ đè lên một dòng, với một terminal hay một người đọc log, viết thành thoát, để không dòng nào mở
// đầu bằng tên nào khác lead (một site không viết được một dòng mang tên site khác vào log chung):
//
//   - ký tự điều khiển (C0 trừ tab, DEL, và C1: U+0085, U+009B) thành \xNN;
//   - byte không phải UTF-8, kể cả dạng WTF-8 của một nửa cặp, thành \xNN;
//   - U+2028, U+2029 (JavaScript và Python ngắt dòng ở đó), các ký tự nhúng, đè, cô lập hai chiều (U+202A tới U+202E,
//     U+2066 tới U+2069) thành dạng thoát u sáu ký tự JavaScript viết.
func logLine(lead string, parts []string) []byte {
	total := max(0, len(parts)-1)
	for _, part := range parts {
		total += len(part)
	}
	// chỉ dựng phần sẽ viết (thêm vài byte để cắt ở đầu một ký tự): ba đối số mỗi cái 100 MB không thành 300 MB ở đây
	text := make([]byte, 0, min(total, maxConsoleText+utf8.UTFMax))
	for i, part := range parts {
		if i > 0 {
			text = append(text, ' ')
		}
		text = append(text, part[:min(len(part), cap(text)-len(text))]...)
		if len(text) == cap(text) {
			break
		}
	}
	cut := 0
	if total > maxConsoleText {
		end := maxConsoleText
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		cut = total - end
		text = text[:end]
	}
	out := make([]byte, 0, len(lead)+len(text)+32)
	out = append(out, lead...)
	start := 0
	for i := 0; i < len(text); {
		character := text[i]
		if character < utf8.RuneSelf {
			if character >= 0x20 && character != 0x7f || character == '\t' {
				i++
				continue
			}
			out = append(out, text[start:i]...)
			if character == '\n' {
				out = append(out, '\n')
				out = append(out, lead...)
			} else {
				out = appendByteEscape(out, character)
			}
			i++
			start = i
			continue
		}
		size := utf8Length(text, i)
		switch {
		case size == 0:
			out = append(out, text[start:i]...)
			out = appendByteEscape(out, character)
			size = 1
		case character == 0xc2 && text[i+1] < 0xa0: // U+0080 tới U+009F: byte thứ hai là chính mã
			out = append(out, text[start:i]...)
			out = appendByteEscape(out, text[i+1])
		case character == 0xe2:
			r := 0x2000 | rune(text[i+1]&0x3f)<<6 | rune(text[i+2]&0x3f)
			if r != 0x2028 && r != 0x2029 && (r < 0x202a || r > 0x202e) && (r < 0x2066 || r > 0x2069) {
				i += size
				continue
			}
			out = append(out, text[start:i]...)
			out = append(out, '\\', 'u', hexDigits[r>>12], hexDigits[r>>8&15], hexDigits[r>>4&15], hexDigits[r&15])
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	out = append(out, text[start:]...)
	if cut > 0 {
		out = append(out, " … ("+strconv.Itoa(cut)+" bytes more)"...)
	}
	return append(out, '\n')
}

// appendByteEscape viết \xNN của b.
func appendByteEscape(out []byte, b byte) []byte {
	return append(out, '\\', 'x', hexDigits[b>>4], hexDigits[b&15])
}

// utf8Length là độ dài của ký tự nhiều byte mở ở text[i], hay 0 khi các byte ở đó không phải một ký tự (byte không mở
// ký tự nào, chuỗi bị cắt, dạng dài thừa, nửa cặp, quá U+10FFFF): cỡ utf8.DecodeRune cho một ký tự nó giải mã được,
// theo khoảng nó nhận cho byte thứ hai, không giải mã.
func utf8Length(text []byte, i int) int {
	lead := text[i]
	switch {
	case lead < 0xc2:
		return 0 // byte tiếp nối, hay byte đầu của dạng dài thừa hai byte
	case lead < 0xe0:
		if i+1 < len(text) && text[i+1]&0xc0 == 0x80 {
			return 2
		}
	case lead < 0xf0:
		if i+2 >= len(text) {
			return 0
		}
		low, high := byte(0x80), byte(0xbf)
		if lead == 0xe0 {
			low = 0xa0 // dạng dài thừa ở dưới
		} else if lead == 0xed {
			high = 0x9f // nửa cặp ở trên
		}
		if second := text[i+1]; second >= low && second <= high && text[i+2]&0xc0 == 0x80 {
			return 3
		}
	case lead < 0xf5:
		if i+3 >= len(text) {
			return 0
		}
		low, high := byte(0x80), byte(0xbf)
		if lead == 0xf0 {
			low = 0x90 // dạng dài thừa ở dưới
		} else if lead == 0xf4 {
			high = 0x8f // quá U+10FFFF ở trên
		}
		if second := text[i+1]; second >= low && second <= high && text[i+2]&0xc0 == 0x80 && text[i+3]&0xc0 == 0x80 {
			return 4
		}
	}
	return 0
}
