package host

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Static is the files a site serves as they are, with no VM (docs/KITWORK_UI.md mục 5): its assets/ at
// /assets/…, the engine's catalogue of root files (/robots.txt, /favicon.ico, …) and .well-known/. No
// other file of the site goes out, whatever its name: not router.work, not a .ui, not a .env. Built once
// at boot; requests only read it.
type Static struct {
	// folder is the site's folder. Every file is opened in it (os.OpenInRoot), so no name leaves it,
	// through .. or through a link that points out of it, and no handle stays open between requests.
	folder string
	// memory is the files held in memory of a site app.prewarm(…) names (prewarm.go), nil for one it does not: each
	// file is then read from the disk at each request, as it is read
	memory *keptFiles
	// local is the website's allowLocal: a file whose name carries no hash is asked again at every load.
	local bool
	// favicon is the file /favicon.ico answers with, the icon router.favicon(…) names at the site's root, from the
	// site's folder; "" for the site's own favicon.ico
	favicon string
}

// staticLifetime is how long a browser keeps a file whose name carries no hash before asking again: an
// hour, short enough that a file edited in place reaches every visitor within it; the ETag and
// Last-Modified make the question after it a 304 without the bytes (KITWORK_UI.md mục 5).
const staticLifetime = time.Hour

// rootFiles is the engine's catalogue of files a site serves from its root (KITWORK_UI.md mục 5).
var rootFiles = map[string]bool{
	"robots.txt": true, "llms.txt": true, "llms-full.txt": true, "sitemap.xml": true, "favicon.ico": true,
	"apple-touch-icon.png": true, "site.webmanifest": true, "ads.txt": true, "app-ads.txt": true, "humans.txt": true,
}

// Serve answers r with a file of the site when its path is one the site serves and the file is there,
// and reports whether it did; otherwise the site's routes answer r. Only GET and HEAD read a file.
func (s *Static) Serve(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	name, ok := staticName(r.URL.Path)
	if !ok {
		return false
	}
	if name == "favicon.ico" && s.favicon != "" {
		name = s.favicon // the icon the site's root links answers what browsers ask for unprompted
	}
	if s.memory != nil && s.memory.serve(w, r, s.folder, name) {
		return true
	}
	file, err := os.OpenInRoot(s.folder, filepath.FromSlash(name))
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false // a folder or a device: no file to send
	}
	kind, etag, cache := s.headers(name, file, info)
	header := w.Header()
	header.Set("Content-Type", kind)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("ETag", etag)
	header.Set("Cache-Control", cache)
	if s.memory != nil {
		if k := s.memory.keep(name, file, info, kind, etag, cache); k != nil {
			http.ServeContent(w, r, "", k.modified, bytes.NewReader(k.data))
			return true
		}
	}
	// ServeContent answers Range, If-None-Match, If-Modified-Since and HEAD, and copies the file to the
	// connection as it is read (sendfile where the system has it): a site not prewarmed holds no file in memory
	http.ServeContent(w, r, "", info.ModTime(), file)
	return true
}

// headers are what the answer of the file name says of it: its type; one ETag for the file as it is on disk, from its
// time of change and its size, by which and by Last-Modified http.ServeContent answers 304; how long a browser keeps it.
func (s *Static) headers(name string, file *os.File, info os.FileInfo) (kind, etag, cache string) {
	kind = staticType(name)
	etag = `W/"` + strconv.FormatInt(info.ModTime().UnixNano(), 16) + "-" + strconv.FormatInt(info.Size(), 16) + `"`
	switch {
	case s.carriesItsHash(name, file, info):
		cache = "public, max-age=31536000, immutable"
	case s.local:
		cache = "no-cache"
	default:
		cache = "public, max-age=" + strconv.Itoa(int(staticLifetime/time.Second))
	}
	return kind, etag, cache
}

