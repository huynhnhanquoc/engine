package runtime

import (
	"github.com/kitwork/engine2/core/value"
)

func (vm *VM) currentInstruction() int {
	if vm == nil || vm.FrameIdx < 0 || vm.FrameIdx >= len(vm.Frames) {
		return -1
	}
	return vm.Frames[vm.FrameIdx].LastIP
}

// nativeValue contains a panic only around a host-owned call. Interpreter
// panics remain visible as engine bugs rather than being mislabeled as tenant
// failures. A host call may run a nested VM (nesting.go): the VM shows it what
// is left before the call and takes back what it spent after.
func (vm *VM) nativeValue(label string, call func() value.Value) (result value.Value) {
	vm.showHost()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = vm.nativePanic(label, recovered)
		}
		if vm.nested.returned.Load() != nil {
			result = vm.takeBack(result)
		}
	}()
	return call()
}

func (vm *VM) nativePanic(label string, recovered any) value.Value {
	return vm.diagnosticValue(
		DiagnosticNativePanic,
		nativePanicMessage(label, recovered),
		vm.currentInstruction(),
	)
}

// nativeProperty reads a property through the host. It is the hottest host
// boundary, so it does not show the VM's budget (a nested VM started by a read
// sees it as of the latest host call or checkpoint), but it still takes back
// what such a VM handed back.
func (vm *VM) nativeProperty(target value.Value, key string) (result value.Value) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = vm.nativePanic("property access", recovered)
		}
		if vm.nested.returned.Load() != nil {
			result = vm.takeBack(result)
		}
	}()
	return target.Get(key)
}

// nativeMethod keeps the hot INVOKE path free of a per-call closure and label
// concatenation while preserving the same native-panic diagnostic boundary. own is
// the function of the host an own key of target holds, which INVOKE has looked up
// already and which is called as value.Invoke would call it; for any other method
// it is undefined, and value.Invoke finds the method.
func (vm *VM) nativeMethod(target, own value.Value, method string, args []value.Value) (result value.Value) {
	vm.showHost()
	mark := vm.passLambdas(args)
	defer func() {
		if recovered := recover(); recovered != nil {
			result = vm.nativePanic("method "+method, recovered)
		}
		vm.withdrawLambdas(mark)
		if vm.nested.returned.Load() != nil {
			result = vm.takeBack(result)
		}
	}()
	if own.IsFunction() {
		return own.CallWithin(vm, args) // a builtin that makes much asks vm as it grows (value.Budget)
	}
	if target.Keeps() && !vm.frozen {
		// a struct that calls functions it keeps (a schema's transform) runs this call on a copy whose
		// functions run on vm, as an argument's do (value.Keeper, 06/11/2026)
		target = target.HandedTo(vm.leasedHand())
	}
	// a struct that notes what the run carries (a page's reads, value.Attender) is given the run's context
	target = target.AttendedBy(vm.Context)
	return target.InvokeWithin(vm, method, args)
}

func (vm *VM) nativeAction(label string, call func()) value.Value {
	return vm.nativeValue(label, func() value.Value {
		call()
		return value.Undefined()
	})
}
