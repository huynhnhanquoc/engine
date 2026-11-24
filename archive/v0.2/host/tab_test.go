package host

import (
	"fmt"
	"slices"
	"sort"
	"testing"
)

// An app remembers its tabs in the order they were last used: past what it remembers, the one least lately used goes,
// whatever the clock reads. Fifty tabs kept as fast as a page is drawn (many within one tick of a clock that moves by
// half a millisecond on Windows) leave the last ten, and a tab used again stays over those kept after it.
//
// CONTROL (11/11/2026): with used read off time.Now(), tabs kept within one tick were alike, and the tabs left were
// others than the last ten.
func TestAnAppForgetsTheTabLeastLatelyUsed(t *testing.T) {
	a := newAppTabs(&Host{StreamLimit: 1}) // ten tabs for the one channel it may hold
	for i := range 50 {
		a.keep(fmt.Sprintf("tab%02d", i), nil, nil)
	}
	var left []string
	for id := range a.tabs {
		left = append(left, id)
	}
	sort.Strings(left)
	want := []string{"tab40", "tab41", "tab42", "tab43", "tab44", "tab45", "tab46", "tab47", "tab48", "tab49"}
	if !slices.Equal(left, want) {
		t.Fatalf("tabs left %v, want %v", left, want)
	}
	a.keep("tab40", nil, nil) // used again: now the latest
	a.keep("tab50", nil, nil)
	if a.tabs["tab40"] == nil || a.tabs["tab41"] != nil {
		t.Errorf("after tab40 was used again and tab50 kept: tab40 %v, tab41 %v; want tab41 gone", a.tabs["tab40"] != nil, a.tabs["tab41"] != nil)
	}
}
