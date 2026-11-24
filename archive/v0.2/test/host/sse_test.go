package host_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// sseImport is the imports of a router.work that opens streams.
const sseImport = "import { memory } from \"kitwork/storage\";\nimport * as routine from \"kitwork/routine\";\n" + routerImport

// sseFiles are an app of two sites, shop.test and other.test, whose doors open streams (engine2/docs/sse.md).
func sseFiles() map[string]string {
	site := "apps/shop/shop.test/"
	return map[string]string{
		"application.work": manifest,
		site + "room/router.work": sseImport + `router.get((context) => {
  const stream = context.header("X-Room", "lobby").cookie("room", "lobby").sse.reconnect("3s");
  stream.send({ event: "hello", id: "0", data: "hi" });
  memory("rooms").segment("lobby").observe((message) => {
    if (message.data.text === "slower") { stream.reconnect("5s"); return; }
    stream.send({ event: message.event, data: { said: message.data.text } });
  });
  return stream;
});
router.post((context) => {
  const { room, text } = context.request.json();
  memory("rooms").segment(room).send({ event: "chat", id: "1", data: { text } });
  return context.json({ ok: true });
});
`,
		site + "lobby/router.work": sseImport + `router.get((context) => context.sse.subscribe(memory("rooms").segment("lobby")).subscribe(memory("rooms").segment("hall")));
`,
		"apps/shop/other.test/lobby/router.work": sseImport + `router.get((context) => context.sse.subscribe(memory("rooms").segment("lobby")));
`,
		site + "words/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  const code = (f) => { const { error } = safe(f); return error ? error.code : null; };
  const closedBefore = stream.closed;
  stream.event("a").id("2").data(5);
  stream.data(6);
  stream.send({ event: "", data: 7 });
  stream.send({ data: {
    same: context.sse === stream, closedBefore,
    eventTwice: code(() => stream.event("a").event("b")), eventBreak: code(() => stream.event("a\nb")),
    idNumber: code(() => stream.id(2)), sendBare: code(() => stream.send(1)), sendExtra: code(() => stream.send({ data: 1, text: "x" })),
    reconnectBad: code(() => stream.reconnect("soon")), reconnectLong: code(() => stream.reconnect("25h")),
    reconnectNumber: code(() => stream.reconnect(3)), reconnectZero: code(() => stream.reconnect("0s")),
    dataTwo: code(() => stream.data(1, 2)), subscribeTwo: code(() => stream.subscribe(memory("a"), memory("b"))),
    subscribeBad: code(() => stream.subscribe("rooms")), closeArgument: code(() => stream.close(1)),
    headerAfter: code(() => context.header("X-A", "1")), jsonAfter: code(() => context.json({})),
    observeNoFunction: code(() => memory("x").observe(1)), observerClose: code(() => memory("x").observe((m) => m).close(1)),
    connect: safe(() => stream.connect()).error?.message, publish: safe(() => stream.publish()).error?.message
  } });
  return stream;
});
`,
		site + "early/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  const first = memory("early").segment("a").observe((message) => stream.send({ event: "a", data: message.data }));
  memory("early").segment("b").observe((message) => stream.send({ event: "b", data: message.data }));
  first.close();
  return stream;
});
router.post((context) => {
  memory("early").segment("a").send({ data: 1 });
  memory("early").segment("b").send({ data: 2 });
  return context.json({ ok: true });
});
`,
		site + "closing/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  memory("closing").observe((message) => {
    memory("closing-last").set(message.data);
    if (message.data === 0) { memory("closing").send({ data: 1 }); memory("closing").send({ data: 2 }); return; }
    stream.send({ data: "last" });
    stream.close();
    const after = safe(() => stream.send({ data: "after" }));
    memory("closing-said").set({ closed: stream.closed, failed: after.error !== null });
  });
  return stream;
});
router.post((context) => {
  memory("closing").send({ data: 0 });
  return context.json({ ok: true });
});
`,
		site + "said/router.work": sseImport + `router.get((context) => {
  const said = memory("closing-said").get() ?? {};
  return context.json({ closed: said.closed, failed: said.failed, last: memory("closing-last").get() });
});
`,
		site + "shut/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  stream.close();
  const many = safe(() => { for (let i = 0; i < 10001; i++) { stream.send({ data: i }); } });
  return error("said " + (many.error === null) + " " + stream.closed);
});
`,
		site + "many/router.work": sseImport + `router.get((context) => {
  for (let i = 0; i < 10000; i++) { memory("many").observe((message) => message); }
  return context.text("kept");
});
`,
		site + "plain/router.work": sseImport + `router.get((context) => context.sse);
`,
		site + "fail/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  memory("fail").observe((message) => message.data.nope.deeper);
  return stream;
});
router.post((context) => {
  memory("fail").send({ data: 1 });
  return context.json({ ok: true });
});
`,
		site + "flood/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  memory("flood").observe((message) => {
    if (message.data === "go") { for (let i = 0; i < 300; i++) { memory("flood").send({ data: i }); } }
    stream.send(message);
  });
  return stream;
});
router.post((context) => {
  memory("flood").send({ data: "go" });
  return context.json({ ok: true });
});
`,
		site + "energy/router.work": sseImport + `const loop = (n) => { let s = 0; for (let i = 0; i < n; i++) { s = s + 1; } return s; };
router.get((context) => {
  loop(250000);
  const stream = context.sse;
  memory("energy").observe((message) => { loop(250000); stream.send(message); });
  return stream;
});
router.post((context) => {
  memory("energy").send({ data: 1 });
  memory("energy").send({ data: 2 });
  memory("energy").send({ data: 3 });
  return context.json({ ok: true });
});
`,
		site + "big/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  memory("big").observe((message) => {
    for (let i = 0; i < 30; i++) { stream.send({ data: "x".repeat(900000) }); }
  });
  return stream;
});
router.post((context) => {
  memory("big").send({ data: "flood" });
  return context.json({ ok: true });
});
`,
		site + "backlog/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  const many = safe(() => { for (let i = 0; i < 10001; i++) { stream.send({ data: i }); } });
  return error("said " + many.error?.code + " " + many.error?.at + ": " + many.error?.message);
});
`,
		site + "backlog/bytes/router.work": sseImport + `router.get((context) => {
  const stream = context.sse;
  const large = safe(() => { for (let i = 0; i < 5; i++) { stream.send({ data: "x".repeat(900000) }); } });
  return error("said " + large.error?.code + " " + large.error?.at + ": " + large.error?.message);
});
`,
		site + "routine/router.work": sseImport + `router.get((context) => {
  const results = routine.wait(routine.go(() => memory("x").observe((message) => message)));
  return context.json({ code: results[0].error?.code, says: results[0].error?.message });
});
`,
		site + "refused/post/router.work":     sseImport + "router.post((context) => context.sse);\n",
		site + "refused/guard/router.work":    sseImport + "router.guard((context) => context.sse).get((context) => context.text(\"x\"));\n",
		site + "refused/status/router.work":   sseImport + "router.get((context) => context.status(201).sse);\n",
		site + "refused/type/router.work":     sseImport + "router.get((context) => context.type(\"text/plain\").sse);\n",
		site + "refused/title/router.work":    sseImport + "router.get((context) => context.title(\"x\").sse);\n",
		site + "refused/answered/router.work": sseImport + "router.get((context) => { context.text(\"x\"); return context.sse; });\n",
		site + "refused/returned/router.work": sseImport + "router.get((context) => { const stream = context.sse; return 1; });\n",
		site + "refused/view/router.work":     sseImport + "router.view((context) => context.sse);\n",
		site + "refused/view/content.ui":      "<content />",
	}
}

