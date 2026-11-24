package value

import (
	"unicode/utf8"
	"unsafe"
)

// TextCursor reads a string's length and its units by index without walking from the start each time.
// A string's units are UTF-16 code units over UTF-8 bytes (text.go), so s[i] and s.length on a string
// that is not ASCII walk the bytes: a loop `for (let i = 0; i < s.length; i++) { s[i] }` walked from the
// start twice a turn, O(n²) (4.6 ms for 1,000 units of Vietnamese, 04/11/2026). A cursor remembers the
// last string it read (its bytes' address and length) with its length in units once counted, and the
// unit it stands at with that unit's byte offset; the next read of the same string goes on from there,
// forward or backward, or from the start or the end when one of them is nearer. The runtime holds one
// per VM and resets it with the VM; Value.Index and Get("length") stay as they are for a caller with none.
//
// What it reads costs the run's energy (15/11/2026): a length counted, each step it walks (Owed). A cursor meets
// one string at a time, so a.length + b.length, or a[i] + b[i], of two texts other than ASCII read each again
// from the start every turn (12 ms a turn for two of 3 MB) at the price of their instructions.
//
// The zero TextCursor is ready to use. It is not safe for use by two goroutines at once.
type TextCursor struct {
	data   *byte  // the bytes of the last string read; a pointer, so the string cannot be freed and another put at its address
	size   int    // its length in bytes
	units  int    // its length in units, -1 until counted
	ascii  bool   // the string is all ASCII, found when its length was counted: unit i is byte i
	unit   int    // the unit the cursor stands at
	offset int    // the byte offset of the character that holds that unit
	second bool   // the unit is the second of a pair: the low half of a character past U+FFFF
	work   uint64 // what it read since Owed last gave it, in units of energy (work.go)
	short  bool   // the string is shorter than LongText: what is read of it is the instruction's price

	// recent is the one-unit strings last read past ASCII, by unit, so that a loop over Vietnamese makes
	// each of its letters once rather than once a read. A one-unit string says nothing of the string it
	// came from, so the table outlives Reset.
	recent      [recentUnits]Value
	recentUnits [recentUnits]uint16
}

// recentUnits is how many one-unit strings a cursor keeps, each in the place its unit's low bits give.
const recentUnits = 32

// Reset forgets the last string, as a VM forgets its run.
func (c *TextCursor) Reset() {
	c.data, c.size, c.units, c.ascii, c.unit, c.offset, c.second = nil, 0, -1, false, 0, 0, false
	c.work, c.short = 0, false
}

// Owed is what the cursor read since it last gave it, in units of energy, once that is smallWork or more; 0
// before, the reading kept for a later call. A length counted reads the text once (a unit for EqualBytesPerUnit
// of ASCII, for countBytesPerUnit of other text), each step of a walk stepUnits (a character, or a run of ASCII
// and a unit for EqualBytesPerUnit of it): a loop over one text pays a unit a character it walks, a turn that
// reads two texts in turn pays each text again. What is read of a text shorter than LongText is the price of the
// instruction that reads it, as a short text compared or a short key hashed is.
func (c *TextCursor) Owed() uint64 {
	if c.work < smallWork {
		return 0
	}
	owed := c.work
	c.work = 0
	return owed
}

// Length is v.length: a string's length in units, counted once for each string the cursor meets; every
// other kind's as Get("length") counts it (an array's items, bytes), 0 for a kind with none.
func (c *TextCursor) Length(v Value) int {
	if v.kind != text {
		return v.length()
	}
	s := v.Text()
	if s == "" {
		return 0
	}
	c.attach(s)
	if c.units < 0 {
		if c.ascii = isASCII(s); c.ascii {
			c.units = len(s)
			c.work += uint64(len(s)) / EqualBytesPerUnit
		} else {
			c.units = unitLength(s)
			if !c.short {
				c.work += uint64(len(s)) / countBytesPerUnit
			}
		}
	}
	return c.units
}

// Index is v[i]: a string's unit i as a one-unit string, as Value.Index gives it, read on from where the
// cursor stands; every other kind as Value.Index reads it.
func (c *TextCursor) Index(v Value, i int) Value {
	if v.kind != text {
		return v.Index(i)
	}
	s := v.Text()
	if i < 0 || i >= len(s) { // a string has no more units than bytes
		return Value{}
	}
	c.attach(s)
	if c.ascii {
		return asciiUnitValues[s[i]]
	}
	unit, ok := c.seek(s, i)
	switch {
	case !ok:
		return Value{}
	case unit < utf8.RuneSelf:
		return asciiUnitValues[unit]
	}
	place := unit % recentUnits
	if c.recentUnits[place] != unit {
		c.recent[place], c.recentUnits[place] = String(unitString(unit)), unit
	}
	return c.recent[place]
}

// CodePointAt is the character of s that starts at byte at, and the byte the next starts at: what for…of over a
// string reads (runtime ITER), an ASCII one made once (asciiUnitValues).
func CodePointAt(s string, at int) (Value, int) {
	if at >= 0 && at < len(s) && s[at] < utf8.RuneSelf {
		return asciiUnitValues[s[at]], at + 1
	}
	_, size := utf8.DecodeRuneInString(s[at:])
	return String(s[at : at+size]), at + size
}

// asciiUnitValues is the String of each ASCII unit, made once: s[i] of an ASCII letter allocates nothing.
var asciiUnitValues = func() (table [utf8.RuneSelf]Value) {
	for unit := range table {
		table[unit] = String(asciiStrings[unit])
	}
	return table
}()

