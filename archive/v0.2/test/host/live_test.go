package host_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// liveFiles are an app whose pages hold live names (engine2/docs/view.md mục 2.3, host/tab.go): a board a view keeps
// in sync, a component of the site that keeps a count of it on a page with no router.view, a page that binds
// without sync, a page behind a guard, a probe of the reads a page notes.
func liveFiles() map[string]string {
	site := "apps/shop/shop.test/"
	storage := "import { memory } from \"kitwork/storage\";\n"
	return map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		site + "board/router.work": routerImport + storage + `const list = memory("board").segment("list");
const other = memory("board").segment("other");

router.view((context) => {
  var note = "";
  const items = list.get() ?? [];
  const count = items.length;
  const add = (text) => {
    list.set(items.concat([text]));
  };
  const touch = () => {
    other.set(1);
  };
  const wipe = () => {
    memory("board").clear();
  };
  const drop = () => {
    memory("gone").delete();
  };
  const del = () => {
    list.delete();
  };
  return context.sync({ items, count, add, touch, wipe, drop, del }).bind({ note });
});
`,
		site + "board/content.ui": "<content><text>{{ count }}</text><list><item for=\"x of items\"><text>{{ x }}</text></item></list><text>{{ note }}</text></content>\n",
		site + "components.work": "import * as component from \"kitwork/component\";\n" + storage + `component.define("board-count", (context) => {
  var extra = 0;
  const n = (memory("board").segment("list").get() ?? []).length + extra;
  const bump = () => {
    extra = extra + 1;
  };
  return context.sync({ n }).bind({ extra, bump });
});
`,
		site + "counted/content.ui": "<content><board-count><text>{{ n }}</text></board-count></content>\n",
		site + "plain/router.work":  routerImport + storage + "router.view((context) => context.bind({ items: memory(\"board\").segment(\"list\").get() ?? [], go: () => {} }));\n",
		site + "plain/content.ui":   "<content><text>{{ items.length }}</text></content>\n",
		site + "guarded/router.work": routerImport + storage + `router.guard((context) => context.cookie("k") === "1" ? context.error("Không") : context.next()).limit({ rate: 1, per: "1m", by: "ip" }).view((context) => {
  const n = (memory("board").segment("list").get() ?? []).length;
  return context.sync({ n }).bind({});
});
`,
		site + "guarded/content.ui": "<content><text>{{ n }}</text></content>\n",
		site + "probe/router.work": routerImport + storage + `router.view((context) => {
  const there = memory("probe").segment("a").exists();
  const left = memory("probe").segment("b").ttl();
  return context.sync({ there, left }).bind({});
});
`,
		site + "probe/content.ui": "<content><text>{{ there }}</text></content>\n",
		site + "both/router.work": routerImport + storage + "router.view((context) => context.sync({ count: (memory(\"board\").segment(\"list\").get() ?? []).length }).bind({}));\n",
		site + "both/content.ui":  "<content><text>{{ count }}</text><board-count><text>{{ n }}</text></board-count></content>\n",
		site + "pick/router.work": routerImport + storage + `router.view((context) => {
  var which = "a";
  const v = memory("pick").segment(which).get();
  return context.sync({ v }).bind({ which });
});
`,
		site + "pick/content.ui":        "<content><text>{{ which }}</text></content>\n",
		site + "pick/write/router.work": routerImport + storage + "router.post((context) => context.text(memory(\"pick\").segment(context.request.query(\"key\")).set(1) ?? \"ok\"));\n",
		site + "probe/root/router.work": routerImport + storage + "router.post((context) => context.text(memory(\"probe\").set(1) ?? \"ok\"));\n",
		site + "probe/write/router.work": routerImport + storage + `router.post((context) => {
  memory("probe").segment(context.request.query("key")).set(1);
  return context.text("ok");
});
`,
	}
}

// liveSite boots the app of liveFiles, with set changing the host before it serves.
func liveSite(t *testing.T, set func(*host.Host)) *booted {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, liveFiles()), "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	if set != nil {
		set(read)
	}
	return serve(t, read, log)
}

// tabOf is the tab a page was drawn for, as its data block names it; "" for none.
func tabOf(body string) string {
	start := strings.Index(body, `"tab":"`)
	if start < 0 {
		return ""
	}
	rest := body[start+len(`"tab":"`):]
	return rest[:strings.Index(rest, `"`)]
}

