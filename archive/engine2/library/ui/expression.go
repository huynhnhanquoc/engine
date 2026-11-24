package ui

import (
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
)

// expression is one expression a .ui file writes, compiled once with .ui's rules (core/compiler
// CompileExpression): its program, and the names it reads that the page must be given.
type expression struct {
	source  string
	program *runtime.Program
	reads   []string
	// pure says it calls no builtin whose value is not the same each time it runs (Date, Math.random), nor one that
	// does something (console): what it gives is what the names it reads are (frozen.go)
	pure bool
}

// compile is the expression of source, compiled the first time a site's page meets it and kept by the site
// after: the same {{ }} on a thousand rows compiles once.
func (s *Site) compile(source string) (*expression, error) {
	if known, ok := s.expressions.Load(source); ok {
		return known.(*expression), nil
	}
	bytecode, reads, builtins, err := compiler.CompileExpressionBuiltins(source)
	if err != nil {
		return nil, err
	}
	compiled := &expression{source: source, program: bytecode.Program, reads: reads, pure: pureBuiltins(builtins)}
	known, _ := s.expressions.LoadOrStore(source, compiled)
	return known.(*expression), nil
}
