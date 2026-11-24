package ui

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// What a page says without being asked, moved from the first engine with its kernel (06/11/2026, slice 2):
// the progressbar of a move between pages and the reader that says the new page aloud, which a site moving
// without a reload (Drive) gets when its frame writes none; the network's notice; the toast's stack of
// notices. Each is a component of the kernel; the server writes its host, its inputs and what it draws, as
// the first engine did (docs/components/toast.md, network.md; KITWORK_UI mục 13).

// host is what a component's tag adds to the element it builds besides what its attributes say: classes of its
// own after theirs, attributes the engine writes after the host's mark, its inputs for the kernel
// (data-kitwork-scope), markup after what the author drew.
type host struct {
	classes, attributes, inputs []string
	tail                        string
}

// withFrameParts gives a page whose frame writes no progressbar the one it would have written, first in the
// frame, and one that writes no reader the one it would have written, last: on a site that moves without a
// reload a move tells a screen reader nothing, so the reader says the new title, and the bar shows the move.
// A page with no frame (no view.ui) gets neither.
func withFrameParts(page *Node) *Node {
	if page.Tag != "view" {
		return page
	}
	bar, reader := false, false
	for _, child := range page.Elements() {
		bar = bar || child.Tag == "progressbar"
		reader = reader || child.Tag == "reader"
	}
	if bar && reader {
		return page
	}
	with := *page
	with.source = nil // the frame with the parts it did not write (frozen.go)
	with.Children = append([]*Node(nil), page.Children...)
	if !bar {
		with.Children = append([]*Node{{Tag: "progressbar", Closed: true, File: page.File, Line: page.Line, Column: page.Column}}, with.Children...)
	}
	if !reader {
		with.Children = append(with.Children, &Node{Tag: "reader", Closed: true, File: page.File, Line: page.Line, Column: page.Column})
	}
	return &with
}

// lintFrame warns of the bar and the reader a frame writes, each drawn as written: a second bar runs with the first,
// a bar written lower still sits on the top edge, a bar on a site that reloads every page never runs, and a second
// reader may say a sentence twice.
func (r *renderer) lintFrame(page *Node) {
	if page.Tag != "view" {
		return
	}
	var bar, reader *Node
	for i, child := range page.Elements() {
		switch child.Tag {
		case "progressbar":
			if bar != nil {
				r.warn(child, nil, "page chỉ có một progressbar (thanh này chạy cùng thanh đầu)")
				continue
			}
			if i > 0 {
				r.warn(child, nil, "progressbar đứng đầu page, trên <%s />: vạch nằm ở mép trên của trang", page.Elements()[0].Tag)
			}
			bar = child
		case "reader":
			if reader != nil {
				r.warn(child, nil, "page chỉ có một reader (trình đọc màn hình có thể đọc một câu hai lần)")
				continue
			}
			reader = child
		}
	}
	if bar != nil && !r.head.Drive {
		r.warn(bar, nil, "router.reload(true) tải lại cả trang mỗi lần chuyển nên progressbar không bao giờ chạy; bỏ thẻ này")
	}
}

// english says the page is written in a language other than Vietnamese: the words the engine writes itself
// (a bar's name, a stack's) are English there.
func (r *renderer) english() bool {
	return !strings.HasPrefix(strings.ToLower(r.head.Language), "vi")
}

// Progressbar: how a move between pages is going.
const (
	progressDelay        = 120   // ms a move may take before the bar shows: Drive's own when not written
	progressLongestDelay = 10000 // longer, the bar of most moves never shows
	progressThickestLine = 8     // px: thicker is a band, not a bar
)

