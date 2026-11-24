package runtime

import (
	"github.com/kitwork/engine2/core/value"
)

// A host object is built once and used by every request: a module imported at the top of a file
// (import * as routine from "kitwork/routine") is made by the VM that loads the file, before any request.
// A function of the host that needs the VM calling it (the request's context it runs under, where the
// call is written, whether a file is still loading) is a value.Function whose closure captures the VM
// that made it; nothing in the call path checks for it, so CALL and INVOKE cost nothing for the seam.
// What such a function reads of its VM is here: where the call is written (CallSite), and how a failure
// of something it ran on the program's behalf is told as its own (WrapFailure).

// CallSite is where the call the VM runs now is written: the instruction of the host call in progress,
// in its current frame.
func (vm *VM) CallSite() SourceLocation {
	if vm == nil {
		return SourceLocation{}
	}
	return vm.currentLocation(vm.currentInstruction())
}

// WrapFailure is the failure of something a host call ran on the program's behalf, cause, told as the
// call's own: a runtime error at the call (the place of the host call in progress on vm; none when vm is
// nil), with message and cause's code; the Diagnostic keeps cause as its Cause for the log. A host that
// ran several things reports the one that failed this way (routine.wait). A cause nothing catches (a
// limit of the host, the request over) is given back as it is: it ends the run, wrapped or not.
func WrapFailure(vm *VM, message string, cause value.Value) value.Value {
	if !cause.IsInvalid() {
		return cause // not a failure: nothing to wrap
	}
	if _, never := uncatchableCode(cause.Code()); never {
		return cause
	}
	if vm != nil && vm.stop != nil {
		return cause // vm's run is ending with its stop
	}
	// the cause's place stays in the log's text of it: the inner Diagnostic has no place of its own
	inner := &Diagnostic{Code: DiagnosticRuntimeError, CauseCode: cause.Code(), Message: cause.Headline(), IP: -1}
	diagnostic := &Diagnostic{Code: DiagnosticRuntimeError, Message: message, IP: -1}
	if vm != nil {
		diagnostic = vm.buildDiagnostic(DiagnosticRuntimeError, message, vm.currentInstruction())
	}
	diagnostic.CauseCode, diagnostic.Fields, diagnostic.Progress = inner.CauseCode, cause.Fields(), cause.Progress()
	diagnostic.Cause = inner
	return diagnosticResult(diagnostic)
}
