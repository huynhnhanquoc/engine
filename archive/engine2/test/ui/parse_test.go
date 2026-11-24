// Package ui_test holds library/ui from outside: what a .ui file reads as, and what it renders.
package ui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/library/ui"
)

// dump writes nodes as a tree of text, one node a line with its place, so a test compares the whole reading:
// <tag line:column name="value" bare closed>, then its children indented; text as a quoted string.
func dump(nodes []*ui.Node) string {
	var b strings.Builder
	var walk func(nodes []*ui.Node, depth int)
	walk = func(nodes []*ui.Node, depth int) {
		for _, node := range nodes {
			b.WriteString(strings.Repeat("  ", depth))
			if node.IsText() {
				fmt.Fprintf(&b, "%d:%d %q\n", node.Line, node.Column, node.Text)
				continue
			}
			fmt.Fprintf(&b, "<%s %d:%d", node.Tag, node.Line, node.Column)
			if node.Written != "" {
				fmt.Fprintf(&b, " written=%s", node.Written)
			}
			for _, attribute := range node.Attributes {
				if attribute.Bare {
					fmt.Fprintf(&b, " %s", attribute.Name)
				} else {
					fmt.Fprintf(&b, " %s=%q", attribute.Name, attribute.Value)
				}
			}
			if node.Closed {
				b.WriteString(" closed")
			}
			b.WriteString(">\n")
			walk(node.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return b.String()
}

// A .ui file reads as a tree of elements and text: quoted values in either quote, bare attributes, prefixes
// and modifiers inside a name, a closed element, a void <input> and a stray </input>, a comment dropped,
// {{ … }} whole (a '<' or a "}}" in quotes inside it is no end), a CRLF or CR line as one line, columns in
// characters, an upper-case tag read as its lower case.
func TestAFileReadsAsATreeOfElementsAndText(t *testing.T) {
	source := "<!-- a frame -->\r\n<box gap=\"4\" medium:padding='6' required>\r\n" +
		"  <text color=\"ink-30\">Chào {{ a < b ? \"}}\" : 'x' }} bạn</text>\r" +
		"  <input name=\"q\" input:debounce(300)=\"search()\"></input>\n" +
		"  <icon value=\"x\" />\n" +
		"  <Box>Đ</Box>\n" +
		"</box>\n"
	nodes, err := ui.Parse("page.ui", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	want := `1:17 "\n"
<box 2:1 gap="4" medium:padding="6" required>
  2:42 "\n  "
  <text 3:3 color="ink-30">
    3:24 "Chào {{ a < b ? \"}}\" : 'x' }} bạn"
  3:64 "\n  "
  <input 4:3 name="q" input:debounce(300)="search()" closed>
  4:58 "\n  "
  <icon 5:3 value="x" closed>
  5:21 "\n  "
  <box 6:3 written=Box>
    6:8 "Đ"
  6:15 "\n"
7:7 "\n"
`
	if got := dump(nodes); got != want {
		t.Errorf("read as\n%s\nwant\n%s", got, want)
	}
}

// Every mistake of the markup is an error at its place, file:line:column, in the words of KITWORK_UI.
func TestAMistakeOfTheMarkupIsAnErrorAtItsPlace(t *testing.T) {
	for _, want := range []struct{ source, error string }{
		{"<box>", "page.ui:1:1: thẻ <box> chưa được đóng"},
		{"<box><text></box>", "page.ui:1:12: thẻ đóng </box> không khớp thẻ mở <text> ở dòng 1"},
		{"</box>", "page.ui:1:1: thẻ đóng </box> không có thẻ mở"},
		{"<box gap=4>", "page.ui:1:10: giá trị của gap phải đặt trong dấu nháy"},
		{"<box gap=\"4>", "page.ui:1:6: giá trị của gap thiếu dấu nháy đóng"},
		{"<box", "page.ui:1:1: thẻ <box> chưa được đóng bằng >"},
		{"<text>{{ a </text>", "page.ui:1:7: biểu thức {{ chưa được đóng bằng }}"},
		{"<!-- a", "page.ui:1:1: chú thích <!-- chưa được đóng bằng -->"},
		{"<box gap=\"2\"\n  gap=\"3\">", "page.ui:2:3: gap viết hai lần trên <box> (lần đầu ở page.ui:1:6): giữ một"},
		{"<box </box", "page.ui:1:7: ký tự không hợp lệ '/' trong thẻ <box>"},
		{"<box></box", "page.ui:1:6: thẻ đóng </box thiếu dấu >"},
		{"<text>Việt</text>\n<box", "page.ui:2:1: thẻ <box> chưa được đóng bằng >"},
		// a column counts characters: ệ is three bytes and one column
		{"<text>Việt nam</text><box gap=4>", "page.ui:1:31: giá trị của gap phải đặt trong dấu nháy"},
	} {
		_, err := ui.Parse("page.ui", []byte(want.source))
		if err == nil || err.Error() != want.error {
			t.Errorf("%q: %v, want %q", want.source, err, want.error)
		}
	}
}

// Every .ui file of the two apps of this repository reads: the parser takes what the sites are written in.
func TestEveryUIFileOfTheAppsReads(t *testing.T) {
	root := filepath.Join("..", "..", "..", "apps")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no apps beside engine2: %v", err)
	}
	read := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".ui" {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := ui.Parse(path, source); err != nil {
			t.Errorf("%v", err)
		}
		read++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if read == 0 {
		t.Fatal("no .ui file under ../../../apps")
	}
	t.Logf("%d .ui files read", read)
}
