package ui

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Kitwork's countdown (decisions of 19–20/10/2026; moved from the first engine on 06/11/2026, slice 4): time that
// runs out, counted from the moment play() starts it. It has no UI of its own: the author draws the time left,
// once, in a <time>, and the buttons:
//
//	<countdown as="$tea" duration="5m" finish="$toast.success('Trà đã sẵn sàng')">
//	  <text>Còn <time>{{ minutes }}:{{ String(seconds).padStart(2, '0') }}</time></text>
//	  <button click="toggle()">{{ playing ? 'Tạm dừng' : 'Chạy' }}</button>
//	</countdown>
//
//   - duration, its one word, is whole seconds from 1s to 24h, handed to the instance in milliseconds. The server
//     paints the start: all of duration left, not playing.
//   - The one <time> without value inside the host is the time left: role="timer", class countdown-time, and a
//     datetime bound to the parts (PT0H5M0S).
//   - The fields written in the <time> (<input value="{{ minutes }}" />) are the clock itself, its segments: the
//     engine writes each (two digits, digits only, a spinbutton with its range) and binds none, the countdown's JS
//     owning them; the <time> holding them is a group named by its alt, not a timer.
//   - finish is its event, heard on its host only.
//
// What a click writes to a part (minutes = 120 outside its range, a part the <time> does not show) is checked by the
// first engine and not yet here.

// countdownScope is one countdown host: what r.countdown points to while its inside is drawn.
type countdownScope struct {
	duration float64
	time     *Node
	label    string // the <time>'s alt when it holds segments: the group's name
	segments map[*Node]*countdownSegment
	order    []*countdownSegment
}

// countdownSegment is a field in the countdown's <time>: one part of the time; low and high what an edit sets it to.
type countdownSegment struct {
	node      *Node
	part      string
	low, high float64
}

const countdownLongest = 24 * 60 * 60 * 1000

var (
	countdownLeft     = []string{"remaining", "hours", "minutes", "seconds"}
	countdownSettable = []string{"hours", "minutes", "seconds"}
	countdownMost     = map[string]float64{"hours": 24, "minutes": 59, "seconds": 59}
	countdownUnits    = map[string]string{"hours": "giờ", "minutes": "phút", "seconds": "giây"}
)

// countdownStrangers are the words of other countdowns and of the web, written on the tag, each with what to write
// here; %s is the word as written.
var countdownStrangers = func() map[string]string {
	out := map[string]string{}
	add := func(message string, words ...string) {
		for _, word := range words {
			out[word] = message
		}
	}
	add(`countdown không có %s: chạy ngay khi trang mở là ready="play()"`, "autoplay", "autostart", "start")
	add(`countdown không có %s: đếm bao lâu là duration (duration="5m")`, "time", "length", "total")
	add(`countdown không có %s: trong Kitwork timeout là giới hạn thời gian của một request; đếm bao lâu là duration, hết giờ là finish`, "timeout")
	add(`countdown không có %[1]s: countdown đếm từng giây, engine lo; bỏ %[1]s`, "interval", "step", "precision", "millisecond")
	add(`countdown không có %s: thời gian do người viết vẽ, <time>{{ minutes }}:{{ String(seconds).padStart(2, '0') }}</time>`, "format")
	add(`countdown không có %s: hết giờ thì chạy lại là finish="play()"`, "loop", "repeat")
	add(`countdown chưa đếm tới một thời điểm: đếm bao lâu là duration (duration="10m"); until chưa có`, "until", "deadline", "date", "target", "at")
	add(`countdown chưa có %s: chạy lúc nào là ready="play()" hay một nút click="play()"`, "delay")
	add(`countdown không có %s: chữ số đổi ngay; hiệu ứng cho chữ số là animate-… trên phần con`, "transition", "animation", "animate", "motion", "effect")
	add(`countdown không có %s: thời gian còn lại không giữ qua lần tải lại; một hạn phải giữ là việc của server`, "persist")
	add(`countdown chưa có %s: chữ quanh <time> đã nói nó đếm gì (Còn <time>…</time>)`, "alt")
	add(`countdown không có %s: seconds là phần giây còn lại, engine tính từ duration; đếm 30 giây là duration="30s"`, "seconds")
	add(`countdown không có %s: minutes là phần phút còn lại, engine tính từ duration; đếm 5 phút là duration="5m"`, "minutes")
	add(`countdown không có %s: hours là phần giờ còn lại, engine tính từ duration; đếm 2 giờ là duration="2h"`, "hours")
	add(`countdown không có %s: remaining là thời gian còn lại (mili giây), engine tính từ duration; đếm 30 giây là duration="30s"`, "remaining")
	add(`countdown không có %s: playing là trạng thái của nó; chạy khi trang mở là ready="play()"`, "playing")
	add(`countdown không có %s: đang dừng là !playing`, "paused")
	add(`%s là hàm của countdown, không phải một chữ trên thẻ: gọi nó trong một sự kiện (ready="play()", click="pause()")`, "play", "pause", "toggle", "stop")
	add(`vai trò của countdown là việc của engine (role="timer" trên <time> của nó): bỏ %s`, "role")
	add(`countdown không có sự kiện %s: hết giờ là finish (finish="$toast.info('Hết giờ')")`, "end", "ended", "done", "complete", "expire", "onfinish")
	add(`countdown không gửi %s: remaining, hours, minutes, seconds tự vẽ lại mỗi giây; việc lúc hết giờ là finish`, "tick")
	return out
}()

