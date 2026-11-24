package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/value"
)

// A table's transform (docs/KITWORK_SCHEMA.md mục 5): (value) => value.trim().toLowerCase(), read as the steps it
// takes, never run. A step is one of trim, toLowerCase, toUpperCase, each the same if run again, so a value a
// .parse() already transformed goes through them once more unchanged. The engine takes them on every value it
// writes into the column, before the column's rules are held, and on every value a condition compares with it, so
// customers.email === "AN@x.vn" finds the row kept as "an@x.vn". A step runs as the engine's own string method of
// that name (value.Trim, ToLowerCase, ToUpperCase): what .parse() runs in JavaScript and what the database writes are
// one function. A transform written with anything else is the boot's error, where the table is declared.
//
// The port's clients (TablePlus, a PostgreSQL driver) are not held to it yet: that is the port's slice.

// transformSteps are the steps a table's transform takes.
var transformSteps = map[string]bool{"trim": true, "toLowerCase": true, "toUpperCase": true}

// translateTransforms reads the transforms of every table of the entries, as their declaring pass made them.
func (c *Catalog) translateTransforms() error {
	for _, entry := range c.entries {
		file, _, _ := strings.Cut(entry.Where, ":")
		for i := range entry.Tables {
			table := &entry.Tables[i]
			for _, f := range table.Fields {
				if f.Transforms == 0 {
					continue
				}
				var steps []string
				for _, fn := range f.TransformFunctions() {
					read, err := c.transformOf(fn, file)
					if err != nil {
						return fmt.Errorf("%s.%s: transform chưa dịch được sang SQL: %v (%s là bảng ở %s)", table.Name, f.Name, err, table.Name, entry.Where)
					}
					steps = append(steps, read...)
				}
				if len(steps) > 0 && f.Kind != "string" && f.Kind != "choice" {
					return fmt.Errorf("%s.%s: transform của bảng đổi chữ (trim, toLowerCase, toUpperCase); %s là %s (%s là bảng ở %s)", table.Name, f.Name, f.Name, f.Kind, table.Name, entry.Where)
				}
				if table.Transforms == nil {
					table.Transforms = map[string][]string{}
				}
				table.Transforms[f.Name] = steps
			}
		}
	}
	return nil
}

// transformOf reads one transform: its lambda's body, found by its place in the file that wrote it.
func (c *Catalog) transformOf(fn value.Value, schemaFile string) ([]string, error) {
	lambda := fn.Lambda()
	if lambda == nil {
		return nil, fmt.Errorf("transform không phải một lambda của chương trình")
	}
	path := filepath.Join(c.root, filepath.FromSlash(filepath.ToSlash(filepath.Dir(schemaFile))), filepath.FromSlash(lambda.SourceFile))
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("không đọc được %s", lambda.SourceFile)
	}
	parser := compiler.NewParser(compiler.NewLexerSource(string(text), lambda.SourceFile))
	parsed := parser.ParseProgram()
	reader := &place{text: string(text), file: lambda.SourceFile}
	var function *compiler.FunctionLiteral
	walk(parsed, func(n any) {
		if f, ok := n.(*compiler.FunctionLiteral); ok && function == nil {
			if line, column := reader.lineColumn(f.Token.Position); line == lambda.SourceLine && column == lambda.SourceColumn {
				function = f
			}
		}
	})
	if function == nil {
		return nil, fmt.Errorf("không thấy lambda ở %s:%d:%d", lambda.SourceFile, lambda.SourceLine, lambda.SourceColumn)
	}
	if len(function.Parameters) != 1 {
		return nil, fmt.Errorf("transform nhận một tham số: (value) => value.trim()")
	}
	parameter := function.Parameters[0].Value
	body, err := lambdaBody(function, reader.at(function.Token))
	if err != nil {
		return nil, fmt.Errorf("transform là một biểu thức: (value) => value.trim().toLowerCase()")
	}
	var steps []string
	for expression := body; ; {
		switch e := unchain(expression).(type) {
		case *compiler.Identifier:
			if e.Value != parameter {
				return nil, fmt.Errorf("%s", body.String())
			}
			return steps, nil
		case *compiler.MethodCallExpression:
			if e.Method == nil || !transformSteps[e.Method.Value] || len(e.Arguments) != 0 {
				return nil, fmt.Errorf("%s: trên bảng, transform chỉ có trim(), toLowerCase(), toUpperCase()", body.String())
			}
			steps = append([]string{e.Method.Value}, steps...)
			expression = e.Object
		default:
			return nil, fmt.Errorf("%s: trên bảng, transform chỉ có trim(), toLowerCase(), toUpperCase()", body.String())
		}
	}
}

// transformed is a value as the table's transform of the column makes it: a string through each step, anything
// else as it is (the column's rules say what it is).
func (e *Entry) transformed(table, column string, item value.Value) value.Value {
	if !item.IsString() {
		return item
	}
	t := e.table(table)
	if t == nil {
		return item
	}
	for _, step := range t.Transforms[column] {
		switch step {
		case "trim":
			item = item.Trim()
		case "toLowerCase":
			item = item.ToLowerCase()
		case "toUpperCase":
			item = item.ToUpperCase()
		}
	}
	return item
}
