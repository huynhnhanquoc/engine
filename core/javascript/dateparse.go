package javascript

import (
	"math"
	"time"
	"unicode/utf8"
)

// Chép nguyên từ engine2 (core/builtins/dateparse.go), nơi nó đã đối chiếu với node; chú thích giữ tiếng Anh để đọc
// song song với bộ đọc ngày của V8 mà nó chép.

// parseDate is Date.parse(text): the milliseconds since 1970 the text writes, NaN when it writes none.
// It is V8's date parser (src/date/dateparser*), so that a text node reads, Kitwork reads the same:
//
//   - first the ISO form of ECMAScript (21.4.1.32), [+-YY]YYYY[-MM[-DD]][THH:mm[:ss[.sss]][Z|+hh:mm]],
//     with V8's extensions: any number of digits after the point (the first three count), the offset
//     written +hhmm, the letters T and Z in either case, 24:00 for the end of a day. A date alone is
//     UTC, a date with a time and no offset is the host's local time.
//   - then, for what the ISO form does not read to its end, the legacy reading V8 shares with other
//     browsers: numbers, month names, AM and PM, zone names (UTC, GMT, Z, EST…), signed offsets after a
//     time, words in parentheses skipped ("Tue Jan 02 2024 10:04:05 GMT+0700 (Indochina Time)",
//     "2024/01/02", "Jan 2, 2024 10:30 PM"). Without a zone it is local time.
//
// Local time is location's: the zone of where the program runs (locationOf), the host's when location is nil.
func parseDate(text string, location *time.Location) float64 {
	var (
		day     dayComposer
		clock   = timeComposer{hourOffset: none}
		zone    = zoneComposer{sign: none, hour: none, minute: none}
		scanner = newDateScanner(text)
		next    = parseISODate(scanner, &day, &clock, &zone)
		hasRead = day.index > 0
	)
	if next.kind == tokenInvalid {
		return math.NaN()
	}
	for token := next; token.kind != tokenEnd; token = scanner.next() {
		switch {
		case token.kind == tokenNumber:
			hasRead = true
			n := token.number
			switch {
			case scanner.skipSymbol(':'):
				if scanner.skipSymbol(':') {
					// n followed by "::": hours, and the minutes 0
					if clock.index > 0 {
						return math.NaN()
					}
					clock.add(n)
					clock.add(0)
				} else {
					if !clock.add(n) {
						return math.NaN()
					}
					if scanner.peek().isSymbol('.') {
						scanner.next()
					}
				}
			// the point is consumed whether or not the time expects n, as V8's && does
			case scanner.skipSymbol('.') && clock.expecting(n):
				clock.add(n)
				if scanner.peek().kind != tokenNumber {
					return math.NaN()
				}
				clock.addFinal(readMilliseconds(scanner.next()))
			case zone.expecting(n):
				zone.minute = n
			case clock.expecting(n):
				clock.addFinal(n)
				// the end of a time is followed by the end, a space, Z or a sign, nothing else
				if peek := scanner.peek(); peek.kind != tokenEnd && peek.kind != tokenSpace && !peek.isZ() && !peek.isSign() {
					return math.NaN()
				}
			default:
				if !day.add(n) {
					return math.NaN()
				}
				scanner.skipSymbol('-')
			}
		case token.kind == tokenKeyword:
			switch {
			case token.keyword == keywordAMPM && clock.index > 0:
				clock.hourOffset = token.number // 0 for AM, 12 for PM
			case token.keyword == keywordMonth:
				day.namedMonth = token.number
				scanner.skipSymbol('-')
			case token.keyword == keywordZone && hasRead:
				zone.set(token.number)
			default:
				// a word is garbage: none may follow a number, and none may touch the first number
				if hasRead || scanner.peek().kind == tokenNumber {
					return math.NaN()
				}
			}
		case token.isSign() && (zone.isUTC() || clock.index > 0):
			// an offset, after a time or after UTC/GMT: +7, +07, +0700, +07:00
			zone.sign = token.sign()
			n, length := 0, 0
			if scanner.peek().kind == tokenNumber {
				number := scanner.next()
				n, length = number.number, number.length
			}
			hasRead = true
			switch {
			case scanner.peek().isSymbol(':'):
				zone.hour, zone.minute = n, none
			case length == 1 || length == 2:
				zone.hour, zone.minute = n, 0
			case length == 3 || length == 4:
				zone.hour, zone.minute = n/100, n%100
			default:
				return math.NaN()
			}
		case (token.isSign() || token.isSymbol(')')) && hasRead:
			return math.NaN()
		}
		// anything else, a space, a slash or a comma, separates and is skipped
	}
	var parts [8]float64
	if !day.write(&parts) || !clock.write(&parts) || !zone.write(&parts) {
		return math.NaN()
	}
	date := makeDate(makeDay(parts[0], parts[1], parts[2]), makeTime(parts[3], parts[4], parts[5], parts[6]))
	if offset := parts[7]; offset != offset {
		// local time: V8 checks the range before the zone is taken away, not after
		if !(math.Abs(date) <= maxLocalMilliseconds) {
			return math.NaN()
		}
		return fromLocal(date, location)
	}
	date -= float64(parts[7] * 1000)
	if !(math.Abs(date) <= maxMilliseconds) {
		return math.NaN()
	}
	return date
}

