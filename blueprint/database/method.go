package database

import "github.com/huynhnhanquoc/engine/core/value"

// Chữ JS của Database, như của Website (blueprint/website/method.go): mỗi chữ chỉ ghi giá trị vào trường rồi trả lại
// Database, để viết nối nhau, và không kiểm gì: giá trị đúng hay sai là việc của engine khi nạp. Trường một giá trị:
// lần viết sau đè lần trước. Trường danh sách (listen): mỗi lần viết thêm vào cuối, theo thứ tự viết. Chữ cuối (open)
// ghi thứ được mở, đóng câu và trả về app, không trả Database.

// New là app.database(…): app.database(), app.database({ … }) (assign, helper.go); parent là app, thứ chữ cuối trả về.
// Chỉ application gọi nó (application.Database): thư viện "application" không có chữ database đứng riêng.
func New(parent value.Value, options ...value.Value) value.Value {
	d := &Database{parent: parent}
	return d.assign(options...)
}

// --- Chữ cuối ---

// Open là chữ cuối của câu app.database(…): .open(".database") ghi thư mục các file database, đóng câu, trả về app. Nó
// không mở gì: engine mở khi chương trình đã trả app.run() về. Chữ cuối đòi giá trị của nó: không viết được một
// database không mở gì.
func (d *Database) Open(v value.Value) value.Value {
	if d.shared {
		return readOnly()
	}
	if v.IsUndefined() {
		return value.Invalid(`database.open(…) takes the folder of the databases: .open(".database")`)
	}
	d.fields.Open = v
	d.closed = true
	return d.parent
}

// --- Lúc mở cửa ---

func (d *Database) Listen(v ...value.Value) value.Value   { return d.add(&d.fields.Listen, v) }
func (d *Database) Port(v ...value.Value) value.Value     { return d.add(&d.fields.Listen, v) } // tên thứ hai của listen
func (d *Database) Concurrency(v value.Value) value.Value { return d.set(&d.fields.Concurrency, v) }

// --- Trần của một câu SQL qua cổng ---

func (d *Database) Memory(v value.Value) value.Value  { return d.set(&d.fields.Memory, v) }
func (d *Database) Timeout(v value.Value) value.Value { return d.set(&d.fields.Timeout, v) }

// --- Đăng nhập qua cổng ---

func (d *Database) User(v value.Value) value.Value      { return d.set(&d.fields.User, v) }
func (d *Database) Password(v value.Value) value.Value  { return d.set(&d.fields.Password, v) }
func (d *Database) TLS(v value.Value) value.Value       { return d.set(&d.fields.TLS, v) }
func (d *Database) Binding(v value.Value) value.Value   { return d.set(&d.fields.Binding, v) }
func (d *Database) Authority(v value.Value) value.Value { return d.set(&d.fields.Authority, v) }
