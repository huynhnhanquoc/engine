package compiler_test

import (
	"strings"
	"testing"
)

// "kitwork/<name>" is a library of Kitwork, a thư viện, and an app's file is a file (17/11/2026, the owner): "module" is
// the author's word for an app's root (docs/KITWORK_MODULE_WORK.md, mục 2), so a compile error a file reads never
// calls either a module. Each error an import of the wrong kind gives says thư viện or file, and none says module.
func TestImportErrorsSayLibraryNotModule(t *testing.T) {
	for source, want := range map[string]string{
		`import * as k from "kitwork";`:       `import "kitwork": một thư viện của Kitwork là một chữ sau "kitwork/": import from "kitwork/<tên>"`,
		`import { a } from "kitwork/a/b";`:    `import "kitwork/a/b": một thư viện của Kitwork là một chữ sau "kitwork/"`,
		`import { a } from "lodash";`:         `import "lodash": chỉ import được thư viện của Kitwork ("kitwork/<tên>"), file từ gốc app`,
		`import * as shared from "./x.work";`: `import * chỉ dùng với thư viện của Kitwork ("kitwork/<tên>"); file của site thì gọi tên từng thứ cần`,
		`const kitwork = 5;`:                  `kitwork là tên của chính Kitwork (mỗi thư viện là kitwork.<tên>)`,
	} {
		_, err := compileFile(t, source)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", source, err, want)
			continue
		}
		if strings.Contains(err.Error(), "module") {
			t.Errorf("%s: the error calls something a module: %v", source, err)
		}
	}
}
