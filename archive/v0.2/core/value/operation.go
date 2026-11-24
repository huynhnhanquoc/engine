package value

import (
	"math"
	"strings"
	"time"
)

// The operations the runtime runs for the program's operators and conversions. None is variadic, so
// none reaches a program as a method: `typeof x`, `+x`, `a === b`, `a < b`, `a + b`, `a.b`, `a[i]`.
// A failure on either side comes out unchanged: the program stops at the instruction that made it,
// and a Go caller that chains calls sees the first one.

// ---- conversions (ECMAScript 7.1) ----

// Typeof is what `typeof v` answers: twelve kinds folded into JavaScript's seven words.
func (v Value) Typeof() Value {
	if v.kind == invalid {
		return v
	}
	return typeofText[v.kind]
}

// typeofText is the String typeof gives for each kind, made once: a String made from the word on each
// call boxes its text, one allocation per typeof in a loop.
var typeofText = func() (table [invalid + 1]Value) {
	for k := range table {
		table[k] = String(kind(k).typeof())
	}
	return table
}()

// ToBoolean is what `if (v)`, `!v`, `&&` and `||` test. A failure is true, so that `if (x)` cannot
// swallow one on its way to safe().
func (v Value) ToBoolean() bool {
	switch v.kind {
	case 0, null:
		return false
	case boolean:
		return v.number == 1
	case number:
		f := v.Number()
		return f != 0 && f == f
	case text:
		return v.Text() != ""
	}
	return true
}

// ToNumber is what `+v`, `Number(v)`, `-`, `*` apply: a number itself, null 0, undefined NaN, a boolean
// 0 or 1, a string read as JavaScript reads it, a time its milliseconds (as a Date), a duration its
// milliseconds, an array through its text ([5] is 5, [1,2] NaN), everything else NaN.
func (v Value) ToNumber() Value {
	switch v.kind {
	case number, invalid:
		return v
	case 0:
		return Number(math.NaN())
	case null:
		return Number(0)
	case boolean:
		return Number(float64(v.number))
	case text:
		return Number(stringToNumber(v.Text()))
	case instant:
		return Number(float64(v.Time().UnixMilli()))
	case duration:
		return Number(float64(v.Duration()) / 1e6)
	case array:
		text := v.ToString()
		if text.kind == invalid {
			return text
		}
		return Number(stringToNumber(text.Text()))
	case object:
		if primitive, ok := v.ownPrimitive(false); ok {
			if primitive.kind == invalid {
				return primitive
			}
			return primitive.ToNumber()
		}
	}
	return Number(math.NaN())
}

// toPrimitive is ToPrimitive with the default hint, for `+` and `==`: a scalar is itself, a time is
// its text (a Date's default hint is string), an object through its own valueOf and toString
// (primitive.go), an array its text. A failure of the conversion is the result.
func (v Value) toPrimitive() Value {
	if v.kind <= duration && v.kind != instant || v.kind == text || v.kind == invalid {
		return v
	}
	if primitive, ok := v.ownPrimitive(false); ok {
		return primitive
	}
	return v.ToString()
}

// toPrimitiveNumber is ToPrimitive with the number hint, for `<`: a time is its milliseconds, as a
// Date's valueOf; the rest as toPrimitive.
func (v Value) toPrimitiveNumber() Value {
	if v.kind == instant {
		return v.ToNumber()
	}
	return v.toPrimitive()
}

// ---- equality and order (ECMAScript 7.2) ----

// Equal is `===`: same kind and same value for a scalar, a string, a time, a duration or bytes; the
// same object, array, function or struct otherwise. NaN is not equal to itself.
func (a Value) Equal(b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case 0, null:
		return true
	case number:
		return a.Number() == b.Number()
	case boolean, instant, duration:
		return a.number == b.number
	case text:
		return a.Text() == b.Text()
	case bytes:
		return string(a.Bytes()) == string(b.Bytes())
	case function:
		// a method read twice is one function to JavaScript ([1].map === [2].map, [].concat !== ''.concat),
		// a struct's by its type and name (napas.bank === napas.bank)
		x, y := a.data.(*callable), b.data.(*callable)
		// a stand-in is the function it stands in for (RunningOn, HandingOver)
		if x.stands != nil {
			x = x.stands
		}
		if y.stands != nil {
			y = y.stands
		}
		if (x.standsIn() || y.standsIn()) && x.lambda == y.lambda {
			return true
		}
		return x == y || x.prototype != 0 && x.prototype == y.prototype && x.name == y.name && x.owner == y.owner
	}
	return a.data == b.data // every other payload is a pointer, so this is identity and never panics
}

