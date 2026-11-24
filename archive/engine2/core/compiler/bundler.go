package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// nativeBundle giải quyết các ImportStatement TƯƠNG ĐỐI trong entryProg bằng cách
// đọc + parse + bọc IIFE từng module, rồi trả về MỘT Program đã ghép (không còn
// ImportStatement). Mỗi module được bọc:
//
//	const __kw_mod_N = (() => { <mã module>; return { <exports> }; })();
//
// nhờ vậy biến cục bộ của module nằm trong frame riêng (f.Vars) — không đụng tên
// với entry hay module khác (đã xác nhận qua runtime/vm.go). Import của entry/module
// được thay bằng khai báo binding (const { x } = __kw_mod_N).
//
// Bất kỳ trường hợp nào không xử lý được (cycle, không tìm thấy file, …) đều trả lỗi
// biên dịch.
func nativeBundle(entryPath string, entryProg *Program) (*Program, []string, error) {
	program, files, _, err := nativeBundleWithSources(entryPath, entryProg, "", nil)
	return program, files, err
}

// nativeBundleWithSources is nativeBundle that also gives every module's source by its name. appRoot is
// the folder of the app the entry belongs to, where an `app/…` import resolves ("" when the file is no
// app's, as the host's manifest: an `app/…` import is then a compile error). sources holds the entry's
// own source by its name (nil for none), so an error of an import written in it says where it stands.
func nativeBundleWithSources(
	entryPath string,
	entryProg *Program,
	appRoot string,
	sources map[string]string,
) (*Program, []string, map[string]string, error) {
	abs, err := filepath.Abs(entryPath)
	if err != nil {
		abs = entryPath
	}
	if appRoot != "" {
		if absolute, err := filepath.Abs(appRoot); err == nil {
			appRoot = absolute
		}
	}
	known := make(map[string]string, len(sources))
	for name, text := range sources {
		known[name] = text
	}
	b := &bundler{
		modules:   map[string]string{},
		exported:  map[string]map[string]bool{},
		visiting:  map[string]bool{},
		sources:   known,
		debugRoot: filepath.Dir(abs),
		appRoot:   appRoot,
	}
	body, err := b.rewriteStatements(entryProg.Statements, filepath.Dir(abs))
	if err != nil {
		return nil, nil, nil, err
	}
	combined := &Program{}
	combined.Statements = append(combined.Statements, b.defs...) // IIFE const của module (thứ tự phụ thuộc)
	combined.Statements = append(combined.Statements, body...)   // thân entry (chạy ở top-level)

	// Every bundled module file, sorted for determinism — surfaced on Bytecode.Files so hot
	// reload can watch imports, not just the entry.
	files := make([]string, 0, len(b.modules))
	for absPath := range b.modules {
		files = append(files, absPath)
	}
	sort.Strings(files)
	return combined, files, b.sources, nil
}

type bundler struct {
	sources   map[string]string
	debugRoot string
	modules   map[string]string // abs path đã resolve -> tên biến module (__kw_mod_N)
	// exported are the names each module exports, "default" among them when it has one: an import of a name the
	// module does not export is an error, as JavaScript's (18/11/2026: it was undefined)
	exported map[string]map[string]bool
	visiting map[string]bool // phát hiện cycle
	defs     []Statement
	counter  int
	// appRoot is the app's folder an `app/…` import resolves from, "" for a file of no app;
	// appRootReal is it past links, found at the first app/ import.
	appRoot     string
	appRootReal string
}

