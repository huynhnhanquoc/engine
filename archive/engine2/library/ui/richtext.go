package ui

import (
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/ui/richtext"
)

// <richtext value="{{ post.body }}" /> (decision of 20/10/2026; moved from the first engine on 06/11/2026, slice 4)
// shows a post the site saved (an editor's value, a paste from Google Docs or Word, plain text from a form) as it
// was written. The value is cleaned on the server into one closed grammar (library/ui/richtext, moved whole: p and
// h2–h6 with an alignment class or none, blockquote, ul, ol, li, pre holding one code, hr, strong, em, u, s, code,
// a href, br, rebuilt from what was read, never filtered in place) and written inside <div class="richtext">, for
// the page's own origin: a link to another host carries rel="external noopener". No JavaScript runs it. It is the
// only way .ui writes HTML that came as data: there is no option to skip the cleaning, and no html=, <html>, <raw>,
// <prose> or <markdown> (each refused with a hint that names richtext).
//
//   - value is one {{ }} that reads the page's data: the server builds the richtext, so a value read from what
//     lives in the browser (an alias, a component's names) is an error, and so is a value written in the markup.
//   - It is a block: never inside a line (<text>, <link>, <button>, <label>…), and nothing is written inside it.
//   - Its look is the text scope of the page's stylesheet, on a page that holds one.

// richtextCall is how .ui shows a saved post: what every refusal of another way to write HTML names.
const richtextCall = `<richtext value="{{ post.body }}" />`

// htmlFromData is why every other way to write HTML from data is refused.
const htmlFromData = ".ui không ghi HTML từ dữ liệu; bài đã lưu hiện bằng " + richtextCall + ", làm sạch ở server"

// refusedTags are the tags other systems show a saved post with, each told what .ui writes.
var refusedTags = map[string]string{
	"html":     "không có thẻ <html> (trang đã là <view>): HTML đã soạn, một bài viết hay một mô tả, hiện bằng " + richtextCall + ", làm sạch ở server",
	"raw":      "không có thẻ <raw>: .ui không ghi HTML chưa làm sạch; bài đã lưu hiện bằng " + richtextCall,
	"prose":    "không có thẻ <prose>: khối chữ của một bài đã lưu là " + richtextCall + ", khoảng cách, danh sách, link và trích dẫn là của engine",
	"markdown": "không có thẻ <markdown>: bài đã lưu hiện bằng " + richtextCall + " (richtext đọc HTML hoặc chữ thường, không đọc markdown)",
}

// htmlAttributes are the attribute names other systems write HTML from data with, in lower case: refused.
var htmlAttributes = map[string]bool{
	"html": true, "inner-html": true, "innerhtml": true, "v-html": true, "x-html": true, "dangerouslysetinnerhtml": true,
}

// lineTags are the tags whose content is a line: a block inside one is no HTML a browser keeps where it is written.
var lineTags = map[string]bool{
	"text": true, "link": true, "button": true, "label": true, "hint": true, "error": true, "badge": true,
	"time": true, "option": true,
}

// richtextElement draws a richtext: its value, cleaned, inside <div class="richtext"> and the classes of its
// attributes.
func (r *renderer) richtextElement(n *Node, inline bool) error {
	if len(n.Significant()) > 0 {
		return at(n, nil, "richtext không có nội dung bên trong: bài đã lưu đến từ value")
	}
	for i := len(r.parents) - 1; i >= 0; i-- {
		if p := r.parents[i]; lineTags[p.Tag] || inline {
			return at(n, nil, "richtext là một khối: không đặt trong <text>, <link>, <button>, <label> (nó đang nằm trong <%s> ở %s:%d:%d)", p.Tag, p.File, p.Line, p.Column)
		}
	}
	value, ok := n.Attribute("value")
	if !ok {
		for i := range n.Attributes {
			if a := &n.Attributes[i]; htmlAttributes[strings.ToLower(a.Name)] {
				return at(n, a, "%s không có: %s", a.Name, htmlFromData)
			}
		}
		return at(n, nil, "richtext cần value: %s", richtextCall)
	}
	written := "richtext hiện bài đã lưu, value là {{ }}; chữ viết sẵn thì viết bằng thẻ .ui (<text>, <list>, <quote>)"
	source, whole := wrapped(strings.TrimSpace(value.Value))
	if !whole {
		return at(n, &value, "%s", written) // words, or words around a {{ }}
	}
	if _, literal := compiler.ExpressionStrings(source); literal {
		return at(n, &value, "%s", written) // {{ '<p>…</p>' }}: words of the markup still
	}
	compiled, err := r.site.compile(source)
	if err != nil {
		return at(n, &value, "biểu thức %q: %v", source, err)
	}
	for _, name := range compiled.reads {
		if owner := r.componentDeclaring(name); strings.HasPrefix(name, "$") || owner != nil {
			return at(n, &value, "richtext dựng ở server: value đọc dữ liệu của trang, không đọc %s (trạng thái ở trình duyệt)", name)
		}
	}
	post, err := r.value(n, &value, source)
	if err != nil {
		return err
	}
	if err := r.site.richtext(r.table, r.rules); err != nil {
		return at(n, nil, "%s", err.Error())
	}
	without := *n
	without.Attributes, without.source = nil, nil
	for _, a := range n.Attributes {
		if a.Name != "value" {
			without.Attributes = append(without.Attributes, a)
		}
	}
	open, err := r.opening(&without, kindInline)
	if err != nil {
		return err
	}
	r.write("div", open)
	if !post.IsNullish() {
		r.out.WriteString(displayed.html(post.ToString().Text(), r.head.Origin))
	}
	r.out.WriteString("</div>")
	return nil
}

// displayCache holds the HTML of the values richtexts wrote, by value and origin, for every page: a post read on
// every request is cleaned once. Bounded by the bytes it holds: an entry larger than an eighth of the bound is never
// kept, an entry that would pass the bound empties it first. Safe for concurrent use.
type displayCache struct {
	mu      sync.Mutex
	entries map[displayKey]string
	size    int
	limit   int
}

type displayKey struct{ source, origin string }

// displayed is the cache of every page's richtexts.
var displayed = &displayCache{limit: 8 << 20}

// html is source cleaned for a page of origin (richtext.Display), from the cache when it is there.
func (cache *displayCache) html(source, origin string) string {
	key := displayKey{source: source, origin: origin}
	cache.mu.Lock()
	markup, found := cache.entries[key]
	cache.mu.Unlock()
	if found {
		return markup
	}
	markup = richtext.Display(source, origin)
	size := len(source) + len(origin) + len(markup)
	if size > cache.limit/8 {
		return markup
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.size+size > cache.limit {
		cache.entries, cache.size = nil, 0
	}
	if cache.entries == nil {
		cache.entries = map[displayKey]string{}
	}
	if _, kept := cache.entries[key]; !kept {
		cache.entries[key] = markup
		cache.size += size
	}
	return markup
}
