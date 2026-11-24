package ui_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// countedSite is a site whose files are in memory, as memorySite, that counts each read by its "/"-separated path,
// and fails a read of a path in failing once, with an error that is not a missing file's (an editor holding it).
type countedSite struct {
	*ui.Site
	mutex   sync.Mutex
	reads   map[string]int
	failing map[string]bool
}

func newCountedSite(files map[string]string, failing ...string) *countedSite {
	c := &countedSite{reads: map[string]int{}, failing: map[string]bool{}}
	for _, path := range failing {
		c.failing[path] = true
	}
	c.Site = &ui.Site{
		Root: filepath.FromSlash("/app/site"),
		App:  filepath.FromSlash("/app"),
		Read: func(path string) ([]byte, error) {
			path = filepath.ToSlash(filepath.Clean(path))
			c.mutex.Lock()
			defer c.mutex.Unlock()
			c.reads[path]++
			if c.failing[path] {
				delete(c.failing, path)
				return nil, errors.New("the file is held by another process")
			}
			if text, ok := files[path]; ok {
				return []byte(text), nil
			}
			return nil, fs.ErrNotExist
		},
	}
	return c
}

// counts is a copy of the reads so far.
func (c *countedSite) counts() map[string]int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	out := map[string]int{}
	for path, count := range c.reads {
		out[path] = count
	}
	return out
}

