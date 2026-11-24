package compiler_test

import "testing"

// A template literal makes each ${…} text by ToString, the string hint, as JavaScript's template does:
// an object's own toString before its valueOf (bytecode 8, TEMPLATE). It compiled to ADD, the default
// hint of +, so `${{ toString: () => "T", valueOf: () => 7 }}` was "7". + keeps its own hint. The
// answers are node 22's.
func TestATemplateMakesEachPartTextByToString(t *testing.T) {
	names, _ := instructions(t, "a = 1; b = `x${a}y${a}`;")
	if !contains(names, "TEMPLATE") || contains(names, "ADD") {
		t.Errorf("a template compiled to %v, want TEMPLATE and no ADD", names)
	}
	for _, c := range []struct{ source, want string }{
		{"`${({ toString: () => \"T\", valueOf: () => 7 })}`", `"T"`},
		{"`a${({ toString: () => \"T\", valueOf: () => 7 })}b`", `"aTb"`},
		{"`${({ valueOf: () => 42 })}`", `"[object Object]"`},
		{"\"\" + ({ toString: () => \"T\", valueOf: () => 7 })", `"7"`},
		{"`${1}${2}`", `"12"`},
		{"`${[1, [2, 3]]}|${null}|${undefined}|${true}`", `"1,2,3|null|undefined|true"`},
		{"`${({ toString: () => ({}), valueOf: () => 8 })}`", `"8"`},
		{"safe(() => `${({ toString: () => error(\"t\") })}`).error === null", `false`},
	} {
		got := runSource(t, "const result = JSON.stringify("+c.source+");")
		if got.Text() != c.want {
			t.Errorf("%s gives %s, want %s", c.source, got.Failure()+got.Text(), c.want)
		}
	}
}
