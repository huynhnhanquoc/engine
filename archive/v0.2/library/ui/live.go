package ui

import (
	"html"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/component"
	"github.com/kitwork/engine2/library/ui/icons"
	"github.com/kitwork/engine2/library/ui/kernel"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// Inside one of Kitwork's components the page is live in the browser before the page itself is: the component's
// state and methods are its file's (library/component), so what reads only the component's names runs there. The
// server writes it in the grouped form the kernel reads (KITWORK_UI mục 11), moved from the first engine with the
// kernel: data-kitwork-component on the host, "dropdown: $menu" with its alias; data-kitwork-event for its events
// (click: toggle(); keydown.escape: close()); data-kitwork-bind for the values that follow its state (show: opened,
// aria-expanded: opened, text: …); data-kitwork-class for a class one of its names turns on and off. The server
// still paints the first frame, with the component's names as they are before anything happened. What reads only
// the page's data stays as the server printed it; one expression reading both is an error, the page running in
// the browser being a later step.

// runs says the kernel runs a component of tag in the browser: every component of Kitwork but richtext, which the
// server draws whole (to the page's checks a component all the same: nothing inside it is another's), and a group of
// accordions written with an alias, whose close() is its file's (accordion.go; not a component around what it
// holds). The server draws each host and what it holds, as the first engine did, for the kernel to run.
func runs(tag string) bool {
	_, component := componentState[tag]
	return component && tag != "richtext" || tag == "accordions"
}

// hostTag is the element a component's tag builds: a button for appearance, what the visitor presses; the
// browser's <dialog> for a dialog, which the kernel opens modal; a box for the others, which hold what the author
// draws inside (a dropdown's trigger and popover).
func hostTag(name string) (string, tagKind) {
	switch name {
	case "appearance":
		return "button", kindInline
	case "dialog":
		return "dialog", kindContainer
	}
	return "div", kindContainer
}

// frame is one component around what is being drawn: its name and the names it declares.
type frame struct {
	component string
	names     map[string]any
	// kind is whose names they are: a component of Kitwork's, the page's, a site component's (bridge.go)
	kind string
	// fixed says the names start the same on every page: what the instance starts with read nothing of the request
	// (an editor's words are its value's, which may be the page's data): an expression reading only them is fixed
	fixed bool
}

// declaring is the nearest component around that declares name, nil when none does.
func (r *renderer) declaring(name string) *frame {
	for i := len(r.frames) - 1; i >= 0; i-- {
		if _, ok := r.frames[i].names[name]; ok {
			return r.frames[i]
		}
	}
	return nil
}

// around is the nearest component around what is being drawn, nil outside every one.
func (r *renderer) around() *frame {
	if len(r.frames) == 0 {
		return nil
	}
	return r.frames[len(r.frames)-1]
}

// within says where an expression stands, for a message.
func (r *renderer) within() string {
	if owner := r.around(); owner != nil {
		return "bên trong " + owner.component
	}
	return "ngoài mọi component"
}

// reservedAliases are the kernel's own handles: a component's alias is never one.
var reservedAliases = map[string]bool{"$this": true, "$el": true, "$host": true, "$root": true, "$event": true, "$element": true}

// aliasName says s is a component's alias: $, a letter, then letters, digits and _.
func aliasName(s string) bool {
	if len(s) < 2 || s[0] != '$' || reservedAliases[s] {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 1 && (c >= '0' && c <= '9' || c == '_'):
		default:
			return false
		}
	}
	return true
}

