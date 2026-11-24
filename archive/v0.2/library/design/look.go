package design

import (
	"html"
	"sync"

	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// Look is what a folder's pages are drawn with: Kit Design's (mục 3), then the design.work of the app root,
// the site's root and each branch down to the folder, the nearest winning per token, per role, per kind,
// per gradient, and for the three words one value. It is built at boot, once a folder, and read by every
// request of its pages, never written after.
type Look struct {
	Palette *stylesheet.Palette
	// Body and Code are the typefaces of the two roles, the defaults when no folder names one.
	Body, Code string
	// Appearance is "system", "light" or "dark"; Contrast "warning" or "strict"; Transition "none", "fade"
	// or "slide".
	Appearance, Contrast, Transition string
	// the declarations down to the folder, the nearest last, for the look of a folder below
	colors    []map[string]stylesheet.Color
	gradients []map[string]stylesheet.Gradient
	highlight []map[string]stylesheet.HighlightColour
}

// kitDesign is the look with no design.work: Kit Design's palette and typefaces.
var kitDesign = sync.OnceValues(func() (*Look, error) {
	palette, err := stylesheet.NewPalette(false)
	if err != nil {
		return nil, err
	}
	return &Look{Palette: palette, Body: DefaultBody, Code: DefaultCode, Appearance: "system", Contrast: "warning", Transition: "none"}, nil
})

// KitDesign is the look of a folder no design.work above it declares anything for.
func KitDesign() (*Look, error) { return kitDesign() }

// Under is the look of a folder whose design.work declared s, below a folder whose look is l: l itself when
// s declares nothing. The palette is built again from the whole chain, so a colour, a gradient or a kind the
// palette refuses is the boot's error, at the folder that wrote it.
func (l *Look) Under(s *Scope) (*Look, error) {
	if s == nil || s.Empty() {
		return l, nil
	}
	below := *l
	below.colors = append(append([]map[string]stylesheet.Color(nil), l.colors...), s.colors)
	below.gradients = append(append([]map[string]stylesheet.Gradient(nil), l.gradients...), s.gradients)
	below.highlight = append(append([]map[string]stylesheet.HighlightColour(nil), l.highlight...), s.highlight)
	if s.body != "" {
		below.Body = s.body
	}
	if s.code != "" {
		below.Code = s.code
	}
	if s.appearance != "" {
		below.Appearance = s.appearance
	}
	if s.contrast != "" {
		below.Contrast = s.contrast
	}
	if s.transition != "" {
		below.Transition = s.transition
	}
	palette, err := stylesheet.NewPalette(below.Appearance == "dark", below.colors...)
	if err == nil {
		palette, err = palette.WithGradients(below.gradients...)
	}
	if err == nil {
		palette, err = palette.WithHighlight(below.highlight...)
	}
	if err != nil {
		return nil, err
	}
	below.Palette = palette
	return &below, nil
}

// CSS is the look's part of a page's stylesheet: the palette's colours in both looks, then the typefaces.
func (l *Look) CSS() string {
	return string(stylesheet.Colors(l.Palette).CSS) + fontsCSS(l.Body, l.Code)
}

// HTMLAttributes are what the look writes on <html>: the look a page shows first when it is not the
// device's, which the prepaint reads; how a page changes when Drive moves to it (design.transition), which Drive
// reads, when it is not "none".
func (l *Look) HTMLAttributes() string {
	out := ""
	if l.Appearance == "light" || l.Appearance == "dark" {
		out += ` data-kitwork-appearance="` + html.EscapeString(l.Appearance) + `"`
	}
	if l.Transition != "" && l.Transition != "none" {
		out += ` data-kitwork-transition="` + html.EscapeString(l.Transition) + `"`
	}
	return out
}

// Head is what the look puts in a page's <head> before its stylesheet: the prepaint, then the preload of
// the face running text is drawn in.
func (l *Look) Head() string {
	return prepaint + preload(l.Body)
}

// prepaint decides the look before the first paint, in <head>, so the page never shows the other one first
// (KITWORK_UI mục 7, "Chế độ sáng/tối"): the visitor's choice kept in localStorage "appearance" (or "theme",
// where it was kept before), else the look the site shows first (<html data-kitwork-appearance>,
// design.appearance), else the device's. It writes the class dark on <html>, which the kernel's
// kitwork.appearance reads the look shown from (library/ui/kernel web.js: the two decide by one rule, so the
// first paint and the kernel agree), data-theme, which the palette's dark tokens are under, and color-scheme,
// so the browser's own controls follow. It is one constant on every page, so one hash allows it in a Content
// Security Policy.
const prepaint = `<script data-kitwork-generated="appearance">(function(){var r=document.documentElement,m="";` +
	`function v(t){t=t&&t.toLowerCase();return t==="light"||t==="dark"||t==="system"?t:""}` +
	`try{var s=localStorage;m=v(s.getItem("appearance"))||v(s.getItem("theme"))}catch(e){}` +
	`m=m||v(r.getAttribute("data-kitwork-appearance"))||"system";` +
	`if(m==="system"){try{m=matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light"}catch(e){m="light"}}` +
	`r.classList.toggle("dark",m==="dark");r.setAttribute("data-theme",m);r.style.colorScheme=m})();</script>` + "\n"
