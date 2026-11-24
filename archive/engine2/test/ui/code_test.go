package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// A <code> standing on its own is a block, <pre><code class="language-…">, whose text keeps its lines without
// the indent the source gives them all; each token of a kind is a span, and the page's stylesheet has the
// rule of each kind it shows, and of no other.
func TestABlockOfCodeIsColouredOnTheServer(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/content.ui": "<content><code language=\"javascript\" padding=\"4\">\n      const total = 1;\n        return \"x\";\n    </code></content>",
	}, value.Value{})
	want := `<pre class="padding-4"><code class="language-javascript"><span class="code-keyword">const</span> total <span class="code-operator">=</span> <span class="code-number">1</span><span class="code-punctuation">;</span>` + "\n" +
		`  <span class="code-keyword">return</span> <span class="code-string">&#34;x&#34;</span><span class="code-punctuation">;</span></code></pre>`
	if !strings.Contains(body, want) {
		t.Errorf("body\n%s\nwant it to hold\n%s", body, want)
	}
	for _, kind := range []string{".code-keyword{", ".code-string{", ".code-number{"} {
		if !strings.Contains(document, kind) {
			t.Errorf("the page's stylesheet has no rule %s", kind)
		}
	}
	if strings.Contains(document, ".code-comment{") {
		t.Errorf("the page's stylesheet has the rule of a kind the page does not show")
	}
}

// A <code> inside a line of text is a <code> in the line, its language its first class; what a code holds is
// text, escaped, never markup; code with no language is plain.
func TestCodeInALineAndPlainCode(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><text>Run <code language="bash">go run .</code> now</text><code>{{ hostile }}</code></content>`,
	}, object("hostile", value.String("</code><script>alert(1)</script>")))
	for _, want := range []string{
		`<p>Run <code class="language-bash"><span class="code-function">go</span> run .</code> now</p>`,
		`<pre><code>&lt;/code&gt;&lt;script&gt;alert(1)&lt;/script&gt;</code></pre>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body\n%s\nwant it to hold %s", body, want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("a code's text became markup:\n%s", body)
	}
	// the plain text between a language's tokens is escaped too
	_, body = render(t, map[string]string{
		"/app/site/content.ui": `<content><code language="html">{{ sample }}</code></content>`,
	}, object("sample", value.String("<b>x & y</b>")))
	if strings.Contains(body, "x & y") || !strings.Contains(body, "&amp;") {
		t.Errorf("the plain text of code in a language is not escaped:\n%s", body)
	}
}

// A language the page writes that the highlighter lacks is an error at its place naming what to write; one from
// the page's data shows the code plain; a <code> holds text only.
func TestALanguageTheHighlighterLacks(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><code language="js">x</code></content>`, `content.ui:1:16: ngôn ngữ "js" chưa có; viết javascript`},
		{`<content><code language="bash"><text>x</text></code></content>`, "content.ui:1:10: code chỉ chứa chữ, không có thẻ con"},
	} {
		site := memorySite(map[string]string{"/app/site/content.ui": c.content})
		page, err := site.Page(route(""))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = site.Document(page, value.Value{}, ui.Head{}, kitDesign(t))
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s: %v, want an error beginning %q", c.content, err, c.error)
		}
	}
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><code language="{{ language }}">a &lt; b</code></content>`,
	}, object("language", value.String("klingon")))
	if !strings.Contains(body, `<pre><code>a &amp;lt; b</code></pre>`) {
		t.Errorf("code in a language from data the highlighter lacks:\n%s", body)
	}
}
