package host

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/ui"
)

// enginePrefix is the folder of what the engine itself serves to a page, the same on every site
// (KITWORK_UI mục 18): today the faces of the typefaces, /_kitwork/fonts/<face>.<hash>.woff2. No site's route
// or file is under it, so a path there the engine does not have is a 404, never a page of the site.
const enginePrefix = "/_kitwork/"

// engineFile answers a request under /_kitwork/ and says so; false for any other path. A face is named by
// its bytes, so it is kept for a year and never asked again.
func engineFile(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, enginePrefix) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		answer(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return true
	}
	if strings.HasSuffix(r.URL.Path, ".js") {
		script(w, r)
		return true
	}
	font, ok := design.FontAt(r.URL.Path)
	if !ok {
		answer(w, http.StatusNotFound, "Not Found")
		return true
	}
	header := w.Header()
	header.Set("Content-Type", "font/woff2")
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("X-Content-Type-Options", "nosniff")
	// a face is fetched by a page of any origin the site serves (the font's CORS mode): it holds nothing
	// of the site
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("ETag", `"`+font.Digest+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(font.Body))
	return true
}

// script answers a file of JavaScript the engine gives a page (library/ui FileAt): the kernel, Drive, the scrolled
// flag, a component. Today's name is kept for a year; an older name of the same file, from a page written before
// a change and kept somewhere, gets today's file, not to be kept, so the page keeps its JavaScript.
func script(w http.ResponseWriter, r *http.Request) {
	file, exact := ui.FileAt(r.URL.Path)
	if file == nil {
		answer(w, http.StatusNotFound, "Not Found")
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/javascript; charset=utf-8")
	header.Set("X-Content-Type-Options", "nosniff")
	if exact {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
		header.Set("ETag", `"`+file.Digest+`"`)
	} else {
		header.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file.Body))
}
