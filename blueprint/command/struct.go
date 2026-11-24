package command

import "github.com/huynhnhanquoc/engine/core/value"

// Command là bản thiết kế chương trình dòng lệnh: engine không giữ lệnh nào, mọi tên là của người viết. Thụ động như
// Website (23/11/2026): chỉ giữ, không hiểu; engine đọc bằng Fields() khi nạp.
//
// Một Command chỉ sinh ra từ app.command(…) (owner 24/11/2026: các cửa gom về application), và câu của nó kết thúc bằng
// chữ cuối execute(…), chữ trả về app (owner: "execute được đó chứ"):
//
//	app.command("kitcli").execute({ backup: (context) => backup(), migrate });
type Command struct {
	fields Fields
	parent value.Value // app đã mở cửa này: chữ cuối trả nó về (undefined khi Go dựng cửa một mình, trong bài test)
	closed bool        // câu đã có chữ cuối (execute)
	shared bool        // engine đã nạp và đóng băng (Share): từ đây chỉ đọc
}

// Fields là các trường của Command: đúng các value.Value chương trình đưa vào, không kiểm, không đổi.
type Fields struct {
	Name   value.Value   // "kitcli": tên chương trình, cũng là tên file của bản dựng
	Handle []value.Value // { tên: (context: CommandContext) => … } (chữ cuối execute({ … })): bảng lệnh; engine gộp theo tên khi nạp

	// --- Sau mỗi lệnh: đúng một trong success hay errors, rồi luôn finally ---
	Success []value.Value // (context: CommandContext) => …: lệnh xong không lỗi
	Errors  []value.Value // (context: CommandContext) => …: lỗi ở context.command.error
	Finally []value.Value // (context: CommandContext) => …: sau mỗi lệnh
}

// Fields là các trường của c, như đang có, không chép: engine chỉ đọc, và đọc sau khi bản thiết kế đã đóng băng.
func (c *Command) Fields() Fields { return c.fields }

// Share là value.Sharer: engine gọi nó khi đóng băng bản thiết kế sau khi nạp; lần gọi sau trả nil.
func (c *Command) Share() []value.Value {
	if c.shared {
		return nil
	}
	c.shared = true
	return value.Held(c.fields)
}

// Closed là câu của c đã có chữ cuối execute(…). Một cửa chưa đóng là lỗi khi nạp (application.Dangling).
func (c *Command) Closed() bool { return c.closed }

// Shared là engine đã nạp c: từ đây mọi chữ ghi lên nó là lỗi.
func (c *Command) Shared() bool { return c.shared }
