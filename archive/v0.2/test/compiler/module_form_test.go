package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// A file is a JavaScript module in its form (18/11/2026, docs/language.md mục 6): return stands only in a function
// (at the top level it ended the file's run, and in an imported file left its exports undefined); import and export
// stand only at the top level (in a function a library's import ran at each call); a module never binds the names
// JavaScript's strict mode keeps (const this = 1 compiled); export { … } is names a comma holds apart (export { x y }
// exported both), each declared at the top level (an undeclared one was exported as undefined), and export … from is
// said to be missing (it was read as export { x } and then two statements).
//
// CONTROL (18/11/2026): with return allowed at the top level, the first case compiled; with import and export
// allowed anywhere, the import in a function and the export in an arrow compiled, the import in a block failed only
// as an import the bundler had not run; with the strict names bindable, const this, (yield) => and function
// package compiled; with exports not held to declarations, export { x, missing } compiled.
func TestAFileIsAModuleInItsForm(t *testing.T) {
	for source, want := range map[string]string{
		"const a = 1\nreturn": "router.work:2:1: return chỉ đứng trong một hàm",
		"function f() {\n  import { a } from \"kitwork/http\"\n}": "router.work:2:3: import chỉ đứng ở cấp cao nhất của file",
		"if (true) {\n  import \"./x.work\"\n}":                   "router.work:2:3: import chỉ đứng ở cấp cao nhất của file",
		"const f = () => {\n  export const b = 1\n}":              "router.work:2:3: export chỉ đứng ở cấp cao nhất của file",
		"const this = 1":                           "router.work:1:7: this là tên mà JavaScript giữ riêng trong một module",
		"const f = (yield) => yield":               "router.work:1:12: yield là tên mà JavaScript giữ riêng",
		"function package() {}":                    "router.work:1:10: package là tên mà JavaScript giữ riêng",
		"const x = 1\nconst y = 2\nexport { x y }": "router.work:3:12: export { … } liệt kê tên khai ở cấp cao nhất của file, ngăn bằng dấu phẩy",
		"export { x } from \"./o.work\"":           "router.work:1:14: Kitwork chưa có export … from",
		"const x = 1\nexport { x, missing }":       "router.work:2:13: export { missing }: missing không được khai ở cấp cao nhất của file",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		"const x = 1\nconst y = 2\nexport { x, y as z, }\nexport const w = 3\nexport function f() { return 1 }\nexport default 4",
		"const f = () => {\n  return\n}\nfunction g() { return 2 }",
	} {
		if _, err := compileFile(t, source); err != nil {
			t.Errorf("%q: %v", source, err)
		}
	}
}

