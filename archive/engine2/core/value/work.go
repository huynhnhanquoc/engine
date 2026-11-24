package value

import (
	"math/bits"
	"unsafe"
)

// What the engine's own code does for a program costs that program's energy (15/11/2026). Energy counted instructions
// alone, and one instruction over a large value ran for seconds at the price of a call, 150 units: a sort of 100,000
// numbers took 364 ms, a === of two texts of 20 MB 0.5 ms, s.includes, s.slice or s.toUpperCase of 20 MB 2 to 4 ms a
// call, so a loop of them ran until the request's time (30 s) on 1% of its energy. A unit is what a plain instruction
// takes, about 4.4 ns on the machine measured (a request's 10,000,000 units are 44 ms of a loop of additions); what the
// engine does for a call is charged in units of what it takes, measured on that machine, through the run's Budget
// (Spend):
//
//   - what a run makes, a unit for each MadeBytesPerUnit bytes, as its meter counts it again (runtime, work.go,
//     spendMade): repeat and + copy 0.10 to 0.15 ns a byte, an array's item of 32 bytes 6 ns;
//   - what a method of a text or an array reads (workAfter): the text, a unit for TextBytesPerUnit bytes, the items a
//     unit each; a sort before it begins, as many comparisons as n·log n (workBefore);
//   - what a function of the engine or of a module reads of the texts it is handed (HandedText);
//   - what the engine's builders write (JSON.stringify, join, flat) as they write it, what JSON.parse reads before it
//     begins, what Object.keys, values, entries, assign and fromEntries go over.
//
// Counted from sizes, never from a clock: a run spends the same energy on any machine and under any load. A figure
// is one rate for a kind of work whose cases differ, so a call is charged what it takes within a few times either way
// (test/runtime, TestEnergyEndsWhatTheEngineDoes: 0.1 to 4.5 times a loop of additions for the same energy): a text
// of ASCII is read eight bytes a step, one of Vietnamese a character a step, both charged as one rate between them.
const (
	// MadeBytesPerUnit is the bytes a unit of energy makes: memmove and the allocation, 0.10 to 0.15 ns a byte
	// (repeat, +), an array's item 1.4 units (slice).
	MadeBytesPerUnit = 32
	// TextBytesPerUnit is the bytes of a text a method reads a unit: its length in units counted, a search, a copy
	// of its cases, 0.08 to 0.3 ns a byte of ASCII (isASCII, strings.Index), 1.2 to 1.9 ns of other text (walked a
	// character at a time); 8 bytes are 0.55 ns.
	TextBytesPerUnit = 8
	// HandedBytesPerUnit is the bytes of a text handed to a function of the engine or a module a unit: parseInt 4 ns a
	// byte, encodeURIComponent 3.3, Number 1.3; 2 bytes are 2.2 ns.
	HandedBytesPerUnit = 2
	// longNeedle is the length from which an array's search counts the needle it compares each item to: two texts
	// of one length are compared byte by byte (0.03 ns a byte, EqualBytesPerUnit), others by their length alone.
	longNeedle = 4 << 10
	// EqualBytesPerUnit is the bytes two texts compared for equality, or a key hashed, take a unit: 0.017 to 0.03 ns
	// a byte, strings' memequal and the map's hash.
	EqualBytesPerUnit = 64
)

