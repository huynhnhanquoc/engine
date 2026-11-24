// Package value is the one type a Kitwork program holds: every constant, variable, argument and result
// of a .work program is a Value, 32 bytes, with numbers kept inline so arithmetic never allocates.
//
// Value{} is undefined. A failure is a Value too (Fail), carried through every operation unchanged
// until safe() reads it; no Go error runs beside the program and nothing here panics on a program's
// input.
//
// What a program can call on a value is every exported method with the signature (...Value) Value
// (javascript.go); every other method is for Go. A module's struct adds one more: a method of no
// parameter, () Value, is a property the program reads without a call (method.go).
package value

import (
	"math"
	"time"
)

// Value is a value of the program. Its fields never leave the package: a Value is built by a
// constructor (Number, String, Struct, …) and read by an accessor (Number(), Text(), …), so the
// kind always says what the payload is.
type Value struct {
	kind   kind
	number uint64 // Number: the bits of a float64 · Boolean: 0 or 1 · Time: ns since 1970 UTC · Duration: ns
	data   any    // String: string · Bytes: []byte · Object: *properties · Array: *list · Struct: a pointer · Function: *callable · Invalid: *failure
}

// callable is what a Function value holds: a Go function, or a program's lambda (lambda.go), and the
// properties a program or a host set on it (Number.isNaN, Object.keys): a JavaScript function is an
// object too, and that is all of "object" a Kitwork function has.
type callable struct {
	native     func(arguments ...Value) Value
	lambda     *Lambda
	properties *properties

	// name and length are what fn.name and fn.length read for a Go function (a lambda answers from its
	// own Name and Params): "" and 0 unless NamedFunction or a method read gave them.
	name   string
	length int32
	// prototype is, for a method read without a call ('abc'.at), the kind whose JavaScript prototype
	// holds the method; 0 for every other function. Two reads of one method are === by it and the name,
	// as in JavaScript, though each read is a new function bound to its receiver (method, javascript.go).
	prototype kind
	// fresh marks a Go function whose result is all new (Fresh, footprint.go); it sits in prototype's padding, so
	// callable, inside every Lambda, keeps its size
	fresh bool
	// owner is, for a struct's method read without a call (napas.bank), the table of the struct's type:
	// two kinds of struct may each have a bank, and only the reads of one type's are === (method.go).
	owner *Methods
	// stands is, for a stand-in of a Go function (HandingOver), the function it stands in for, which it
	// is to === (Equal). It keeps callable, which lives inside every Lambda, in its size class: a Lambda
	// is 240 bytes with it, as without (05/11/2026).
	stands *callable
	// within is, for a Go function that makes much from little (FunctionWithin), the function a run calls with
	// its Budget (CallWithin). It takes the 8 bytes length gave up as an int32: a Lambda stays 240 bytes.
	within func(budget Budget, arguments ...Value) Value
}

// A stand-in (RunningOn) is a callable with both lambda and native: the program's function, run through
// native rather than through its lambda's Run. It costs callable no field: callable lives inside every
// Lambda, one allocation a closure, which two more fields made a size class larger (05/11/2026).

// standsIn reports a stand-in.
func (c *callable) standsIn() bool { return c.lambda != nil && c.native != nil }

// RunningOn is v, a program's function, as a stand-in a host calls through run instead of the Run of
// the VM that made it. The runtime hands a host call a function another VM made this way (a function
// of a file's top level, made as the site booted, given to sort at a request), so that the host's calls
// of it run on the VM of the request making the host call: the VM that made it is shared by every
// request at once and runs nothing more. The stand-in is v to === (Equal: the same lambda) and holds v's
// properties as they are. Any other value is itself.
func (v Value) RunningOn(run func(lambda *Lambda, arguments []Value) Value) Value {
	c, ok := v.data.(*callable)
	if !ok || c.lambda == nil || run == nil {
		return v
	}
	lambda := c.lambda
	return Value{kind: function, data: &callable{
		lambda:     lambda,
		native:     func(arguments ...Value) Value { return run(lambda, arguments) },
		properties: c.properties,
	}}
}

