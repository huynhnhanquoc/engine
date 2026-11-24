package runtime_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// What an instruction reads (core/value, work.go, its second part, 15/11/2026): an instruction costs a few units
// whatever it does, and one over a long text, a long key or a large object read it for milliseconds at that
// price, so a loop of it ran until the request's time on a sliver of its energy, as a builtin's did before its work
// was counted. What it reads is now counted from 64 bytes on, and each loop below stops at its energy within a few
// times what a loop of additions takes for the same energy.
func TestEnergyEndsWhatAnInstructionReads(t *testing.T) {
	const forever = "for (let i = 0; i < 100000000; i++) "
	_, _, plain := spentUnder(t, "let x = 0; "+forever+"{ x = x + i; }", requestEnergy, 10*time.Second)
	name := "v" + strings.Repeat("x", 1<<20)
	// a program's variables are a map Go reads without hashing while it holds eight keys or fewer: past them, a
	// name is hashed whole
	many := ""
	for j := range 12 {
		many += "let p" + strconv.Itoa(j) + " = 0; "
	}
	keys := `const k = "k".repeat(20000000); const o = {}; for (let j = 0; j < 20; j++) { o["a" + j] = j; } o[k] = 1; `
	for name, source := range map[string]string{
		"two texts the same":               `const a = "x".repeat(20000000); const b = "x".repeat(20000000); ` + forever + `{ a === b; }`,
		"two texts not the same":           `const a = "x".repeat(20000000); const b = "x".repeat(20000000); ` + forever + `{ a !== b; }`,
		"two texts ordered":                `const a = "x".repeat(20000000); const b = "x".repeat(20000000); ` + forever + `{ a < b; }`,
		"two texts of Vietnamese ordered":  `const a = "ệ".repeat(3000000); const b = "ệ".repeat(3000000); ` + forever + `{ a >= b; }`,
		"a text equal to a number":         `const s = " ".repeat(20000000) + "1"; ` + forever + `{ s == 1; }`,
		"a text ordered with a number":     `const s = " ".repeat(20000000) + "1"; ` + forever + `{ s < 2; }`,
		"a text less a number":             `const s = "1".repeat(20000000); ` + forever + `{ s - 0; }`,
		"a text times a number":            `const s = " ".repeat(20000000) + "1"; ` + forever + `{ s * 1; }`,
		"a text incremented":               `const s = " ".repeat(20000000) + "1"; ` + forever + `{ let t = s; t++; }`,
		"a text negated":                   `const s = " ".repeat(20000000) + "1"; ` + forever + `{ -s; }`,
		"an array's remainder":             `const a = Array.from({ length: 1000000 }, (_, j) => j); ` + forever + `{ a % 2; }`,
		"a number over an array":           `const a = Array.from({ length: 1000000 }, (_, j) => j); ` + forever + `{ 1 / a; }`,
		"an array negated":                 `const a = Array.from({ length: 1000000 }, (_, j) => j); ` + forever + `{ -a; }`,
		"an object's text equal to a text": `const big = "x".repeat(20000000); const o = { toString: () => big }; const b = "x".repeat(20000000); ` + forever + `{ o == b; }`,
		"an object's text ordered":         `const big = "x".repeat(20000000); const o = { valueOf: () => big }; const b = "x".repeat(20000000); ` + forever + `{ o < b; }`,
		"a long key read":                  keys + forever + `{ o[k]; }`,
		"a long key written":               keys + forever + `{ o[k] = i; }`,
		"a key made text":                  keys + `const key = { toString: () => k }; ` + forever + `{ o[key]; }`,
		"a long key of a text":             `const k = "k".repeat(20000000); const s = "x"; ` + forever + `{ s[k]; }`,
		"a long key of an array":           `const k = "k".repeat(20000000); const a = [1]; ` + forever + `{ a[k]; }`,
		"a long name read":                 many + `let ` + name + ` = 1; ` + name + ` = 2; let x = 0; ` + forever + `{ x = ` + name + `; }`,
		"a long name written":              `let ` + name + ` = 0; ` + forever + `{ ` + name + ` = i; }`,
		"a long method's name":             `const o = {}; for (let j = 0; j < 9; j++) { o["a" + j] = j; } o["` + name + `"] = () => 1; ` + forever + `{ o.` + name + `(); }`,
		"a spread":                         `const o = {}; for (let j = 0; j < 20000; j++) { o["k" + j] = j; } ` + forever + `{ const p = { ...o, ...o, ...o, ...o }; }`,
		"lengths of two texts in turn":     `const a = "ệ".repeat(3000000); const b = "ệ".repeat(3000000); ` + forever + `{ a.length + b.length; }`,
		"units of two texts in turn":       `const a = "ệ".repeat(3000000); const b = "ệ".repeat(3000000); ` + forever + `{ a[2000000] + b[2000000]; }`,
		"a for…of beside another text":     `const a = "ệ".repeat(1000000); const b = "ệ".repeat(1000000); ` + forever + `{ for (const c of a) { b.length; } }`,
	} {
		t.Run(name, func(t *testing.T) {
			message, spent, took := spentUnder(t, source, requestEnergy, 5*time.Second)
			if !strings.Contains(message, "Energy Limit Exceeded") {
				t.Fatalf("ended %q after %v having spent %d units, want its energy spent", message, took, spent)
			}
			t.Logf("%v for its energy, %.1f times a loop of additions (%v)", took, float64(took)/float64(plain), plain)
			if !underRace && testing.CoverMode() == "" && took > 25*plain+100*time.Millisecond {
				t.Errorf("took %v for its energy, %.0f times a loop of additions (%v): what it reads is charged too little", took, float64(took)/float64(plain), plain)
			}
		})
	}
}

