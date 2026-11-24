package javascript

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// What a number is as text and what text is as a number, as JavaScript says. Carried over from engine2 (number.go,
// itself from the first engine, 15–16/10/2026), where each rule was checked against node.

// appendNumber writes n as JavaScript's Number::toString does (String(n), `${n}`, "" + n, a key of an
// object): the fewest digits that read back as n, written plain from 1e-7 up to 1e21 ("1234567.5",
// "0.000001", "100000000000000000000") and with an exponent outside ("1e+21", "1e-7", "1.5e-10"); -0 is
// "0"; NaN, Infinity and -Infinity as words. It allocates nothing beyond b.
func appendNumber(b []byte, n float64) []byte {
	if text, nonFinite := nonFiniteText(n); nonFinite {
		return append(b, text...)
	}
	if n == 0 {
		return append(b, '0')
	}
	if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
		return strconv.AppendInt(b, int64(n), 10)
	}
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	// the shortest digits that read back as n, as d.ddde±x; JavaScript's k digits and point n - k
	var scratch [32]byte
	written := strconv.AppendFloat(scratch[:0], n, 'e', -1, 64)
	mark := 0
	for written[mark] != 'e' {
		mark++
	}
	exponent, negativeExponent := 0, written[mark+1] == '-'
	for _, digit := range written[mark+2:] {
		exponent = exponent*10 + int(digit-'0')
	}
	if negativeExponent {
		exponent = -exponent
	}
	var digitScratch [24]byte
	digits := append(digitScratch[:0], written[0])
	if mark > 2 {
		digits = append(digits, written[2:mark]...)
	}
	k, point := len(digits), exponent+1
	switch {
	case k <= point && point <= 21:
		b = append(b, digits...)
		for i := k; i < point; i++ {
			b = append(b, '0')
		}
	case 0 < point && point <= 21:
		b = append(append(append(b, digits[:point]...), '.'), digits[point:]...)
	case -6 < point && point <= 0:
		b = append(b, "0."...)
		for i := point; i < 0; i++ {
			b = append(b, '0')
		}
		b = append(b, digits...)
	default:
		b = append(b, digits[0])
		if k > 1 {
			b = append(append(b, '.'), digits[1:]...)
		}
		b = append(b, 'e')
		if point > 0 {
			b = append(b, '+')
		}
		b = strconv.AppendInt(b, int64(point-1), 10)
	}
	return b
}

// numberText is appendNumber as a string: the text JavaScript gives a number, one allocation.
func numberText(n float64) string {
	if n >= 0 && n < float64(len(smallNumbers)) && n == math.Trunc(n) { // -0 cũng là "0", như String(-0)
		return smallNumbers[int(n)]
	}
	var scratch [32]byte
	return string(appendNumber(scratch[:0], n))
}

// smallNumbers là chữ của các số nguyên 0 tới 1023, dựng một lần: chỉ số của một vòng lặp, một độ dài, một mã ký tự
// nối vào chuỗi ("item " + i) không cấp phát chữ của số.
var smallNumbers = func() (table [1024]string) {
	for i := range table {
		table[i] = strconv.Itoa(i)
	}
	return
}()

// fixedText is JavaScript's n.toFixed(digits) for digits 0 to 100: the number nearest n with that many
// digits after the point, and of two as near the one further from zero, as JavaScript rounds the exact
// value n holds ((1.25).toFixed(1) is "1.3", (2.5).toFixed(0) "3", (1.005).toFixed(2) "1.00", for 1.005
// holds a little less); -0 is "0.00"; from 1e21 up, n as String(n) writes it; NaN and the infinities
// as words.
func fixedText(n float64, digits int) string {
	if text, nonFinite := nonFiniteText(n); nonFinite {
		return text
	}
	if math.Abs(n) >= 1e21 {
		return numberText(n)
	}
	sign := ""
	if n < 0 {
		sign = "-"
	}
	scaled := new(big.Rat).SetFloat64(math.Abs(n))
	scaled.Mul(scaled, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)))
	scaled.Add(scaled, big.NewRat(1, 2))
	text := new(big.Int).Quo(scaled.Num(), scaled.Denom()).String()
	if digits == 0 {
		return sign + text
	}
	for len(text) <= digits {
		text = "0" + text
	}
	return sign + text[:len(text)-digits] + "." + text[len(text)-digits:]
}

// exponentialText is JavaScript's n.toExponential(fraction) for a finite n: one digit, a point and
// fraction more when fraction > 0, then e and the exponent with its sign ("1.25e+1", "4.941e-324");
// shortest (no argument given) writes the fewest digits that read back as n. -0 is "0e+0".
func exponentialText(n float64, fraction int, shortest bool) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	var digits string
	exponent := 0
	switch {
	case n == 0 && shortest:
		digits = "0"
	case n == 0:
		digits = strings.Repeat("0", fraction+1)
	case shortest:
		digits, exponent = shortestDigits(n)
	default:
		exact, at := exactDigits(n)
		digits, exponent = roundDigits(exact, at, fraction+1)
	}
	return sign + scientificText(digits, exponent)
}

