package ui

import (
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/icons"
	"github.com/kitwork/engine2/library/ui/kernel"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// renderer is one render of one page: the site its pieces are found in, the stylesheet's table and the rules
// of the classes the page carries, the expressions' evaluator, and the HTML written so far.
type renderer struct {
	site  *Site
	table *stylesheet.Table
	rules *stylesheet.Rules
	eval  *evaluator
	out   output
	// open are the files of the pieces being drawn, so a piece that calls itself is an error
	open []string
	// unread are the attributes engine2 does not draw yet (an event outside every component, which waits for the page
	// to run in the browser; a word nothing reads yet), each once: what the page will gain, never an error
	unread map[string]bool
	// icons are the icons the page shows, by name, for the rule of each in its stylesheet
	icons []string
	// container is the width <view container="…"> gives each region directly inside it, set while its
	// children are drawn and nil below them
	container []string
	// frames are Kitwork's components around what is being drawn, the nearest last: inside one its names are its
	// own, and what reads them runs in the browser when the browser runs it (live.go)
	frames []*frame
	// aliasOf is the component each alias of the page names ($menu: dropdown)
	aliasOf map[string]string
	// facts are what the page holds, its editors when it is a page assembled for the boot (kept.go)
	facts pageFacts
	// components are the components the browser runs on this page, each a file it loads
	components map[string]bool
	// live says the page holds something the kernel reads, so it loads the kernel; intersect that it writes an
	// intersect-enter or intersect-leave, so it loads their observer (kernel.Intersect)
	live, intersect bool
	// program is the page's program, the sources the kernel runs as JavaScript (program.go), nil for none; replayed
	// where the parts kept for later pages were written again, with the values they hold (frozen.go)
	program  *kernel.File
	replayed []replaySpan
	// head is what the route says of the page: its language, the address viewed, whether the site moves
	// without a reload
	head Head
	// hosts are what a component's tag adds to the element it builds (notices.go), by the node drawn
	hosts map[*Node]*host
	// summaries are the headers of the page's accordions, drawn as their <summary>, each with what its click runs
	// after toggle(), the browser's; accordions is the group whose accordions are being drawn (accordion.go)
	summaries  map[*Node]string
	accordions *accordionsScope
	// avatars is the group whose avatars are being drawn, nil outside one (avatar.go); autocomplete the
	// autocomplete whose inside is (autocomplete.go)
	avatars      *avatarsScope
	autocomplete *autocompleteScope
	// blueprint is the toast's notice the kernel makes each row of, nil before a toast; toastAt is where the
	// page's toast is written, "" for none (one stack on a page)
	blueprint *blueprint
	toastAt   string
	// inBlueprint is how deep in the toast's notice the drawing is: there the notice is the browser's, and the
	// server paints no value of it
	inBlueprint int
	// parents are the elements around what is being drawn, the nearest last
	parents []*Node
	// inContent is how deep in the route's content the drawing is: a link outside it (a header's, a footer's)
	// stays when Drive swaps the content, so it says how to be marked again
	inContent int
	// terminal is the terminal whose commands the <code> elements being drawn are, nil outside one and inside a
	// component nested in it, whose code is its own (terminal.go)
	terminal *terminalScope
	// tabsHosts, tabButtons and tabPanels are the page's tabs as planned before it is drawn (tabs.go): each host,
	// each tab's button, each panel, by node
	tabsHosts  map[*Node]*tabsPlan
	tabButtons map[*Node]*tabsPlan
	tabPanels  map[*Node]*panelPlan
	// after is markup written right after an element closes (a tabs pre-paint), by node
	after map[*Node]string
	// carousel is the carousel whose inside is being drawn, nil outside one; carousels counts the page's, for the
	// ids of their tracks (carousel.go)
	carousel  *carouselScope
	carousels int
	// playing are the page's carousels that play by itself, checked for a way to stop once the page is drawn;
	// countdowns its countdowns, checked for a way to start
	playing    []*carouselScope
	countdowns []*Node
	// countdown is the countdown whose inside is being drawn, nil outside one (countdown.go)
	countdown *countdownScope
	// editor is the editor whose inside is being drawn, nil outside one; editors counts the page's, for their ids,
	// editorRows are the rows each writes, for the heights of the editor's look, and editorStarts what each
	// instance starts with, read once (editor.go)
	editor       *editorScope
	editors      int
	editorRows   []int
	editorStarts map[*Node]map[string]any
	// editorRead says the start of an editor was read from what the page is given (frozen.go)
	editorRead map[*Node]bool
	// field is the ids of the <field> whose inside is being drawn, nil outside one and inside an editor's link
	// popover; fields counts the page's (field.go)
	field  *fieldIDs
	fields int
	// form is what the page's forms are while it is drawn (form.go), nil before a select bound to a field
	form *formState
	// breadcrumb says what is drawn is a breadcrumb's step or inside one's item, crumbs the steps its trail has so
	// far (breadcrumb.go)
	breadcrumb bool
	crumbs     *[]crumb
	// warnings are what the drawing found that works but is probably not what the author wants, each once, in the
	// order found; warned are their lines (warning.go)
	warnings []Warning
	warned   map[string]bool
	// lazyTold are the lazy frames told of what they hold that lazy breaks, each once (lint.go)
	lazyTold map[*Node]bool
	// rows says how many lists the element being drawn is a row of (each)
	rows int
	// page is the page while it is drawn when it runs in the browser, nil for one the server prints (bridge.go);
	// marks are what a row of its lists adds to the element it is drawn as, by node
	page  *pageBridge
	marks map[*Node][]string
	// places counts the instances drawn from each place of a site component (bridge.go)
	places map[string]int
	// freezer draws the page in parts, each kept once drawn (frozen.go)
	freezer
}

// stateOf is what component n declares as its instance starts: its kind's state, with what its call chose (the tab
// a tabs starts on, the name it keeps its choice under).
func (r *renderer) stateOf(n *Node) map[string]any {
	if n.Tag == "editor" {
		return r.editorStart(n) // its value's words, the format it starts in
	}
	state := componentState[n.Tag]
	if plan := r.tabsPlanOf(n); plan != nil {
		own := make(map[string]any, len(state))
		for name, initial := range state {
			own[name] = initial
		}
		own["selected"], own["persist"] = plan.start, plan.persist
		return own
	}
	return state
}

// parent is the element around what is being drawn, nil at the top.
func (r *renderer) parent() *Node {
	if len(r.parents) == 0 {
		return nil
	}
	return r.parents[len(r.parents)-1]
}

// at is an error at the place of a node or of one of its attributes.
func at(n *Node, a *Attribute, format string, args ...any) error {
	line, column := n.Line, n.Column
	if a != nil {
		line, column = a.Line, a.Column
	}
	return &Error{File: n.File, Line: line, Column: column, Message: fmt.Sprintf(format, args...)}
}

// children draws list in order; inline says they stand in a line (a <text> there is a <span>).
func (r *renderer) children(list []*Node, inline bool) error {
	for _, child := range list {
		if err := r.node(child, inline); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) node(n *Node, inline bool) error {
	if n.IsText() {
		text, err := r.interpolate(n, nil, n.Text)
		if err != nil {
			return err
		}
		r.out.escape(text)
		return nil
	}
	if a, ok := n.Attribute("for"); ok {
		if r.blueprint != nil && n == r.blueprint.node && r.inBlueprint == 0 {
			return r.drawBlueprint(n, inline) // the toast's notice: one blueprint, rows made by the kernel
		}
		return r.each(n, &a, inline)
	}
	return r.part(n, inline)
}

// each draws n once for every item of its for: "item of list", "item, index of list", an optional
// "; key: expression" (the key is the kernel's, for a row that keeps its state).
func (r *renderer) each(n *Node, a *Attribute, inline bool) error {
	r.rows++ // what is drawn is a row of a list (a dialog refuses to be one: dialog.go)
	defer func() { r.rows-- }()
	source := strings.TrimSpace(a.Value)
	if bare, ok := wrapped(source); ok {
		source = bare
	}
	head := source
	if i := strings.Index(source, ";"); i >= 0 {
		head = source[:i]
		if tail := strings.TrimSpace(source[i+1:]); !strings.HasPrefix(tail, "key:") {
			return at(n, a, "sau ; trong for chỉ có key: (for=\"x of list; key: x.id\")")
		}
	}
	names, list, ok := strings.Cut(head, " of ")
	if !ok {
		return at(n, a, "for viết là \"x of list\" (hoặc \"x, i of list\")")
	}
	item, index, _ := strings.Cut(names, ",")
	item, index = strings.TrimSpace(item), strings.TrimSpace(index)
	if !identifier(item) || index != "" && !identifier(index) {
		return at(n, a, "tên biến của for không hợp lệ: %s", names)
	}
	if err := r.dialogUnread(n, a, list, "for"); err != nil {
		return err
	}
	items, err := r.value(n, a, list)
	if err != nil {
		return err
	}
	live, err := r.liveList(n, a, list)
	if err != nil {
		return err
	}
	if items.IsNullish() && !live {
		return nil
	}
	if !items.IsArray() && !items.IsNullish() {
		return at(n, a, "for đi qua một mảng; %s là %s", strings.TrimSpace(list), items.Kind())
	}
	if live {
		key := ""
		if i := strings.Index(source, ";"); i >= 0 {
			key = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(source[i+1:]), "key:"))
		}
		return r.eachLive(n, a, inline, item, index, list, key, items)
	}
	for i, each := range items.Items() {
		takeItem := r.eval.put(item, each)
		takeIndex := func() {}
		if index != "" {
			takeIndex = r.eval.put(index, value.Number(float64(i)))
		}
		err := r.element(n, inline)
		takeIndex()
		takeItem()
		if err != nil {
			return err
		}
	}
	return nil
}

