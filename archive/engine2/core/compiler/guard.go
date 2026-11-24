package compiler

import (
	"fmt"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
)

// InternalError is a fault of the compiler's own code met while it read or compiled a source: a nil it did
// not expect, an index past an end, never something the source got wrong. It is that compile's error and no
// more: the host tells it for that file and goes on with every other app (13/11/2026; a panic of the parser on
// `let x = 1; const r = --------x;` in one tenant's file ended the whole host at boot).
//
// Every entry of the package holds one (contain): CompileFile, CompileFileInApp, CompileSource,
// CompileExpression, CompileExpressionBuiltins, CompileHandler, ExpressionStrings and Parser.ParseProgram.
type InternalError struct {
	// Fault is what the panic carried.
	Fault any
	// Where is the compiler's file:line the panic was raised at, "" when no frame of it was found.
	Where string
	// Earlier is the source's first error the parser had found before the fault, "" when none: the fault most
	// often comes of it, and it is what the author can mend.
	Earlier string
	// Stack is the goroutine's stack at the fault, for whoever mends the compiler; Error does not show it.
	Stack []byte
}

func (e *InternalError) Error() string {
	text := fmt.Sprintf("compiler của Kitwork gặp lỗi của chính nó, không phải của mã này: %v", e.Fault)
	if e.Where != "" {
		text += " (" + e.Where + ")"
	}
	if e.Earlier != "" {
		return e.Earlier + "; sau đó " + text
	}
	return text
}

// contain turns a panic of the compiler, while the function that defers it runs, into *err. recover is called
// here, the deferred function itself, as Go requires.
func contain(err *error) {
	if fault := recover(); fault != nil {
		*err = internalError(fault, "")
	}
}

// internalError is the InternalError of fault, recovered just now in a deferred function of the compiler.
func internalError(fault any, earlier string) *InternalError {
	return &InternalError{Fault: fault, Where: faultSite(), Earlier: earlier, Stack: debug.Stack()}
}

// faultSite is file:line of the first frame past the panic's own (runtime.gopanic and the runtime functions
// that call it: a nil dereference, an index out of range), as the deferred function sees the stack while it
// recovers: the compiler's, or a package it called.
func faultSite() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	panicked := false
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "runtime.gopanic":
			panicked = true
		case panicked && !strings.HasPrefix(frame.Function, "runtime."):
			return path.Base(frame.File) + ":" + fmt.Sprint(frame.Line)
		}
		if !more {
			return ""
		}
	}
}
