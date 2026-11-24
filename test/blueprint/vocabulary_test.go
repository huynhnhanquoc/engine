package blueprint_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/command"
	"github.com/huynhnhanquoc/engine/blueprint/database"
	"github.com/huynhnhanquoc/engine/blueprint/schedule"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Test từ vựng: chữ JS của mỗi cửa là các dòng chép tay gần giống nhau, nên lỗi dễ nhất là chép nhầm (Energy ghi vào
// Memory), quên chữ cho một trường mới, hay quên doorWords cho một cửa mới. Mỗi chữ được gọi với một giá trị đánh dấu,
// rồi Fields cho biết dấu nằm ở đâu.

var (
	valueType = reflect.TypeFor[value.Value]()
	listType  = reflect.TypeFor[[]value.Value]()
)

// aliases là các chữ thứ hai: ghi vào trường của chữ đầu (owner 23/11: port là listen). Lời báo khoá lạ chỉ nêu tên
// đầy đủ, không nêu chúng.
var aliases = map[string]string{"Port": "Listen", "Certs": "Certificates"}

// closing là các chữ cuối của một cửa (owner 24/11): tên chữ → trường nó ghi. Chữ cuối không phải khoá của dạng object,
// và trả về app, không trả cửa.
type closing map[string]string

// wordsOf là các chữ JS của type của p, theo luật của engine (như value.Assign đọc): phương thức xuất ra mà mọi tham số
// và kết quả là value.Value, một tham số ...value.Value là một danh sách.
func wordsOf(p any) []reflect.Method {
	t := reflect.TypeOf(p)
	var words []reflect.Method
	for i := range t.NumMethod() {
		m := t.Method(i)
		in := m.Type // In(0) là receiver
		if in.NumOut() != 1 || in.Out(0) != valueType || in.NumIn() < 2 {
			continue
		}
		word := true
		for j := 1; j < in.NumIn(); j++ {
			last := j == in.NumIn()-1
			if in.In(j) != valueType && !(last && in.IsVariadic() && in.In(j) == listType) {
				word = false
			}
		}
		if word {
			words = append(words, m)
		}
	}
	return words
}

// fieldsOf là Fields() của p, đọc bằng reflect: một struct.
func fieldsOf(p any) reflect.Value {
	return reflect.ValueOf(p).MethodByName("Fields").Call(nil)[0]
}

// holding là các trường của fields giữ marker: một trường Value bằng nó, hay một trường []Value có nó.
func holding(fields reflect.Value, marker value.Value) []string {
	var names []string
	for i := range fields.NumField() {
		switch field := fields.Field(i).Interface().(type) {
		case value.Value:
			if field.Identical(marker) {
				names = append(names, fields.Type().Field(i).Name)
			}
		case []value.Value:
			if slices.ContainsFunc(field, marker.Identical) {
				names = append(names, fields.Type().Field(i).Name)
			}
		}
	}
	return names
}

// call gọi chữ m của p với các đối số.
func call(p any, m reflect.Method, arguments ...value.Value) value.Value {
	in := []reflect.Value{reflect.ValueOf(p)}
	for _, argument := range arguments {
		in = append(in, reflect.ValueOf(argument))
	}
	return m.Func.Call(in)[0].Interface().(value.Value)
}

// checkVocabulary: mỗi chữ (trừ verbs và các chữ cuối) ghi vào đúng một trường, tên trường là tên chữ hay tên chữ thêm
// s (Guard → Guards); chữ danh sách thêm vào cuối, không đè; mỗi chữ cuối ghi đúng trường của nó, đóng câu, trả về app
// và không phải khoá; mọi trường giá trị của Fields có một chữ ghi vào nó (written là các trường do verbs ghi, kiểm ở
// bài khác); sau Share mọi chữ là lỗi và không ghi gì.
func checkVocabulary(t *testing.T, made func() any, build func(value.Value) value.Value, verbs []string, closes closing, written ...string) {
	t.Helper()
	covered := map[string]bool{}
	for _, field := range written {
		covered[field] = true
	}
	for word, field := range closes {
		verbs = append(verbs, word)
		p := made()
		m, found := reflect.TypeOf(p).MethodByName(word)
		if !found {
			t.Errorf("no closing word %s", word)
			continue
		}
		marker := value.String("closing:" + word)
		result := call(p, m, marker)
		if !result.IsUndefined() { // made() dựng cửa một mình: app của nó là undefined
			t.Errorf("%s gives the application back, not the door: %s", word, result.Typeof())
		}
		if got := holding(fieldsOf(p), marker); len(got) != 1 || got[0] != field {
			t.Errorf("%s writes %v, want %s", word, got, field)
		}
		if closed := reflect.ValueOf(p).MethodByName("Closed").Call(nil)[0].Bool(); !closed {
			t.Errorf("%s closes the sentence", word)
		}
		if fresh := reflect.ValueOf(made()).MethodByName("Closed").Call(nil)[0].Bool(); fresh {
			t.Errorf("a door is not closed before %s", word)
		}
		covered[field] = true
		key := jsName(word)
		if built := build(value.Object().Set(key, marker)); !built.IsInvalid() || !strings.Contains(built.Error().Error(), "is the closing word") {
			t.Errorf("{ %s } must be refused, the closing word is no key: %v", key, built.Error())
		}
	}
	var keys []string // tên JS của mọi chữ (trừ động từ): khoá dạng object nhận
	for _, m := range wordsOf(made()) {
		if slices.Contains(verbs, m.Name) {
			continue
		}
		p := made()
		first, second := value.String("first:"+m.Name), value.String("second:"+m.Name)
		if result := call(p, m, first); result.IsInvalid() {
			t.Errorf("%s: %v", m.Name, result.Error())
			continue
		}
		fields := holding(fieldsOf(p), first)
		if len(fields) != 1 {
			t.Errorf("%s writes %d fields %v, want exactly one", m.Name, len(fields), fields)
			continue
		}
		field := fields[0]
		want := m.Name
		if first, ok := aliases[m.Name]; ok {
			want = first
		}
		if field != want && field != want+"s" {
			t.Errorf("%s writes %s: the word and its field do not match", m.Name, field)
		}
		covered[field] = true

		// dạng object: website({ guard: x }) ghi đúng trường mà chữ guard ghi
		key := jsName(m.Name)
		if _, ok := aliases[m.Name]; !ok {
			keys = append(keys, key)
		}
		built := build(value.Object().Set(key, first))
		if built.IsInvalid() {
			t.Errorf("{ %s }: %v", key, built.Error())
		} else if got := holding(fieldsOf(built.Struct()), first); len(got) != 1 || got[0] != field {
			t.Errorf("{ %s } writes %v, the word %s writes %s", key, got, m.Name, field)
		}

		call(p, m, second)
		f, _ := fieldsOf(p).Type().FieldByName(field)
		now := fieldsOf(p).FieldByIndex(f.Index).Interface()
		switch now := now.(type) {
		case value.Value:
			if !now.Identical(second) {
				t.Errorf("%s: a later write wins", m.Name)
			}
		case []value.Value:
			if len(now) != 2 || !now[0].Identical(first) || !now[1].Identical(second) {
				t.Errorf("%s adds at the end, in the order written: %d items", m.Name, len(now))
			}
		}
	}
	fields := fieldsOf(made())
	for i := range fields.NumField() {
		name, kind := fields.Type().Field(i).Name, fields.Type().Field(i).Type
		if (kind == valueType || kind == listType) && !covered[name] {
			t.Errorf("field %s has no word writing it", name)
		}
	}

	// lời báo của một khoá lạ nêu đúng các khoá nhận được: không thiếu, không thừa
	unknown := build(value.Object().Set("nope", value.Null()))
	if !unknown.IsInvalid() {
		t.Error(`{ nope } must be refused`)
	} else {
		message := unknown.Error().Error()
		listed := strings.Split(message[strings.LastIndex(message, ": ")+2:], ", ")
		slices.Sort(listed)
		slices.Sort(keys)
		if !slices.Equal(listed, keys) {
			t.Errorf("the keys a refusal lists %v, the keys taken %v", listed, keys)
		}
	}

	// sau khi engine nạp (Share), mọi chữ là lỗi và không ghi gì
	sealed := made()
	sealed.(value.Sharer).Share()
	for _, m := range wordsOf(sealed) {
		marker := value.String("after:" + m.Name)
		var result value.Value
		if slices.Contains(verbs, m.Name) {
			result = call(sealed, m, value.Undefined())
		} else {
			result = call(sealed, m, marker)
		}
		if !result.IsInvalid() || !strings.Contains(result.Error().Error(), "was loaded by the engine and is read only now") {
			t.Errorf("%s after Share: %v", m.Name, result.Error())
		}
		if len(holding(fieldsOf(sealed), marker)) != 0 {
			t.Errorf("%s after Share wrote a field", m.Name)
		}
	}
}

func TestWebsiteVocabulary(t *testing.T) {
	build := func(options value.Value) value.Value { return website.New(alone(), options) }
	checkVocabulary(t, func() any { return websiteOf(t, website.New(alone())) }, build, nil, closing{"Listen": "Listen", "Port": "Listen"})
}

func TestDatabaseVocabulary(t *testing.T) {
	made := func() any {
		d, _ := value.As[database.Database](database.New(alone()))
		return d
	}
	build := func(options value.Value) value.Value { return database.New(alone(), options) }
	checkVocabulary(t, made, build, nil, closing{"Open": "Open"})
}

func TestScheduleVocabulary(t *testing.T) {
	made := func() any {
		s, _ := value.As[schedule.Schedule](schedule.New(alone()))
		return s
	}
	build := func(options value.Value) value.Value { return schedule.New(alone(), options) }
	checkVocabulary(t, made, build, nil, closing{"Run": "Folders"})
}

func TestCommandVocabulary(t *testing.T) {
	made := func() any {
		c, _ := value.As[command.Command](command.New(alone()))
		return c
	}
	build := func(options value.Value) value.Value { return command.New(alone(), options) }
	checkVocabulary(t, made, build, nil, closing{"Execute": "Handle"})
}

func TestApplicationVocabulary(t *testing.T) {
	made := func() any {
		a, _ := value.As[application.Application](application.New(value.Undefined()))
		return a
	}
	build := func(options value.Value) value.Value { return application.New(options) }
	// bốn chữ mở câu của một cửa là động từ ở đây: chúng dựng cửa và ghi nó vào trường cùng tên (kiểm ở dưới), không
	// nhận một giá trị để giữ
	doors := []string{"Website", "Database", "Schedule", "Command"}
	checkVocabulary(t, made, build, append([]string{"Run", "Check"}, doors...), nil, doors...)

	// hai động từ: mỗi cái đặt đúng dấu của nó
	for _, verb := range []string{"Run", "Check"} {
		a := made().(*application.Application)
		m, _ := reflect.TypeOf(a).MethodByName(verb)
		call(a, m, value.Undefined())
		if a.Running() != (verb == "Run") || a.Checking() != (verb == "Check") {
			t.Errorf("%s sets its own mark only: run %v, check %v", verb, a.Running(), a.Checking())
		}
	}

	// mỗi chữ cửa dựng đúng cửa của nó, ghi vào đúng trường cùng tên, và trả về cửa ấy
	for _, door := range doors {
		a := made().(*application.Application)
		m, _ := reflect.TypeOf(a).MethodByName(door)
		built := call(a, m, value.Undefined())
		if built.IsInvalid() || !built.IsStruct() {
			t.Errorf("app.%s(): %v", jsName(door), built.Error())
			continue
		}
		fields := reflect.ValueOf(a.Fields())
		for i := range fields.NumField() {
			field, ok := fields.Field(i).Interface().(value.Value)
			if !ok {
				continue
			}
			name := fields.Type().Field(i).Name
			if (name == door) != field.Identical(built) {
				t.Errorf("app.%s() and the field %s", jsName(door), name)
			}
		}
		want := "*" + strings.ToLower(door) + "." + door
		if got := reflect.TypeOf(built.Struct()).String(); got != want {
			t.Errorf("app.%s() builds %s, want %s", jsName(door), got, want)
		}
	}
}

// jsName là tên JS của một tên Go: toàn chữ hoa thì viết thường hết (SSL → ssl), còn lại viết thường chữ đầu.
func jsName(name string) string {
	if strings.ToUpper(name) == name {
		return strings.ToLower(name)
	}
	return strings.ToLower(name[:1]) + name[1:]
}
