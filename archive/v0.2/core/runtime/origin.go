package runtime

import "github.com/kitwork/engine2/core/value"

// closureOrigin is what a closure keeps of where it was made, in value.Lambda.Program: the program whose
// code it runs, and the VM that made it. ProgramFromRef reads the program. The VM is how a host call
// knows a callback is one of its own VM's closures (VM.made), the common case, which then needs no entry
// in the shared registry of callers (nesting.go, passLambdas) while it is the last lambda passed: the
// registry's lock and map were most of what a call of an array method with a callback cost beside its
// callbacks (04/11/2026).
//
// A VM makes one origin for the program it runs, the first time that program makes a closure, and every
// closure it makes while it runs that program shares it, so a closure still costs its Lambda alone.
type closureOrigin struct {
	program *Program
	machine *VM
}

// Keep is value.Holder's: what Go code holds while it calls a closure of this origin is held by the VM
// that made it, or the VM that runs it for that one (VM.Keep).
func (o *closureOrigin) Keep(lambda *value.Lambda, kept value.Kept) (value.Holder, int) {
	if o.machine == nil {
		return nil, 0
	}
	return o.machine.Keep(lambda, kept)
}

// LetGo is value.Holder's; an origin hands out its VM as the Holder, so it lets go of nothing itself.
func (o *closureOrigin) LetGo(int) {}

// currentOrigin is the origin of the closures vm makes now, made anew when the program it runs has
// changed since its last closure.
func (vm *VM) currentOrigin() *closureOrigin {
	if origin := vm.origin; origin != nil && origin.program == vm.program {
		return origin
	}
	vm.origin = &closureOrigin{program: vm.program, machine: vm}
	return vm.origin
}

// made reports a closure vm made, whatever program it ran then.
func (vm *VM) made(lambda *value.Lambda) bool {
	origin, ok := lambda.Program.(*closureOrigin)
	return ok && origin.machine == vm
}

// maker is the VM that made lambda; nil for a lambda no VM made.
func maker(lambda *value.Lambda) *VM {
	if origin, ok := lambda.Program.(*closureOrigin); ok {
		return origin.machine
	}
	return nil
}
