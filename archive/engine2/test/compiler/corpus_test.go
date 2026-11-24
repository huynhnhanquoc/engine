// Package compiler_test holds core/compiler and core/runtime to JavaScript from outside, over the
// corpus the first engine kept (engine/compiler/javascript_corpus_test.go, 27/10/2026).
package compiler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The JavaScript corpus: testdata/parity_corpus.json holds ~4000 expressions over every kind of value,
// every operator and every member a program reaches, with a scope of globals; testdata/answers.json
// holds what node answers for each, run as `const result = (src);` in a strict function at UTC+7
// (engine/compiler/testdata/javascript_corpus/generate.js). This runs every case on core/compiler and
// core/runtime the same way and puts it in one class:
//
//   - match: the VM answers as JavaScript does (both reject it, at compile or run time, included);
//   - compile: the compiler rejects it where JavaScript gives a value, which the rule allows ("nghĩa
//     của JS, hoặc lỗi biên dịch"), whether the subset keeps it out for good or not yet;
//   - differ: a value JavaScript does not give, a run-time failure where JavaScript gives a value, or a
//     value where JavaScript throws;
//   - unstable: two runs of the same program on fresh VMs answer differently (an order taken from a Go
//     map), counted apart from differ;
//   - panic: a Go panic in the compiler or the VM, recovered here, or one the runtime's host boundary
//     turned into a NATIVE_PANIC failure: an engine fault is never JavaScript's answer.
//
// It is a ratchet: testdata/ratchet.json records every case's class, and the test fails when a case
// moves down (a match that no longer matches, anything that now panics) or when the record is behind
// the engine (a case moved up, a new case, a case gone). KITWORK_CORPUS=update rewrites the record
// after the moves down have been looked at; it is the only way the record changes. KITWORK_CORPUS=all
// logs every case that is not a match, with the failure or the compile error behind it.
//
// The test sets time.Local to UTC+7 while it runs, as generate.js sets TZ for node: not UTC, so a case
// that reads local time is held to JavaScript there too, the same on every machine.

const (
	classMatch    = "match"
	classCompile  = "compile"
	classDiffer   = "differ"
	classUnstable = "unstable"
	classPanic    = "panic"
)

// corpusShown is how many differences a plain run logs.
const corpusShown = 60

// corpusRuns is how many times a case with more than one part runs. An order taken from a small Go map
// (8 keys or fewer: one of 8 rotations) repeats on the next run one time in 8, so 8 runs all alike happen
// about once in two million.
const corpusRuns = 8

type corpusCase struct {
	Group string          `json:"group"`
	Src   string          `json:"src"`
	Scope json.RawMessage `json:"scope"`
}

// outcome is one run of one case: got is its description as generate.js writes JavaScript's
// ("throws" for a rejection), detail what lies behind a rejection (the compile error, the failure's
// code and text, the panic), for reading a difference.
type outcome struct {
	class, got, detail string
}

func TestJavaScriptCorpus(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+7", 7*60*60)
	t.Cleanup(func() { time.Local = local })

	var corpus struct {
		Scope json.RawMessage `json:"scope"`
		Cases []corpusCase    `json:"cases"`
	}
	readCorpusJSON(t, filepath.Join("testdata", "parity_corpus.json"), &corpus)
	answers := map[string]string{}
	readCorpusJSON(t, filepath.Join("testdata", "answers.json"), &answers)
	scope := decodeScope(t, "scope", corpus.Scope)

	all := os.Getenv("KITWORK_CORPUS") == "all"
	update := os.Getenv("KITWORK_CORPUS") == "update"
	ratchetPath := filepath.Join("testdata", "ratchet.json")
	recorded := map[string]string{}
	readCorpusJSON(t, ratchetPath, &recorded)
	classes := map[string]string{}
	var down, up []string
	count := map[string]int{}
	unanswered := 0
	var differences, panics []string
	seen := map[string]int{}
	for _, c := range corpus.Cases {
		key := corpusKey(seen, c)
		js, ok := answers[key]
		if !ok {
			unanswered++
			continue
		}
		var own ordered
		if len(c.Scope) > 0 {
			own = decodeScope(t, key, c.Scope)
		}
		result := classifyCase(scope, own, c, js)
		count[result.class]++
		classes[key] = result.class

		line := fmt.Sprintf("%s: Kitwork %s, JavaScript %s", key, clipCorpus(result.got), clipCorpus(js))
		if result.detail != "" {
			line += " — " + clipCorpus(result.detail)
		}
		switch result.class {
		case classDiffer, classUnstable:
			differences = append(differences, line)
		case classPanic:
			panics = append(panics, line)
		}
		if all && result.class != classMatch {
			t.Logf("[%s] %s", result.class, line)
		}
		switch was, ok := recorded[key]; {
		case !ok:
			up = append(up, fmt.Sprintf("%s (new, %s)", line, result.class))
		case classRank[result.class] < classRank[was]:
			down = append(down, fmt.Sprintf("%s (was %s, now %s)", line, was, result.class))
		case classRank[result.class] > classRank[was]:
			up = append(up, fmt.Sprintf("%s (was %s, now %s)", line, was, result.class))
		}
	}
	answered := 0
	for _, n := range count {
		answered += n
	}
	if answered == 0 {
		t.Fatalf("no case of the corpus ran: %d cases, %d without an answer", len(corpus.Cases), unanswered)
	}

	t.Logf("JavaScript corpus: %d match, %d compile, %d differ, %d unstable, %d panic, of %d answered; %d without an answer",
		count[classMatch], count[classCompile], count[classDiffer], count[classUnstable], count[classPanic], answered, unanswered)
	for _, line := range panics {
		t.Logf("panic: %s", line)
	}
	for i, line := range differences {
		if i == corpusShown {
			t.Logf("… and %d more differences", len(differences)-corpusShown)
			break
		}
		t.Logf("differs: %s", line)
	}

	gone := 0
	for key := range recorded {
		if _, ok := classes[key]; !ok {
			gone++
		}
	}
	if update {
		for _, line := range down {
			t.Logf("moved down: %s", line)
		}
		writeRatchet(t, ratchetPath, classes)
		t.Logf("%s rewritten: %d moved down, %d moved up or new, %d gone", ratchetPath, len(down), len(up), gone)
		return
	}
	for i, line := range down {
		if i == corpusShown {
			t.Errorf("… and %d more moved down", len(down)-corpusShown)
			break
		}
		t.Errorf("moved down: %s", line)
	}
	if len(up) > 0 || gone > 0 {
		for i, line := range up {
			if i == 10 {
				t.Logf("… and %d more moved up", len(up)-10)
				break
			}
			t.Logf("moved up: %s", line)
		}
		t.Errorf("%s is behind the engine: %d cases moved up or are new, %d are gone; record them with KITWORK_CORPUS=update", ratchetPath, len(up), gone)
	}
}

