package js

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTMLBindingDelivery(t *testing.T) {
	for _, test := range []struct {
		markup string
		want   bool
	}{
		{`<div data-kitwork-bind="html: content"></div>`, true},
		{`<div data-kitwork-bind="show: opened; html: content"></div>`, true},
		{`<div data-kitwork-bind="text: 'html: content'"></div>`, false},
		{`<div data-kitwork-bind="title: content"></div>`, false},
		{`<code>&lt;div data-kitwork-bind="html: content"&gt;</code>`, false},
	} {
		var found bool
		for _, asset := range PageAssets(test.markup, Options{}) {
			if strings.HasPrefix(asset.Path, AssetPrefix+"directives/html.") {
				found = true
				if resolved, exact := AssetAt(asset.Path); !exact || resolved == nil {
					t.Fatalf("HTML directive is not addressable: %s", asset.Path)
				}
			}
		}
		if found != test.want {
			t.Errorf("HTML module delivered = %v, want %v for %s", found, test.want, test.markup)
		}
	}
}

// A real HTML parser is part of the boundary. A node shim cannot prove that malformed HTML,
// foreign namespaces, character references or resource elements stay inert while being parsed.
func TestBrowserHTMLBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("HTML binding proof needs a real browser")
	}
	browser := findHeadlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	payloads, err := json.Marshal([]string{
		`<script>window.__htmlExecuted++</script><b>kept</b>`,
		`<img src="/forbidden-image" onerror="window.__htmlExecuted++"><strong>kept</strong>`,
		`<svg onload="window.__htmlExecuted++"><foreignObject><div>foreign</div></foreignObject></svg>`,
		`<math><mtext><img src="/forbidden-math" onerror="window.__htmlExecuted++"></mtext></math>`,
		`<a href="jav&#x61;script:window.__htmlExecuted++">link</a>`,
		`<a href="java&#x09;script:window.__htmlExecuted++">link</a>`,
		`<a href="data:text/html,evil">link</a>`,
		`<iframe src="/forbidden-frame" srcdoc="evil"></iframe><object data="/forbidden-object"></object>`,
		`<div id="clobber" name="clobber" class="hidden" style="display:none" onclick="window.__htmlExecuted++" data-kitwork-event="click: count++" data-kitwork-component="html-probe: $injected" data-kitwork-mark="5"><span>kept</span></div>`,
		`<template><b>not mounted</b></template><style>body{display:none}</style><!-- no comments -->`,
		`<math><mtext><table><mglyph><style><!--</style><img title="--><img src=/forbidden-mxss onerror=window.__htmlExecuted++>">`,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html><head></head><body>
<main id="app" data-kitwork-scope="content: ''">
  <div id="html" data-kitwork-bind="html: content"><div data-kitwork-component="html-probe: $old"><button>old</button></div></div>
  <pre id="text" data-kitwork-bind="text: content"></pre>
</main><pre id="result">waiting</pre>
<script>
window.__htmlExecuted = 0;
var mounted = 0, disposed = 0;
kitwork.component('html-probe', { init: function(context) {
  mounted++;
  context.listen(window, 'html-probe-event', function() { window.__htmlExecuted++; });
  context.cleanup(function() { disposed++; });
}});
window.addEventListener('load', function() {
  var result = document.getElementById('result');
  function check(value, message) { if (!value) throw new Error(message); }
  try {
    var target = document.getElementById('html');
    var state = kitwork.scopeFor(document.getElementById('app'));
    function update(value) { state.content = value; kitwork.render(); }
    check(mounted === 1 && disposed === 1, 'old component must mount then clean up once');
    check(!kitwork.components['html-probe'], 'old instance must leave the active registry immediately');
    check(state.$old === 0, 'old alias must be released');
    window.dispatchEvent(new Event('html-probe-event'));
    check(window.__htmlExecuted === 0, 'old listener must be removed');

    update('<strong>Hello</strong> <em>Kitwork</em>');
    check(target.querySelector('strong').textContent === 'Hello', 'allowed markup must render');
    check(document.getElementById('text').textContent === state.content, 'text binding must stay literal');
    var stable = target.firstChild;
    kitwork.render();
    check(target.firstChild === stable, 'unchanged HTML must retain its nodes');

    var attacks = ` + string(payloads) + `;
    attacks.forEach(function(source) {
      update(source);
      check(!target.querySelector('script,img,svg,math,iframe,object,embed,template,style,form,input,link'), 'unsafe tag survived');
      target.querySelectorAll('*').forEach(function(node) {
        node.getAttributeNames().forEach(function(name) {
          check(['title','href','rel'].indexOf(name) >= 0, 'unsafe attribute survived: ' + name);
        });
        if (node.hasAttribute('href')) check(/^https?:$/.test(new URL(node.getAttribute('href')).protocol), 'unsafe URL survived');
      });
      check(window.__htmlExecuted === 0, 'inserted HTML executed code');
    });
    check(mounted === 1, 'inserted directives must never mount a component');

    update('<a href="/safe" title="Safe">link</a>');
    check(target.querySelector('a').getAttribute('href') === location.origin + '/safe', 'relative URL must resolve safely');
    update('<b>${removeAll()}</b>');
    check(target.textContent === '${removeAll()}', 'HTML text must not be evaluated as a template');

    ['x'.repeat(65537), '<div>'.repeat(34) + 'x' + '</div>'.repeat(34), '<b>x</b>'.repeat(2100)].forEach(function(source) {
      var before = target.innerHTML, rejected = false;
      try { kitwork.write(target, 'html', source); } catch (_) { rejected = true; }
      check(rejected && target.innerHTML === before, 'limits must fail before replacing the old content');
    });
    var rejectedHost = false;
    try { kitwork.write(document.createElement('script'), 'html', 'alert(1)'); } catch (_) { rejectedHost = true; }
    check(rejectedHost, 'an executable host must be refused');

    for (var i = 0; i < 100; i++) { update('<b>' + i + '</b>'); update(''); }
    check(target.childNodes.length === 0 && disposed === 1, 'repeated updates must not repeat old cleanup');
    update(null);
    check(target.childNodes.length === 0, 'null must clear content');
    result.textContent = 'passed';
  } catch (error) { result.textContent = 'FAILED: ' + error.message; }
});
</script></body></html>`
	delivered := deliverTestPage(t, page, nil)
	var unexpected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delivered.serveFiles(w, r) {
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(delivered.html))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/forbidden-") {
			unexpected.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	args := []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage", "--disable-background-networking",
		"--no-first-run", "--user-data-dir=" + filepath.Join(t.TempDir(), "browser"), "--virtual-time-budget=3000", "--dump-dom", server.URL}
	if runtime.GOOS == "windows" {
		args = append([]string{"--disable-background-mode", "--disable-breakpad", "--disable-crashpad-for-testing"}, args...)
	}
	output, commandErr := exec.CommandContext(ctx, browser, args...).CombinedOutput()
	if !strings.Contains(string(output), `<pre id="result">passed</pre>`) {
		t.Fatalf("HTML binding browser proof failed (%v):\n%s", commandErr, output)
	}
	if count := unexpected.Load(); count != 0 {
		t.Fatal(fmt.Sprintf("inert HTML parsing requested %d forbidden resources", count))
	}
}
