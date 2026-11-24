package website

import "github.com/huynhnhanquoc/engine/core/value"

// Chữ JS của Website: phương thức chữ hoa mà mọi tham số và kết quả là value.Value, tên viết thường chữ đầu (SSL là
// ssl); một tham số ...value.Value là một danh sách. Mỗi chữ chỉ ghi giá trị vào trường rồi trả lại Website, để viết
// nối nhau, và không kiểm gì: giá trị đúng hay sai là việc của engine khi nạp. Trường một giá trị: lần viết sau đè lần
// trước. Trường danh sách (guard, success, error, finally): mỗi lần viết thêm vào cuối, theo thứ tự viết. Chữ cuối
// (listen, port) ghi các địa chỉ, đóng câu và trả về app, không trả Website.

// New là app.website(…): app.website(), app.website({ … }) (assign, helper.go); parent là app, thứ chữ cuối trả về.
// Trong object, một khoá không phải chữ nào là lỗi nêu mọi khoá Website nhận: đó là gọi một chữ không có, như gọi sai
// tên hàm. Chỉ application gọi nó (application.Website): thư viện "application" không có chữ website đứng riêng.
func New(parent value.Value, options ...value.Value) value.Value {
	w := &Website{parent: parent}
	return w.assign(options...)
}

// --- Chữ cuối ---

// Listen là chữ cuối của câu app.website(…): .listen(443, 80) ghi các địa chỉ nghe, đóng câu, trả về app. Nó không
// nghe gì: engine mở cổng khi chương trình đã trả app.run() về.
func (w *Website) Listen(v ...value.Value) value.Value { return w.close("listen", v) }
func (w *Website) Port(v ...value.Value) value.Value   { return w.close("port", v) } // tên thứ hai của listen

// --- Lúc mở cửa ---

func (w *Website) Certs(v value.Value) value.Value        { return w.Certificates(v) } // tên thứ hai của certificates
func (w *Website) Certificates(v value.Value) value.Value { return w.set(&w.fields.Certificates, v) }
func (w *Website) Concurrency(v value.Value) value.Value  { return w.set(&w.fields.Concurrency, v) }

// --- Trần của một lượt chạy ---

func (w *Website) Memory(v value.Value) value.Value { return w.set(&w.fields.Memory, v) }
func (w *Website) Energy(v value.Value) value.Value { return w.set(&w.fields.Energy, v) }

// --- Lúc boot site và bắt tay TLS ---

func (w *Website) Warm(v value.Value) value.Value { return w.set(&w.fields.Warm, v) }
func (w *Website) SSL(v value.Value) value.Value  { return w.set(&w.fields.SSL, v) }

// --- Mỗi request ---

func (w *Website) Guard(v ...value.Value) value.Value { return w.add(&w.fields.Guards, v) }
func (w *Website) Routes(v value.Value) value.Value   { return w.set(&w.fields.Routes, v) }

// --- Sau khi đã gửi câu trả lời ---

func (w *Website) Success(v ...value.Value) value.Value { return w.add(&w.fields.Success, v) }
func (w *Website) Error(v ...value.Value) value.Value   { return w.add(&w.fields.Errors, v) }
func (w *Website) Finally(v ...value.Value) value.Value { return w.add(&w.fields.Finally, v) }