// element draws one element: its if, a piece of the site, a component or a tag of Kitwork.
func (r *renderer) element(n *Node, inline bool) error {
	r.keptLint(n) // how it is written: house style the page reads all the same (lint.go), read once a node
	if a, ok := n.Attribute("if"); ok {
		if err := r.dialogUnread(n, &a, a.Value, "if"); err != nil {
			return err
		}
		shown, err := r.value(n, &a, a.Value)
		if err != nil {
			return err
		}
		_, live, err := r.liveCondition(n, &a)
		if err != nil {
			return err
		}
		if !shown.ToBoolean() && !live {
			return nil // a condition of the page in the browser is drawn, hidden while false (opening)
		}
	}
	if strings.Contains(n.Tag, "-") {
		if r.defined(n.Tag) {
			return r.component(n, inline)
		}
		return r.piece(n, inline)
	}
	r.requireName(n) // a button, a link, a badge a screen reader can name
	r.lintPlace(n)
	r.lintRegion(n)
	r.lintSubmit(n)  // a send button held back by an editor's empty (editor.go)
	r.lintOutside(n) // what floats out of a lazy frame around
	if _, header := r.summaries[n]; header {
		// an accordion's header: its <summary> itself, its layout with it (accordion.go)
		kind := kindInline
		if known, ok := catalog[n.Tag]; ok {
			kind = known.kind
		}
		return r.tag(n, "summary", kind)
	}
	if r.autocomplete != nil {
		switch n.Tag {
		case "option":
			return r.autocompleteOption(n) // a choice of the listbox (autocomplete.go)
		case "empty":
			r.hosts[n] = &host{attributes: []string{"hidden"}} // shown by the component when nothing matches
			return r.tag(n, "div", kindContainer)
		}
	}
	switch n.Tag {
	case "avatar":
		return r.avatarElement(n)
	case "avatars":
		return r.avatarsElement(n)
	case "accordion":
		return r.accordionElement(n)
	case "accordions":
		return r.accordionsElement(n)
	}
	if n.Tag == "text" {
		htmlTag := r.textElement(n, inline)
		if a, ok := n.Attribute("wrap"); ok && strings.TrimSpace(a.Value) == "false" && htmlTag != "span" {
			// a whole paragraph or heading already stands on its own lines: nothing to keep together; drawn as written
			r.warn(n, &a, "wrap=\"false\" giữ một cụm chữ trong câu liền nhau: đặt trên <text> nằm trong một <text> khác (đoạn văn hay tiêu đề đã đứng riêng dòng nên ở đây không có gì để giữ liền)")
		}
		return r.tag(n, htmlTag, kindInline)
	}
	if n.Tag == "code" {
		return r.code(n, inline)
	}
	if r.editor != nil && n == r.editor.area {
		return r.editorArea(n) // the editor's writing area and the field the form posts (editor.go)
	}
	if n.Tag == "breadcrumb" {
		return r.breadcrumbElement(n) // a <nav> holding the trail, then its data (breadcrumb.go)
	}
	if known, ok := catalog[n.Tag]; ok {
		return r.tag(n, known.html, known.kind)
	}
	switch n.Tag {
	case "progressbar":
		return r.progressbar(n)
	case "reader":
		return r.reader(n)
	case "richtext":
		return r.richtextElement(n, inline)
	}
	if says := refusedTags[n.Tag]; says != "" {
		return at(n, nil, "%s", says)
	}
	if _, ok := componentState[n.Tag]; ok {
		touched := r.touched // what the instance starts with read of the request: its names are not fixed (frozen.go)
		state := r.stateOf(n)
		htmlTag, kind := hostTag(n.Tag)
		drawnNode := n
		var carousel *carouselScope
		var countdown *countdownScope
		var editor *editorScope
		var autocomplete *autocompleteScope
		// the component's own words read off its host, what it adds to the element, what its instance starts with
		// (notices.go, terminal.go, tabs.go, carousel.go, countdown.go, editor.go)
		var own *host
		var err error
		switch n.Tag {
		case "network":
			drawnNode, own, err = r.network(n)
			if err == nil && len(own.classes) > 1 && own.classes[0] == "network" {
				r.lintFixed(n, `<network place="`+strings.TrimPrefix(own.classes[1], "network-")+`">`) // fixed to the screen
			}
		case "toast":
			drawnNode, own, err = r.toast(n)
			if err == nil {
				r.lintFixed(n, "<toast>")
				if _, named := n.Attribute("as"); !named {
					r.warn(n, nil, "%s", `toast không có as: trang không gọi được show; viết <toast as="$toast"> rồi gọi $toast.success('Đã lưu.') ở bất cứ đâu`)
				}
			}
		case "terminal":
			drawnNode, own, err = r.terminalHost(n)
		case "tabs":
			drawnNode, own, err = r.tabsHost(n)
		case "carousel":
			starts := make(map[string]any, len(state))
			for name, initial := range state {
				starts[name] = initial
			}
			state = starts
			drawnNode, own, carousel, err = r.carouselHost(n, state)
		case "countdown":
			starts := make(map[string]any, len(state))
			for name, initial := range state {
				starts[name] = initial
			}
			state = starts
			drawnNode, own, countdown, err = r.countdownHost(n, state)
		case "editor":
			drawnNode, own, editor, err = r.editorHost(n, state, inline)
		case "dialog":
			drawnNode, own, err = r.dialogHost(n)
		case "autocomplete":
			drawnNode, own, autocomplete, err = r.autocompleteHost(n)
		}
		if err != nil {
			return err
		}
		if own != nil {
			r.hosts[drawnNode] = own
		}
		var takeBack []func()
		for name, initial := range state {
			takeBack = append(takeBack, r.eval.put(name, stateValue(initial)))
		}
		r.frames = append(r.frames, &frame{component: n.Tag, names: state, fixed: r.touched == touched})
		outerCarousel, outerCountdown, outerEditor, outerAutocomplete := r.carousel, r.countdown, r.editor, r.autocomplete
		if autocomplete != nil {
			r.autocomplete = autocomplete
		}
		if carousel != nil {
			r.carousel = carousel
		}
		if countdown != nil {
			r.countdown = countdown
		}
		if editor != nil {
			r.editor = editor // kept inside the components nested in it: a toolbar's dropdown is its too
		}
		outerTerminal := r.terminal
		var commands *terminalScope
		if n.Tag == "terminal" {
			commands = &terminalScope{}
		}
		r.terminal = commands // a component nested in a terminal: its code is its own
		err = r.tag(drawnNode, htmlTag, kind)
		r.terminal, r.carousel, r.countdown, r.editor, r.autocomplete = outerTerminal, outerCarousel, outerCountdown, outerEditor, outerAutocomplete
		if err == nil && commands != nil && commands.commands == 0 {
			err = at(n, nil, "terminal cần ít nhất một lệnh: một <code> bên trong (mỗi <code> trong terminal là một lệnh, trừ <code> của một component lồng bên trong)")
		}
		r.frames = r.frames[:len(r.frames)-1]
		for i := len(takeBack) - 1; i >= 0; i-- {
			takeBack[i]()
		}
		return err
	}
	return at(n, nil, "thẻ <%s> không có trong từ vựng của Kitwork; thẻ của site có gạch ngang (<%s-card />)", n.Tag, n.Tag)
}

