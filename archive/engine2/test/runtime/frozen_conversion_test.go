package runtime_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// An object a frozen run made, with its own toString or valueOf, cannot be converted at a request: the
// function would run on the frozen VM (KITWORK_BOOT mục 10, a TYPE failure). Every way a program converts
// one gives that failure, as + did (05/11/2026): join, String of an array, a key, a comparison, ==, the
// default sort, the arguments of a string's methods, Math, parseInt and isNaN read it as "" or NaN, and
// answered a value that was neither JavaScript's nor the failure ([p].join() was "", table[p] read the key
// "", price > 10 was false, Math.max(price, 1) was 1).
func TestEveryConversionOfAFrozenRunsObjectIsItsFailure(t *testing.T) {
	routes := map[string]string{
		"join":         `[p].join(",")`,
		"stringArray":  `String([p])`,
		"template":     "`${[p]}`",
		"plusArray":    `[p] + ""`,
		"keyRead":      `table[p]`,
		"arrayKey":     `[1, 2][p]`,
		"textKey":      `"ab"[p]`,
		"keyWrite":     `(() => { const o = {}; o[p] = 1; return Object.keys(o) })()`,
		"computedKey":  `JSON.stringify({ [p]: 1 })`,
		"sortDefault":  `[p, q].sort()`,
		"less":         `p < "z"`,
		"greater":      `price > 10`,
		"lessEqual":    `price <= 50`,
		"looseEqual":   `p == "k"`,
		"looseNumber":  `price == 42`,
		"split":        `"akb".split(p)`,
		"replace":      `"akb".replace(p, "-")`,
		"replaceAll":   `"akb".replaceAll(p, "+")`,
		"indexOf":      `"xkx".indexOf(p)`,
		"includes":     `"xkx".includes(p)`,
		"padStart":     `"a".padStart(price)`,
		"arrayAt":      `[1, 2].at(price)`,
		"arrayJoin":    `[1, 2].join(p)`,
		"mathMax":      `Math.max(price, 1)`,
		"mathFloor":    `Math.floor(price)`,
		"parseInt":     `parseInt(p)`,
		"isNaN":        `isNaN(price)`,
		"number":       `Number(price)`,
		"fromCharCode": `String.fromCharCode(price)`,
		"encode":       `encodeURIComponent(p)`,
	}
	source := `const p = { toString: () => "k" };
const q = { toString: () => "a" };
const price = { valueOf: () => 42 };
const table = { k: "found-k", "": "found-empty" };
`
	for name, expression := range routes {
		source += "const route_" + name + " = () => " + expression + ";\n"
	}
	boot := booted(t, source)
	boot.vm.Freeze()
	machine := boot.request(nil)
	for name, expression := range routes {
		got := boot.call(machine, "route_"+name)
		if !got.IsInvalid() || got.Code() != value.TypeCode || !strings.Contains(got.Message(), "was made as the site booted") {
			t.Errorf("%s: %s, want the TYPE failure of a frozen run's toString or valueOf", expression, describe(got))
		}
	}
}

// A conversion that fails inside a request is the failure of what converts, as JavaScript throws out of
// it: an object whose toString or valueOf calls error(), joined, compared, used as a key or as an
// argument, is caught by safe as { ok: false } (05/11/2026: join, String of an array, <, ==, keys and
// Math.max read the failure as "" or NaN and went on).
func TestAConversionThatFailsIsTheFailureOfWhatConverts(t *testing.T) {
	routes := []string{
		`[bad].join()`, `String([bad])`, `[1, bad] + ""`, "`${[bad]}`", `bad < 1`, `bad > 1`, `bad <= 1`,
		`bad == 1`, `bad != 1`, `Math.max(badNumber, 1)`, `Math.min(1, badNumber)`, `({})[bad]`,
		`(() => { const o = {}; o[bad] = 1; return o })()`, `"akb".split(bad)`, `"akb".indexOf(bad)`,
		`"a".concat(bad)`, `parseInt(bad)`, `isNaN(badNumber)`, `[bad, bad].sort()`, `[2, 1].sort(() => badNumber)`,
		`[badNumber].indexOf(1, badNumber)`, `JSON.stringify({ [bad]: 1 })`, `Math.max([bad], 1)`,
		`new Date(badNumber)`, `"ab".padStart(5, bad)`, `[1, 2].join(bad)`, `Math.hypot(3, badNumber)`,
		`Math.floor(badNumber)`, `parseFloat(bad)`, `isFinite(badNumber)`, `encodeURI(bad)`,
	}
	source := ""
	for i, expression := range routes {
		source += "const route" + strconv.Itoa(i) + ` = () => {
  const bad = { toString: () => error("t") };
  const badNumber = { valueOf: () => error("v") };
  return safe(() => ` + expression + `).error === null;
};
`
	}
	boot := booted(t, source)
	boot.vm.Freeze()
	machine := boot.request(nil)
	for i, expression := range routes {
		if got := boot.call(machine, "route"+strconv.Itoa(i)); got.IsInvalid() || got.Boolean() {
			t.Errorf("safe(() => %s).error === null: %s, want false: the conversion's failure", expression, describe(got))
		}
	}
}

