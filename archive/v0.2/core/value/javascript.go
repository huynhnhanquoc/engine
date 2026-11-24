package value

import (
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Every exported method here has the signature (...Value) Value and is what a program calls on a value:
// "a".toUpperCase(), [1].push(2), now.format("15:04"). The name a program uses is the Go name with its
// first letter lowered. A method written for one kind fails with TYPE on the others, as JavaScript
// throws; which kinds answer for which name is also written out as data (kindMethods), for a method
// read without a call. Order follows the kind, then MDN's listing. Nothing else in the package is
// variadic.

// argument is the i-th argument, undefined when the caller gave fewer.
func argument(arguments []Value, i int) Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return Value{}
}

// maxStringLength is JavaScript's: a string past it is a RANGE failure, not a gigabyte. It is counted in
// bytes, which a string of other than ASCII has more of than units: such a string meets it sooner.
// Every way a program builds a string checks it (+, concat, join, replace, repeat, padStart and
// padEnd, JSON.stringify), since a few instructions can double one: energy is counted by the
// instruction, not by the byte.
const maxStringLength = 1<<29 - 24

// MaxStringLength is maxStringLength for the builders of strings outside value (encodeURIComponent's),
// which refuse past it with TooLong.
const MaxStringLength = maxStringLength

// tooLong is the failure of a string past maxStringLength, JavaScript's RangeError.
func tooLong() Value { return Fail(RangeCode, "invalid string length") }

// TooLong is the failure of a string past MaxStringLength, for a builder outside value.
func TooLong() Value { return tooLong() }

// smallIntegers is how many whole numbers, from 0, have their text made once in smallIntegerText:
// o[3] reads key "3" and "a" + i joins the text of i, and for these numbers neither makes a string.
const smallIntegers = 1024

// smallIntegerText is the String of each whole number below smallIntegers, as numberText writes it.
var smallIntegerText = func() (table [smallIntegers]Value) {
	for i := range table {
		table[i] = String(numberText(float64(i)))
	}
	return table
}()

// ---- a method read without a call ----

// kindMethods is, per kind, the methods of this file its JavaScript prototype holds (a time is a
// Date, a duration and bytes are Kitwork's), each with the length JavaScript gives the function: what
// `'abc'.at`, `[].map ? … : …` and `s['toUpperCase']()` read. A kind also answers for objectMethods,
// Object.prototype's, which every value but null and undefined reaches; a name in neither is undefined
// ('abc'.map), as in JavaScript. A test holds every list to the kind guards of the methods, both ways.
var kindMethods = [invalid + 1]map[string]int{
	text: {
		"at": 1, "charAt": 1, "charCodeAt": 1, "codePointAt": 1, "concat": 1, "endsWith": 1, "includes": 1,
		"indexOf": 1, "isWellFormed": 0, "lastIndexOf": 1, "padEnd": 1, "padStart": 1, "repeat": 1,
		"replace": 2, "replaceAll": 2, "slice": 2, "split": 2, "startsWith": 1, "substr": 2, "substring": 2,
		"toLowerCase": 0, "toString": 0, "toUpperCase": 0, "toWellFormed": 0, "trim": 0, "trimEnd": 0,
		"trimLeft": 0, "trimRight": 0, "trimStart": 0, "valueOf": 0,
	},
	number:  {"toExponential": 1, "toFixed": 1, "toPrecision": 1, "toString": 1, "valueOf": 0},
	boolean: {"toString": 0, "valueOf": 0},
	array: {
		"at": 1, "concat": 1, "copyWithin": 2, "every": 1, "fill": 1, "filter": 1, "find": 1, "findIndex": 1,
		"findLast": 1, "findLastIndex": 1, "flat": 0, "flatMap": 1, "forEach": 1, "includes": 1, "indexOf": 1,
		"join": 1, "lastIndexOf": 1, "map": 1, "pop": 0, "push": 1, "reduce": 1, "reduceRight": 1,
		"reverse": 0, "shift": 0, "slice": 2, "some": 1, "sort": 1, "splice": 2, "toReversed": 0,
		"toSorted": 1, "toSpliced": 2, "toString": 0, "unshift": 1, "with": 2,
	},
	instant:  {"format": 1, "getTime": 0, "toISOString": 0, "toString": 0, "valueOf": 0},
	duration: {"hours": 0, "milliseconds": 0, "minutes": 0, "seconds": 0, "toString": 0},
	bytes:    {"toString": 0},
	function: {"toString": 0},
}

// objectMethods is Object.prototype's part of this file; JavaScript's others (isPrototypeOf,
// propertyIsEnumerable, __defineGetter__ …) are not Kitwork's.
var objectMethods = map[string]int{"hasOwnProperty": 1, "toString": 0, "valueOf": 0}

// javascriptOnlyNames is, per kind, the names JavaScript's prototype of that kind holds and Kitwork's does not; the
// ones of Object.prototype, which every kind but null and undefined reaches, are under object. Read, each is the TYPE
// error (javascriptOnly) where it was undefined, a value other than JavaScript's: [1].entries ? 1 : 2 was 2, and
// f.caller undefined where strict JavaScript throws (19/11/2026, docs/language.md mục 4.4, 8.2, 8.3). A duration and
// bytes are Kitwork's kinds, with no prototype of JavaScript to answer for.
var javascriptOnlyNames = [invalid + 1]map[string]bool{
	text: names("anchor", "big", "blink", "bold", "fixed", "fontcolor", "fontsize", "italics", "link", "localeCompare",
		"match", "matchAll", "normalize", "search", "small", "strike", "sub", "sup", "toLocaleLowerCase",
		"toLocaleUpperCase"),
	number: names("toLocaleString"),
	array:  names("entries", "keys", "values", "toLocaleString"),
	instant: names("getDate", "getDay", "getFullYear", "getHours", "getMilliseconds", "getMinutes", "getMonth",
		"getSeconds", "getTimezoneOffset", "getUTCDate", "getUTCDay", "getUTCFullYear", "getUTCHours",
		"getUTCMilliseconds", "getUTCMinutes", "getUTCMonth", "getUTCSeconds", "getYear", "setDate", "setFullYear",
		"setHours", "setMilliseconds", "setMinutes", "setMonth", "setSeconds", "setTime", "setUTCDate",
		"setUTCFullYear", "setUTCHours", "setUTCMilliseconds", "setUTCMinutes", "setUTCMonth", "setUTCSeconds",
		"setYear", "toDateString", "toGMTString", "toJSON", "toLocaleDateString", "toLocaleString",
		"toLocaleTimeString", "toTimeString", "toUTCString"),
	function: names("apply", "bind", "call", "caller", "arguments"),
	object: names("constructor", "__proto__", "isPrototypeOf", "propertyIsEnumerable", "toLocaleString",
		"__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__"),
}

// javascriptStatics are, per builtin Kitwork shares with JavaScript, the names JavaScript's holds and Kitwork's does not
// (node's own names of each, 19/11/2026): read off the builtin, each is the TYPE error (JavaScriptStatic) where it was
// undefined, a value other than JavaScript's (Array.prototype was undefined, typeof Array.prototype "undefined");
// Object.getOwnPropertyNames and Object.hasOwn refuse them too. Each is rare: a program never writes the builtin's
// prototype, and the calls among them are compile errors already (Object.seal(o), console.table(rows)). console's are
// those of the console standard, not node's own. Math holds every name of JavaScript's.
var javascriptStatics = map[string]map[string]bool{
	"Array":   names("prototype", "fromAsync"),
	"Boolean": names("prototype"),
	"Date":    names("prototype"),
	"Number":  names("prototype"),
	"String":  names("prototype", "raw"),
	"Object": names("prototype", "create", "defineProperty", "defineProperties", "getOwnPropertyDescriptor",
		"getOwnPropertyDescriptors", "getOwnPropertySymbols", "getPrototypeOf", "setPrototypeOf", "preventExtensions",
		"isExtensible", "seal", "isSealed"),
	"JSON": names("rawJSON", "isRawJSON"),
	"console": names("assert", "clear", "count", "countReset", "dir", "dirxml", "group", "groupCollapsed", "groupEnd",
		"table", "time", "timeEnd", "timeLog", "trace"),
}

// builtinName is the builtin of javascriptStatics v is: a function of the engine by that name (its own name, which
// core/builtins sets as a key, as JavaScript's), which no program's function is (a lambda, and a program writes no key
// of a function), nor a method read off a value or a struct; a stand-in is the function it stands in for; or a library
// (MarkLibrary). "" for any other value.
func (v Value) builtinName() string {
	switch v.kind {
	case function:
		c, ok := v.data.(*callable)
		if ok && c.stands != nil {
			c = c.stands // handed to a function of the engine (Object.hasOwn(Array, …)), it is the builtin still
		}
		if !ok || c.native == nil || c.lambda != nil || c.owner != nil || c.prototype != 0 {
			return ""
		}
		name := c.name
		if o := v.properties(); o != nil {
			if item, ok := o.get("name"); ok && item.kind == text {
				name = item.data.(string)
			}
		}
		if javascriptStatics[name] != nil {
			return name
		}
	case object:
		return v.Library()
	}
	return ""
}

// JavaScriptStatic is the TYPE error of name read off a builtin whose JavaScript twin holds it where Kitwork's does not
// (javascriptStatics): Array.prototype, Object.create, console.table. Value{} for any other name or value.
func (v Value) JavaScriptStatic(name string) Value {
	if builtin := v.builtinName(); builtin != "" && javascriptStatics[builtin][name] {
		return Fail(TypeCode, builtin+" của Kitwork chưa có "+name+" (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)")
	}
	return Value{}
}

