package ui

import (
	"html"
	"regexp"
	"strings"
)

// An accordion and a group of them (docs/components/accordion.md, accordions.md): elements of Kitwork that run on
// the browser's own tags, <details> and <summary>, so opening, the keyboard, Ctrl+F and a page without JavaScript
// are the browser's.
//
//	<accordions as="$faq" gap="2">
//	  <text heading="2">Questions</text>
//	  <accordion for="faq of faqs" opened="{{ faq.id == 'delivery' }}">
//	    <box click="toggle()" flex="row" space="between"><text heading="3">{{ faq.title }}</text>
//	      <icon value="chevron-down" opened:rotate="180" /></box>
//	    <text>{{ faq.answer }}</text>
//	  </accordion>
//	</accordions>
//
// An accordion is a <details>; its first child, which carries click="toggle()", is its <summary> itself, its layout
// with it, and toggle() there is the browser's own (what follows it in the click is the page's: toggle(); track(id)).
// opened="true", or a {{ }} true, is the open attribute. In a group, every accordion gets the group's name, so opening
// one closes the others, the browser's rule; multiple="true" leaves the name out. A group's first text names it
// (role="group", aria-labelledby); with an alias, its close() closes every one of its accordions
// (library/component/accordions.js), the only part with JavaScript.

// toggleFirst is a header's click: toggle() first, then what the page adds.
var toggleFirst = regexp.MustCompile(`^\s*toggle\(\)\s*(?:;\s*)?`)

// accordionsScope is a group of accordions whose direct children are being drawn: the name each takes ("" for
// multiple="true").
type accordionsScope struct {
	name string
}

// accordionElement draws an accordion: its header the <summary>, its opened the open attribute, the group's name.
func (r *renderer) accordionElement(n *Node) error {
	group := r.accordions
	if err := r.accordionParts(n); err != nil {
		return err
	}
	own := &host{}
	if group != nil && group.name != "" {
		own.attributes = append(own.attributes, `name="`+html.EscapeString(group.name)+`"`)
	}
	r.hosts[n] = own
	r.accordions = nil // an accordion inside this one's content is no item of the group
	err := r.tag(n, "details", kindContainer)
	r.accordions = group
	return err
}

// accordionParts reads an accordion's parts where they are written: its first child carries click="toggle()" and
// becomes its <summary>, with words a screen reader says and no link or button inside; toggle() is called nowhere
// else in it; it takes no name (a group gives one) and no open (its word is opened since 26/10/2026).
func (r *renderer) accordionParts(n *Node) error {
	for i := range n.Attributes {
		a := &n.Attributes[i]
		switch a.Name {
		case "open":
			return at(n, a, `open nay là opened (26/10/2026, trạng thái là phân từ): viết opened="true" hay opened="{{ … }}"`)
		case "name":
			return at(n, a, "name trên accordion: bọc các mục trong <accordions>, nhóm tự đặt tên chung để mở mục này thì mục kia đóng")
		}
	}
	children := n.Elements()
	if len(children) == 0 {
		return at(n, nil, `accordion cần phần đầu: con đầu tiên mang click="toggle()", rồi nội dung`)
	}
	header := children[0]
	click, ok := header.Attribute("click")
	if !ok || !toggleFirst.MatchString(click.Value) {
		return at(header, nil, `con đầu tiên của accordion là phần đầu, bấm vào thì mở hay đóng: viết click="toggle()" trên nó (thêm việc thì nối sau: click="toggle(); track(id)")`)
	}
	if !holdsWords(header) {
		return at(header, nil, `phần đầu của accordion là một nút, cần chữ làm tên của nó (hay một icon có alt)`)
	}
	if inner := firstOf(header, func(m *Node) bool { return m != header && (m.Tag == "button" || m.Tag == "link") }); inner != nil {
		return at(inner, nil, "<%s> trong phần đầu của accordion: phần đầu đã là một nút (nút trong nút); đặt nó trong nội dung", inner.Tag)
	}
	for _, child := range children {
		if found := firstOf(child, func(m *Node) bool {
			if m == header {
				return false
			}
			a, ok := m.Attribute("click")
			return ok && strings.Contains(a.Value, "toggle()") && !strings.Contains(m.Tag, "-") && componentState[m.Tag] == nil && !insideComponent(child, m)
		}); found != nil {
			return at(found, nil, `toggle() của accordion chỉ ở phần đầu (con đầu tiên): cả hàng đầu là chỗ bấm, để vùng bấm đủ lớn và giữ được thẻ gốc`)
		}
	}
	for _, child := range children[1:] {
		if child.Tag == "accordion" {
			r.warn(child, nil, "%s", "accordion nằm thẳng trong accordion: các mục cùng cấp thì bọc bằng <accordions>, mở mục này thì mục kia đóng")
			break
		}
	}
	r.summaries[header] = strings.TrimSpace(toggleFirst.ReplaceAllString(click.Value, ""))
	return nil
}

