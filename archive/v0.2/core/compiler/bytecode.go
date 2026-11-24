package compiler

import (
	"fmt"
	"os"
	"path/filepath"
)

// parseProgram lexes + parses source into an AST, returning the first parser
// error (if any).
func parseProgram(content string) (*Program, error) {
	return parseProgramSource(content, "")
}

func parseProgramSource(content, source string) (*Program, error) {
	l := NewLexerSource(content, source)
	p := NewParser(l)
	prog := p.ParseProgram()
	if p.internal != nil {
		return nil, p.internal
	}
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("assemble error: %s", p.Errors()[0])
	}
	return prog, nil
}

// Bytecode compiles a tenant entry file to bytecode. import/export are FULLY
// native, no esbuild:
//   - `import * as x from "kitwork/x"` → `const x = kitwork.x`, a member of the global kitwork
//   - `import { a as b } from "kitwork/x"` → member bindings (`const b = kitwork.x.a`)
//   - relative modules (`./x`, `../x`) → resolved + IIFE-wrapped by nativeBundle
//   - `export …` / `export default …` → tracked for the bundler
//
// The hand-written parser is the single source of truth for the Kitwork language
// subset: anything it can't express is a compile error (by design), not silently
// normalized by a bundler.
//
// The file belongs to no app here, so an `app/…` import is a compile error: a file of an app is
// compiled with CompileFileInApp.
func CompileFile(paths ...string) (*Bytecode, error) {
	return CompileFileInApp("", paths...)
}

// CompileFileInApp compiles a file of an app. appRoot is the app's folder, app/ for one app,
// apps/<identity>/ for many (22/10/2026, KITWORK_UI.md mục 4): `import { x } from "app/core/x.work"`
// in any folder of the app resolves from it, never past it, and never into its .database/.
func CompileFileInApp(appRoot string, paths ...string) (bytecode *Bytecode, err error) {
	defer contain(&err)
	prog, files, sources, err := prepareFile(appRoot, paths...)
	if err != nil {
		return nil, err
	}
	return compilePreparedFile(prog, files, sources)
}

func prepareFile(appRoot string, paths ...string) (*Program, []string, map[string]string, error) {
	if paths == nil {
		return nil, nil, nil, fmt.Errorf("path is required")
	}

	entryPath := filepath.Join(paths...)
	entryAbs, err := filepath.Abs(entryPath)
	if err != nil {
		entryAbs = entryPath
	}

	data, err := os.ReadFile(entryAbs)
	if err != nil {
		return nil, nil, nil, err
	}
	content := string(data)
	sourceName := filepath.ToSlash(filepath.Base(entryAbs))

	prog, err := parseProgramSource(content, sourceName)
	if err != nil {
		return nil, nil, nil, err
	}
	files := []string{entryAbs}
	sources := map[string]string{sourceName: content}
	if hasRelativeImports(prog) {
		var moduleFiles []string
		var moduleSources map[string]string
		prog, moduleFiles, moduleSources, err = nativeBundleWithSources(entryAbs, prog, appRoot, sources)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, moduleFiles...)
		for name, source := range moduleSources {
			sources[name] = source
		}
	}
	prog.App = appRoot != ""
	return prog, files, sources, nil
}

func compilePreparedFile(
	prog *Program,
	files []string,
	sources map[string]string,
) (*Bytecode, error) {
	c := newCompilerWithSources(sources)
	c.declaredOnly = true
	if err := c.Compile(prog); err != nil {
		return nil, err
	}
	bc, err := c.ByteCodeResult()
	if err != nil {
		return nil, err
	}
	bc.Files = files
	return bc, nil
}
