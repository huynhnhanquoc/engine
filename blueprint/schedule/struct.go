package schedule

import "github.com/huynhnhanquoc/engine/core/value"

// Schedule là bản thiết kế lịch của host và của các tenant, một struct: chỗ khác nhau giữa hai loại là quyết định theo
// từng job, đọc context.job.folder. Thụ động như Website (23/11/2026): chỉ giữ, không hiểu; các trường khai đúng một lần
// trong Fields, chữ JS ghi vào chúng (method.go), engine đọc bằng Fields() khi nạp.
//
// Một Schedule chỉ sinh ra từ app.schedule(…) (owner 24/11/2026: các cửa gom về application), và câu của nó kết thúc
// bằng chữ cuối run(…), chữ trả về app (owner: "schedule nên kết thúc bằng run()"):
//
//	app.schedule()
//	  .memory((context) => (context.job.folder === ".schedules" ? "1gb" : "512mb"))
//	  .run(() => [".schedules", ...tenants.map((tenant) => `apps/${tenant.identity}/.schedules`)]);
type Schedule struct {
	fields Fields
	parent value.Value // app đã mở cửa này: chữ cuối trả nó về (undefined khi Go dựng cửa một mình, trong bài test)
	closed bool        // câu đã có chữ cuối (run)
	shared bool        // engine đã nạp và đóng băng (Share): từ đây chỉ đọc
}

// Fields là các trường của Schedule: đúng các value.Value chương trình đưa vào, không kiểm, không đổi.
type Fields struct {
	// --- Lúc boot ---
	Folders value.Value // ".schedules" | [".schedules", …] | () => […] (chữ cuối run(…)): các thư mục có lịch; thư mục không có thì bỏ qua, một dòng log

	// --- Mỗi job ---
	Memory value.Value // "512mb" | (context: JobContext) => "…": trần bộ nhớ của một job
	Energy value.Value // 10000000 | (context: JobContext) => number: trần năng lượng của một job

	// --- Sau mỗi job: đúng một trong success hay errors, rồi luôn finally ---
	Success []value.Value // (context: JobContext) => …: job xong không lỗi
	Errors  []value.Value // (context: JobContext) => …: lỗi ở context.job.error
	Finally []value.Value // (context: JobContext) => …: sau mỗi job
}

// Fields là các trường của s, như đang có, không chép: engine chỉ đọc, và đọc sau khi bản thiết kế đã đóng băng.
func (s *Schedule) Fields() Fields { return s.fields }

// Share là value.Sharer: engine gọi nó khi đóng băng bản thiết kế sau khi nạp; lần gọi sau trả nil.
func (s *Schedule) Share() []value.Value {
	if s.shared {
		return nil
	}
	s.shared = true
	return value.Held(s.fields)
}

// Closed là câu của s đã có chữ cuối run(…). Một cửa chưa đóng là lỗi khi nạp (application.Dangling).
func (s *Schedule) Closed() bool { return s.closed }

// Shared là engine đã nạp s: từ đây mọi chữ ghi lên nó là lỗi.
func (s *Schedule) Shared() bool { return s.shared }
