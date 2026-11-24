// Kitwork hydrate kernel — the whole client runtime. Delivered as /_kitwork/kernel.<hash>.js with the
// bridge and platform modules; Drive and each component a page names come as files of their own.
// Expressions, boundaries, components, structure and events all ride one
// window.kitwork root (the only one since 28/10/2026), one registry, one delegated event system and one
// DOM observer.
//
// Boot-guarded: safe under double inclusion and under Kitwork Drive re-running head scripts.
// The directives are the eight grouped data-kitwork-* attributes (component, scope, bind, class,
// style, event, if, for) the engine writes for a Kitwork UI page (KITWORK_UI.md §11); the hand-written
// data-kit-* of template pages went on 26/10/2026. The values of those attributes are sources the
// server writes as JavaScript into the page's program (/_kitwork/page.<hash>.js, 15/11/2026), which the
// kernel calls; it carries the one grouped splitter (twin of library/ui program.go) and reads no
// expression itself. No eval, no new Function, ever.
//
// Leak-free by architecture: nodes carry no listeners and no closures (everything is delegated),
// per-element state hides behind a Symbol and dies with the node, SSE streams are deduped by URL
// and auto-closed when their last subscriber leaves the DOM.
(function () {
  "use strict";
  // window.kitwork is the runtime's one root: the engine delivers the kernel, and components, services and
  // the page's program reach it there (the shorter name beside it went on 28/10/2026). A root the
  // bridge seeded first is adopted, so nothing initialises twice.
  var kitwork = (window.kitwork = window.kitwork || {});

  // Two facts that belong to the RUNTIME, not to the native bridge, even though the bridge is what
  // makes them interesting. They live here so a page that ships no bridge at all still has them:
  // web.js throws KitworkError from theme.set(), and runtimeMeta reads platform two lines below.
  if (typeof kitwork.KitworkError !== "function") {
    kitwork.KitworkError = function (message, code, moduleName, actionName, details) {
      var err = new Error(message || "Kitwork execution error");
      err.name = "KitworkError";
      err.code = code || "INTERNAL_ERROR";
      err.module = moduleName || "system";
      err.action = actionName || "unknown";
      err.details = details || {};
      return err;
    };
  }
  if (!Object.getOwnPropertyDescriptor(kitwork, "platform")) {
    Object.defineProperty(kitwork, "platform", {
      get: function () {
        var bridge = kitwork.bridge;
        return bridge && typeof bridge.call === "function" ? (bridge.platform || "native") : "web";
      },
      configurable: true,
      enumerable: true
    });
    Object.defineProperty(kitwork, "isNative", {
      get: function () { return !!(kitwork.bridge && typeof kitwork.bridge.call === "function"); },
      configurable: true,
      enumerable: true
    });
  }

  if (kitwork.runtime && kitwork.runtime.loaded) return;
  var runtimeMeta = kitwork.runtime && typeof kitwork.runtime === "object" ? kitwork.runtime : {};
  runtimeMeta.name = "kitwork";
  runtimeMeta.version = "2.0.0";
  runtimeMeta.engine = kitwork.platform || "web";
  runtimeMeta.development = !!runtimeMeta.development;
  runtimeMeta.loaded = true;
  runtimeMeta.booted = false;
  runtimeMeta.info = function () {
    return {
      name: this.name,
      version: this.version,
      engine: kitwork.platform || this.engine,
      development: this.development
    };
  };
  kitwork.runtime = runtimeMeta;

  var modules = Object.create(null);
  var startHooks = [];
  kitwork.modules = modules;
  kitwork.module = function (name, value) {
    if (arguments.length === 1) return modules[name];
    modules[name] = value;
    return value;
  };
  kitwork.has = function (name) {
    return has(modules, name);
  };

  // Platform capabilities are services, separate from the runtime's control API, and JavaScript
  // only: component code reaches kitwork.<name>; markup never calls kitwork (03/10) — it reaches a service
  // through the component that is its view (theme, progressbar, titlebar, clipboard…).
  var services = Object.create(null);
  var reservedServiceNames = Object.create(null);
  ("service bridge runtime internal module modules has onStart cleanup onCleanup context " +
    "compile run scope scopeFor render set destroy start component expressions " +
    "components blueprints state morph hydrate " +
    "platform isNative mode version").split(" ").forEach(function (name) {
      reservedServiceNames[name] = true;
    });

  function validServiceName(name) {
    return /^[A-Za-z][A-Za-z0-9]*$/.test(name) &&
      (!blockedKey(name) || name === "window") &&
      !has(reservedServiceNames, name);
  }

  kitwork.service = function (name, value) {
    name = String(name || "");
    if (arguments.length === 1) {
      return has(services, name) ? services[name] : undefined;
    }
    if (!validServiceName(name)) throw new Error("kit: invalid or reserved service name '" + name + "'");

    services[name] = value;
    kitwork[name] = value;
    return value;
  };

  kitwork.onStart = function (callback) {
    if (typeof callback !== "function") return function () { };
    startHooks.push(callback);
    if (runtimeMeta.booted) callback();
    return function () {
      var index = startHooks.indexOf(callback);
      if (index >= 0) startHooks.splice(index, 1);
    };
  };

  var globalCleanups = [];
  function cleanup(callback) {
    if (typeof callback === "function") globalCleanups.push(callback);
    return callback;
  }
  function listen(target, eventName, handler, options) {
    target.addEventListener(eventName, handler, options);
    cleanup(function () { target.removeEventListener(eventName, handler, options); });
    return handler;
  }
  kitwork.cleanup = cleanup;

  // ---- expressions: the page's program ----
  // What the kernel runs on a page, a value of data-kitwork-bind, -class, -style or -if, the list and the key of a
  // data-kitwork-for, what a data-kitwork-event does, is written by the server as JavaScript (core/compiler
  // javascript.go) into the page's program, /_kitwork/page.<hash>.js (library/ui program.go): each source a function
  // the kernel calls with the element's scope as this, so the browser's own engine gives it JavaScript's meaning, the
  // one the server's VM gives it (15/11/2026, round 4 of the 13/11 review; the owner chose "Server sinh JS thật").
  // The kernel had a language of its own until then, a lexer, a parser and a tree walker, whose meaning was another:
  // null was "" in a string and 0 in a sum, a call of what is no function undefined, a name nobody gave 0, and a
  // template, typeof, a spread or an arrow with a body refused. A source no program holds is said once, and runs
  // nothing.
  function has(o, k) { return Object.prototype.hasOwnProperty.call(o, k); }
  // blockedKey seals the ONLY member names that can reach code execution — `.constructor` leads to
  // Function (i.e. eval), and __proto__/prototype enable prototype pollution. Denying them in every
  // read / call / write makes "no eval" true BY CONSTRUCTION (not merely blocked by CSP).
  function blockedKey(k) {
    return k === "constructor" || k === "__proto__" || k === "prototype" ||
      k === "__defineGetter__" || k === "__defineSetter__" ||
      k === "__lookupGetter__" || k === "__lookupSetter__" ||
      k === "ownerDocument" || k === "defaultView" || k === "contentWindow" ||
      k === "window" || k === "parent" || k === "top" || k === "self" || k === "globalThis";
  }
  // propertyKey is a key as JavaScript makes it of a value: a number itself, anything else its text, so
  // keys[null] reads keys["null"] (16/10/2026: it read nothing); o[k] and o[k]() read alike.
  function propertyKey(k) { return typeof k === "number" ? k : String(k); }
  // readKey and callKey are what the page's expressions, written as JavaScript by the server (core/compiler
  // javascript.go, 15/11/2026), use for a key in brackets and for a member blockedKey holds: a blocked name reads
  // only what the object holds itself ({ ["__proto__"]: 1 }), never what a prototype or the DOM lends, as on the
  // server, whose values hold their own keys only. optional is the link's ?.; without it, null and undefined have no
  // member, as in JavaScript. callKey calls with the object as this, as o[k]() does.
  function keyOf(o, k) {
    return blockedKey(k) && !(typeof o === "object" && has(o, k)) ? undefined : o[k];
  }
  function readKey(o, k, optional) {
    if (o == null) {
      if (optional) return undefined;
      throw new TypeError("Cannot read properties of " + o + " (reading '" + String(k) + "')");
    }
    return keyOf(o, propertyKey(k));
  }
  function callKey(o, k, args, optional, optionalCall) {
    if (o == null) {
      if (optional) return undefined;
      throw new TypeError("Cannot read properties of " + o + " (reading '" + String(k) + "')");
    }
    k = propertyKey(k);
    var f = keyOf(o, k);
    if (f == null && optionalCall) return undefined;
    if (typeof f !== "function") throw new TypeError(String(k) + " is not a function");
    return f.apply(o, args);
  }
  // run calls a source of the page's program with the scope as this.
  function run(program, s) {
    if (typeof program !== "function") throw new TypeError("hydrate: not a source of the page's program");
    return program.call(s);
  }
  // literal reads a value of data-kitwork-scope, which the engine writes as a literal (library/ui: a number, true or
  // false, a string in quotes, live.go quote): the values it seeds a component with, no program.
  function literal(raw) {
    var text = String(raw).trim();
    if (text === "true") return true;
    if (text === "false") return false;
    if (text === "null") return null;
    if (/^-?(\d+\.?\d*|\.\d+)(e[+-]?\d+)?$/i.test(text)) return Number(text);
    var q = text.charAt(0);
    if ((q === "'" || q === '"') && text.length >= 2 && text.charAt(text.length - 1) === q) {
      var out = "";
      for (var i = 1; i < text.length - 1; i++) {
        var c = text.charAt(i);
        if (c !== "\\") { out += c; continue; }
        var e = text.charAt(++i);
        out += e === "n" ? "\n" : e === "t" ? "\t" : e === "r" ? "\r" : e;
      }
      return out;
    }
    throw new Error('hydrate: "' + text + '" is no literal: data-kitwork-scope holds the values the engine seeds a component with');
  }

  // ---- directives ----
  // data-kitwork-* is what the engine writes for a Kitwork UI page (KITWORK_UI.md §11): EIGHT fixed
  // attributes — component, scope, bind, class, style, event, if, for — each found by its own
  // selector, never by walking the page. Every other data-kitwork-* attribute (generated, layout,
  // current, delay, transition…) is transport and never a directive. Their values are the sources the
  // page's program holds as functions (15/11/2026): the kernel reads no expression itself, and runs no eval.
  //
  // programs are the page's sources written as JavaScript by the server (/_kitwork/page.<hash>.js, library/ui
  // program.go): each a function the kernel calls with the element's scope as this, so the browser's own engine
  // gives it JavaScript's meaning, the server's. names says which sources are a name alone (bare), which a two-way
  // field writes back. A Drive move registers the program of the page it moves to before it paints.
  var programs = Object.create(null);
  // missing are the sources a page wrote that no program holds, each said once
  var missing = Object.create(null);
  kitwork.expressions = function (functions, names) {
    for (var source in functions) {
      if (!has(functions, source) || typeof functions[source] !== "function") continue;
      if (names && has(names, source)) functions[source].bare = names[source];
      programs[source] = functions[source];
    }
    groupedCache = {}; // a value read before its program came is read again
  };
  // program is the function of a source of the page's program; it throws for one no program holds.
  function program(raw) {
    var source = String(raw).trim();
    if (has(programs, source)) return programs[source];
    throw new Error('hydrate: "' + source + '" is in no program of this page (/_kitwork/page.<hash>.js)');
  }
  // compiled is the function of a source, null for one no program holds, said once naming the attribute.
  function compiled(raw, el, attr) {
    if (!raw) return null;
    try { return program(raw); } catch (e) {
      var source = String(raw).trim();
      if (!missing[source]) { missing[source] = true; reportError(e, el, attr); }
      return null;
    }
  }

  // ---- the grouped reader: ONE splitter for scope, bind, class, style and event (twin: pairs.go) ----
  // split cuts at every ';' outside a string ('…' "…" `…`, backslash escapes) and outside ( ) [ ] { };
  // empty parts drop out, so a trailing ';' is fine. cut takes a part apart at its FIRST ':' — the
  // name (letters, digits, - . _ + / and a modifier's flat (…)) and the value, a raw expression. Both
  // are held to grouped_corpus.json, which the Go twin runs too.
  var ALIAS = /^\$[A-Za-z][A-Za-z0-9_]*$/;
  var FOR_HEAD = /^\s*([$A-Za-z_][\w$]*)\s*(?:,\s*([$A-Za-z_][\w$]*)\s*)?\s+of\s+([\s\S]+)$/;
  function misread(text, want) { return new Error('hydrate: "' + text + '" is not ' + want); }
  function split(raw) {
    var out = [], stack = [], quote = "", start = 0, c;
    for (var i = 0; i < raw.length; i++) {
      c = raw[i];
      if (quote) { if (c === "\\") i++; else if (c === quote) quote = ""; }
      else if ("'\"`".indexOf(c) >= 0) quote = c;
      else if ("([{".indexOf(c) >= 0) stack.push(")]}"["([{".indexOf(c)]);
      else if (")]}".indexOf(c) >= 0) { if (stack.pop() !== c) throw misread(raw, "balanced"); }
      else if (c === ";" && !stack.length) { out.push(raw.slice(start, i)); start = i + 1; }
    }
    if (quote || stack.length) throw misread(raw, "closed");
    out.push(raw.slice(start));
    return out.map(function (part) { return part.trim(); }).filter(Boolean);
  }
  function cut(part) {
    var at = part.indexOf(":"), name = part.slice(0, at).trim(), value = part.slice(at + 1).trim();
    if (at < 0 || !/^([\w.+\/-]|\([\w.+\/-]*\))+$/.test(name) || !value) throw misread(part, "name: value");
    return [name, value];
  }
  function pairs(raw) { return split(raw).map(cut); }
  // data-kitwork-component: "name" or "name: $alias". The engine writes no version; a name written
  // with one (theme@1.0.0) still reads, as pairs.go's twin reads it, and names no registered component
  // (reported once per host).
  function component(raw) {
    var at = raw.indexOf(":"), name = (at < 0 ? raw : raw.slice(0, at)).trim(), alias = at < 0 ? "" : raw.slice(at + 1).trim();
    if (!/^[\w.@-]+$/.test(name) || at >= 0 && !ALIAS.test(alias)) throw misread(raw, "name or name: $alias");
    return { name: name, alias: alias };
  }
  // data-kitwork-for: "item[, index] of list", then only "key: expr" (the row's identity).
  function loop(raw) {
    var parts = split(raw), m = FOR_HEAD.exec(parts[0] || ""), key = "";
    if (!m) throw misread(raw, "item of list");
    parts.slice(1).forEach(function (part) {
      var p = cut(part);
      if (p[0] !== "key" || key) throw misread(part, "the one key: of a for");
      key = p[1];
    });
    return { item: m[1], index: m[2] || "", list: m[3].trim(), key: key };
  }
  // grouped(el, kind) → [[name, program, source], …] for one pair attribute. Parsed once per VALUE, not
  // per element, so a Drive morph that rewrites a value is read afresh; a failure is reported once,
  // naming the attribute (and the pair), and a pair that does not compile is skipped by its reader.
  var groupedCache = {};
  function grouped(el, kind) {
    var attr = "data-kitwork-" + kind, raw = el.getAttribute(attr), key = attr + "=" + raw, list = [];
    if (!raw) return list;
    if (!(key in groupedCache)) {
      try { list = pairs(raw); } catch (e) { reportError(e, el, attr); }
      groupedCache[key] = list.map(function (p) { return [p[0], compiled(p[1], el, attr + " " + p[0]), p[1]]; });
    }
    return groupedCache[key];
  }
  // eachPair runs fn(el, name, program, label) for every compiled pair of every element carrying the
  // attribute — found by its selector — but one asleep in a closed dialog.
  function eachPair(kind, fn) {
    document.querySelectorAll("[data-kitwork-" + kind + "]").forEach(function (el) {
      if (asleep(el)) return;
      grouped(el, kind).forEach(function (p) { if (p[1]) fn(el, p[0], p[1], "data-kitwork-" + kind + " " + p[0]); });
    });
  }
  // asleep says node stands inside a dialog that is closed (docs/components/dialog.md): what it holds reads the
  // dialog's value, which open(x) gives, so nothing of it is painted until the dialog opens, as the server drew
  // none of it. dialogs is whether the page has one, read once a paint, so a page without stays one query.
  var dialogs = false;
  function asleep(node) {
    if (!dialogs) return false;
    var el = node && node.nodeType === 1 ? node.parentElement : node && node.parentNode;
    var d = el && el.closest ? el.closest("dialog") : null;
    return !!d && !d.open;
  }

  // The binding groups, shared in spirit with the component runtime's dom.js: same names, same writes.
  var REFLECTED_BOOLEAN = { disabled: "disabled", required: "required", readonly: "readOnly", multiple: "multiple", hidden: "hidden", open: "open" };
  var LIVE_PROPERTY = { checked: "checked", selected: "selected", value: "value", indeterminate: "indeterminate" };
  var LIVE_BOOLEAN = { checked: true, selected: true, indeterminate: true };
  var SVG_CASE = { viewbox: "viewBox", preserveaspectratio: "preserveAspectRatio", gradientunits: "gradientUnits", gradienttransform: "gradientTransform", patternunits: "patternUnits", markerwidth: "markerWidth", markerheight: "markerHeight", refx: "refX", refy: "refY", textlength: "textLength", stddeviation: "stdDeviation" };
  function writeAttribute(el, name, v) {
    if (el.namespaceURI === "http://www.w3.org/2000/svg" && SVG_CASE[name]) name = SVG_CASE[name];
    var aria = name.indexOf("aria-") === 0;
    if (v == null || v === false && !aria) { if (el.hasAttribute(name)) el.removeAttribute(name); return; }
    var text = v === true && !aria ? "" : String(v);
    if (el.getAttribute(name) !== text) el.setAttribute(name, text);
  }
  // setText writes text only when it differs: a repaint that rewrites the same words is still a DOM
  // change, and a live region (the reader) would read it out again on every paint.
  function setText(el, v) {
    v = v == null ? "" : String(v);
    if (el.textContent !== v) el.textContent = v;
  }
  // A place to go is a page of the site, a web page, an e-mail or a phone number, and an image comes from the site,
  // the web or data:image/…: the server's rules (links.go, safeLink and safeImage), the same here, since the kernel
  // writes again what follows a component's state (13/11/2026: the server refused href="javascript:…" from data at
  // the first paint, and the kernel wrote it at the next). The attributes held to the first are those a browser
  // goes to or runs: href and xlink:href on any element, a form's action and a button's formaction, and the src or
  // data of what loads a document or a script (iframe, frame, embed, object, script: a script runs
  // data:text/javascript,…); to the second, an image's src.
  var GOES_TO = { href: 1, "xlink:href": 1, action: 1, formaction: 1 };
  var LOADS_DOCUMENT = { IFRAME: 1, FRAME: 1, EMBED: 1, OBJECT: 1, SCRIPT: 1 };
  // address is a value as a browser reads an address: control characters dropped, the spaces before it gone, a
  // backslash a slash (links.go, normalizeAddress).
  function address(value) {
    return String(value).replace(/[\u0000-\u001f]/g, "").replace(/^ +/, "").replace(/\\/g, "/");
  }
  // linkScheme is the scheme an address starts with, lower case; "" for a path, an anchor or a query.
  function linkScheme(v) {
    var m = /^([A-Za-z][A-Za-z0-9+.-]*):/.exec(v);
    return m ? m[1].toLowerCase() : "";
  }
  function safeLink(value) {
    var scheme = linkScheme(address(value));
    return scheme === "" || scheme === "http" || scheme === "https" || scheme === "mailto" || scheme === "tel";
  }
  function safeImage(value) {
    var v = address(value), scheme = linkScheme(v);
    return scheme === "" || scheme === "http" || scheme === "https" || scheme === "data" && /^data:image\//i.test(v);
  }
  // placeOf is the rule el's attribute name is held to: "link", "image", or "" for any other attribute.
  function placeOf(el, name) {
    if (GOES_TO[name] === 1 || (name === "src" || name === "data") && LOADS_DOCUMENT[el.tagName] === 1) return "link";
    return name === "src" && el.tagName === "IMG" ? "image" : "";
  }
  function writeBinding(el, name, v) {
    if (/^on/.test(name) || /^data-kit/.test(name) || name === "style" || name === "srcdoc") return;
    var place = v == null || v === false || v === true ? "" : placeOf(el, name);
    if (place && !(place === "link" ? safeLink(v) : safeImage(v))) {
      // no place to go: the words stay and nothing is there to click, as the server draws it; no image: the alt
      // stays. Never the address written before, which the data no longer says.
      if (el.hasAttribute(name)) el.removeAttribute(name);
      throw new Error("hydrate: " + name + (place === "link"
        ? " goes to a page of the site (/…, ./…, #…), http:, https:, mailto: or tel:, not "
        : " is an image of the site (/…, ./…), http:, https: or data:image/…, not ") + JSON.stringify(String(v)));
    }
    var reflected = REFLECTED_BOOLEAN[name];
    if (reflected) {
      var on = !!v;
      if (el[reflected] !== on) el[reflected] = on;
      if (el.hasAttribute(name) !== on) el.toggleAttribute(name, on);
      return;
    }
    var live = LIVE_PROPERTY[name];
    if (live) {
      var next = LIVE_BOOLEAN[name] ? !!v : v == null ? "" : v;
      // Written only when the value changed since the kernel last wrote it (28/10/2026): what the person
      // did to the field stays until the data says otherwise. A box ticked while the call it made is out
      // is not unticked by the paint that shows the call pending; kitwork.forget(el) is how a failed call puts
      // the field back to its data.
      var written = state(el).written || (state(el).written = {});
      if (has(written, name) && written[name] === next) return;
      written[name] = next;
      if (el[live] !== next) el[live] = next;
      return;
    }
    if (name.indexOf("-") < 0 && name in el && typeof el[name] !== "function" && typeof el[name] !== "object") {
      var plain = v == null ? "" : v;
      if (el[name] !== plain) el[name] = plain;
      return;
    }
    writeAttribute(el, name, v);
  }

  // A two-way field: in data-kitwork-bind a `value:` / `checked:` whose value is a bare name, on an
  // input, select or textarea. modelOf(el) → [key, "value" | "checked"], or null. Any other
  // value/checked binding is one-way and goes through writeBinding.
  var MODEL = "[data-kitwork-bind]";
  // bareName is the name a source is alone ("" for any other): a program's, or a tree's ["$", name]
  function bareName(source) {
    return typeof source === "function" && source.bare || "";
  }
  function twoWay(el, name, source) {
    var bare = bareName(source);
    return (name === "value" || name === "checked") && !!bare && !blockedKey(bare) && /^(INPUT|SELECT|TEXTAREA)$/.test(el.tagName);
  }
  function modelOf(el) {
    var hit = null;
    grouped(el, "bind").forEach(function (p) { if (!hit && p[1] && twoWay(el, p[0], p[1])) hit = [bareName(p[1]), p[0]]; });
    return hit;
  }
  // number AND range are numeric inputs — coerce to a float so arithmetic (n + step) adds, not
  // string-concatenates. Every other input type stays a string; a checkbox's `checked` is a boolean.
  function modelValue(el, m) { return m[1] === "checked" ? !!el.checked : (el.type === "number" || el.type === "range") ? (parseFloat(el.value) || 0) : (el.value || ""); }

  var raw = {};

  // A write to any scope OUTSIDE a pass — a component method finishing in a .then, a timer, a
  // service callback — schedules one coalesced repaint, so `copied` set two seconds later paints
  // without the component knowing the kernel exists. Inside a pass (render itself, an event
  // handler, a model write) the pass paints synchronously and nothing is scheduled.
  var painting = 0;
  function wrote() { if (!painting) scheduleRender(); }
  function pass(fn) {
    painting++;
    try { return fn(); } finally { painting--; }
  }

  var scope = new Proxy(raw, {
    get: function (t, k) {
      if (k === "$") return t;
      if (k in aliases) return aliases[k]; // kitwork / $app / $sidebar … → a public surface or component handle
      return has(t, k) ? t[k] : undefined;
    },
    set: function (t, k, v) {
      t[k] = v;
      wrote();
      return true;
    }
  });

  // ---- scopes: data-kitwork-scope and data-kitwork-component mark a boundary ----
  // Lexical, closure-like resolution: reads fall through ancestor scopes up to the page scope;
  // writes go to the scope that OWNS the key, else the nearest one — "private shadows shared".
  // `$` addresses the page scope explicitly ($.total = $.total + 1) — the same $ the server's
  // template language uses for its root data. Scope objects live in the node's Symbol state,
  // so they die with their node; two sibling scopes never see each other.
  // A boundary is either: data-kitwork-component names a REGISTERED blueprint (see kitwork.component);
  // data-kitwork-scope carries a literal's values.
  // (HOST is the same list without the rows a live for makes, which are not hosts.)
  var HOST = "[data-kitwork-component],[data-kitwork-scope]";
  var SCOPE = HOST + ",[data-kitwork-item]";

  // The component registry: kitwork.component("counter", { count: 0, inc() {…} }). A blueprint is a
  // plain JS object — state values + methods. Methods are real functions (called with this = the
  // component scope); state is deep-cloned per instance so two boundaries never share it.
  // (Named `blueprints` to stay clear of kitwork.components, which is the registry of live surface.)
  var blueprints = {};
  function cloneState(v) {
    if (v === null || typeof v !== "object") return v;
    try { return JSON.parse(JSON.stringify(v)); } catch (e) { return v; }
  }
  // Copy a blueprint onto a boundary's scope. A GETTER is copied as a getter, not called once and
  // frozen: `get visibleTodos() { return this.todos.filter(...) }` is how a component writes a
  // computed value, and reading it through the scope proxy is what makes it repaint when todos
  // change. Plain values are cloned so two instances never share one array.
  function seedComponent(target, def) {
    for (var k in def) {
      if (!has(def, k)) continue;
      var descriptor = Object.getOwnPropertyDescriptor(def, k);
      if (descriptor && (descriptor.get || descriptor.set)) {
        Object.defineProperty(target, k, {
          get: descriptor.get,
          set: descriptor.set,
          enumerable: true,
          configurable: true
        });
        continue;
      }
      target[k] = (typeof def[k] === "function") ? def[k] : cloneState(def[k]);
    }
  }

  // literalInto writes a host's data-kitwork-scope into its scope, once: the values that override the
  // blueprint's defaults (data-kitwork-component="countdown" data-kitwork-scope="duration: 300000"), each a
  // literal the engine writes (literal), no program. False when there is none.
  function literalInto(b, target) {
    var raw = b.getAttribute && b.getAttribute("data-kitwork-scope"), list = [];
    if (!raw) return false;
    try { list = pairs(raw); } catch (e) { reportError(e, b, "data-kitwork-scope"); }
    list.forEach(function (p) {
      if (!blockedKey(p[0])) guarded(b, "data-kitwork-scope " + p[0], function () { target[p[0]] = literal(p[1]); });
    });
    return true;
  }
  // hostOf(b) → { name, alias } for a component host, else null. data-kitwork-component carries
  // "name[: $alias]" (read once per value); a value that does not read is reported and the host is a
  // plain boundary.
  function hostOf(b) {
    var raw = b.getAttribute("data-kitwork-component"), key = "component=" + raw;
    if (!raw) return null;
    if (!(key in groupedCache)) {
      groupedCache[key] = null;
      try { groupedCache[key] = component(raw); }
      catch (e) { reportError(e, b, "data-kitwork-component"); }
    }
    return groupedCache[key];
  }
  function boundaryScope(b) {
    var st = state(b);
    var tag = hostOf(b);
    if (tag) {
      var cname = tag.name;
      // Component registration (kitwork.component) can run AFTER the first render — so seed lazily,
      // the first time the blueprint is available, and never re-seed once done (keeps mutations).
      // HOST_KEY ties the scope to its host, unseen by markup (a Symbol is never a name), so
      // kitwork.context(this) finds the host along the same chain a name is read through.
      if (!st.scope) Object.defineProperty(st.scope = {}, HOST_KEY, { value: b });
      registerAlias(tag.alias, st.scope, b);
      if (!st.seeded) {
        if (blueprints[cname]) {
          seedComponent(st.scope, blueprints[cname]);
          literalInto(b, st.scope); // the host's own values over the blueprint's defaults (03/10)
          st.seeded = true;
          runInit(b);
        } else {
          // Nothing registered under this name. The engine ships exactly the components a page
          // names, so this is a typo. Say it ONCE per host: every later pass would otherwise repeat
          // it for as long as the page is open.
          if (!st.reportedMissing) {
            st.reportedMissing = true;
            reportError(new Error("hydrate: no component registered as \"" + cname + "\""), b, "data-kitwork-component");
          }
        }
      }
      return st.scope;
    }
    if (st.scope) return st.scope;
    st.scope = {};
    if (literalInto(b, st.scope)) runInit(b);
    return st.scope;
  }
  // ---- init(context): the component's hands on its own DOM (ideaship-final §6, B9 one kernel) ----
  // Six keys:
  //   host                — the element carrying data-kitwork-component
  //   owned(selector)     — matches under the host, the host itself included, a nested host's
  //                         subtree excluded (its elements are its own)
  //   listen(target, type, fn, options) — an event listener released with the host
  //   cleanup(fn)         — runs when morph removes the host (this = the scope); the returned
  //                         cancel runs it now instead
  //   afterRender(fn)     — runs ONCE after the next paint, outside the pass — so a state write
  //                         inside it schedules its own repaint (no lost update)
  //   emit(name, detail)  — an event the page hears on the component's tag (data-kitwork-event)
  // (element, elements and require read data-kit-element, which only template pages wrote: they went
  // with them on 26/10/2026.)
  // Every resource lives on the host's state, so cleanupTree releases it when the host leaves.
  var afterRenders = [];
  var HOST_KEY = Symbol("kit:host");
  // contextOf is the ONE context a host has: init receives it, kitwork.context(this) returns the same.
  function contextOf(b) { var st = state(b); return st.context || (st.context = initContext(b)); }
  function initContext(b) {
    function owned(selector) {
      if (typeof selector !== "string") throw new TypeError("hydrate: context.owned(selector) expects a string");
      var out = [];
      if (b.matches && b.matches(selector)) out.push(b);
      b.querySelectorAll(selector).forEach(function (el) {
        if (!el.matches(HOST) && el.parentElement && el.parentElement.closest(HOST) === b) out.push(el);
      });
      return out;
    }
    function cleanup(fn) {
      if (typeof fn !== "function") throw new TypeError("hydrate: context.cleanup(fn) expects a function");
      var active = true;
      var release = function () {
        if (!active) return;
        active = false;
        try { fn.call(scopeFor(b)); } catch (error) { reportError(error, b, "cleanup"); }
      };
      onCleanup(b, release);
      return release;
    }
    function listen(target, type, fn, options) {
      if (!target || typeof target.addEventListener !== "function") throw new TypeError("hydrate: context.listen(target, type, fn, options) expects an EventTarget");
      if (typeof type !== "string" || typeof fn !== "function") throw new TypeError("hydrate: context.listen(target, type, fn, options) expects a string and a function");
      var capture = typeof options === "boolean" ? options : !!(options && options.capture);
      target.addEventListener(type, fn, options);
      return cleanup(function () { target.removeEventListener(type, fn, capture); });
    }
    function afterRender(fn) {
      if (typeof fn !== "function") throw new TypeError("hydrate: context.afterRender(fn) expects a function");
      var entry = { host: b, run: fn, active: true };
      afterRenders.push(entry);
      return function () { entry.active = false; };
    }
    // emit(name, detail) — tell the page something happened: a CustomEvent on the host that bubbles,
    // heard in the tag's data-kitwork-event as <name> and read as $event.detail. A DOM event's name is
    // refused, so click always means the DOM's click. Emitted during a paint (from init, say),
    // it waits for the paint to end: a handler must not run inside the pass that is drawing the
    // page, and the paint is also where the document learns to listen for a new name.
    function emit(name, detail) {
      if (!EVENT_NAME.test(name) || DOM_EVENT_NAMES[name]) throw new TypeError('hydrate: emit("' + name + '"): lower case, and not a DOM event');
      var send = function () {
        ensureListener(name);
        b.dispatchEvent(new CustomEvent(name, { bubbles: true, detail: detail }));
      };
      if (painting > 0) afterRender(send);
      else send();
    }
    return Object.freeze({ host: b, owned: owned, listen: listen, cleanup: cleanup, afterRender: afterRender, emit: emit });
  }
  // flushAfterRender runs after a paint has ended (render → pass → paint, then this): one-shot,
  // in registration order, only for hosts still in the document; a host that morph removed
  // between registration and paint is skipped, its entry dropped.
  function flushAfterRender() {
    if (!afterRenders.length) return;
    var entries = afterRenders.splice(0);
    entries.forEach(function (entry) {
      if (!entry.active) return;
      entry.active = false;
      if (!entry.host.isConnected) return;
      try { entry.run.call(scopeFor(entry.host)); } catch (error) { reportError(error, entry.host, "afterRender"); }
    });
  }
  // runInit calls a boundary's init() ONCE, right after it is seeded — the mount lifecycle hook.
  // A registered component's init is real JS (this = the scope, one argument: the context above).
  // Set the guard BEFORE calling so a re-entrant scopeFor
  // never loops. An init that throws reaches the error boundary like any directive (directive
  // "init"), and the component stays mounted with whatever state it seeded.
  function runInit(b) {
    var st = state(b);
    if (st.inited) return;
    st.inited = true;
    var fn = st.scope && st.scope.init;
    if (!fn) return;
    try {
      var proxy = scopeFor(b);
      var result;
      // a call the component's scope makes: this = the scope
      if (typeof fn === "function") result = fn.call(proxy, contextOf(b));
      observeEffect(result, proxy);
    } catch (e) { reportError(e, b, "init"); }
  }
  function chainFor(el) {
    var objs = [];
    var b = el && el.closest ? el.closest(SCOPE) : null;
    while (b) {
      objs.push(boundaryScope(b));
      b = b.parentElement ? b.parentElement.closest(SCOPE) : null;
    }
    objs.push(raw);
    return objs;
  }
  // elementScope wraps a scope with the acting element's DOM handles — the escape hatch for the
  // rare imperative need (focus, scroll, integrate a widget, toggle an attribute on a child). It is
  // NOT a prototype mutation: they are variables in the expression context that resolve to native
  // objects, so `$this.querySelector('input').focus()` executes exactly as it reads. The system
  // variables of ideaship-final §3: `$this` is the element that owns the directive; `$host` is the
  // boundary element — nearest data-kitwork-scope/component, else <html> — so a query stays inside
  // what the component owns; `$event` is the native DOM event of the handler that is running. `$el`,
  // `$root` and `$element` (the boundary's data-kit-element names, a template page's, until
  // 26/10/2026) resolve to nothing; their names stay reserved so no alias can take them.
  // Reads and method calls only — value/attribute CHANGES belong to bindings (a two-way field,
  // state→CSS), not to reaching in and poking the DOM.
  function elementScope(el, event) {
    var base = scopeFor(el);
    return new Proxy(base, {
      get: function (t, k) {
        if (k === "$this") return el;
        if (k === "$host") return (el.closest && el.closest(SCOPE)) || document.documentElement;
        if (k === "$event") return event || null;
        if (k in aliases) return aliases[k]; // kitwork / $app / $sidebar … → a public surface or component handle
        return base[k];
      },
      set: function (t, k, v) { base[k] = v; return true; }
    });
  }
  // ---- data-kitwork-style="property: expr; …" ----
  // One attribute that reads like CSS (KITWORK_UI.md §11), written through writeStyle below. A
  // component with a continuous value — a bar's width, a popover's offset, a
  // carousel's transform — says it in markup instead of writing element.style from JavaScript,
  // which is the whole point: the server sees the expression and the CSS JIT can read the names.
  //
  // A nullish, false or empty result RESTORES what the author wrote in the style attribute, so a
  // binding that stops applying never leaves a value behind. The baseline is captured once, the
  // first time this element's property is written.
  //
  // What a value may not contain: control characters, `;{}\@`, comments, !important, and the
  // functions that can fetch or execute (url, image-set, src, expression, attr), plus the
  // javascript:/vbscript:/data:text/html spellings. `var()` IS allowed — a Kitwork site's colours
  // are custom properties, so blocking it would make the directive useless here.
  var STYLE_BLOCKED = { "css-text": 1, csstext: 1, behavior: 1, "-moz-binding": 1 };
  function styleName(name) {
    if (name.indexOf("--") === 0) {
      return /^--[A-Za-z_][A-Za-z0-9_-]*$/.test(name) && !/^--(?:kit|kitwork)-/i.test(name) ? name : "";
    }
    return /^-?[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(name) && !STYLE_BLOCKED[name] ? name : "";
  }
  function unsafeStyleValue(text) {
    if (/[\u0000-\u001f\u007f-\u009f]/.test(text) || /[;{}\\@]/.test(text) ||
      text.indexOf("/*") >= 0 || text.indexOf("*/") >= 0 || /!\s*important\b/i.test(text) ||
      /(^|[^A-Za-z0-9_-])(url|image-set|-webkit-image-set|src|expression|attr)\s*\(/i.test(text)) return true;
    var compact = text.replace(/\s+/g, "").toLowerCase();
    return compact.indexOf("javascript:") >= 0 || compact.indexOf("vbscript:") >= 0 ||
      compact.indexOf("data:text/html") >= 0;
  }
  // (label names the attribute in the error: data-kitwork-style <property>.)
  function writeStyle(el, property, value, label) {
    var store = state(el);
    var baseline = store.styleBaseline || (store.styleBaseline = {});
    if (!(property in baseline)) {
      baseline[property] = { value: el.style.getPropertyValue(property), priority: el.style.getPropertyPriority(property) };
    }
    if (value == null || value === false || value === "") {
      var was = baseline[property];
      if (was.value !== "") el.style.setProperty(property, was.value, was.priority);
      else el.style.removeProperty(property);
      return;
    }
    var text = typeof value === "number" ? (isFinite(value) ? String(value) : "") : String(value);
    if (!text || unsafeStyleValue(text)) throw new Error("hydrate: unsafe value for " + (label || "data-kitwork-style ") + property);
    el.style.setProperty(property, text, "");
  }

  function scopeFor(el) {
    // A comment node (a for / if anchor) has no closest() — resolve through its
    // parent element, else the region's local/component scope is lost and reads fall to the PAGE
    // scope (which is why a for/if over a component-scoped array or condition would render nothing).
    var node = el;
    if (node && !node.closest) node = node.parentElement || node.parentNode;
    var b = node && node.closest ? node.closest(SCOPE) : null;
    if (!b) return scope;
    var st = state(b);
    if (st.scopeProxy) return st.scopeProxy;
    st.scopeProxy = new Proxy(boundaryScope(b), {
      get: function (t, k) {
        if (k === "$") return raw;
        if (k in aliases) return aliases[k]; // kitwork / $app / $sidebar … → a public surface or component handle
        var objs = chainFor(b);
        for (var i = 0; i < objs.length; i++) { if (has(objs[i], k)) return objs[i][k]; }
        return undefined;
      },
      set: function (t, k, v) {
        // a Symbol is a component's private slot: its instance's own, never one around it of the same kind
        if (typeof k === "symbol") { t[k] = v; return true; }
        var objs = chainFor(b);
        for (var i = 0; i < objs.length; i++) {
          if (k in objs[i]) {
            objs[i][k] = v;
            wrote();
            return true;
          }
        }
        objs[0][k] = v;
        wrote();
        return true;
      }
    });
    return st.scopeProxy;
  }

  // Seed scope keys from the inputs present in the DOM — at boot AND after every swap. Seeding at
  // boot (not at script parse) matters: an inline bundle executes in <head> before the body exists,
  // and a morphed-in page may bring new two-way fields whose server-rendered value must win.
  // A key is seeded into the input's NEAREST scope, only when no scope in its chain owns it yet.
  function seedModels() {
    document.querySelectorAll(MODEL).forEach(function (el) {
      var m = modelOf(el);
      if (!m) return;
      var k = m[0];
      var objs = chainFor(el), found = false;
      for (var i = 0; i < objs.length; i++) { if (k in objs[i]) { found = true; break; } }
      if (!found) objs[0][k] = modelValue(el, m);
    });
  }

  // Named component handles: data-kitwork-component="countdown: $tea" registers `$tea` → that
  // instance's scope, so ANY expression reaches it ($tea.pause()), even from outside its DOM subtree
  // (the scattered-controls case). No alias = purely lexical (a bare pause() = nearest scope). The
  // alias names the component INSTANCE (ideaship-final §6).
  var aliases = Object.create(null);
  var reservedAliases = Object.create(null);
  ["$", "$this", "$el", "$host", "$root", "$event", "$element"].forEach(function (name) { reservedAliases[name] = true; });
  // aliasOwners remembers which boundary each alias names, so a handle resolves to that boundary's
  // SCOPE PROXY rather than to its raw state object.
  //
  // This is the difference between $menu.hide() working and half-working. A method invoked from a
  // directive receives the proxy as `this`, and the proxy's set trap calls wrote() — so a write
  // from a timer the method started still repaints. A method invoked through a handle used to
  // receive the raw object: the write landed, nothing was told, and the DOM kept the old answer
  // until something else happened to repaint. Inside a pass nobody noticed, because the pass
  // repaints anyway; outside one — a timer, a promise, another component — it simply did not work.
  var aliasOwners = Object.create(null);
  function registerAlias(alias, target, owner) {
    alias = String(alias || "").trim();
    var store = owner ? state(owner) : null;
    var previous = store && store.componentAlias;
    if (previous && previous !== alias && aliasOwners[previous] === owner) {
      delete aliases[previous];
      delete aliasOwners[previous];
    }
    if (store) store.componentAlias = "";
    if (!ALIAS.test(alias) || reservedAliases[alias]) return false;
    if (owner) {
      aliasOwners[alias] = owner;
      Object.defineProperty(aliases, alias, {
        get: function () { return scopeFor(owner); },
        configurable: true,
        enumerable: true
      });
    } else {
      aliases[alias] = target;
    }
    if (store) store.componentAlias = alias;
    return true;
  }
  // Markup has no `kitwork` (03/10): a read of it is a name nothing declares, and the server names it at
  // render. `$app` is not installed either — an application's state is its own components, each
  // reached through its alias.

  var activeComponents = {};
  // Every host MOUNTS on the pass — seeded from its blueprint and init(context) run — whether or
  // not a directive inside it ever asks for its scope. An init-only component (a favicon fallback,
  // a live counter that owns its DOM) has nothing to bind, and still has to start.
  function rebuildActiveComponents() {
    var next = {};
    document.querySelectorAll("[data-kitwork-component]").forEach(function (el) {
      var tag = hostOf(el);
      if (!tag) return;
      var cname = tag.name;
      boundaryScope(el);
      if (state(el).seeded) {
        var inst = scopeFor(el);
        (next[cname] = next[cname] || []).push(inst);
      }
    });

    for (var k in activeComponents) {
      if (!(k in next)) {
        delete activeComponents[k];
      }
    }
    for (var k in next) {
      if (next[k].length === 1) {
        activeComponents[k] = next[k][0];
      } else {
        activeComponents[k] = next[k];
      }
    }
  }

  // ---- data-kitwork-for: client list rendering ----
  // The one capability the kernel lacked. It owns STRUCTURE — which item nodes exist, in what order,
  // matched by data-kitwork-key — and hands CONTENT to the ordinary text/bind/class pass by giving each
  // materialised item a per-item scope in its Symbol state (item / index), which chainFor then reads
  // through to the enclosing component scope. It reuses the SAME keyed-identity idea as morph rather
  // than diffing: a keyed node that survives is MOVED, never rebuilt, so focus/cursor/input on a row
  // are preserved across re-renders. Only the author's list (`items`) and key (`item.id`) run, as the
  // page's program wrote them, like any other attribute.
  // data-kitwork-for="x of list; key: x.id" is read by loop(); rows the kernel makes carry its own
  // data-kitwork-item / data-kitwork-key markers (data-kit-item / data-kit-key until 26/10/2026).
  var FOR = "[data-kitwork-for]";
  var forRegistry = [];
  var forSerial = 0;
  function parseFor(raw) {
    var f = loop(raw || "");
    return { item: f.item, index: f.index, list: program(f.list), keyIR: f.key ? program(f.key) : null };
  }
  // First sight of a for element: capture it as a template, replace it with a comment anchor.
  // The anchor is where materialised rows are inserted; the source element itself never renders.
  //
  // A list the server drew (a page of router.view that runs here, 28/10/2026, ui/render.go) arrives as the
  // marker pair already, its template hidden right after the start marker (data-kitwork-template) and its
  // rows after that, each with the list's id and its key:
  //
  //   <!--kitwork-for-start id=s1--><li data-kitwork-for="todo of todos; key: todo.id" data-kitwork-template hidden>…</li>
  //   <li data-kitwork-item="s1" data-kitwork-key="45">…</li>…<!--kitwork-for-end-->
  //
  // Those markers are kept and the rows adopted by key: the first pass moves and rebuilds nothing the
  // server drew, so a row keeps its focus and what was typed in it from the first paint on.
  function collectFor() {
    document.querySelectorAll(FOR).forEach(function (el) {
      var parent = el.parentNode;
      if (!parent) return;
      var spec = null;
      try { spec = parseFor(el.getAttribute("data-kitwork-for")); } catch (e) { reportError(e, el, "data-kitwork-for"); }
      el.removeAttribute("data-kitwork-for");
      if (!spec) return;
      var drawn = el.hasAttribute("data-kitwork-template");
      var template = el.cloneNode(true);
      if (drawn) {
        template.removeAttribute("data-kitwork-template");
        template.removeAttribute("hidden");
        var start = el.previousSibling;
        var opened = start && start.nodeType === 8 && /^kitwork-for-start id=(\S+)$/.exec(start.nodeValue);
        var close = el.nextSibling;
        while (close && !(close.nodeType === 8 && close.nodeValue === "kitwork-for-end")) close = close.nextSibling;
        if (opened && close) {
          parent.removeChild(el);
          forRegistry.push({ id: opened[1], anchor: start, end: close, template: template, spec: spec, keyIR: spec.keyIR });
          return;
        }
      }
      // The list's place in the document is the marker pair of ideaship-final §7 — the same shape
      // a server-rendered list arrives in — and every row carries the list's id in data-kitwork-item.
      var id = "f" + (++forSerial);
      var anchor = document.createComment("kitwork-for-start id=" + id);
      var end = document.createComment("kitwork-for-end");
      parent.insertBefore(anchor, el);
      parent.insertBefore(end, el);
      parent.removeChild(el);
      forRegistry.push({ id: id, anchor: anchor, end: end, template: template, spec: spec, keyIR: spec.keyIR });
    });
  }
  function renderFor() {
    collectFor();
    for (var r = 0; r < forRegistry.length; r++) {
      var reg = forRegistry[r];
      var parent = reg.anchor.parentNode;
      if (!parent) continue; // anchor left the DOM (a swap removed the region) — nothing to render
      if (asleep(reg.anchor)) continue;
      var arr = run(reg.spec.list, scopeFor(reg.anchor));
      if (!(arr instanceof Array)) arr = [];

      // current rows: the data-kitwork-item siblings between the start and end markers.
      var current = [], curByKey = {}, n = reg.anchor.nextSibling;
      while (n && n !== reg.end) {
        if (n.nodeType === 1 && n.getAttribute("data-kitwork-item") === reg.id) {
          curByKey[n.getAttribute("data-kitwork-key")] = n;
          current.push(n);
        }
        n = n.nextSibling;
      }

      var insertAfter = reg.anchor, used = {}, created = false;
      for (var i = 0; i < arr.length; i++) {
        // The row's scope is an overlay (§7, rule 2): the item and index under their authored
        // names plus count / first / last / even / odd — the item object itself is never touched.
        var itemScope = { count: arr.length, first: i === 0, last: i === arr.length - 1, even: i % 2 === 0, odd: i % 2 === 1 };
        itemScope[reg.spec.item] = arr[i];
        if (reg.spec.index) itemScope[reg.spec.index] = i;
        var key = reg.keyIR ? String(run(reg.keyIR, itemScope)) : String(i);
        used[key] = true;

        var node = curByKey[key];
        if (!node) {
          node = reg.template.cloneNode(true);
          node.setAttribute("data-kitwork-item", reg.id);
          node.setAttribute("data-kitwork-key", key);
          created = true;
        }
        var st = state(node);
        st.scope = itemScope; // (re)bind the row to its current item; content is filled by render()
        st.seeded = true;
        if (insertAfter.nextSibling !== node) parent.insertBefore(node, insertAfter.nextSibling);
        insertAfter = node;
      }

      for (var d = 0; d < current.length; d++) {
        if (!used[current[d].getAttribute("data-kitwork-key")]) {
          cleanupTree(current[d]);
          current[d].remove();
        }
      }
      // A row made in this pass may carry a for of its own (weeks of days, groups of
      // items). Collect it NOW: the loop re-reads forRegistry.length, so the inner list fills in
      // this same pass. Collected only at the start, a new row stayed empty until something else
      // happened to repaint — the calendar drew September's title over a grid with no days in it.
      if (created) collectFor();
    }
  }

  // A subtree mounted DURING a click (a modal opened by that very click) must not be closed by the
  // same click's :outside check — the opening click is, by definition, outside a panel that did
  // not exist yet. Fresh mounts are remembered until the next microtask, i.e. until the whole click
  // dispatch (every delegated listener) has finished; the NEXT outside click closes normally.
  var freshMounts = [];
  function markFresh(node) {
    if (freshMounts.indexOf(node) < 0) freshMounts.push(node);
    if (freshMounts.length === 1) {
      (typeof queueMicrotask === "function" ? queueMicrotask : function (f) { setTimeout(f, 0); })(function () { freshMounts.length = 0; });
    }
  }
  function isFresh(el) {
    for (var i = 0; i < freshMounts.length; i++) {
      var f = freshMounts[i];
      if (f === el || (f.contains && f.contains(el))) return true;
    }
    return false;
  }

  // ---- data-kitwork-if: conditional mount / unmount ----
  // The sibling of for, on the same machinery — anchor comment, captured template,
  // cleanupTree on removal. The difference from show is the whole point: `show` keeps the
  // DOM and only toggles `hidden`, so a hidden subtree's bindings and effects keep running; `if`
  // MOUNTS and UNMOUNTS, so an absent branch does nothing at all. That is what a modal, an editing
  // panel or a lazy region needs — not a hidden node quietly holding an SSE stream open.
  var IF = "[data-kitwork-if]";
  var ifRegistry = [];
  function collectIf() {
    document.querySelectorAll(IF).forEach(function (el) {
      var parent = el.parentNode;
      if (!parent) return;
      var cond = compiled(el.getAttribute("data-kitwork-if"), el, "data-kitwork-if");
      el.removeAttribute("data-kitwork-if");
      if (!cond) return;
      var template = el.cloneNode(true);
      var anchor = document.createComment("kitwork-if");
      parent.insertBefore(anchor, el);
      parent.removeChild(el);
      ifRegistry.push({ anchor: anchor, template: template, cond: cond, mounted: null });
    });
  }
  function renderIf() {
    collectIf();
    for (var r = 0; r < ifRegistry.length; r++) {
      var reg = ifRegistry[r];
      var parent = reg.anchor.parentNode;
      if (!parent) continue;
      if (asleep(reg.anchor)) continue;
      var show = !!run(reg.cond, scopeFor(reg.anchor));
      if (show && !reg.mounted) {
        // Mount a fresh clone right after the anchor; the ordinary binding pass (below) fills it in
        // this same render, because renderIf runs before the text/show/bind queries.
        reg.mounted = reg.template.cloneNode(true);
        parent.insertBefore(reg.mounted, reg.anchor.nextSibling);
        markFresh(reg.mounted); // don't let the click that opened it also close it via click-outside
      } else if (!show && reg.mounted) {
        cleanupTree(reg.mounted); // release the subtree's listeners/observers/streams before removal
        reg.mounted.remove();
        reg.mounted = null;
      }
    }
  }


  function render() { pass(paint); flushAfterRender(); }
  function paint() {
    dialogs = !!document.querySelector("dialog");
    seedModels();
    rebuildActiveComponents();
    renderFor();
    renderIf();
    // The grouped attributes, each found by its selector. event: listen for every name, and queue a
    // `ready` to run once after this element's first paint. bind: text (never HTML), show (hidden),
    // a two-way value/checked (left to the model pass below), anything else through writeBinding, the
    // name lower-cased as the HTML parser lower-cases an attribute's (a class is data-kitwork-class).
    // class: each pair is one exact class name and its condition, truthy adds it, falsy removes it,
    // and no other class is touched. The name is held to the splitter's name rule (no ':'), so a
    // variant class (hover:x) is never toggled this way; an alpha or a glass (bg-x/50, glass/60) is.
    // style: writeStyle, with its checks.
    document.querySelectorAll("[data-kitwork-event]").forEach(function (el) {
      groupedEvents(el).forEach(function (h) {
        if (h.type !== "ready") ensureListener(h.type);
        else if (!(state(el).once || {})[h.attr]) afterRenders.push({ host: el, active: true, run: function () { pipeline(el, h, { type: "ready", target: el }); } });
      });
    });
    eachPair("bind", function (el, name, program, label) {
      if (!twoWay(el, name, program)) guarded(el, label, function () {
        var v = run(program, scopeFor(el));
        if (name === "text") setText(el, v);
        else if (name === "show") el.hidden = !v;
        else writeBinding(el, name.toLowerCase(), v);
      });
    });
    eachPair("class", function (el, name, program, label) {
      guarded(el, label, function () { el.classList.toggle(name, !!run(program, scopeFor(el))); });
    });
    eachPair("style", function (el, name, program, label) {
      var property = styleName(name);
      if (property) guarded(el, label, function () { writeStyle(el, property, run(program, scopeFor(el)), "data-kitwork-style "); });
    });
    document.querySelectorAll(MODEL).forEach(function (el) {
      var m = modelOf(el);
      if (!m) return;
      var k = m[0], s = scopeFor(el);
      if (m[1] === "checked") { if (el.checked !== !!s[k]) el.checked = !!s[k]; }
      else if (String(s[k]) !== el.value) el.value = s[k];
    });
  }

  // ---- per-element runtime state ----
  // Lives behind a private Symbol. Resources registered on the state are explicitly released when
  // morph or another DOM owner removes the node. (The verb registry that used to sit here —
  // data-kit-action, data-kit-target, the fire() dispatcher — is gone: behaviour is a component or an
  // expression, transport is Drive. 22/09.)
  var stateKey = Symbol("kitwork");
  function state(element) {
    return element[stateKey] || (element[stateKey] = {});
  }
  function onCleanup(element, callback) {
    var store = state(element);
    (store.cleanups || (store.cleanups = [])).push(callback);
    return callback;
  }
  function cleanupElement(element) {
    var store = element && element[stateKey];
    if (!store) return;
    // Ownership is the test, not identity: reading aliases[name] now resolves the boundary's scope
    // proxy, which is never the raw scope object this store holds.
    if (store.componentAlias && aliasOwners[store.componentAlias] === element) {
      delete aliases[store.componentAlias];
      delete aliasOwners[store.componentAlias];
      store.componentAlias = "";
    }
    (store.cleanups || []).splice(0).forEach(function (callback) {
      try { callback(); } catch (_) { }
    });
  }
  function cleanupTree(node) {
    if (!node || node.nodeType !== 1) return;
    cleanupElement(node);
    node.querySelectorAll("*").forEach(cleanupElement);
  }
  kitwork.onCleanup = onCleanup;
  // Register a reusable stateful component blueprint. Activate it with data-kitwork-component="name".
  // A component has state + methods + a scope boundary. Registering (re)renders on the next tick,
  // so components registered after boot still paint.
  var renderScheduled = false;
  function scheduleRender() {
    if (renderScheduled) return;
    renderScheduled = true;
    (typeof queueMicrotask === "function" ? queueMicrotask : function (f) { setTimeout(f, 0); })(function () {
      renderScheduled = false;
      render();
    });
  }
  //
  // Not before boot. Components arrive as files of their own, one after another, ahead of
  // DOMContentLoaded — and a render scheduled by the first would paint while the rest had not run,
  // reporting every one of them missing. Boot's first pass sees them all; after boot, a component
  // that arrives later (a file Drive added) still schedules its own render.
  kitwork.component = function (name, def) {
    blueprints[name] = def;
    if (runtimeMeta.booted) scheduleRender();
    return kitwork;
  };
  // kitwork.context(this) — a component method reaching its context after init (emit, element,
  // listen…) without stashing it. `this` is whatever scope the call ran in: the host's own, a
  // nested scope inside it, or the host's through its alias from outside — all find the same host.
  // Anything else is refused by name, never answered with a context that belongs to something else.
  kitwork.context = function (self) {
    var host = self && self[HOST_KEY];
    if (!host) throw new TypeError("kitwork: kitwork.context(this) needs the this of a component method");
    return contextOf(host);
  };
  kitwork.state = state;
  kitwork.components = activeComponents;
  kitwork.blueprints = blueprints;

  function observeEffect(result, current) {
    if (result && typeof result.then === "function") {
      result.then(function () {
        scheduleRender();
      }, function (error) {
        current.error = error && error.message ? error.message : String(error);
        scheduleRender();
      });
    }
    return result;
  }
  function runEffect(expression, element, event) {
    var current = elementScope(element, event);
    return observeEffect(run(expression, current), current);
  }
  // A directive that fails — an action, a binding, an init, a component's require — reaches the
  // console. guarded() wraps every place a directive's expression runs, so one failing element never
  // aborts the pass for the others. (data-kit-error, a boundary that ran an expression with $error,
  // was removed on 02/10: nothing used it. A failure a person should see is an action's rejected
  // promise, which lands in the scope's `error`; a programming mistake belongs to the verify pass.)
  function reportError(error, el, directive) {
    if (typeof console !== "undefined" && console.error) console.error(error, directive || "", el || "");
  }
  // acting is the element whose directive runs now (a binding painted, an event handled), null between:
  // kitwork.element() lets a function the markup calls know where it was called from, and a value it reads
  // know where it is painted (a job's pending, read in a row, is that row's call: call.js).
  var acting = null;
  function guarded(el, directive, fn) {
    var was = acting;
    acting = el;
    try { return fn(); } catch (error) { reportError(error, el, directive); } finally { acting = was; }
  }
  kitwork.element = function () { return acting; };
  // kitwork.event() is the event whose handler runs now, while it is dispatched; null between, and for a handler
  // a timing modifier runs later (debounce(n)): a call made as a field is typed in waits for a pause (call.js).
  var dispatching = null;
  kitwork.event = function () { return dispatching; };
  // kitwork.forget(el): the fields at and under el are written again by the next paint, whatever the person
  // did to them (writeBinding).
  // kitwork.write(el, name, v) and kitwork.text(el, v): how the kernel writes a bound value and a text, for a page's
  // program to write the same way (call.js, ui/page_program.go).
  kitwork.write = function (el, name, v) { writeBinding(el, String(name).toLowerCase(), v); };
  kitwork.text = setText;
  kitwork.forget = function (el) {
    if (!el || el.nodeType !== 1) return;
    delete state(el).written;
    el.querySelectorAll("[data-kitwork-bind]").forEach(function (x) { delete state(x).written; });
  };

  // ---- the event family: data-kitwork-event="<event>[.modifier...]: expr; …" (KITWORK_UI.md §11) ----
  // Dispatch is delegated at the document, one listener per event type; the attribute is read once
  // per value (groupedEvents). The author may write modifiers in any order; the runtime always runs
  // the fixed pipeline of ideaship-final §4:
  //   target (window, document — listen regardless of where the event lands)
  //   → filter (outside, escape, enter, self — a failed filter STOPS here, the event is not swallowed)
  //   → prevent → stop → timing (debounce(n), throttle(n)) → once → run the expression.
  // (The hand-written data-kit-<event>[:modifier…] of template pages went on 26/10/2026.)
  var EVENT_TYPES = ["click", "dblclick", "submit", "input", "change", "keydown", "keyup", "pointerdown", "pointerup", "focusin", "focusout"];
  // An event name is lower case: one a component emits (finish), or a DOM event outside the short list.
  var EVENT_NAME = /^[a-z][a-z0-9-]*$/;
  // The names a component may not emit under: the DOM events a page listens for, so each spelling
  // has one meaning. (Not every DOM event: the kernel's size budget pays for the likely ones.)
  var DOM_EVENT_NAMES = {};
  (EVENT_TYPES.join(" ") + " focus blur scroll load error close toggle paste copy cut drop").split(" ").forEach(function (name) {
    DOM_EVENT_NAMES[name] = true;
  });
  var KEY_FILTER = { escape: function (e) { return e.key === "Escape" || e.key === "Esc" || e.keyCode === 27; }, enter: function (e) { return e.key === "Enter" || e.keyCode === 13; } };
  // ---- key combinations: keydown.mod+k: open() (ideaship-final §4) ----
  // `mod` is Control on Windows and Linux, Command on macOS — EXACTLY one of the two, so
  // Ctrl+Cmd+K is not the shortcut. Every modifier the author did NOT name must be off, which is
  // what makes a combination predictable: mod+k does not also fire on mod+shift+k, so a page can
  // give those two different meanings.
  //
  // A repeat (a held key), an IME composition and the legacy keyCode 229 are refused: they are the
  // browser assembling text, not a person pressing a shortcut. The key itself is compared
  // case-insensitively against event.key, so `mod+k` matches whatever the layout produces for K.
  var COMBO_FLAGS = { ctrl: "ctrlKey", meta: "metaKey", shift: "shiftKey", alt: "altKey" };
  function parseCombo(source) {
    var parts = source.split("+");
    var combo = { mod: false, ctrl: false, meta: false, shift: false, alt: false, key: "" };
    for (var i = 0; i < parts.length; i++) {
      var part = parts[i];
      if (part === "mod") combo.mod = true;
      else if (part in COMBO_FLAGS) combo[part] = true;
      else if (combo.key || !part) return null; // one key, and it must be there
      else combo.key = part;
    }
    return combo.key ? combo : null;
  }
  function comboMatches(combo, e) {
    if (!e || e.repeat || e.isComposing || e.keyCode === 229) return false;
    if (combo.mod) {
      if (!!e.ctrlKey === !!e.metaKey) return false; // exactly one of them
    } else {
      if (!!e.ctrlKey !== combo.ctrl || !!e.metaKey !== combo.meta) return false;
    }
    if (!!e.shiftKey !== combo.shift || !!e.altKey !== combo.alt) return false;
    var key = String(e.key || "").toLowerCase();
    if (combo.key === "escape") return key === "escape" || key === "esc" || e.keyCode === 27;
    if (combo.key === "space") return key === " " || key === "spacebar" || e.keyCode === 32;
    return key === combo.key;
  }

  var OUTSIDE_TYPES = { click: true, dblclick: true, pointerdown: true, pointerup: true, focusin: true };
  // Events that do not bubble: the document hears them in the capture phase, and only the element
  // the event is for answers (an ancestor gets its own event, or none).
  var CAPTURE = { scroll: 1, load: 1, error: 1, toggle: 1, pointerenter: 1, pointerleave: 1 };
  // parseHandler(attr, type, modifiers): attr names the handler (its state key and its label in an
  // error); the modifiers come from the pair's name (click.prevent).
  function parseHandler(attr, type, mods) {
    var h = { attr: attr, type: type, target: "self", outside: false, self: false, key: "", combo: null, prevent: false, stop: false, debounce: 0, throttle: 0, once: false, valid: true };
    for (var i = 0; i < mods.length; i++) {
      var m = mods[i], timing = /^(debounce|throttle)\(([0-9]+)\)$/.exec(m);
      if (m === "window" || m === "document") h.target = m;
      else if (m === "outside") h.outside = true;
      else if (m === "self") h.self = true;
      else if (m === "escape" || m === "enter") h.key = m;
      else if (m.indexOf("+") > 0 && parseCombo(m)) h.combo = parseCombo(m);
      else if (m === "prevent") h.prevent = true;
      else if (m === "stop") h.stop = true;
      else if (m === "once") h.once = true;
      else if (timing) h[timing[1]] = parseInt(timing[2], 10);
      else if (/intersect/.test(type) && /^(leave|half|full|margin\(.+\))$/.test(m)) { } // the observer's, read by the intersect file
      else h.valid = false;
    }
    // A modifier that cannot apply to this event disables the handler rather than misfiring: the
    // server's verify pass already named the mistake.
    if ((h.key || h.combo) && type !== "keydown" && type !== "keyup") h.valid = false;
    if (h.key && h.combo) h.valid = false;
    if (h.outside && !OUTSIDE_TYPES[type]) h.valid = false;
    if (h.self && (h.outside || h.target !== "self")) h.valid = false;
    if (h.debounce && h.throttle) h.valid = false;
    return h;
  }
  // data-kitwork-event="click.prevent: save(); keydown.escape: close(); click-outside: close()": the
  // names are Kitwork UI's, mapped here — double-click, context-menu, focus-in/-out, pointer-down/-up/
  // -enter/-leave to the DOM's; click-outside is click with the outside modifier; intersect-enter /
  // intersect-leave are what the intersect file dispatches, under the pair's own name, on the element
  // itself; ready runs once after the element's first paint; any other name is an event of that name
  // (one a component emits). Modifiers are cut at the dots outside a modifier's (…), as the server's
  // splitEventName does, so intersect-enter.margin(-1.5rem) keeps its argument. Each handler carries
  // its own program, and its state key and label are the attribute plus the pair, so two pairs never
  // share once/debounce/throttle. Read once per VALUE.
  function groupedEvents(el) {
    var raw = el.getAttribute && el.getAttribute("data-kitwork-event"), key = "on=" + raw;
    if (!raw) return [];
    return groupedCache[key] || (groupedCache[key] = grouped(el, "event").filter(function (p) { return p[1]; }).map(function (p) {
      var mods = p[0].split(/\.(?![^(]*\))/), name = mods.shift(), type = name;
      if (name === "click-outside") { type = "click"; mods.push("outside"); }
      else if (/^intersect-(enter|leave)$/.test(name)) { type = p[0]; mods.push("self"); }
      else if (name === "ready") mods.push("once");
      else if (name === "double-click") type = "dblclick";
      else if (/^(context-menu|focus-(in|out)|pointer-(down|up|enter|leave))$/.test(name)) type = name.replace("-", "");
      // a component's event (finish, offline) is its host's, written on that tag alone: one nested inside
      // a host of its kind bubbles past it unheard
      else if (!DOM_EVENT_NAMES[name]) mods.push("self");
      var h = parseHandler("data-kitwork-event " + p[0] + ": " + p[2], type, mods);
      h.program = p[1];
      return h;
    }));
  }
  // pipeline runs one handler for one event, filter first; returns true when :stop ends the walk.
  function pipeline(el, h, e) {
    if (!h.valid) return false;
    var st = state(el);
    if (st.once && st.once[h.attr]) return false;
    if (h.key && !KEY_FILTER[h.key](e)) return false;
    if (h.combo && !comboMatches(h.combo, e)) return false;
    if (h.self && e.target !== el) return false;
    if (h.prevent && e.preventDefault) e.preventDefault();
    if (h.stop && e.stopPropagation) e.stopPropagation();
    var program = h.program;
    if (!program) return h.stop;
    var execute = function (now) {
      if (h.once) { (st.once || (st.once = {}))[h.attr] = true; }
      var was = dispatching;
      dispatching = now ? e : null;
      try { pass(function () { guarded(el, h.attr, function () { runEffect(program, el, e); }); }); }
      finally { dispatching = was; }
      render();
    };
    if (h.debounce) {
      var timers = st.timers || (st.timers = {});
      if (timers[h.attr]) clearTimeout(timers[h.attr]);
      timers[h.attr] = setTimeout(function () { timers[h.attr] = null; execute(false); }, h.debounce);
    } else if (h.throttle) {
      var last = st.lastRun || (st.lastRun = {}), now = Date.now();
      if (last[h.attr] && now - last[h.attr] < h.throttle) return h.stop;
      last[h.attr] = now;
      execute(true);
    } else {
      execute(true);
    }
    return h.stop;
  }
  // One delegated listener per event type, the short list at once and any other name the first time
  // a paint finds an element that listens for it (ensureListener, called from paint).
  var listening = {};
  function ensureListener(type) {
    if (listening[type]) return;
    listening[type] = true;
    var capture = !!CAPTURE[type];
    listen(document, type, function (e) {
      // Direct handlers: the element that owns the attribute is the target or an ancestor of it (the
      // target alone for an event that does not bubble).
      for (var el = e.target; el && el !== document; el = capture ? null : el.parentElement) {
        var hs = groupedEvents(el), stopped = false;
        for (var i = 0; i < hs.length; i++) {
          var h = hs[i];
          if (h.type !== type || h.target !== "self" || h.outside) continue;
          if (pipeline(el, h, e)) stopped = true;
        }
        if (stopped) break;
      }
      if (capture && e.target !== document) return;
      // Listeners elsewhere: :window/:document run wherever the event landed; :outside runs when it
      // landed anywhere but inside the element — and not on the element the very same event mounted.
      // The attribute is found by its selector.
      elsewhere("[data-kitwork-event]", groupedEvents);
      function elsewhere(selector, of) {
        document.querySelectorAll(selector).forEach(function (owner) {
          var hs = of(owner);
          for (var i = 0; i < hs.length; i++) {
            var h = hs[i];
            if (h.type !== type) continue;
            if (h.outside) {
              if (owner === e.target || (owner.contains && owner.contains(e.target))) continue;
              if (isFresh(owner)) continue;
            } else if (h.target === "self") continue;
            pipeline(owner, h, e);
          }
        });
      }
    }, capture);
  }
  EVENT_TYPES.forEach(ensureListener);
  // CAPTURE, so a two-way field writes before any input handler runs. Both listen on the
  // document; in bubble order the handler went first, its paint synced every two-way field FROM the
  // scope — which did not have the new value yet — and wiped what was typed before the field could
  // write it. A search box that also reset its page (input: page = 1) lost every keystroke. The
  // value lands first now, so the handler and its paint see it.
  listen(document, "input", function (e) {
    var el = e.target.closest && e.target.closest(MODEL), m = el && modelOf(el);
    if (!m) return;
    pass(function () { scopeFor(el)[m[0]] = modelValue(el, m); });
    render();
  }, true);
  // Submit gate: a form holding data-state="invalid" does not submit. The kernel only READS that
  // state — the page decides who writes it (the server rendering the attribute, a component, or the
  // browser's own constraint validation reflected onto it). data-kit-validate, the directive that
  // used to write it from an expression, is gone (22/09): 0 sites wrote one, and required/pattern/
  // type do the same work natively. The server still re-checks for truth with ctx.validate, which
  // is the half nothing else can replace.
  listen(document, "submit", function (e) {
    var f = e.target;
    if (f.matches && (f.matches('[data-state="invalid"]') || f.querySelector('[data-state="invalid"]'))) {
      e.preventDefault();
    }
  }, true);

  // (data-kit-api, data-kit-live and data-kit-remember — a JSON fetch into a boundary, SSE patches,
  // page keys kept in localStorage — were optional modules plugged into hooks here. Nothing used
  // them, and they went on 02/10 with their hooks. A site that keeps something in the browser does
  // it in a component of its own.)

  // ONE observer for the whole kernel: DOM is the manifest — a removed subtree is cleaned up at once.
  //
  // REMOVED means no longer on the page. A node that was MOVED — taken out and put back somewhere
  // else, which is how morph reorders a page — appears in removedNodes too, and this callback runs a
  // tick later, after the component inside it has already been initialised in its new place. Cleaning
  // it up then took the component's own listeners while its state stayed seeded, so it never
  // initialised again: after a Drive swap the tree and the tabs on kitwork.io's tree page stopped
  // answering the keyboard (01/10). Only a node that is not connected any more is cleaned up.
  var domObserver = new MutationObserver(function (records) {
    records.forEach(function (record) {
      record.removedNodes.forEach(function (node) {
        if (!node.isConnected) cleanupTree(node);
      });
    });
  });
  domObserver.observe(document.documentElement, { childList: true, subtree: true });
  cleanup(function () { domObserver.disconnect(); });

  // ---- exports + boot ----
  // (kitwork.compile and kitwork.run, the kernel's own parser and walker for tools, went with them on 15/11/2026: the
  // server writes what the kernel runs.)
  kitwork.scope = scope;
  kitwork.scopeFor = scopeFor;
  kitwork.render = render;
  kitwork.set = function (k, v) { pass(function () { scope[k] = v; }); render(); };
  kitwork.destroy = function () {
    while (globalCleanups.length) {
      try { globalCleanups.pop()(); } catch (_) { }
    }
    cleanupTree(document.documentElement);
    kitwork.hydrate = false;
    kitwork.runtime.booted = false;
    kitwork.runtime.loaded = false;
  };

  function boot() {
    if (runtimeMeta.booted) return kitwork;
    runtimeMeta.booted = true;
    seedModels();
    render();
    startHooks.slice().forEach(function (start) {
      try { start(); } catch (error) {
        if (runtimeMeta.development && window.console) {
          console.error("kitwork: module start failed", error);
        }
      }
    });
    document.dispatchEvent(new CustomEvent("kitwork:ready", {
      detail: { runtime: runtimeMeta.info(), modules: Object.keys(modules) }
    }));
    return kitwork;
  }
  kitwork.start = boot;

  Object.defineProperty(kitwork, "internal", {
    value: {
      cleanup: cleanup,
      listen: listen,
      cleanupTree: cleanupTree,
      state: state,
      // the grouped reader, for Drive (a link with a click entry is not driven), the intersect file
      // and the corpus test — the one copy of it the client has
      grouped: { pairs: pairs, component: component, loop: loop },
      // what the expressions the server writes as JavaScript read and call by a key
      read: readKey,
      call: callKey
    },
    configurable: true,
    enumerable: false
  });
  kitwork.module("kernel", {
    start: boot,
    render: render
  });
  // After every swap: seed any new inputs, re-render expressions.
  listen(document, "kitwork:load", function () { seedModels(); render(); });
})();
