package value_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/kitwork/engine2/core/value"
)

// A TextCursor reads s[i] and s.length on from where it stood (04/11/2026): a loop over a string that is
// not ASCII walked from the start for each read, O(n²). What it reads must be what JavaScript reads: the
// string's UTF-16 units, here held to unicode/utf16 over Vietnamese, emoji, Korean and lone surrogates,
// at indexes in random order, forward and backward, and to Value.Index over bytes that are not UTF-8.

// unitsText is the String of units and the units themselves, as JavaScript holds them.
func unitsText(parts ...any) (value.Value, []uint16) {
	var units []uint16
	for _, part := range parts {
		switch p := part.(type) {
		case string:
			units = append(units, utf16.Encode([]rune(p))...)
		case uint16:
			units = append(units, p) // a lone surrogate, half of a pair alone
		}
	}
	return value.StringFromUnits(units), units
}

// cursorTexts are the strings every cursor test reads: one of each kind of unit, and mixtures.
func cursorTexts() map[string]func() (value.Value, []uint16) {
	high, low := uint16(0xD83D), uint16(0xDE00)
	return map[string]func() (value.Value, []uint16){
		"Vietnamese": func() (value.Value, []uint16) {
			return unitsText(strings.Repeat("Phường Bến Nghé, Quận 1, Thành phố Hồ Chí Minh. ", 8))
		},
		"emoji":  func() (value.Value, []uint16) { return unitsText(strings.Repeat("😀a🎉🇻🇳 ", 20)) },
		"Korean": func() (value.Value, []uint16) { return unitsText(strings.Repeat("한국어 텍스트 힣", 20)) },
		"lone surrogates": func() (value.Value, []uint16) {
			return unitsText("a", high, "b", low, "c", low, high, "đ", high)
		},
		"mixed": func() (value.Value, []uint16) {
			return unitsText("Xin chào ", high, "😀 한국 ", low, "ASCII run of some length here ", "ở cuối 🎉", high, high, low)
		},
		"ASCII": func() (value.Value, []uint16) { return unitsText(strings.Repeat("abcdefghij", 30)) },
	}
}

// unitAt is what s[i] gives for units: the unit as a one-unit string, undefined outside.
func unitAt(units []uint16, i int) value.Value {
	if i < 0 || i >= len(units) {
		return value.Undefined()
	}
	return value.StringFromUnits([]uint16{units[i]})
}

func TestATextCursorReadsTheUnitsJavaScriptReads(t *testing.T) {
	random := rand.New(rand.NewPCG(4, 11))
	for name, build := range cursorTexts() {
		s, units := build()
		var cursor value.TextCursor
		if got := cursor.Length(s); got != len(units) {
			t.Fatalf("%s: length %d, want %d", name, got, len(units))
		}
		check := func(how string, i int) {
			t.Helper()
			want := unitAt(units, i)
			got := cursor.Index(s, i)
			if !got.Equal(want) && !(got.IsUndefined() && want.IsUndefined()) {
				t.Fatalf("%s, %s: s[%d] = %q, want %q", name, how, i, got.Text(), want.Text())
			}
			if direct := s.Index(i); !direct.Equal(got) && !(direct.IsUndefined() && got.IsUndefined()) {
				t.Fatalf("%s, %s: the cursor reads s[%d] as %q, Value.Index as %q", name, how, i, got.Text(), direct.Text())
			}
		}
		for i := 0; i <= len(units); i++ {
			check("forward", i)
		}
		for i := len(units) + 1; i >= -1; i-- {
			check("backward", i)
		}
		for round := 0; round < 2000; round++ {
			check("random", random.IntN(len(units)+4)-2)
		}
		cursor.Reset()
		for round := 0; round < 200; round++ { // a fresh cursor, reading before it knows the length
			check("random after Reset", random.IntN(len(units)+4)-2)
		}
		if got := cursor.Length(s); got != len(units) {
			t.Errorf("%s: length after reads %d, want %d", name, got, len(units))
		}
	}
}