// sseSite boots the app of sseFiles, with set changing the host before it serves.
func sseSite(t *testing.T, set func(*host.Host)) *booted {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, sseFiles()), "application.work"), log)
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

// events is a stream as a browser reads it: its frames, each the lines before a blank one.
type events struct {
	response *http.Response
	frames   chan string
}

// open opens the stream of path on the site hostname of b's server.
func open(t *testing.T, b *booted, hostname, path string) *events {
	t.Helper()
	return openOn(t, b.server.URL, b.client, hostname, path)
}

func openOn(t *testing.T, address string, client *http.Client, hostname, path string) *events {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = hostname
	response, err := client.Do(request)
	if err != nil {
		cancel()
		t.Fatalf("GET %s%s: %v", hostname, path, err)
	}
	t.Cleanup(func() { cancel(); response.Body.Close() })
	stream := &events{response: response, frames: make(chan string, 1024)}
	go func() {
		defer close(stream.frames)
		reader := bufio.NewReader(response.Body)
		var frame strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				stream.frames <- strings.TrimSuffix(frame.String(), "\n")
				frame.Reset()
				continue
			}
			frame.WriteString(line)
		}
	}()
	return stream
}

// next is the stream's next frame, heartbeats skipped; "" once the stream closed.
func (e *events) next(t *testing.T) string {
	t.Helper()
	for {
		select {
		case frame, open := <-e.frames:
			if !open {
				return ""
			}
			if frame == ": heartbeat" {
				continue
			}
			return frame
		case <-time.After(5 * time.Second):
			t.Fatal("no frame for 5 s")
			return ""
		}
	}
}

