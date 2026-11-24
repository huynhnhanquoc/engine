package runtime_test

import (
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// A name JavaScript's prototype of a kind holds and Kitwork's does not is the TYPE error when read, as a call of it
// is (19/11/2026, docs/language.md mục 4.4, 8.2, 8.3): it read undefined, a value other than JavaScript's function, so
// [1].entries ? 1 : 2 was 2; and f.caller, f.arguments, which strict JavaScript throws for, read undefined. Object's
// (isPrototypeOf, constructor read by a key held in a variable…) for every kind. A name no prototype holds reads
// undefined as in JavaScript, and an object's own key is its own whatever its name.
//
// CONTROL (19/11/2026): with javascriptOnly giving undefined, every refused read was "read"; with caller not said
// apart, f.caller said the general message.
func TestANameOnlyJavaScriptsPrototypeHoldsIsTheTypeError(t *testing.T) {
	program := load(t, `const read = (f) => { const r = safe(f); return r.error === null ? "read" : r.error.code + " " + r.error.message; };
const f = (x) => x;
const constructorKey = "constructor";
const protoKey = "__proto__";
const ownKey = "isPrototypeOf";
result = [
  read(() => [1].entries), read(() => [1].values), read(() => 'x'.match), read(() => 'x'.normalize),
  read(() => (5).toLocaleString), read(() => at.getFullYear), read(() => at.toJSON), read(() => f.call),
  read(() => ({}).isPrototypeOf), read(() => ({})[constructorKey]), read(() => ({})[protoKey]),
  read(() => f.caller), read(() => f.arguments), read(() => Math.max.caller),
  [1].map ? 1 : 2, ({ entries: 5 }).entries, ({ isPrototypeOf: 6 })[ownKey], ({}).hasOwnProperty ? 1 : 2,
  'x'.nope, [1].groupBy, typeof at.getTime, gap.hours()
];`)
	program.globals["at"] = value.Time(time.Unix(0, 0).UTC())
	program.globals["gap"] = value.Duration(2 * time.Hour)
	program.run(t)
	want := `array ["TYPE array của Kitwork chưa có entries (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE array của Kitwork chưa có values (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE string của Kitwork chưa có match (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE string của Kitwork chưa có normalize (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE number của Kitwork chưa có toLocaleString (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE time của Kitwork chưa có getFullYear (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE time của Kitwork chưa có toJSON (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE function của Kitwork chưa có call (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE object của Kitwork chưa có isPrototypeOf (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE object của Kitwork chưa có constructor (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE object của Kitwork chưa có __proto__ (JavaScript có): đọc hay gọi nó là lỗi (docs/language.md mục 4.4)",` +
		`"TYPE caller của một hàm không đọc được: JavaScript strict ném TypeError ở đây, Kitwork không có caller",` +
		`"TYPE arguments của một hàm không đọc được: JavaScript strict ném TypeError ở đây, Kitwork không có arguments",` +
		`"TYPE caller của một hàm không đọc được: JavaScript strict ném TypeError ở đây, Kitwork không có caller",` +
		`1,5,6,1,null,null,"function",2]` // describe writes an undefined item as JSON does, null
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
