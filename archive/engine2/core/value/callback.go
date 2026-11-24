package value

// callback is the function an array method calls back once per item (map, filter, find, forEach, some,
// every, reduce, reduceRight, flatMap, findLast, sort's comparator), set up once per call of the method:
// the function read out of its Value once, and one argument slice the method overwrites, every slot of
// it, for each item. Until 04/11/2026 each callback went through Call with a slice made for it, one
// allocation an item, and cost 48% more than the runtime's own path for the same methods.
//
// The slice is reused, so no callee may keep it: a program's lambda binds its arguments into its frame
// before its body runs (the runtime's Run, bindParameters), and a Go function reads its arguments and
// lets go of them, the contract Invoke states for a window on the caller's stack. The slice is full, so
// a function that appends to its arguments writes a copy.
type callback struct {
	lambda    *Lambda
	native    func(arguments ...Value) Value
	arguments []Value
}

// newCallback is fn, which callbackArgument found to be a function, to be called with count arguments.
func newCallback(fn Value, count int) callback {
	c := fn.data.(*callable)
	each := callback{arguments: make([]Value, count)}
	switch {
	case c.native != nil:
		// a Go function, or a stand-in (RunningOn), which runs its lambda where the runtime said
		each.native = c.native
	case c.lambda.Run != nil:
		each.lambda = c.lambda
	default:
		each.native = func(...Value) Value { return Fail(TypeCode, "a program function with no runtime to run it") }
	}
	return each
}

// call calls the function with the arguments as the method wrote them. The method has already given back
// a failure among them, as Call gives back the first failure of its arguments: an item is the one
// argument that can be one (the index is a number, the array an array, and reduce's accumulator ends the
// method as soon as it fails).
func (each *callback) call() Value {
	if each.lambda != nil {
		return each.lambda.Run(each.lambda, each.arguments)
	}
	return each.native(each.arguments...)
}

// keep tells the run of the function, when it is a program's, that the method holds kept while it calls
// it (Kept): the array it walks and what it made so far.
func (each *callback) keep(kept Kept) Release {
	if each.lambda != nil {
		return keep(each.lambda, kept)
	}
	return Release{}
}

// item writes the arguments of fn(item, index, array) for item i.
func (each *callback) item(item Value, i int, array Value) Value {
	each.arguments[0], each.arguments[1], each.arguments[2] = item, Number(float64(i)), array
	return each.call()
}

// accumulate writes the arguments of fn(accumulator, item, index, array), reduce's, for item i.
func (each *callback) accumulate(accumulator, item Value, i int, array Value) Value {
	each.arguments[0], each.arguments[1], each.arguments[2], each.arguments[3] = accumulator, item, Number(float64(i)), array
	return each.call()
}

// compare writes the arguments of a comparator, fn(a, b).
func (each *callback) compare(a, b Value) Value {
	each.arguments[0], each.arguments[1] = a, b
	return each.call()
}