// progressbar is <progressbar color="…" thickness="…" delay="…" alt="…" />: the move to the next page that Drive
// reports, through the kernel's progressbar component, on a host fixed to the top edge that takes no room, the
// line inside it. Its words are fixed when the page is built; it stands first in the frame, above the header.
func (r *renderer) progressbar(n *Node) error {
	if parent := r.parent(); parent == nil || parent.Tag != "view" {
		return at(n, nil, "progressbar đặt trong view.ui, ở đầu <view>, trên <header />; không đặt trong vùng hay nội dung")
	}
	if len(n.Significant()) > 0 {
		return at(n, nil, "progressbar không có nội dung con; viết <progressbar />")
	}
	color, label, delay, thickness := "brand", "", "", "1"
	var colorAttribute *Attribute
	for i := range n.Attributes {
		a := &n.Attributes[i]
		value := strings.TrimSpace(a.Value)
		switch {
		case a.Bare:
			return at(n, a, "%s cần một giá trị", a.Name)
		case strings.Contains(value, "{{"):
			return at(n, a, "%s của progressbar là chữ cố định, không có {{ }}", a.Name)
		case a.Name == "color":
			color, colorAttribute = value, a
		case a.Name == "alt":
			label = a.Value
		case a.Name == "thickness":
			px, err := strconv.Atoi(value)
			if err != nil || px < 1 {
				return at(n, a, "thickness của progressbar là số px từ 1 đến 8 (mặc định 1), không phải %q", a.Value)
			}
			if px > progressThickestLine {
				// drawn as written: a band, not a bar
				r.warn(n, a, "thickness của progressbar là số px từ 1 đến 8 (mặc định 1); %q dày hơn một vạch (trang vẽ đúng %dpx)", a.Value, px)
			}
			thickness = value
		case a.Name == "delay":
			ms, err := strconv.Atoi(value)
			if err != nil || ms < 0 {
				return at(n, a, "delay là số mili giây từ 0 đến 10000 (mặc định 120; 0 là hiện ngay), không phải %q", a.Value)
			}
			if ms > progressLongestDelay {
				// kept as written: the bar of most moves never shows
				r.warn(n, a, "delay là số mili giây từ 0 đến 10000 (mặc định 120; 0 là hiện ngay); %q lâu tới mức thanh gần như không bao giờ hiện (trang giữ đúng %dms)", a.Value, ms)
			}
			if ms != progressDelay {
				delay = strconv.Itoa(ms)
			}
		default:
			return at(n, a, "progressbar chỉ nhận color (màu của vạch), thickness (độ dày), delay (chờ bao lâu mới hiện) và alt (tên đọc được), không nhận %s", a.Name)
		}
	}
	// the line's colour is read as the color written, so a colour the palette lacks is named as written, then
	// made the line's background
	if _, _, err := r.table.Read(nil, stylesheet.Intent{Tag: "progressbar", Name: "color", Value: color}); err != nil {
		return at(n, colorAttribute, "%s", err.Error())
	}
	bar, _, err := r.table.Read(r.rules, stylesheet.Intent{Tag: "progressbar", Name: "background", Value: color})
	if err != nil {
		return at(n, colorAttribute, "%s", err.Error())
	}
	line, _, err := r.table.Read(r.rules, stylesheet.Intent{Name: "thickness", Value: thickness})
	if err != nil {
		return at(n, nil, "%s", err.Error())
	}
	if label == "" {
		label = "Đang chuyển trang"
		if r.english() {
			label = "Loading the next page"
		}
	}
	r.load("progressbar")
	// fixed, not sticky: a sticky host cannot leave <body>, so on a short page it could not reach the bottom of a
	// title bar the page draws itself. hidden is the first paint, no move being under way when the page arrives
	r.out.WriteString(`<div class="progressbar" data-kitwork-component="progressbar" data-kitwork-bind="show: active; aria-valuenow: value" hidden role="progressbar"`)
	if delay != "" {
		r.out.WriteString(` data-kitwork-delay="` + delay + `"`) // read by Drive, which decides when a move is slow
	}
	r.out.WriteString(` aria-label="` + html.EscapeString(label) + `" aria-valuemin="0" aria-valuemax="100" data-kitwork-style="top: top">`)
	r.out.WriteString(`<div class="` + html.EscapeString(line+" "+bar) + `" data-kitwork-style="width: value + &#39;%&#39;"></div></div>`)
	return nil
}

// reader is <reader /> or <reader as="$reader" />: what a screen reader says aloud, an invisible live region
// whose text is the kernel's reader component's. Drive says the new page's title through it after every move;
// a page says a sentence of its own through its alias ($reader.say('Đã lưu.')).
func (r *renderer) reader(n *Node) error {
	if parent := r.parent(); parent == nil || parent.Tag != "view" {
		// it is drawn where it is written, and works until a move replaces what is around it
		r.warn(n, nil, "reader đặt trong view.ui, là con của <view>; không đặt trong vùng hay nội dung (Drive thay nội dung khi chuyển trang nên câu đọc có thể mất)")
	}
	if len(n.Significant()) > 0 {
		return at(n, nil, "reader không có nội dung con; câu cần đọc gửi bằng $reader.say(…)")
	}
	host := "reader"
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if a.Name != "as" {
			return at(n, a, "reader chỉ nhận as, không nhận %s", a.Name)
		}
		alias := strings.TrimSpace(a.Value)
		if !aliasName(alias) {
			return at(n, a, "as của <reader> là $ và một tên (as=\"$reader\")")
		}
		host += ": " + alias
	}
	r.load("reader")
	r.out.WriteString(`<div class="reader" data-kitwork-component="` + html.EscapeString(host) + `" data-kitwork-bind="text: text" aria-live="polite" aria-atomic="true"></div>`)
	return nil
}

