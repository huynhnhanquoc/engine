// Package javascript là luật của JavaScript trên value.Value: các phương thức theo loại ("a".toUpperCase(), [1].map(f),
// (1.5).toFixed(1)), phép chuyển đổi (ToString, ToNumber, ToBoolean) và phép so bằng (===, Object.is, SameValueZero),
// và các tên toàn cục: Math, JSON, Object, Number, String, Array, Boolean, parseInt, parseFloat, isNaN, isFinite
// (global.go: Global, Members, Namespace, Native). value chỉ giữ dữ liệu; runtime (VM) gọi vào đây; ở đây không biết
// VM, chỉ biết Run để gọi lại một hàm của chương trình. Mỗi luật theo JavaScript đúng từng chữ, đối chiếu với node
// (test/javascript). Lời báo lỗi tiếng Anh.
package javascript

import (
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Run gọi một hàm của chương trình: runtime cài nó. Phương thức nhận hàm gọi lại (map, sort, replace với một hàm…) và
// phép chuyển đổi gặp valueOf, toString riêng của một object gọi qua đây. Kết quả là một lỗi khi hàm ném.
type Run interface {
	Call(function value.Value, arguments ...value.Value) value.Value
}

// Located là phần của Run cho biết múi giờ của nơi đang chạy, mà Date(năm, tháng, …), chữ ngày có giờ không có múi, và
// t.format(…) đọc: runtime cài nó theo nơi chạy (engine2/docs/language.md mục 4.3): một request, job, lịch của một site
// là múi của app (module.timezone), không có thì app.timezone của host, không có thì UTC; module.work, design.work,
// config.work ở gốc app là UTC; application.work và app.commands là múi của máy chạy host. Một Run không có Location,
// hay Location trả nil, là múi của máy chạy host (time.Local).
type Located interface {
	Location() *time.Location
}

// locationOf là múi giờ của nơi run chạy: Location của nó, hay time.Local, đọc ở mỗi lời gọi.
func locationOf(run Run) *time.Location {
	if located, ok := run.(Located); ok {
		if location := located.Location(); location != nil {
			return location
		}
	}
	return time.Local
}

// method là một phương thức của JavaScript: this, các đối số, và run để gọi lại.
type method func(run Run, this value.Value, arguments []value.Value) value.Value

// Invoke là this.name(arguments…): khoá riêng của một object khi nó là hàm, rồi phương thức của loại this, rồi những
// phương thức mọi giá trị có (hasOwnProperty, toString, valueOf). this là một tên toàn cục thì khoá của nó trước:
// Math.max(1, 2), Number.isInteger(5). Một đối số là lỗi thì lỗi ấy là kết quả, như một lỗi đưa vào phép tính đi ra
// lại. Struct khác của engine không qua đây: chữ của chúng là việc của runtime.
func Invoke(run Run, this value.Value, name string, arguments []value.Value) value.Value {
	meter, _ := run.(Meter)
	return InvokeNamed(run, meter, this, nameOf(name), arguments)
}

// InvokeNamed là Invoke với tên đã tra (names.go) và meter của run (nil: không đếm): lệnh INVOKE của runtime gọi nó,
// không băm tên, không ép kiểu run mỗi lần (một phép ép interface là khoảng 40% giá của việc đếm một lời gọi).
func InvokeNamed(run Run, meter Meter, this value.Value, n *Name, arguments []value.Value) value.Value {
	name := n.text
	if this.IsInvalid() {
		return this
	}
	for _, argument := range arguments {
		if argument.IsInvalid() {
			return argument
		}
	}
	if this.IsNullish() {
		return typeError("cannot read properties of %s (reading %q)", kindName(this), name)
	}
	if ns := Members(this); ns != nil {
		if member, found := ns.Get(name); found {
			if !member.IsFunction() {
				return typeError("%s.%s is not a function", ns.name, name)
			}
			return dispatch(run, meter, member, arguments)
		}
		if !this.IsFunction() { // Number.call(…) là phương thức của mọi hàm; Math.call thì không có
			return typeError("%s.%s is not a function", ns.name, name)
		}
	}
	if this.IsObject() {
		if own, found := this.Get(name); found {
			if !own.IsFunction() {
				return typeError("%s is not a function, it is %s", name, kindName(own))
			}
			return dispatch(run, meter, own, arguments)
		}
	}
	if n.holes && this.HasHoles() {
		return value.Error(holesRefused("a." + name + "(…)"))
	}
	if k := kindOf(this); k >= 0 && n.methods[k] != nil {
		return invoke(run, meter, name, n.methods[k], this, arguments)
	}
	if n.every != nil {
		return invoke(run, meter, name, n.every, this, arguments)
	}
	return typeError("%s has no method %q", kindName(this), name)
}

// invoke gọi phương thức name (m) của this, và khi run đếm: đếm cái nó làm ra (meter.go) và tính năng lượng cho việc
// nó làm (work.go): một sort trước khi bắt đầu, việc khác sau khi xong.
func invoke(run Run, meter Meter, name string, m method, this value.Value, arguments []value.Value) value.Value {
	if meter == nil {
		return m(run, this, arguments)
	}
	thisBefore, firstBefore := this.Count(), 0
	if len(arguments) > 0 {
		firstBefore = arguments[0].Count()
	}
	array := this.IsArray()
	if array {
		if work := arrayMethodBefore(name, this, arguments); work > 0 {
			if failure := meter.Spend(work); failure.IsInvalid() {
				return failure
			}
		}
	}
	result := counted(meter, m(run, this, arguments), this, arguments, thisBefore, firstBefore)
	if result.IsInvalid() {
		return result
	}
	var work uint64
	switch {
	case this.IsString():
		work = textMethodWork(name, this.String(), arguments, result)
	case array:
		work = arrayMethodAfter(name, this, arguments, result, thisBefore)
	}
	if work > 0 {
		if failure := meter.Spend(work); failure.IsInvalid() {
			return failure
		}
	}
	return result
}

// dispatch là this.name(…) khi name là một hàm: của một tên toàn cục (Math.max) hay khoá riêng của một object. Chương
// trình gọi nó, như một lệnh CALL: không phải một lời gọi lại từ code của engine (call), nên không tốn callbackUnits.
func dispatch(run Run, meter Meter, function value.Value, arguments []value.Value) value.Value {
	if n, ok := function.Function().(*Native); ok {
		return n.metered(run, meter, arguments)
	}
	if run == nil {
		return value.Invalid("cannot call a function here: no run")
	}
	return run.Call(function, arguments...)
}

// HasMethod là this có phương thức name không, như Invoke tìm: khoá riêng của object, phương thức của loại, phương thức
// mọi giá trị có. Runtime đọc nó để biết `"a".at` là một hàm hay undefined.
func HasMethod(this value.Value, name string) bool {
	if this.IsNullish() || this.IsInvalid() {
		return false
	}
	if ns := Members(this); ns != nil {
		if member, found := ns.Get(name); found {
			return member.IsFunction()
		}
		if !this.IsFunction() {
			return false
		}
	}
	if this.IsObject() {
		if own, found := this.Get(name); found {
			return own.IsFunction()
		}
	}
	_, ofKind := methodsOf(this)[name]
	_, ofEvery := everyMethods[name]
	return ofKind || ofEvery
}

// methodsOf là bảng phương thức của loại this.
func methodsOf(this value.Value) map[string]method {
	switch {
	case this.IsString():
		return stringMethods
	case this.IsNumber():
		return numberMethods
	case this.IsBoolean():
		return booleanMethods
	case this.IsArray():
		return arrayMethods
	case this.IsTime():
		return timeMethods
	case this.IsDuration():
		return durationMethods
	case this.IsBytes():
		return bytesMethods
	case this.IsFunction():
		return functionMethods
	}
	return nil
}

// everyMethods là của Object.prototype, mọi giá trị trừ null và undefined đều có: phương thức cùng tên của loại thắng
// (toString của số viết số).
var everyMethods = map[string]method{
	"hasOwnProperty": hasOwnProperty,
	"toString":       objectToString,
	"valueOf":        func(_ Run, this value.Value, _ []value.Value) value.Value { return this },
}

// hasOwnProperty là this.hasOwnProperty(key): khoá của object; chỉ số và "length" của mảng và chuỗi.
func hasOwnProperty(run Run, this value.Value, arguments []value.Value) value.Value {
	key, err := toText(run, argument(arguments, 0))
	if err != nil {
		return value.Error(err)
	}
	return value.Boolean(hasOwn(this, key))
}

// hasOwn là v có khoá riêng key không: khoá của object; chỉ số và "length" của mảng và chuỗi; khoá của một tên toàn
// cục (Object.hasOwn(Math, "PI")).
func hasOwn(v value.Value, key string) bool {
	switch {
	case v.IsObject():
		_, found := v.Get(key)
		return found
	case v.IsArray():
		return key == "length" || isIndexBelow(key, v.Length())
	case v.IsString():
		return key == "length" || isIndexBelow(key, unitLength(v.String()))
	}
	if ns := Members(v); ns != nil {
		_, found := ns.Get(key)
		return found
	}
	return false
}

// objectToString là Object.prototype.toString của một object: "[object Object]"; loại khác đổi như String(v).
func objectToString(run Run, this value.Value, _ []value.Value) value.Value {
	if this.IsObject() {
		return value.String("[object Object]")
	}
	return ToString(run, this)
}

// isIndexBelow là key viết đúng một chỉ số từ 0 tới dưới length ("01" không phải).
func isIndexBelow(key string, length int) bool {
	if key == "" || len(key) > 10 || (key[0] == '0' && len(key) > 1) {
		return false
	}
	n := 0
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return false
		}
		n = n*10 + int(key[i]-'0')
	}
	return n < length
}

