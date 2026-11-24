package application

import (
	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Chữ JS của Application. Chữ của giá trị chung (memory, timezone) chỉ ghi vào trường rồi trả lại Application, không
// kiểm giá trị; lần viết sau đè lần trước. Application không có cổng của riêng nó (owner 24/11/2026, "Bỏ đi"): địa chỉ
// chỉ có một chỗ, sau chữ cuối của cửa nghe nó (app.website(…).listen(443), app.database({ port })). Chữ của một cửa (website, database, schedule, command) mở câu của cửa
// ấy và trả về chính cửa; chữ cuối của cửa trả lại Application. run và check là hai động từ, đứng cuối file.

// New là application(…): application() hay application({ … }) (assign, helper.go).
func New(options ...value.Value) value.Value {
	a := &Application{}
	return a.assign(options...)
}

func (a *Application) Memory(v value.Value) value.Value   { return a.set(&a.fields.Memory, v) }
func (a *Application) Timezone(v value.Value) value.Value { return a.set(&a.fields.Timezone, v) }

// --- Các cửa: mỗi cửa một câu, app.<cửa>(cấu hình).<hàm>….<chữ cuối>(giá trị chính) ---
//
// Một cửa chỉ sinh ra ở đây (owner 24/11/2026: các cửa gom về application): thư viện "application" không có chữ
// website(…), database(…), schedule(…), command(…) đứng riêng. Mỗi chữ dưới dựng cửa, gắn nó vào Application ngay, và
// trả về cửa để viết nối các chữ của nó; một cửa khai hai lần là lỗi (open, helper.go).

func (a *Application) Website(options ...value.Value) value.Value {
	return a.open(&a.fields.Website, "website", func() value.Value { return website.New(value.Struct(a), options...) })
}

func (a *Application) Database(options ...value.Value) value.Value {
	return a.open(&a.fields.Database, "database", func() value.Value { return database.New(value.Struct(a), options...) })
}

func (a *Application) Schedule(options ...value.Value) value.Value {
	return a.open(&a.fields.Schedule, "schedule", func() value.Value { return schedule.New(value.Struct(a), options...) })
}

func (a *Application) Command(options ...value.Value) value.Value {
	return a.open(&a.fields.Command, "command", func() value.Value { return command.New(value.Struct(a), options...) })
}

// Desktop và Mobile chưa có câu của mình (chữ cuối của hai cửa này chưa bàn): chúng giữ giá trị được đưa, và trả lại
// Application.
func (a *Application) Desktop(v value.Value) value.Value { return a.set(&a.fields.Desktop, v) }
func (a *Application) Mobile(v value.Value) value.Value  { return a.set(&a.fields.Mobile, v) }

// --- Động từ ---

// Run là app.run(), không đối số: ghi rằng chương trình xin chạy. Không xử lý gì: engine đọc bản thiết kế sau khi
// chương trình trả về, và mở các cửa có mặt. Nó là dòng cuối: sau nó không chữ nào ghi được nữa (set, add, open).
func (a *Application) Run(arguments ...value.Value) value.Value {
	return a.verb("run", &a.run, arguments)
}

// Check như Run, nhưng xin chỉ nạp và kiểm, không mở gì.
func (a *Application) Check(arguments ...value.Value) value.Value {
	return a.verb("check", &a.check, arguments)
}