// Places and transitions of what floats at an edge of the screen: the toast's stack and the network's notice.

// places are where a stack or a notice stands on the screen, the toast's default first. Left and right are the
// design's: on a page written right to left a stack at the bottom right stands at the bottom left.
var places = []string{"bottom-right", "bottom", "bottom-left", "top-right", "top", "top-left"}

// renamedPlaces are the places of before 19/10/2026, each with the word written now.
var renamedPlaces = map[string]string{"bottom-end": "bottom-right", "bottom-start": "bottom-left", "top-end": "top-right", "top-start": "top-left"}

// transitions are how a notice comes and goes, the default of a placed host first: slide from the edge its place
// names, fade in place, none at once. How fast is the engine's; the classes are reboot.css's.
var transitions = []string{"slide", "fade", "none"}

// transitionStrangers are the words other libraries give the same choice, each told to write transition.
var transitionStrangers = map[string]bool{"animation": true, "animate": true, "motion": true, "effect": true}

// transitionDirections are the words other libraries put after slide: here the direction is the place's.
var transitionDirections = map[string]bool{"up": true, "down": true, "left": true, "right": true, "in": true, "out": true, "top": true, "bottom": true}

// transitionClasses are the host's classes for a transition, none for slide (the placed host's own).
func transitionClasses(transition string) []string {
	switch transition {
	case "fade", "none":
		return []string{"transition-" + transition}
	}
	return nil
}

// transitionOf reads transition= on the host of who: one of the three, a word fixed when the page is built. A
// value of CSS's transition (a property, a time, an easing) is told it is not one.
func transitionOf(n *Node, a *Attribute, chain, who string) (string, error) {
	value := strings.TrimSpace(a.Value)
	switch {
	case chain != "":
		return "", at(n, a, "transition của %s không nhận tiền tố: nó được quyết một lần khi trang dựng", who)
	case a.Bare || value == "":
		return "", at(n, a, "transition của %s cần một giá trị: %s", who, strings.Join(transitions, ", "))
	case strings.Contains(value, "{{"):
		return "", at(n, a, "transition của %s là chữ cố định (%s), không có {{ }}: nó được quyết một lần khi trang dựng", who, strings.Join(transitions, ", "))
	case contains(transitions, value):
		return value, nil
	case cssTransition(value):
		return "", at(n, a, "transition=%q là cách viết của CSS; transition của .ui là kiểu hiệu ứng: slide (trượt từ mép), fade (hiện dần, mờ dần) hay none (ngay); nhanh chậm là việc của engine", value)
	}
	if first, rest, ok := strings.Cut(strings.ToLower(value), "-"); ok || strings.Contains(value, " ") {
		if !ok {
			first, rest, _ = strings.Cut(strings.ToLower(value), " ")
		}
		rest = strings.TrimSpace(rest)
		switch {
		case first == "slide" && transitionDirections[rest]:
			return "", at(n, a, "transition=%q: hướng trượt là của place (ở mép dưới thì trượt lên khi hiện, ở mép trên thì trượt xuống); transition chỉ là một chữ: slide, fade hay none", value)
		case first == "fade" && (rest == "in" || rest == "out"):
			return "", at(n, a, "transition=%q: fade đã gồm hiện dần lúc vào và mờ dần lúc ra; viết transition=\"fade\"", value)
		}
	}
	return "", at(n, a, "transition của %s là slide, fade hay none, không phải %q", who, value)
}

// cssTransition says a value reads as CSS's transition: a function, or a time, an easing or a property among its
// words.
func cssTransition(value string) bool {
	if strings.ContainsAny(value, "()") {
		return true
	}
	for _, word := range strings.FieldsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		if d := readDuration(word); (d.problem == durationRead || d.problem == durationZero) && (d.unit == "s" || d.unit == "ms") {
			return true
		}
		switch word {
		case "all", "opacity", "transform", "translate", "ease", "ease-in", "ease-out", "ease-in-out", "linear":
			return true
		}
	}
	return false
}