// onClient says whether an expression reading names runs in the browser: it reads the names of a component the
// kernel runs, or a component through its alias ($toast.success('Saved.')), and nothing only the server knows
// (the page's data, a row of a for over it). One that reads a live component's names and the page's is an error;
// one that reads an alias and the page's stays the server's.
func (r *renderer) onClient(n *Node, a *Attribute, source string, names []string) (bool, error) {
	own, alias := false, false
	var page []string
	for _, name := range names {
		r.lintShadow(n, a, name) // a name an outer component declares as well (lint.go)
		if name == "open" {
			if owner := r.componentDeclaring("opened"); owner != nil && compiler.ReadsAsValue(source, name) {
				// the state's name until 26/10/2026: a verb is a function, a participle a state
				return false, at(n, a, "%s %s dùng open như một giá trị; trạng thái mở của %s là opened%s",
					r.within(), source, owner.component, openedHint(owner))
			}
		}
		switch owner := r.declaring(name); {
		case owner != nil && owner.kind != frameSite:
			own = true
		case aliasName(name) && runs(r.aliasOf[name]):
			alias = true
			if r.aliasOf[name] == "dialog" {
				if err := dialogCall(n, a, source, name); err != nil {
					return false, err
				}
			}
			if _, opens := componentState[r.aliasOf[name]]["opened"]; opens && compiler.ReadsAsValue(source, name+".open") {
				return false, at(n, a, "%s %s dùng %s.open như một giá trị; trạng thái mở là %s.opened", r.within(), source, name, name)
			}
		case strings.HasPrefix(name, "$") && name != "$":
			// $event and the kernel's other handles, known in the browser
		default:
			if says := browserDialogs[name]; says != "" && r.eval.given[name] == 0 {
				return false, at(n, a, "%s", says)
			}
			page = append(page, name)
		}
	}
	if !own && !alias {
		return false, nil
	}
	if len(page) > 0 {
		if !own {
			return false, nil
		}
		sort.Strings(page)
		return false, at(n, a, "%s biểu thức %s đọc cả tên của component lẫn %s của trang; phần chạy ở trình duyệt mới chỉ có tên của component",
			r.within(), source, strings.Join(page, ", "))
	}
	return true, nil
}

// openedHint says what open is in a component that has opened: a dropdown's function that opens it, nothing of
// a network's or an editor's.
func openedHint(owner *frame) string {
	if _, function := owner.names["open"]; function {
		return ", open() là hàm mở nó"
	}
	return ""
}

// events are the events of .ui by their name, the DOM event the kernel listens for each ("" for one it raises
// itself).
var events = map[string]string{
	"click": "click", "double-click": "dblclick", "context-menu": "contextmenu", "click-outside": "click",
	"keydown": "keydown", "keyup": "keyup", "input": "input", "change": "change", "submit": "submit",
	"focus-in": "focusin", "focus-out": "focusout",
	"pointer-down": "pointerdown", "pointer-up": "pointerup", "pointer-enter": "pointerenter", "pointer-leave": "pointerleave",
	"scroll": "scroll", "load": "load", "error": "error", "toggle": "toggle",
	"intersect-enter": "", "intersect-leave": "", "ready": "",
	"offline": "", "online": "", "finish": "",
}

// componentEvents are the events a component of Kitwork sends, each heard on its host only: the network's
// offline and online, the countdown's finish.
var componentEvents = map[string]string{"offline": "network", "online": "network", "finish": "countdown"}

// isEvent says an attribute is an event: its name, before the ':' of its modifiers, one event or a list of them
// (pointer-enter,focus-in:once).
func isEvent(name string) bool {
	head, _, _ := strings.Cut(name, ":")
	for {
		each, rest, more := strings.Cut(head, ",")
		if _, ok := events[strings.TrimSpace(each)]; !ok {
			return false
		}
		if !more {
			return true
		}
		head = rest
	}
}

