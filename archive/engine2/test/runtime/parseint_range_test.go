package runtime_test

import (
	"strings"
	"testing"
)

// parseInt past 2^53 in a base other than 10 and the powers of 2 is the RANGE error (19/11/2026, docs/language.md mục
// 4.3): JavaScript calls that result an implementation-approximated integer, and V8's last digits were not Kitwork's
// (parseInt("9007199254740993", 36) was 1.989698611603181e+24, node's 1.9896986116031807e+24). Below 2^53, in base 10
// and in the powers of 2, parseInt reads every digit exactly, as JavaScript does, and Number.parseInt is parseInt.
//
// CONTROL (19/11/2026): with ParseIntValue giving ParseInt's number, every refused case read a number.
func TestParseIntPastTwoToTheFiftyThreeInAnOddBaseIsTheRangeError(t *testing.T) {
	program := load(t, `const read = (f) => { const r = safe(f); return r.error === null ? r.value : r.error.code; };
result = [read(() => parseInt("9007199254740993", 36)), read(() => Number.parseInt("zzzzzzzzzzzzzzzzzzzz", 36)),
  read(() => parseInt("-" + "1".repeat(60), 3)), read(() => parseInt("zz", 36)), read(() => parseInt("2gosa7pa2gv", 36)),
  read(() => parseInt("ffffffffffffffffffff", 16)), read(() => parseInt("1".repeat(70), 2)), read(() => parseInt("123456789012345678901234567890"))];`)
	program.run(t)
	got := describe(program.variable("result"))
	if !strings.HasPrefix(got, `array ["RANGE","RANGE","RANGE",1295,9007199254740991,1.2089258196146292e+24,1.1805916207174113e+21,1.2345678901234568e+29]`) {
		t.Errorf("got %s", got)
	}
}
