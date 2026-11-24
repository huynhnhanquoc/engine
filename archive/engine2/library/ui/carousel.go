package ui

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// The carousel (18/10/2026, docs/components/carousel.md; moved from the first engine on 06/11/2026, slice 4) is
// one of Kitwork's components, written as its tag. The author draws the slides, the buttons and the dots; the
// engine recognises them and writes the wiring:
//
//	<carousel as="$hero" alt="Món nổi bật" duration="5s">
//	  <list><item>…</item><item>…</item></list>
//	  <button click="previous()">…</button><button click="next()">…</button>
//	  <button for="mon, i of mons" click="go(i)" active:background="brand" />
//	</carousel>
//
//   - The host is a region a screen reader names: role="region", aria-roledescription (carousel, in the page's
//     language), aria-label from alt.
//   - The one <list> right inside the host is the track (class carousel-track): a row that scrolls sideways and
//     snaps to each slide; tabindex="0" takes the arrow keys; aria-live is polite while it does not play, off while
//     it does. Each <item> of it is a slide: aria-roledescription, "1 / 3" as its name when the slides are written
//     out, aria-current on the one it starts on.
//   - A <button> whose click is go(…) of a place the page knows (a number, a for's index) is a dot: the place is its
//     value, aria-controls names the track, its click becomes go(Number($this.value)), aria-current marks the
//     slide it starts on. A go(…) of the component's own names (go(current + 2)) is a button like next().
//   - transition: slide (the default), fade (carousel-fade), none (carousel-none). duration: how long each slide
//     stays while it plays by itself (2s to 60s); not written, it plays only when play() is called.

// carouselScope is one carousel host: what r.carousel points to while its inside is drawn.
type carouselScope struct {
	track    *Node
	trackID  string
	slides   map[*Node]int // each <item> of the track, its place; -1 for one written with for or if
	count    int           // the slides written out, -1 when a for or an if makes it the page's data's
	start    int
	duration float64
	// host is the carousel's element, durationAt its duration=, nil when it does not play by itself (lintCarousels)
	host       *Node
	durationAt *Attribute
}

// carouselStrangers are the words other carousels use, each with what to write here.
var carouselStrangers = map[string]string{
	"autoplay": `tự chạy là duration: mỗi trang ở lại bao lâu (duration="5s")`,
	"interval": `mỗi trang ở lại bao lâu là duration (duration="5s")`,
	"timeout":  `mỗi trang ở lại bao lâu là duration (duration="5s")`,
	"delay":    `mỗi trang ở lại bao lâu là duration (duration="5s")`,
	"speed":    `nhanh chậm của lúc chuyển là việc của engine; mỗi trang ở lại bao lâu là duration (duration="5s")`,
	"loop":     `next() ở trang cuối đã quay về trang đầu, previous() ở trang đầu về trang cuối: bỏ loop`,
	"infinite": `next() ở trang cuối đã quay về trang đầu, previous() ở trang đầu về trang cuối: bỏ infinite`,
	"index":    `trang đang hiện là current (current="0" là trang đầu)`,
	"active":   `trang đang hiện là current (current="0" là trang đầu)`,
	"count":    `count là số trang, engine đếm từ các <item> của dải: bỏ count`,
	"playing":  `carousel tự chạy khi có duration (duration="5s"); không tự chạy thì bỏ duration: bỏ playing`,
}

// carouselWords are the words a screen reader hears of a carousel, in the page's language.
func (r *renderer) carouselWords() (region, slide, label string) {
	if r.english() {
		return "carousel", "slide", "Carousel"
	}
	return "trình chiếu", "trang chiếu", "Trình chiếu"
}