// ask posts the tab's ask for path again.
func (b *booted) ask(t testing.TB, path, body string, headers ...string) reply {
	t.Helper()
	return b.request(t, http.MethodPost, "shop.test", path+"?kitwork-refresh=1", strings.NewReader(body),
		append([]string{"Content-Type", "application/json"}, headers...)...)
}

// quiet says the channel sends nothing for a while.
func quiet(t *testing.T, e *events, what string) {
	t.Helper()
	select {
	case frame := <-e.frames:
		if frame != ": heartbeat" {
			t.Errorf("%s: the channel said %q", what, frame)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// A page that holds live names is drawn for a tab; the tab listens on its channel, which says "changed" when a key
// the page read is written; asked again, the page answers the live names that differ from what the tab holds.
func TestATabIsToldWhatChangedAndAsksAgain(t *testing.T) {
	b := liveSite(t, nil)
	page := b.get(t, "shop.test", "/board")
	id := tabOf(page.body)
	if len(id) != 22 {
		t.Fatalf("the board's tab: %q\n%s", id, page.body)
	}
	if tabOf(b.get(t, "shop.test", "/plain").body) != "" {
		t.Errorf("a page with no sync is drawn for a tab")
	}
	if other := tabOf(b.get(t, "shop.test", "/board").body); other == id {
		t.Errorf("two draws, one tab")
	}
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+id)
	if channel.response.StatusCode != http.StatusOK || channel.response.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("the channel: %d %q", channel.response.StatusCode, channel.response.Header.Get("Content-Type"))
	}
	if got := channel.next(t); got != "retry: 2000" {
		t.Errorf("the channel opens with %q", got)
	}
	// another tab writes a key the page read: the tab is told, asks, and gets what changed
	answers(t, "add", b.call(t, "/board", "add", `{"seq":1,"args":["a"]}`), 200, `{"seq":1,"page":{"count":1,"items":["a"]}}`)
	if got := channel.next(t); !strings.HasPrefix(got, "event: changed\ndata: ") {
		t.Errorf("the channel after a write: %q", got)
	}
	answers(t, "the ask", b.ask(t, "/board", `{"seq":2,"tab":"`+id+`","page":{"note":"đang gõ"}}`), 200, `{"seq":2,"page":{"items":["a"],"count":1}}`)
	answers(t, "an ask with nothing new", b.ask(t, "/board", `{"seq":3,"tab":"`+id+`"}`), 200, `{"seq":3}`)
	// a key the page did not read tells nothing; a delete of nothing tells nothing
	b.call(t, "/board", "touch", `{}`)
	quiet(t, channel, "a key not read")
	b.call(t, "/board", "drop", `{}`)
	quiet(t, channel, "a delete of nothing")
	// the clear of a branch tells the keys under it
	b.call(t, "/board", "wipe", `{}`)
	if got := channel.next(t); !strings.HasPrefix(got, "event: changed") {
		t.Errorf("the channel after a clear: %q", got)
	}
	answers(t, "the ask after the clear", b.ask(t, "/board", `{"seq":4,"tab":"`+id+`"}`), 200, `{"seq":4,"page":{"items":[],"count":0}}`)
	b.call(t, "/board", "wipe", `{}`)
	quiet(t, channel, "the clear of an empty branch")
	b.call(t, "/board", "del", `{}`)
	quiet(t, channel, "the delete of a key not there")
	b.call(t, "/board", "add", `{"args":["z"]}`)
	channel.next(t)
	b.call(t, "/board", "del", `{}`)
	if got := channel.next(t); !strings.HasPrefix(got, "event: changed") {
		t.Errorf("the channel after a delete: %q", got)
	}
	answers(t, "the ask after the delete", b.ask(t, "/board", `{"seq":4,"tab":"`+id+`"}`), 200, `{"seq":4}`)
	// a tab that calls a work holds the page as the work left it: asked then, nothing
	answers(t, "a call that names its tab", b.call(t, "/board", "add", `{"seq":5,"tab":"`+id+`","args":["b"]}`), 200, `{"seq":5,"page":{"count":1,"items":["b"]}}`)
	answers(t, "the ask after its own call", b.ask(t, "/board", `{"seq":6,"tab":"`+id+`"}`), 200, `{"seq":6}`)
	// a tab the server does not remember gets every live name, and is remembered
	unknown := strings.Repeat("A", 22)
	answers(t, "an ask of a tab not known", b.ask(t, "/board", `{"seq":7,"tab":"`+unknown+`"}`), 200, `{"seq":7,"page":{"items":["b"],"count":1}}`)
	answers(t, "the same tab again", b.ask(t, "/board", `{"seq":8,"tab":"`+unknown+`"}`), 200, `{"seq":8}`)
}

// A site component that keeps names in sync is drawn for the tab of any page, its instance listening as the page;
// what changed of it comes under its place.
func TestAComponentsLiveNamesComeUnderItsPlace(t *testing.T) {
	b := liveSite(t, nil)
	page := b.get(t, "shop.test", "/counted")
	id, places := tabOf(page.body), placesOf(page.body, "board-count")
	if id == "" || len(places) != 1 {
		t.Fatalf("tab %q, places %v\n%s", id, places, page.body)
	}
	contains(t, "the page", page.body, `data-kitwork-bind="text: n">0</p>`, `{"`+places[0]+`":{"extra":0,"n":0},"jobs":{"`+places[0]+`":["bump"]},"vars":{"`+places[0]+`":["extra"]},"tab":"`+id+`"}`)
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+id)
	channel.next(t)
	b.call(t, "/board", "add", `{"args":["a"]}`)
	if got := channel.next(t); !strings.HasPrefix(got, "event: changed") {
		t.Errorf("the channel of the component's page: %q", got)
	}
	answers(t, "the ask", b.ask(t, "/counted", `{"seq":1,"tab":"`+id+`"}`), 200, fmt.Sprintf(`{"seq":1,"%s":{"n":1}}`, places[0]))
	// the instance runs with the vars it holds
	answers(t, "the ask with the vars", b.ask(t, "/counted", fmt.Sprintf(`{"seq":2,"tab":"%s","%s":{"extra":5}}`, id, places[0])), 200,
		fmt.Sprintf(`{"seq":2,"%s":{"n":6}}`, places[0]))
	// a work of it keeps the record: asked then, nothing
	answers(t, "the work", b.call(t, "/counted", "board-count.bump", fmt.Sprintf(`{"seq":3,"tab":"%s","at":"%s","%s":{"extra":5}}`, id, places[0], places[0])), 200,
		fmt.Sprintf(`{"seq":3,"%s":{"extra":6,"n":7}}`, places[0]))
	answers(t, "the ask after the work", b.ask(t, "/counted", fmt.Sprintf(`{"seq":4,"tab":"%s","%s":{"extra":6}}`, id, places[0])), 200, `{"seq":4}`)
	// the page's live names come first, then each instance's
	both := b.get(t, "shop.test", "/both")
	instance := placesOf(both.body, "board-count")[0]
	b.call(t, "/board", "add", `{"args":["b"]}`)
	answers(t, "the page and its instance", b.ask(t, "/both", `{"seq":5,"tab":"`+tabOf(both.body)+`"}`), 200,
		fmt.Sprintf(`{"seq":5,"page":{"count":2},"%s":{"n":2}}`, instance))
}