// event is the data-kitwork-event pairs of an event attribute, nil when it waits for the page to run in the
// browser (an event outside every component that reads no alias). The ':' of its .ui name is '.' on the wire
// (keydown:escape is keydown.escape), and a list of events is one pair each. A modifier the kernel would refuse is
// an error where it is written, inside a component or not: a typo never vanishes.
func (r *renderer) event(n *Node, a *Attribute) ([]string, error) {
	source := strings.TrimSpace(a.Value)
	if bare, ok := wrapped(source); ok {
		source = bare
	}
	names, err := r.site.handler(source)
	if err != nil {
		return nil, at(n, a, "%s=%q: %v", a.Name, source, err)
	}
	head, modifiers, _ := strings.Cut(a.Name, ":")
	for _, each := range strings.Split(head, ",") {
		if problem := modifierProblem(strings.TrimSpace(each), modifiers); problem != "" {
			return nil, at(n, a, "%s: %s", a.Name, problem)
		}
		if owner := componentEvents[strings.TrimSpace(each)]; owner != "" {
			if n.Tag != owner {
				written, end := strings.TrimSpace(each), " />"
				if modifiers != "" {
					written += ":" + modifiers
				}
				if owner == "countdown" {
					end = ">"
				}
				return nil, at(n, a, "%s là sự kiện của component %s, chỉ thẻ của nó nghe được: <%s %s=\"…\"%s", strings.TrimSpace(each), owner, owner, written, end)
			}
		}
	}
	client, err := r.onClient(n, a, source, names)
	if err != nil {
		return nil, err
	}
	if !client {
		if owner := r.around(); owner != nil && owner.kind == frameKitwork {
			// a component of Kitwork's: its events are its own; the page's and a site component's that read
			// nothing they keep are the server's, unread
			return nil, at(n, a, "bên trong %s sự kiện %s chỉ gọi tên của component (%s); sự kiện của trang chạy ở trình duyệt là bước sau",
				owner.component, a.Name, strings.Join(sortedKeys(owner.names), ", "))
		}
		return nil, nil
	}
	if n.Tag == "form" && head == "submit" && r.page != nil && !contains(splitModifiers(modifiers), "prevent") {
		// a form that calls a work of the page stays on the page: the browser does not send it
		if modifiers == "" {
			modifiers = "prevent"
		} else {
			modifiers += ".prevent"
		}
	}
	var pairs []string
	for _, each := range strings.Split(head, ",") {
		name := strings.TrimSpace(each)
		if modifiers != "" {
			name += "." + modifiers
		}
		pairs = append(pairs, name+": "+source)
	}
	return pairs, nil
}

// liveExpression is the expression a value written with {{ }} becomes in the browser, and whether it runs there:
// one {{ }} filling the value is its expression; a value with more than one part, or text around it, that reads a
// live component's names is an error for now.
func (r *renderer) liveExpression(n *Node, a *Attribute, written string) (string, bool, error) {
	if !strings.Contains(written, "{{") {
		return "", false, nil
	}
	source, whole := wrapped(strings.TrimSpace(written))
	if !whole {
		// more than one part, or text around one: the parts in a list joined with no separator, as the server
		// prints them ([(minutes), ':', (seconds)].join('')), so a number and a boolean are written, never added
		var pieces, names []string
		rest := written
		for {
			start := strings.Index(rest, "{{")
			if start < 0 {
				if rest != "" {
					pieces = append(pieces, quote(rest))
				}
				break
			}
			if start > 0 {
				pieces = append(pieces, quote(rest[:start]))
			}
			end := closingBraces(rest[start+2:])
			if end < 0 {
				return "", false, nil // the interpolation's own error, where it is drawn
			}
			part := strings.TrimSpace(rest[start+2 : start+2+end])
			compiled, err := r.site.compile(part)
			if err != nil {
				return "", false, nil
			}
			names = append(names, compiled.reads...)
			pieces = append(pieces, "("+part+")")
			rest = rest[start+2+end+2:]
		}
		joined := "[" + strings.Join(pieces, ", ") + "].join('')"
		client, err := r.onClient(n, a, joined, names)
		return joined, client, err
	}
	compiled, err := r.site.compile(source)
	if err != nil {
		return "", false, at(n, a, "biểu thức %q: %v", source, err)
	}
	client, err := r.onClient(n, a, source, compiled.reads)
	return source, client, err
}

