package runtime

import (
	"github.com/kitwork/engine2/core/value"
)

// The operand of COMPARE: which comparison it runs. 0 to 5 are the modes the instruction set started
// with; == and != were the strict ones until 03/11/2026, so null == undefined was false. The loose two
// were added after the others, so a program written before keeps its meaning byte for byte.
const (
	CompareStrictEqual    = 0 // ===: value.Equal
	CompareStrictNotEqual = 1 // !==
	CompareGreater        = 2 // >
	CompareLess           = 3 // <
	CompareGreaterEqual   = 4 // >=
	CompareLessEqual      = 5 // <=
	CompareLooseEqual     = 6 // ==: value.LooseEqual (null == undefined, [1] == 1)
	CompareLooseNotEqual  = 7 // !=
)

// compareRead is COMPARE of two values one of which is a text, bytes, an object or an array (value.Plain): a
// Boolean, or the failure of a conversion it makes, or the run's energy failure once what it reads of a text is
// past the run's energy (comparePrimitives). === and !== convert nothing; any other comparison of an object or an
// array converts it (compareConverting), an array made text within the run's budget first (comparedText).
//
//go:noinline
func (vm *VM) compareRead(left, right value.Value, mode uint8) value.Value {
	if left.IsTime() && right.IsTime() && equality(mode) {
		// two times are no two Dates: JavaScript's === and == of two Date objects ask whether they are one object,
		// which a Kitwork time, a value, cannot say (18/11/2026, docs/language.md mục 8.2: it compared their instants,
		// so new Date(0) === new Date(0) was true where JavaScript gives false)
		return value.Fail(value.TypeCode, "hai thời gian không so bằng "+compareOperator(mode)+": JavaScript so hai Date theo chỗ, không theo thời điểm (new Date(0) === new Date(0) là false); so thời điểm thì a.getTime() === b.getTime()")
	}
	if mode > CompareStrictNotEqual && (left.ConvertsWithCode() || right.ConvertsWithCode()) {
		if left.IsArray() || right.IsArray() {
			// an array made text that passed the run's budget: its failure is the comparison's
			if failure := vm.comparedText(&left, &right, mode); failure.IsInvalid() {
				return failure
			}
		}
		return vm.compareConverting(left, right, mode)
	}
	return vm.comparePrimitives(left, right, mode)
}

// compareConverting is a comparison of the mode above of which a side is an object or an array (=== and
// !== compare it as it is): a Boolean, or the failure of a conversion it makes, as JavaScript throws out of a comparison
// whose valueOf throws (05/11/2026: compare read the failure as NaN or false, so price > 10 was false for
// an object whose valueOf a request could not run). An order converts each side to a primitive with the
// number hint once, the left first, as JavaScript does (Greater converted the right first, and <= and
// >= converted each side twice); == follows its own steps (value.LooseEquals). What it reads of the texts the
// conversions give costs the run's energy (comparePrimitives, value.LooseEqualsWithin).
func (vm *VM) compareConverting(a, b value.Value, mode uint8) value.Value {
	switch mode {
	case CompareStrictEqual, CompareStrictNotEqual:
		return value.Boolean(compare(a, b, mode)) // === converts nothing
	case CompareLooseEqual, CompareLooseNotEqual:
		equal := a.LooseEqualsWithin(b, vm)
		if equal.IsInvalid() || mode == CompareLooseEqual {
			return equal
		}
		return value.Boolean(!equal.Boolean())
	}
	if a.IsObject() || a.IsArray() {
		if a = a.ToPrimitiveNumber(); a.IsInvalid() {
			return a
		}
	}
	if b.IsObject() || b.IsArray() {
		if b = b.ToPrimitiveNumber(); b.IsInvalid() {
			return b
		}
	}
	return vm.comparePrimitives(a, b, mode)
}