// The units of energy a builder's work costs, measured with the figures above (4.4 ns a unit).
const (
	// sortCompareUnits is a comparison of sort by a comparator, beside the comparator's own instructions (142 ns a
	// comparison where its instructions are 16 units); sortTextUnits one of the default order between two texts,
	// beside reading them (45 ns); sortOtherUnits one between items of another kind, each made text to compare
	// (255 ns: two numbers written).
	sortCompareUnits = 16
	sortTextUnits    = 10
	sortOtherUnits   = 58
	// jsonParseUnits is a byte of JSON.parse's text (7.8 to 9.9 ns a byte of numbers and objects); a byte
	// JSON.stringify writes is a unit (4.7 to 5.3 ns), as a byte join writes is.
	jsonParseUnits = 2
	// joinItemUnits is an item join writes, beside its bytes (17.6 ns for "ab,"); joinNumberUnits one more for an
	// item made text first, a number (94.8 ns an item of 5.9 bytes).
	joinItemUnits   = 1
	joinNumberUnits = 16
	// flatItemUnits is an item flat writes (48.7 ns).
	flatItemUnits = 11
	// KeysUnits, ValuesUnits and EntriesUnits are a key Object.keys, values and entries give (73, 30 and 127 ns);
	// AssignUnits a key Object.assign or fromEntries writes (287 ns).
	KeysUnits    = 17
	ValuesUnits  = 7
	EntriesUnits = 29
	AssignUnits  = 65
)

// smallWork is the work a call's own price covers: an INVOKE or a CALL costs 150 units whatever it does, so what a
// call does below smallWork is not spent again, and a call over small values pays nothing for being counted (a push,
// "a,b".split(","), Object.keys of three keys).
const smallWork = 64

// Spend counts units of the run's energy on budget, Afford's twin: Value{} while the run has energy left, the run's
// energy failure once it has none, which ends it. With no budget (no run counts the call), or below smallWork, it
// spends nothing.
func Spend(budget Budget, units uint64) Value {
	if budget == nil || units < smallWork {
		return Value{}
	}
	return budget.Spend(units)
}

// spendRead counts what an instruction read on budget, every unit of it: an instruction's work is counted from
// LongText on (work.go), not from smallWork as a call's. With no budget it spends nothing.
func spendRead(budget Budget, units uint64) Value {
	if budget == nil || units == 0 {
		return Value{}
	}
	return budget.Spend(units)
}

// working is what a method of a text or an array reads, which its energy is counted by (methodWork). Each method has
// one for a text and one for an array, found with it (valueMethod), so a call looks nothing up for it.
type working uint8

const (
	// readsNothing: nothing but what it makes, counted as made (push, pop, an array's at and slice, a text's concat,
	// repeat, padStart)
	readsNothing working = iota
	// readsText: the text whole, its length in units counted first by every method of a text but these, and the texts
	// it is handed (a needle, a separator, a pattern)
	readsText
	// readsTextUpTo: the text up to the index (charAt, charCodeAt)
	readsTextUpTo
	// readsTrimmed: the spaces it trims (trim, trimStart, trimEnd)
	readsTrimmed
	// readsItems: every item, moved or written (reverse, fill, copyWithin, shift, unshift, splice)
	readsItems
	// findsItem: the items compared until the one found (includes, indexOf); findsLastItem from the end
	// (lastIndexOf)
	findsItem
	findsLastItem
	// sortsItems: n·log n comparisons, counted before they begin (sort, toSorted)
	sortsItems
)

// workOf is what the method name reads of a text and of an array; readsNothing for a kind it is not a method of.
func workOf(name string) (ofText, ofArray working) {
	if _, ok := kindMethods[text][name]; ok {
		switch name {
		case "toString", "valueOf", "concat", "repeat", "padStart", "padEnd":
		case "charAt", "charCodeAt":
			ofText = readsTextUpTo
		case "trim", "trimStart", "trimEnd", "trimLeft", "trimRight":
			ofText = readsTrimmed
		default:
			ofText = readsText
		}
	}
	if _, ok := kindMethods[array][name]; ok {
		switch name {
		case "includes", "indexOf":
			ofArray = findsItem
		case "lastIndexOf":
			ofArray = findsLastItem
		case "reverse", "fill", "copyWithin", "shift", "unshift", "splice":
			ofArray = readsItems
		case "sort", "toSorted":
			ofArray = sortsItems
		}
	}
	return ofText, ofArray
}

