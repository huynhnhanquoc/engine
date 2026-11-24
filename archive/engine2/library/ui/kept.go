package ui

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/design/highlight"
	"github.com/kitwork/engine2/library/ui/icons"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// A site keeps its .ui files for the life of its boot (10/11/2026): each file is read and parsed once, and what exists
// says of a path is asked once, the first time a page needs it. A file changed on the disk is seen at the next boot:
// app.hotReload boots the app again when its files change (docs/KITWORK_BOOT.md), and a server without it draws what it
// read until it starts again, as it runs the .work it compiled. The paths are the site's own (its folders, and the frame, regions and
// pieces found up from them), never a request's, so what is kept is bounded by the site's files.
//
// What a read failed on otherwise than a missing file (a file an editor holds while it saves, a disk that answers
// late) is not kept: the next page reads again.

// parsed is a .ui file as the boot read it: the tree its one root parsed into, or why it has none.
type parsed struct {
	root *Node
	err  error
}

// keepable says a read or a look that ended with err tells what the boot sees: the file, or that it is not there.
func keepable(err error) bool {
	return err == nil || errors.Is(err, fs.ErrNotExist)
}

// clone is a copy of the tree under n that its caller owns: the tree a file parsed into is kept, and each use of it
// gets its own, as it got one when every page read its files (a page places its regions into its frame, a call's
// show goes on the root of the piece it calls, and the renderer knows an instance by its node, so a piece called twice
// on a page is two trees). The copy is three blocks, its nodes, their attributes and their children, and each slice
// of it is capped at its length, so an append to one makes its own array and never writes over its neighbour's.
func (n *Node) clone() *Node {
	if n == nil {
		return nil
	}
	nodes, attributes, children := n.count()
	c := &cloner{
		nodes:      make([]Node, 0, nodes),
		attributes: make([]Attribute, 0, attributes),
		children:   make([]*Node, 0, children),
	}
	return c.copy(n)
}

// count is how many nodes, attributes and children the tree under n holds, n counted.
func (n *Node) count() (nodes, attributes, children int) {
	nodes, attributes, children = 1, len(n.Attributes), len(n.Children)
	for _, child := range n.Children {
		a, b, c := child.count()
		nodes, attributes, children = nodes+a, attributes+b, children+c
	}
	return nodes, attributes, children
}

// cloner fills the three blocks of a clone. Each was made with room for the whole tree, so an append never moves one
// and a node's address in it stays.
type cloner struct {
	nodes      []Node
	attributes []Attribute
	children   []*Node
}

func (c *cloner) copy(n *Node) *Node {
	c.nodes = append(c.nodes, *n)
	copied := &c.nodes[len(c.nodes)-1]
	copied.source = n
	if n.Attributes != nil {
		start := len(c.attributes)
		c.attributes = append(c.attributes, n.Attributes...)
		end := len(c.attributes)
		copied.Attributes = c.attributes[start:end:end]
	}
	if n.Children != nil {
		start := len(c.children)
		end := start + len(n.Children)
		c.children = c.children[:end] // the node's children, its children's children after them
		for i, child := range n.Children {
			c.children[start+i] = c.copy(child)
		}
		copied.Children = c.children[start:end:end]
	}
	return copied
}

// unchanged says n is still the copy of its source it was made: its tag, attributes and text as the file's, and
// each of its children a copy of the source's child at that place. A page that places a region into its frame, or a
// call that adds its show to a piece's root, changes the copy, which then no longer stands for its source (frozen.go).
func (n *Node) unchanged() bool {
	s := n.source
	if s == n {
		return true // a node of a page assembled for the boot, read and never changed (keptPage)
	}
	if s == nil || n.Tag != s.Tag || n.Text != s.Text || n.Closed != s.Closed || len(n.Attributes) != len(s.Attributes) ||
		len(n.Children) != len(s.Children) {
		return false
	}
	for i := range n.Attributes {
		if n.Attributes[i] != s.Attributes[i] {
			return false
		}
	}
	for i, child := range n.Children {
		if child.source != s.Children[i] {
			return false
		}
	}
	return true
}