// precisionText is JavaScript's n.toPrecision(precision) for a finite n: precision significant digits,
// with an exponent when it is below -6 or not below the precision ("1.2e+5", "1e-7"), plain otherwise
// ("12.5", "0.00012", "100"). -0 writes as 0.
func precisionText(n float64, precision int) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits, exponent := strings.Repeat("0", precision), 0
	if n != 0 {
		exact, at := exactDigits(n)
		digits, exponent = roundDigits(exact, at, precision)
	}
	switch {
	case exponent < -6 || exponent >= precision:
		return sign + scientificText(digits, exponent)
	case exponent == precision-1:
		return sign + digits
	case exponent >= 0:
		return sign + digits[:exponent+1] + "." + digits[exponent+1:]
	}
	return sign + "0." + strings.Repeat("0", -exponent-1) + digits
}

// scientificText writes digits d₁d₂… and an exponent as d₁.d₂…e±x, the way toExponential and
// toPrecision do: no point after a single digit, the exponent's sign always written.
func scientificText(digits string, exponent int) string {
	text := digits[:1]
	if len(digits) > 1 {
		text += "." + digits[1:]
	}
	if exponent < 0 {
		return text + "e-" + strconv.Itoa(-exponent)
	}
	return text + "e+" + strconv.Itoa(exponent)
}

// exactDigits is the decimal digits of n > 0 exactly as the float64 holds it, without trailing zeros,
// and the exponent of the first: n = d₁.d₂… × 10^exponent. Every float64 has a finite decimal
// expansion of at most 767 significant digits, and strconv writes it exactly when asked for more.
func exactDigits(n float64) (string, int) {
	written := strconv.FormatFloat(n, 'e', 780, 64)
	mark := strings.IndexByte(written, 'e')
	exponent, _ := strconv.Atoi(written[mark+1:])
	digits := strings.TrimRight(written[:1]+written[2:mark], "0")
	return digits, exponent
}

// shortestDigits is as exactDigits, with the fewest digits that read back as n (what String(n) writes).
func shortestDigits(n float64) (string, int) {
	written := strconv.FormatFloat(n, 'e', -1, 64)
	mark := strings.IndexByte(written, 'e')
	exponent, _ := strconv.Atoi(written[mark+1:])
	digits := written[:1]
	if mark > 2 {
		digits += written[2:mark]
	}
	return digits, exponent
}

// roundDigits rounds exact digits to count of them: up when the first digit dropped is 5 or more,
// which, the digits being exact, also takes a tie to the larger one, as JavaScript says ((2.5) to one
// digit is 3, (1.005) to three is 1.00 for it holds 1.00499…). A carry past the first digit raises
// the exponent (9.99 to two digits is 1.0 × 10¹).
func roundDigits(digits string, exponent, count int) (string, int) {
	if len(digits) <= count {
		return digits + strings.Repeat("0", count-len(digits)), exponent
	}
	rounded := []byte(digits[:count])
	if digits[count] >= '5' {
		at := count - 1
		for at >= 0 && rounded[at] == '9' {
			rounded[at] = '0'
			at--
		}
		if at < 0 {
			return "1" + string(rounded[:count-1]), exponent + 1
		}
		rounded[at]++
	}
	return string(rounded), exponent
}

// toIntegerOrInfinity is JavaScript's ToIntegerOrInfinity of a number: NaN is 0, an infinity itself,
// any other number without its fraction (-0 is 0).
func toIntegerOrInfinity(n float64) float64 {
	if n != n {
		return 0
	}
	return math.Trunc(n) + 0
}

// saturatedInt64 is toIntegerOrInfinity(n) as an int64, clamped to the int64 range: Go's int64(n) of
// NaN, an infinity or a number past the range is -9223372036854775808 on amd64.
func saturatedInt64(n float64) int64 {
	n = toIntegerOrInfinity(n)
	switch {
	case n >= 1<<63:
		return math.MaxInt64
	case n <= -(1 << 63):
		return math.MinInt64
	}
	return int64(n)
}

// nonFiniteText is the text JavaScript gives NaN, Infinity and -Infinity, and false for a finite number.
func nonFiniteText(n float64) (string, bool) {
	switch {
	case n != n:
		return "NaN", true
	case math.IsInf(n, 1):
		return "Infinity", true
	case math.IsInf(n, -1):
		return "-Infinity", true
	}
	return "", false
}

// toInt32 is JavaScript's ToInt32 of a number: NaN and the infinities are 0, any other number loses its
// fraction and wraps into -2^31 to 2^31 - 1 (16.9 is 16, 2^32 + 16 is 16, -1 is -1).
func toInt32(n float64) int32 {
	if n != n || math.IsInf(n, 0) {
		return 0
	}
	n = math.Mod(math.Trunc(n), 1<<32)
	if n < 0 {
		n += 1 << 32
	}
	return int32(uint32(n))
}

