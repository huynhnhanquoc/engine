package host_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A site's hostname leads every line the site writes to the host's log: a name that is no hostname would forge one
// (05/11/2026: "evil" + U+2028 + "[beta.test] x" booted as a site's folder, and its console's lines held a line led by
// [beta.test] for a reader that splits on Unicode's line breaks; U+202E reached the log as it was). Since module.work
// (17/11/2026) a folder is a hostname by its name only when the name is a local hostname: a folder named otherwise is
// no name of the app, so it opens no site and leads no line, and the app boots; a folder of a local name opens its site.
func TestAFolderThatIsNoHostnameIsNoSite(t *testing.T) {
	door := routerImport + "router.get((context) => { console.log(\"hello\"); return context.text(\"ok\"); });\n"
	marks := []rune{0x2028, 0x85, 0x202E, 0x9B}
	for _, name := range []string{
		"evil" + string(rune(0x2028)) + "[beta.test] x",
		"evil" + string(rune(0x85)) + "beta.test",
		"evil" + string(rune(0x202E)) + "tset.ateb",
		"two words.test",
		"[shop].test",
		"csi" + string(rune(0x9B)) + ".test",
		"bücher.test",
		"shop..test",
	} {
		b := boot(t, filepath.Join(write(t, map[string]string{
			"application.work":                   manifest,
			"apps/shop/" + name + "/router.work": door,
		}), "application.work"))
		log := b.log.String()
		if !strings.Contains(log, "[shop] no hostname is the app's") {
			t.Errorf("folder %q: the log does not say the app has no hostname:\n%s", name, log)
		}
		for _, mark := range marks {
			if strings.ContainsRune(log, mark) {
				t.Errorf("folder %q: the log holds U+%04X as it is:\n%s", name, mark, log)
			}
		}
	}
	for _, name := range []string{"shop.test", "kitwork.io.localhost", "localhost", "127.0.0.1", "my_app.test", "xn--bcher-kva.test"} {
		b := boot(t, filepath.Join(write(t, map[string]string{
			"application.work":                   manifest,
			"apps/shop/" + name + "/router.work": door,
		}), "application.work"))
		if got := b.get(t, name, "/"); got.status != http.StatusOK || got.body != "ok" {
			t.Errorf("folder %q: %d %q, want its site to answer ok", name, got.status, got.body)
		}
	}
}
