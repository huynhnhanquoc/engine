package value

// A program's function is a Lambda: where its code starts in its program, what it is called, what
// it captured. The runtime makes one when the program reaches the function's definition, and runs
// it through Run when the program calls it; value only carries it, so that a Function value is one
// kind whether Go or the program wrote the body.
type Lambda struct {
	Address      int    // where the body starts in Program's bytecode
	Name         string // for a stack trace; "" for an anonymous function
	SourceFile   string
	SourceLine   int32
	SourceColumn int32
	Params       []string
	// Rest says the last of Params gathers the arguments past the others into an array, (a, ...rest) => …
	// (15/11/2026, round 5 of the 13/11 review); a function's length leaves it out, as JavaScript's does.
	Rest bool
	// Defaulted counts the parameters from the first with a default up to the rest, which the function's length
	// leaves out too (18/11/2026: (a, b = 1) => 0 had the length 2, where JavaScript's is 1).
	Defaulted int
	// Locals names the slots of the function's frame by index (bytecode 7): its parameters first, then
	// its own variables, the objects its loops make for each iteration and the compiler's temporaries.
	// The frame holds len(Locals) slots; the names are for a stack trace and a tool, never looked up.
	Locals []string
	// Scope is the map of variables by name of the frame the closure was made in, which a function
	// inside reads by name: the program's top level, or a function's names nothing declares (var x = …).
	// nil when nothing in the closure reads a variable by name.
	Scope map[string]Value
	// Slots are the slots of the frame the closure was made in, held by reference: the closure and the
	// functions inside it read and write that frame's variables there (LOAD_OUTER). nil when none does.
	Slots   []Value
	Program any     // the immutable program that owns Address; the runtime's type
	Parent  *Lambda // the enclosing closure, so nested functions reach the variables of every function around

	// What the compiler tells the runtime of the function, on the template and on every closure of it:
	// CapturesSlots, a closure keeps the slots of the frame it is made in; CapturesNames, it keeps that
	// frame's map of variables by name; WritesNames, the function writes a name nothing declares, so
	// its frame makes a map for such names as it starts (a closure made before the first write shares it).
	CapturesSlots bool
	CapturesNames bool
	WritesNames   bool

	// Run executes the lambda with the arguments on the VM that made it; the runtime sets it when it
	// makes the closure, so Call needs no global and no import of the runtime.
	Run func(lambda *Lambda, arguments []Value) Value

	// function is what the Function value of the lambda's first Closure holds. It lives inside the
	// lambda so that a closure costs the one allocation of its Lambda: the runtime makes a Lambda each
	// time a program reaches a function literal, a callback in a loop as often as the loop turns.
	function callable
}

// Length is the function's length, as JavaScript counts it: its parameters before the first with a default value
// and before the rest.
func (l *Lambda) Length() int {
	length := len(l.Params) - l.Defaulted
	if l.Rest {
		length--
	}
	return max(length, 0)
}

// Kept is what Go code of the engine holds for a run while program code it calls runs (14/11/2026): the
// array map walks (Value), the items it made so far, read as they grow (Items), the length of a text it
// is writing (Size). A run past its ceiling of bytes is counted again by what it can still reach
// (runtime, memory.go): its stack and frames, never a Go function's variables, so map's items were let
// go by that count, and a.map(() => "y".repeat(100000)) held 20 MB under a ceiling of 1 MiB, and went on.
type Kept struct {
	Value Value
	Items *[]Value
	Size  *int
}

// Holder is what a closure's Program is when the run that runs it counts what Go code holds for it
// (Kept): the runtime's origin of the closure, which knows the VM that made it. Keep gives the Holder
// that holds kept, the VM that will run lambda, and the mark its LetGo takes back.
type Holder interface {
	Keep(lambda *Lambda, kept Kept) (Holder, int)
	LetGo(mark int)
}

// Release is what a Keep gives back: Release forgets what it kept. A Release of nothing kept does
// nothing; it allocates nothing, so a method defers it for each call.
type Release struct {
	holder Holder
	mark   int
}

// Release forgets what the Keep kept.
func (r Release) Release() {
	if r.holder != nil {
		r.holder.LetGo(r.mark)
	}
}

// Keep tells the run of fn, a program's function, that Go code about to call it holds kept until the
// Release it gives (Kept). A Go function has no run to tell, and its Release does nothing.
func (v Value) Keep(kept Kept) Release {
	if l := v.Lambda(); l != nil {
		return keep(l, kept)
	}
	return Release{}
}

// keep is Keep of a program's lambda.
func keep(l *Lambda, kept Kept) Release {
	if by, ok := l.Program.(Holder); ok {
		holder, mark := by.Keep(l, kept)
		return Release{holder: holder, mark: mark}
	}
	return Release{}
}

// Closure is a Function value holding a program's lambda. The first Closure of a lambda uses the
// callable kept inside it, so it allocates nothing and writes into the lambda: make the closure before
// the lambda is shared. A later Closure of the same lambda is another function, as every Closure
// was, with a callable of its own; a Lambda copied from another starts as one never closed.
func Closure(lambda *Lambda) Value {
	if lambda == nil {
		return Fail(TypeCode, "a function without a body")
	}
	if lambda.function.lambda == lambda {
		return Value{kind: function, data: &callable{lambda: lambda}}
	}
	lambda.function = callable{lambda: lambda}
	return Value{kind: function, data: &lambda.function}
}

// Lambda is the program's lambda a Function holds; nil for a Go function and every other kind.
func (v Value) Lambda() *Lambda {
	if c, ok := v.data.(*callable); ok {
		return c.lambda
	}
	return nil
}