// draw draws the page of the route whole, its pieces opened, and gives the document.
func (c *countedSite) draw(t *testing.T, path string) string {
	t.Helper()
	page, err := c.Page(route(path))
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := c.Document(page, value.Value{}, ui.Head{}, kitDesign(t))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// keptFiles are a site of a frame, a region found up from a branch, a page calling one piece twice, and an image.
var keptFiles = map[string]string{
	"/app/site/view.ui":           `<view><header show="true" /><content /></view>`,
	"/app/site/header.ui":         `<header gap="4"><text weight="bold">Shop</text></header>`,
	"/app/site/shop-card.ui":      `<shop-card><text>{{ title }}</text></shop-card>`,
	"/app/site/docs/content.ui":   `<content><shop-card title="Một" /><shop-card title="Hai" /><image value="/assets/pic.png" alt="p" /><image value="/assets/none.png" alt="n" /></content>`,
	"/app/site/assets/pic.png":    "",
	"/app/site/docs/notfound.txt": "",
}

// A site reads each of its files once for its boot (10/11/2026): the frame, a region, the page, a piece called twice,
// an image, and a path that has no file (a page.ui looked for, a frame looked for in the branch, an image not there),
// each read by the first page, none by the next.
func TestASiteReadsEachFileOnceForItsBoot(t *testing.T) {
	files := map[string]string{}
	for path, text := range keptFiles {
		files[path] = text
	}
	files["/app/site/assets/pic.png"] = pngOf(t, 3, 2)
	site := newCountedSite(files)
	first := site.draw(t, "/docs")
	holds(t, first, "Shop", "Một", "Hai", `width="3" height="2"`)
	read := site.counts()
	for _, path := range []string{"/app/site/view.ui", "/app/site/header.ui", "/app/site/shop-card.ui", "/app/site/docs/content.ui",
		"/app/site/assets/pic.png", "/app/site/docs/page.ui", "/app/site/docs/view.ui", "/app/site/assets/none.png"} {
		if read[path] == 0 {
			t.Errorf("the first page did not read %s: %v", path, read)
		}
	}
	for range 3 {
		if again := site.draw(t, "/docs"); again != first {
			t.Fatalf("the next page differs:\n%s\nwant\n%s", again, first)
		}
	}
	for path, count := range site.counts() {
		if count != read[path] {
			t.Errorf("%s read %d times by four pages, %d by the first", path, count, read[path])
		}
	}
}

// A read that failed otherwise than on a missing file (an editor holds it, a disk answers late) is not kept: the next
// page reads again, and draws what the file says. A page, a frame looked for, and an image, each failing once.
func TestAReadThatFailedIsReadAgain(t *testing.T) {
	files := map[string]string{}
	for path, text := range keptFiles {
		files[path] = text
	}
	files["/app/site/assets/pic.png"] = pngOf(t, 3, 2)

	site := newCountedSite(files, "/app/site/docs/content.ui")
	if _, err := site.Page(route("/docs")); err == nil || !strings.Contains(err.Error(), "held by another process") {
		t.Fatalf("the page whose read failed: %v", err)
	}
	holds(t, site.draw(t, "/docs"), "Một")

	site = newCountedSite(files, "/app/site/view.ui", "/app/site/assets/pic.png")
	first := site.draw(t, "/docs")
	if strings.Contains(first, "Shop") || strings.Contains(first, `width="3"`) {
		t.Fatalf("the frame and the image whose reads failed were drawn:\n%s", first)
	}
	holds(t, site.draw(t, "/docs"), "Shop", `width="3" height="2"`)
}

// A file that does not parse is read once: each page of the boot fails as the first did, saying the same.
func TestAFileThatDoesNotParseIsReadOnce(t *testing.T) {
	site := newCountedSite(map[string]string{"/app/site/docs/content.ui": "<content><text>Một</content>"})
	_, first := site.Page(route("/docs"))
	_, again := site.Page(route("/docs"))
	if first == nil || again == nil || first.Error() != again.Error() {
		t.Fatalf("two pages of a file that does not parse: %v, then %v", first, again)
	}
	if read := site.counts()["/app/site/docs/content.ui"]; read != 1 {
		t.Errorf("content.ui that does not parse read %d times", read)
	}
}

// Each use of a file is a tree of its own: a page changed by whoever holds it leaves the next page as the file says,
// and a region's call puts its show on the region's root without touching what stands beside it.
func TestEachUseOfAFileIsATreeOfItsOwn(t *testing.T) {
	site := newCountedSite(keptFiles)
	page, err := site.Page(route("/docs"))
	if err != nil {
		t.Fatal(err)
	}
	want := dump([]*ui.Node{page})
	for _, text := range []string{`header 1:1 gap="4" show="true"`, `text 1:17 weight="bold"`} {
		if !strings.Contains(want, text) {
			t.Fatalf("the page lacks %q:\n%s", text, want)
		}
	}
	// whoever holds the page changes all of it, down to its last word
	var change func(n *ui.Node)
	change = func(n *ui.Node) {
		n.Tag, n.Text = n.Tag+"x", n.Text+"x"
		for i := range n.Attributes {
			n.Attributes[i].Value += "x"
		}
		n.Attributes = append(n.Attributes, ui.Attribute{Name: "changed"})
		for _, child := range n.Children {
			change(child)
		}
		n.Children = append(n.Children, &ui.Node{Text: "changed"})
	}
	change(page)
	again, err := site.Page(route("/docs"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dump([]*ui.Node{again}); got != want {
		t.Errorf("the next page is\n%s\nwant\n%s", got, want)
	}
}

// Pages drawn at once by many requests of one boot are each what the files say: the first of them reads, the others
// wait or read the same, and none draws into another's tree.
func TestPagesDrawnAtOnceAreEachWhatTheFilesSay(t *testing.T) {
	files := map[string]string{}
	for path, text := range keptFiles {
		files[path] = text
	}
	files["/app/site/assets/pic.png"] = pngOf(t, 3, 2)
	want := newCountedSite(files).draw(t, "/docs")
	site := newCountedSite(files)
	page := func() (string, error) {
		assembled, err := site.Page(route("/docs"))
		if err != nil {
			return "", err
		}
		document, _, err := site.Document(assembled, value.Value{}, ui.Head{}, kitDesign(t))
		return document, err
	}
	var group sync.WaitGroup
	got := make(chan string, 16*20)
	for range 16 {
		group.Go(func() {
			for range 20 {
				document, err := page()
				if err != nil {
					document = err.Error()
				}
				got <- document
			}
		})
	}
	group.Wait()
	close(got)
	for document := range got {
		if document != want {
			t.Fatalf("a page drawn at once with others is\n%.600s\nwant\n%.600s", document, want)
		}
	}
}
