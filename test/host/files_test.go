package host_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// Các file application.work import (host/files.go): export const, đường dẫn tương đối, mỗi file một lần, vòng import.

// tree viết các file vào một thư mục tạm (tên có thể có thư mục con) và trả thư mục ấy.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func bootFolder(dir string) (host.Booted, error) {
	source, err := os.ReadFile(filepath.Join(dir, "application.work"))
	if err != nil {
		return host.Booted{}, err
	}
	return host.Program{File: "application.work", Source: string(source), Folder: dir}.Boot()
}

const programHead = `import { application } from "application";
`

func TestFiles(t *testing.T) {
	// một file nạp một lần dù hai file import nó (count.work chạy một lần, n chung); đường dẫn tính từ thư mục của file import
	dir := tree(t, map[string]string{
		"application.work": programHead + `import { a } from "./lib/a.work";
import { b } from "./lib/b.work";
return application({ timezone: a + "," + b }).check();`,
		"lib/a.work":            `import { count } from "./shared/count.work"; export const a = count();`,
		"lib/b.work":            `import { count } from "../lib/shared/count.work"; export const b = count();`,
		"lib/shared/count.work": `let n = 0; export const count = () => { n++; return n; };`,
	})
	booted, err := bootFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := value.As[application.Application](booted.Blueprint)
	if got := a.Fields().Timezone.String(); got != "1,2" {
		t.Errorf("count.work runs once, both files share its n: got %s, want 1,2", got)
	}
}

func TestFileErrors(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a cycle", map[string]string{
			"application.work": programHead + `import { a } from "./a.work"; return application().check();`,
			"a.work":           `import { b } from "./b.work"; export const a = 1;`,
			"b.work":           `import { a } from "./a.work"; export const b = 2;`,
		}, "b.work:1:1: import cycle: application.work → a.work → b.work → a.work"},
		{"outside the folder", map[string]string{
			"application.work": programHead + `import { a } from "../a.work"; return application().check();`,
		}, `application.work:2:1: import "../a.work": the file is outside the folder of application.work`},
		{"no .work", map[string]string{
			"application.work": programHead + `import { a } from "./a"; return application().check();`,
		}, `application.work:2:1: import "./a": write the whole name of the file, with .work: "./a.work"`},
		{"no such file", map[string]string{
			"application.work": programHead + `import { a } from "./a.work"; return application().check();`,
		}, `application.work:2:1: import "./a.work": no such file`},
		{"a file imports the application library", map[string]string{
			"application.work": programHead + `import { a } from "./a.work"; return application().check();`,
			"a.work":           `import { website } from "application"; export const a = website(443);`,
		}, `a.work:1:1: import "application": a file application.work imports sees other files and the libraries database, schema`},
		{"a file returns", map[string]string{
			"application.work": programHead + `import { a } from "./a.work"; return application().check();`,
			"a.work":           `export const a = 1; return a;`,
		}, "a.work:1:21: a file another file imports returns nothing: write export const x = …"},
		{"a name the file does not export", map[string]string{
			"application.work": programHead + `import { b } from "./a.work"; return application().check();`,
			"a.work":           `const b = 1; export const a = 2;`,
		}, `application.work:2:10: the module "./a.work" has no b: a`},
		{"an error while the file runs", map[string]string{
			"application.work": programHead + `import { a } from "./a.work"; return application().check();`,
			"a.work":           "const o = null;\nexport const a = o.x;",
		}, `a.work:2:20: cannot read properties of null (reading "x")`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := bootFolder(tree(t, c.files))
			if err == nil || err.Error() != c.want {
				t.Errorf("got  %v\nwant %s", err, c.want)
			}
		})
	}
}
