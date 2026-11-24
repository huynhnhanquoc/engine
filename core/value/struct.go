package value

import (
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// Assign đặt từng khoá của object options vào target bằng phương thức cùng tên của *T, theo thứ tự Keys():
//
//   - phương thức func(*T, Value) Value nhận thẳng giá trị của khoá (Certificates, Memory, SSL…);
//   - phương thức func(*T, ...Value) Value nhận các phần tử khi giá trị là mảng, hay chính giá trị ấy khi không phải
//     mảng (Listen, Guard, Success, Error, Finally…);
//   - phương thức có chữ ký khác là của Go, chương trình không chạm tới;
//   - khoá không có phương thức nào là lỗi nêu mọi khoá *T nhận.
//
// Tên khoá là tên phương thức viết thường chữ đầu (Certificates → certificates, ChannelBinding → channelBinding), hay
// viết thường cả chữ khi toàn chữ hoa (SSL → ssl). Bảng phương thức của *T chỉ dựng một lần; mỗi lần gọi sau là lời gọi
// hàm Go trực tiếp, không qua reflect.
func Assign[T any](target *T, options Value) Value {
	table := tableFor[T]()
	if target == nil {
		return Invalid("cannot assign to nil %s", table.name)
	}
	if !options.IsObject() {
		return Invalid("%s takes an object, got %s", table.name, options.Typeof())
	}
	for _, key := range options.Keys() {
		setter, ok := table.setters[key]
		if !ok {
			return Invalid("%s has no key %q: %s", table.name, key, table.keys)
		}
		item, _ := options.Get(key)
		if result := setter(target, item); result.IsInvalid() {
			return result
		}
	}
	return Struct(target)
}

// structTable là bảng các phương thức đặt khoá của một loại *T.
type structTable[T any] struct {
	name    string
	keys    string
	setters map[string]func(*T, Value) Value
}

// structTables giữ bảng của mỗi loại *T đã gặp.
var structTables sync.Map

func tableFor[T any]() *structTable[T] {
	pointer := reflect.TypeFor[*T]()
	if cached, ok := structTables.Load(pointer); ok {
		return cached.(*structTable[T])
	}
	table := buildTable[T](pointer)
	actual, _ := structTables.LoadOrStore(pointer, table)
	return actual.(*structTable[T])
}

func buildTable[T any](pointer reflect.Type) *structTable[T] {
	setters := make(map[string]func(*T, Value) Value)
	var names []string
	for i := range pointer.NumMethod() {
		method := pointer.Method(i)
		if !method.Func.IsValid() {
			continue
		}
		key := lowerName(method.Name)
		switch fn := method.Func.Interface().(type) {
		case func(*T, Value) Value:
			setters[key] = fn
			names = append(names, key)
		case func(*T, ...Value) Value:
			setters[key] = func(target *T, item Value) Value {
				if item.IsArray() {
					return fn(target, item.Items()...)
				}
				return fn(target, item)
			}
			names = append(names, key)
		}
	}
	return &structTable[T]{
		name:    lowerName(pointer.Elem().Name()),
		keys:    strings.Join(names, ", "),
		setters: setters,
	}
}

// lowerName đổi tên Go sang tên khoá JS: toàn chữ hoa thì viết thường hết (SSL → ssl), còn lại viết thường chữ đầu
// (Certificates → certificates, VersionName → versionName).
func lowerName(s string) string {
	if s == "" {
		return ""
	}
	allUpper := true
	for _, r := range s {
		if unicode.IsLower(r) {
			allUpper = false
			break
		}
	}
	if allUpper {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
