package database

import (
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// set ghi v vào một trường một giá trị của fields; add thêm v vào cuối một trường danh sách. Mọi phép ghi lên Database
// đi qua hai hàm này, nên đây là chỗ duy nhất giữ nó chỉ đọc sau khi engine nạp (Share). Chữ thường: không phải chữ JS.
func (d *Database) set(field *value.Value, v value.Value) value.Value {
	if d.shared {
		return readOnly()
	}
	*field = v
	return value.Struct(d)
}

func (d *Database) add(list *[]value.Value, v []value.Value) value.Value {
	if d.shared {
		return readOnly()
	}
	*list = append(*list, v...)
	return value.Struct(d)
}

// readOnly là lỗi của một chữ ghi lên Database đã nạp.
func readOnly() value.Value {
	return value.Invalid("database was loaded by the engine and is read only now: write it in application.work, not while serving")
}

// assign đọc các đối số của app.database(…). Thứ quan trọng nhất của cửa là mở cái gì (owner 23/11), và nó là giá trị
// của chữ cuối, .open(…), không đứng ở đây; cổng không có mặc định, viết bằng khoá port hay listen. Chỉ có các dạng
// dưới; dạng khác là lỗi nêu chúng.
//
//	app.database()                   rỗng: viết các chữ sau
//	app.database({ … })              một object: mỗi khoá là một chữ (keys)
func (d *Database) assign(options ...value.Value) value.Value {
	switch len(options) {
	case 0:
		return value.Struct(d)
	case 1:
		option := options[0]
		switch {
		case option.IsUndefined():
			return value.Struct(d)
		case option.IsObject():
			return d.keys(option)
		}
	}
	return value.Invalid(`app.database(…) takes app.database() or app.database({ … }), and the folder goes to the closing word, .open(".database"): got app.database(%s)`, typeofs(options))
}

// keys ghi từng khoá của object bằng chữ cùng tên: đúng tên chữ, viết tay để thấy cả từ vựng ở một chỗ. Khoá của chữ
// danh sách nhận một giá trị hay một mảng (listen: 5445, listen: [5445, "127.0.0.1:5446"]); port là tên thứ hai của
// listen (owner 23/11). open là chữ cuối, không phải khoá. Khoá lạ là lỗi nêu các tên đầy đủ.
func (d *Database) keys(object value.Value) value.Value {
	for _, key := range object.Keys() {
		item, _ := object.Get(key)
		var result value.Value
		switch key {
		case "open":
			return value.Invalid("database has no key %q: open is the closing word, end the statement with .open(…)", key)
		case "listen", "port":
			result = d.Listen(item.List()...)
		case "concurrency":
			result = d.Concurrency(item)
		case "memory":
			result = d.Memory(item)
		case "timeout":
			result = d.Timeout(item)
		case "user":
			result = d.User(item)
		case "password":
			result = d.Password(item)
		case "tls":
			result = d.TLS(item)
		case "binding":
			result = d.Binding(item)
		case "authority":
			result = d.Authority(item)
		default:
			return value.Invalid("database has no key %q: listen, concurrency, memory, timeout, user, password, tls, binding, authority", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(d)
}

// typeofs là loại của từng đối số, cho lời báo: "number, boolean".
func typeofs(arguments []value.Value) string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Typeof()
	}
	return strings.Join(names, ", ")
}
