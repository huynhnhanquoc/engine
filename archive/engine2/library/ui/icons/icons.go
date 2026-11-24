// Package icons is the icons of a .ui page (KITWORK_UI mục 7, "Icon"): <icon value="shield-check" /> is
// <i class="icon-shield-check"></i>, and the page's stylesheet holds a rule for each icon the page shows, a
// CSS mask painted in the text's colour, so an icon takes color= and the text's size like a letter. No icon
// font, no CDN, no icon the page does not show.
//
// The set is Tabler Icons (MIT, TABLER_LICENSE), vendored in tabler/: the outline icon tabler/<name>.svg is
// icon-<name>, the filled one tabler/filled/<name>.svg is icon-<name>-fill. One icon is Kitwork's own: mesh.
package icons

import (
	"embed"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed tabler
var tabler embed.FS

// own are the icons Kitwork draws itself: the network glyph of its brand, which Tabler has no clean form of.
var own = map[string]string{
	"mesh": `<path d="M12 12V5.5M12 12 6 18M12 12 18 18"/><circle cx="12" cy="5" r="2"/><circle cx="5.5" cy="18.2" r="2"/><circle cx="18.5" cy="18.2" r="2"/><circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/>`,
}

// fill ends the name of a filled icon: no outline icon of the set ends in it.
const fill = "-fill"

// name is an icon's name: lower case letters, digits and hyphens, so it never leaves tabler/.
var name = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// drawn are the icons read once, by name; a name with no icon is kept as "".
var drawn sync.Map

// inner is what an icon draws inside its <svg>, and whether the set has it.
func inner(icon string) (string, bool) {
	if !name.MatchString(icon) {
		return "", false
	}
	if svg, ok := own[icon]; ok {
		return svg, true
	}
	if kept, ok := drawn.Load(icon); ok {
		return kept.(string), kept.(string) != ""
	}
	file := "tabler/" + icon + ".svg"
	if base, filled := strings.CutSuffix(icon, fill); filled {
		file = "tabler/filled/" + base + ".svg"
	}
	svg := ""
	if data, err := tabler.ReadFile(file); err == nil {
		svg = contents(string(data))
	}
	drawn.Store(icon, svg)
	return svg, svg != ""
}

// box is Tabler's invisible 24×24 path, in the two orders of its attributes the set is written in.
var box = regexp.MustCompile(`<path[^>]* d="M0 0h24v24H0z"[^>]*/>`)

// contents is a Tabler file's drawing: what its <svg> holds, without the box. A file begins with a comment
// (<!-- tags: … -->) whose '>' is not the <svg>'s.
func contents(svg string) string {
	open := strings.Index(svg, "<svg")
	if open < 0 {
		return ""
	}
	end := strings.IndexByte(svg[open:], '>')
	closing := strings.LastIndex(svg, "</svg>")
	if end < 0 || closing <= open+end+1 {
		return ""
	}
	return strings.TrimSpace(box.ReplaceAllString(svg[open+end+1:closing], ""))
}

// Has says whether the set has the icon.
func Has(icon string) bool {
	_, ok := inner(icon)
	return ok
}

// Names are every icon of the set, sorted: what a misspelt name is compared with.
var Names = sync.OnceValue(func() []string {
	names := []string{}
	for icon := range own {
		names = append(names, icon)
	}
	for _, folder := range []struct{ path, suffix string }{{"tabler", ""}, {"tabler/filled", fill}} {
		entries, _ := tabler.ReadDir(folder.path)
		for _, entry := range entries {
			if base, ok := strings.CutSuffix(entry.Name(), ".svg"); ok && !entry.IsDir() {
				names = append(names, base+folder.suffix)
			}
		}
	}
	sort.Strings(names)
	return names
})

// encode escapes what matters in a url("data:image/svg+xml,…"): the drawing is written with single quotes,
// so a double quote never is in it, and one pass never escapes the % it writes.
var encode = strings.NewReplacer("%", "%25", "#", "%23", "<", "%3C", ">", "%3E")

// mask is an icon as the image of its mask: black on nothing, since a mask reads only how opaque each point
// is; the colour is the element's text colour (background: currentColor). An outline icon is stroked 1.75
// wide, a filled one filled.
func mask(drawing string, filled bool) string {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">`
	if filled {
		svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#000" stroke="none">`
	}
	svg += drawing + `</svg>`
	svg = strings.ReplaceAll(svg, "currentColor", "#000")
	svg = strings.ReplaceAll(svg, `"`, `'`)
	svg = strings.Join(strings.Fields(svg), " ")
	return `url("data:image/svg+xml,` + encode.Replace(svg) + `")`
}

// CSS is the rules of the icons named, each once, sorted: one rule of no specificity (:where) for their
// shape, a letter's size and baseline, so any class of the page sizes or hides an icon, then one mask each.
// An icon the set does not have has no rule. "" when none is drawn.
func CSS(named []string) string {
	seen := map[string]bool{}
	var shown []string
	for _, icon := range named {
		if !seen[icon] && Has(icon) {
			seen[icon] = true
			shown = append(shown, icon)
		}
	}
	if len(shown) == 0 {
		return ""
	}
	sort.Strings(shown)
	var b strings.Builder
	b.WriteString(":where(")
	for i, icon := range shown {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(".icon-" + icon)
	}
	b.WriteString("){display:inline-block;width:1em;height:1em;vertical-align:-.125em;flex:none;background:currentColor;" +
		"-webkit-mask:var(--i) center/contain no-repeat;mask:var(--i) center/contain no-repeat}")
	for _, icon := range shown {
		drawing, _ := inner(icon)
		b.WriteString(".icon-" + icon + "{--i:" + mask(drawing, strings.HasSuffix(icon, fill)) + "}")
	}
	b.WriteByte('\n')
	return b.String()
}

// Nearest is what a misspelt icon's error adds: the icons of the set closest to the name, ": did you mean
// x?", or "" when none is close.
func Nearest(icon string) string {
	best, distance := []string{}, 3
	for _, each := range Names() {
		d := editDistance(icon, each)
		switch {
		case d < distance:
			best, distance = []string{each}, d
		case d == distance && len(best) < 3:
			best = append(best, each)
		}
	}
	if len(best) == 0 {
		return ""
	}
	return ": did you mean " + strings.Join(best, ", ") + "?"
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}