// One cursor read across several strings, in turn, and a copy of a string at another address: what it
// remembers of one string is never read as another's.
func TestATextCursorKeepsStringsApart(t *testing.T) {
	random := rand.New(rand.NewPCG(11, 4))
	var cursor value.TextCursor
	type text struct {
		value value.Value
		units []uint16
	}
	var texts []text
	for _, build := range cursorTexts() {
		s, units := build()
		texts = append(texts, text{s, units}, text{value.String(strings.Clone(s.Text())), units})
		texts = append(texts, text{value.String(s.Text()[:len(s.Text())/2]), nil}) // a prefix sharing the bytes
	}
	for i := range texts {
		if texts[i].units == nil {
			texts[i].units = utf16Of(texts[i].value)
		}
	}
	for round := 0; round < 5000; round++ {
		x := texts[random.IntN(len(texts))]
		if random.IntN(4) == 0 {
			if got := cursor.Length(x.value); got != len(x.units) {
				t.Fatalf("length %d, want %d", got, len(x.units))
			}
			continue
		}
		i := random.IntN(len(x.units)+2) - 1
		if got, want := cursor.Index(x.value, i), unitAt(x.units, i); !got.Equal(want) && !(got.IsUndefined() && want.IsUndefined()) {
			t.Fatalf("round %d: s[%d] = %q, want %q", round, i, got.Text(), want.Text())
		}
	}
	other := value.Array([]value.Value{value.Number(7)})
	if cursor.Length(other) != 1 || cursor.Index(other, 0).Number() != 7 || cursor.Length(value.String("")) != 0 || !cursor.Index(value.String(""), 0).IsUndefined() {
		t.Error("a cursor reads any other kind, and the empty string, as Value does")
	}
}

// utf16Of is the units of a String, read through Value.Index, the reference reading.
func utf16Of(s value.Value) []uint16 {
	n := int(s.Get("length").Number())
	units := make([]uint16, n)
	for i := range units {
		units[i] = uint16(s.Index(i).Invoke("charCodeAt", []value.Value{value.Number(0)}).Number())
	}
	return units
}

// Bytes that are not UTF-8 (a body read as text) are units too, a bad byte U+FFFD on its own: the cursor
// reads them as Value.Index does, forward, backward and at random.
func TestATextCursorReadsBytesThatAreNotUTF8(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 7))
	pieces := [][]byte{{0x80}, {0xBF, 0xBF}, {0xC3}, {0xE4, 0xB8}, {0xF0, 0x9F, 0x98}, {0xED, 0xA0}, {0xED, 0xB0, 0x80},
		[]byte("ư"), []byte("😀"), []byte("한"), {'a'}, {0xFF}, {0xF0, 0x9F, 0x98, 0x80, 0x80}}
	for trial := 0; trial < 300; trial++ {
		var b []byte
		for n := random.IntN(12) + 1; n > 0; n-- {
			b = append(b, pieces[random.IntN(len(pieces))]...)
		}
		s := value.Bytes(b).ToString()
		length := int(s.Get("length").Number())
		var cursor value.TextCursor
		if got := cursor.Length(s); got != length {
			t.Fatalf("% x: length %d, want %d", b, got, length)
		}
		order := make([]int, 0, 3*length+6)
		for i := -1; i <= length; i++ {
			order = append(order, i)
		}
		for i := length; i >= -1; i-- {
			order = append(order, i)
		}
		for n := 0; n < length+2; n++ {
			order = append(order, random.IntN(length+2)-1)
		}
		for _, i := range order {
			got, want := cursor.Index(s, i), s.Index(i)
			if !got.Equal(want) && !(got.IsUndefined() && want.IsUndefined()) {
				t.Fatalf("% x: s[%d] = %q, Value.Index gives %q", b, i, got.Text(), want.Text())
			}
		}
	}
}

// What the cursor is for: a loop over a string, s[i] and s.length each turn, costs in proportion to the
// string, forward and backward: one loop over 1,000 units of Vietnamese costs about what ten loops over
// 100 units cost, where walking from the start made it ten times as much (O(n²)).
func TestATextCursorReadsALoopInLinearTime(t *testing.T) {
	long := value.String(strings.Repeat("Phường Bến", 100))
	short := value.String(strings.Repeat("Phường Bến", 10))
	forward := func(s value.Value, times int) func(int) {
		return func(rounds int) {
			var cursor value.TextCursor
			for round := 0; round < rounds*times; round++ {
				for i := 0; i < cursor.Length(s); i++ {
					sinkValue = cursor.Index(s, i)
				}
			}
		}
	}
	backward := func(s value.Value, times int) func(int) {
		return func(rounds int) {
			var cursor value.TextCursor
			for round := 0; round < rounds*times; round++ {
				for i := cursor.Length(s) - 1; i >= 0; i-- {
					sinkValue = cursor.Index(s, i)
				}
			}
		}
	}
	requireCost(t, "a forward loop over 1,000 units", forward(long, 1), forward(short, 10), 2.5)
	requireCost(t, "a backward loop over 1,000 units", backward(long, 1), backward(short, 10), 2.5)
}
