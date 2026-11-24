/* autocomplete — type, see what matches, choose one (docs/components/autocomplete.md). The engine
 * draws its input as a combobox (aria-controls its listbox, aria-expanded following opened), a hidden
 * input after it that the form sends (the chosen option's value), its popover as the listbox, each
 * option a div with role option and data-value, its empty a role="status" kept hidden:
 *
 *   <autocomplete name="district" value="q3" required="true" change="loadWards()">
 *     <input placeholder="Choose a district" />
 *     <popover place="below-left"><option value="q1">District 1</option>…<empty>None</empty></popover>
 *   </autocomplete>
 *
 * Typing opens the list and, unless the input has an input event of the page's (filter: false, the
 * page filling the list), keeps the options whose words hold what is typed, without accents or case
 * ("quan 1" finds "Quận 1"); a choice typed over is undone. Down and Up light an option
 * (aria-activedescendant, the focus staying in the input), Enter chooses it, Escape closes the list
 * and, closed, clears what is typed; a press on an option chooses it, one outside closes the list.
 * Choosing writes the hidden input, shows the option's words, marks it aria-selected and sends
 * change on the autocomplete, $event.value and $event.label; the input's own change does not reach
 * the page. A required autocomplete with words typed but nothing chosen is invalid.
 *
 * Two-way (value="{{ district }}", a var of the page): the hidden input is the var's field, so a
 * choice writes the var through the scope, and after each paint the component reads the field again:
 * the page writing the var shows the option it names. A page that searches binds the input's own
 * words (value="{{ keyword }}"), fills its options as a list of its data, and gets the option's words
 * written in keyword when one is chosen. After each paint the options are read again (the kernel
 * holds a list's template out of the page): a row it made gets an id of its own when lit, the chosen
 * one is marked, the empty shows when nothing is there for what is typed.
 *
 *   opened     the list shows
 *   close()    close the list
 */
var MARKS = new RegExp("[" + String.fromCharCode(0x300) + "-" + String.fromCharCode(0x36f) + "]", "g");
var BARRED_D = new RegExp("[" + String.fromCharCode(0x110) + String.fromCharCode(0x111) + "]", "g");

// plain is s without accents or case: what typing is compared on
function plain(s) {
  return String(s).normalize("NFD").replace(MARKS, "").replace(BARRED_D, "d").toLowerCase().replace(/\s+/g, " ").trim();
}

// words is an option's words: its first element's when it has one (a name above a city), else its own
function words(option) {
  var first = option.firstElementChild;
  return (first ? first.textContent : option.textContent).replace(/\s+/g, " ").trim();
}

