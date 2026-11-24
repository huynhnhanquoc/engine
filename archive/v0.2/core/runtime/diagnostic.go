package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// DiagnosticCode identifies a runtime failure independently of its wording.
type DiagnosticCode string

const (
	DiagnosticRuntimeError      DiagnosticCode = "RUNTIME_ERROR"
	DiagnosticUnknownOpcode     DiagnosticCode = "UNKNOWN_OPCODE"
	DiagnosticTruncatedBytecode DiagnosticCode = "TRUNCATED_BYTECODE"
	DiagnosticEnergyLimit       DiagnosticCode = "ENERGY_LIMIT"
	DiagnosticMemoryLimit       DiagnosticCode = "MEMORY_LIMIT"
	DiagnosticHostMemory        DiagnosticCode = "HOST_MEMORY_LIMIT"
	DiagnosticCancelled         DiagnosticCode = "EXECUTION_CANCELLED"
	DiagnosticStackOverflow     DiagnosticCode = "STACK_OVERFLOW"
	DiagnosticProgramMismatch   DiagnosticCode = "PROGRAM_MISMATCH"
	DiagnosticNoProgram         DiagnosticCode = "NO_PROGRAM"
	DiagnosticVMStopped         DiagnosticCode = "VM_STOPPED"
	DiagnosticNativePanic       DiagnosticCode = "NATIVE_PANIC"
)

// StackFrame is a source-aware snapshot of one active VM frame.
type StackFrame struct {
	Function string
	IP       int
	File     string
	Line     int32
	Column   int32
}

// Diagnostic is the structured form of a runtime failure. Message never holds where the failure is:
// File, Line and Column do (the innermost frame at the failing instruction), and Error() writes
// "file:line:column: message" when it prints (14/10/2026). What reaches the program is not the
// Diagnostic but the failure value it becomes (diagnosticResult): its code, and its headline as text;
// the Diagnostic itself stays in the runtime for the stack text and the logs (vm.stop).
type Diagnostic struct {
	Code       DiagnosticCode
	CauseCode  string
	Message    string
	IP         int
	File       string
	Line       int32
	Column     int32
	Function   string
	Stack      []StackFrame
	Suppressed []*Diagnostic
	// Cause is the failure this one wraps, a runtime error itself: what failed among the things a host
	// call ran (WrapFailure). Error() prints it for the log; the program reads only the code it gives
	// this one. nil for a failure that wraps none.
	Cause *Diagnostic
	// Fields are the fields of data from outside the cause names (value.Value.WithFields), kept on the failure
	// the program holds: what failed is told at the call, the fields are still the data's. Undefined for none.
	Fields value.Value
	// Progress is how much of its work the cause did before it stopped (value.Value.WithProgress), kept on the
	// failure the program holds as Fields is. Undefined for none.
	Progress value.Value
}

func (d *Diagnostic) Error() string {
	if d == nil {
		return ""
	}

	var out strings.Builder
	out.WriteString(d.headline())
	for _, frame := range d.Stack {
		out.WriteString("\n    at ")
		out.WriteString(frame.Function)
		switch {
		case frame.File != "" && frame.Line > 0 && frame.Column > 0 && frame.IP >= 0:
			fmt.Fprintf(
				&out,
				" (%s:%d:%d, byte %d)",
				frame.File,
				frame.Line,
				frame.Column,
				frame.IP,
			)
		case frame.File != "" && frame.Line > 0 && frame.IP >= 0:
			fmt.Fprintf(&out, " (%s:%d, byte %d)", frame.File, frame.Line, frame.IP)
		case frame.File != "" && frame.Line > 0:
			fmt.Fprintf(&out, " (%s:%d)", frame.File, frame.Line)
		case frame.Line > 0 && frame.IP >= 0:
			fmt.Fprintf(&out, " (line %d, byte %d)", frame.Line, frame.IP)
		case frame.Line > 0:
			fmt.Fprintf(&out, " (line %d)", frame.Line)
		case frame.IP >= 0:
			fmt.Fprintf(&out, " (byte %d)", frame.IP)
		}
	}
	for _, suppressed := range d.Suppressed {
		if suppressed == nil {
			continue
		}
		out.WriteString("\n    suppressed: ")
		text := suppressed.Error()
		text = strings.ReplaceAll(text, "\n", "\n        ")
		out.WriteString(text)
	}
	if d.Cause != nil {
		out.WriteString("\n    cause: ")
		out.WriteString(strings.ReplaceAll(d.Cause.Error(), "\n", "\n        "))
	}
	return out.String()
}

