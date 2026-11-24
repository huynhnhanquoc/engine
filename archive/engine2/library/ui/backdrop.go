package ui

// A page backdrop (decision D4 of 13/10/2026; moved from the first engine's ui/backdrop.go on 06/11/2026):
// <view background="cirrus"> paints its gradient on a layer fixed to the screen, behind everything, never
// stretched over the document's height. The layer is the first element of <body>, <div class="page-backdrop
// background-cirrus" aria-hidden="true" data-kitwork-backdrop>, carrying every background class <view> writes,
// and <body> keeps none of them. A real element, so Drive keeps it on a move between pages of one frame. The
// layer's rules, the focus ring where the brand falls short on it and, for a backdrop that paints solid
// everywhere, <html>'s ground in the gradient's first stop are the stylesheet's (Table.Backdrop); the document's
// <meta name="theme-color"> is that colour in each mode (themeColor).
//
// A backdrop is written plainly: a background of <view> from data beside a gradient, or one that spells a gradient
// out ({{ night ? 'dusk' : 'cirrus' }}), is an error, since the layer is written once with the view's classes and
// a value from data would change only <body>'s.

import (
	"html"
	"strings"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/design"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// viewBackdrop moves the background classes of a <view> one of whose backgrounds paints a gradient onto the
// backdrop layer, records the layer's rules, and gives the classes <body> keeps and the layer's markup; the
// classes as they were and "" for a view whose backgrounds are colours, and for any other element.
func (r *renderer) viewBackdrop(n *Node, classes []string) ([]string, string, error) {
	if n.Tag != "view" {
		return classes, "", nil
	}
	gradient := func(name string) bool {
		_, ok := r.table.Palette().Gradient(strings.TrimSpace(name))
		return ok
	}
	painted, ground := false, ""
	var fromData *Attribute
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if _, name := splitPrefix(a.Name); name != "background" {
			continue
		}
		v := strings.TrimSpace(a.Value)
		if strings.Contains(v, "{{") {
			fromData = a
			// a gradient it spells out would be a backdrop from data: an error as the rest is, never left on
			// <body> stretched over the document
			if source, whole := wrapped(v); whole {
				literals, _ := compiler.ExpressionStrings(source)
				for _, literal := range literals {
					painted = painted || gradient(literal)
				}
			}
			continue
		}
		if gradient(v) {
			painted = true
			if a.Name == "background" {
				ground = v
			}
		}
	}
	if !painted {
		return classes, "", nil
	}
	if fromData != nil {
		return nil, "", at(n, fromData, "%s=\"%s\": <view> có nền gradient, vẽ trên một lớp cố định theo màn hình (nền trang); nền của trang viết thẳng, không lấy từ dữ liệu: lớp nền viết một lần với các class của trang, giá trị từ {{ }} chỉ đổi <body>",
			fromData.Name, strings.TrimSpace(fromData.Value))
	}
	var layer, rest []string
	for _, class := range classes {
		if strings.HasPrefix(class[strings.LastIndexByte(class, ':')+1:], "background-") {
			layer = append(layer, class)
			continue
		}
		rest = append(rest, class)
	}
	r.table.Backdrop(r.rules, ground)
	return rest, `<div class="` + html.EscapeString(strings.Join(append([]string{stylesheet.BackdropClass}, layer...), " ")) +
		`" aria-hidden="true" data-kitwork-backdrop></div>`, nil
}

// themeColor is the document's <meta name="theme-color">, the colour a mobile browser paints its bar and the strip
// around the page in, when the page's backdrop is a gradient that paints solid: its first stop, as <html>'s ground.
// In the look the site shows when it fixes one (design.appearance "light" or "dark"), or when the palette has no
// dark look; otherwise one per mode, for the device's. With a dark look each also carries the colour of both
// modes, which the prepaint and the kernel set when the visitor chose the other mode. "" for a page without such a
// backdrop.
func themeColor(rules *stylesheet.Rules, look *design.Look) string {
	ground := rules.Backdrop()
	if ground == "" {
		return ""
	}
	palette := look.Palette
	hex := func(m stylesheet.Mode) string {
		c, _ := palette.GradientGround(ground, m)
		return c.Painted().Hex()
	}
	light, dark, modes := hex(stylesheet.LightMode), "", ""
	if palette.Dark() {
		dark = hex(stylesheet.DarkMode)
		modes = ` data-kitwork-light="` + light + `" data-kitwork-dark="` + dark + `"`
	}
	meta := func(content, media string) string {
		out := `<meta name="theme-color"`
		if media != "" {
			out += ` media="` + media + `"`
		}
		return out + ` content="` + content + `"` + modes + ">\n"
	}
	switch {
	case !palette.Dark() || look.Appearance == "light":
		return meta(light, "")
	case look.Appearance == "dark":
		return meta(dark, "")
	}
	return meta(light, "(prefers-color-scheme: light)") + meta(dark, "(prefers-color-scheme: dark)")
}
