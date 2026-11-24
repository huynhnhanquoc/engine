package host

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Describe là bản thiết kế đã dựng, viết ra để đọc cả chương trình ở một chỗ (owner 24/11/2026, "Làm đi";
// docs/KITWORK_APPLICATION_WORK.md mục 4): dòng đầu là application, rồi mỗi cửa một dòng, theo thứ tự website, database,
// schedule, command, desktop, mobile. Mỗi dòng có ba cột: tên, chỗ khai trong application.work (file:dòng:cột của câu),
// và các giá trị đã ghi, cách nhau bằng " · ", theo thứ tự các trường của Fields: giá trị chính của cửa (thứ chữ cuối
// nhận) đứng đầu.
//
//	application  application.work:14:13  memory "2gb" · timezone "Asia/Ho_Chi_Minh"
//	  website    application.work:17:1   listen 443, 80 · certificates ".certificates" · 5 guard · routes application.work:71:11
//	  database   application.work:99:1   open ".database" · listen 5445 · password ••••
//
// Mỗi giá trị viết bằng chữ JS của nó (guard, không Guards; run và execute cho thư mục lịch và bảng lệnh, vì đó là chữ
// người viết đã viết). Một hàm là chỗ của nó; một danh sách toàn hàm là số hàm và chữ ("5 guard"); một bảng lệnh là
// tên các lệnh; mật khẩu không bao giờ in ra ("••••"). Describe không kiểm gì: nó in đúng thứ chương trình viết, kể cả
// giá trị sai, nên dùng được trước bộ nạp. "" khi b không giữ một application.
func (b Booted) Describe() string {
	a, ok := value.As[application.Application](b.Blueprint)
	if !ok {
		return ""
	}
	f := a.Fields()
	type row struct{ name, place, items string }
	own := f // dòng của application chỉ có giá trị chung: mỗi cửa có dòng của mình
	none := value.Undefined()
	own.Website, own.Schedule, own.Database, own.Command, own.Desktop, own.Mobile = none, none, none, none, none, none
	rows := []row{{"application", b.Places["application"], described(own, nil)}}
	if w, ok := value.As[website.Website](f.Website); ok {
		rows = append(rows, row{"  website", b.Places["website"], described(w.Fields(), nil)})
	}
	if d, ok := value.As[database.Database](f.Database); ok {
		rows = append(rows, row{"  database", b.Places["database"], described(d.Fields(), []string{"Password"})})
	}
	if s, ok := value.As[schedule.Schedule](f.Schedule); ok {
		rows = append(rows, row{"  schedule", b.Places["schedule"], described(s.Fields(), nil)})
	}
	if c, ok := value.As[command.Command](f.Command); ok {
		rows = append(rows, row{"  command", b.Places["command"], described(c.Fields(), nil)})
	}
	if !f.Desktop.IsUndefined() {
		rows = append(rows, row{"  desktop", b.Places["desktop"], show(f.Desktop)})
	}
	if !f.Mobile.IsUndefined() {
		rows = append(rows, row{"  mobile", b.Places["mobile"], show(f.Mobile)})
	}

	names, places := 0, 0
	for _, r := range rows {
		names = max(names, utf8.RuneCountInString(r.name))
		places = max(places, utf8.RuneCountInString(r.place))
	}
	var out strings.Builder
	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %-*s  %s", names, r.name, places, r.place, r.items)
		out.WriteString(strings.TrimRight(line, " "))
		out.WriteByte('\n')
	}
	return out.String()
}

// described là các giá trị đã ghi của một Fields, theo thứ tự các trường: "chữ giá trị", cách nhau bằng " · ". Trường
// chưa ghi thì không có mặt; trường có tên trong secret chỉ in là có ("••••"). Đọc bằng reflect, để một trường mới của
// một cửa có mặt mà không phải nhớ thêm nó ở đây.
func described(fields any, secret []string) string {
	v := reflect.ValueOf(fields)
	var items []string
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		word := fieldWord(name)
		hidden := false
		for _, s := range secret {
			hidden = hidden || s == name
		}
		switch field := v.Field(i).Interface().(type) {
		case value.Value:
			switch {
			case field.IsUndefined():
			case hidden:
				items = append(items, word+" ••••")
			default:
				items = append(items, word+" "+show(field))
			}
		case []value.Value:
			switch {
			case len(field) == 0:
			case functionsOnly(field):
				items = append(items, strconv.Itoa(len(field))+" "+word)
			default:
				texts := make([]string, len(field))
				for j, item := range field {
					if item.IsObject() { // bảng lệnh của execute: tên các lệnh
						texts[j] = strings.Join(item.Keys(), ", ")
						continue
					}
					texts[j] = show(item)
				}
				items = append(items, word+" "+strings.Join(texts, ", "))
			}
		}
	}
	return strings.Join(items, " · ")
}

// describedWords là chữ JS của các trường mà tên trường không phải chữ viết thường của nó: trường danh sách mang s, và
// hai trường do chữ cuối ghi.
var describedWords = map[string]string{
	"Guards":  "guard",
	"Errors":  "error",
	"Folders": "run",
	"Handle":  "execute",
}

// fieldWord là chữ JS của một trường: theo describedWords, hay tên trường theo luật tên chữ (jsName, doors.go).
func fieldWord(field string) string {
	if word, found := describedWords[field]; found {
		return word
	}
	return jsName(field)
}

// functionsOnly là mọi phần tử của list là hàm.
func functionsOnly(list []value.Value) bool {
	for _, item := range list {
		if !item.IsFunction() {
			return false
		}
	}
	return true
}

// show là một giá trị viết ra để đọc: chuỗi trong ngoặc kép, số và boolean như JavaScript viết, một hàm là chỗ của nó
// (file:dòng:cột), một mảng là các phần tử, một object là các khoá của nó.
func show(v value.Value) string {
	switch {
	case v.IsString():
		return strconv.Quote(v.String())
	case v.IsFunction():
		if at := where(v); at != "" {
			return at
		}
		return "function"
	case v.IsArray():
		texts := make([]string, 0, v.Length())
		for _, item := range v.Items() {
			texts = append(texts, show(item))
		}
		return "[" + strings.Join(texts, ", ") + "]"
	case v.IsObject():
		return "{ " + strings.Join(v.Keys(), ", ") + " }"
	case v.IsNull():
		return "null"
	case v.IsNumber():
		return strconv.FormatFloat(v.Number(), 'f', -1, 64)
	case v.IsBoolean():
		return strconv.FormatBool(v.Boolean())
	}
	return v.Typeof()
}
