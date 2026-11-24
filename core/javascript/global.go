package javascript

import (
	"slices"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Tên toàn cục của JavaScript mà chương trình có sẵn, không import. Math, JSON, Object là một Namespace (không gọi
// được); Number, String, Array, Boolean là một hàm (Native) gọi được và dùng làm hàm gọi lại được (arr.map(Number),
// arr.filter(Boolean)), có thêm các khoá tĩnh của nó (Number.isInteger, Array.from); parseInt, parseFloat, isNaN,
// isFinite, encodeURIComponent, decodeURIComponent, encodeURI, decodeURI là hàm; console là một Namespace viết ra
// os.Stdout, host thay bằng Console(w, lead) của từng site; Date cho thời điểm, đọc giờ địa phương theo múi của nơi
// chạy (Located). Tất cả dựng một lần khi engine chạy và chỉ đọc: chương trình không thêm, sửa, xoá được khoá nào
// (Math.x = 1 là lỗi, như TypeScript báo), nên mọi lượt chạy của mọi app đọc chung một bộ mà không app nào ghi được
// sang app khác, không cần cờ đóng băng. Number.parseInt là chính parseInt (=== là true), như JavaScript.
//
// Chỗ Kitwork khác JavaScript: Object là một Namespace, không gọi được (Object(x) bọc một giá trị nguyên thuỷ thành
// object, Kitwork không có object bọc); Math, JSON, Object không phải dữ liệu (JSON.stringify(Math), Object.keys(Math)
// là lỗi, JavaScript cho "{}" và []).

// Native là một hàm có sẵn (Math.abs, JSON.stringify, Object.keys) trong value.Function. Nó không đọc this: các hàm
// này của JavaScript cũng không đọc.
type Native struct {
	name    string // f.name của JavaScript: "abs"
	length  int    // f.length: số đối số khai báo
	run     func(run Run, arguments []value.Value) value.Value
	statics *Namespace // khoá tĩnh của Number, String, Array (Number.isInteger); nil với hàm khác
}

// Name là f.name: "abs".
func (n *Native) Name() string { return n.name }

// Length là f.length: số đối số hàm khai báo (Math.max là 2, như JavaScript).
func (n *Native) Length() int { return n.length }

// Call gọi n với các đối số. run để n gọi lại hàm của chương trình (hàm gọi lại của Object.groupBy, reviver của
// JSON.parse, valueOf riêng của một object); nil được khi không có gì cần gọi lại. Một đối số là lỗi thì lỗi ấy là kết
// quả, như Invoke.
func (n *Native) Call(run Run, arguments ...value.Value) value.Value {
	for _, argument := range arguments {
		if argument.IsInvalid() {
			return argument
		}
	}
	meter, _ := run.(Meter)
	return n.metered(run, meter, arguments)
}

// metered là n với các đối số (đã biết không lỗi), đếm cái nó làm ra trên meter (nil: không đếm). Invoke đưa meter nó
// đã có, không ép kiểu run lần nữa.
func (n *Native) metered(run Run, meter Meter, arguments []value.Value) value.Value {
	if meter == nil {
		return n.run(run, arguments)
	}
	first := 0
	if len(arguments) > 0 {
		first = arguments[0].Count()
	}
	return counted(meter, n.run(run, arguments), value.Value{}, arguments, 0, first)
}

// native là value.Function của một Native.
func native(name string, length int, run func(run Run, arguments []value.Value) value.Value) value.Value {
	return value.Function(&Native{name: name, length: length, run: run})
}

// constructor là một hàm có sẵn có khoá tĩnh: Number, String, Array, Boolean.
func constructor(name string, length int, run func(run Run, arguments []value.Value) value.Value, statics ...member) value.Value {
	return value.Function(&Native{name: name, length: length, run: run, statics: newNamespace(name, statics...)})
}

// Members là các khoá của một tên toàn cục v: của Math, JSON, Object, hay các khoá tĩnh của Number, String, Array,
// Boolean (Number.isInteger, Number.MAX_SAFE_INTEGER). nil khi v không phải: runtime đọc Math.PI, Number.isInteger
// qua nó.
func Members(v value.Value) *Namespace {
	if ns, ok := value.As[Namespace](v); ok {
		return ns
	}
	if n, ok := v.Function().(*Native); ok {
		return n.statics
	}
	return nil
}

// Namespace là một tên toàn cục: các khoá theo thứ tự node liệt kê, mỗi khoá một hàm (Native) hay một hằng (Math.PI).
// Chỉ đọc sau khi dựng.
type Namespace struct {
	name    string
	keys    []string
	members []value.Value
	index   map[string]int // khoá → chỗ: Math có hơn bốn mươi khoá, Math.max mỗi lần duyệt chúng là chậm; chỉ đọc sau khi dựng
}

// member là một khoá của Namespace lúc dựng.
type member struct {
	key  string
	item value.Value
}

func newNamespace(name string, members ...member) *Namespace {
	ns := &Namespace{name: name, keys: make([]string, len(members)), members: make([]value.Value, len(members)),
		index: make(map[string]int, len(members))}
	for i, m := range members {
		ns.keys[i], ns.members[i] = m.key, m.item
		ns.index[m.key] = i
	}
	return ns
}

// Name là tên toàn cục: "Math".
func (ns *Namespace) Name() string { return ns.name }

// Get là khoá key của ns (Math.PI, Math.abs), và có khoá ấy hay không.
func (ns *Namespace) Get(key string) (value.Value, bool) {
	if i, found := ns.index[key]; found {
		return ns.members[i], true
	}
	return value.Value{}, false
}

// Keys là các khoá của ns theo thứ tự node liệt kê; bản chép.
func (ns *Namespace) Keys() []string { return slices.Clone(ns.keys) }

// globals là mọi tên toàn cục, dựng một lần.
var globals = map[string]value.Value{
	"Math":       value.Struct(mathNamespace),
	"JSON":       value.Struct(jsonNamespace),
	"Object":     value.Struct(objectNamespace),
	"Number":     numberFunction,
	"String":     stringFunction,
	"Array":      arrayFunction,
	"Boolean":    booleanFunction,
	"parseInt":   parseIntFunction,
	"parseFloat": parseFloatFunction,
	"isNaN":      isNaNFunction,
	"isFinite":   isFiniteFunction,

	"encodeURIComponent": encodeURIComponentFunction,
	"decodeURIComponent": decodeURIComponentFunction,
	"encodeURI":          encodeURIFunction,
	"decodeURI":          decodeURIFunction,
	"console":            consoleNamespace,
	"Date":               dateFunction,
}

// Global là tên toàn cục name, và có tên ấy hay không: value.Struct của một *Namespace (Math), hay value.Function của
// một *Native (Number, parseInt). Mỗi lần gọi trả cùng một giá trị: runtime đặt nó vào chỗ của tên khi dựng VM.
func Global(name string) (value.Value, bool) {
	g, ok := globals[name]
	return g, ok
}

// GlobalNames là các tên Global biết, xếp theo chữ cái: để compiler biết một tên chương trình không khai báo.
func GlobalNames() []string {
	names := make([]string, 0, len(globals))
	for name := range globals {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
