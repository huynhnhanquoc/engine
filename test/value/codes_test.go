package value_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tập mã đóng (core/value/failure.go): mọi value.Fail và value.Errorf trong code của engine3 (không kể test) mang một
// trong các hằng của tập, viết bằng tên, không bằng chuỗi. Thêm một mã là thêm một hằng ở failure.go, cùng lý do.
func TestEveryCodeIsInTheSet(t *testing.T) {
	problems, seen, err := outsideTheSet(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
	for _, folder := range []string{"core/value", "core/javascript"} {
		if !seen[folder] {
			t.Errorf("no call of Fail or Errorf found in %s: the walk reads the wrong folder (%v)", folder, seen)
		}
	}
}

// Phép kiểm trên chính nó: một thư mục có một mã viết bằng chuỗi và một mã đúng, một file test (không đọc), và một
// module khác nằm trong cây (không đọc).
func TestOutsideTheSetFindsALiteral(t *testing.T) {
	root := t.TempDir()
	write := func(path, source string) {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("core/javascript/a.go", `package javascript
func a() { _ = value.Fail("OUT_OF_RANGE", "x"); _ = value.Fail(value.TypeCode, "y") }`)
	write("core/value/b.go", `package value
func b() { _ = Errorf(RangeCode, "z"); _ = Fail(code, "w") }`)
	write("core/javascript/a_test.go", `package javascript
func c() { _ = value.Fail("ANYTHING", "a test may") }`)
	write("archive/v0.1/go.mod", "module old\n")
	write("archive/v0.1/library/a.go", `package library
func d() { _ = value.Fail("ANOTHER_MODULE", "its codes are its own") }`)
	problems, seen, err := outsideTheSet(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], `got "OUT_OF_RANGE"`) || !seen["core/value"] || !seen["core/javascript"] {
		t.Errorf("problems %q, seen %v", problems, seen)
	}
}

// outsideTheSet đọc mọi file .go dưới root (trừ thư mục test, thư mục ẩn, module khác, file _test.go) và trả một dòng cho mỗi lời
// gọi Fail hay Errorf mà mã không phải một hằng của tập; seen là các thư mục có lời gọi, đường dẫn tính từ root.
func outsideTheSet(root string) (problems []string, seen map[string]bool, err error) {
	set := map[string]bool{"TypeCode": true, "RangeCode": true, "InvalidInputCode": true, "EnergyCode": true, "MemoryCode": true, "DatabaseCode": true}
	seen = map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (name == "test" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			// một thư mục có go.mod riêng là một module khác (archive/v0.1, archive/v0.2): Go không đọc nó, phép kiểm cũng không
			if _, err := os.Stat(filepath.Join(path, "go.mod")); path != root && err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		folder, _ := filepath.Rel(root, filepath.Dir(path))
		inValue := file.Name.Name == "value"
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			var name string
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr: // value.Fail, value.Errorf
				if x, ok := fun.X.(*ast.Ident); ok && x.Name == "value" {
					name = fun.Sel.Name
				}
			case *ast.Ident: // Fail, Errorf trong package value
				if inValue {
					name = fun.Name
				}
			}
			if name != "Fail" && name != "Errorf" {
				return true
			}
			seen[filepath.ToSlash(folder)] = true
			var code string
			switch first := call.Args[0].(type) {
			case *ast.SelectorExpr:
				if x, ok := first.X.(*ast.Ident); ok && x.Name == "value" {
					code = first.Sel.Name
				}
			case *ast.Ident:
				if inValue {
					code = first.Name
				}
			}
			// Fail chuyển tiếp tham số code của nó cho Errorf: chỗ duy nhất mã không phải một hằng
			if !set[code] && !(inValue && code == "code") {
				problems = append(problems, path+": "+name+" takes a code of the set (TypeCode, RangeCode, InvalidInputCode, EnergyCode, MemoryCode, DatabaseCode), got "+written(call.Args[0]))
			}
			return true
		})
		return nil
	})
	return problems, seen, err
}

func written(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	}
	return "an expression"
}