// accordionsElement draws a group of accordions: its direct children an optional first text that names it, then
// accordions only, at most one written opened="true" unless multiple="true"; the name the accordions share, its role
// and name, its component when it has an alias.
func (r *renderer) accordionsElement(n *Node) error {
	multiple := false
	if a, ok := n.Attribute("multiple"); ok {
		v := strings.TrimSpace(a.Value)
		if !a.Bare && v != "true" && v != "false" {
			return at(n, &a, `multiple của accordions chỉ nhận "true" hoặc "false"`)
		}
		multiple = a.Bare || v == "true"
	}
	var title *Node
	opened := 0
	for i, child := range n.Elements() {
		switch {
		case i == 0 && child.Tag == "text":
			title = child
		case child.Tag == "accordion":
			if a, ok := child.Attribute("opened"); ok && (a.Bare || strings.TrimSpace(a.Value) == "true") {
				opened++
				if opened > 1 && !multiple {
					return at(child, &a, `hai mục cùng opened="true" trong accordions: mở mục này thì mục kia đóng, nên chỉ một mục mở sẵn được (hay viết multiple="true" trên <accordions>)`)
				}
			}
		case child.Tag == "group":
			return at(child, nil, "<group> trong accordions: con của accordions là các accordion; bố cục đặt trong từng accordion, hay bọc cả nhóm trong <section>/<box>")
		default:
			return at(child, nil, "<%s> trong accordions: con của accordions là các accordion (và một <text> đầu tiên làm tên của nhóm); bố cục đặt trong từng accordion, hay bọc cả nhóm trong <section>/<box>", child.Tag)
		}
	}
	id := placeID("kitwork-accordions", n)
	own := &host{}
	if title != nil {
		titleID := id + "-title"
		if a, ok := title.Attribute("id"); ok && !strings.Contains(a.Value, "{{") {
			titleID = strings.TrimSpace(a.Value)
		} else {
			r.hosts[title] = &host{attributes: []string{`id="` + titleID + `"`}}
		}
		own.attributes = append(own.attributes, `role="group"`, `aria-labelledby="`+html.EscapeString(titleID)+`"`)
	}
	r.hosts[n] = own
	scope := &accordionsScope{}
	if !multiple {
		scope.name = id
	}
	outer := r.accordions
	r.accordions = scope
	err := r.tag(n, "div", kindContainer)
	r.accordions = outer
	return err
}

// holdsWords says n holds words a screen reader says: text, a {{ }} or an icon with alt.
func holdsWords(n *Node) bool {
	for _, child := range n.Children {
		if child.IsText() && strings.TrimSpace(child.Text) != "" {
			return true
		}
		if child.Tag == "icon" {
			if a, ok := child.Attribute("alt"); ok && strings.TrimSpace(a.Value) != "" {
				return true
			}
		}
		if !child.IsText() && holdsWords(child) {
			return true
		}
	}
	return false
}

// firstOf is the first node under n, n included, in the order written, that match says yes to; nil when none does.
func firstOf(n *Node, match func(*Node) bool) *Node {
	if match(n) {
		return n
	}
	for _, child := range n.Elements() {
		if found := firstOf(child, match); found != nil {
			return found
		}
	}
	return nil
}

// insideComponent says m stands inside a component of Kitwork under top, or inside an accordion top is or holds (a
// dropdown's toggle() is its own, and so is an accordion's own header's).
func insideComponent(top, m *Node) bool {
	var walk func(n *Node, in bool) (bool, bool)
	walk = func(n *Node, in bool) (bool, bool) {
		if n == m {
			return in, true
		}
		_, component := componentState[n.Tag]
		owner := component && n != top || n.Tag == "accordion"
		for _, child := range n.Elements() {
			if inside, found := walk(child, in || owner); found {
				return inside, true
			}
		}
		return false, false
	}
	inside, _ := walk(top, false)
	return inside
}

// placeID is prefix, a dash and a hash of the file, line and column of n's tag: an id that stays the same from one
// draw to the next.
func placeID(prefix string, n *Node) string {
	return prefix + strings.TrimPrefix(dialogID(n), "kitwork-dialog")
}
