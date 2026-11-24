package host_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
	"github.com/kitwork/engine2/internal/loopback"
)

// heldUp is an upstream whose /hold answers once release is closed, and whose /slow answers after 200 ms; at counts
// the requests it holds now, most the most it held at once.
type heldUp struct {
	server  *httptest.Server
	release chan struct{}
	at      atomic.Int64
	most    atomic.Int64
}

func newHeldUp(t *testing.T) *heldUp {
	t.Helper()
	up := &heldUp{release: make(chan struct{})}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := up.at.Add(1)
		defer up.at.Add(-1)
		for {
			most := up.most.Load()
			if now <= most || up.most.CompareAndSwap(most, now) {
				break
			}
		}
		if r.URL.Path == "/hold" {
			<-up.release
		} else {
			time.Sleep(200 * time.Millisecond)
		}
		w.Write([]byte("up"))
	}))
	t.Cleanup(up.server.Close)
	t.Cleanup(loopback.Open())
	t.Cleanup(func() {
		select {
		case <-up.release:
		default:
			close(up.release)
		}
	})
	return up
}

// gated boots files with a site running at most requests at once, a request waiting wait for a place.
func gated(t *testing.T, files map[string]string, requests int, wait time.Duration) *booted {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, files), "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	read.SiteRequests, read.SiteWait = requests, wait
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	return serve(t, read, log)
}

// A site runs so many requests at once (12/11/2026, the second slice of the tenants' limits): two requests held by a
// slow upstream take a site of two places, a third waits its 200 ms and is answered 503 with Retry-After, its line in
// the log; a file of assets/ takes no place, another site has places of its own; once the two end, the site answers.
func TestASiteRunsSoManyRequestsAtOnce(t *testing.T) {
	up := newHeldUp(t)
	http := "import * as http from \"kitwork/http\";\n"
	b := gated(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/held/router.work":  http + routerImport + `router.get((context) => context.text(http.get("` + up.server.URL + `/hold").send().text));` + "\n",
		"apps/shop/shop.test/quick/router.work": routerImport + `router.get((context) => context.text("quick"));` + "\n",
		"apps/shop/shop.test/assets/a.txt":      "a file",
		"apps/shop/kho.test/quick/router.work":  routerImport + `router.get((context) => context.text("kho"));` + "\n",
	}, 2, 200*time.Millisecond)

	var held sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		held.Add(1)
		go func() {
			defer held.Done()
			statuses <- b.get(t, "shop.test", "/held").status
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for up.at.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if up.at.Load() != 2 {
		t.Fatalf("the upstream holds %d requests, want 2", up.at.Load())
	}

	began := time.Now()
	got := b.get(t, "shop.test", "/quick")
	waited := time.Since(began)
	if got.status != 503 || got.header.Get("Retry-After") != "1" {
		t.Errorf("a third request: %d %q, Retry-After %q, want 503 and 1", got.status, got.body, got.header.Get("Retry-After"))
	}
	if waited < 150*time.Millisecond {
		t.Errorf("a third request answered after %v, want it to wait its 200 ms for a place", waited)
	}
	if got := b.get(t, "shop.test", "/assets/a.txt"); got.status != 200 || got.body != "a file" {
		t.Errorf("a file of assets/ while the site is full: %d %q, want it served", got.status, got.body)
	}
	if got := b.get(t, "kho.test", "/quick"); got.status != 200 || got.body != "kho" {
		t.Errorf("another site while shop.test is full: %d %q, want 200", got.status, got.body)
	}
	contains(t, "the log", b.log.String(), "[shop.test] busy: 1 request(s) answered 503", "for one of the 2 the site runs at once")

	close(up.release)
	held.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 {
			t.Errorf("a held request: %d, want 200", status)
		}
	}
	if got := b.get(t, "shop.test", "/quick"); got.status != 200 || got.body != "quick" {
		t.Errorf("once the two ended: %d %q, want 200", got.status, got.body)
	}
}

// A request waiting for a place takes the first one given back, not its whole wait: a site of one place, one request
// held by the upstream, a second waits; the first lets go and the second is answered.
func TestARequestWaitingForAPlaceTakesTheFirstFreed(t *testing.T) {
	up := newHeldUp(t)
	http := "import * as http from \"kitwork/http\";\n"
	b := gated(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/held/router.work":  http + routerImport + `router.get((context) => context.text(http.get("` + up.server.URL + `/hold").send().text));` + "\n",
		"apps/shop/shop.test/quick/router.work": routerImport + `router.get((context) => context.text("quick"));` + "\n",
	}, 1, 5*time.Second)
	first := make(chan int, 1)
	go func() { first <- b.get(t, "shop.test", "/held").status }()
	deadline := time.Now().Add(5 * time.Second)
	for up.at.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second := make(chan reply, 1)
	go func() { second <- b.get(t, "shop.test", "/quick") }()
	time.Sleep(100 * time.Millisecond)
	select {
	case got := <-second:
		t.Fatalf("the second request was answered %d while the site's one place was held", got.status)
	default:
	}
	began := time.Now()
	close(up.release)
	select {
	case got := <-second:
		if got.status != 200 || time.Since(began) > time.Second {
			t.Errorf("the second request: %d after %v, want 200 soon after the first let go", got.status, time.Since(began))
		}
	case <-time.After(4 * time.Second):
		t.Fatal("the second request was not answered once the place was free")
	}
	if status := <-first; status != 200 {
		t.Errorf("the first request: %d", status)
	}
}

// A stream gives its place back as it opens: it counts against the streams' own ceiling, so a site of one place with a
// stream open still answers its requests.
func TestAStreamGivesItsPlaceBack(t *testing.T) {
	b := gated(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/lobby/router.work": sseImport + `router.get((context) => context.sse.subscribe(memory("rooms").segment("lobby")));` + "\n",
		"apps/shop/shop.test/quick/router.work": routerImport + `router.get((context) => context.text("quick"));` + "\n",
	}, 1, 100*time.Millisecond)
	open(t, b, "shop.test", "/lobby")
	for i := 0; i < 3; i++ {
		if got := b.get(t, "shop.test", "/quick"); got.status != 200 {
			t.Fatalf("a request while a stream is open on a site of one place: %d, want 200", got.status)
		}
	}
}

// A site runs as many routines of go() at once as requests: four tasks of 200 ms on a site of two run two at a time.
func TestASitesRoutinesRunAsManyAsItsRequests(t *testing.T) {
	up := newHeldUp(t)
	b := gated(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/fan/router.work": routineImport + `router.get((context) => {
  const tasks = [];
  for (let i = 0; i < 4; i++) {
    tasks.push(routine.go(() => http.get("` + up.server.URL + `/slow").send().text));
  }
  return context.json(routine.wait(tasks).map((result) => result.data));
});
`,
	}, 2, time.Second)
	got := b.get(t, "shop.test", "/fan")
	if got.status != 200 || !strings.Contains(got.body, `"up","up","up","up"`) {
		t.Fatalf("GET /fan: %d %q", got.status, got.body)
	}
	if most := up.most.Load(); most != 2 {
		t.Errorf("the upstream held %d of the site's tasks at once, want 2", most)
	}
}
