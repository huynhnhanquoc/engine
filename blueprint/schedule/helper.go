package schedule

import (
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// set ghi v vào một trường một giá trị; add thêm v vào cuối một trường danh sách. Mọi phép ghi đi qua hai hàm này, nên
// đây là chỗ duy nhất giữ Schedule chỉ đọc sau khi engine nạp (Share).
func (s *Schedule) set(field *value.Value, v value.Value) value.Value {
	if s.shared {
		return readOnly()
	}
	*field = v
	return value.Struct(s)
}

func (s *Schedule) add(list *[]value.Value, v []value.Value) value.Value {
	if s.shared {
		return readOnly()
	}
	*list = append(*list, v...)
	return value.Struct(s)
}

// readOnly là lỗi của một chữ ghi lên Schedule đã nạp.
func readOnly() value.Value {
	return value.Invalid("schedule was loaded by the engine and is read only now: write it in application.work, not while serving")
}

// assign đọc các đối số của app.schedule(…). Thứ quan trọng nhất của cửa là các thư mục có lịch, và chúng là giá trị
// của chữ cuối, .run(…), không đứng ở đây.
//
//	app.schedule()                     rỗng: viết các chữ sau
//	app.schedule({ … })                một object: mỗi khoá là một chữ (keys)
func (s *Schedule) assign(options ...value.Value) value.Value {
	switch len(options) {
	case 0:
		return value.Struct(s)
	case 1:
		option := options[0]
		switch {
		case option.IsUndefined():
			return value.Struct(s)
		case option.IsObject() && !option.IsArray() && !option.IsFunction():
			return s.keys(option)
		}
	}
	return value.Invalid(`app.schedule(…) takes app.schedule() or app.schedule({ … }), and the folders go to the closing word, .run(".schedules"): got app.schedule(%s)`, typeofs(options))
}

// keys ghi từng khoá của object bằng chữ cùng tên. run là chữ cuối, không phải khoá. Khoá lạ là lỗi nêu các tên.
func (s *Schedule) keys(object value.Value) value.Value {
	for _, key := range object.Keys() {
		item, _ := object.Get(key)
		var result value.Value
		switch key {
		case "run":
			return value.Invalid("schedule has no key %q: run is the closing word, end the statement with .run(…)", key)
		case "memory":
			result = s.Memory(item)
		case "energy":
			result = s.Energy(item)
		case "success":
			result = s.Success(item.List()...)
		case "error":
			result = s.Error(item.List()...)
		case "finally":
			result = s.Finally(item.List()...)
		default:
			return value.Invalid("schedule has no key %q: memory, energy, success, error, finally", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(s)
}

// typeofs là loại của từng đối số, cho lời báo.
func typeofs(arguments []value.Value) string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Typeof()
	}
	return strings.Join(names, ", ")
}