// comparePrimitives is a comparison of the mode above of two values neither of which converts with code (an
// object's or an array's is converted first): a Boolean, what it reads of a text counted on the run's energy
// first (value/work.go), or the run's energy failure. === and == of two texts of one length compare them byte by
// byte (value.StrictWork); an order of two texts reads them to where they differ (value.CompareTexts); == of a
// text and a number, and an order of a text and anything but a text, make the text a number (value.NumberWork),
// twice for <= and >=, which compare it once in Less and once more to see it is not NaN.
func (vm *VM) comparePrimitives(a, b value.Value, mode uint8) value.Value {
	var units uint64
	switch mode {
	case CompareStrictEqual, CompareStrictNotEqual:
		units = value.StrictWork(a, b)
	case CompareLooseEqual, CompareLooseNotEqual:
		units = value.LooseWork(a, b)
	default:
		if a.IsString() && b.IsString() {
			order, read := value.CompareTexts(a.Text(), b.Text())
			if failure := vm.read(read); failure.IsInvalid() {
				return failure
			}
			switch mode {
			case CompareGreater:
				return value.Boolean(order > 0)
			case CompareLess:
				return value.Boolean(order < 0)
			case CompareGreaterEqual:
				return value.Boolean(order >= 0)
			}
			return value.Boolean(order <= 0)
		}
		if units = value.NumberWork(a) + value.NumberWork(b); mode == CompareGreaterEqual || mode == CompareLessEqual {
			units *= 2
		}
	}
	if failure := vm.read(units); failure.IsInvalid() {
		return failure
	}
	return value.Boolean(compare(a, b, mode))
}

// comparedText makes the two sides of a comparison with an array text first, within the run's budget
// (value.Budget), where the comparison makes it text: an order (<, >, <=, >=) always, as ToPrimitive with the
// number hint makes an array its text; == and != when the other side is a primitive ([1] == 1), never between
// two arrays or an array and an object, which they compare as they are. The result is JavaScript's either way.
//
// It writes each side made text in place, and gives the failure of the first that could not be made, or
// Value{}.
//
//go:noinline
func (vm *VM) comparedText(a, b *value.Value, mode uint8) value.Value {
	loose := mode == CompareLooseEqual || mode == CompareLooseNotEqual
	if a.IsArray() && (!loose || !b.ConvertsWithCode()) {
		if *a = vm.arrayText(*a); a.IsInvalid() {
			return *a
		}
	}
	if b.IsArray() && (!loose || !a.ConvertsWithCode()) {
		if *b = vm.arrayText(*b); b.IsInvalid() {
			return *b
		}
	}
	return value.Value{}
}

// compare is the result of one comparison of the mode above: a boolean. COMPARE calls it when neither
// side is an object or an array, so no conversion it makes runs code or fails.
func compare(a, b value.Value, mode uint8) bool {
	var res bool
	switch mode {
	case CompareStrictEqual:
		res = a.Equal(b)
	case CompareStrictNotEqual:
		res = !a.Equal(b)
	case CompareGreater:
		res = a.Greater(b)
	case CompareLess:
		res = a.Less(b)
	case CompareGreaterEqual:
		res = a.GreaterEqual(b)
	case CompareLessEqual:
		res = a.LessEqual(b)
	case CompareLooseEqual:
		res = a.LooseEqual(b)
	case CompareLooseNotEqual:
		res = !a.LooseEqual(b)
	}
	return res
}

// equality reports the modes ===, !==, == and != of COMPARE.
func equality(mode uint8) bool {
	return mode == CompareStrictEqual || mode == CompareStrictNotEqual || mode == CompareLooseEqual || mode == CompareLooseNotEqual
}

// compareOperator is the operator a mode of COMPARE is written with.
func compareOperator(mode uint8) string {
	switch mode {
	case CompareStrictEqual:
		return "==="
	case CompareStrictNotEqual:
		return "!=="
	case CompareLooseEqual:
		return "=="
	case CompareLooseNotEqual:
		return "!="
	}
	return "so sánh"
}
