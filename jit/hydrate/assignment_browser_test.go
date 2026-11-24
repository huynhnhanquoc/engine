package hydrate

import (
	"html"
	"strings"
	"testing"
)

func TestBrowserGeneralAssignmentUpdatesView(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a browser")
	}
	browser := findHeadlessBrowserForDrive()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	page := `<!doctype html><html><head><meta charset="utf-8"></head><body>
<section id="host" data-kitwork-scope="user: { name: 'An' }; items: [{ done: false }]">
<b id="name" data-kitwork-bind="text: user.name"></b>
<i id="done" data-kitwork-bind="text: items[0].done"></i>
<button id="go" data-kitwork-event="click: user.name = 'Binh'">Set name</button>
<button id="toggle" data-kitwork-event="click: items[0].done = true">Set done</button>
</section><pre id="result"></pre>
<script src="/kit/kernel.js" defer></script>
<script>window.addEventListener('load',function(){
  var kitwork=window.kitwork, report=document.getElementById('result');
  function check(){
    try {
      if(document.getElementById('name').textContent!=='An')throw new Error('first paint');
      document.getElementById('go').click();
      document.getElementById('toggle').click();
      if(document.getElementById('name').textContent!=='Binh'||document.getElementById('done').textContent!=='true')throw new Error('event paint');
      kitwork.run(kitwork.compile('user.name = "Chi"'),kitwork.scopeFor(document.getElementById('host')));
      Promise.resolve().then(function(){
        if(document.getElementById('name').textContent!=='Chi')throw new Error('scheduled paint');
        report.textContent='ok';
      }).catch(function(e){report.textContent=String(e);});
    } catch(e) { report.textContent=String(e); }
  }
  setTimeout(check,20);
});</script></body></html>`
	output := runGroupedBrowserPage(t, browser, map[string]string{"/": page, "/kit/kernel.js": KernelFile(false)}, "1500")
	match := groupedResultRe.FindSubmatch(output)
	if match == nil || html.UnescapeString(string(match[1])) != "ok" {
		t.Fatalf("browser assignment failed:\n%s", strings.TrimSpace(string(output)))
	}
}