// parseISODate reads the ISO form (V8's ParseES5DateTime). It returns the end token when it read the
// whole text, the invalid token when the text breaks the ISO form past the point of no return (a time
// begun), else the first token it did not read, for the legacy reading to go on from.
func parseISODate(scanner *dateScanner, day *dayComposer, clock *timeComposer, zone *zoneComposer) dateToken {
	switch {
	case scanner.peek().isSign():
		// ±YYYYYY; -000000 is not a year (year zero is +000000)
		sign := scanner.next()
		if !scanner.peek().fixedLength(6) {
			return sign
		}
		year := scanner.next().number
		if sign.sign() < 0 && year == 0 {
			return sign
		}
		day.add(sign.sign() * year)
	case scanner.peek().fixedLength(4):
		day.add(scanner.next().number)
	default:
		return scanner.next()
	}
	if scanner.skipSymbol('-') {
		if !scanner.peek().fixedLength(2) || !isMonth(scanner.peek().number) {
			return scanner.next()
		}
		day.add(scanner.next().number)
		if scanner.skipSymbol('-') {
			if !scanner.peek().fixedLength(2) || !isDay(scanner.peek().number) {
				return scanner.next()
			}
			day.add(scanner.next().number)
		}
	}
	if !scanner.peek().isKeyword(keywordTime) {
		if scanner.peek().kind != tokenEnd {
			return scanner.next()
		}
	} else {
		scanner.next()
		if !scanner.peek().fixedLength(2) || !between(scanner.peek().number, 0, 24) {
			return dateToken{kind: tokenInvalid}
		}
		// 24:00, 24:00:00 and 24:00:00.000 are the end of the day; no other time begins with 24
		hourIs24 := scanner.peek().number == 24
		clock.add(scanner.next().number)
		if !scanner.skipSymbol(':') {
			return dateToken{kind: tokenInvalid}
		}
		if !scanner.peek().fixedLength(2) || !isMinute(scanner.peek().number) || hourIs24 && scanner.peek().number > 0 {
			return dateToken{kind: tokenInvalid}
		}
		clock.add(scanner.next().number)
		if scanner.skipSymbol(':') {
			if !scanner.peek().fixedLength(2) || !isSecond(scanner.peek().number) || hourIs24 && scanner.peek().number > 0 {
				return dateToken{kind: tokenInvalid}
			}
			clock.add(scanner.next().number)
			if scanner.skipSymbol('.') {
				if scanner.peek().kind != tokenNumber || hourIs24 && scanner.peek().number > 0 {
					return dateToken{kind: tokenInvalid}
				}
				clock.add(readMilliseconds(scanner.next()))
			}
		}
		switch {
		case scanner.peek().isZ():
			scanner.next()
			zone.set(0)
		case scanner.peek().isSign():
			zone.sign = scanner.next().sign()
			if scanner.peek().fixedLength(4) {
				// the +hhmm extension
				hourMinute := scanner.next().number
				hour, minute := hourMinute/100, hourMinute%100
				if !isHour(hour) || !isMinute(minute) {
					return dateToken{kind: tokenInvalid}
				}
				zone.hour, zone.minute = hour, minute
			} else {
				if !scanner.peek().fixedLength(2) || !isHour(scanner.peek().number) {
					return dateToken{kind: tokenInvalid}
				}
				zone.hour = scanner.next().number
				if !scanner.skipSymbol(':') {
					return dateToken{kind: tokenInvalid}
				}
				if !scanner.peek().fixedLength(2) || !isMinute(scanner.peek().number) {
					return dateToken{kind: tokenInvalid}
				}
				zone.minute = scanner.next().number
			}
		}
		if scanner.peek().kind != tokenEnd {
			return dateToken{kind: tokenInvalid}
		}
	}
	// a date alone is UTC; a date and a time without an offset are local time
	if zone.empty() && clock.index == 0 {
		zone.set(0)
	}
	day.iso = true
	return dateToken{kind: tokenEnd}
}