// headline is the first line of Error(): "file:line:column: message", or the message with its line
// for a source with no name. It is the text of the failure value the diagnostic becomes.
func (d *Diagnostic) headline() string {
	if place := diagnosticAt(d); d.File != "" && place != "" {
		return place + ": " + d.Message
	}
	if d.Line > 0 && !strings.Contains(d.Message, "(at line") {
		return d.Message + " (at line " + strconv.Itoa(int(d.Line)) + ")"
	}
	return d.Message
}

type runtimeFault struct {
	code    DiagnosticCode
	message string
}

func (f *runtimeFault) Error() string {
	if f == nil {
		return ""
	}
	return f.message
}

// diagnosticValue is the failure of the instruction at ip. A diagnostic that ends the run whatever
// catches it (every code but RUNTIME_ERROR) is recorded on vm.stop as it is made (stopWith).
func (vm *VM) diagnosticValue(code DiagnosticCode, message string, ip int) value.Value {
	diagnostic := vm.buildDiagnostic(code, message, ip)
	vm.stopWith(diagnostic)
	return diagnosticResult(diagnostic)
}

// stopWith records diagnostic as the stop of vm's run when it is one (every code but RUNTIME_ERROR). The
// first stop is the run's; a later one (a defer that fails again while the run unwinds) goes in its
// Suppressed, so the log shows both, as merging two failures once did. Every stop recorded makes
// vm.stop a new pointer, a copy of the first with the later one added, so an invocation that began
// under the first (its defers) sees that another stopped it.
func (vm *VM) stopWith(diagnostic *Diagnostic) {
	if vm == nil || diagnostic == nil || diagnostic.Code == DiagnosticRuntimeError || diagnostic == vm.stop {
		return
	}
	if vm.stop == nil {
		vm.stop = diagnostic
		return
	}
	merged := *vm.stop
	merged.Suppressed = append(append([]*Diagnostic(nil), vm.stop.Suppressed...), diagnostic)
	vm.stop = &merged
}

// diagnosticResult is the failure of a run as the program holds it: value.FailAt with the diagnostic's
// code (FailureCode), its message, and where it happened, "router.work:12:5" (diagnosticAt), apart: safe() hands
// the program error.message and error.at (19/11/2026; the text was the headline, "router.work:12:5: message",
// so error.message held the place, decided 14/10/2026 to be at's alone). The stack is not in it: it is in the
// Diagnostic, which a stop keeps on vm.stop for the log (14/10/2026: a failure safe(() => …) catches never pays
// for the stack's text).
func diagnosticResult(diagnostic *Diagnostic) value.Value {
	return failureOf(diagnostic.FailureCode(), diagnostic.Message, diagnosticAt(diagnostic)).WithFields(diagnostic.Fields).WithProgress(diagnostic.Progress)
}

// Failure is the failure value of this diagnostic, for a host that holds it as a Go error
// (Tenant.Execute) and hands it on: the program reads of it what it reads of the run itself, its code
// its message, its place, never the stack. It is diagnosticResult, the one conversion.
func (d *Diagnostic) Failure() value.Value { return diagnosticResult(d) }

// FailureCode is the code of the failure value this diagnostic becomes (Failure): a runtime error's
// cause (FAIL for a cause outside the set), EXECUTION_CANCELLED for a cancellation, FAIL for every other
// stop, whose own code stays on the Diagnostic.
func (d *Diagnostic) FailureCode() string {
	switch d.Code {
	case DiagnosticRuntimeError:
		for _, code := range value.Codes() {
			if code == d.CauseCode {
				return code
			}
		}
	case DiagnosticCancelled:
		return value.CancelledCode
	case DiagnosticHostMemory:
		// the host is busy, not the program wrong: a site answers 503 (pool.go, HostBusy)
		return value.UnavailableCode
	}
	return value.FailCode
}

// diagnosticAt is "file:line:column" of where a failure happened, the innermost frame at the failing
// instruction ("line:column" for a source with no name); "" when the bytecode does not tell.
func diagnosticAt(diagnostic *Diagnostic) string {
	return locationAt(SourceLocation{File: diagnostic.File, Line: diagnostic.Line, Column: diagnostic.Column})
}

