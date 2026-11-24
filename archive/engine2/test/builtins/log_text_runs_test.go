package builtins_test

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/builtins"
)

// LogText copies a line a run at a time and decodes only where an escape may begin (05/11/2026: it decoded
// and appended every character but ASCII alone, 2.2 times the time for a line of Vietnamese). It writes
// what the character at a time loop wrote, byte for byte: escapes at the boundaries of each range it
// escapes, text that is not UTF-8 among them, and the cut of a text past 64 KiB.
func TestALineCopiedARunAtATimeIsTheLineEscapedACharacterAtATime(t *testing.T) {
	pieces := []string{
		"a", "Z", " ", "\t", "\n", "[beta.test] ", string(rune(0x01)), string(rune(0x1b)), string(rune(0x1f)), string(rune(0x7f)),
		"ạ", "ế", "ữ", "đ", "😀", string(rune(0x80)), string(rune(0x85)), string(rune(0x9f)), string(rune(0xa0)), string(rune(0xff)),
		string(rune(0x2027)), string(rune(0x2028)), string(rune(0x2029)), string(rune(0x202a)), string(rune(0x202e)), string(rune(0x202f)),
		string(rune(0x2065)), string(rune(0x2066)), string(rune(0x2069)), string(rune(0x206a)), string(rune(0x20ac)), string(rune(0xfffd)),
		string([]byte{0x80}), string([]byte{0xbf}), string([]byte{0xc2}), string([]byte{0xe2}), string([]byte{0xe2, 0x80}),
		string([]byte{0xe2, 0x80, 0xa8, 0xa8}), string([]byte{0xc0, 0x80}), string([]byte{0xed, 0xa0, 0x80}), string([]byte{0xf5}),
		string([]byte{0xf0, 0x9f, 0x98}), string([]byte{0xc2, 0x85}), string([]byte{0xc2, 0xc2, 0x85}),
	}
	random := rand.New(rand.NewSource(5112026))
	for round := 0; round < 20_000; round++ {
		var text strings.Builder
		for count := random.Intn(24); count > 0; count-- {
			text.WriteString(pieces[random.Intn(len(pieces))])
		}
		compare(t, text.String())
	}
	// past the cut, with characters of every width across it
	for _, filler := range []string{"a", "ạ", "😀", string(rune(0x2028)), string([]byte{0xe2, 0x80})} {
		for shift := 0; shift < 4; shift++ {
			compare(t, strings.Repeat("b", shift)+strings.Repeat(filler, (64<<10)/len(filler)+3))
		}
	}
}

func compare(t *testing.T, text string) {
	t.Helper()
	if got, want := builtins.LogText("[alpha.test] ", text), escapedACharacterAtATime("[alpha.test] ", text); string(got) != string(want) {
		t.Fatalf("LogText(%q) = %q, want %q", text, got, want)
	}
}

// escapedACharacterAtATime is LogText as it was until 05/11/2026, the reference the run at a time copy is
// held to.
func escapedACharacterAtATime(lead, text string) []byte {
	const maxConsoleText = 64 << 10
	const hexDigits = "0123456789abcdef"
	escape := func(out []byte, b byte) []byte { return append(out, '\\', 'x', hexDigits[b>>4], hexDigits[b&15]) }
	cut := 0
	if len(text) > maxConsoleText {
		end := maxConsoleText
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		cut = len(text) - end
		text = text[:end]
	}
	out := append([]byte(nil), lead...)
	for i := 0; i < len(text); {
		character := text[i]
		if character < utf8.RuneSelf {
			switch {
			case character == '\n':
				out = append(out, '\n')
				out = append(out, lead...)
			case character < 0x20 && character != '\t' || character == 0x7f:
				out = escape(out, character)
			default:
				out = append(out, character)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			out = escape(out, character)
		case r < 0xa0:
			out = escape(out, byte(r))
		case r == 0x2028 || r == 0x2029 || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069:
			out = append(out, '\\', 'u', hexDigits[r>>12], hexDigits[r>>8&15], hexDigits[r>>4&15], hexDigits[r&15])
		default:
			out = append(out, text[i:i+size]...)
		}
		i += size
	}
	if cut > 0 {
		out = append(out, " … ("+strconv.Itoa(cut)+" bytes more)"...)
	}
	return append(out, '\n')
}