// HandingOver is v, a Go function, as a stand-in that passes each argument it is called with through hand
// first, for a host call that hands v a program's function it did not make: the runtime hands a host call
// a Go function this way (safe given to map, to Array.from), so that a function of a frozen file's top
// level it then receives (an item of the array) runs on the VM making the host call, as one handed to the
// host directly does (RunningOn). The stand-in is v to ===, has v's name and length, reads and writes v's
// properties (properties, through stands), and costs nothing but itself when no argument is a program's
// function. Any other value is itself.
func (v Value) HandingOver(hand func(argument Value) Value) Value {
	c, ok := v.data.(*callable)
	if !ok || c.native == nil || c.lambda != nil || hand == nil {
		return v
	}
	if c.stands != nil {
		c = c.stands
	}
	original := c.native
	return Value{kind: function, data: &callable{
		native: func(arguments ...Value) Value {
			var handed []Value
			for i, argument := range arguments {
				if argument.kind != function {
					continue
				}
				if next := hand(argument); next.data != argument.data {
					if handed == nil {
						handed = append([]Value(nil), arguments...)
					}
					handed[i] = next
				}
			}
			if handed != nil {
				return original(handed...)
			}
			return original(arguments...)
		},
		name:      c.name,
		length:    c.length,
		prototype: c.prototype,
		owner:     c.owner,
		stands:    c,
	}}
}

// ---- constructors: one per kind, every one a function; none takes any, so the kind always matches
// the payload ----

func Undefined() Value { return Value{} }

func Null() Value { return Value{kind: null} }

func Boolean(b bool) Value {
	if b {
		return Value{kind: boolean, number: 1}
	}
	return Value{kind: boolean}
}

func Number(f float64) Value { return Value{kind: number, number: math.Float64bits(f)} }

func String(s string) Value { return Value{kind: text, data: s} }

func Bytes(b []byte) Value { return Value{kind: bytes, data: b} }

// Time is an instant kept to the nanosecond, from 1678 to 2262 (the range of int64 nanoseconds);
// outside it, and for the zero time, it is a RANGE failure rather than a wrong instant. A time
// compares by value and adds a Duration.
func Time(t time.Time) Value {
	if t.IsZero() || t.Before(earliestTime) || t.After(latestTime) {
		return Fail(RangeCode, "time out of range: "+t.Format(time.RFC3339))
	}
	return Value{kind: instant, number: uint64(t.UnixNano())}
}

var (
	earliestTime = time.Unix(0, math.MinInt64).UTC()
	latestTime   = time.Unix(0, math.MaxInt64).UTC()
)

func Duration(d time.Duration) Value { return Value{kind: duration, number: uint64(d)} }

// Array holds items by reference: pushing into one copy is seen through every copy.
func Array(items []Value) Value {
	if items == nil {
		items = []Value{}
	}
	return Value{kind: array, data: &list{items: items}}
}

// Object is an empty object; its keys keep the order JavaScript gives them (object.go).
func Object() Value { return Value{kind: object, data: newProperties()} }

// Struct is a Go value the program calls methods of, by pointer so that two values of the same
// struct are === and every method sees the one struct. The first Struct of a type exposes its
// methods and properties (method.go); nothing registers at boot.
func Struct[T any](h *T) Value {
	if h == nil {
		return Null()
	}
	exposed[T]()
	return Value{kind: host, data: h}
}

// Function is a Go function the program calls.
func Function(f func(arguments ...Value) Value) Value {
	if f == nil {
		return Fail(TypeCode, "a function without a body")
	}
	return Value{kind: function, data: &callable{native: f}}
}

// NamedFunction is a Go function with the name and length JavaScript gives its builtin (Math.max is
// "max" with length 2), for fn.name and fn.length; Function's are "" and 0.
func NamedFunction(name string, length int, f func(arguments ...Value) Value) Value {
	if f == nil {
		return Fail(TypeCode, "a function without a body")
	}
	return Value{kind: function, data: &callable{native: f, name: name, length: int32(min(max(length, 0), 1<<20))}}
}