// LooseEqual is `==`: null equals undefined, a number and a string compare as numbers, a boolean as
// its number, an object against a primitive through its text; otherwise as `===`. A conversion that fails
// makes it false; LooseEquals gives that failure, and is what the runtime runs when a side is an object or
// an array.
func (a Value) LooseEqual(b Value) bool {
	if a.kind == b.kind {
		return a.Equal(b)
	}
	if a.IsNullish() && b.IsNullish() {
		return true
	}
	if a.IsNullish() || b.IsNullish() || a.kind.isObject() && b.kind.isObject() {
		return false
	}
	if a.kind == boolean {
		return a.ToNumber().LooseEqual(b)
	}
	if b.kind == boolean {
		return a.LooseEqual(b.ToNumber())
	}
	a, b = a.toPrimitive(), b.toPrimitive()
	if a.kind == b.kind {
		return a.Equal(b)
	}
	if a.IsNullish() || b.IsNullish() {
		return false // as LooseEquals says
	}
	if a.kind == number || b.kind == number || a.kind == boolean || b.kind == boolean {
		return a.ToNumber().Equal(b.ToNumber())
	}
	return false
}

// LooseEquals is `==` as LooseEqual, as a Boolean, or the failure of a conversion it makes (an object's
// own valueOf), as JavaScript throws out of ==: the runtime's ==, where a failure read as false made
// price == 42 false for an object whose valueOf failed (05/11/2026).
func (a Value) LooseEquals(b Value) Value {
	return a.looseEquals(b, nil)
}

// LooseEqualsWithin is LooseEquals counting what it reads on budget, as the runtime's == does (15/11/2026): two
// texts compared once converted (StrictWork), a text made a number (NumberWork); the run's energy failure once
// the run is past its energy.
func (a Value) LooseEqualsWithin(b Value, budget Budget) Value {
	return a.looseEquals(b, budget)
}

func (a Value) looseEquals(b Value, budget Budget) Value {
	if a.kind == b.kind {
		if spent := spendRead(budget, StrictWork(a, b)); spent.kind == invalid {
			return spent
		}
		return Boolean(a.Equal(b))
	}
	if a.IsNullish() && b.IsNullish() {
		return Boolean(true)
	}
	// two objects of other kinds (an object and an array, a function or a time) are not one object, so
	// not equal, and neither is converted, as JavaScript compares two objects (05/11/2026: both were
	// converted, so [1] == { toString: () => "1" } was true, and a failure of either was the answer)
	if a.IsNullish() || b.IsNullish() || a.kind.isObject() && b.kind.isObject() {
		return Boolean(false)
	}
	if a.kind == boolean {
		return a.ToNumber().looseEquals(b, budget)
	}
	if b.kind == boolean {
		return a.looseEquals(b.ToNumber(), budget)
	}
	if a = a.toPrimitive(); a.kind == invalid {
		return a
	}
	if b = b.toPrimitive(); b.kind == invalid {
		return b
	}
	if a.kind == b.kind {
		if spent := spendRead(budget, StrictWork(a, b)); spent.kind == invalid {
			return spent
		}
		return Boolean(a.Equal(b))
	}
	// one side was an object, the other a number or a text, and ToPrimitive(object) == other is compared
	// as JavaScript compares two primitives: a null or undefined it gave equals neither, and a boolean is
	// its number (05/11/2026: the null was read as 0, so { valueOf: () => null } == 0 was true, and a
	// boolean against a text was false: { valueOf: () => true } == "1")
	if a.IsNullish() || b.IsNullish() {
		return Boolean(false)
	}
	if a.kind == number || b.kind == number || a.kind == boolean || b.kind == boolean {
		if spent := spendRead(budget, NumberWork(a)+NumberWork(b)); spent.kind == invalid {
			return spent
		}
		x, y := a.ToNumber(), b.ToNumber()
		if x.kind == invalid {
			return x
		}
		if y.kind == invalid {
			return y
		}
		return Boolean(x.Equal(y))
	}
	return Boolean(false)
}

