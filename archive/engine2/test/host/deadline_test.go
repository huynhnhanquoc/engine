package host_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kitwork/engine2/host"
)

// A run past its time stops within a few turns of a loop whatever one turn costs: the deadline reads the
// clock at every Err the VM makes, every 64 instructions (05/11/2026). It read the clock at every
// sixteenth, a thousand instructions, so a loop whose instruction is a reverse or a sort of a large array
// ran on for a hundred turns, or 12 s past a 300 ms timeout. The overrun is counted in turns, measured on
// the same machine with the same loop, so the test holds on a slow or a busy one.
func TestARequestStopsWithinAFewTurnsOfItsTime(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => {
  const items = "x".repeat(100000).split("");
  const turns = "x".repeat(Number(context.query("turns"))).split("");
  for (const turn of turns) { items.reverse(); }
  return context.text("done");
});
`,
	})
	read, err := host.ReadWithLog(filepath.Join(folder, "application.work"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	read.RunEnergy = longEnergy // a reverse costs its items (core/runtime, work.go): the default ends the loop first
	if err := read.Boot(); err != nil {
		t.Fatal(err)
	}
	serve := func(turns string) (int, time.Duration) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/?turns="+turns, nil)
		request.Host = "shop.test"
		started := time.Now()
		read.ServeHTTP(recorder, request)
		return recorder.Code, time.Since(started)
	}
	const calibration = 400
	_, none := serve("0")
	status, measured := serve("400")
	if status != 200 {
		t.Fatalf("GET of %d turns: %d", calibration, status)
	}
	turn := (measured - none) / calibration
	if turn <= 0 {
		t.Skipf("a turn measured %v", turn)
	}
	timeout := 50*turn + 20*time.Millisecond
	read.RequestTimeout = timeout
	// where in the clock's window the time passes is chance, so the median of nine runs is what is held:
	// a turn of this loop is about eight instructions, so a thousand are some hundred and twenty turns,
	// sixty-four about eight
	overruns := make([]float64, 0, 9)
	for range 9 {
		status, took := serve("20000")
		if status != 500 {
			t.Fatalf("a request past its time answered %d after %v", status, took)
		}
		overruns = append(overruns, float64(took-timeout)/float64(turn))
	}
	slices.Sort(overruns)
	median := overruns[len(overruns)/2]
	t.Logf("a turn %v, timeout %v: %.0f turns past it (median), %.0f to %.0f", turn, timeout, median, overruns[0], overruns[len(overruns)-1])
	if median > 25 {
		t.Errorf("a request past its time ran %.0f turns of %v past %v (median of nine); want a few", median, turn, timeout)
	}
}
