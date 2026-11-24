package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// context.cookie(name) reads the cookie the browser sent, "" for one it did not (16/11/2026, the owner: "không cần
// context.request.cookie(), nhanh bằng context.cookie luôn"); context.cookie(name, value) still sets one; and the old
// spelling is an error saying the new one. CONTROL: one argument sent to the writer again, red.
func TestAContextReadsACookieByItsName(t *testing.T) {
	site := "apps/shop/shop.test/"
	b := boot(t, filepath.Join(write(t, map[string]string{
		"application.work":       manifest,
		site + "router.work":     routerImport + "router.get((context) => context.cookie(\"seen\", \"1\").text(\"[\" + context.cookie(\"who\") + \"]\"));\n",
		site + "old/router.work": routerImport + "router.get((context) => context.text(safe(() => context.request.cookie(\"who\")).error?.message));\n",
	}), "application.work"))
	got := b.request(t, http.MethodGet, "shop.test", "/", nil, "Cookie", "who=quoc")
	if got.body != "[quoc]" || got.header.Get("Set-Cookie") != "seen=1; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("GET / with who=quoc: %q, Set-Cookie %q", got.body, got.header.Get("Set-Cookie"))
	}
	if got := b.get(t, "shop.test", "/"); got.body != "[]" {
		t.Errorf("GET / with no cookie: %q, want []", got.body)
	}
	if got := b.get(t, "shop.test", "/old"); !strings.Contains(got.body, "context.request.cookie(name) is context.cookie(name) now") {
		t.Errorf("GET /old: %q", got.body)
	}
}
