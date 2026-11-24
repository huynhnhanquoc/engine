package database

import "github.com/huynhnhanquoc/engine/core/value"

// Database là bản thiết kế của cổng PostgreSQL trên các file SQLite (KitDB) mà chương trình dựng: chỉ giữ, không hiểu.
// Các trường giữ nghĩa của app.database hôm nay (engine2/docs/kitdb.md mục 5), khai đúng một lần, trong Fields;
// Database giữ chúng trong trường fields (có tên, viết thường, không nhúng: package khác không ghi được gì) và thêm dấu
// đã nạp, như Website. Chữ JS ghi vào fields (method.go); engine đọc bằng Fields(), kiểm mọi trường một lần trước khi
// mở cổng, gom mọi lỗi theo đường dẫn trường (application.database.password: …).
//
// Thứ quan trọng của database là mở cái gì (open), bắt buộc; cổng PostgreSQL (listen) không có mặc định và không bắt
// buộc: vắng là không mở cổng, chỉ app của host dùng database (owner 23/11).
// Không có công tắc search: MATCH qua cổng chạy mặc định trên mọi bảng có chỉ mục toàn văn, và chỉ mục khai trong chính
// database (cột .searchable() của schema, hay CREATE INDEX … USING search qua cổng), không trong application.work.
// Không có energy: cổng chạy SQL trên SQLite, không chạy JS trên VM, nên trần của một câu là byte (memory) và thời gian
// (timeout), không phải năng lượng.
//
// Một Database chỉ sinh ra từ app.database(…) (owner 24/11/2026: các cửa gom về application), và câu của nó kết thúc
// bằng chữ cuối open(…), chữ trả về app:
//
//	app.database({ port: 5445, user: "kitdb", password: env.require("KITDB_PASSWORD") }).open(".database");
type Database struct {
	fields Fields
	parent value.Value // app đã mở cửa này: chữ cuối trả nó về (undefined khi Go dựng cửa một mình, trong bài test)
	closed bool        // câu đã có chữ cuối (open)
	shared bool        // engine đã nạp và đóng băng (Share): từ đây chỉ đọc
}

// Fields là các trường của Database: đúng các value.Value chương trình đưa vào, không kiểm, không đổi. Ghi chú của mỗi
// trường nói các dạng engine nhận. Thêm một trường là thêm một dòng ở đây và một chữ JS ở method.go.
type Fields struct {
	// --- Lúc mở cửa ---
	Open        value.Value   // ".database" (chữ cuối open(".database")): thư mục các file database của host; bắt buộc, không mặc định
	Listen      []value.Value // 5445, "127.0.0.1:5445" (port là chữ thứ hai): cổng PostgreSQL, như website; ngoài 127.0.0.1 và ::1 chỉ nhận TLS; vắng: không mở cổng
	Concurrency value.Value   // 64: số phiên cùng lúc tối đa của cổng; vắng: mặc định của cổng

	// --- Trần của một câu SQL qua cổng: một lượt chạy của database, như một request của website (owner 23/11) ---
	Memory  value.Value // "256mb": số byte một câu được dùng, kết quả trả về cộng file tạm của SQLite; vắng: mặc định của KitDB
	Timeout value.Value // "30s", "500ms", "2m": thời gian một câu được chạy, như statement_timeout của PostgreSQL; vắng: 30 giây

	// --- Đăng nhập qua cổng ---
	User      value.Value // "kitdb": tài khoản quản trị của mọi cụm; vắng: "kitdb"
	Password  value.Value // mật khẩu của user, bắt buộc khi có cổng; bí mật, engine không in ra
	TLS       value.Value // true: nhận TLS (SSLRequest); chứng chỉ của website, kho AutoSSL
	Binding   value.Value // true: BẮT BUỘC client máy khác dùng SCRAM-SHA-256-PLUS (bằng chứng mật khẩu gắn với chứng chỉ server: kẻ đứng giữa không chuyển tiếp được); qua TLS PLUS luôn được mời dù không bật; cần tls; vắng: không bắt buộc
	Authority value.Value // "authority.pem": client máy khác phải đưa chứng chỉ do chứng chỉ gốc này ký, tên (CN) đúng user (clientcert=verify-full), mật khẩu vẫn hỏi; cần tls; vắng: không đòi
}

// Fields là các trường của d, như đang có, không chép: engine chỉ đọc, và đọc sau khi bản thiết kế đã đóng băng. Không
// phải chữ JS (kết quả không phải value.Value), nên chương trình không thấy nó.
func (d *Database) Fields() Fields { return d.fields }

// Share là value.Sharer: engine gọi nó khi đóng băng bản thiết kế sau khi nạp (value.FreezeShared). Database đánh dấu
// chính nó chỉ đọc và trả mọi giá trị nó giữ (value.Held), để chúng được đóng băng tiếp; lần gọi sau trả nil. Không
// phải chữ JS.
func (d *Database) Share() []value.Value {
	if d.shared {
		return nil
	}
	d.shared = true
	return value.Held(d.fields)
}

// Closed là câu của d đã có chữ cuối open(…). Một cửa chưa đóng là lỗi khi nạp (application.Dangling). Không phải chữ
// JS.
func (d *Database) Closed() bool { return d.closed }

// Shared là engine đã nạp d: từ đây mọi chữ ghi lên nó là lỗi. Không phải chữ JS.
func (d *Database) Shared() bool { return d.shared }
