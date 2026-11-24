package value_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A replacement with $& or $$ costs no allocation a match (05/11/2026): every match becomes text of one
// length, so strings.Count counts the result before it is built, and each match is written into the
// result, not made a string of its own. The walk that counted the result first, then built a string for
// each match, made 'abcabc'.repeat(1000).replaceAll('b', '[$&]') 2,004 allocations, and 10% slower than
// before the count was added.
func TestAReplacementsDollarPatternsCostNoAllocationAMatch(t *testing.T) {
	text := value.String(strings.Repeat("abcabc", 1000))
	search := value.String("b")
	for _, replacement := range []string{"[$&]", "$$", "<$&|$$>"} {
		pattern := value.String(replacement)
		allocations := testing.AllocsPerRun(20, func() {
			sinkValue = text.ReplaceAll(search, pattern)
		})
		if allocations > 4 {
			t.Errorf("replaceAll(\"b\", %q) of 2,000 matches allocates %.0f times, want a few: none a match", replacement, allocations)
		}
	}
	if got := text.ReplaceAll(search, value.String("<$&|$$>")).Text(); got != strings.Repeat("a<b|$>ca<b|$>c", 1000) {
		t.Errorf("replaceAll with $& and $$: %.40q…", got)
	}
}
