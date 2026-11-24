package ui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui"
)

// pixel is a gif of one pixel, an image that loads.
const pixel = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

// avatarPage holds an avatar whose image fails, one whose image loads, one with no image, one with no name, one
// named beside, and a group of four showing two.
var avatarPage = map[string]string{"content.ui": `<content>` +
	`<avatar size="large"><image value="/missing.png" alt="Lan Nguyễn" /><badge place="bottom-right" color="success" alt="Online"><icon value="point-fill" /></badge></avatar>` +
	`<avatar><image value="` + pixel + `" alt="Pixel" /></avatar>` +
	`<avatar><image value="{{ null }}" alt="Quốc" /></avatar>` +
	`<avatar radius="medium"><image value="" alt="" /></avatar>` +
	`<box flex="row"><avatar size="small"><image value="/a.png" alt="{{ 'Minh' }}" /></avatar><text>{{ 'Minh' }}</text></box>` +
	`<avatars max="2" size="small"><avatar for="m of ['An', 'Bình', 'Chi', 'Dũng']"><image value="{{ '/' + m + '.png' }}" alt="{{ m }}" /></avatar></avatars>` +
	`</content>`}

// An avatar is two spans (16/11/2026, round 5 of the 13/11 review, dialog + catalogue, slice 4; its size a word, as a
// text's, the owner's choice): the outer one sized by its word and holding the corner badges, the face holding the
// image over the initials of its alt on the tone its name picks; no image draws the initials only and loads no file,
// no name a person's icon, a name written beside it an image a screen reader skips (alt=""). A group gives its size,
// draws max and a "+N" a screen reader skips, and is named once. It was "thẻ <avatar> không có trong từ vựng".
// CONTROLS: the initials left out, red; the group drawing every avatar, red.
func TestAnAvatarIsAFaceOverItsInitials(t *testing.T) {
	document, warnings := drawWarned(t, avatarPage, value.Value{}, ui.Head{})
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	holds(t, document,
		`<span class="avatar avatar-large" data-kitwork-component="avatar"><span class="avatar-face avatar-tone-5"><img src="/missing.png" alt="Lan Nguyễn" loading="lazy" decoding="async"><span aria-hidden="true">LN</span></span><span class="badge flex-row place-bottom-right color-success" aria-label="Online"><i class="icon-point-fill" aria-hidden="true"></i></span></span>`,
		`<span class="avatar avatar-medium"><span class="avatar-face avatar-tone-3"><span aria-hidden="true">Q</span></span></span>`,
		`<span class="avatar avatar-medium"><span class="avatar-face avatar-tone-2 radius-medium"><i class="icon-user" aria-hidden="true"></i></span></span>`,
		`<img src="/a.png" alt="" loading="lazy" decoding="async"><span aria-hidden="true">M</span></span></span><p>Minh</p>`,
		`<span class="avatars" role="group" aria-label="An, Bình và 2 người khác">`,
		`<img src="/Bình.png" alt="Bình" loading="lazy" decoding="async"><span aria-hidden="true">B</span></span></span><span class="avatar avatar-small" aria-hidden="true"><span class="avatar-face">+2</span></span></span>`,
		`/_kitwork/components/avatar.`, `.icon-user{`,
	)
	if strings.Contains(document, `alt="Chi"`) {
		t.Error("the group drew an avatar past its max")
	}
	_, quiet := render(t, map[string]string{"/app/site/content.ui": `<content><avatar><image value="{{ null }}" alt="Quốc" /></avatar></content>`}, value.Value{})
	if strings.Contains(quiet, "data-kitwork-component") {
		t.Error("an avatar with no image loads its file")
	}
}