// textPair is the bind pair of an element whose whole content is one {{ }} that follows a component's state
// ({{ connected ? 'Back online' : 'You are offline' }}), "" for any other. The words around a {{ }} are the ones a
// page shows: each run of white space one space, none at the ends, as the first engine wrote them.
func (r *renderer) textPair(n *Node) (string, error) {
	if len(n.Children) != 1 || !n.Children[0].IsText() {
		return "", nil
	}
	source, client, err := r.liveExpression(n, nil, strings.Trim(collapseSpace(n.Children[0].Text), " "))
	if err != nil || !client {
		return "", err
	}
	return "text: " + source, nil
}

// classPairs are the data-kitwork-class pairs of a class that follows a component's state: written out once for
// every value its expression can be, as `<the class>: <when it is on>`, the kernel turning each on and off. Each
// value's class is one the page's stylesheet holds (an icon's mask too), so the browser has what it turns on.
// classOf makes the class of one value.
func (r *renderer) classPairs(n *Node, a *Attribute, source string, classOf func(string) (string, error)) ([]string, error) {
	values, ok := compiler.ExpressionStrings(source)
	if !ok {
		return nil, at(n, a, "%s %s=\"%s\" đổi theo trạng thái, nên phải viết ra mọi giá trị (vd. {{ opened ? 'x' : 'y' }})", r.within(), a.Name, a.Value)
	}
	sort.Strings(values)
	var order []string
	when := map[string][]string{}
	for _, each := range values {
		class, err := classOf(each)
		if err != nil {
			return nil, err
		}
		if class == "" {
			continue
		}
		if !classPairName(class) {
			return nil, at(n, a, "%s class %s đổi theo trạng thái, nhưng tên có tiền tố (hover:…) hay ký tự ([ ]) nên trình duyệt chưa bật/tắt được", r.within(), class)
		}
		if _, seen := when[class]; !seen {
			order = append(order, class)
		}
		when[class] = append(when[class], "("+source+") == "+quote(each))
	}
	pairs := make([]string, 0, len(order))
	for _, class := range order {
		pairs = append(pairs, class+": "+strings.Join(when[class], " || "))
	}
	return pairs, nil
}

// iconClass is the class of an icon named value, its mask in the page's stylesheet; an error for a name the set
// does not have.
func (r *renderer) iconClass(n *Node, a *Attribute) func(string) (string, error) {
	return func(value string) (string, error) {
		if !icons.Has(value) {
			return "", at(n, a, "không có icon %q trong bộ icon của Kitwork (Tabler)%s", value, icons.Nearest(value))
		}
		r.icons = append(r.icons, value)
		return "icon-" + value, nil
	}
}

// presentationClass is the class of a presentation attribute at one value, its rule in the page's stylesheet.
func (r *renderer) presentationClass(n *Node, a *Attribute, chain, name string) func(string) (string, error) {
	return func(value string) (string, error) {
		copied := *a
		copied.Value = value
		return r.class(n, &copied, chain, name)
	}
}

// classPairName says a class can be the name of a data-kitwork-class pair: the grouped form cuts a pair at its first
// ':', so a name is letters, digits, '-', '.', '_', '+', '/' and flat parentheses (hover:background-brand cannot be).
func classPairName(class string) bool {
	if class == "" {
		return false
	}
	open := false
	for i := 0; i < len(class); i++ {
		switch c := class[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '+', c == '/':
		case c == '(' && !open:
			open = true
		case c == ')' && open:
			open = false
		default:
			return false
		}
	}
	return !open
}

// collapseSpace is s with each run of spaces, tabs and line ends one space.
func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(c)
	}
	return b.String()
}

// quote is s as a string of an expression in markup: single quotes.
func quote(s string) string {
	return "'" + quoter.Replace(s) + "'"
}

