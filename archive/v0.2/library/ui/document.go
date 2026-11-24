package ui

import (
	"bytes"
	"fmt"
	"html"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// Head is what a route says of its page for the document's <head>: context.title and context.description over
// router.meta of its folders, router.language ("vi" when no folder says one, as the first engine) and the icon of
// router.favicon.
type Head struct {
	Title, Description, Language string
	// Favicon is the address of the page's icon, <link rel="icon">; "" links none
	Favicon string
	// Path is the address being viewed: a link to it says it is the current page (aria-current)
	Path string
	// Drive says the site moves between its pages without a reload (router.reload(false), what a site does
	// when it writes nothing): the page loads Drive, and its frame gets the reader and the progressbar it does
	// not write
	Drive bool
	// Origin is the site's own, "https://kitwork.io": a web address on another host leaves the site
	// (rel="external noopener"); "" counts every web address as another's
	Origin string
	// Method is the request's: a page that answers a save (no GET) holds what the person wrote, not saved yet
	// (an editor's unsaved)
	Method string
	// Development says the site runs on a developer's machine (a website that allows local): a page with
	// warnings links the file that says them in the browser's console (warning.go)
	Development bool
	// Minify says the page loads the engine's JavaScript minified at build time (kernel/minified.go), as a site in
	// production does
	Minify bool
	// Components are the site's components the page draws (component.define), which the host runs; nil when the
	// site defines none (sitecomponent.go)
	Components Components
	// Bridge says the page runs in the browser, its works and vars, for a router.view whose bind holds either;
	// nil for a page the server prints (bridge.go)
	Bridge *Bridge
	// Tab is the id of the tab the page is drawn for, which the page's data carries when it shows live names: the
	// server remembers what the tab holds (host, tab.go)
	Tab string
}

// Notes are what drawing a page says besides the page: what engine2 does not draw yet (a word nothing reads yet,
// an event that waits for the page to run in the browser), each once, and the page's warnings in the order found.
type Notes struct {
	Unread   []string
	Warnings []Warning
}

// Document is a page drawn as a whole HTML document (KITWORK_UI mục 18) in the look of its folder: its <head>
// with the look's prepaint, the title, the description, the preload of the body's face and the page's CSS
// written inline (the ground every page stands on, the look's colours and typefaces, the rule of every class
// the page carries, the mask of every icon it shows), then the page, its <view> the <body>. data is what the route gives
// its page, an object whose keys are the names the page reads; nothing for a page given nothing.
//
// The page loads, deferred at the end of <head>, the files the browser runs it with (live.go Scripts): the kernel
// and each component it holds, when it holds something the kernel reads, and Drive on a site that moves without a
// reload. Its CSS is three blocks Drive trades on a move, each by its bytes: the site's (the ground and the
// look's colours and typefaces), the masks of the icons it shows, the rules of the classes it carries.
//
// Its notes are what engine2 does not draw yet, so the host can say it once, and the page's warnings, which a page
// drawn while developing also links as /_kitwork/warnings.<hash>.js; the page still renders.
func (s *Site) Document(page *Node, data value.Value, head Head, look *design.Look) (document string, notes Notes, err error) {
	drawn, notes, err := s.DocumentBytes(page, data, head, look)
	return string(drawn), notes, err
}

// DocumentBytes is Document as the bytes a server writes: one buffer the size the page took when last drawn
// (10/11/2026), no copy after.
func (s *Site) DocumentBytes(page *Node, data value.Value, head Head, look *design.Look) (document []byte, notes Notes, err error) {
	document, notes, err = s.documentBytes(page, data, head, look)
	if err != nil || !CheckParts || head.Components != nil {
		return document, notes, err
	}
	// drawn again, from the parts the first drawing kept: the same page, the same notes
	again, againNotes, err := s.documentBytes(page, data, head, look)
	if err != nil {
		return nil, Notes{}, fmt.Errorf("the page drawn again from its parts fails: %w", err)
	}
	if !bytes.Equal(document, again) {
		at := 0
		for at < len(document) && at < len(again) && document[at] == again[at] {
			at++
		}
		return nil, Notes{}, fmt.Errorf("the page drawn again from its parts differs at byte %d: %q, drawn whole %q", at,
			again[max(0, at-60):min(len(again), at+60)], document[max(0, at-60):min(len(document), at+60)])
	}
	if !slices.Equal(notes.Unread, againNotes.Unread) || !slices.Equal(notes.Warnings, againNotes.Warnings) {
		return nil, Notes{}, fmt.Errorf("the page drawn again from its parts says otherwise: %v, drawn whole %v", againNotes, notes)
	}
	return document, notes, nil
}

// CheckParts makes DocumentBytes draw each page twice, the second time from the parts the first kept (frozen.go), and
// fail when the two differ in a byte or in what they say: engine2's tests run with it (test/ui, test/host), so a part
// written again is always the part drawn. A page that runs the site's components is drawn once: they run the site's
// code, whose effects a second run would repeat.
var CheckParts bool

func (s *Site) documentBytes(page *Node, data value.Value, head Head, look *design.Look) (document []byte, notes Notes, err error) {
	r := &renderer{
		site:         s,
		table:        stylesheet.NewTable(look.Palette),
		rules:        &stylesheet.Rules{},
		eval:         newEvaluator(&s.machines, s.Pool, s.MaxBytes),
		unread:       map[string]bool{},
		aliasOf:      map[string]string{},
		components:   map[string]bool{},
		hosts:        map[*Node]*host{},
		summaries:    map[*Node]string{},
		head:         head,
		tabsHosts:    map[*Node]*tabsPlan{},
		tabButtons:   map[*Node]*tabsPlan{},
		tabPanels:    map[*Node]*panelPlan{},
		after:        map[*Node]string{},
		editorStarts: map[*Node]map[string]any{},
		marks:        map[*Node][]string{},
	}
	if r.head.Language == "" {
		r.head.Language = "vi"
	}
	defer r.eval.release()
	r.lintFrame(page) // the bar and the reader the frame writes (notices.go)
	if head.Drive {
		page = r.site.framed(page) // the bar of a move and the reader of the new page, when the frame writes none
	}
	size := s.sizeOf(page) // what the page drawn took the last time (keepSize, of the same page)
	r.out.lend(size.body)
	defer r.out.giveBack()
	if data.IsObject() {
		for _, key := range data.Keys() {
			defer r.eval.put(key, data.Get(key))()
		}
	}
	r.startPage(data, head.Bridge)
	r.keepParts(look)             // a page the server prints keeps its parts (frozen.go)
	facts := r.site.factsOf(page) // what of the page the walks below look for, once for a page assembled (kept.go)
	r.facts = facts
	if facts.tabs {
		if err := r.keptPlanTabs(page); err != nil {
			return nil, Notes{}, err
		}
	}
	if facts.aliases {
		for _, takeBack := range r.aliases(page) {
			defer takeBack()
		}
	}
	if err := r.node(page, false); err != nil {
		return nil, Notes{}, err
	}
	r.lintCarousels(page)  // a carousel that plays by itself and nothing on the page stops (carousel.go)
	r.lintCountdowns(page) // a countdown nothing on the page starts (countdown.go)
	r.commit()
	body := r.out.buf
	out := make([]byte, 0, max(size.document, len(body)+64<<10))
	w := func(parts ...string) {
		for _, part := range parts {
			out = append(out, part...)
		}
	}
	w("<!doctype html>\n<html lang=\"", html.EscapeString(r.head.Language), "\"", look.HTMLAttributes(), ">\n<head>\n")
	w("<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w(r.site.themeColor(r.rules, look)) // a page backdrop's first stop, per mode (backdrop.go)
	w(look.Head())
	if head.Title != "" {
		w("<title>", html.EscapeString(head.Title), "</title>\n")
	}
	if head.Description != "" {
		w("<meta name=\"description\" content=\"", html.EscapeString(head.Description), "\">\n")
	}
	if head.Favicon != "" {
		w(`<link rel="icon" href="`, html.EscapeString(head.Favicon), `"`)
		if kind := iconType(head.Favicon); kind != "" {
			w(` type="`, kind, `"`) // so a browser skips an icon it cannot show
		}
		w(">\n")
	}
	w("<style data-kitwork-generated=\"site\">\n", rebootCSS(), r.site.lookCSS(look), "</style>\n")
	if masks := r.site.iconCSS(r.icons); masks != "" {
		w("<style data-kitwork-generated=\"icons\">", masks, "</style>\n")
	}
	w("<style data-kitwork-generated=\"css\">\n")
	out = append(out, r.site.pageCSS(look, r.rules)...)
	w("</style>\n")
	// every source the kernel runs on the page, as JavaScript (program.go): read from the page as drawn
	program, err := r.site.pageProgram(body, r.replayed)
	if err != nil {
		return nil, Notes{}, err
	}
	r.program = program
	for _, file := range r.Scripts() {
		// deferred, in this order: the kernel starts once every file after it has registered its component
		w(`<script data-kitwork-generated="`, file.Role, `" src="`, file.Path, `" defer></script>`, "\n")
	}
	if head.Development && len(r.warnings) > 0 {
		// while developing, the page says its warnings in the browser's console on every load: a file, last
		file := warningsFile(r.warnings)
		keepWarnings(file)
		w(`<script data-kitwork-generated="warnings" src="`, file.Path, `" defer></script>`, "\n")
	}
	w("</head>\n")
	if page.Tag != "view" {
		w("<body>")
		out = append(out, body...)
		w("</body>")
	} else {
		out = append(out, body...)
	}
	w("\n</html>\n")
	s.keepSize(page, pageSize{body: len(body), document: len(out)})
	return out, Notes{Unread: r.Unread(), Warnings: r.warnings}, nil
}

// rebootCSS is the reboot's stylesheet as the page writes it, read once.
var rebootCSS = sync.OnceValue(func() string { return string(stylesheet.Reboot().CSS) })

// iconType is the media type of an icon by its extension, "" for one the engine does not name.
func iconType(address string) string {
	if i := strings.IndexAny(address, "?#"); i >= 0 {
		address = address[:i]
	}
	switch strings.ToLower(path.Ext(address)) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// output is what a page writes, in a buffer it borrows for the drawing (lend) and gives back once its document is
// made (giveBack): what is kept of it is copied out (since).
type output struct{ buf []byte }

func (o *output) WriteString(s string) { o.buf = append(o.buf, s...) }

func (o *output) Len() int { return len(o.buf) }

// since is a copy of what was written from start on.
func (o *output) since(start int) string { return string(o.buf[start:]) }

// outputs are the buffers pages write into, lent one at a time.
var outputs sync.Pool

// largestOutput is the largest buffer given back for another page: a page of more holds its own.
const largestOutput = 1 << 20

// lend gives o a buffer with room for size bytes.
func (o *output) lend(size int) {
	if lent, ok := outputs.Get().(*[]byte); ok {
		o.buf = (*lent)[:0]
	}
	if cap(o.buf) < size {
		o.buf = make([]byte, 0, size)
	}
}

// giveBack takes the buffer back for another page; nothing written is read after.
func (o *output) giveBack() {
	if cap(o.buf) == 0 || cap(o.buf) > largestOutput {
		return
	}
	buf := o.buf[:0]
	o.buf = nil
	outputs.Put(&buf)
}

// escape writes s as html.EscapeString writes it, each of & ' < > " as its reference, straight into the buffer.
func (o *output) escape(s string) {
	from := 0
	for i := 0; i < len(s); i++ {
		var reference string
		switch s[i] {
		case '&':
			reference = "&amp;"
		case '\'':
			reference = "&#39;"
		case '<':
			reference = "&lt;"
		case '>':
			reference = "&gt;"
		case '"':
			reference = "&#34;"
		default:
			continue
		}
		o.buf = append(o.buf, s[from:i]...)
		o.buf = append(o.buf, reference...)
		from = i + 1
	}
	o.buf = append(o.buf, s[from:]...)
}
