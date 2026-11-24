package value

// kind is what a Value holds. It never leaves the package: a program asks Typeof, Go asks IsNumber.
// The first five kinds after null keep their payload in Value.number, the rest in Value.data.
type kind uint8

const (
	_ kind = iota // Value{} is undefined
	null
	number
	boolean
	instant
	duration // up to here the payload is in number
	text
	bytes
	object
	array
	host
	function
	invalid // a failure is a value
)

// What typeof answers: JavaScript's seven words, a contract the program switches on.
const (
	typeofUndefined = "undefined"
	typeofNull      = "null" // typeof null is "object"; the word itself names the value
	typeofNumber    = "number"
	typeofBoolean   = "boolean"
	typeofString    = "string"
	typeofObject    = "object"
	typeofFunction  = "function"
)

// What name answers for the kinds typeof folds into "object": words for a person reading a failure.
const (
	nameTime     = "time"
	nameDuration = "duration"
	nameBytes    = "bytes"
	nameArray    = "array"
	nameHost     = "value of the engine" // a struct of a module whose type was never built (hostName)
	nameFailure  = "failure"
)

// isObject reports a kind JavaScript calls an object, which ToPrimitive converts (toPrimitive): a time,
// bytes, an object, an array, a struct, a function. A duration is a primitive of Kitwork's.
func (k kind) isObject() bool { return k == instant || k >= bytes && k <= function }

// typeof is what `typeof` answers: twelve kinds fold into JavaScript's seven words.
func (k kind) typeof() string {
	switch k {
	case 0:
		return typeofUndefined
	case number:
		return typeofNumber
	case boolean:
		return typeofBoolean
	case text:
		return typeofString
	case function:
		return typeofFunction
	}
	return typeofObject
}

// name is the kind's word in a failure text ("number has no method x"): typeof's word, except that
// the kinds JavaScript calls object are told apart, since the text is for a person.
func (k kind) name() string {
	switch k {
	case 0:
		return typeofUndefined
	case null:
		return typeofNull
	case number:
		return typeofNumber
	case boolean:
		return typeofBoolean
	case instant:
		return nameTime
	case duration:
		return nameDuration
	case text:
		return typeofString
	case bytes:
		return nameBytes
	case object:
		return typeofObject
	case array:
		return nameArray
	case host:
		return nameHost
	case function:
		return typeofFunction
	case invalid:
		return nameFailure
	}
	return "unknown"
}
