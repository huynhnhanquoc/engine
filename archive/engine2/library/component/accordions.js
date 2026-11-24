/* accordions — a group of accordions with an alias (docs/components/accordions.md). Opening one
 * closes the others by the browser's own rule: every <details> of the group has the name the engine
 * gives it. What needs JavaScript is closing them all from elsewhere:
 *
 *   <accordions as="$faq">…</accordions>
 *   <button click="$faq.close()">Close all</button>
 *
 * A group without an alias loads no file.
 *
 *   close()    close every accordion of this group, no other on the page
 */
var accordionsDef = {
  close: function () {
    var host = window.kitwork.context(this).host;
    host.querySelectorAll(":scope > details").forEach(function (item) { item.open = false; });
  }
};

window.kitwork.component("accordions", accordionsDef);