// textElement is what a <text> is: h1…h6 for heading="N", <s> struck through, a <span> in a line and a <p>
// standing on its own.
func (r *renderer) textElement(n *Node, inline bool) string {
	if a, ok := n.Attribute("heading"); ok {
		if level, err := strconv.Atoi(a.Value); err == nil && level >= 1 && level <= 6 {
			return "h" + a.Value
		}
	}
	if a, ok := n.Attribute("strikethrough"); ok && isTrue(a) {
		return "s"
	}
	if inline {
		return "span"
	}
	return "p"
}

// sharedTags are the tags whose HTML element other tags have too: the tag's own word is a class, so the reboot
// draws a card as a card and not as any <div> (KITWORK_UI mục 7).
var sharedTags = map[string]bool{"card": true, "badge": true, "popover": true, "field": true, "hint": true, "error": true, "richtext": true}

// engineWords are the classes an element has before any attribute, each drawn by the reboot: the tag's word
// where its element is shared, then, when flex is not written, how it lays out what it holds (a container a
// column, a grid its grid, a button, a badge and a link holding elements a row). Without them gap and align
// have no box to act on, and a grid's columns none to divide.
func engineWords(n *Node, kind tagKind) []string {
	if n.Tag == "dialog" {
		// the screen, see-through, what it holds placed against it (reboot.css): no direction, and never relative,
		// which would take it out of the top layer's fixed frame
		return nil
	}
	var words []string
	if sharedTags[n.Tag] {
		words = append(words, n.Tag)
	}
	_, flex := n.Attribute("flex")
	switch {
	case flex:
	case n.Tag == "grid":
		words = append(words, "grid")
	case kind == kindContainer:
		words = append(words, "flex-column")
	case n.Tag == "button", n.Tag == "appearance", n.Tag == "badge", n.Tag == "link" && len(n.Elements()) > 0:
		words = append(words, "flex-row")
	}
	if containsPlaceChild(n) {
		words = append(words, "place-anchor") // what a popover or a child with place is placed against
	}
	return words
}

// htmlVoid are HTML's elements that have no closing tag.
var htmlVoid = map[string]bool{"img": true, "input": true, "br": true, "hr": true, "meta": true, "link": true}

// booleanOn says a boolean attribute (lint.go booleanAttributes) of passed is on, which HTML reads by its being
// there and the page writes bare, as the first engine did: written bare, "true", or {{ }} of a value that is true; "false" and a
// false value are off.
func (r *renderer) booleanOn(n *Node, a *Attribute) (bool, error) {
	if a.Bare {
		return true, nil
	}
	if !strings.Contains(a.Value, "{{") {
		return strings.TrimSpace(a.Value) != "false", nil
	}
	got, err := r.value(n, a, a.Value)
	if err != nil {
		return false, err
	}
	return got.ToBoolean(), nil
}

// passed are the attributes each element writes as they are, by their own name (with {{ }} in the value).
var passed = map[string]bool{
	"id": true, "role": true, "name": true, "type": true, "placeholder": true, "required": true, "readonly": true,
	"disabled": true, "min": true, "max": true, "step": true, "pattern": true, "autocomplete": true, "inputmode": true,
	"autofocus": true, "checked": true, "rows": true, "method": true, "selected": true, "datetime": true,
}

// start is what an element's opening tag writes, and what a value= puts inside it. regions are the classes of
// a <view>'s container, which are its regions' and never the <body>'s.
type start struct {
	classes, attributes, regions []string
	content                      string
	hasContent                   bool
	// textBound says the element's whole content is one value the kernel keeps (text: …)
	textBound bool
	// first is written first inside the element (a view's backdrop layer); tail after what it holds, before it
	// closes (a new tab's note)
	first, tail string
	// crumb says the element is a step of a breadcrumb's trail, crumbURL its address
	crumb    bool
	crumbURL string
}

