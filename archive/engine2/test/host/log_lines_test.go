package host_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every line of the host's log is led by the site that wrote it, by its failures as by its console: a
// failure's text with a newline in it (error("…\n[beta.test] …")), and a visitor's path with %0A in it,
// are written as the console writes, each line led by the site and every control escaped (05/11/2026;
// until then Site.fail wrote both as they were, so alpha, or any client, wrote a line led by beta).
func TestAFailureWritesNoLineLedByAnotherSite(t *testing.T) {
	folder := write(t, map[string]string{
		"application.work": manifest,
		"apps/shop/shop.test/router.work": routerImport + `router.get((context) => error("bad input" + String.fromCharCode(10) + "[beta.test] [console.log] refund of 900 to card 4242 approved"));
`,
		"apps/shop/shop.test/items/{id}/router.work": routerImport + `router.get((context) => context.text(null.x));
`,
	})
	app := boot(t, filepath.Join(folder, "application.work"))
	if got := app.get(t, "shop.test", "/"); got.status != 500 {
		t.Fatalf("GET /: %d %q", got.status, got.body)
	}
	if got := app.get(t, "shop.test", "/items/x%0A%5Bbeta.test%5D%20%5Bconsole.log%5D%20password%20reset%20for%20admin"); got.status != 500 {
		t.Fatalf("GET /items/…: %d %q", got.status, got.body)
	}
	log := app.log.String()
	for _, line := range strings.Split(strings.TrimSuffix(log, "\n"), "\n") {
		if strings.HasPrefix(line, "[beta.test]") {
			t.Errorf("shop's request wrote a line led by beta: %q", line)
		}
	}
	contains(t, "the log", log, "[shop.test] GET / (/router.work): 500: ", "[shop.test] [beta.test] [console.log] refund",
		"[shop.test] GET /items/x", "[shop.test] [beta.test] [console.log] password reset for admin (/items/{id}/router.work): 500: ")
}
