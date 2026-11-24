package ui

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// The tabs (decisions of 16/10/2026, docs/components/tabs.md; moved from the first engine on 06/11/2026, slice 4)
// are one of Kitwork's components, written as their tag. The author writes the tabs and the panels the way they
// would anyway; the engine recognises them and writes the accessibility wiring itself:
//
//	<tabs as="$shell" persist="true" alt="Shell">
//	  <button click="select('bash')">bash</button>
//	  <button click="select('powershell')">PowerShell</button>
//	</tabs>
//	<box show="$shell.selected == 'bash'">…</box>
//
//   - The host is the tablist (role="tablist"). A tab is a <button> inside it whose click is select('<name>'), a
//     name written out: role="tab", the name as its value, aria-selected and tabindex bound to the choice.
//   - A panel is an element anywhere on the page whose show compares the alias's selected with a tab's name
//     (=== too, either side): role="tabpanel", tabindex="0", and the ids that tie it to its tab (aria-controls,
//     aria-labelledby). The ids are planned before the page is drawn, so a panel before its tablist is one too.
//   - selected starts as the first tab's name unless the call chose one (selected="page"), handed to the
//     instance; a tabs persisting under a name an earlier one of the page has starts on that one's tab.
//   - persist="true" keeps the choice under the set of the tab names (bash+powershell), persist="<name>" under
//     that name; a few lines of script after the last tablist or panel of the name show the kept tab before the
//     first frame (tabsPrepaint).

// tabsPlan is one tabs host as planned before the page is drawn.
type tabsPlan struct {
	number  int    // its place among the page's tablists: the ids the engine writes
	alias   string // its as, "" for none
	persist string // the name its choice is kept under, "" for none
	start   string // the tab it starts on
	names   []string
	ids     map[string]string   // each tab's id, by name
	authors map[string]bool     // the tabs whose id the author wrote
	panels  map[string][]string // each tab's panels' ids, by name
	seq     int                 // where it starts among the page's tablists and panels
	// node is the host; buttons each tab's button, by name, where a tab with no panel is told (lintTabs)
	node    *Node
	buttons map[string]*Node
}

// panelPlan is one panel as planned: its tab's host and name, and its id.
type panelPlan struct {
	host       *tabsPlan
	name, id   string
	authorID   bool
	seq        int
	node       *Node
	inFor      bool
	showSource string
}

// tabCall reads a click that chooses a tab, select('<name>'), into the name.
var tabCall = regexp.MustCompile(`^\s*select\(\s*(?:'([^'\\]+)'|"([^"\\]+)")\s*\)\s*$`)

// selectCall is a click that is one call of select, whatever its argument.
var selectCall = regexp.MustCompile(`^\s*select\s*\(.*\)\s*;?\s*$`)

// pressable says an element is something a person presses or types in: a control, or an element with an event.
func pressable(n *Node) bool {
	switch n.Tag {
	case "button", "link", "input", "select", "textarea", "accordion":
		return true
	}
	for _, a := range n.Attributes {
		if isEvent(a.Name) {
			return true
		}
	}
	return false
}

// panelShow reads a show that compares an alias's selected with a name, either side, == or ===.
var (
	panelShowLeft  = regexp.MustCompile(`^\s*(\$[A-Za-z][A-Za-z0-9_]*)\.selected\s*===?\s*(?:'([^'\\]+)'|"([^"\\]+)")\s*$`)
	panelShowRight = regexp.MustCompile(`^\s*(?:'([^'\\]+)'|"([^"\\]+)")\s*===?\s*(\$[A-Za-z][A-Za-z0-9_]*)\.selected\s*$`)
)

func tabNameOf(click string) (string, bool) {
	m := tabCall.FindStringSubmatch(click)
	if m == nil {
		return "", false
	}
	return m[1] + m[2], true
}

func panelOf(show string) (alias, name string, ok bool) {
	source := strings.TrimSpace(show)
	if bare, whole := wrapped(source); whole {
		source = bare
	}
	if m := panelShowLeft.FindStringSubmatch(source); m != nil {
		return m[1], m[2] + m[3], true
	}
	if m := panelShowRight.FindStringSubmatch(source); m != nil {
		return m[3], m[1] + m[2], true
	}
	return "", "", false
}

