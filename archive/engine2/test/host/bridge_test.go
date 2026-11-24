package host_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bridgeImport is the imports of a router.work of pages whose works keep values.
const bridgeImport = routerImport + "import { memory } from \"kitwork/storage\";\n"

// bridgeFiles are an app whose pages have works the browser calls (docs/view.md mục 4): a list kept in memory, a
// page behind a guard, one behind a guard that refuses, one under a limit, a counter many call at once.
func bridgeFiles() map[string]string {
	site := "apps/shop/shop.test/"
	page := "<content><text>{{ left }}</text></content>\n"
	return map[string]string{
		"application.work": `import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.test", port: 8080, allowLocal: true });
app.run();
`,
		site + "todo/router.work": bridgeImport + `const list = memory("todo").segment("list");
const lastId = memory("todo").segment("lastId");

router.view((context) => {
  var title = "";
  var count = 0;
  var picked = null;
  const todos = list.get() ?? [];
  const left = todos.filter((todo) => !todo.done).length;
  const add = () => {
    const text = title.trim();
    if (text === "") {
      return;
    }
    const id = (lastId.get() ?? 0) + 1;
    lastId.set(id);
    list.set(todos.concat([{ id: id, title: text, done: false }]));
    title = "";
  };
  const toggle = (id, done) => {
    list.set(todos.map((todo) => todo.id === id ? { id: todo.id, title: todo.title, done: done } : todo));
  };
  const bump = () => {
    count = count + 1;
  };
  const pick = (id) => {
    picked = id;
  };
  const broken = () => error("hỏng ở tầng dữ liệu");
  const typed = () => todos.nothing.at(1);
  const refuse = () => context.status(409).error("Đã có người sửa");
  const gone = () => context.notFound();
  const away = () => context.redirect("/login");
  const wrong = () => context.json({ a: 1 });
  const signIn = () => {
    context.cookie("user", "quoc");
  };
  const user = context.cookie("user") ?? "";
  return context.title("Việc").sync({ todos, left, add, toggle, broken, typed }).bind({ title, count, picked, user, bump, pick, refuse, gone, away, wrong, signIn });
});
`,
		site + "todo/content.ui": `<content gap="4">
  <form gap="3" submit="add()">
    <input value="{{ title }}" placeholder="Việc cần làm…" />
    <button type="submit" disabled="{{ add.pending }}">Thêm</button>
  </form>
  <text show="add.error" color="danger">{{ add.error?.message }}</text>
  <text if="todos.length === 0">Chưa có việc nào.</text>
  <list gap="2">
    <item for="todo of todos; key: todo.id" flex="row" gap="3">
      <input type="checkbox" checked="{{ todo.done }}" change="toggle(todo.id, !todo.done)" disabled="{{ toggle.pending }}" />
      <text color="{{ todo.done ? 'ink-30' : 'ink' }}">{{ todo.title }}</text>
    </item>
  </list>
  <list><item for="todo, i of todos; key: todo.id"><text>{{ i }}</text></item></list>
  <text>{{ left }}</text>
  <text>Còn {{ left }} việc, {{ count }} lần bấm.</text>
  <form submit:once="bump()"><button type="submit">B</button></form>
  <form submit:prevent="bump()"><button type="submit">C</button></form>
  <button disabled="false">X</button>
</content>
`,
		site + "plain/content.ui": page,
		site + "guarded/router.work": bridgeImport + `router.guard((context) => context.cookie("user") === "quoc" ? context.next() : context.redirect("/login")).view((context) => {
  const left = 1;
  const signOut = () => {
    context.cookie("user", null);
  };
  const stay = () => {
    context.cookie("seen", "1");
  };
  return context.bind({ left, signOut, stay });
});
`,
		site + "guarded/content.ui": page,
		site + "refused/router.work": bridgeImport + `router.guard((context) => context.cookie("why", "no").error("Không cho")).view((context) => {
  const left = 1;
  const go = () => {};
  return context.bind({ left, go });
});
`,
		site + "refused/content.ui": page,
		site + "branch/config.work": routerImport + "router.guard((context) => context.cookie(\"pass\") === \"1\" ? context.next() : context.error(\"Cửa GET\")).methods(\"get\");\n",
		site + "branch/router.work": bridgeImport + `router.view((context) => {
  const left = 1;
  const go = () => {};
  return context.bind({ left, go });
});
`,
		site + "branch/content.ui": page,
		site + "limited/router.work": bridgeImport + `router.limit({ rate: 2, per: "1m", by: "ip" }).view((context) => {
  const left = 1;
  const go = () => {};
  return context.bind({ left, go });
});
`,
		site + "limited/content.ui": page,
		site + "counter/router.work": bridgeImport + `const total = memory("counter").segment("n");

router.view((context) => {
  const left = total.get() ?? 0;
  const add = () => {
    total.set(left + 1);
  };
  return context.bind({ left, add });
});
`,
		site + "counter/content.ui": page,
		site + "away/router.work": bridgeImport + `router.view((context) => {
  if (context.cookie("user") !== "quoc") {
    return context.redirect("/login");
  }
  const go = () => {};
  return context.bind({ go });
});
`,
		site + "away/content.ui":     "<content />\n",
		site + "printed/router.work": bridgeImport + "router.view((context) => context.bind({ left: 1 }));\n",
		site + "printed/content.ui":  page,
		site + "components.work": componentImport + `component.define("account-menu", (context) => {
  var opened = false;
  const name = context.cookie("user") === "quoc" ? "Quốc" : "";
  const toggle = () => {
    opened = !opened;
  };
  const signIn = () => {
    context.cookie("user", "quoc");
    opened = false;
  };
  const signOut = () => {
    context.cookie("user", null);
    opened = false;
  };
  const fail = () => error("hỏng");
  const leave = () => context.redirect("/login");
  const deny = () => context.status(409).error("Không");
  const wrong = () => context.json({});
  return context.bind({ opened, name, toggle, signIn, signOut, fail, leave, deny, wrong });
});
component.define("count-badge", (context) => {
  const { start = 0 } = context.attributes();
  var count = start;
  const add = () => {
    count = count + start;
  };
  return context.bind({ count, add });
});
component.define("plain-badge", (context) => context.bind({ word: "chữ" }));
`,
		site + "members/view.ui": "<view><account-menu /><content /></view>" + "\n",
		site + "members/account-menu.ui": `<account-menu flex="row" gap="2">
  <button show="name == ''" click="signIn()" disabled="{{ signIn.pending }}">Đăng nhập thử</button>
  <button show="name != ''" click="toggle()"><text>{{ name }}</text></button>
  <button show="opened && name != ''" click="signOut()">Đăng xuất</button>
</account-menu>
`,
		site + "members/content.ui": `<content><plain-badge><text>{{ word }}</text><button click="nothing()">n</button></plain-badge>
  <list><item for="n of [1, 2]"><count-badge start="{{ n }}"><button click="add()">{{ count }}</button></count-badge></item></list>
</content>
`,
		site + "members/live/router.work": bridgeImport + `router.view((context) => {
  var seen = 0;
  const user = context.request.cookies.user ?? "none";
  const note = "<p>ghi chú</p>";
  const look = () => {
    seen = seen + 1;
  };
  return context.bind({ seen, user, note, look });
});
`,
		site + "members/live/content.ui": "<content><text>{{ user }}</text><richtext value=\"{{ note }}\" /></content>\n",
		site + "members/guarded/router.work": bridgeImport + `router.guard((context) => context.cookie("user") === "quoc" ? context.next() : context.redirect("/login")).view((context) => {
  const go = () => {};
  return context.bind({ go });
});
`,
		site + "members/guarded/content.ui": "<content />\n",
		"apps/shop/config.work":             routerImport + "router.guard((context) => context.cookie(\"banned\") === \"1\" ? context.error(\"Bị chặn\") : context.next()).methods(\"*\");\n",
		site + "query/router.work": bridgeImport + `router.view((context) => {
  var seen = "";
  const look = () => {
    seen = (context.query("kitwork-call") ?? "none") + "/" + (context.query("x") ?? "");
  };
  return context.bind({ seen, look });
});
`,
		site + "query/content.ui":     page,
		site + "cookied/router.work":  bridgeImport + "router.view((context) => context.cookie(\"visit\", \"1\").bind({ left: 1, go: () => {} }));\n",
		site + "cookied/content.ui":   page,
		site + "getter/router.work":   bridgeImport + "router.get((context) => context.text(\"x\"));\n",
		site + "erring/router.work":   bridgeImport + "router.view((context) => context.status(503).error(\"Bảo trì\"));\n",
		site + "erring/content.ui":    page,
		site + "jsonview/router.work": bridgeImport + "router.view((context) => context.json({ a: 1 }));\n",
		site + "jsonview/content.ui":  page,
		site + "jsonguard/router.work": bridgeImport + `router.guard((context) => context.cookie("k") === "1" ? context.json({}) : context.status(401).json({ a: 1 })).view((context) => {
  const go = () => {};
  return context.bind({ go });
});
`,
		site + "jsonguard/content.ui": "<content />\n",
		site + "shrinking/router.work": bridgeImport + `router.view((context) => {
  var big = true;
  const toggle = () => {
    big = !big;
  };
  return big ? context.bind({ big, extra: 1, toggle }) : context.bind({ big, toggle });
});
`,
		site + "shrinking/content.ui":  "<content />\n",
		site + "badgelist/router.work": bridgeImport + "router.view((context) => context.bind({ items: [1, 2], go: () => {} }));\n",
		site + "badgelist/content.ui":  "<content><count-badge for=\"n of items\" start=\"{{ n }}\"><text>{{ count }}</text></count-badge></content>\n",
		site + "popover/router.work":   bridgeImport + "router.view((context) => context.bind({ open: true, go: () => {} }));\n",
		site + "popover/content.ui":    "<content><popover><text>x</text></popover></content>\n",
		site + "editing/router.work":   bridgeImport + "router.view((context) => context.bind({ post: \"chữ của bài\", go: () => {} }));\n",
		site + "editing/content.ui":    "<content><form method=\"post\"><field><label>Post</label><editor><textarea name=\"body\" value=\"{{ post }}\" alt=\"Body\" /></editor></field></form></content>\n",
		site + "nomethod/router.work":  bridgeImport + "router.view((context) => context.nothing(\"a\", 1, () => 1));\n",
		site + "nomethod/content.ui":   "<content />\n",
		site + "postonly/config.work":  routerImport + "router.guard((context) => context.error(\"Cửa POST\")).methods(\"post\");\n",
		site + "postonly/router.work":  bridgeImport + "router.view((context) => context.bind({ left: 1, go: () => {} }));\n",
		site + "postonly/content.ui":   page,
		site + "nulllist/router.work":  bridgeImport + "router.view((context) => context.bind({ items: null, go: () => {} }));\n",
		site + "nulllist/content.ui":   "<content><list><item for=\"x of items\"><text>{{ x }}</text></item></list></content>\n",
		site + "config.work":           routerImport + "router.reload(true);\n",
	}
}