// readMilliseconds is the first three digits of a fraction of a second, from a number token: "1" is
// 100, "12" 120, "123456" 123 (the token keeps nine digits at most, and its length tells the zeros it
// began with).
func readMilliseconds(token dateToken) int {
	n, length := token.number, token.length
	switch {
	case length == 1:
		return n * 100
	case length == 2:
		return n * 10
	case length > 3:
		length = min(length, maxSignificantDigits)
		for ; length > 3; length-- {
			n /= 10
		}
	}
	return n
}

// ---- what the reading composes ----

// none is a part not read yet (V8's kNone).
const none = math.MaxInt32

func between(x, low, high int) bool { return x >= low && x <= high }
func isMonth(x int) bool            { return between(x, 1, 12) }
func isDay(x int) bool              { return between(x, 1, 31) }
func isHour(x int) bool             { return between(x, 0, 23) }
func isMinute(x int) bool           { return between(x, 0, 59) }
func isSecond(x int) bool           { return between(x, 0, 59) }
func isMillisecond(x int) bool      { return between(x, 0, 999) }

// dayComposer gathers up to three numbers of the date and a month name, and decides at the end which
// is the year, the month and the day.
type dayComposer struct {
	parts      [3]int
	index      int
	namedMonth int // 0 for none, else 1 to 12
	iso        bool
}

func (d *dayComposer) add(n int) bool {
	if d.index == len(d.parts) {
		return false
	}
	d.parts[d.index] = n
	d.index++
	return true
}

func (d *dayComposer) write(out *[8]float64) bool {
	if d.index < 1 {
		return false
	}
	for ; d.index < len(d.parts); d.index++ {
		d.parts[d.index] = 1 // the month and the day default to 1
	}
	year, month, day := 0, none, none // no year is 0, which is 2000
	switch {
	case d.namedMonth == 0 && (d.iso || !isDay(d.parts[0])):
		year, month, day = d.parts[0], d.parts[1], d.parts[2]
	case d.namedMonth == 0:
		month, day, year = d.parts[0], d.parts[1], d.parts[2]
	case !isDay(d.parts[0]):
		month, year, day = d.namedMonth, d.parts[0], d.parts[1]
	default:
		month, day, year = d.namedMonth, d.parts[0], d.parts[1]
	}
	if !d.iso {
		switch {
		case between(year, 0, 49):
			year += 2000
		case between(year, 50, 99):
			year += 1900
		}
	}
	if !isMonth(month) || !isDay(day) {
		return false
	}
	out[0], out[1], out[2] = float64(year), float64(month-1), float64(day)
	return true
}

// timeComposer gathers hours, minutes, seconds and milliseconds, and the hour offset AM (0) or PM
// (12) gave, none without one.
type timeComposer struct {
	parts      [4]int
	index      int
	hourOffset int
}

func (t *timeComposer) add(n int) bool {
	if t.index == len(t.parts) {
		return false
	}
	t.parts[t.index] = n
	t.index++
	return true
}

func (t *timeComposer) addFinal(n int) bool {
	if !t.add(n) {
		return false
	}
	for ; t.index < len(t.parts); t.index++ {
		t.parts[t.index] = 0
	}
	return true
}