// A site keeps, for the boot, what every page assembled and drew makes the same way each time (10/11/2026): a page
// as assembled from its files, a page's stylesheet by what its rules recorded, the icons' stylesheet by the icons.

// assembled is a page as the boot assembled it, or why it could not be.
type assembled struct {
	page *Node
	err  error
}

// Assembled is the page of route as Page assembles it, assembled once for the boot and shared by every request: the
// tree is read, never changed, while many pages are drawn from it at once.
func (s *Site) Assembled(route string) (*Node, error) {
	return s.keptPage(route, "content")
}

// AssembledNotFound is NotFound of route, assembled once as Assembled is.
func (s *Site) AssembledNotFound(route string) (*Node, error) {
	return s.keptPage(route, "notfound")
}

func (s *Site) keptPage(route, kind string) (*Node, error) {
	key := route + "|" + kind
	if kept, ok := s.pages.Load(key); ok {
		page := kept.(*assembled)
		return page.page, page.err
	}
	var page *Node
	var err error
	if kind == "notfound" {
		page, err = s.NotFound(route)
	} else {
		page, err = s.Page(route)
	}
	var failed *Error
	if keepable(err) || errors.As(err, &failed) {
		stand(page)
		if page != nil && page.source != page {
			// a page with no frame is its content's root, as the file holds it: the page is its own, as any page
			// assembled for the boot (factsOf, sizeOf, keptPlanTabs)
			page.source, page.memo = page, &nodeMemo{}
		}
		kept, _ := s.pages.LoadOrStore(key, &assembled{page: page, err: err})
		page := kept.(*assembled)
		return page.page, page.err
	}
	return page, err
}

// maxKeptSheets is how many stylesheets a site keeps at most, its pages' and its icons': a page whose classes come
// from data in many ways writes its stylesheet again past it.
const maxKeptSheets = 1024

// sheetKey is a page's stylesheet: the look its rules were read in and what they recorded.
type sheetKey struct {
	look  *design.Look
	trace [2]uint64
}

// pageCSS is the stylesheet of the rules a page recorded, written once for every page that records the same.
func (s *Site) pageCSS(look *design.Look, rules *stylesheet.Rules) []byte {
	key := sheetKey{look: look, trace: rules.Trace()}
	if kept, ok := s.sheets.Load(key); ok {
		return kept.([]byte)
	}
	css := rules.CSS()
	if s.keptSheets.Add(1) <= maxKeptSheets {
		s.sheets.Store(key, css)
	}
	return css
}

// iconCSS is the stylesheet of the icons a page shows, written once for every page that shows the same.
func (s *Site) iconCSS(names []string) string {
	key := strings.Join(names, " ")
	if kept, ok := s.masks.Load(key); ok {
		return kept.(string)
	}
	css := icons.CSS(names)
	if s.keptSheets.Add(1) <= maxKeptSheets {
		s.masks.Store(key, css)
	}
	return css
}

// stand makes each node of a page assembled for the boot that no file holds as it is (one a region went into, a
// region's root with its call's show) its own source: the page is shared and never changed, so the node is the same at
// every request, and a part of the page it holds is kept by it (frozen.go).
func stand(n *Node) {
	if n == nil {
		return
	}
	if n.source == nil {
		n.source, n.memo = n, &nodeMemo{}
	}
	for _, child := range n.Children {
		stand(child)
	}
}

// compiledHandler is an event's handler as the compiler read it: the names it calls, or why it cannot be read.
type compiledHandler struct {
	names []string
	err   error
}

