package host

import (
	"crypto/rand"
	"encoding/base64"
	"hash/fnv"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// The record of a tab (engine2/docs/view.md mục 2.3: the names of sync are the server's, kept live; docs/
// KITWORK_BRIDGE.md mục 8, level 1 on the web; slice 4). A page whose page or instances bind names in sync is drawn
// for a tab: the server remembers what the tab holds of those names (a hash of each, by instance) and the keys of
// kitwork/storage its runs read (storage.Reads, noted through the run's context). The tab listens on one channel,
//
//	GET /_kitwork/changes?tab=<id>
//
// a stream that says only "changed". A write of a key a tab's runs read tells it; the tab asks for its page again,
// through the queue of its calls, with its cookies and past its page's guards,
//
//	POST <the page>?kitwork-refresh=1   { "seq": 7, "tab": "…", "page": { var… }, "c3fq8w": { var… } }
//
// and the server runs the page again, its instances with the vars they hold, and answers the names of sync that differ
// from what the record says the tab holds, by instance: { "seq": 7, "page": { "todos": [ … ] } }. A call of a work
// that names its tab keeps the record as the call leaves the page. A tab that wrote is told too: what it asks then is
// usually nothing, and its other instances that read the key come right.
//
// The record is the server's memory, not the truth: lost (the process started again, the tab idle too long), the
// next ask makes it again, every live name sent whole. It is one process's: tabs of another server are not told.
// Writes of kitwork/database tell no tab yet.

const (
	// tabsPerChannel is how many tabs one app remembers for each channel it may hold open (Host.StreamLimit, 1,000
	// by default, as many as streams of context.sse): 10,000 tabs; past it the one least lately used and not
	// listening goes
	tabsPerChannel = 10
	// refreshParameter asks for a page again, with no work
	refreshParameter = "kitwork-refresh"
	// changesPath is the channel a tab listens on
	changesPath = "/_kitwork/changes"
)

// tabReads notes the keys of kitwork/storage a request's runs read (storage.Reads), what a tab is told about. A
// handle a routine kept may note after the request: the lock keeps that safe, and nothing reads it then.
type tabReads struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (t *tabReads) Read(key string) {
	t.mu.Lock()
	if t.keys == nil {
		t.keys = map[string]bool{}
	}
	t.keys[key] = true
	t.mu.Unlock()
}

// list is the keys read, in order.
func (t *tabReads) list() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	keys := make([]string, 0, len(t.keys))
	for key := range t.keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// tab is what the server remembers of one tab: the keys its runs read, each live name of each instance as the tab
// holds it, when it was last used, the channel's wake and how many channels it has open.
type tab struct {
	id   string
	keys []string
	held map[string]map[string]uint64
	// used is when it was last used, in the order of its app's uses (appTabs.uses), never a clock: a clock of
	// Windows moves by half a millisecond, within which a page is drawn more than once (11/11/2026)
	used      uint64
	wake      chan struct{}
	listening int
}

// appTabs are the tabs of one app, which its sites share, as its storage is: a write of one site's page tells the
// tabs of another. byKey finds the tabs that read a key.
type appTabs struct {
	host     *Host
	mu       sync.Mutex
	tabs     map[string]*tab
	byKey    map[string]map[*tab]struct{}
	channels int
	// uses counts the uses of its tabs, the last of each its used
	uses uint64
}

func newAppTabs(h *Host) *appTabs {
	return &appTabs{host: h, tabs: map[string]*tab{}, byKey: map[string]map[*tab]struct{}{}}
}

// newTabID is a tab's id: 16 random bytes, URL-safe base64.
func newTabID() string {
	var b [16]byte
	_, _ = io.ReadFull(rand.Reader, b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// tabID says id can be a tab's.
func tabID(id string) bool {
	if len(id) != 22 {
		return false
	}
	for i := 0; i < len(id); i++ {
		switch c := id[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// pageDraw is a page drawn for a tab: the tab's id, the keys its runs read, each instance drawn with live names, by
// place ("page" for the page).
type pageDraw struct {
	tab    string
	reads  *tabReads
	places map[string]livePlace
}

// newDraw is a page about to be drawn for a new tab, its runs reading into reads.
func (s *Site) newDraw(reads *tabReads) *pageDraw {
	if reads == nil {
		reads = &tabReads{} // a page a POST draws: what its runs read was not noted
	}
	return &pageDraw{tab: newTabID(), reads: reads, places: map[string]livePlace{}}
}

// note keeps an instance drawn with its names, when it has live ones.
func (d *pageDraw) note(place string, names value.Value, live []string) {
	if len(live) > 0 {
		d.places[place] = livePlace{names: names, live: live}
	}
}

// keepDraw remembers the tab a page was drawn for, when it shows live names: the page told the browser its id.
func (s *Site) keepDraw(draw *pageDraw) {
	if draw != nil && len(draw.places) > 0 {
		s.tabs.keep(draw.tab, draw.reads.list(), draw.places)
	}
}

// livePlace is one instance as a run left it: the names it binds and those of sync, which are kept live.
type livePlace struct {
	names value.Value
	live  []string
}

// heldOf is what the tab holds of an instance's live names once sent: a hash of each, as JSON.
func heldOf(place livePlace) map[string]uint64 {
	held := make(map[string]uint64, len(place.live))
	for _, name := range place.live {
		held[name] = hashOf(place.names.Get(name))
	}
	return held
}

// hashOf is the hash of a value as JSON, what the record keeps of it.
func hashOf(item value.Value) uint64 {
	sum := fnv.New64a()
	if written := value.Stringify(item, ""); written.IsString() {
		sum.Write([]byte(written.Text()))
	}
	return sum.Sum64()
}

// keep remembers the tab of id as a run left it: the keys it read, what it holds of each instance's live names (the
// instances given; the others as they were). A tab not known is made.
func (a *appTabs) keep(id string, keys []string, places map[string]livePlace) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tabs[id]
	if t == nil {
		a.makeRoom()
		t = &tab{id: id, held: map[string]map[string]uint64{}, wake: make(chan struct{}, 1)}
		a.tabs[id] = t
	}
	for _, key := range t.keys {
		if readers := a.byKey[key]; readers != nil {
			delete(readers, t)
			if len(readers) == 0 {
				delete(a.byKey, key)
			}
		}
	}
	t.keys = keys
	for _, key := range keys {
		readers := a.byKey[key]
		if readers == nil {
			readers = map[*tab]struct{}{}
			a.byKey[key] = readers
		}
		readers[t] = struct{}{}
	}
	for place, drawn := range places {
		t.held[place] = heldOf(drawn)
	}
	a.use(t)
}

// use marks t used now, after every tab used before. The lock is held.
func (a *appTabs) use(t *tab) {
	a.uses++
	t.used = a.uses
}

// makeRoom forgets the tab least lately used of those not listening once the app holds as many as it remembers. The
// lock is held.
func (a *appTabs) makeRoom() {
	if len(a.tabs) < a.host.streamLimit()*tabsPerChannel {
		return
	}
	var oldest *tab
	for _, t := range a.tabs {
		if t.listening == 0 && (oldest == nil || t.used < oldest.used) {
			oldest = t
		}
	}
	if oldest != nil {
		a.forget(oldest)
	}
}

// forget drops a tab and its keys. The lock is held.
func (a *appTabs) forget(t *tab) {
	delete(a.tabs, t.id)
	for _, key := range t.keys {
		if readers := a.byKey[key]; readers != nil {
			delete(readers, t)
			if len(readers) == 0 {
				delete(a.byKey, key)
			}
		}
	}
}

// held is what the record says the tab of id holds of place's live names; nil for a tab or an instance it does not
// know, which then gets every live name.
func (a *appTabs) held(id, place string) map[string]uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.tabs[id]; t != nil {
		return t.held[place]
	}
	return nil
}

// changed tells the tabs that read key, or a key under it when it is a branch a clear emptied, that it changed.
func (a *appTabs) changed(key string, branch bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tell := func(readers map[*tab]struct{}) {
		for t := range readers {
			select {
			case t.wake <- struct{}{}:
			default: // told already, not asked yet
			}
		}
	}
	if !branch {
		tell(a.byKey[key])
		return
	}
	for read, readers := range a.byKey {
		if strings.HasPrefix(read, key) {
			tell(readers)
		}
	}
}

// listen opens a channel of the tab of id: its wake; known is false for a tab not known, room when the app holds as
// many channels as Host.StreamLimit.
func (a *appTabs) listen(id string) (*tab, bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tabs[id]
	if t == nil {
		return nil, false, true
	}
	if a.channels >= a.host.streamLimit() {
		return nil, true, false
	}
	a.channels++
	t.listening++
	a.use(t)
	return t, true, true
}

// unlisten closes a channel of the tab.
func (a *appTabs) unlisten(t *tab) {
	a.mu.Lock()
	a.channels--
	t.listening--
	a.use(t)
	a.mu.Unlock()
}

// serveChanges is a tab's channel: a stream that says "changed" when a key the tab's runs read is written, a comment
// every heartbeat, until the browser goes. 404 for a tab the server does not know (the tab asks for its page again,
// which makes it), 503 past Host.StreamLimit channels.
func (s *Site) serveChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		answer(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	id := r.URL.Query().Get("tab")
	if !tabID(id) {
		answer(w, http.StatusBadRequest, "Bad Request")
		return
	}
	t, known, room := s.tabs.listen(id)
	switch {
	case !known:
		answer(w, http.StatusNotFound, "Not Found")
		return
	case !room:
		w.Header().Set("Retry-After", "5")
		answer(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	defer s.tabs.unlisten(t)
	flusher, ok := w.(http.Flusher)
	if !ok {
		answer(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	controller := http.NewResponseController(w)
	host := s.streams.host
	write := func(text string) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(host.streamWriteTimeout()))
		_, err := io.WriteString(w, text)
		if err == nil {
			flusher.Flush()
		}
		_ = controller.SetWriteDeadline(time.Time{})
		return err == nil
	}
	if !write("retry: 2000\n\n") {
		return
	}
	heartbeat := time.NewTicker(host.streamHeartbeat())
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.wake:
			if !write("event: changed\ndata: " + strconv.FormatInt(time.Now().UnixMilli(), 10) + "\n\n") {
				return
			}
		case <-heartbeat.C:
			if !write(": heartbeat\n\n") {
				return
			}
		}
	}
}
