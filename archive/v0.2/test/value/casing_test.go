package value_test

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"os/exec"
	"strconv"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// toUpperCase and toLowerCase give what node gives (18/11/2026, docs/language.md mục 4.3): every code point alone,
// both ways, and 4,000 texts drawn from a fixed seed around Σ, whose lowercase is ς where it ends a word (cased letters,
// case-ignorable marks, quotes, points and format characters before and after it), and the full mappings that make
// one character two or three ("ß", "ﬁ", "İ"). A lone surrogate is kept as it is.
//
// CONTROL (18/11/2026): with Go's one-to-one strings.ToUpper and strings.ToLower, 157 code points and 1,913 of the
// texts differed from node, "ß" and "ΑΣ" among them.
func TestCaseMappingIsNodes(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	alphabet := []rune{0x03A3, 'A', 'a', 0x03B1, '\'', '.', ':', ' ', '1', 0x0301, 0x02B0, 0x00AD, 0x24B6, 0x01C5, 0x00DF,
		0x0130, 0xFB01, 0x0345, 0x2019, 0x00B7}
	random := rand.New(rand.NewSource(18112026))
	texts := make([]string, 4000)
	for i := range texts {
		runes := make([]rune, 1+random.Intn(7))
		for j := range runes {
			runes[j] = alphabet[random.Intn(len(alphabet))]
		}
		texts[i] = string(runes)
	}
	encoded, _ := json.Marshal(texts)
	command := exec.Command("node", "-e", `const texts = JSON.parse(require("fs").readFileSync(0, "utf8"));
const up = {}, low = {};
for (let c = 0; c <= 0x10FFFF; c++) {
  if (c >= 0xD800 && c <= 0xDFFF) continue;
  const s = String.fromCodePoint(c);
  if (s.toUpperCase() !== s) up[c] = s.toUpperCase();
  if (s.toLowerCase() !== s) low[c] = s.toLowerCase();
}
process.stdout.write(JSON.stringify({ up, low, texts: texts.map((s) => [s.toUpperCase(), s.toLowerCase()]) }));`)
	command.Stdin = bytes.NewReader(encoded)
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected struct {
		Up, Low map[string]string
		Texts   [][2]string
	}
	if err := json.Unmarshal(raw, &expected); err != nil || len(expected.Texts) != len(texts) {
		t.Fatalf("node output: %v", err)
	}
	upper := func(s string) string { return value.String(s).ToUpperCase().Text() }
	lower := func(s string) string { return value.String(s).ToLowerCase().Text() }
	points := 0
	for c := rune(0); c <= 0x10FFFF; c++ {
		if c >= 0xD800 && c <= 0xDFFF {
			continue
		}
		s := string(c)
		key := strconv.Itoa(int(c))
		wantUp, mapped := expected.Up[key]
		if !mapped {
			wantUp = s
		}
		wantLow, mapped := expected.Low[key]
		if !mapped {
			wantLow = s
		}
		if got := upper(s); got != wantUp {
			if points++; points <= 5 {
				t.Errorf("U+%04X upper: %q, node %q", c, got, wantUp)
			}
		}
		if got := lower(s); got != wantLow {
			if points++; points <= 5 {
				t.Errorf("U+%04X lower: %q, node %q", c, got, wantLow)
			}
		}
	}
	differ := 0
	for i, s := range texts {
		if got := [2]string{upper(s), lower(s)}; got != expected.Texts[i] {
			if differ++; differ <= 5 {
				t.Errorf("%q: %q, node %q", s, got, expected.Texts[i])
			}
		}
	}
	if points > 0 || differ > 0 {
		t.Errorf("%d code points and %d of %d texts differ from node", points, differ, len(texts))
	}
	lone := "a\xed\xa0\xbdb" // a lone high surrogate between two letters, as a text holds it
	if got := upper(lone); got != "A\xed\xa0\xbdB" {
		t.Errorf("a lone surrogate's text upper: %q", got)
	}
	if got := lower("ΑΣ\xed\xa0\xbd"); got != "ας\xed\xa0\xbd" {
		t.Errorf("a final sigma before a lone surrogate: %q", got)
	}
}
