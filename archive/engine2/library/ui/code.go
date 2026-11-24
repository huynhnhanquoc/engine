package ui

import (
	"html"
	"strings"

	"github.com/kitwork/engine2/library/design/highlight"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// code draws a <code> (KITWORK_UI mục 7, "Mã có màu"): standing on its own a block, <pre><code>, which keeps
// its lines and spaces; inside a line of text a <code>. With language= its text is coloured on the server,
// with no JavaScript in the browser (decision of 12/10/2026):
//
//   - The text is read by the highlighter (library/design/highlight) into tokens; each token that is not plain
//     text is <span class="code-<kind>">, plain text stays bare, every text escaped, so the element's text is
//     the source byte for byte and code never becomes markup.
//   - The colour of each kind the code shows is a rule of the page's stylesheet: the palette's roles, or the
//     site's design.highlight, in both looks (stylesheet.Table.Code).
//   - A language the page writes that the highlighter does not have is an error at its place, naming what to
//     write (js → javascript); a language from the page's data it does not have, or a text longer than the
//     highlighter takes, shows the code plain.
//
// A block's text is its lines as written, without the blank lines around them and the indent the source gives
// all of them; a value from {{ }} keeps its own.
//
// Inside a terminal a <code> is a command: the prompt of its language before it, outside the <code> (terminal.go).
func (r *renderer) code(n *Node, inline bool) error {
	if len(n.Elements()) > 0 {
		return at(n, nil, "code chỉ chứa chữ, không có thẻ con")
	}
	language, named := "", "" // named: the language the code names, for a command's prompt
	said := ""                // how the language is written, for a warning: language="go"
	if a, ok := n.Attribute("language"); ok {
		written, err := r.interpolate(n, &a, a.Value)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(written)
		named, said = name, a.Name+`="`+a.Value+`"`
		switch {
		case highlight.Has(name):
			language = name
		case !strings.Contains(a.Value, "{{"):
			return at(n, &a, "%s", (&highlight.UnknownLanguageError{Name: name, Suggestions: highlight.Suggest(name)}).Error())
		}
	}
	raw := ""
	for _, child := range n.Children {
		raw += child.Text
	}
	if !inline {
		raw = blockText(raw)
	}
	source, err := r.interpolate(n, nil, raw)
	if err != nil {
		return err
	}
	open, err := r.opening(n, kindInline)
	if err != nil {
		return err
	}
	prompt := ""
	if r.terminal != nil {
		r.readScope(scopeTerminal) // what a scope around gives it: drawn again (frozen.go)
		r.terminal.commands++
		if prompt, err = r.prompt(named); err != nil {
			return err
		}
	}
	inner := ""
	if language != "" {
		inner = ` class="language-` + language + `"`
	}
	wrapped := inline && prompt != "" // a command in a line: the prompt and the code in one <span>, its look the span's
	switch {
	case wrapped:
		r.write("span", open)
		r.out.WriteString(prompt + "<code" + inner + ">")
	case inline:
		if language != "" {
			open.classes = append([]string{"language-" + language}, open.classes...)
		}
		r.write("code", open)
	default:
		r.write("pre", open)
		r.out.WriteString(prompt + "<code" + inner + ">")
	}
	if err := r.coloured(n, said, language, source); err != nil {
		return err
	}
	switch {
	case wrapped:
		r.out.WriteString("</code></span>")
	case inline:
		r.out.WriteString("</code>")
	default:
		r.out.WriteString("</code></pre>")
	}
	return nil
}

// coloured writes source in language, each token of a kind in its span and the kind's rule in the page's
// stylesheet; plain, escaped, with no language or when the highlighter gives it back whole. said is how the code
// element n writes its language.
func (r *renderer) coloured(n *Node, said, language, source string) error {
	if language == "" {
		r.out.escape(source)
		return nil
	}
	// a text the highlighter refuses (too long) comes back as one plain token: shown plain, and told
	tokens, failure := r.site.tokens(language, source)
	if failure != nil {
		r.warn(n, nil, "%s", "<code "+said+">: "+failure.Error())
	}
	written, err := r.site.highlighted(r.table, r.rules, language, source, tokens)
	r.out.WriteString(written)
	return err
}

// highlight writes tokens, each of a kind in its span and the kind's rule in rules.
func (s *Site) highlight(table *stylesheet.Table, rules *stylesheet.Rules, tokens []highlight.Token) (string, error) {
	var out strings.Builder
	for _, token := range tokens {
		if token.Kind == highlight.Plain {
			out.WriteString(html.EscapeString(token.Text))
			continue
		}
		kind := token.Kind.String()
		class, err := s.codeClass(table, rules, kind)
		if err != nil {
			return out.String(), err
		}
		out.WriteString(`<span class="` + class + `">` + html.EscapeString(token.Text) + `</span>`)
	}
	return out.String(), nil
}

// blockText is a code block's text as it shows: its lines as written, without the blank lines around them (the
// line break after <code>, the indent before </code>) and without the indent the source gives every one.
func blockText(raw string) string {
	lines := strings.Split(raw, "\n")
	for len(lines) > 1 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 1 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if n := len(line) - len(strings.TrimLeft(line, " \t")); indent < 0 || n < indent {
			indent = n
		}
	}
	for i, line := range lines {
		if indent > 0 && len(line) >= indent {
			lines[i] = line[indent:]
		}
	}
	return strings.Join(lines, "\n")
}