// StaticsMissing is the names v's JavaScript twin holds and Kitwork's v does not, in order, when v is such a builtin:
// Object.getOwnPropertyNames(Array) would list fewer than JavaScript's. nil for any other value.
func (v Value) StaticsMissing() []string {
	builtin := v.builtinName()
	if builtin == "" {
		return nil
	}
	missing := make([]string, 0, len(javascriptStatics[builtin]))
	for name := range javascriptStatics[builtin] {
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

// names is a set of names.
func names(list ...string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, name := range list {
		set[name] = true
	}
	return set
}

// javascriptOnly is v.name when v's kind answers for no method name: the TYPE error when JavaScript's prototype of
// that kind, or Object.prototype, holds name (javascriptOnlyNames), undefined otherwise, as JavaScript gives for a name
// no prototype holds ('abc'.map).
func (v Value) javascriptOnly(name string) Value {
	if v.kind == function && (name == "caller" || name == "arguments") {
		return Fail(TypeCode, name+" của một hàm không đọc được: JavaScript strict ném TypeError ở đây, Kitwork không có "+name)
	}
	if failure := v.JavaScriptStatic(name); failure.kind == invalid {
		return failure
	}
	if javascriptOnlyNames[v.kind][name] || javascriptOnlyNames[object][name] {
		return Fail(TypeCode, v.Kind()+" của Kitwork chưa có "+name+" (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)")
	}
	return Value{}
}

// methodAliases is a method JavaScript keeps under a second name: String.prototype.trimLeft is
// trimStart itself, so its name is "trimStart" and the two are ===.
var methodAliases = map[string]string{"trimLeft": "trimStart", "trimRight": "trimEnd"}

// method is v.name read without a call: the method as a function, or undefined when v's kind does not answer for
// name. A Kitwork call carries no `this`, so the function has no value to work on, as JavaScript's prototype method
// read off its value has none: calling it is the TYPE error JavaScript throws, which says to call the method on its
// value or to write an arrow (18/11/2026, docs/language.md mục 8.2: it was bound to v, so `const up =
// s.toUpperCase; up()` gave s's capitals where JavaScript throws). Equal still answers === for two reads of one
// method, as JavaScript does ([1].map === [2].map).
func (v Value) method(name string) Value {
	if v.kind <= null || v.kind == host || v.kind == invalid {
		return Value{} // Get answers for these before it asks for a method
	}
	owner := v.kind
	length, ok := kindMethods[v.kind][name]
	if !ok {
		if length, ok = objectMethods[name]; !ok {
			return v.javascriptOnly(name)
		}
		owner = object
	}
	if canonical, ok := methodAliases[name]; ok {
		name = canonical
	}
	call := valueMethods[name]
	if owner == object && name == "toString" {
		// Object.prototype.toString read off an object is that method, not the conversion String(o):
		// it is "[object Object]" whatever own toString or valueOf the object gains later
		call = valueMethod{call: func(Value, ...Value) Value { return String("[object Object]") }}
	}
	if call.call == nil {
		return Value{} // a listed name without its method is the list's fault; the test keeps them one
	}
	return Value{kind: function, data: &callable{
		native:    func(...Value) Value { return detachedMethod(name) },
		within:    func(Budget, ...Value) Value { return detachedMethod(name) },
		name:      name,
		length:    int32(length),
		prototype: owner,
	}}
}

// detachedMethod is the failure of a method called apart from the value it was read off (method).
func detachedMethod(name string) Value {
	return Fail(TypeCode, "method "+name+" đọc ra khỏi giá trị rồi gọi riêng: không có giá trị để làm việc (JavaScript ném TypeError); gọi thẳng x."+name+"(…), hay viết (x) => x."+name+"(…)")
}

// MethodNames lists, in order, the methods a program can read on v without calling them: its kind's
// and Object.prototype's. Documentation and tests read it.
func (v Value) MethodNames() []string {
	if v.kind <= null || v.kind == host || v.kind == invalid {
		return nil
	}
	names := make([]string, 0, len(kindMethods[v.kind])+len(objectMethods))
	for name := range kindMethods[v.kind] {
		names = append(names, name)
	}
	for name := range objectMethods {
		if _, own := kindMethods[v.kind][name]; !own {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ---- every kind ----

// ToString is what String(v), `${v}` and v.toString() give: a number as JavaScript writes it, an array
// its items joined by commas, an object through its own toString and valueOf (primitive.go) and else
// "[object Object]", a duration as Go writes it ("1h30m"), a failure its code and text; a function the TYPE
// error (JavaScript writes its source), and a time the TYPE error (JavaScript writes it in the machine's zone).
func (v Value) ToString(arguments ...Value) Value {
	switch v.kind {
	case 0:
		return String(typeofUndefined)
	case null:
		return String(typeofNull)
	case boolean:
		if v.number == 1 {
			return String("true")
		}
		return String("false")
	case number:
		if radix := argument(arguments, 0); !radix.IsUndefined() {
			r := toIntegerOrInfinity(radix.ToNumber().Number())
			if r < 2 || r > 36 {
				return Fail(RangeCode, "toString() radix must be between 2 and 36")
			}
			if r != 10 {
				return String(radixText(v.Number(), int(r)))
			}
		}
		if n := v.Number(); n >= 0 && n < smallIntegers && n == math.Trunc(n) {
			return smallIntegerText[int(n)] // -0 lands on 0 too: its text is "0"
		}
		return String(numberText(v.Number()))
	case text:
		return v
	case instant:
		// JavaScript writes a Date in the zone of the machine that runs it ("Thu Jan 01 1970 07:00:00 GMT+0700 (…)"):
		// the text of a time is the TYPE error, its form written by the program (18/11/2026, docs/language.md mục 8.2:
		// it was RFC 3339 in UTC, a text other than JavaScript's)
		return Fail(TypeCode, "một thời gian không đổi thành chữ: JavaScript viết nó theo múi giờ của máy chạy; viết rõ dạng, t.toISOString() hay t.format(\"2006-01-02 15:04\")")
	case duration:
		return String(v.Duration().String())
	case bytes:
		return String(string(v.Bytes()))
	case array:
		return joined(v, ",")
	case function:
		// JavaScript writes a function's source, which Kitwork does not keep: the text of a function is the TYPE error
		// (18/11/2026, docs/language.md mục 8.2: it was "function", a text other than JavaScript's)
		return Fail(TypeCode, "một hàm không đổi thành chữ: JavaScript viết mã nguồn của nó, Kitwork không giữ; gọi hàm, hay đọc tên nó, f.name")
	case invalid:
		return String(v.Failure())
	case object:
		if primitive, ok := v.ownPrimitive(true); ok {
			if primitive.kind == invalid {
				return primitive
			}
			return primitive.ToString()
		}
	}
	if library := v.Library(); library != "" {
		return LibraryAsData(library, "String("+library+")")
	}
	return String("[object Object]")
}

// ValueOf is v.valueOf(): a number, a string or a boolean is itself, as are an object, an array, a
// function, bytes and a duration (Object.prototype.valueOf gives the object back); a time is its
// milliseconds since 1970, as a Date's valueOf.
func (v Value) ValueOf(...Value) Value {
	switch v.kind {
	case 0, null, host, invalid:
		return v.noMethod("valueOf")
	case instant:
		return v.ToNumber()
	}
	return v
}

// HasOwnProperty is v.hasOwnProperty(key), the key read as text (0 is "0", undefined "undefined"): an
// object's own key; an array's or a string's index below its length, and "length"; a byte's index; a
// function's own property, "length" and "name". A number, a boolean, a time or a duration owns none.
func (v Value) HasOwnProperty(arguments ...Value) Value {
	key := argument(arguments, 0).ToString().Text()
	switch v.kind {
	case 0, null, host, invalid:
		return v.noMethod("hasOwnProperty")
	case object:
		if failure := v.JavaScriptStatic(key); failure.kind == invalid {
			return failure // JSON.hasOwnProperty("rawJSON")
		}
		return Boolean(v.Has(key))
	case function:
		if failure := v.JavaScriptStatic(key); failure.kind == invalid {
			return failure // Array.hasOwnProperty("prototype"): JavaScript's has it, Kitwork's not
		}
		return Boolean(v.Has(key) || key == "length" || key == "name")
	case array, text, bytes:
		if key == "length" && v.kind != bytes {
			return Boolean(true)
		}
		i, ok := arrayIndex(key)
		return Boolean(ok && int64(i) < int64(v.length()))
	}
	return Boolean(false)
}

// ---- number ----

// ToFixed is n.toFixed(digits): the count is made an integer first, so 100.5 is 100 and -0.5 is 0.
func (v Value) ToFixed(arguments ...Value) Value {
	if v.kind != number {
		return v.noMethod("toFixed")
	}
	digits := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if digits < 0 || digits > 100 {
		return Fail(RangeCode, "toFixed() digits argument must be between 0 and 100")
	}
	return String(fixedText(v.Number(), int(digits)))
}

// ToExponential is n.toExponential(digits): one digit before the point and digits after it, rounded
// from the exact value n holds, a tie away from zero ((1.25).toExponential(1) is "1.3e+0"); without
// digits, as few as tell n apart ("1.25e+1"). NaN and the infinities are words before the count is
// checked; a count outside 0 to 100 is a RANGE failure.
func (v Value) ToExponential(arguments ...Value) Value {
	if v.kind != number {
		return v.noMethod("toExponential")
	}
	digits := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if text, nonFinite := nonFiniteText(v.Number()); nonFinite {
		return String(text)
	}
	if digits < 0 || digits > 100 {
		return Fail(RangeCode, "toExponential() argument must be between 0 and 100")
	}
	return String(exponentialText(v.Number(), int(digits), argument(arguments, 0).IsUndefined()))
}

// ToPrecision is n.toPrecision(precision): that many significant digits, rounded as toExponential
// rounds, written plain or, when the exponent is below -6 or not below the precision, with one
// ("1.2e+5"). Without a precision it is String(n); a precision outside 1 to 100 is a RANGE failure.
func (v Value) ToPrecision(arguments ...Value) Value {
	if v.kind != number {
		return v.noMethod("toPrecision")
	}
	if argument(arguments, 0).IsUndefined() {
		return v.ToString()
	}
	precision := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if text, nonFinite := nonFiniteText(v.Number()); nonFinite {
		return String(text)
	}
	if precision < 1 || precision > 100 {
		return Fail(RangeCode, "toPrecision() argument must be between 1 and 100")
	}
	return String(precisionText(v.Number(), int(precision)))
}

// ---- string ----

// ToUpperCase and ToLowerCase are JavaScript's: Unicode's full mappings and the final sigma (casing.go).
func (v Value) ToUpperCase(...Value) Value {
	if v.kind != text {
		return v.noMethod("toUpperCase")
	}
	return String(upperText(v.Text()))
}

func (v Value) ToLowerCase(...Value) Value {
	if v.kind != text {
		return v.noMethod("toLowerCase")
	}
	return String(lowerText(v.Text()))
}

func (v Value) Trim(...Value) Value {
	if v.kind != text {
		return v.noMethod("trim")
	}
	return String(trimJavaScriptSpace(v.Text()))
}

func (v Value) TrimStart(...Value) Value {
	if v.kind != text {
		return v.noMethod("trimStart")
	}
	return String(trimJavaScriptSpaceLeft(v.Text()))
}

func (v Value) TrimEnd(...Value) Value {
	if v.kind != text {
		return v.noMethod("trimEnd")
	}
	return String(trimJavaScriptSpaceRight(v.Text()))
}

// TrimLeft and TrimRight are the older names JavaScript keeps for trimStart and trimEnd; each guards
// its kind under its own name, so a failure names the method the program called.
func (v Value) TrimLeft(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("trimLeft")
	}
	return v.TrimStart(arguments...)
}

func (v Value) TrimRight(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("trimRight")
	}
	return v.TrimEnd(arguments...)
}

// Includes is "abc".includes("b", from) and [1, 2].includes(2, from); an array compares as ===, except
// that NaN is found.
func (v Value) Includes(arguments ...Value) Value {
	switch v.kind {
	case text:
		s, needle := v.Text(), argument(arguments, 0).ToString().Text()
		from := clampIndex(argument(arguments, 1).ToNumber().Number(), unitLength(s))
		return Boolean(unitIndex(s, needle, from) >= 0)
	case array:
		// an empty array answers before its fromIndex is read, as JavaScript's does (conversion.go)
		items := v.Items()
		if len(items) == 0 {
			return Boolean(false)
		}
		start := 0 // no fromIndex is 0, with no conversion to make
		if len(arguments) > 1 {
			from, failure := fromIndex(arguments)
			if failure.kind == invalid {
				return failure
			}
			items = v.Items() // as its conversion left them: a valueOf may change the array
			start = relativeIndex(from, len(items))
		}
		needle := argument(arguments, 0)
		for _, item := range items[start:] {
			if item.Equal(needle) || (item.kind == number && needle.kind == number && math.IsNaN(item.Number()) && math.IsNaN(needle.Number())) {
				return Boolean(true)
			}
		}
		return Boolean(false)
	}
	return v.noMethod("includes")
}

func (v Value) StartsWith(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("startsWith")
	}
	s, needle := v.Text(), argument(arguments, 0).ToString().Text()
	from := clampIndex(argument(arguments, 1).ToNumber().Number(), unitLength(s))
	offset, midPair := byteOffset(s, from)
	if midPair || hasLoneSurrogate(needle) {
		return Boolean(strings.HasPrefix(unitSlice(s, from, unitLength(s)), needle))
	}
	return Boolean(strings.HasPrefix(s[offset:], needle))
}

func (v Value) EndsWith(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("endsWith")
	}
	s, needle := v.Text(), argument(arguments, 0).ToString().Text()
	end := unitLength(s)
	if !argument(arguments, 1).IsUndefined() {
		end = clampIndex(argument(arguments, 1).ToNumber().Number(), end)
	}
	offset, midPair := byteOffset(s, end)
	if midPair || hasLoneSurrogate(needle) {
		return Boolean(strings.HasSuffix(unitSlice(s, 0, end), needle))
	}
	return Boolean(strings.HasSuffix(s[:offset], needle))
}

// Split is s.split(separator, limit): no separator gives [s]; "" splits into units; limit cuts.
func (v Value) Split(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("split")
	}
	s := v.Text()
	separator := argument(arguments, 0)
	// the limit is converted before the separator, and the separator even when the limit is 0, as
	// JavaScript's split converts them, each failure kept (conversion.go)
	limit := -1
	if !argument(arguments, 1).IsUndefined() {
		n := argument(arguments, 1).ToNumber()
		if n.kind == invalid {
			return n
		}
		limit = int(uint32(toInt32(n.Number())))
	}
	var text string
	if !separator.IsUndefined() {
		converted := separator.ToString()
		if converted.kind == invalid {
			return converted
		}
		text = converted.Text()
	}
	if limit == 0 {
		return Array(nil)
	}
	if separator.IsUndefined() {
		return Array([]Value{v})
	}
	var parts []string
	paired := false
	if text == "" {
		if u := units(s); u != nil {
			for _, unit := range u {
				parts = append(parts, unitString(unit))
			}
		} else {
			for i := 0; i < len(s); i++ {
				parts = append(parts, s[i:i+1])
			}
		}
	} else if hasLoneSurrogate(text) {
		// a half of a pair is a unit JavaScript splits at, inside the pair too ('😀'.split(high half))
		parts, paired = strings.Split(unitForm(s), unitForm(text)), true
	} else {
		parts = strings.Split(s, text)
	}
	if limit >= 0 && limit < len(parts) {
		parts = parts[:limit]
	}
	items := make([]Value, len(parts))
	for i, part := range parts {
		if paired {
			part = mergeSurrogates(part)
		}
		items[i] = String(part)
	}
	return Array(items)
}