// An object the request makes converts through its own toString or valueOf as JavaScript does, whoever
// made the function it holds: one of a frozen file's top level is kept as a stand-in that runs on the
// request's VM (05/11/2026; { toString: label } was a TYPE failure, and so was safe given to map, flatMap
// or Array.from, which handed each function of a frozen array to safe). The answers are node 22's.
func TestAFrozenRunsFunctionsConvertAndRunWhereTheRequestHoldsThem(t *testing.T) {
	boot := booted(t, `
const label = () => "LABEL";
const five = () => 5;
const key = () => "k";
const tasks = [() => 1, () => 2];
const checks = { a: () => true, b: () => false };
const strings = () => { const o = { toString: label }; return String(o) + "/" + `+"`${o}`"+`; };
const plus = () => ({ valueOf: five }) + 1;
const joined = () => [{ toString: label }, { toString: label }].join("+");
const keyed = () => { const box = {}; box[{ toString: key }] = 1; return Object.keys(box).join(","); };
const methods = { toString: label };
const spread = () => { const o = { ...methods }; return String(o); };
const mapSafe = () => tasks.map(safe).map((r) => r.value).join(",");
const fromSafe = () => Array.from(tasks, safe).map((r) => r.value).join(",");
const flatSafe = () => tasks.flatMap(safe).map((r) => r.value).join(",");
const valuesSafe = () => Object.values(checks).map(safe).map((r) => r.value).join(",");
const lastSafe = () => tasks.findLast((t) => safe(t).value === 1) === tasks[0];
const same = () => [[].concat(safe)[0] === safe, [label].includes(label), { f: label }.f === label];
`)
	boot.vm.Freeze()
	machine := boot.request(nil)
	for name, want := range map[string]string{
		"strings": "LABEL/LABEL", "plus": "6", "joined": "LABEL+LABEL", "keyed": "k", "spread": "LABEL",
		"mapSafe": "1,2", "fromSafe": "1,2", "flatSafe": "1,2", "valuesSafe": "true,false", "lastSafe": "true",
		"same": "true,true,true",
	} {
		got := boot.call(machine, name)
		if text := got.ToString().Text(); got.IsInvalid() || text != want {
			t.Errorf("%s: %s, want %s", name, describe(got), want)
		}
	}
}

// A closure the file made writes a let of its loop's iteration into the object the iteration made: the
// refusal names the variable as the source wrote it, as the refusal of every other variable a frozen run
// holds does (05/11/2026: "cannot assign to read only property 'i#1' of object '#<Object>'").
func TestARefusedWriteToAnIterationsVariableNamesIt(t *testing.T) {
	boot := booted(t, `
const counters = [];
for (let i = 0; i < 2; i++) { counters.push(() => { i = i + 10; return i; }); }
const items = [];
for (const x of [1, 2]) { let j = x; items.push(() => { j = j + 10; return j; }); }
const first = () => counters[0]();
const second = () => items[0]();
`)
	boot.vm.Freeze()
	machine := boot.request(nil)
	for name, variable := range map[string]string{"first": "i", "second": "j"} {
		got := boot.call(machine, name)
		if !got.IsInvalid() || !strings.Contains(got.Message(), "cannot assign to "+variable+": it belongs to the code that ran as the site booted") {
			t.Errorf("%s: %s, want the refusal naming %s", name, describe(got), variable)
		}
	}
}

// A stand-in a VM made while it booted (a host kept a function another VM made, or a Go function, handed
// to it then) runs nothing on that VM once it is frozen: a kept stand-in of a function is the TYPE failure
// of a frozen run, and a kept stand-in of a Go function hands its arguments over as they are, so a request's
// own function given to it runs on the request's VM (05/11/2026; such a stand-in ran its function on the
// frozen VM, from every request at once).
func TestAStandInAFrozenVMMadeRunsNothingOnIt(t *testing.T) {
	other := booted(t, `const double = (x) => x * 2;`)
	var kept []value.Value
	keep := value.Function(func(arguments ...value.Value) value.Value {
		kept = append(kept, arguments...)
		return value.Undefined()
	})
	globals := map[string]value.Value{"keep": keep, "double": other.variable("double")}
	for name, item := range other.globals {
		globals[name] = item
	}
	boot := load(t, `keep(double); keep(safe);`)
	boot.vm.FastReset(boot.program, globals)
	if got := boot.vm.Run(); got.IsInvalid() {
		t.Fatal(got.Failure())
	}
	boot.vm.Freeze()
	other.vm.Freeze()
	if len(kept) != 2 {
		t.Fatalf("kept %d functions", len(kept))
	}
	if got := kept[0].Call([]value.Value{value.Number(2)}); !got.IsInvalid() || !strings.Contains(got.Message(), "was made as the site booted") {
		t.Errorf("a kept stand-in of another VM's function, its VM frozen: %s, want the TYPE failure", describe(got))
	}
	request := load(t, `answer = kept(() => 7).value;`)
	request.vm.FastReset(request.program, map[string]value.Value{"kept": kept[1]})
	if got := request.vm.Run(); got.IsInvalid() || request.variable("answer").Number() != 7 {
		t.Errorf("a request's function given to a kept stand-in of safe: %s %s, want 7", describe(got), describe(request.variable("answer")))
	}
}

// A builtin converts only the arguments it reads, as JavaScript does: an object past them, even one whose
// valueOf fails, is not converted (parseInt's third argument, Math.floor's second), and the call answers as
// without it (05/11/2026: the conversion of the arguments that may run code first looked at every one, and
// a third argument of parseInt that was an object called it again and again).
func TestAnArgumentABuiltinDoesNotReadIsNotConverted(t *testing.T) {
	boot := booted(t, `
const bad = { valueOf: () => error("v"), toString: () => error("t") };
const answers = () => [parseInt("12", 10, bad), parseFloat("1.5", bad), Math.floor(1.5, bad), isNaN(1, bad), Math.pow(2, 3, bad)].join(",");
`)
	boot.vm.Freeze()
	machine := boot.request(nil)
	if got := boot.call(machine, "answers"); got.IsInvalid() || got.Text() != "12,1.5,1,false,8" {
		t.Errorf("builtins given an object past what they read: %s, want 12,1.5,1,false,8", describe(got))
	}
}