// post sends body as JSON to path of shop.test.
func post(t *testing.T, b *booted, path, body string) {
	t.Helper()
	if got := b.request(t, http.MethodPost, "shop.test", path, strings.NewReader(body), "Content-Type", "application/json"); got.status != http.StatusOK {
		t.Fatalf("POST %s: %d %s\n%s", path, got.status, got.body, b.log.String())
	}
}

// A GET door that reads context.sse answers with its stream (sse.md mục 2, 3): the headers it set before, the
// stream's own, the retry: of reconnect, what it sent before it returned, then each message observe(…) runs its
// function for; a message to another key does not come, reconnect once open writes retry: at once.
func TestAStreamSendsWhatItObserves(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/room")
	header := stream.response.Header
	for name, want := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "X-Accel-Buffering": "no", "X-Room": "lobby"} {
		if got := header.Get(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if header.Get("Set-Cookie") != "room=lobby; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("a cookie set before context.sse: %q", header.Get("Set-Cookie"))
	}
	if stream.response.StatusCode != http.StatusOK || header.Get("Content-Encoding") != "" {
		t.Errorf("the stream: %d, Content-Encoding %q; want 200 as it is", stream.response.StatusCode, header.Get("Content-Encoding"))
	}
	for _, want := range []string{"retry: 3000", "event: hello\nid: 0\ndata: \"hi\""} {
		if got := stream.next(t); got != want {
			t.Fatalf("frame %q, want %q\n%s", got, want, b.log.String())
		}
	}
	post(t, b, "/room", `{"room":"hall","text":"elsewhere"}`)
	post(t, b, "/room", `{"room":"lobby","text":"hello"}`)
	if got, want := stream.next(t), "event: chat\ndata: {\"said\":\"hello\"}"; got != want {
		t.Errorf("observed: %q, want %q", got, want)
	}
	post(t, b, "/room", `{"room":"lobby","text":"slower"}`)
	if got := stream.next(t); got != "retry: 5000" {
		t.Errorf("reconnect on an open stream: %q, want retry: 5000", got)
	}
}

