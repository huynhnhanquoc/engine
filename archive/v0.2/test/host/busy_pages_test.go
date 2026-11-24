package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A page the host cannot give to is a 503, not a 500 (12/11/2026, after the tenants' limits): an expression of a page
// making 3 MB on a host of 2 MiB stops for the host's pool, on a page of content.ui alone and on one of router.view,
// and a work of a page that would make as much answers the bridge's 503; each with Retry-After and a line of the log.
func TestAPageTheHostCannotGiveToIs503(t *testing.T) {
	heavy := "<content><text>{{ \"x\".repeat(3000000).length }}</text></content>\n"
	b := pooled(t, map[string]string{
		"application.work":                      manifest,
		"apps/shop/shop.test/plain/content.ui":  heavy,
		"apps/shop/shop.test/bound/router.work": bridgeImport + "router.view((context) => context.bind({ n: 1 }));\n",
		"apps/shop/shop.test/bound/content.ui":  heavy,
		"apps/shop/shop.test/light/content.ui":  "<content><text>{{ \"x\".repeat(10).length }}</text></content>\n",
		"apps/shop/shop.test/grow/content.ui":   "<content><text>{{ n }}</text></content>\n",
		"apps/shop/shop.test/grow/router.work": bridgeImport + `router.view((context) => {
  var n = 0;
  const grow = () => {
    const held = [];
    for (let i = 0; i < 3000; i++) { held.push("y".repeat(1000) + i); }
    n = held.length;
  };
  return context.bind({ n, grow });
});
`,
	}, 2<<20)
	for _, path := range []string{"/plain", "/bound"} {
		got := b.get(t, "shop.test", path)
		if got.status != http.StatusServiceUnavailable || got.header.Get("Retry-After") != "1" {
			t.Errorf("GET %s, an expression past the host's pool: %d %q, Retry-After %q, want 503 and 1", path, got.status, got.body, got.header.Get("Retry-After"))
		}
		contains(t, "the log of "+path, b.log.String(), "GET "+path+" ("+path+"/content.ui): 503:")
	}
	if got := b.get(t, "shop.test", "/light"); got.status != 200 || !strings.Contains(got.body, ">10<") {
		t.Errorf("a page that makes little on the same host: %d", got.status)
	}
	got := b.call(t, "/grow", "grow", `{"seq":1,"args":[],"page":{"n":0}}`)
	if got.status != http.StatusServiceUnavailable || got.header.Get("Retry-After") != "1" || !strings.Contains(got.body, `"code":503`) {
		t.Errorf("a work past the host's pool: %d %q, Retry-After %q, want the bridge's 503", got.status, got.body, got.header.Get("Retry-After"))
	}
	contains(t, "the log of the work", b.log.String(), "work grow): 503:", "Host Memory Limit Exceeded")
}

// A page's expressions make no more than its site's runs (its tenant's row): on a site of 2 MB an expression making 3 MB
// is the page's 500, not the host's 503; a site of the default draws it.
func TestAPagesExpressionsRunWithinItsSitesMemory(t *testing.T) {
	heavy := "<content><text>{{ \"x\".repeat(3000000).length }}</text></content>\n"
	folder := write(t, map[string]string{
		"application.work":                    manifest,
		"apps/kho/kho.test/page/content.ui":   heavy,
		"apps/shop/shop.test/page/content.ui": heavy,
	})
	writeTenants(t, folder, tenantRow{"kho", "active", "2mb", "", ""}) // the app kho's runs make 2 MB (the table tenant)
	b := boot(t, filepath.Join(folder, "application.work"))
	if got := b.get(t, "kho.test", "/page"); got.status != http.StatusInternalServerError {
		t.Errorf("3 MB in an expression on a site of 2 MB: %d, want 500", got.status)
	}
	contains(t, "the log", b.log.String(), "[kho.test] GET /page (/page/content.ui): 500:", "Memory Limit Exceeded")
	if got := b.get(t, "shop.test", "/page"); got.status != 200 || !strings.Contains(got.body, ">3000000<") {
		t.Errorf("3 MB in an expression on a site of the default: %d", got.status)
	}
}