// classRank orders the classes for the ratchet: a match is the top, a panic the bottom, and a compile
// error, a difference and an unstable answer are all "not JavaScript's answer yet".
var classRank = map[string]int{classMatch: 2, classDiffer: 1, classCompile: 1, classUnstable: 1, classPanic: 0}

// writeRatchet records every case's class, keys sorted, one per line, so a change reads as a diff.
func writeRatchet(t *testing.T, path string, classes map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(classes))
	for key := range classes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for i, key := range keys {
		name, _ := json.Marshal(key)
		fmt.Fprintf(&b, " %s: %q", name, classes[key])
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corpusKey names a case as generate.js does: group and source, and #2, #3… for a later case with both
// alike (the corpus runs one source over scopes of different sizes).
func corpusKey(seen map[string]int, c corpusCase) string {
	base := c.Group + " | " + c.Src
	seen[base]++
	if n := seen[base]; n > 1 {
		return fmt.Sprintf("%s #%d", base, n)
	}
	return base
}

// classifyCase compiles one case as `const result = (src);` and puts it in its class against js,
// JavaScript's answer. A case that answers with more than one part (a comma in it, where an order can
// change) runs corpusRuns times on fresh VMs, and two different answers make it unstable.
func classifyCase(scope, own ordered, c corpusCase, js string) outcome {
	program, fault := compileCase(c.Src)
	if fault != "" {
		return outcome{class: classPanic, got: "panic", detail: "compile: " + fault}
	}
	if program.err != nil {
		if js == "throws" {
			return outcome{class: classMatch, got: "throws", detail: "compile: " + program.err.Error()}
		}
		return outcome{class: classCompile, got: "throws", detail: "compile: " + program.err.Error()}
	}
	first := runCase(program.bytecode, scope, own)
	if first.class == classPanic {
		return first
	}
	if strings.Contains(first.got, ",") {
		for run := 1; run < corpusRuns; run++ {
			if again := runCase(program.bytecode, scope, own); again.got != first.got {
				return outcome{class: classUnstable, got: first.got + " / " + again.got}
			}
		}
	}
	if first.got == js {
		first.class = classMatch
	} else {
		first.class = classDiffer
	}
	return first
}

type compiled struct {
	bytecode *compiler.Bytecode
	err      error
}

// compileCase compiles one case; fault is a Go panic of the compiler, recovered.
func compileCase(src string) (program compiled, fault string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fault = fmt.Sprint(recovered)
		}
	}()
	bytecode, err := compiler.CompileSource("const result = (" + strings.TrimSpace(src) + ");")
	return compiled{bytecode: bytecode, err: err}, ""
}

// runCase runs a compiled case on a fresh VM whose globals are the language's builtins and the
// corpus's scope, the case's own over it, and describes result as generate.js does; a run-time
// failure is "throws". Builtins (slot 0 is the app, kitwork) stays empty: the corpus never names it.
func runCase(bytecode *compiler.Bytecode, scope, own ordered) (result outcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = outcome{class: classPanic, got: "panic", detail: fmt.Sprint("run: ", recovered)}
		}
	}()
	vm := runtime.New(bytecode.Program)
	vm.MaxEnergy = 10_000_000
	builtins.Install(vm.Globals)
	for i, name := range scope.keys {
		vm.Globals[name] = scope.items[i].build()
	}
	for i, name := range own.keys {
		vm.Globals[name] = own.items[i].build()
	}
	returned := vm.Run()
	failure := returned
	if !failure.IsInvalid() {
		failure = vm.Vars["result"]
	}
	if failure.IsInvalid() {
		if strings.Contains(failure.Message(), "Native panic") {
			return outcome{class: classPanic, got: "panic", detail: failure.Failure()}
		}
		return outcome{got: "throws", detail: failure.Failure()}
	}
	return outcome{got: describe(vm.Vars["result"], 0)}
}

