package runtime_test

import "testing"

// An object's computed key, { [k]: v }, is made a key before its value runs, as JavaScript makes it
// (ToPropertyKey, then the value), and null and undefined are the keys "null" and "undefined" (05/11/2026:
// the parser reads [k] as an array, which SET made text after the value ran, so the log was [v, k], a key
// whose toString failed still ran the value first, and { [null]: 1 } had the key "", as [null] joins).
// node 22's answers.
func TestAComputedKeyIsMadeAKeyBeforeItsValue(t *testing.T) {
	program := load(t, `const log = [];
const k = { toString: () => { log.push("k"); return "name" } };
const o = { [k]: (() => { log.push("v"); return 1 })() };
const failed = [];
const bad = { toString: () => error("bad key") };
const r = safe(() => ({ [bad]: (() => { failed.push("v"); return 1 })() }));
const none = null;
const result = [
  o.name, log, r.error === null, failed,
  Object.keys({ [none]: 1, [undefined]: 2, [1]: 3, [[1, 2]]: 4, [-0]: 5, ["b" + 1]: 6 })
];`)
	program.run(t)
	want := `array [1,["k","v"],false,[],["0","1","null","undefined","1,2","b1"]]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