// Replace replaces the first occurrence of a text (no pattern); ReplaceAll every one. The replacement
// reads $$ as $, $& as the match, $` as the text before it and $' as the text after it; a function is
// called for each match instead (replaceCalling).
func (v Value) Replace(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("replace")
	}
	return v.replace(arguments, 1)
}

func (v Value) ReplaceAll(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("replaceAll")
	}
	return v.replace(arguments, -1)
}

// replace is replace (count 1) and replaceAll (count -1) of v, a text: the search made text, then the
// replacement unless it is a function, as JavaScript converts them, each failure kept. A function was made
// text too, so the word "function" replaced each match (05/11/2026).
func (v Value) replace(arguments []Value, count int) Value {
	search := argument(arguments, 0).ToString()
	if search.kind == invalid {
		return search
	}
	replacement := argument(arguments, 1)
	if replacement.kind == function {
		return replaceCalling(v, search.Text(), replacement, count)
	}
	if replacement = replacement.ToString(); replacement.kind == invalid {
		return replacement
	}
	s, searched := v.Text(), search.Text()
	if hasLoneSurrogate(searched) {
		// a half of a pair is a unit JavaScript matches, inside the pair too: '😀'.replaceAll(high half, 'X')
		result, ok := replaceText(unitForm(s), unitForm(searched), replacement.Text(), count)
		return replaced(mergeSurrogates(result), ok)
	}
	result, ok := replaceText(s, searched, replacement.Text(), count)
	if ok && (hasLoneSurrogate(s) || hasLoneSurrogate(replacement.Text())) {
		// a half the replacement brings, or one a match cut off, beside the other half is the character
		// again, as JavaScript's units are: 'x' + low half, x replaced by the high half, is the pair
		result = mergeSurrogates(result)
	}
	return replaced(result, ok)
}

// replaceCalling is replace (count 1) and replaceAll (count -1) of v with a function, called as JavaScript
// calls it for each match, in order: with the match, its offset in units and the whole text, and what it
// returns made text. A failure of the call, or of making its result text, is the method's. The matches of
// replaceAll are those of the text as it was: a text does not change, so finding each after the call
// before it finds the ones JavaScript finds first.
func replaceCalling(v Value, search string, replacer Value, count int) Value {
	s := v.Text()
	match := String(search)
	var b strings.Builder
	// what is written so far, which the run of replacer counts while the function runs (Kept)
	written := 0
	defer replacer.Keep(Kept{Size: &written}).Release()
	offset := 0
	var failure Value
	// call writes what the function gives for the match at offset; false when that failed or the text
	// would pass the longest
	call := func() bool {
		got := replacer.Call([]Value{match, Number(float64(offset)), v})
		if got.kind != invalid {
			got = got.ToString()
		}
		if got.kind == invalid {
			failure = got
			return false
		}
		if b.Len()+len(got.Text()) > maxStringLength {
			failure = tooLong()
			return false
		}
		b.WriteString(got.Text())
		written = b.Len()
		return true
	}
	if search == "" {
		if count == 1 {
			// replace: the one place is the start, and the text follows it whole
			if !call() {
				return failure
			}
			if b.Len()+len(s) > maxStringLength {
				return tooLong()
			}
			b.WriteString(s)
			return String(mergeSurrogates(b.String()))
		}
		// every place between two units, the start, the end and inside a pair: offset counts them
		walkPlaces(s, func(before, after int, unit string) bool {
			if !call() {
				return false
			}
			b.WriteString(unit)
			offset++
			return true
		})
		if failure.kind == invalid {
			return failure
		}
		if b.Len() > maxStringLength {
			return tooLong()
		}
		return String(mergeSurrogates(b.String()))
	}
	text, searched := s, search
	if hasLoneSurrogate(search) {
		text, searched = unitForm(s), unitForm(search)
	}
	length := unitLength(searched)
	at := 0
	for count != 0 {
		i := strings.Index(text[at:], searched)
		if i < 0 {
			break
		}
		i += at
		b.WriteString(text[at:i])
		offset += unitLength(text[at:i])
		if !call() {
			return failure
		}
		offset += length
		at = i + len(searched)
		count--
	}
	if b.Len()+len(text)-at > maxStringLength {
		return tooLong()
	}
	b.WriteString(text[at:])
	// a half the function gave beside the other half, of the text or of another call, is the character again
	return String(mergeSurrogates(b.String()))
}