// Less is `a < b`: two strings by unit, a time or a duration against its own kind by value, everything
// else as numbers; false when either side is NaN.
func (a Value) Less(b Value) bool {
	if a.kind == number && b.kind == number { // a loop's i < n: no conversion to run
		return math.Float64frombits(a.number) < math.Float64frombits(b.number)
	}
	if a.kind == b.kind && (a.kind == instant || a.kind == duration) {
		return int64(a.number) < int64(b.number)
	}
	a, b = a.toPrimitiveNumber(), b.toPrimitiveNumber()
	if a.kind == text && b.kind == text {
		return compareText(a.Text(), b.Text()) < 0
	}
	return a.ToNumber().Number() < b.ToNumber().Number()
}

// LessEqual is `a <= b`: false when either side is NaN, as JavaScript says (NaN orders with nothing).
// Two numbers compare as floats, where NaN already orders with nothing; any other pair converts once
// in Less and once more in unordered, which only the rare non-number comparison pays.
func (a Value) LessEqual(b Value) bool {
	if a.kind == number && b.kind == number {
		return math.Float64frombits(a.number) <= math.Float64frombits(b.number)
	}
	return !b.Less(a) && !unordered(a, b)
}

func (a Value) Greater(b Value) bool {
	if a.kind == number && b.kind == number {
		return math.Float64frombits(a.number) > math.Float64frombits(b.number)
	}
	return b.Less(a)
}

func (a Value) GreaterEqual(b Value) bool {
	if a.kind == number && b.kind == number {
		return math.Float64frombits(a.number) >= math.Float64frombits(b.number)
	}
	return !a.Less(b) && !unordered(a, b)
}

func unordered(a, b Value) bool {
	if a.kind == b.kind && (a.kind == instant || a.kind == duration) {
		return false
	}
	x, y := a.toPrimitiveNumber(), b.toPrimitiveNumber()
	if x.kind == text && y.kind == text {
		return false
	}
	return math.IsNaN(x.ToNumber().Number()) || math.IsNaN(y.ToNumber().Number())
}

// ---- arithmetic ----

// Add is `a + b`: two numbers add, a string on either side joins the texts, a time and a duration
// make a time, two durations a duration; otherwise both sides become numbers.
func (a Value) Add(b Value) Value {
	sum, _ := a.AddJoining(b, nil)
	return sum
}

// AddJoining is Add by a run that builds texts with j (join.go): a text joined is written after the text j holds when
// the left side is it, and made is the bytes the join allocated, 0 for a number, a time, a duration, a side given back
// whole and a text written in a buffer's room. A nil j joins as Add does.
func (a Value) AddJoining(b Value, j *Joiner) (Value, uint64) {
	switch {
	case a.kind == number && b.kind == number:
		return Number(a.Number() + b.Number()), 0
	case a.kind == invalid:
		return a, 0
	case b.kind == invalid:
		return b, 0
	case a.kind == instant && b.kind == duration:
		return Time(a.Time().Add(b.Duration())), 0
	case a.kind == duration && b.kind == instant:
		return Time(b.Time().Add(a.Duration())), 0
	case a.kind == duration && b.kind == duration:
		return Duration(a.Duration() + b.Duration()), 0
	case a.kind == text && b.kind == text:
		// two strings, the common join: no conversion, the longest string checked (Join)
		return j.Join(a.data.(string), b.data.(string))
	}
	// the left side first, and the right only once it converted, as JavaScript stops at the left side's
	// throw (05/11/2026: both converted, so the right side's valueOf ran after the left side failed)
	x := a.toPrimitive()
	if x.kind == invalid {
		return x, 0
	}
	y := b.toPrimitive()
	if y.kind == invalid {
		return y, 0
	}
	if x.kind == text || y.kind == text {
		left, right := x.ToString(), y.ToString()
		if left.kind == invalid {
			return left, 0
		}
		if right.kind == invalid {
			return right, 0
		}
		// the longest string, as repeat and padStart have it: doubling a string by + passed it, a
		// gigabyte for a few hundred instructions of energy (05/11/2026); Join checks it
		return j.Join(left.Text(), right.Text())
	}
	return Number(x.ToNumber().Number() + y.ToNumber().Number()), 0
}

// Template is a template literal's `${b}` after its text a: b made text by ToString, the string hint (an
// object's own toString before its valueOf, as JavaScript's template takes it), joined to a. A + takes
// the default hint instead: "" + o runs o's valueOf first, `${o}` its toString. A failure on either side,
// or of the conversion, is the result; so is a text past the longest string.
func (a Value) Template(b Value) Value {
	joined, _ := a.TemplateJoining(b, nil)
	return joined
}

