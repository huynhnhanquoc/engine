package ui

import (
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/library/design/highlight"
)

// The terminal (decisions of 12/10/2026, moved from the first engine on 06/11/2026, slice 3) is one of Kitwork's
// components, written as its tag. The author writes the whole look in markup; the component gives behaviour:
//
//	<terminal typing="3s" radius="medium">
//	  <box flex="row" …><text>bash</text><button click="copy()">{{ copied ? 'Copied' : 'Copy' }}</button></box>
//	  <box padding="4">
//	    <code language="bash">git clone https://github.com/kitwork/engine.git</code>
//	    <text color="ink-30">Cloning into 'engine'...</text>
//	  </box>
//	</terminal>
//
// Every <code> inside a terminal is a command, wherever it sits in the host, except inside a component nested in
// it, whose code is its own. Before each the engine writes the prompt of its language (terminalPrompts) as
// <span class="code-prompt">, muted and never selected, outside the <code>: a block is
// <pre><span class="code-prompt">$ </span><code>…</code></pre>, a command in a line a <span> holding both. A
// <text> is whatever the author wants (what a command printed, a title), never copied. At the end of the host one
// polite status region, where the component says what a copy did.
//
// typing is the terminal's own: the total time typing every command takes ("3s", "800ms"), handed to the kernel
// in milliseconds (data-kitwork-scope="typing: 3000"). The whole text is in the HTML from the first byte; what is
// not typed yet is only painted transparent, so a screen reader and find in page have every line at once.

// terminalPrompts are the prompts written before a command, by the language of the shell it is typed in; a
// command that names no language takes "$ ". Any other language is a file's, shown in the terminal: no prompt.
var terminalPrompts = map[string]string{"": "$ ", "bash": "$ ", "powershell": "PS> ", "python": ">>> ", "sql": "sql> "}

// terminalStatus is the terminal's polite status region: seen by a screen reader, not by the eye, empty until the
// component says what a copy did.
const terminalStatus = `<span class="reader" role="status"></span>`

// terminalScope is the terminal whose commands the <code> elements being drawn are: it counts them, so a terminal
// without one is told.
type terminalScope struct {
	commands int
}

// terminalHost reads the terminal's own word off its host (typing, fixed when the page is built): the host goes on as
// the host of a component without a UI, holding what the author draws, then its status region.
func (r *renderer) terminalHost(n *Node) (*Node, *host, error) {
	if len(n.Significant()) == 0 {
		return nil, nil, at(n, nil, "terminal cần ít nhất một lệnh bên trong: <terminal><code>lệnh</code></terminal> (mỗi <code> bên trong là một lệnh)")
	}
	own := &host{tail: terminalStatus}
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		switch {
		case base == "shell":
			language := "bash"
			if value := strings.TrimSpace(a.Value); !a.Bare && !strings.Contains(value, "{{") && highlight.Has(value) {
				language = value
			}
			return false, at(n, a, `shell đã bỏ: dấu nhắc nay theo language của từng <code> ("$ " cho bash và cho <code> không viết language, "PS> " cho powershell, ">>> " cho python, "sql> " cho sql); bỏ shell và viết <code language="%s">…</code>`, language)
		case base == "typing" && chain != "":
			return false, at(n, a, "typing của terminal không nhận tiền tố: nó được quyết một lần khi trang dựng")
		case base == "typing":
			ms, err := typing(n, a)
			if err != nil {
				return false, err
			}
			if ms > typingLongest {
				// motion that plays by itself past 5 seconds needs a way to stop it: typed as written
				r.warn(n, a, "%s", `typing="`+a.Value+`" gõ lâu hơn 5 giây: chuyển động tự chạy quá 5 giây cần một cách để dừng (WCAG 2.2.2 Pause, Stop, Hide); viết typing="5s" hoặc ngắn hơn`)
			}
			own.inputs = append(own.inputs, "typing: "+strconv.FormatFloat(ms, 'f', -1, 64))
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return copied, own, nil
}

// typingLongest is the longest a terminal types in by itself, in ms: longer, motion that plays by itself needs a way to
// stop it (WCAG 2.2.2).
const typingLongest = 5000

// typing reads typing=: the total time typing every command takes, a duration ("3s", "800ms"), in milliseconds.
func typing(n *Node, a *Attribute) (float64, error) {
	value := strings.TrimSpace(a.Value)
	switch {
	case a.Bare || value == "":
		return 0, at(n, a, `typing cần một thời lượng: viết typing="3s" (tổng thời gian gõ mọi lệnh); không gõ thì bỏ typing`)
	case strings.Contains(value, "{{"):
		return 0, at(n, a, `typing của terminal là một thời lượng cố định (typing="3s"), không có {{ }}: nó được quyết một lần khi trang dựng`)
	case value == "true" || value == "false":
		return 0, at(n, a, `typing nay là tổng thời gian gõ mọi lệnh, không phải %s: viết typing="3s" hoặc typing="800ms"; không gõ thì bỏ typing`, value)
	}
	d := readDuration(value)
	switch d.problem {
	case durationNegative:
		return 0, at(n, a, `typing không nhận thời lượng âm (%s): viết một thời lượng như typing="%s"`, value, d.suggestion(durationMilliseconds))
	case durationZero:
		return 0, at(n, a, `typing="%s" là không giây: không gõ thì bỏ typing; gõ thì viết một thời lượng như typing="3s"`, a.Value)
	case durationNoUnit:
		return 0, at(n, a, `typing="%s" thiếu đơn vị: viết typing="%s"`, a.Value, d.suggestion(durationMilliseconds))
	case durationTwoUnits, durationForeign:
		return 0, at(n, a, `typing="%s" %s: viết typing="%s"`, a.Value, d.reason(), d.suggestion(durationMilliseconds))
	}
	if d.problem == durationNotOne || math.IsInf(d.ms, 0) {
		return 0, at(n, a, `typing="%s" không phải một thời lượng: viết typing="3s" hoặc typing="800ms" (tổng thời gian gõ mọi lệnh)`, a.Value)
	}
	return d.ms, nil
}

// prompt is the prompt of a command in language as markup, "" for a file's language (no prompt), its rule in the
// page's stylesheet.
func (r *renderer) prompt(language string) (string, error) {
	text, ok := terminalPrompts[language]
	if !ok || text == "" {
		return "", nil
	}
	class, err := r.table.Code(r.rules, highlight.Prompt.String())
	if err != nil {
		return "", err
	}
	return `<span class="` + class + `">` + html.EscapeString(text) + `</span>`, nil
}