// placeOf reads place= on the host of who: one of the six, fixed when the page is built.
func placeOf(n *Node, a *Attribute, chain, who string) (string, error) {
	value := strings.TrimSpace(a.Value)
	switch {
	case chain != "":
		return "", at(n, a, "place của %s không nhận tiền tố: nó được quyết một lần khi trang dựng", who)
	case a.Bare || value == "":
		return "", at(n, a, "place của %s cần một giá trị: %s", who, strings.Join(places, ", "))
	case strings.Contains(value, "{{"):
		return "", at(n, a, "place của %s là chữ cố định, không có {{ }}: nó được quyết một lần khi trang dựng", who)
	case renamedPlaces[value] != "":
		return "", at(n, a, `place="%s": start và end nay là left và right, viết place="%s" (tự lật khi trang viết từ phải sang trái)`, value, renamedPlaces[value])
	case !contains(places, value):
		return "", at(n, a, "place của %s là một mép hay góc màn hình: %s; không phải %q", who, strings.Join(places, ", "), value)
	}
	return value, nil
}

// splitHost is n's own words read off a copy of it: the copy keeps the attributes own does not take, and own is
// told each it takes, with its prefix chain apart (an event's name is no prefix).
func splitHost(n *Node, own func(a *Attribute, chain, base string) (bool, error)) (*Node, error) {
	copied := *n
	copied.Attributes, copied.source = nil, nil
	for i := range n.Attributes {
		a := &n.Attributes[i]
		chain, base := "", a.Name
		if !isEvent(a.Name) {
			chain, base = splitPrefix(a.Name)
		}
		taken, err := own(a, chain, base)
		if err != nil {
			return nil, err
		}
		if !taken {
			copied.Attributes = append(copied.Attributes, *a)
		}
	}
	return &copied, nil
}

// Network: whether the page can reach the network, and the moment that changes.
const (
	networkLongestDelay = 10000 // ms: a page that does not answer longer goes untold too long
	toastLongest        = 60000 // ms: a notice meant to stay longer stays until closed (0)
	toastShortest       = 2000  // ms: the least a person reads in
)

// networkStrangers are the words other libraries give the network's two times, each with the one to write.
var networkStrangers = map[string]string{
	"timeout": "duration", "time": "duration", "autoclose": "duration", "autoClose": "duration",
	"wait": "delay", "debounce": "delay", "grace": "delay",
}

// network reads the network's own words off its host: place (an edge or corner it floats at, none to stay in the
// page's flow), delay (how long the network must stay gone before it counts, 1s when not written) and duration
// (how long "back online" stays, 4s), transition; a host with something inside is a status region.
func (r *renderer) network(n *Node) (*Node, *host, error) {
	status := len(n.Significant()) > 0
	place, transition := "", ""
	var written *Attribute
	var inputs []string
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		if word := networkStrangers[base]; word != "" {
			return false, at(n, a, "network không có %s: %s", base, networkTimes(word))
		}
		if transitionStrangers[base] {
			return false, at(n, a, "network không có %s: hiệu ứng hiện và ẩn là transition (slide, fade hay none)", base)
		}
		switch base {
		case "transition":
			value, err := transitionOf(n, a, chain, "network")
			transition, written = value, a
			return true, err
		case "role", "aria-live", "aria-atomic":
			return false, at(n, a, "vai trò của network là việc của engine (role=\"status\" khi bên trong có thông báo): bỏ %s", base)
		case "delay", "duration":
			if chain != "" {
				return false, at(n, a, "%s của network không nhận tiền tố: nó được quyết một lần khi trang dựng", base)
			}
			ms, err := networkTime(n, a, base)
			if err != nil {
				return false, err
			}
			if base == "duration" {
				// on an empty host the one thing to say is that nothing shows. An empty host with an alias may be
				// drawn elsewhere ($network.open on a card of the page): its duration counts
				if _, named := n.Attribute("as"); !status && !named {
					r.warn(n, a, "duration=%q trên <network /> rỗng: nó không hiện gì, duration không đóng gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ duration", strings.TrimSpace(a.Value))
					return true, nil
				}
				if ms > 0 && ms < toastShortest {
					r.warn(n, a, "%s", `duration="`+a.Value+`" dưới 2 giây: người xem đọc không kịp; viết duration từ "2s", hay bỏ duration (4 giây)`)
				}
			}
			inputs = append(inputs, base+": "+strconv.FormatFloat(ms, 'f', -1, 64))
			return true, nil
		case "place":
			value, err := placeOf(n, a, chain, "network")
			if err == nil && !status {
				// an empty host only hears the events and never shows: there is nothing to place
				r.warn(n, a, "place=%q trên <network /> rỗng: nó chỉ nghe sự kiện và không hiện, place không đặt gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ place", value)
				return true, nil
			}
			place = value
			return true, err
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, err
	}
	switch {
	case written != nil && !status:
		r.warn(n, written, "transition=%q trên <network /> rỗng: nó không hiện gì, transition không làm gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ transition", strings.TrimSpace(written.Value))
		transition = "none" // told it does nothing, it writes nothing, as place and duration there
	case written != nil && transition == "slide" && place == "":
		return nil, nil, at(n, written, "transition=\"slide\" trượt vào từ mép màn hình mà network không có place: viết place (%s), hay transition=\"fade\" cho khung nằm trong trang", strings.Join(places, ", "))
	case written == nil && place != "":
		transition = transitions[0]
	case written == nil:
		transition = "none"
	}
	h := &host{inputs: inputs}
	if place == "" {
		if transition == "fade" {
			h.classes = transitionClasses("fade") // in the flow only fade moves
		}
	} else {
		h.classes = append([]string{"network", "network-" + place}, transitionClasses(transition)...)
	}
	if status {
		h.attributes = append(h.attributes, `role="status"`) // what shows there, and what it changes to, is said
	}
	return copied, h, nil
}

