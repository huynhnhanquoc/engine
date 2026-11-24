package runtime

// What kitwork/routine runs (docs/KITWORK_ROUTINE.md mục 3, 4; 07/11/2026): a function started apart from its caller,
// on a VM of its own, maybe on another goroutine, maybe after the request has answered. What it holds is copied as the
// call starts it (Snapshot), so the routine and its caller never share a value one of them writes: a write of the
// routine goes to its copy, the caller sees none, and a Go map is never written from two goroutines (which ends the
// process). What the site's boot made is shared as it is, since nothing writes it (freeze.go). The request (context,
// request, response) is no value a routine holds: in the copy it stands as a value that refuses every read.

import (
	"github.com/kitwork/engine2/core/value"
)

// snapshotLimit bounds the values a routine holds, copied as it starts and again for each attempt: a routine is
// handed what it needs, not a site's worth of data. resultLimit bounds what it gives back, copied for each who waits
// for it, which may be rows it read.
const (
	snapshotLimit = 1 << 16
	resultLimit   = 1 << 20
)

// Snapshot is fn copied to be run apart (RunSnapshot), as of now: every object, array, function and frame of
// variables it holds that a request made, its sharing and its cycles kept; what the boot made shared. A failure for a
// value that is no function, or one that holds too much to copy.
func Snapshot(fn value.Value) (value.Value, value.Value) {
	lambda := fn.Lambda()
	if !fn.IsFunction() || lambda == nil {
		return value.Value{}, value.Fail(value.TypeCode, "a routine runs a function of the program, not "+fn.Kind())
	}
	c := &copier{values: map[value.Value]value.Value{}, slots: map[*value.Value][]value.Value{}, lambdas: map[*value.Lambda]*value.Lambda{}, limit: snapshotLimit}
	copied := c.value(fn)
	if c.failure.IsInvalid() {
		return value.Value{}, c.failure
	}
	return copied, value.Value{}
}

// Result is what a routine gave back copied for one who waits for it: data the caller may write without the
// routine, or a second waiter, seeing it. A function made apart is no result: it would run on a VM gone back to
// the site.
func Result(result value.Value) value.Value {
	if result.IsInvalid() {
		return result
	}
	c := &copier{values: map[value.Value]value.Value{}, slots: map[*value.Value][]value.Value{}, lambdas: map[*value.Lambda]*value.Lambda{}, data: true, limit: resultLimit}
	copied := c.value(result)
	if c.failure.IsInvalid() {
		return c.failure
	}
	return copied
}

// RunSnapshot runs a snapshot on vm (prepared by the host: reset, its budget, its context): a copy of the snapshot
// made vm's own, so each attempt starts from what the call copied and writes only its own. stopped says the run
// ended with a stop nothing catches other than its context ending (its energy, its frames, a panic of the host),
// which another attempt would meet again.
func (vm *VM) RunSnapshot(snapshot value.Value) (result value.Value, stopped bool) {
	c := &copier{values: map[value.Value]value.Value{}, slots: map[*value.Value][]value.Value{}, lambdas: map[*value.Lambda]*value.Lambda{}, into: vm, limit: snapshotLimit}
	fn := c.value(snapshot)
	if c.failure.IsInvalid() {
		return c.failure, false
	}
	result = vm.ExecuteLambda(fn.Lambda(), nil)
	return result, vm.stop != nil && vm.stop.Code != DiagnosticCancelled
}

// copier copies values once each: values and lambdas by what they were, a frame's slots by their first slot (two
// closures of one frame share the frame's copy, as they shared the frame). into is the VM the copied functions are
// made for (RunSnapshot), nil for a snapshot or a result, which no VM runs as it is; data refuses functions.
type copier struct {
	values  map[value.Value]value.Value
	slots   map[*value.Value][]value.Value
	lambdas map[*value.Lambda]*value.Lambda
	into    *VM
	data    bool
	limit   int
	count   int
	failure value.Value
}

