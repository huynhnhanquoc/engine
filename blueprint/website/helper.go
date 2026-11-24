package website

import (
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// set ghi v vào một trường một giá trị của fields; add thêm v vào cuối một trường danh sách. Mọi phép ghi lên Website
// đi qua hai hàm này, nên đây là chỗ duy nhất giữ nó chỉ đọc sau khi engine nạp (Share): một guard giữ biến web mà gọi
// web.guard(…) lúc request nhận lỗi, không ghi vào thứ mọi request cùng đọc. Chữ thường: không phải chữ JS.
func (w *Website) set(field *value.Value, v value.Value) value.Value {
	if w.shared {
		return readOnly()
	}
	*field = v
	return value.Struct(w)
}

func (w *Website) add(list *[]value.Value, v []value.Value) value.Value {
	if w.shared {
		return readOnly()
	}
	*list = append(*list, v...)
	return value.Struct(w)
}

// readOnly là lỗi của một chữ ghi lên Website đã nạp.
func readOnly() value.Value {
	return value.Invalid("website was loaded by the engine and is read only now: write it in application.work, not while serving")
}

// close là chữ cuối (listen, port): thêm các địa chỉ, đánh dấu câu đã đóng, trả về app. Chữ cuối đòi giá trị của nó:
// không viết được một website không cổng nào.
func (w *Website) close(word string, addresses []value.Value) value.Value {
	if w.shared {
		return readOnly()
	}
	if len(addresses) == 0 || (len(addresses) == 1 && addresses[0].IsUndefined()) {
		return value.Invalid("website.%s(…) takes the addresses the website listens on: .%s(443, 80)", word, word)
	}
	w.fields.Listen = append(w.fields.Listen, addresses...)
	w.closed = true
	return w.parent
}

// assign đọc các đối số của app.website(…). Chỉ có các dạng dưới; dạng khác là lỗi nêu chúng. Địa chỉ không đứng ở
// đây: nó là giá trị của chữ cuối, .listen(…).
//
//	app.website()              rỗng: viết các chữ sau
//	app.website({ … })         một object: mỗi khoá là một chữ (keys)
func (w *Website) assign(options ...value.Value) value.Value {
	switch len(options) {
	case 0:
		return value.Struct(w)
	case 1:
		option := options[0]
		switch {
		case option.IsUndefined():
			return value.Struct(w)
		case option.IsObject():
			return w.keys(option)
		}
	}
	return value.Invalid("app.website(…) takes app.website() or app.website({ … }), and the addresses go to the closing word, .listen(443, 80): got app.website(%s)", typeofs(options))
}

// keys ghi từng khoá của object bằng chữ cùng tên: đúng tên chữ, viết tay để thấy cả từ vựng ở một chỗ. Khoá của chữ
// danh sách nhận một giá trị hay một mảng (guard: fn, guard: [a, b]). listen và port là chữ cuối, không phải khoá: địa
// chỉ chỉ có một chỗ, sau động từ của nó. Khoá lạ là lỗi nêu các tên đầy đủ.
func (w *Website) keys(object value.Value) value.Value {
	for _, key := range object.Keys() {
		item, _ := object.Get(key)
		var result value.Value
		switch key {
		case "listen", "port":
			return value.Invalid("website has no key %q: %s is the closing word, end the statement with .%s(…)", key, key, key)
		case "certificates", "certs":
			result = w.Certificates(item)
		case "concurrency":
			result = w.Concurrency(item)
		case "memory":
			result = w.Memory(item)
		case "energy":
			result = w.Energy(item)
		case "warm":
			result = w.Warm(item)
		case "ssl":
			result = w.SSL(item)
		case "guard":
			result = w.Guard(item.List()...)
		case "routes":
			result = w.Routes(item)
		case "success":
			result = w.Success(item.List()...)
		case "error":
			result = w.Error(item.List()...)
		case "finally":
			result = w.Finally(item.List()...)
		default:
			return value.Invalid("website has no key %q: certificates, concurrency, memory, energy, warm, ssl, guard, routes, success, error, finally", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(w)
}

// typeofs là loại của từng đối số, cho lời báo: "number, boolean".
func typeofs(arguments []value.Value) string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Typeof()
	}
	return strings.Join(names, ", ")
}
