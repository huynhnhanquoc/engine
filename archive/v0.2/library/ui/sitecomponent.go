package ui

import (
	"path/filepath"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// A site's component on a page (docs/component.md, KITWORK_COMPONENTS.md mục 1, 4): a tag with a dash that
// component.define declares. Its function runs on the server each time the page draws it, with the inputs written
// on its tag read where it stands, and answers with context.bind({ … }); its interface, what the tag holds or else
// the file <tag>.ui whose root is the tag, draws as one box with the names it binds before the names where it
// stands. Slice 1 draws it on the server; the browser's half (its works through the bridge, its state held there,
// its alias) comes later.

// Components are the site's components a page draws, which the host runs.
type Components interface {
	// Defines reports a component of the tag.
	Defines(tag string) bool
	// Interface is the file <tag>.ui beside the file that declares the component, there or not; "" for none.
	Interface(tag string) string
	// Run runs the tag's function with inputs, the attributes written on it, for its instance at place (bridge.go),
	// and gives the names it binds, and its works and vars when it binds either: it then runs in the browser.
	Run(tag string, inputs value.Value, place string) (value.Value, *Bridge, error)
}

// component draws a site's component.
func (r *renderer) component(n *Node, inline bool) error {
	inputs := value.Object()
	var drawn []Attribute // what the box itself carries: its look, its show read where it stands, its events
	for i := range n.Attributes {
		a := &n.Attributes[i]
		_, name := splitPrefix(a.Name)
		switch {
		case a.Name == "for" || a.Name == "if" || a.Name == "key" || a.Name == "as":
			continue // as names the instance in the browser, which slice 1 does not run
		case a.Name == "source":
			return at(n, a, "source trên <%s>: engine2 chưa chọn giao diện của component bằng source; viết nội dung trong thẻ hay đặt %s.ui", n.Tag, n.Tag)
		case a.Name == "show" && a.Value != "true" && a.Value != "false":
			// read where it stands, never the component's names (KITWORK_COMPONENTS.md mục 4)
			shown, err := r.value(n, a, a.Value)
			if err != nil {
				return err
			}
			fixed := *a
			fixed.Value = "false"
			if shown.ToBoolean() {
				fixed.Value = "true"
			}
			drawn = append(drawn, fixed)
		case name == "show" || stylesheet.IsPresentation(name) || isEvent(a.Name):
			drawn = append(drawn, *a)
		case !identifier(a.Name):
			return at(n, a, "đầu vào %s của <%s> không phải một tên: viết chữ, số và _ (orderId=\"{{ order.id }}\")", a.Name, n.Tag)
		default:
			input, err := r.input(n, a)
			if err != nil {
				return err
			}
			inputs.Set(a.Name, input)
		}
	}
	place := r.place(n)
	names, bridge, err := r.siteComponents().Run(n.Tag, inputs, place)
	if err != nil {
		return at(n, nil, "<%s>: %v", n.Tag, err)
	}
	box := &Node{Tag: "box", File: n.File, Line: n.Line, Column: n.Column, Attributes: drawn, Children: n.Children}
	if len(n.Significant()) == 0 {
		// the interface used again: the file <tag>.ui, whose root is the tag, the nearest from the folder of the file
		// that calls it up to the site's root (a site's own), then the one beside the file that declares the component
		folder := filepath.Join(r.site.Root, filepath.Dir(filepath.FromSlash(n.File)))
		file := r.site.find(folder, n.Tag)
		if beside := r.siteComponents().Interface(n.Tag); file == "" && beside != "" && r.site.exists(beside) {
			file = beside
		}
		if file == "" {
			return at(n, nil, "<%s /> không có nội dung và không tìm thấy %s.ui (tìm từ thư mục của file gọi lên gốc site, rồi cạnh file khai component.define(%q, …)): giao diện của component viết trong thẻ, hay là file có gốc <%s>", n.Tag, n.Tag, n.Tag, n.Tag)
		}
		for _, open := range r.open {
			if open == file {
				return at(n, nil, "<%s /> gọi %s bên trong chính nó", n.Tag, r.site.relative(file))
			}
		}
		root, err := r.site.root(file)
		if err != nil {
			return err
		}
		if root.Tag != n.Tag {
			return at(root, nil, "%s có gốc <%s>: giao diện dùng lại của component %s có gốc là chính thẻ <%s>", r.site.relative(file), root.Tag, n.Tag, n.Tag)
		}
		written := map[string]bool{}
		for _, a := range root.Attributes {
			written[a.Name] = true
		}
		for _, a := range drawn {
			if written[a.Name] {
				return at(n, nil, "%s viết cả ở chỗ gọi <%s /> và ở gốc của %s: một chỗ thôi", a.Name, n.Tag, r.site.relative(file))
			}
		}
		box = &Node{Tag: "box", File: root.File, Line: root.Line, Column: root.Column, Attributes: append(append([]Attribute{}, root.Attributes...), drawn...), Children: root.Children}
		r.open = append(r.open, file)
		defer func() { r.open = r.open[:len(r.open)-1] }()
	}
	kind := frameSite
	if bridge != nil {
		// an instance the browser runs: its box is its host, by its place (bridge.go)
		r.instance(n.Tag, place, names, bridge)
		r.hosts[box] = &host{attributes: []string{`data-kitwork-component="` + n.Tag + `"`, `data-kitwork-at="` + place + `"`}}
		kind = frameInstance
	}
	// the names it binds, before those where it stands; one that hides a name there is said
	var takeBack []func()
	defer func() {
		for i := len(takeBack) - 1; i >= 0; i-- {
			takeBack[i]()
		}
	}()
	own := map[string]any{}
	for _, key := range names.Keys() {
		if r.eval.given[key] > 0 {
			r.warn(n, nil, "%s của <%s> che tên %s ở chỗ gọi: trong component, %s là của component; đổi một trong hai tên", key, n.Tag, key, key)
		}
		takeBack = append(takeBack, r.eval.put(key, names.Get(key)))
		own[key] = nil
	}
	// its names are the browser's when it runs there, else the server's: what reads them is printed (bridge.go)
	r.frames = append(r.frames, &frame{component: "<" + n.Tag + ">", names: own, kind: kind})
	defer func() { r.frames = r.frames[:len(r.frames)-1] }()
	// a component that holds itself does so through its file, which r.open finds
	return r.element(box, inline)
}

// defined reports a site's component of the tag: a tag with a dash that component.define declares.
func (r *renderer) defined(tag string) bool {
	return r.head.Components != nil && strings.Contains(tag, "-") && r.head.Components.Defines(tag)
}
