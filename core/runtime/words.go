package runtime

import (
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Chữ JS của một struct engine (blueprint/README.md): phương thức chữ hoa mà mọi tham số và kết quả là value.Value
// (một tham số ...value.Value là một danh sách), tên viết thường chữ đầu (SSL, TLS toàn chữ hoa thì viết thường hết).
// Gọi qua reflect: chỉ chạy lúc boot (application.work), nơi reflect chấp nhận được; hàm của request sẽ gọi thẳng.

type word struct {
	index      int
	parameters int  // không kể receiver
	variadic   bool // tham số cuối là ...value.Value
}

type vocabulary struct {
	door  string // tên của struct cho lời báo: "website"
	words map[string]word
	names []string // các chữ, xếp theo abc
}

var (
	valueType   = reflect.TypeFor[value.Value]()
	listType    = reflect.TypeFor[[]value.Value]()
	vocabularys sync.Map // reflect.Type → *vocabulary
)

func vocabularyOf(t reflect.Type) *vocabulary {
	if v, ok := vocabularys.Load(t); ok {
		return v.(*vocabulary)
	}
	name := t.String()
	if t.Kind() == reflect.Pointer {
		name = t.Elem().Name()
	}
	v := &vocabulary{door: jsName(name), words: map[string]word{}}
	for i := range t.NumMethod() {
		m := t.Method(i)
		in := m.Type
		if in.NumOut() != 1 || in.Out(0) != valueType || in.NumIn() < 2 {
			continue
		}
		ok := true
		for j := 1; j < in.NumIn(); j++ {
			last := j == in.NumIn()-1
			if in.In(j) != valueType && !(last && in.IsVariadic() && in.In(j) == listType) {
				ok = false
			}
		}
		if ok {
			v.words[jsName(m.Name)] = word{index: i, parameters: in.NumIn() - 1, variadic: in.IsVariadic()}
			v.names = append(v.names, jsName(m.Name))
		}
	}
	slices.Sort(v.names)
	actual, _ := vocabularys.LoadOrStore(t, v)
	return actual.(*vocabulary)
}

// jsName là tên JS của một tên Go: toàn chữ hoa thì viết thường hết (SSL → ssl), còn lại viết thường chữ đầu.
func jsName(name string) string {
	if strings.ToUpper(name) == name {
		return strings.ToLower(name)
	}
	return strings.ToLower(name[:1]) + name[1:]
}

// callWord là this.name(arguments…) trên một struct engine: thiếu đối số là undefined, thừa thì bỏ, như JavaScript.
func callWord(this value.Value, name string, arguments []value.Value) value.Value {
	receiver := reflect.ValueOf(this.Struct())
	v := vocabularyOf(receiver.Type())
	w, found := v.words[name]
	if !found {
		return value.Fail(value.TypeCode, "%s has no word %q: %s", v.door, name, strings.Join(v.names, ", "))
	}
	method := receiver.Method(w.index)
	var in []reflect.Value
	if w.variadic {
		fixed := w.parameters - 1
		for i := range fixed {
			in = append(in, reflect.ValueOf(argumentAt(arguments, i)))
		}
		for i := fixed; i < len(arguments); i++ {
			in = append(in, reflect.ValueOf(arguments[i]))
		}
	} else {
		for i := range w.parameters {
			in = append(in, reflect.ValueOf(argumentAt(arguments, i)))
		}
	}
	return method.Call(in)[0].Interface().(value.Value)
}

// readWord là this.name không gọi: một hàm gọi chữ ấy trên this, như JavaScript cho một phương thức đọc ra.
func readWord(this value.Value, name string) value.Value {
	v := vocabularyOf(reflect.TypeOf(this.Struct()))
	if _, found := v.words[name]; !found {
		return value.Fail(value.TypeCode, "%s has no word %q: %s", v.door, name, strings.Join(v.names, ", "))
	}
	return javascript.NewNative(name, 0, func(_ javascript.Run, arguments []value.Value) value.Value {
		return callWord(this, name, arguments)
	})
}

// hasWord là struct this có chữ name.
func hasWord(this value.Value, name string) bool {
	_, found := vocabularyOf(reflect.TypeOf(this.Struct())).words[name]
	return found
}

// doorOf là tên của struct this cho lời báo: "environment", "website".
func doorOf(this value.Value) string { return vocabularyOf(reflect.TypeOf(this.Struct())).door }

func argumentAt(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}