// workBefore is what the call of method on v costs before it is made: a sort's comparisons, which a sort of a large
// array makes for seconds; 0 for every other call, counted once it returns (workAfter).
func workBefore(v Value, method valueMethod, arguments []Value) uint64 {
	if v.kind != array || method.array != sortsItems {
		return 0
	}
	items := v.Items()
	n := uint64(len(items))
	if n < 2 {
		return 0
	}
	levels := uint64(bits.Len64(n)) // log2 n, rounded up: what each item is compared, about
	if argument(arguments, 0).kind == function {
		return n * levels * sortCompareUnits
	}
	units := uint64(0)
	for _, item := range items {
		if item.kind == text {
			units += sortTextUnits + uint64(len(item.data.(string)))/TextBytesPerUnit*2
		} else {
			units += sortOtherUnits
		}
	}
	return units * levels
}

// workAfter is what the call of method on v read, counted once it returned result.
func workAfter(v Value, method valueMethod, arguments []Value, result Value) uint64 {
	switch v.kind {
	case text:
		s := v.data.(string)
		switch method.text {
		case readsText:
			return (uint64(len(s)) + handedText(arguments)) / TextBytesPerUnit
		case readsTextUpTo:
			read := uint64(1)
			if at := argument(arguments, 0); at.kind == number {
				read = uint64(min(max(at.Integer(), 0), int64(len(s)))) + 1
			}
			return read / TextBytesPerUnit
		case readsTrimmed:
			if trimmed, ok := result.data.(string); ok && len(trimmed) <= len(s) {
				return uint64(len(s)-len(trimmed)) / TextBytesPerUnit
			}
		}
		return 0
	case array:
		n := uint64(len(v.Items()))
		switch method.array {
		case readsItems:
			return n
		case findsItem, findsLastItem:
			// the item found tells how far the search went; includes says only whether, so all of it
			compared := n
			if at := result.Number(); result.kind == number && at >= 0 && at < float64(n) {
				if method.array == findsLastItem {
					compared = n - uint64(at)
				} else {
					compared = uint64(at) + 1
				}
			}
			if needle := argument(arguments, 0); needle.kind == text && len(needle.data.(string)) >= longNeedle {
				compared += compared * uint64(len(needle.data.(string))) / EqualBytesPerUnit
			}
			return compared
		}
		return 0
	}
	// a number's, a time's, an object's: the texts it is handed (hasOwnProperty's key, format's pattern)
	return handedText(arguments) / TextBytesPerUnit
}

// worth reports a call of method on v that may read past what its call's price covers (smallWork), which countedCall
// counts; any other is called as it is, so a push, an array's at or a method of a short text pays nothing for being
// counted.
func (method valueMethod) worth(v Value, arguments []Value) bool {
	switch v.kind {
	case text:
		return method.text != readsNothing && uint64(len(v.data.(string)))+handedText(arguments) >= smallWork*TextBytesPerUnit
	case array:
		n := len(v.data.(*list).items)
		switch method.array {
		case readsNothing:
			return false
		case sortsItems:
			return n > 1
		case findsItem, findsLastItem:
			if needle, ok := argument(arguments, 0).data.(string); ok && len(needle) >= longNeedle {
				return true
			}
		}
		return n >= smallWork
	}
	return len(arguments) > 0 && handedText(arguments) >= smallWork*TextBytesPerUnit
}

// countedCall is method called on v by a run whose budget is budget, what it reads counted on the run's energy: a
// sort's comparisons before they begin (a sort of a large array takes seconds), what any other method read once it
// returns. A method read without a call (s.includes) is called so too, by the run that calls it (method).
func countedCall(v Value, method valueMethod, arguments []Value, budget Budget) Value {
	if v.kind == array && method.array == sortsItems {
		if spent := Spend(budget, workBefore(v, method, arguments)); spent.kind == invalid {
			return spent
		}
	}
	result := callMethodWithin(v, method, arguments, budget)
	if spent := Spend(budget, workAfter(v, method, arguments, result)); spent.kind == invalid {
		return spent
	}
	return result
}