// subscribe(source) sends each message of source as it was sent, from one source or more, on every site of the app.
func TestAStreamSubscribes(t *testing.T) {
	b := sseSite(t, nil)
	lobby := open(t, b, "shop.test", "/lobby")
	other := open(t, b, "other.test", "/lobby")
	time.Sleep(100 * time.Millisecond) // both handlers returned: their observers are kept
	post(t, b, "/room", `{"room":"lobby","text":"one"}`)
	post(t, b, "/room", `{"room":"hall","text":"two"}`)
	for _, want := range []string{"event: chat\nid: 1\ndata: {\"text\":\"one\"}", "event: chat\nid: 1\ndata: {\"text\":\"two\"}"} {
		if got := lobby.next(t); got != want {
			t.Errorf("subscribed: %q, want %q", got, want)
		}
	}
	if got, want := other.next(t), "event: chat\nid: 1\ndata: {\"text\":\"one\"}"; got != want {
		t.Errorf("the other site of the app: %q, want %q", got, want)
	}
}

// The words of a stream (sse.md mục 3): event and id written link by link go with one message only, data is JSON;
// each word refuses what it does not take, an edit or an answer after context.sse fails, connect and publish say what
// replaced them.
func TestAStreamsWords(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/words")
	for _, want := range []string{"event: a\nid: 2\ndata: 5", "data: 6", "data: 7"} {
		if got := stream.next(t); got != want {
			t.Fatalf("frame %q, want %q\n%s", got, want, b.log.String())
		}
	}
	got := stream.next(t)
	for _, want := range []string{`"same":true`, `"closedBefore":false`, `"eventTwice":"INVALID_ARGUMENT"`, `"eventBreak":"INVALID_ARGUMENT"`,
		`"idNumber":"INVALID_ARGUMENT"`, `"sendBare":"INVALID_ARGUMENT"`, `"sendExtra":"INVALID_ARGUMENT"`, `"reconnectBad":"INVALID_ARGUMENT"`,
		`"reconnectLong":"INVALID_ARGUMENT"`, `"reconnectNumber":"INVALID_ARGUMENT"`, `"reconnectZero":"INVALID_ARGUMENT"`,
		`"dataTwo":"INVALID_ARGUMENT"`, `"subscribeTwo":"INVALID_ARGUMENT"`, `"subscribeBad":"INVALID_ARGUMENT"`, `"closeArgument":"INVALID_ARGUMENT"`,
		`"headerAfter":"TYPE"`, `"jsonAfter":"TYPE"`, `"observeNoFunction":"INVALID_ARGUMENT"`, `"observerClose":"INVALID_ARGUMENT"`,
		"context.sse.subscribe(memory(", ".send({ event, id, data }) reaches every stream"} {
		if !strings.Contains(got, want) {
			t.Errorf("the words: %s\ndoes not say %s", got, want)
		}
	}
}

// observer.close() ends an observer early; the stream's others go on.
func TestAnObserverClosesEarly(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/early")
	time.Sleep(100 * time.Millisecond)
	post(t, b, "/early", `{}`)
	if got := stream.next(t); got != "event: b\ndata: 2" {
		t.Errorf("after the first observer closed: %q, want only b's", got)
	}
}

// stream.close() closes the stream now; a send after it does nothing, and stream.closed says it.
func TestAStreamCloses(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/closing")
	time.Sleep(100 * time.Millisecond)
	post(t, b, "/closing", `{}`)
	if got := stream.next(t); got != `data: "last"` {
		t.Errorf("before close: %q", got)
	}
	if got := stream.next(t); got != "" {
		t.Errorf("after close: %q, want the stream closed", got)
	}
	if got := b.get(t, "shop.test", "/said"); got.body != `{"closed":true,"failed":false,"last":1}` {
		t.Errorf("after close: %s, want closed, a send that does nothing, and the message after the close not run", got.body)
	}
	b.get(t, "shop.test", "/shut")
	contains(t, "the log", b.log.String(), "said true true") // a stream closed before it opens keeps nothing
}

// The observers of a request end with it, a request that answers with no stream too: the app's ceiling of
// observers is free again for the next.
func TestARequestsObserversEndWithIt(t *testing.T) {
	b := sseSite(t, nil)
	for range 2 {
		if got := b.get(t, "shop.test", "/many"); got.status != http.StatusOK {
			t.Fatalf("10,000 observers: %d %s\n%s", got.status, got.body, b.log.String())
		}
	}
}