// countdownParts are the hours, minutes and seconds of a time left, in milliseconds.
func countdownParts(ms float64) (hours, minutes, seconds float64) {
	whole := math.Ceil(ms / 1000)
	return math.Floor(whole / 3600), math.Mod(math.Floor(whole/60), 60), math.Mod(whole, 60)
}

func countdownNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func countdownRank(part string) int {
	for i, name := range countdownSettable {
		if name == part {
			return i
		}
	}
	return -1
}

// countdownHost reads the countdown's own word (duration) and the words it does not have off its host, and finds
// the <time> that holds the time left; the host goes on as the host of a component without a UI.
func (r *renderer) countdownHost(n *Node, state map[string]any) (*Node, *host, *countdownScope, error) {
	scope := &countdownScope{segments: map[*Node]*countdownSegment{}}
	var written *Attribute
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		head, _, _ := strings.Cut(a.Name, ":")
		says, word := countdownStrangers[base], base
		if says == "" && strings.HasPrefix(base, "aria-") {
			says = countdownStrangers["role"]
		}
		if says == "" && countdownStrangers[head] != "" {
			says, word = countdownStrangers[head], head // an event of other countdowns, with its modifiers
		}
		if says != "" {
			if strings.Contains(says, "%") {
				says = fmt.Sprintf(says, word)
			}
			return false, at(n, a, "%s", says)
		}
		if base == "duration" {
			ms, err := countdownDuration(n, a, chain)
			scope.duration, written = ms, a
			return true, err
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(n.Significant()) == 0 {
		return nil, nil, nil, at(n, nil, `countdown rỗng: vẽ thời gian còn lại bên trong, <countdown duration="60s"><time>{{ remaining / 1000 }}</time></countdown>`)
	}
	if written == nil {
		return nil, nil, nil, at(n, nil, `countdown cần duration: đếm bao lâu (duration="5m")`)
	}
	if err := r.countdownTime(n, scope, written); err != nil {
		return nil, nil, nil, err
	}
	finishes := false
	for _, a := range n.Attributes {
		if head, _, _ := strings.Cut(a.Name, ":"); isEvent(a.Name) && strings.Contains(","+head+",", ",finish,") {
			finishes = true
		}
	}
	if !finishes {
		// the time runs out unheard: role="timer" does not read the count
		r.warn(n, nil, "%s", `countdown không có finish: hết giờ thì người dùng trình đọc màn hình không biết (role="timer" không đọc số đếm); viết finish="$toast.info('Hết giờ')" hay finish="$reader.say('Hết giờ')"`)
	}
	r.countdowns = append(r.countdowns, n) // told at the end if nothing on the page starts it (lintCountdowns)
	hours, minutes, seconds := countdownParts(scope.duration)
	state["duration"], state["remaining"] = scope.duration, scope.duration
	state["hours"], state["minutes"], state["seconds"], state["playing"] = hours, minutes, seconds, false
	return copied, &host{inputs: []string{"duration: " + countdownNumber(scope.duration)}}, scope, nil
}

// countdownStart is a call that starts a countdown: play() or toggle(), bare inside it.
var countdownStart = regexp.MustCompile(`(?:^|[^\w$.])(?:play|toggle)\s*\(`)

// lintCountdowns tells a countdown nothing on the page ever starts: no ready="play()" on it, no click inside it
// calling play() or toggle(), nothing calling them through its alias. A finish that plays it again starts nothing.
func (r *renderer) lintCountdowns(page *Node) {
	for _, n := range r.countdowns {
		starts := false
		for _, a := range n.Attributes {
			head, _, _ := strings.Cut(a.Name, ":")
			if isEvent(a.Name) && head != "finish" && countdownStart.MatchString(a.Value) {
				starts = true
			}
		}
		var inside func(nodes []*Node)
		inside = func(nodes []*Node) {
			for _, child := range nodes {
				if starts || child.IsText() {
					continue
				}
				for _, a := range child.Attributes {
					if isEvent(a.Name) && countdownStart.MatchString(a.Value) {
						starts = true
						return
					}
				}
				inside(child.Children)
			}
		}
		inside(n.Children)
		if as, ok := n.Attribute("as"); ok && !starts {
			call := regexp.MustCompile(regexp.QuoteMeta(strings.TrimSpace(as.Value)) + `\.(?:play|toggle)\s*\(`)
			var anywhere func(nodes []*Node)
			anywhere = func(nodes []*Node) {
				for _, child := range nodes {
					if starts || child.IsText() {
						continue
					}
					for _, a := range child.Attributes {
						if isEvent(a.Name) && call.MatchString(a.Value) {
							starts = true
							return
						}
					}
					anywhere(child.Children)
				}
			}
			anywhere([]*Node{page})
		}
		if !starts {
			r.warn(n, nil, "%s", `countdown không bao giờ chạy: không có ready="play()", không nút nào gọi play() hay toggle(); viết ready="play()" để chạy khi trang mở`)
		}
	}
}

// countdownAliasPart reads $alias.<part> in an expression: the countdown's own names through its alias.
var countdownAliasPart = regexp.MustCompile(`\$[A-Za-z][A-Za-z0-9_]*\.(remaining|hours|minutes|seconds)\b`)

// countdownTime finds the one <time> without value inside the host (never inside a component nested in it), the
// time left, checks it is one and that it shows all of the time, and reads its segments.
func (r *renderer) countdownTime(n *Node, scope *countdownScope, written *Attribute) error {
	var second, repeated *Node
	var find func(nodes []*Node, repeats *Node)
	find = func(nodes []*Node, repeats *Node) {
		for _, child := range nodes {
			if child.IsText() {
				continue
			}
			if _, component := componentState[child.Tag]; component || strings.Contains(child.Tag, "-") {
				continue
			}
			around := repeats
			if _, ok := child.Attribute("for"); ok && around == nil {
				around = child
			}
			if _, valued := child.Attribute("value"); child.Tag == "time" && !valued {
				switch {
				case scope.time == nil:
					scope.time, repeated = child, around
				case second == nil:
					second = child
				}
			}
			find(child.Children, around)
		}
	}
	find(n.Children, nil)
	t := scope.time
	switch {
	case t == nil:
		return at(n, nil, `countdown cần một <time> chứa thời gian còn lại, engine viết role="timer" lên nó: <time>{{ minutes }}:{{ String(seconds).padStart(2, '0') }}</time>`)
	case second != nil:
		return at(second, nil, "countdown có một thời gian còn lại: <time> ở %s:%d:%d đã là của nó; gộp vào một <time>", t.File, t.Line, t.Column)
	case repeated == t:
		a, _ := t.Attribute("for")
		return at(t, &a, "countdown có một thời gian còn lại: <time> của nó không lặp bằng for; bỏ for")
	case repeated != nil:
		a, _ := repeated.Attribute("for")
		return at(repeated, &a, "countdown có một thời gian còn lại: for này lặp <time> ở %s:%d:%d của nó; đặt <time> ngoài for", t.File, t.Line, t.Column)
	}
	reads := map[string]bool{}
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, child := range nodes {
			if !child.IsText() {
				walk(child.Children)
				continue
			}
			rest := child.Text
			for {
				start := strings.Index(rest, "{{")
				if start < 0 {
					break
				}
				end := closingBraces(rest[start+2:])
				if end < 0 {
					break
				}
				part := strings.TrimSpace(rest[start+2 : start+2+end])
				if compiled, err := r.site.compile(part); err == nil {
					for _, name := range compiled.reads {
						if contains(countdownLeft, name) {
							reads[name] = true
						}
					}
				}
				for _, m := range countdownAliasPart.FindAllStringSubmatch(part, -1) {
					reads[m[1]] = true
				}
				rest = rest[start+2+end+2:]
			}
		}
	}
	walk(t.Children)
	if err := r.countdownSegments(t, scope, written); err != nil {
		return err
	}
	for _, segment := range scope.order {
		reads[segment.part] = true
	}
	if len(reads) == 0 {
		return at(t, nil, "<time> của countdown không đọc thời gian còn lại: đọc remaining, hours, minutes hay seconds bên trong")
	}
	if unshown := countdownUnshown(reads, scope.duration); unshown != "" {
		return at(t, nil, `duration="%s" %s`, strings.TrimSpace(written.Value), unshown)
	}
	if len(scope.order) > 0 {
		var has, lacks []string
		for _, part := range countdownSettable[countdownRank(scope.order[0].part):] {
			if scope.segmentOf(part) != nil {
				has = append(has, part)
			} else {
				lacks = append(lacks, part)
			}
		}
		if len(lacks) > 0 {
			var clock []string
			for _, part := range countdownSettable[countdownRank(scope.order[0].part):] {
				clock = append(clock, `<input value="{{ `+part+` }}" />`)
			}
			return at(t, nil, "<time> của countdown có ô %s mà không có ô %s: các ô là một đồng hồ đọc tới giây, không thiếu phần nào ở giữa; viết %s",
				strings.Join(has, ", "), strings.Join(lacks, ", "), strings.Join(clock, ":"))
		}
		if alt, ok := t.Attribute("alt"); ok {
			scope.label = alt.Value
		}
	}
	return nil
}

