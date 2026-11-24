package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// An attribute of a tag of Kitwork that no tag of .ui reads is the page's error at its place (15/11/2026, the owner,
// round 3 of the 13/11 review), saying the word it and the next make when a space cut one, else the nearest word:
// it was dropped with one line of the log, so placeh older drew a field without its placeholder in silence. A word
// of .ui engine2 does not draw on that tag is still a note; an attribute of a piece of the site is its input.
func TestAnAttributeNoTagReadsIsThePagesError(t *testing.T) {
	for page, want := range map[string]string{
		`<content><input type="text" placeh older /></content>`:   `content.ui:1:29: thuộc tính placeh không có trong .ui: có phải placeholder, viết liền? (placeh older tách bởi một khoảng trắng)`,
		`<content><box paddng="2"><text>x</text></box></content>`: `content.ui:1:15: thuộc tính paddng không có trong .ui: có phải padding?`,
		`<content><text colour="red">x</text></content>`:          `thuộc tính colour không có trong .ui: có phải color?`,
		`<content><box xyzzy="1"><text>x</text></box></content>`:  `thuộc tính xyzzy không có trong .ui`,
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": page})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want the error %q", page, err, want)
		}
	}
	if err := renderError(t, map[string]string{
		"/app/site/shop-card.ui": `<shop-card><text>{{ title }}</text></shop-card>`,
		"/app/site/content.ui":   `<content><shop-card title="Một" ribbon="new" /></content>`,
	}); err != nil {
		t.Errorf("a piece's inputs: %v, want them its own", err)
	}
	site := memorySite(map[string]string{"/app/site/content.ui": `<content><box duration="5s"><text>x</text></box></content>`})
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, notes, err := site.Document(page, value.Value{}, ui.Head{}, kitDesign(t)); err != nil || !strings.Contains(strings.Join(notes.Unread, " "), "duration") {
		t.Errorf("a word of .ui not drawn on a box: %v, unread %v; want it a note", err, notes.Unread)
	}
}