// TemplateJoining is Template by a run that builds texts with j, as AddJoining is Add.
func (a Value) TemplateJoining(b Value, j *Joiner) (Value, uint64) {
	switch {
	case a.kind == text && b.kind == text:
		return j.Join(a.data.(string), b.data.(string))
	case a.kind == invalid:
		return a, 0
	case b.kind == invalid:
		return b, 0
	}
	left, failure := a.ToText()
	if failure.kind == invalid {
		return failure, 0
	}
	right, failure := b.ToText()
	if failure.kind == invalid {
		return failure, 0
	}
	return j.Join(left, right)
}

// Subtract is `a - b`: a time less a duration is a time, a time less a time is a duration, two
// durations a duration; otherwise both sides become numbers.
func (a Value) Subtract(b Value) Value {
	switch {
	case a.kind == number && b.kind == number:
		return Number(a.Number() - b.Number())
	case a.kind == invalid:
		return a
	case b.kind == invalid:
		return b
	case a.kind == instant && b.kind == duration:
		return Time(a.Time().Add(-b.Duration()))
	case a.kind == instant && b.kind == instant:
		return Duration(a.Time().Sub(b.Time()))
	case a.kind == duration && b.kind == duration:
		return Duration(a.Duration() - b.Duration())
	}
	x, y, bad := numbers(a, b)
	if bad.kind == invalid {
		return bad
	}
	return Number(x - y)
}

// Multiply is `a * b`; a duration times a number scales the duration.
func (a Value) Multiply(b Value) Value {
	switch {
	case a.kind == number && b.kind == number: // first, as in Add: the common case pays one test
		return Number(math.Float64frombits(a.number) * math.Float64frombits(b.number))
	case a.kind == invalid:
		return a
	case b.kind == invalid:
		return b
	case a.kind == duration && b.kind == number:
		return Duration(time.Duration(float64(a.Duration()) * b.Number()))
	case a.kind == number && b.kind == duration:
		return Duration(time.Duration(a.Number() * float64(b.Duration())))
	}
	x, y, bad := numbers(a, b)
	if bad.kind == invalid {
		return bad
	}
	return Number(x * y)
}

// Divide is `a / b` by IEEE 754, as JavaScript: 1 / 0 is Infinity, 0 / 0 is NaN. A duration over a
// number is a duration; a duration over a duration is a number.
func (a Value) Divide(b Value) Value {
	switch {
	case a.kind == number && b.kind == number:
		return Number(math.Float64frombits(a.number) / math.Float64frombits(b.number))
	case a.kind == invalid:
		return a
	case b.kind == invalid:
		return b
	case a.kind == duration && b.kind == number:
		return Duration(time.Duration(float64(a.Duration()) / b.Number()))
	case a.kind == duration && b.kind == duration:
		return Number(float64(a.Duration()) / float64(b.Duration()))
	}
	x, y, bad := numbers(a, b)
	if bad.kind == invalid {
		return bad
	}
	return Number(x / y)
}

// Modulo is JavaScript's `%`: the sign of the dividend, NaN for x % 0 and Infinity % x, x % Infinity is x.
func (a Value) Modulo(b Value) Value {
	if a.kind == number && b.kind == number {
		x, y := math.Float64frombits(a.number), math.Float64frombits(b.number)
		// i % n of two whole numbers is the integer remainder, exact below 2^53, where math.Mod runs a
		// software frexp/ldexp loop; a negative dividend, a fraction, NaN and Infinity take math.Mod.
		if x >= 0 && y > 0 && x < 1<<53 && y < 1<<53 {
			if dividend, divisor := int64(x), int64(y); float64(dividend) == x && float64(divisor) == y {
				if dividend == 0 {
					return a // -0 % y is -0: the dividend keeps its sign
				}
				return Number(float64(dividend % divisor))
			}
		}
		return Number(math.Mod(x, y))
	}
	if a.kind == invalid {
		return a
	}
	if b.kind == invalid {
		return b
	}
	x, y, bad := numbers(a, b)
	if bad.kind == invalid {
		return bad
	}
	return Number(math.Mod(x, y))
}