// handler is compiler.CompileHandler of source, read once for the boot.
func (s *Site) handler(source string) ([]string, error) {
	if kept, ok := s.handlers.Load(source); ok {
		h := kept.(*compiledHandler)
		return h.names, h.err
	}
	names, err := compiler.CompileHandler(source)
	kept, _ := s.handlers.LoadOrStore(source, &compiledHandler{names: names, err: err})
	h := kept.(*compiledHandler)
	return h.names, h.err
}

// framed is withFrameParts of page, made once for a page assembled for the boot (its own source: stand), whose frame
// and parts are then the same at every request too.
func (s *Site) framed(page *Node) *Node {
	if page.source != page {
		return withFrameParts(page)
	}
	if kept, ok := s.framedPages.Load(page); ok {
		return kept.(*Node)
	}
	with := withFrameParts(page)
	stand(with)
	kept, _ := s.framedPages.LoadOrStore(page, with)
	return kept.(*Node)
}

// lookCSS is the look's stylesheet, its colours and its typefaces, written once for the boot.
func (s *Site) lookCSS(look *design.Look) string {
	if kept, ok := s.looks.Load(look); ok {
		return kept.(string)
	}
	kept, _ := s.looks.LoadOrStore(look, look.CSS())
	return kept.(string)
}

// pageFacts say what a page holds that its drawing walks the whole page for: tabs and their panels (planTabs), the
// aliases of its components (aliases). For a page assembled for the boot, root is the page and editors are its
// editors, nodes every request draws (keptEditorParts); nil for any other.
type pageFacts struct {
	tabs, aliases bool
	root          *Node
	editors       []*Node
}

