package application

import "github.com/huynhnhanquoc/engine/core/value"

// Application là bản thiết kế duy nhất application.work trả về: chỉ giữ, không hiểu.
//
//	const app = application({ memory: "2gb", timezone: "Asia/Ho_Chi_Minh" });
//	app.website({ certificates: ".certificates" }).guard(https).routes(pick).listen(443, 80);
//	app.database({ port: 5445, password: env.require("KITDB_PASSWORD") }).open(".database");
//	return app.run();
//
// Cách viết của owner 24/11/2026 (docs/KITWORK_APPLICATION_WORK.md): application({ … }) giữ thứ chung mọi cửa; mỗi cửa
// một câu app.<cửa>(cấu hình).<hàm>….<chữ cuối>(giá trị chính), và chỉ Application dựng được cửa (method.go); app.run()
// ghi rằng chương trình xin chạy, app.check() xin chỉ nạp và kiểm, cả hai không đối số và là dòng cuối. Các trường khai
// đúng một lần, trong Fields; Application giữ chúng trong trường fields (có tên, không nhúng), thêm dấu đã nạp và hai
// dấu động từ. Engine đọc bằng Fields(), Running(), Checking(), Dangling(), kiểm mọi trường và mọi cửa một lần, gom mọi
// lỗi theo đường dẫn trường (application.website.listen[1]: …), đóng băng cả cây (value.FreezeShared): Running thì mở
// các cửa có mặt; Checking thì chỉ nạp và kiểm (kitwork check); không có cái nào là lỗi khi boot.
type Application struct {
	fields Fields
	shared bool // engine đã nạp và đóng băng (Share): từ đây chỉ đọc

	// --- Động từ: chương trình đã gọi app.run(), app.check() chưa (không phải giá trị chương trình viết, nên không sai được) ---
	run   bool
	check bool
}

// Fields là các trường của Application: đúng các value.Value chương trình đưa vào. Thêm một trường là thêm một dòng ở
// đây và một chữ JS ở method.go.
type Fields struct {
	// --- Chung cho mọi cửa ---
	Memory   value.Value // "2gb": tổng byte mọi lượt chạy cùng lúc của cả chương trình; vắng: không giới hạn
	Timezone value.Value // "Asia/Ho_Chi_Minh": múi giờ của lịch và của Date; vắng: UTC

	// --- Các cửa: chương trình mở ra những gì (vắng: không có cửa ấy) ---
	Website  value.Value // app.website(…): *website.Website
	Schedule value.Value // app.schedule(…): *schedule.Schedule
	Database value.Value // app.database(…): *database.Database
	Command  value.Value // app.command(…): *command.Command
	Desktop  value.Value // app.desktop(…): *desktop.Desktop
	Mobile   value.Value // app.mobile(…): *mobile.Mobile
}

// Fields là các trường của a, như đang có: engine chỉ đọc, và đọc sau khi bản thiết kế đã đóng băng. Không phải chữ JS
// (kết quả không phải value.Value), nên chương trình không thấy nó.
func (a *Application) Fields() Fields { return a.fields }

// Share là value.Sharer: engine gọi nó khi đóng băng bản thiết kế sau khi nạp (value.FreezeShared). Application đánh
// dấu chính nó chỉ đọc và trả mọi giá trị nó giữ (value.Held: các cửa và cài đặt; hai dấu bool không phải giá trị), để
// chúng được đóng băng tiếp; lần gọi sau trả nil. Không phải chữ JS.
func (a *Application) Share() []value.Value {
	if a.shared {
		return nil
	}
	a.shared = true
	return value.Held(a.fields)
}

// Shared là engine đã nạp a: từ đây mọi chữ ghi lên nó là lỗi. Không phải chữ JS.
func (a *Application) Shared() bool { return a.shared }

// Running là chương trình đã gọi app.run(): engine mở các cửa có mặt. Không phải chữ JS.
func (a *Application) Running() bool { return a.run }

// Checking là chương trình đã gọi app.check(): engine chỉ nạp và kiểm. Không phải chữ JS.
func (a *Application) Checking() bool { return a.check }