// Each run of a function of observe(…) has a time of its own (Host.RequestTimeout), the handler's long past.
func TestAnObserverRunsWithATimeOfItsOwn(t *testing.T) {
	// the race detector runs a loop of 250,000 turns some ten times slower, past 300 ms: its times are ten times longer
	scale := time.Duration(1)
	if underRace {
		scale = 10
	}
	b := sseSite(t, func(h *host.Host) { h.RequestTimeout = 300 * time.Millisecond * scale })
	stream := open(t, b, "shop.test", "/energy")
	time.Sleep(500 * time.Millisecond * scale)
	post(t, b, "/energy", `{}`)
	for _, want := range []string{"data: 1", "data: 2", "data: 3"} {
		if got := stream.next(t); got != want {
			t.Fatalf("past the handler's time: %q, want %q\n%s", got, want, b.log.String())
		}
	}
}

// A function of observe(…) that fails closes the stream, in the log as the handler's failure; one whose queue fills
// (the stream does not send fast enough) closes it too, in the log.
func TestAStreamClosesOnAFailureAndAFullQueue(t *testing.T) {
	b := sseSite(t, nil)
	failing := open(t, b, "shop.test", "/fail")
	time.Sleep(100 * time.Millisecond)
	post(t, b, "/fail", `{}`)
	if got := failing.next(t); got != "" {
		t.Errorf("after the function failed: %q, want the stream closed", got)
	}
	contains(t, "the log", b.log.String(), "GET /fail (/fail/router.work, observe): the stream closed: ")
	flood := open(t, b, "shop.test", "/flood")
	time.Sleep(100 * time.Millisecond)
	post(t, b, "/flood", `{}`)
	if got := flood.next(t); got != `data: "go"` {
		t.Errorf("flood: %q", got)
	}
	if got := flood.next(t); got != "" {
		t.Errorf("after the queue filled: %q, want the stream closed", got)
	}
	contains(t, "the log", b.log.String(), "GET /flood (/flood/router.work): the stream closed: an observer held 256 messages")
}

// Each run of a function of observe(…) has an energy of its own: three runs of nearly a request's energy each, after a
// handler that spent as much, all run.
func TestAnObserverRunsWithAnEnergyOfItsOwn(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/energy")
	time.Sleep(100 * time.Millisecond)
	post(t, b, "/energy", `{}`)
	for _, want := range []string{"data: 1", "data: 2", "data: 3"} {
		if got := stream.next(t); got != want {
			t.Fatalf("frame %q, want %q\n%s", got, want, b.log.String())
		}
	}
}

// What a handler sends before it returns is at most 10,000 messages and 4 MiB (sse.md mục 3).
func TestAStreamsBacklogHasACeiling(t *testing.T) {
	b := sseSite(t, nil)
	b.get(t, "shop.test", "/backlog")
	b.get(t, "shop.test", "/backlog/bytes")
	contains(t, "the log", b.log.String(), "said INVALID_ARGUMENT router.work:6:69: stream.send: what a handler sends before it returns its stream is at most 10000 messages and 4194304 bytes (10000 messages,",
		"said INVALID_ARGUMENT router.work:6:66: stream.send: what a handler sends before it returns its stream is at most 10000 messages and 4194304 bytes (4 messages, 3600040 bytes held)")
}

// observe(…) in a routine has no stream to send to.
func TestObserveIsARequestsAlone(t *testing.T) {
	b := sseSite(t, nil)
	got := fetchJSON(t, b, "/routine")
	if got["code"] != "UNSUPPORTED" || !strings.Contains(got["says"].(string), "a job, a schedule, a routine has no stream") {
		t.Errorf("observe in a routine: %s", shown(got))
	}
}