func locationAt(location SourceLocation) string {
	if location.Line <= 0 {
		return ""
	}
	place := strconv.Itoa(int(location.Line))
	if location.Column > 0 {
		place += ":" + strconv.Itoa(int(location.Column))
	}
	if location.File != "" {
		place = location.File + ":" + place
	}
	return place
}

// failureOf is value.FailAt with code, one of the set (value.Codes); FAIL for any other. Each code is
// written as its constant, since a failure's code is never a string made at a call site.
func failureOf(code, text, at string) value.Value {
	switch code {
	case value.TypeCode:
		return value.FailAt(value.TypeCode, text, at)
	case value.RangeCode:
		return value.FailAt(value.RangeCode, text, at)
	case value.InvalidArgumentCode:
		return value.FailAt(value.InvalidArgumentCode, text, at)
	case value.InvalidInputCode:
		return value.FailAt(value.InvalidInputCode, text, at)
	case value.UnavailableCode:
		return value.FailAt(value.UnavailableCode, text, at)
	case value.UnsupportedCode:
		return value.FailAt(value.UnsupportedCode, text, at)
	case value.DatabaseCode:
		return value.FailAt(value.DatabaseCode, text, at)
	case value.ConflictCode:
		return value.FailAt(value.ConflictCode, text, at)
	case value.PartialCode:
		return value.FailAt(value.PartialCode, text, at)
	case value.TimeoutCode:
		return value.FailAt(value.TimeoutCode, text, at)
	case value.NetworkCode:
		return value.FailAt(value.NetworkCode, text, at)
	case value.DeniedCode:
		return value.FailAt(value.DeniedCode, text, at)
	case value.NotFoundCode:
		return value.FailAt(value.NotFoundCode, text, at)
	case value.TooLargeCode:
		return value.FailAt(value.TooLargeCode, text, at)
	case value.RoutineCancelledCode:
		return value.FailAt(value.RoutineCancelledCode, text, at)
	case value.NotInitializedCode:
		return value.FailAt(value.NotInitializedCode, text, at)
	case value.CancelledCode:
		return value.FailAt(value.CancelledCode, text, at)
	}
	return value.FailAt(value.FailCode, text, at)
}

// mergeFailures is the failure of a frame whose defer (secondary) ran while it unwound with primary: the
// one of the two that is a failure, primary when both are. A failure value carries no Diagnostic to
// merge into, so the secondary failure is dropped here; when it was a stop, stopWith has it in
// vm.stop's Suppressed already.
func mergeFailures(primary, secondary value.Value) value.Value {
	if !secondary.IsInvalid() {
		return primary
	}
	if !primary.IsInvalid() {
		return secondary
	}
	return primary
}

func (vm *VM) buildDiagnostic(code DiagnosticCode, message string, ip int) *Diagnostic {
	diagnostic := &Diagnostic{
		Code:    code,
		Message: message,
		IP:      ip,
	}
	if vm == nil || vm.program == nil || vm.FrameIdx < 0 {
		return diagnostic
	}

	for index := vm.FrameIdx; index >= 0; index-- {
		frame := &vm.Frames[index]
		frameIP := frame.LastIP
		if index == vm.FrameIdx && ip >= 0 {
			frameIP = ip
		}
		if frameIP < 0 && frame.Fn == nil {
			continue
		}

		name := "<main>"
		location := vm.currentLocation(frameIP)
		if frame.Fn != nil {
			name = lambdaDisplayName(frame.Fn)
			if location.File == "" {
				location.File = frame.Fn.SourceFile
			}
			if location.Line == 0 {
				location.Line = frame.Fn.SourceLine
			}
			if location.Column == 0 {
				location.Column = frame.Fn.SourceColumn
			}
		}
		diagnostic.Stack = append(diagnostic.Stack, StackFrame{
			Function: name,
			IP:       frameIP,
			File:     location.File,
			Line:     location.Line,
			Column:   location.Column,
		})
	}

	if len(diagnostic.Stack) > 0 {
		top := diagnostic.Stack[0]
		diagnostic.IP = top.IP
		diagnostic.File = top.File
		diagnostic.Line = top.Line
		diagnostic.Column = top.Column
		diagnostic.Function = top.Function
	}
	return diagnostic
}

func lambdaDisplayName(lambda *value.Lambda) string {
	if lambda == nil {
		return "<anonymous>"
	}
	if lambda.Name == "" {
		return "<anonymous>"
	}
	return lambda.Name
}
