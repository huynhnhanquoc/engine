package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// The longest string is JavaScript's, 2^29 - 24 (counted in bytes), and every way a program builds a
// string refuses to pass it with RANGE "invalid string length" (05/11/2026): only repeat and padStart
// did, so a request doubled a string by + to a gigabyte for a few hundred instructions of energy.
// replace and replaceAll count the result before building it; these cases allocate a few megabytes.
func TestAReplaceThatWouldPassTheLongestStringIsRefused(t *testing.T) {
	text := value.String(strings.Repeat("x", 1<<20))
	replacement := value.String(strings.Repeat("y", 1<<10))
	for name, got := range map[string]value.Value{
		"replaceAll":         text.ReplaceAll(value.String("x"), replacement),
		"replaceAll with $&": text.ReplaceAll(value.String("x"), value.String("$&"+strings.Repeat("y", 1<<10))),
	} {
		if got.Code() != value.RangeCode || got.Message() != "invalid string length" {
			t.Errorf("%s of a megabyte by a kilobyte: %s, want RANGE: invalid string length", name, got.Failure())
		}
	}
	if got := text.Replace(value.String("x"), replacement); !got.IsString() || len(got.Text()) != 1<<20+1<<10-1 {
		t.Errorf("one replacement is within the longest string: %s", got.Failure())
	}
}

// The count replace makes first agrees with what it builds: every $ pattern as node 22 expands it.
func TestAReplacementsPatternsAreCountedAsTheyAreWritten(t *testing.T) {
	for _, c := range []struct{ got, want value.Value }{
		{value.String("abcabc").Replace(value.String("b"), value.String("[$`|$&|$'|$$|$x]")), value.String("a[a|b|cabc|$|$x]cabc")},
		{value.String("abc").ReplaceAll(value.String("b"), value.String("$'$`")), value.String("acac")},
		{value.String("aaa").ReplaceAll(value.String("a"), value.String("$&$&")), value.String("aaaaaa")},
	} {
		if !c.got.Equal(c.want) {
			t.Errorf("got %s, want %s", c.got.Failure()+c.got.Text(), c.want.Text())
		}
	}
}
