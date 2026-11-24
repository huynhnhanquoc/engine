// Package value là giá trị của engine3, và chỉ là dữ liệu: không VM, không năng lượng, không phương thức của
// JavaScript. Phương thức JS (toLowerCase, map…), phép so sánh và phép chuyển đổi là việc của runtime. Ở đây chỉ có:
// tạo (make.go), hỏi loại (is.go, kind.go), đọc ra Go (read.go), object (object.go), mảng (array.go), và đọc địa chỉ
// lắng nghe hay kích thước bộ nhớ (helper.go). Lời báo lỗi viết tiếng Anh.
package value

// Value là một giá trị: 32 byte, ba trường, như engine2 (đã đo 25/10/2026: nhanh hơn generics). Value{} là undefined.
// Chép một Value là chép cả ba trường: số và chuỗi là của riêng bản chép (không phép nào sửa chúng tại chỗ), object và
// mảng nằm sau con trỏ nên mọi bản chép cùng thấy một chỗ.
type Value struct {
	kind kind
	// number: bit của float64 · boolean: 0 hoặc 1 · datetime: ns từ 1970 UTC · duration: ns · text: độ dài chuỗi
	number uint64
	// text: *byte, byte đầu của chuỗi (đọc bằng text()) · bytes: []byte · object: *properties · array: *list ·
	// structure: con trỏ tới struct của engine · function: thứ runtime chạy · invalid: error. Chuỗi giữ con trỏ, không
	// giữ string (23/11/2026): một con trỏ trong interface không cấp phát, một string thì cấp 16 byte mỗi chuỗi, một
	// phần tư số lần cấp phát của một chương trình làm ra chữ.
	data any
}
