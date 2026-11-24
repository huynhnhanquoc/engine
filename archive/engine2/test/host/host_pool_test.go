package host_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// pooled boots files with a host whose runs make HostBytes together at most (runtime.Pool).
func pooled(t *testing.T, files map[string]string, bytes uint64) *booted {
	t.Helper()
	log := &lines{}
	read, err := host.ReadWithLog(filepath.Join(write(t, files), "application.work"), log)
	if err != nil {
		t.Fatal(err)
	}
	read.HostBytes = bytes
	if err := read.Boot(); err != nil {
		t.Fatalf("boot: %v\n%s", err, log)
	}
	return serve(t, read, log)
}

// What every run of the host makes together is bounded (12/11/2026, the third slice of the tenants' limits): a
// request of one site holding 5 MB while an upstream holds it, a request of another site making 5 MB more of a host of
// 8 MiB is answered 503 with Retry-After, its line in the log; once the first ends, the second is answered.
func TestARequestPastWhatTheHostGivesIs503(t *testing.T) {
	up := newHeldUp(t)
	b := pooled(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/hold/router.work": "import * as http from \"kitwork/http\";\n" + routerImport + `router.get((context) => {
  const held = [];
  for (let i = 0; i < 5000; i++) { held.push("x".repeat(1000) + i); }
  const answer = http.get("` + up.server.URL + `/hold").send().text;
  return context.text(answer + held.length);
});
`,
		"apps/shop/kho.test/make/router.work": routerImport + `router.get((context) => {
  const held = [];
  for (let i = 0; i < 5000; i++) { held.push("y".repeat(1000) + i); }
  return context.text("" + held.length);
});
`,
	}, 8<<20)
	first := make(chan reply, 1)
	go func() { first <- b.get(t, "shop.test", "/hold") }()
	deadline := time.Now().Add(5 * time.Second)
	for up.at.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if up.at.Load() != 1 {
		t.Fatal("the first request did not reach the upstream")
	}
	got := b.get(t, "kho.test", "/make")
	if got.status != 503 || got.header.Get("Retry-After") != "1" {
		t.Errorf("a request past what the host gives: %d %q, Retry-After %q, want 503 and 1", got.status, got.body, got.header.Get("Retry-After"))
	}
	contains(t, "the log", b.log.String(), "[kho.test] GET /make (/make/router.work): 503:", "Host Memory Limit Exceeded")
	close(up.release)
	if got := <-first; got.status != 200 || got.body != "up5000" {
		t.Errorf("the first request: %d %q, want 200 up5000", got.status, got.body)
	}
	if got := b.get(t, "kho.test", "/make"); got.status != 200 || got.body != "5000" {
		t.Errorf("the second request once the first ended: %d %q, want 200", got.status, got.body)
	}
}

// Without HostBytes no pool is made: the same two requests both run.
func TestAHostWithoutACeilingRunsBoth(t *testing.T) {
	up := newHeldUp(t)
	b := pooled(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/hold/router.work": "import * as http from \"kitwork/http\";\n" + routerImport + `router.get((context) => {
  const held = [];
  for (let i = 0; i < 5000; i++) { held.push("x".repeat(1000) + i); }
  return context.text(http.get("` + up.server.URL + `/hold").send().text + held.length);
});
`,
		"apps/shop/kho.test/make/router.work": routerImport + `router.get((context) => {
  const held = [];
  for (let i = 0; i < 5000; i++) { held.push("y".repeat(1000) + i); }
  return context.text("" + held.length);
});
`,
	}, 0)
	first := make(chan reply, 1)
	go func() { first <- b.get(t, "shop.test", "/hold") }()
	deadline := time.Now().Add(5 * time.Second)
	for up.at.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := b.get(t, "kho.test", "/make"); got.status != 200 {
		t.Errorf("a host without a ceiling: %d %q, want 200", got.status, got.body)
	}
	close(up.release)
	<-first
}