// argument là đối số thứ i, undefined khi bên gọi đưa ít hơn.
func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// call gọi function: một hàm có sẵn (Native) thì gọi thẳng, hàm của chương trình thì qua run; không có run (Go gọi
// thẳng, không VM) là lỗi, không phải panic.
func call(run Run, function value.Value, arguments ...value.Value) value.Value {
	// một lời gọi từ code của engine (map, sort, replace…) tốn hơn một lệnh CALL: giá của nó (work.go)
	if failure := spend(run, callbackUnits); failure.IsInvalid() {
		return failure
	}
	if n, ok := function.Function().(*Native); ok {
		return n.Call(run, arguments...)
	}
	if run == nil {
		return value.Invalid("cannot call a function here: no run")
	}
	return run.Call(function, arguments...)
}

// callArguments ghi các đối số của một lời gọi lại vào buffer (một mảng cấp một lần cho cả vòng map, sort, reduce…)
// và trả nó: một lời gọi variadic cấp một mảng mới mỗi lần. Bên được gọi chép đối số (execute) hay chỉ đọc chúng (hàm
// có sẵn), không giữ mảng.
func callArguments(buffer []value.Value, arguments ...value.Value) []value.Value {
	return append(buffer[:0], arguments...)
}

// kindName là loại của v cho lời báo: typeof, trừ null và mảng (typeof của cả hai là "object", đọc sai).
func kindName(v value.Value) string {
	switch {
	case v.IsNull():
		return "null"
	case v.IsArray():
		return "array"
	}
	return v.Typeof()
}

// maxStringLength là của JavaScript (V8): một chuỗi dài hơn là RangeError, không phải một gigabyte. Đếm bằng byte, mà
// chuỗi ngoài ASCII có nhiều byte hơn đơn vị: chuỗi ấy chạm trần sớm hơn. Mọi cách dựng chuỗi dài (concat, join,
// repeat, padStart, padEnd, replace) kiểm nó trước khi cấp phát.
const maxStringLength = 1<<29 - 24

// tooLong là lỗi của một chuỗi quá maxStringLength.
func tooLong() value.Value { return rangeError("invalid string length") }
