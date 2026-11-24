package value

import "unsafe"

// Số byte một giá trị giữ, như bộ đếm byte của runtime đếm (core/runtime/memory.go, 23/11/2026). Go không nói khi nào
// một giá trị được thả, nên một lượt chạy được đếm bằng cái nó làm ra; quá trần thì đếm lại bằng cái nó còn với tới
// (Holding). Các con số là cái Go cấp cho mỗi phần, gần đúng: một chuỗi là các byte của nó, một mảng một Value mỗi
// phần tử, một object một khoá (string header) và một Value mỗi khoá, mỗi thứ được làm ra thêm phần đầu của nó.
const (
	// ItemBytes là một phần tử của mảng: một Value.
	ItemBytes = uint64(unsafe.Sizeof(Value{}))
	// KeyBytes là một khoá của object: string header của khoá, Value của nó, và phần của nó trong chỉ mục (object.go).
	KeyBytes = uint64(unsafe.Sizeof("")) + ItemBytes + 16
	// MadeBytes là một object, một mảng hay một hàm được làm ra: phần đầu của nó và con trỏ tới nó.
	MadeBytes = 64
)

// Count là số phần tử của mảng v, số khoá của object v; 0 với loại khác. Chỉ đọc độ dài, không chép.
func (v Value) Count() int {
	switch data := v.data.(type) {
	case *list:
		return len(data.items)
	case *properties:
		return len(data.keys)
	}
	return 0
}

// Shallow là cái v tự giữ, không kể cái các phần tử và khoá của nó giữ: độ dài của chuỗi hay bytes, phần đầu và các
// phần tử của mảng, phần đầu và các khoá của object, phần đầu của hàm hay struct; 0 với số, boolean, null và các loại
// Value giữ tại chỗ.
func (v Value) Shallow() uint64 {
	switch v.kind {
	case text:
		return v.number // độ dài chuỗi
	case bytes:
		return uint64(len(v.data.([]byte)))
	case array:
		return MadeBytes + uint64(v.Count())*ItemBytes
	case object:
		if o := v.properties(); o.borrowed != nil { // khoá mượn của một Shape: chỉ các giá trị là của nó
			return MadeBytes + uint64(len(o.items))*ItemBytes
		}
		return MadeBytes + uint64(v.Count())*KeyBytes
	case function, structure:
		return MadeBytes
	}
	return 0
}

// SharesWith là v là chính of, hay (chuỗi, bytes) nằm trong bộ nhớ của of: s.slice(1) của Go là một khúc của s, không
// làm ra byte nào. Runtime đọc nó để không đếm kết quả một lời gọi trả lại một phần của cái nó được đưa.
func (v Value) SharesWith(of Value) bool {
	switch v.kind {
	case text:
		if of.kind != text {
			return false
		}
		s, whole := v.text(), of.text()
		return len(s) > 0 && inside(unsafe.Pointer(unsafe.StringData(s)), len(s), unsafe.Pointer(unsafe.StringData(whole)), len(whole))
	case bytes:
		if of.kind != bytes {
			return false
		}
		b, whole := v.data.([]byte), of.data.([]byte)
		return len(b) > 0 && inside(unsafe.Pointer(unsafe.SliceData(b)), len(b), unsafe.Pointer(unsafe.SliceData(whole)), len(whole))
	case object, array, function, structure:
		return v.kind == of.kind && v.data == of.data
	}
	return false
}

// inside là khúc [p, p+n) nằm trong [whole, whole+size).
func inside(p unsafe.Pointer, n int, whole unsafe.Pointer, size int) bool {
	start, end := uintptr(whole), uintptr(whole)+uintptr(size)
	return uintptr(p) >= start && uintptr(p)+uintptr(n) <= end
}

// Holder là một hàm hay một struct còn giữ giá trị bên trong (closure của runtime giữ các biến nó bắt): Holding đi vào
// nó qua Hold. Nó tự đếm một lần (h.First với địa chỉ của nó, rồi h.Bytes phần đầu của nó) và đưa các giá trị nó giữ
// bằng h.Add (h.First cho mỗi ô, để một ô hai closure cùng giữ chỉ đếm một lần).
type Holder interface {
	Hold(h *Holding)
}