// attach keeps the cursor where it stands when s is the string it read last: the same bytes at the same
// address are the same text, since a Go string never changes; any other string starts it afresh.
func (c *TextCursor) attach(s string) {
	if data := unsafe.StringData(s); data != c.data || len(s) != c.size {
		c.data, c.size, c.units, c.ascii, c.unit, c.offset, c.second = data, len(s), -1, false, 0, 0, false
		c.short = len(s) < LongText
	}
}

// seek moves the cursor to unit i of s and gives that unit; false when s has no unit i.
func (c *TextCursor) seek(s string, i int) (uint16, bool) {
	if c.units >= 0 && i >= c.units {
		return 0, false
	}
	// set out from the nearest place known: the start, the cursor, or the end once the length is known
	distance := i - c.unit
	if distance < 0 {
		distance = -distance
	}
	switch {
	case c.units >= 0 && c.units-i < min(distance, i):
		c.unit, c.offset, c.second = c.units, len(s), false
	case i < distance:
		c.unit, c.offset, c.second = 0, 0, false
	}
	for c.unit < i {
		if c.offset >= len(s) {
			c.units = c.unit // walked off the end: now the length is known
			return 0, false
		}
		if !c.second && s[c.offset] < utf8.RuneSelf {
			// a run of ASCII is one unit a byte: skipped at once, up to unit i
			run := asciiPrefix(s[c.offset:min(len(s), c.offset+i-c.unit)])
			c.offset += run
			c.unit += run
			if !c.short {
				c.work += stepUnits + uint64(run)/EqualBytesPerUnit
			}
			continue
		}
		if !c.short {
			c.work += stepUnits
		}
		size, units := characterAt(s, c.offset)
		if units == 2 && !c.second {
			c.second = true
		} else {
			c.offset += size
			c.second = false
		}
		c.unit++
	}
	for c.unit > i {
		if !c.short {
			c.work += stepUnits
		}
		switch {
		case c.second:
			c.second = false
			c.unit--
		case s[c.offset-1] < utf8.RuneSelf:
			// a run of ASCII before the cursor: skipped back at once, down to unit i
			run := asciiSuffix(s[max(0, c.offset-(c.unit-i)):c.offset])
			c.offset -= run
			c.unit -= run
			c.work += uint64(run) / EqualBytesPerUnit
		default:
			c.offset = previousCharacter(s, c.offset)
			_, units := characterAt(s, c.offset)
			c.second = units == 2
			c.unit--
		}
	}
	if c.offset >= len(s) {
		c.units = c.unit
		return 0, false
	}
	return c.current(s), true
}

// current is the unit the cursor stands at.
func (c *TextCursor) current(s string) uint16 {
	if b := s[c.offset]; b < utf8.RuneSelf {
		return uint16(b)
	}
	if unit, ok := loneSurrogateAt(s, c.offset); ok {
		return unit
	}
	r, _ := utf8.DecodeRuneInString(s[c.offset:])
	if r < 0x10000 {
		return uint16(r) // RuneError for a bad byte, as unitReader reads it
	}
	r -= 0x10000
	if c.second {
		return uint16(0xDC00 + r&0x3FF)
	}
	return uint16(0xD800 + r>>10)
}

// characterAt is the character that starts at offset, as unitReader reads it: its size in bytes and
// its units, 2 for a character past U+FFFF and 1 for any other (a lone surrogate's three bytes, and a
// bad byte, which is U+FFFD on its own).
func characterAt(s string, offset int) (size, units int) {
	if s[offset] < utf8.RuneSelf {
		return 1, 1
	}
	if _, ok := loneSurrogateAt(s, offset); ok {
		return 3, 1
	}
	r, size := utf8.DecodeRuneInString(s[offset:])
	if r < 0x10000 {
		return size, 1
	}
	return size, 2
}

// previousCharacter is where the character that ends at offset starts, offset being where one starts.
// Every byte that is not a continuation byte (10xxxxxx) starts a character as unitReader reads them, and
// a character is at most four bytes; so the character before offset starts at the last such byte in the
// four before it, when the character read from there ends at offset. Anything else ending at offset (a
// continuation byte with no lead, the tail of a character cut short) is one bad byte, a character alone.
func previousCharacter(s string, offset int) int {
	for p := offset - 1; p >= 0 && p >= offset-4; p-- {
		if s[p]&0xC0 != 0x80 {
			if size, _ := characterAt(s, p); p+size == offset {
				return p
			}
			break
		}
	}
	return offset - 1
}

// asciiPrefix is how many bytes s begins with that are ASCII, read eight at a time as isASCII reads.
func asciiPrefix(s string) int {
	n := len(s)
	for len(s) >= 8 {
		first := uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
		second := uint32(s[4]) | uint32(s[5])<<8 | uint32(s[6])<<16 | uint32(s[7])<<24
		if (first|second)&0x80808080 != 0 {
			break
		}
		s = s[8:]
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return n - len(s) + i
		}
	}
	return n
}

// asciiSuffix is how many bytes s ends with that are ASCII, read eight at a time from the end.
func asciiSuffix(s string) int {
	n := len(s)
	for len(s) >= 8 {
		t := s[len(s)-8:]
		first := uint32(t[0]) | uint32(t[1])<<8 | uint32(t[2])<<16 | uint32(t[3])<<24
		second := uint32(t[4]) | uint32(t[5])<<8 | uint32(t[6])<<16 | uint32(t[7])<<24
		if (first|second)&0x80808080 != 0 {
			break
		}
		s = s[:len(s)-8]
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] >= utf8.RuneSelf {
			return n - i - 1
		}
	}
	return n
}
