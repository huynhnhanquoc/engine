package runtime_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
)

// A parameter's default (15/11/2026, round 5 of the 13/11 review): (x = 1, y = x * 2) => …, function g(a, b = a + 1),
// a destructured parameter with one, an array method's callback with one. It is used when the argument is undefined,
// missing or written so, and not for null, as in JavaScript, and reads the parameters before it. It was the compile
// error "Kitwork chưa có giá trị mặc định cho tham số", written in the body as x ?? d, which null took too.
// CONTROL: the default used for null too, red.
func TestAParameterTakesItsDefaultForUndefined(t *testing.T) {
	program := load(t, `const f = (x = 1, y = x * 2) => [x, y];
function g(a, b = a + 1) { return b; }
const h = ({ a, b } = { a: 9, b: 8 }) => a + b;
const k = (o = {}) => o;
const named = (fn = () => 1) => fn.name;
const result = [f(), f(3), f(3, 4), f(undefined, 7), f(null), g(1), g(1, null), h(), h({ a: 1, b: 2 }), k(),
  [1, 2].map((x, i = 9) => i), named()];`)
	program.run(t)
	want := `array [[1,2],[3,6],[3,4],[1,7],[null,0],2,null,17,3,{},[0,1],"fn"]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A pattern's default is used when what it reads is undefined, as in JavaScript (15/11/2026): a key that is missing
// or undefined takes it, a null keeps null, in an object pattern, an array pattern and a nested one. It took null
// too, from when Kitwork had one null. CONTROL: the default used for null too, red.
func TestAPatternTakesItsDefaultForUndefined(t *testing.T) {
	program := load(t, `const { p = 1, q = 2, r = 3 } = { p: null, q: undefined };
const [m = 5, n = 6] = [undefined, null];
const { o: { deep = "d" } = {} } = { o: { deep: null } };
const result = [p, q, r, m, n, deep];`)
	program.run(t)
	want := `array [null,2,3,5,null,null]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A default stands where JavaScript has one: not on a rest.
func TestADefaultStandsWhereJavaScriptHasOne(t *testing.T) {
	for source, want := range map[string]string{
		`const f = (...a = []) => a;`:         "phần còn lại (...tên) không có giá trị mặc định",
		`function f(...a = []) { return a; }`: "phần còn lại (...tên) không có giá trị mặc định",
	} {
		if _, err := compiler.CompileSource(source); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", source, err, want)
		}
	}
}