func (c *copier) value(item value.Value) value.Value {
	if c.failure.IsInvalid() {
		return value.Value{}
	}
	switch {
	case item.IsStruct():
		if _, bound := item.Host().(value.RequestBound); bound {
			return value.Struct(&requestStandIn{})
		}
		return item // a host object guards itself (Freeze); the request's alone is the caller's
	case item.IsFunction() && item.Lambda() == nil:
		if name, bound := item.RequestMethod(); bound {
			return value.NamedFunction(name, 0, func(...value.Value) value.Value { return (&requestStandIn{}).MissingMember(name) })
		}
		return item // a builtin, a method of a module: shared as it is
	case !(item.IsObject() || item.IsArray() || item.IsFunction()):
		return item // a text, a number: no program writes it
	case item.IsFrozen():
		return item // the boot's, only read; a function of the boot handed to the call runs by its lambda (RunSnapshot)
	}
	if copied, seen := c.values[item]; seen {
		return copied
	}
	if c.count++; c.count > c.limit {
		what := "what a routine holds is past " + itoa(c.limit) + " objects, arrays and functions to copy: hand it what it needs"
		if c.data {
			what = "what a routine gave back is past " + itoa(c.limit) + " objects, arrays and functions to copy: give back what the caller needs"
		}
		c.failure = value.Fail(value.TooLargeCode, what)
		return value.Value{}
	}
	switch {
	case item.IsArray():
		items := make([]value.Value, len(item.Items()))
		copied := value.Array(items)
		c.values[item] = copied
		for i, inner := range item.Items() {
			items[i] = c.value(inner)
		}
		return copied
	case item.IsFunction():
		if c.data {
			c.failure = value.Fail(value.TypeCode, "a routine gives back data, not a function: a function it made runs only where it was made")
			return value.Value{}
		}
		copied := value.Closure(c.lambda(item.Lambda()))
		c.values[item] = copied
		return copied
	}
	copied := value.Object()
	c.values[item] = copied
	for _, key := range item.Keys() {
		copied.Set(key, c.value(item.Get(key)))
	}
	return copied
}

// lambda is lambda copied with the frames of variables it holds; one the boot made is shared, its variables only
// read; nil for no lambda.
func (c *copier) lambda(lambda *value.Lambda) *value.Lambda {
	if lambda == nil {
		return nil
	}
	if madeFrozen(lambda) {
		return lambda // the boot's: its variables are shared and only read, wherever it runs
	}
	if copied, seen := c.lambdas[lambda]; seen {
		return copied
	}
	copied := *lambda
	c.lambdas[lambda] = &copied
	program, ok := ProgramFromRef(lambda.Program)
	if !ok {
		return lambda // no program's: not the copier's to make
	}
	copied.Program, copied.Run = &closureOrigin{program: program, machine: c.into}, nil
	if c.into != nil {
		copied.Run = c.into.runLambda
	}
	copied.Slots = c.frame(lambda.Slots)
	if lambda.Scope != nil {
		copied.Scope = make(map[string]value.Value, len(lambda.Scope))
		for name, item := range lambda.Scope {
			copied.Scope[name] = c.value(item)
		}
	}
	copied.Parent = c.lambda(lambda.Parent)
	return &copied
}

// frame is a frame's slots copied once, however many closures hold it.
func (c *copier) frame(slots []value.Value) []value.Value {
	if len(slots) == 0 {
		return slots
	}
	if copied, seen := c.slots[&slots[0]]; seen {
		return copied
	}
	copied := make([]value.Value, len(slots))
	c.slots[&slots[0]] = copied
	for i, item := range slots {
		copied[i] = c.value(item)
	}
	return copied
}

// requestStandIn is the request a routine's copy holds in its place: every read of it is a failure saying to read
// what is needed before the routine starts.
type requestStandIn struct{}

// Freeze: nothing to keep.
func (*requestStandIn) Freeze() {}

// MissingMember refuses every word of the request in a routine.
func (*requestStandIn) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "a routine runs apart from the request, maybe after it has answered: read "+name+
		" before routine.go(…) or routine.wait(…) and hand the routine its value, const email = context.query(\"email\"); routine.go(() => send(email))")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