// dedupeText là độ dài từ đó một chuỗi được đếm một lần dù nhiều chỗ giữ nó (theo địa chỉ byte của nó): một mảng một
// nghìn phần tử cùng là một chuỗi 1 MB giữ 1 MB, không phải 1 GB. Chuỗi ngắn hơn đếm mỗi lần gặp: thừa ít, nhanh hơn.
const dedupeText = 1024

// Holding đếm cái một lượt chạy còn với tới, tới Limit: runtime thêm các gốc (ngăn xếp, các ô của các khung, cái code
// Go của engine đang giữ cho lượt chạy), Holding đi sâu vào object, mảng, closure. Mỗi object, mảng, closure, chuỗi dài
// đếm một lần (một vòng dừng ở đó); thứ dùng chung từ lúc boot (FreezeShared) là của mọi lượt chạy, không đếm. Đi bằng
// danh sách chờ, không đệ quy; dừng khi Total quá Limit, nên một lần đi tốn không hơn cái nó đếm.
type Holding struct {
	Limit   uint64
	Total   uint64
	seen    map[unsafe.Pointer]struct{}
	pending []Value
}

// Past là đã đếm quá Limit.
func (h *Holding) Past() bool { return h.Total > h.Limit }

// First là lần đầu Holding gặp p: true một lần cho mỗi địa chỉ. Một Holder hỏi nó trước khi đưa cái một ô giữ.
func (h *Holding) First(p unsafe.Pointer) bool {
	if h.seen == nil {
		h.seen = map[unsafe.Pointer]struct{}{}
	}
	if _, found := h.seen[p]; found {
		return false
	}
	h.seen[p] = struct{}{}
	return true
}

// Bytes thêm n byte không phải một Value: cái code Go đang viết cho lượt chạy, một ô của closure.
func (h *Holding) Bytes(n uint64) { h.Total += n }

// Add xếp v và mọi thứ trong nó để Walk đếm.
func (h *Holding) Add(v Value) {
	if v.kind >= text {
		h.pending = append(h.pending, v)
	}
}

// Walk đi hết các giá trị đã Add, tới khi hết hay quá Limit.
func (h *Holding) Walk() {
	for len(h.pending) > 0 && !h.Past() {
		v := h.pending[len(h.pending)-1]
		h.pending = h.pending[:len(h.pending)-1]
		switch data := v.data.(type) {
		case *byte: // chuỗi: byte đầu, độ dài ở number
			if v.number >= dedupeText && !h.First(unsafe.Pointer(data)) {
				continue
			}
			h.Total += v.number
		case []byte:
			h.Total += uint64(len(data))
		case *list:
			if data.shared || !h.First(unsafe.Pointer(data)) {
				continue
			}
			h.Total += MadeBytes + uint64(len(data.items))*ItemBytes
			for _, item := range data.items {
				h.Add(item)
			}
		case *properties:
			if data.shared || !h.First(unsafe.Pointer(data)) {
				continue
			}
			if s := data.borrowed; s != nil { // khoá mượn của một Shape: chỉ các giá trị là của object này
				h.Total += MadeBytes + uint64(len(data.items))*ItemBytes
				// khoá của riêng một object lượt chạy làm ra, thành Shape ở { ...o }: vẫn là của lượt chạy, đếm một lần
				// dù bao nhiêu object mượn; khoá của một hằng hay của dữ liệu dùng chung thì không
				if s.held && h.First(unsafe.Pointer(s)) {
					h.Total += MadeBytes + uint64(len(s.keys))*(KeyBytes-ItemBytes)
					for _, key := range s.keys {
						h.Add(String(key))
					}
				}
			} else {
				h.Total += MadeBytes + uint64(len(data.keys))*KeyBytes
				for _, key := range data.keys { // chữ của khoá: một khoá tính ra lúc chạy dài bao nhiêu cũng được
					h.Add(String(key))
				}
			}
			for _, item := range data.items {
				h.Add(item)
			}
		default:
			if v.kind != function && v.kind != structure {
				continue
			}
			if holder, ok := data.(Holder); ok {
				holder.Hold(h)
				continue
			}
			h.Total += MadeBytes // hàm có sẵn, struct của engine: không đi vào (lát sau, khi request có context)
		}
	}
}