// What an instruction reads below 64 bytes is its price (value.LongText): a loop of comparisons, a text made a
// number, keys looked up and written, lengths of two texts in turn over texts of 63 bytes spends what it spends over
// texts of one byte; over texts of 64 it spends more.
func TestAShortTextCostsItsInstruction(t *testing.T) {
	loop := func(n int) uint64 {
		t.Helper()
		x := strings.Repeat("x", n-1)
		v := strings.Repeat("ệ", n/3) + strings.Repeat("x", n%3)
		_, spent, _ := spentUnder(t, `const a = "`+x+`" + "1"; const b = "`+x+`" + "2"; const o = {}; let x = 0;
const v = "`+v+`"; const w = ("`+v+`" + "y").slice(0, -1);
for (let i = 0; i < 1000; i++) {
  x = (a === b) + (a < b) + (a == b) + (a - 0) + (a == 1) + a.length + b.length;
  x = (v < w) + (v === w) + v.length + w.length + v[i % 7] + w[i % 7];
  o[a] = i; x = x + o[a];
}`, 0, time.Minute)
		return spent
	}
	one, short, long := loop(1), loop(63), loop(64)
	if short != one {
		t.Fatalf("texts of 63 bytes spent %d units, of one byte %d", short, one)
	}
	if long <= short {
		t.Fatalf("texts of 64 bytes spent %d units, of 63 %d", long, short)
	}
}

// A spread of an object of eight keys or fewer, an object without its index map, is its instruction's price; one
// of more pays for each key past the eighth (value.SpreadWork).
func TestASmallSpreadCostsItsInstruction(t *testing.T) {
	spread := func(keys int) uint64 {
		t.Helper()
		_, spent, _ := spentUnder(t, `const o = {}; for (let j = 0; j < `+strconv.Itoa(keys)+`; j++) { o["k" + j] = j; }
let n = 0; for (let i = 0; i < 1000; i++) { const p = { ...o }; n = n + 1; }`, 0, time.Minute)
		return spent
	}
	if one, eight := spread(1), spread(8); eight-one != 7*(spread(2)-spread(1)) {
		t.Fatalf("a spread of eight keys spent %d units, of one %d: more than its keys' making", eight, one)
	}
	if nine, eight := spread(9), spread(8); nine-eight <= spread(8)-spread(7) {
		t.Fatalf("a spread of nine keys spent %d units, of eight %d: the ninth key costs nothing", nine, eight)
	}
}

// What the instructions give is what they gave: a comparison, a text or an array made a number, a long key, a
// spread, a text's length and units, read as before their work was counted. node 22's answers.
func TestWhatAnInstructionReadsGivesWhatItGave(t *testing.T) {
	program := load(t, `const v = "ệ".repeat(100); const x = "x".repeat(100); const k = "k".repeat(100);
const log = [];
const o = { valueOf: () => { log.push("o"); return 1; } };
const a = [{ toString: () => { log.push("a"); return "2"; } }];
const r = o - a;
const big = {}; for (let j = 0; j < 20; j++) { big["k" + j] = j; } big[k] = "long";
const spread = { ...big };
const ordered = {}; ordered["10"] = 1; ordered["b"] = 2; ordered["2"] = 3;
let walked = 0; for (const c of Object.keys(ordered)) { walked++; }
let t = [4]; t++;
let s = "  41"; s++;
const result = [
  v + "a" < v + "b", v + "b" < v + "a", v + "a" <= v + "a", v + "😀" < v + "", v + "" > v + "😀",
  x + "a" < x + "b", x === x + "", x + "a" !== x + "b", x == x + "",
  " ".repeat(100) + "7" == 7, " ".repeat(100) + "7" - 2, " ".repeat(100) + "7" < 8, "1".repeat(100) > 1e98,
  [5] - 1, [1, 2] * 1, -[3], t, s, r, log, big[k], spread[k], Object.keys(spread).length, walked,
  Object.keys(ordered), v.length, v[99] === "ệ", x[99]
];`)
	program.run(t)
	want := `array [true,false,true,false,true,true,true,true,true,true,5,true,true,4,null,-3,5,42,-1,["o","a"],"long","long",21,3,["2","10","b"],100,true,"x"]`
	if got := describe(program.variable("result")); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// What an instruction reads is counted from sizes, never from a clock: the same run spends the same energy each
// time, its text cursor reset between runs.
func TestAnInstructionSpendsTheSameEnergyEachRun(t *testing.T) {
	source := `const v = "ệ".repeat(5000); const w = "ệ".repeat(5000); const x = "x".repeat(10000); const k = "k".repeat(1000);
const o = {}; for (let j = 0; j < 100; j++) { o["k" + j] = j; } o[k] = 1;
let n = 0; for (let i = 0; i < 20; i++) {
  n = n + (v < w) + (x === x + "") + (x == x.slice(1) + "x") + (" ".repeat(500) + i - 0) + o[k] + v.length + w.length + v[i * 100] + w[i * 100];
  const p = { ...o };
  o["" + (100 - i)] = i;
  for (const y of o) { break; }
}`
	_, first, _ := spentUnder(t, source, 0, time.Minute)
	for range 3 {
		if _, again, _ := spentUnder(t, source, 0, time.Minute); again != first {
			t.Fatalf("spent %d units, then %d", first, again)
		}
	}
}