// planTabs reads every tabs of the page and every panel before it is drawn, and ties each tab to its panels.
func (r *renderer) planTabs(page *Node) error {
	seq := 0
	var hosts []*tabsPlan
	var panels []*panelPlan
	byPersist := map[string][]*tabsPlan{}
	var walk func(n *Node, inFor bool) error
	walk = func(n *Node, inFor bool) error {
		if n.IsText() {
			return nil
		}
		if _, ok := n.Attribute("for"); ok {
			inFor = true
		}
		if show, ok := n.Attribute("show"); ok {
			if alias, name, found := panelOf(show.Value); found {
				seq++
				id, own := "", false
				if a, ok := n.Attribute("id"); ok {
					id, own = a.Value, true
				}
				panels = append(panels, &panelPlan{name: name, id: id, authorID: own, seq: seq, node: n, inFor: inFor, showSource: alias})
			}
		}
		if n.Tag == "tabs" {
			seq++
			plan, err := r.planHost(n, len(hosts)+1, seq, byPersist)
			if err != nil {
				return err
			}
			hosts = append(hosts, plan)
			r.tabsHosts[n] = plan
		}
		for _, child := range n.Children {
			if err := walk(child, inFor); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(page, false); err != nil {
		return err
	}
	byAlias := map[string]*tabsPlan{}
	for _, h := range hosts {
		if h.alias != "" {
			byAlias[h.alias] = h
		}
	}
	for _, p := range panels {
		h := byAlias[p.showSource]
		if h == nil {
			continue // another component's selected: the page draws it as written
		}
		if h.ids[p.name] == "" {
			show, _ := p.node.Attribute("show")
			r.warn(p.node, &show, "show so %s.selected với '%s', mà %s không có tab %s (%s): phần tử này không bao giờ hiện", h.alias, p.name, h.alias, p.name, strings.Join(h.names, ", "))
			continue
		}
		show, _ := p.node.Attribute("show")
		if p.node.Tag == "code" {
			return at(p.node, &show, "<code> không làm panel của tab %s được: đặt show lên một khung bọc nó, <box show=\"%s\">…</box>", p.name, strings.TrimSpace(show.Value))
		}
		if p.inFor {
			return at(p.node, &show, "panel của tab %s nằm trong for: một tab có panel của nó, viết một lần", p.name)
		}
		if !p.authorID {
			index := 0
			for i, name := range h.names {
				if name == p.name {
					index = i + 1
				}
			}
			p.id = fmt.Sprintf("kitwork-tabpanel-%d-%d", h.number, index)
			if count := len(h.panels[p.name]); count > 0 {
				p.id += fmt.Sprintf("-%d", count+1)
			}
		}
		p.host = h
		h.panels[p.name] = append(h.panels[p.name], p.id)
		r.tabPanels[p.node] = p
	}
	for _, h := range hosts {
		for _, name := range h.names {
			if len(h.panels[name]) == 0 && h.alias != "" {
				button := h.buttons[name]
				click, _ := button.Attribute("click")
				r.warn(button, &click, `tab %s của %s không có panel: viết show="%s.selected == '%s'" trên phần tử nó cho hiện`, name, h.alias, h.alias, name)
			}
		}
		if h.alias == "" {
			r.warn(h.node, nil, "%s", `tabs không có as: ngoài <tabs> không gì đọc được tab đang chọn; viết <tabs as="$tên"> rồi viết show="$tên.selected == 'tên tab'" trên panel của mỗi tab`)
		}
	}
	// the pre-paint of each name the page's tabs persist under, after the element of its tabs that starts last
	keys := make([]string, 0, len(byPersist))
	for key := range byPersist {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var ids []string
		var last *Node
		at := -1
		for node, h := range r.tabsHosts {
			if h.persist != key {
				continue
			}
			if h.seq > at {
				last, at = node, h.seq
			}
		}
		for _, h := range byPersist[key] {
			for _, name := range h.names {
				if len(h.panels[name]) > 0 {
					ids = append(ids, h.ids[name])
				}
			}
		}
		for _, p := range panels {
			if p.host != nil && p.host.persist == key && p.seq > at {
				last, at = p.node, p.seq
			}
		}
		if len(ids) == 0 || last == nil {
			continue
		}
		r.after[last] += `<script data-kitwork-generated="tabs" data-kitwork-persist="` + html.EscapeString(key) +
			`" data-kitwork-tabs="` + html.EscapeString(strings.Join(ids, " ")) + `">` + tabsPrepaint + `</script>`
	}
	return nil
}

// planHost reads one tabs host: its tabs (the buttons whose click is select('<name>'), outside a component nested
// in it), the tab it starts on, the name its choice is kept under.
func (r *renderer) planHost(n *Node, number, seq int, byPersist map[string][]*tabsPlan) (*tabsPlan, error) {
	plan := &tabsPlan{number: number, seq: seq, ids: map[string]string{}, authors: map[string]bool{}, panels: map[string][]string{},
		node: n, buttons: map[string]*Node{}}
	if a, ok := n.Attribute("as"); ok {
		plan.alias = strings.TrimSpace(a.Value)
	}
	persist, err := tabsPersist(n)
	if err != nil {
		return nil, err
	}
	seen := map[string]*Attribute{}
	var scan func(nodes []*Node, inFor bool) error
	scan = func(nodes []*Node, inFor bool) error {
		for _, child := range nodes {
			if child.IsText() {
				continue
			}
			if _, component := componentState[child.Tag]; component || strings.Contains(child.Tag, "-") {
				continue // a nested component's buttons are its own
			}
			childFor := inFor
			if _, ok := child.Attribute("for"); ok {
				childFor = true
			}
			tab := false
			if click, ok := child.Attribute("click"); ok && child.Tag == "button" {
				name, isTab := tabNameOf(click.Value)
				if !isTab && selectCall.MatchString(click.Value) {
					// it selects a tab, drawn as written, but is none: no role="tab", no arrow keys
					tab = true
					r.warn(child, &click, "%s", `click="`+click.Value+`": engine nhận một nút là tab khi click là select('tên') với tên viết sẵn; nút này chọn được tab nhưng không phải tab (không có role="tab", không đi bằng phím mũi tên)`)
				}
				if isTab {
					tab = true
					if first, twice := seen[name]; twice {
						return at(child, &click, "tab %s đã có ở dòng %d: mỗi tab một tên", name, first.Line)
					}
					if childFor {
						return at(child, &click, "tab %s nằm trong for: mỗi tab viết một lần, với tên viết sẵn trong select('…')", name)
					}
					if value, ok := child.Attribute("value"); ok {
						return at(child, &value, "value của một tab là tên của nó, engine viết từ select('%s'): bỏ value", name)
					}
					seen[name] = &click
					plan.names = append(plan.names, name)
					id := fmt.Sprintf("kitwork-tab-%d-%d", number, len(plan.names))
					if a, ok := child.Attribute("id"); ok {
						id = a.Value
						plan.authors[name] = true
					}
					plan.ids[name] = id
					plan.buttons[name] = child
					r.tabButtons[child] = plan
				}
			}
			if !tab && pressable(child) {
				// role="tablist" holds tabs only: the arrows skip it, and a screen reader reads it as part of the list
				r.warn(child, nil, `<%s> trong <tabs> không phải tab: dải tab chỉ chứa tab (role="tablist"), phím mũi tên bỏ qua nó mà trình đọc màn hình đọc nó như một phần của dải; đặt nó ra ngoài <tabs>, bên cạnh dải tab`, child.Tag)
			}
			if err := scan(child.Children, childFor); err != nil {
				return err
			}
		}
		return nil
	}
	if err := scan(n.Children, false); err != nil {
		return nil, err
	}
	if len(plan.names) == 0 {
		return nil, at(n, nil, `tabs cần ít nhất một tab bên trong <tabs>: <button click="select('tên')">…</button> (mỗi nút gọi select với một tên viết sẵn là một tab)`)
	}
	if persist != "" {
		plan.persist = tabsKey(persist, plan.names)
	}
	if a, ok := n.Attribute("selected"); ok {
		value := strings.TrimSpace(a.Value)
		if strings.Contains(value, "{{") || !contains(plan.names, value) {
			return nil, at(n, &a, "selected=\"%s\" không phải tên của tab nào (%s)", a.Value, strings.Join(plan.names, ", "))
		}
		plan.start = value
	} else {
		// the first tab, unless an earlier tabs of the page keeps its choice under the same name and starts on a tab
		// this one has: tabs that follow one choice agree from the first frame
		plan.start = plan.names[0]
		for _, earlier := range byPersist[plan.persist] {
			if plan.persist != "" && contains(plan.names, earlier.start) {
				plan.start = earlier.start
				break
			}
		}
	}
	if plan.persist != "" {
		byPersist[plan.persist] = append(byPersist[plan.persist], plan)
	}
	return plan, nil
}

// persistName is a name a choice is kept under: a letter without marks, then such letters, digits, '-', '_', '.'.
var persistName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

// tabsPersist reads persist on a tabs host: "" for none (persist="false" or not written), "true" for the set of
// the tab names, else the name written. remember, the word before, is an error naming persist.
func tabsPersist(n *Node) (string, error) {
	persist := ""
	for i := range n.Attributes {
		a := &n.Attributes[i]
		chain, base := splitPrefix(a.Name)
		if base != "persist" && base != "remember" {
			continue
		}
		if chain != "" {
			return "", at(n, a, "%s của tabs không nhận tiền tố: lựa chọn được giữ một cách trên mọi độ rộng", base)
		}
		value := strings.TrimSpace(a.Value)
		if base == "remember" {
			name := "tên"
			if persistName.MatchString(value) && !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
				name = value
			}
			return "", at(n, a, `remember đã đổi thành persist (16/10/2026): viết persist="true" để mọi tabs có cùng các tab trên site đi theo một lựa chọn, hay persist="%s" để giữ lựa chọn dưới tên này`, name)
		}
		if a.Bare || value == "" {
			return "", at(n, a, `persist cần một giá trị: persist="true" giữ lựa chọn theo bộ tên tab, persist="tên" giữ theo một tên chọn`)
		}
		const words = `persist nhận "true" (giữ lựa chọn theo bộ tên tab: mọi tabs có cùng các tab đi theo nhau), "false" (không giữ, như không viết) hay một tên (giữ dưới tên đó, cho những bộ tab khác nhau mà nên đi cùng nhau)`
		switch lower := strings.ToLower(value); {
		case strings.Contains(value, "{{"):
			return "", at(n, a, `persist viết sẵn, không đọc từ {{ }}: engine đặt tên giữ lựa chọn và pre-paint của nó khi dựng trang; %s`, words)
		case value == "true" || value == "false":
		case lower == "true" || lower == "false":
			return "", at(n, a, `persist="%s": viết chữ thường, persist="%s"`, value, lower)
		case lower == "yes" || lower == "on" || lower == "1":
			return "", at(n, a, `persist="%s" không phải một giá trị của persist: muốn giữ lựa chọn thì viết persist="true"; %s`, value, words)
		case lower == "no" || lower == "off" || lower == "0":
			return "", at(n, a, `persist="%s" không phải một giá trị của persist: không muốn giữ thì bỏ persist (hay persist="false"); %s`, value, words)
		case !persistName.MatchString(value):
			suggest := `persist="true"`
			if name := persistSuggestion(value); name != "" {
				suggest = `persist="` + name + `" (hay persist="true")`
			}
			return "", at(n, a, `persist="%s": tên giữ lựa chọn bắt đầu bằng một chữ cái không dấu (a–z), rồi chữ không dấu, số, '-', '_', '.'; viết %s`, value, suggest)
		}
		if value != "false" {
			persist = value
		}
	}
	return persist, nil
}

// persistSuggestion is the name nearest what was written: a letter with marks loses them, what is not a letter
// a-z, a digit, '-', '_' or '.' becomes '-', and what comes before the first letter goes; "" when none is left.
func persistSuggestion(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if base, ok := unmarked[unicode.ToLower(r)]; ok {
			if unicode.IsUpper(r) {
				base = unicode.ToUpper(base)
			}
			r = base
		}
		switch {
		case r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	for name != "" && !(name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z') {
		name = strings.TrimLeft(name[1:], "-._")
	}
	return name
}

// unmarked is each lowercase letter with marks of Vietnamese and the Latin alphabets near it, to the letter without
// them (đ is a letter of its own Unicode does not decompose: d).
var unmarked = func() map[rune]rune {
	m := map[rune]rune{}
	for base, marked := range map[rune]string{
		'a': "áàảãạăắằẳẵặâấầẩẫậäåā", 'e': "éèẻẽẹêếềểễệëē", 'i': "íìỉĩịîïī",
		'o': "óòỏõọôốồổỗộơớờởỡợöøō", 'u': "úùủũụưứừửữựûüū", 'y': "ýỳỷỹỵÿ", 'd': "đ", 'c': "ç", 'n': "ñ",
	} {
		for _, r := range marked {
			m[r] = base
		}
	}
	return m
}()

// tabsKey is the name a choice is kept under: for persist="true" the set of the tab names, sorted, joined by "+".
func tabsKey(persist string, names []string) string {
	if persist != "true" {
		return persist
	}
	set := append([]string(nil), names...)
	sort.Strings(set)
	return strings.Join(set, "+")
}

// tabsPrepaint is the body of the pre-paint of the tabs that persist under one name: where the parser meets it,
// before the first frame, it reads what the kernel's tabs keeps (localStorage "kitwork:tabs:<name>", the tab's
// name as JSON) and shows that tab on every tablist of the name that has it; anything unexpected changes nothing.
// The name and the tabs' ids are read off the script element, so the body is one constant.
const tabsPrepaint = `(function(s){try{var k=JSON.parse(localStorage.getItem("kitwork:tabs:"+s.getAttribute("data-kitwork-persist")));if(typeof k!="string"||!k)return;` +
	`var t=s.getAttribute("data-kitwork-tabs").split(" ").map(function(i){return document.getElementById(i)}).filter(Boolean),` +
	`h=t.filter(function(b){return b.value===k}).map(function(b){return b.closest('[role="tablist"]')});` +
	`t.forEach(function(b){if(h.indexOf(b.closest('[role="tablist"]'))<0)return;var o=b.value===k;` +
	`b.setAttribute("aria-selected",o);b.setAttribute("tabindex",o?0:-1);` +
	`(b.getAttribute("aria-controls")||"").split(" ").forEach(function(i){var p=document.getElementById(i);if(p)p.hidden=!o})})` +
	`}catch(e){}})(document.currentScript)`

// tabsHost is what a tabs host adds: the tablist's role, its instance's inputs (the name it keeps its choice under,
// the tab it starts on); the host goes on without persist and selected.
func (r *renderer) tabsHost(n *Node) (*Node, *host, error) {
	plan := r.tabsPlanOf(n)
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		return base == "persist" || base == "selected", nil
	})
	if err != nil {
		return nil, nil, err
	}
	own := &host{attributes: []string{`role="tablist"`}}
	if plan.persist != "" {
		own.inputs = append(own.inputs, "persist: "+quote(plan.persist))
	}
	own.inputs = append(own.inputs, "selected: "+quote(plan.start))
	r.tabsHosts[copied] = plan
	return copied, own, nil
}

