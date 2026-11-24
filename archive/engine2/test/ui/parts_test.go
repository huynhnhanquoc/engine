package ui_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// A page drawn from what earlier pages kept (library/ui frozen.go, kept.go, 10–11/11/2026) is the page drawn whole: the
// pages below are drawn one after another by one site, whose parts, openings and their variants, plans, highlights
// and sizes the pages before kept, and each must be what a site that kept nothing draws for it, byte for byte and
// in what it says.

// keptCase is one page drawn: its address, its data, its head.
type keptCase struct {
	path string
	data value.Value
	head ui.Head
}

// drawCase draws c on site, its page assembled for the boot as the host draws it, and gives the document and what
// drawing it said, or its error.
func drawCase(t *testing.T, site *ui.Site, c keptCase) string {
	t.Helper()
	page, err := site.Assembled(route(c.path))
	if err != nil {
		return "error: " + err.Error()
	}
	document, notes, err := site.Document(page, c.data, c.head, kitDesign(t))
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("%s\nunread %v\nwarnings %v", document, notes.Unread, notes.Warnings)
}

// sameAsFresh draws cases in order on one site, and fails where a page differs from what a site of its own draws for
// it; every case draws.
func sameAsFresh(t *testing.T, files map[string]string, cases []keptCase) {
	t.Helper()
	kept := memorySite(files)
	for i, c := range cases {
		want := drawCase(t, memorySite(files), c)
		if strings.HasPrefix(want, "error: ") {
			t.Fatalf("page %d (%s) does not draw: %s", i, c.path, want)
		}
		if got := drawCase(t, kept, c); got != want {
			at := 0
			for at < len(got) && at < len(want) && got[at] == want[at] {
				at++
			}
			t.Fatalf("page %d (%s) drawn after the others differs at byte %d:\n%s\nwant\n%s", i, c.path, at,
				got[max(0, at-300):min(len(got), at+200)], want[max(0, at-300):min(len(want), at+200)])
		}
	}
}

// listed is the data of the list page: a title, a user, and an item for each name, ready when its place is even, its
// icon the name's.
func listed(title, user string, icons ...string) value.Value {
	items := make([]value.Value, len(icons))
	for i, icon := range icons {
		items[i] = object("id", value.Number(float64(i)), "name", value.String("Item "+icon), "ready", value.Boolean(i%2 == 0),
			"icon", value.String(icon), "url", value.String("/items/"+icon))
	}
	return object("title", value.String(title), "user", object("name", value.String(user)), "items", value.Array(items))
}

// listFiles are a page that reads its data in text, in a class (a badge's colour), in an icon and in a link's address,
// each row of a for.
var listFiles = map[string]string{
	"/app/site/content.ui": `<content gap="2"><text heading="1">{{ title }}</text><box padding="2"><text weight="bold">{{ user.name }}</text></box>` +
		`<list gap="1"><item for="item of items; key: item.id" flex="row" gap="2">` +
		`<badge color="{{ item.ready ? 'success' : 'ink-30' }}"><text>{{ item.ready ? 'Built' : 'Planned' }}</text></badge>` +
		`<icon value="{{ item.icon }}" /><link value="{{ item.url }}">{{ item.name }}</link></item></list></content>`,
}

// A part that reads the page's data is drawn with each page's data: its text, the class a value makes, the icon a
// value names and an address, in rows of a for. An opening kept for the values it read is written again only for
// those values; past the variants a place keeps (12 icons at one place), it is read at each page, and still right.
func TestAPartThatReadsDataIsDrawnWithEachPagesData(t *testing.T) {
	many := []string{"world", "server", "bolt", "code", "database", "cloud", "lock", "mail", "user", "home", "star", "heart"}
	sameAsFresh(t, listFiles, []keptCase{
		{path: "", data: listed("One", "An", "world", "server", "bolt")},
		{path: "", data: listed("Two", "Bình", "bolt", "world", "code")},
		{path: "", data: listed("One", "An", "world", "server", "bolt")},
		{path: "", data: listed("Many", "<b>Chi</b>", many...)},
		{path: "", data: listed("Two", "Bình", "bolt", "world", "code")},
		{path: "", data: listed("None", "Dũng")},
		{path: "", data: listed("Many", "Chi", many...)},
	})
}

