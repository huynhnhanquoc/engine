package runtime

// safe(f) is how a program reads a failure (14/10/2026). It is a builtin of core/builtins now, not the
// runtime's: it calls f and gives { ok: true, value } or { ok: false, error: { code, message } }, since
// a failure is a value that comes back out of the call. What the runtime keeps here is the list of the
// failures safe never catches; what a failure of a run is to the program (a Diagnostic made a failure
// value whose code is a runtime error's cause and whose text leads with its place) is diagnostic.go's
// diagnosticResult, the one conversion.
//
// What safe catches is a runtime error of the program, or of something the program asked for (a query,
// a body read, a call): fail, a call of what is not a function, an operation on kinds it does not take,
// a failure of the host. What it never catches is a limit the host puts on the program, and the
// engine's own integrity: ENERGY_LIMIT, MEMORY_LIMIT, HOST_MEMORY_LIMIT, EXECUTION_CANCELLED, STACK_OVERFLOW, NATIVE_PANIC,
// PROGRAM_MISMATCH, NO_PROGRAM, VM_STOPPED, UNKNOWN_OPCODE, TRUNCATED_BYTECODE. The VM records such a
// stop on vm.stop the moment it happens (diagnosticValue) and ends the run with it after the CALL or
// INVOKE that met it, so a safe that wrapped its failure into { ok: false } does not keep the run going
// and the program never reads it. Its failure value has the code EXECUTION_CANCELLED
// (value.CancelledCode) for a cancellation and FAIL for every other stop.

// uncatchableCode reports whether code is the code of a failure nothing catches, and which: every
// diagnostic code but RUNTIME_ERROR. Of the codes a failure value carries (value.Codes) only
// EXECUTION_CANCELLED is one, the failure of a host call made because the request was over.
func uncatchableCode(code string) (DiagnosticCode, bool) {
	switch DiagnosticCode(code) {
	case DiagnosticEnergyLimit, DiagnosticMemoryLimit, DiagnosticHostMemory, DiagnosticCancelled, DiagnosticStackOverflow, DiagnosticNativePanic,
		DiagnosticProgramMismatch, DiagnosticNoProgram, DiagnosticVMStopped, DiagnosticUnknownOpcode,
		DiagnosticTruncatedBytecode:
		return DiagnosticCode(code), true
	}
	return "", false
}

// cancelled reports whether the request this VM runs for is over: a host call that failed then failed
// because of it, whatever it said.
func (vm *VM) cancelled() bool {
	return vm.Context != nil && vm.Context.Err() != nil
}

func cancelledMessage(message string) string {
	return "Execution Cancelled: " + message
}