// replaced is replaceText's result as a value: ok false is a string past the longest.
func replaced(text string, ok bool) Value {
	if !ok {
		return tooLong()
	}
	return String(text)
}

// replaceText replaces count occurrences of search: the first for 1 (replace), every one for -1
// (replaceAll). ok is false when the result would pass maxStringLength, which is then not built:
// replaceAll("x", s) squares a string's length. The length is counted before anything is written. An
// empty search matches at every place between two UTF-16 units (replaceEmpty).
func replaceText(s, search, replacement string, count int) (string, bool) {
	if search == "" {
		return replaceEmpty(s, replacement, count)
	}
	dollar := strings.IndexByte(replacement, '$') >= 0
	size := -1
	if !dollar || !strings.Contains(replacement, "$`") && !strings.Contains(replacement, "$'") {
		// every match becomes text of one length (the replacement with $$ and $& expanded), so counting
		// the matches counts the result: strings.Count does it faster than a walk of strings.Index, which
		// made replaceAll with a $& 10% slower (05/11/2026)
		found := strings.Count(s, search)
		if count >= 0 {
			found = min(found, count)
		}
		size = len(s) + found*(substitutionLength(replacement, 0, len(search), len(s))-len(search))
		if size > maxStringLength {
			return "", false
		}
		if !dollar {
			return strings.Replace(s, search, replacement, count), true
		}
	} else {
		// $` and $' write the text before and after each match, which squares the length: a walk counts it
		size = 0
		at, left := 0, count
		for left != 0 {
			i := strings.Index(s[at:], search)
			if i < 0 {
				break
			}
			i += at
			size += i - at + substitutionLength(replacement, i, len(search), len(s))
			if size > maxStringLength {
				return "", false
			}
			at = i + len(search)
			left--
		}
		size += len(s) - at
		if size > maxStringLength {
			return "", false
		}
	}
	var b strings.Builder
	b.Grow(size)
	at := 0
	for count != 0 {
		i := strings.Index(s[at:], search)
		if i < 0 {
			break
		}
		i += at
		b.WriteString(s[at:i])
		writeSubstitution(&b, replacement, s[:i], "", s[i:i+len(search)], "", s[i+len(search):])
		at = i + len(search)
		count--
	}
	b.WriteString(s[at:])
	return b.String(), true
}

// replaceEmpty is replaceText of an empty search, which JavaScript matches at every place between two
// UTF-16 units, the start and the end included, and between the two units of a character past U+FFFF
// too: "😀".replaceAll("", "-") is five units, '-', the high half, '-', the low half, '-', each half kept
// in its three-byte form. Until 05/11/2026 the walk for a replacement with a $ stepped one byte, cutting
// 'ạ' into bytes that are no UTF-8, and the walk without one (strings.Replace) stepped one character.
func replaceEmpty(s, replacement string, count int) (string, bool) {
	if count == 0 {
		return s, true
	}
	var b strings.Builder
	if count > 0 {
		// replace: the one place is the start, and s follows it whole
		size := substitutionLength(replacement, 0, 0, len(s)) + len(s)
		if size > maxStringLength {
			return "", false
		}
		b.Grow(size)
		writeSubstitution(&b, replacement, "", "", "", "", s)
		b.WriteString(s)
		return mergeSurrogates(b.String()), true
	}
	size := 0
	walkPlaces(s, func(before, after int, unit string) bool {
		written := substitutionLength(replacement, before, 0, before+after)
		size += written + len(unit)
		if written == 0 && before+after > len(s) {
			size -= 2 // the halves of a pair nothing came between join again: four bytes, not six
		}
		return size <= maxStringLength
	})
	if size > maxStringLength {
		return "", false
	}
	b.Grow(size)
	walkPlaces(s, func(before, after int, unit string) bool {
		if before+after > len(s) {
			// a place inside a pair: each side has its half of the character
			start := before - 3
			pair := s[start : start+4]
			writeSubstitution(&b, replacement, s[:start], halfOfPair(pair, true), "", halfOfPair(pair, false), s[start+4:])
		} else {
			writeSubstitution(&b, replacement, s[:before], "", "", "", s[before:])
		}
		b.WriteString(unit)
		return true
	})
	// the halves of a pair nothing came between are the character again
	return mergeSurrogates(b.String()), true
}

// walkPlaces calls each for every place between two units of s, in order: before and after are the bytes
// of the text on either side, a half of a pair counted as the three bytes of its form, and unit is the
// unit that follows the place ("" after the last). It stops when each returns false.
func walkPlaces(s string, each func(before, after int, unit string) bool) {
	for at := 0; at < len(s); {
		size := 1
		if s[at] >= utf8.RuneSelf {
			if _, ok := loneSurrogateAt(s, at); ok {
				size = 3
			} else {
				_, size = utf8.DecodeRuneInString(s[at:])
			}
		}
		if size == 4 {
			// a character past U+FFFF is two units, with a place between them
			pair := s[at : at+4]
			if !each(at, len(s)-at, halfOfPair(pair, true)) || !each(at+3, len(s)-at-1, halfOfPair(pair, false)) {
				return
			}
		} else if !each(at, len(s)-at, s[at:at+size]) {
			return
		}
		at += size
	}
	each(len(s), 0, "")
}

// halfOfPair is the high (or the low) surrogate of pair, the four bytes of a character past U+FFFF, in
// its three-byte form.
func halfOfPair(pair string, high bool) string {
	r, _ := utf8.DecodeRuneInString(pair)
	r -= 0x10000
	unit := uint16(0xDC00 + r&0x3FF)
	if high {
		unit = uint16(0xD800 + r>>10)
	}
	return string(appendUnit(nil, unit))
}

// substitutionLength is the length of the text writeSubstitution writes for a match at start of
// matchLength bytes in a text of total bytes, counted without building it.
func substitutionLength(replacement string, start, matchLength, total int) int {
	length := 0
	for i := 0; i < len(replacement); i++ {
		if replacement[i] != '$' || i+1 == len(replacement) {
			length++
			continue
		}
		switch replacement[i+1] {
		case '$':
			length++
		case '&':
			length += matchLength
		case '`':
			length += start
		case '\'':
			length += total - start - matchLength
		default:
			length++
			continue
		}
		i++
	}
	return length
}

// writeSubstitution writes the replacement with its $ patterns expanded, as JavaScript's GetSubstitution
// does: $$ is $, $& the match, $` the text before it and $' the text after it, each side given in two
// pieces (the second a half of a pair, for a place inside a character). It writes into the result, so a
// match costs no string of its own.
func writeSubstitution(b *strings.Builder, replacement, before, beforeHalf, match, afterHalf, after string) {
	for i := 0; i < len(replacement); i++ {
		if replacement[i] != '$' || i+1 == len(replacement) {
			b.WriteByte(replacement[i])
			continue
		}
		switch replacement[i+1] {
		case '$':
			b.WriteByte('$')
		case '&':
			b.WriteString(match)
		case '`':
			b.WriteString(before)
			b.WriteString(beforeHalf)
		case '\'':
			b.WriteString(afterHalf)
			b.WriteString(after)
		default:
			b.WriteByte('$')
			continue
		}
		i++
	}
}

// Slice is s.slice(start, end) and a.slice(start, end): negative counts from the end; an array's slice
// is a new array.
func (v Value) Slice(arguments ...Value) Value {
	switch v.kind {
	case text:
		s := v.Text()
		length := unitLength(s)
		start, end := relativeIndex(argument(arguments, 0).ToNumber().Number(), length), length
		if !argument(arguments, 1).IsUndefined() {
			end = relativeIndex(argument(arguments, 1).ToNumber().Number(), length)
		}
		return String(unitSlice(s, start, end))
	case array:
		items := v.Items()
		start, end := relativeIndex(argument(arguments, 0).ToNumber().Number(), len(items)), len(items)
		if !argument(arguments, 1).IsUndefined() {
			end = relativeIndex(argument(arguments, 1).ToNumber().Number(), len(items))
		}
		if start >= end {
			return Array(nil)
		}
		sliced := Array(append([]Value(nil), items[start:end]...))
		if v.HasHoles() {
			sliced.WithHoles() // JavaScript's slice keeps the holes
		}
		return sliced
	}
	return v.noMethod("slice")
}

// Substring is s.substring(start, end): a negative index is 0, start past end swaps them.
func (v Value) Substring(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("substring")
	}
	s := v.Text()
	length := unitLength(s)
	start, end := clampIndex(argument(arguments, 0).ToNumber().Number(), length), length
	if !argument(arguments, 1).IsUndefined() {
		end = clampIndex(argument(arguments, 1).ToNumber().Number(), length)
	}
	if start > end {
		start, end = end, start
	}
	return String(unitSlice(s, start, end))
}

// Substr is s.substr(start, count): count units from start, a negative start counted from the end, a
// count below 0 taken as 0 and past the end as the rest.
func (v Value) Substr(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("substr")
	}
	s := v.Text()
	length := unitLength(s)
	start, count := relativeIndex(argument(arguments, 0).ToNumber().Number(), length), length
	if !argument(arguments, 1).IsUndefined() {
		count = clampIndex(argument(arguments, 1).ToNumber().Number(), length)
	}
	return String(unitSlice(s, start, min(start+count, length)))
}

