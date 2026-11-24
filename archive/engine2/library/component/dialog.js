/* dialog — asking before acting, and any modal a page draws (docs/components/dialog.md). The page
 * draws it once, often in overlay.ui, and any page opens it in one line:
 *
 *   <dialog as="$dialog" keydown:escape="close()">
 *     <box place="fill" background="black/50" click="close()" />
 *     <card place="center" gap="4" padding="6">
 *       <text heading="2">{{ value.title }}</text>
 *       <button click="close()">No</button><button click="confirm()">Yes</button>
 *     </card>
 *   </dialog>
 *   <button click="$dialog.open({ title: 'Delete order ' + order.id + '?', confirm: () => remove(order.id) })">
 *
 * The engine writes it as the browser's <dialog> with data-kitwork-component="dialog: $dialog" and
 * aria-labelledby its first heading. opened is the browser's own: writing it (open(), close(), or
 * opened = …) shows the dialog with showModal() or closes it, so it is on the top layer, the page
 * behind it inert, focus kept inside it and given back to what opened it when it closes. The
 * browser's own closing on Escape is held back (cancel), so the author's keydown:escape decides and
 * opened stays true to what shows; one the browser closes all the same (a second Escape with no
 * click between, which Chrome does not let a page hold back) sets opened false. The close event
 * comes after the closing, as a task of its own, so it may come after the dialog has opened again:
 * it is read only while the dialog is closed. While it is closed the kernel paints nothing inside it
 * (kernel.js, asleep): what it holds reads value, which may throw on null (Object.keys(value.x)).
 *
 *   opened     true while it is open
 *   value      what open(x) was given; null while closed
 *   open(x)    keep x as value, then open
 *   close()    close; value null
 *   confirm()  run value.confirm, then close (value.confirm is the arrow the call wrote)
 */
var OPENED = Symbol("opened");

var dialogDef = {
  value: null,

  get opened() { return this[OPENED] === true; },
  set opened(v) {
    v = !!v;
    if (this[OPENED] === v) return;
    this[OPENED] = v;
    var host = window.kitwork.context(this).host;
    if (v) {
      if (!host.open) host.showModal();
    } else {
      this.value = null;
      if (host.open) host.close();
    }
  },

  open: function (x) {
    this.value = x === undefined ? null : x;
    this.opened = true;
  },
  close: function () { this.opened = false; },
  confirm: function () {
    this.value.confirm();
    this.opened = false;
  },

  init: function (context) {
    var scope = this;
    context.listen(context.host, "cancel", function (event) { event.preventDefault(); });
    context.listen(context.host, "close", function () { if (scope.opened && !context.host.open) scope.opened = false; });
  }
};

window.kitwork.component("dialog", dialogDef);
