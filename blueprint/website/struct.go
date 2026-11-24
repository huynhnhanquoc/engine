package website

import "github.com/huynhnhanquoc/engine/core/value"

// Website là bản thiết kế của server web mà chương trình dựng: chỉ giữ, không hiểu. Các trường của nó khai đúng một lần,
// trong Fields; Website giữ chúng trong trường fields (có tên, viết thường, không nhúng: không trường nào được đưa lên
// Website, nên package khác không ghi được gì) và thêm dấu đã nạp. Chữ JS ghi vào fields (method.go); engine đọc bằng
// Fields(), kiểm mọi trường một lần trước khi mở cổng, gom mọi lỗi theo đường dẫn trường (application.website.listen[1]:
// …), rồi gọi các hàm của nó khi có sự kiện.
//
// Một Website chỉ sinh ra từ app.website(…) (owner 24/11/2026: các cửa gom về application), và câu của nó kết thúc bằng
// chữ cuối listen(…), chữ trả về app:
//
//	app.website({ certificates: ".certificates" }).guard(https).routes(pick).listen(443, 80);
type Website struct {
	fields Fields
	parent value.Value // app đã mở cửa này: chữ cuối trả nó về (undefined khi Go dựng cửa một mình, trong bài test)
	closed bool        // câu đã có chữ cuối (listen, port)
	shared bool        // engine đã nạp và đóng băng (Share): từ đây chỉ đọc
}

// Fields là các trường của Website: đúng các value.Value chương trình đưa vào, không kiểm, không đổi. Ghi chú của mỗi
// trường nói các dạng engine nhận và lúc nó chạy. Thêm một trường là thêm một dòng ở đây và một chữ JS ở method.go.
type Fields struct {
	// --- Lúc mở cửa ---
	Listen       []value.Value // 8080, ":443", "127.0.0.1:8080" (chữ cuối listen(…), port(…)): mỗi cổng nhận cả TLS lẫn HTTP thường
	Certificates value.Value   // ".certificates": thư mục chứng chỉ TLS; vắng: không cổng nào bật TLS
	Concurrency  value.Value   // 16: số request chạy cùng lúc tối đa mỗi site; vắng: gấp đôi số nhân CPU, ít nhất 8

	// --- Trần của một lượt chạy ---
	Memory value.Value // "64mb" | (context: RequestContext) => "…"
	Energy value.Value // 10000000 | (context: RequestContext) => number

	// --- Lúc boot site và bắt tay TLS ---
	Warm value.Value // true | false | (context: SiteContext) => bool: giữ ấm site trong RAM lúc boot
	SSL  value.Value // (context: HandshakeContext) => bool: tên miền này có được bắt tay TLS không

	// --- Mỗi request, theo thứ tự chạy: guards, routes, rồi memory và energy của lượt chạy ---
	Guards []value.Value // (context: RequestContext) => Decision, chạy theo thứ tự viết
	Routes value.Value   // "apps/…" | (context: RequestContext) => "apps/…": thư mục site của request

	// --- Sau khi đã gửi câu trả lời: đúng một trong success hay errors, rồi luôn finally ---
	Success []value.Value // (context: RequestContext) => …: câu trả lời không mang lỗi
	Errors  []value.Value // (context: RequestContext) => …: guard từ chối hay code gặp lỗi
	Finally []value.Value // (context: RequestContext) => …: luôn chạy cuối
}

// Fields là các trường của w, như đang có, không chép: engine chỉ đọc, và đọc sau khi bản thiết kế đã đóng băng (không
// chữ nào ghi được nữa). Không phải chữ JS (kết quả không phải value.Value), nên chương trình không thấy nó.
func (w *Website) Fields() Fields { return w.fields }

// Share là value.Sharer: engine gọi nó khi đóng băng bản thiết kế sau khi nạp (value.FreezeShared). Website đánh dấu
// chính nó chỉ đọc và trả mọi giá trị nó giữ (value.Held), để chúng được đóng băng tiếp; lần gọi sau trả nil. Không
// phải chữ JS.
func (w *Website) Share() []value.Value {
	if w.shared {
		return nil
	}
	w.shared = true
	return value.Held(w.fields)
}

// Closed là câu của w đã có chữ cuối: listen(…) hay port(…). Một cửa chưa đóng là lỗi khi nạp (application.Dangling).
// Không phải chữ JS.
func (w *Website) Closed() bool { return w.closed }

// Shared là engine đã nạp w: từ đây runtime từ chối mọi chữ ghi lên nó (một guard giữ biến web mà gọi web.guard(…) lúc
// request là ghi vào thứ mọi request cùng đọc). Không phải chữ JS.
func (w *Website) Shared() bool { return w.shared }