// IndexOf is s.indexOf(needle, from) and a.indexOf(item, from), -1 when absent.
func (v Value) IndexOf(arguments ...Value) Value {
	switch v.kind {
	case text:
		s := v.Text()
		from := clampIndex(argument(arguments, 1).ToNumber().Number(), unitLength(s))
		return Number(float64(unitIndex(s, argument(arguments, 0).ToString().Text(), from)))
	case array:
		items := v.Items()
		if len(items) == 0 {
			return Number(-1) // before its fromIndex is read, as Includes
		}
		start := 0
		if len(arguments) > 1 {
			from, failure := fromIndex(arguments)
			if failure.kind == invalid {
				return failure
			}
			items = v.Items()
			start = relativeIndex(from, len(items))
		}
		needle := argument(arguments, 0)
		for i := start; i < len(items); i++ {
			if items[i].Equal(needle) {
				return Number(float64(i))
			}
		}
		return Number(-1)
	}
	return v.noMethod("indexOf")
}

// LastIndexOf searches backwards from the end, or from the second argument when one is given (even
// an undefined one, which reads as 0); a start below -length finds nothing.
func (v Value) LastIndexOf(arguments ...Value) Value {
	switch v.kind {
	case text:
		s := v.Text()
		from := unitLength(s)
		if n := argument(arguments, 1).ToNumber().Number(); !math.IsNaN(n) {
			from = clampIndex(n, from)
		}
		return Number(float64(unitLastIndex(s, argument(arguments, 0).ToString().Text(), from)))
	case array:
		items := v.Items()
		if len(items) == 0 {
			return Number(-1) // before its fromIndex is read, as Includes
		}
		var given float64
		if len(arguments) > 1 {
			var failure Value
			if given, failure = fromIndex(arguments); failure.kind == invalid {
				return failure
			}
			items = v.Items()
		}
		needle := argument(arguments, 0)
		from := len(items) - 1
		if len(arguments) > 1 {
			n := toIntegerOrInfinity(given)
			if n >= 0 {
				from = int(math.Min(n, float64(len(items)-1)))
			} else {
				from = int(math.Max(float64(len(items))+n, -1))
			}
		}
		for i := from; i >= 0; i-- {
			if items[i].Equal(needle) {
				return Number(float64(i))
			}
		}
		return Number(-1)
	}
	return v.noMethod("lastIndexOf")
}

// fromIndex is the second argument of an array's includes, indexOf and lastIndexOf as a number, or the
// failure of its conversion: they read it only when the array has an item, and only when it was given
// (none starts at the first item, as its NaN would), then read the items again, since its valueOf may
// change the array.
func fromIndex(arguments []Value) (float64, Value) {
	n := argument(arguments, 1).ToNumber()
	if n.kind == invalid {
		return 0, n
	}
	return n.Number(), Value{}
}

// CharAt is s.charAt(i): one unit, "" outside.
func (v Value) CharAt(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("charAt")
	}
	i := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if unit, ok := unitAt(v.Text(), int(i)); ok && i == float64(int(i)) {
		return String(unitString(unit))
	}
	return String("")
}

// CharCodeAt is s.charCodeAt(i): the unit as a number, NaN outside.
func (v Value) CharCodeAt(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("charCodeAt")
	}
	i := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if unit, ok := unitAt(v.Text(), int(i)); ok && i == float64(int(i)) {
		return Number(float64(unit))
	}
	return Number(math.NaN())
}

// CodePointAt is s.codePointAt(i): the character starting at unit i, both units of a pair as one code
// point ("😀".codePointAt(0) is 128512), the second unit alone (56832 at 1); undefined outside.
func (v Value) CodePointAt(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("codePointAt")
	}
	i := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if i < 0 || i >= float64(unitLength(v.Text())) {
		return Value{}
	}
	first, _ := unitAt(v.Text(), int(i))
	if first >= 0xD800 && first < 0xDC00 {
		if second, ok := unitAt(v.Text(), int(i)+1); ok && second >= 0xDC00 && second < 0xE000 {
			return Number(float64(0x10000 + (rune(first)-0xD800)<<10 + (rune(second) - 0xDC00)))
		}
	}
	return Number(float64(first))
}

// IsWellFormed is s.isWellFormed(): false when the string holds half of a pair alone.
func (v Value) IsWellFormed(...Value) Value {
	if v.kind != text {
		return v.noMethod("isWellFormed")
	}
	return Boolean(!hasLoneSurrogate(v.Text()))
}

// ToWellFormed is s.toWellFormed(): every half of a pair alone replaced by U+FFFD.
func (v Value) ToWellFormed(...Value) Value {
	if v.kind != text {
		return v.noMethod("toWellFormed")
	}
	if !hasLoneSurrogate(v.Text()) {
		return v
	}
	u := units(v.Text())
	for i, unit := range u {
		if unit >= 0xD800 && unit < 0xE000 {
			paired := unit < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 ||
				unit >= 0xDC00 && i > 0 && u[i-1] >= 0xD800 && u[i-1] < 0xDC00
			if !paired {
				u[i] = 0xFFFD
			}
		}
	}
	return String(encodeUnits(u))
}

// At is s.at(i) and a.at(i): negative counts from the end; undefined outside.
func (v Value) At(arguments ...Value) Value {
	i := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	switch v.kind {
	case text:
		length := unitLength(v.Text())
		if i < 0 {
			i += float64(length)
		}
		if i < 0 || i >= float64(length) {
			return Value{}
		}
		unit, _ := unitAt(v.Text(), int(i))
		return String(unitString(unit))
	case array:
		items := v.Items()
		if i < 0 {
			i += float64(len(items))
		}
		if i < 0 || i >= float64(len(items)) {
			return Value{}
		}
		return items[int(i)]
	}
	return v.noMethod("at")
}

// Repeat is s.repeat(count); a negative or infinite count, or a result past the longest string, is a
// RANGE failure before anything is allocated.
func (v Value) Repeat(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("repeat")
	}
	count := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if count < 0 || math.IsInf(count, 0) {
		return Fail(RangeCode, "invalid count value: "+numberText(count))
	}
	if count*float64(len(v.Text())) > maxStringLength {
		return tooLong()
	}
	return String(mergeSurrogates(strings.Repeat(v.Text(), int(count))))
}

func (v Value) PadStart(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("padStart")
	}
	return pad(v.Text(), arguments, true)
}

func (v Value) PadEnd(arguments ...Value) Value {
	if v.kind != text {
		return v.noMethod("padEnd")
	}
	return pad(v.Text(), arguments, false)
}

func pad(s string, arguments []Value, atStart bool) Value {
	target := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if target > maxStringLength {
		return tooLong()
	}
	length := unitLength(s)
	if target <= float64(length) {
		return String(s)
	}
	// the filler is read once there is something to pad, as JavaScript's StringPad reads it: one that
	// cannot convert fails the call only then (conversion.go)
	filler := " "
	if given := argument(arguments, 1); !given.IsUndefined() {
		converted := given.ToString()
		if converted.kind == invalid {
			return converted
		}
		filler = converted.Text()
	}
	if filler == "" {
		return String(s)
	}
	// the fill is the filler whole as often as it fits, then the units of it that are left, its bytes
	// counted before any is written: the target bounds units, and the longest string bounds bytes, which
	// a filler of other than ASCII has up to three times as many of (05/11/2026: 'a'.padEnd(536870888, '😀')
	// built a string of a gigabyte, and the whole filler once more before it was cut, 6.6 GB for one call)
	missing := int(target) - length
	fillerUnits := unitLength(filler)
	rest := unitSlice(filler, 0, missing%fillerUnits)
	if len(s)+missing/fillerUnits*len(filler)+len(rest) > maxStringLength {
		return tooLong()
	}
	fill := strings.Repeat(filler, missing/fillerUnits)
	if rest != "" {
		fill += rest
	}
	if !isASCII(filler) {
		// a filler that ends with half a pair and begins with the other half makes pairs where its
		// copies meet, as repeat's do
		fill = mergeSurrogates(fill)
	}
	if atStart {
		return String(concatText(fill, s))
	}
	return String(concatText(s, fill))
}

// Concat is s.concat(…) and a.concat(…): a new string or a new array, an array argument spread.
func (v Value) Concat(arguments ...Value) Value {
	switch v.kind {
	case text:
		var b strings.Builder
		b.WriteString(v.Text())
		for _, a := range arguments {
			part := a.ToString()
			if part.kind == invalid {
				return part
			}
			if b.Len()+len(part.Text()) > maxStringLength {
				return tooLong()
			}
			b.WriteString(part.Text())
		}
		return String(mergeSurrogates(b.String()))
	case array:
		items := append([]Value(nil), v.Items()...)
		holes := v.HasHoles()
		for _, a := range arguments {
			if a.kind == array {
				items = append(items, a.Items()...)
				holes = holes || a.HasHoles()
			} else {
				items = append(items, a)
			}
		}
		if holes {
			return Array(items).WithHoles() // JavaScript's concat keeps the holes
		}
		return Array(items)
	}
	return v.noMethod("concat")
}

// ---- array ----

// The writers of an array (push, pop, shift, unshift, splice, fill, copyWithin, reverse, sort, and
// SetIndex) refuse a frozen array with the TYPE failure JavaScript gives for its first write (list.go):
// a writer that would write nothing is let through, as JavaScript lets fill(0, 5) of [2, 1] through.

// Push appends and returns the new length.
func (v Value) Push(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("push")
	}
	if l.frozen {
		if len(arguments) == 0 {
			return readOnlyLength()
		}
		return addedItem(len(l.items))
	}
	l.items = append(l.items, arguments...)
	return Number(float64(len(l.items)))
}

// Pop removes and returns the last item; undefined when empty.
func (v Value) Pop(...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("pop")
	}
	if l.frozen {
		if len(l.items) == 0 {
			return readOnlyLength()
		}
		return deletedItem(len(l.items) - 1)
	}
	if len(l.items) == 0 {
		return Value{}
	}
	last := l.items[len(l.items)-1]
	l.items = l.items[:len(l.items)-1]
	return last
}

