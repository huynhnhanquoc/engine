package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// A failure's line in the log keeps its reason however long the visitor's path: what a visitor wrote (the
// path, the method) is cut at 2 KiB with the count of the rest before the line is written, so the line's
// own cut at 64 KiB never reaches the reason (05/11/2026: a path of 70,000 bytes pushed the reason, and the
// router.work it came from, out of the line).
func TestAFailuresLineKeepsItsReasonPastALongPath(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/x/{...rest}/router.work": routerImport + `router.get((context) => error("plain failure reason"));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/x/"+strings.Repeat("a", 70_000)); got.status != 500 {
		t.Fatalf("GET /x/a…: %d %q", got.status, got.body)
	}
	log := app.log.String()
	contains(t, "the log", log, "[shop.test] GET /x/aaaa", " bytes more) (/x/{...rest}/router.work): 500: ", "plain failure reason")
	if len(log) > 8<<10 {
		t.Errorf("the failure's line is %d bytes, want the path cut", len(log))
	}
}
