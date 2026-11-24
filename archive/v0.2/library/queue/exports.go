package queue

import (
	"fmt"
	"path"
	"strconv"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/compiler"
)

// A job's name is the name it is exported under (docs/KITWORK_QUEUE.md mục 3), a schedule's too (KITWORK_SCHEDULE.md
// mục 6). The declaration runs again in every file that imports it, so a handle is told its name by where it is
// declared: the dot of queue.job(…), of schedule.daily(…), which the runtime gives as file:line:column (VM CallSite;
// a method call stands at its dot). ExportedChains reads, from a file's source, which declaration each exported name
// holds, by that place.

// ExportedChains reads source, the file of the app at file (".shared/queue/camOn.work"): for each top-level `export
// const NAME = …` whose value is a chain of calls that begins with <name>.<word>(…), begins(word) true, the place of
// that first call (file:line:column of its dot) → NAME. Which name the module is imported under is not read: a place
// no declaration runs at names nothing. An export default, an export { a as b }, and a declaration exported again
// under a second name are its errors: one declaration, one name. A source that does not parse gives nothing; its
// compile error is the boot's.
func ExportedChains(file, source string, begins func(word string) bool) (map[string]string, error) {
	parser := compiler.NewParser(compiler.NewLexerSource(source, path.Base(file)))
	program := parser.ParseProgram()
	if len(parser.Errors()) > 0 {
		return map[string]string{}, nil
	}
	folder := path.Dir(file) + "/"
	if program.HasDefault {
		return nil, fmt.Errorf("%s: export default không có trong %s: tên là tên export, export const <tên> = …", file, folder)
	}
	if len(program.ExportAliases) > 0 {
		alias := program.ExportAliases[0]
		return nil, fmt.Errorf("%s: export { … as %s } không có trong %s: tên là tên export, viết export const %s = …", file, alias.Exported, folder, alias.Exported)
	}
	exported := map[string]bool{}
	for _, name := range program.Exports {
		exported[name] = true
	}
	places := map[string]string{}
	declared := map[string]bool{}
	for _, statement := range program.Statements {
		declaration, ok := statement.(*compiler.VarStatement)
		if !ok || len(declaration.Names) != 1 || !exported[declaration.Names[0].Value] {
			continue
		}
		name := declaration.Names[0].Value
		if call := beginning(declaration.Value, begins); call != nil {
			places[file+":"+position(source, int(call.Token.Position))] = name
			declared[name] = true
		}
	}
	for _, statement := range program.Statements {
		declaration, ok := statement.(*compiler.VarStatement)
		if !ok || len(declaration.Names) != 1 || !exported[declaration.Names[0].Value] {
			continue
		}
		if other, ok := unchain(declaration.Value).(*compiler.Identifier); ok && declared[other.Value] {
			return nil, fmt.Errorf("%s: %s export thêm dưới tên %s: một khai báo, một tên", file, other.Value, declaration.Names[0].Value)
		}
	}
	return places, nil
}

// beginning is the first call of the chain expression is, when it begins with <name>.<word>(…) and begins(word):
// queue.job(fn) of queue.job(fn).retry(3).timeout("30s"). nil for any other value.
func beginning(expression compiler.Expression, begins func(string) bool) *compiler.MethodCallExpression {
	for {
		call, ok := unchain(expression).(*compiler.MethodCallExpression)
		if !ok || call.Method == nil {
			return nil
		}
		if _, ok := unchain(call.Object).(*compiler.Identifier); ok {
			if begins(call.Method.Value) {
				return call
			}
			return nil
		}
		expression = call.Object
	}
}

func unchain(expression compiler.Expression) compiler.Expression {
	if chain, ok := expression.(*compiler.ChainExpression); ok {
		return chain.Expression
	}
	return expression
}

// position is the line and the column of the byte at offset as the runtime writes a place: lines from 1, columns
// from 1 counted in characters.
func position(source string, offset int) string {
	offset = min(max(offset, 0), len(source))
	line, start := 1, 0
	for index := 0; index < offset; index++ {
		if source[index] == '\n' {
			line, start = line+1, index+1
		}
	}
	return strconv.Itoa(line) + ":" + strconv.Itoa(utf8.RuneCountInString(source[start:offset])+1)
}