// handedText is the bytes of the texts among arguments.
func handedText(arguments []Value) uint64 {
	total := uint64(0)
	for _, argument := range arguments {
		if s, ok := argument.data.(string); ok {
			total += uint64(len(s))
		}
	}
	return total
}

// HandedText is what a function of the engine or of a module reads of the texts it is handed, in units of energy: a
// unit for HandedBytesPerUnit bytes, counted by CallWithin and by a struct's method called with a budget.
func HandedText(arguments []Value) uint64 {
	return handedText(arguments) / HandedBytesPerUnit
}

// What an instruction reads (15/11/2026, the second part). An instruction costs a few units whatever it does
// (runtime, instruction.go), which a plain value is worth; one over a long text or a large object read it for
// milliseconds at that price: a === of two texts of 4 MB 0.15 ms, a < of two of Vietnamese 3 MB 10.5 ms, s - 0 of
// 4 MB of digits 16 ms, an o[k] of a key of 4 MB 0.09 ms, { ...o } of 20,000 keys 4.2 ms, a.length + b.length of two
// texts of Vietnamese read in turn 12 ms, a for…of over an object an out-of-place index had unordered 2.3 ms, each
// for 6 to 300 units. What the instruction reads is now counted as a builtin's is, at the rates below, measured on
// the machine of the figures above (a unit 4.4 ns), from 64 bytes of a text or a key on: below, it is the
// instruction's price.
const (
	// NumberBytesPerUnit is the bytes of a text made a number a unit: - * / % ++, an order or == against a number
	// (StringToNumber, its spaces trimmed a character at a time, its digits read by ParseFloat: 1.3 ns a byte of
	// spaces, 1.7 of other spaces, 4.0 of digits), as a function of the engine reads a text it is handed.
	NumberBytesPerUnit = 2
	// LongText is the length from which an instruction counts what it reads of a text or a key: a key hashed,
	// two texts compared, a text made a number. Below, a byte read is worth less than a unit (0.022 ns a byte
	// hashed), and what the instruction reads is its price.
	LongText = EqualBytesPerUnit
	// countBytesPerUnit is the bytes of a text other than ASCII counted in units, a unit (s.length, for…of: 2.0 ns a
	// byte of Vietnamese); a text all ASCII is checked at EqualBytesPerUnit (0.075 ns a byte).
	countBytesPerUnit = 2
	// stepUnits is a step of the text cursor (TextCursor), a character other than ASCII or a run of ASCII (6.3 ns
	// a character of Vietnamese); a long run of ASCII its bytes besides, at EqualBytesPerUnit.
	stepUnits = 1
	// compareStepUnits is a unit of each of two texts other than ASCII compared by unit (10.5 ns two characters of
	// Vietnamese); texts of ASCII are checked and compared at EqualBytesPerUnit (0.058 ns a byte).
	compareStepUnits = 2
	// spreadKeyUnits is a key { ...o } writes past the first linearKeys, which the instruction's price covers
	// (MERGE: 100 to 135 ns a key of objects of 8 to 1,000 keys, 270 of 20,000).
	spreadKeyUnits = 30
)

// Plain reports two values neither of which is a text, bytes, an object or an array: what an instruction does
// with them reads nothing past themselves (two numbers, a number and null), so it costs its price. A pair it
// does not report may still read nothing (a boolean and a time): it is the slow path, where what is read is
// counted. One test of the two kinds, for an instruction's fast path.
func Plain(a, b Value) bool { return a.kind|b.kind < text }

// PlainCompare is Plain for a comparison, but for two times, whose equality is the error (runtime compareRead,
// 18/11/2026): two numbers, booleans or nullish values, the common case, are told by the one test Plain makes.
func PlainCompare(a, b Value) bool {
	k := a.kind | b.kind
	return k < instant || k < text && (a.kind != instant || b.kind != instant)
}