func (s *countdownScope) segmentOf(part string) *countdownSegment {
	for _, segment := range s.order {
		if segment.part == part {
			return segment
		}
	}
	return nil
}

// countdownUnshown is what of a time of ms a <time> reading reads cannot show, "" when it shows all of it.
func countdownUnshown(reads map[string]bool, ms float64) string {
	longer := func(than float64, what string) string {
		if ms == than {
			return "dài đúng " + what
		}
		return "dài hơn " + what
	}
	switch {
	case reads["remaining"]:
	case ms >= 60*1000 && reads["seconds"] && !reads["minutes"]:
		reading := "chỉ đọc seconds"
		if reads["hours"] {
			reading = "đọc hours và seconds mà không đọc minutes"
		}
		return longer(60*1000, "một phút") + " mà <time> " + reading + ": seconds đếm từ 59 về 0 rồi quay lại; đọc thêm minutes"
	case ms >= 60*60*1000 && !reads["hours"]:
		return longer(60*60*1000, "một giờ") + " mà <time> không đọc hours: minutes chỉ tới 59; đọc thêm hours"
	}
	return ""
}

// countdownPart reads a segment's value, value="{{ minutes }}", into its part.
var countdownPart = regexp.MustCompile(`^\s*\{\{\s*(hours|minutes|seconds)\s*\}\}\s*$`)