// stringToNumber is JavaScript's StringToNumber: white space around the text dropped, "" is 0,
// "Infinity" with or without a sign, 0x 0o 0b integers, else a decimal literal or NaN.
func stringToNumber(text string) float64 {
	text = trimSpace(text)
	if text == "" {
		return 0
	}
	switch text {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(text) > 2 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			return integerText(text[2:], base)
		}
	}
	if !decimalLiteral(text) {
		return math.NaN()
	}
	// ParseFloat reads every decimal literal the check lets through; past the largest number it gives
	// ±Infinity with ErrRange, and JavaScript gives the same
	n, _ := strconv.ParseFloat(text, 64)
	return n
}

// decimalLiteral reports [+-]? (digits [. digits?] | . digits) ([eE] [+-]? digits)?.
func decimalLiteral(text string) bool {
	end := decimalPrefix(text)
	return end > 0 && end == len(text)
}

// decimalPrefix is the length of the longest beginning of text that is a decimal literal, 0 when none
// is. An e without digits after it ends the number before the e ("1e" is "1", "1e+x" is "1").
func decimalPrefix(text string) int {
	at := 0
	if at < len(text) && (text[at] == '+' || text[at] == '-') {
		at++
	}
	digits := func() int {
		start := at
		for at < len(text) && text[at] >= '0' && text[at] <= '9' {
			at++
		}
		return at - start
	}
	whole := digits()
	fraction := 0
	if at < len(text) && text[at] == '.' {
		at++
		fraction = digits()
	}
	if whole == 0 && fraction == 0 {
		return 0
	}
	end := at
	if at < len(text) && (text[at] == 'e' || text[at] == 'E') {
		at++
		if at < len(text) && (text[at] == '+' || text[at] == '-') {
			at++
		}
		if digits() > 0 {
			end = at
		}
	}
	return end
}

// integerText reads digits in a base from 2 to 36, of any length: the nearest number to the integer
// they write. No digit, or one the base does not have, is NaN.
func integerText(digits string, base int) float64 {
	if digits == "" {
		return math.NaN()
	}
	for at := 0; at < len(digits); at++ {
		if digitValue(digits[at]) >= base {
			return math.NaN()
		}
	}
	if n, err := strconv.ParseUint(digits, base, 64); err == nil {
		return float64(n)
	}
	whole, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return math.NaN()
	}
	n, _ := new(big.Float).SetInt(whole).Float64()
	return n
}

func digitValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'z':
		return int(character-'a') + 10
	case character >= 'A' && character <= 'Z':
		return int(character-'A') + 10
	}
	return 99
}

// radixText is (n).toString(radix) for a radix other than 10, as V8 writes it (DoubleToRadixCString), the engine of
// node, which the parity tests hold Kitwork to (18/11/2026): the fraction digit by digit while what is left is past
// half the gap to the next double, the last digit rounded half to even with its carry taken back through the digits
// written; the integer part by division, each digit below a double's 53 bits of precision a 0. It wrote an integer
// part from 2^63 on in decimal ((1e21).toString(16) was "1000000000000000000000") and the fraction to 52 digits
// whatever the number held ((0.1).toString(3) had 52 digits where node writes 34).
func radixText(f float64, radix int) string {
	if text, nonFinite := nonFiniteText(f); nonFinite {
		return text
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	base := float64(radix)
	negative := f < 0
	if negative {
		f = -f
	}
	integer := math.Floor(f)
	fraction := f - integer
	// the fraction's digits stop at the precision of f: half the gap to the next double, at least the smallest
	delta := math.Max(math.SmallestNonzeroFloat64, 0.5*(math.Nextafter(f, math.Inf(1))-f))
	var tail []byte
	if fraction >= delta {
		tail = append(tail, '.')
		for {
			fraction *= base
			delta *= base
			digit := int(fraction)
			tail = append(tail, digits[digit])
			fraction -= float64(digit)
			if fraction > 0.5 || fraction == 0.5 && digit&1 == 1 {
				if fraction+delta > 1 {
					// round up: the carry goes back through the digits written, past the point into the integer
					for {
						last := len(tail) - 1
						if last == 0 {
							integer++
							tail = tail[:0]
							break
						}
						written := strings.IndexByte(digits, tail[last])
						if written+1 < radix {
							tail[last] = digits[written+1]
							break
						}
						tail = tail[:last]
					}
					break
				}
			}
			if fraction < delta {
				break
			}
		}
	}
	// the integer part, its last digit first: those below a double's precision are 0
	var head []byte
	for integer/base >= 1<<53 {
		integer /= base
		head = append(head, '0')
	}
	for {
		remainder := math.Mod(integer, base)
		head = append(head, digits[int(remainder)])
		integer = (integer - remainder) / base
		if integer <= 0 {
			break
		}
	}
	if negative {
		head = append(head, '-')
	}
	slices.Reverse(head)
	return string(head) + string(tail)
}
