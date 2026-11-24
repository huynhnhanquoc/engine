package javascript

import (
	"math"
	"strconv"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// parseInt, parseFloat, isNaN, isFinite toàn cục, theo JavaScript, đối chiếu với node. Number.parseInt và
// Number.parseFloat là chính hai hàm này. Chép từ engine2 (core/builtins/number.go, core/value/number.go).

var (
	parseIntFunction   = native("parseInt", 2, parseInt)
	parseFloatFunction = native("parseFloat", 1, parseFloat)
	isNaNFunction      = native("isNaN", 1, numberThen(math.IsNaN))
	isFiniteFunction   = native("isFinite", 1, numberThen(func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }))
)

// numberThen là isNaN, isFinite toàn cục: đối số đổi thành số trước (isNaN("abc") là true, khác Number.isNaN).
func numberThen(test func(float64) bool) func(Run, []value.Value) value.Value {
	return func(run Run, arguments []value.Value) value.Value {
		n, err := toNumber(run, argument(arguments, 0))
		if err != nil {
			return value.Error(err)
		}
		return value.Boolean(test(n))
	}
}

// parseInt là parseInt(text, radix): text đổi thành chữ trước, rồi radix thành số, như JavaScript; các chữ số của cơ số
// ở đầu chữ ("12px" là 12, "0x1f" là 31, ("ff", 16) là 255); không có chữ số nào là NaN. Chỗ Kitwork khác: một số quá
// 2^53 ở cơ số không phải 10 và không phải luỹ thừa của 2 là lỗi, vì JavaScript để mỗi engine tự làm tròn các chữ số
// cuối của nó (V8 có cách riêng); không ai viết nó, và chép cách làm tròn của V8 không đáng.
func parseInt(run Run, arguments []value.Value) value.Value {
	text, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	radix, err := toNumber(run, argument(arguments, 1))
	if err != nil {
		return value.Error(err)
	}
	n, base := parseIntText(text, radix)
	if math.Abs(n) >= 1<<53 && base != 10 && base&(base-1) != 0 {
		return rangeError("parseInt: a number past 2^53 in base %d is not exact, JavaScript leaves its last digits to each engine: read it in base 10 or a power of 2", base)
	}
	return value.Number(n)
}

// parseIntText là parseInt và cơ số nó đọc các chữ số: khoảng trắng đầu bỏ đi, một dấu, 0x khi radix là 0 hay 16, rồi
// các chữ số của cơ số. radix đổi bằng ToInt32: NaN và undefined là 0, nghĩa là 10; ngoài 2 tới 36 là NaN.
func parseIntText(text string, radix float64) (float64, int) {
	text = trimSpaceLeft(text)
	negative := false
	if text != "" && (text[0] == '+' || text[0] == '-') {
		negative = text[0] == '-'
		text = text[1:]
	}
	base := int(toInt32(radix))
	prefixed := true // 0x được đứng trước các chữ số
	switch {
	case base == 0:
		base = 10
	case base < 2 || base > 36:
		return math.NaN(), base
	default:
		prefixed = base == 16
	}
	if prefixed && len(text) >= 2 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X') {
		text, base = text[2:], 16
	}
	end := 0
	for end < len(text) && digitValue(text[end]) < base {
		end++
	}
	if end == 0 {
		return math.NaN(), base
	}
	var n float64
	if base == 10 {
		n, _ = strconv.ParseFloat(text[:end], 64) // làm tròn đúng, như V8; quá lớn là vô cực
	} else {
		n = integerText(text[:end], base)
	}
	if negative {
		n = -n
	}
	return n, base
}

// parseFloat là parseFloat(text): text đổi thành chữ trước; số thập phân dài nhất ở đầu chữ (khoảng trắng đầu bỏ đi),
// hay Infinity có dấu hoặc không; không có là NaN.
func parseFloat(run Run, arguments []value.Value) value.Value {
	text, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	return value.Number(parseFloatText(text))
}

func parseFloatText(text string) float64 {
	text = trimSpaceLeft(text)
	unsigned := text
	if unsigned != "" && (unsigned[0] == '+' || unsigned[0] == '-') {
		unsigned = unsigned[1:]
	}
	if strings.HasPrefix(unsigned, "Infinity") {
		if text[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	end := decimalPrefix(text)
	if end == 0 {
		return math.NaN()
	}
	n, _ := strconv.ParseFloat(text[:end], 64)
	return n
}
