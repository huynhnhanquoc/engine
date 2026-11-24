package design

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path"
	"strconv"
	"strings"
	"sync"
)

// The typefaces of a page (docs/KITWORK_DESIGN.md mục 2, design.fonts): self-hosted Google Fonts, already
// subset by Google, embedded in the binary and served by the engine under FontsPrefix. A visitor never
// contacts Google. A family engine2 does not carry stays in the page's font stack, by its name, and the
// system's font draws it until the engine carries it (the first engine downloads it from Google once).

//go:embed families
var fontFiles embed.FS

// FontsPrefix is where the faces are served, one file each, named by the face and its bytes:
// /_kitwork/fonts/outfit-100-900-latin.<16 hex>.woff2. The name changes with the bytes, so the file is
// kept by a browser for ever.
const FontsPrefix = "/_kitwork/fonts/"

// The typefaces of the two roles when design.fonts names none (Kit Design, mục 3).
const (
	DefaultBody = "Google Sans Flex"
	DefaultCode = "Google Sans Code"
)

// The system stacks after a role's typeface: what every platform has, while the face loads or when the
// engine does not carry it.
const (
	systemSans = "ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
	systemMono = "ui-monospace, SFMono-Regular, Menlo, monospace"
)

type family struct {
	slug, name string
	faces      []face
}

// face is one file of a family: the weights it draws and the characters it holds.
type face struct {
	lightest, heaviest int
	subset, file       string
	unicode            string
}

// Font is one face as the engine serves it.
type Font struct {
	Path   string // /_kitwork/fonts/<face>.<hash>.woff2
	Body   []byte
	Digest string // the whole SHA-256, hex: the ETag
}

var (
	fontsOnce sync.Once
	fontByURL map[string]*Font
	fontURL   map[string]string // the face's file → its URL
)

// fonts names every face by its bytes, once.
func fonts() {
	fontsOnce.Do(func() {
		fontByURL = map[string]*Font{}
		fontURL = map[string]string{}
		for _, f := range families {
			for _, each := range f.faces {
				body, err := fontFiles.ReadFile(each.file)
				if err != nil {
					continue
				}
				sum := sha256.Sum256(body)
				digest := hex.EncodeToString(sum[:])
				name := f.slug + "-" + strings.TrimSuffix(path.Base(each.file), ".woff2")
				url := FontsPrefix + name + "." + digest[:16] + ".woff2"
				fontByURL[url] = &Font{Path: url, Body: body, Digest: digest}
				fontURL[each.file] = url
			}
		}
	})
}

// FontAt is the face a request path names, exactly as a page writes it.
func FontAt(url string) (*Font, bool) {
	fonts()
	font, ok := fontByURL[url]
	return font, ok
}

// find is the family named: by its name in any case and spacing, or by its slug ("google sans code",
// "Google Sans Code", "google-sans-code").
func find(name string) (family, bool) {
	key := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(name, "-", " ")), " "))
	for _, f := range families {
		if strings.ToLower(f.name) == key || strings.ReplaceAll(f.slug, "-", " ") == key {
			return f, true
		}
	}
	return family{}, false
}

// Carried says whether the engine carries the family named.
func Carried(name string) bool {
	_, ok := find(name)
	return ok
}

// stack is a role's font-family: its typeface, written as the family spells it and quoted, then the
// system stack of the role.
func stack(role, name string) string {
	fallback := systemSans
	if role == "code" {
		fallback = systemMono
	}
	if f, ok := find(name); ok {
		name = f.name
	}
	if strings.ContainsAny(name, " ") {
		name = "'" + strings.ReplaceAll(name, "'", "") + "'"
	}
	return name + ", " + fallback
}

// fontsCSS is the page's typefaces: --font-body and --font-code, which the reboot and font="code" read, and
// the @font-face of each family the engine carries among the two, once each, in Google's order of the
// subsets. A browser fetches a face only when text on the page is set in it and holds its characters.
func fontsCSS(body, code string) string {
	fonts()
	var b strings.Builder
	b.WriteString(":root{--font-body:" + stack("body", body) + ";--font-code:" + stack("code", code) + "}\n")
	seen := map[string]bool{}
	for _, name := range []string{body, code} {
		f, ok := find(name)
		if !ok || seen[f.slug] {
			continue
		}
		seen[f.slug] = true
		for _, each := range f.faces {
			url, ok := fontURL[each.file]
			if !ok {
				continue
			}
			b.WriteString("@font-face{font-family:'" + f.name + "';font-style:normal;font-weight:" + strconv.Itoa(each.lightest))
			if each.heaviest != each.lightest {
				b.WriteString(" " + strconv.Itoa(each.heaviest))
			}
			b.WriteString(";font-display:swap;src:url(" + url + ") format('woff2');unicode-range:" + each.unicode + "}\n")
		}
	}
	return b.String()
}

// preload is the <link rel="preload"> of the face running text is drawn in first: the body family's latin
// face, when the engine carries it.
func preload(body string) string {
	fonts()
	f, ok := find(body)
	if !ok {
		return ""
	}
	for _, each := range f.faces {
		if each.subset == "latin" {
			if url, ok := fontURL[each.file]; ok {
				return `<link rel="preload" href="` + url + `" as="font" type="font/woff2" crossorigin>` + "\n"
			}
		}
	}
	return ""
}