// networkTimes says what each of the network's two times is, ending on the one a stranger meant.
func networkTimes(word string) string {
	if word == "delay" {
		return `chờ mạng mất bao lâu rồi mới báo là delay (delay="1s"); khung "đã có mạng lại" ở lại bao lâu là duration (duration="4s")`
	}
	return `khung "đã có mạng lại" ở lại bao lâu rồi tự đóng là duration (duration="4s", 0 là ở lại tới khi đóng); chờ mạng mất bao lâu rồi mới báo là delay (delay="1s")`
}

// networkTime reads delay= or duration= on a network's host: a duration ("1s", "800ms") or 0, in milliseconds.
func networkTime(n *Node, a *Attribute, word string) (float64, error) {
	value := strings.TrimSpace(a.Value)
	example, zero := `delay="1s"`, "báo ngay"
	if word == "duration" {
		example, zero = `duration="4s"`, "ở lại tới khi đóng"
	}
	switch {
	case a.Bare || value == "":
		return 0, at(n, a, "%s cần một thời lượng: viết %s (0 là %s)", word, example, zero)
	case strings.Contains(value, "{{"):
		return 0, at(n, a, "%s của network là một thời lượng cố định (%s), không có {{ }}: nó được quyết một lần khi trang dựng", word, example)
	}
	d := readDuration(value)
	switch d.problem {
	case durationNegative:
		return 0, at(n, a, "%s không nhận thời lượng âm (%s): viết một thời lượng như %s", word, value, example)
	case durationZero:
		return 0, nil
	case durationNoUnit:
		return 0, at(n, a, `%s="%s" thiếu đơn vị: viết %s="%s"`, word, a.Value, word, d.suggestion(durationMilliseconds))
	case durationTwoUnits, durationForeign:
		return 0, at(n, a, `%s="%s" %s: viết %s="%s"`, word, a.Value, d.reason(), word, d.suggestion(durationMilliseconds))
	case durationNotOne:
		return 0, at(n, a, `%s="%s" không phải một thời lượng: viết %s hay %s="800ms" (0 là %s)`, word, a.Value, example, word, zero)
	}
	switch {
	case word == "delay" && d.ms > networkLongestDelay:
		return 0, at(n, a, `delay="%s" dài hơn 10 giây: mất mạng lâu vậy mà không báo thì người xem không biết vì sao trang không chạy; viết delay="10s" hay ngắn hơn`, a.Value)
	case word == "duration" && d.ms > toastLongest:
		return 0, at(n, a, `duration="%s" dài hơn 60 giây: khung ở lại tới khi đóng thì viết duration="0"`, a.Value)
	}
	return d.ms, nil
}

// Toast: a stack of short notices at an edge of the screen. The author draws the stack and ONE notice, the
// blueprint: the host's one direct child written for="notice of notices; key: notice.id". The kernel makes a row
// of it for each notice and keeps it by its key; the server paints none, and reboot.css never draws the
// blueprint. Last in the host, two regions only a screen reader hears, polite and assertive.

// toastFields are what a notice has; toastActionFields what its action has.
var (
	toastFields       = []string{"id", "message", "title", "tone", "loading", "action", "count", "leaving"}
	toastActionFields = []string{"label", "click", "link"}
)