// rewriteStatements thay mỗi ImportStatement tương đối bằng binding, và bảo đảm
// mỗi module được bundle đúng một lần. fromDir là thư mục của file chứa stmts.
func (b *bundler) rewriteStatements(stmts []Statement, fromDir string) ([]Statement, error) {
	out := make([]Statement, 0, len(stmts))
	for _, s := range stmts {
		imp, ok := s.(*ImportStatement)
		if !ok {
			out = append(out, s)
			continue
		}
		absPath, err := b.resolve(imp, fromDir)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(absPath)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() && filepath.Ext(absPath) != ".work" {
			// a file of another kind is no module (18/11/2026: any file was compiled as .work, a .txt, a .env)
			return nil, fmt.Errorf("%s: import %q: chỉ file .work import được (và một thư mục, bằng import không tên)", b.place(imp.SourceToken), imp.Source)
		}
		if fi.IsDir() && !imp.SideEffect {
			// said where the import stands, in the author's words, with the two forms that work (22/10/2026)
			return nil, fmt.Errorf("%s: import %q là một thư mục, không có gì để lấy theo tên: lấy từ một file trong đó (%q), hay viết import %q không tên để chạy cả thư mục",
				b.place(imp.SourceToken), imp.Source, strings.TrimSuffix(imp.Source, "/")+"/x.work", imp.Source)
		}
		if b.visiting[absPath] {
			// said at the import that closes the cycle (18/11/2026: the absolute path of the file, no line)
			return nil, fmt.Errorf("%s: import %q tạo một vòng import: file ấy, trực tiếp hay qua file khác, import file đang viết; đưa phần dùng chung ra một file thứ ba", b.place(imp.SourceToken), imp.Source)
		}
		modVar, err := b.ensureModule(absPath)
		if err != nil {
			return nil, err
		}
		if imp.SideEffect {
			continue // IIFE của module đã chạy (side-effect), không cần binding
		}
		if imp.Default != nil {
			if err := b.exports(absPath, imp, "default"); err != nil {
				return nil, err
			}
			out = append(out, memberBinding(imp.Default.Value, modVar, "default", imp.Token))
		}
		for _, spec := range imp.Names {
			if err := b.exports(absPath, imp, spec.Imported); err != nil {
				return nil, err
			}
			out = append(out, memberBinding(spec.Local, modVar, spec.Imported, imp.Token))
		}
	}
	return out, nil
}

// exports is the error of importing name from the module at absPath when it does not export it.
func (b *bundler) exports(absPath string, imp *ImportStatement, name string) error {
	names, ok := b.exported[absPath]
	if !ok || names[name] {
		return nil
	}
	if name == "default" {
		return fmt.Errorf("%s: import … from %q: file ấy không có export default", b.place(imp.Token), imp.Source)
	}
	return fmt.Errorf("%s: import { %s } from %q: file ấy không export %s", b.place(imp.Token), name, imp.Source, name)
}

// ensureModule parse + bọc IIFE một module đã resolve (absPath, đệ quy cho import của nó),
// trả về tên biến module. Dùng cache để dedupe.
func (b *bundler) ensureModule(absPath string) (string, error) {
	if v, ok := b.modules[absPath]; ok {
		return v, nil
	}
	if b.visiting[absPath] {
		return "", fmt.Errorf("native bundle: import cycle tại %s", absPath)
	}
	b.visiting[absPath] = true
	defer delete(b.visiting, absPath)

	fi, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}

	if fi.IsDir() {
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return "", err
		}
		var files []string
		for _, entry := range entries {
			// a .work file, as the error above says (27/10/2026: only .kitwork.js members were read, so
			// import "./dir/" of .work files ran nothing; 18/11/2026: a .kitwork.js was still run)
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".work") {
				files = append(files, filepath.Join(absPath, entry.Name()))
			}
		}
		sort.Strings(files)
		for _, f := range files {
			_, err := b.ensureModule(f)
			if err != nil {
				return "", err
			}
		}
		b.modules[absPath] = ""
		return "", nil
	}

	src, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	sourceName := b.sourceName(absPath)
	sourceText := string(src)
	b.sources[sourceName] = sourceText
	prog, err := parseProgramSource(sourceText, sourceName)
	if err != nil {
		return "", err
	}
	// Đệ quy: import tương đối của module này → binding + append IIFE con trước.
	body, err := b.rewriteStatements(prog.Statements, filepath.Dir(absPath))
	if err != nil {
		return "", err
	}
	origin := Token{
		Kind:     Return,
		Source:   sourceName,
		Position: int32(len(src)),
	}
	body = append(body, exportReturn(prog, origin))
	names := make(map[string]bool, len(prog.Exports)+1)
	for _, name := range prog.Exports {
		names[name] = true
	}
	if prog.HasDefault {
		names["default"] = true
	}
	b.exported[absPath] = names

	b.counter++
	name := fmt.Sprintf("__kw_mod_%d", b.counter)
	b.modules[absPath] = name
	b.defs = append(b.defs, moduleIIFE(name, body)) // append SAU các module con → đúng thứ tự
	return name, nil
}