// The keys a tab is told about are those its last run read: a var that chooses the key moves the tab to the new one.
func TestATabListensToWhatItsLastRunRead(t *testing.T) {
	b := liveSite(t, nil)
	id := tabOf(b.get(t, "shop.test", "/pick").body)
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+id)
	channel.next(t)
	answers(t, "the ask with another key", b.ask(t, "/pick", `{"seq":1,"tab":"`+id+`","page":{"which":"b"}}`), 200, `{"seq":1}`)
	b.request(t, http.MethodPost, "shop.test", "/pick/write?key=a", nil)
	quiet(t, channel, "the key it read before")
	b.request(t, http.MethodPost, "shop.test", "/pick/write?key=b", nil)
	if got := channel.next(t); !strings.HasPrefix(got, "event: changed") {
		t.Errorf("the key it reads now: %q", got)
	}
}

// The reads a page notes: exists() and ttl() as get(); a key written under its own name tells the tab that read it.
func TestAPageNotesWhatItReads(t *testing.T) {
	b := liveSite(t, nil)
	id := tabOf(b.get(t, "shop.test", "/probe").body)
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+id)
	channel.next(t)
	for _, key := range []string{"a", "b"} {
		if got := b.request(t, http.MethodPost, "shop.test", "/probe/write?key="+key, nil); got.status != 200 {
			t.Fatalf("write %s: %d %s", key, got.status, b.log.String())
		}
		if got := channel.next(t); !strings.HasPrefix(got, "event: changed") {
			t.Errorf("a write of %s: %q", key, got)
		}
	}
	b.request(t, http.MethodPost, "shop.test", "/probe/write?key=c", nil)
	quiet(t, channel, "a key not read")
	// a write of the key a branch begins with is that key's alone
	if got := b.request(t, http.MethodPost, "shop.test", "/probe/root", nil); got.status != 200 {
		t.Fatalf("write the root: %d %s", got.status, b.log.String())
	}
	quiet(t, channel, "the key of the branch")
}