// quoter writes a string inside a quote of the kernel's expressions.
var quoter = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)

// grouped are the grouped attributes of one element, in the order they sit on its tag.
type grouped struct {
	binds, classes, events []string
}

func (p grouped) empty() bool { return len(p.binds)+len(p.classes)+len(p.events) == 0 }

// attributes are p as HTML attributes: data-kitwork-bind, data-kitwork-class, data-kitwork-event.
func (p grouped) attributes() []string {
	var out []string
	for _, group := range []struct {
		name  string
		pairs []string
	}{{"data-kitwork-bind", p.binds}, {"data-kitwork-class", p.classes}, {"data-kitwork-event", p.events}} {
		if len(group.pairs) > 0 {
			out = append(out, group.name+`="`+html.EscapeString(strings.Join(group.pairs, "; "))+`"`)
		}
	}
	return out
}

// containsPlaceChild says n holds an element placed against it (a popover, a child with place): n is then the
// anchor of what is placed, the class place-anchor.
func containsPlaceChild(n *Node) bool {
	for _, child := range n.Elements() {
		switch child.Tag {
		case "popover":
			return true
		case "fragment":
			if containsPlaceChild(child) {
				return true
			}
			continue
		case "reader", "toast", "network":
			continue // placed against an edge of the screen, not against what holds it
		}
		for _, a := range child.Attributes {
			if _, name := splitPrefix(a.Name); name == "place" {
				return true
			}
		}
	}
	return false
}

// Scripts are the files a page drawn by r loads, in the order it runs them: the scrolled flag its scrolled:
// classes read, the kernel when it holds something the kernel reads or its site moves without a reload, Drive on
// such a site, the bridge for a page that runs in the browser, the observer of intersect-enter and intersect-leave
// for a page that writes one, then the file of each component it holds. A page drawn with Head.Minify loads each file minified at
// build time (kernel/minified.go), the readable one where its source changed since.
func (r *renderer) Scripts() []*kernel.File {
	var files []*kernel.File
	add := func(readable func() *kernel.File, minified func() *kernel.File) {
		if r.head.Minify {
			if file := minified(); file != nil {
				files = append(files, file)
				return
			}
		}
		if file := readable(); file != nil {
			files = append(files, file)
		}
	}
	if r.rules.Scrolled() {
		add(Scrolled, ScrolledMin)
	}
	if !r.live && !r.head.Drive {
		return files
	}
	add(kernel.Kernel, kernel.KernelMin)
	if r.head.Drive {
		// every page of a site that moves without a reload: a page with nothing of its own to run still moves
		add(kernel.Drive, kernel.DriveMin)
	}
	if r.page != nil {
		add(kernel.Call, kernel.CallMin) // the page runs in the browser: its works call the server (bridge.go)
	}
	if r.intersect {
		add(kernel.Intersect, kernel.IntersectMin)
	}
	if r.program != nil {
		// the page's sources as JavaScript, read by the kernel before it starts (program.go)
		files = append(files, r.program)
	}
	names := sortedKeys(r.components)
	for _, name := range names {
		add(func() *kernel.File { return component.File(name) }, func() *kernel.File { return component.MinFile(name) })
	}
	return files
}

// Scrolled is the file that keeps the flag a page's scrolled: classes read (stylesheet.ScrolledScript);
// ScrolledMin it minified at build time, nil when its source changed since.
var (
	Scrolled = sync.OnceValue(func() *kernel.File {
		script := stylesheet.ScrolledScript()
		return kernel.New(script.Role, "scrolled", string(script.JavaScript))
	})
	ScrolledMin = sync.OnceValue(func() *kernel.File {
		return kernel.Min(Scrolled().Role, "scrolled", string(Scrolled().Body))
	})
)