// tag draws n as the element htmlTag: its attributes, its classes, then what it holds.
func (r *renderer) tag(n *Node, htmlTag string, kind tagKind) error {
	open, err := r.keptOpening(n, kind)
	if err != nil {
		return err
	}
	r.lintShownByWidth(n, htmlTag, open.classes) // a width that cannot show it again (lint.go)
	r.write(htmlTag, open)
	r.out.WriteString(open.first)
	if kind == kindVoid {
		// a tag that holds nothing still closes its element when HTML's is no void one: <icon /> is <i></i>,
		// or what follows it would be inside it
		if !htmlVoid[htmlTag] {
			r.out.WriteString("</" + htmlTag + ">")
		}
		r.out.WriteString(r.afterOf(n))
		return nil
	}
	switch {
	case open.hasContent:
		r.out.escape(open.content)
	case open.textBound && r.inBlueprint > 0:
		// a value of the toast's notice: the kernel writes it in each row, the blueprint holds none
	default:
		outer, outerField, outerTrail := r.container, r.field, r.breadcrumb
		if n.Tag != "item" && n.Tag != "field" && n.Tag != "accordions" {
			r.breadcrumb = false // a trail's steps are its items' own
		}
		switch {
		case n.Tag == "field":
			// the ids it gives what it holds (field.go)
			f, err := r.fieldOf(n)
			if err != nil {
				return err
			}
			r.field = f
		case r.editor != nil && n == r.editor.popover:
			r.field = nil // the address input is the editor's, never the field's control
		}
		r.container = open.regions
		r.enter(n)
		if n.Tag == "content" {
			r.inContent++
		}
		from := r.out.Len()
		// the words of a trail's item stand in a line
		err := r.children(n.Children, kind == kindInline || outerTrail && n.Tag == "item")
		if n.Tag == "content" {
			r.inContent--
			if r.inContent == 0 && err == nil {
				r.writeBlock() // the page's data, at the end of its content, which Drive carries (bridge.go)
			}
		}
		r.leave()
		r.container, r.field, r.breadcrumb = outer, outerField, outerTrail
		if err != nil {
			return err
		}
		if open.crumb && r.crumbs != nil {
			r.readScope(scopeBreadcrumb) // what a scope around gives it: drawn again (frozen.go)
			*r.crumbs = append(*r.crumbs, crumb{name: plainText(r.out.since(from)), url: open.crumbURL})
		}
	}
	r.out.WriteString(open.tail)
	if own := r.hostOf(n); own != nil {
		r.out.WriteString(own.tail) // after what the author drew (the toast's two regions a screen reader hears)
	}
	r.out.WriteString("</" + htmlTag + ">")
	r.out.WriteString(r.afterOf(n)) // a tabs pre-paint, once every tab and panel of its name is in the document
	return nil
}

// writesWidth says n writes its own width, container or max-width, at any width or state: a region that does
// keeps it, not the one its <view> gives.
func writesWidth(n *Node) bool {
	for _, a := range n.Attributes {
		if _, name := splitPrefix(a.Name); name == "container" || name == "max-width" {
			return true
		}
	}
	return false
}

// write writes the opening tag of htmlTag.
func (r *renderer) write(htmlTag string, open start) {
	r.out.WriteString("<")
	r.out.WriteString(htmlTag)
	if len(open.classes) > 0 {
		r.out.WriteString(` class="`)
		for i, class := range open.classes {
			if i > 0 {
				r.out.WriteString(" ")
			}
			r.out.escape(class)
		}
		r.out.WriteString(`"`)
	}
	for _, attribute := range open.attributes {
		r.out.WriteString(" ")
		r.out.WriteString(attribute)
	}
	r.out.WriteString(">")
}