// factsOf is what page holds, found once for a page assembled for the boot; for any other, it holds everything.
func (s *Site) factsOf(page *Node) pageFacts {
	if page.source != page {
		return pageFacts{tabs: true, aliases: true}
	}
	if kept, ok := s.facts.Load(page); ok {
		return kept.(pageFacts)
	}
	facts := pageFacts{root: page}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.IsText() {
			return
		}
		switch n.Tag {
		case "tabs":
			facts.tabs = true
		case "editor":
			facts.editors = append(facts.editors, n)
		}
		if show, ok := n.Attribute("show"); ok {
			if _, _, found := panelOf(show.Value); found {
				facts.tabs = true
			}
		}
		if _, ok := componentState[n.Tag]; ok {
			if alias, ok := n.Attribute("as"); ok && strings.HasPrefix(alias.Value, "$") {
				facts.aliases = true
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(page)
	kept, _ := s.facts.LoadOrStore(page, facts)
	return kept.(pageFacts)
}

// maxKeptReads is how many classes and domains a site keeps read at most.
const maxKeptReads = 1 << 16

// readKey is a class read: the palette it is read against, and what is read.
type readKey struct {
	palette *stylesheet.Palette
	intent  stylesheet.Intent
}

// keptRead is what the stylesheet read of an intent: the class, what it warns of or why it refuses, and the rules
// reading it recorded.
type keptRead struct {
	class    string
	warnings []*stylesheet.Problem
	err      error
	rules    *stylesheet.Rules
}

// readClass is table.Read of intent into rules, read once for the boot: the same intent against the same palette is the
// same class and the same rules.
func (s *Site) readClass(table *stylesheet.Table, rules *stylesheet.Rules, intent stylesheet.Intent) (string, []*stylesheet.Problem, error) {
	key := readKey{palette: table.Palette(), intent: intent}
	if kept, ok := s.reads.Load(key); ok {
		k := kept.(*keptRead)
		rules.Merge(k.rules)
		return k.class, k.warnings, k.err
	}
	own := &stylesheet.Rules{}
	class, warnings, err := table.Read(own, intent)
	rules.Merge(own)
	if s.keptReads.Add(1) <= maxKeptReads {
		s.reads.Store(key, &keptRead{class: class, warnings: warnings, err: err, rules: own})
	}
	return class, warnings, err
}

// domain is table.ReadDomain of every, the classes each value of an attribute from data becomes, read once for the
// boot; the map is read, never changed.
func (s *Site) domain(table *stylesheet.Table, every stylesheet.Intent) map[string]string {
	key := readKey{palette: table.Palette(), intent: every}
	if kept, ok := s.domains.Load(key); ok {
		return kept.(map[string]string)
	}
	domain, _, _ := table.ReadDomain(&stylesheet.Rules{}, every)
	if s.keptReads.Add(1) <= maxKeptReads {
		s.domains.Store(key, domain)
	}
	return domain
}

// codeKey is a kind of the words of a code block, as the palette colours it.
type codeKey struct {
	palette *stylesheet.Palette
	kind    string
}

// codeClass is table.Code of kind into rules, read once for the boot as readClass is.
func (s *Site) codeClass(table *stylesheet.Table, rules *stylesheet.Rules, kind string) (string, error) {
	key := codeKey{palette: table.Palette(), kind: kind}
	if kept, ok := s.codes.Load(key); ok {
		k := kept.(*keptRead)
		rules.Merge(k.rules)
		return k.class, k.err
	}
	own := &stylesheet.Rules{}
	class, err := table.Code(own, kind)
	rules.Merge(own)
	s.codes.LoadOrStore(key, &keptRead{class: class, err: err, rules: own})
	return class, err
}

// tokenKey is a code block's text in its language.
type tokenKey struct{ language, source string }

// tokenized is what the highlighter made of a text: its words, and why it read it plain.
type tokenized struct {
	tokens  []highlight.Token
	failure error
}

// tokens is highlight.Tokenize of source, made once for the boot (the words are read, never changed).
func (s *Site) tokens(language, source string) ([]highlight.Token, error) {
	key := tokenKey{language: language, source: source}
	if kept, ok := s.tokenized.Load(key); ok {
		t := kept.(*tokenized)
		return t.tokens, t.failure
	}
	tokens, failure := highlight.Tokenize(language, source)
	if s.keptReads.Add(1) <= maxKeptReads {
		s.tokenized.Store(key, &tokenized{tokens: tokens, failure: failure})
	}
	return tokens, failure
}

// pageSize is what a page took when last drawn: its body, and the whole document.
type pageSize struct{ body, document int }

// sizeOf is what page took when last drawn, nothing for one not drawn yet or not assembled for the boot.
func (s *Site) sizeOf(page *Node) pageSize {
	if page.source != page {
		return pageSize{}
	}
	if kept, ok := s.sizes.Load(page); ok {
		return kept.(pageSize)
	}
	return pageSize{}
}

// keepSize keeps what page took, for the buffers of its next drawing.
func (s *Site) keepSize(page *Node, size pageSize) {
	if page.source == page {
		s.sizes.Store(page, size)
	}
}

// keptLint is lint of n, read once for each node a file holds: what it warns of depends on how n is written alone.
func (r *renderer) keptLint(n *Node) {
	if n.source == nil {
		r.lint(n)
		return
	}
	if kept, ok := r.site.lints.Load(n.source); ok {
		for _, w := range kept.([]Warning) {
			r.say(w)
		}
		return
	}
	from, scopes, scopeReads := len(r.warnLog), r.openScopes(), r.scopeReads
	r.lint(n)
	if !r.readOpen(scopes, scopeReads) { // what the node says of itself, not of a scope around
		r.site.lints.LoadOrStore(n.source, append([]Warning(nil), r.warnLog[from:]...))
	}
}

// colourKey is a page backdrop's ground in a look.
type colourKey struct {
	look   *design.Look
	ground string
}

// themeColor is themeColor of the rules' backdrop, made once for each look and ground.
func (s *Site) themeColor(rules *stylesheet.Rules, look *design.Look) string {
	key := colourKey{look: look, ground: rules.Backdrop()}
	if kept, ok := s.colours.Load(key); ok {
		return kept.(string)
	}
	kept, _ := s.colours.LoadOrStore(key, themeColor(rules, look))
	return kept.(string)
}

// aliasHosts are aliasHostsOf page, found once for a page assembled for the boot.
func (s *Site) aliasHosts(page *Node) []*Node {
	if page.source != page {
		return aliasHostsOf(page)
	}
	if kept, ok := s.aliased.Load(page); ok {
		return kept.([]*Node)
	}
	kept, _ := s.aliased.LoadOrStore(page, aliasHostsOf(page))
	return kept.([]*Node)
}

// planKey is an editor's plan: its node in a page assembled for the boot and the page, its place among the page's
// editors, its alias.
type planKey struct {
	node, page *Node
	number     int
	alias      string
}

// keptPlan is an editor's plan as editorParts made it, and what making it warned of.
type keptPlan struct {
	plan     editorScope
	warnings []Warning
}

// keptEditorParts is editorParts of the editor n, made once for the boot when n stands in a page assembled for it:
// the plan reads n's tree, its alias and the components around, never the request (editorValue checks the value's
// names, it reads none). Its maps are kept and read, never written, by every page that draws the editor.
func (r *renderer) keptEditorParts(n *Node, s *editorScope, alias string) error {
	if !slices.Contains(r.facts.editors, n) {
		return r.editorParts(n, s, alias) // a copy made for this page: its nodes are its own
	}
	key := planKey{node: n, page: r.facts.root, number: s.number, alias: alias}
	if kept, ok := r.site.editorPlans.Load(key); ok {
		k := kept.(*keptPlan)
		field, format := s.field, s.format
		*s = k.plan
		s.field, s.format = field, format
		for _, w := range k.warnings {
			r.say(w)
		}
		return nil
	}
	from, touched := len(r.warnLog), r.touched
	if err := r.editorParts(n, s, alias); err != nil {
		return err
	}
	if r.touched == touched {
		r.site.editorPlans.LoadOrStore(key, &keptPlan{plan: *s, warnings: append([]Warning(nil), r.warnLog[from:]...)})
	}
	return nil
}

// richtext is table.Richtext into rules, the look of what a richtext holds, read once for each palette.
func (s *Site) richtext(table *stylesheet.Table, rules *stylesheet.Rules) error {
	if kept, ok := s.richtexts.Load(table.Palette()); ok {
		k := kept.(*keptRead)
		rules.Merge(k.rules)
		return k.err
	}
	own := &stylesheet.Rules{}
	err := table.Richtext(own)
	rules.Merge(own)
	s.richtexts.LoadOrStore(table.Palette(), &keptRead{err: err, rules: own})
	return err
}

// fromData is fromData of a value written in a file, read once for the boot.
func (s *Site) fromData(value string) bool {
	if !strings.Contains(value, "{{") {
		return false
	}
	if kept, ok := s.spelled.Load(value); ok {
		return kept.(bool)
	}
	data := fromData(value)
	if s.keptReads.Add(1) <= maxKeptReads {
		s.spelled.Store(value, data)
	}
	return data
}

// keptPrefix is a prefix as readPrefix read it, or what is wrong with it; its lists are read, never changed.
type keptPrefix struct {
	when    prefix
	problem string
}

// readPrefix is readPrefix of chain for a website, read once for the boot.
func (s *Site) readPrefix(chain string) (prefix, string) {
	if chain == "" {
		return prefix{keep: true}, ""
	}
	if kept, ok := s.prefixes.Load(chain); ok {
		k := kept.(*keptPrefix)
		return k.when, k.problem
	}
	when, problem := readPrefix(chain, "website")
	if s.keptReads.Add(1) <= maxKeptReads {
		s.prefixes.Store(chain, &keptPrefix{when: when, problem: problem})
	}
	return when, problem
}

// highlightKey is a text in a language against a palette.
type highlightKey struct {
	palette          *stylesheet.Palette
	language, source string
}

// keptHighlight is a text written in its colours, the rules of its colours, and why it could not be.
type keptHighlight struct {
	written string
	rules   *stylesheet.Rules
	err     error
}

// highlighted is highlight of tokens, the words of source in language, written once for the boot: the same text
// against the same palette is the same spans and the same rules.
func (s *Site) highlighted(table *stylesheet.Table, rules *stylesheet.Rules, language, source string, tokens []highlight.Token) (string, error) {
	key := highlightKey{palette: table.Palette(), language: language, source: source}
	if kept, ok := s.highlights.Load(key); ok {
		k := kept.(*keptHighlight)
		rules.Merge(k.rules)
		return k.written, k.err
	}
	own := &stylesheet.Rules{}
	written, err := s.highlight(table, own, tokens)
	rules.Merge(own)
	if s.keptReads.Add(1) <= maxKeptReads {
		s.highlights.Store(key, &keptHighlight{written: written, rules: own, err: err})
	}
	return written, err
}

// keptTabs is planTabs of a page assembled for the boot: its plans by node and the pre-paint written after a node,
// read and never changed, and what planning them warned of.
type keptTabs struct {
	hosts, buttons map[*Node]*tabsPlan
	panels         map[*Node]*panelPlan
	after          map[*Node]string
	warnings       []Warning
}

// keptPlanTabs is planTabs of page, planned once for a page assembled for the boot: its plans read only its nodes. The
// renderer's maps are its own copies, which drawing adds to.
func (r *renderer) keptPlanTabs(page *Node) error {
	if r.facts.root != page || len(r.after) > 0 || len(r.tabsHosts) > 0 || len(r.tabButtons) > 0 || len(r.tabPanels) > 0 {
		return r.planTabs(page)
	}
	if kept, ok := r.site.tabPlans.Load(page); ok {
		k := kept.(*keptTabs)
		maps.Copy(r.tabsHosts, k.hosts)
		maps.Copy(r.tabButtons, k.buttons)
		maps.Copy(r.tabPanels, k.panels)
		maps.Copy(r.after, k.after)
		for _, w := range k.warnings {
			r.say(w)
		}
		return nil
	}
	from := len(r.warnLog)
	if err := r.planTabs(page); err != nil {
		return err
	}
	r.site.tabPlans.LoadOrStore(page, &keptTabs{hosts: maps.Clone(r.tabsHosts), buttons: maps.Clone(r.tabButtons),
		panels: maps.Clone(r.tabPanels), after: maps.Clone(r.after), warnings: append([]Warning(nil), r.warnLog[from:]...)})
	return nil
}

// Prewarm reads and parses every .ui file of the site, and assembles the page of
// each route of routes and the site's not-found page, as the first request of each would: all kept for the boot
// (app.prewarm, 11/11/2026). What could not be read, parsed or assembled is given back, once each; the page that holds
// it says it again at its request. pages and files count what it kept.
func (s *Site) Prewarm(routes []string) (pages, files int, failed []error) {
	said := map[string]bool{}
	fail := func(err error) {
		if !said[err.Error()] {
			said[err.Error()] = true
			failed = append(failed, err)
		}
	}
	read := func(folder string) {
		_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil
			case entry.IsDir() && path != folder && entry.Name()[0] == '.':
				return filepath.SkipDir // .database, .git: no page of the site
			case entry.IsDir() || filepath.Ext(path) != ".ui":
				return nil
			}
			if _, err, _ := s.parsedRoot(path); err != nil {
				fail(err)
				return nil
			}
			files++
			return nil
		})
	}
	read(s.Root)
	for _, route := range routes {
		if _, err := s.Assembled(route); err != nil {
			fail(err)
			continue
		}
		pages++
	}
	switch page, err := s.AssembledNotFound(s.Root); {
	case err != nil:
		fail(err)
	case page != nil:
		pages++
	}
	return pages, files, failed
}