// FileAt is the file the engine serves at path under /_kitwork/: the kernel, Drive, the bridge, the observer of
// intersect-*, the scrolled flag, a component, a part a component loads, each readable or minified at build time, the warnings of a page drawn while
// developing; nil when there is none. exact says the name is today's; an older name of the same file still gets it.
func FileAt(path string) (file *kernel.File, exact bool) {
	rest, ok := strings.CutPrefix(path, kernel.Prefix)
	if !ok || !strings.HasSuffix(rest, ".js") || len(rest) > 160 {
		return nil, false
	}
	var candidate, minified *kernel.File
	switch label := rest[:max(strings.LastIndexByte(strings.TrimSuffix(rest, ".js"), '.'), 0)]; {
	case label == "kernel":
		candidate, minified = kernel.Kernel(), kernel.KernelMin()
	case label == "drive":
		candidate, minified = kernel.Drive(), kernel.DriveMin()
	case label == "call":
		candidate, minified = kernel.Call(), kernel.CallMin()
	case label == "page":
		// a page's program: by its own name only (program.go)
		if file := programAt(path); file != nil {
			return file, true
		}
		return nil, false
	case label == "directives/intersect":
		candidate, minified = kernel.Intersect(), kernel.IntersectMin()
	case label == "scrolled":
		candidate, minified = Scrolled(), ScrolledMin()
	case strings.HasPrefix(label, "components/"):
		name := strings.TrimPrefix(label, "components/")
		candidate, minified = component.File(name), component.MinFile(name)
	case strings.HasPrefix(label, "parts/"):
		// a part a component loads: its name of today only (component.Part)
		name := strings.TrimPrefix(label, "parts/")
		for _, part := range []*kernel.File{component.Part(name), component.MinPart(name)} {
			if part != nil && part.Path == path {
				return part, true
			}
		}
		return nil, false
	case label == "warnings":
		// a page's warnings, drawn while developing (warning.go): by its own name only
		if file := warningsAt(path); file != nil {
			return file, true
		}
		return nil, false
	}
	if minified != nil && minified.Path == path {
		return minified, true
	}
	if candidate == nil {
		return nil, false
	}
	named, exact := candidate.Named(path)
	if !named {
		return nil, false
	}
	return candidate, exact
}

// modifierProblem says what the kernel would refuse in the modifiers of one event, "" when it runs them: modifiers
// follow the ':' joined by '.', each known, none twice.
func modifierProblem(event, modifiers string) string {
	if modifiers == "" {
		return ""
	}
	list := splitModifiers(modifiers)
	for _, m := range list {
		if m == "" {
			return "phần đi kèm rỗng: các phần đi kèm nối nhau bằng một dấu chấm (keydown:escape, input:once.debounce(300))"
		}
	}
	switch event {
	case "ready":
		return "ready chạy một lần khi component bắt đầu sống ở trình duyệt, không có phần đi kèm"
	case "intersect-enter", "intersect-leave":
		seen := map[string]bool{}
		for _, m := range list {
			name := m
			if strings.HasPrefix(m, "margin(") {
				if !strings.HasSuffix(m, ")") || m == "margin()" {
					return m + " cần một khoảng lề CSS, như margin(-20%)"
				}
				name = "margin"
			}
			switch name {
			case "once", "half", "full", "margin":
			default:
				return "không có phần đi kèm " + m + " cho " + event + " (nhận once, half, full, margin(…))"
			}
			if seen[name] {
				return "phần đi kèm " + name + " viết hai lần"
			}
			seen[name] = true
		}
		if seen["half"] && seen["full"] {
			return "half và full là hai ngưỡng (nửa phần tử, cả phần tử); chọn một"
		}
		return ""
	case "click-outside":
		for _, m := range list {
			if m == "outside" {
				return "click-outside đã nghe ở bên ngoài: không viết thêm outside"
			}
		}
		return pipelineProblem("click", append([]string{"outside"}, list...))
	}
	dom, known := events[event]
	if !known {
		dom = event
	}
	return pipelineProblem(dom, list)
}

