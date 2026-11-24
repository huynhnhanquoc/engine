package ui

import (
	"hash/maphash"
	"math"
	"sync"
	"sync/atomic"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/schema"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// A page drawn in parts (10/11/2026): most of a page is the same at every request, written in its files, and only
// what reads the request changes (the page's data, the address viewed, a component the site runs). The first page of
// the boot that draws a part keeps what drawing it wrote (its HTML, the rules of its classes, the icons it shows,
// what it warned of and noted as unread); every later page that holds the same part, at the same place and in the
// same page, writes that again instead of drawing it, as the first engine's compile of a page did. A part that reads
// the request is kept as such, and drawn each time; a part that reads only the address viewed is kept for each address.
//
// A part is the same where its key is: the node its file holds (source), the elements around it down to the page's
// root (by their files' nodes, or as written for one the page changed: its ancestry), whether it stands in a line
// and in the route's content, the page's counts of ids where it starts (carousels, editors, fields), and what is the
// same for every part of its page (its look, its language, whether the site moves without a reload, minifies, runs
// while developing, its origin). What a part reads of what stands around it, a view's container, a carousel's track,
// a countdown's time, the toast's notice, a field's ids, an editor's plan, is its file's and those counts: the key
// holds it. What a component's state, a bound form, a breadcrumb's trail or a terminal's commands give it is the
// request's, read as such. A page that runs in the browser (a router.view that binds works or vars) keeps nothing.
// A part whose drawing changed anything of the page besides what it wrote (a component the page loads, an id it took
// that the page counts) does it again when written again (effects); one that changed what no effect replays (what goes
// after another element, an alias, a breadcrumb's trail, the form it opens) is drawn each time.

// freezer is what a renderer keeps to draw a page in parts.
type freezer struct {
	// touched counts what the drawing read that is the request's: an expression (the page's data), a component the
	// site runs, a plan of this page keyed by its nodes (tabs, a host's tail), the method, the tab
	touched int
	// pathReads counts the reads of the address viewed (a link's aria-current)
	pathReads int
	// warnLog and unreadLog are every warning said and every word noted unread, in order, said before or not;
	// loadLog every component the page loads, in order, loaded before or not
	warnLog   []Warning
	unreadLog []string
	loadLog   []string
	lazyLog   []*Node
	// scopeReads counts the reads of each kind of scope (a carousel, a countdown, an editor, a terminal, a field, a
	// breadcrumb, a toast's notice): a part, or an opening, that read no scope open when it began is the same wherever
	// it stands; one begun inside it is its own
	scopeReads [scopeKinds]int
	// hostReads counts the reads of what a component's tag adds to the element it builds (notices.go): written
	// while the part that holds the component is drawn, so a part reads its own, and an opening another's
	hostReads int
	// pending are the parts drawn on this page, kept by the site once the whole page is drawn (commit)
	pending []pendingPart
	// ancestry is, for each element around what is being drawn (r.parents), a hash of it and of those around it
	ancestry []uint64
	// pageKey is what every part of this page shares; nil when the page keeps no part
	pageKey *pageKey
	// reading keeps what the opening being read reads of the page's data, nil when none is kept (variantsOf)
	reading *variant
}

// pageKey is what a part of a page reads that is the same for every part of it.
type pageKey struct {
	look                       *design.Look
	language                   string
	drive, minify, development bool
	origin                     string
}

// partKey is where a part stands: see the top of this file.
type partKey struct {
	source            *Node
	ancestry          uint64
	inline, inContent bool
	// counters are the page's counts where the part starts: the ids its carousels, editors and fields take
	counters counters
	page     *pageKey // one for each value, the site's (keepParts)
	// bound says the key is a part kept for one address, path; the key without it is the part's for every address,
	// which says only that it is kept by address (boundPart)
	bound bool
	path  string
}

// part is what drawing a part wrote, or that it reads the request.
type part struct {
	dynamic   bool // drawn at each page: it reads the request
	pathBound bool // kept by address: the part of this key holds none, the key with the address does
	html      string
	// program is the values of the attributes holding sources html holds, read as it is kept (program.go)
	program  []valueHash
	rules    *stylesheet.Rules
	icons    []string
	warnings []Warning
	unread   []string
	effects
}

// effects are what a part does to the page besides what it writes: the components it loads, whether the kernel
// loads, the page's toast. A part that loads a component the page loads again later, or that holds the page's toast,
// does the same each time it is drawn.
type effects struct {
	loads     []string
	live      bool
	intersect bool
	toastAt   string
	blueprint *blueprint
	// a scope drawn in the part: the carousels, editors and fields it counted, the heights of its editors, its
	// carousels that play by themselves and its countdowns (told at the end of the page), the lazy frames it told of
	carousels, editors, fields int
	editorRows                 []int
	playing                    []*carouselScope
	countdowns                 []*Node
	lazyTold                   []*Node
}

// pendingPart is a part drawn on the page being drawn, and its key.
type pendingPart struct {
	key  partKey
	part *part
}

var (
	// dynamicPart is a part drawn at each page; boundPart one kept by address
	dynamicPart = &part{dynamic: true}
	boundPart   = &part{pathBound: true}
	// partSeed hashes the elements around a part
	partSeed = maphash.MakeSeed()
)

// maxBoundParts is how many parts kept by address a site holds at most: a site of many addresses (a route with
// a parameter) draws its links again past it.
const maxBoundParts = 4096

// keepParts says the page being drawn keeps its parts; a page that runs in the browser keeps none.
func (r *renderer) keepParts(look *design.Look) {
	if r.page != nil {
		return
	}
	key := pageKey{look: look, language: r.head.Language, drive: r.head.Drive, minify: r.head.Minify,
		development: r.head.Development, origin: r.head.Origin}
	// one pointer for each value, so a part's key holds a pointer, not the strings: an origin is a request's host,
	// so they are counted with the parts kept by address, and past maxBoundParts a page keeps no part
	if kept, ok := r.site.pageKeys.Load(key); ok {
		r.pageKey = kept.(*pageKey)
		return
	}
	if r.site.boundParts.Add(1) > maxBoundParts {
		return
	}
	kept, _ := r.site.pageKeys.LoadOrStore(key, &key)
	r.pageKey = kept.(*pageKey)
}

// part draws the element n, or writes again what drawing it wrote on an earlier page.
func (r *renderer) part(n *Node, inline bool) error {
	if !r.freezable(n) {
		return r.element(n, inline)
	}
	key := r.partKeyOf(n, inline)
	if p := r.keptPart(key); p != nil {
		if p.dynamic {
			return r.element(n, inline)
		}
		if p.pathBound {
			key.bound, key.path = true, r.head.Path
			p = nil
			if bound, ok := r.site.parts.Load(key); ok {
				p = bound.(*part)
			}
		}
		if p != nil && n.unchanged() {
			r.replay(p)
			return nil
		}
	}
	return r.freeze(n, inline, key)
}

// freezable says n may be a part: a page that keeps parts, a node its file holds, outside every scope whose drawing
// is the page's.
func (r *renderer) freezable(n *Node) bool {
	return r.openingKeepable(n) // a scope around is no matter: a part that reads it is drawn again (scopeReads)
}

// openingKeepable says n's opening may be kept: a page that keeps parts, a node its file holds, inside Kitwork's
// components only. A scope around is no matter: an opening that reads it is drawn again (scopeReads).
func (r *renderer) openingKeepable(n *Node) bool {
	if r.pageKey == nil || n.source == nil {
		return false
	}
	for _, f := range r.frames {
		if f.kind != frameKitwork {
			return false
		}
	}
	return true
}

// partKeyOf is the key of a part n starts where it stands. An editor's rule, which holds the heights of the editors
// before it, is drawn with the editor, which reads the request (its state, the method): no key holds those heights.
func (r *renderer) partKeyOf(n *Node, inline bool) partKey {
	key := partKey{source: n.source, inline: inline, inContent: r.inContent > 0, page: r.pageKey, counters: r.counters()}
	if len(r.ancestry) > 0 {
		key.ancestry = r.ancestry[len(r.ancestry)-1]
	}
	return key
}

// freeze draws n, and keeps what it wrote when it read nothing of the request and changed nothing of the page but what
// it wrote; its parts kept apart are let go then, the whole holding them.
func (r *renderer) freeze(n *Node, inline bool, key partKey) error {
	outer := r.rules
	r.rules = &stylesheet.Rules{}
	start, touched, pathReads := r.out.Len(), r.touched, r.pathReads
	scopes, scopeReads := r.openScopes(), r.scopeReads
	icons, warned, unread, pending := len(r.icons), len(r.warnLog), len(r.unreadLog), len(r.pending)
	before, mark := r.state(), r.effectsMark()
	err := r.element(n, inline)
	own := r.rules
	r.rules = outer
	outer.Merge(own)
	if err != nil {
		return err
	}
	if r.touched != touched || r.readOpen(scopes, scopeReads) || r.state() != before {
		r.pending = append(r.pending, pendingPart{key: key, part: dynamicPart})
		return nil
	}
	p := &part{
		html:      r.out.since(start),
		rules:     own,
		icons:     append([]string(nil), r.icons[icons:]...),
		warnings:  append([]Warning(nil), r.warnLog[warned:]...),
		unread:    append([]string(nil), r.unreadLog[unread:]...),
		pathBound: r.pathReads != pathReads,
		effects:   r.effectsSince(mark),
	}
	p.program = partValues(p.html)
	r.pending = append(r.pending[:pending], pendingPart{key: key, part: p})
	return nil
}

// replay writes again what drawing a part wrote.
func (r *renderer) replay(p *part) {
	start := r.out.Len()
	r.out.WriteString(p.html)
	r.replayed = append(r.replayed, replaySpan{start: start, end: r.out.Len(), values: p.program})
	r.rules.Merge(p.rules)
	r.icons = append(r.icons, p.icons...)
	for _, w := range p.warnings {
		r.say(w)
	}
	for _, name := range p.unread {
		r.unreadNow(name)
	}
	r.apply(p.effects)
}

// effectsMark is where the page's effects stand before a part is drawn.
type effectsMark struct {
	loads                                                             int
	live, intersect                                                   bool
	toastAt                                                           string
	carousels, editors, fields, editorRows, playing, countdowns, lazy int
}

func (r *renderer) effectsMark() effectsMark {
	return effectsMark{loads: len(r.loadLog), live: r.live, intersect: r.intersect, toastAt: r.toastAt, carousels: r.carousels, editors: r.editors,
		fields: r.fields, editorRows: len(r.editorRows), playing: len(r.playing), countdowns: len(r.countdowns),
		lazy: len(r.lazyLog)}
}

// effectsSince are the effects of what was drawn since mark.
func (r *renderer) effectsSince(mark effectsMark) effects {
	e := effects{loads: append([]string(nil), r.loadLog[mark.loads:]...), live: r.live && !mark.live,
		intersect: r.intersect && !mark.intersect,
		carousels: r.carousels - mark.carousels, editors: r.editors - mark.editors, fields: r.fields - mark.fields,
		editorRows: append([]int(nil), r.editorRows[mark.editorRows:]...),
		playing:    append([]*carouselScope(nil), r.playing[mark.playing:]...),
		countdowns: append([]*Node(nil), r.countdowns[mark.countdowns:]...),
		lazyTold:   append([]*Node(nil), r.lazyLog[mark.lazy:]...)}
	if r.toastAt != mark.toastAt {
		e.toastAt, e.blueprint = r.toastAt, r.blueprint
	}
	return e
}

// apply does a part's effects again.
func (r *renderer) apply(e effects) {
	for _, name := range e.loads {
		r.load(name)
	}
	if e.live {
		r.live = true
	}
	if e.intersect {
		r.intersect = true
	}
	if e.toastAt != "" {
		r.toastAt, r.blueprint = e.toastAt, e.blueprint
	}
	r.carousels, r.editors, r.fields = r.carousels+e.carousels, r.editors+e.editors, r.fields+e.fields
	r.editorRows = append(r.editorRows, e.editorRows...)
	r.playing = append(r.playing, e.playing...)
	r.countdowns = append(r.countdowns, e.countdowns...)
	for _, frame := range e.lazyTold {
		r.toldLazy(frame)
	}
}

// counters are the page's counts a part's drawing reads: see partKey.
type counters struct {
	carousels, editors, fields, playing, countdowns int
}

func (r *renderer) counters() counters {
	return counters{carousels: r.carousels, editors: r.editors, fields: r.fields, playing: len(r.playing),
		countdowns: len(r.countdowns)}
}

// toldLazy keeps that the lazy frame was told of what it holds (lint.go), and logs it for a part.
func (r *renderer) toldLazy(frame *Node) {
	if r.lazyTold == nil {
		r.lazyTold = map[*Node]bool{}
	}
	r.lazyTold[frame] = true
	r.lazyLog = append(r.lazyLog, frame)
}

// load says the page loads the component name, which the kernel runs: the kernel loads too.
func (r *renderer) load(name string) {
	r.components[name], r.live = true, true
	r.loadLog = append(r.loadLog, name)
}

// commit keeps the parts the page drew, once the whole page is drawn.
func (r *renderer) commit() {
	for _, each := range r.pending {
		key, p := each.key, each.part
		if p.pathBound {
			base := key
			base.bound, base.path = false, ""
			r.site.parts.LoadOrStore(base, boundPart)
			if r.site.boundParts.Add(1) > maxBoundParts {
				continue
			}
			key.bound, key.path = true, r.head.Path
		}
		r.site.parts.LoadOrStore(key, p)
	}
	r.pending = nil
}

// drawnState is what of the page a part may change only by drawing something of the request: when it differs after a
// part, the part is drawn at each page.
type drawnState struct {
	after, marks, places, aliasOf, frames, open, crumbs int
	form                                                *formState
}

func (r *renderer) state() drawnState {
	s := drawnState{after: len(r.after), marks: len(r.marks), aliasOf: len(r.aliasOf), frames: len(r.frames),
		open: len(r.open), form: r.form}
	for _, count := range r.places {
		s.places += count
	}
	if r.crumbs != nil {
		s.crumbs = len(*r.crumbs)
	}
	return s
}

// enter and leave keep the elements around what is being drawn, and the hash of each with those around it.
func (r *renderer) enter(n *Node) {
	previous := uint64(0)
	if len(r.ancestry) > 0 {
		previous = r.ancestry[len(r.ancestry)-1]
	}
	r.parents = append(r.parents, n)
	r.ancestry = append(r.ancestry, ancestorHash(previous, n))
}

func (r *renderer) leave() {
	r.parents = r.parents[:len(r.parents)-1]
	r.ancestry = r.ancestry[:len(r.ancestry)-1]
}

// ancestorHash is the hash of an element around a part, after those around it: the node its file holds, or, for one
// changed or made by the engine, its tag and attributes as they are.
func ancestorHash(previous uint64, n *Node) uint64 {
	if n.unchanged() {
		return maphash.Comparable(partSeed, struct {
			previous uint64
			source   *Node
		}{previous, n.source})
	}
	var h maphash.Hash
	h.SetSeed(partSeed)
	var b [8]byte
	for i := range b {
		b[i] = byte(previous >> (8 * i))
	}
	h.Write(b[:])
	h.WriteString(n.Tag)
	for _, a := range n.Attributes {
		h.WriteByte(0)
		h.WriteString(a.Name)
		h.WriteByte(1)
		h.WriteString(a.Value)
	}
	return h.Sum64()
}

// An element whose inside reads the request still opens the same way at every page when its attributes read nothing
// of it: <text weight="bold">{{ item.name }}</text> is <p class="weight-bold"> each time. Its opening tag is kept as a
// part is, by where it stands and its kind, and read again only where it reads the request (a value from data, a
// row of a list the browser runs).

// openingKey is where an opening stands.
type openingKey struct {
	part partKey
	kind tagKind
}

// keptStart is an opening kept, what reading it wrote besides it, or that it reads the request: then variants are
// the openings it was, each kept for the values of the page's data it read.
type keptStart struct {
	dynamic, pathBound bool
	open               start
	rules              *stylesheet.Rules
	icons              []string
	warnings           []Warning
	unread             []string
	effects
	variants *variants
}

// boundStart says an opening is kept by address: the key with the address holds it.
var boundStart = &keptStart{pathBound: true}

// maxVariants is how many openings one place keeps for the values it read: past it, the place's values are the
// page's own (a name, a price), and it is read at each page.
const maxVariants = 8

// variants are the openings one place was, each for the values of the page's data it read. list only grows, under
// mu, a new array each time; varies says it reached maxVariants.
type variants struct {
	mu     sync.Mutex
	list   atomic.Pointer[[]*variant]
	varies atomic.Bool
}

// variant is an opening kept for what it read of the page's data: each expression in the order read, and what it was.
// Reading it again gives the same opening when each expression is again what it was: the opening reads nothing else
// of the request, so what it does after a read follows from the value read.
type variant struct {
	reads []readValue
	kept  *keptStart
}

// readValue is one value an opening read: a string, a number, a boolean, null or undefined.
type readValue struct {
	expression *expression
	result     value.Value
}

// sameValue says a and b are the same string, number (bit for bit), boolean, null or undefined; false for anything
// else, whose drawing may read inside it.
func sameValue(a, b value.Value) bool {
	switch {
	case a.IsUndefined():
		return b.IsUndefined()
	case a.IsNull():
		return b.IsNull()
	case a.IsString():
		return b.IsString() && a.Text() == b.Text()
	case a.IsNumber():
		return b.IsNumber() && math.Float64bits(a.Number()) == math.Float64bits(b.Number())
	case a.IsBoolean():
		return b.IsBoolean() && a.Boolean() == b.Boolean()
	}
	return false
}

// variantOf is the opening of vs whose reads are again what they were, nil for none; the reads count as the
// request's, as reading them did.
func (r *renderer) variantOf(vs *variants) *keptStart {
	list := vs.list.Load()
	if list == nil {
		return nil
	}
	var read []readValue // the expressions read so far, each read once
	for _, v := range *list {
		same := true
		for _, want := range v.reads {
			got, found := value.Value{}, false
			for _, done := range read {
				if done.expression == want.expression {
					got, found = done.result, true
					break
				}
			}
			if !found {
				result, err := r.eval.evaluate(want.expression)
				if err != nil {
					return nil // drawn again: its error is the drawing's to say
				}
				got = result
				read = append(read, readValue{expression: want.expression, result: result})
			}
			if !sameValue(got, want.result) {
				same = false
				break
			}
		}
		if same {
			r.touched += len(v.reads)
			return v.kept
		}
	}
	return nil
}

// primitive says sameValue reads v: a string, a number, a boolean, null or undefined.
func primitive(v value.Value) bool {
	return v.IsNullish() || v.IsString() || v.IsNumber() || v.IsBoolean()
}

// keep adds an opening read with v.reads to vs, when each value is primitive and vs has room; an opening that read
// an object or an array is read at each page.
func (vs *variants) keep(site *Site, v *variant) {
	for _, read := range v.reads {
		if !primitive(read.result) {
			vs.varies.Store(true)
			return
		}
	}
	vs.mu.Lock()
	defer vs.mu.Unlock()
	var list []*variant
	if old := vs.list.Load(); old != nil {
		list = *old
	}
	if len(list) >= maxVariants {
		vs.varies.Store(true)
		return
	}
	if site.keptReads.Add(1) > maxKeptReads {
		return
	}
	grown := append(list[:len(list):len(list)], v)
	vs.list.Store(&grown)
}

// keptOpening is n's opening, read once where it stands.
func (r *renderer) keptOpening(n *Node, kind tagKind) (start, error) {
	if !r.openingKeepable(n) {
		return r.opening(n, kind)
	}
	key := openingKey{part: r.partKeyOf(n, false), kind: kind}
	var vs *variants // the openings this place was, when it reads the page's data
	if k := r.keptStartOf(key); k != nil {
		switch {
		case k.dynamic && (k.variants == nil || k.variants.varies.Load() || !n.unchanged()):
			return r.opening(n, kind)
		case k.dynamic:
			if v := r.variantOf(k.variants); v != nil {
				r.replayStart(v)
				return v.open.clipped(), nil
			}
			vs = k.variants // read below, and kept for its values
		case k.pathBound:
			key.part.bound, key.part.path = true, r.head.Path
			k = nil
			if bound, ok := r.site.openings.Load(key); ok {
				k = bound.(*keptStart)
			}
		}
		if vs == nil && k != nil && n.unchanged() {
			r.replayStart(k)
			return k.open.clipped(), nil
		}
	}
	outer, reading := r.rules, r.reading
	r.rules, r.reading = &stylesheet.Rules{}, &variant{}
	touched, pathReads, hostReads := r.touched, r.pathReads, r.hostReads
	scopes, scopeReads := r.openScopes(), r.scopeReads
	icons, warned, unread := len(r.icons), len(r.warnLog), len(r.unreadLog)
	before, mark := r.state(), r.effectsMark()
	open, err := r.opening(n, kind)
	own, read := r.rules, r.reading
	r.rules, r.reading = outer, reading
	outer.Merge(own)
	if err != nil {
		return open, err
	}
	k := &keptStart{open: open.clipped(), rules: own, icons: append([]string(nil), r.icons[icons:]...),
		warnings: append([]Warning(nil), r.warnLog[warned:]...), unread: append([]string(nil), r.unreadLog[unread:]...),
		effects: r.effectsSince(mark)}
	if r.touched != touched || r.hostReads != hostReads || r.readOpen(scopes, scopeReads) || r.state() != before {
		if vs == nil {
			kept, _ := r.site.openings.LoadOrStore(key, &keptStart{dynamic: true, variants: &variants{}})
			vs = kept.(*keptStart).variants
		}
		// read only by its expressions: kept for the values they were (variantOf); read otherwise, at each page
		switch {
		case vs == nil:
		case r.touched-touched == len(read.reads) && r.hostReads == hostReads && r.pathReads == pathReads &&
			!r.readOpen(scopes, scopeReads) && r.state() == before && n.unchanged():
			read.kept = k
			vs.keep(r.site, read)
		default:
			vs.varies.Store(true)
		}
		return open, nil
	}
	if r.pathReads != pathReads {
		r.site.openings.LoadOrStore(key, boundStart)
		if r.site.boundParts.Add(1) > maxBoundParts {
			return open, nil
		}
		key.part.bound, key.part.path = true, r.head.Path
	}
	r.site.openings.LoadOrStore(key, k)
	return open, nil
}

// clipped is s with each of its lists capped at its length: an append to one makes its own array, so a start kept
// for every page is never written over by one.
func (s start) clipped() start {
	s.classes = s.classes[:len(s.classes):len(s.classes)]
	s.attributes = s.attributes[:len(s.attributes):len(s.attributes)]
	s.regions = s.regions[:len(s.regions):len(s.regions)]
	return s
}

// what the drawing reads of the request, counted (part).
func (r *renderer) path() string               { r.pathReads++; return r.head.Path }
func (r *renderer) method() string             { r.touched++; return r.head.Method }
func (r *renderer) tab() string                { r.touched++; return r.head.Tab }
func (r *renderer) siteComponents() Components { r.touched++; return r.head.Components }
func (r *renderer) unreadNow(name string) {
	r.unread[name] = true
	r.unreadLog = append(r.unreadLog, name)
}
func (r *renderer) afterOf(n *Node) string { return touchedIf(r, r.after[n], r.after[n] != "") }
func (r *renderer) hostOf(n *Node) *host {
	h := r.hosts[n]
	if h != nil {
		r.hostReads++
	}
	return h
}
func (r *renderer) marksOf(n *Node) []string { return touchedIf(r, r.marks[n], r.marks[n] != nil) }
func (r *renderer) tabsPlanOf(n *Node) *tabsPlan {
	return touchedIf(r, r.tabsHosts[n], r.tabsHosts[n] != nil)
}
func (r *renderer) tabButtonOf(n *Node) *tabsPlan {
	return touchedIf(r, r.tabButtons[n], r.tabButtons[n] != nil)
}
func (r *renderer) tabPanelOf(n *Node) *panelPlan {
	return touchedIf(r, r.tabPanels[n], r.tabPanels[n] != nil)
}
func (r *renderer) selectOf(parent *Node) *schema.Input {
	if r.form == nil {
		return nil
	}
	return touchedIf(r, r.form.selects[parent], r.form.selects[parent] != nil)
}

func touchedIf[T any](r *renderer, v T, found bool) T {
	if found {
		r.touched++
	}
	return v
}

// pureNames are the builtins an expression may read and still give the same each time: what they do depends on
// what they are given alone.
var pureNames = map[string]bool{"Array": true, "Boolean": true, "Infinity": true, "JSON": true, "NaN": true, "Number": true,
	"Object": true, "String": true, "decodeURI": true, "decodeURIComponent": true, "encodeURI": true,
	"encodeURIComponent": true, "isFinite": true, "isNaN": true, "parseFloat": true, "parseInt": true}

// pureBuiltins says every builtin read is one of pureNames.
func pureBuiltins(builtins []string) bool {
	for _, name := range builtins {
		if !pureNames[name] {
			return false
		}
	}
	return true
}

// fromFrames says x reads only the names of the Kitwork components around, as they start (their state, a tabs' plan),
// with pure builtins: what it gives is the same on every page, as the component starts the same.
func (r *renderer) fromFrames(x *expression) bool {
	if !x.pure {
		return false
	}
	for _, name := range x.reads {
		if f := r.declaring(name); f == nil || f.kind != frameKitwork || !f.fixed {
			return false
		}
	}
	return true
}

// The kinds of scope a part may stand in that it can read the request through: a terminal's commands, a field bound
// to a form, a breadcrumb's trail, an autocomplete's value chosen (whose option is aria-selected).
const (
	scopeTerminal = iota
	scopeField
	scopeBreadcrumb
	scopeAutocomplete
	scopeKinds
)

// readScope counts a read of the scope of kind that is open.
func (r *renderer) readScope(kind int) { r.scopeReads[kind]++ }

// openScopes says which kinds of scope are open, one bit each.
func (r *renderer) openScopes() uint {
	var open uint
	set := func(kind int, on bool) {
		if on {
			open |= 1 << kind
		}
	}
	set(scopeTerminal, r.terminal != nil)
	set(scopeField, r.field != nil)
	set(scopeBreadcrumb, r.breadcrumb || r.crumbs != nil)
	set(scopeAutocomplete, r.autocomplete != nil)
	return open
}

// readOpen says a scope of a kind open when the part began (open) was read since (reads were the counts then): a
// scope of a kind begun inside the part counts as read too when one of that kind was open, which only draws it again.
func (r *renderer) readOpen(open uint, reads [scopeKinds]int) bool {
	for kind := range scopeKinds {
		if open&(1<<kind) != 0 && r.scopeReads[kind] != reads[kind] {
			return true
		}
	}
	return false
}

// replayStart writes again what reading the opening k wrote besides it.
func (r *renderer) replayStart(k *keptStart) {
	r.rules.Merge(k.rules)
	r.icons = append(r.icons, k.icons...)
	for _, w := range k.warnings {
		r.say(w)
	}
	for _, name := range k.unread {
		r.unreadNow(name)
	}
	r.apply(k.effects)
}

// nodeMemo is the part and the opening a site last kept for a source node, each with its key: a node drawn where it
// stood before finds them by comparing the key, without the site's maps.
type nodeMemo struct {
	part    atomic.Pointer[memoPart]
	opening atomic.Pointer[memoStart]
}

type memoPart struct {
	key  partKey
	part *part
}

type memoStart struct {
	key   openingKey
	start *keptStart
}

// keptPart is the part the site keeps for key, nil for none. What the site keeps for a key never changes once kept.
func (r *renderer) keptPart(key partKey) *part {
	memo := key.source.memo
	if memo != nil {
		if last := memo.part.Load(); last != nil && last.key == key {
			return last.part
		}
	}
	kept, ok := r.site.parts.Load(key)
	if !ok {
		return nil
	}
	p := kept.(*part)
	if memo != nil && !key.bound {
		memo.part.Store(&memoPart{key: key, part: p})
	}
	return p
}

// keptStartOf is the opening the site keeps for key, nil for none.
func (r *renderer) keptStartOf(key openingKey) *keptStart {
	memo := key.part.source.memo
	if memo != nil {
		if last := memo.opening.Load(); last != nil && last.key == key {
			return last.start
		}
	}
	kept, ok := r.site.openings.Load(key)
	if !ok {
		return nil
	}
	k := kept.(*keptStart)
	if memo != nil && !key.part.bound {
		memo.opening.Store(&memoStart{key: key, start: k})
	}
	return k
}