// What an avatar and a group refuse, each where it is written (docs/components/avatar.md, avatars.md, "Compiler
// kiểm"), and what they warn of. CONTROL: an avatar with no image let through, red.
func TestWhatAnAvatarRefuses(t *testing.T) {
	for _, c := range []struct{ content, error string }{
		{`<content><avatar><badge place="top-right">1</badge></avatar></content>`, "content.ui:1:10: avatar cần một image"},
		{`<content><avatar><image value="/a.png" alt="a" /><image value="/b.png" alt="b" /></avatar></content>`,
			"content.ui:1:50: avatar có đúng một image"},
		{`<content><avatar><image value="/a.png" /></avatar></content>`, "content.ui:1:18: image của avatar cần alt"},
		{`<content><avatar><image value="/a.png" alt="a" /><button click="x()">b</button></avatar></content>`,
			"content.ui:1:50: <button> trong avatar: con của avatar là một image và các badge ở góc; nút đặt ngoài avatar"},
		{`<content><avatar><image value="/a.png" alt="a" /><badge>1</badge></avatar></content>`,
			"content.ui:1:50: badge trong avatar đặt ở một góc"},
		{`<content><avatar><image value="/a.png" alt="a" /><badge place="center">1</badge></avatar></content>`,
			`content.ui:1:57: badge trong avatar đặt ở một góc: top-left, top-right, bottom-left hay bottom-right, không "center"`},
		{`<content><avatar size="10"><image value="/a.png" alt="a" /></avatar></content>`,
			"content.ui:1:18: size của avatar là một chữ: mini, small, medium, large, huge"},
		{`<content><avatar><image value="/a.png" alt="a" /><avatar><image value="/b.png" alt="b" /></avatar></avatar></content>`,
			"content.ui:1:50: avatar trong avatar"},
		{`<content><avatars><avatar size="small"><image value="/a.png" alt="a" /></avatar></avatars></content>`,
			"content.ui:1:27: size trên avatar trong avatars"},
		{`<content><avatars max="0"><avatar><image value="/a.png" alt="a" /></avatar></avatars></content>`,
			"content.ui:1:19: max của avatars là số avatar hiện ra, từ 1"},
		{`<content><avatars gap="2"><avatar><image value="/a.png" alt="a" /></avatar></avatars></content>`,
			"content.ui:1:19: gap trên avatars"},
		{`<content><avatars><box>x</box></avatars></content>`, "content.ui:1:19: <box> trong avatars: con của avatars là các avatar"},
	} {
		err := renderError(t, map[string]string{"/app/site/content.ui": c.content})
		if err == nil || !strings.HasPrefix(err.Error(), c.error) {
			t.Errorf("%s:\n %v\nwant an error beginning\n %s", c.content, err, c.error)
		}
	}
	_, warnings := drawWarned(t, map[string]string{"content.ui": `<content><avatar><image value="/a.png" alt="a" />` +
		`<badge place="top-right" alt="x"><icon value="point-fill" /></badge><badge place="top-right">2</badge></avatar></content>`}, value.Value{}, ui.Head{})
	if !strings.Contains(strings.Join(warnings, "\n"), `hai badge cùng place="top-right" trên avatar`) {
		t.Errorf("warnings %q, want one for two badges on a corner", warnings)
	}
}

// In a browser, an avatar whose image fails shows its initials (the image hidden), one whose image loads shows it, one
// with no image its initials; the faces are the sizes their words say, round. CONTROL: avatar.js hiding nothing, red.
func TestBrowserAnAvatarFallsBackToItsInitials(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the browser proof in short mode")
	}
	browser := headlessBrowser()
	if browser == "" {
		t.Skip("Chrome, Chromium, or Edge is not installed")
	}
	document, _ := drawWarned(t, avatarPage, value.Value{}, ui.Head{})
	page := strings.Replace(document, "</body>", `<script>
window.addEventListener("load", function () {
  fetch("/hold").then(function () {
    var seen = Array.prototype.slice.call(document.querySelectorAll("main > .avatar"), 0, 3).map(function (a) {
      var img = a.querySelector("img"), face = a.querySelector(".avatar-face");
      return (img ? (img.hidden ? "hidden" : "shown") : "none") + " " + a.getBoundingClientRect().width + " " + getComputedStyle(face).borderRadius;
    });
    fetch("/report", { method: "POST", body: JSON.stringify(seen) });
  });
});
</script></body>`, 1)
	var seen []string
	if err := json.Unmarshal(runInBrowser(t, browser, page), &seen); err != nil {
		t.Fatal(err)
	}
	if want := "hidden 48 999px|shown 40 999px|none 40 999px"; strings.Join(seen, "|") != want {
		t.Errorf("the avatars are %q, want %s", seen, want)
	}
}
