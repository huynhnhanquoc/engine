package value

import "reflect"

// Đóng băng (Object.freeze): một object hay mảng đã đóng băng không thêm, sửa, gỡ khoá hay phần tử nào nữa: Set,
// Delete, Push, SetIndex, Splice của nó là lỗi, lời báo như TypeError của JavaScript. Đóng băng nông, như JavaScript:
// object, mảng bên trong vẫn mở. Không mở lại được. Dấu nằm cạnh các phần tử (object.go, array.go), nên mọi bản chép
// của Value thấy nó; một thứ đã đóng băng chỉ được đọc, nên mọi lượt chạy đọc nó cùng lúc được.
//
// Dữ liệu dùng chung (FreezeShared) là thứ engine đóng băng, không phải chương trình: phần cấp cao nhất của mỗi file một
// site chạy một lần lúc boot, rồi mọi request, của mọi người dùng, cùng lúc đọc thứ nó dựng (engine2/docs/language.md
// mục 1.7). Engine đóng băng sâu nó sau boot: một request ghi vào nó là lỗi nói vì sao và cách viết thay (ghi vào một
// bản chép), không phải lời của JavaScript, vì ở JavaScript nó mở; Object.isFrozen của nó cũng vậy (core/javascript).

// Freeze đóng băng v rồi trả v: object và mảng mang dấu từ đây; loại khác không có gì để ghi, trả nguyên. Đóng băng lại
// một thứ đã đóng băng không ghi gì: thứ ấy có thể đang được nhiều lượt chạy đọc cùng lúc, ghi lại dấu là một cuộc đua
// dữ liệu (engine2, 05/11/2026).
func (v Value) Freeze() Value {
	switch v.kind {
	case object:
		v.freezeObject()
	case array:
		v.freezeArray()
	}
	return v
}

// IsFrozen là v là object hay mảng đã đóng băng, bởi chương trình hay bởi engine.
func (v Value) IsFrozen() bool {
	switch v.kind {
	case object:
		return v.objectFrozen()
	case array:
		return v.arrayFrozen()
	}
	return false
}

// Sharer là thứ một hàm (Function) hay một struct (Struct) mang mà còn giữ giá trị bên trong: các biến một closure của
// runtime giữ, trạng thái của một struct của engine. FreezeShared gọi Share của nó: nó đánh dấu chính nó là dùng chung
// (từ đó từ chối ghi vào thứ nó giữ) và trả các giá trị nó giữ, để FreezeShared đóng băng tiếp. Lần gọi sau trả nil:
// một closure với tới được theo hai đường chỉ được đi qua một lần.
type Sharer interface {
	Share() []Value
}

// FreezeShared đóng băng sâu v như dữ liệu dùng chung rồi trả v: v, mọi object và mảng nó giữ, tới bất kỳ độ sâu nào,
// và những gì các Sharer (closure, struct) nó với tới giữ. Đi bằng một danh sách chờ, không đệ quy, nên một mảng lồng
// trăm nghìn tầng không tràn ngăn xếp; thứ đã dùng chung không đi lại, nên một vòng (object chứa chính nó) dừng. Thứ
// chương trình đã Object.freeze cũng thành dùng chung, và những gì nó giữ cũng đóng băng. Engine gọi nó một lần, sau
// khi boot chạy xong và trước khi request nào đọc: chính nó ghi dấu, không chạy cùng lúc với một lượt đọc.
func (v Value) FreezeShared() Value {
	pending := []Value{v}
	for len(pending) > 0 {
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		switch item.kind {
		case object:
			pending = item.shareObject(pending)
		case array:
			pending = item.shareArray(pending)
		case function, structure:
			if sharer, ok := item.data.(Sharer); ok {
				pending = append(pending, sharer.Share()...)
			}
		}
	}
	return v
}

// IsShared là v là object hay mảng dùng chung (FreezeShared): engine đóng băng nó, không phải chương trình.
func (v Value) IsShared() bool {
	switch v.kind {
	case object:
		return v.objectShared()
	case array:
		return v.arrayShared()
	}
	return false
}

// Held là mọi Value một struct giữ trong các trường xuất ra kiểu Value và []Value, theo thứ tự khai: thứ Share của một
// Sharer trả mà không phải liệt kê tay từng trường (blueprint: return value.Held(w.fields)). Trường viết thường, trường
// kiểu khác bị bỏ qua; structure là struct hay con trỏ tới struct, khác thì nil. Dùng reflect: gọi lúc boot, một lần mỗi
// struct, không phải mỗi request.
func Held(structure any) []Value {
	v := reflect.ValueOf(structure)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	var held []Value
	for i := range v.NumField() {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		switch field := v.Field(i).Interface().(type) {
		case Value:
			held = append(held, field)
		case []Value:
			held = append(held, field...)
		}
	}
	return held
}