// Power is `a ** b`; JavaScript says 1 ** Infinity is NaN where Go says 1.
func (a Value) Power(b Value) Value {
	if a.kind == invalid {
		return a
	}
	if b.kind == invalid {
		return b
	}
	x, y, bad := numbers(a, b)
	if bad.kind == invalid {
		return bad
	}
	if y != y || (math.Abs(x) == 1 && math.IsInf(y, 0)) {
		return Number(math.NaN())
	}
	return Number(math.Pow(x, y))
}

// Negate is `-v`; a duration negates as a duration.
func (v Value) Negate() Value {
	switch v.kind {
	case invalid:
		return v
	case duration:
		return Duration(-v.Duration())
	}
	n := v.ToNumber()
	if n.kind == invalid {
		return n
	}
	return Number(-n.Number())
}

// Not is `!v`.
func (v Value) Not() Value {
	if v.kind == invalid {
		return v
	}
	return Boolean(!v.ToBoolean())
}

// ---- members ----

// Get is `v.key` for a key that is not a call: an object's own key; the length of an array, a string
// or bytes, and their item at a canonical index ("0", "12": l['0'] is l[0]); a function's length and
// name; else a method of the kind read as a function ('abc'.at, [].map), or undefined when the kind
// has none of that name. On null or undefined it is a TYPE failure, as JavaScript throws; the .ui
// safety layer that reads every access as ?. is the compiler's, not this package's.
func (v Value) Get(key string) Value {
	switch v.kind {
	case object:
		if item, ok := v.properties().get(key); ok {
			return item
		}
	case function:
		c := v.data.(*callable)
		if o := v.properties(); o != nil { // a stand-in's are its function's
			if item, ok := o.get(key); ok {
				return item
			}
		}
		switch key {
		case "length":
			if c.lambda != nil {
				return Number(float64(c.lambda.Length()))
			}
			return Number(float64(c.length))
		case "name":
			if c.lambda != nil {
				return String(lambdaName(c.lambda.Name))
			}
			return String(c.name)
		}
	case array, text, bytes:
		if key == "length" {
			return Number(float64(v.length()))
		}
		if i, ok := arrayIndex(key); ok {
			return v.Index(int(i))
		}
	case host:
		return propertyOfHost(v.data, key) // request.url, a method of no parameter; napas.bank, a bound function
	case 0, null:
		return v.cannotRead(key)
	case invalid:
		return v
	}
	return v.method(key)
}

// length is the length of an array, a string in UTF-16 units, or bytes; 0 for every other kind.
func (v Value) length() int {
	switch v.kind {
	case array:
		return len(v.Items())
	case text:
		return unitLength(v.Text())
	case bytes:
		return len(v.Bytes())
	}
	return 0
}

// lambdaName is a lambda's name as fn.name reads it: the compiler names an anonymous lambda for a
// stack trace with a word no source can write ("<anonymous>"), and JavaScript's name for it is "".
func lambdaName(name string) string {
	if strings.HasPrefix(name, "<") {
		return ""
	}
	return name
}

func (v Value) cannotRead(key string) Value {
	return Fail(TypeCode, "cannot read properties of "+v.Kind()+" (reading '"+key+"')")
}

// Index is `v[i]` for an integer i: an array's item, a string's unit as a one-unit string, a byte as
// a number, else undefined; an index outside is undefined, as in JavaScript.
func (v Value) Index(i int) Value {
	switch v.kind {
	case array:
		items := v.Items()
		if i < 0 || i >= len(items) {
			return Value{}
		}
		return items[i]
	case text:
		if unit, ok := unitAt(v.Text(), i); ok {
			return String(unitString(unit))
		}
		return Value{}
	case bytes:
		b := v.Bytes()
		if i < 0 || i >= len(b) {
			return Value{}
		}
		return Number(float64(b[i]))
	case 0, null:
		return v.cannotRead(numberText(float64(i)))
	case invalid:
		return v
	}
	return Value{}
}

// SetIndex is `v[i] = item` on an array: past the end the array grows, the gap filled with undefined
// (JavaScript keeps a hole there; a hole and an undefined differ only to indexOf and flat). A frozen
// array refuses it, before anything else is looked at: an item it has is read only, any other index
// (one past the end, a negative one) one it cannot add, as JavaScript words both.
func (v Value) SetIndex(i int, item Value) Value {
	l, ok := v.data.(*list)
	if !ok {
		if v.kind == invalid {
			return v
		}
		return Fail(TypeCode, "cannot index "+v.Kind())
	}
	if l.frozen {
		if i >= 0 && i < len(l.items) {
			return readOnlyItem(i)
		}
		return addedItem(i)
	}
	if i < 0 {
		return Fail(RangeCode, "negative index")
	}
	if item.kind == invalid {
		return item
	}
	for i >= len(l.items) {
		l.items = append(l.items, Value{})
	}
	l.items[i] = item
	return v
}