// carouselHost reads the carousel's own words off its host (alt, transition, duration, current) and finds its
// track; the host goes on as the host of a component without a UI. It gives the instance what it starts with.
func (r *renderer) carouselHost(n *Node, state map[string]any) (*Node, *host, *carouselScope, error) {
	scope := &carouselScope{slides: map[*Node]int{}}
	transition, label, labelBound := transitions[0], "", false
	var startAt *Attribute
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		if says := carouselStrangers[base]; says != "" {
			return false, at(n, a, "carousel không có %s: %s", base, says)
		}
		if transitionStrangers[base] {
			return false, at(n, a, "carousel không có %s: cách một trang nhường trang sau là transition (slide, fade hay none)", base)
		}
		switch base {
		case "role", "aria-live", "aria-label", "aria-roledescription":
			return false, at(n, a, "vai trò của carousel là việc của engine (role=\"region\", trang chiếu đánh số): bỏ %s; tên của nó là alt (alt=\"Món nổi bật\")", base)
		case "transition":
			if value := strings.TrimSpace(a.Value); chain == "" && slideDirection(value) {
				return false, at(n, a, `transition=%q: trang chiếu trượt theo dòng chữ của trang, trang sau đến từ cuối dòng; transition chỉ là một chữ: slide, fade hay none`, value)
			}
			value, err := transitionOf(n, a, chain, "carousel")
			transition = value
			return true, err
		case "duration":
			ms, err := carouselDuration(n, a, chain)
			if err == nil && ms < toastShortest {
				// drawn as written: a page goes by before it is read
				r.warn(n, a, "%s", `duration="`+a.Value+`" dưới 2 giây: người xem đọc không kịp một trang; viết duration từ "2s"`)
			}
			scope.duration, scope.durationAt = ms, a
			return true, err
		case "alt":
			if chain != "" {
				return false, at(n, a, "alt của carousel không nhận tiền tố: nó là tên của carousel")
			}
			if strings.Contains(a.Value, "{{") {
				labelBound = true // from data: the host's own alt, bound to aria-label like any box's
				return false, nil
			}
			label = strings.TrimSpace(a.Value)
			return true, nil
		case "current":
			if chain != "" || strings.Contains(a.Value, "{{") {
				return false, nil
			}
			start, err := strconv.Atoi(strings.TrimSpace(a.Value))
			if err != nil || start < 0 {
				return false, at(n, a, `current là trang nó bắt đầu, đếm từ 0 (current="0" là trang đầu), không phải %q`, a.Value)
			}
			scope.start, startAt = start, a
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	count, err := r.carouselTrack(n, scope)
	if err != nil {
		return nil, nil, nil, err
	}
	if startAt != nil && scope.count >= 0 && scope.start >= scope.count {
		return nil, nil, nil, at(n, startAt, `current="%d" quá số trang: dải có %d trang, đếm từ 0 (trang cuối là current="%d")`, scope.start, scope.count, scope.count-1)
	}
	region, _, word := r.carouselWords()
	own := &host{attributes: []string{`role="region"`, `aria-roledescription="` + region + `"`}}
	if !labelBound {
		if label == "" {
			label = word
		}
		own.attributes = append(own.attributes, `aria-label="`+html.EscapeString(label)+`"`)
	}
	own.classes = []string{"carousel"}
	if transition == "fade" || transition == "none" {
		own.classes = append(own.classes, "carousel-"+transition)
	}
	if scope.duration > 0 {
		own.inputs = append(own.inputs, "duration: "+strconv.FormatFloat(scope.duration, 'f', -1, 64))
		scope.host = n
		r.playing = append(r.playing, scope) // told at the end if nothing on the page stops it (lintCarousels)
	}
	if startAt != nil {
		own.inputs = append(own.inputs, "current: "+strconv.Itoa(scope.start))
	}
	if scope.count < 0 {
		// slides a for repeats or an if keeps: how many is the page's data's, read here as the call could have
		// written it (count="{{ 2 + (mons).length }}")
		own.inputs = append(own.inputs, "count: "+strconv.Itoa(count))
	}
	state["count"], state["playing"], state["current"] = float64(count), scope.duration > 0, float64(scope.start)
	return copied, own, scope, nil
}

// carouselTrack finds the track, the one <list> right inside the host, and its slides; it gives how many slides the
// server paints (those written out, and the rows of a for and the item of an if the page's data keeps).
func (r *renderer) carouselTrack(n *Node, scope *carouselScope) (int, error) {
	const how = `<list><item>…</item><item>…</item></list>`
	for _, child := range n.Elements() {
		if child.Tag != "list" {
			continue
		}
		if scope.track != nil {
			return 0, at(child, nil, "carousel có một dải trang: <list> ở %s:%d:%d đã là dải của nó; gộp các trang vào một <list>", scope.track.File, scope.track.Line, scope.track.Column)
		}
		scope.track = child
	}
	if scope.track == nil {
		return 0, at(n, nil, "carousel cần một <list> ngay bên trong, mỗi <item> là một trang chiếu: %s", how)
	}
	for i := range scope.track.Attributes {
		a := &scope.track.Attributes[i]
		if _, name := splitPrefix(a.Name); name == "flex" || name == "grid" || name == "columns" {
			return 0, at(scope.track, a, "dải trang của carousel là một hàng engine đặt (cuộn ngang, dừng ở mỗi trang): bỏ %s; độ rộng của mỗi trang là width trên <item>", a.Name)
		}
	}
	written, painted := 0, 0
	for _, item := range scope.track.Elements() {
		switch item.Tag {
		case "empty":
			continue // what the list shows when it has no item: no slide
		case "fragment":
			return 0, at(item, nil, "mỗi trang của carousel là một <item> viết thẳng trong <list> (for và if đặt trên <item>): bỏ <fragment>")
		}
		forAttribute, repeated := item.Attribute("for")
		ifAttribute, kept := item.Attribute("if")
		if !repeated && !kept {
			scope.slides[item] = written
			written++
			painted++
			continue
		}
		scope.slides[item] = -1
		rows, err := r.keptRows(item, repeated, &forAttribute, kept, &ifAttribute)
		if err != nil {
			return 0, err
		}
		painted += rows
	}
	if len(scope.slides) == 0 {
		return 0, at(scope.track, nil, "dải trang của carousel chưa có trang nào: mỗi <item> trong %s là một trang", how)
	}
	scope.count = written
	if painted != written {
		scope.count = -1
	}
	for _, item := range scope.track.Elements() {
		if scope.slides[item] < 0 {
			scope.count = -1
		}
	}
	if a, ok := scope.track.Attribute("id"); ok && strings.TrimSpace(a.Value) != "" {
		scope.trackID = strings.TrimSpace(a.Value)
	} else {
		r.carousels++
		scope.trackID = fmt.Sprintf("kitwork-carousel-%d", r.carousels)
	}
	return painted, nil
}

// keptRows is how many slides an item written with for or if draws now: the rows of its list its if keeps.
func (r *renderer) keptRows(item *Node, repeated bool, forAttribute *Attribute, kept bool, ifAttribute *Attribute) (int, error) {
	keeps := func() (bool, error) {
		if !kept {
			return true, nil
		}
		shown, err := r.value(item, ifAttribute, ifAttribute.Value)
		return shown.ToBoolean(), err
	}
	if !repeated {
		if shown, err := keeps(); err != nil || !shown {
			return 0, err
		}
		return 1, nil
	}
	itemName, index, list, _, ok := forHead(forAttribute.Value)
	if !ok {
		return 0, at(item, forAttribute, "for viết là \"x of list\" (hoặc \"x, i of list\")")
	}
	rows, err := r.value(item, forAttribute, list)
	if err != nil || !rows.IsArray() {
		return 0, err
	}
	count := 0
	for i, each := range rows.Items() {
		takeItem := r.eval.put(itemName, each)
		takeIndex := func() {}
		if index != "" {
			takeIndex = r.eval.put(index, value.Number(float64(i)))
		}
		shown, err := keeps()
		takeIndex()
		takeItem()
		if err != nil {
			return 0, err
		}
		if shown {
			count++
		}
	}
	return count, nil
}

// slideDirection says a transition is slide and a direction (slide-left, slide up): a carousel's slides only move
// along the line.
func slideDirection(value string) bool {
	first, rest, ok := strings.Cut(strings.ToLower(value), "-")
	if !ok {
		first, rest, _ = strings.Cut(strings.ToLower(value), " ")
	}
	return first == "slide" && transitionDirections[strings.TrimSpace(rest)]
}

// carouselDuration reads duration= on a carousel: how long each slide stays while it plays by itself, a duration
// from 2 seconds to 60.
func carouselDuration(n *Node, a *Attribute, chain string) (float64, error) {
	value := strings.TrimSpace(a.Value)
	switch {
	case chain != "":
		return 0, at(n, a, "duration của carousel không nhận tiền tố: nó được quyết một lần khi trang dựng")
	case a.Bare || value == "":
		return 0, at(n, a, `duration cần một thời lượng: mỗi trang ở lại bao lâu (duration="5s"); không tự chạy thì bỏ duration`)
	case strings.Contains(value, "{{"):
		return 0, at(n, a, `duration của carousel là một thời lượng cố định (duration="5s"), không có {{ }}: nó được quyết một lần khi trang dựng`)
	}
	d := readDuration(value)
	switch d.problem {
	case durationNegative:
		return 0, at(n, a, `duration không nhận thời lượng âm (%s): viết một thời lượng như duration="5s"`, value)
	case durationZero:
		return 0, at(n, a, `duration="%s": không tự chạy thì bỏ duration`, a.Value)
	case durationNoUnit:
		return 0, at(n, a, `duration="%s" thiếu đơn vị: viết duration="%s"`, a.Value, d.suggestion(durationMilliseconds))
	case durationTwoUnits, durationForeign:
		return 0, at(n, a, `duration="%s" %s: viết duration="%s"`, a.Value, d.reason(), d.suggestion(durationMilliseconds))
	case durationNotOne:
		return 0, at(n, a, `duration="%s" không phải một thời lượng: viết duration="5s" hay duration="800ms"`, a.Value)
	}
	if d.ms > 60000 {
		return 0, at(n, a, `duration="%s" dài hơn 60 giây: mỗi trang ở lại tới khi người xem chuyển thì bỏ duration`, a.Value)
	}
	return d.ms, nil
}

// carouselStop is a call that stops a carousel playing by itself: pause() or toggle(), bare inside it.
var carouselStop = regexp.MustCompile(`(?:^|[^\w$.])(?:pause|toggle)\s*\(`)

// lintCarousels tells a carousel that plays by itself with nothing on the page to stop it (WCAG 2.2.2): no click
// inside it calls pause() or toggle(), and nothing on the page calls them through its alias. page is the whole
// page drawn, so a button anywhere counts.
func (r *renderer) lintCarousels(page *Node) {
	for _, s := range r.playing {
		stops := false
		var inside func(nodes []*Node)
		inside = func(nodes []*Node) {
			for _, child := range nodes {
				if stops || child.IsText() {
					continue
				}
				if a, ok := child.Attribute("click"); ok && carouselStop.MatchString(a.Value) {
					stops = true
					return
				}
				inside(child.Children)
			}
		}
		inside(s.host.Children)
		if as, ok := s.host.Attribute("as"); ok && !stops {
			call := regexp.MustCompile(regexp.QuoteMeta(strings.TrimSpace(as.Value)) + `\.(?:pause|toggle)\s*\(`)
			var anywhere func(nodes []*Node)
			anywhere = func(nodes []*Node) {
				for _, child := range nodes {
					if stops {
						return
					}
					if child.IsText() {
						stops = call.MatchString(child.Text)
						continue
					}
					for _, a := range child.Attributes {
						if a.Name != "as" && call.MatchString(a.Value) {
							stops = true
							return
						}
					}
					anywhere(child.Children)
				}
			}
			anywhere([]*Node{page})
		}
		if !stops {
			r.warn(s.host, s.durationAt, "%s", `carousel tự chạy (duration="`+strings.TrimSpace(s.durationAt.Value)+`") mà trang không có cách dừng: nội dung tự chuyển cần một nút dừng (WCAG 2.2.2); viết <button click="toggle()"> trong <carousel>, hay gọi $…pause() ở chỗ khác`)
		}
	}
}

// goCall reads a click that is one go(…) into the source of its argument.
var goCall = regexp.MustCompile(`^\s*go\((.*)\)\s*;?\s*$`)

// carouselAttributes are what the engine adds to an element inside a carousel: the track's id, focus and live
// region; a slide's role, name and aria-current; a dot's place, the track it controls and aria-current. For a dot,
// click is the handler the browser runs instead of the one written (go(Number($this.value))), "" for any other.
func (r *renderer) carouselAttributes(n *Node, live *grouped) (attributes []string, classes []string, click string, err error) {
	// what it reads of the carousel is its file's (the track, the slides, current, duration) and the page's count of
	// carousels, which a part's key holds (frozen.go)
	s := r.carousel
	_, slide, _ := r.carouselWords()
	if n == s.track {
		if _, ok := n.Attribute("id"); !ok {
			attributes = append(attributes, `id="`+html.EscapeString(s.trackID)+`"`)
		}
		state := "polite"
		if playing, _ := r.eval.globals["playing"]; playing.ToBoolean() {
			state = "off"
		}
		attributes = append(attributes, `tabindex="0"`, `aria-live="`+state+`"`)
		live.binds = append(live.binds, "aria-live: playing ? 'off' : 'polite'")
		return attributes, []string{"carousel-track"}, "", nil
	}
	if place, ok := s.slides[n]; ok {
		attributes = append(attributes, `aria-roledescription="`+slide+`"`)
		if place >= 0 && s.count > 0 {
			if _, named := n.Attribute("alt"); !named {
				attributes = append(attributes, fmt.Sprintf(`aria-label="%d / %d"`, place+1, s.count))
			}
			attributes = append(attributes, `aria-current="`+strconv.FormatBool(place == s.start)+`"`)
		}
		return attributes, nil, "", nil
	}
	if n.Tag != "button" {
		return nil, nil, "", nil
	}
	a, ok := n.Attribute("click")
	if !ok {
		return nil, nil, "", nil
	}
	m := goCall.FindStringSubmatch(a.Value)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return nil, nil, "", nil
	}
	argument := strings.TrimSpace(m[1])
	compiled, err := r.site.compile(argument)
	if err != nil {
		return nil, nil, "", nil // not one argument: the event's own error, or a button like next()
	}
	if client, err := r.onClient(n, &a, argument, compiled.reads); err != nil || client {
		return nil, nil, "", err // go(current + 2): a button that moves, no dot
	}
	if v, ok := n.Attribute("value"); ok {
		return nil, nil, "", at(n, &v, "value của một chấm là trang go(…) trỏ tới, engine viết từ go(%s): bỏ value", argument)
	}
	for i := range n.Attributes {
		if name := n.Attributes[i].Name; strings.HasPrefix(name, "selected:") {
			r.warn(n, &n.Attributes[i], "%s", name+`: một chấm của carousel là trang đang hiện khi go(…) trỏ tới nó, và vẽ bằng active: (aria-current), không phải selected: (của tab); viết active:`+strings.TrimPrefix(name, "selected:"))
		}
	}
	place, err := r.value(n, &a, argument)
	if err != nil {
		return nil, nil, "", err
	}
	text := place.ToString().Text()
	attributes = append(attributes, `aria-controls="`+html.EscapeString(s.trackID)+`"`, `value="`+html.EscapeString(text)+`"`,
		`aria-current="`+strconv.FormatBool(text == strconv.Itoa(s.start))+`"`)
	return attributes, nil, "go(Number($this.value))", nil
}