// opening reads n's attributes into its classes (the engine's words first) and its HTML attributes.
func (r *renderer) opening(n *Node, kind tagKind) (start, error) {
	classes := engineWords(n, kind)
	if r.breadcrumb {
		r.readScope(scopeBreadcrumb) // what a scope around gives it: drawn again (frozen.go)
	}
	if r.breadcrumb && n.Tag == "item" {
		// a step of a trail lays its words and separator out in a row (breadcrumb.go)
		for i, word := range classes {
			if word == "flex-column" {
				classes[i] = "flex-row"
			}
		}
	}
	if neutralButton(n) {
		// a button whose author writes nothing of its fill or edge: translucent ink, deeper under the pointer and
		// while pressed, fainter while disabled, so it shows on any ground a page is made of
		for _, step := range neutralSteps {
			if class, err := r.table.Class(r.rules, stylesheet.Intent{State: step.state, Name: "background", Value: step.value}); err == nil {
				classes = append(classes, class)
			}
		}
	}
	var attributes, regionWidth []string
	attributes = append(attributes, r.marksOf(n)...) // a row of a list of the page in the browser (bridge.go)
	content := ""
	hasContent := false
	fail := func(err error) (start, error) { return start{}, err }
	// live are the grouped attributes the kernel reads on this element (live.go)
	var live grouped
	if _, named := n.Attribute("as"); runs(n.Tag) && (n.Tag != "accordions" || named) {
		// the host of a component the kernel runs, first on its tag: data-kitwork-component="dropdown", with its
		// alias "dropdown: $menu", how the rest of the page reaches the instance
		host := n.Tag
		if a, ok := n.Attribute("as"); ok {
			alias := strings.TrimSpace(a.Value)
			if !aliasName(alias) {
				return fail(at(n, &a, "as của <%s> là $ và một tên (as=\"$menu\")", n.Tag))
			}
			host += ": " + alias
		}
		attributes = append(attributes, `data-kitwork-component="`+html.EscapeString(host)+`"`)
		r.load(n.Tag)
	}
	if own := r.hostOf(n); own != nil {
		attributes = append(attributes, own.attributes...)
		if len(own.inputs) > 0 {
			// what the kernel seeds the instance with before its init (a toast's place and limit)
			attributes = append(attributes, `data-kitwork-scope="`+html.EscapeString(strings.Join(own.inputs, "; "))+`"`)
		}
	}
	if r.blueprint != nil && n == r.blueprint.node {
		// the toast's notice, written once: the kernel makes a row of it for each notice, keeps it by its key,
		// and marks it while it leaves
		attributes = append(attributes, `data-kitwork-for="`+html.EscapeString(r.blueprint.item+" of notices; key: "+r.blueprint.key)+`"`)
		live.binds = append(live.binds, "data-leaving: "+r.blueprint.item+".leaving")
	}
	if n.Tag == "view" && n.File != "" {
		// the frame the page stands in: Drive swaps only the content between two pages of one frame
		attributes = append(attributes, `data-kitwork-layout="`+html.EscapeString(strings.ReplaceAll(n.File, `\`, "/"))+`"`)
	}
	exact, reload, newTab := false, false, false
	if n.Tag == "link" {
		if a, ok := n.Attribute("target"); ok {
			return fail(at(n, &a, "target là chữ của web; mở ở tab mới viết new-tab=\"true\""))
		}
		for _, word := range []string{"exact", "reload", "new-tab"} {
			a, ok := n.Attribute(word)
			if !ok {
				continue
			}
			if !a.Bare && a.Value != "true" && a.Value != "false" {
				return fail(at(n, &a, "%s của link chỉ nhận \"true\" hoặc \"false\"", word))
			}
			switch word {
			case "exact":
				exact = isTrue(a)
			case "reload":
				reload = isTrue(a)
			default:
				newTab = isTrue(a)
			}
		}
	}
	tail := "" // what the element holds after what the author wrote: a new tab's note
	linked := false
	crumbed, crumbURL := false, "" // a step of a breadcrumb's trail, and its address
	if r.tabPanelOf(n) != nil {
		attributes = append(attributes, r.panelAttributes(n)...) // a tab's panel (tabs.go)
	}
	var carouselClasses []string
	dotClick := "" // a carousel's dot: the click the browser runs (carousel.go)
	if r.carousel != nil {
		more, classes, click, err := r.carouselAttributes(n, &live)
		if err != nil {
			return fail(err)
		}
		attributes, carouselClasses, dotClick = append(attributes, more...), classes, click
	}
	var countdownClasses []string
	var engineOwns map[string]bool // what the engine writes in place of the attribute written (a segment's value)
	if r.countdown != nil {
		more, classes, skip := r.countdownAttributes(n, &live)
		attributes, countdownClasses, engineOwns = append(attributes, more...), classes, skip
	}
	// a control bound to a form's field, an option of a bound select, the empty label, hint and error of a bound
	// field (form.go): what it writes in place of the author's comes first
	form, err := r.formPart(n)
	if err != nil {
		return fail(err)
	}
	if form != nil && len(form.owns) > 0 {
		owns := map[string]bool{}
		for name := range engineOwns {
			owns[name] = true
		}
		for name := range form.owns {
			owns[name] = true
		}
		engineOwns = owns
	}
	var tabAfter []string
	if r.tabButtonOf(n) != nil {
		var before []string
		before, tabAfter = r.tabAttributes(n, &live) // a tab: its id first, its role and state after its type
		attributes = append(attributes, before...)
	}
	if n.Tag == "button" || n.Tag == "appearance" {
		// a button does what its click says, never sends the form around it unless it says so
		kind := "button"
		if a, ok := n.Attribute("type"); ok {
			switch a.Value {
			case "button", "submit", "reset":
				kind = a.Value
			default:
				return fail(at(n, &a, "type của button là button, submit hoặc reset"))
			}
		}
		attributes = append(attributes, `type="`+kind+`"`)
	}
	attributes = append(attributes, tabAfter...)
	// bind keeps an attribute written with {{ }} up to date in the browser, as htmlName, when it follows a
	// component's state
	bind := func(a *Attribute, htmlName string) error {
		source, client, err := r.liveExpression(n, a, a.Value)
		if err == nil && client {
			live.binds = append(live.binds, htmlName+": "+source)
		}
		return err
	}
	named := false  // an icon has a name a screen reader says
	iconClass := "" // an icon's mask, its class written after the others
	for i := range n.Attributes {
		a := &n.Attributes[i]
		chain, name := splitPrefix(a.Name)
		switch {
		case engineOwns[a.Name]:
			continue
		case a.Name == "if" && r.page != nil:
			// a condition of the page: the kernel shows and hides it, as a show (bridge.go)
			source, inBrowser, err := r.liveCondition(n, a)
			if err != nil {
				return fail(err)
			}
			if !inBrowser {
				continue
			}
			live.binds = append(live.binds, "show: "+source)
			shown, err := r.value(n, a, a.Value)
			if err != nil {
				return fail(err)
			}
			if !shown.ToBoolean() {
				attributes = append(attributes, "hidden")
			}
			continue
		case a.Name == "for" || a.Name == "if" || a.Name == "key" || a.Name == "heading" || a.Name == "strikethrough" || a.Name == "as",
			a.Name == "language" && n.Tag == "code", a.Name == "type" && (n.Tag == "button" || n.Tag == "appearance"),
			(a.Name == "exact" || a.Name == "reload" || a.Name == "new-tab") && n.Tag == "link":
			continue
		case isEvent(a.Name):
			if a.Name == "click" && dotClick != "" {
				dot := *a
				dot.Value = dotClick
				a = &dot
			}
			if rest, header := r.summaries[n]; header && a.Name == "click" {
				// an accordion's header: toggle() is the browser's own, what follows it the page's (accordion.go)
				if rest == "" {
					continue
				}
				after := *a
				after.Value = rest
				a = &after
			}
			pairs, err := r.event(n, a)
			if err != nil {
				return fail(err)
			}
			if pairs == nil {
				r.unreadNow(a.Name) // waits for the page to run in the browser
			}
			for _, pair := range pairs {
				if strings.HasPrefix(pair, "intersect-") {
					r.intersect = true
				}
			}
			live.events = append(live.events, pairs...)
			continue
		case a.Name == "opened" && n.Tag == "accordion":
			// open or closed as the page is built: the browser's open attribute (accordion.go)
			if v := strings.TrimSpace(a.Value); !a.Bare && v != "true" && v != "false" && !strings.Contains(v, "{{") {
				return fail(at(n, a, "opened chỉ nhận \"true\", \"false\" hoặc {{ }}"))
			}
			on, err := r.booleanOn(n, a)
			if err != nil {
				return fail(err)
			}
			if on {
				attributes = append(attributes, "open")
			}
			continue
		case a.Name == "expanded" && (n.Tag == "button" || n.Tag == "appearance"):
			// what this button opens is open: aria-expanded on the web
			if v := strings.TrimSpace(a.Value); v != "true" && v != "false" && !strings.Contains(v, "{{") {
				return fail(at(n, a, "expanded chỉ nhận \"true\", \"false\" hoặc {{ }}"))
			}
			if err := bind(a, "aria-expanded"); err != nil {
				return fail(err)
			}
			written, err := r.interpolate(n, a, a.Value)
			if err != nil {
				return fail(err)
			}
			attributes = append(attributes, `aria-expanded="`+html.EscapeString(written)+`"`)
			continue
		case a.Name == "show" && a.Value != "true" && a.Value != "false":
			// show="condition", unprefixed: shown by what the page is (the hidden attribute); a prefixed
			// show (medium:show="true") is a fixed word, a class of the stylesheet. One that reads a component's
			// state the kernel keeps: show: open
			source := strings.TrimSpace(a.Value)
			if bare, ok := wrapped(source); ok {
				source = bare
			}
			if compiled, err := r.site.compile(source); err == nil {
				client, err := r.onClient(n, a, source, compiled.reads)
				if err != nil {
					return fail(err)
				}
				if client {
					live.binds = append(live.binds, "show: "+source)
				}
			}
			shown, err := r.value(n, a, a.Value)
			if err != nil {
				return fail(err)
			}
			if !shown.ToBoolean() {
				attributes = append(attributes, "hidden")
			}
			continue
		case name == "container" && n.Tag == "view":
			// the regions line up on one width while the <body>, and its background, spans the screen
			// (KITWORK_UI mục 7)
			if strings.Contains(a.Value, "{{") {
				return fail(at(n, a, "container của <view> là một chữ cố định (small, medium, large, wide, huge), không đổi theo dữ liệu"))
			}
			class, err := r.class(n, a, chain, name)
			if err != nil {
				return fail(err)
			}
			if class != "" {
				regionWidth = append(regionWidth, class)
			}
			continue
		case a.Name == "show" && a.Value == "true":
			continue // shown, as every element is: no class (a prefixed show is one, medium:show="false")
		case name == "show" || stylesheet.IsPresentation(name):
			if strings.Contains(a.Value, "{{") {
				// a class that follows a component's state: each value it can be, turned on and off by the kernel
				source, client, err := r.liveExpression(n, a, a.Value)
				if err != nil {
					return fail(err)
				}
				if client {
					pairs, err := r.classPairs(n, a, source, r.presentationClass(n, a, chain, name))
					if err != nil {
						return fail(err)
					}
					live.classes = append(live.classes, pairs...)
				}
			}
			class, err := r.class(n, a, chain, name)
			if err != nil {
				return fail(err)
			}
			if class != "" {
				classes = append(classes, class)
			}
			continue
		case chain != "" && strings.Contains(a.Name, "("), chain != "" && !stylesheet.IsPresentation(name):
			r.unreadNow(a.Name) // an event and its modifiers (input:debounce(300), keydown:escape)
			continue
		}
		// what follows a component's state, read before the server paints its first value
		liveSource, liveClient, err := r.liveExpression(n, a, a.Value)
		if err != nil {
			return fail(err)
		}
		written, err := r.interpolate(n, a, a.Value)
		if err != nil {
			return fail(err)
		}
		// inside the toast's notice a value of the notice is the kernel's to write: the blueprint holds none
		first := !(r.inBlueprint > 0 && liveClient)
		htmlName := ""
		switch {
		case a.Name == "value" && n.Tag == "link":
			htmlName = "href"
			if !safeLink(written) {
				if !strings.Contains(a.Value, "{{") {
					return fail(at(n, a, "link đi tới đường dẫn trong site (/…, ./…, #…), http:, https:, mailto: hay tel:; %q không phải chỗ đi tới", written))
				}
				first = false // from data, no place to go: the words stay, nothing to click
			}
			if first {
				attributes = append(attributes, r.linkAttributes(written, exact, newTab)...)
				linked = true
				if r.breadcrumb {
					r.readScope(scopeBreadcrumb)      // what a scope around gives it: drawn again (frozen.go)
					crumbed, crumbURL = true, written // a link in a trail's item is a step of it
				}
			}
		case a.Name == "value" && n.Tag == "icon":
			// a name the page writes is the set's or an error at its place; a name from the page's data that
			// the set does not have draws an empty icon
			if icons.Has(written) {
				iconClass = "icon-" + written // after the classes of its attributes, as the first engine writes it
				r.icons = append(r.icons, written)
			} else if !strings.Contains(a.Value, "{{") {
				return fail(at(n, a, "không có icon %q trong bộ icon của Kitwork (Tabler)%s", written, icons.Nearest(written)))
			} else if written != "" && !iconName(written) {
				r.warn(n, a, "%s", a.Name+`="`+a.Value+`" cho "`+written+`": tên icon chỉ gồm chữ thường, số và dấu gạch nối; không có class`)
			}
			if liveClient {
				// an icon that follows a component's state: the class of each name it can be
				pairs, err := r.classPairs(n, a, liveSource, r.iconClass(n, a))
				if err != nil {
					return fail(err)
				}
				live.classes = append(live.classes, pairs...)
			}
		case a.Name == "value" && n.Tag == "image":
			htmlName = "src"
			if !safeImage(written) {
				if !strings.Contains(a.Value, "{{") {
					return fail(at(n, a, "image lấy ảnh từ đường dẫn trong site (/…, ./…), http:, https: hay data:image/…; %q không phải ảnh", written))
				}
				first = false // from data, not an image: no src, the alt still says what it was
			}
			if first {
				attributes = append(attributes, `src="`+html.EscapeString(written)+`"`)
			}
		case a.Name == "value" && n.Tag == "textarea":
			content, hasContent = written, true
		case a.Name == "value" && n.Tag == "option" && r.autocomplete != nil:
			htmlName = "data-value" // a choice of an autocomplete's listbox (autocomplete.go)
			if first {
				attributes = append(attributes, `data-value="`+html.EscapeString(written)+`"`)
			}
		case a.Name == "value" && (n.Tag == "input" || n.Tag == "option" || n.Tag == "time"):
			htmlName = "value"
			if first {
				attributes = append(attributes, `value="`+html.EscapeString(written)+`"`)
			}
		case a.Name == "alt" && n.Tag == "image":
			htmlName = "alt"
			if first {
				attributes = append(attributes, `alt="`+html.EscapeString(written)+`"`)
			}
		case a.Name == "alt":
			htmlName = "aria-label"
			if n.Tag == "icon" && strings.TrimSpace(written) != "" {
				named = true
				attributes = append(attributes, `role="img"`)
			}
			if first && !(n.Tag == "icon" && strings.TrimSpace(written) == "") {
				// an icon with alt="" is decoration: hidden, with no name to read
				attributes = append(attributes, `aria-label="`+html.EscapeString(written)+`"`)
			}
		case passed[a.Name]:
			htmlName = a.Name
			if a.Name == "id" {
				r.lintAnchor(n, a)
			}
			if !first {
				break
			}
			if booleanAttributes[a.Name] {
				// there or not, as HTML reads it: disabled="false" disables, so a false value writes nothing
				on, err := r.booleanOn(n, a)
				if err != nil {
					return fail(err)
				}
				if on {
					attributes = append(attributes, a.Name)
				}
				break
			}
			if a.Bare {
				attributes = append(attributes, a.Name)
			} else {
				attributes = append(attributes, a.Name+`="`+html.EscapeString(written)+`"`)
			}
		default:
			if !strings.Contains(n.Tag, "-") && !uiWord(a.Name) {
				return fail(notAWord(n, a)) // no word of .ui: misspelt, never drawn (vocabulary.go)
			}
			r.unreadNow(a.Name)
		}
		if htmlName != "" && liveClient {
			live.binds = append(live.binds, htmlName+": "+liveSource)
		}
	}
	// after what the author wrote, what the engine writes of the editor around (editor.go) and of the field
	// around (field.go), as the first engine did
	var editorClasses []string
	if r.editor != nil {
		more, classes, err := r.editorAttributes(n, &live)
		if err != nil {
			return fail(err)
		}
		attributes, editorClasses = append(attributes, more...), classes
	}
	attributes = append(attributes, r.fieldAttributes(n)...)
	if form != nil {
		attributes = append(attributes, form.attributes...)
		if form.hasContent {
			content, hasContent = form.content, true
		}
		tail += form.tail
	}
	if newTab && linked {
		tail = newTabNote(r.english()) // a screen reader hears the link opens a new tab (links.go)
	}
	if r.breadcrumb && n.Tag == "text" && len(n.Elements()) == 0 {
		r.readScope(scopeBreadcrumb) // what a scope around gives it: drawn again (frozen.go)
		// the last step of a trail: the page viewed, not a link (breadcrumb.go)
		attributes = append(attributes, `aria-current="page"`)
		crumbed, crumbURL = true, r.path()
	}
	if n.Tag == "image" {
		if _, ok := n.Attribute("alt"); !ok {
			// the image shows as written, without an alt: a screen reader reads its file name or nothing
			r.warn(n, nil, "image cần alt (alt=\"\" nếu chỉ để trang trí)")
		}
		// an image of the site written as its path: the size its file says, so the browser keeps its room before
		// it arrives (image.go); every image loads lazily, decoded off the main thread
		if a, ok := n.Attribute("value"); ok {
			if width, height, ok := r.site.imageSize(strings.TrimSpace(a.Value)); ok {
				attributes = append(attributes, `width="`+strconv.Itoa(width)+`"`, `height="`+strconv.Itoa(height)+`"`)
			}
		}
		attributes = append(attributes, `loading="lazy"`, `decoding="async"`)
	}
	if reload {
		attributes = append(attributes, `data-kitwork-reload="true"`) // this link loads its page whole
	}
	switch n.Tag {
	case "list":
		// no markers (reboot.css): Safari drops a list's semantics without them unless it says it is one
		attributes = append(attributes, `role="list"`)
	case "empty":
		attributes = append(attributes, `role="status"`) // what a list says when it has nothing
	}
	if n.Tag == "icon" && !named {
		// an icon with no name is decoration, as alt="" is: the words beside it already say it
		attributes = append(attributes, `aria-hidden="true"`)
	}
	if r.autocomplete != nil && n == r.autocomplete.input {
		// the combobox says whether its list shows (autocomplete.go)
		attributes = append(attributes, `aria-expanded="false"`)
		live.binds = append(live.binds, "aria-expanded: opened")
	}
	if n.Tag == "popover" {
		// the floating surface of a component that opens (a dropdown's menu): closed when the page is built,
		// opened by its component in the browser while it is open (KITWORK_UI mục 13)
		owner := r.componentDeclaring("opened")
		if owner == nil {
			return fail(at(n, nil, "popover hiện khi component chứa nó mở: đặt nó trong một component có opened và close (<dropdown>…<popover>…</popover></dropdown>)"))
		}
		if a, ok := n.Attribute("show"); ok {
			if strings.TrimSpace(a.Value) != "true" {
				// left out, the popover would show what it was written to hide
				return fail(at(n, &a, "popover hiện khi component chứa nó mở; show=\"%s\" trên popover chưa được hỗ trợ (bỏ nó thì popover vẫn hiện khi mở): ẩn hay hiện phần bên trong bằng show trên một thẻ trong popover", a.Value))
			}
			r.warn(n, &a, "popover hiện khi component chứa nó mở; không viết show (trang bỏ show này)")
		}
		if _, closes := owner.names["close"]; closes {
			live.binds = append(live.binds, "show: opened")
		}
		attributes = append(attributes, "hidden")
	}
	textBound := false
	if n.Tag != "code" {
		// an element whose whole content is one {{ }} that follows a component's state
		text, err := r.textPair(n)
		if err != nil {
			return fail(err)
		}
		if text != "" {
			live.binds = append(live.binds, text)
			textBound = true
		}
	}
	if !live.empty() {
		attributes = append(attributes, live.attributes()...)
		r.live = true // what the kernel reads: the kernel loads (a part keeps it, frozen.go)
	}
	if regions[n.Tag] && n.Tag != "overlay" && !writesWidth(n) {
		// overlay is a layer over the screen, not a band of the page: what it holds floats, so it is not capped
		classes = append(classes, r.container...)
	}
	if own := r.hostOf(n); own != nil {
		classes = append(classes, own.classes...) // a toast's stack and place, a network's
	}
	if iconClass != "" {
		classes = append(classes, iconClass)
	}
	classes = append(classes, carouselClasses...)  // a carousel's track
	classes = append(classes, countdownClasses...) // a countdown's time left
	classes = append(classes, editorClasses...)    // an editor's toolbar, group, popover, address
	// a view whose background paints a gradient: its background on the backdrop layer, first in <body> (backdrop.go)
	classes, backdrop, err := r.viewBackdrop(n, classes)
	if err != nil {
		return fail(err)
	}
	return start{classes: classes, attributes: attributes, regions: regionWidth, content: content, hasContent: hasContent, textBound: textBound, tail: tail, first: backdrop, crumb: crumbed, crumbURL: crumbURL}, nil
}

// class is the class of one presentation attribute, read by the stylesheet against the page's palette and
// scales; its rule goes into the page's CSS. A value with {{ }} is read as it renders.
func (r *renderer) class(n *Node, a *Attribute, chain, name string) (string, error) {
	when, err := r.prefixOf(n, a, chain, name)
	if err != nil {
		return "", err
	}
	if !when.keep {
		return "", nil
	}
	written := "true" // a true-or-false word written bare (lint.go)
	if !a.Bare {
		if written, err = r.interpolate(n, a, a.Value); err != nil {
			return "", err
		}
	}
	intentTag := n.Tag
	if n.Tag == "text" {
		if heading, ok := n.Attribute("heading"); ok && heading.Value != "" {
			intentTag = "heading"
		}
	}
	intent := stylesheet.Intent{
		Width: strings.Join(when.widths, ","), State: strings.Join(when.states, ","),
		Tag: intentTag, Name: name, Value: strings.TrimSpace(written),
	}
	if r.site.fromData(a.Value) {
		// a value from data is one the attribute takes, a token of the palette or a step of its scale, or no class:
		// data never reaches the CSS, and the author hears of a value outside while developing
		if intent.Value == "" {
			return "", nil // no value is no class
		}
		every := intent
		every.Value = ""
		domain := r.site.domain(r.table, every)
		if _, takes := domain[intent.Value]; !takes {
			values := make([]string, 0, len(domain))
			for value := range domain {
				values = append(values, value)
			}
			sort.Strings(values)
			r.warn(n, a, "%s", a.Name+`="`+a.Value+`" cho "`+written+`": không phải giá trị `+name+` nhận; không có class. Nhận: `+strings.Join(values, ", "))
			return "", nil
		}
	}
	class, warnings, err := r.site.readClass(r.table, r.rules, intent)
	if err != nil {
		return "", at(n, a, "%s", err.Error())
	}
	// what the stylesheet only warns of (columns="13", padding="17px", fit on a box): drawn as written
	for _, w := range warnings {
		r.warn(n, a, "%s", w.Message)
	}
	r.lintWidths(n, a, when.widths, class, name)
	if message := misplaced(n.Tag, name); message != "" {
		r.warn(n, a, "%s", message)
	}
	return class, nil
}

// piece draws a piece of the site, a call of its tag: its file found from the folder of the file that calls
// it up to the site's root; each attribute of the call but its for, if
// and show is an input the piece reads, its value read where the call stands. The file's root renders as it
// is, with the call's show on it.
func (r *renderer) piece(n *Node, inline bool) error {
	folder := filepath.Join(r.site.Root, filepath.Dir(filepath.FromSlash(n.File)))
	file := r.site.find(folder, n.Tag)
	if file == "" {
		return at(n, nil, "không tìm thấy %s.ui (tìm từ thư mục của file gọi lên gốc site; một mảnh nhiều site dùng chung là giao diện của một component, cạnh file khai nó)", n.Tag)
	}
	for _, open := range r.open {
		if open == file {
			return at(n, nil, "<%s /> gọi %s bên trong chính nó", n.Tag, r.site.relative(file))
		}
	}
	root, err := r.site.shared(file) // the file's own tree, never changed: the call's show goes on a copy of its root
	if err != nil {
		return err
	}
	own := false // the root is a copy of the file's
	if root.Tag == n.Tag {
		// a layout whose root is its own tag (the interface a component uses again, drawn here with no logic): the
		// root is the box it builds, never a call of itself
		boxed := *root
		boxed.Tag, boxed.source = "box", nil
		root, own = &boxed, true
	}
	var takeBack []func()
	defer func() {
		for i := len(takeBack) - 1; i >= 0; i-- {
			takeBack[i]()
		}
	}()
	for i := range n.Attributes {
		a := &n.Attributes[i]
		_, name := splitPrefix(a.Name)
		switch {
		case a.Name == "for" || a.Name == "if" || a.Name == "key":
			continue
		case name == "show":
			if !own {
				copied := *root
				root, own = &copied, true
			}
			root.Attributes = append(slices.Clip(root.Attributes), *a) // a slice of its own, the file's untouched
			root.source = nil                                          // the call's show is on it, not on its file's root (frozen.go)
			continue
		case a.Name == "as":
			// it names nothing, so it does nothing, and the page renders as written; a mistyped alias is still an error
			alias := strings.TrimSpace(a.Value)
			if !aliasName(alias) {
				return at(n, a, "as là $ và một tên bắt đầu bằng chữ (as=\"$account\"), không phải %q", a.Value)
			}
			r.warn(n, a, "as đặt tên cho một instance; %s là layout (chỉ có giao diện, không có logic) nên không có instance để gọi: bỏ as", n.Tag)
			continue
		case !identifier(a.Name):
			return at(n, a, "đầu vào %s của <%s /> không phải một tên: viết chữ, số và _ (product=\"{{ featured }}\")", a.Name, n.Tag)
		}
		input, err := r.input(n, a)
		if err != nil {
			return err
		}
		takeBack = append(takeBack, r.eval.put(a.Name, input))
	}
	if parent := r.parent(); root.Tag == "reader" && parent != nil && parent.Tag == "view" {
		r.warn(root, nil, "gốc của %s là <reader>: reader đặt thẳng trong view.ui, một lần", r.site.relative(file))
	}
	r.open = append(r.open, file)
	err = r.element(root, inline)
	r.open = r.open[:len(r.open)-1]
	return err
}

// input is the value a call passes a piece: what one {{ }} filling the whole value is (an object stays an
// object), else the text the value makes.
func (r *renderer) input(n *Node, a *Attribute) (value.Value, error) {
	if bare, ok := wrapped(strings.TrimSpace(a.Value)); ok {
		return r.value(n, a, bare)
	}
	written, err := r.interpolate(n, a, a.Value)
	if err != nil {
		return value.Value{}, err
	}
	return value.String(written), nil
}

// value is what source, an expression (if="…", the list of a for, a {{ }} filling a value), is now.
func (r *renderer) value(n *Node, a *Attribute, source string) (value.Value, error) {
	source = strings.TrimSpace(source)
	if bare, ok := wrapped(source); ok {
		source = bare
	}
	compiled, err := r.site.compile(source)
	if err != nil {
		return value.Value{}, at(n, a, "biểu thức %q: %v", source, err)
	}
	if r.dialogValue(compiled.reads) {
		return value.Value{}, nil // what open(x) gives, in the browser: the server draws the dialog closed (dialog.go)
	}
	given := !r.fromFrames(compiled)
	if given {
		r.touched++ // what the page is given: the part being drawn is the request's (frozen.go)
	}
	result, err := r.eval.evaluate(compiled)
	if err != nil {
		failure := at(n, a, "%v", err)
		if failed, ok := failure.(*Error); ok && errors.Is(err, ErrBusy) {
			failed.Busy = true // the host is busy, not the page wrong: a 503 (host page.go)
		}
		return value.Value{}, failure
	}
	if given && r.reading != nil {
		r.reading.reads = append(r.reading.reads, readValue{expression: compiled, result: result})
	}
	return result, nil
}

// interpolate is text with each {{ }} in it replaced by what it is: nothing for undefined and null
// (KITWORK_LANG mục 3), the value's text for the rest.
func (r *renderer) interpolate(n *Node, a *Attribute, text string) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	var b strings.Builder
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			b.WriteString(text)
			return b.String(), nil
		}
		b.WriteString(text[:start])
		end := closingBraces(text[start+2:])
		if end < 0 {
			return "", at(n, a, "biểu thức {{ chưa được đóng bằng }}")
		}
		result, err := r.value(n, a, text[start+2:start+2+end])
		if err != nil {
			return "", err
		}
		if result.IsFunction() {
			// a function has no text (value.ToString, 18/11/2026: the page wrote "function")
			return "", at(n, a, "{{ %s }} là một hàm, không hiện được thành chữ: gọi nó, hay hiện tên nó", strings.TrimSpace(text[start+2:start+2+end]))
		}
		if result.IsTime() {
			// a time has no text (value.ToString, 18/11/2026: the page wrote RFC 3339 in UTC)
			return "", at(n, a, "{{ %s }} là một thời gian, không hiện được thành chữ: viết rõ dạng, .toISOString() hay .format(\"02/01/2006 15:04\")", strings.TrimSpace(text[start+2:start+2+end]))
		}
		if !result.IsNullish() {
			b.WriteString(result.ToString().Text())
		}
		text = text[start+2+end+2:]
	}
}

