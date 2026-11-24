package runtime

import (
	"fmt"
	"unsafe"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Bộ đếm byte của một lượt chạy (23/11/2026; bài học của engine2, core/runtime/memory.go, 12–14/11). Năng lượng đếm
// lệnh, mà một lệnh làm ra được một gigabyte: bốn mươi lần a.push("y".repeat(10_000_000)) giữ 400 MB với 0,13% năng
// lượng của một request. Go không nói khi nào một giá trị được thả, nên lượt chạy được đếm bằng cái nó làm ra:
//   - lệnh: chữ của + và của template, mảng và object (MAKE, cả [...a] và { ...a }), closure, ô của biến bị bắt, khoá
//     hay phần tử một phép ghi thêm vào;
//   - hàm có sẵn: kết quả như nó tự giữ trừ khi là một phần của cái nó được đưa, và cái nó thêm vào this, đối số đầu
//     (core/javascript, meter.go); hàm làm ra nhiều từ ít hỏi chỗ trước khi cấp phát (Room).
//
// Quá trần thì lượt chạy được đếm lại bằng cái nó còn với tới (holding): phần lớn cái nó làm ra đã thả (s += piece làm
// ra mọi chuỗi ngắn hơn chuỗi cuối), nên đếm cộng dồn sẽ dừng một lượt chạy giữ rất ít. Giữ quá nửa trần là hết chỗ:
// giữa hai lần đếm lượt chạy làm ra ít nhất nửa trần, nên một lần đếm, tốn không hơn cái nó đếm, tốn không hơn cái làm
// ra từ lần trước. Vậy trần là đỉnh: cái lượt chạy giữ (không quá nửa) cộng cái nó làm ra từ lần đếm trước.
//
// Mỗi lần đếm lại, cái làm ra từ lần trước tốn năng lượng, một đơn vị mỗi madeBytesPerUnit byte (spendMade): một vòng
// làm ra chuỗi lớn rồi thả được bộ đếm cho đi vì giữ ít, nhưng mỗi byte nó chép tốn năng lượng, nên s += "x" trăm nghìn
// lần dừng ở ENERGY_LIMIT, không chạy vài giây. Lượt chạy không bao giờ quá trần thì không tốn gì thêm.
//
// Quá trần là MEMORY_LIMIT, như ENERGY_LIMIT: lượt chạy dừng ở lệnh ấy, và mọi lệnh sau đó cũng dừng (stop).
// Không đếm: cái engine làm cho chính nó (khung, ngăn xếp), cái host đưa vào (đối số của Call).

// DefaultMemory là trần byte của một lượt chạy khi Options không nói, như một request của engine2.
const DefaultMemory = 64 << 20

// madeBytesPerUnit là số byte làm ra tốn một đơn vị năng lượng khi lượt chạy được đếm lại (engine2: 32).
const madeBytesPerUnit = 32

// closureBytes là một closure lượt chạy làm ra (chưa kể các ô nó bắt, mỗi ô một con trỏ); cellBytes là một ô của biến
// bị bắt.
const (
	closureBytes = uint64(unsafe.Sizeof(Closure{})) + value.MadeBytes
	cellBytes    = uint64(unsafe.Sizeof(cell{})) + 16
)

// charge đếm n byte lượt chạy vừa làm ra (chưa nằm ở chỗ holding thấy); false khi quá trần, lượt chạy đã dừng.
func (m *Machine) charge(n uint64) bool {
	if m.made += n; m.made <= m.memory {
		return true
	}
	return m.chargeAfterCount(n)
}

// chargeAfterCount là charge khi quá trần: đếm lại bằng cái còn giữ, rồi n trên đó. Để ngoài, cho charge inline được.
//
//go:noinline
func (m *Machine) chargeAfterCount(n uint64) bool {
	if !m.recount(n) {
		return false
	}
	m.made += n
	return true
}

// recount đếm lại lượt chạy bằng cái nó còn với tới; false (và lượt chạy dừng) khi nó giữ quá nửa trần, hay cái giữ
// cộng extra (cái sắp làm ra, hay vừa làm ra mà holding chưa thấy) quá trần.
func (m *Machine) recount(extra uint64) bool {
	if m.stopped.IsInvalid() {
		return false
	}
	m.spendMade()
	// các chỗ nhớ chuỗi là cache: giữ lại thì giữ cả chuỗi chương trình đã bỏ (một vòng làm chuỗi 3 MB rồi đọc .length
	// sẽ giữ bốn cái), đếm chúng thì dừng oan một lượt chạy chẳng giữ gì; nên quên chúng, cái nào cần sẽ nhớ lại
	m.memos, m.nextMemo = [len(m.memos)]javascript.TextMemo{}, 0
	half := m.memory / 2
	held := m.holding(half)
	m.made, m.counted = held, held
	if held > half || extra > m.memory-held {
		m.stop()
		return false
	}
	return true
}

// spendMade tính năng lượng cho cái làm ra từ lần đếm trước: lệnh sau sẽ dừng ở ENERGY_LIMIT nếu hết.
func (m *Machine) spendMade() {
	if m.made > m.counted {
		m.energy -= int64((m.made - m.counted) / madeBytesPerUnit)
	}
}

// stop là MEMORY_LIMIT: lỗi ghi lại, và năng lượng về 0 để lệnh sau cũng dừng dù một hàm có sẵn đã nuốt lỗi.
func (m *Machine) stop() {
	m.stopped = value.Fail(value.MemoryCode, "the run used all its memory (%s)", size(m.memory))
	m.energy = 0
}

// holding là cái lượt chạy còn với tới, đếm tới limit: ngăn xếp (slot của mọi khung, closure và các ô của mỗi khung
// nằm ngay sau slot của nó, phép tính đang dở, this và đối số của hàm có sẵn đang chạy), và cái code Go của engine
// đang giữ cho lượt chạy (kept). Thứ dùng chung từ lúc boot không đếm (value.Holding).
func (m *Machine) holding(limit uint64) uint64 {
	h := value.Holding{Limit: limit}
	for _, v := range m.stack {
		h.Add(v)
	}
	for _, kept := range m.kept {
		if kept.Items != nil {
			h.Bytes(uint64(len(*kept.Items)) * value.ItemBytes)
			for _, v := range *kept.Items {
				h.Add(v)
			}
		}
		if kept.Bytes != nil {
			h.Bytes(uint64(*kept.Bytes))
		}
	}
	h.Walk()
	return h.Total
}

// Hold là value.Holder: một ô trên ngăn xếp của khung khai nó (execute).
func (c *cell) Hold(h *value.Holding) { holdCell(h, c) }

// holdCell đếm một ô (một lần, dù nhiều closure giữ nó) và giá trị của nó.
func holdCell(h *value.Holding, c *cell) {
	if c != nil && h.First(unsafe.Pointer(c)) {
		h.Bytes(cellBytes)
		h.Add(c.value)
	}
}

// Hold là value.Holder: closure đếm chính nó một lần và các ô nó bắt; closure dùng chung từ lúc boot không đếm.
func (c *Closure) Hold(h *value.Holding) {
	if c.shared || !h.First(unsafe.Pointer(c)) {
		return
	}
	h.Bytes(closureBytes + uint64(len(c.cells))*8)
	for _, held := range c.cells {
		holdCell(h, held)
	}
}

// Room là javascript.Meter: bytes nữa còn vừa trần không, đếm lại bằng cái còn giữ khi cần; không đếm gì.
func (m *Machine) Room(bytes uint64) value.Value {
	if !m.stopped.IsInvalid() && bytes <= m.memory-min(m.made, m.memory) {
		return value.Value{}
	}
	if m.recount(bytes) {
		return value.Value{}
	}
	return m.stopped
}

// Made là javascript.Meter: đếm bytes một hàm có sẵn làm ra.
func (m *Machine) Made(bytes uint64) value.Value {
	if m.charge(bytes) {
		return value.Value{}
	}
	return m.stopped
}

// Keep là javascript.Meter: cái code Go đang giữ cho lượt chạy, tới LetGo.
func (m *Machine) Keep(kept javascript.Kept) int {
	m.kept = append(m.kept, kept)
	return len(m.kept) - 1
}

// LetGo là javascript.Meter: quên cái giữ từ mark trở đi.
func (m *Machine) LetGo(mark int) {
	if mark < len(m.kept) {
		clear(m.kept[mark:])
		m.kept = m.kept[:mark]
	}
}

// Memory là trần byte của lượt chạy.
func (m *Machine) Memory() uint64 { return m.memory }

// size viết một số byte như cách người viết viết memory: "64mb", "1gb", "512kb".
func size(n uint64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%dgb", n>>30)
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%dmb", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%dkb", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
