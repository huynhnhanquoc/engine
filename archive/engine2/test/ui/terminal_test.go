package ui_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// The terminal of kitwork.io's home: the host keeps its look and says its component, seeded with the time typing
// takes in milliseconds; each <code> inside is a command with the prompt of its language before it, outside the
// <code>, the prompt's rule in the page; a <text> is no command; the copy button follows copied; the host ends on
// the polite status region a copy is said in.
func TestATerminalsCodeIsItsCommands(t *testing.T) {
	document, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><terminal typing="3s" radius="medium"><box flex="row">` +
			`<text>bash</text><button click="copy()">{{ copied ? 'Copied' : 'Copy' }}</button></box>` +
			`<box padding="4"><code language="bash">cd core</code><text color="ink-30">Cloning…</text><code>go run .</code></box>` +
			`</terminal></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column radius-medium" data-kitwork-component="terminal" data-kitwork-scope="typing: 3000">`,
		`data-kitwork-bind="text: copied ? &#39;Copied&#39; : &#39;Copy&#39;" data-kitwork-event="click: copy()">Copy</button>`,
		`<pre><span class="code-prompt">$ </span><code class="language-bash"><span class="code-function">cd</span> core</code></pre>`,
		`<p class="color-ink-30">Cloning…</p>`,
		`<pre><span class="code-prompt">$ </span><code>go run .</code></pre>`,
		`</div><span class="reader" role="status"></span></div>`)
	holds(t, document, ".code-prompt{")
	if strings.Count(body, "code-prompt") != 2 {
		t.Errorf("a text in a terminal took a prompt:\n%s", body)
	}
}

// The prompt is the shell's: "$ " for bash and for a command that names no language, "PS> " for powershell, ">>> "
// for python, "sql> " for sql, none for a file's language (a file shown in the terminal). A command in a line is a
// <span> holding the prompt and the <code>, the span taking the code's look; a <code> of a component nested in the
// terminal is its own, and one outside every terminal no command. A terminal that types nothing seeds nothing.
func TestTheTerminalsPromptFollowsTheLanguage(t *testing.T) {
	_, body := render(t, map[string]string{
		"/app/site/content.ui": `<content><terminal>` +
			`<code language="powershell">dir</code><code language="python">x</code><code language="sql">select 1</code>` +
			`<code language="work">const a = 1;</code><text>Run <code language="bash" color="brand">go run .</code></text>` +
			`<dropdown><code>inside</code><popover>p</popover></dropdown></terminal><code>outside</code></content>`,
	}, value.Value{})
	holds(t, body,
		`<div class="flex-column" data-kitwork-component="terminal">`,
		`<pre><span class="code-prompt">PS&gt; </span><code class="language-powershell">`,
		`<pre><span class="code-prompt">&gt;&gt;&gt; </span><code class="language-python">`,
		`<pre><span class="code-prompt">sql&gt; </span><code class="language-sql">`,
		`<pre><code class="language-work">`,
		`<p>Run <span class="color-brand"><span class="code-prompt">$ </span><code class="language-bash"><span class="code-function">go</span> run .</code></span></p>`,
		`<pre><code>inside</code></pre>`,
		`<pre><code>outside</code></pre>`)
}

// What a terminal cannot be is an error where it is written: no command, nothing inside, shell (gone: the prompt
// follows each command's language), a typing that is no duration, or one that changes with the page.
func TestWhatATerminalCannotBe(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><terminal></terminal></content>`, "content.ui:1:10: terminal cần ít nhất một lệnh bên trong"},
		{`<content><terminal><text>no command</text></terminal></content>`, "content.ui:1:10: terminal cần ít nhất một lệnh: một <code> bên trong"},
		{`<content><terminal shell="powershell"><code>x</code></terminal></content>`, `content.ui:1:20: shell đã bỏ: dấu nhắc nay theo language của từng <code>`},
		{`<content><terminal typing="300"><code>x</code></terminal></content>`, `content.ui:1:20: typing="300" thiếu đơn vị: viết typing="300ms"`},
		{`<content><terminal typing="0s"><code>x</code></terminal></content>`, `content.ui:1:20: typing="0s" là không giây`},
		{`<content><terminal typing="true"><code>x</code></terminal></content>`, "content.ui:1:20: typing nay là tổng thời gian gõ mọi lệnh, không phải true"},
		{`<content><terminal typing="{{ time }}"><code>x</code></terminal></content>`, "content.ui:1:20: typing của terminal là một thời lượng cố định"},
		{`<content><terminal medium:typing="2s"><code>x</code></terminal></content>`, "content.ui:1:20: typing của terminal không nhận tiền tố"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
}
