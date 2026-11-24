package runtime_test

import (
	"testing"
)

// replace and replaceAll with a function call it for each match, as JavaScript does: with the match, its
// offset in units and the whole text, its result made text, and its failure (or its result's) the call's.
// Until 05/11/2026 the function was made text, so "function" replaced each match: 'abc'.replace('b',
// (m) => m + m) was "afunctionc". The answers are node 22's for the same program.
func TestAFunctionReplacerIsCalledForEachMatch(t *testing.T) {
	program := load(t, `const log = [];
const result = [
  'abc'.replace('b', (m) => m + m),
  'abcb'.replaceAll('b', () => 'X'),
  'a-b-c'.replaceAll('-', (m, o, s) => '[' + m + o + s + ']'),
  'xyz'.replace('q', () => 'never'),
  'abab'.replace('b', (m, o) => { log.push(o); return o }),
  'abc'.replaceAll('', (m, o) => '<' + o + '>'),
  'abc'.replace('', (m, o) => '<' + o + '>'),
  '😀a😀'.replaceAll('a', (m, o) => o),
  '😀a'.replaceAll('', () => '|').length,
  'ạb'.replaceAll('b', (m, o) => o),
  'abc'.replace('b', () => ({ toString: () => 'T' })),
  'abc'.replace('b', () => [1, 2]),
  'abc'.replace('b', () => null),
  'abc'.replace('b', Math.max),
  'abc'.replace({ toString: () => 'b' }, (m) => m.toUpperCase()),
  safe(() => 'abc'.replace('b', () => error('inner'))).error === null,
  safe(() => 'abc'.replace('b', () => ({ toString: () => error('ts') }))).error === null,
  safe(() => 'abc'.replace('b', () => ({ toString: () => ({}) }))).error === null,
  'x\uDE00'.replace('x', () => '\uD83D') === '😀',
  log
];`)
	program.run(t)
	got := describe(program.variable("result"))
	want := `array ["abbc","aXcX","a[-1a-b-c]b[-3a-b-c]c","xyz","a1ab","<0>a<1>b<2>c<3>","<0>abc","😀2😀",7,"ạ1","aTc","a1,2c","anullc","aNaNc","aBc",false,false,false,true,[1]]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// A function a frozen boot made, given to replace at a request, runs on the request's VM, as any function
// a request hands a method does (a stand-in): the boot's VM is shared and runs nothing more.
func TestAFrozenRunsFunctionReplacesAtARequest(t *testing.T) {
	boot := booted(t, `const upper = (m) => m.toUpperCase() + "!";
const route = () => ["abcb".replaceAll("b", upper), "abc".replace("c", upper)];
`)
	boot.vm.Freeze()
	machine := boot.request(nil)
	if got := describe(boot.call(machine, "route")); got != `array ["aB!cB!","abC!"]` {
		t.Errorf("got %s, want the array [\"aB!cB!\",\"abC!\"]", got)
	}
}

// replace, replaceAll and split match a lone half of a pair inside a pair too, as JavaScript matches unit
// by unit (05/11/2026: they searched the bytes, where a pair is one four-byte character, and found nothing),
// and a half a replacement brings beside the other half is the character again. node 22's answers.
func TestAHalfOfAPairIsMatchedInsideThePair(t *testing.T) {
	program := load(t, `const result = [
  '😀'.replaceAll('\uD83D', 'X').charCodeAt(0),
  '😀'.split('\uD83D').length,
  'a😀b😀c'.split('\uDE00'),
  'a😀b😀c'.split('\uDE00', 2).length,
  '😀'.replace('\uDE00', 'X').length,
  '😀😀'.replaceAll('\uDE00\uD83D', '-').length,
  '😀'.replace('\uD83D', '$&$&').length,
  '😀x'.replaceAll('\uDE00', (m, o, s) => o + ':' + m.length).length,
  '\uD83Dx\uDE00'.replace('x', '') === '😀',
  'x\uDE00'.replace('x', '\uD83D') === '😀',
  '한국'.split('국').length
];`)
	program.run(t)
	got := describe(program.variable("result"))
	want := `array [88,2,["a\ud83d","b\ud83d","c"],2,2,3,3,5,true,true,2]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
