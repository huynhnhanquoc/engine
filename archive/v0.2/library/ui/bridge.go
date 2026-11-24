package ui

import (
	"hash/fnv"
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// A page that runs in the browser (docs/view.md mục 3, 4; docs/KITWORK_BRIDGE.md mục 2, 3; slice 2): the page of a
// router.view whose bind holds a work or a var is one instance the kernel runs, "page". What it reads of its names is
// written as the kernel's attributes, as inside a component of Kitwork (live.go): the page is a frame whose names are
// those of its bind. A list over its names is drawn as the kernel adopts it, by key; an if over them is drawn and
// shown by the kernel, as a show; a form that calls a work stays on the page. Its data is written once, as JSON, at
// the end of its <content />, which Drive carries to the next page:
//
//	<script type="application/json" data-kitwork-page>{"page":{"title":"","todos":[…],"left":3},"jobs":{"page":["add","remove"]},"vars":{"page":["title"]}}</script>
//
// call.js (library/ui/kernel) puts it in the page's state and makes each work a function that calls it on the
// server, through the page's own URL (host/bridge.go). A page whose bind holds neither stays as the server prints it.

// Bridge is what the host says of a page of router.view whose bind holds a work, a var or a live name: the names
// that are works (functions), those that are vars, those of sync that are data, which the server keeps live for the
// tab (engine2/docs/view.md mục 2.3).
type Bridge struct {
	Works, Vars, Live []string
}

// The kinds of a frame: a component of Kitwork, which the kernel runs; the page that runs in the browser, its
// names and the item and index of each row of its lists; a site's component, whose names stay the server's
// until it runs in the browser too.
const (
	frameKitwork  = ""
	framePage     = "page"
	frameSite     = "site"
	frameInstance = "instance" // a site component that runs in the browser
)

// pageBridge is a page running in the browser while it is drawn: its works and vars, its data (nil works and an
// undefined data when only components of it run there), the instances of the site's components that run there,
// the lists drawn so far.
type pageBridge struct {
	works, vars []string
	data        value.Value
	instances   []instance
	lists       int
	// live says the page or one of its instances holds live names: the data names its tab
	live bool
}

// instance is a site component's instance the browser runs: its tag, its place, the names it binds, its works and
// vars (docs/KITWORK_BRIDGE.md mục 3).
type instance struct {
	tag, place  string
	names       value.Value
	works, vars []string
}

// place is where a site component is called from, its instance's name on the wire: c and a hash of the file, line
// and column of its tag, then -2, -3… for the second and later instances drawn from one place (a row of a list).
func (r *renderer) place(n *Node) string {
	sum := fnv.New32a()
	sum.Write([]byte(strings.ReplaceAll(n.File, `\`, "/") + ":" + strconv.Itoa(n.Line) + ":" + strconv.Itoa(n.Column)))
	place := "c" + strconv.FormatUint(uint64(sum.Sum32()), 36)
	if r.places == nil {
		r.places = map[string]int{}
	}
	r.places[place]++
	if count := r.places[place]; count > 1 {
		place += "-" + strconv.Itoa(count)
	}
	return place
}

// instance records a site component's instance the browser runs: the page loads call.js, and its data block holds
// the instance's names (its box's attributes load the kernel).
func (r *renderer) instance(tag, place string, names value.Value, bridge *Bridge) {
	if r.page == nil {
		r.page = &pageBridge{}
	}
	r.page.instances = append(r.page.instances, instance{tag: tag, place: place, names: names, works: bridge.Works, vars: bridge.Vars})
	r.page.live = r.page.live || len(bridge.Live) > 0
}

// startPage makes the page a frame when the host says it runs in the browser: every name of data, which the
// browser holds.
func (r *renderer) startPage(data value.Value, bridge *Bridge) {
	if bridge == nil || !data.IsObject() {
		return
	}
	names := map[string]any{}
	for _, key := range data.Keys() {
		names[key] = nil
	}
	r.page = &pageBridge{works: bridge.Works, vars: bridge.Vars, data: data, live: len(bridge.Live) > 0}
	r.frames = append(r.frames, &frame{component: "page", names: names, kind: framePage})
	r.live = true
}

// block is the page's data as the browser reads it, by instance (docs/KITWORK_BRIDGE.md mục 3): the page's names
// but its works under "page" when the page runs there, each instance's under its place; the works and the vars of
// each, under "jobs" and "vars".
func (p *pageBridge) block(tab string) string {
	var b, jobs, vars strings.Builder
	b.WriteString("{")
	add := func(place string, names value.Value, works, held []string) {
		if b.Len() > 1 {
			b.WriteString(",")
			jobs.WriteString(",")
			vars.WriteString(",")
		}
		b.WriteString(jsonString(place) + ":" + dataOf(names))
		jobs.WriteString(jsonString(place) + ":" + jsonList(works))
		vars.WriteString(jsonString(place) + ":" + jsonList(held))
	}
	if p.data.IsObject() {
		add("page", p.data, p.works, p.vars)
	}
	for _, each := range p.instances {
		add(each.place, each.names, each.works, each.vars)
	}
	b.WriteString(`,"jobs":{` + jobs.String() + `},"vars":{` + vars.String() + `}`)
	if p.live && tab != "" {
		b.WriteString(`,"tab":` + jsonString(tab)) // the tab the server remembers, which its channel names
	}
	b.WriteString("}")
	return `<script type="application/json" data-kitwork-page>` + scriptSafe(b.String()) + `</script>`
}

// dataOf is the names of a bind as a JSON object, a work's aside, in the order of their names.
func dataOf(names value.Value) string {
	var keys []string
	for _, key := range names.Keys() {
		if !names.Get(key).IsFunction() {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, key := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		written := value.Stringify(names.Get(key), "")
		text := "null"
		if written.IsString() {
			text = written.Text()
		}
		b.WriteString(jsonString(key) + ":" + text)
	}
	b.WriteString("}")
	return b.String()
}

// jsonList is names as a JSON array of strings, in order.
func jsonList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = jsonString(name)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// jsonString is s as a JSON string.
func jsonString(s string) string {
	written := value.Stringify(value.String(s), "")
	return written.Text()
}

// scriptSafe is JSON a <script> holds whole: <, > and & written as escapes, which JSON reads as the same
// characters, so no text of the data closes the element or opens a comment.
func scriptSafe(text string) string {
	escape := func(code string) string { return "\\" + "u00" + code }
	return strings.NewReplacer("<", escape("3c"), ">", escape("3e"), "&", escape("26")).Replace(text)
}

// writeBlock writes the page's data where its content ends (a content.ui has one <content />, its root).
func (r *renderer) writeBlock() {
	if r.page != nil {
		r.out.WriteString(r.page.block(r.tab()))
	}
}

// liveList says the for of n runs in the browser: its list reads the page's names.
func (r *renderer) liveList(n *Node, a *Attribute, list string) (bool, error) {
	if r.page == nil {
		return false, nil
	}
	compiled, err := r.site.compile(strings.TrimSpace(list))
	if err != nil {
		return false, nil // the list's own error, where it is read
	}
	return r.onClient(n, a, list, compiled.reads)
}

// eachLive draws a list of the page that runs in the browser as the kernel adopts it (kernel.js collectFor): the
// start marker naming the list, the row once as a template with no item, hidden, then each row with the list's id
// and its key, then the end marker. In the rows the item and the index are names of the page.
func (r *renderer) eachLive(n *Node, a *Attribute, inline bool, item, index, list, key string, items value.Value) error {
	if strings.Contains(n.Tag, "-") {
		return at(n, a, "for trên <%s>: danh sách chạy ở trình duyệt chưa lặp một component của site; đặt for trên một thẻ của Kitwork (<item for=\"…\">) bao lấy nó", n.Tag)
	}
	r.page.lists++
	id := "s" + strconv.Itoa(r.page.lists)
	spec := item
	if index != "" {
		spec += ", " + index
	}
	spec += " of " + strings.TrimSpace(list)
	if key != "" {
		spec += "; key: " + key
	}
	names := map[string]any{item: nil}
	if index != "" {
		names[index] = nil
	}
	r.frames = append(r.frames, &frame{component: "page", names: names, kind: framePage})
	defer func() { r.frames = r.frames[:len(r.frames)-1] }()
	draw := func(row value.Value, i int, marks []string) error {
		takeItem := r.eval.put(item, row)
		takeIndex := func() {}
		if index != "" {
			takeIndex = r.eval.put(index, value.Number(float64(i)))
		}
		defer func() {
			takeIndex()
			takeItem()
		}()
		r.marks[n] = marks
		defer delete(r.marks, n)
		return r.element(n, inline)
	}
	r.out.WriteString("<!--kitwork-for-start id=" + id + "-->")
	// the template holds no item: what it shows is never seen, the kernel writes each value of a row it makes
	if err := draw(value.Null(), 0, []string{`data-kitwork-for="` + html.EscapeString(spec) + `"`, "data-kitwork-template", "hidden"}); err != nil {
		return err
	}
	for i, row := range items.Items() {
		rowKey := strconv.Itoa(i)
		if key != "" {
			takeItem := r.eval.put(item, row)
			takeIndex := func() {}
			if index != "" {
				takeIndex = r.eval.put(index, value.Number(float64(i)))
			}
			got, err := r.value(n, a, key)
			takeIndex()
			takeItem()
			if err != nil {
				return err
			}
			shown := got.ToString()
			if shown.IsInvalid() {
				// a key with no text (a function, a time, an object whose toString fails) is no row's key: every row's
				// was "", one key for all (18/11/2026)
				return at(n, a, "khoá %s của hàng thứ %d không thành chữ được: %s", key, i+1, shown.Failure())
			}
			rowKey = shown.Text()
		}
		if err := draw(row, i, []string{`data-kitwork-item="` + id + `"`, `data-kitwork-key="` + html.EscapeString(rowKey) + `"`}); err != nil {
			return err
		}
	}
	r.out.WriteString("<!--kitwork-for-end-->")
	return nil
}

// liveCondition says an if of n is shown by the kernel: it reads the page's names. The element is then drawn
// whatever it says, hidden while it is false, as a show.
func (r *renderer) liveCondition(n *Node, a *Attribute) (string, bool, error) {
	if r.page == nil {
		return "", false, nil
	}
	source := strings.TrimSpace(a.Value)
	if bare, ok := wrapped(source); ok {
		source = bare
	}
	compiled, err := r.site.compile(source)
	if err != nil {
		return "", false, nil // the condition's own error, where it is read
	}
	client, err := r.onClient(n, a, source, compiled.reads)
	return source, client, err
}

// componentDeclaring is the nearest component of Kitwork around that declares name, nil when none does: the page's
// names and a site component's are not a component's state.
func (r *renderer) componentDeclaring(name string) *frame {
	for i := len(r.frames) - 1; i >= 0; i-- {
		if _, ok := r.frames[i].names[name]; ok && r.frames[i].kind == frameKitwork {
			return r.frames[i]
		}
	}
	return nil
}
