package ui

import (
	"html"
	"strings"
)

// A link goes to a page of the site, a web page, an e-mail or a phone number (KITWORK_UI mục 7, "Link ra
// ngoài"), and says when it is the page being viewed. The rules are the first engine's (ui/render.go,
// utilities/richtext/link.go), moved with the kernel on 06/11/2026, since Drive marks the links it keeps again.

// linkAttributes are the attributes of a link to href: the address; target="_blank" for one opened in a new tab
// (new-tab="true"); rel="external noopener" for one that leaves the site, rel="noopener" for one of the site in a
// new tab; aria-current for one to the page being viewed ("page") or to a folder it stands in ("true"; "/" and an
// exact link only for themselves); and, outside the route's content (a header's, a footer's, which Drive keeps
// when it swaps the content), data-kitwork-current, how Drive marks it again after a move.
func (r *renderer) linkAttributes(href string, exact, newTab bool) []string {
	out := []string{`href="` + html.EscapeString(href) + `"`}
	if newTab {
		out = append(out, `target="_blank"`)
	}
	switch {
	case externalLink(href, r.head.Origin):
		out = append(out, `rel="external noopener"`)
	case newTab:
		out = append(out, `rel="noopener"`)
	}
	if current := linkCurrent(r.path(), href, exact); current != "" {
		out = append(out, `aria-current="`+current+`"`)
	}
	if r.inContent == 0 && internalLink(href) {
		mode := ""
		if exact {
			mode = "exact"
		}
		out = append(out, `data-kitwork-current="`+mode+`"`)
	}
	return out
}

// linkCurrent is what a link to href says about the page viewed at path: "page" when it is that page, "true"
// when the page stands in the folder href is (not for "/" nor an exact link), "" otherwise.
func linkCurrent(path, href string, exact bool) string {
	if path == "" || href == "" || strings.Contains(href, "://") || strings.HasPrefix(href, "#") {
		return ""
	}
	clean := func(s string) string {
		if i := strings.IndexAny(s, "?#"); i >= 0 {
			s = s[:i]
		}
		if len(s) > 1 {
			s = strings.TrimSuffix(s, "/")
		}
		return s
	}
	p, h := clean(path), clean(href)
	if p == h {
		return "page"
	}
	if exact || h == "/" {
		return ""
	}
	if strings.HasPrefix(p, h+"/") {
		return "true"
	}
	return ""
}

// normalizeAddress is an address as a browser reads it: control characters dropped, a backslash a slash, the
// spaces before it gone.
func normalizeAddress(address string) string {
	clean := true
	for i := 0; i < len(address); i++ {
		if c := address[i]; c < 0x20 || c == '\\' || c == ' ' && i == 0 {
			clean = false
			break
		}
	}
	if clean {
		return address
	}
	var b strings.Builder
	for i := 0; i < len(address); i++ {
		switch c := address[i]; {
		case c < 0x20:
		case c == ' ' && b.Len() == 0:
		case c == '\\':
			b.WriteByte('/')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// internalLink says href is a path of the site: one "/", not followed by another.
func internalLink(href string) bool {
	href = normalizeAddress(href)
	return strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//")
}

// linkScheme is the scheme a URL starts with, lower case, read the way a browser reads it: tabs and line breaks
// inside are dropped and leading spaces and control characters ignored ("java\tscript:" is javascript:). "" for a
// path, an anchor or a query.
func linkScheme(value string) string {
	var b strings.Builder
	for _, c := range value {
		if c == '\t' || c == '\n' || c == '\r' || (b.Len() == 0 && c <= ' ') {
			continue
		}
		b.WriteRune(c)
	}
	v := b.String()
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == ':':
			if i == 0 {
				return ""
			}
			return strings.ToLower(v[:i])
		case c == '/' || c == '?' || c == '#':
			return ""
		case i == 0 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			return ""
		case !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
			return ""
		}
	}
	return ""
}

// safeLink says href is a place to go: a page of the site, a web page, an e-mail, a phone number. javascript:,
// data:, vbscript: and every other scheme are not.
func safeLink(href string) bool {
	switch linkScheme(normalizeAddress(href)) {
	case "", "http", "https", "mailto", "tel":
		return true
	}
	return false
}

// safeImage says src is an image's address: a path of the site, a web address, or data:image/…, the first engine's
// rule (ui/render.go) that engine2 had left out until 13/11/2026. javascript:, a data: of another kind and every
// other scheme are not.
func safeImage(src string) bool {
	address := normalizeAddress(src)
	switch linkScheme(address) {
	case "", "http", "https":
		return true
	case "data":
		return strings.HasPrefix(strings.ToLower(address), "data:image/")
	}
	return false
}

// externalLink says href is a web address on another host than origin's (any web address when origin is not
// known): such a link leaves the site.
func externalLink(href, origin string) bool {
	address := normalizeAddress(href)
	var rest string
	switch lower := strings.ToLower(address); {
	case strings.HasPrefix(lower, "http:"), strings.HasPrefix(lower, "https:"):
		rest = address[strings.IndexByte(address, ':')+1:]
	case strings.HasPrefix(address, "//"):
		rest = address
	default:
		return false
	}
	if origin == "" {
		return true
	}
	host := hostName(strings.TrimLeft(rest, "/"))
	if i := strings.Index(origin, "://"); i >= 0 {
		origin = origin[i+3:]
	}
	return host == "" || host != hostName(origin)
}

// hostName is the host at the start of rest (what follows a web address's scheme and slashes), lower case,
// without the user in front or the port after it.
func hostName(rest string) string {
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndexByte(rest, '@'); i >= 0 {
		rest = rest[i+1:]
	}
	if strings.HasPrefix(rest, "[") {
		if i := strings.IndexByte(rest, ']'); i >= 0 {
			return strings.ToLower(rest[:i+1])
		}
	}
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

// neutralSteps are the fill of a neutral button: translucent ink, so the button shows on the grounds a page is
// made of, deeper under the pointer and while pressed, fainter while disabled.
var neutralSteps = []struct{ state, value string }{
	{"", "ink/6"}, {"hover", "ink/10"}, {"pressed", "ink/14"}, {"disabled", "ink/4"},
}

// neutralButton says a button's author writes nothing of its fill or its edge: no background, material or border,
// with or without a prefix. hover:background="panel" alone, or border="none", is the author's.
func neutralButton(n *Node) bool {
	if n.Tag != "button" {
		return false
	}
	for _, a := range n.Attributes {
		if _, name := splitPrefix(a.Name); name == "background" || name == "material" || name == "border" {
			return false
		}
	}
	return true
}

// newTabNote tells a screen reader that a link opens a new tab, in the page's language; it is not seen.
func newTabNote(english bool) string {
	words := "(mở trong tab mới)"
	if english {
		words = "(opens in a new tab)"
	}
	return `<span style="position:absolute;width:1px;height:1px;margin:-1px;padding:0;border:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap"> ` + words + `</span>`
}
