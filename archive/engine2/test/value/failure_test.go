package value_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// Every Fail in the engine draws its code from the closed set: this test reads every .go file under
// engine2 and checks the first argument of each Fail call is one of value.Codes(), written as its
// constant. A new code is a line in failure.go, never a string at a call site.
func TestEveryFailUsesAListedCode(t *testing.T) {
	listed := map[string]bool{}
	for _, code := range value.Codes() {
		listed[code] = true
	}
	constants := map[string]bool{
		"FailCode": true, "TypeCode": true, "RangeCode": true, "InvalidArgumentCode": true,
		"InvalidInputCode": true, "UnavailableCode": true, "UnsupportedCode": true, "DatabaseCode": true,
		"ConflictCode": true, "PartialCode": true, "TimeoutCode": true, "NetworkCode": true,
		"DeniedCode": true, "NotFoundCode": true, "TooLargeCode": true, "RoutineCancelledCode": true,
		"NotInitializedCode": true, "CancelledCode": true,
	}
	if len(constants) != len(listed) {
		t.Fatalf("failure.go lists %d codes, this test knows %d", len(listed), len(constants))
	}
	set := token.NewFileSet()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || !isFail(call.Fun) {
				return true
			}
			switch code := call.Args[0].(type) {
			case *ast.Ident:
				if !constants[code.Name] {
					t.Errorf("%s: Fail(%s, …) is not a listed code", set.Position(call.Pos()), code.Name)
				}
			case *ast.SelectorExpr:
				if !constants[code.Sel.Name] {
					t.Errorf("%s: Fail(%s, …) is not a listed code", set.Position(call.Pos()), code.Sel.Name)
				}
			default:
				t.Errorf("%s: Fail's code must be a constant of failure.go, not an expression", set.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isFail(f ast.Expr) bool {
	switch f := f.(type) {
	case *ast.Ident:
		return f.Name == "Fail"
	case *ast.SelectorExpr:
		return f.Sel.Name == "Fail"
	}
	return false
}
