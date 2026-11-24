package schema

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// The rules a value from outside can break, each a word the program and a form read in error.fields (docs/schema.md
// mục 6): the rule's own name where the author wrote one (.min(), .max(), .email(), choice(…), .unique(),
// foreign(…)), required for a value left out or null that the field needs, type for a value that is not of the
// field's kind (a text where a number goes, a date not YYYY-MM-DD, a decimal with too many places).
const (
	RuleRequired = "required"
	RuleType     = "type"
	RuleMin      = "min"
	RuleMax      = "max"
	RuleEmail    = "email"
	RuleChoice   = "choice"
	RuleUnique   = "unique"
	RuleForeign  = "foreign"
)

// maxProblems is the most a failure's message lists: data from outside may be wrong in every one of many items.
const maxProblems = 20

// maxFields is the most a failure's fields hold: a form's every input, never a list of ten thousand wrong items.
const maxFields = 100

// problems are what parse found wrong, in the order met: the lines of the message and the fields.
type problems struct {
	count  int
	lines  []string
	fields []value.Value
}

// add is the value at path refused by rule, text what a person reads of it (never the value).
func (p *problems) add(path []value.Value, rule, text string) {
	p.count++
	switch {
	case len(p.lines) < maxProblems && len(path) == 0:
		p.lines = append(p.lines, text)
	case len(p.lines) < maxProblems:
		p.lines = append(p.lines, written(path)+": "+text)
	case len(p.lines) == maxProblems:
		p.lines = append(p.lines, "…")
	}
	if len(p.fields) < maxFields {
		p.fields = append(p.fields, FieldProblem(path, rule, text))
	}
}

// failure is the one INVALID_INPUT failure of every problem found, with its fields.
func (p *problems) failure() value.Value {
	return value.Fail(value.InvalidInputCode, "dữ liệu không hợp lệ: "+strings.Join(p.lines, "; ")).WithFields(value.Array(p.fields))
}

// FieldProblem is one entry of a failure's fields: { path, rule, message }, path a copy of the field's place
// (["email"], ["items", 2, "name"]). kitwork/database makes its own with it, for the value a write refused.
func FieldProblem(path []value.Value, rule, message string) value.Value {
	return value.Object().
		Set("path", value.Array(append([]value.Value(nil), path...))).
		Set("rule", value.String(rule)).
		Set("message", value.String(message))
}

// written is a field's path as a person reads it: email, address.city, tags[2].
func written(path []value.Value) string {
	var out strings.Builder
	for i, step := range path {
		switch {
		case step.IsNumber():
			out.WriteString("[" + strconv.Itoa(int(step.Number())) + "]")
		case i > 0:
			out.WriteString("." + step.Text())
		default:
			out.WriteString(step.Text())
		}
	}
	return out.String()
}
