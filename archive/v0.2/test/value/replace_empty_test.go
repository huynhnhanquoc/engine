package value_test

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// An empty search matches at every place between two UTF-16 units, as JavaScript's replace and
// replaceAll match it, the place inside a character past U+FFFF included, with a $ pattern in the
// replacement or not (05/11/2026): with one, the walk stepped a byte at a time and cut "ạ" into bytes
// that are no UTF-8 ("$\xe1$\xba$\xa1$b$"); without one, strings.Replace stepped a character and left
// "😀" whole. The units are node 22's.
func TestAnEmptySearchMatchesBetweenEveryTwoUnits(t *testing.T) {
	smile := string(rune(0x1F600))
	for _, c := range []struct {
		s, replacement string
		want           []int
	}{
		{"ạb", "$$", []int{36, 7841, 36, 98, 36}},
		{"ạb", "[$&]", []int{91, 93, 7841, 91, 93, 98, 91, 93}},
		{"ạb", "[$`]", []int{91, 93, 7841, 91, 7841, 93, 98, 91, 7841, 98, 93}},
		{"ạb", "[$']", []int{91, 7841, 98, 93, 7841, 91, 98, 93, 98, 91, 93}},
		{smile, "-", []int{45, 55357, 45, 56832, 45}},
		{smile, "$$", []int{36, 55357, 36, 56832, 36}},
		{smile, "[$`]", []int{91, 93, 55357, 91, 55357, 93, 56832, 91, 55357, 56832, 93}},
		{smile, "", []int{55357, 56832}},
		{"a" + smile + "b", "[$']", []int{91, 97, 55357, 56832, 98, 93, 97, 91, 55357, 56832, 98, 93, 55357, 91, 56832, 98, 93, 56832, 91, 98, 93, 98, 91, 93}},
		{"việt", "$$", []int{36, 118, 36, 105, 36, 7879, 36, 116, 36}},
	} {
		got := value.String(c.s).ReplaceAll(value.String(""), value.String(c.replacement))
		if fmt.Sprint(textUnits(got.Text())) != fmt.Sprint(c.want) {
			t.Errorf("%q.replaceAll(\"\", %q): units %v, want %v", c.s, c.replacement, textUnits(got.Text()), c.want)
		}
	}
	if got := value.String("ạb").Replace(value.String(""), value.String("[$']")); got.Text() != "[ạb]ạb" {
		t.Errorf(`"ạb".replace("", "[$']"): %q, want "[ạb]ạb"`, got.Text())
	}
	if got := value.String(smile).ReplaceAll(value.String(""), value.String("")); got.Text() != smile {
		t.Errorf("a pair nothing comes between is the character again: %q", got.Text())
	}
}

// textUnits is a string's UTF-16 units, read from its WTF-8 (a lone surrogate's three-byte form one
// unit); a byte that is no UTF-8 is its negative, so a cut character shows.
func textUnits(s string) []int {
	var out []int
	for i := 0; i < len(s); {
		if i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			out = append(out, 0xD000|int(s[i+1]&0x3F)<<6|int(s[i+2]&0x3F))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			out = append(out, -int(s[i]))
		case r >= 0x10000:
			r -= 0x10000
			out = append(out, 0xD800+int(r>>10), 0xDC00+int(r&0x3FF))
		default:
			out = append(out, int(r))
		}
		i += size
	}
	return out
}