// StrictWork is what === of a and b reads, as == of two of one kind: two texts, or bytes, of one length compared
// byte by byte (0.038 ns a byte), a unit for EqualBytesPerUnit from LongText on. Two of another length, or one
// text twice (its bytes are seen to be the same at once, as Go compares), read nothing past their lengths.
func StrictWork(a, b Value) uint64 {
	if a.kind != b.kind {
		return 0
	}
	var x, y string
	switch a.kind {
	case text:
		x, y = a.data.(string), b.data.(string)
	case bytes:
		p, q := a.data.([]byte), b.data.([]byte)
		if len(p) != len(q) || len(p) < LongText || &p[0] == &q[0] {
			return 0
		}
		return uint64(len(p)) / EqualBytesPerUnit
	default:
		return 0
	}
	if len(x) != len(y) || len(x) < LongText || unsafe.StringData(x) == unsafe.StringData(y) {
		return 0
	}
	return uint64(len(x)) / EqualBytesPerUnit
}

// NumberWork is what making v a number reads of it: a text whole, a unit for NumberBytesPerUnit from LongText
// on (StringToNumber); nothing for any other kind.
func NumberWork(v Value) uint64 {
	if v.kind != text {
		return 0
	}
	if s := v.data.(string); len(s) >= LongText {
		return uint64(len(s)) / NumberBytesPerUnit
	}
	return 0
}

// KeyWork is what looking key up reads of it: its bytes hashed, or compared to a key of its length, a unit for
// EqualBytesPerUnit from LongText on (0.022 ns a byte hashed). A map of eight keys or fewer is read by Go without
// hashing, a key of another length passed at once; it is counted alike, a long key being rare and its count a few
// units.
func KeyWork(key string) uint64 {
	if len(key) < LongText {
		return 0
	}
	return uint64(len(key)) / EqualBytesPerUnit
}

// IndexedSpreadWork is what { ...v } of an array, a string or bytes of n items writes past the price of its
// instruction: a key past the first linearKeys, as an object's spread (SpreadWork).
func IndexedSpreadWork(n int) uint64 {
	if n <= linearKeys {
		return 0
	}
	return uint64(n-linearKeys) * spreadKeyUnits
}

// SpreadWork is what { ...v } writes past the price of its instruction (MERGE): a key past the first linearKeys,
// as many as an object holds without its index map, spreadKeyUnits; a long key's bytes hashed (KeyWork). 0 for
// every kind but an object.
func SpreadWork(v Value) uint64 {
	o := v.properties()
	if o == nil {
		return 0
	}
	units := uint64(0)
	if live := len(o.keys) - int(o.dead); live > linearKeys {
		units += uint64(live-linearKeys) * spreadKeyUnits
	}
	for _, key := range o.keys {
		units += KeyWork(key)
	}
	return units
}

// CompareTexts orders a and b as JavaScript's < does (compareText): -1, 0 or 1, with what it read in units of
// energy. The ASCII each was checked for and the bytes compared after (at most the shorter), a unit for
// EqualBytesPerUnit; two texts other than ASCII compareStepUnits a unit of each, read to the first that differs.
// Two texts shorter than LongText read nothing past the instruction's price.
func CompareTexts(a, b string) (int, uint64) {
	order, units := compareTextRead(a, b)
	if len(a) < LongText && len(b) < LongText {
		return order, 0
	}
	return order, units
}

// LooseWork is what == of a and b reads: two of one kind as === (StrictWork); a text against anything but null
// or undefined made a number (NumberWork), as == makes it one to compare with a number or a boolean.
func LooseWork(a, b Value) uint64 {
	if a.kind == b.kind {
		return StrictWork(a, b)
	}
	if a.IsNullish() || b.IsNullish() {
		return 0
	}
	return NumberWork(a) + NumberWork(b)
}
