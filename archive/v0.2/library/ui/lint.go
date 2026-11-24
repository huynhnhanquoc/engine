package ui

// The warnings of how a page writes its elements, moved from the first engine's ui/compile.go and ui/classes.go on
// 06/11/2026 (warning.go): house style the page reads all the same (a tag with capitals, a true-or-false attribute
// written bare, an expression wrapped in {{ }}, the axes of a prefix out of order, widths that say one width, an id
// with capitals), and what works but leaves a reader out or does nothing where it is written (a button no screen
// reader can name, an image with no alt, pin beside place, pin, material or lazy on a tag that takes none, a region
// with nothing in it). The words are the first engine's.

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/kitwork/engine2/core/compiler"
)

// booleanAttributes are the attributes whose value is "true", "false" or {{ }}; never written bare.
var booleanAttributes = map[string]bool{
	"required": true, "readonly": true, "disabled": true, "open": true, "checked": true,
	"multiple": true, "autofocus": true, "exact": true, "italic": true, "underline": true,
	"strikethrough": true, "wrap": true, "selected": true, "suggest": true, "reload": true, "new-tab": true,
}

// lint warns of what an element writes in a form the page reads but the house writes another way: a tag name with
// capitals (<Box> is box: tag names do not tell case apart), a true-or-false attribute of a tag of Kitwork written
// bare (required is required="true", and reads as it), and for, if, show or an event wrapped in {{ }} (they are
// expressions already: the page reads what the braces hold).
func (r *renderer) lint(n *Node) {
	if n.Written != "" {
		r.warn(n, nil, "tên thẻ <%s> viết thường: <%s> (tên thẻ không phân biệt hoa thường, nên đây là <%s>)", n.Written, n.Tag, n.Tag)
	}
	kitwork := !strings.Contains(n.Tag, "-") // a piece's attribute written bare is its input, the piece's to read
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if a.Bare {
			if kitwork && booleanAttributes[a.Name[strings.LastIndex(a.Name, ":")+1:]] {
				r.warn(n, a, "thuộc tính đúng/sai luôn có giá trị: viết %s=\"true\" (viết trơn đọc là true)", a.Name)
			}
			continue
		}
		if !expressionAttribute(a) {
			continue
		}
		if bare, ok := wrapped(strings.TrimSpace(a.Value)); ok && bare != "" && !strings.Contains(bare, "{{") {
			r.warn(n, a, "%s", a.Name+" đã là biểu thức, không bọc {{ }}: viết "+a.Name+"=\""+bare+"\"")
		}
	}
}

// isTrue says a true-or-false attribute is true: "true", or written bare (which lint warns of).
func isTrue(a Attribute) bool {
	return a.Bare || strings.TrimSpace(a.Value) == "true"
}

// expressionAttribute says an attribute's whole value is an expression: for, if, a show that is no plain true or
// false, an event. scroll written as a word is the area that scrolls.
func expressionAttribute(a *Attribute) bool {
	switch {
	case a.Bare:
		return false
	case a.Name == "for", a.Name == "if":
		return true
	case a.Name == "show":
		v := strings.TrimSpace(a.Value)
		return v != "true" && v != "false"
	}
	return isEvent(a.Name) && !scrollArea(a.Name, a.Value)
}