// splitModifiers cuts modifiers at the dots outside parentheses: once.debounce(300), margin(-2.5rem).
func splitModifiers(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '.':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// keyComboWords are the words a key combination may hold besides its one key (mod is Control, or Command on macOS).
var keyComboWords = map[string]bool{"mod": true, "ctrl": true, "meta": true, "shift": true, "alt": true}

// pipelineProblem is the kernel's modifier pipeline for one DOM event: where it listens (window, document, outside,
// self), which key (escape, enter, a combination), what it does to the event (prevent, stop), how often
// (debounce(n), throttle(n), once).
func pipelineProblem(event string, modifiers []string) string {
	seen := map[string]bool{}
	timing := 0
	for _, m := range modifiers {
		name := m
		if open := strings.IndexByte(m, '('); open > 0 && strings.HasSuffix(m, ")") && (m[:open] == "debounce" || m[:open] == "throttle") {
			name = m[:open]
			timing++
			// digits only, as the kernel reads them: debounce(+300) disables the handler there
			ms, err := strconv.Atoi(m[open+1 : len(m)-1])
			if err != nil || ms < 1 || ms > 60000 || !digits(m[open+1:len(m)-1]) {
				return name + "(" + m[open+1:len(m)-1] + "): thời gian chờ là số mili giây từ 1 đến 60000"
			}
		}
		if seen[name] {
			return "phần đi kèm " + name + " viết hai lần"
		}
		seen[name] = true
		if strings.Contains(name, "+") {
			if event != "keydown" && event != "keyup" {
				return name + " là tổ hợp phím, đặt trên keydown hoặc keyup"
			}
			key := ""
			parts := map[string]bool{}
			for _, part := range strings.Split(name, "+") {
				switch {
				case part == "":
					return name + " có một phần rỗng: viết như mod+k"
				case keyComboWords[part]:
					if parts[part] {
						return name + " lặp lại " + part
					}
					parts[part] = true
				case key != "":
					return name + " có hai phím (" + key + " và " + part + "); một tổ hợp bấm một phím"
				default:
					key = part
				}
			}
			if key == "" {
				return name + " chưa có phím: viết như mod+k"
			}
			if parts["mod"] && (parts["ctrl"] || parts["meta"]) {
				return name + " trộn mod với ctrl hay meta: mod đã là phím của nền tảng (Control, Command trên macOS), viết như mod+" + key
			}
			seen["combo"] = true
			continue
		}
		switch name {
		case "window", "document", "outside", "self", "escape", "enter", "prevent", "stop", "once", "debounce", "throttle":
		default:
			return "không có phần đi kèm " + m + " (nhận window, document, outside, self, escape, enter, tổ hợp phím như mod+k, prevent, stop, debounce(…), throttle(…), once)"
		}
	}
	switch {
	case seen["combo"] && (seen["escape"] || seen["enter"]):
		return "tổ hợp phím đã là bộ lọc phím, không đi cùng escape hay enter"
	case (seen["escape"] || seen["enter"]) && event != "keydown" && event != "keyup":
		return "escape và enter lọc một phím, đặt trên keydown hoặc keyup"
	case seen["escape"] && seen["enter"]:
		return "escape và enter không cùng là bộ lọc của một sự kiện"
	case seen["outside"] && event != "click" && event != "dblclick" && event != "pointerdown" && event != "pointerup" && event != "focusin":
		return "outside chỉ đi với click, double-click, pointer-down, pointer-up và focus-in"
	case seen["self"] && (seen["outside"] || seen["window"] || seen["document"]):
		return "self giữ sự kiện ở chính phần tử, không đi cùng outside, window hay document"
	case seen["window"] && seen["document"]:
		return "window và document là hai chỗ nghe; chọn một"
	case timing > 1:
		return "debounce và throttle không cùng hẹn giờ một sự kiện"
	}
	return ""
}

// digits says s is decimal digits only.
func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sortedKeys are the keys of m, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