// Shift removes and returns the first item; undefined when empty.
func (v Value) Shift(...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("shift")
	}
	if l.frozen {
		switch len(l.items) {
		case 0:
			return readOnlyLength()
		case 1:
			return deletedItem(0)
		}
		return readOnlyItem(0)
	}
	if len(l.items) == 0 {
		return Value{}
	}
	first := l.items[0]
	l.items = append(l.items[:0], l.items[1:]...)
	return first
}

// Unshift prepends and returns the new length.
func (v Value) Unshift(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("unshift")
	}
	if l.frozen {
		switch {
		case len(arguments) == 0:
			return readOnlyLength()
		case len(l.items) == 0:
			return addedItem(0) // nothing to move: the first argument is the first write
		}
		return addedItem(len(l.items) + len(arguments) - 1) // the last item moves first, past the end
	}
	l.items = append(append([]Value(nil), arguments...), l.items...)
	return Number(float64(len(l.items)))
}

// Join is a.join(separator), "," by default; null and undefined items write as "".
func (v Value) Join(arguments ...Value) Value {
	return v.joinWithin(nil, arguments)
}

// joinWithin is join by a run whose budget is budget (InvokeWithin): the separator's text and the array's, each
// within it.
func (v Value) joinWithin(budget Budget, arguments []Value) Value {
	if v.kind != array {
		return v.noMethod("join")
	}
	separator := ","
	if !argument(arguments, 0).IsUndefined() {
		converted := argument(arguments, 0).TextWithin(budget)
		if converted.kind == invalid {
			return converted
		}
		separator = converted.Text()
	}
	return joinedWithin(v, separator, budget)
}

// joined is the array's items written with separator between them, or the failure of an item's text, or
// the RANGE failure of a text past the longest string, or past UnbudgetedLength: no run counts it.
func joined(v Value, separator string) Value {
	return joinedWithin(v, separator, nil)
}

// joinedWithin is joined by a run whose budget is budget, asked as the text grows: an array holding one text a
// thousand times is a thousand texts once joined, made from what the run holds once.
func joinedWithin(v Value, separator string, budget Budget) Value {
	l := limit{budget: budget, work: 32} // a byte written a unit, an item and a number made text more (work.go)
	text, failure := joinItems(v, separator, nil, &l, 0)
	if failure.kind == invalid {
		return failure
	}
	if spent := l.finish(len(text)); spent.kind == invalid {
		return spent
	}
	return String(text)
}

// TextWithin is ToString by a run whose budget is budget: an array's text is asked of it as it grows; any other
// value's text is ToString's.
func (v Value) TextWithin(budget Budget) Value {
	if v.kind == array {
		return joinedWithin(v, ",", budget)
	}
	return v.ToString()
}

// joinItems writes the array's items; an array met again on the way down writes as "", which is how
// JavaScript ends the loop of an array that holds itself. It stops at the first item whose text fails
// (an object's own toString), whose failure it gives, as JavaScript throws out of join (05/11/2026: the
// item was written as "", so [bad].join() was "" and String([p]) of an object of a frozen file was "[]"
// where the conversion failed), and once the text would pass what lim allows: an array that holds one
// array twice, thirty levels down, is a billion items of text for thirty instructions. before is the text
// the arrays around it wrote already, which the limit counts with this one's.
func joinItems(v Value, separator string, active []*list, lim *limit, before int) (string, Value) {
	l := v.data.(*list)
	for _, seen := range active {
		if seen == l {
			return "", Value{}
		}
	}
	active = append(active, l)
	var b strings.Builder
	for i, item := range l.items {
		gap := separator
		if i == 0 {
			gap = ""
		}
		part := ""
		lim.owed += joinItemUnits
		switch item.kind {
		case 0, null:
		case number:
			lim.owed += joinNumberUnits
			part, _ = item.ToText()
		case array:
			inner, failure := joinItems(item, ",", active, lim, before+b.Len()+len(gap))
			if failure.kind == invalid {
				return "", failure
			}
			part = inner
		default:
			text, failure := item.ToText()
			if failure.kind == invalid {
				return "", failure
			}
			part = text
		}
		// before the part is written, so the text never grows past what is allowed
		if failure := lim.allow(before + b.Len() + len(gap) + len(part)); failure.kind == invalid {
			return "", failure
		}
		b.WriteString(gap)
		b.WriteString(part)
	}
	return mergeSurrogates(b.String()), Value{}
}

// Reverse reverses in place and returns the array.
func (v Value) Reverse(...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("reverse")
	}
	if l.frozen && len(l.items) > 1 {
		return readOnlyItem(0)
	}
	s := l.items
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return v
}

// ToReversed is a.toReversed(): a new array, the items last to first; a is left as it was.
func (v Value) ToReversed(...Value) Value {
	if v.kind != array {
		return v.noMethod("toReversed")
	}
	items := v.Items()
	out := make([]Value, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return Array(out)
}

// Flat is a.flat(depth), depth 1 by default: a new array with nested arrays spread. An array that
// holds itself is a RANGE failure, as JavaScript's stack overflow is, before the stack overflows.
func (v Value) Flat(arguments ...Value) Value {
	return v.flatWithin(nil, arguments)
}

// flatWithin is flat by a run whose budget is budget, asked as the items grow: an array holding one array a
// thousand times is its items a thousand times once flat.
func (v Value) flatWithin(budget Budget, arguments []Value) Value {
	if v.kind != array {
		return v.noMethod("flat")
	}
	depth := 1.0
	if !argument(arguments, 0).IsUndefined() {
		depth = toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	}
	lim := limit{budget: budget, items: true, work: flatItemUnits} // an item, 32 bytes counted, written
	out, failure := flatten(nil, v, depth, nil, &lim)
	if failure.kind == invalid {
		return failure
	}
	if spent := lim.finish(len(out) * int(ItemBytes)); spent.kind == invalid {
		return spent
	}
	return Array(out)
}

// flatten writes v's items into out, an array among them flattened while depth lasts; the failure of an
// array that holds itself, or of out past what lim allows.
func flatten(out []Value, v Value, depth float64, active []*list, lim *limit) ([]Value, Value) {
	l := v.data.(*list)
	for _, seen := range active {
		if seen == l {
			return nil, Fail(RangeCode, "flat: the array holds itself")
		}
	}
	if l.holes {
		return nil, HolesRefused("flat()") // JavaScript's flat drops the holes, of an array inside too
	}
	active = append(active, l)
	for _, item := range l.items {
		if item.kind == array && depth >= 1 {
			var failure Value
			if out, failure = flatten(out, item, depth-1, active, lim); failure.kind == invalid {
				return nil, failure
			}
		} else {
			if failure := lim.allow((len(out) + 1) * int(ItemBytes)); failure.kind == invalid {
				return nil, failure
			}
			out = append(out, item)
		}
	}
	return out, Value{}
}

// ---- time: JavaScript has Date; a Kitwork time is a value and these are its words ----

// ToISOString is the time in RFC 3339 UTC with milliseconds, as a Date writes it.
func (v Value) ToISOString(...Value) Value {
	if v.kind != instant {
		return v.noMethod("toISOString")
	}
	return String(v.Time().Format("2006-01-02T15:04:05.000Z07:00"))
}

// GetTime is the milliseconds since 1970, as a Date's.
func (v Value) GetTime(...Value) Value {
	if v.kind != instant {
		return v.noMethod("getTime")
	}
	return Number(float64(v.Time().UnixMilli()))
}

// Format writes the time with a Go layout: now.format("2006-01-02 15:04").
func (v Value) Format(arguments ...Value) Value {
	if v.kind != instant {
		return v.noMethod("format")
	}
	return String(v.Time().Format(argument(arguments, 0).ToString().Text()))
}

// ---- duration ----

func (v Value) Milliseconds(...Value) Value {
	if v.kind != duration {
		return v.noMethod("milliseconds")
	}
	return Number(float64(v.Duration()) / 1e6)
}

func (v Value) Seconds(...Value) Value {
	if v.kind != duration {
		return v.noMethod("seconds")
	}
	return Number(v.Duration().Seconds())
}

func (v Value) Minutes(...Value) Value {
	if v.kind != duration {
		return v.noMethod("minutes")
	}
	return Number(v.Duration().Minutes())
}

func (v Value) Hours(...Value) Value {
	if v.kind != duration {
		return v.noMethod("hours")
	}
	return Number(v.Duration().Hours())
}

// radixText is (n).toString(radix) for a radix other than 10, as V8 writes it (DoubleToRadixCString), the engine of
// node, which the parity tests hold Kitwork to (18/11/2026): the fraction digit by digit while what is left is past
// half the gap to the next double, the last digit rounded half to even with its carry taken back through the digits
// written; the integer part by division, each digit below a double's 53 bits of precision a 0. It wrote an integer
// part from 2^63 on in decimal ((1e21).toString(16) was "1000000000000000000000") and the fraction to 52 digits
// whatever the number held ((0.1).toString(3) had 52 digits where node writes 34).
func radixText(f float64, radix int) string {
	if text, nonFinite := nonFiniteText(f); nonFinite {
		return text
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	base := float64(radix)
	negative := f < 0
	if negative {
		f = -f
	}
	integer := math.Floor(f)
	fraction := f - integer
	// the fraction's digits stop at the precision of f: half the gap to the next double, at least the smallest
	delta := math.Max(math.SmallestNonzeroFloat64, 0.5*(math.Nextafter(f, math.Inf(1))-f))
	var tail []byte
	if fraction >= delta {
		tail = append(tail, '.')
		for {
			fraction *= base
			delta *= base
			digit := int(fraction)
			tail = append(tail, digits[digit])
			fraction -= float64(digit)
			if fraction > 0.5 || fraction == 0.5 && digit&1 == 1 {
				if fraction+delta > 1 {
					// round up: the carry goes back through the digits written, past the point into the integer
					for {
						last := len(tail) - 1
						if last == 0 {
							integer++
							tail = tail[:0]
							break
						}
						written := strings.IndexByte(digits, tail[last])
						if written+1 < radix {
							tail[last] = digits[written+1]
							break
						}
						tail = tail[:last]
					}
					break
				}
			}
			if fraction < delta {
				break
			}
		}
	}
	// the integer part, its last digit first: those below a double's precision are 0
	var head []byte
	for integer/base >= 1<<53 {
		integer /= base
		head = append(head, '0')
	}
	for {
		remainder := math.Mod(integer, base)
		head = append(head, digits[int(remainder)])
		integer = (integer - remainder) / base
		if integer <= 0 {
			break
		}
	}
	if negative {
		head = append(head, '-')
	}
	slices.Reverse(head)
	return string(head) + string(tail)
}

// ---- array: in place ----

// Splice is a.splice(start, deleteCount, items…): removes, inserts, returns the removed items.
func (v Value) Splice(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("splice")
	}
	length := len(l.items)
	start := relativeIndex(argument(arguments, 0).ToNumber().Number(), length)
	count := length - start
	if len(arguments) > 1 {
		count = int(math.Max(0, math.Min(toIntegerOrInfinity(arguments[1].ToNumber().Number()), float64(length-start))))
	}
	if len(arguments) == 0 {
		count = 0
	}
	inserted := arguments[min(2, len(arguments)):]
	if l.frozen {
		return refuseSplice(start, count, len(inserted), length)
	}
	removed := append([]Value(nil), l.items[start:start+count]...)
	rest := append([]Value(nil), l.items[start+count:]...)
	l.items = append(append(l.items[:start], inserted...), rest...)
	if l.holes {
		return Array(removed).WithHoles() // what JavaScript's splice takes out keeps its holes
	}
	return Array(removed)
}

// refuseSplice is the failure of splice on a frozen array, at the first write JavaScript's splice
// makes: the items after the cut move left or right first, then the inserted items are written, then
// the ones left past the end are deleted, and a splice that changes nothing still writes the length.
func refuseSplice(start, removed, inserted, length int) Value {
	moves := start+removed < length
	switch {
	case inserted < removed && moves:
		return readOnlyItem(start + inserted)
	case inserted < removed:
		return deletedItem(length - 1)
	case inserted > removed && moves:
		return addedItem(length - removed + inserted - 1)
	case inserted > 0 && start < length:
		return readOnlyItem(start)
	case inserted > 0:
		return addedItem(start)
	}
	return readOnlyLength()
}

// Fill is a.fill(value, start, end) in place.
func (v Value) Fill(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("fill")
	}
	length := len(l.items)
	start, end := relativeIndex(argument(arguments, 1).ToNumber().Number(), length), length
	if !argument(arguments, 2).IsUndefined() {
		end = relativeIndex(arguments[2].ToNumber().Number(), length)
	}
	if l.frozen && start < end {
		return readOnlyItem(start)
	}
	for i := start; i < end; i++ {
		l.items[i] = argument(arguments, 0)
	}
	if start == 0 && end == length {
		l.holes = false // filled whole, as Array(n).fill(x): no hole is left
	}
	return v
}

