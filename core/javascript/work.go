package javascript

import (
	"math/bits"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Năng lượng cho việc của engine (23/11/2026; bài học của engine2, core/value/work.go, 15/11). Năng lượng đếm lệnh, mà
// một lệnh gọi một hàm có sẵn trên một chuỗi hay một mảng lớn làm việc hàng mili giây: s.endsWith("x") trên 1 MB
// tiếng Việt 6 ms, một sort trăm nghìn số 50 ms, mỗi cái một đơn vị. Việc ấy giờ tốn năng lượng của lượt chạy, tính từ
// kích thước, không bao giờ từ đồng hồ: một lượt chạy tốn như nhau trên mọi máy, lúc máy rảnh hay bận.
//
// Một đơn vị là một lệnh thường, 3,3 ns trên máy đo (vòng cộng, 23/11); mỗi loại việc một tỉ lệ, đo trên cùng máy
// (bench: test/runtime/work_test.go). Một tỉ lệ cho một loại việc mà các ca khác nhau, nên một lời gọi tốn đúng việc
// nó làm trong vài lần hơn kém: chuỗi ASCII đọc tám byte một bước, chuỗi tiếng Việt từng ký tự, nên hai tỉ lệ.
// Việc nhỏ (chuỗi dưới smallText byte) là giá của lệnh, không tính thêm, không kiểm gì.
const (
	// smallText là độ dài từ đó việc đọc một chuỗi được tính: dưới nó, đọc tốn ít hơn vài đơn vị.
	smallText = 256
	// asciiBytesPerUnit là số byte ASCII một phương thức đọc một đơn vị: isASCII, tìm, chép (0,07–0,25 ns/B).
	asciiBytesPerUnit = 16
	// textBytesPerUnit là số byte của một chuỗi có chữ ngoài ASCII đọc một đơn vị: đi từng ký tự (1,0–3,1 ns/B;
	// endsWith 6).
	textBytesPerUnit = 1
	// equalBytesPerUnit là số byte hai chuỗi cùng độ dài so bằng nhau (===), hay một chuỗi ASCII đếm độ dài, một
	// đơn vị (0,03–0,07 ns/B).
	equalBytesPerUnit = 64
	// unitsBytesPerUnit là số byte một chuỗi có chữ ngoài ASCII đếm độ dài hay đổi thành đơn vị UTF-16 (s.length,
	// s[i], <: 1,5–3,9 ns/B).
	unitsBytesPerUnit = 2
	// numberBytesPerUnit là số byte một chuỗi đổi thành số (Number, -, so với số: 2,1 ns/B).
	numberBytesPerUnit = 2
	// uriBytesPerUnit: encodeURIComponent và các hàm cùng họ (2,7 ns/B).
	uriBytesPerUnit = 1

	// itemUnits là một phần tử so (indexOf, includes), dời hay ghi (fill, splice, shift): 1–4 ns; reverseItemUnits
	// một phần tử reverse đổi chỗ (12 ns).
	itemUnits        = 1
	reverseItemUnits = 4
	// copyItemUnits là một phần tử một phương thức chép sang mảng mới (slice, concat, with: 9–13 ns cả cấp phát và quét
	// của GC); bộ đếm byte chỉ tính năng lượng cho cái làm ra khi quá trần, và ít hơn thế (madeBytesPerUnit).
	copyItemUnits = 3
	// joinItemUnits là một phần tử join viết (26–35 ns), flatItemUnits một phần tử flat viết (38 ns), fromItemUnits một
	// phần tử Array.from đọc (8–50 ns: đọc khoá "0", "1"… của một object giống mảng là chậm nhất).
	joinItemUnits = 8
	flatItemUnits = 11
	fromItemUnits = 10
	// pieceUnits là một mảnh split làm ra (một chỗ của nó: 10–40 ns với split(""), theo GC của mảng; chuỗi không cấp phát),
	// matchUnits một chỗ khớp replace thay (30–150 ns, theo GC của mảng chỗ khớp): việc của chúng theo số mảnh, số chỗ
	// khớp, không theo số byte đọc.
	pieceUnits = 4
	matchUnits = 15
	// sortUnits là một phép so của thứ tự mặc định (đổi thành chữ rồi so: 20–40 ns), tính trước khi sort bắt đầu.
	sortUnits = 10
	// callbackUnits là một lời gọi hàm của chương trình từ code của engine (map, sort, replace…): ≈90 ns ngoài các
	// lệnh của chính hàm ấy, mà lệnh CALL của VM thì gọi bằng giá của một lệnh.
	callbackUnits = 27
	// keyUnits là một khoá Object.keys, values, entries đọc (20–40 ns); writeKeyUnits một khoá Object.assign,
	// fromEntries, { ...o } ghi (≈165 ns cả đọc); copyKeyUnits một khoá { ...o } hay Object.assign({}, o) chép vào
	// một object chưa có khoá, mượn khoá của o (chỉ chép giá trị).
	keyUnits      = 10
	pairUnits     = 25 // một cặp [khoá, giá trị] Object.entries làm ra (một mảng hai phần tử: ≈80 ns cả quét của GC)
	writeKeyUnits = 25
	copyKeyUnits  = 2
	// jsonReadUnits là một byte JSON.parse đọc: một mảng số 9 ns/B, các dòng cùng khoá 3 ns/B từ khi chúng mượn chung
	// khoá (24/11/2026; trước đó 7,5, và giá là 4); jsonWriteUnits một byte JSON.stringify viết (6–8 ns).
	jsonReadUnits  = 2
	jsonWriteUnits = 2
)

// spend trừ units năng lượng của run cho việc code Go của engine làm; lỗi ENERGY_LIMIT khi hết. Không làm gì khi run
// không đếm hay units là 0.
func spend(run Run, units uint64) value.Value {
	if units == 0 {
		return value.Value{}
	}
	if m, ok := run.(Meter); ok {
		return m.Spend(units)
	}
	return value.Value{}
}

// textReading là units năng lượng đọc n byte của một chuỗi, ascii hay không.
func textReading(n int, ascii bool) uint64 {
	if ascii {
		return uint64(n) / asciiBytesPerUnit
	}
	return uint64(n) / textBytesPerUnit
}

// makesOnly là các phương thức của chuỗi chỉ chép cái chúng làm ra: tính theo chữ làm ra, như một chuỗi ASCII đọc
// (chép, rồi tìm nửa cặp để ghép: 0,5 ns/B), không theo chữ được đưa.
var makesOnly = map[string]bool{"concat": true, "repeat": true, "padStart": true, "padEnd": true, "toString": true, "valueOf": true}

// walksItself là các phương thức của chuỗi đi bằng con trỏ (cursor.go) và tự tính phần chúng đã đi.
var walksItself = map[string]bool{
	"slice": true, "substring": true, "startsWith": true, "endsWith": true,
	"at": true, "charAt": true, "charCodeAt": true, "codePointAt": true,
}

// slowASCII là số byte ASCII một đơn vị của các phương thức đi chậm cả với ASCII (đi từng ký tự): toUpperCase 1,65
// ns/B, lastIndexOf 2,5.
var slowASCII = map[string]int{"toUpperCase": 2, "toLowerCase": 2, "lastIndexOf": 1}

// textMethodWork là units một phương thức của chuỗi s làm: đọc cả s và các chuỗi nó được đưa (một chuỗi để tìm, một
// dấu tách), cộng từng mảnh split làm ra; makesOnly thì theo chữ làm ra (result). Chuỗi dài hơn smallText mới tính, và
// mới hỏi s có phải ASCII không (một lần quét nữa, chỉ với ASCII: chuỗi có chữ khác dừng ở chữ đầu tiên ấy). Chỗ khớp
// của replace tính ở stringReplace, nơi biết chúng.
func textMethodWork(name, s string, arguments []value.Value, result value.Value) uint64 {
	n := len(s)
	for _, argument := range arguments {
		if argument.IsString() {
			n += len(argument.String())
		}
	}
	made := 0
	if result.IsString() {
		made = len(result.String())
	}
	pieces := uint64(0)
	if name == "split" {
		pieces = uint64(result.Length()) * pieceUnits
	}
	if n < smallText && made < smallText { // phần lớn lời gọi: chuỗi ngắn, không tra bảng nào
		return pieces
	}
	if walksItself[name] {
		return 0
	}
	if makesOnly[name] {
		return uint64(made) / asciiBytesPerUnit
	}
	if n < smallText {
		return pieces
	}
	ascii := isASCII(s)
	if rate, slow := slowASCII[name]; slow && ascii {
		return pieces + uint64(n/rate)
	}
	return pieces + textReading(n, ascii)
}

// arrayMethodBefore là units một phương thức của mảng a tốn trước khi nó bắt đầu: phép so của sort theo thứ tự mặc
// định, n·⌈log₂ n⌉ (sort với một hàm so thì mỗi lần gọi hàm ấy tốn của nó: callbackUnits).
func arrayMethodBefore(name string, a value.Value, arguments []value.Value) uint64 {
	if (name != "sort" && name != "toSorted") || argument(arguments, 0).IsFunction() {
		return 0
	}
	n := uint64(a.Length())
	if n < 2 {
		return 0
	}
	return n * uint64(bits.Len64(n)) * sortUnits
}

// arrayMethodAfter là units một phương thức của mảng a đã làm, đọc từ a (before là số phần tử trước lời gọi) và từ
// kết quả: các phần tử đã so tới chỗ tìm thấy, các phần tử dời hay ghi, các phần tử join hay flat viết.
func arrayMethodAfter(name string, a value.Value, arguments []value.Value, result value.Value, before int) uint64 {
	n := uint64(max(before, a.Length()))
	switch name {
	case "indexOf", "includes", "lastIndexOf":
		compared := n
		if at := result.Number(); result.IsNumber() && at >= 0 && at < float64(n) {
			if name == "lastIndexOf" {
				compared = n - uint64(at)
			} else {
				compared = uint64(at) + 1
			}
		}
		if needle := argument(arguments, 0); needle.IsString() && len(needle.String()) >= smallText {
			compared += compared * uint64(len(needle.String())) / equalBytesPerUnit
		}
		return compared * itemUnits
	case "reverse", "toReversed":
		return n * reverseItemUnits
	case "fill", "copyWithin", "splice", "shift", "unshift":
		return n * itemUnits
	case "slice", "concat", "with", "toSpliced":
		return uint64(result.Length()) * copyItemUnits
	case "join":
		return n * joinItemUnits
	case "flat":
		return uint64(result.Length()) * flatItemUnits
	}
	return 0
}

// Read là Get của một lượt chạy: như Get, và đọc một chuỗi dài (độ dài của nó, một chỉ số của nó) đi bằng con trỏ
// mà lượt chạy nhớ (cursor.go), tốn năng lượng theo phần đã đi. Lỗi ENERGY_LIMIT khi hết.
func Read(run Run, v value.Value, key string) value.Value {
	if !v.IsString() || len(v.String()) < smallText {
		return Get(v, key)
	}
	var local TextMemo
	t := textOf(run, v.String(), &local) // một lượt chạy nhớ nó: lần sau O(1), s[i] lần lượt đi tiếp (cursor.go)
	var result value.Value
	if key == "length" {
		result = value.Number(float64(t.Length()))
	} else if i, ok := arrayIndex(key); ok {
		result = value.Undefined()
		if unit, ok := t.unitAt(i); ok {
			result = value.String(unitString(unit))
		}
	} else {
		return Get(v, key)
	}
	if failure := spend(run, t.take()); failure.IsInvalid() {
		return failure
	}
	return result
}

// ConcatWork là units năng lượng của một phép nối làm ra n byte (+, template của hai chuỗi): chép rồi tìm nửa cặp để
// ghép, như makesOnly; 0 dưới smallText.
func ConcatWork(n int) uint64 {
	if n < smallText {
		return 0
	}
	return uint64(n) / asciiBytesPerUnit
}

// EqualWork là units năng lượng của a === b khi cả hai là chuỗi cùng độ dài từ smallText (so từng byte); 0 khác.
func EqualWork(a, b value.Value) uint64 {
	if !a.IsString() || !b.IsString() {
		return 0
	}
	if n := len(a.String()); n >= smallText && n == len(b.String()) {
		return uint64(n) / equalBytesPerUnit
	}
	return 0
}
