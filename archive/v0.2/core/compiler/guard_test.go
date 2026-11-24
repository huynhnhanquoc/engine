package compiler

import (
	"errors"
	"strings"
	"testing"
)

// A panic of the compiler's own code under contain is the compile's error, an InternalError that names where it was
// raised and says it is the compiler's, not the source's (13/11/2026).
func TestContainMakesAPanicTheCompilesError(t *testing.T) {
	err := func() (err error) {
		defer contain(&err)
		var parser *Parser
		parser.nextToken() // a nil dereference inside the parser
		return nil
	}()
	var internal *InternalError
	if !errors.As(err, &internal) {
		t.Fatalf("err = %v, want an InternalError", err)
	}
	if !strings.HasPrefix(internal.Where, "parser.go:") {
		t.Errorf("Where = %q, want the parser's line the panic was raised at", internal.Where)
	}
	if !strings.Contains(err.Error(), "compiler của Kitwork gặp lỗi của chính nó, không phải của mã này") || len(internal.Stack) == 0 {
		t.Errorf("err = %v (stack %d bytes), want the compiler's fault said as its own, with its stack", err, len(internal.Stack))
	}
}

// ParseProgram, which the host and the modules call on their own, recovers too: the fault is the last of Errors, after
// the source's first error when it had one, and parseProgramSource gives it as the InternalError.
func TestParseProgramRecoversItsOwnFault(t *testing.T) {
	parser := &Parser{curToken: Token{Kind: Ident}, errors: []string{"<source>:1:1: lỗi của nguồn"}} // no lexer
	parser.ParseProgram()
	if parser.internal == nil {
		t.Fatal("ParseProgram let the parser's panic out or kept no InternalError")
	}
	errs := parser.Errors()
	if last := errs[len(errs)-1]; !strings.HasPrefix(last, "<source>:1:1: lỗi của nguồn; sau đó compiler của Kitwork gặp lỗi của chính nó") {
		t.Errorf("last error = %q, want the source's first error and then the fault", last)
	}
}

// FuzzExpressionStrings looks for a fault in expressionStrings, which ExpressionStrings turns into false.
func FuzzExpressionStrings(f *testing.F) {
	for _, seed := range []string{"dark ? 'sun' : 'moon'", "a ? 'x' : (b ? 'y' : 'z')", "'a'", "a", "(", "--------x", "`${a}`"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		expressionStrings(source)
	})
}
