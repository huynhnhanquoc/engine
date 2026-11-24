package runtime_test

import "testing"

// The order and the number of a program's conversions are JavaScript's (05/11/2026, each answer node
// 22's, the program run in node with the file-level values frozen as the boot freezes them):
//
//   - textOnce: an argument made text is converted once, by ToString, which runs toString before valueOf;
//     ToNumber ran first, so a valueOf ran where JavaScript never calls it, and toString twice.
//   - earlyAnswers: an argument a method answers before reading is not converted: an empty array's
//     fromIndex, the filler of a pad with nothing to pad, Object.hasOwn's key of null (ToObject first).
//   - splitOrder: split converts its limit before its separator, the separator even for a limit of 0.
//   - readsSeven: String.fromCodePoint checks each argument before it reads the next; Date.UTC and new
//     Date read seven arguments and convert no eighth.
//   - frozenShort: a frozen array of fewer than two items sorts as it is, comparator or not, one a
//     site's boot made among them.
//   - looseObjects: == of two objects of other kinds (an object and an array, a function, a time) is
//     false and converts neither side.
//   - nullKey: null[k] and undefined[k] fail before k is converted.
//   - sortNested: the default sort of arrays holding objects sorts a private copy, so a toString that
//     pops loses no item and one that fails leaves the array as it was.
//   - freezeMethod: Object.freeze of a method read ('abc'.at), handed to freeze as a stand-in, freezes the
//     method itself.
func TestTheConversionsOfAProgramAreJavaScriptsInOrderAndNumber(t *testing.T) {
	boot := booted(t, `const label = { toString: () => "x" };
const one = [{ id: 1 }];
const single = [3];
const textOnce = () => {
  let count = 0;
  const counted = { toString: () => { count = count + 1; return "k" + count; } };
  let k = 0;
  const sided = { valueOf: () => { k = 1; return 0; }, toString: () => (k === 1 ? "b" : "a") };
  const log = [];
  const both = { valueOf: () => { log.push("v"); return 1; }, toString: () => { log.push("s"); return "y"; } };
  return JSON.stringify(["k1k2".concat(counted), count, "abc".indexOf(sided), k, "xy".includes(both), "y-".split(both), log]);
};
const earlyAnswers = () => {
  const bad = { valueOf: () => error("never read"), toString: () => error("never read") };
  const a = safe(() => [].includes("q", bad));
  const b = safe(() => [].indexOf("q", bad));
  const c = safe(() => [].lastIndexOf("q", bad));
  const d = safe(() => "abcd".padEnd(2, bad));
  const e = safe(() => "abcd".padStart(4, bad));
  const log = [];
  const key = { toString: () => { log.push("k"); return "a"; } };
  const f = safe(() => Object.hasOwn(null, key));
  const g = safe(() => Object.hasOwn(undefined, { toString: () => error("key") }));
  const h = safe(() => "ab".padEnd(4, bad));
  const i = safe(() => ["q"].includes("q", bad));
  return JSON.stringify([a.error === null, a.value, b.error === null, b.value, c.error === null, c.value, d.error === null, d.value, e.error === null, e.value, f.error === null, log,
    g.error === null, g.error.message.includes("null to object"), h.error === null, i.error === null]);
};
const splitOrder = () => {
  const log = [];
  const separator = { toString: () => { log.push("S"); return ","; } };
  const limit = { valueOf: () => { log.push("L"); return 2; } };
  const parts = "x,y,z".split(separator, limit);
  const none = "x,y".split(separator, 0);
  const r = safe(() => "a,b".split({ toString: () => error("separator") }, { valueOf: () => error("limit") }));
  return JSON.stringify([parts, none, log, r.error === null, r.error.message.includes("limit")]);
};
const readsSeven = () => {
  const log = [];
  const logged = { valueOf: () => { log.push("v"); return 65; } };
  const a = safe(() => String.fromCodePoint(1114112, logged));
  const b = safe(() => String.fromCodePoint(0.5, { valueOf: () => error("second") }));
  const c = safe(() => Date.UTC(2001, 1, 2, 3, 4, 5, 6, { valueOf: () => error("eighth") }));
  const d = safe(() => new Date(2001, 1, 2, 3, 4, 5, 6, logged));
  return JSON.stringify([a.error === null, b.error === null, b.error.message.toLowerCase().includes("invalid code point 0.5"), c.error === null, c.value, d.error === null, log]);
};
const frozenShort = () => {
  const a = safe(() => Object.freeze([7]).sort((p, q) => p - q));
  const b = safe(() => Object.freeze([]).sort((p, q) => p - q));
  const c = safe(() => Object.freeze([{ n: 1 }]).sort());
  const d = safe(() => Object.freeze([2, 1]).sort((p, q) => p - q));
  const e = safe(() => one.sort((p, q) => p.id - q.id));
  const f = safe(() => single.sort((p, q) => p - q));
  return JSON.stringify([a.error === null, a.value, b.error === null, b.value, c.error === null, c.value, d.error === null, e.error === null, f.error === null]);
};
const looseObjects = () => {
  const failing = { toString: () => error("t"), valueOf: () => error("v") };
  const f = () => 1;
  const a = safe(() => failing == ["a"]);
  const b = safe(() => [2] != failing);
  const c = safe(() => failing == f);
  const d = safe(() => new Date(1) == failing);
  const e = safe(() => [] == label);
  const g = safe(() => label == [1]);
  return JSON.stringify([a.error === null, a.value, b.error === null, b.value, c.error === null, c.value, d.error === null, d.value, e.error === null, e.value, g.error === null, g.value,
    [1] == { toString: () => "1" }, { toString: () => "k" } == "k", [1] == 1]);
};
const nullKey = () => {
  const log = [];
  const base = null;
  const r = safe(() => base[{ toString: () => { log.push("k"); return error("KEY"); } }]);
  const nothing = undefined;
  const s = safe(() => nothing[[1]]);
  return JSON.stringify([r.error === null, r.error.message.toLowerCase().includes("cannot read properties of null"), log,
    s.error === null, s.error.message.toLowerCase().includes("cannot read properties of undefined")]);
};
const sortNested = () => {
  let calls = 0;
  const a = [];
  const popping = (x) => ({ toString: () => { calls = calls + 1; if (calls === 2) { a.pop(); } return x; } });
  a.push([popping("c")], [popping("a")], [popping("b")]);
  a.sort();
  let failures = 0;
  const failing = (x) => ({ toString: () => { failures = failures + 1; if (failures === 4) { return error("fourth"); } return x; } });
  const b = [[failing("d")], [failing("c")], [failing("b")], [failing("a")]];
  const r = safe(() => b.sort());
  return JSON.stringify([a.length, a.map((x) => String(x)), r.error === null, b.map((x) => String(x))]);
};
const freezeMethod = () => {
  const m = "abc".at;
  const r = Object.freeze(m);
  const items = [];
  items.push("abc".slice);
  Object.freeze(items[0]);
  return JSON.stringify([r === m, Object.isFrozen(m), Object.isFrozen(r), Object.isFrozen(items[0])]);
};
`)
	boot.vm.Freeze()
	machine := boot.request(nil)
	for _, c := range []struct{ name, want string }{
		{"textOnce", `["k1k2k1",1,0,0,true,["","-"],["s","s"]]`},
		{"earlyAnswers", `[true,false,true,-1,true,-1,true,"abcd",true,"abcd",false,[],false,true,false,false]`},
		{"splitOrder", `[["x","y"],[],["L","S","S"],false,true]`},
		{"readsSeven", `[false,false,true,true,981083045006,true,[]]`},
		{"frozenShort", `[true,[7],true,[],true,[{"n":1}],false,true,true]`},
		{"looseObjects", `[true,false,true,true,true,false,true,false,true,false,true,false,false,true,true]`},
		{"nullKey", `[false,true,[],false,true]`},
		{"sortNested", `[3,["a","b","c"],false,["d","c","b","a"]]`},
		{"freezeMethod", `[true,true,true,true]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := boot.call(machine, c.name); got.IsInvalid() || got.Text() != c.want {
				t.Errorf("%s: %s, want %s", c.name, describe(got), c.want)
			}
		})
	}
}