// A call that names its tab answers each live name the record says the tab does not hold, a change it missed too,
// and the record then says it holds the page as the work left it.
func TestACallBringsWhatTheTabMissed(t *testing.T) {
	b := liveSite(t, nil)
	id := tabOf(b.get(t, "shop.test", "/board").body)
	b.call(t, "/board", "add", `{"args":["a"]}`) // another tab's, while this one listens to nothing
	answers(t, "a work that changes nothing", b.call(t, "/board", "touch", `{"seq":1,"tab":"`+id+`"}`), 200, `{"seq":1,"page":{"count":1,"items":["a"]}}`)
	answers(t, "the ask then", b.ask(t, "/board", `{"seq":2,"tab":"`+id+`"}`), 200, `{"seq":2}`)
	answers(t, "a call of a tab that holds it all", b.call(t, "/board", "touch", `{"seq":3,"tab":"`+id+`"}`), 200, `{"seq":3}`)
	answers(t, "a call that names no tab", b.call(t, "/board", "touch", `{"seq":3}`), 200, `{"seq":3}`)
	both := b.get(t, "shop.test", "/both")
	tab, instance := tabOf(both.body), placesOf(both.body, "board-count")[0]
	b.call(t, "/board", "add", `{"args":["b"]}`)
	answers(t, "a component's work", b.call(t, "/both", "board-count.bump", fmt.Sprintf(`{"seq":4,"tab":"%s","at":"%s","%s":{"extra":0}}`, tab, instance, instance)), 200,
		fmt.Sprintf(`{"seq":4,"page":{"count":2},"%s":{"extra":1,"n":3}}`, instance))
	answers(t, "the ask after it", b.ask(t, "/both", fmt.Sprintf(`{"seq":5,"tab":"%s","%s":{"extra":1}}`, tab, instance)), 200, `{"seq":5}`)
	answers(t, "its work again", b.call(t, "/both", "board-count.bump", fmt.Sprintf(`{"seq":6,"tab":"%s","at":"%s","%s":{"extra":1}}`, tab, instance, instance)), 200,
		fmt.Sprintf(`{"seq":6,"%s":{"extra":2,"n":4}}`, instance))
}

