package javascript

import "github.com/huynhnhanquoc/engine/core/value"

// Bộ đếm byte của lượt chạy, nhìn từ các hàm có sẵn (23/11/2026). Runtime đếm cái lệnh làm ra (chuỗi của +, mảng,
// object, closure); ở đây là cái một hàm có sẵn làm ra, ở hai chỗ mọi lời gọi đi qua (Native.Call, phương thức trong
// Invoke): kết quả như nó tự giữ (Shallow), trừ khi nó là một phần của cái lời gọi được đưa, và cái lời gọi thêm vào
// this hay đối số đầu (push, Object.assign). Ba việc chỉ hàm có sẵn làm được:
//   - hỏi chỗ trước khi cấp phát cái làm ra nhiều từ ít ("y".repeat(5e8), Array(1e7), split(""), join, JSON.stringify):
//     room, và grower cho một thứ lớn dần;
//   - đếm cái bên trong kết quả mà Shallow không thấy (cây của JSON.parse, các cặp của Object.entries): made;
//   - cho runtime thấy cái nó đang giữ trong khi chương trình nó gọi lại chạy (các phần tử map đã làm): keep.
// Một Run không phải Meter (test, Go gọi thẳng) thì không đếm gì.

// Meter là phần của Run đếm byte lượt chạy làm ra: runtime cài nó.
type Meter interface {
	// Room là bytes nữa còn vừa trần của lượt chạy, đếm lại bằng cái nó còn giữ khi cần; không đếm gì. Lỗi
	// MEMORY_LIMIT khi không vừa.
	Room(bytes uint64) value.Value
	// Made đếm bytes lượt chạy vừa làm ra; lỗi MEMORY_LIMIT khi quá trần.
	Made(bytes uint64) value.Value
	// Spend trừ units năng lượng cho việc code Go của engine làm cho lượt chạy (work.go); lỗi ENERGY_LIMIT khi hết.
	Spend(units uint64) value.Value
	// Keep cho runtime thấy cái code Go đang giữ cho lượt chạy tới LetGo với dấu nó trả.
	Keep(kept Kept) int
	LetGo(mark int)
}

// Kept là cái code Go đang giữ cho lượt chạy trong khi chương trình nó gọi lại chạy: các phần tử đã làm (đọc khi lớn
// dần), số byte của một chữ đang viết.
type Kept struct {
	Items *[]value.Value
	Bytes *int
}

// room hỏi run còn chỗ cho bytes không; undefined (không lỗi) khi run không đếm.
func room(run Run, bytes uint64) value.Value {
	if m, ok := run.(Meter); ok {
		return m.Room(bytes)
	}
	return value.Value{}
}

// made đếm bytes run vừa làm ra; undefined (không lỗi) khi run không đếm.
func made(run Run, bytes uint64) value.Value {
	if m, ok := run.(Meter); ok {
		return m.Made(bytes)
	}
	return value.Value{}
}

// keep, letGo là Keep, LetGo của run; không làm gì khi run không đếm.
func keep(run Run, kept Kept) int {
	if m, ok := run.(Meter); ok {
		return m.Keep(kept)
	}
	return 0
}

func letGo(run Run, mark int) {
	if m, ok := run.(Meter); ok {
		m.LetGo(mark)
	}
}

// grower hỏi run chỗ khi code Go dựng một thứ lớn dần mà lượt chạy sẽ giữ (một chữ, các phần tử): mỗi khi nó lớn quá
// lần hỏi trước một phần tư (ít nhất 64 KB), nên hỏi O(log n) lần và đi quá chỗ được cho không hơn một phần tư.
type grower struct {
	run  Run
	next uint64
}

// growStep là bước nhỏ nhất giữa hai lần hỏi.
const growStep = 64 << 10

// fits là size byte (cả thứ đang dựng) còn vừa; lỗi MEMORY_LIMIT khi không.
func (g *grower) fits(size uint64) value.Value {
	if size <= g.next {
		return value.Value{}
	}
	if failure := room(g.run, size); failure.IsInvalid() {
		return failure
	}
	g.next = size + max(size/4, growStep)
	return value.Value{}
}

// counted đếm cái một lời gọi hàm có sẵn làm ra (Native.Call, Invoke): kết quả như nó tự giữ, trừ khi nó là this hay
// một đối số hay một khúc của chúng; và cái lời gọi thêm vào this, đối số đầu (before là số phần tử, số khoá của chúng
// trước lời gọi). Kết quả, hay lỗi MEMORY_LIMIT khi quá trần.
func counted(meter Meter, result, this value.Value, arguments []value.Value, thisBefore, firstBefore int) value.Value {
	if result.IsInvalid() {
		return result
	}
	bytes := grownBy(this, thisBefore)
	if len(arguments) > 0 {
		bytes += grownBy(arguments[0], firstBefore)
	}
	if size := result.Shallow(); size > 0 && !sharedWith(result, this, arguments) {
		bytes += size
	}
	if bytes == 0 {
		return result
	}
	if failure := meter.Made(bytes); failure.IsInvalid() {
		return failure
	}
	return result
}

// sharedWith là result là this, một đối số, hay một khúc của chúng.
func sharedWith(result, this value.Value, arguments []value.Value) bool {
	if result.SharesWith(this) {
		return true
	}
	for _, argument := range arguments {
		if result.SharesWith(argument) {
			return true
		}
	}
	return false
}

// grownBy là số byte v lớn thêm từ khi có before phần tử hay khoá.
func grownBy(v value.Value, before int) uint64 {
	n := v.Count()
	if n <= before {
		return 0
	}
	if v.IsArray() {
		return uint64(n-before) * value.ItemBytes
	}
	return uint64(n-before) * value.KeyBytes
}
