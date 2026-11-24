package value

import "strings"

// A method of JavaScript converts its arguments as it reads them: 'akb'.split(p) makes p a string,
// 'abc'.at(n) makes n a number, and a conversion that throws (an object's own toString that fails, one a
// frozen file made that a request cannot run) throws out of the method. The methods of javascript.go read
// a converted argument as Text() or Number(), which read a failure as "" or 0, so the failure was lost:
// 'akb'.split(p) split on "" and 'hello'.replaceAll(p, 'L') replaced at every place (05/11/2026).
//
// So a method's arguments that can run code to convert (an object, an array, whose items can be such
// objects) are converted before the method runs, each as the method converts it and in order, and the
// first failure is the method's result: methodHints says, for each method that converts its arguments,
// how each is converted. The method then reads a primitive, which converts to the same text or number
// it would have, and runs nothing.

// methodHints is, per kind of receiver and method, how the method converts each of its arguments: 't' to
// text (ToString), 'n' to a number (ToNumber), 'k' kept as it is, 'f' a function kept (a replacer the method calls
// back) and anything else to text; a '*' after a letter converts every
// argument from there on so. An argument past the string is kept. Object.prototype's methods, which every
// kind answers for, are objectMethodHints.
//
// An argument JavaScript may return before reading, or reads out of order, is kept here and converted by
// the method itself, its failure kept, where JavaScript converts it: the filler of padStart and padEnd
// once there is something to pad, the fromIndex of an array's includes, indexOf and lastIndexOf once the
// array has an item, and split's limit before its separator (05/11/2026: converted up front, a filler
// whose toString failed made 'abcd'.padEnd(2, bad) fail, and [].includes(1, bad) too).
var methodHints = [invalid + 1]map[string]string{
	text: {
		"at": "n", "charAt": "n", "charCodeAt": "n", "codePointAt": "n", "concat": "t*", "endsWith": "tn",
		"includes": "tn", "indexOf": "tn", "lastIndexOf": "tn", "padEnd": "nk", "padStart": "nk",
		"repeat": "n", "replace": "tf", "replaceAll": "tf", "slice": "nn", "startsWith": "tn",
		"substr": "nn", "substring": "nn",
	},
	number: {"toExponential": "n", "toFixed": "n", "toPrecision": "n", "toString": "n"},
	array: {
		"at": "n", "copyWithin": "nnn", "fill": "knn", "flat": "n", "join": "t", "slice": "nn", "splice": "nn",
		"toSpliced": "nn", "with": "nk",
	},
	instant: {"format": "t"},
}

// objectMethodHints is methodHints for the methods of Object.prototype.
var objectMethodHints = map[string]string{"hasOwnProperty": "t"}

// valueMethod is one method of valueMethods: the function, and hints, how it converts its arguments for
// each kind of receiver (methodHints, objectMethodHints), nil for a method that converts none for any
// kind. The hints are found with the method, so a call of one that converts none (push, concat, map) looks
// at no argument: looking them up when an argument was an object made a.push(o) 65% slower (05/11/2026).
//
// text and array are what it reads of a text and of an array, which its energy is counted by (work.go).
type valueMethod struct {
	call        func(Value, ...Value) Value
	hints       *[invalid + 1]string
	text, array working
}

// hintsOf is the hints of the method name for each kind, nil when it converts no argument for any.
func hintsOf(name string) *[invalid + 1]string {
	var hints [invalid + 1]string
	converts := false
	for k := range hints {
		found, ok := methodHints[k][name]
		if !ok {
			found = objectMethodHints[name]
		}
		hints[k] = found
		converts = converts || found != ""
	}
	if !converts {
		return nil
	}
	return &hints
}

// ConvertsWithCode reports a value whose conversion to a primitive may run a program's code, or fail:
// an object (its own valueOf, toString), an array (its items' text), a function and a time, whose texts are the
// TYPE error (ToString, 18/11/2026; a time's number is its milliseconds). The first two kinds are next to each other.
func (v Value) ConvertsWithCode() bool {
	return v.kind-object <= array-object || v.kind == function || v.kind == instant
}

