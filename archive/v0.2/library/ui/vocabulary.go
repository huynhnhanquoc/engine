package ui

import (
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

// The words of .ui an attribute of a tag of Kitwork may be (15/11/2026, the owner, round 3 of the 13/11 review): an
// attribute no tag of .ui reads is the page's error at its place, as the first engine had it (engine/ui compile.go,
// "thuộc tính … không có trên <…>"), where engine2 dropped it with one line of the log, so a misspelt placeholder
// (placeh older) drew a field with no placeholder in silence. A word of .ui engine2 does not draw yet, or does not
// draw on this tag, is still that line (unreadNow). An attribute of a piece of the site (a tag with a dash) is its
// input, the piece's to read.

// uiWords are the words a tag of .ui reads besides events, presentation, booleans and what goes to the HTML as
// written (passed): the first engine's (tagWords, passThrough, its control words) and engine2's own.
var uiWords = func() map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.Fields(`
		for if show as source component delay key
		value alt exact reload new-tab expanded type heading strikethrough language open method
		name min max step pattern placeholder required readonly disabled autofocus checked autocomplete inputmode
		multiple rows selected datetime role id
		address align centre columns duration empty end formatted height html-source lazy linear max-width pin place
		remember separator spellcheck start state target thickness throttle transition width wrap italic underline
		suggest persist interval aria-atomic aria-roledescription opened`) {
		words[word] = true
	}
	return words
}()

// uiWord reports name a word of .ui: an event and its modifiers, a presentation word or show with or without a
// prefix (md:padding), a boolean, a word that goes to the HTML as written, or one of uiWords.
func uiWord(name string) bool {
	if isEvent(name) {
		return true
	}
	base := name[strings.LastIndex(name, ":")+1:]
	if base == "show" || stylesheet.IsPresentation(base) {
		return true
	}
	if base != name {
		return false // only presentation, show and an event take a prefix
	}
	return uiWords[name] || booleanAttributes[name] || passed[name]
}

// notAWord is the error of an attribute of a tag of Kitwork that is no word of .ui, saying the word it and the next
// attribute make when written together (placeh older: placeholder, cut by a space), else the nearest word.
func notAWord(n *Node, a *Attribute) error {
	text := "thuộc tính " + a.Name + " không có trong .ui"
	for i := range n.Attributes {
		if &n.Attributes[i] == a && i+1 < len(n.Attributes) {
			if next := n.Attributes[i+1]; uiWord(a.Name + next.Name) {
				return at(n, a, "%s: có phải %s, viết liền? (%s %s tách bởi một khoảng trắng)", text, a.Name+next.Name, a.Name, next.Name)
			}
		}
	}
	words := stylesheet.Attributes()
	for word := range uiWords {
		words = append(words, word)
	}
	if nearest := value.NearestName(a.Name, words); nearest != "" {
		text += ": có phải " + nearest + "?"
	}
	return at(n, a, "%s", text)
}