// pathFiles are a frame whose header links to each page, and pages that link to each other.
var pathFiles = map[string]string{
	"/app/site/view.ui":          `<view><header /><content /></view>`,
	"/app/site/header.ui":        `<header flex="row" gap="2"><link value="/">Home</link><link value="/docs">Docs</link><link value="/about">About</link></header>`,
	"/app/site/content.ui":       `<content><text>Home</text><link value="/docs">Docs</link></content>`,
	"/app/site/docs/content.ui":  `<content><text>Docs</text><link value="/about">About</link><link value="/docs/a">A</link><link value="/docs">All</link></content>`,
	"/app/site/about/content.ui": `<content><text>About</text><link value="/docs">Docs</link></content>`,
}

// A link says the page it leads to is the one viewed (aria-current) at each address, the header's links kept for
// each; one page viewed at several addresses (a route with a parameter) marks its links again for each, its
// links' openings kept by address; a site that moves without a reload (Drive) gets its frame's bar and reader, and
// one page drawn with and without it is each.
func TestALinkFollowsTheAddressViewed(t *testing.T) {
	sameAsFresh(t, pathFiles, []keptCase{
		{path: "/docs", head: ui.Head{Path: "/docs"}},
		{path: "/docs", head: ui.Head{Path: "/docs/a"}},
		{path: "/docs", head: ui.Head{Path: "/docs/b"}},
		{path: "/about", head: ui.Head{Path: "/about"}},
		{path: "/docs", head: ui.Head{Path: "/docs"}},
		{path: "/docs", head: ui.Head{Path: "/docs/a"}},
		{path: "", head: ui.Head{Path: "/"}},
		{path: "/docs", head: ui.Head{Path: "/docs", Drive: true}},
		{path: "/about", head: ui.Head{Path: "/about", Drive: true}},
		{path: "/docs", head: ui.Head{Path: "/docs"}},
		{path: "/docs", head: ui.Head{Path: "/docs", Drive: true}},
	})
}

// An editor's state follows its value and the request's method (unsaved on a save), its plan kept for the boot; what
// reads its words inside it ({{ words }}) reads the state of an instance that starts from the request, never kept.
func TestAnEditorFollowsItsValue(t *testing.T) {
	page := strings.Replace(editorPage, `<textarea name="body"`, `<text>{{ words }} words, {{ characters }} characters</text><textarea name="body"`, 1)
	files := map[string]string{"/app/site/content.ui": page}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("post", value.String("")), head: ui.Head{Method: "POST"}},
		{path: "", data: object("post", value.String("một hai ba")), head: ui.Head{Method: "GET"}},
		{path: "", data: object("post", value.String("")), head: ui.Head{Method: "GET"}},
		{path: "", data: object("post", value.String("<p>một <b>hai</b> ba bốn năm</p>")), head: ui.Head{Method: "POST"}},
		{path: "", data: object("post", value.String("một hai ba")), head: ui.Head{Method: "GET"}},
	})
}

// An editor's rule holds the heights of every editor drawn before it: two editors an if chooses between are one
// count of editors, and the editor after them is drawn with the heights of the one drawn.
func TestAnEditorHoldsTheHeightsOfThoseBeforeIt(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><form method="post"><box if="{{ short }}"><editor><textarea name="a" alt="A" rows="5" /></editor></box>` +
			`<box if="{{ !short }}"><editor><textarea name="b" alt="B" rows="9" /></editor></box>` +
			`<editor><textarea name="c" alt="C" rows="3" /></editor></form></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("short", value.Boolean(true))},
		{path: "", data: object("short", value.Boolean(false))},
		{path: "", data: object("short", value.Boolean(true))},
	})
}

