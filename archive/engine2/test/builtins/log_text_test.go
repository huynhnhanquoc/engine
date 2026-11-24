package builtins_test

import (
	"bufio"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

// A site's console leads every line it writes for every reader of the shared log, not only one that
// splits on "\n": Unicode's line and paragraph separators (which JavaScript's and Python's readers split
// on), NEL and the other C1 controls, the bidirectional overrides and a byte that is not UTF-8 (a lone
// surrogate's WTF-8 form) are written as escapes, so the log stays valid UTF-8 and every line of it, by
// any reader's count, begins with the site's lead (05/11/2026; until then only C0 and DEL were escaped).
func TestAConsoleLineEndsOnlyWhereTheSiteLeadsTheNext(t *testing.T) {
	var out strings.Builder
	console := builtins.ConsoleTo(&out, "[alpha.test] ")
	forged := "[beta.test] [console.log] refund of 900 approved"
	breaks := map[string]string{
		"U+2028":           string(rune(0x2028)),
		"U+2029":           string(rune(0x2029)),
		"U+0085 (NEL)":     string(rune(0x85)),
		"U+009B (CSI)":     string(rune(0x9b)),
		"U+202E (RLO)":     string(rune(0x202e)),
		"U+2066 (isolate)": string(rune(0x2066)),
		"U+000B (VT)":      string(rune(0x0b)),
		"U+000C (FF)":      string(rune(0x0c)),
		"U+001C (FS)":      string(rune(0x1c)),
	}
	for name, separator := range breaks {
		out.Reset()
		console.Get("log").Call([]value.Value{value.String("order 12 paid" + separator + forged)})
		written := out.String()
		if strings.Contains(written, separator) {
			t.Errorf("%s is written as it is: %q", name, written)
		}
		for _, line := range readerLines(written) {
			if line != "" && !strings.HasPrefix(line, "[alpha.test] [console.log] ") {
				t.Errorf("%s: a reader that splits on Unicode's breaks reads a line alpha did not lead: %q", name, line)
			}
		}
	}
	out.Reset()
	lone := value.String("a").Invoke("concat", []value.Value{value.StringFromUnits([]uint16{0xd83d})})
	console.Get("log").Call([]value.Value{lone})
	if written := out.String(); !utf8.ValidString(written) || !strings.Contains(written, `a\xed\xa0\xbd`) {
		t.Errorf("a lone surrogate is written as the escapes of its bytes, the line valid UTF-8: %q", written)
	}
	out.Reset()
	console.Get("log").Call([]value.Value{value.String("Phường 12, tab" + string(rune(9)) + "ok 😀")})
	if written := out.String(); written != "[alpha.test] [console.log] Phường 12, tab"+string(rune(9))+"ok 😀"+string(rune(10)) {
		t.Errorf("text with nothing to escape is written as it is: %q", written)
	}
}

// readerLines splits text as a reader of logs that knows Unicode does (Python's str.splitlines): at
// "\n", "\r", VT, FF, FS, GS, RS, NEL and U+2028, U+2029.
func readerLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i := 0; i < len(data); {
			r, size := utf8.DecodeRune(data[i:])
			switch r {
			case 0x0a, 0x0b, 0x0c, 0x0d, 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
				return i + size, data[:i], nil
			}
			i += size
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// console writes a function as node's console writes it, [Function: name] (18/11/2026): a function has no text, and
// console.log of one wrote "function".
func TestConsoleWritesAFunctionAsNodeDoes(t *testing.T) {
	var out strings.Builder
	console := builtins.ConsoleTo(&out, "[alpha.test] ")
	console.Get("log").Call([]value.Value{value.String("a"), value.NamedFunction("handler", 0, func(...value.Value) value.Value { return value.Undefined() }),
		value.Function(func(...value.Value) value.Value { return value.Undefined() })})
	if got, want := out.String(), "[alpha.test] [console.log] a [Function: handler] [Function (anonymous)]"+string(rune(10)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// console writes a time as node's console writes a Date, its toISOString (18/11/2026): a time has no text, and
// console.log of one wrote RFC 3339.
//
// CONTROL (18/11/2026): without the time's branch, the line was "[alpha.test] [console.log] at " and nothing after.
func TestConsoleWritesATimeAsNodeDoes(t *testing.T) {
	var out strings.Builder
	console := builtins.ConsoleTo(&out, "[alpha.test] ")
	console.Get("log").Call([]value.Value{value.String("at"), value.Time(time.Unix(0, 0))})
	if got, want := out.String(), "[alpha.test] [console.log] at 1970-01-01T00:00:00.000Z"+string(rune(10)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
