package javascript

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Object của JavaScript: các hàm tĩnh đọc và ghi khoá (keys, values, entries, fromEntries, assign, hasOwn, is,
// groupBy, freeze, isFrozen), theo JavaScript, đối chiếu với node. Khoá riêng của một mảng và một chuỗi là các chỉ số của nó (một chuỗi
// tính theo đơn vị UTF-16, như "😀" có hai); loại khác (số, thời điểm, hàm…) không có khoá. Chỗ Kitwork khác: Object
// không gọi được (Object(x)); Object.assign không bọc một số, một chuỗi, một boolean thành object, và không ghi khoá
// lên thứ không giữ khoá (mảng, thời điểm, hàm); Object.isFrozen của thứ Kitwork không bao giờ ghi khoá lên (thời điểm,
// hàm, struct…) là lỗi, vì JavaScript để chúng mở. Chép từ engine2 (core/builtins/builtins.go).
var objectNamespace = newNamespace("Object",
	member{"assign", native("assign", 2, objectAssign)},
	member{"hasOwn", native("hasOwn", 2, objectHasOwn)},
	member{"is", native("is", 2, func(_ Run, arguments []value.Value) value.Value {
		return value.Boolean(SameValue(argument(arguments, 0), argument(arguments, 1)))
	})},
	member{"freeze", native("freeze", 1, objectFreeze)},
	member{"isFrozen", native("isFrozen", 1, objectIsFrozen)},
	member{"keys", native("keys", 1, objectKeys)},
	member{"entries", native("entries", 1, objectEntries)},
	member{"fromEntries", native("fromEntries", 1, objectFromEntries)},
	member{"values", native("values", 1, objectValues)},
	member{"groupBy", native("groupBy", 2, objectGroupBy)},
)

// errNullishObject là TypeError của JavaScript khi một hàm của Object cần một object và nhận null hay undefined.
var errNullishObject = value.Errorf(value.TypeCode, "cannot convert undefined or null to object")

// ownEntries là các khoá riêng của v và giá trị của chúng, như Object.keys, values, entries đi qua: khoá của một object
// theo thứ tự của nó; chỉ số của một mảng; chỉ số của một chuỗi, mỗi đơn vị UTF-16 là một chuỗi; không có gì cho loại
// khác. null và undefined là lỗi, như JavaScript; một struct của engine cũng vậy: nó không phải dữ liệu.
func ownEntries(run Run, v value.Value) ([]string, []value.Value, error) {
	switch {
	case v.HasHoles():
		return nil, nil, holesRefused("Object.keys(a), values, entries, assign") // lỗ không phải khoá của JavaScript
	case v.IsNullish():
		return nil, nil, errNullishObject
	case v.IsStruct():
		return nil, nil, notData(v)
	case v.IsObject():
		keys := v.Keys()
		if failure := spend(run, uint64(len(keys))*keyUnits); failure.IsInvalid() {
			return nil, nil, failure.Error()
		}
		items := make([]value.Value, len(keys))
		for i, key := range keys {
			items[i], _ = v.Get(key)
		}
		return keys, items, nil
	case v.IsArray():
		items := v.Items()
		keys := make([]string, len(items))
		for i := range items {
			keys[i] = strconv.Itoa(i)
		}
		return keys, items, nil
	case v.IsString():
		us := units(v.String())
		keys, items := make([]string, len(us)), make([]value.Value, len(us))
		for i, unit := range us {
			keys[i], items[i] = strconv.Itoa(i), value.String(unitString(unit))
		}
		return keys, items, nil
	}
	return nil, nil, nil
}

// objectKeys là Object.keys(v).
func objectKeys(run Run, arguments []value.Value) value.Value {
	keys, _, err := ownEntries(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	out := make([]value.Value, len(keys))
	for i, key := range keys {
		out[i] = value.String(key)
	}
	return value.ArrayOf(out)
}

// objectValues là Object.values(v).
func objectValues(run Run, arguments []value.Value) value.Value {
	_, items, err := ownEntries(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	return value.ArrayOf(items)
}

// objectEntries là Object.entries(v): mỗi khoá một mảng [khoá, giá trị].
func objectEntries(run Run, arguments []value.Value) value.Value {
	keys, items, err := ownEntries(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	pairs := uint64(len(keys)) * (value.MadeBytes + 2*value.ItemBytes) // mỗi khoá một mảng hai phần tử, bên trong kết quả
	if failure := room(run, pairs+uint64(len(keys))*value.ItemBytes); failure.IsInvalid() {
		return failure
	}
	if failure := spend(run, uint64(len(keys))*pairUnits); failure.IsInvalid() {
		return failure
	}
	out := make([]value.Value, len(keys))
	for i, key := range keys {
		out[i] = value.Array(value.String(key), items[i])
	}
	if failure := made(run, pairs); failure.IsInvalid() {
		return failure
	}
	return value.ArrayOf(out)
}

// objectAssign là Object.assign(target, …sources): khoá riêng của từng nguồn, theo thứ tự, ghi lên target rồi trả
// target; nguồn null hay undefined bị bỏ qua. target null hay undefined là lỗi, như JavaScript. target là một số, một
// chuỗi, một boolean là lỗi: JavaScript bọc nó thành object, Kitwork không có object bọc. target là thứ khác không giữ
// khoá (mảng, thời điểm, hàm) mà có nguồn ghi khoá lên nó là lỗi; không có gì để ghi thì trả nó như JavaScript.
func objectAssign(run Run, arguments []value.Value) value.Value {
	target := argument(arguments, 0)
	sources := arguments[min(1, len(arguments)):]
	switch {
	case target.IsNullish():
		return value.Error(errNullishObject)
	case target.IsNumber(), target.IsString(), target.IsBoolean():
		return typeError("Object.assign cannot write keys on a %s: Kitwork has no object wrapping one", target.Typeof())
	}
	for _, source := range sources {
		if target.IsObject() {
			if r := Spread(run, target, source, 0); r.IsInvalid() {
				return r
			}
			continue
		}
		if source.IsNullish() {
			continue
		}
		keys, _, err := ownEntries(run, source)
		if err != nil {
			return value.Error(err)
		}
		if len(keys) > 0 {
			return typeError("Object.assign cannot write keys on %s: only an object holds keys", describeKind(target))
		}
	}
	return target
}

// Spread viết các khoá riêng của source vào object target rồi trả target: CopyDataProperties của JavaScript, việc của
// { ...source } và của mỗi nguồn Object.assign. source null hay undefined không có gì; một object viết khoá của nó
// (value.Assign: target chưa có khoá thì mượn khoá của source, chỉ chép giá trị, với chỗ cho room giá trị nữa); một mảng,
// một chuỗi viết các chỉ số của nó (ownEntries); một struct của engine, một mảng có lỗ là lỗi; loại khác không có khoá.
// target đã đóng băng mà source có khoá là lỗi ở khoá đầu tiên, như JavaScript. Bộ đếm byte là của bên gọi (MAKE, hay
// counted của một lời gọi Object.assign).
func Spread(run Run, target, source value.Value, room int) value.Value {
	if source.IsObject() {
		n := uint64(source.Count())
		units := n * writeKeyUnits
		if target.Count() == 0 && !target.IsFrozen() {
			units = n * copyKeyUnits // mượn khoá: chỉ chép giá trị
		}
		if failure := spend(run, units); failure.IsInvalid() {
			return failure
		}
		return target.Assign(source, room)
	}
	if source.IsNullish() {
		return target
	}
	keys, items, err := ownEntries(run, source)
	if err != nil {
		return value.Error(err)
	}
	if failure := spend(run, uint64(len(keys))*writeKeyUnits); failure.IsInvalid() {
		return failure
	}
	for i, key := range keys {
		if written := target.Set(key, items[i]); written.IsInvalid() {
			return written // target đã đóng băng: JavaScript ném ở khoá đầu tiên nó ghi
		}
	}
	return target
}

// objectFreeze là Object.freeze(v): object và mảng đóng băng (nông, value.Freeze) rồi trả chính nó; giá trị khác trả
// nguyên, như JavaScript trả một giá trị nguyên thuỷ. bytes có phần tử là lỗi như JavaScript (một Uint8Array có phần tử
// không đóng băng được). Thời điểm, khoảng thời gian, hàm, struct của engine: Kitwork không ghi khoá lên chúng, không có
// gì để đóng băng.
func objectFreeze(_ Run, arguments []value.Value) value.Value {
	v := argument(arguments, 0)
	if v.IsBytes() && len(v.Bytes()) > 0 {
		return typeError("cannot freeze array buffer views with elements")
	}
	return v.Freeze()
}

// objectIsFrozen là Object.isFrozen(v): object và mảng là dấu của chúng; giá trị nguyên thuỷ là true, như JavaScript.
// Dữ liệu dùng chung (value.FreezeShared, thứ boot dựng) là lỗi: Kitwork đóng băng nó, JavaScript để nó mở.
// Thời điểm, khoảng thời gian, bytes, hàm là lỗi: Kitwork không ghi khoá lên chúng, JavaScript để chúng mở
// (Object.isFrozen(new Date()) là false), nên không câu trả lời nào đúng ở cả hai. Một struct của engine (Math) không
// phải dữ liệu.
func objectIsFrozen(_ Run, arguments []value.Value) value.Value {
	v := argument(arguments, 0)
	switch {
	case v.IsShared():
		return typeError("Object.isFrozen: Kitwork froze this %s because every run shares it (it was made as the site "+
			"booted), where JavaScript leaves it open, so neither answer is true of both", kindName(v))
	case v.IsObject(), v.IsArray():
		return value.Boolean(v.IsFrozen())
	case v.IsNullish(), v.IsNumber(), v.IsBoolean(), v.IsString():
		return value.Boolean(true)
	case v.IsStruct():
		return value.Error(fmt.Errorf("Object.isFrozen: %w", notData(v)))
	}
	kind := "a time"
	switch {
	case v.IsDuration():
		kind = "a duration"
	case v.IsBytes():
		kind = "bytes"
	case v.IsFunction():
		kind = "a function"
	}
	return typeError("Object.isFrozen: Kitwork never writes keys on %s and JavaScript leaves it open, so neither answer is true of both: ask it of an object or an array", kind)
}

// describeKind là loại của v có mạo từ, cho lời báo: "an array", "a function".
func describeKind(v value.Value) string {
	name := kindName(v)
	if name == "array" || name == "object" || name == "undefined" {
		return "an " + name
	}
	return "a " + name
}

// objectHasOwn là Object.hasOwn(v, key): v null hay undefined là lỗi trước khi key được đổi thành chữ, như JavaScript.
func objectHasOwn(run Run, arguments []value.Value) value.Value {
	v := argument(arguments, 0)
	if v.IsNullish() {
		return value.Error(errNullishObject)
	}
	if v.HasHoles() {
		return value.Error(holesRefused("Object.hasOwn(a, i)")) // lỗ không phải khoá của JavaScript
	}
	key, err := toText(run, argument(arguments, 1))
	if err != nil {
		return value.Error(err)
	}
	return value.Boolean(hasOwn(v, key))
}

// objectFromEntries là Object.fromEntries(entries): entries là một mảng, mỗi phần tử là một mảng hay một object có
// phần tử 0 (khoá, đổi thành chữ) và 1 (giá trị). Thứ khác là lỗi, như TypeError của JavaScript: không đi qua được,
// hay một phần tử không phải object. Chuỗi đi qua được nhưng mỗi ký tự của nó không phải object: chỉ "" cho {}.
func objectFromEntries(run Run, arguments []value.Value) value.Value {
	iterable := argument(arguments, 0)
	switch {
	case iterable.IsString():
		if points := codePoints(iterable.String()); len(points) > 0 {
			return typeError("iterator value %q is not an entry object", points[0].String())
		}
		return value.Object()
	case !iterable.IsArray():
		return typeError("%s is not iterable", kindName(iterable))
	}
	out := value.Object()
	for i := 0; i < iterable.Length(); i++ {
		var k, v value.Value
		switch entry := iterable.Index(i); {
		case entry.IsArray():
			k, v = entry.Index(0), entry.Index(1)
		case entry.IsObject():
			k, _ = entry.Get("0")
			v, _ = entry.Get("1")
		default:
			return typeError("iterator value %s is not an entry object", kindName(entry))
		}
		key, err := toText(run, k)
		if err != nil {
			return value.Error(err)
		}
		out.Set(key, v)
	}
	return out
}

// objectGroupBy là Object.groupBy(items, callback): một object mà khoá là thứ callback(phần tử, chỉ số) cho, đổi thành
// chữ như JavaScript đổi một khoá, mỗi khoá giữ mảng các phần tử của nó theo thứ tự. items là một mảng (đọc dần, nên
// phần tử callback thêm vào cũng được đi qua, như JavaScript) hay một chuỗi (từng ký tự); thứ khác không đi qua được.
func objectGroupBy(run Run, arguments []value.Value) value.Value {
	items, callback := argument(arguments, 0), argument(arguments, 1)
	if items.IsNullish() {
		return value.Error(errNullishObject)
	}
	if !callback.IsFunction() {
		return value.Error(errNotAFunction(callback))
	}
	var walked []value.Value
	switch {
	case items.IsArray():
	case items.IsString():
		if failure := room(run, uint64(utf8.RuneCountInString(items.String()))*value.ItemBytes); failure.IsInvalid() {
			return failure
		}
		walked = codePoints(items.String())
	default:
		return typeError("%s is not iterable", kindName(items))
	}
	groups := value.Object()
	defer letGo(run, keep(run, Kept{Items: &[]value.Value{groups}})) // các nhóm đã làm: runtime thấy chúng khi callback chạy
	buffer := make([]value.Value, 2)
	for i := 0; ; i++ {
		var item value.Value
		if items.IsArray() {
			if i >= items.Length() {
				return groups
			}
			item = items.Index(i)
		} else {
			if i >= len(walked) {
				return groups
			}
			item = walked[i]
		}
		got := call(run, callback, callArguments(buffer, item, value.Number(float64(i)))...)
		if got.IsInvalid() {
			return got
		}
		key, err := toText(run, got)
		if err != nil {
			return value.Error(err)
		}
		if group, found := groups.Get(key); found {
			group.Push(item)
		} else {
			groups.Set(key, value.Array(item))
		}
	}
}

// codePoints là các ký tự của s như for…of đi qua một chuỗi: một cặp là một ký tự, một nửa cặp đứng một mình là một ký
// tự của riêng nó.
func codePoints(s string) []value.Value {
	var out []value.Value
	for i := 0; i < len(s); {
		size := 3
		if _, lone := loneSurrogateAt(s, i); !lone {
			_, size = utf8.DecodeRuneInString(s[i:])
		}
		out = append(out, value.String(s[i:i+size]))
		i += size
	}
	return out
}

// notData là lỗi của một struct của engine dùng như dữ liệu (Object.keys, JSON.stringify): một Namespace thì nêu tên.
func notData(v value.Value) error {
	if ns, ok := value.As[Namespace](v); ok {
		return value.Errorf(value.TypeCode, "%s is a library, not data", ns.name)
	}
	return value.Errorf(value.TypeCode, "a struct of the engine is not data")
}
