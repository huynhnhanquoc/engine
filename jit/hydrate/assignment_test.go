package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssignmentTargetsAndOrder(t *testing.T) {
	cases := []struct{ src, want string }{
		{"user.name = 'An'; user.name", `"An"`},
		{"user.profile.name = 'An'; user.profile.name", `"An"`},
		{"items[index].done = true; items[index].done", `true`},
		{"items[1] = 7; items.join(',')", `"1,7"`},
		{"items[4] = 7; items.join(',')", `"1,2,,,7"`},
		{"items.length = 1; items.join(',')", `"1"`},
		{"items.length = 4; items.join(',')", `"1,2,,"`},
		{"i = 0; items[i++] += 2; [items[0], i]", `[3,1]`},
		{"obj = { n: 1 }; alias = obj; obj.n++; alias.n", `2`},
		{"obj = { n: '5' }; old = obj.n++; [old, obj.n]", `[5,6]`},
		{"obj = { n: 5 }; old = --obj.n; [old, obj.n]", `[4,4]`},
		{"obj = { n: 8 }; obj.n -= 2; obj.n *= 3; obj.n /= 2; obj.n %= 4; obj.n", `1`},
		{"obj = { n: 'a' }; obj.n += 2; obj.n", `"a2"`},
		{"i = 0; obj = { n: false }; obj.n &&= i++; [obj.n, i]", `[false,0]`},
		{"i = 0; obj = { n: true }; obj.n ||= i++; [obj.n, i]", `[true,0]`},
		{"i = 0; obj = { n: 0 }; obj.n ??= i++; [obj.n, i]", `[0,0]`},
		{"i = 2; obj = { n: null }; obj.n ??= i++; [obj.n, i]", `[2,3]`},
		{"obj = {}; obj.n ??= 3; obj.n", `3`},
		{"obj = { n: false }; obj.n ||= 7; obj.n", `7`},
		{"obj = { n: true }; obj.n &&= 7; obj.n", `7`},
		{"i = 0; obj = { a: 1 }; key = () => i++ ? 'b' : 'a'; obj[key()] += 3; [i, obj.a]", `[1,4]`},
		{"i = 0; obj = { a: 1 }; receiver = () => i++ ? null : obj; receiver().a += 3; [i, obj.a]", `[1,4]`},
		{"$.answer = user.name = 'An'; $.answer", `"An"`},
		{"obj = { a: 1, b: 0 }; obj.a = obj.b = 3; [obj.a, obj.b]", `[3,3]`},
	}
	encoded, _ := json.Marshal(casesAsJSON(cases))
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			var scope map[string]any
			json.Unmarshal([]byte(`{"user":{"name":"","profile":{}},"items":[1,2],"index":0}`), &scope)
			// The object-row case needs an object rather than a numeric row.
			if strings.Contains(c.src, ".done") {
				scope["items"] = []any{map[string]any{}}
			}
			ir, err := Compile(c.src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Eval(ir, scope)
			if err != nil {
				t.Fatal(err)
			}
			if actual := canonicalJSON(t, Plain(got)); actual != c.want {
				t.Fatalf("got %s, want %s", actual, c.want)
			}
		})
	}
	runNodeDOMScript(t, "assignment-targets.js", `
var cases = `+string(encoded)+`;
cases.forEach(function (c) {
  var s = { user: { name: '', profile: {} }, items: [1,2], index: 0 };
  if (c.src.indexOf('.done') >= 0) s.items = [{}];
  var got = window.kitwork.run(window.kitwork.compile(c.src), s);
  if (JSON.stringify(got) !== c.want) throw new Error(c.src + ': got ' + JSON.stringify(got) + ', want ' + c.want);
});
`)
}

func casesAsJSON(cases []struct{ src, want string }) []map[string]string {
	out := make([]map[string]string, len(cases))
	for i, c := range cases {
		out[i] = map[string]string{"src": c.src, "want": c.want}
	}
	return out
}

