package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// A page reads a name value as any other (15/11/2026): an expression read its program's own variable value, so the
// part of the page that held {{ value }} was kept as reading nothing, and the next request showed the first one's
// value, another visitor's; a page not given value printed nothing. Each request now shows its own value, and a page
// not given value says so. CONTROL: the expression's variable named value again, both red.
func TestAPartReadsAPageNamedValue(t *testing.T) {
	files := map[string]string{"/app/site/content.ui": `<content><box padding="2"><text>[{{ value }}]</text></box></content>`}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("value", value.Number(5))},
		{path: "", data: object("value", value.Number(7))},
	})
	site := memorySite(map[string]string{"/app/site/content.ui": `<content><text>[{{ value }}]</text></content>`})
	page, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := site.Document(page, object("other", value.Number(1)), ui.Head{}, kitDesign(t)); err == nil || !strings.Contains(err.Error(), "trang không được đưa tên value") {
		t.Errorf("a page not given value: %v", err)
	}
}