// CopyWithin is a.copyWithin(target, start, end) in place: the items from start to end copied over
// those from target, as many as fit before the end; each place counts from the end when negative.
func (v Value) CopyWithin(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("copyWithin")
	}
	length := len(l.items)
	to := relativeIndex(argument(arguments, 0).ToNumber().Number(), length)
	from, end := relativeIndex(argument(arguments, 1).ToNumber().Number(), length), length
	if !argument(arguments, 2).IsUndefined() {
		end = relativeIndex(arguments[2].ToNumber().Number(), length)
	}
	if count := min(end-from, length-to); count > 0 {
		if l.frozen {
			return readOnlyItem(to)
		}
		copy(l.items[to:to+count], l.items[from:from+count]) // copy moves overlapping items as JavaScript does
	}
	return v
}

// ToSpliced is a.toSpliced(start, deleteCount, items…): what splice would leave, as a new array; a is
// left as it was.
func (v Value) ToSpliced(arguments ...Value) Value {
	if v.kind != array {
		return v.noMethod("toSpliced")
	}
	items := v.Items()
	start := relativeIndex(argument(arguments, 0).ToNumber().Number(), len(items))
	skip := 0
	switch {
	case len(arguments) == 1:
		skip = len(items) - start
	case len(arguments) > 1:
		skip = clampIndex(arguments[1].ToNumber().Number(), len(items)-start)
	}
	inserted := arguments[min(2, len(arguments)):]
	out := make([]Value, 0, len(items)-skip+len(inserted))
	out = append(append(append(out, items[:start]...), inserted...), items[start+skip:]...)
	return Array(out)
}

// With is a.with(index, item): a new array with item at index, counted from the end when negative;
// an index outside the array is a RANGE failure.
func (v Value) With(arguments ...Value) Value {
	if v.kind != array {
		return v.noMethod("with")
	}
	items := v.Items()
	i := toIntegerOrInfinity(argument(arguments, 0).ToNumber().Number())
	if i < 0 {
		i += float64(len(items))
	}
	if i < 0 || i >= float64(len(items)) {
		return Fail(RangeCode, "invalid index: "+numberText(i))
	}
	out := append([]Value(nil), items...)
	out[int(i)] = argument(arguments, 1)
	return Array(out)
}

// ---- array: with a callback ----
// Each calls fn(item, index, array) through one callback per call of the method (callback.go), so a Go
// function and a program's function both work; a failure the callback returns ends the method with it,
// and an item that is a failure comes back out before the callback is called, as from Call. A callback
// that is not a function is a TYPE failure before anything runs, as JavaScript throws.
//
// As in JavaScript, a method walks the items the array had when it began: an item the callback adds is
// not visited, and one it removes is skipped (forEach, map, filter, some, every, reduce, reduceRight) or
// read as undefined (find, findIndex, findLast, findLastIndex); each answers with the item it read. (Until
// 04/11/2026 a callback that pushed made forEach run on, and map panic past the end of its output.)

func callbackArgument(arguments []Value, name string) (Value, Value) {
	fn := argument(arguments, 0)
	if fn.kind != function {
		return fn, Fail(TypeCode, fn.ToString().Text()+" is not a function ("+name+")")
	}
	return fn, Value{}
}

func (v Value) ForEach(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("forEach")
	}
	fn, bad := callbackArgument(arguments, "forEach")
	if bad.kind == invalid {
		return bad
	}
	each := newCallback(fn, 3)
	defer each.keep(Kept{Value: v}).Release()
	for i, length := 0, len(l.items); i < length && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		if out := each.item(item, i, v); out.kind == invalid {
			return out
		}
	}
	return Value{}
}

func (v Value) Map(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("map")
	}
	fn, bad := callbackArgument(arguments, "map")
	if bad.kind == invalid {
		return bad
	}
	each := newCallback(fn, 3)
	out := make([]Value, len(l.items)) // an item the callback removed stays undefined, JavaScript's hole
	defer each.keep(Kept{Value: v, Items: &out}).Release()
	for i := 0; i < len(out) && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		if out[i] = each.item(item, i, v); out[i].kind == invalid {
			return out[i]
		}
	}
	return Array(out)
}

func (v Value) FlatMap(arguments ...Value) Value {
	if v.kind != array {
		return v.noMethod("flatMap")
	}
	if _, bad := callbackArgument(arguments, "flatMap"); bad.kind == invalid {
		return bad
	}
	// each item the callback gives, an array spread one level, as map then flat() would make it, except
	// for an item the callback removed: JavaScript's flatMap passes it over (05/11/2026; map leaves
	// undefined in its place, which flat() kept)
	l := v.data.(*list)
	each := newCallback(arguments[0], 3)
	length := len(l.items)
	out := make([]Value, 0, length)
	defer each.keep(Kept{Value: v, Items: &out}).Release()
	for i := 0; i < length && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		mapped := each.item(item, i, v)
		switch {
		case mapped.kind == invalid:
			return mapped
		case mapped.kind == array:
			if mapped.HasHoles() {
				return HolesRefused("flatMap()") // JavaScript's flatMap drops the holes of what the callback gives
			}
			out = append(out, mapped.data.(*list).items...)
		default:
			out = append(out, mapped)
		}
	}
	return Array(out)
}

// Filter keeps the items the callback says to; its output is made as long as the array, so it grows no
// further whatever it keeps.
func (v Value) Filter(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("filter")
	}
	fn, bad := callbackArgument(arguments, "filter")
	if bad.kind == invalid {
		return bad
	}
	each := newCallback(fn, 3)
	length := len(l.items)
	out := make([]Value, 0, length)
	defer each.keep(Kept{Value: v, Items: &out}).Release()
	for i := 0; i < length && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		keep := each.item(item, i, v)
		if keep.kind == invalid {
			return keep
		}
		if keep.ToBoolean() {
			out = append(out, item)
		}
	}
	return Array(out)
}

func (v Value) Find(arguments ...Value) Value {
	_, item, found := v.find("find", arguments, false)
	if found.kind == invalid || !found.ToBoolean() {
		return found
	}
	return item
}

func (v Value) FindIndex(arguments ...Value) Value {
	i, _, found := v.find("findIndex", arguments, false)
	if found.kind == invalid {
		return found
	}
	if !found.ToBoolean() {
		return Number(-1)
	}
	return Number(float64(i))
}