// The ids of fields and carousels count those drawn before them: a field and a carousel the page's data keeps or not
// move the ids of those after, and what a part kept for one count is never written for another. A carousel's slides
// from data, and a countdown, are kept by their own keys.
func TestIdsFollowWhatIsDrawnBeforeThem(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><box if="{{ first }}"><field><label>A</label><input name="a" /></field></box>` +
			`<field><label>B</label><input name="b" /><hint>h</hint></field>` +
			`<box if="{{ first }}"><carousel alt="One"><list><item>a</item><item>b</item></list></carousel></box>` +
			`<carousel alt="Two"><list><item>c</item><item for="x of xs">{{ x }}</item></list><button for="x, i of xs" click="go(i)">.</button></carousel>` +
			`<countdown as="$tea" duration="10s" finish="$tea.stop()"><text>Left: <time>{{ minutes }}:{{ seconds }}</time></text>` +
			`<button click="toggle()">{{ playing ? 'Pause' : 'Play' }}</button></countdown></content>`,
	}
	xs := func(names ...string) value.Value {
		list := make([]value.Value, len(names))
		for i, name := range names {
			list[i] = value.String(name)
		}
		return value.Array(list)
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("first", value.Boolean(true), "xs", xs("d", "e"))},
		{path: "", data: object("first", value.Boolean(false), "xs", xs("d"))},
		{path: "", data: object("first", value.Boolean(true), "xs", xs("d", "e", "f", "g"))},
		{path: "", data: object("first", value.Boolean(false), "xs", xs())},
		{path: "", data: object("first", value.Boolean(true), "xs", xs("d", "e"))},
	})
}

// What a page loads follows what it draws: a dropdown the data keeps loads its component, one it drops does not; a
// toast's notice is kept by its key, the toast drawn on every page.
func TestWhatAPageLoadsFollowsWhatItDraws(t *testing.T) {
	notice := `<card for="notice of notices; key: notice.id"><text show="notice.title">{{ notice.title }}</text>` +
		`<button click="dismiss(notice.id)"><icon value="x" alt="Dismiss" /></button></card>`
	files := map[string]string{
		"/app/site/content.ui": `<content><box if="{{ open }}"><dropdown as="$menu"><button click="toggle()">Menu</button><popover>p</popover></dropdown></box>` +
			`<toast as="$toast">` + notice + `</toast><text>{{ open ? 'open' : 'shut' }}</text></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("open", value.Boolean(false))},
		{path: "", data: object("open", value.Boolean(true))},
		{path: "", data: object("open", value.Boolean(false))},
		{path: "", data: object("open", value.Boolean(true))},
	})
}