func TestAssignmentInvalidTargets(t *testing.T) {
	sources := []string{"1 = 2", "(a + b) = 2", "$ = 1", "a?.b = 1", "a?.[k]++", "++a?.b", "a?.b.c += 1", "(a?.b) = 1"}
	for _, src := range sources {
		if _, err := Compile(src); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
	b, _ := json.Marshal(sources)
	runNodeDOMScript(t, "invalid-targets.js", `
`+string(b)+`.forEach(function (src) { var refused = false; try { window.kitwork.compile(src); } catch (_) { refused = true; } if (!refused) throw new Error('accepted ' + src); });
// An optional read inside a key does not make the outer target optional.
window.kitwork.compile('obj[other?.key] = 1');
window.kitwork.compile('(obj?.child).name = 1');
`)
}

func TestAssignmentWriteBoundaries(t *testing.T) {
	sources := []string{"obj['__proto__'] = 1", "obj['constructor'] = 1", "obj.prototype = 1", "obj = null; obj.x = 1", "obj = null; obj.x &&= 1", "obj = null; obj.x++", "obj = 3; obj.x = 1", "a = []; a[1048576] = 1", "a = []; a.length = -1", "a = []; a.length = 1.5", "a = []; a.foo = 1"}
	for _, src := range sources {
		ir, err := Compile(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Eval(ir, map[string]any{"obj": map[string]any{}}); err == nil {
			t.Errorf("accepted write %s", src)
		}
	}
	b, _ := json.Marshal(sources)
	runNodeDOMScript(t, "write-boundaries.js", `
`+string(b)+`.forEach(function (src) { var refused = false; try { window.kitwork.run(window.kitwork.compile(src), {obj:{}}); } catch (_) { refused = true; } if (!refused) throw new Error('accepted write ' + src); });
var gets=0, sets=0, obj={}; Object.defineProperty(obj,'n',{get:function(){gets++;return 1;},set:function(v){sets++;}});
window.kitwork.run(window.kitwork.compile('obj.n = 3'), {obj:obj});
if (gets !== 0 || sets !== 1) throw new Error('plain assignment read the old value');
var frozen=Object.freeze({n:1}), refused=false;
try { window.kitwork.run(window.kitwork.compile('obj.n = 2'), {obj:frozen}); } catch (_) { refused=true; }
if (!refused || frozen.n !== 1) throw new Error('read-only target was not refused');
`)
}

func TestAssignmentBindingIsReadOnly(t *testing.T) {
	for _, src := range []string{"user.name = 'An'", "items[index]++", "user.count += 1", "user.count ??= 1"} {
		ir, err := Compile(src)
		if err != nil {
			t.Fatal(err)
		}
		if firstWrite(ir) == "" {
			t.Errorf("missed mutation in %s", src)
		}
	}
}

func TestAssignmentPreservesHostCollectionOwnership(t *testing.T) {
	user := map[string]any{"profile": map[string]any{"name": "An"}}
	items := []any{float64(1), float64(2)}
	scope := map[string]any{"user": user, "items": items}
	ir, err := Compile("user.profile.name = 'Binh'; items[0] = 7; [user.profile.name, items[0]]")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Eval(ir, scope)
	if err != nil || canonicalJSON(t, Plain(got)) != `["Binh",7]` {
		t.Fatalf("result %v, error %v", got, err)
	}
	if user["profile"].(map[string]any)["name"] != "An" || items[0] != float64(1) {
		t.Fatal("assignment mutated the host's borrowed collections")
	}
}

func TestAssignmentDeepWriteSchedulesComponentPaint(t *testing.T) {
	runNodeDOMScript(t, "deep-write-paint.js", `
var kitwork=window.kitwork;
kitwork.component('deepwriter',{user:{name:'An'},items:[{done:false}]});
var host=el('div',{'data-kitwork-component':'deepwriter'});
var text=el('b',{'data-kitwork-bind':'text: user.name'});
var done=el('i',{'data-kitwork-bind':'text: items[0].done'});
host.appendChild(text); host.appendChild(done); document.body.appendChild(host);
kitwork.render();
if(text.textContent!=='An'||done.textContent!=='false')throw new Error('initial state');
kitwork.run(kitwork.compile('user.name = "Binh"; items[0].done = true'),kitwork.scopeFor(host));
Promise.resolve().then(function(){
  if(text.textContent!=='Binh'||done.textContent!=='true')throw new Error('deep write did not schedule paint');
}).catch(function(e){console.error(e);process.exit(1);});
`)
}

func TestAssignmentProgramMarksNestedWrites(t *testing.T) {
	runNodeDOMScript(t, "deep-write-program.js", programScript(callJS, `
var view=kitwork.page.view(), first=rows()[0], second=rows()[1];
program.g[2].e[0].x=P('todos[0].title = "Changed"');
fire('submit',form);
if(first.childNodes[1].textContent!=='Changed')throw new Error('page object mutation did not update its row');
if(rows()[0]!==first||rows()[1]!==second)throw new Error('mutation replaced stable rows');
program.g[6].e=[{n:'click',x:P('todo.title = "From row"')}];
fire('click',second.childNodes[1]);
if(second.childNodes[1].textContent!=='From row')throw new Error('row object mutation did not paint');
program.g[2].e[0].x=P('title = "Assigned"');
fire('submit',form);
if(input.value!=='Assigned')throw new Error('bare assignment did not mark page readers');
program.g[2].e[0].x=P('todos[0].id = 1001');
fire('submit',form);
if(rows()[0].getAttribute('data-kitwork-key')!=='1001')throw new Error('mutated key stayed cached');
var before=view.runs;
program.g[2].e[0].x=P('todos.forEach((todo) => todo.title = "Batch")');
fire('submit',form);
if(rows()[0].childNodes[1].textContent!=='Batch'||rows()[N-1].childNodes[1].textContent!=='Batch')throw new Error('lambda writes did not paint');
if(view.runs-before>N*10)throw new Error('batch writes repainted the same list repeatedly');
`))
}