// FunctionWithin is a Go function that makes much from little (JSON.stringify, String of an array, Array.from):
// a run calls it with its Budget (CallWithin), which it asks as what it makes grows; called without one (Call),
// it is given none and makes at most UnbudgetedLength.
func FunctionWithin(f func(budget Budget, arguments ...Value) Value) Value {
	if f == nil {
		return Fail(TypeCode, "a function without a body")
	}
	return Value{kind: function, data: &callable{
		native: func(arguments ...Value) Value { return f(nil, arguments...) },
		within: f,
	}}
}

// CallWithin is Call by a run whose budget is budget: a function FunctionWithin made is given it, any other is
// called as Call calls it. A Go function's call, the engine's or a module's, costs the run's energy for the texts it
// is handed, which it reads (HandedText, work.go): parseInt, encodeURIComponent, a text written to a file. A program's
// function is not counted so: its instructions count themselves; nor a method read without a call, which counts what
// it reads itself (countedCall).
func (v Value) CallWithin(budget Budget, arguments []Value) Value {
	c, ok := v.data.(*callable)
	if !ok || v.kind != function || budget == nil || c.lambda != nil {
		return v.Call(arguments)
	}
	var result Value
	if c.within != nil {
		if bad := firstFailure(arguments); bad.kind != 0 {
			return bad
		}
		result = c.within(budget, arguments...)
	} else {
		result = v.Call(arguments)
	}
	if c.prototype == 0 {
		if spent := Spend(budget, HandedText(arguments)); spent.kind == invalid {
			return spent
		}
	}
	return result
}

// ---- accessors: Go reads the payload; a wrong kind reads as the zero value, never as bits ----

func (v Value) Number() float64 {
	if v.kind != number {
		return 0
	}
	return math.Float64frombits(v.number)
}

// Integer is the number as an int64: the fraction dropped, NaN 0, out of range clamped (never wrapped).
func (v Value) Integer() int64 { return saturatedInt64(v.Number()) }

func (v Value) Boolean() bool { return v.kind == boolean && v.number == 1 }

// Text is the payload of a String; "" for every other kind. ToString is what a program means.
func (v Value) Text() string {
	s, _ := v.data.(string)
	return s
}

func (v Value) Bytes() []byte {
	b, _ := v.data.([]byte)
	return b
}

func (v Value) Time() time.Time {
	if v.kind != instant {
		return time.Time{}
	}
	return time.Unix(0, int64(v.number)).UTC()
}

func (v Value) Duration() time.Duration {
	if v.kind != duration {
		return 0
	}
	return time.Duration(v.number)
}

// Items is the array's backing slice; nil for every other kind.
func (v Value) Items() []Value {
	if l, ok := v.data.(*list); ok {
		return l.items
	}
	return nil
}

// Host is the pointer a Struct holds; nil for every other kind.
func (v Value) Host() any {
	if v.kind == host {
		return v.data
	}
	return nil
}

// ---- predicates ----

func (v Value) IsUndefined() bool { return v.kind == 0 }
func (v Value) IsNull() bool      { return v.kind == null }
func (v Value) IsNumber() bool    { return v.kind == number }
func (v Value) IsBoolean() bool   { return v.kind == boolean }
func (v Value) IsString() bool    { return v.kind == text }
func (v Value) IsBytes() bool     { return v.kind == bytes }
func (v Value) IsTime() bool      { return v.kind == instant }
func (v Value) IsDuration() bool  { return v.kind == duration }
func (v Value) IsObject() bool    { return v.kind == object }
func (v Value) IsArray() bool     { return v.kind == array }
func (v Value) IsStruct() bool    { return v.kind == host }
func (v Value) IsFunction() bool  { return v.kind == function }
func (v Value) IsInvalid() bool   { return v.kind == invalid }

// IsNullish is null or undefined: what ?? and ?. test.
func (v Value) IsNullish() bool { return v.kind <= null }

// Kind is the kind's name for a person ("number", "array", "failure"), and a struct's its type's ("napas",
// "request"); Typeof is what a program sees.
func (v Value) Kind() string {
	if v.kind == host {
		return hostName(v.data)
	}
	return v.kind.name()
}
