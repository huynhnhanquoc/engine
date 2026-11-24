// Package kernel is the browser's half of a .ui page (KITWORK_UI mục 11): the kernel that reads the grouped
// data-kitwork-* attributes the server writes, runs the components and keeps what follows their state up to
// date, and Drive, which moves between pages without a reload. Its JavaScript was moved whole from the first
// engine's jit/hydrate on 06/11/2026 (kernel.js, its storage and web modules, boot.js; morph.js and drive.js),
// as ui/stylesheet and jit/highlight were: the server half is engine2's own, the wire between them the same.
//
// What the engine gives a page is served under one prefix, /_kitwork/, each file named by its bytes,
// <name>.<hash>.js: the same file on every page of every site, kept by a browser for a year, and a changed byte
// is a new name, never an old file under it.
package kernel

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"sync"
)

//go:embed *.js
var files embed.FS

// Prefix is where the engine serves what it gives a page, the same on every site; no site's route is under it.
const Prefix = "/_kitwork/"

// hashLength is how much of the SHA-256 a name carries: 64 bits tell two versions of a file apart.
const hashLength = 16

// File is one file the engine gives a page.
type File struct {
	// Path is its URL, /_kitwork/<name>.<hash>.js
	Path string
	// Role is what it is to the page, the value of data-kitwork-generated on its <script>: runtime, drive,
	// bridge, directive, component, scrolled
	Role string
	Body []byte
	// Digest is the whole SHA-256 of Body, hex: the ETag
	Digest string
}

// New is the file of role named name (components/tabs) holding source, at name.<hash>.js under the prefix.
func New(role, name, source string) *File {
	body := []byte(source)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	return &File{Path: Prefix + name + "." + digest[:hashLength] + ".js", Role: role, Body: body, Digest: digest}
}

// Named says whether path is a name of f: today's, or one an older engine gave the same file (its name, a
// hash of other bytes). exact says it is today's. A page outlives the engine that wrote it (a cache, a tab left
// open across a deploy), so the old name still gets the file, though not to be kept.
func (f *File) Named(path string) (named, exact bool) {
	if path == f.Path {
		return true, true
	}
	stem := strings.TrimSuffix(f.Path, ".js")
	stem = stem[:strings.LastIndexByte(stem, '.')+1]
	rest, ok := strings.CutPrefix(path, stem)
	if !ok {
		return false, false
	}
	hash, ok := strings.CutSuffix(rest, ".js")
	if !ok || len(hash) != hashLength {
		return false, false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false, false
		}
	}
	return true, false
}

// read is the source of an embedded file.
func read(names ...string) string {
	parts := make([]string, len(names))
	for i, name := range names {
		body, err := files.ReadFile(name)
		if err != nil {
			panic("kernel: " + name + " is not embedded")
		}
		parts[i] = string(body)
	}
	return strings.Join(parts, "\n")
}

// Kernel is the kernel as one file, the first a page loads: the kernel, its storage and web modules, and boot,
// which starts it once every deferred file after it (Drive, each component) has registered.
var Kernel = sync.OnceValue(func() *File {
	return New("runtime", "kernel", read("kernel.js", "storage.js", "web.js", "boot.js"))
})

// Drive is Drive and the morph it drives, as one file: only a site that moves between pages without a reload
// loads it (router.reload(false)).
var Drive = sync.OnceValue(func() *File {
	return New("drive", "drive", read("morph.js", "drive.js"))
})

// Call is the browser's half of the bridge of router.view (call.js): only a page that runs in the browser, whose
// router.view binds a work or a var, loads it (docs/view.md mục 4).
var Call = sync.OnceValue(func() *File {
	return New("bridge", "call", read("call.js"))
})

// Intersect is the eyes of intersect-enter and intersect-leave (intersect.js): an IntersectionObserver for each
// pair, sending the pair's own event on its element, which the kernel runs. Only a page that writes one loads it, at
// /_kitwork/directives/intersect.<hash>.js (KITWORK_UI, the files of the client: a heavy event, a file of its own).
var Intersect = sync.OnceValue(func() *File {
	return New("directive", "directives/intersect", read("intersect.js"))
})