// describe writes a value in the corpus's canonical form (describeParity of the first engine's
// jit/hydrate, and __describe of generate.js): undefined and null by name, a number as JavaScript writes
// it with -0 and NaN by name, a string as JSON.stringify quotes it, a list its items, an object its keys
// in its own order, a function fn: (a lambda keeps no source, JavaScript's has it), anything else
// <kind>.
func describe(v value.Value, depth int) string {
	if depth > 12 {
		return "<deep>"
	}
	switch {
	case v.IsUndefined():
		return "undefined"
	case v.IsNull():
		return "null"
	case v.IsBoolean():
		return strconv.FormatBool(v.Boolean())
	case v.IsNumber():
		n := v.Number()
		switch {
		case n != n:
			return "NaN"
		case n == 0 && math.Signbit(n):
			return "-0"
		}
		return v.ToString().Text()
	case v.IsString():
		return quoteCorpus(v.Text())
	case v.IsArray():
		items := v.Items()
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = describe(item, depth+1)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case v.IsObject():
		keys := v.Keys()
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = quoteCorpus(k) + ":" + describe(v.Get(k), depth+1)
		}
		return "{" + strings.Join(parts, ",") + "}"
	case v.IsFunction():
		return "fn:"
	}
	return "<" + v.Kind() + ">"
}

// quoteCorpus is JSON.stringify of a string: the quote, the backslash and the control characters
// escaped, and a lone surrogate (kept in its three-byte form, core/value text.go) as \udxxx, nothing
// else.
func quoteCorpus(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		if i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80 {
			unit := 0xD000 | uint16(s[i+1]&0x3F)<<6 | uint16(s[i+2]&0x3F)
			fmt.Fprintf(&b, `\u%04x`, unit)
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// ordered is a JSON value as the file writes it, an object's keys in the file's order, so each run
// builds fresh values (a case may push into its scope) with the order JavaScript's JSON.parse gives.
type ordered struct {
	kind   byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' boolean, 'z' null
	keys   []string
	items  []ordered
	text   string
	number float64
	truth  bool
}

func (o ordered) build() value.Value {
	switch o.kind {
	case 'o':
		object := value.Object()
		for i, k := range o.keys {
			object.Set(k, o.items[i].build())
		}
		return object
	case 'a':
		items := make([]value.Value, len(o.items))
		for i, item := range o.items {
			items[i] = item.build()
		}
		return value.Array(items)
	case 's':
		return value.String(o.text)
	case 'n':
		return value.Number(o.number)
	case 'b':
		return value.Boolean(o.truth)
	}
	return value.Null()
}

// decodeScope reads a scope, a JSON object, by walking its tokens, so its keys keep their order.
func decodeScope(t *testing.T, name string, raw json.RawMessage) ordered {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	scope, err := decodeOrdered(decoder)
	if err == nil {
		if _, extra := decoder.Token(); extra != io.EOF {
			err = fmt.Errorf("text after the scope")
		}
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if scope.kind != 'o' {
		t.Fatalf("%s: the scope is not an object", name)
	}
	return scope
}

func decodeOrdered(decoder *json.Decoder) (ordered, error) {
	token, err := decoder.Token()
	if err != nil {
		return ordered{}, err
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			out := ordered{kind: 'o'}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return ordered{}, err
				}
				item, err := decodeOrdered(decoder)
				if err != nil {
					return ordered{}, err
				}
				out.keys = append(out.keys, key.(string))
				out.items = append(out.items, item)
			}
			_, err := decoder.Token() // }
			return out, err
		case '[':
			out := ordered{kind: 'a'}
			for decoder.More() {
				item, err := decodeOrdered(decoder)
				if err != nil {
					return ordered{}, err
				}
				out.items = append(out.items, item)
			}
			_, err := decoder.Token() // ]
			return out, err
		}
		return ordered{}, fmt.Errorf("unexpected %v", token)
	case string:
		return ordered{kind: 's', text: token}, nil
	case json.Number:
		n, err := strconv.ParseFloat(string(token), 64)
		if numberError, ok := err.(*strconv.NumError); ok && numberError.Err == strconv.ErrRange {
			err = nil // 1e400 is Infinity to JSON.parse, as ParseFloat rounds it
		}
		return ordered{kind: 'n', number: n}, err
	case bool:
		return ordered{kind: 'b', truth: token}, nil
	case nil:
		return ordered{kind: 'z'}, nil
	}
	return ordered{}, fmt.Errorf("unexpected token %v", token)
}

func clipCorpus(s string) string {
	if utf8.RuneCountInString(s) > 120 {
		return string([]rune(s)[:120]) + "…"
	}
	return s
}

func readCorpusJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
