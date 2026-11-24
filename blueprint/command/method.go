package command

import "github.com/huynhnhanquoc/engine/core/value"

// Chữ JS của Command, như của Website: mỗi chữ chỉ ghi giá trị vào trường rồi trả lại Command, không kiểm gì. Chữ cuối
// (execute) ghi bảng lệnh, đóng câu và trả về app, không trả Command.

// New là app.command(…): app.command("kitcli"), app.command({ … }), app.command("kitcli", { … }) (assign, helper.go),
// và app.command() rỗng; parent là app, thứ chữ cuối trả về. Chỉ application gọi nó (application.Command): thư viện
// "application" không có chữ command đứng riêng.
func New(parent value.Value, options ...value.Value) value.Value {
	c := &Command{parent: parent}
	return c.assign(options...)
}

// --- Chữ cuối ---

// Execute là chữ cuối của câu app.command(…): .execute({ backup, migrate }) ghi bảng lệnh, đóng câu, trả về app. Nó
// không thực thi gì: engine chạy một lệnh khi chương trình được gọi bằng tên lệnh ấy. Chữ cuối đòi giá trị của nó:
// không viết được một cửa lệnh không lệnh nào.
func (c *Command) Execute(v ...value.Value) value.Value {
	if c.shared {
		return readOnly()
	}
	if len(v) == 0 || (len(v) == 1 && v[0].IsUndefined()) {
		return value.Invalid("command.execute(…) takes the commands: .execute({ backup: (context) => … })")
	}
	for _, table := range v {
		if table.IsObject() && !table.IsArray() && !table.IsFunction() && len(table.Keys()) == 0 {
			return value.Invalid("command.execute({ … }) needs at least one command: .execute({ backup: (context) => … })")
		}
	}
	c.fields.Handle = append(c.fields.Handle, v...)
	c.closed = true
	return c.parent
}

// --- Tên chương trình ---

func (c *Command) Name(v value.Value) value.Value { return c.set(&c.fields.Name, v) }

// --- Sau mỗi lệnh ---

func (c *Command) Success(v ...value.Value) value.Value { return c.add(&c.fields.Success, v) }
func (c *Command) Error(v ...value.Value) value.Value   { return c.add(&c.fields.Errors, v) }
func (c *Command) Finally(v ...value.Value) value.Value { return c.add(&c.fields.Finally, v) }
