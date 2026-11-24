package ui

// <breadcrumb>, the trail to the page viewed (moved from the first engine's ui/compile.go and ui/render.go on
// 06/11/2026): a <nav> named for a screen reader, holding an <ol class="breadcrumb"> of its <item>s, each a
// <li> laid out in a row, its words in a line; a link in an item is a step of the trail, a <text> with no element
// inside is the page viewed (aria-current="page"). After the <nav>, the trail as the BreadcrumbList data search
// engines read (schema.org, JSON-LD): each step's words and address, the site's origin before a path.
//
// The first engine named every trail "Vị trí trang", whatever the page's language; engine2 names it in the page's
// language as its other components do ("Breadcrumb" on an English page), alt="…" naming it otherwise.

import (
	"encoding/json"
	"html"
	"strings"

	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// crumb is one step of a trail: its words and its address.
type crumb struct{ name, url string }

// breadcrumbElement draws a breadcrumb and its data.
func (r *renderer) breadcrumbElement(n *Node) error {
	label := "Vị trí trang"
	if r.english() {
		label = "Breadcrumb"
	}
	nav, list := *n, *n
	nav.Attributes, nav.Children, list.Attributes, list.Children = nil, nil, nil, nil
	nav.source, list.source = nil, nil // drawn as made here, never as its file's (frozen.go)
	for i := range n.Attributes {
		a := &n.Attributes[i]
		_, base := splitPrefix(a.Name)
		switch {
		case a.Name == "for" || a.Name == "if" || a.Name == "key":
			// read where the breadcrumb is drawn
		case a.Name == "alt":
			written, err := r.interpolate(n, a, a.Value)
			if err != nil {
				return err
			}
			label = written
		case base == "show" || isEvent(a.Name):
			nav.Attributes = append(nav.Attributes, *a) // the whole trail shown or not, heard
		case stylesheet.IsPresentation(base):
			list.Attributes = append(list.Attributes, *a) // how its steps are laid out
		default:
			return at(n, a, "thuộc tính %s không có trên <breadcrumb>; breadcrumb nhận alt", a.Name)
		}
	}
	for _, child := range n.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return at(child, nil, "chữ không đặt thẳng trong breadcrumb; mỗi mục là một <item>")
			}
			continue
		}
		if child.Tag != "item" && !strings.Contains(child.Tag, "-") {
			return at(child, nil, "con của breadcrumb chỉ là item; dấu ngăn đặt trong item")
		}
	}
	open, err := r.opening(&nav, kindInline)
	if err != nil {
		return err
	}
	open.attributes = append(open.attributes, `aria-label="`+html.EscapeString(label)+`"`)
	r.write("nav", open)
	steps, err := r.opening(&list, kindInline)
	if err != nil {
		return err
	}
	// the trail wraps and centres its steps across (reboot.css's breadcrumb)
	classes := []string{"breadcrumb", "flex-row"}
	for _, class := range steps.classes {
		if !contains(classes, class) {
			classes = append(classes, class)
		}
	}
	steps.classes = classes
	steps.attributes = append(steps.attributes, `role="list"`)
	r.write("ol", steps)
	var crumbs []crumb
	outerCrumbs, outerTrail := r.crumbs, r.breadcrumb
	r.crumbs, r.breadcrumb = &crumbs, true
	r.enter(n)
	err = r.children(n.Children, false)
	r.leave()
	r.crumbs, r.breadcrumb = outerCrumbs, outerTrail
	if err != nil {
		return err
	}
	r.out.WriteString("</ol></nav>")
	r.out.WriteString(breadcrumbData(crumbs, r.head.Origin))
	return nil
}

// breadcrumbData is a trail as the BreadcrumbList data search engines read, "" for a trail of no step. A path is
// written with the site's origin before it.
func breadcrumbData(crumbs []crumb, origin string) string {
	if len(crumbs) == 0 {
		return ""
	}
	type entry struct {
		Type     string `json:"@type"`
		Position int    `json:"position"`
		Name     string `json:"name"`
		Item     string `json:"item,omitempty"`
	}
	list := struct {
		Context string  `json:"@context"`
		Type    string  `json:"@type"`
		Items   []entry `json:"itemListElement"`
	}{Context: "https://schema.org", Type: "BreadcrumbList"}
	for i, c := range crumbs {
		url := c.url
		if strings.HasPrefix(url, "/") {
			url = origin + url
		}
		list.Items = append(list.Items, entry{Type: "ListItem", Position: i + 1, Name: c.name, Item: url})
	}
	// Marshal writes <, > and & as \u003c, \u003e, \u0026: nothing in the data ends the script element
	data, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	return `<script type="application/ld+json">` + string(data) + `</script>`
}

// plainText is markup without its tags, its white space collapsed: the words of a step of a trail.
func plainText(s string) string {
	var b strings.Builder
	inTag := false
	for _, c := range s {
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			b.WriteRune(c)
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}