// sourceName is how a module's file is named in the debug table, and so in an error's at: relative to
// the entry's folder, never the machine's absolute path. When no relative path exists (another drive on
// Windows) it is the file's name alone (14/10/2026; until then the absolute path).
func (b *bundler) sourceName(path string) string {
	relative, err := filepath.Rel(b.debugRoot, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(relative)
}

// resolve is the file an import names: an `app/…` path from the app's folder, any other from the
// folder of the file that imports (fromDir).
func (b *bundler) resolve(imp *ImportStatement, fromDir string) (string, error) {
	if !isAppRootSpecifier(imp.Source) {
		// an app's file imports only the app's files (KITWORK_UI.md mục 4, KITWORK_ENVIRONMENT.md mục 1):
		// an absolute path names a file of no app (05/11/2026; until then it compiled, as a relative path
		// out of the app and the _core/ walk up did, so a site imported another app's code and quoted the
		// host's .env in its boot error)
		if b.appRoot != "" && (filepath.IsAbs(imp.Source) || filepath.VolumeName(imp.Source) != "" || strings.HasPrefix(imp.Source, "/") || strings.HasPrefix(imp.Source, `\`)) {
			return "", fmt.Errorf("%s: import %q là đường dẫn tuyệt đối, ra ngoài app; file của app import bằng app/… hay đường tương đối trong app", b.place(imp.SourceToken), imp.Source)
		}
		base := imp.Source
		if !filepath.IsAbs(base) {
			base = filepath.Join(fromDir, base)
		}
		resolved, ok := statModule(base)
		if !ok {
			// said where the import stands (18/11/2026: the bundler named the folder's absolute path and no line)
			return "", b.missing(imp, base, fmt.Sprintf("import %q: không có file ấy", imp.Source))
		}
		if b.appRoot != "" {
			if outside, err := b.outsideApp(resolved); err != nil {
				return "", fmt.Errorf("%s: import %q %v", b.place(imp.SourceToken), imp.Source, err)
			} else if outside {
				return "", fmt.Errorf("%s: import %q ra ngoài app: file nó tới không nằm trong gốc app (app khác, hay file của host); code dùng chung của app đặt trong một thư mục của app (core/), import bằng app/…", b.place(imp.SourceToken), imp.Source)
			}
		}
		// nothing imports from an app's data, whatever the spelling (KITWORK_UI.md mục 4): a relative path into
		// a .database/ is refused as app/.database/ is (22/10/2026)
		if b.appRoot != "" && b.inAppData(resolved) {
			return "", fmt.Errorf("%s: import %q: .database/ là dữ liệu của app, không import được; code dùng chung đặt trong một thư mục của app (core/)", b.place(imp.SourceToken), imp.Source)
		}
		return resolved, nil
	}
	where := b.place(imp.SourceToken)
	if b.appRoot == "" {
		return "", fmt.Errorf("%s: import %q: file này không thuộc app nào nên không có app/; app/ là thư mục của app chứa file đang viết (app/ khi một app, apps/<identity>/ khi nhiều app)", where, imp.Source)
	}
	// the parser refused every spelling that leaves the app; a program built another way meets the rule here
	if problem := appRootSpecifierProblem(imp.Source); problem != "" {
		return "", fmt.Errorf("%s: %s", where, problem)
	}
	rest := strings.TrimPrefix(imp.Source, "app/")
	base := filepath.Join(b.appRoot, filepath.FromSlash(rest))
	resolved, ok := statModule(base)
	if !ok {
		return "", b.missing(imp, base, fmt.Sprintf("import %q: app không có file %s", imp.Source, rest))
	}
	// the same rule on the disk, past links: the file stays inside the app and out of its data
	relative, err := b.appRelative(resolved)
	if err != nil {
		// a link of the app that the system does not let Go follow (a junction on Windows): refused,
		// never guessed inside, and named
		return "", fmt.Errorf("%s: import %q %v; app/ chỉ trỏ tới file nằm trong gốc app", where, imp.Source, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s: import %q ra ngoài app: file nó tới (qua liên kết) không nằm trong gốc app", where, imp.Source)
	}
	if first, _, _ := strings.Cut(filepath.ToSlash(relative), "/"); strings.EqualFold(first, ".database") {
		return "", fmt.Errorf("%s: import %q: .database/ là dữ liệu của app, không import được; code dùng chung đặt trong một thư mục của app (core/)", where, imp.Source)
	}
	return resolved, nil
}

// outsideApp reports whether path lies outside the app's folder, as written or past links; err is a link
// of the app the system does not let Go follow, which is refused, never guessed inside (appRelative).
func (b *bundler) outsideApp(path string) (bool, error) {
	if relative, err := filepath.Rel(b.appRoot, path); err != nil || leaves(relative) {
		return true, nil
	}
	relative, err := b.appRelative(path)
	if err != nil {
		return false, err
	}
	return leaves(relative), nil
}

// leaves reports a relative path that goes up out of its base, or is not relative at all.
func leaves(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative)
}

// inAppData reports whether path lies in a .database/ folder of this app or of an app beside it (counted
// from the folder that holds the app: apps/ for many apps, the host for one), as written or past links.
func (b *bundler) inAppData(path string) bool {
	holder := filepath.Dir(b.appRoot)
	if hasDataSegment(holder, path) {
		return true
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	holderReal, err := filepath.EvalSymlinks(holder)
	if err != nil {
		return false
	}
	return hasDataSegment(holderReal, real)
}

// hasDataSegment reports a folder .database on the way from base to path (".database." and ".database "
// too, which Windows opens as .database).
func hasDataSegment(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if strings.EqualFold(strings.TrimRight(segment, ". "), ".database") {
			return true
		}
	}
	return false
}

// appRelative is path from the app's folder, both past links. When the system does not let Go follow a
// link on the way (filepath.EvalSymlinks fails below a junction on Windows, "The system cannot find the
// path specified"), the path is read as written (writtenRelative): a link above the app's folder is the
// host's layout (its data kept on another disk), only one at or below it is the app's doing. Until
// 05/11/2026 every import of an app whose apps/ folder, or any folder above, was a junction was refused as
// leaving the app.
func (b *bundler) appRelative(path string) (string, error) {
	if b.appRootReal == "" {
		if real, err := filepath.EvalSymlinks(b.appRoot); err == nil {
			b.appRootReal = real
		}
	}
	if b.appRootReal != "" {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Rel(b.appRootReal, real)
		}
	}
	return b.writtenRelative(path)
}

// writtenRelative is path from the app's folder as written, when no folder or file on the way from the
// app's folder down to it is a link (a symbolic link, a junction, any other reparse point): one that is
// is an error naming it, since where it leads cannot be told, and is never guessed inside.
func (b *bundler) writtenRelative(path string) (string, error) {
	relative, err := filepath.Rel(b.appRoot, path)
	if err != nil || leaves(relative) {
		return relative, err
	}
	at := b.appRoot
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		at = filepath.Join(at, segment)
		info, err := os.Lstat(at)
		if err != nil {
			return "", err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			written, _ := filepath.Rel(b.appRoot, at)
			return "", fmt.Errorf("đi qua liên kết %s trong app, không lần được nó dẫn tới đâu", filepath.ToSlash(written))
		}
	}
	return relative, nil
}

// place is where tok stands, file:line:column as the parser writes it, read from the source of the file it
// is in; the file's name alone when the bundler does not hold that source.
func (b *bundler) place(tok Token) string {
	text, ok := b.sources[tok.Source]
	if !ok {
		if tok.Source != "" {
			return tok.Source
		}
		return fmt.Sprintf("pos %d", tok.Position)
	}
	return textLocation([]byte(text), tok)
}

// missing is the error of an import whose file is not there, said with the name to write when the import left off
// .work (18/11/2026: x.work was found for x, after x.kitwork.js and x.js).
func (b *bundler) missing(imp *ImportStatement, base, message string) error {
	where := b.place(imp.SourceToken)
	if !strings.HasSuffix(imp.Source, ".work") && !strings.HasSuffix(imp.Source, "/") {
		if info, err := os.Stat(base + ".work"); err == nil && !info.IsDir() {
			return fmt.Errorf("%s: %s; import viết đủ tên file, cả đuôi: %q", where, message, imp.Source+".work")
		}
	}
	return fmt.Errorf("%s: %s", where, message)
}

// statModule is the file or folder at path, named as an import writes it: a file by its whole name (18/11/2026:
// x, x.kitwork.js, x.js, x/index.kitwork.js, x/index.js, x.work and x/index.work were tried in turn, so an import
// read a file it did not name, a .js beside a .work first; and a specifier that began with _ walked up the folders
// to the first _core/ above, past the app).
func statModule(path string) (string, bool) {
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return filepath.Clean(path), true
}

/* ---- builder AST tổng hợp (lower về node sẵn có, không opcode mới) ---- */

// moduleFunctionName names the function an imported file's top level runs in (moduleIIFE): its top level is a file's,
// as the program's is (scope.go, letPlace).
const moduleFunctionName = "<module>"

// moduleIIFE: const <name> = (() => { body })();
func moduleIIFE(name string, body []Statement) Statement {
	origin := Token{}
	if len(body) > 0 {
		origin.Source = getNodeSource(body[0])
		origin.Position = getNodePosition(body[0])
	}
	fn := &FunctionLiteral{
		Token:      tokenFrom(origin, Function),
		Parameters: []*Identifier{},
		Body:       &BlockStatement{Token: tokenFrom(origin, LeftBrace), Statements: body},
		DebugName:  moduleFunctionName,
	}
	return &VarStatement{
		Token:        constTok(origin),
		Names:        []*Identifier{ident(name, origin)},
		DestructMode: DestructNone,
		Value: &CallExpression{
			Token:    tokenFrom(origin, LeftParen),
			Function: fn,
		},
	}
}

// exportReturn: return { a: a, c: b, default: __kw_default }; (export { b as c } gives c the value of b)
func exportReturn(prog *Program, origin Token) Statement {
	locals := make(map[string]string, len(prog.ExportAliases))
	for _, alias := range prog.ExportAliases {
		locals[alias.Exported] = alias.Local
	}
	entries := make([]ObjectEntry, 0, len(prog.Exports)+1)
	for _, name := range prog.Exports {
		local := name
		if aliased, ok := locals[name]; ok {
			local = aliased
		}
		entries = append(entries, ObjectEntry{
			Key:   ident(name, origin),
			Value: ident(local, origin),
		})
	}
	if prog.HasDefault {
		entries = append(entries, ObjectEntry{
			Key:   ident("default", origin),
			Value: ident(DefaultExportName, origin),
		})
	}
	return &ReturnStatement{
		Token:       tokenFrom(origin, Return),
		ReturnValue: &ObjectLiteral{Token: tokenFrom(origin, LeftBrace), Entries: entries},
	}
}

// memberBinding: const <local> = <modVar>.<prop>;  (xử lý cả named + alias + default)
func memberBinding(local, modVar, prop string, origin Token) Statement {
	return &VarStatement{
		Token:        constTok(origin),
		Names:        []*Identifier{ident(local, origin)},
		DestructMode: DestructNone,
		Value: &MemberExpression{
			Token:    tokenFrom(origin, Dot),
			Object:   ident(modVar, origin),
			Property: ident(prop, origin),
		},
	}
}

func ident(name string, origin Token) *Identifier {
	return &Identifier{
		Token: Token{
			Kind:     Ident,
			Value:    value.String(name),
			Source:   origin.Source,
			Position: origin.Position,
		},
		Value: name,
	}
}

func constTok(origin Token) Token {
	token := tokenFrom(origin, Const)
	token.Value = value.String("const")
	return token
}

func tokenFrom(origin Token, kind Kind) Token {
	return Token{
		Kind:     kind,
		Source:   origin.Source,
		Position: origin.Position,
	}
}

// hasRelativeImports báo Program có ImportStatement (luôn là import tương đối, vì
// import "kitwork/<tên>" được hạ thẳng về VarStatement trong parser).
func hasRelativeImports(prog *Program) bool {
	for _, s := range prog.Statements {
		if _, ok := s.(*ImportStatement); ok {
			return true
		}
	}
	return false
}
