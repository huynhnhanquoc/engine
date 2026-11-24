// call.js — the browser's half of the bridge of router.view (docs/view.md mục 4, docs/tunnel.md mục 1; slice 2 of
// engine2). Moved from the first engine's jit/hydrate/call.js (28/10/2026) and cut to what the server answers today
// (host/bridge.go): no program of the page, no revisions, no stamp, no tickets, no tab told by another.
//
// A page whose router.view binds a work or a var runs in the browser as one instance, "page". The server writes its
// data once, as JSON at the end of its content (<script type="application/json" data-kitwork-page>, library/ui
// bridge.go): the names of its bind under "page", its works under "jobs", its vars under "vars". This file puts the
// data in the page's state (kitwork.scope.$), and each work there as a function of the same name, which the markup
// calls: remove(todo.id).
//
// A call is a POST to the page's own URL naming the work, ?kitwork-call=remove, with the vars the page holds as the
// call leaves: { seq, args, page: { title: "" } }. The answer is the names the work changed, never the page:
//
//   { "seq": 4, "page": { "left": 1, "todos": [ … ] } }
//
// each set straight into the page's state, which the kernel paints; { seq, redirect } sends the page where it says;
// { seq, error: { code, message } } is what work.error reads. Calls go one at a time, in the order made: the next
// leaves when the one before has answered, so an answer never lands on a page newer than the one it was computed
// from. While a call waits or is out, work.pending is true, read in a row of a list for the call made from that row,
// outside every row for any; work.error is the failed call's { code, message }, null once the work is called again.
// A call that fails changes nothing, and what the person did to the element that made it (a box ticked) goes back
// to the data.
//
// A site component the page draws whose bind holds a work or a var runs here too, one instance per place it is
// called from: its box is its host (data-kitwork-component="account-menu" data-kitwork-at="c3fq8w"), its data in the
// block under its place. Each such tag is registered with the kernel: its instance takes its data as it starts, and
// each work is a function that calls account-menu.signIn on the instance it runs in, naming its place and carrying
// its vars, { seq, args, at, page: { … }, c3fq8w: { … } }; the answer holds what changed of the page and of the
// instance, each under its place, written into each.
//
// A page whose page or instances bind names in sync holds them live (engine2/docs/view.md mục 2.3; host/tab.go): the
// block names the tab the server drew it for, and the tab listens on GET /_kitwork/changes?tab=…, which says only
// "changed" when a key its page read is written. The tab then asks for its page again, in the queue of its calls,
// POST <the page>?kitwork-refresh=1 with { seq, tab, page: { var… }, c3fq8w: { var… } }, and takes what changed of
// each instance as it takes a call's answer. A tab not seen closes its channel; seen again, or connected again after
// the channel broke, it asks once, for what it may have missed.
(function () {
  "use strict";
  var kitwork = window.kitwork;
  if (!kitwork || !kitwork.scope || window.__kitworkCall) return;
  window.__kitworkCall = true;

  var page = null;   // { jobs, vars, names } of the page drawn now, null on a page with none
  var epoch = 0;     // the page drawn now: an answer to a page Drive has left is dropped
  var seq = 0;       // the number of the last call sent
  var queue = [];    // calls waiting for the one in flight
  var flying = null; // the call in flight
  var track = {};    // work → { pending, error, rows: { row → { pending, error } } }
  var places = {};   // place → { data, jobs, vars } of each instance of a site component the page draws
  var registered = {}; // tag → the works registered for it
  var tab = "";      // the tab the page was drawn for, "" when it holds nothing live
  var channel = null; // the tab's channel while it listens
  var opened = false; // the channel opened once: an open after it is a connection again, which asks

  function acting() { return typeof kitwork.element === "function" ? kitwork.element() : null; }
  function get(name) { return kitwork.scope.$[name]; }
  function put(name, value) { kitwork.scope.$[name] = value; }
  function paint() { if (typeof kitwork.render === "function") kitwork.render(); }

  // the row of a list an element stands in, "" outside every row: the list's id and the row's key
  function rowOf(el) {
    var row = el && el.closest ? el.closest("[data-kitwork-item]") : null;
    return row ? row.getAttribute("data-kitwork-item") + ":" + row.getAttribute("data-kitwork-key") : "";
  }

  // work.pending and work.error, read where they are painted: in a row, that row's call; outside, any
  function read(name, field) {
    var t = track[name];
    if (!t) return field === "pending" ? false : null;
    var row = rowOf(acting());
    var at = row ? t.rows[row] : t;
    if (!at) return field === "pending" ? false : null;
    return field === "pending" ? at.pending > 0 : at.error;
  }

  function mark(call, on, error) {
    var key = call.at ? call.name + "@" + call.at : call.name;
    var t = track[key] || (track[key] = { pending: 0, error: null, rows: {} });
    var at = call.row ? (t.rows[call.row] || (t.rows[call.row] = { pending: 0, error: null })) : null;
    var step = on ? 1 : -1;
    t.pending += step;
    if (at) at.pending += step;
    if (on) {
      t.error = null;
      if (at) at.error = null;
    } else if (error) {
      t.error = error;
      if (at) at.error = error;
    }
  }

  // a work of the page: a function the markup calls, which calls the server
  function work(name) {
    var fn = function () {
      var el = acting();
      make({ name: name, args: Array.prototype.slice.call(arguments), row: rowOf(el), el: el, epoch: epoch });
    };
    Object.defineProperty(fn, "pending", { get: function () { return read(name, "pending"); } });
    Object.defineProperty(fn, "error", { get: function () { return read(name, "error"); } });
    return fn;
  }

  // the place of the instance around an element, "" outside every one
  function atOf(el) {
    var host = el && el.closest ? el.closest("[data-kitwork-at]") : null;
    return host ? host.getAttribute("data-kitwork-at") : "";
  }
  // the host of the instance a scope belongs to, null outside every one
  function hostOf(self) {
    try { return kitwork.context(self).host; } catch (e) { return null; }
  }
  function instanceOf(host) {
    var st = host && typeof kitwork.state === "function" ? kitwork.state(host) : null;
    return st && st.scope || null;
  }

  // a work of a site component: called on the instance whose scope it runs in; .pending and .error are that
  // instance's, read where the kernel paints
  function componentWork(tag, name) {
    var wire = tag + "." + name;
    var fn = function () {
      var host = hostOf(this), at = host ? host.getAttribute("data-kitwork-at") : "";
      if (!at) {
        if (window.console) console.error("kitwork: việc " + wire + " gọi ngoài một instance của " + tag);
        return;
      }
      var el = acting();
      make({ name: wire, at: at, host: host, args: Array.prototype.slice.call(arguments), row: rowOf(el), el: el, epoch: epoch });
    };
    Object.defineProperty(fn, "pending", { get: function () { return read(wire + "@" + atOf(acting()), "pending"); } });
    Object.defineProperty(fn, "error", { get: function () { return read(wire + "@" + atOf(acting()), "error"); } });
    return fn;
  }

  // each tag of the instances the page draws, registered with the kernel once with every work its instances have:
  // an instance takes its data, by its place, as it starts
  function register() {
    var works = {};
    document.querySelectorAll("[data-kitwork-at]").forEach(function (host) {
      var tag = host.getAttribute("data-kitwork-component"), own = places[host.getAttribute("data-kitwork-at")];
      if (!tag || !own) return;
      var list = works[tag] || (works[tag] = (registered[tag] || []).slice());
      own.jobs.forEach(function (name) { if (list.indexOf(name) < 0) list.push(name); });
    });
    Object.keys(works).forEach(function (tag) {
      if (registered[tag] && registered[tag].length === works[tag].length) return;
      registered[tag] = works[tag];
      var def = {
        init: function (context) {
          var own = places[context.host.getAttribute("data-kitwork-at")], self = this;
          if (!own) return;
          Object.keys(own.data).forEach(function (k) { self[k] = own.data[k]; });
        }
      };
      works[tag].forEach(function (name) { def[name] = componentWork(tag, name); });
      kitwork.component(tag, def);
    });
  }

  // the page's data block: the last one, a page drawn by Drive brings its own
  function block() {
    var all = document.querySelectorAll("script[data-kitwork-page]");
    var el = all.length ? all[all.length - 1] : null;
    if (!el) return null;
    try { return JSON.parse(el.textContent || "{}"); } catch (e) { return null; }
  }

  // the page's data and works in its state; the names of the page before it taken out
  function seed() {
    var s = kitwork.scope.$;
    if (page) page.names.forEach(function (k) { delete s[k]; });
    page = null;
    places = {};
    var data = block();
    var drawn = data && data.tab || "";
    if (drawn !== tab) {
      unlisten();
      tab = drawn;
    }
    listen();
    if (!data) return;
    var jobs = data.jobs || {}, vars = data.vars || {};
    Object.keys(data).forEach(function (at) {
      if (at === "page" || at === "jobs" || at === "vars") return;
      places[at] = { data: data[at] || {}, jobs: jobs[at] || [], vars: vars[at] || [] };
    });
    register();
    if (!data.page) return;
    page = { jobs: jobs.page || [], vars: vars.page || [], names: [] };
    Object.keys(data.page).forEach(function (k) { s[k] = data.page[k]; page.names.push(k); });
    page.jobs.forEach(function (name) { s[name] = work(name); page.names.push(name); });
  }

  // the tab asks for its page again, once at a time: an ask waiting in the queue stands for every change told
  function refresh() {
    if (!tab || queue.some(function (c) { return c.refresh; })) return;
    queue.push({ refresh: true, name: "", args: [], row: "", el: null, epoch: epoch });
    pump();
  }

  function listen() {
    if (!tab || channel || typeof EventSource !== "function" || document.visibilityState === "hidden") return;
    var source = channel = new EventSource("/_kitwork/changes?tab=" + encodeURIComponent(tab));
    source.addEventListener("changed", refresh);
    source.onopen = function () {
      if (opened) refresh(); // connected again: what changed meanwhile
      opened = true;
    };
    source.onerror = function () {
      if (source.readyState !== 2 || channel !== source) return; // the browser connects again by itself
      // refused (the server does not know the tab, or holds too many): asking makes the tab again
      unlisten();
      refresh();
      setTimeout(listen, 2000);
    };
  }
  function unlisten() {
    if (channel) channel.close();
    channel = null;
    opened = false;
  }
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") {
      unlisten();
      return;
    }
    listen();
    refresh();
  });

  function make(call) {
    mark(call, true);
    queue.push(call);
    pump();
  }

  function pump() {
    if (flying || !queue.length) return;
    var call = flying = queue.shift();
    if (call.epoch !== epoch) { flying = null; pump(); return; }
    call.seq = ++seq;
    // the vars the page holds, read as the call leaves
    var vars = {};
    if (page) page.vars.forEach(function (k) { vars[k] = get(k); });
    var body = { seq: call.seq, args: call.args, page: vars };
    if (tab) body.tab = tab;
    if (call.refresh) {
      // the vars each instance holds, by place
      delete body.args;
      Object.keys(places).forEach(function (at) {
        if (!places[at].vars.length) return;
        var host = document.querySelector('[data-kitwork-at="' + at + '"]'), instance = instanceOf(host) || {}, own = {};
        places[at].vars.forEach(function (k) { own[k] = instance[k]; });
        body[at] = own;
      });
    }
    if (call.at) {
      // the instance's vars; its inputs never go: the server reads them where it stands
      var own = {}, instance = instanceOf(call.host) || {}, held = places[call.at] ? places[call.at].vars : [];
      held.forEach(function (k) { own[k] = instance[k]; });
      body.at = call.at;
      body[call.at] = own;
    }
    send(call, body).then(function (answer) {
      settle(call, answer);
    }, function (error) {
      settle(call, { error: { code: 0, message: String(error && error.message || error) } });
    }).then(function () {
      flying = null;
      pump();
    }, function (error) {
      flying = null;
      if (window.console) console.error("kitwork: áp câu trả lời của việc " + call.name + " không thành", error);
      pump();
    });
  }

  // the transport: the page's own URL naming the work, and no header of Kitwork's own: the shell of a native app
  // passes a request's method, URL and body as a browser does, and not always its headers
  function send(call, body) {
    var named = call.refresh ? "kitwork-refresh=1" : "kitwork-call=" + encodeURIComponent(call.name);
    var url = location.pathname + (location.search ? location.search + "&" : "?") + named;
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "Accept": "application/json" },
      body: JSON.stringify(body)
    }).then(function (response) {
      var type = response.headers.get("Content-Type") || "";
      if (type.indexOf("application/json") < 0) return { reload: true }; // something else answered: the page is opened again
      return response.json().then(function (data) {
        if (!response.ok && !data.error) data.error = { code: response.status, message: "" };
        return data;
      });
    });
  }

  function settle(call, answer) {
    answer = answer || {};
    if (call.epoch !== epoch) return; // the page it was made on is gone
    if (answer.redirect) {
      var drive = kitwork.drive;
      if (drive && typeof drive.visit === "function") drive.visit(answer.redirect);
      else location.href = answer.redirect;
      return;
    }
    if (answer.reload) { location.reload(); return; }
    if (answer.error && call.refresh) {
      if (window.console) console.error("kitwork: hỏi lại trang không thành", answer.error);
      return;
    }
    if (answer.error) {
      mark(call, false, { code: answer.error.code, message: answer.error.message || "" });
      var back = call.el && call.el.closest && call.el.closest("[data-kitwork-item]") || call.el;
      if (back && typeof kitwork.forget === "function") kitwork.forget(back);
      if (window.console) console.error("kitwork: việc " + call.name + " không thành", answer.error);
      paint();
      return;
    }
    var changes = answer.page || {};
    Object.keys(changes).forEach(function (k) { put(k, changes[k]); });
    // each instance's changes, under its place
    Object.keys(answer).forEach(function (at) {
      if (at === "seq" || at === "page") return;
      var host = at === call.at ? call.host : document.querySelector('[data-kitwork-at="' + at + '"]');
      var instance = host ? instanceOf(host) : null, mine = answer[at];
      if (instance && mine) Object.keys(mine).forEach(function (k) { instance[k] = mine[k]; });
    });
    if (!call.refresh) mark(call, false, null);
    paint();
  }

  // Drive leaves the page: its calls are dropped. The page it brings is seeded from its own data before the kernel
  // paints it (the kernel listens on the document; this, on the window in the capture phase, runs first).
  document.addEventListener("kitwork:before-swap", function () {
    epoch++;
    queue = [];
    track = {};
  });
  window.addEventListener("kitwork:load", seed, true);

  seed();
})();
