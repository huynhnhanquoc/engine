;(function (window, document) {
  "use strict";
  // Prototype content binding, not a template loader. Never execute or hydrate inserted markup.
  // Rebuild a small HTML vocabulary into fresh nodes: source attributes are never copied wholesale.
  var kitwork = window.kitwork;
  if (!kitwork || kitwork.module("html")) return;
  var namespace = "http://www.w3.org/1999/xhtml";
  var tags = "a b blockquote br code div em h2 h3 h4 h5 h6 hr i li ol p pre s span strong u ul".split(" ");
  var hosts = "ARTICLE ASIDE DIV FOOTER HEADER MAIN NAV SECTION SPAN".split(" ");
  var written = new WeakMap(); // Only the current source; no global cache of nodes or past HTML.

  function safeLink(source) {
    if (!source) return null;
    try {
      var url = new URL(source, window.location.href);
      return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
    } catch (_) { return null; }
  }

  function fragment(source) {
    if (source.length > 65536) throw new RangeError("hydrate: html exceeds 65536 characters");
    var template = document.createElement("template");
    // A template's contents stay inert. Only rebuilt, allowed nodes ever enter the live document.
    template.innerHTML = source;
    var result = document.createDocumentFragment(), remaining = 4096;
    function copy(node, into, depth) {
      if (--remaining < 0 || depth > 32) throw new RangeError("hydrate: html exceeds the node or depth limit");
      if (node.nodeType === 3) { into.appendChild(document.createTextNode(node.nodeValue)); return; }
      if (node.nodeType !== 1 || node.namespaceURI !== namespace) return;
      var tag = node.localName;
      if (tags.indexOf(tag) < 0) return; // Drop the entire subtree, including SVG, MathML and templates.
      var clean = document.createElement(tag);
      if (node.hasAttribute("title")) clean.setAttribute("title", node.getAttribute("title"));
      if (tag === "a") {
        var href = safeLink(node.getAttribute("href"));
        if (href) { clean.setAttribute("href", href); clean.setAttribute("rel", "noopener noreferrer"); }
      }
      for (var child = node.firstChild; child; child = child.nextSibling) copy(child, clean, depth + 1);
      into.appendChild(clean);
    }
    for (var node = template.content.firstChild; node; node = node.nextSibling) copy(node, result, 0);
    return result;
  }

  function write(element, value) {
    if (element.namespaceURI !== namespace || hosts.indexOf(element.tagName) < 0) {
      throw new Error("hydrate: html needs an HTML content host such as div or span");
    }
    if (value == null) value = "";
    if (typeof value !== "string") throw new TypeError("hydrate: html expects a string");
    if (written.has(element) && written.get(element) === value) return;
    var clean = fragment(value); // Validate and sanitize before touching the current subtree.
    if (!written.has(element)) kitwork.onCleanup(element, function () { written.delete(element); });
    for (var child = element.firstChild; child; child = child.nextSibling) kitwork.internal.cleanupTree(child);
    element.replaceChildren(clean);
    written.set(element, value);
  }

  kitwork.module("html", { write: write });
  if (kitwork.runtime.booted) kitwork.render();
})(window, document);