// call posts a call of work to path with body, from the page's own origin.
func (b *booted) call(t testing.TB, path, work, body string, headers ...string) reply {
	t.Helper()
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return b.request(t, http.MethodPost, "shop.test", path+separator+"kitwork-call="+work, strings.NewReader(body),
		append([]string{"Content-Type", "application/json"}, headers...)...)
}

// answers checks a call's answer: its status and its body.
func answers(t testing.TB, what string, got reply, status int, body string) {
	t.Helper()
	if got.status != status || got.body != body {
		t.Errorf("%s: %d %s, want %d %s", what, got.status, got.body, status, body)
	}
	if got.status != 0 && got.header.Get("Content-Type") != "application/json; charset=utf-8" || got.header.Get("Cache-Control") != "no-store" {
		t.Errorf("%s: Content-Type %q, Cache-Control %q", what, got.header.Get("Content-Type"), got.header.Get("Cache-Control"))
	}
}

// A work of the page runs on what its router.view binds with the vars the page held, and the call answers the names
// the run after it changed (docs/view.md mục 4).
func TestAWorkOfThePageAnswersWhatItChanged(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	if got := b.get(t, "shop.test", "/todo"); got.status != http.StatusOK || !strings.Contains(got.body, `<p data-kitwork-bind="text: left">0</p>`) {
		t.Fatalf("GET /todo: %d\n%s\n%s", got.status, got.body, b.log.String())
	}
	answers(t, "add", b.call(t, "/todo", "add", `{"seq":1,"args":[],"page":{"title":"  Mua sữa ","count":0}}`), http.StatusOK,
		`{"seq":1,"page":{"left":1,"title":"","todos":[{"id":1,"title":"Mua sữa","done":false}]}}`)
	answers(t, "add again", b.call(t, "/todo", "add", `{"seq":2,"page":{"title":"Rửa bát"}}`), http.StatusOK,
		`{"seq":2,"page":{"left":2,"title":"","todos":[{"id":1,"title":"Mua sữa","done":false},{"id":2,"title":"Rửa bát","done":false}]}}`)
	answers(t, "toggle", b.call(t, "/todo", "toggle", `{"seq":3,"args":[1,true],"page":{"title":"đang gõ"}}`), http.StatusOK,
		`{"seq":3,"page":{"left":1,"todos":[{"id":1,"title":"Mua sữa","done":true},{"id":2,"title":"Rửa bát","done":false}]}}`)
	// a work that changes a var answers that var; the page keeps the others it holds
	answers(t, "bump", b.call(t, "/todo", "bump", `{"seq":4,"page":{"title":"đang gõ","count":4}}`), http.StatusOK, `{"seq":4,"page":{"count":5}}`)
	answers(t, "a var that starts null takes any kind", b.call(t, "/todo", "pick", `{"seq":5,"args":[2],"page":{"picked":"x"}}`), http.StatusOK,
		`{"seq":5,"page":{"picked":2}}`)
	answers(t, "nothing changed", b.call(t, "/todo", "add", `{"seq":6,"page":{"title":""}}`), http.StatusOK, `{"seq":6}`)
	answers(t, "no body", b.call(t, "/todo", "bump", ``), http.StatusOK, `{"seq":0,"page":{"count":1}}`)
	answers(t, "null for a var", b.call(t, "/todo", "bump", `{"seq":7,"page":{"title":null,"count":null}}`), http.StatusOK, `{"seq":7,"page":{"count":1}}`)
	answers(t, "a var the page does not have is left", b.call(t, "/todo", "bump", `{"seq":8,"page":{"nothing":1,"count":1}}`), http.StatusOK,
		`{"seq":8,"page":{"count":2}}`)
	// the rest of the query is the page's own
	answers(t, "the page's query", b.call(t, "/todo?x=1", "bump", `{"seq":9}`), http.StatusOK, `{"seq":9,"page":{"count":1}}`)
	// a cookie the work sets goes with the answer, and the run after it reads it as the browser sends it next
	signed := b.call(t, "/todo", "signIn", `{"seq":10}`, "Cookie", "theme=dark")
	answers(t, "signIn", signed, http.StatusOK, `{"seq":10,"page":{"user":"quoc"}}`)
	if got := signed.header.Get("Set-Cookie"); got != "user=quoc; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("signIn: Set-Cookie %q", got)
	}
	if got := b.get(t, "shop.test", "/todo"); !strings.Contains(got.body, `<p data-kitwork-bind="text: left">1</p>`) {
		t.Errorf("GET /todo after the works: %s", got.body)
	}
}