// scrollArea says scroll="…" is the area that scrolls rather than the scroll event: a word (x, y, or a word the
// stylesheet refuses with what scroll takes) or a value from data. A handler calls or changes something
// (scroll="track()"), which a word cannot.
func scrollArea(name, value string) bool {
	if name != "scroll" {
		return false
	}
	v := strings.TrimSpace(value)
	if strings.Contains(v, "{{") {
		return true
	}
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// requireName warns of a button, a link or a badge that says nothing a screen reader can read (KITWORK_UI mục 7,
// "Tên đọc được"): an icon is decoration unless it has an alt, so a button holding only an icon needs that alt,
// the button's name. A badge may carry the name itself (alt on the badge). It works and shows as written.
func (r *renderer) requireName(n *Node) {
	switch n.Tag {
	case "button", "link":
		if readableName(n) {
			return
		}
		switch heldMedia(n) {
		case "icon":
			r.warn(n, nil, "<%s> chỉ có icon nên chưa có tên để trình đọc màn hình đọc: viết alt trên icon (vd. <icon value=\"x\" alt=\"Đóng\" />)", n.Tag)
			return
		case "image":
			r.warn(n, nil, "<%s> chỉ có ảnh nên chưa có tên để trình đọc màn hình đọc: viết alt trên ảnh, nói nó dẫn tới đâu (vd. <image value=\"/logo.svg\" alt=\"Trang chủ\" />)", n.Tag)
			return
		}
		r.warn(n, nil, "<%s> cần tên đọc được: chữ bên trong, hoặc icon có alt", n.Tag)
	case "badge":
		if alt, ok := n.Attribute("alt"); ok && strings.TrimSpace(alt.Value) != "" {
			return
		}
		if readableName(n) {
			return
		}
		if what := heldMedia(n); what != "" {
			held := "icon"
			if what == "image" {
				held = "ảnh"
			}
			r.warn(n, nil, "%s", "<badge> chỉ có "+held+" cần alt nói nó là gì (vd. <badge alt=\"Đang online\">)")
			return
		}
		r.warn(n, nil, "<badge> cần tên đọc được: chữ bên trong, hoặc alt nói nó là gì (vd. <badge alt=\"Đang online\">)")
	}
}

// readableName says an element holds something a screen reader reads: text (written or from {{ }}), an icon or an
// image with an alt that is not empty, or what another file draws (a piece, a component), whose own markup says it.
func readableName(n *Node) bool {
	for _, child := range n.Children {
		switch {
		case child.IsText():
			if strings.TrimSpace(child.Text) != "" {
				return true
			}
		case child.Tag == "icon" || child.Tag == "image":
			if alt, ok := child.Attribute("alt"); ok && strings.TrimSpace(alt.Value) != "" {
				return true
			}
		default:
			if !drawnHere(child.Tag) {
				return true // a piece or a component: its own markup decides, not this check
			}
			if readableName(child) {
				return true
			}
		}
	}
	return false
}

// drawnHere says a tag is one of Kitwork's elements, drawn from what the page writes inside it: not a piece of the
// site nor a component, whose markup is their own.
func drawnHere(tag string) bool {
	if _, ok := catalog[tag]; ok {
		return true
	}
	return tag == "text" || tag == "richtext" || tag == "reader"
}

// heldMedia is what an element without a spoken name holds that could carry one: "icon" when it holds an icon,
// else "image" when it holds an image, else "".
func heldMedia(n *Node) string {
	found := ""
	for _, child := range n.Elements() {
		what := child.Tag
		if what != "icon" && what != "image" {
			what = heldMedia(child)
		}
		if what == "icon" {
			return "icon"
		}
		if what == "image" {
			found = "image"
		}
	}
	return found
}

// anchorWord says an id is lowercase letters and digits in words joined by single hyphens, starting with a letter.
func anchorWord(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' || strings.HasSuffix(value, "-") || strings.Contains(value, "--") {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

// lintAnchor warns of an id written with capitals: the page writes it as it is, and a link must write it the same.
func (r *renderer) lintAnchor(n *Node, a *Attribute) {
	value := strings.TrimSpace(a.Value)
	if !strings.Contains(value, "{{") && !anchorWord(value) && anchorWord(strings.ToLower(value)) {
		r.warn(n, a, "id \"%s\" có chữ hoa: id viết bằng chữ thường, số và dấu gạch nối (id=\"%s\"); link phải viết đúng hoa thường như id", value, strings.ToLower(value))
	}
}

// prefixOf reads the prefix of a presentation attribute. Its axes written out of order (hover+large:) still say
// one thing, so that is house style: the prefix is read in its order (large+hover) and a warning says how to write
// it. Every other problem of a prefix is a word the page does not understand: an error.
func (r *renderer) prefixOf(n *Node, a *Attribute, chain, name string) (prefix, error) {
	when, problem := r.site.readPrefix(chain)
	if problem == "" {
		return when, nil
	}
	if ordered := orderedPrefix(chain); ordered != chain {
		again, second := readPrefix(ordered, "website")
		if second != "" {
			return when, at(n, a, "%s", second) // out of order and wrong besides: the wrong is the error
		}
		r.warn(n, a, "%s", problem+": viết "+ordered+":"+name)
		return again, nil
	}
	return when, at(n, a, "%s", problem)
}

// orderedPrefix puts the groups of a prefix in the order target → width → state, each group as written; a group
// whose first word is on no axis leaves the prefix as it is (readPrefix names it).
func orderedPrefix(chain string) string {
	groups := strings.Split(chain, "+")
	rank := func(group string) int {
		word, _, _ := strings.Cut(group, ",")
		switch {
		case targetWords[word]:
			return 0
		case widthWords[word]:
			return 1
		case stateWords[word]:
			return 2
		}
		return -1
	}
	for _, g := range groups {
		if rank(g) < 0 {
			return chain
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return rank(groups[i]) < rank(groups[j]) })
	return strings.Join(groups, "+")
}

// widthOrder is each width word's place, the narrowest first.
var widthOrder = map[string]int{"small": 1, "medium": 2, "large": 3, "wide": 4, "huge": 5}

// lintWidths warns of a prefix whose widths say one width: a width holds from itself up, so small,large is small,
// and small,small writes small twice. class is the class the attribute became, its prefix before the ':'.
func (r *renderer) lintWidths(n *Node, a *Attribute, widths []string, class, name string) {
	if len(widths) < 2 || !strings.Contains(class, ":") {
		return
	}
	from := widths[0]
	for _, w := range widths {
		if widthOrder[w] < widthOrder[from] {
			from = w
		}
	}
	write := class[:strings.IndexByte(class, ':')]
	var wider []string
	seen := map[string]bool{from: true}
	for _, w := range widths {
		if !seen[w] {
			seen[w] = true
			wider = append(wider, w)
		}
	}
	if len(wider) == 0 {
		r.warn(n, a, "%s — độ rộng %s viết lặp lại: viết %s:%s", a.Name, from, write, name)
		return
	}
	r.warn(n, a, "%s — độ rộng là từ đó trở lên, nên %s đã gồm cả %s: viết %s:%s", a.Name, from, strings.Join(wider, ", "), write, name)
}

// frames are the tags that lay out children in the page's flow and take a background: where pin and material go.
var frames = map[string]bool{
	"view": true, "header": true, "footer": true, "navbar": true, "content": true, "overlay": true, "sidebar": true,
	"tabbar": true, "section": true, "box": true, "card": true, "grid": true, "list": true, "item": true, "empty": true,
	"form": true, "field": true, "accordions": true, "popover": true, "group": true,
}

// pinnable says pin goes on a tag: a frame in the page's flow. Not <view> (the page itself scrolls), not what
// already floats (popover, overlay), not text or a control.
func pinnable(tag string) bool {
	return frames[tag] && tag != "view" && tag != "popover" && tag != "overlay"
}

// lazyOnScreen are the tags always on the screen, each with why: the browser never skips them.
var lazyOnScreen = map[string]string{
	"view":    "cả trang luôn ở trên màn hình",
	"content": "vùng nội dung luôn có phần ở trên màn hình",
	"overlay": "lớp nổi luôn ở trên màn hình",
	"popover": "khung nổi ở trên màn hình khi mở",
}

// misplaced is the warning of pin, material or lazy on a tag that takes none, "" where it goes: the page draws it
// as written.
func misplaced(tag, name string) string {
	switch name {
	case "pin":
		if !pinnable(tag) {
			return fmt.Sprintf("pin ghim một khung vào mép màn hình khi cuộn (box, section, card, list, header, footer…); <%s> không ghim được (trang vẫn ghim nó như đã viết)", tag)
		}
	case "material":
		if !frames[tag] || tag == "view" {
			const frame = "material là chất liệu của một khung (box, card, section, header, footer, popover…)"
			if tag == "view" {
				return frame + "; <view> là nền của cả trang, phía sau không có gì để nhìn qua: đặt material trên một vùng hay một khung bên trong (trang vẫn vẽ material như đã viết)"
			}
			return frame + "; <" + tag + "> không mang material được (trang vẫn vẽ material như đã viết)"
		}
	case "lazy":
		if why, ok := lazyOnScreen[tag]; ok {
			message := "lazy trên <" + tag + ">: " + why + ", trình duyệt không bao giờ bỏ qua nó"
			if tag == "view" || tag == "content" {
				message += "; đặt lazy trên từng phần ở xa màn hình bên trong (section, grid, card…)"
			}
			return message + " (trang vẫn vẽ lazy như đã viết)"
		}
		if pinnable(tag) {
			return ""
		}
		message := "lazy bỏ qua một khung ở xa màn hình (section, box, card, grid, list, footer…); <" + tag + "> không phải khung"
		if tag == "image" {
			message = "lazy bỏ qua một khung ở xa màn hình (section, box, card, grid, list, footer…); ảnh đã tự tải khi tới gần màn hình (loading=\"lazy\")"
		}
		return message + " (trang vẫn vẽ lazy như đã viết)"
	}
	return ""
}

// lintPlace warns of pin beside place: with place the element floats over its parent, so pin does nothing.
func (r *renderer) lintPlace(n *Node) {
	var pin *Attribute
	place := false
	for i := range n.Attributes {
		a := &n.Attributes[i]
		chain, name := splitPrefix(a.Name)
		if name != "pin" && name != "place" {
			continue
		}
		if when, _ := readPrefix(orderedPrefix(chain), "website"); !when.keep {
			continue
		}
		if name == "place" {
			place = true
		} else if pin == nil {
			pin = a
		}
	}
	if pin != nil && place {
		r.warn(n, pin, "pin và place không đi cùng nhau: phần tử có place đã nổi trên cha, không nằm trong dòng chảy để ghim")
	}
}

// lintRegion warns of a region whose file holds nothing: the page draws an empty element, maybe a place kept on
// purpose.
func (r *renderer) lintRegion(n *Node) {
	if regions[n.Tag] && n.Tag != "content" && path.Base(n.File) == n.Tag+".ui" && len(n.Significant()) == 0 {
		r.warn(n, nil, "<%s> trong %s không có nội dung: trang dựng một <%s> rỗng", n.Tag, n.File, n.Tag)
	}
}

// fromData says a value is the page's data, not one the page spells out: {{ }} that is not one expression whose
// every value is written in it ({{ open ? 'brand' : 'paper' }}). A value the page spells out that an attribute does
// not take is an error; one from data is a warning, and makes no class.
func fromData(value string) bool {
	if !strings.Contains(value, "{{") {
		return false
	}
	bare, whole := wrapped(strings.TrimSpace(value))
	if !whole {
		return true
	}
	_, spelled := compiler.ExpressionStrings(bare)
	return !spelled
}

// iconName says a value from data can name an icon: lowercase letters and digits in words joined by hyphens
// (shopping-cart, number-1), nothing a selector or an address could be made of.
func iconName(value string) bool {
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// lazyCuts is why lazy breaks what floats out of the frame (a popover, a part placed outside its parent).
const lazyCuts = "lazy cắt những gì vẽ ra ngoài khung (content-visibility:auto chứa cả phần vẽ trong khung), nên phần nổi ra ngoài mép khung bị cắt mất"

// fixedInFrame is why a frame around breaks what is fixed to the screen inside it (the toast's stack, a placed
// network).
const fixedInFrame = "một khung lazy giữ phần cố định (position:fixed) bên trong theo chính nó, không theo màn hình, nên ngăn thông báo nằm ngoài màn hình"

// tellLazyAround warns, once a lazy frame, at its lazy attribute, that it holds what lazy breaks (what, at): lazy
// cuts what is drawn outside the frame, and a fixed child is placed by the frame, not by the screen. The page draws
// lazy as written.
func (r *renderer) tellLazyAround(what string, at *Node, why string) {
	for i := len(r.parents) - 1; i >= 0; i-- {
		frame := r.parents[i]
		if lazyOnScreen[frame.Tag] != "" || misplaced(frame.Tag, "lazy") != "" {
			continue
		}
		for j := range frame.Attributes {
			a := &frame.Attributes[j]
			if _, name := splitPrefix(a.Name); name != "lazy" || strings.TrimSpace(a.Value) == "false" {
				continue
			}
			if r.lazyTold[frame] {
				return
			}
			r.toldLazy(frame)
			r.warn(frame, a, "%s=\"%s\": khung này giữ %s ở %s:%d:%d; %s: đặt lazy trên khung bên trong không giữ nó, hoặc đưa nó ra ngoài khung lazy (trang vẫn vẽ lazy như đã viết)",
				a.Name, strings.TrimSpace(a.Value), what, at.File, at.Line, at.Column, why)
			return
		}
	}
}

// lintOutside tells a lazy frame around of what floats out of it: a popover, a part placed outside its parent
// (place="below…", "above…").
func (r *renderer) lintOutside(n *Node) {
	if n.Tag == "popover" {
		r.tellLazyAround("<popover>", n, lazyCuts)
		return
	}
	if n.Tag == "text" {
		return // the first engine read a text's place apart, and told nothing of it
	}
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if _, name := splitPrefix(a.Name); name != "place" {
			continue
		}
		if v := strings.TrimSpace(a.Value); strings.HasPrefix(v, "below") || strings.HasPrefix(v, "above") {
			r.tellLazyAround(fmt.Sprintf("<%s %s=\"%s\"> (đặt ra ngoài cha)", n.Tag, a.Name, v), n, lazyCuts)
			return
		}
	}
}

// glassAround is the nearest frame around whose material is a glass (any material but none, or one from data), and
// its material attribute; nil when there is none. Glass holds what is fixed inside it by itself (backdrop-filter).
func (r *renderer) glassAround() (*Node, *Attribute) {
	for i := len(r.parents) - 1; i >= 0; i-- {
		frame := r.parents[i]
		for j := range frame.Attributes {
			a := &frame.Attributes[j]
			if _, name := splitPrefix(a.Name); name == "material" && strings.TrimSpace(a.Value) != "none" {
				return frame, a
			}
		}
	}
	return nil, nil
}

// lintFixed tells what a notice fixed to the screen (the toast's stack, a placed network) is held by instead: a lazy
// frame or a glass around it. what names it, who is the tag.
func (r *renderer) lintFixed(n *Node, what string) {
	r.tellLazyAround(what, n, fixedInFrame)
	if frame, glass := r.glassAround(); glass != nil {
		subject := "toast"
		if n.Tag == "network" {
			subject = "network có place"
		}
		r.warn(n, nil, "%s trong khung kính (material ở %s:%d:%d) bám theo khung đó, không theo màn hình: kính giữ phần cố định bên trong theo chính nó; đặt %s ngoài khung kính, như trong overlay.ui",
			subject, frame.File, glass.Line, glass.Column, n.Tag)
	}
}

// lintShadow tells a name read inside a component that an outer component declares as well: the nearer one is
// read, so one name has two meanings. An editor's toolbar dropdown opened by its own toggle() is the engine's
// reading, not a shadow.
func (r *renderer) lintShadow(n *Node, a *Attribute, name string) {
	owner := -1
	for i := len(r.frames) - 1; i >= 0; i-- {
		if _, ok := r.frames[i].names[name]; ok {
			owner = i
			break
		}
	}
	if owner < 0 {
		return
	}
	for i := owner - 1; i >= 0; i-- {
		if _, ok := r.frames[i].names[name]; !ok {
			continue
		}
		if (name == "toggle" || name == "close") && r.editor != nil && (r.editor.menus[n] != nil || r.editor.triggers[n] != nil) {
			return
		}
		r.warn(n, a, "%s ở đây là của %s, mà %s bao ngoài cũng khai %s: một tên một nghĩa — gọi qua alias ($tên.%s) hoặc đổi tên",
			name, r.frames[owner].component, r.frames[i].component, name, name)
		return
	}
}

// lintShownByWidth warns of a width that shows an element whose show comes from the page (the hidden attribute)
// when the element has no display of its own for that width to give back: the browser's [hidden] keeps it hidden
// at every width, so the width does nothing there. A frame's direction, a grid, a clamp of lines, a text kept whole,
// an image, an icon have one.
func (r *renderer) lintShownByWidth(n *Node, htmlTag string, classes []string) {
	show, ok := n.Attribute("show")
	if v := strings.TrimSpace(show.Value); !ok || v == "true" || v == "false" {
		return
	}
	if htmlTag == "img" || n.Tag == "icon" {
		return
	}
	for _, class := range classes {
		switch base := class[strings.LastIndex(class, ":")+1:]; {
		case base == "flex-row", base == "flex-column", base == "grid", strings.HasPrefix(base, "lines-"),
			base == "wrap-false" && n.Tag == "text": // inline-block on a text only (on a frame it is flex-wrap)
			return
		}
	}
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if strings.HasSuffix(a.Name, ":show") && strings.TrimSpace(a.Value) == "true" {
			r.warn(n, a, `%s="true" bên cạnh show từ dữ liệu không hiện lại một <%s>: thuộc tính hidden của show giấu nó ở mọi độ rộng, vì nó không có cách hiện của riêng nó để trả lại (khung, image, icon, badge, button thì có); đặt show và %s lên một <box> bọc nó`, a.Name, n.Tag, a.Name)
			return
		}
	}
}