// An import of a name the file does not export is an error at the import, as JavaScript's link error (18/11/2026):
// it read undefined. A test fixture of the queue imported rules this way, from the file of the queue's rules, which
// exports nothing, unseen since the file was renamed on 17/11 (queue_test.go).
//
// CONTROL (18/11/2026): with the bundler not holding the names, both imports compiled.
func TestAnImportOfANameTheFileDoesNotExportIsAnError(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("lib.work", "export const a = 1\n")
	for source, want := range map[string]string{
		"import { a, b } from \"./lib.work\"\nconst result = a": "router.work:1:1: import { b } from \"./lib.work\": file ấy không export b",
		"import d from \"./lib.work\"\nconst result = d":        "router.work:1:1: import … from \"./lib.work\": file ấy không có export default",
	} {
		if _, err := compiler.CompileFile(write("router.work", source)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	if _, err := compiler.CompileFile(write("router.work", "import { a } from \"./lib.work\"\nconst result = a")); err != nil {
		t.Errorf("an import of what the file exports: %v", err)
	}
}

// Each refusal says what to write, in the words of now (18/11/2026, docs/language.md mục 8.5): .safe() named a result
// with ok; delete and in pointed at o.delete, o.omit and o.has, which are gone, and a in o said only that a second
// statement stood on the line (delete is made since 18/11/2026); ...a said rests do not exist; async () => said "arrow function parameters must be
// identifiers"; the forms of import and export it does not read had no place; a missing file and an import cycle named
// an absolute path and no line.
//
// CONTROL (18/11/2026): with the old texts, each case failed on its want (the stale words, or no line).
func TestARefusalSaysWhatToWrite(t *testing.T) {
	for source, want := range map[string]string{
		"const o = {}\nconst result = o.safe()":          "router.work:2:18: .safe() đã bỏ (14/10/2026): viết safe(() => o), nhận { value, error }",
		"const o = { a: 1 }\nconst result = \"a\" in o":  "router.work:2:20: Kitwork chưa có toán tử in: hỏi một khoá thì Object.hasOwn(o, \"khoá\")",
		"const f = async () => 1":                        "router.work:1:23: Kitwork không có async, await: việc chạy song song và chờ đi qua kitwork/routine",
		"const f = (1 + 2) => 3":                         "router.work:1:17: tham số của hàm mũi tên là tên hay mẫu tách",
		"const a = [1]\nconst result = ...a":             "router.work:2:16: ... chỉ trải trong một mảng ([...a]), đối số của lời gọi (f(...args)) hay một object ({ ...o }); phần còn lại",
		"import x, { y } from \"./a.work\"":              "router.work:1:8: import: cần from ở đây",
		"import 5":                                       "router.work:1:1: import viết một trong các dạng",
		"export 1 + 2":                                   "router.work:1:1: export viết một trong các dạng",
		"import { a } from \"./nope.work\"\nconst b = a": "router.work:1:19: import \"./nope.work\": không có file ấy",
	} {
		if _, err := compileFile(t, source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"a.work":      "import { b } from \"./b.work\"\nexport const a = 1\n",
		"b.work":      "import { a } from \"./a.work\"\nexport const b = 2\n",
		"router.work": "import { a } from \"./a.work\"\nconst result = a\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := "b.work:1:19: import \"./a.work\" tạo một vòng import"
	if _, err := compiler.CompileFile(filepath.Join(dir, "router.work")); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a cycle: %v, want %q", err, want)
	}
}

// An import names its file whole, as JavaScript's modules do, and only a .work file is a module (18/11/2026,
// docs/language.md mục 6.2): x.kitwork.js, x.js, x/index.kitwork.js, x/index.js, x.work and x/index.work were tried
// in turn for an import of x, so ./lib read lib.kitwork.js beside lib.work; a file of any kind was compiled as .work;
// a folder's .kitwork.js files ran with its .work files; and a specifier that began with _ walked up the folders to
// the first _core/ above, past the app.
//
// CONTROL (18/11/2026): with the old candidates, ./lib and app/core/lib compiled; with any file a module, ./lib.js,
// ./notes.txt and ./lib.kitwork.js compiled; with a folder's .kitwork.js members run, import "./dir" failed on the
// broken one; with the _ walk-up, _core/shared.work compiled. (./sub was the folder's error before too.)
func TestAnImportNamesItsFileWhole(t *testing.T) {
	app := t.TempDir()
	dir := filepath.Join(app, "site")
	for name, source := range map[string]string{
		"_core/shared.work":     "export const a = \"core\"\n",
		"core/lib.work":         "export const a = \"app\"\n",
		"site/lib.work":         "export const a = \"work\"\n",
		"site/lib.kitwork.js":   "export const a = \"js\"\n",
		"site/lib.js":           "export const a = \"js\"\n",
		"site/notes.txt":        "export const a = \"txt\"\n",
		"site/sub/index.work":   "export const a = \"index\"\n",
		"site/dir/a.work":       "const one = 1\n",
		"site/dir/b.kitwork.js": "const = broken\n",
	} {
		path := filepath.Join(app, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(dir, "router.work")
	compile := func(source string) (*compiler.Bytecode, error) {
		if err := os.WriteFile(entry, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		return compiler.CompileFileInApp(app, entry)
	}
	for source, want := range map[string]string{
		"import { a } from \"./lib\"\nconst result = a":                "router.work:1:19: import \"./lib\": không có file ấy; import viết đủ tên file, cả đuôi: \"./lib.work\"",
		"import { a } from \"app/core/lib\"\nconst result = a":         "router.work:1:19: import \"app/core/lib\": app không có file core/lib; import viết đủ tên file, cả đuôi: \"app/core/lib.work\"",
		"import { a } from \"./sub\"\nconst result = a":                "router.work:1:19: import \"./sub\" là một thư mục",
		"import { a } from \"./lib.js\"\nconst result = a":             "router.work:1:19: import \"./lib.js\": chỉ file .work import được",
		"import { a } from \"./notes.txt\"\nconst result = a":          "router.work:1:19: import \"./notes.txt\": chỉ file .work import được",
		"import { a } from \"_core/shared.work\"\nconst result = a":    "router.work:1:19: import \"_core/shared.work\": chỉ import được thư viện của Kitwork",
		"import \"../_core/shared.work\"\nimport \"./lib.kitwork.js\"": "router.work:2:8: import \"./lib.kitwork.js\": chỉ file .work import được",
	} {
		if _, err := compile(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want the compile error %q", source, err, want)
		}
	}
	for _, source := range []string{
		"import \"./dir\"\nimport \"./dir/\"",
		"import { a } from \"./lib.work\"\nimport { a as b } from \"app/core/lib.work\"\nimport \"./sub\"\nconst result = a + b",
	} {
		if _, err := compile(source); err != nil {
			t.Errorf("%q: %v", source, err)
		}
	}
}
