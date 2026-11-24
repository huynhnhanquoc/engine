package host

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
)

// Các file application.work import (23/11/2026): import { system } from "./host/system.work". Đường dẫn tính từ thư mục
// của file import nó, không ra ngoài thư mục của application.work, viết đủ tên file và đuôi .work (như module của
// JavaScript). Mỗi file chạy một lần mỗi lần boot, trước file import nó, trên cùng một lượt chạy: năng lượng boot là của
// cả chương trình. Kết quả của một file là các export const của nó; vòng import là lỗi. Chỉ application.work import
// "application": một file nó import thấy các file khác, và "schema", "database" (24/11/2026, lát 3: dữ liệu của host,
// host/system.work, khai bảng ở đó; connection() là của thư mục application.work).

// files nạp các file của một lần boot.
type files struct {
	folder  string // thư mục của application.work
	file    string // tên của application.work trong lời báo
	machine *runtime.Machine
	done    map[string]map[string]value.Value // đường dẫn (tính từ folder, dấu /) → các export
	loading []string                          // các file đang nạp, cho lời báo vòng import
	globals map[string]value.Value            // console của host (Program.Console), cho mọi file
	// libraries là các thư viện một file application.work import thấy: "schema", "database"
	libraries map[string]map[string]value.Value
}

// libraryNames là tên các thư viện, theo thứ tự chữ: "database, schema".
func libraryNames(libraries map[string]map[string]value.Value) string {
	names := make([]string, 0, len(libraries))
	for name := range libraries {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// relative là from chỉ một file: "./…", "../…".
func relative(from string) bool {
	return strings.HasPrefix(from, "./") || strings.HasPrefix(from, "../")
}

// imports là các module program thấy: libraries, và các file nó import, đã nạp. file là tên của program trong lời báo,
// dir là thư mục của nó tính từ folder.
func (l *files) imports(program *syntax.Program, file, dir string, libraries map[string]map[string]value.Value) (map[string]map[string]value.Value, error) {
	modules := map[string]map[string]value.Value{}
	for name, members := range libraries {
		modules[name] = members
	}
	for _, imp := range program.Imports {
		if !relative(imp.From) {
			continue // một thư viện: compiler nói khi không có
		}
		exports, err := l.load(file, dir, imp)
		if err != nil {
			return nil, err
		}
		modules[imp.From] = exports
	}
	return modules, nil
}

// load nạp file mà imp (của file importer, trong thư mục dir) chỉ tới, và trả các export của nó.
func (l *files) load(importer, dir string, imp *syntax.Import) (map[string]value.Value, error) {
	fail := func(format string, arguments ...any) error {
		return &syntax.Error{File: importer, Pos: imp.Pos, Message: fmt.Sprintf(format, arguments...)}
	}
	name := path.Clean(path.Join(dir, imp.From))
	if name == ".." || strings.HasPrefix(name, "../") {
		return nil, fail("import %q: the file is outside the folder of %s", imp.From, l.file)
	}
	if !strings.HasSuffix(name, ".work") {
		return nil, fail("import %q: write the whole name of the file, with .work: %q", imp.From, imp.From+".work")
	}
	if exports, found := l.done[name]; found {
		return exports, nil
	}
	if slices.Contains(l.loading, name) {
		return nil, fail("import cycle: %s → %s", strings.Join(append([]string{l.file}, l.loading...), " → "), name)
	}
	source, err := os.ReadFile(filepath.Join(l.folder, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fail("import %q: no such file", imp.From)
	}
	if err != nil {
		return nil, fail("import %q: %v", imp.From, err)
	}
	program, err := syntax.Parse(name, string(source))
	if err != nil {
		return nil, err
	}
	for _, inner := range program.Imports {
		if _, library := l.libraries[inner.From]; !relative(inner.From) && !library {
			return nil, &syntax.Error{File: name, Pos: inner.Pos, Message: fmt.Sprintf(
				"import %q: a file %s imports sees other files and the libraries %s", inner.From, l.file, libraryNames(l.libraries))}
		}
	}
	l.loading = append(l.loading, name)
	modules, err := l.imports(program, name, path.Dir(name), l.libraries)
	l.loading = l.loading[:len(l.loading)-1]
	if err != nil {
		return nil, err
	}
	main, err := compiler.Compile(program, compiler.Options{Modules: modules, Globals: l.globals, Module: true})
	if err != nil {
		return nil, err
	}
	result := l.machine.Run(main)
	if result.IsInvalid() {
		return nil, result.Error()
	}
	exports := map[string]value.Value{}
	for _, key := range result.Keys() {
		exports[key], _ = result.Get(key)
	}
	l.done[name] = exports
	return exports, nil
}
