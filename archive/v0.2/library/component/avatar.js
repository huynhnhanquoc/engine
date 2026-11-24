/* avatar — a face that falls back to its initials (docs/components/avatar.md). The server draws
 * the image over the initials of its alt; an image that fails to load is hidden here, so the
 * initials show. An avatar with no image loads no file. (No onerror in the HTML: a page has no
 * inline script.)
 */
var avatarDef = {
  init: function (context) {
    context.owned("img").forEach(function (img) {
      var fail = function () { img.hidden = true; };
      if (img.complete && img.getAttribute("src") && img.naturalWidth === 0) fail();
      else context.listen(img, "error", fail);
    });
  }
};

window.kitwork.component("avatar", avatarDef);
