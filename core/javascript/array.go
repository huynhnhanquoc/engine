package javascript

import (
	"math"
	"slices"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Phương thức của mảng, theo Array.prototype. Mảng của Kitwork không có lỗ: mọi chỗ từ 0 tới length − 1 đều có giá
// trị. Phương thức sửa mảng (pop, push, shift, unshift, splice, sort, reverse, fill, copyWithin) sửa tại chỗ qua
// Push, SetIndex, Splice của value, nên mọi bản chép của mảng thấy; phương thức to… (toSorted, toReversed, toSpliced,
// with) trả mảng mới. Hàm gọi lại nhận (phần tử, chỉ số, mảng); độ dài đọc một lần lúc bắt đầu, như JavaScript: một
// hàm gọi lại làm mảng ngắn đi thì chỗ đã mất bị bỏ qua (find… đọc nó là undefined).
var arrayMethods = map[string]method{
	"at":         arrayAt,
	"concat":     arrayConcat,
	"copyWithin": arrayCopyWithin,
	"every":      arrayEvery,
	"fill":       arrayFill,
	"filter":     arrayFilter,
	"find": func(run Run, this value.Value, a []value.Value) value.Value {
		return arrayFind(run, this, a, false, false)
	},
	"findIndex": func(run Run, this value.Value, a []value.Value) value.Value {
		return arrayFind(run, this, a, false, true)
	},
	"findLast": func(run Run, this value.Value, a []value.Value) value.Value {
		return arrayFind(run, this, a, true, false)
	},
	"findLastIndex": func(run Run, this value.Value, a []value.Value) value.Value {
		return arrayFind(run, this, a, true, true)
	},
	"flat":        arrayFlat,
	"flatMap":     arrayFlatMap,
	"forEach":     arrayForEach,
	"includes":    arrayIncludes,
	"indexOf":     arrayIndexOf,
	"join":        arrayJoin,
	"lastIndexOf": arrayLastIndexOf,
	"map":         arrayMap,
	"pop":         arrayPop,
	"push":        arrayPush,
	"reduce":      func(run Run, this value.Value, a []value.Value) value.Value { return arrayReduce(run, this, a, false) },
	"reduceRight": func(run Run, this value.Value, a []value.Value) value.Value { return arrayReduce(run, this, a, true) },
	"reverse":     arrayReverse,
	"shift":       arrayShift,
	"slice":       arraySlice,
	"some":        arraySome,
	"sort":        func(run Run, this value.Value, a []value.Value) value.Value { return arraySort(run, this, a, true) },
	"splice":      func(run Run, this value.Value, a []value.Value) value.Value { return arraySplice(run, this, a, true) },
	"toReversed":  arrayToReversed,
	"toSorted":    func(run Run, this value.Value, a []value.Value) value.Value { return arraySort(run, this, a, false) },
	"toSpliced":   func(run Run, this value.Value, a []value.Value) value.Value { return arraySplice(run, this, a, false) },
	"toString":    func(run Run, this value.Value, _ []value.Value) value.Value { return ToString(run, this) },
	"unshift":     arrayUnshift,
	"with":        arrayWith,
}

// relativeArgument là đối số thứ i đổi thành chỉ số tương đối kẹp vào 0..length; vắng (undefined) là whenAbsent.
func relativeArgument(run Run, arguments []value.Value, i, length, whenAbsent int) (int, error) {
	a := argument(arguments, i)
	if a.IsUndefined() {
		return whenAbsent, nil
	}
	n, err := toInteger(run, a)
	if err != nil {
		return 0, err
	}
	return relativeIndex(n, length), nil
}

func arrayAt(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	length := float64(this.Length())
	if n < 0 {
		n += length
	}
	if n < 0 || n >= length {
		return value.Undefined()
	}
	return this.Index(int(n))
}

// arrayConcat là a.concat(…items): một mảng trải các phần tử của nó ra, giá trị khác thêm nguyên. Lỗ của a và của
// mảng được trải giữ nguyên trong kết quả, như JavaScript: kết quả mang dấu lỗ.
func arrayConcat(run Run, this value.Value, arguments []value.Value) value.Value {
	total := this.Length()
	for _, item := range arguments {
		total += max(item.Length(), 1)
	}
	if failure := room(run, uint64(total)*value.ItemBytes); failure.IsInvalid() {
		return failure
	}
	items, holes := this.Items(), this.HasHoles()
	for _, item := range arguments {
		if item.IsArray() {
			items = append(items, item.Items()...)
			holes = holes || item.HasHoles()
		} else {
			items = append(items, item)
		}
	}
	if holes {
		return value.ArrayOf(items).WithHoles()
	}
	return value.ArrayOf(items)
}

// arrayCopyWithin là a.copyWithin(target, start, end): chép một khúc của mảng đè lên chính nó, đọc từ bản chép trước
// khi ghi nên hai khúc chồng nhau vẫn đúng.
func arrayCopyWithin(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	to, err := relativeArgument(run, arguments, 0, length, 0)
	if err != nil {
		return value.Error(err)
	}
	from, err := relativeArgument(run, arguments, 1, length, 0)
	if err != nil {
		return value.Error(err)
	}
	end, err := relativeArgument(run, arguments, 2, length, length)
	if err != nil {
		return value.Error(err)
	}
	items := this.Items()
	for i := range min(end-from, length-to) {
		if written := this.SetIndex(to+i, items[from+i]); written.IsInvalid() {
			return written // mảng đã đóng băng: JavaScript ném ở lần ghi đầu
		}
	}
	return this
}

func arrayFill(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	start, err := relativeArgument(run, arguments, 1, length, 0)
	if err != nil {
		return value.Error(err)
	}
	end, err := relativeArgument(run, arguments, 2, length, length)
	if err != nil {
		return value.Error(err)
	}
	for i := start; i < end; i++ {
		if written := this.SetIndex(i, argument(arguments, 0)); written.IsInvalid() {
			return written // mảng đã đóng băng; không chỗ nào để điền thì không ghi gì, như JavaScript
		}
	}
	if start == 0 && end == length {
		this.WithoutHoles() // điền kín, như Array(n).fill(x): không còn lỗ
	}
	return this
}

// each gọi f(phần tử, chỉ số, mảng) cho từng chỗ còn trong mảng, từ đầu (hay từ cuối), độ dài đọc một lần; f trả
// false để dừng. Lỗi của f, hay hàm gọi lại không phải hàm, là kết quả.
func each(run Run, this value.Value, callback value.Value, backward bool, f func(item value.Value, i int, result value.Value) bool) value.Value {
	if !callback.IsFunction() {
		return value.Error(errNotAFunction(callback))
	}
	length := this.Length()
	buffer := make([]value.Value, 3) // một mảng cho cả vòng (callArguments)
	for step := range length {
		i := step
		if backward {
			i = length - 1 - step
		}
		if i >= this.Length() {
			continue
		}
		item := this.Index(i)
		result := call(run, callback, callArguments(buffer, item, value.Number(float64(i)), this)...)
		if result.IsInvalid() {
			return result
		}
		if !f(item, i, result) {
			break
		}
	}
	return value.Undefined()
}

func arrayForEach(run Run, this value.Value, arguments []value.Value) value.Value {
	return each(run, this, argument(arguments, 0), false, func(value.Value, int, value.Value) bool { return true })
}

func arrayMap(run Run, this value.Value, arguments []value.Value) value.Value {
	out := make([]value.Value, this.Length())
	defer letGo(run, keep(run, Kept{Items: &out}))
	if failure := each(run, this, argument(arguments, 0), false, func(_ value.Value, i int, result value.Value) bool {
		out[i] = result
		return true
	}); failure.IsInvalid() {
		return failure
	}
	return value.ArrayOf(out)
}

func arrayFilter(run Run, this value.Value, arguments []value.Value) value.Value {
	var out []value.Value // chỉ các phần tử của this: không cần Keep, this giữ chúng
	if failure := each(run, this, argument(arguments, 0), false, func(item value.Value, _ int, result value.Value) bool {
		if ToBoolean(result) {
			out = append(out, item)
		}
		return true
	}); failure.IsInvalid() {
		return failure
	}
	return value.ArrayOf(out)
}

func arraySome(run Run, this value.Value, arguments []value.Value) value.Value {
	found := false
	if failure := each(run, this, argument(arguments, 0), false, func(_ value.Value, _ int, result value.Value) bool {
		found = ToBoolean(result)
		return !found
	}); failure.IsInvalid() {
		return failure
	}
	return value.Boolean(found)
}

func arrayEvery(run Run, this value.Value, arguments []value.Value) value.Value {
	all := true
	if failure := each(run, this, argument(arguments, 0), false, func(_ value.Value, _ int, result value.Value) bool {
		all = ToBoolean(result)
		return all
	}); failure.IsInvalid() {
		return failure
	}
	return value.Boolean(all)
}

// arrayFind là find, findIndex, findLast, findLastIndex: một chỗ đã mất khi mảng ngắn đi được đọc là undefined, không
// bỏ qua, như JavaScript.
func arrayFind(run Run, this value.Value, arguments []value.Value, backward, wantIndex bool) value.Value {
	callback := argument(arguments, 0)
	if !callback.IsFunction() {
		return value.Error(errNotAFunction(callback))
	}
	length := this.Length()
	buffer := make([]value.Value, 3)
	for step := range length {
		i := step
		if backward {
			i = length - 1 - step
		}
		item := this.Index(i)
		result := call(run, callback, callArguments(buffer, item, value.Number(float64(i)), this)...)
		if result.IsInvalid() {
			return result
		}
		if ToBoolean(result) {
			if wantIndex {
				return value.Number(float64(i))
			}
			return item
		}
	}
	if wantIndex {
		return value.Number(-1)
	}
	return value.Undefined()
}

// arrayReduce là reduce và reduceRight: không có giá trị đầu thì phần tử đầu (hay cuối) là giá trị đầu; mảng rỗng mà
// không có giá trị đầu là TypeError.
func arrayReduce(run Run, this value.Value, arguments []value.Value, backward bool) value.Value {
	callback := argument(arguments, 0)
	if !callback.IsFunction() {
		return value.Error(errNotAFunction(callback))
	}
	length := this.Length()
	step := 0
	var accumulator value.Value
	if len(arguments) > 1 {
		accumulator = arguments[1]
	} else {
		if length == 0 {
			return typeError("reduce of empty array with no initial value")
		}
		accumulator = this.Index(position(0, length, backward))
		step = 1
	}
	buffer := make([]value.Value, 4)
	for ; step < length; step++ {
		i := position(step, length, backward)
		if i >= this.Length() {
			continue
		}
		accumulator = call(run, callback, callArguments(buffer, accumulator, this.Index(i), value.Number(float64(i)), this)...)
		if accumulator.IsInvalid() {
			return accumulator
		}
	}
	return accumulator
}

// position là chỗ của bước step khi đi qua length chỗ, từ đầu hay từ cuối.
func position(step, length int, backward bool) int {
	if backward {
		return length - 1 - step
	}
	return step
}

// arrayFlat là a.flat(depth): depth vắng là 1. Một mảng chứa chính nó trên đường đi xuống là RangeError, như JavaScript
// tràn ngăn xếp, không phải vòng lặp mãi.
func arrayFlat(run Run, this value.Value, arguments []value.Value) value.Value {
	depth := 1.0
	if d := argument(arguments, 0); !d.IsUndefined() {
		var err error
		if depth, err = toInteger(run, d); err != nil {
			return value.Error(err)
		}
	}
	if hasHolesWithin(this, depth, nil) {
		return value.Error(holesRefused("a.flat(…)")) // flat bỏ lỗ của mảng nó trải ra
	}
	out, failure := flatten(&grower{run: run}, nil, this, depth, nil)
	if failure.IsInvalid() {
		return failure
	}
	return value.ArrayOf(out)
}

// hasHolesWithin là trong các mảng flat trải ra (v, và mảng bên trong tới depth tầng) có một mảng mang dấu lỗ. Một
// mảng chứa chính nó thì dừng ở đó: flatten báo nó.
func hasHolesWithin(v value.Value, depth float64, path []value.Value) bool {
	for _, outer := range path {
		if outer.Identical(v) {
			return false
		}
	}
	if v.HasHoles() {
		return true
	}
	path = append(path, v)
	for i := range v.Length() {
		if item := v.Index(i); item.IsArray() && depth > 0 && hasHolesWithin(item, depth-1, path) {
			return true
		}
	}
	return false
}

// flatten thêm các phần tử của v (trải các mảng bên trong tới depth tầng) vào out, hỏi g chỗ khi out lớn dần (một mảng
// lặp lại một mảng lớn trải ra nhiều hơn nó giữ). Một mảng chứa chính nó là RangeError.
func flatten(g *grower, out []value.Value, v value.Value, depth float64, path []value.Value) ([]value.Value, value.Value) {
	for _, outer := range path {
		if outer.Identical(v) {
			return nil, rangeError("maximum call stack size exceeded")
		}
	}
	path = append(path, v)
	for i := range v.Length() {
		item := v.Index(i)
		if item.IsArray() && depth > 0 {
			var failure value.Value
			if out, failure = flatten(g, out, item, depth-1, path); failure.IsInvalid() {
				return nil, failure
			}
			continue
		}
		out = append(out, item)
		if failure := g.fits(uint64(len(out)) * value.ItemBytes); failure.IsInvalid() {
			return nil, failure
		}
	}
	return out, value.Value{}
}

// arrayFlatMap là a.flatMap(f): kết quả của f là mảng thì trải ra một tầng.
func arrayFlatMap(run Run, this value.Value, arguments []value.Value) value.Value {
	var out []value.Value
	defer letGo(run, keep(run, Kept{Items: &out}))
	holes := false
	if failure := each(run, this, argument(arguments, 0), false, func(_ value.Value, _ int, result value.Value) bool {
		if holes = result.HasHoles(); holes {
			return false // flatMap bỏ lỗ của mảng hàm cho
		}
		if result.IsArray() {
			out = append(out, result.Items()...)
		} else {
			out = append(out, result)
		}
		return true
	}); failure.IsInvalid() {
		return failure
	}
	if holes {
		return value.Error(holesRefused("a.flatMap(…)"))
	}
	return value.ArrayOf(out)
}

// fromIndex là chỗ bắt đầu tìm của includes và indexOf: âm đếm từ cuối (kẹp ở 0); +Infinity là không tìm.
func fromIndex(run Run, arguments []value.Value, length int) (int, bool, error) {
	n, err := toInteger(run, argument(arguments, 1))
	if err != nil {
		return 0, false, err
	}
	switch {
	case math.IsInf(n, 1):
		return 0, false, nil
	case n < 0:
		return max(0, int(math.Max(n+float64(length), 0))), true, nil
	}
	return int(math.Min(n, float64(length))), true, nil
}

// arrayIncludes là a.includes(x, from): so như SameValueZero (NaN thấy NaN). Mảng rỗng trả false trước khi đổi from.
func arrayIncludes(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	if length == 0 {
		return value.Boolean(false)
	}
	start, ok, err := fromIndex(run, arguments, length)
	if err != nil {
		return value.Error(err)
	}
	for i := start; ok && i < length; i++ {
		if SameValueZero(this.Index(i), argument(arguments, 0)) {
			return value.Boolean(true)
		}
	}
	return value.Boolean(false)
}

// arrayIndexOf là a.indexOf(x, from): so như === (NaN không thấy NaN).
func arrayIndexOf(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	if length == 0 {
		return value.Number(-1)
	}
	start, ok, err := fromIndex(run, arguments, length)
	if err != nil {
		return value.Error(err)
	}
	for i := start; ok && i < length; i++ {
		if StrictEquals(this.Index(i), argument(arguments, 0)) {
			return value.Number(float64(i))
		}
	}
	return value.Number(-1)
}

// arrayLastIndexOf là a.lastIndexOf(x, from): from có mặt (kể cả undefined, là 0) thì tìm ngược từ đó, vắng thì từ
// cuối.
func arrayLastIndexOf(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	if length == 0 {
		return value.Number(-1)
	}
	n := float64(length - 1)
	if len(arguments) > 1 {
		var err error
		if n, err = toInteger(run, arguments[1]); err != nil {
			return value.Error(err)
		}
	}
	start := int(math.Min(n, float64(length-1)))
	if n < 0 {
		if n+float64(length) < 0 {
			return value.Number(-1)
		}
		start = int(n + float64(length))
	}
	for i := start; i >= 0; i-- {
		if StrictEquals(this.Index(i), argument(arguments, 0)) {
			return value.Number(float64(i))
		}
	}
	return value.Number(-1)
}

func arrayJoin(run Run, this value.Value, arguments []value.Value) value.Value {
	separator := ","
	if s := argument(arguments, 0); !s.IsUndefined() {
		var err error
		if separator, err = toText(run, s); err != nil {
			return value.Error(err)
		}
	}
	text, err := join(run, this, separator, nil)
	if err != nil {
		return value.Error(err)
	}
	return value.String(text)
}

// arrayPop, arrayPush, arrayShift, arrayUnshift ghi length cả khi mảng rỗng hay không có gì để thêm, như JavaScript:
// trên một mảng đã đóng băng chúng luôn là lỗi (value.Splice, value.Push).
func arrayPop(_ Run, this value.Value, _ []value.Value) value.Value {
	removed := this.Splice(this.Length()-1, 1)
	if removed.IsInvalid() {
		return removed
	}
	return removed.Index(0)
}

func arrayPush(_ Run, this value.Value, arguments []value.Value) value.Value {
	if pushed := this.Push(arguments...); pushed.IsInvalid() {
		return pushed
	}
	return value.Number(float64(this.Length()))
}

func arrayShift(_ Run, this value.Value, _ []value.Value) value.Value {
	removed := this.Splice(0, 1)
	if removed.IsInvalid() {
		return removed
	}
	return removed.Index(0)
}

func arrayUnshift(_ Run, this value.Value, arguments []value.Value) value.Value {
	if spliced := this.Splice(0, 0, arguments...); spliced.IsInvalid() {
		return spliced
	}
	return value.Number(float64(this.Length()))
}

// arrayReverse là a.reverse(): dưới hai phần tử thì không ghi gì, như JavaScript (một mảng đã đóng băng không lỗi).
func arrayReverse(_ Run, this value.Value, _ []value.Value) value.Value {
	items := this.Items()
	if len(items) < 2 {
		return this
	}
	slices.Reverse(items)
	if written := this.Splice(0, len(items), items...); written.IsInvalid() {
		return written
	}
	return this
}

func arrayToReversed(_ Run, this value.Value, _ []value.Value) value.Value {
	items := this.Items()
	slices.Reverse(items)
	return value.ArrayOf(items)
}

func arraySlice(run Run, this value.Value, arguments []value.Value) value.Value {
	length := this.Length()
	start, err := relativeArgument(run, arguments, 0, length, 0)
	if err != nil {
		return value.Error(err)
	}
	end, err := relativeArgument(run, arguments, 1, length, length)
	if err != nil {
		return value.Error(err)
	}
	if start >= end {
		return value.Array()
	}
	sliced := value.ArrayOf(this.ItemsBetween(start, end))
	if this.HasHoles() {
		sliced.WithHoles() // slice giữ lỗ, như JavaScript
	}
	return sliced
}

// arraySplice là a.splice(start, deleteCount, …items) (inPlace) và a.toSpliced: vắng deleteCount thì gỡ tới cuối,
// không đối số nào thì không gỡ gì. splice trả các phần tử đã gỡ, toSpliced trả mảng mới.
func arraySplice(run Run, this value.Value, arguments []value.Value, inPlace bool) value.Value {
	length := this.Length()
	start, err := relativeArgument(run, arguments, 0, length, 0)
	if err != nil {
		return value.Error(err)
	}
	count := 0
	switch len(arguments) {
	case 0:
	case 1:
		count = length - start
	default:
		n, err := toInteger(run, arguments[1])
		if err != nil {
			return value.Error(err)
		}
		count = int(math.Max(0, math.Min(n, float64(length-start))))
	}
	inserted := arguments[min(2, len(arguments)):]
	target := this
	if !inPlace {
		target = value.ArrayOf(this.Items())
	}
	removed := target.Splice(start, count, inserted...)
	switch {
	case removed.IsInvalid():
		return removed // mảng đã đóng băng
	case !inPlace:
		return target // toSpliced đọc lỗ ra undefined: mảng mới liền
	}
	if this.HasHoles() {
		removed.WithHoles() // thứ splice gỡ ra giữ lỗ, như JavaScript
	}
	return removed
}

// arrayWith là a.with(i, x): mảng mới, chỗ i (âm đếm từ cuối) là x; i ngoài mảng là RangeError.
func arrayWith(run Run, this value.Value, arguments []value.Value) value.Value {
	n, err := toInteger(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	length := float64(this.Length())
	if n < 0 {
		n += length
	}
	if n < 0 || n >= length {
		return rangeError("invalid index: %s", numberText(n))
	}
	out := value.ArrayOf(this.Items())
	out.SetIndex(int(n), argument(arguments, 1))
	return out
}

// arraySort là a.sort(compare) (inPlace) và a.toSorted: ổn định; undefined luôn ở cuối, không đưa vào compare; không
// có compare thì so chữ của từng phần tử theo đơn vị. Kết quả của compare là NaN thì coi như 0. Lỗi (của compare,
// hay của toString riêng một phần tử) để mảng như cũ.
func arraySort(run Run, this value.Value, arguments []value.Value, inPlace bool) value.Value {
	compare := argument(arguments, 0)
	if !compare.IsUndefined() && !compare.IsFunction() {
		return typeError("the comparison function must be either a function or undefined")
	}
	items := this.Items()
	if len(items) < 2 {
		// không so gì, không ghi gì, như V8: [x].sort() không gọi toString của x, và không lỗi trên mảng đã đóng băng
		if inPlace {
			return this
		}
		return value.ArrayOf(items)
	}
	var defined, undefined []value.Value
	for _, item := range items {
		if item.IsUndefined() {
			undefined = append(undefined, item)
		} else {
			defined = append(defined, item)
		}
	}
	var failure value.Value
	if compare.IsUndefined() {
		type keyed struct {
			key  string
			item value.Value
		}
		keys := make([]keyed, len(defined))
		for i, item := range defined {
			key, err := toText(run, item)
			if err != nil {
				return value.Error(err)
			}
			keys[i] = keyed{key, item}
		}
		slices.SortStableFunc(keys, func(a, b keyed) int { return compareText(a.key, b.key) })
		for i := range keys {
			defined[i] = keys[i].item
		}
	} else {
		buffer := make([]value.Value, 2)
		slices.SortStableFunc(defined, func(a, b value.Value) int {
			if failure.IsInvalid() {
				return 0
			}
			result := call(run, compare, callArguments(buffer, a, b)...)
			if result.IsInvalid() {
				failure = result
				return 0
			}
			n, err := toNumber(run, result)
			switch {
			case err != nil:
				failure = value.Error(err)
			case n < 0:
				return -1
			case n > 0:
				return 1
			}
			return 0
		})
	}
	if failure.IsInvalid() {
		return failure
	}
	sorted := append(defined, undefined...)
	if !inPlace {
		return value.ArrayOf(sorted)
	}
	if written := this.Splice(0, len(items), sorted...); written.IsInvalid() {
		return written // mảng đã đóng băng: JavaScript ném dù thứ tự không đổi
	}
	return this
}
