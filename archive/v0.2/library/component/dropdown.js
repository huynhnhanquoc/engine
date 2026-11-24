/* dropdown component — something that opens and closes (KITWORK_UI §7, docs/components/
 * dropdown.md). It has no look of its own: what is written inside it is its interface, and the
 * behaviour is written in the markup, on the dropdown's tag itself:
 *
 *   <dropdown as="$account" click-outside="close()" keydown:escape="close()">
 *     <button click="toggle()" expanded="{{ opened }}">Quốc</button>
 *     <popover place="below-right">…</popover>
 *   </dropdown>
 *
 * The engine writes data-kitwork-component="dropdown: $account", the events in data-kitwork-event,
 * aria-expanded in data-kitwork-bind, and the popover with data-kitwork-bind="show: opened", so the
 * popover shows while the dropdown is opened. Through the alias the rest of the page can open or
 * close it ($account.open()). A verb is a function, a participle a state (26/10/2026): open() opens,
 * opened says it is.
 *
 *   opened     true while it is open
 *   open()     open
 *   close()    close
 *   toggle()   open when closed, close when open
 */
var dropdownDef = {
  opened: false,

  open: function () { this.opened = true; },
  close: function () { this.opened = false; },
  toggle: function () { this.opened = !this.opened; }
};

window.kitwork.component("dropdown", dropdownDef);