// staticName is the file urlPath names in the site's folder, "/"-separated, when the path is one a site
// serves as a file: under /assets/ or /.well-known/, or a name of the catalogue at the root. A path the
// file system could read as another name is none, nor is a source of the site, whatever folder holds it:
//   - an empty, . or .. segment, a backslash or a colon (a stream of Windows, logo.svg::$DATA);
//   - a segment ending in a dot or a space, which Windows drops, so router.work. opens router.work;
//   - a short name of Windows, ROUTER~1.WOR, which opens router.work under a name with no .work;
//   - a dot segment, but .well-known first: .env, .shared, .database;
//   - a .work, a .ui or a *.kitwork.* file.
func staticName(urlPath string) (string, bool) {
	name, ok := strings.CutPrefix(urlPath, "/")
	// the allowlist first, so the path of a route pays this test and nothing more: no allocation
	if !ok || !strings.HasPrefix(name, "assets/") && !strings.HasPrefix(name, ".well-known/") && !rootFiles[name] {
		return "", false
	}
	if strings.ContainsAny(name, "\\:\x00") {
		return "", false
	}
	rest := name
	for first := true; ; first = false {
		segment, after, more := strings.Cut(rest, "/")
		if segment == "" || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || shortName(segment) {
			return "", false
		}
		if segment[0] == '.' && (!first || segment != ".well-known") {
			return "", false
		}
		if !more {
			break
		}
		rest = after
	}
	lower := strings.ToLower(name)
	if extension := path.Ext(lower); extension == ".work" || extension == ".ui" || strings.Contains(lower, ".kitwork.") {
		return "", false
	}
	return name, true
}

// shortName reports a segment written as a short name of Windows: a ~ before a digit.
func shortName(segment string) bool {
	for i := 0; i+1 < len(segment); i++ {
		if segment[i] == '~' && segment[i+1] >= '0' && segment[i+1] <= '9' {
			return true
		}
	}
	return false
}

// staticTypes are the types of the files a site serves most, written here rather than read from the
// system: a Windows registry can give .js as text/plain, which a browser with nosniff refuses to run.
var staticTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json",
	".txt": "text/plain; charset=utf-8", ".xml": "text/xml; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
	".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf",
	".wasm": "application/wasm", ".pdf": "application/pdf",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg",
}

// staticType is the Content-Type of the file name: the engine's own, the system's for an extension it
// has not, and bytes for none, never a type guessed from the content.
func staticType(name string) string {
	extension := strings.ToLower(path.Ext(name))
	if known, ok := staticTypes[extension]; ok {
		return known
	}
	if system := mime.TypeByExtension(extension); system != "" {
		return system
	}
	return "application/octet-stream"
}

// carriesItsHash reports whether the file's name carries the hash of its bytes, so a browser may keep it
// a year: a part of its name between dots, dashes or underscores (logo.3f2a9c1be04d77a1.svg,
// app-3f2a9c1b.js) is 8 to 64 lowercase hex digits, a letter among them, that begin the SHA-256 of the
// file. Checked against the bytes, never guessed from the spelling, since a year kept for a name that
// does not carry its hash keeps the old file after an edit. A part of digits only is a date or a
// counter (logo.20261014.svg) and is not read for it, so an upload so named is never hashed.
func (s *Static) carriesItsHash(name string, file *os.File, info os.FileInfo) bool {
	base := path.Base(name)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot] // the extension is never the hash
	}
	var candidates []string
	for _, part := range strings.FieldsFunc(base, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		if len(part) >= 8 && len(part) <= 64 && lowerHex(part) && strings.ContainsAny(part, "abcdef") {
			candidates = append(candidates, part)
		}
	}
	if len(candidates) == 0 {
		return false
	}
	key := s.folder + "\x00" + name + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	digest, ok := staticHashes.digest(key, func() (string, bool) {
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			return "", false
		}
		return hex.EncodeToString(sum.Sum(nil)), true
	})
	if !ok {
		return false
	}
	for _, candidate := range candidates {
		if strings.HasPrefix(digest, candidate) {
			return true
		}
	}
	return false
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
