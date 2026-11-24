package command

import (
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// set ghi v vào một trường một giá trị; add thêm v vào cuối một trường danh sách. Mọi phép ghi đi qua hai hàm này, nên
// đây là chỗ duy nhất giữ Command chỉ đọc sau khi engine nạp (Share).
func (c *Command) set(field *value.Value, v value.Value) value.Value {
	if c.shared {
		return readOnly()
	}
	*field = v
	return value.Struct(c)
}

func (c *Command) add(list *[]value.Value, v []value.Value) value.Value {
	if c.shared {
		return readOnly()
	}
	*list = append(*list, v...)
	return value.Struct(c)
}

// readOnly là lỗi của một chữ ghi lên Command đã nạp.
func readOnly() value.Value {
	return value.Invalid("command was loaded by the engine and is read only now: write it in application.work, not while serving")
}

// assign đọc các đối số của app.command(…). Chuỗi đầu là tên chương trình: nó đứng ở đầu câu, vì nó không phải thứ động
// từ execute tác động lên.
//
//	app.command()                  rỗng: viết các chữ sau
//	app.command("kitcli")          một chuỗi: tên chương trình
//	app.command({ … })             một object: mỗi khoá là một chữ (keys)
//	app.command("kitcli", { … })   tên, rồi một object
func (c *Command) assign(options ...value.Value) value.Value {
	switch len(options) {
	case 0:
		return value.Struct(c)
	case 1:
		option := options[0]
		switch {
		case option.IsUndefined():
			return value.Struct(c)
		case option.IsString():
			return c.Name(option)
		case option.IsObject():
			return c.keys(option)
		}
	case 2:
		if options[0].IsString() && options[1].IsObject() {
			if result := c.Name(options[0]); result.IsInvalid() {
				return result
			}
			return c.keys(options[1])
		}
	}
	return value.Invalid(`app.command(…) takes app.command("kitcli"), app.command({ … }) or app.command("kitcli", { … }), got app.command(%s)`, typeofs(options))
}

// keys ghi từng khoá của object bằng chữ cùng tên. execute là chữ cuối, không phải khoá. Khoá lạ là lỗi nêu các tên.
func (c *Command) keys(object value.Value) value.Value {
	for _, key := range object.Keys() {
		item, _ := object.Get(key)
		var result value.Value
		switch key {
		case "name":
			result = c.Name(item)
		case "execute":
			return value.Invalid("command has no key %q: execute is the closing word, end the statement with .execute({ … })", key)
		case "success":
			result = c.Success(item.List()...)
		case "error":
			result = c.Error(item.List()...)
		case "finally":
			result = c.Finally(item.List()...)
		default:
			return value.Invalid("command has no key %q: name, success, error, finally", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(c)
}

// typeofs là loại của từng đối số, cho lời báo.
func typeofs(arguments []value.Value) string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Typeof()
	}
	return strings.Join(names, ", ")
}