// Call is `v(arguments…)` on a function; anything else is a TYPE failure, as JavaScript throws. It
// takes a slice, not a variadic, so it is the runtime's and not a method of the program.
func (v Value) Call(arguments []Value) Value {
	switch v.kind {
	case function:
		if bad := firstFailure(arguments); bad.kind != 0 {
			return bad
		}
		c := v.data.(*callable)
		if c.lambda != nil && c.native == nil {
			if c.lambda.Run == nil {
				return Fail(TypeCode, "a program function with no runtime to run it")
			}
			return c.lambda.Run(c.lambda, arguments)
		}
		// a Go function, or a stand-in's run (RunningOn)
		return c.native(arguments...)
	case invalid:
		return v
	}
	return Fail(TypeCode, v.Kind()+" is not a function")
}

// Invoke is `v.name(arguments…)`: an object's own key when it is a function, a struct's method from
// its registered table, every other kind's from the (...Value) Value methods of Value. An unknown name
// is a TYPE failure. arguments may be a window on the caller's stack: a method reads it and lets go.
func (v Value) Invoke(name string, arguments []Value) Value {
	return v.InvokeWithin(nil, name, arguments)
}

// InvokeWithin is Invoke by a run whose budget is budget: an array's methods that make much from little ask it
// as what they make grows, and an argument a method reads as text is made text within it.
func (v Value) InvokeWithin(budget Budget, name string, arguments []Value) Value {
	switch v.kind {
	case invalid:
		return v
	case 0, null:
		return v.cannotRead(name)
	}
	if bad := firstFailure(arguments); bad.kind != 0 {
		return bad
	}
	switch v.kind {
	case host:
		return invokeHost(v.data, budget, name, arguments)
	case object, function:
		if o := v.properties(); o != nil {
			if own, ok := o.get(name); ok {
				if own.kind == function {
					return own.Call(arguments)
				}
				return Fail(TypeCode, name+" is not a function")
			}
		}
	}
	// an array's methods that make much from little are called with the budget (a switch, not a map: every
	// method of an array, push too, passes here)
	if budget != nil && v.kind == array {
		switch name {
		case "join":
			return v.joinWithin(budget, arguments)
		case "toString":
			return v.TextWithin(budget)
		case "flat":
			return v.flatWithin(budget, arguments)
		}
	}
	if method, ok := valueMethods[name]; ok {
		// what it reads past its call's price is counted on the run's energy (work.go): a method that reads nothing
		// (push, an array's at) or another kind's with no argument is not looked at, written here, so the common
		// call pays no call more for it
		if budget != nil && (v.kind == text && method.text != readsNothing || v.kind == array && method.array != readsNothing ||
			v.kind != text && v.kind != array && len(arguments) > 0) && method.worth(v, arguments) {
			return countedCall(v, method, arguments, budget)
		}
		// an argument that could run code to convert is converted first, as the method converts it
		// (conversion.go): the test is written here, so the common call pays no call more for it; a
		// method that converts no argument for any kind (push), or none for this kind (an array's indexOf,
		// which a string's converts), looks at none, and an argument the method keeps as it is is passed over
		if method.hints != nil {
			if hints := method.hints[v.kind]; hints != "" {
				for i, argument := range arguments {
					if argument.ConvertsWithCode() && hintAt(hints, i) != 'k' {
						return callConverted(v, hints, method.call, arguments, budget)
					}
				}
			}
		}
		return method.call(v, arguments...)
	}
	return v.noMethod(name)
}

// firstFailure is the first failure among arguments, or Value{}: a failure handed to a call comes
// back out, as it does from an operator.
func firstFailure(arguments []Value) Value {
	for _, argument := range arguments {
		if argument.kind == invalid {
			return argument
		}
	}
	return Value{}
}

// noMethod is a method called on a kind it is not written for; a failure passes through instead, so
// a Go caller chaining v.Slice(…).Push(…) sees the first failure, as the program does.
func (v Value) noMethod(name string) Value {
	if v.kind == invalid {
		return v
	}
	return Fail(TypeCode, v.Kind()+" has no method "+name)
}