// toastFieldHints are the fields of other libraries' notices, each with the toast's own word.
var toastFieldHints = map[string]string{
	"text": "câu là notice.message", "description": "câu là notice.message", "body": "câu là notice.message",
	"content": "câu là notice.message", "type": "loại là notice.tone", "variant": "loại là notice.tone",
	"kind": "loại là notice.tone", "status": "loại là notice.tone", "heading": "tiêu đề là notice.title",
	"actions": "nút là notice.action", "key": "tên của thông báo là notice.id",
}

// toastActionHints are the keys of a button in other libraries, each with the toast's word.
var toastActionHints = map[string]string{
	"text": "label", "title": "label", "name": "label", "onClick": "click", "onclick": "click", "do": "click",
	"then": "click", "handler": "click", "run": "click", "href": "link", "url": "link", "to": "link",
}

// toastRegions are the two regions a screen reader hears, last in the host: polite for every notice but a danger,
// assertive for that one; each says the line added to it alone.
const toastRegions = `<span class="reader" role="status" aria-atomic="false"></span><span class="reader" role="alert" aria-atomic="false"></span>`

// toastBlueprintHint is how the notice is written, for a message.
const toastBlueprintHint = `<card for="notice of notices; key: notice.id">…<button click="dismiss(notice.id)"><icon value="x" alt="Đóng thông báo" /></button></card>`

// blueprint is the toast's notice as the kernel makes each row of it.
type blueprint struct {
	node      *Node
	item, key string
}

// toast reads the toast's own words off its host (place, limit, alt, transition) and finds the blueprint of a
// notice inside it: one stack on a page.
func (r *renderer) toast(n *Node) (*Node, *host, error) {
	if r.toastAt != "" {
		return nil, nil, at(n, nil, "trang đã có một toast ở %s: mỗi trang một chồng thông báo (một phím F8, một chỗ cho thông báo)", r.toastAt)
	}
	place, limit, label, transition := places[0], 3, "", transitions[0]
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		if base == "transition" {
			value, err := transitionOf(n, a, chain, "toast")
			transition = value
			return true, err
		}
		if transitionStrangers[base] {
			return false, at(n, a, "toast không có %s: hiệu ứng hiện và ẩn là transition (slide, fade hay none)", base)
		}
		switch base {
		case "if", "for", "show":
			return false, at(n, a, "toast luôn có mặt và có một trên mỗi trang: bỏ %s trên <toast> (ẩn hiện từng thông báo là việc của toast)", base)
		case "persist", "remember":
			return false, at(n, a, "toast không giữ gì qua các lần ghé: thông báo là chuyện của lúc này; bỏ %s", base)
		case "role", "aria-live", "aria-label", "aria-atomic":
			return false, at(n, a, "vai trò của toast là việc của engine (role=\"region\", hai vùng đọc cho trình đọc màn hình): bỏ %s; tên của chồng là alt (alt=\"Tin nhắn\")", base)
		case "place":
			value, err := placeOf(n, a, chain, "toast")
			place = value
			return true, err
		case "limit", "alt":
		default:
			return false, nil
		}
		value := strings.TrimSpace(a.Value)
		switch {
		case chain != "":
			return false, at(n, a, "%s của toast không nhận tiền tố: nó được quyết một lần khi trang dựng", base)
		case a.Bare || value == "":
			return false, at(n, a, "%s của toast cần một giá trị", base)
		case strings.Contains(value, "{{"):
			return false, at(n, a, "%s của toast là chữ cố định, không có {{ }}: nó được quyết một lần khi trang dựng", base)
		}
		if base == "alt" {
			label = value
			return true, nil
		}
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > 10 {
			return false, at(n, a, "limit là số thông báo hiện cùng lúc, từ 1 đến 10 (mặc định 3), không phải %q", a.Value)
		}
		limit = count
		return true, nil
	})
	if err != nil {
		return nil, nil, err
	}
	print, err := r.toastBlueprint(copied)
	if err != nil {
		return nil, nil, err
	}
	r.blueprint = print
	r.toastAt = fmt.Sprintf("%s:%d:%d", n.File, n.Line, n.Column)
	if label == "" {
		label = "Thông báo"
		if r.english() {
			label = "Notifications"
		}
	}
	return copied, &host{
		classes: append([]string{"toast", "toast-" + place}, transitionClasses(transition)...),
		// the stack: a region a screen reader lists by its name, which F8 brings the focus into
		attributes: []string{`role="region"`, `aria-label="` + html.EscapeString(label+" (F8)") + `"`, `tabindex="-1"`},
		inputs:     []string{"place: " + quote(place), "limit: " + strconv.Itoa(limit)},
		tail:       toastRegions,
	}, nil
}

