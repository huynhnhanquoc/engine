package hydrate

import (
	"strings"
	"testing"
)

func TestHTMLBindingChecks(t *testing.T) {
	markup := `<div data-kitwork-scope="content: ''"><div data-kitwork-bind="html: content"></div></div>`
	if problems := authoringMistakes(markup); len(problems) != 0 {
		t.Fatalf("valid HTML binding: %v", problems)
	}
	markup = `<div data-kitwork-scope="content: ''"><div data-kitwork-bind="html: content++"></div></div>`
	if problems := strings.Join(authoringMistakes(markup), "\n"); !strings.Contains(problems, `writes to "content"`) {
		t.Fatalf("HTML expressions must remain read-only, got %s", problems)
	}
}

func TestHTMLBindingFailsClosedWithoutModule(t *testing.T) {
	runNodeDOMScript(t, "html-module-required.test.js", `
var target = el('div');
document.body.appendChild(target);
var rejected = false;
try { window.kitwork.write(target, 'html', '<b>unsafe without a sanitizer</b>'); }
catch (error) { rejected = /needs directives\/html/.test(error.message); }
if (!rejected || target.childNodes.length) throw new Error('HTML must not fall back to a raw sink');
`)
}
