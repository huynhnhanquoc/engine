package runtime_test

import (
	"strings"
	"testing"
)

// A name JavaScript's builtin holds and Kitwork's does not, read off it, is the TYPE error (19/11/2026, docs/language.md
// mục 4.4), where it was undefined, a value other than JavaScript's: Array.prototype was undefined (JavaScript's []),
// typeof Array.prototype "undefined" ("object"), Object.hasOwn(Array, "prototype") false (true),
// Object.getOwnPropertyNames(Array) a list without prototype and fromAsync. The builtin is known by what it is, not by
// how it is written: one read through another name is refused too. A name neither has stays undefined, as in
// JavaScript, and so does a program's function, whatever it is named.
//
// CONTROL (19/11/2026): with JavaScriptStatic and StaticsMissing answering nothing, every refused read gave undefined,
// typeof "undefined", the hasOwn false and getOwnPropertyNames its lists; with builtinName not following a stand-in,
// Object.getOwnPropertyNames(Array) gave its list and Object.hasOwn(Array, "prototype") false, the builtin handed to
// them being a stand-in; with the name read off the function's own field only, no constructor was refused (core/builtins
// writes it as a key).
func TestANameOnlyJavaScriptsBuiltinHoldsIsRefused(t *testing.T) {
	program := load(t, `const refused = (f) => { const r = safe(f); return r.error === null ? "gave " + (JSON.stringify(r.value) ?? "undefined") : r.error.code; };
const A = Array;
const f = { Array: (x) => x };
result = [refused(() => Array.prototype), refused(() => Object.prototype), refused(() => Number.prototype),
  refused(() => String.prototype), refused(() => Boolean.prototype), refused(() => Date.prototype),
  refused(() => typeof Array.prototype), refused(() => Array.fromAsync), refused(() => String.raw),
  refused(() => Object.create), refused(() => Object.getPrototypeOf), refused(() => JSON.rawJSON),
  refused(() => console.table), refused(() => A.prototype),
  refused(() => Object.getOwnPropertyNames(Array)), refused(() => Object.getOwnPropertyNames(console)),
  refused(() => Object.hasOwn(Array, "prototype")), refused(() => Array.hasOwnProperty("prototype")),
  refused(() => JSON.hasOwnProperty("rawJSON")),
  refused(() => Math.sqr), refused(() => Array.foo), refused(() => ((x) => x).prototype), refused(() => Array.from.prototype),
  refused(() => f.Array.prototype), refused(() => ({ prototype: 1 }).prototype), refused(() => Object.hasOwn(Array, "from")),
  refused(() => Object.getOwnPropertyNames(Math).length > 40), refused(() => Array.isArray([]))];`)
	program.run(t)
	got := describe(program.variable("result"))
	want := `array [` + strings.Repeat(`"TYPE",`, 19) + `"gave undefined","gave undefined","gave undefined","gave undefined","gave undefined","gave 1","gave true","gave true","gave true"]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	message := load(t, `result = safe(() => Array.prototype).error.message;`)
	message.run(t)
	if text := message.variable("result").Text(); text != "Array của Kitwork chưa có prototype (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)" {
		t.Errorf("the message is %q", text)
	}
}
