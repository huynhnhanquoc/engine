package value

import "unsafe"

// Joining texts in place (19/11/2026). a + b made a new text of both each time, so s = s + piece, written in a loop as
// JavaScript is, copied s whole at every turn: 30,000 turns of s += "x", a text of 30 KB, copied 450 MB and ran a
// request out of its energy, and 100,000 turns took 0.73 s on a VM of no ceiling. JavaScript's engines keep such a
// text as a rope; a Joiner keeps the run's last few texts made by a join in a buffer with room after them, as Go's
// strings.Builder does: a join whose left side is the whole of what a buffer holds writes the right side after it, in
// the room, and the text it gives is the buffer up to there. No text that exists sees a byte change: every text of a
// buffer is a prefix of it, and a join writes only past the longest, which is the left side itself. A buffer is given
// room when the text it holds is joined again, as much again as it holds, within what the run may still make: a text
// joined once costs what it did, a text built piece by piece costs what it holds, twice at most.

// joinerSlots is how many texts a run builds at once in place: s and t built in the same loop each keep theirs.
const joinerSlots = 4

// joinFrom is the length from which a join is kept in a buffer: a shorter text is joined as before, so small joins
// ("<li>" + name) neither take a slot nor change what they cost.
const joinFrom = 64

// Joiner is one run's buffers of the texts its + and template literals build (runtime ADD, TEMPLATE). The zero value
// joins with no bound but the longest text; a nil Joiner joins as Go's + does.
type Joiner struct {
	// Spare is how many bytes the run may still make, asked before a buffer is given room; nil for no bound.
	Spare func() uint64
	slots [joinerSlots][]byte
	next  int
}

// Forget lets every buffer go: at the start of a run, and as the run is counted by what it holds (runtime memory.go),
// since the room of a buffer is no text the count can see.
func (j *Joiner) Forget() {
	clear(j.slots[:])
	j.next = 0
}

// Join is a + b of two texts, and the bytes it allocated: 0 for a side given back whole or b written in a buffer's
// room. A text past the longest is the RANGE failure.
func (j *Joiner) Join(a, b string) (Value, uint64) {
	switch {
	case len(b) == 0:
		return String(a), 0
	case len(a) == 0:
		return String(b), 0
	}
	total := len(a) + len(b)
	if total > maxStringLength {
		return tooLong(), 0
	}
	if j == nil || total < joinFrom || surrogatesMeet(a, b) {
		joined := concatText(a, b)
		return String(joined), uint64(len(joined))
	}
	data := unsafe.StringData(a)
	for i, buffer := range j.slots {
		if len(buffer) != len(a) || unsafe.SliceData(buffer) != data {
			continue
		}
		if cap(buffer)-len(buffer) >= len(b) {
			buffer = append(buffer, b...)
			j.slots[i] = buffer
			return String(unsafe.String(unsafe.SliceData(buffer), len(buffer))), 0
		}
		// a is built piece by piece: the next buffer has room for as much again
		return j.write(i, a, b, total)
	}
	i := j.next
	j.next = (j.next + 1) % joinerSlots
	return j.write(i, a, b, 0) // joined once so far: no room until it is joined again
}

// write puts a and b in a new buffer in slot i, with room for at most more bytes after them, within what the run may
// still make and the longest text, and gives the text and the bytes the buffer took.
func (j *Joiner) write(i int, a, b string, more int) (Value, uint64) {
	total := len(a) + len(b)
	if more > 0 && j.Spare != nil {
		if spare := j.Spare(); spare <= uint64(total) {
			more = 0
		} else if left := spare - uint64(total); left < uint64(more) {
			more = int(left)
		}
	}
	more = min(more, maxStringLength-total)
	buffer := make([]byte, total, total+more)
	copy(buffer, a)
	copy(buffer[len(a):], b)
	j.slots[i] = buffer
	return String(unsafe.String(unsafe.SliceData(buffer), total)), uint64(cap(buffer))
}

// surrogatesMeet says a ends with the high half of a character and b begins with its low half: concatText makes them
// the character, which is no byte written after a.
func surrogatesMeet(a, b string) bool {
	return len(a) >= 3 && len(b) >= 3 && a[len(a)-3] == 0xED && b[0] == 0xED
}