// A piece is drawn from its file's own tree: a call's show goes on a copy of its root, never on the file's, so a call
// without one and the next page are as the file says; its text follows what each call passes.
func TestAPieceCallLeavesItsFileAsItIs(t *testing.T) {
	files := map[string]string{
		"/app/site/shop-card.ui": `<box gap="1"><text weight="bold">{{ title }}</text><text>{{ note }}</text></box>`,
		"/app/site/content.ui": `<content><shop-card show="{{ shown }}" title="{{ first }}" note="a" />` +
			`<shop-card title="Two" note="{{ first }}" /><shop-card for="x of xs" title="{{ x }}" note="b" /></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("shown", value.Boolean(true), "first", value.String("One"), "xs", value.Array([]value.Value{value.String("x")}))},
		{path: "", data: object("shown", value.Boolean(false), "first", value.String("Uno"), "xs", value.Array(nil))},
		{path: "", data: object("shown", value.Boolean(true), "first", value.String("One"), "xs", value.Array([]value.Value{value.String("y"), value.String("z")}))},
	})
}

// The tabs of a page are planned once for its boot: each page holds their tabs, panels and pre-paint as the first
// did, and what it reads of its data besides.
func TestTheTabsArePlannedOnceForTheBoot(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><tabs as="$shell" persist="true" selected="powershell"><button click="select('powershell')">p</button>` +
			`<button click="select('bash')">b</button></tabs><box show="$shell.selected == 'bash'"><text>{{ bash }}</text></box>` +
			`<box show="$shell.selected == 'powershell'"><text>{{ powershell }}</text></box><text>after</text></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("bash", value.String("ls"), "powershell", value.String("dir"))},
		{path: "", data: object("bash", value.String("cat"), "powershell", value.String("type"))},
		{path: "", data: object("bash", value.String("ls"), "powershell", value.String("dir"))},
	})
}

// The tabs of a region shared by pages are numbered on each page among that page's tabs: a page whose content holds
// tabs before the region's, and one whose content holds none, each number the region's as a site of its own does,
// the region's openings never written from another page's plan.
func TestTheTabsOfARegionAreNumberedForEachPage(t *testing.T) {
	tabs := func(alias string) string {
		return `<tabs as="$` + alias + `"><button click="select('a')">a</button><button click="select('b')">b</button></tabs>` +
			`<box show="$` + alias + `.selected == 'a'">a</box><box show="$` + alias + `.selected == 'b'">b</box>`
	}
	files := map[string]string{
		"/app/site/view.ui":          `<view><content /><footer /></view>`,
		"/app/site/footer.ui":        `<footer>` + tabs("shell") + `</footer>`,
		"/app/site/docs/content.ui":  `<content>` + tabs("files") + `</content>`,
		"/app/site/about/content.ui": `<content><text>About</text></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "/docs", head: ui.Head{Path: "/docs"}},
		{path: "/about", head: ui.Head{Path: "/about"}},
		{path: "/docs", head: ui.Head{Path: "/docs"}},
		{path: "/about", head: ui.Head{Path: "/about"}},
	})
}

// Code from data is coloured once for each text: two texts at one place, and the first again.
func TestCodeFromDataIsColouredForEachText(t *testing.T) {
	files := map[string]string{
		"/app/site/content.ui": `<content><code language="javascript">{{ source }}</code></content>`,
	}
	sameAsFresh(t, files, []keptCase{
		{path: "", data: object("source", value.String(`const a = "<b>" + 1;`))},
		{path: "", data: object("source", value.String(`let b = 'x' && true;`))},
		{path: "", data: object("source", value.String(`const a = "<b>" + 1;`))},
	})
}

// Pages drawn at once with different data by one site are each what a site of its own draws for its data: the
// variants of an opening kept by one request are read by the others while they are added.
func TestPagesDrawnAtOnceWithTheirOwnData(t *testing.T) {
	icons := [][]string{{"world", "server"}, {"bolt", "code", "lock"}, {"mail"}, {"user", "home", "star", "heart", "cloud"},
		{"world", "bolt", "mail", "user", "database", "lock", "code", "cloud", "star", "home"}}
	cases := make([]keptCase, len(icons))
	want := make([]string, len(icons))
	for i, names := range icons {
		cases[i] = keptCase{path: "", data: listed(fmt.Sprint("Page ", i), "An", names...)}
		want[i] = drawCase(t, memorySite(listFiles), cases[i])
	}
	site := memorySite(listFiles)
	var group sync.WaitGroup
	failed := make(chan string, 16)
	for worker := range 16 {
		group.Go(func() {
			for round := range 30 {
				i := (worker + round) % len(cases)
				if got := drawCase(t, site, cases[i]); got != want[i] {
					select {
					case failed <- fmt.Sprintf("page %d drawn at once with others:\n%.600s\nwant\n%.600s", i, got, want[i]):
					default:
					}
					return
				}
			}
		})
	}
	group.Wait()
	close(failed)
	for message := range failed {
		t.Fatal(message)
	}
}
