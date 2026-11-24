package host_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// gunzip is body as it was before gzip, failing the test when it is no gzip stream.
func gunzip(t *testing.T, body string) string {
	t.Helper()
	reader, err := gzip.NewReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("not a gzip stream: %v", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("a broken gzip stream: %v", err)
	}
	return string(plain)
}

// A browser that takes gzip gets a page, a script, a stylesheet, a JSON answer or an SVG gzipped, the same bytes once
// opened, announced by Content-Encoding and Vary; a strong ETag becomes weak, and asking again with it is a 304. Left
// as written: an answer under a kilobyte, an image, a part of a file (Range, 206), a HEAD, and every answer for a
// client that does not take gzip or refuses it (q=0).
func TestAnAnswerIsGzippedForABrowserThatTakesIt(t *testing.T) {
	site := "apps/shop/shop.test/"
	big := strings.Repeat("Trà và cà phê. ", 200)
	svg := `<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat(`<circle r="1"/>`, 100) + `</svg>`
	folder := write(t, map[string]string{
		"application.work":         manifest,
		site + "content.ui":        "<content><text>" + big + "</text></content>\n",
		site + "small/router.work": routerImport + "router.get((context) => context.text(\"a\"));\n",
		site + "json/router.work":  routerImport + "router.get((context) => context.json({ words: \"" + big + "\" }));\n",
		site + "assets/notes.txt":  big,
		site + "assets/logo.svg":   svg,
		site + "assets/photo.png":  "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 4096),
	})
	b := boot(t, filepath.Join(folder, "application.work"))
	gzipped := func(path string, headers ...string) reply {
		return b.request(t, http.MethodGet, "shop.test", path, nil, append([]string{"Accept-Encoding", "gzip, deflate, br"}, headers...)...)
	}

	// Go's client asks for gzip itself and opens it unless told: identity is a client that does not take gzip
	identity := func(path string) reply {
		return b.request(t, http.MethodGet, "shop.test", path, nil, "Accept-Encoding", "identity")
	}
	page := identity("/")
	if page.header.Get("Content-Encoding") != "" || page.header.Get("Vary") != "" {
		t.Errorf("a client that does not take gzip: Content-Encoding %q, Vary %q", page.header.Get("Content-Encoding"), page.header.Get("Vary"))
	}
	kernel := regexp.MustCompile(`/_kitwork/kernel\.[0-9a-f]{16}\.js`).FindString(page.body)
	if kernel == "" {
		t.Fatalf("the page loads no kernel:\n%s", page.body)
	}
	for _, path := range []string{"/", "/json", "/assets/notes.txt", "/assets/logo.svg", kernel} {
		plain := identity(path)
		got := gzipped(path)
		if got.status != http.StatusOK || got.header.Get("Content-Encoding") != "gzip" || got.header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("GET %s: %d, Content-Encoding %q, Vary %q: want it gzipped", path, got.status, got.header.Get("Content-Encoding"), got.header.Get("Vary"))
			continue
		}
		if opened := gunzip(t, got.body); opened != plain.body {
			t.Errorf("GET %s: opened, the gzipped body is not the body (%d bytes, want %d)", path, len(opened), len(plain.body))
		}
		if len(got.body) >= len(plain.body) {
			t.Errorf("GET %s: gzipped %d bytes, the body %d", path, len(got.body), len(plain.body))
		}
		if length := got.header.Get("Content-Length"); length != "" && length != strconv.Itoa(len(got.body)) {
			t.Errorf("GET %s: Content-Length %s of a %d-byte body", path, length, len(got.body))
		}
	}
	script := gzipped(kernel)
	etag := script.header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Errorf("the gzipped kernel's ETag %q, want weak", etag)
	}
	if again := gzipped(kernel, "If-None-Match", etag); again.status != http.StatusNotModified {
		t.Errorf("asked again with its ETag: %d, want 304", again.status)
	}
	for _, c := range []struct {
		path    string
		headers []string
	}{
		{"/small", nil},
		{"/assets/photo.png", nil},
		{"/assets/notes.txt", []string{"Range", "bytes=0-1499"}},
		{"/", []string{"Accept-Encoding", "gzip;q=0"}},
	} {
		// the headers are set in order: an Accept-Encoding of the case is the one sent
		got := b.request(t, http.MethodGet, "shop.test", c.path, nil, append([]string{"Accept-Encoding", "gzip"}, c.headers...)...)
		if got.header.Get("Content-Encoding") != "" || bytes.HasPrefix([]byte(got.body), []byte{0x1f, 0x8b}) {
			t.Errorf("GET %s %v: %d gzipped, want it as written", c.path, c.headers, got.status)
		}
	}
	if small := gzipped("/small"); small.body != "a" {
		t.Errorf("GET /small: %q, want the answer as written", small.body)
	}
	if part := b.request(t, http.MethodGet, "shop.test", "/assets/notes.txt", nil, "Accept-Encoding", "gzip", "Range", "bytes=0-1499"); part.status != http.StatusPartialContent || len(part.body) != 1500 {
		t.Errorf("a part of a file: %d, %d bytes, want 206 and 1500", part.status, len(part.body))
	}
	if head := b.request(t, http.MethodHead, "shop.test", "/", nil, "Accept-Encoding", "gzip"); head.status != http.StatusOK || head.body != "" {
		t.Errorf("HEAD /: %d %q", head.status, head.body)
	}
}