// expecting reports that n fits the part the time waits for after its hours.
func (t *timeComposer) expecting(n int) bool {
	return t.index == 1 && isMinute(n) || t.index == 2 && isSecond(n) || t.index == 3 && isMillisecond(n)
}

func (t *timeComposer) write(out *[8]float64) bool {
	for ; t.index < len(t.parts); t.index++ {
		t.parts[t.index] = 0
	}
	hour, minute, second, millisecond := t.parts[0], t.parts[1], t.parts[2], t.parts[3]
	if t.hourOffset != none {
		if !between(hour, 0, 12) {
			return false
		}
		hour = hour%12 + t.hourOffset
	}
	if !isHour(hour) || !isMinute(minute) || !isSecond(second) || !isMillisecond(millisecond) {
		// the 24th hour only as the end of the day
		if hour != 24 || minute != 0 || second != 0 || millisecond != 0 {
			return false
		}
	}
	out[3], out[4], out[5], out[6] = float64(hour), float64(minute), float64(second), float64(millisecond)
	return true
}

// zoneComposer gathers the offset from UTC: a sign, hours and minutes, each none until read; no sign
// at the end means local time.
type zoneComposer struct {
	sign, hour, minute int
}

func (z *zoneComposer) empty() bool { return z.hour == none }

// set is a zone name's offset in whole hours (0 for Z, UTC and GMT, -5 for EST).
func (z *zoneComposer) set(hours int) {
	z.sign = 1
	if hours < 0 {
		z.sign = -1
	}
	z.hour, z.minute = hours*z.sign, 0
}

func (z *zoneComposer) isUTC() bool { return z.hour == 0 && z.minute == 0 }

// expecting reports that n is the minutes of an offset whose hours came before a colon.
func (z *zoneComposer) expecting(n int) bool {
	return z.hour != none && z.minute == none && isMinute(n)
}

func (z *zoneComposer) write(out *[8]float64) bool {
	if z.sign == none {
		out[7] = math.NaN()
		return true
	}
	hour, minute := z.hour, z.minute
	if hour == none {
		hour = 0
	}
	if minute == none {
		minute = 0
	}
	seconds := int64(hour)*3600 + int64(minute)*60
	if seconds > 1<<30-1 {
		return false // V8 refuses an offset past a small integer of seconds
	}
	out[7] = float64(seconds * int64(z.sign))
	return true
}

// ---- the tokens ----

type tokenKind uint8

const (
	tokenInvalid tokenKind = iota
	tokenEnd
	tokenNumber
	tokenSymbol
	tokenKeyword
	tokenSpace
	tokenUnknown
)

type keywordKind uint8

const (
	keywordNone keywordKind = iota
	keywordMonth
	keywordAMPM
	keywordZone
	keywordTime
)

// dateToken is one token: a number (its value and how many digits wrote it), a symbol, a word (its
// kind, its value in number, its length), a space, anything else, or the end.
type dateToken struct {
	kind    tokenKind
	number  int
	length  int
	symbol  byte
	keyword keywordKind
}

func (t dateToken) isSymbol(c byte) bool         { return t.kind == tokenSymbol && t.symbol == c }
func (t dateToken) isSign() bool                 { return t.isSymbol('+') || t.isSymbol('-') }
func (t dateToken) fixedLength(n int) bool       { return t.kind == tokenNumber && t.length == n }
func (t dateToken) isKeyword(k keywordKind) bool { return t.kind == tokenKeyword && t.keyword == k }

// isZ is the one-letter zone name Z, the only zone the ISO form takes.
func (t dateToken) isZ() bool { return t.isKeyword(keywordZone) && t.length == 1 && t.number == 0 }

func (t dateToken) sign() int {
	if t.symbol == '-' {
		return -1
	}
	return 1
}

