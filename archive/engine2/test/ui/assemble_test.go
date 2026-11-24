package ui_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kitwork/engine2/library/ui"
)

// memorySite is a site whose files are in memory: paths "/"-separated under /app/site, the app at /app.
func memorySite(files map[string]string) *ui.Site {
	return &ui.Site{
		Root: filepath.FromSlash("/app/site"),
		App:  filepath.FromSlash("/app"),
		Read: func(path string) ([]byte, error) {
			if text, ok := files[filepath.ToSlash(filepath.Clean(path))]; ok {
				return []byte(text), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

func route(path string) string { return filepath.FromSlash("/app/site" + path) }

// A page is its route's content.ui in the nearest view.ui, each region the frame and the regions place put
// where it stands: the nearest file of the region from the route's folder up (a branch's footer.ui over the
// site's), the call's if and show (medium:show too) added to the placed root.
func TestAPageIsItsContentInTheNearestFrameWithItsRegions(t *testing.T) {
	site := memorySite(map[string]string{
		"/app/site/view.ui":         "<view background=\"canvas\"><header /><content /><footer /></view>",
		"/app/site/header.ui":       "<header gap=\"4\"><navbar show=\"false\" medium:show=\"true\" /></header>",
		"/app/site/navbar.ui":       "<navbar><link value=\"/\">Home</link></navbar>",
		"/app/site/footer.ui":       "<footer>site</footer>",
		"/app/site/content.ui":      "<content><text>Home</text></content>",
		"/app/site/docs/content.ui": "<content><text>Docs</text></content>",
		"/app/site/docs/footer.ui":  "<footer>docs</footer>",
	})
	page, err := site.Page(route("/docs"))
	if err != nil {
		t.Fatal(err)
	}
	want := `<view 1:1 background="canvas">
  <header 1:1 gap="4">
    <navbar 1:1 show="false" medium:show="true">
      <link 1:9 value="/">
        1:25 "Home"
  <content 1:1>
    <text 1:10>
      1:16 "Docs"
  <footer 1:1>
    1:9 "docs"
`
	if got := dump([]*ui.Node{page}); got != want {
		t.Errorf("/docs reads as\n%s\nwant\n%s", got, want)
	}
	home, err := site.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := dump([]*ui.Node{home}); !strings.Contains(got, `"site"`) || !strings.Contains(got, `"Home"`) {
		t.Errorf("/ took the site's footer and its own content? got\n%s", got)
	}
}

// A branch's view.ui is the frame of its pages; a page with no view.ui up its folders is its content alone;
// notfound.ui is the nearest from the route up, in the frame, and a site without one has none.
func TestABranchFrameAPageWithNoFrameAndNotFound(t *testing.T) {
	site := memorySite(map[string]string{
		"/app/site/view.ui":            "<view><content /></view>",
		"/app/site/notfound.ui":        "<content><text>Not found</text></content>",
		"/app/site/admin/view.ui":      "<view color=\"ink\"><content /></view>",
		"/app/site/admin/content.ui":   "<content>admin</content>",
		"/app/site/admin/x/content.ui": "<content>x</content>",
	})
	page, err := site.Page(route("/admin/x"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dump([]*ui.Node{page}); !strings.HasPrefix(got, `<view 1:1 color="ink">`) {
		t.Errorf("/admin/x is framed by admin's view.ui? got\n%s", got)
	}
	missing, err := site.NotFound(route("/admin/x"))
	if err != nil || missing == nil || !strings.Contains(dump([]*ui.Node{missing}), "Not found") {
		t.Errorf("NotFound(/admin/x): %v %v, want the site's notfound.ui in admin's frame", missing, err)
	}

	bare := memorySite(map[string]string{"/app/site/content.ui": "<content><header /></content>", "/app/site/header.ui": "<header>h</header>"})
	page, err = bare.Page(route(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := dump([]*ui.Node{page}); !strings.HasPrefix(got, "<content") || !strings.Contains(got, `"h"`) {
		t.Errorf("a page with no frame is its content, its regions placed; got\n%s", got)
	}
	if none, err := bare.NotFound(route("")); none != nil || err != nil {
		t.Errorf("a site with no notfound.ui: %v %v, want none", none, err)
	}
}

// Every mistake of a frame or a region is an error at its place.
func TestAMistakeOfAFrameOrARegionIsAnErrorAtItsPlace(t *testing.T) {
	for _, want := range []struct {
		files map[string]string
		error string
	}{
		{map[string]string{"/app/site/view.ui": "<view><footer /><content /></view>", "/app/site/content.ui": "<content />"},
			"view.ui:1:7: không tìm thấy footer.ui (tìm từ thư mục của route lên gốc site)"},
		{map[string]string{"/app/site/view.ui": "<view><footer /><content /></view>", "/app/site/content.ui": "<content />", "/app/site/footer.ui": "<box />"},
			"footer.ui:1:1: footer.ui có gốc <footer>, không phải <box>"},
		{map[string]string{"/app/site/view.ui": "<view><footer gap=\"2\" /><content /></view>", "/app/site/content.ui": "<content />", "/app/site/footer.ui": "<footer />"},
			"view.ui:1:15: thẻ vùng <footer /> ở chỗ gọi chỉ nhận source, if, show; gap đặt trên gốc của file footer.ui"},
		{map[string]string{"/app/site/view.ui": "<view><content /><content /></view>", "/app/site/content.ui": "<content />"},
			"view.ui:1:18: <content /> đặt lần thứ hai: một trang có một chỗ cho nội dung của route"},
		{map[string]string{"/app/site/view.ui": "<view><box /></view>", "/app/site/content.ui": "<content />"},
			"view.ui:1:1: view.ui không đặt <content />: trang của route không có chỗ"},
		{map[string]string{"/app/site/view.ui": "<view><header /><content /></view>", "/app/site/content.ui": "<content />", "/app/site/header.ui": "<header><header /></header>"},
			"header.ui:1:9: <header /> đặt header.ui bên trong chính nó"},
		{map[string]string{"/app/site/content.ui": "<content />", "/app/site/page.ui": "<page />"},
			"page.ui:1:1: page.ui là tên cũ (22/10/2026): khung là view.ui với gốc <view>, trang là content.ui với gốc <content>"},
		{map[string]string{"/app/site/view.ui": "<view><header /><content /></view>", "/app/site/content.ui": "<content />", "/app/site/header.ui": "<header />", "/app/site/header.work": ""},
			"header.work:1:1: vùng không có file .work: điều header làm là một component bên trong header.ui, viết bằng thẻ của nó"},
		{map[string]string{"/app/site/content.ui": "<box />"}, "content.ui:1:1: content.ui có gốc <content>, không phải <box>"},
		{map[string]string{"/app/site/content.ui": "<content />\n<content />"}, "content.ui:2:1: phần tử thứ hai ở ngoài cùng: một file .ui có đúng một gốc, <content> ở dòng 1"},
		{map[string]string{"/app/site/content.ui": "hi <content />"}, "content.ui:1:1: chữ nằm ngoài gốc: một file .ui có đúng một phần tử ở ngoài cùng"},
		{map[string]string{"/app/site/content.ui": "  "}, "content.ui:1:1: file trống: một file .ui có đúng một gốc"},
		{map[string]string{"/app/site/view.ui": "<box><content /></box>", "/app/site/content.ui": "<content />"}, "view.ui:1:1: view.ui có gốc <view>, không phải <box>"},
		{map[string]string{"/app/site/view.ui": "<view><header /><content /></view>", "/app/site/content.ui": "<content />", "/app/site/header.ui": "<header show=\"true\" />", "/app/site/x/content.ui": "<content />"},
			""},
	} {
		_, err := memorySite(want.files).Page(route(""))
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != want.error {
			t.Errorf("%v:\n got  %q\n want %q", want.files, got, want.error)
		}
	}
}

// Every route of kitwork.io that has a content.ui, and its notfound.ui, assembles in its frame.
func TestEveryPageOfKitworkIoAssembles(t *testing.T) {
	app := filepath.Join("..", "..", "..", "apps", "029w8decto4uabhpsmfjlxgknzqy7356riv1")
	root := filepath.Join(app, "kitwork.io")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no kitwork.io beside engine2: %v", err)
	}
	site := &ui.Site{Root: root, App: app}
	pages := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "content.ui" {
			return err
		}
		page, err := site.Page(filepath.Dir(path))
		if err != nil {
			t.Errorf("%s: %v", path, err)
		} else if page.Tag != "view" {
			t.Errorf("%s: framed by <%s>, want kitwork.io's view.ui", path, page.Tag)
		}
		pages++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing, err := site.NotFound(root); err != nil || missing == nil {
		t.Errorf("notfound.ui: %v %v", missing, err)
	}
	if pages == 0 {
		t.Fatal(errors.New("no content.ui in kitwork.io"))
	}
	t.Logf("%d pages of kitwork.io assembled", pages)
}
