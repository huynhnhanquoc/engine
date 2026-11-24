package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// Math, JSON and console are libraries, not data (19/11/2026, docs/language.md mục 4.3): read as data (made text,
// their keys listed, spread, assigned from, written as JSON, a replacer's walk included) each is the TYPE error, where
// String(Math) was "[object Object]" (JavaScript's "[object Math]"), Object.keys(JSON) ["parse","stringify"]
// (JavaScript's []) and JSON.stringify(Math) wrote its constants (JavaScript's "{}"). What agrees with JavaScript
// stays: Object.getOwnPropertyNames(Math), Object.hasOwn(Math, "PI"), +Math. Object.isFrozen of what the engine froze
// (a builtin, what a site's boot made) is the TYPE error, JavaScript leaving it open; of what the program froze, true.
//
// CONTROL (19/11/2026): with no library marked, every library read was "read"; with FreezeShared freezing as Freeze
// does, Object.isFrozen of the builtins and of the value the engine froze was "read" (true).
func TestALibraryIsNoData(t *testing.T) {
	program := load(t, `const read = (f) => { const r = safe(f); return r.error === null ? "read" : r.error.code + " " + r.error.message; };
result = [read(() => String(Math)), read(() => `+"`${JSON}`"+`), read(() => Object.keys(Math)), read(() => Object.values(JSON)),
  read(() => Object.entries(console)), read(() => ({ ...Math })), read(() => Object.assign({}, JSON)),
  read(() => JSON.stringify(Math)), read(() => JSON.stringify({ m: Math })), read(() => JSON.stringify([1], (k, v) => k === "0" ? JSON : v)),
  read(() => Object.isFrozen(Math)), read(() => Object.isFrozen(Number)), read(() => Object.isFrozen(engineFroze)),
  Object.getOwnPropertyNames(Math).length, Object.hasOwn(Math, "PI"), Number.isNaN(+Math), Object.isFrozen(Object.freeze({ a: 1 })),
  Object.isFrozen({}), Object.isFrozen(programFroze), Object.isFrozen(5)];`)
	program.globals["engineFroze"] = value.Object().Set("a", value.Number(1)).FreezeShared()
	program.globals["programFroze"] = value.Object().Set("a", value.Number(1)).Freeze()
	program.run(t)
	data := func(library, written string) string {
		return `"TYPE ` + written + `: ` + library + ` là thư viện, không phải dữ liệu; đọc từng tên của nó (` + library + `.` +
			map[string]string{"Math": "PI", "JSON": "parse", "console": "log"}[library] + `)",`
	}
	frozen := `"TYPE Object.isFrozen(x): x do Kitwork đóng băng vì mọi request cùng đọc nó (builtin, dữ liệu cấp file sau boot), JavaScript để nó mở; ghi vào nó là lỗi TYPE",`
	want := `array [` + data("Math", "String(Math)") + data("JSON", "String(JSON)") +
		data("Math", "Object.keys(Math), values, entries") + data("JSON", "Object.keys(JSON), values, entries") +
		data("console", "Object.keys(console), values, entries") + data("Math", "{ ...Math }") +
		data("JSON", "Object.assign(x, JSON)") + data("Math", "JSON.stringify(Math)") + data("Math", "JSON.stringify(Math)") +
		data("JSON", "JSON.stringify(JSON)") + frozen + frozen + frozen + `43,true,true,true,false,true,true]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