// forHead reads a for's value: its item, its index, its list and its key, as written.
func forHead(value string) (item, index, list, key string, ok bool) {
	source := strings.TrimSpace(value)
	if bare, whole := wrapped(source); whole {
		source = bare
	}
	head, tail, _ := strings.Cut(source, ";")
	if tail = strings.TrimSpace(tail); tail != "" {
		rest, isKey := strings.CutPrefix(tail, "key:")
		if !isKey {
			return "", "", "", "", false
		}
		key = strings.TrimSpace(rest)
	}
	names, list, found := strings.Cut(head, " of ")
	if !found {
		return "", "", "", "", false
	}
	item, index, _ = strings.Cut(names, ",")
	return strings.TrimSpace(item), strings.TrimSpace(index), strings.TrimSpace(list), key, true
}

// toastBlueprint finds the blueprint of a notice among the host's children and checks what it draws: a frame, keyed
// by the notice's id, a dismiss button with a name a screen reader says that nothing can hide, and only the fields a
// notice has.
func (r *renderer) toastBlueprint(n *Node) (*blueprint, error) {
	var found *blueprint
	for _, child := range n.Elements() {
		a, ok := child.Attribute("for")
		if !ok {
			continue
		}
		item, _, list, key, valid := forHead(a.Value)
		if !valid {
			continue // the for's own error, where it is drawn
		}
		if list != "notices" {
			hint := ""
			if list == "items" {
				hint = " (items là tên cũ)"
			}
			return nil, at(child, &a, "danh sách của toast là notices%s: viết for=\"%s of notices; key: %s.id\"", hint, item, item)
		}
		if found != nil {
			return nil, at(child, &a, "toast có hai mẫu thông báo (%s:%d:%d và %s:%d:%d): mỗi toast một mẫu", found.node.File, found.node.Line, found.node.Column, child.File, child.Line, child.Column)
		}
		if key != item+".id" {
			return nil, at(child, &a, "mẫu thông báo cần key: %s.id: mỗi thông báo giữ chỗ của nó khi chồng đổi (focus không mất); viết for=\"%s of notices; key: %s.id\"", item, item, item)
		}
		if _, component := componentState[child.Tag]; component || strings.Contains(child.Tag, "-") {
			return nil, at(child, nil, "mẫu thông báo không là component (<%s>): một phần tử, một component; đặt <%s> bên trong một <card>", child.Tag, child.Tag)
		}
		if child.Tag != "box" && child.Tag != "card" && child.Tag != "section" {
			return nil, at(child, nil, "mẫu thông báo là một khung: <card for=\"%s of notices; key: %s.id\">, hay <box>, <section>; không phải <%s>", item, item, child.Tag)
		}
		found = &blueprint{node: child, item: item, key: key}
	}
	if found == nil {
		if deep := deepNotices(n.Children); deep != nil {
			a, _ := deep.Attribute("for")
			return nil, at(deep, &a, "mẫu thông báo là con trực tiếp của <toast>: đưa phần tử có for=\"… of notices\" lên ngay dưới <toast>")
		}
		return nil, at(n, nil, "toast cần một mẫu thông báo bên trong <toast>: %s", toastBlueprintHint)
	}
	if deep := deepNotices(found.node.Children); deep != nil {
		a, _ := deep.Attribute("for")
		return nil, at(deep, &a, "mẫu thông báo không lồng một mẫu thông báo khác")
	}
	field := regexp.MustCompile(`(?:^|[^\w$.])` + regexp.QuoteMeta(found.item) + `\.([A-Za-z_$][\w$]*)(?:\.([A-Za-z_$][\w$]*))?`)
	read := map[string]bool{} // the fields of a notice the blueprint reads
	reads := func(n *Node, a *Attribute, text string) error {
		for _, m := range field.FindAllStringSubmatch(text, -1) {
			read[m[1]] = true
			if !contains(toastFields, m[1]) {
				if hint, ok := toastFieldHints[m[1]]; ok {
					return at(n, a, "%s không có %s: %s", found.item, m[1], strings.Replace(hint, "notice.", found.item+".", 1))
				}
				return at(n, a, "%s không có %s; một thông báo có %s", found.item, m[1], strings.Join(toastFields, ", "))
			}
			if m[1] == "action" && m[2] != "" && !contains(toastActionFields, m[2]) {
				hint := ""
				if word, ok := toastActionHints[m[2]]; ok {
					hint = ": viết " + found.item + ".action." + word
				}
				return at(n, a, "%s.action không có %s%s (nút của thông báo có %s)", found.item, m[2], hint, strings.Join(toastActionFields, ", "))
			}
		}
		return nil
	}
	dismissCall := regexp.MustCompile(`^\s*dismiss\(\s*` + regexp.QuoteMeta(found.item) + `\.id\s*\)\s*$`)
	dismiss := false
	// hides is the nearest element around (the notice among them) a show can hide
	var walk func(nodes []*Node, hides *Node) error
	walk = func(nodes []*Node, hides *Node) error {
		for _, child := range nodes {
			if child.IsText() {
				if err := reads(child, nil, child.Text); err != nil {
					return err
				}
				continue
			}
			if _, component := componentState[child.Tag]; component || strings.Contains(child.Tag, "-") {
				return at(child, nil, "mẫu thông báo không chứa component (<%s>): một phần tử, một component; đặt nó ngoài toast", child.Tag)
			}
			for i := range child.Attributes {
				if err := reads(child, &child.Attributes[i], child.Attributes[i].Value); err != nil {
					return err
				}
			}
			if click, ok := child.Attribute("click"); ok && child.Tag == "button" && dismissCall.MatchString(click.Value) {
				if !spokenName(child) {
					return at(child, nil, "nút đóng của thông báo cần tên đọc được: <button click=\"dismiss(%s.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button>", found.item)
				}
				if show := showOf(child); show != nil {
					return at(child, show, "nút đóng của thông báo luôn hiện: bỏ %s ở đây; thông báo lỗi ở lại tới khi đóng, nút đóng ẩn là thông báo không đóng được", show.Name)
				}
				if hides != nil {
					show := showOf(hides)
					return at(hides, show, "nút đóng của thông báo (%s:%d:%d) luôn hiện: bỏ %s trên khung bọc nó; thông báo lỗi ở lại tới khi đóng, nút đóng ẩn là thông báo không đóng được", child.File, child.Line, child.Column, show.Name)
				}
				dismiss = true
			}
			inner := hides
			if showOf(child) != nil {
				inner = child
			}
			if err := walk(child.Children, inner); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk([]*Node{found.node}, nil); err != nil {
		return nil, err
	}
	if !dismiss {
		return nil, at(found.node, nil, "mỗi thông báo cần một nút đóng: <button click=\"dismiss(%s.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button> (thông báo lỗi ở lại tới khi đóng)", found.item)
	}
	if !read["message"] {
		r.warn(found.node, nil, "mẫu thông báo không hiện %s.message: thông báo sẽ không có chữ; viết <text>{{ %s.message }}</text>", found.item, found.item)
	}
	return found, nil
}

// deepNotices is an element below nodes whose for runs over notices.
func deepNotices(nodes []*Node) *Node {
	for _, child := range nodes {
		if child.IsText() {
			continue
		}
		if a, ok := child.Attribute("for"); ok {
			if _, _, list, _, valid := forHead(a.Value); valid && list == "notices" {
				return child
			}
		}
		if found := deepNotices(child.Children); found != nil {
			return found
		}
	}
	return nil
}

// showOf is a show written on n, with or without a prefix, nil for none.
func showOf(n *Node) *Attribute {
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if _, name := splitPrefix(a.Name); name == "show" && !isEvent(a.Name) {
			return a
		}
	}
	return nil
}

// spokenName says a button has a name a screen reader says: its alt, words of its own, or an icon inside with alt.
func spokenName(n *Node) bool {
	if alt, ok := n.Attribute("alt"); ok && strings.TrimSpace(alt.Value) != "" {
		return true
	}
	for _, child := range n.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return true
			}
			continue
		}
		if child.Tag == "icon" {
			if alt, ok := child.Attribute("alt"); ok && strings.TrimSpace(alt.Value) != "" {
				return true
			}
			continue
		}
		if spokenName(child) {
			return true
		}
	}
	return false
}

// drawBlueprint draws the toast's notice once, as the kernel makes each row of it: data-kitwork-for on it, its
// item a live name of the browser's (the toast's notice), unknown to the server, which paints no value of it.
func (r *renderer) drawBlueprint(n *Node, inline bool) error {
	r.frames = append(r.frames, &frame{component: "toast", names: map[string]any{r.blueprint.item: nil}, fixed: true})
	takeBack := r.eval.put(r.blueprint.item, value.Undefined())
	r.inBlueprint++
	err := r.element(n, inline)
	r.inBlueprint--
	takeBack()
	r.frames = r.frames[:len(r.frames)-1]
	return err
}

// contains says list holds s.
func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