// What the channel and the ask refuse, and the rules an ask meets: the page's guards, never a limit.
func TestAnAskMeetsThePagesGuardsAndTheChannelItsCeiling(t *testing.T) {
	b := liveSite(t, func(h *host.Host) { h.StreamLimit = 1 })
	id := tabOf(b.get(t, "shop.test", "/board").body)
	for path, status := range map[string]int{
		"/_kitwork/changes": 400,
		"/_kitwork/changes?tab=" + strings.Repeat("*", 22): 400,
		"/_kitwork/changes?tab=short":                      400,
		"/_kitwork/changes?tab=" + strings.Repeat("B", 22): 404,
	} {
		if got := b.get(t, "shop.test", path); got.status != status {
			t.Errorf("GET %s: %d, want %d", path, got.status, status)
		}
	}
	if got := b.request(t, http.MethodPost, "shop.test", "/_kitwork/changes?tab="+id, nil); got.status != 405 || got.header.Get("Allow") != "GET" {
		t.Errorf("POST to the channel: %d %q", got.status, got.header.Get("Allow"))
	}
	first := open(t, b, "shop.test", "/_kitwork/changes?tab="+id)
	first.next(t)
	if got := b.get(t, "shop.test", "/_kitwork/changes?tab="+id); got.status != 503 || got.header.Get("Retry-After") != "5" {
		t.Errorf("a channel past the ceiling: %d %q", got.status, got.header.Get("Retry-After"))
	}
	first.response.Body.Close()
	opened := false
	for i := 0; i < 50 && !opened; i++ {
		time.Sleep(20 * time.Millisecond) // the server sees the first go
		again := openStatus(t, b, "/_kitwork/changes?tab="+id)
		opened = again == http.StatusOK
	}
	if !opened {
		t.Errorf("a channel after the one before closed: still refused")
	}
	error := func(code int, message string) string { return fmt.Sprintf(`{"code":%d,"message":%q}`, code, message) }
	for what, body := range map[string]string{
		"no body":          ``,
		"no tab":           `{"seq":1}`,
		"a bad tab":        `{"tab":"x"}`,
		"vars of no whole": `{"tab":"` + id + `","c1":[1]}`,
	} {
		answers(t, what, b.ask(t, "/board", body), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	}
	// the ask meets the guards, and no limit: the page's limit of one a minute let its draw on, and lets each ask on
	guarded := tabOf(b.get(t, "shop.test", "/guarded").body)
	answers(t, "an ask past the limit", b.ask(t, "/guarded", `{"seq":1,"tab":"`+guarded+`"}`), 200, `{"seq":1}`)
	answers(t, "an ask again", b.ask(t, "/guarded", `{"seq":2,"tab":"`+guarded+`"}`), 200, `{"seq":2}`)
	answers(t, "an ask the guard refuses", b.ask(t, "/guarded", `{"seq":3,"tab":"`+guarded+`"}`, "Cookie", "k=1"), 403, `{"seq":3,"error":`+error(403, "Không")+`}`)
	contains(t, "the log", b.log.String(), "(kitwork-refresh): 400: an ask for the page again names its tab", "(kitwork-refresh): 400: tab is the id")
}

// An app remembers ten tabs for each channel it may hold open: past that, the one least lately used goes, and its
// next ask gets every live name.
func TestAnAppRemembersSoManyTabs(t *testing.T) {
	b := liveSite(t, func(h *host.Host) { h.StreamLimit = 1 })
	listening := tabOf(b.get(t, "shop.test", "/board").body)
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+listening)
	channel.next(t)
	first := tabOf(b.get(t, "shop.test", "/board").body)
	for i := 0; i < 9; i++ {
		b.get(t, "shop.test", "/board")
	}
	answers(t, "the first tab not listening, forgotten", b.ask(t, "/board", `{"seq":1,"tab":"`+first+`"}`), 200, `{"seq":1,"page":{"items":[],"count":0}}`)
	answers(t, "the tab that listens, remembered", b.ask(t, "/board", `{"seq":2,"tab":"`+listening+`"}`), 200, `{"seq":2}`)
	// a page that holds nothing live takes no room
	kept := tabOf(b.get(t, "shop.test", "/board").body)
	for i := 0; i < 10; i++ {
		b.get(t, "shop.test", "/plain")
	}
	answers(t, "a tab past pages that hold nothing live", b.ask(t, "/board", `{"seq":3,"tab":"`+kept+`"}`), 200, `{"seq":3}`)
}

// A channel writes a comment every heartbeat, so a proxy keeps it.
func TestAChannelBeats(t *testing.T) {
	b := liveSite(t, func(h *host.Host) { h.StreamHeartbeat = 30 * time.Millisecond })
	channel := open(t, b, "shop.test", "/_kitwork/changes?tab="+tabOf(b.get(t, "shop.test", "/board").body))
	channel.next(t)
	select {
	case frame := <-channel.frames:
		if frame != ": heartbeat" {
			t.Errorf("the channel beats %q", frame)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("no heartbeat")
	}
}

// openStatus is the status a GET of path answers with, its body let go at once.
func openStatus(t *testing.T, b *booted, path string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.server.URL+path, nil)
	request.Host = "shop.test"
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}