// wrapped is what one {{ }} holds when it is the whole of s.
func wrapped(s string) (string, bool) {
	if !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return "", false
	}
	if end := closingBraces(s[2:]); end != len(s)-4 {
		return "", false
	}
	return strings.TrimSpace(s[2 : len(s)-2]), true
}

// identifier reports a name a program can read: letters, digits, _ and $, not starting with a digit.
func identifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// Unread are the attributes the last render met that engine2 does not draw yet, sorted.
func (r *renderer) Unread() []string {
	out := make([]string, 0, len(r.unread))
	for name := range r.unread {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// aliases gives the page every alias its components are written with (as="$menu"), each its component's state
// as it is before anything happened: a page reads one anywhere ({{ $menu.opened }}), the nearest of a name
// winning (KITWORK_UI mục 13). It walks the tree as assembled; a piece's own aliases wait for slice 3.
func (r *renderer) aliases(n *Node) []func() {
	var takeBack []func()
	for _, host := range r.site.aliasHosts(n) {
		alias, _ := host.Attribute("as")
		takeBack = append(takeBack, r.eval.put(alias.Value, stateObject(r.stateOf(host))))
		r.aliasOf[strings.TrimSpace(alias.Value)] = host.Tag
	}
	return takeBack
}

// aliasHostsOf are the components of the tree under n written with an alias (as="$menu"), in the order of the tree.
func aliasHostsOf(n *Node) []*Node {
	var hosts []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if _, ok := componentState[n.Tag]; ok || n.Tag == "accordions" {
			if alias, ok := n.Attribute("as"); ok && strings.HasPrefix(alias.Value, "$") {
				hosts = append(hosts, n)
			}
		}
		for _, child := range n.Children {
			if !child.IsText() {
				walk(child)
			}
		}
	}
	walk(n)
	return hosts
}