// countdownSegments finds the fields written in the countdown's <time>, never inside a component nested there:
// each is one part of the time.
func (r *renderer) countdownSegments(t *Node, scope *countdownScope, written *Attribute) error {
	var walk func(nodes []*Node, repeats *Node) error
	walk = func(nodes []*Node, repeats *Node) error {
		for _, child := range nodes {
			if child.IsText() {
				continue
			}
			if _, component := componentState[child.Tag]; component || strings.Contains(child.Tag, "-") {
				continue
			}
			around := repeats
			if _, ok := child.Attribute("for"); ok && around == nil {
				around = child
			}
			switch child.Tag {
			case "input":
				if err := countdownSegmentOf(child, scope, around, written); err != nil {
					return err
				}
				continue
			case "select", "textarea":
				return at(child, nil, `<%s> trong <time> của countdown không phải một phần của thời gian: mỗi phần là một <input value="{{ minutes }}" />, engine viết nó (hai chữ số, bàn phím số); nút chọn là của người viết, đặt ngoài <time>: <button click="minutes++">+1</button>`, child.Tag)
			}
			if err := walk(child.Children, around); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(t.Children, nil)
}

// countdownSegmentOf reads one field in the countdown's <time> as a segment: its value one part, bare; one field a
// part, in a clock's order, never repeated; the field the engine's to write; min and max narrowing its range.
func countdownSegmentOf(n *Node, scope *countdownScope, repeats *Node, written *Attribute) error {
	value, _ := n.Attribute("value")
	m := countdownPart.FindStringSubmatch(value.Value)
	if m == nil {
		return at(n, &value, `ô trong <time> của countdown là một phần của thời gian: value="{{ minutes }}" (hours, minutes hay seconds, tên trần); ô khác đặt ngoài <time>`)
	}
	part := m[1]
	if repeats != nil {
		a, _ := repeats.Attribute("for")
		return at(repeats, &a, "một phần của thời gian không lặp bằng for: mỗi phần một ô; bỏ for")
	}
	if other := scope.segmentOf(part); other != nil {
		return at(n, &value, "<time> của countdown đã có ô %s ở %s:%d:%d: mỗi phần một ô", part, other.node.File, other.node.Line, other.node.Column)
	}
	if k := len(scope.order); k > 0 && countdownRank(scope.order[k-1].part) > countdownRank(part) {
		before := scope.order[k-1]
		return at(n, &value, `ô %s đứng sau ô %s ở %s:%d:%d: các ô của <time> theo thứ tự của đồng hồ, giờ rồi phút rồi giây: <input value="{{ %[1]s }}" />:<input value="{{ %[2]s }}" />`,
			part, before.part, before.node.File, before.node.Line, before.node.Column)
	}
	most := countdownMost[part]
	segment := &countdownSegment{node: n, part: part, high: most}
	var minAt, maxAt *Attribute
	for i := range n.Attributes {
		a := &n.Attributes[i]
		switch a.Name {
		case "type":
			if strings.TrimSpace(a.Value) == "number" {
				return at(n, a, `một phần của thời gian không có type="number": engine viết ô (hai chữ số, bàn phím số, không có nút tăng giảm); bỏ type, nút tăng giảm là của người viết, <button click="%s++">+1</button>`, part)
			}
			return at(n, a, "một phần của thời gian không có type: engine viết ô (hai chữ số, bàn phím số); bỏ type")
		case "inputmode", "autocomplete", "step", "pattern":
			return at(n, a, `một phần của thời gian không có %[1]s: engine viết ô (inputmode="numeric", autocomplete="off"); bỏ %[1]s`, a.Name)
		case "readonly", "disabled":
			return at(n, a, "một phần của thời gian không có %s: bấm vào thì countdown dừng ở đúng số đó, Enter hay rời <time> thì chạy tiếp từ số đã sửa; chỉ để hiện thì viết {{ %s }} trong <time>", a.Name, part)
		case "min", "max":
			example := "30"
			if part == "hours" {
				example = "12"
			}
			if a.Name == "min" {
				example = "1"
			}
			text := strings.TrimSpace(a.Value)
			if strings.Contains(text, "{{") {
				return at(n, a, `%s của một phần là một số cố định (%s="%s"), không có {{ }}: engine viết khoảng của ô khi trang dựng`, a.Name, a.Name, example)
			}
			number, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsInf(number, 0) || number != math.Floor(number) {
				return at(n, a, `%s="%s" của %s không phải một số nguyên: viết một số từ 0 tới %d, như %s="%s"`, a.Name, a.Value, part, int(most), a.Name, example)
			}
			if number < 0 || number > most {
				return at(n, a, `%s="%s" rộng hơn khoảng của %s (0 tới %d): min, max của một phần chỉ hẹp lại, như %s="%s"`, a.Name, a.Value, part, int(most), a.Name, example)
			}
			if a.Name == "min" {
				segment.low, minAt = number, a
			} else {
				segment.high, maxAt = number, a
			}
		default:
			if head, _, _ := strings.Cut(a.Name, ":"); isEvent(a.Name) && strings.Contains(","+head+",", ",input,") {
				return at(n, a, `một phần của thời gian không gửi input: countdown nhận từng phím và tự viết ô; ô đã đổi gửi change khi focus rời nó (Enter, Tab), change="…"`)
			}
		}
	}
	if segment.low > segment.high {
		return at(n, minAt, `min="%s" lớn hơn max="%s": ô %s không còn số nào`, strings.TrimSpace(minAt.Value), strings.TrimSpace(maxAt.Value), part)
	}
	hours, minutes, seconds := countdownParts(scope.duration)
	start := map[string]float64{"hours": hours, "minutes": minutes, "seconds": seconds}[part]
	if start < segment.low || start > segment.high {
		bound := maxAt
		if start < segment.low {
			bound = minAt
		}
		return at(n, bound, `duration="%s" bắt đầu ở %s %s, ngoài khoảng của ô %s (%s tới %s): viết duration trong khoảng đó, hay nới min, max`,
			strings.TrimSpace(written.Value), countdownNumber(start), countdownUnits[part], part, countdownNumber(segment.low), countdownNumber(segment.high))
	}
	scope.segments[n] = segment
	scope.order = append(scope.order, segment)
	return nil
}

// countdownSeconds is the duration a countdown suggests for one written another way: whole seconds, rounded up.
func countdownSeconds(d duration) string {
	if math.Mod(d.ms, 1000) == 0 && d.ms > 0 {
		return d.suggestion(durationSeconds)
	}
	return strconv.FormatFloat(math.Max(1, math.Ceil(d.ms/1000)), 'f', -1, 64) + "s"
}

var countdownUnitWords = map[string]string{"ms": "mili giây", "s": "giây", "m": "phút", "h": "giờ"}

// countdownDuration reads duration= on a countdown: a literal duration of whole seconds from 1s to 24h, in
// milliseconds; a number written without its unit is seconds (300 → 300s).
func countdownDuration(n *Node, a *Attribute, chain string) (float64, error) {
	value := strings.TrimSpace(a.Value)
	switch {
	case chain != "":
		return 0, at(n, a, "duration của countdown không nhận tiền tố: nó được quyết một lần khi trang dựng")
	case a.Bare || value == "":
		return 0, at(n, a, `countdown cần duration: đếm bao lâu (duration="5m")`)
	case strings.Contains(value, "{{"):
		return 0, at(n, a, `duration của countdown là một thời lượng cố định (duration="5m"), không có {{ }}: nó được quyết một lần khi trang dựng`)
	}
	d := readDuration(value)
	if d.problem == durationNoUnit || d.problem == durationTwoUnits || d.problem == durationForeign {
		meant := d.ms
		if d.problem == durationNoUnit {
			number, _ := strconv.ParseFloat(d.number, 64)
			meant = number * 1000
		}
		switch {
		case meant == 0:
			return 0, at(n, a, `duration="%s": countdown cần ít nhất một giây để đếm, như duration="30s"`, a.Value)
		case meant > countdownLongest && d.problem == durationNoUnit:
			return 0, at(n, a, `duration="%s" thiếu đơn vị, mà %s giây dài hơn 24 giờ: countdown đếm từ lúc trang mở và đếm lại khi tải lại trang; đếm tới một thời điểm (until) chưa có`, a.Value, d.number)
		case meant > countdownLongest:
			return 0, at(n, a, `duration="%s" dài hơn 24 giờ: countdown đếm từ lúc trang mở và đếm lại khi tải lại trang; đếm tới một thời điểm (until) chưa có`, a.Value)
		}
	}
	switch d.problem {
	case durationNegative:
		example, time := "5m", d
		if time.unit == "" {
			number, _ := strconv.ParseFloat(time.number, 64)
			time.ms = number * 1000
		}
		if time.ms > 0 && time.ms <= countdownLongest {
			example = countdownSeconds(time)
		}
		return 0, at(n, a, `duration không nhận thời lượng âm (%s): viết một thời lượng như duration="%s"`, value, example)
	case durationZero:
		return 0, at(n, a, `duration="%s": countdown cần ít nhất một giây để đếm, như duration="30s"`, a.Value)
	case durationNoUnit:
		number, _ := strconv.ParseFloat(d.number, 64)
		if math.Mod(number, 1) != 0 {
			return 0, at(n, a, `duration="%s" thiếu đơn vị: viết duration="%s" (%s giây, countdown đếm từng giây)`, a.Value, countdownSeconds(duration{ms: number * 1000}), d.number)
		}
		also := ""
		if number >= 60 {
			switch {
			case math.Mod(number, 3600) == 0:
				also = ` hay duration="` + strconv.FormatFloat(number/3600, 'f', -1, 64) + `h"`
			case math.Mod(number, 60) == 0:
				also = ` hay duration="` + strconv.FormatFloat(number/60, 'f', -1, 64) + `m"`
			}
		}
		return 0, at(n, a, `duration="%s" thiếu đơn vị: viết duration="%s" (%s giây)%s`, a.Value, d.suggestion(durationSeconds), d.number, also)
	case durationTwoUnits:
		count := strconv.Itoa(d.units)
		if d.units == 2 {
			count = "hai"
		}
		return 0, at(n, a, `duration="%s" là %s đơn vị: viết một số với một đơn vị, duration="%s"`, a.Value, count, countdownSeconds(d))
	case durationForeign:
		if d.foreign == "" && !d.iso {
			return 0, at(n, a, `duration="%s" %s: viết duration="%s"`, a.Value, d.reason(), countdownSeconds(d))
		}
		return 0, at(n, a, `duration="%s": %s viết %s, viết duration="%s"`, a.Value, countdownUnitWords[d.unit], d.unit, countdownSeconds(d))
	case durationNotOne:
		return 0, at(n, a, `duration="%s" không phải một thời lượng: viết duration="30s", duration="5m" hay duration="1h"`, a.Value)
	}
	if d.ms > countdownLongest {
		return 0, at(n, a, `duration="%s" dài hơn 24 giờ: countdown đếm từ lúc trang mở và đếm lại khi tải lại trang; đếm tới một thời điểm (until) chưa có`, a.Value)
	}
	if math.Mod(d.ms, 1000) != 0 {
		return 0, at(n, a, `duration="%s": countdown đếm từng giây, viết một số giây tròn như duration="%s"`, a.Value, countdownSeconds(d))
	}
	return d.ms, nil
}

// countdownSaid is a number of a part as a screen reader says it: 5 phút, 5 minutes, 1 minute.
func (r *renderer) countdownSaid(part string, n float64) string {
	if !r.english() {
		return countdownNumber(n) + " " + countdownUnits[part]
	}
	if n == 1 {
		return "1 " + strings.TrimSuffix(part, "s")
	}
	return countdownNumber(n) + " " + part
}

// countdownAttributes are what the engine writes on the countdown's <time> and its segments. skip says the
// element's own value and alt are the engine's to write (a segment's value, the group's alt).
func (r *renderer) countdownAttributes(n *Node, live *grouped) (attributes, classes []string, skip map[string]bool) {
	s := r.countdown // what it reads of the countdown is its file's (its time, its segments, its duration) (frozen.go)
	if n == s.time {
		switch {
		case len(s.segments) == 0:
			attributes = append(attributes, `role="timer"`)
		default:
			label := s.label
			if label == "" {
				label = "Thời gian còn lại"
				if r.english() {
					label = "Time left"
				}
			}
			attributes = append(attributes, `role="group"`, `aria-label="`+html.EscapeString(label)+`"`)
			skip = map[string]bool{"alt": true}
		}
		hours, minutes, seconds := r.eval.globals["hours"], r.eval.globals["minutes"], r.eval.globals["seconds"]
		attributes = append(attributes, `datetime="PT`+hours.ToString().Text()+`H`+minutes.ToString().Text()+`M`+seconds.ToString().Text()+`S"`)
		live.binds = append(live.binds, "datetime: 'PT' + hours + 'H' + minutes + 'M' + seconds + 'S'")
		return attributes, []string{"countdown-time"}, skip
	}
	segment := s.segments[n]
	if segment == nil {
		return nil, nil, nil
	}
	hours, minutes, seconds := countdownParts(s.duration)
	start := map[string]float64{"hours": hours, "minutes": minutes, "seconds": seconds}[segment.part]
	if _, named := n.Attribute("alt"); !named {
		name := map[string]string{"hours": "Giờ", "minutes": "Phút", "seconds": "Giây"}[segment.part]
		if r.english() {
			name = strings.ToUpper(segment.part[:1]) + segment.part[1:]
		}
		attributes = append(attributes, `aria-label="`+name+`"`)
	}
	attributes = append(attributes, `role="spinbutton"`, `inputmode="numeric"`, `autocomplete="off"`, `spellcheck="false"`, `size="2"`)
	if _, own := n.Attribute("placeholder"); !own {
		attributes = append(attributes, `placeholder="00"`)
	}
	attributes = append(attributes,
		`aria-valuemin="`+countdownNumber(segment.low)+`"`, `aria-valuemax="`+countdownNumber(segment.high)+`"`,
		`aria-valuenow="`+countdownNumber(start)+`"`, `aria-valuetext="`+html.EscapeString(r.countdownSaid(segment.part, start))+`"`,
		`data-part="`+segment.part+`"`, fmt.Sprintf(`value="%02d"`, int(start)))
	return attributes, nil, map[string]bool{"value": true}
}