// context.sse is a GET door's answer alone, with no status but 200, no type, no page word, no answer before it, and
// a handler that reads it returns it (sse.md mục 2): otherwise 500, the reason in the log.
func TestContextSseIsRefusedWhereItMeansNothing(t *testing.T) {
	b := sseSite(t, nil)
	for path, says := range map[string]string{
		"/refused/post":     "context.sse in router.post(…): a stream is router.get's",
		"/refused/guard":    "context.sse in a guard",
		"/refused/status":   "status(201) before context.sse",
		"/refused/type":     "type(…) before context.sse",
		"/refused/title":    "context.title(…) before sse(…)",
		"/refused/answered": "sse(…) after text(…)",
		"/refused/returned": "returned a number: it returns the stream",
	} {
		method := http.MethodGet
		if path == "/refused/post" {
			method = http.MethodPost
		}
		if got := b.request(t, method, "shop.test", path, nil); got.status != http.StatusInternalServerError {
			t.Errorf("%s: %d %q, want 500", path, got.status, got.body)
		}
		contains(t, path+": the log", b.log.String(), says)
	}
}

// router.view draws its page: context.sse in its function is the request's failure where it is read, 500, the
// reason in the log (sse.md mục 2), never a stream the GET of a page opens. CONTROL: Response.Sse not refusing it
// while router.view runs, the page answers 200 text/event-stream, red.
func TestContextSseInRouterViewIsRefused(t *testing.T) {
	b := sseSite(t, nil)
	stream := open(t, b, "shop.test", "/refused/view")
	if stream.response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("GET /refused/view: %d %q, want 500", stream.response.StatusCode, stream.response.Header.Get("Content-Type"))
	}
	contains(t, "the log", b.log.String(), "context.sse in router.view(…): router.view draws its page, return context.bind({ … })")
}

// HEAD of a stream is its headers, holding nothing: the app's one stream is still free for a GET.
func TestAStreamsHeadHoldsNothing(t *testing.T) {
	b := sseSite(t, func(h *host.Host) { h.StreamLimit = 1 })
	head := b.request(t, http.MethodHead, "shop.test", "/lobby", nil)
	if head.status != http.StatusOK || head.header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("HEAD: %d %q", head.status, head.header.Get("Content-Type"))
	}
	if stream := open(t, b, "shop.test", "/lobby"); stream.response.StatusCode != http.StatusOK {
		t.Errorf("GET after a HEAD: %d, want the app's one stream", stream.response.StatusCode)
	}
}