// keywords are the words V8 knows, by their first three letters in lower case: a month name may be
// longer ("january"), any other word must be the keyword itself.
var keywords = []struct {
	prefix string
	kind   keywordKind
	value  int
}{
	{"jan", keywordMonth, 1}, {"feb", keywordMonth, 2}, {"mar", keywordMonth, 3}, {"apr", keywordMonth, 4},
	{"may", keywordMonth, 5}, {"jun", keywordMonth, 6}, {"jul", keywordMonth, 7}, {"aug", keywordMonth, 8},
	{"sep", keywordMonth, 9}, {"oct", keywordMonth, 10}, {"nov", keywordMonth, 11}, {"dec", keywordMonth, 12},
	{"am", keywordAMPM, 0}, {"pm", keywordAMPM, 12},
	{"ut", keywordZone, 0}, {"utc", keywordZone, 0}, {"z", keywordZone, 0}, {"gmt", keywordZone, 0},
	{"cdt", keywordZone, -5}, {"cst", keywordZone, -6}, {"edt", keywordZone, -4}, {"est", keywordZone, -5},
	{"mdt", keywordZone, -6}, {"mst", keywordZone, -7}, {"pdt", keywordZone, -7}, {"pst", keywordZone, -8},
	{"t", keywordTime, 0},
}

// maxSignificantDigits is how many digits of a number V8 keeps; the rest still count in its length.
const maxSignificantDigits = 9

// dateScanner reads tokens with one of lookahead.
type dateScanner struct {
	text      string
	at        int
	lookahead dateToken
}

func newDateScanner(text string) *dateScanner {
	s := &dateScanner{text: text}
	s.lookahead = s.scan()
	return s
}

func (s *dateScanner) peek() dateToken { return s.lookahead }

func (s *dateScanner) next() dateToken {
	token := s.lookahead
	s.lookahead = s.scan()
	return token
}

func (s *dateScanner) skipSymbol(c byte) bool {
	if s.lookahead.isSymbol(c) {
		s.next()
		return true
	}
	return false
}

// character is the character at the reading position and its size in bytes; 0 at the end.
func (s *dateScanner) character() (rune, int) {
	if s.at >= len(s.text) {
		return 0, 0
	}
	if c := s.text[s.at]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	if _, lone := loneSurrogateAt(s.text, s.at); lone {
		return 0xD800, 3 // a surrogate: a letter to the scanner, as any unit past 'A' is
	}
	return utf8.DecodeRuneInString(s.text[s.at:])
}

func (s *dateScanner) scan() dateToken {
	c, size := s.character()
	switch {
	case size == 0:
		return dateToken{kind: tokenEnd}
	case c >= '0' && c <= '9':
		n, length := 0, 0
		for ; s.at < len(s.text) && s.text[s.at] >= '0' && s.text[s.at] <= '9'; s.at++ {
			if length < maxSignificantDigits {
				n = n*10 + int(s.text[s.at]-'0')
			}
			length++
		}
		return dateToken{kind: tokenNumber, number: n, length: length}
	case c == ':' || c == '-' || c == '+' || c == '.' || c == ')':
		s.at++
		return dateToken{kind: tokenSymbol, symbol: byte(c)}
	case c >= 'A' && !dateSpace(c):
		var prefix [3]byte
		length := 0
		for {
			c, size := s.character()
			if size == 0 || c < 'A' || dateSpace(c) {
				break
			}
			if length < len(prefix) {
				prefix[length] = byte(c) | 0x20 // V8's lowering: right for ASCII letters, and no other character matches a keyword
				if c >= utf8.RuneSelf {
					prefix[length] = 0xFF
				}
			}
			length++
			s.at += size
		}
		word := string(prefix[:min(length, len(prefix))])
		for _, k := range keywords {
			if word == k.prefix && (length <= len(prefix) || k.kind == keywordMonth) {
				return dateToken{kind: tokenKeyword, keyword: k.kind, number: k.value, length: length}
			}
		}
		return dateToken{kind: tokenKeyword, keyword: keywordNone, length: length}
	case dateSpace(c):
		s.at += size
		return dateToken{kind: tokenSpace}
	case c == '(':
		// a parenthesised comment, nested ones included, is skipped as one token
		for balance := 0; ; {
			c, size := s.character()
			if size == 0 {
				break
			}
			switch c {
			case ')':
				balance--
			case '(':
				balance++
			}
			s.at += size
			if balance <= 0 {
				break
			}
		}
		return dateToken{kind: tokenUnknown}
	}
	s.at += size
	return dateToken{kind: tokenUnknown}
}

// dateSpace is ECMAScript's white space and line terminators.
func dateSpace(c rune) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}
