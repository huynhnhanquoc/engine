package javascript

import (
	"math"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Phương thức của chuỗi, theo String.prototype. Vị trí và độ dài đếm bằng đơn vị UTF-16 (text.go). Đối số đổi theo
// đúng thứ tự JavaScript đổi, nên một valueOf riêng chạy đúng chỗ, đúng số lần. Không có RegExp: tham số mẫu là chuỗi.
var stringMethods = map[string]method{
	"at":           stringAt,
	"charAt":       stringCharAt,
	"charCodeAt":   stringCharCodeAt,
	"codePointAt":  stringCodePointAt,
	"concat":       stringConcat,
	"endsWith":     stringEndsWith,
	"includes":     stringIncludes,
	"indexOf":      stringIndexOf,
	"isWellFormed": stringIsWellFormed,
	"lastIndexOf":  stringLastIndexOf,
	"padEnd":       func(run Run, this value.Value, a []value.Value) value.Value { return stringPad(run, this, a, false) },
	"padStart":     func(run Run, this value.Value, a []value.Value) value.Value { return stringPad(run, this, a, true) },
	"repeat":       stringRepeat,
	"replace": func(run Run, this value.Value, a []value.Value) value.Value {
		return stringReplace(run, this, a, false)
	},
	"replaceAll": func(run Run, this value.Value, a []value.Value) value.Value { return stringReplace(run, this, a, true) },
	"slice":      stringSlice,
	"split":      stringSplit,
	"startsWith": stringStartsWith,
	"substr":     stringSubstr,
	"substring":  stringSubstring,
	"toLowerCase": func(_ Run, this value.Value, _ []value.Value) value.Value {
		return value.String(lowerText(this.String()))
	},
	"toUpperCase": func(_ Run, this value.Value, _ []value.Value) value.Value {
		return value.String(upperText(this.String()))
	},
	"toString":     func(_ Run, this value.Value, _ []value.Value) value.Value { return this },
	"toWellFormed": stringToWellFormed,
	"trim": func(_ Run, this value.Value, _ []value.Value) value.Value {
		return value.String(trimSpace(this.String()))
	},
	"trimEnd":   stringTrimEnd,
	"trimLeft":  stringTrimStart,
	"trimRight": stringTrimEnd,
	"trimStart": stringTrimStart,
	"valueOf":   func(_ Run, this value.Value, _ []value.Value) value.Value { return this },
}

func stringTrimStart(_ Run, this value.Value, _ []value.Value) value.Value {
	return value.String(trimSpaceLeft(this.String()))
}

func stringTrimEnd(_ Run, this value.Value, _ []value.Value) value.Value {
	return value.String(trimSpaceRight(this.String()))
}

// unitAtArgument là đơn vị ở vị trí đối số đầu chỉ ra (ToIntegerOrInfinity, không đếm từ cuối), và nó có không.
// unitAtArgument là đơn vị ở chỉ số đối số đầu của t (charAt, charCodeAt, codePointAt), đọc bằng con trỏ (cursor.go):
// s.charAt(i) lần lượt đi tiếp từ chỗ trước, không đi lại từ đầu.
// stringAt là s.at(i): i âm đếm từ cuối (độ dài: một lượt chạy nhớ nó); ngoài chuỗi là undefined.
func stringAt(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	if n < 0 {
		n += float64(t.Length())
	}
	result := value.Undefined()
	if unit, _, ok := t.unitAtNumber(n); ok {
		result = value.String(unitString(unit))
	}
	return spent(run, t, result)
}

func stringCharAt(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	result := value.String("")
	if unit, _, ok := t.unitAtNumber(n); ok {
		result = value.String(unitString(unit))
	}
	return spent(run, t, result)
}

func stringCharCodeAt(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	result := value.Number(math.NaN())
	if unit, _, ok := t.unitAtNumber(n); ok {
		result = value.Number(float64(unit))
	}
	return spent(run, t, result)
}

// stringCodePointAt là s.codePointAt(i): một nửa cao có nửa thấp theo sau là cả ký tự; ngoài chuỗi là undefined.
func stringCodePointAt(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	result := value.Undefined()
	if unit, at, ok := t.unitAtNumber(n); ok {
		result = value.Number(float64(unit))
		if unit >= 0xD800 && unit < 0xDC00 {
			if low, ok := t.unitAt(at + 1); ok && low >= 0xDC00 && low < 0xE000 {
				result = value.Number(float64(0x10000 + (rune(unit)-0xD800)<<10 + (rune(low) - 0xDC00)))
			}
		}
	}
	return spent(run, t, result)
}

// spent tính năng lượng cho phần con trỏ của t đã đi (work.go) rồi trả result, hay ENERGY_LIMIT khi hết.
func spent(run Run, t *TextMemo, result value.Value) value.Value {
	if failure := spend(run, t.take()); failure.IsInvalid() {
		return failure
	}
	return result
}

func stringConcat(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	for _, item := range arguments {
		piece, err := toText(run, item)
		if err != nil {
			return value.Error(err)
		}
		if len(s)+len(piece) > maxStringLength {
			return tooLong()
		}
		if failure := room(run, uint64(len(s)+len(piece))); failure.IsInvalid() {
			return failure
		}
		s = concatText(s, piece)
	}
	return value.String(s)
}

// searchAndPosition đổi chuỗi cần tìm rồi vị trí (đối số thứ hai, kẹp vào 0..length; vắng thì whenAbsent), theo thứ
// tự của includes, indexOf, startsWith, endsWith.
func searchAndPosition(run Run, s string, arguments []value.Value, whenAbsent int) (string, int, error) {
	search, err := toText(run, argument(arguments, 0))
	if err != nil {
		return "", 0, err
	}
	length := unitLength(s)
	position := whenAbsent
	if at := argument(arguments, 1); !at.IsUndefined() {
		n, err := toInteger(run, at)
		if err != nil {
			return "", 0, err
		}
		position = clampIndex(n, length)
	}
	return search, position, nil
}

func stringIncludes(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	search, from, err := searchAndPosition(run, s, arguments, 0)
	if err != nil {
		return value.Error(err)
	}
	return value.Boolean(unitIndex(s, search, from) >= 0)
}

func stringIndexOf(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	search, from, err := searchAndPosition(run, s, arguments, 0)
	if err != nil {
		return value.Error(err)
	}
	return value.Number(float64(unitIndex(s, search, from)))
}

// stringStartsWith là s.startsWith(search, position): chỉ đi tới position rồi bằng độ dài của search (cursor.go),
// không đếm độ dài của s.
func stringStartsWith(run Run, this value.Value, arguments []value.Value) value.Value {
	search, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	position := 0.0
	if at := argument(arguments, 1); !at.IsUndefined() {
		if position, err = toInteger(run, at); err != nil {
			return value.Error(err)
		}
	}
	var local, searchLocal TextMemo
	t, ts := textOf(run, this.String(), &local), textOf(run, search, &searchLocal)
	start := atMost(max(position, 0), len(t.text)) // âm là 0; một chuỗi không có nhiều đơn vị hơn byte
	searchLength := ts.Length()
	from, reached, cutStart := t.offset(start)
	result := false
	if reached < start { // position sau cuối: chỉ "" khớp ở đó
		result = searchLength == 0
	} else if to, reachedEnd, cutEnd := t.offset(start + searchLength); reachedEnd == start+searchLength {
		if !cutStart && !cutEnd {
			result = t.text[from:to] == search
		} else {
			result = t.slice(start, start+searchLength) == search
		}
	}
	if failure := spend(run, t.take()+ts.take()); failure.IsInvalid() {
		return failure
	}
	return value.Boolean(result)
}

// stringEndsWith là s.endsWith(search, end): độ dài của s (một lượt chạy nhớ nó), rồi đi tới end − độ dài của search.
func stringEndsWith(run Run, this value.Value, arguments []value.Value) value.Value {
	search, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	position := math.Inf(1)
	if at := argument(arguments, 1); !at.IsUndefined() {
		if position, err = toInteger(run, at); err != nil {
			return value.Error(err)
		}
	}
	var local, searchLocal TextMemo
	t, ts := textOf(run, this.String(), &local), textOf(run, search, &searchLocal)
	end := clampIndex(position, t.Length())
	start := end - ts.Length()
	result := start >= 0 && t.slice(start, end) == search
	if failure := spend(run, t.take()+ts.take()); failure.IsInvalid() {
		return failure
	}
	return value.Boolean(result)
}

// stringLastIndexOf là s.lastIndexOf(search, position): vị trí là NaN (hay vắng) thì tìm từ cuối.
func stringLastIndexOf(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	search, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	position, err := toNumber(run, argument(arguments, 1))
	if err != nil {
		return value.Error(err)
	}
	length := unitLength(s)
	from := length
	if position == position {
		from = clampIndex(position, length)
	}
	return value.Number(float64(unitLastIndex(s, search, from)))
}

func stringIsWellFormed(_ Run, this value.Value, _ []value.Value) value.Value {
	return value.Boolean(!hasLoneSurrogate(this.String()))
}

// stringToWellFormed thay mỗi nửa cặp đứng một mình bằng U+FFFD.
func stringToWellFormed(_ Run, this value.Value, _ []value.Value) value.Value {
	s := this.String()
	if !hasLoneSurrogate(s) {
		return this
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if _, ok := loneSurrogateAt(s, i); ok {
			b.WriteString("\uFFFD")
			i += 3
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return value.String(b.String())
}

// stringPad là s.padStart(maxLength, fill) và s.padEnd: fill lặp lại rồi cắt (theo đơn vị) cho đủ maxLength. fill chỉ
// được đổi khi phải đệm, như JavaScript.
func stringPad(run Run, this value.Value, arguments []value.Value, atStart bool) value.Value {
	s := this.String()
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	length := unitLength(s)
	if n <= float64(length) {
		return this
	}
	fill := " "
	if f := argument(arguments, 1); !f.IsUndefined() {
		if fill, err = toText(run, f); err != nil {
			return value.Error(err)
		}
	}
	if fill == "" {
		return this
	}
	if n > maxStringLength {
		return tooLong()
	}
	missing := int(n) - length
	fillLength := unitLength(fill)
	if failure := room(run, uint64(len(s))+uint64(missing/fillLength+1)*uint64(len(fill))); failure.IsInvalid() {
		return failure
	}
	filler := strings.Repeat(fill, missing/fillLength) + unitSlice(fill, 0, missing%fillLength)
	if len(filler)+len(s) > maxStringLength {
		return tooLong()
	}
	if atStart {
		return value.String(mergeSurrogates(filler + s))
	}
	return value.String(mergeSurrogates(s + filler))
}

// stringRepeat là s.repeat(count): count âm hay vô cực là RangeError.
func stringRepeat(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	if n < 0 || math.IsInf(n, 1) {
		return rangeError("invalid count value: %s", numberText(n))
	}
	if n == 0 || s == "" {
		return value.String("")
	}
	if n > float64(maxStringLength/len(s)) {
		return tooLong()
	}
	if failure := room(run, uint64(len(s))*uint64(n)); failure.IsInvalid() {
		return failure
	}
	return value.String(mergeSurrogates(strings.Repeat(s, int(n))))
}

// stringReplace là s.replace(search, replacement) và s.replaceAll: replacement là một hàm (gọi với chữ khớp, vị trí,
// cả chuỗi, mỗi chỗ khớp một lần theo thứ tự) hay một chuỗi có $$, $&, $` và $'. Chuỗi rỗng khớp ở mọi vị trí với
// replaceAll.
func stringReplace(run Run, this value.Value, arguments []value.Value, all bool) value.Value {
	s := this.String()
	search, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	replaceValue := argument(arguments, 1)
	functional := replaceValue.IsFunction()
	var replacement string
	if !functional {
		if replacement, err = toText(run, replaceValue); err != nil {
			return value.Error(err)
		}
	}
	text, needle := newUnitText(s), newUnitText(search)
	pattern := needle.allUnits()
	var positions []int
	g := grower{run: run}
	for at := text.index(needle, pattern, 0); at >= 0; {
		positions = append(positions, at)
		if failure := g.fits(uint64(len(positions)) * 8); failure.IsInvalid() {
			return failure
		}
		if !all {
			break
		}
		at = text.index(needle, pattern, at+max(1, len(pattern)))
	}
	if positions == nil {
		return this
	}
	if failure := spend(run, uint64(len(positions))*matchUnits); failure.IsInvalid() {
		return failure
	}
	var b strings.Builder
	end := 0
	buffer := make([]value.Value, 3)
	for _, at := range positions {
		b.WriteString(text.slice(end, at))
		if functional {
			result := call(run, replaceValue, callArguments(buffer, value.String(search), value.Number(float64(at)), this)...)
			if result.IsInvalid() {
				return result
			}
			piece, err := toText(run, result)
			if err != nil {
				return value.Error(err)
			}
			b.WriteString(piece)
		} else {
			b.WriteString(substitution(text, search, at, len(pattern), replacement))
		}
		if b.Len() > maxStringLength {
			return tooLong()
		}
		if failure := g.fits(uint64(len(positions))*8 + uint64(b.Len())); failure.IsInvalid() {
			return failure
		}
		end = at + len(pattern)
	}
	b.WriteString(text.slice(end, text.length()))
	if b.Len() > maxStringLength {
		return tooLong()
	}
	return value.String(mergeSurrogates(b.String()))
}

// substitution là GetSubstitution không có nhóm bắt: $$ là $, $& là chữ khớp, $` là phần trước, $' là phần sau; mọi
// $ khác (kể cả $1, $<) giữ nguyên.
func substitution(text unitText, matched string, at, matchLength int, replacement string) string {
	if strings.IndexByte(replacement, '$') < 0 {
		return replacement
	}
	var b strings.Builder
	for i := 0; i < len(replacement); i++ {
		if replacement[i] != '$' || i+1 == len(replacement) {
			b.WriteByte(replacement[i])
			continue
		}
		switch replacement[i+1] {
		case '$':
			b.WriteByte('$')
		case '&':
			b.WriteString(matched)
		case '`':
			b.WriteString(text.slice(0, at))
		case '\'':
			b.WriteString(text.slice(min(at+matchLength, text.length()), text.length()))
		default:
			b.WriteByte('$')
			continue
		}
		i++
	}
	return b.String()
}

// stringSlice là s.slice(start, end): hai đầu không âm thì chỉ đi tới end (slice(1) không đi gì ngoài một đơn vị);
// một đầu âm mới cần độ dài (cursor.go).
func stringSlice(run Run, this value.Value, arguments []value.Value) value.Value {
	start, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	end := math.Inf(1)
	e := argument(arguments, 1)
	if !e.IsUndefined() {
		if end, err = toInteger(run, e); err != nil {
			return value.Error(err)
		}
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	var out string
	if e.IsUndefined() && start >= 0 {
		out = t.sliceFrom(atMost(start, len(t.text)))
	} else {
		if start >= 0 && end >= 0 {
			out = t.slice(atMost(start, len(t.text)), atMost(end, len(t.text)))
		} else {
			length := t.Length()
			out = t.slice(relativeIndex(start, length), relativeIndex(end, length))
		}
	}
	if failure := spend(run, t.take()); failure.IsInvalid() {
		return failure
	}
	return value.String(out)
}

// atMost là một chỉ số không âm (n ≥ 0, có thể vô cực) kẹp ở limit: một chuỗi không có nhiều đơn vị hơn byte, nên
// kẹp ở số byte rồi đi là đủ, không cần độ dài.
func atMost(n float64, limit int) int {
	if n >= float64(limit) {
		return limit
	}
	return int(n)
}

// stringSplit là s.split(separator, limit): giới hạn đổi trước dấu ngăn, như JavaScript; dấu ngăn rỗng tách từng đơn
// vị (một emoji thành hai nửa).
func stringSplit(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	limit := uint32(math.MaxUint32)
	if l := argument(arguments, 1); !l.IsUndefined() {
		n, err := toNumber(run, l)
		if err != nil {
			return value.Error(err)
		}
		limit = toUint32(n)
	}
	separatorValue := argument(arguments, 0)
	separator, err := toText(run, separatorValue)
	if err != nil {
		return value.Error(err)
	}
	switch {
	case limit == 0:
		return value.Array()
	case separatorValue.IsUndefined():
		return value.Array(this)
	}
	text, needle := newUnitText(s), newUnitText(separator)
	if separator == "" {
		count := min(int(min(limit, math.MaxInt32)), text.length())
		if failure := room(run, uint64(count)*value.ItemBytes); failure.IsInvalid() {
			return failure
		}
		parts := make([]value.Value, count)
		for i := range count {
			parts[i] = value.String(text.slice(i, i+1))
		}
		return value.ArrayOf(parts)
	}
	if s == "" {
		return value.Array(this)
	}
	pattern := needle.allUnits()
	var parts []value.Value
	start := 0
	g := grower{run: run}
	for at := text.index(needle, pattern, 0); at >= 0; at = text.index(needle, pattern, start) {
		parts = append(parts, value.String(text.slice(start, at)))
		if failure := g.fits(uint64(len(parts)) * value.ItemBytes); failure.IsInvalid() {
			return failure
		}
		if uint32(len(parts)) == limit {
			return value.ArrayOf(parts)
		}
		start = at + len(pattern)
	}
	parts = append(parts, value.String(text.slice(start, text.length())))
	return value.ArrayOf(parts)
}

// stringSubstr là s.substr(start, length): start âm đếm từ cuối.
func stringSubstr(run Run, this value.Value, arguments []value.Value) value.Value {
	s := this.String()
	size := unitLength(s)
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	start := relativeIndex(n, size)
	count := float64(size)
	if l := argument(arguments, 1); !l.IsUndefined() {
		if count, err = toInteger(run, l); err != nil {
			return value.Error(err)
		}
	}
	count = math.Max(0, math.Min(count, float64(size)))
	end := min(start+int(count), size)
	return value.String(unitSlice(s, start, end))
}

// stringSubstring là s.substring(start, end): âm là 0, hai đầu đổi chỗ khi start lớn hơn.
func stringSubstring(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	m := math.Inf(1)
	if e := argument(arguments, 1); !e.IsUndefined() {
		if m, err = toInteger(run, e); err != nil {
			return value.Error(err)
		}
	}
	var local TextMemo
	t := textOf(run, this.String(), &local)
	// kẹp ở số byte, không ở độ dài: một đầu sau cuối cho cùng kết quả (cursor.go), nên không phải đếm
	start, end := clampIndex(n, len(t.text)), clampIndex(m, len(t.text))
	out := t.slice(min(start, end), max(start, end))
	if failure := spend(run, t.take()); failure.IsInvalid() {
		return failure
	}
	return value.String(out)
}

// unitText là một chuỗi đọc nhiều lần theo đơn vị (split, replace): ASCII đọc thẳng byte, chuỗi khác đổi ra đơn vị
// một lần, để mỗi lần tìm và cắt không phải đi lại từ đầu.
type unitText struct {
	s     string
	ascii bool
	u     []uint16 // nil khi ASCII
}

func newUnitText(s string) unitText {
	t := unitText{s: s, ascii: isASCII(s)}
	if !t.ascii {
		t.u = units(s)
	}
	return t
}

func (t unitText) length() int {
	if t.ascii {
		return len(t.s)
	}
	return len(t.u)
}

func (t unitText) slice(a, b int) string {
	if a >= b {
		return ""
	}
	if t.ascii {
		return t.s[a:b]
	}
	return encodeUnits(t.u[a:b])
}

// allUnits là mọi đơn vị của t, kể cả khi t là ASCII (để so với một chuỗi không ASCII).
func (t unitText) allUnits() []uint16 {
	if !t.ascii {
		return t.u
	}
	out := make([]uint16, len(t.s))
	for i := 0; i < len(t.s); i++ {
		out[i] = uint16(t.s[i])
	}
	return out
}

// index là vị trí đầu tiên của needle (pattern là needle.allUnits()) trong t từ from trở đi, -1 khi không có.
func (t unitText) index(needle unitText, pattern []uint16, from int) int {
	if from > t.length() {
		return -1
	}
	if t.ascii {
		if !needle.ascii {
			return -1 // needle có một đơn vị ngoài ASCII, t không có
		}
		i := strings.Index(t.s[from:], needle.s)
		if i < 0 {
			return -1
		}
		return from + i
	}
	for at := from; at+len(pattern) <= len(t.u); at++ {
		if equalUnits(t.u[at:at+len(pattern)], pattern) {
			return at
		}
	}
	return -1
}