var autocompleteDef = {
  opened: false,
  close: function () { this.opened = false; },

  init: function (context) {
    var scope = this, host = context.host;
    var input = host.querySelector("[role=combobox]");
    var listbox = host.querySelector("[role=listbox]");
    if (!input || !listbox) return;
    var hidden = input.nextElementSibling && input.nextElementSibling.type === "hidden" ? input.nextElementSibling : null;
    var empty = listbox.querySelector("[role=status]");
    var english = (document.documentElement.lang || "").indexOf("en") === 0;
    var chosenWords = input.value, active = null, chosen = hidden ? hidden.value : "", fresh = 0;

    var all = function () { return Array.prototype.slice.call(listbox.querySelectorAll("[role=option]")); };
    // model is the var an element's value is bound to two-way (data-kitwork-bind="value: district"), "" for none
    var model = function (el) {
      var found = /(?:^|;)\s*value:\s*([A-Za-z_$][\w$]*)\s*(?:;|$)/.exec(el && el.getAttribute("data-kitwork-bind") || "");
      return found ? found[1] : "";
    };
    // write gives el value and, when it is a var's field, the var (the page repaints from it)
    var write = function (el, value) {
      if (!el) return;
      el.value = value;
      var name = model(el);
      if (name) scope[name] = value;
    };
    var shown = function () { return all().filter(function (o) { return !o.hidden; }); };
    var light = function (option) {
      if (active) active.removeAttribute("data-active");
      active = option;
      if (option) {
        if (!option.id || document.getElementById(option.id) !== option) option.id = listbox.id + "-row-" + (++fresh);
        option.setAttribute("data-active", "");
        input.setAttribute("aria-activedescendant", option.id);
        if (option.scrollIntoView) option.scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };
    var valid = function () {
      if (!input.required) return;
      var undone = input.value.trim() !== "" && (!hidden || hidden.value === "");
      input.setCustomValidity(undone ? (english ? "Choose one from the list" : "Chọn một mục trong danh sách") : "");
    };
    var send = function (value, label) {
      var event = new Event("change", { bubbles: true });
      event.value = value;
      event.label = label;
      host.dispatchEvent(event);
    };
    var choose = function (option) {
      var value = option ? option.getAttribute("data-value") || "" : "", label = option ? words(option) : "";
      chosen = value;
      write(hidden, value);
      chosenWords = label;
      write(input, label);
      all().forEach(function (o) { o.setAttribute("aria-selected", o === option ? "true" : "false"); });
      light(null);
      scope.opened = false;
      valid();
      send(value, label);
    };
    var filter = function () {
      if (scope.filter !== false) {
        var typed = input.value === chosenWords ? "" : plain(input.value); // the chosen words shown are no search
        all().forEach(function (o) { o.hidden = typed !== "" && plain(o.textContent).indexOf(typed) < 0; });
      }
      // a page that searches has nothing to say before anything is typed
      if (empty) empty.hidden = shown().length > 0 || scope.filter === false && input.value.trim() === "";
      if (active && (active.hidden || !listbox.contains(active))) light(null);
    };
    // sync reads the field and the options again after a paint: the var the page wrote, a list it filled
    var sync = function () {
      if (hidden && hidden.value !== chosen) {
        chosen = hidden.value;
        var named = null;
        all().forEach(function (o) { if (!named && o.getAttribute("data-value") === chosen) named = o; });
        if (named || chosen === "") {
          chosenWords = named ? words(named) : "";
          write(input, chosenWords);
          valid();
        }
      }
      all().forEach(function (o) { o.setAttribute("aria-selected", chosen !== "" && o.getAttribute("data-value") === chosen ? "true" : "false"); });
      filter();
      context.afterRender(sync);
    };

    context.listen(input, "input", function () {
      if (hidden && hidden.value !== "" && input.value !== chosenWords) {
        chosen = "";
        write(hidden, "");
        all().forEach(function (o) { o.setAttribute("aria-selected", "false"); });
        send("", "");
      }
      filter();
      scope.opened = true;
      valid();
    });
    context.listen(input, "change", function (event) { event.stopPropagation(); });
    context.listen(input, "click", function () { if (!scope.opened) { filter(); scope.opened = true; } });
    context.listen(input, "keydown", function (event) {
      var list = shown(), at = list.indexOf(active);
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        if (!scope.opened) { filter(); scope.opened = true; list = shown(); at = list.indexOf(active); }
        if (!list.length) return;
        var step = event.key === "ArrowDown" ? 1 : -1;
        light(list[at < 0 ? (step > 0 ? 0 : list.length - 1) : (at + step + list.length) % list.length]);
      } else if (event.key === "Enter" && scope.opened && active) {
        event.preventDefault();
        choose(active);
      } else if (event.key === "Escape") {
        if (scope.opened) {
          event.preventDefault();
          light(null);
          scope.opened = false;
        } else if (input.value !== "") {
          event.preventDefault();
          write(input, "");
          if (hidden && hidden.value !== "") choose(null);
          else valid();
        }
      }
    });
    context.listen(listbox, "pointerdown", function (event) {
      var option = event.target.closest && event.target.closest("[role=option]");
      if (!option || !listbox.contains(option)) return;
      event.preventDefault(); // the focus stays in the input
      choose(option);
    });
    context.listen(document, "pointerdown", function (event) {
      if (scope.opened && !host.contains(event.target)) { light(null); scope.opened = false; }
    });
    context.listen(host, "focusout", function (event) {
      if (scope.opened && !host.contains(event.relatedTarget)) { light(null); scope.opened = false; }
    });
    valid();
    context.afterRender(sync);
  }
};

window.kitwork.component("autocomplete", autocompleteDef);
