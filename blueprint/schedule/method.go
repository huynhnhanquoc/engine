package schedule

import "github.com/huynhnhanquoc/engine/core/value"

// Chữ JS của Schedule, như của Website: mỗi chữ chỉ ghi giá trị vào trường rồi trả lại Schedule, không kiểm gì. Trường
// một giá trị: lần viết sau đè lần trước; trường danh sách (success, error, finally): thêm vào cuối. Chữ cuối (run) ghi
// các thư mục có lịch, đóng câu và trả về app, không trả Schedule.

// New là app.schedule(…): app.schedule(), app.schedule({ … }) (assign, helper.go); parent là app, thứ chữ cuối trả về.
// Chỉ application gọi nó (application.Schedule): thư viện "application" không có chữ schedule đứng riêng.
func New(parent value.Value, options ...value.Value) value.Value {
	s := &Schedule{parent: parent}
	return s.assign(options...)
}

// --- Chữ cuối ---

// Run là chữ cuối của câu app.schedule(…): .run(".schedules"), .run([…]), .run(() => […]) ghi các thư mục có lịch, đóng
// câu, trả về app. Nó không chạy lịch nào: engine đặt lịch khi chương trình đã trả app.run() về. Chữ cuối đòi giá trị
// của nó: engine không có thư mục lịch nào của riêng nó (owner 24/11).
func (s *Schedule) Run(v value.Value) value.Value {
	if s.shared {
		return readOnly()
	}
	if v.IsUndefined() {
		return value.Invalid(`schedule.run(…) takes the folders of the schedules: ".schedules", a list, or a function returning a list`)
	}
	s.fields.Folders = v
	s.closed = true
	return s.parent
}

// --- Mỗi job ---

func (s *Schedule) Memory(v value.Value) value.Value { return s.set(&s.fields.Memory, v) }
func (s *Schedule) Energy(v value.Value) value.Value { return s.set(&s.fields.Energy, v) }

// --- Sau mỗi job ---

func (s *Schedule) Success(v ...value.Value) value.Value { return s.add(&s.fields.Success, v) }
func (s *Schedule) Error(v ...value.Value) value.Value   { return s.add(&s.fields.Errors, v) }
func (s *Schedule) Finally(v ...value.Value) value.Value { return s.add(&s.fields.Finally, v) }
