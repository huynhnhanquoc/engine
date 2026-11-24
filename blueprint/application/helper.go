package application

import (
	"strings"

	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/value"
)

// set ghi v vào một trường một giá trị của fields; open mở câu của một cửa. Mọi phép ghi lên Application đi qua hai hàm
// này (verb kiểm riêng trước khi đặt dấu), nên đây là chỗ giữ nó chỉ đọc sau khi engine nạp (Share), và giữ run, check
// là dòng cuối (writable). Chữ thường: không phải chữ JS.
func (a *Application) set(field *value.Value, v value.Value) value.Value {
	if failure := a.writable(); failure.IsInvalid() {
		return failure
	}
	*field = v
	return value.Struct(a)
}

// open là app.<cửa>(…): dựng cửa bằng build, gắn nó vào trường của nó, trả về cửa. Một cửa khai hai lần là lỗi: lần
// khai sau không lặng lẽ đè lần trước. Cửa dựng không được (đối số sai) thì không gắn gì.
func (a *Application) open(field *value.Value, name string, build func() value.Value) value.Value {
	if failure := a.writable(); failure.IsInvalid() {
		return failure
	}
	if !field.IsUndefined() {
		return value.Invalid("application already has a %s: a door is declared once, in one statement", name)
	}
	door := build()
	if door.IsInvalid() {
		return door
	}
	*field = door
	return door
}

// verb là app.run() hay app.check(): đặt dấu mark. Không đối số: các cửa đã khai bằng câu của chúng. Một chương trình
// xin một trong hai, một lần.
func (a *Application) verb(name string, mark *bool, arguments []value.Value) value.Value {
	if failure := a.writable(); failure.IsInvalid() {
		return failure
	}
	for _, argument := range arguments {
		if !argument.IsUndefined() {
			return value.Invalid("app.%s() takes nothing: a door is written in its own statement before it, like app.website(…).listen(443), got app.%s(%s)",
				name, name, typeofs(arguments))
		}
	}
	*mark = true
	return value.Struct(a)
}

// writable là lỗi của một chữ ghi lên Application không còn ghi được: engine đã nạp nó, hay chương trình đã viết
// app.run(), app.check() (dòng cuối); undefined khi còn ghi được.
func (a *Application) writable() value.Value {
	switch {
	case a.shared:
		return readOnly()
	case a.run:
		return value.Invalid("app.run() is the last line of application.work: write this before it")
	case a.check:
		return value.Invalid("app.check() is the last line of application.work: write this before it")
	}
	return value.Undefined()
}

// readOnly là lỗi của một chữ ghi lên Application đã nạp.
func readOnly() value.Value {
	return value.Invalid("application was loaded by the engine and is read only now: write it in application.work, not while serving")
}

// Dangling là một cửa có câu chưa đóng: tên cửa, và chữ cuối còn thiếu.
type Dangling struct{ Door, Word string }

// Dangling là các cửa của a mà câu chưa có chữ cuối (app.website({ … }).guard(https); không có .listen(…)), theo thứ tự
// website, database, schedule, command. Một câu chưa đóng chỉ thấy được khi file đã chạy xong, nên engine hỏi nó khi
// nạp, như Dangling của router. Không phải chữ JS.
func (a *Application) Dangling() []Dangling {
	var dangling []Dangling
	if w, ok := value.As[website.Website](a.fields.Website); ok && !w.Closed() {
		dangling = append(dangling, Dangling{"website", "listen"})
	}
	if d, ok := value.As[database.Database](a.fields.Database); ok && !d.Closed() {
		dangling = append(dangling, Dangling{"database", "open"})
	}
	if s, ok := value.As[schedule.Schedule](a.fields.Schedule); ok && !s.Closed() {
		dangling = append(dangling, Dangling{"schedule", "run"})
	}
	if c, ok := value.As[command.Command](a.fields.Command); ok && !c.Closed() {
		dangling = append(dangling, Dangling{"command", "execute"})
	}
	return dangling
}

// assign đọc các đối số của application(…). Chỉ có các dạng dưới; dạng khác là lỗi nêu chúng. Một cổng không đứng ở
// đây (application(8080) của 23/11 bỏ, owner 24/11): địa chỉ là của cửa nghe nó.
//
//	application()                  rỗng
//	application({ … })             một object: mỗi khoá là một chữ (keys)
func (a *Application) assign(options ...value.Value) value.Value {
	switch len(options) {
	case 0:
		return value.Struct(a)
	case 1:
		option := options[0]
		switch {
		case option.IsUndefined():
			return value.Struct(a)
		case option.IsObject():
			return a.keys(option)
		}
	}
	return value.Invalid("application(…) takes application() or application({ … }), and an address is written by the door that listens on it, "+
		"like app.website(…).listen(443): got application(%s)", typeofs(options))
}

// keys ghi từng khoá của object bằng chữ cùng tên: đúng tên chữ, viết tay để thấy cả từ vựng ở một chỗ. Một cửa không
// phải khoá: nó có câu của mình, app.website(…).listen(…) (owner 24/11); listen và port cũng không: địa chỉ là của cửa
// nghe nó; run và check là động từ. Khoá lạ là lỗi nêu các tên đầy đủ.
func (a *Application) keys(object value.Value) value.Value {
	for _, key := range object.Keys() {
		item, _ := object.Get(key)
		var result value.Value
		switch key {
		case "memory":
			result = a.Memory(item)
		case "timezone":
			result = a.Timezone(item)
		case "listen", "port":
			return value.Invalid("application has no key %q: an address is written by the door that listens on it, like app.website(…).listen(443)", key)
		case "desktop":
			result = a.Desktop(item)
		case "mobile":
			result = a.Mobile(item)
		case "website", "database", "schedule", "command", "web", "db", "cron", "cli":
			return value.Invalid("application has no key %q: a door is written in its own statement, like app.website(…).listen(443)", key)
		case "run", "check":
			return value.Invalid("application has no key %q: %s is a verb, end with return app.%s()", key, key, key)
		default:
			return value.Invalid("application has no key %q: memory, timezone, desktop, mobile", key)
		}
		if result.IsInvalid() {
			return result
		}
	}
	return value.Struct(a)
}

// typeofs là loại của từng đối số, cho lời báo: "number, boolean".
func typeofs(arguments []value.Value) string {
	names := make([]string, len(arguments))
	for i, argument := range arguments {
		names[i] = argument.Typeof()
	}
	return strings.Join(names, ", ")
}
