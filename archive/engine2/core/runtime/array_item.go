package runtime

import (
	"math"

	"github.com/kitwork/engine2/core/value"
)

// MaxArrayGap is the most holes one a[i] = v adds between an array's last item and item i (about 32 MB
// of items). Past it the write is the INVALID_ARGUMENT failure, as "x".repeat(n) is past 8 MB: a sparse
// key belongs in an object.
const MaxArrayGap = 1 << 20

// setArrayItem is a[key] = item on an array, as JavaScript sets an array's item (14/10/2026; until then a
// key that was not an index from 0 to the last appended item, whatever it was):
//
//   - a whole number from 0 to the last index replaces that item, and the length appends one (an array
//     literal fills itself this way);
//   - a whole number past the length pads the gap with undefined, what a hole of JavaScript reads (until
//     04/11/2026 null, so typeof a[2] was "object"): [1, 2] with a[4] = 5 is [1, 2, undefined,
//     undefined, 5]. Each hole costs one unit of energy, and more than MaxArrayGap of them is the
//     INVALID_ARGUMENT failure;
//   - on a frozen array any key is the TYPE failure value words for it, before the key is looked at, as
//     JavaScript refuses a[-1], a.x and a.length on a frozen array as it refuses a write to a frozen object
//     (05/11/2026: a key that is no index gave the message below);
//   - any other key (a negative or fractional number, NaN, Infinity, a string, "2" included, a boolean,
//     null, and length: a.length = n) is the UNSUPPORTED failure, however far past the end: JavaScript
//     keeps such a value as a property of the array object, and an array of Kitwork has none (a read of
//     a.x or a[1.5] gives undefined, GET), so the value would be lost.
//
// It returns a failure (an Invalid value) or null. The message never shows the key or the value: either
// may be a secret a request carried.
func (vm *VM) setArrayItem(target value.Value, key, item value.Value, ip int) value.Value {
	length := float64(len(target.Items()))
	index := key.Number()
	if target.IsFrozen() {
		// a frozen array (Object.freeze) refuses the write with the failure value words for it, before a
		// gap is counted or padded (04/11/2026): an index through SetIndex, an item it has or one past its
		// end, any other key as its text, a[-1] and a.x as JavaScript refuses them (05/11/2026)
		if key.IsNumber() && index >= 0 && index <= math.MaxInt32 && math.Trunc(index) == index {
			return target.SetIndex(int(index), item)
		}
		name, failure := propertyKey(key)
		if failure.IsInvalid() {
			return failure
		}
		return target.Set(name, item)
	}
	switch {
	case !key.IsNumber() || index != index || index < 0 || math.IsInf(index, 1) || math.Trunc(index) != index:
		// not an index, however large: 1048578.5 and Infinity too, which past the gap limit were the gap's
		// failure (15/10/2026)
		return notAnArrayIndex(key)
	case index-length > MaxArrayGap:
		return value.Fail(value.InvalidArgumentCode,
			"chỉ số cách cuối mảng hơn 1048576 phần tử, mảng sẽ phải đệm ngần ấy chỗ trống: cần khoá thưa như vậy thì dùng object { }")
	case index < length:
		// Items is the array's backing slice, so this replaces the item in the array itself; not SetIndex,
		// which refuses a failure as the item where the old engine stored whatever came (as Set on an
		// object still does)
		target.Items()[int(index)] = item
	case index == length:
		target.Push(item)
	default:
		gap := int(index) - len(target.Items())
		if !vm.consumeEnergy(Cost(gap)) {
			return vm.diagnosticValue(DiagnosticEnergyLimit, "Energy Limit Exceeded: Execution halted", ip)
		}
		// the holes stop before they are made when they would pass what the run may make (memory.go); SET counts
		// them once made
		if !vm.room(uint64(gap+1) * value.ItemBytes) {
			return vm.memoryLimit(ip)
		}
		for ; gap > 0; gap-- {
			target.Push(value.Undefined())
		}
		target.Push(item)
	}
	return value.Null()
}

// notAnArrayIndex is the failure of a key an array has no item for, naming its kind, never its value.
func notAnArrayIndex(key value.Value) value.Value {
	what := key.Kind()
	n := key.Number()
	switch {
	case !key.IsNumber():
	case n != n:
		what = "NaN"
	case n < 0:
		what = "số âm"
	case math.IsInf(n, 1):
		what = "vô cực"
	default:
		what = "số không nguyên"
	}
	return value.Fail(value.UnsupportedCode,
		"mảng chỉ nhận chỉ số là số nguyên từ 0 trở lên, ở đây là "+what+
			": mảng không có thuộc tính như object của JavaScript; cần khoá như vậy thì dùng object { }")
}