// An app holds at most StreamLimit streams open, every site of it together: one more is 503 with Retry-After; one
// closed frees its place.
func TestAnAppsStreamsHaveACeiling(t *testing.T) {
	b := sseSite(t, func(h *host.Host) { h.StreamLimit = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.server.URL+"/lobby", nil)
	request.Host = "shop.test"
	first, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	second := b.get(t, "other.test", "/lobby")
	if second.status != http.StatusServiceUnavailable || second.header.Get("Retry-After") != "5" {
		t.Errorf("a second stream of the app: %d, Retry-After %q; want 503, 5", second.status, second.header.Get("Retry-After"))
	}
	cancel()
	first.Body.Close()
	waitFor(t, "the first stream's place", func() bool {
		got := b.request(t, http.MethodHead, "other.test", "/lobby", nil) // HEAD holds nothing, and answers as GET would
		if got.status != http.StatusOK {
			return false
		}
		stream := open(t, b, "other.test", "/lobby")
		return stream.response.StatusCode == http.StatusOK
	})
}

// An open stream writes a heartbeat now and then (StreamHeartbeat).
func TestAStreamBeats(t *testing.T) {
	b := sseSite(t, func(h *host.Host) { h.StreamHeartbeat = 50 * time.Millisecond })
	stream := open(t, b, "shop.test", "/lobby")
	select {
	case frame := <-stream.frames:
		if frame != ": heartbeat" {
			t.Errorf("first frame %q, want the heartbeat", frame)
		}
	case <-time.After(2 * time.Second):
		t.Error("no heartbeat within 2 s")
	}
}

// A browser that stops reading has its stream closed after a write waits StreamWriteTimeout: its place is free again.
func TestAStreamNobodyReadsCloses(t *testing.T) {
	b := sseSite(t, func(h *host.Host) {
		h.StreamLimit = 1
		h.StreamWriteTimeout = 200 * time.Millisecond
	})
	connection, err := net.Dial("tcp", strings.TrimPrefix(b.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "GET /big HTTP/1.1\r\nHost: shop.test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // its handler ran before the first probe takes the app's one place
	waitFor(t, "the stream that does not read to open", func() bool {
		return status(t, b, "other.test", "/lobby") == http.StatusServiceUnavailable
	})
	post(t, b, "/big", `{}`)
	waitFor(t, "the stream that does not read to close", func() bool {
		got := b.request(t, http.MethodHead, "other.test", "/lobby", nil)
		if got.status != http.StatusOK {
			return false
		}
		stream := open(t, b, "other.test", "/lobby")
		return stream.response.StatusCode == http.StatusOK
	})
}

// The host closing closes every open stream.
func TestTheHostClosesItsStreams(t *testing.T) {
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, sseFiles()), "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(read)
	t.Cleanup(server.Close)
	lobby := openOn(t, server.URL, server.Client(), "shop.test", "/lobby")
	plain := openOn(t, server.URL, server.Client(), "shop.test", "/plain") // no observer to wake it
	time.Sleep(100 * time.Millisecond)
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	for name, stream := range map[string]*events{"/lobby": lobby, "/plain": plain} {
		if got := stream.next(t); got != "" {
			t.Errorf("%s after the host closed: %q, want the stream closed", name, got)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/plain", nil)
	request.Host = "shop.test"
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a stream after the host closed: %d, want 503", response.StatusCode)
	}
}

// The host closing cuts a write that waits on a browser that does not read: the connection ends now, not once the
// browser reads or the write's time has passed.
func TestTheHostClosingCutsAWriteInProgress(t *testing.T) {
	read, err := host.ReadWithLog(filepath.Join(write(t, sseFiles()), "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	read.StreamWriteTimeout = time.Minute
	server := httptest.NewServer(read)
	t.Cleanup(server.Close)
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "GET /big HTTP/1.1\r\nHost: shop.test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/big", strings.NewReader("{}"))
	request.Host = "shop.test"
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	time.Sleep(300 * time.Millisecond)
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.Copy(io.Discard, connection); err != nil {
		t.Errorf("reading after the host closed: %v, want the connection ended", err)
	}
}

// An idle stream over HTTP/2 outlives StreamWriteTimeout: a write's deadline is cleared after it, since under HTTP/2
// a deadline is a timer that resets the stream when it fires.
func TestAnIdleStreamOutlivesItsWriteTimeoutOverHTTP2(t *testing.T) {
	read, err := host.ReadWithLog(filepath.Join(write(t, sseFiles()), "application.work"), &lines{})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	read.StreamWriteTimeout = 200 * time.Millisecond
	server := httptest.NewUnstartedServer(read)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = read.Close() })
	stream := openOn(t, server.URL, server.Client(), "shop.test", "/room")
	if stream.response.ProtoMajor != 2 {
		t.Fatalf("the stream is HTTP/%d.%d, want HTTP/2", stream.response.ProtoMajor, stream.response.ProtoMinor)
	}
	stream.next(t)
	stream.next(t)
	time.Sleep(600 * time.Millisecond)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/room", strings.NewReader(`{"room":"lobby","text":"idle"}`))
	request.Host = "shop.test"
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got, want := stream.next(t), "event: chat\ndata: {\"said\":\"idle\"}"; got != want {
		t.Errorf("after 600 ms idle: %q, want %q", got, want)
	}
}

// status is the status of GET path of hostname, its body closed unread: a stream that opens closes again.
func status(t *testing.T, b *booted, hostname, path string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, b.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = hostname
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// waitFor waits up to 5 s for done.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(until) {
			t.Fatalf("waited 5 s for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
