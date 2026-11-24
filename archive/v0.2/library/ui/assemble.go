package ui

import (
	"fmt"
	"path/filepath"
	"strings"
)

// regions are Kitwork's tags a screen is made of, each the root of a file named after it (KITWORK_UI mục 4 and
// 13, KITWORK_LAYOUT): written closed, <header />, the region is placed there; written open, <header>…</header>,
// it is that file's root.
var regions = map[string]bool{
	"header": true, "footer": true, "navbar": true, "content": true, "overlay": true, "sidebar": true, "tabbar": true,
}

// Page is the page a route folder answers with: its content.ui in the nearest view.ui, each region the frame
// places put where it stands, as one tree.
func (s *Site) Page(route string) (*Node, error) {
	return s.assemble(route, filepath.Join(route, "content.ui"))
}

// NotFound is the nearest notfound.ui from a route folder up, in the nearest view.ui, as Page; nil, nil when
// the site has none.
func (s *Site) NotFound(route string) (*Node, error) {
	file := s.find(route, "notfound")
	if file == "" {
		return nil, nil
	}
	return s.assemble(route, file)
}

// assembly is one page being put together: the route its regions are found from, its content, and the files
// placed on the way down, so a region that places itself is an error, not an endless page.
type assembly struct {
	site    *Site
	route   string
	content *Node
	placed  bool
	open    []string
}

// assemble puts the content file in the frame. A page with no view.ui is its content alone. Pieces of the site
// (<surface-card />) stay calls: what they read is known where they are called, so the compiler opens them.
func (s *Site) assemble(route, contentFile string) (*Node, error) {
	if old := filepath.Join(route, "page.ui"); s.exists(old) {
		return nil, &Error{File: s.relative(old), Line: 1, Column: 1, Message: "page.ui là tên cũ (22/10/2026): khung là view.ui với gốc <view>, trang là content.ui với gốc <content>"}
	}
	content, err := s.root(contentFile)
	if err != nil {
		return nil, err
	}
	if content.Tag != "content" {
		return nil, &Error{File: content.File, Line: content.Line, Column: content.Column, Message: fmt.Sprintf("%s có gốc <content>, không phải <%s>", filepath.Base(contentFile), content.Tag)}
	}
	a := &assembly{site: s, route: route, content: content}
	viewFile := s.find(route, "view")
	if viewFile == "" {
		a.placed = true
		a.open = []string{contentFile}
		if err := a.place(content); err != nil {
			return nil, err
		}
		return content, nil
	}
	view, err := s.root(viewFile)
	if err != nil {
		return nil, err
	}
	if view.Tag != "view" {
		return nil, &Error{File: view.File, Line: view.Line, Column: view.Column, Message: "view.ui có gốc <view>, không phải <" + view.Tag + ">"}
	}
	a.open = []string{viewFile}
	if err := a.place(view); err != nil {
		return nil, err
	}
	if !a.placed {
		return nil, &Error{File: view.File, Line: view.Line, Column: view.Column, Message: "view.ui không đặt <content />: trang của route không có chỗ"}
	}
	return view, nil
}

// place puts every region written closed inside parent where it stands, down the whole tree. A node a region went
// into, or one above it, is no longer the copy of its file's (its source is let go: frozen.go).
func (a *assembly) place(parent *Node) error {
	for i, child := range parent.Children {
		if child.IsText() {
			continue
		}
		if regions[child.Tag] && child.Closed {
			placed, err := a.region(child)
			if err != nil {
				return err
			}
			parent.Children[i] = placed
			parent.source = nil
			continue
		}
		if err := a.place(child); err != nil {
			return err
		}
		if child.source == nil {
			parent.source = nil
		}
	}
	return nil
}

// callWords are what a region's call takes: the file it places (source), and whether it is there (if) or seen
// (show, at a width too: medium:show). Its look is its file's.
func callWord(name string) bool {
	if name == "source" || name == "if" {
		return true
	}
	_, word, prefixed := strings.Cut(name, ":")
	return name == "show" || prefixed && word == "show"
}

// region is the root a closed region tag places: the route's content for <content />, otherwise the nearest
// <tag>.ui (or source's) from the route's folder up, its root of the same tag, with the call's if and show.
func (a *assembly) region(call *Node) (*Node, error) {
	for _, attribute := range call.Attributes {
		if !callWord(attribute.Name) || call.Tag == "content" && attribute.Name == "source" {
			return nil, &Error{File: call.File, Line: attribute.Line, Column: attribute.Column, Message: fmt.Sprintf("thẻ vùng <%s /> ở chỗ gọi chỉ nhận source, if, show; %s đặt trên gốc của file %s.ui", call.Tag, attribute.Name, call.Tag)}
		}
	}
	var root *Node
	file := ""
	if call.Tag == "content" {
		if a.placed {
			return nil, &Error{File: call.File, Line: call.Line, Column: call.Column, Message: "<content /> đặt lần thứ hai: một trang có một chỗ cho nội dung của route"}
		}
		a.placed = true
		root = a.content
		file = filepath.Join(a.route, "content")
	} else {
		name := call.Tag
		if source, ok := call.Attribute("source"); ok {
			name = source.Value
		}
		file = a.site.find(a.route, name)
		if file == "" {
			return nil, &Error{File: call.File, Line: call.Line, Column: call.Column, Message: "không tìm thấy " + name + ".ui (tìm từ thư mục của route lên gốc site)"}
		}
		for _, open := range a.open {
			if open == file {
				return nil, &Error{File: call.File, Line: call.Line, Column: call.Column, Message: "<" + call.Tag + " /> đặt " + a.site.relative(file) + " bên trong chính nó"}
			}
		}
		var err error
		if root, err = a.site.root(file); err != nil {
			return nil, err
		}
		if root.Tag != call.Tag {
			return nil, &Error{File: root.File, Line: root.Line, Column: root.Column, Message: fmt.Sprintf("%s.ui có gốc <%s>, không phải <%s>", name, call.Tag, root.Tag)}
		}
		if logic := strings.TrimSuffix(file, ".ui") + ".work"; a.site.exists(logic) {
			return nil, &Error{File: a.site.relative(logic), Line: 1, Column: 1, Message: fmt.Sprintf("vùng không có file .work: điều %s làm là một component bên trong %s, viết bằng thẻ của nó", call.Tag, filepath.Base(file))}
		}
	}
	for _, attribute := range call.Attributes {
		if attribute.Name == "source" {
			continue
		}
		if earlier, ok := root.Attribute(attribute.Name); ok {
			return nil, &Error{File: call.File, Line: attribute.Line, Column: attribute.Column, Message: fmt.Sprintf("%s viết ở chỗ gọi lẫn trên gốc <%s> (%s:%d:%d): giữ một", attribute.Name, root.Tag, root.File, earlier.Line, earlier.Column)}
		}
		root.Attributes = append(root.Attributes, attribute)
		root.source = nil // the call's if and show are on it, not on its file's root
	}
	a.open = append(a.open, file)
	err := a.place(root)
	a.open = a.open[:len(a.open)-1]
	return root, err
}