// find is the search Find, FindIndex and Some share, over the indexes the array had when it began: the
// index and the item the callback said true for, with true; undefined when it never did; the failure
// when it failed. skipRemoved is some's: an item the callback removed is not visited, where find reads
// it as undefined.
func (v Value) find(name string, arguments []Value, skipRemoved bool) (int, Value, Value) {
	l, ok := v.data.(*list)
	if !ok {
		return 0, Value{}, v.noMethod(name)
	}
	fn, bad := callbackArgument(arguments, name)
	if bad.kind == invalid {
		return 0, Value{}, bad
	}
	each := newCallback(fn, 3)
	defer each.keep(Kept{Value: v}).Release()
	for i, length := 0, len(l.items); i < length; i++ {
		if skipRemoved && i >= len(l.items) {
			break
		}
		item := itemAt(l, i)
		if item.kind == invalid {
			return 0, Value{}, item
		}
		found := each.item(item, i, v)
		if found.kind == invalid {
			return 0, Value{}, found
		}
		if found.ToBoolean() {
			return i, item, Boolean(true)
		}
	}
	return 0, Value{}, Value{}
}

// FindLast and FindLastIndex search as Find and FindIndex do, from the last item to the first.
func (v Value) FindLast(arguments ...Value) Value {
	_, item, found := v.findLast("findLast", arguments)
	if found.kind == invalid || !found.ToBoolean() {
		return found
	}
	return item
}

func (v Value) FindLastIndex(arguments ...Value) Value {
	i, _, found := v.findLast("findLastIndex", arguments)
	if found.kind == invalid {
		return found
	}
	if !found.ToBoolean() {
		return Number(-1)
	}
	return Number(float64(i))
}

// findLast is find from the end, over the items there were when it began, as JavaScript reads them:
// one the callback removed reads as undefined.
func (v Value) findLast(name string, arguments []Value) (int, Value, Value) {
	l, ok := v.data.(*list)
	if !ok {
		return 0, Value{}, v.noMethod(name)
	}
	fn, bad := callbackArgument(arguments, name)
	if bad.kind == invalid {
		return 0, Value{}, bad
	}
	each := newCallback(fn, 3)
	defer each.keep(Kept{Value: v}).Release()
	for i := len(l.items) - 1; i >= 0; i-- {
		item := itemAt(l, i)
		if item.kind == invalid {
			return 0, Value{}, item
		}
		found := each.item(item, i, v)
		if found.kind == invalid {
			return 0, Value{}, found
		}
		if found.ToBoolean() {
			return i, item, Boolean(true)
		}
	}
	return 0, Value{}, Value{}
}

// itemAt is the array's item i, undefined when a callback has shortened the array past it.
func itemAt(l *list, i int) Value {
	if i < len(l.items) {
		return l.items[i]
	}
	return Value{}
}

func (v Value) Some(arguments ...Value) Value {
	_, _, found := v.find("some", arguments, true)
	if found.kind == invalid {
		return found
	}
	return Boolean(found.ToBoolean())
}

func (v Value) Every(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("every")
	}
	fn, bad := callbackArgument(arguments, "every")
	if bad.kind == invalid {
		return bad
	}
	each := newCallback(fn, 3)
	defer each.keep(Kept{Value: v}).Release()
	for i, length := 0, len(l.items); i < length && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		holds := each.item(item, i, v)
		if holds.kind == invalid {
			return holds
		}
		if !holds.ToBoolean() {
			return Boolean(false)
		}
	}
	return Boolean(true)
}

// Reduce is a.reduce(fn, initial): fn(accumulator, item, index, array); without an initial value
// the first item is it, and an empty array is a TYPE failure, as JavaScript throws.
func (v Value) Reduce(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("reduce")
	}
	fn, bad := callbackArgument(arguments, "reduce")
	if bad.kind == invalid {
		return bad
	}
	start := 0
	var accumulator Value
	if len(arguments) > 1 {
		accumulator = arguments[1]
	} else {
		if len(l.items) == 0 {
			return Fail(TypeCode, "reduce of empty array with no initial value")
		}
		accumulator, start = l.items[0], 1
	}
	if accumulator.kind == invalid {
		return accumulator
	}
	each := newCallback(fn, 4)
	defer each.keep(Kept{Value: v}).Release()
	for i, length := start, len(l.items); i < length && i < len(l.items); i++ {
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		if accumulator = each.accumulate(accumulator, item, i, v); accumulator.kind == invalid {
			return accumulator
		}
	}
	return accumulator
}

// ReduceRight is Reduce from the last item to the first: without an initial value the last item is
// it, and an empty array is a TYPE failure.
func (v Value) ReduceRight(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("reduceRight")
	}
	fn, bad := callbackArgument(arguments, "reduceRight")
	if bad.kind == invalid {
		return bad
	}
	start := len(l.items) - 1
	var accumulator Value
	if len(arguments) > 1 {
		accumulator = arguments[1]
	} else {
		if len(l.items) == 0 {
			return Fail(TypeCode, "reduce of empty array with no initial value")
		}
		accumulator, start = l.items[start], start-1
	}
	if accumulator.kind == invalid {
		return accumulator
	}
	each := newCallback(fn, 4)
	defer each.keep(Kept{Value: v}).Release()
	for i := start; i >= 0; i-- {
		if i >= len(l.items) {
			continue // removed by a callback: not visited, as JavaScript skips it
		}
		item := l.items[i]
		if item.kind == invalid {
			return item
		}
		if accumulator = each.accumulate(accumulator, item, i, v); accumulator.kind == invalid {
			return accumulator
		}
	}
	return accumulator
}

// Sort sorts in place and returns the array: by the comparator's number when given, else by text
// as JavaScript does ([10, 9, 1] sorts to [1, 10, 9]); undefined items go last; the sort is stable. A
// failure of the comparator ends the sort: no comparison after it calls the comparator again.
func (v Value) Sort(arguments ...Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		return v.noMethod("sort")
	}
	fn := argument(arguments, 0)
	if fn.kind != 0 && fn.kind != function {
		return Fail(TypeCode, "the comparator is not a function")
	}
	if l.frozen && len(l.items) > 1 {
		return readOnlyItem(0)
	}
	if len(l.items) < 2 {
		// nothing to compare and nothing to write, as V8 returns at once: a frozen [x] sorts as it is
		// with a comparator too (05/11/2026: the private copy below refused its write-back)
		return v
	}
	var comparator callback
	if fn.kind == function {
		comparator = newCallback(fn, 2)
	}
	var failed Value
	less := func(a, b Value) bool {
		switch {
		case failed.kind == invalid:
			return false
		case a.kind == 0:
			return false
		case b.kind == 0:
			return true
		case fn.kind == 0:
			// by text, each item's own toString run as JavaScript runs it; one that fails ends the sort
			x, failure := a.ToText()
			if failure.kind == invalid {
				failed = failure
				return false
			}
			y, failure := b.ToText()
			if failure.kind == invalid {
				failed = failure
				return false
			}
			return compareText(x, y) < 0
		case a.kind == invalid:
			failed = a
			return false
		case b.kind == invalid:
			failed = b
			return false
		}
		out := comparator.compare(a, b)
		if out.kind != number {
			if out = out.ToNumber(); out.kind == invalid {
				failed = out
				return false
			}
		}
		return out.Number() < 0
	}
	// The items are sorted as they were when the sort began, then written back, as JavaScript's
	// SortIndexedProperties collects them first: a comparator (or an item's own toString) that changes the
	// array cannot lose an item from it or write one twice (05/11/2026: a pop in the comparator left
	// [3, 1, 2] as [1, 2], a shift left [3, 1, 2, 5, 4] as [2, 1, 4, 4]). Only then can program code run
	// during the sort: a comparator, or an object among the items.
	items := l.items
	var copied *[]Value
	if fn.kind == function || holdsObject(items) {
		copied = sortCopies.Get().(*[]Value)
		items = append((*copied)[:0], items...)
	}
	// slices.SortStableFunc makes the comparisons sort.SliceStable makes, in the same order (the same
	// insertion sort and symMerge), without reflection or the slice boxed in an interface: three
	// allocations fewer a sort, which pays for the copy above (05/11/2026)
	slices.SortStableFunc(items, func(a, b Value) int {
		if less(a, b) {
			return -1
		}
		return 0
	})
	if copied == nil {
		if failed.kind == invalid {
			return failed
		}
		return v
	}
	defer releaseSortCopy(copied, items)
	if failed.kind == invalid {
		return failed
	}
	if l.frozen {
		return readOnlyItem(0)
	}
	written := copy(l.items, items)
	l.items = append(l.items, items[written:]...)
	return v
}

// sortCopies are the private copies Sort makes, kept for the next sort to take instead of a new one: each
// sort takes its own, so a comparator that sorts takes another, and gives it back cleared, so that it holds
// no item of the request that sorted (05/11/2026: the copy raised a sort's bytes by a third).
var sortCopies = sync.Pool{New: func() any { return new([]Value) }}

// maxKeptSortCopy is the most items a copy given back keeps room for: a larger one is left to the
// collector, not held by the pool.
const maxKeptSortCopy = 1 << 12

// releaseSortCopy gives back the copy a sort took, items its slice, once the items are written back.
func releaseSortCopy(copied *[]Value, items []Value) {
	if cap(items) > maxKeptSortCopy {
		return
	}
	clear(items)
	*copied = items[:0]
	sortCopies.Put(copied)
}

// holdsObject reports an object among items, or an array, whose items may be such objects: one whose own
// toString the default order may run (05/11/2026: an array of arrays of objects was sorted in place).
func holdsObject(items []Value) bool {
	for _, item := range items {
		if item.ConvertsWithCode() {
			return true
		}
	}
	return false
}

// ToSorted is a.toSorted(comparator): Sort on a new array; a is left as it was.
func (v Value) ToSorted(arguments ...Value) Value {
	if v.kind != array {
		return v.noMethod("toSorted")
	}
	if fn := argument(arguments, 0); fn.kind != 0 && fn.kind != function {
		return Fail(TypeCode, "the comparator is not a function")
	}
	return Array(append([]Value(nil), v.Items()...)).Sort(arguments...)
}