// tabAttributes are the attributes of a tab, before its type: its id when the author wrote none; and after it its
// role, the panels it controls, its name as its value; with the bindings of aria-selected and tabindex, painted
// for the tab the instance starts on.
func (r *renderer) tabAttributes(n *Node, live *grouped) (before, after []string) {
	plan := r.tabButtonOf(n)
	click, _ := n.Attribute("click")
	name, _ := tabNameOf(click.Value)
	if !plan.authors[name] {
		before = append(before, `id="`+html.EscapeString(plan.ids[name])+`"`)
	}
	after = append(after, `role="tab"`)
	if panels := plan.panels[name]; len(panels) > 0 {
		after = append(after, `aria-controls="`+html.EscapeString(strings.Join(panels, " "))+`"`)
	}
	after = append(after, `value="`+html.EscapeString(name)+`"`)
	chosen := "selected == " + quote(name)
	live.binds = append(live.binds, "aria-selected: "+chosen, "tabindex: "+chosen+" ? 0 : -1")
	if plan.start == name {
		after = append(after, `aria-selected="true"`, `tabindex="0"`)
	} else {
		after = append(after, `aria-selected="false"`, `tabindex="-1"`)
	}
	return before, after
}

// panelAttributes are a panel's: its id, its role, the tab that labels it, a stop in the tab order.
func (r *renderer) panelAttributes(n *Node) []string {
	p := r.tabPanelOf(n)
	var out []string
	if !p.authorID {
		out = append(out, `id="`+html.EscapeString(p.id)+`"`)
	}
	return append(out, `role="tabpanel"`, `aria-labelledby="`+html.EscapeString(p.host.ids[p.name])+`"`, `tabindex="0"`)
}