// What a call is refused with, and what a work that fails or answers answers (docs/view.md mục 4.4, 4.5, language.md
// mục 5.3): { seq, error: { code, message } }, the status as the code, the message public, the reason in the log.
func TestACallRefusedOrFailedAnswersItsError(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	error := func(code int, message string) string {
		return fmt.Sprintf(`{"code":%d,"message":%q}`, code, message)
	}
	answers(t, "error(…)", b.call(t, "/todo", "broken", `{"seq":1}`), 500, `{"seq":1,"error":`+error(500, "Internal Server Error")+`}`)
	answers(t, "a TYPE failure", b.call(t, "/todo", "typed", `{"seq":2}`), 500, `{"seq":2,"error":`+error(500, "Internal Server Error")+`}`)
	answers(t, "context.error", b.call(t, "/todo", "refuse", `{"seq":3}`), 409, `{"seq":3,"error":{"code":409,"message":"Đã có người sửa"}}`)
	answers(t, "context.notFound", b.call(t, "/todo", "gone", `{"seq":4}`), 404, `{"seq":4,"error":`+error(404, "Not Found")+`}`)
	answers(t, "context.redirect", b.call(t, "/todo", "away", `{"seq":5}`), 200, `{"seq":5,"redirect":"/login"}`)
	answers(t, "context.json", b.call(t, "/todo", "wrong", `{"seq":6}`), 500, `{"seq":6,"error":`+error(500, "Internal Server Error")+`}`)
	answers(t, "a work the page does not bind", b.call(t, "/todo", "nothing", `{"seq":7}`), 404, `{"seq":7,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a name that is data", b.call(t, "/todo", "left", `{"seq":8}`), 404, `{"seq":8,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a var of another kind", b.call(t, "/todo", "bump", `{"seq":9,"page":{"title":5}}`), 400, `{"seq":9,"error":`+error(400, "Bad Request")+`}`)
	for what, body := range map[string]string{
		"not JSON":         `{"seq":`,
		"not an object":    `[1]`,
		"seq below 0":      `{"seq":-1}`,
		"seq not whole":    `{"seq":1.5}`,
		"seq a string":     `{"seq":"1"}`,
		"args not a list":  `{"args":{}}`,
		"page not a whole": `{"page":[]}`,
	} {
		answers(t, what, b.call(t, "/todo", "bump", body), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	}
	answers(t, "a component's work on no instance", b.call(t, "/todo", "shop-cart.checkout", `{}`), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	answers(t, "a name of no work", b.call(t, "/todo", "a-b", `{}`), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	answers(t, "a page of router.get", b.call(t, "/getter", "go", `{}`), 404, `{"seq":0,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a component on a page of router.get", b.call(t, "/getter", "account-menu.toggle", `{"at":"c1"}`), 404, `{"seq":0,"error":`+error(404, "Not Found")+`}`)
	// the page's query is the page's own: the work's name is taken out of it
	answers(t, "the query", b.call(t, "/query?x=1", "look", `{"seq":12}`), 200, `{"seq":12,"page":{"seen":"/1"}}`)
	// a cookie the page's function sets goes with the answer of a call, as with the page
	if got := b.call(t, "/cookied", "go", `{}`).header.Values("Set-Cookie"); len(got) != 2 || got[0] != got[1] || got[0] != "visit=1; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("a cookie of the page's function: %q", got)
	}
	// a name the run after the work does not bind is null
	answers(t, "a name gone", b.call(t, "/shrinking", "toggle", `{"seq":13,"page":{"big":true}}`), 200, `{"seq":13,"page":{"big":false,"extra":null}}`)
	answers(t, "a body too large", b.call(t, "/todo", "bump", `{"page":{"title":"`+strings.Repeat("x", 1<<20)+`"}}`), 400,
		`{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	got := b.request(t, http.MethodGet, "shop.test", "/todo?kitwork-call=bump", nil)
	answers(t, "a GET", got, 405, `{"seq":0,"error":`+error(405, "Method Not Allowed")+`}`)
	if got.header.Get("Allow") != "POST" {
		t.Errorf("a GET call: Allow %q", got.header.Get("Allow"))
	}
	answers(t, "another site's page", b.call(t, "/todo", "bump", `{}`, "Sec-Fetch-Site", "cross-site"), 403, `{"seq":0,"error":`+error(403, "Forbidden")+`}`)
	answers(t, "a page with no router.view", b.call(t, "/plain", "bump", `{}`), 404, `{"seq":0,"error":`+error(404, "Not Found")+`}`)
	answers(t, "no page", b.call(t, "/nowhere", "bump", `{}`), 404, `{"seq":0,"error":`+error(404, "Not Found")+`}`)
	contains(t, "the log", b.log.String(),
		"POST /todo (/todo/router.work, work broken): 500: FAIL: ",
		"hỏng ở tầng dữ liệu",
		"(/todo/router.work, work typed): 500: TYPE",
		"the work answered with context.json(…)",
		"(/todo/router.work, router.view): 404: the page binds no work nothing",
		"(/todo/router.work, router.view): 404: the page binds no work left",
		"400: INVALID_INPUT: ",
		"var title is a string and the call sent a number",
		"(kitwork-call): 400: a work of shop-cart is called on an instance: at names its place",
		"(kitwork-call): 400: seq is the call's number",
		"(kitwork-call): 400: args are the work's arguments",
		"(kitwork-call): 400: page holds the vars of the page",
		"(kitwork-call): 400: the call's body is no JSON object",
		"(kitwork-call): 400: the call's body is over 1048576 bytes",
		"(kitwork-call): 400: kitwork-call=\"a-b\" names no work")
}

// A call meets the rules of its page (docs/view.md mục 4.2, 4.4): the guards of the folders and of router.view, a
// limit counting calls with the page; after the work, the guards again with the cookies it set, so signing out on a
// page that wants a signed in visitor sends the page where the guard sends it.
func TestACallMeetsThePagesRules(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	answers(t, "a guard that sends away", b.call(t, "/guarded", "stay", `{"seq":1}`), 200, `{"seq":1,"redirect":"/login"}`)
	stayed := b.call(t, "/guarded", "stay", `{"seq":2}`, "Cookie", "user=quoc")
	answers(t, "a work the guard lets on", stayed, 200, `{"seq":2}`)
	if got := stayed.header.Get("Set-Cookie"); got != "seen=1; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("stay: Set-Cookie %q", got)
	}
	out := b.call(t, "/guarded", "signOut", `{"seq":3}`, "Cookie", "user=quoc")
	answers(t, "signing out", out, 200, `{"seq":3,"redirect":"/login"}`)
	if got := out.header.Get("Set-Cookie"); got != "user=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" {
		t.Errorf("signOut: Set-Cookie %q", got)
	}
	refused := b.call(t, "/refused", "go", `{"seq":4}`)
	answers(t, "a guard that refuses", refused, 403, `{"seq":4,"error":{"code":403,"message":"Không cho"}}`)
	if got := refused.header.Get("Set-Cookie"); got != "why=no; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("a cookie the refusing guard set: %q", got)
	}
	answers(t, "a guard of the app", b.call(t, "/todo", "bump", `{"seq":40}`, "Cookie", "banned=1"), 403, `{"seq":40,"error":{"code":403,"message":"Bị chặn"}}`)
	answers(t, "a guard that answers 200", b.call(t, "/jsonguard", "go", `{"seq":41}`, "Cookie", "k=1"), 403, `{"seq":41,"error":{"code":403,"message":"Forbidden"}}`)
	answers(t, "a guard that answers JSON", b.call(t, "/jsonguard", "go", `{"seq":42}`), 401, `{"seq":42,"error":{"code":401,"message":"Unauthorized"}}`)
	answers(t, "a guard of the folder's GET", b.call(t, "/branch", "go", `{"seq":5}`), 403, `{"seq":5,"error":{"code":403,"message":"Cửa GET"}}`)
	answers(t, "past the guard of the folder's GET", b.call(t, "/branch", "go", `{"seq":6}`, "Cookie", "pass=1"), 200, `{"seq":6}`)
	answers(t, "a guard of the folder's POST", b.call(t, "/postonly", "go", `{"seq":43}`), 403, `{"seq":43,"error":{"code":403,"message":"Cửa POST"}}`)
	// the limit counts the page and its calls together
	if got := b.get(t, "shop.test", "/limited"); got.status != 200 {
		t.Fatalf("GET /limited: %d", got.status)
	}
	answers(t, "a call within the limit", b.call(t, "/limited", "go", `{"seq":7}`), 200, `{"seq":7}`)
	limited := b.call(t, "/limited", "go", `{"seq":8}`)
	answers(t, "a call over the limit", limited, 429, `{"seq":8,"error":{"code":429,"message":"Too Many Requests"}}`)
	if limited.header.Get("Retry-After") == "" {
		t.Errorf("a call over the limit has no Retry-After")
	}
	// a page whose function sends away answers that, the work never run; one that answers an error, that error
	answers(t, "a page that sends away", b.call(t, "/away", "go", `{"seq":9}`), 200, `{"seq":9,"redirect":"/login"}`)
	answers(t, "a page that answers an error", b.call(t, "/erring", "go", `{"seq":10}`), 503, `{"seq":10,"error":{"code":503,"message":"Bảo trì"}}`)
	answers(t, "a page that answers JSON", b.call(t, "/jsonview", "go", `{"seq":11}`), 500, `{"seq":11,"error":{"code":500,"message":"Internal Server Error"}}`)
	contains(t, "the log", b.log.String(), "(/jsonview/router.work, router.view): 500: router.view returned ")
}

// The calls of one page run one at a time, from the run before the work to the run after it: every add of many sent
// at once is kept, though each writes what it read before plus one.
func TestTheCallsOfAPageRunOneAtATime(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	const calls = 40
	var wait sync.WaitGroup
	for i := 0; i < calls; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if got := b.call(t, "/counter", "add", `{}`); got.status != 200 {
				t.Errorf("add: %d %s", got.status, got.body)
			}
		}()
	}
	wait.Wait()
	if got := b.get(t, "shop.test", "/counter"); !strings.Contains(got.body, fmt.Sprintf(`<p data-kitwork-bind="text: left">%d</p>`, calls)) {
		t.Errorf("after %d adds at once: %s", calls, got.body)
	}
}

// escaped is text as the page's data block writes it: <, > and & as JSON's escapes, so no text closes the <script>.
func escaped(text string) string {
	escape := func(code string) string { return string(rune(92)) + "u00" + code }
	return strings.NewReplacer("<", escape("3c"), ">", escape("3e"), "&", escape("26")).Replace(text)
}

// A page whose router.view binds a work or a var runs in the browser (docs/view.md mục 3, 4): what it reads of its
// names is the kernel's to keep, its lists are drawn by key as the kernel adopts them, an if over its names is
// shown and hidden by the kernel, a form that calls a work stays on the page, and its data is written once at the
// end of its content for call.js. A page that binds neither stays as the server prints it.
func TestAPageWithWorksRunsInTheBrowser(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	b.call(t, "/todo", "add", `{"page":{"title":"Mua <sữa> & trà"}}`)
	b.call(t, "/todo", "add", `{"page":{"title":"Rửa bát"}}`)
	b.call(t, "/todo", "toggle", `{"args":[2,true]}`)
	got := b.get(t, "shop.test", "/todo")
	contains(t, "the page", got.body,
		`<form class="flex-column gap-3" data-kitwork-event="submit.prevent: add()">`,
		`<input value="" placeholder="Việc cần làm…" data-kitwork-bind="value: title">`,
		`type="submit" data-kitwork-bind="disabled: add.pending">Thêm</button>`,
		`<p class="color-danger" hidden data-kitwork-bind="show: add.error; text: add.error?.message"></p>`,
		`<p hidden data-kitwork-bind="show: todos.length === 0">Chưa có việc nào.</p>`,
		`<!--kitwork-for-start id=s1--><li class="flex-row gap-3" data-kitwork-for="todo of todos; key: todo.id" data-kitwork-template hidden>`,
		`<li class="flex-row gap-3" data-kitwork-item="s1" data-kitwork-key="1">`,
		`<input type="checkbox" data-kitwork-bind="checked: todo.done; disabled: toggle.pending" data-kitwork-event="change: toggle(todo.id, !todo.done)">`,
		`<li class="flex-row gap-3" data-kitwork-item="s1" data-kitwork-key="2">`,
		`<input type="checkbox" checked data-kitwork-bind="checked: todo.done; disabled: toggle.pending"`,
		`data-kitwork-bind="text: todo.title" data-kitwork-class="color-ink: (todo.done ? &#39;ink-30&#39; : &#39;ink&#39;) == &#39;ink&#39;; color-ink-30: (todo.done ? &#39;ink-30&#39; : &#39;ink&#39;) == &#39;ink-30&#39;">Mua &lt;sữa&gt; &amp; trà</p>`,
		`</li><!--kitwork-for-end-->`,
		`<li class="flex-column" data-kitwork-item="s2" data-kitwork-key="2"><p data-kitwork-bind="text: i">1</p></li>`,
		`data-kitwork-event="submit.once.prevent: bump()"`, `data-kitwork-event="submit.prevent: bump()"`, `type="button">X</button>`,
		`<p data-kitwork-bind="text: left">1</p>`,
		`<p data-kitwork-bind="text: [&#39;Còn &#39;, (left), &#39; việc, &#39;, (count), &#39; lần bấm.&#39;].join(&#39;&#39;)">Còn 1 việc, 0 lần bấm.</p>`,
		`<script type="application/json" data-kitwork-page>{"page":{"count":0,"left":1,"picked":null,"title":"","todos":[{"id":1,"title":"Mua `+escaped("<sữa> &")+` trà","done":false},{"id":2,"title":"Rửa bát","done":true}],"user":""},`+
			`"jobs":{"page":["add","toggle","broken","typed","bump","pick","refuse","gone","away","wrong","signIn"]},"vars":{"page":["title","count","picked"]},"tab":"`,
		`<script data-kitwork-generated="bridge" src="/_kitwork/call.`)
	if strings.Count(got.body, "data-kitwork-page") != 1 {
		t.Errorf("the page's data is written %d times", strings.Count(got.body, "data-kitwork-page"))
	}
	// call.js is served by its name, as the kernel is
	start := strings.Index(got.body, `src="/_kitwork/call.`) + len(`src="`)
	file := got.body[start : start+strings.Index(got.body[start:], `"`)]
	if served := b.get(t, "shop.test", file); served.status != http.StatusOK || !strings.Contains(served.body, "kitwork-call=") {
		t.Errorf("GET %s: %d", file, served.status)
	}
	if got := b.get(t, "shop.test", "/nulllist"); !strings.Contains(got.body, `<!--kitwork-for-start id=s1--><li class="flex-column" data-kitwork-for="x of items" data-kitwork-template hidden>`) {
		t.Errorf("a list of the page that is null: %s", got.body)
	}
	// a page whose bind holds no work and no var is printed: no kernel, no data
	printed := b.get(t, "shop.test", "/printed")
	if printed.status != http.StatusOK || !strings.Contains(printed.body, "<main class=\"flex-column\"><p>1</p></main>") ||
		strings.Contains(printed.body, "data-kitwork-page") || strings.Contains(printed.body, "/_kitwork/call.") {
		t.Errorf("GET /printed: %d\n%s", printed.status, printed.body)
	}
}

// placesOf are the places of the instances of tag a page draws, in order.
func placesOf(body, tag string) []string {
	var places []string
	marker := `data-kitwork-component="` + tag + `" data-kitwork-at="`
	for rest := body; ; {
		start := strings.Index(rest, marker)
		if start < 0 {
			return places
		}
		rest = rest[start+len(marker):]
		places = append(places, rest[:strings.Index(rest, `"`)])
	}
}

// A site component whose bind holds a work or a var runs in the browser, one instance per place it is called from
// (docs/component.md, docs/KITWORK_BRIDGE.md mục 3): its work is called on the page that draws it, any page, naming
// the instance's place; the page is drawn again on the server for the instance's inputs, and the answer is what
// changed of the instance and of the page, each under its place.
func TestAWorkOfAComponentRunsOnItsInstance(t *testing.T) {
	b := boot(t, filepath.Join(write(t, bridgeFiles()), "application.work"))
	page := b.get(t, "shop.test", "/members")
	menus, badges := placesOf(page.body, "account-menu"), placesOf(page.body, "count-badge")
	if len(menus) != 1 || len(badges) != 2 || badges[1] != badges[0]+"-2" {
		t.Fatalf("places: %v %v\n%s", menus, badges, page.body)
	}
	menu := menus[0]
	contains(t, "the page", page.body,
		`<script data-kitwork-generated="runtime" src="/_kitwork/kernel.`,
		`<div class="flex-row gap-2" data-kitwork-component="account-menu" data-kitwork-at="`+menu+`">`,
		`data-kitwork-bind="show: name == &#39;&#39;; disabled: signIn.pending" data-kitwork-event="click: signIn()">Đăng nhập thử</button>`,
		`<span data-kitwork-bind="text: name"></span>`,
		// a component that binds no work and no var is printed
		`<div class="flex-column"><p>chữ</p><button`,
		`<script type="application/json" data-kitwork-page>{"`+menu+`":{"name":"","opened":false},"`+badges[0]+`":{"count":1},"`+badges[1]+`":{"count":2},`+
			`"jobs":{"`+menu+`":["toggle","signIn","signOut","fail","leave","deny","wrong"],"`+badges[0]+`":["add"],"`+badges[1]+`":["add"]},`+
			`"vars":{"`+menu+`":["opened"],"`+badges[0]+`":["count"],"`+badges[1]+`":["count"]}}</script>`,
		`<script data-kitwork-generated="bridge" src="/_kitwork/call.`)
	answers(t, "toggle", b.call(t, "/members", "account-menu.toggle", `{"seq":1,"at":"`+menu+`","`+menu+`":{"opened":false}}`), 200,
		`{"seq":1,"`+menu+`":{"opened":true}}`)
	signed := b.call(t, "/members", "account-menu.signIn", `{"seq":2,"at":"`+menu+`","`+menu+`":{"opened":true}}`)
	answers(t, "signIn", signed, 200, `{"seq":2,"`+menu+`":{"name":"Quốc","opened":false}}`)
	if got := signed.header.Get("Set-Cookie"); got != "user=quoc; Path=/; HttpOnly; SameSite=Lax" {
		t.Errorf("signIn: Set-Cookie %q", got)
	}
	// each instance of one place is its own: its inputs read where it stands, never sent
	answers(t, "the first badge", b.call(t, "/members", "count-badge.add", `{"seq":3,"at":"`+badges[0]+`","`+badges[0]+`":{"count":7}}`), 200,
		`{"seq":3,"`+badges[0]+`":{"count":8}}`)
	answers(t, "the second badge", b.call(t, "/members", "count-badge.add", `{"seq":3,"at":"`+badges[1]+`","`+badges[1]+`":{"count":7}}`), 200,
		`{"seq":3,"`+badges[1]+`":{"count":9}}`)
	answers(t, "a tag of another place", b.call(t, "/members", "count-badge.add", `{"seq":3,"at":"`+menu+`"}`), 404, `{"seq":3,"error":{"code":404,"message":"Not Found"}}`)
	// on a page of router.view, what the work changed of the page comes too
	live := b.get(t, "shop.test", "/members/live")
	if placesOf(live.body, "account-menu")[0] != menu {
		t.Fatalf("the menu has another place on another page of the same frame: %v", placesOf(live.body, "account-menu"))
	}
	answers(t, "signIn on a page that reads the cookie", b.call(t, "/members/live", "account-menu.signIn", `{"seq":4,"at":"`+menu+`","page":{"seen":3}}`), 200,
		`{"seq":4,"page":{"user":"quoc"},"`+menu+`":{"name":"Quốc"}}`)
	// a cookie the work removes is gone for the run after it, not empty
	answers(t, "signOut", b.call(t, "/members/live", "account-menu.signOut", `{"seq":5,"at":"`+menu+`","page":{"seen":3},"`+menu+`":{"opened":true}}`, "Cookie", "user=quoc"), 200,
		`{"seq":5,"page":{"user":"none"},"`+menu+`":{"name":"","opened":false}}`)
	error := func(code int, message string) string { return fmt.Sprintf(`{"code":%d,"message":%q}`, code, message) }
	answers(t, "a place the page does not draw", b.call(t, "/members", "account-menu.toggle", `{"seq":6,"at":"c0"}`), 404, `{"seq":6,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a tag the site does not define", b.call(t, "/members", "shop-cart.add", `{"seq":7,"at":"`+menu+`"}`), 404, `{"seq":7,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a work the instance does not bind", b.call(t, "/members", "account-menu.nothing", `{"seq":8,"at":"`+menu+`"}`), 404, `{"seq":8,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a data name of the instance", b.call(t, "/members", "account-menu.name", `{"seq":9,"at":"`+menu+`"}`), 404, `{"seq":9,"error":`+error(404, "Not Found")+`}`)
	answers(t, "a work that fails", b.call(t, "/members", "account-menu.fail", `{"seq":10,"at":"`+menu+`"}`), 500, `{"seq":10,"error":`+error(500, "Internal Server Error")+`}`)
	answers(t, "a var of another kind", b.call(t, "/members", "account-menu.toggle", `{"seq":11,"at":"`+menu+`","`+menu+`":{"opened":"yes"}}`), 400,
		`{"seq":11,"error":`+error(400, "Bad Request")+`}`)
	answers(t, "a work that sends away", b.call(t, "/members", "account-menu.leave", `{"seq":12,"at":"`+menu+`"}`), 200, `{"seq":12,"redirect":"/login"}`)
	answers(t, "a work that refuses", b.call(t, "/members", "account-menu.deny", `{"seq":13,"at":"`+menu+`"}`), 409, `{"seq":13,"error":{"code":409,"message":"Không"}}`)
	answers(t, "a work that answers JSON", b.call(t, "/members", "account-menu.wrong", `{"seq":14,"at":"`+menu+`"}`), 500, `{"seq":14,"error":`+error(500, "Internal Server Error")+`}`)
	// after the work the page's guards run again with the cookies it set: signing out where a visitor must be signed
	// in sends the page away
	out := b.call(t, "/members/guarded", "account-menu.signOut", `{"seq":15,"at":"`+menu+`"}`, "Cookie", "user=quoc")
	answers(t, "signing out behind a guard", out, 200, `{"seq":15,"redirect":"/login"}`)
	if got := out.header.Get("Set-Cookie"); got != "user=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" {
		t.Errorf("signOut behind a guard: Set-Cookie %q", got)
	}
	for what, body := range map[string]string{
		"no place":          `{"seq":0}`,
		"no body":           ``,
		"a place of no c":   `{"at":"x1"}`,
		"a place of caps":   `{"at":"cAB"}`,
		"a count of no n":   `{"at":"c1-"}`,
		"a count of a word": `{"at":"c1-x"}`,
		"vars not a whole":  `{"at":"` + menu + `","` + menu + `":[1]}`,
	} {
		answers(t, what, b.call(t, "/members", "account-menu.toggle", body), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	}
	for _, name := range []string{"menu.toggle", "Account-menu.toggle", "1account-menu.toggle", "account-Menu.toggle", "account-menu.a-b"} {
		answers(t, name, b.call(t, "/members", name, `{"at":"`+menu+`"}`), 400, `{"seq":0,"error":`+error(400, "Bad Request")+`}`)
	}
	// a work of the page needs router.view; a component's any page
	answers(t, "a work of a page with no router.view", b.call(t, "/members", "look", `{}`), 404, `{"seq":0,"error":`+error(404, "Not Found")+`}`)
	contains(t, "the log", b.log.String(),
		"(/members/router.work, <account-menu> at c0): 404: the page draws no <account-menu> at c0",
		"(/members/router.work, <count-badge> at "+menu+"): 404: the page draws no <count-badge> at "+menu,
		"(/members/router.work, <shop-cart> at "+menu+"): 404: the site defines no component shop-cart",
		"(/members/router.work, <account-menu> at "+menu+"): 404: the instance binds no work nothing",
		"(/members/router.work, <account-menu> at "+menu+", work fail): 500: FAIL: ",
		"var opened is a boolean and the call sent a string",
		"a work of account-menu is called on an instance: at names its place",
		"the work answered with context.json(…): a work changes the instance's data")
	// a page that runs in the browser reads its data in what the server draws: a richtext, an editor's area
	contains(t, "a richtext of the page's data", b.get(t, "shop.test", "/members/live").body, "<p>ghi chú</p>")
	if got := b.get(t, "shop.test", "/editing"); got.status != 200 || !strings.Contains(got.body, "chữ của bài") {
		t.Errorf("an editor of the page's data: %d\n%s", got.status, b.log.String())
	}
	// what a page in the browser still refuses: a list of a site component, a popover outside its component, a
	// method the context does not have
	for path, says := range map[string]string{
		"/badgelist": "for trên <count-badge>: danh sách chạy ở trình duyệt chưa lặp một component của site",
		"/popover":   "popover hiện khi component chứa nó mở",
		"/nomethod":  "context has no method nothing",
	} {
		if got := b.get(t, "shop.test", path); got.status != 500 {
			t.Errorf("GET %s: %d, want 500", path, got.status)
		}
		contains(t, path+": the log", b.log.String(), says)
	}
}

// A row's key with no text is an error of the page that says which row (18/11/2026): a time, a function, an object
// whose toString fails gave "", one key for every row, rows the kernel could not tell apart.
//
// CONTROL (18/11/2026): with the key read as got.ToString().Text() again, the page was 200, both rows keyed "".
func TestARowKeyWithNoTextIsAnErrorOfThePage(t *testing.T) {
	files := bridgeFiles()
	site := "apps/shop/shop.test/"
	files[site+"timekey/router.work"] = bridgeImport + "router.view((context) => context.bind({ rows: [{ at: new Date(0) }, { at: new Date(1) }], go: () => {} }));\n"
	files[site+"timekey/content.ui"] = "<content><list><item for=\"row of rows; key: row.at\"><text>1</text></item></list></content>\n"
	b := boot(t, filepath.Join(write(t, files), "application.work"))
	if got := b.get(t, "shop.test", "/timekey"); got.status != http.StatusInternalServerError || strings.Contains(got.body, "data-kitwork-key") {
		t.Errorf("GET /timekey: %d\n%s", got.status, got.body)
	}
	contains(t, "the log", b.log.String(), "khoá row.at của hàng thứ 1 không thành chữ được: TYPE: một thời gian không đổi thành chữ")
}