// callMethodWithin calls method on v with arguments, converted first as its hints say when one of them could run
// code to convert (callConverted), by a run whose budget is budget (nil for none): a method that converts no argument
// for any kind (push), or none for this kind (an array's indexOf, which a string's converts), looks at none, and an
// argument the method keeps as it is is passed over. InvokeWithin writes the same test where it calls a method.
func callMethodWithin(v Value, method valueMethod, arguments []Value, budget Budget) Value {
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

// callConverted is callMethod once an argument could run code to convert: f called with arguments
// converted as hints says, or the first failure of that.
func callConverted(v Value, hints string, f func(Value, ...Value) Value, arguments []Value, budget Budget) Value {
	converted, failure := convertArguments(hints, arguments, budget)
	if failure.kind == invalid {
		return failure
	}
	return f(v, converted...)
}

// ConvertArguments is arguments converted as hints says (methodHints' letters), for a function of Go that
// converts its arguments as a method does (the builtins: Math.max makes each a number, parseInt its first
// text): arguments itself when none could run code to convert, else a new slice, or the first failure.
func ConvertArguments(hints string, arguments []Value) ([]Value, Value) {
	for _, argument := range arguments {
		if argument.ConvertsWithCode() {
			return convertArguments(hints, arguments, nil)
		}
	}
	return arguments, Value{}
}

// convertArguments is arguments converted as hints says, or the first failure of a conversion: arguments
// itself when nothing is converted (an object an argument keeps: a.indexOf(o)), else a new slice, since the
// caller's may be a window on its stack. An array made text is made within budget (TextWithin).
func convertArguments(hints string, arguments []Value, budget Budget) ([]Value, Value) {
	if hints == "" {
		return arguments, Value{}
	}
	var converted []Value
	for i, argument := range arguments {
		hint := hintAt(hints, i)
		if !argument.ConvertsWithCode() || hint == 'k' || hint == 'f' && argument.kind == function {
			continue
		}
		// one conversion, the method's: a text is ToString alone, which runs toString before valueOf
		// (05/11/2026: ToNumber ran first, so valueOf ran where JavaScript never calls it, and toString twice)
		var made Value
		if hint == 't' || hint == 'f' {
			made = argument.TextWithin(budget)
		} else {
			made = argument.ToNumber()
		}
		if made.kind == invalid {
			return nil, made
		}
		if converted == nil {
			converted = append([]Value(nil), arguments...)
		}
		converted[i] = made
	}
	if converted == nil {
		return arguments, Value{}
	}
	return converted, Value{}
}

// hintAt is how hints converts argument i.
func hintAt(hints string, i int) byte {
	if i < len(hints) && hints[i] != '*' {
		return hints[i]
	}
	if star := strings.IndexByte(hints, '*'); star > 0 && i >= star-1 {
		return hints[star-1]
	}
	return 'k'
}

// ToPrimitive is ToPrimitive with the default hint, as + and == take it: a scalar is itself, an object
// through its own valueOf then toString, an array its text. A failure of the conversion is the result.
// The runtime converts with it before a comparison that would otherwise lose such a failure.
func (v Value) ToPrimitive() Value { return v.toPrimitive() }

// ToPrimitiveNumber is ToPrimitive with the number hint, as < takes it: a time is its milliseconds.
func (v Value) ToPrimitiveNumber() Value { return v.toPrimitiveNumber() }

// ToText is ToString for Go, the string hint, as Go's text, and the failure of the conversion when it
// failed (an object's own toString), which ToString().Text() would read as "".
func (v Value) ToText() (string, Value) {
	if v.kind == text {
		return v.data.(string), Value{}
	}
	converted := v.ToString()
	if converted.kind == invalid {
		return "", converted
	}
	return converted.Text(), Value{}
}
