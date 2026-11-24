package ui

// <editor>, rich text written in place (decisions of 20–21/10/2026), moved from the first engine's ui/editor.go
// on 06/11/2026, the last of slice 4; its JavaScript is library/component's editor.js and its parts.
//
// The author draws everything: the writing area (one <textarea> inside, which the engine writes as a
// div.richtext the component writes in and a field the form posts, both holding the value cleaned), the toolbars
// (a box, card or section with alt holding buttons whose click is one command, toggle('bold')), their groups, a
// dropdown of more commands that call the editor through its alias, the link's popover (its address input and
// its error). The engine finds each, names it and wires it: the toolbar's role and the area it controls, each
// group's role, a toolbar's roving tabindex, data-kitwork-format or data-kitwork-command on each command,
// aria-pressed, aria-keyshortcuts, the popover's id and role, the field's label and hint on the area.
//
// Not moved yet: what the first engine checked of the editor's names read or called elsewhere on the page (an
// alias read the editor does not have, a command called outside it, a write to its state) and its warnings
// (an editor with no button, a popover no link() opens, a pressed: style, a submit locked on empty).

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/library/ui/richtext"
	"github.com/kitwork/engine2/library/ui/stylesheet"
)

var (
	editorMethods = []string{"toggle", "align", "rule", "link", "unlink", "undo", "redo", "close"}
	editorNames   = []string{"empty", "words", "characters", "unsaved", "opened", "address", "format", "dropped"}
	// editorFormats are how a post is seen and typed: rich text, Markdown or HTML, one value on the wire either way
	editorFormats = []string{"rich", "markdown", "html"}
)

const (
	editorFormatChoices = "rich, markdown hay html"
	editorFormatList    = "bold, italic, underline, strikethrough, code, quote, heading 2 tới 6, list"
	editorSides         = "align('left'), align('center') hay align('right')"
	editorHeadingLevels = "toggle('heading', 2) tới toggle('heading', 6)"
	editorFormatSwitch  = `<button click="format = 'markdown'">`
)

// editorCallExample is each command as a button calls it.
var editorCallExample = map[string]string{
	"toggle": "toggle('bold')", "align": "align('center')", "rule": "rule()", "link": "link()", "unlink": "unlink()",
	"undo": "undo()", "redo": "redo()", "close": "close()",
}

// editorKeyshortcuts are the keys the component answers, said on the buttons that do the same.
var editorKeyshortcuts = map[string]string{
	"bold": "Control+B", "italic": "Control+I", "underline": "Control+U", "link": "Control+K", "undo": "Control+Z", "redo": "Control+Shift+Z",
}

// editorFormatProblem is what is wrong with a format as written ("" for rich, markdown and html); written says
// how the place writes a value.
func editorFormatProblem(value string, written func(string) string) string {
	at := written(value)
	switch value {
	case "rich", "markdown", "html":
		return ""
	case "code":
		return at + ": code không phải một cách viết: mã trong bài là toggle('code') trong dòng, toggle('code', 'block') một khối; xem và sửa bài bằng HTML là " + written("html")
	case "source", "source-code", "raw", "html-source":
		return at + ": cách viết bằng mã HTML tên là html, viết " + written("html")
	case "md":
		return at + ": md là chữ viết tắt; .ui viết đủ chữ: " + written("markdown")
	case "visual", "wysiwyg", "text", "richtext", "rich-text", "formatted":
		return at + ": cách viết thấy như đọc tên là rich, viết " + written("rich")
	}
	return at + ": editor không có cách viết " + value + "; có " + editorFormatChoices + nearestWords(value, editorFormats)
}

// editorScope is one editor as planned when its host is read: its parts, by node, and how each is wired.
type editorScope struct {
	node   *Node
	number int // its place among the page's editors, for its ids

	area                         *Node // the <textarea> the author wrote
	areaID                       string
	name                         string
	value                        *Attribute // nil: it starts empty
	placeholder, alt             *Attribute
	required, readonly, disabled bool
	rows                         int // its least height in lines, 0 when not written

	toolbars map[*Node]bool
	groups   map[*Node]bool          // the <group>s its toolbars hold
	buttons  map[*Node]*editorButton // in a toolbar, in the link's popover, in a toolbar's dropdown
	roving   map[*Node]string        // the tabindex of each button of a toolbar, a dropdown's trigger too
	menus    map[*Node]*editorMenu   // a toolbar's dropdown that holds commands, by the dropdown
	triggers map[*Node]*editorMenu   // the button that opens one
	popovers map[*Node]*editorMenu   // its popover
	selects  map[*Node]bool          // a <select value="{{ format }}">

	popover      *Node // the link's
	popoverID    string
	address      *Node
	addressAlt   *Attribute
	addressError *Node
	errorID      string

	format string // the format it starts in
	field  *fieldIDs
}

// editorButton is one button that runs a command of the editor.
type editorButton struct {
	node    *Node
	click   *Attribute
	command string // toggle, align, rule, link, unlink, undo, redo, close; format for a switch of format
	format  string // the format a toggle names (bold, heading-4, code-block…); "" for every other command
	value   string // the format a switch writes (rich, markdown, html)
	wire    string // data-kitwork-command: the command, an align's with its side (align-center)
	popover bool   // it stands in the link's popover
	apply   bool   // link(address): it links the selection to the address
	path    []*Node
}

// pressed says the button is on or off: a format, a side, a format of writing.
func (b *editorButton) pressed() bool {
	return b.format != "" || b.command == "align" || b.command == "format"
}

// editorMenu is a toolbar's dropdown holding commands of the editor, called through its alias.
type editorMenu struct {
	node, trigger, popover *Node
	popoverID              string
	path                   []*Node
	buttons                []*editorButton
}

// editorArg is one argument of a command as written: a string, a number or a bare name.
type editorArg struct {
	text     string
	str      bool
	number   float64
	isNumber bool
	name     string
}

// written is the argument as a message shows it.
func (a editorArg) written() string {
	switch {
	case a.str:
		return quote(a.text)
	case a.isNumber:
		return strconv.FormatFloat(a.number, 'f', -1, 64)
	case a.name != "":
		return a.name
	}
	return "…"
}

// editorCall is a click read as the editor reads it: one call with its arguments, through an alias or not
// (toggle('heading', 2), $body.toggle('heading', 4)), or format = value.
type editorCall struct {
	alias, method string
	args          []editorArg
	assign        bool
}

var (
	editorCallForm   = regexp.MustCompile(`^(\$[A-Za-z][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)$`)
	editorAssignForm = regexp.MustCompile(`^format\s*=\s*([^=].*)$`)
)

// readEditorCall reads a click's source; false for anything else.
func readEditorCall(source string) (editorCall, bool) {
	s := strings.TrimSpace(source)
	if bare, ok := wrapped(s); ok {
		s = bare
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if m := editorAssignForm.FindStringSubmatch(s); m != nil {
		args, ok := editorArgs(m[1])
		if !ok || len(args) != 1 {
			return editorCall{}, false
		}
		return editorCall{method: "format", args: args, assign: true}, true
	}
	m := editorCallForm.FindStringSubmatch(s)
	if m == nil {
		return editorCall{}, false
	}
	args, ok := editorArgs(m[3])
	if !ok {
		return editorCall{}, false
	}
	return editorCall{alias: strings.TrimSuffix(m[1], "."), method: m[2], args: args}, true
}

// editorArgs splits what a call holds at its commas outside quotes and brackets.
func editorArgs(s string) ([]editorArg, bool) {
	if strings.TrimSpace(s) == "" {
		return nil, true
	}
	var pieces []string
	depth, start := 0, 0
	var in byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case in != 0:
			if c == '\\' {
				i++
			} else if c == in {
				in = 0
			}
		case c == '\'' || c == '"' || c == '`':
			in = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			pieces = append(pieces, s[start:i])
			start = i + 1
		}
	}
	if in != 0 || depth != 0 {
		return nil, false
	}
	pieces = append(pieces, s[start:])
	args := make([]editorArg, 0, len(pieces))
	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		switch {
		case piece == "":
			return nil, false
		case len(piece) >= 2 && (piece[0] == '\'' || piece[0] == '"') && piece[len(piece)-1] == piece[0] && !strings.ContainsAny(piece[1:len(piece)-1], `\`+string(piece[0])):
			args = append(args, editorArg{text: piece[1 : len(piece)-1], str: true})
		default:
			if number, err := strconv.ParseFloat(piece, 64); err == nil {
				args = append(args, editorArg{number: number, isNumber: true})
			} else if identifier(piece) {
				args = append(args, editorArg{name: piece})
			} else {
				args = append(args, editorArg{})
			}
		}
	}
	return args, true
}

// editorCallSource is a call as a message writes it: toggle('heading', 2).
func editorCallSource(method string, args []editorArg) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, arg.written())
	}
	return method + "(" + strings.Join(parts, ", ") + ")"
}

// editorClick is the click of n, with its modifiers, nil for none.
func editorClick(n *Node) *Attribute {
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if head, _, _ := strings.Cut(a.Name, ":"); head == "click" {
			return a
		}
	}
	return nil
}

// editorStrangers are the words other editors give their tag, each with what .ui writes.
var editorStrangers = func() map[string]string {
	out := map[string]string{}
	add := func(message string, words ...string) {
		for _, word := range words {
			out[word] = message
		}
	}
	add(`%s không có trên <editor>: name, value, placeholder, required, readonly, disabled, rows viết trên <textarea> bên trong`,
		"name", "value", "placeholder", "required", "readonly", "disabled", "rows")
	add(`alt không có trên <editor>: tên của vùng viết là <label> của <field> bao ngoài, hay alt trên <textarea>`, "alt")
	add(`editor không có %s: editor không có giao diện dựng sẵn, vẽ nút bên trong (<button click="toggle('bold')">)`,
		"toolbar", "buttons", "formats", "plugins", "extensions", "menu")
	add(`editor không có %s: độ dài bài kiểm ở server (richtext(…).characters)`, "maxlength", "minlength", "limit", "max")
	add(`contenteditable không có trên <editor>: vùng viết là <textarea> bên trong, engine bật soạn khi trang chạy`, "contenteditable")
	add(`editor không có %s: bài lúc đầu là value của <textarea>, server vẽ sẵn`, "html", "content", "text", "initial")
	add(`editor không có %s: viết bằng Markdown lúc trang mở là format="markdown"; nút đổi cách viết là <button click="format = 'markdown'">`, "markdown")
	add(`editor không có %s: cách viết là format, format="rich", format="markdown" hay format="html"`, "mode", "view", "syntax", "source")
	add(`editor không có %s: lưu là việc của form, <button type="submit">, router.post nhận`, "save", "submit", "autosave", "draft", "persist")
	add(`editor không có %s: chưa lưu là unsaved, trạng thái của editor`, "dirty", "changed", "modified")
	add(`editor không có linking: ô liên kết đang mở là opened, trạng thái của editor`, "linking")
	add(`editor không có %s: chữ trong ô địa chỉ là address, trạng thái của editor`, "href", "url")
	add(`editor không có sự kiện %s ở bản này: bài đi theo form khi gửi`, "update", "edit", "text-change")
	add(`editor không có %s: định dạng là nút bên trong, <button click="toggle('bold')">`,
		"command", "run", "exec", "bold", "italic", "underline", "strikethrough", "code", "heading", "quote", "list")
	return out
}()

// editorStart is what an editor's instance starts with: the words and characters of its value cleaned, whether
// the page answers a save (a request that is no GET: what the person wrote is not saved yet), the format it
// starts in. Read before the page is drawn, for its alias, and kept.
func (r *renderer) editorStart(n *Node) map[string]any {
	if known := r.editorStarts[n]; known != nil {
		if r.editorRead[n] {
			r.touched++ // read from what the page is given, before the part that reads it again (frozen.go)
		}
		return known
	}
	touched := r.touched
	defer func() {
		if r.touched != touched {
			if r.editorRead == nil {
				r.editorRead = map[*Node]bool{}
			}
			r.editorRead[n] = true
		}
	}()
	state := make(map[string]any, len(componentState["editor"]))
	for name, initial := range componentState["editor"] {
		state[name] = initial
	}
	source := ""
	if area := editorAreaOf(n); area != nil {
		if a, ok := area.Attribute("value"); ok {
			if _, whole := wrapped(strings.TrimSpace(a.Value)); whole {
				if v, err := r.value(area, &a, a.Value); err == nil && !v.IsNullish() {
					source = v.ToString().Text()
				}
			}
		}
	}
	d := editorCleaned(source)
	state["empty"], state["words"], state["characters"] = d.Empty(), float64(d.Words), float64(d.Characters)
	method := strings.ToUpper(r.method())
	state["unsaved"] = method != "" && method != "GET" && method != "HEAD"
	if a, ok := n.Attribute("format"); ok && contains(editorFormats, strings.TrimSpace(a.Value)) {
		state["format"] = strings.TrimSpace(a.Value)
	}
	r.editorStarts[n] = state
	return state
}

// editorAreaOf is the first <textarea> inside an editor outside the components nested in it.
func editorAreaOf(n *Node) *Node {
	for _, ch := range n.Elements() {
		if ch.Tag == "textarea" {
			return ch
		}
		if _, nested := componentState[ch.Tag]; nested {
			continue
		}
		if found := editorAreaOf(ch); found != nil {
			return found
		}
	}
	return nil
}

// editorHost reads an editor's host and plans its parts: what the host keeps of what is written, what it adds,
// and the scope its inside is drawn in.
func (r *renderer) editorHost(n *Node, state map[string]any, inline bool) (*Node, *host, *editorScope, error) {
	r.editors++
	s := &editorScope{node: n, number: r.editors, toolbars: map[*Node]bool{}, groups: map[*Node]bool{},
		buttons: map[*Node]*editorButton{}, roving: map[*Node]string{}, menus: map[*Node]*editorMenu{},
		triggers: map[*Node]*editorMenu{}, popovers: map[*Node]*editorMenu{}, selects: map[*Node]bool{},
		format: "rich", field: r.field}
	if err := r.editorPlace(n, inline); err != nil {
		return nil, nil, nil, err
	}
	alias := ""
	if a, ok := n.Attribute("as"); ok {
		alias = strings.TrimSpace(a.Value)
	}
	copied, err := splitHost(n, func(a *Attribute, chain, base string) (bool, error) {
		if isEvent(a.Name) {
			head, _, _ := strings.Cut(a.Name, ":")
			for _, event := range strings.Split(head, ",") {
				switch event = strings.TrimSpace(event); event {
				case "input", "change":
					return false, at(n, a, "%s trên <editor> chưa có ở bản này: bài đi theo form khi gửi, số chữ là {{ words }}", event)
				case "toggle":
					return false, at(n, a, `toggle trên <editor> không phải sự kiện nào của nó: toggle là lệnh của editor, gọi bằng nút bên trong (<button click="toggle('bold')">)`)
				}
			}
			return false, nil
		}
		switch {
		case base == "format":
			value := strings.TrimSpace(a.Value)
			switch {
			case chain != "":
				return false, at(n, a, `format không nhận tiền tố: cách viết lúc trang mở là một ở mọi độ rộng (format="markdown")`)
			case strings.Contains(a.Value, "{{"):
				return false, at(n, a, `format trên <editor> là cách viết lúc trang mở, chữ viết sẵn (format="markdown"), không lấy từ dữ liệu`)
			case value == "":
				return false, at(n, a, `format cần một cách viết: format="rich", format="markdown" hay format="html"`)
			}
			if problem := editorFormatProblem(value, func(v string) string { return `format="` + v + `"` }); problem != "" {
				return false, at(n, a, "%s", problem)
			}
			s.format = value
			return true, nil
		case base == "align":
			return false, at(n, a, `%s không viết trên <editor>: align() là lệnh căn lề của editor (<button click="align('center')">); căn các phần bên trong bằng align trên khung bọc chúng (<box align="left">)`, a.Name)
		case editorStrangers[base] != "":
			says := editorStrangers[base]
			if strings.Contains(says, "%s") {
				says = fmt.Sprintf(says, base)
			}
			return false, at(n, a, "%s", says)
		case contains(editorNames, base):
			read := "{{ " + base + " }} bên trong <editor>"
			if alias != "" {
				read = "{{ " + alias + "." + base + " }}"
			}
			return false, at(n, a, "%s là trạng thái của editor, không phải đầu vào: đọc %s", base, read)
		case contains(editorMethods, base):
			return false, at(n, a, `%s là hàm của editor, không phải một chữ trên thẻ: gọi nó trong click của một nút bên trong (<button click="%s">)`, base, editorCallExample[base])
		case base == "role" || strings.HasPrefix(base, "aria-"):
			return false, at(n, a, `vai trò của editor là việc của engine (role="textbox" trên vùng viết, role="toolbar" trên thanh công cụ): bỏ %s`, base)
		}
		return false, nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(n.Significant()) == 0 {
		return nil, nil, nil, at(n, nil, `editor rỗng: vẽ vùng viết bên trong, <editor><textarea name="body" alt="Nội dung" /></editor>`)
	}
	// the plan reads the editor's tree, its alias and the components around, never the request: what a part inside
	// reads of it besides its value (read as an expression) is its file's and the page's counts, which a part's key
	// holds (frozen.go)
	if err := r.keptEditorParts(n, s, alias); err != nil {
		return nil, nil, nil, err
	}
	// the scope, with the height of every rows the page's editors write; the area looks as a richtext does, so
	// writing looks like reading
	r.editorRows = append(r.editorRows, s.rows)
	r.rules.Editor(r.editorRows)
	if err := r.site.richtext(r.table, r.rules); err != nil {
		return nil, nil, nil, at(n, nil, "%s", err.Error())
	}
	state["format"] = s.format
	own := &host{classes: []string{stylesheet.EditorClass}}
	number := func(name string) string { v, _ := state[name].(float64); return strconv.FormatFloat(v, 'f', -1, 64) }
	flag := func(name string) string { v, _ := state[name].(bool); return strconv.FormatBool(v) }
	own.inputs = []string{"empty: " + flag("empty"), "words: " + number("words"), "characters: " + number("characters"),
		"unsaved: " + flag("unsaved"), "opened: false", "address: ''", "format: " + quote(s.format), "dropped: []"}
	return copied, own, s, nil
}

// editorPlace says an editor stands where it can: once on the page (not in a for), not in another, not in a line,
// inside a <form method="post"> the post goes with.
func (r *renderer) editorPlace(n *Node, inline bool) error {
	if a, ok := n.Attribute("for"); ok {
		return at(n, &a, "editor không lặp bằng for: vùng viết, thanh công cụ và khung liên kết của nó mang id riêng, mỗi trang sửa một bài (editor ở %s:%d:%d)", n.File, n.Line, n.Column)
	}
	for i := len(r.parents) - 1; i >= 0; i-- {
		if p := r.parents[i]; p != nil {
			if a, ok := p.Attribute("for"); ok {
				return at(p, &a, "editor không lặp bằng for: vùng viết, thanh công cụ và khung liên kết của nó mang id riêng, mỗi trang sửa một bài (editor ở %s:%d:%d)", n.File, n.Line, n.Column)
			}
		}
	}
	if outer := r.editor; outer != nil {
		return at(n, nil, "editor không lồng trong editor (editor ngoài ở %s:%d:%d)", outer.node.File, outer.node.Line, outer.node.Column)
	}
	var line *Node
	for _, p := range r.parents {
		if lineTags[p.Tag] {
			line = p
		}
	}
	if line != nil || inline {
		message := "editor là một khối: không đặt trong <text>, <link>, <button>, <label>"
		if line != nil {
			message += fmt.Sprintf(" (nó đang nằm trong <%s> ở %s:%d:%d)", line.Tag, line.File, line.Line, line.Column)
		}
		return at(n, nil, "%s", message)
	}
	var form *Node
	for i := len(r.parents) - 1; i >= 0 && form == nil; i-- {
		if r.parents[i].Tag == "form" {
			form = r.parents[i]
		}
	}
	if form == nil {
		return at(n, nil, `editor phải nằm trong <form method="post">: bài viết đi theo form khi gửi, router.post của trang nhận nó`)
	}
	if method, _ := form.Attribute("method"); strings.TrimSpace(method.Value) != "post" {
		return at(n, nil, `editor phải nằm trong <form method="post">: form GET đưa cả bài viết lên địa chỉ trang (<form> ở %s:%d:%d)`, form.File, form.Line, form.Column)
	}
	return nil
}

// editorFound is one part an editor's walk met: the elements around it inside the host (outermost first), the
// nearest of them that writes for or if, the component nested in the host around it, the popover around it.
type editorFound struct {
	node                    *Node
	path                    []*Node
	repeat, nested, popover *Node
}

// editorParts finds an editor's parts and plans each.
func (r *renderer) editorParts(n *Node, s *editorScope, alias string) error {
	var areas, popovers, buttons, inputs, errorsFound, dropdowns, selects []editorFound
	var walk func(nodes []*Node, path []*Node, repeat, nested, popover *Node) error
	walk = func(nodes []*Node, path []*Node, repeat, nested, popover *Node) error {
		for _, ch := range nodes {
			if ch.IsText() {
				continue
			}
			found := editorFound{node: ch, path: path, repeat: repeat, nested: nested, popover: popover}
			switch {
			case ch.Tag == "textarea":
				areas = append(areas, found)
				continue // what is written inside it is told by its words
			case ch.Tag == "content":
				return at(ch, nil, "vùng viết của editor là <textarea> (trước 22/10/2026 viết <content>)")
			case nested != nil:
			case ch.Tag == "editor":
				return at(ch, nil, "editor không lồng trong editor (editor ngoài ở %s:%d:%d)", n.File, n.Line, n.Column)
			case ch.Tag == "form":
				return at(ch, nil, `form không đặt trong <editor>: editor là một ô của form bao ngoài nó (<form method="post">…<editor>…</editor>…</form>)`)
			case ch.Tag == "dropdown" && popover == nil:
				dropdowns = append(dropdowns, found) // more commands of a toolbar, when it holds some (editorMenuOf)
			case ch.Tag == "popover":
				popovers = append(popovers, found)
			case ch.Tag == "button":
				buttons = append(buttons, found)
			case ch.Tag == "select":
				selects = append(selects, found) // a switch of format, when it is two-way on it (editorSelectOf)
			case ch.Tag == "input" && popover != nil:
				inputs = append(inputs, found)
			case ch.Tag == "error" && popover != nil:
				errorsFound = append(errorsFound, found)
			}
			around := repeat
			if _, ok := ch.Attribute("for"); ok {
				around = ch
			} else if _, ok := ch.Attribute("if"); ok {
				around = ch
			}
			inner, within := nested, popover
			if _, component := componentState[ch.Tag]; inner == nil && component {
				inner = ch
			}
			if inner == nil && ch.Tag == "popover" && within == nil {
				within = ch
			}
			next := path
			if inner == nil {
				next = append(append([]*Node(nil), path...), ch)
			}
			if err := walk(ch.Children, next, around, inner, within); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(n.Children, nil, nil, nil, nil); err != nil {
		return err
	}

	if len(areas) == 0 {
		return at(n, nil, `editor cần một vùng viết bên trong, engine viết role="textbox" lên nó: <textarea name="body" value="{{ post.body }}" />`)
	}
	area := areas[0]
	for _, f := range areas {
		if f.nested != nil {
			return at(f.node, nil, "vùng viết của editor viết thẳng trong <editor>, không trong <%s> lồng bên trong (ở %s:%d:%d): tên trần bên trong đó là của nó",
				f.nested.Tag, f.nested.File, f.nested.Line, f.nested.Column)
		}
	}
	if len(areas) > 1 {
		return at(areas[1].node, nil, "editor có một vùng viết: <textarea> ở %s:%d:%d đã là của nó; vùng viết thứ hai là một <editor> khác", area.node.File, area.node.Line, area.node.Column)
	}
	if area.popover != nil {
		return at(area.node, nil, "vùng viết của editor không đặt trong <popover>: popover là khung của liên kết, đóng khi không gắn liên kết")
	}
	if rep := area.repeat; rep != nil {
		if a, ok := rep.Attribute("for"); ok {
			return at(rep, &a, "vùng viết của editor không lặp bằng for: for này lặp <textarea> ở %s:%d:%d; đặt <textarea> ngoài for", area.node.File, area.node.Line, area.node.Column)
		}
		a, _ := rep.Attribute("if")
		return at(rep, &a, "vùng viết của editor luôn có mặt: if này có thể bỏ <textarea> ở %s:%d:%d; đặt <textarea> ngoài if", area.node.File, area.node.Line, area.node.Column)
	}
	s.area = area.node
	if err := r.editorAreaWords(area.node, s); err != nil {
		return err
	}
	if id, ok := area.node.Attribute("id"); ok && strings.TrimSpace(id.Value) != "" {
		s.areaID = strings.TrimSpace(id.Value)
	} else {
		s.areaID = fmt.Sprintf("kitwork-editor-%d-area", s.number)
	}

	if len(popovers) > 1 {
		first := popovers[0].node
		return at(popovers[1].node, nil, "editor có một popover, khung của liên kết: <popover> ở %s:%d:%d đã là của nó", first.File, first.Line, first.Column)
	}
	if len(popovers) == 1 {
		s.popover = popovers[0].node
		if id, ok := s.popover.Attribute("id"); ok && strings.TrimSpace(id.Value) != "" {
			s.popoverID = strings.TrimSpace(id.Value)
		} else {
			s.popoverID = fmt.Sprintf("kitwork-editor-%d-popover", s.number)
		}
	}

	var toolbarButtons []*editorButton
	links, commands := false, 0 // a button that opens the link's popover; the buttons that call a command
	for _, f := range buttons {
		b, err := r.editorButtonOf(f, s)
		if err != nil {
			return err
		}
		if b == nil {
			continue
		}
		r.editorPressedWarning(f.node, b)
		if b.command != "format" {
			commands++
		}
		links = links || b.command == "link" && !b.apply
		s.buttons[f.node] = b
		if b.command == "link" && !b.apply && s.popover == nil {
			return at(f.node, b.click, `link() mở ô địa chỉ mà editor chưa có: vẽ <popover> chứa <input value="{{ address }}" alt="Địa chỉ liên kết" />`)
		}
		if !b.popover {
			toolbarButtons = append(toolbarButtons, b)
		}
	}
	for _, f := range dropdowns {
		menu, err := r.editorMenuOf(f, s, alias)
		if err != nil {
			return err
		}
		if menu == nil {
			continue // no command of the editor: a toolbar refuses it as it refuses any component
		}
		s.menus[menu.node], s.triggers[menu.trigger], s.popovers[menu.popover] = menu, menu, menu
		for _, b := range menu.buttons {
			r.editorPressedWarning(b.node, b)
			s.buttons[b.node] = b
			toolbarButtons = append(toolbarButtons, b)
			commands++
		}
	}
	if err := r.editorToolbars(s, toolbarButtons, area.path); err != nil {
		return err
	}
	for _, f := range selects {
		if err := editorSelectOf(f, s); err != nil {
			return err
		}
	}
	if s.popover != nil {
		if err := editorPopover(s, inputs, errorsFound); err != nil {
			return err
		}
		if !links {
			r.warn(s.popover, nil, "%s", `popover của editor chỉ mở bằng link(): chưa có nút <button click="link()">, nên khung liên kết không bao giờ hiện`)
		}
	}
	if commands == 0 {
		// a phone has no keyboard shortcuts
		r.warn(n, nil, "%s", `editor không có nút nào: điện thoại không có phím tắt; vẽ ít nhất <button click="toggle('bold')"> và <button click="link()"> trong một <box alt="Định dạng">`)
	}
	labelled := s.field != nil && s.field.label != ""
	switch {
	case !labelled && (s.alt == nil || strings.TrimSpace(s.alt.Value) == ""):
		return at(s.area, nil, "vùng viết chưa có tên đọc được: đặt editor trong <field> có <label>, hay viết alt trên <textarea>")
	case labelled:
		if s.alt != nil {
			r.warn(s.area, s.alt, "alt trên <textarea> không có tác dụng: <label> của <field> bao ngoài đã là tên của vùng viết (trang bỏ alt này)")
		}
		s.alt = nil // the field's label names the area
	}
	return nil
}

// editorPressedWarning tells pressed: on a button that says it is on (a format, a side, a format of writing): the
// moment it is pressed is not the moment it is on, which active: paints (aria-pressed).
func (r *renderer) editorPressedWarning(n *Node, b *editorButton) {
	if !b.pressed() {
		return
	}
	on := "nút đang bật định dạng"
	if b.command == "format" {
		on = "nút của cách viết đang chọn"
	}
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if chain, _, ok := strings.Cut(a.Name, ":"); ok && !isEvent(a.Name) && contains(strings.Split(strings.ReplaceAll(chain, "+", ","), ","), "pressed") {
			r.warn(n, a, "%s", "pressed: là lúc đang nhấn; "+on+" tô bằng active: ("+strings.Replace(a.Name, "pressed", "active", 1)+")")
		}
	}
}

// editorSubmit is a read of an editor's empty through its alias.
var editorSubmit = regexp.MustCompile(`(\$[A-Za-z][A-Za-z0-9_]*)\.empty\b`)

// lintSubmit tells a send button held back by an editor's empty: without JavaScript it never gives way, and
// required on the <textarea> already holds an empty post back.
func (r *renderer) lintSubmit(n *Node) {
	if n.Tag != "button" {
		return
	}
	disabled, ok := n.Attribute("disabled")
	kind, _ := n.Attribute("type")
	if !ok || strings.TrimSpace(kind.Value) != "submit" {
		return
	}
	for _, m := range editorSubmit.FindAllStringSubmatch(disabled.Value, -1) {
		if r.aliasOf[m[1]] == "editor" {
			r.warn(n, &disabled, "%s", "nút gửi khoá theo "+m[1]+".empty: không có JavaScript thì không gửi được; required=\"true\" trên <textarea> đã chặn bài rỗng")
		}
	}
}

// editorAreaRefused are words a <textarea> takes that the writing area does not, each with why.
var editorAreaRefused = map[string]string{
	"maxlength":       "%s không có trên vùng viết: độ dài bài kiểm ở server (richtext(…).characters)",
	"minlength":       "%s không có trên vùng viết: độ dài bài kiểm ở server (richtext(…).characters)",
	"contenteditable": "contenteditable không có trên vùng viết: engine bật soạn khi trang chạy",
	"type":            "type không có trên vùng viết: bài là richtext, server làm sạch",
	"cols":            "cols không có trên vùng viết: độ rộng là width",
	"autocomplete":    "autocomplete không có trên vùng viết",
	"autofocus":       "autofocus không có trên vùng viết: chỗ con trỏ đứng khi trang mở là của người dùng",
	"spellcheck":      `spellcheck của vùng viết là việc của engine (spellcheck="true"): bỏ spellcheck`,
	"translate":       `translate của vùng viết là việc của engine (translate="no": bản dịch của trang không được lưu thành bài): bỏ translate`,
	"role":            `vai trò của vùng viết là việc của engine (role="textbox"): bỏ role`,
}

// editorAreaWords reads what the writing area's <textarea> says: its name, its value, its placeholder and alt,
// required, readonly, disabled, rows and its look.
func (r *renderer) editorAreaWords(n *Node, s *editorScope) error {
	if len(n.Significant()) > 0 {
		return at(n, nil, `vùng viết của editor không có nội dung bên trong: bài lúc đầu là value="{{ post.body }}"`)
	}
	named := false
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if isEvent(a.Name) {
			head, _, _ := strings.Cut(a.Name, ":")
			for _, event := range strings.Split(head, ",") {
				if event = strings.TrimSpace(event); event == "input" || event == "change" {
					return at(n, a, "%s viết trên <textarea> của editor chưa có ở bản này: bài đi theo form khi gửi, số chữ là {{ words }}", event)
				}
			}
			return at(n, a, `<textarea> của editor không nhận sự kiện %s ở bản này: lệnh của editor là nút trong thanh công cụ (<button click="toggle('bold')">)`, a.Name)
		}
		chain, base := splitPrefix(a.Name)
		value := strings.TrimSpace(a.Value)
		switch base {
		case "for":
			return at(n, a, "vùng viết của editor không lặp bằng for: mỗi editor một vùng viết")
		case "if", "show":
			return at(n, a, "vùng viết của editor luôn có mặt: bỏ %s", base)
		case "flex", "grid", "columns", "gap", "align", "space", "wrap":
			return at(n, a, "vùng viết là một khối chữ, engine đặt cách các đoạn nằm: bỏ %s", a.Name)
		case "id":
			continue // the area's id
		}
		if stylesheet.IsPresentation(base) {
			continue // the area's look, read as it is drawn
		}
		if chain != "" {
			return at(n, a, "%s không nhận tiền tố (chỉ thuộc tính trình bày nhận tiền tố)", base)
		}
		switch base {
		case "name":
			switch {
			case strings.Contains(a.Value, "{{"):
				return at(n, a, `name của vùng viết là chữ cố định, tên ô form gửi lên (name="body"), không lấy từ dữ liệu`)
			case value == "":
				return at(n, a, `<textarea> của editor cần name: không có name thì form không gửi bài lên server (name="body")`)
			}
			s.name, named = value, true
		case "value":
			if err := r.editorValue(n, a); err != nil {
				return err
			}
			s.value = a
		case "placeholder":
			s.placeholder = a
		case "alt":
			s.alt = a
		case "required", "readonly", "disabled":
			on, err := editorBoolean(n, a)
			if err != nil {
				return err
			}
			switch base {
			case "required":
				s.required = on
			case "readonly":
				s.readonly = on
			default:
				s.disabled = on
			}
		case "rows":
			rows, err := strconv.Atoi(value)
			if err != nil || rows < 1 || rows > 100 || !digits(value) {
				return at(n, a, `rows là số dòng thấp nhất của vùng viết, một số nguyên từ 1 tới 100 (rows="12"), không phải %q`, a.Value)
			}
			s.rows = rows
		default:
			if says := editorAreaRefused[strings.ToLower(base)]; says != "" {
				if strings.Contains(says, "%s") {
					says = fmt.Sprintf(says, base)
				}
				return at(n, a, "%s", says)
			}
			return at(n, a, "thuộc tính %s không có trên vùng viết <textarea>; vùng viết nhận name, value, placeholder, required, readonly, disabled, rows, alt và chữ trình bày", a.Name)
		}
	}
	if !named {
		return at(n, nil, `<textarea> của editor cần name: không có name thì form không gửi bài lên server (name="body")`)
	}
	return nil
}

// editorBoolean reads required, readonly or disabled of the area: true or false, written.
func editorBoolean(n *Node, a *Attribute) (bool, error) {
	if a.Bare {
		return true, nil
	}
	switch strings.TrimSpace(a.Value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if strings.Contains(a.Value, "{{") {
		return false, at(n, a, `%s của vùng viết là "true" hay "false" viết sẵn ở bản này, không lấy từ dữ liệu`, a.Name)
	}
	return false, at(n, a, `%s chỉ nhận "true" hoặc "false"`, a.Name)
}

// editorValue says the area's value is the post saved: one {{ }} reading the page's data, drawn on the server.
func (r *renderer) editorValue(n *Node, a *Attribute) error {
	const written = `value của vùng viết là bài đã lưu, một {{ }} đọc dữ liệu của trang: value="{{ post.body }}"`
	source, whole := wrapped(strings.TrimSpace(a.Value))
	if !whole {
		return at(n, a, "%s", written)
	}
	if _, literal := compiler.ExpressionStrings(source); literal {
		return at(n, a, "%s", written)
	}
	compiled, err := r.site.compile(source)
	if err != nil {
		return at(n, a, "biểu thức %q: %v", source, err)
	}
	for _, name := range compiled.reads {
		owner := r.componentDeclaring(name)
		if strings.HasPrefix(name, "$") || owner != nil || contains(editorNames, name) || contains(editorMethods, name) {
			return at(n, a, "value của vùng viết dựng ở server: đọc dữ liệu của trang, không đọc %s (trạng thái ở trình duyệt)", name)
		}
	}
	return nil
}

// editorButtonOf is the command a button of the editor runs, nil for a button whose click is none of its.
func (r *renderer) editorButtonOf(f editorFound, s *editorScope) (*editorButton, error) {
	n := f.node
	click := editorClick(n)
	if click == nil {
		return nil, nil
	}
	call, ok := readEditorCall(click.Value)
	if !ok {
		return nil, nil
	}
	if call.assign {
		return editorSwitchOf(f, click, call.args[0])
	}
	if call.alias != "" || !contains(editorMethods, call.method) {
		return nil, nil
	}
	b := &editorButton{node: n, click: click, command: call.method, popover: f.popover != nil, path: f.path}
	if err := editorNotRepeated(n, click, f.repeat); err != nil {
		return nil, err
	}
	if err := editorReadCommand(n, click, b, call.args); err != nil {
		return nil, err
	}
	switch method := call.method; {
	case b.popover && method != "link" && method != "unlink" && method != "close":
		return nil, at(n, click, "%s là nút của thanh công cụ, không đặt trong <popover> của liên kết", editorCallSource(method, call.args))
	case b.popover && method == "link" && !b.apply:
		return nil, at(n, click, "link() mở ô địa chỉ, mà nút này đã ở trong ô: nút gắn liên kết là link(address)")
	case !b.popover && b.apply:
		return nil, at(n, click, `link(address) gắn chữ trong ô địa chỉ: đặt nút này trong <popover> của editor, cạnh <input value="{{ address }}">`)
	}
	if b.command == "link" && !b.apply {
		if a, ok := n.Attribute("expanded"); ok {
			return nil, at(n, &a, "expanded của nút liên kết là việc của engine (aria-expanded theo opened): bỏ expanded")
		}
	}
	return b, nil
}

// editorNotRepeated says a button of the editor is written once, not in a for: each has its own place in a
// toolbar, the first its Tab stop.
func editorNotRepeated(n *Node, click *Attribute, repeat *Node) error {
	for _, at2 := range []*Node{n, repeat} {
		if at2 == nil {
			continue
		}
		if a, ok := at2.Attribute("for"); ok {
			return at(n, click, "nút của editor không lặp bằng for (for ở %s:%d:%d): mỗi nút viết một lần, định dạng viết sẵn", at2.File, a.Line, a.Column)
		}
	}
	return nil
}

// editorReadCommand reads what a command's arguments say into b.
func editorReadCommand(n *Node, click *Attribute, b *editorButton, args []editorArg) error {
	switch b.command {
	case "toggle":
		format, err := editorFormat(n, click, args)
		if err != nil {
			return err
		}
		b.format = format
		return nil
	case "align":
		side, err := editorAlign(n, click, args)
		if err != nil {
			return err
		}
		b.wire = "align-" + side
		return nil
	case "link":
		switch {
		case len(args) == 0:
		case len(args) == 1 && args[0].name == "address":
			b.apply = true
		default:
			return at(n, click, "link nhận address, chữ trong ô địa chỉ: link(address); mở ô địa chỉ là link()")
		}
	default:
		if len(args) > 0 {
			return at(n, click, "%s() không nhận gì: viết %s()", b.command, b.command)
		}
	}
	b.wire = b.command
	return nil
}

// editorSwitchOf is a button whose click is format = 'markdown': it switches how the post is seen and typed.
func editorSwitchOf(f editorFound, click *Attribute, value editorArg) (*editorButton, error) {
	n := f.node
	written := "format = " + value.written()
	if err := editorNotRepeated(n, click, f.repeat); err != nil {
		return nil, err
	}
	if !value.str {
		return nil, at(n, click, "%s: cách viết là chữ viết sẵn, format = 'rich', format = 'markdown' hay format = 'html': engine đọc nó để viết aria-pressed", written)
	}
	if problem := editorFormatProblem(value.text, func(v string) string { return "format = " + quote(v) }); problem != "" {
		return nil, at(n, click, "%s", problem)
	}
	if f.popover != nil {
		return nil, at(n, click, "%s đổi cách viết của editor: đặt nút này trong thanh công cụ, không trong <popover> của liên kết", written)
	}
	return &editorButton{node: n, click: click, command: "format", value: value.text, path: f.path}, nil
}

// editorFormatNames are other names of a format, each with its name and the call that toggles it.
var editorFormatNames = func() map[string][2]string {
	out := map[string][2]string{}
	add := func(name, call string, words ...string) {
		for _, word := range words {
			out[word] = [2]string{name, call}
		}
	}
	add("bold", "toggle('bold')", "strong", "b")
	add("italic", "toggle('italic')", "em", "i")
	add("underline", "toggle('underline')", "u", "ins", "underlined")
	add("strikethrough", "toggle('strikethrough')", "strike", "s", "del", "line-through", "strikeout", "strike-through")
	add("code", "toggle('code')", "inline-code", "monospace", "mono", "kbd")
	add("code 'block'", "toggle('code', 'block')", "pre", "codeblock", "code-block", "block-code", "preformatted")
	add("quote", "toggle('quote')", "blockquote")
	add("heading mức 2", "toggle('heading', 2)", "h2", "heading-2", "subheading")
	for level := 3; level <= 6; level++ {
		l := strconv.Itoa(level)
		add("heading mức "+l, "toggle('heading', "+l+")", "h"+l, "heading-"+l)
	}
	add("list", "toggle('list')", "bullets", "bullet", "ul", "unordered")
	add("list 'ordered'", "toggle('list', 'ordered')", "numbers", "ol", "numbered-list", "numbered", "ordered", "ordered-list")
	return out
}()

// editorFormat is the format a toggle names, as data-kitwork-format writes it (bold, heading-4, code-block).
func editorFormat(n *Node, a *Attribute, args []editorArg) (string, error) {
	const named = "toggle cần tên định dạng viết sẵn, như toggle('bold'): engine đọc nó để viết aria-pressed và phím tắt"
	if len(args) == 0 || !args[0].str {
		return "", at(n, a, "%s", named)
	}
	format := args[0].text
	written := editorCallSource("toggle", args)
	switch format {
	case "bold", "italic", "underline", "strikethrough", "quote":
		if len(args) > 1 {
			return "", at(n, a, "%s: %s không nhận thêm gì, viết toggle('%s')", written, format, format)
		}
		return format, nil
	case "code":
		switch {
		case len(args) == 1:
			return "code", nil
		case len(args) == 2 && args[1].str && args[1].text == "block":
			return "code-block", nil
		}
		return "", at(n, a, "%s: code nhận 'block' hay không gì: toggle('code') là mã trong dòng, toggle('code', 'block') là khối mã", written)
	case "heading":
		if len(args) == 1 {
			return "", at(n, a, "toggle('heading') cần mức: %s", editorHeadingLevels)
		}
		if len(args) > 2 {
			return "", at(n, a, "%s: heading nhận một mức, %s", written, editorHeadingLevels)
		}
		level := args[1].number
		switch {
		case !args[1].isNumber:
			return "", at(n, a, "%s: mức của heading là một số viết sẵn, %s", written, editorHeadingLevels)
		case level >= 2 && level <= 6 && level == float64(int(level)):
			return "heading-" + strconv.Itoa(int(level)), nil
		case level == 1:
			return "", at(n, a, `toggle('heading', 1): heading 1 là tiêu đề của trang (<text heading="1">); trong bài viết dùng %s`, editorHeadingLevels)
		}
		return "", at(n, a, "%s: bài viết có tiêu đề mục từ mức 2 tới mức 6; viết %s", written, editorHeadingLevels)
	case "h1", "heading-1", "title":
		return "", at(n, a, `%s: heading 1 là tiêu đề của trang (<text heading="1">); trong bài viết dùng %s`, written, editorHeadingLevels)
	case "rule", "hr", "horizontal-rule", "divider", "separator":
		return "", at(n, a, `%s: đường kẻ ngang là rule(), nó chèn một <hr> sau khối ở con trỏ (<button click="rule()">)`, written)
	case "align", "left", "center", "right", "justify", "start", "end", "align-left", "align-center", "align-right", "align-justify":
		side := strings.TrimPrefix(format, "align-")
		if format == "align" && len(args) == 2 {
			side = args[1].text
		}
		call := editorSides
		if side == "left" || side == "center" || side == "right" {
			call = "align('" + side + "')"
		}
		return "", at(n, a, "%s: căn lề không phải một định dạng của toggle: viết %s", written, call)
	case "list":
		switch {
		case len(args) == 1:
			return "list", nil
		case len(args) == 2:
			if args[1].str && args[1].text == "ordered" {
				return "list-ordered", nil
			}
			return "", at(n, a, `%s: list nhận 'ordered' hay không gì, như <list ordered="true">`, written)
		}
		return "", at(n, a, "%s: list nhận 'ordered' hay không gì", written)
	case "paragraph", "p", "normal", "text":
		return "", at(n, a, "%s: editor không có định dạng %s: bấm lại nút đang bật là về đoạn văn", written, format)
	case "link":
		return "", at(n, a, "%s: liên kết là link(), nó mở ô địa chỉ", written)
	case "image", "table":
		return "", at(n, a, "%s: editor chưa có định dạng này ở bản này; có %s", written, editorFormatList)
	}
	if other, ok := editorFormatNames[format]; ok {
		return "", at(n, a, "%s: định dạng này tên là %s, viết %s", written, other[0], other[1])
	}
	return "", at(n, a, "%s: editor không có định dạng %s; có %s%s", written, format, editorFormatList,
		nearestWords(format, []string{"bold", "italic", "underline", "strikethrough", "code", "quote", "heading", "list"}))
}

// editorAlign is the side an align names: left, center or right.
func editorAlign(n *Node, a *Attribute, args []editorArg) (string, error) {
	const named = "align cần một phía viết sẵn, " + editorSides + ": engine đọc nó để viết aria-pressed"
	if len(args) == 0 || !args[0].str {
		return "", at(n, a, "%s", named)
	}
	side := args[0].text
	written := editorCallSource("align", args)
	if len(args) > 1 {
		return "", at(n, a, "%s: align nhận một phía, %s", written, editorSides)
	}
	switch side {
	case "left", "center", "right":
		return side, nil
	case "start":
		return "", at(n, a, "%s: .ui nói trái, phải (trang viết từ phải sang trái lật như gương): viết align('left')", written)
	case "end":
		return "", at(n, a, "%s: .ui nói trái, phải (trang viết từ phải sang trái lật như gương): viết align('right')", written)
	case "justify":
		return "", at(n, a, "%s: editor chưa có căn đều ở bản này: tiếng Việt căn đều dễ hở chữ giữa các từ; có left, center, right", written)
	case "middle", "centre":
		return "", at(n, a, "%s: giữa là center, viết align('center')", written)
	}
	return "", at(n, a, "%s: editor không căn %s; có left, center, right%s", written, side, nearestWords(side, []string{"left", "center", "right"}))
}

// editorMenuOf is a toolbar's dropdown that holds commands of the editor (its buttons in its popover call them
// through the editor's alias), nil for one that holds none.
func (r *renderer) editorMenuOf(f editorFound, s *editorScope, alias string) (*editorMenu, error) {
	d := f.node
	menu := &editorMenu{node: d, path: f.path}
	var triggers, others, popovers, nested []*Node
	var walk func(nodes []*Node, inside, repeat *Node) error
	walk = func(nodes []*Node, inside, repeat *Node) error {
		for _, ch := range nodes {
			if ch.IsText() {
				continue
			}
			if _, component := componentState[ch.Tag]; component {
				nested = append(nested, ch) // its names are its own
				continue
			}
			if ch.Tag == "popover" && inside == nil {
				popovers = append(popovers, ch)
			}
			around := repeat
			_, repeated := ch.Attribute("for")
			_, kept := ch.Attribute("if")
			if repeated || kept {
				around = ch
			}
			if ch.Tag == "button" {
				b, trigger, err := r.editorMenuButton(ch, alias, menu, inside, around)
				switch {
				case err != nil:
					return err
				case b != nil:
					menu.buttons = append(menu.buttons, b)
				case trigger:
					triggers = append(triggers, ch)
				case inside == nil:
					others = append(others, ch)
				}
			}
			within := inside
			if within == nil && ch.Tag == "popover" {
				within = ch
			}
			if err := walk(ch.Children, within, around); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(d.Children, nil, nil); err != nil {
		return nil, err
	}
	if len(menu.buttons) == 0 {
		return nil, nil
	}
	if len(nested) > 0 {
		return nil, at(nested[0], nil, "<%s> không đặt trong <dropdown> của thanh công cụ của editor (dropdown ở %s:%d:%d): dropdown của thanh giữ nút mở nó, và trong <popover> các nút lệnh của editor",
			nested[0].Tag, d.File, d.Line, d.Column)
	}
	if len(popovers) > 1 {
		first := popovers[0]
		return nil, at(popovers[1], nil, "<dropdown> của thanh công cụ có một <popover>: <popover> ở %s:%d:%d đã là của nó", first.File, first.Line, first.Column)
	}
	if len(triggers) == 0 {
		return nil, at(d, nil, `<dropdown> trong thanh công cụ của editor cần nút mở nó, <button click="toggle()">, ngoài <popover>: nút đó là một điểm dừng của Tab trong thanh`)
	}
	menu.trigger, menu.popover = triggers[0], popovers[0]
	if extra := append(append([]*Node(nil), triggers[1:]...), others...); len(extra) > 0 {
		second := extra[0]
		for _, other := range extra[1:] {
			if other.Line < second.Line || other.Line == second.Line && other.Column < second.Column {
				second = other
			}
		}
		return nil, at(second, nil, `<dropdown> trong thanh công cụ của editor có một nút ngoài <popover>, nút mở nó (<button click="toggle()"> ở %s:%d:%d): đặt nút này trong <popover>`,
			menu.trigger.File, menu.trigger.Line, menu.trigger.Column)
	}
	if a, ok := menu.trigger.Attribute("expanded"); ok {
		return nil, at(menu.trigger, &a, "expanded của nút mở dropdown trong thanh công cụ là việc của engine (aria-expanded theo opened): bỏ expanded")
	}
	if id, ok := menu.popover.Attribute("id"); ok && strings.TrimSpace(id.Value) != "" {
		menu.popoverID = strings.TrimSpace(id.Value)
	} else {
		menu.popoverID = fmt.Sprintf("kitwork-editor-%d-menu-%d", s.number, len(s.menus)+1)
	}
	return menu, nil
}

// editorMenuButton reads one button of a toolbar's dropdown: a command of the editor through its alias (in the
// popover), the dropdown's own trigger (toggle(), outside it), or neither.
func (r *renderer) editorMenuButton(n *Node, alias string, menu *editorMenu, popover, repeat *Node) (*editorButton, bool, error) {
	click := editorClick(n)
	if click == nil {
		return nil, false, nil
	}
	call, ok := readEditorCall(click.Value)
	if !ok {
		return nil, false, nil
	}
	if call.assign {
		return nil, false, at(n, click, `format = %s trong <dropdown>: nút đổi cách viết đứng trong thanh công cụ, một nhóm của nó (<group alt="Viết bằng">%s…</group>), hay là một <select value="{{ format }}" alt="Viết bằng">`,
			call.args[0].written(), editorFormatSwitch)
	}
	if call.alias == "" && contains(editorMethods, call.method) {
		switch {
		case call.method == "toggle" && len(call.args) == 0:
			return nil, popover == nil, nil // the dropdown's own: it opens and closes it
		case call.method == "close" && len(call.args) == 0:
			return nil, false, nil // the dropdown's own close
		}
		written := editorCallSource(call.method, call.args)
		through := `đặt as trên <editor> (<editor as="$body">) rồi gọi $body.` + written
		if alias != "" {
			through = alias + "." + written
		}
		return nil, false, at(n, click, "%s trong <dropdown>: tên trần bên trong <dropdown> là của nó (toggle() mở và đóng nó); lệnh của editor ở đây gọi qua alias, %s", written, through)
	}
	if alias == "" || call.alias != alias || !contains(editorMethods, call.method) {
		return nil, false, nil
	}
	written := alias + "." + editorCallSource(call.method, call.args)
	switch {
	case popover == nil:
		return nil, false, at(n, click, `%s ở ngoài <popover> của <dropdown>: nút lệnh của dropdown trong thanh công cụ nằm trong <popover> của nó; ngoài <popover> chỉ có nút mở nó, <button click="toggle()">`, written)
	case call.method == "link":
		return nil, false, at(n, click, `%s không đặt trong <dropdown>: nút liên kết mở khung liên kết của editor, viết nó thẳng trong thanh công cụ (<button click="link()">)`, written)
	case call.method == "close":
		return nil, false, at(n, click, `%s đóng khung liên kết của editor: nút này là của <popover> của liên kết (<button click="close()">); dropdown của thanh tự đóng sau mỗi lệnh`, written)
	}
	b := &editorButton{node: n, click: click, command: call.method, path: menu.path}
	if err := editorNotRepeated(n, click, repeat); err != nil {
		return nil, false, err
	}
	if err := editorReadCommand(n, click, b, call.args); err != nil {
		return nil, false, err
	}
	return b, false, nil
}

// editorToolbars finds the toolbars of an editor: for each region its buttons share apart from the area, the
// element nearest the buttons that holds them all, a box, card or section with alt; its groups, and the roving
// tabindex of its buttons (and of a dropdown's trigger) in order.
func (r *renderer) editorToolbars(s *editorScope, buttons []*editorButton, areaPath []*Node) error {
	holds := map[*Node]bool{}
	for _, p := range areaPath {
		holds[p] = true // every element around the area holds it
	}
	var order []*Node
	regions := map[*Node][]*editorButton{}
	for _, b := range buttons {
		var region *Node
		for _, p := range b.path {
			if !holds[p] {
				region = p
				break
			}
		}
		if region == nil {
			return at(b.node, b.click, `nút của editor (click="%s") nằm chung khung với vùng viết: đặt các nút trong một khung riêng có alt, <box alt="Định dạng">…</box>, engine viết role="toolbar" lên khung đó`, b.click.Value)
		}
		if regions[region] == nil {
			order = append(order, region)
		}
		regions[region] = append(regions[region], b)
	}
	for _, region := range order {
		members := regions[region]
		common := members[0].path
		for _, b := range members[1:] {
			i := 0
			for i < len(common) && i < len(b.path) && common[i] == b.path[i] {
				i++
			}
			common = common[:i]
		}
		var group *Node
		for len(common) > 0 && common[len(common)-1].Tag == "group" {
			group, common = common[len(common)-1], common[:len(common)-1]
		}
		if group != nil && (len(common) == 0 || holds[common[len(common)-1]]) {
			return at(group, nil, `<group> là một nhóm nút trong thanh công cụ của editor: đặt các <group> trong một khung có alt, <box alt="Định dạng"><group alt="Kiểu chữ">…</group></box>, engine viết role="toolbar" lên khung đó`)
		}
		toolbar := common[len(common)-1] // the region at least: every path holds it
		switch toolbar.Tag {
		case "box", "card", "section":
		default:
			first := members[0].node
			return at(toolbar, nil, "thanh công cụ của editor là một <box>, <card> hay <section> có alt: các nút như nút ở %s:%d:%d nằm chung trong <%s>", first.File, first.Line, first.Column, toolbar.Tag)
		}
		if alt, ok := toolbar.Attribute("alt"); !ok || strings.TrimSpace(alt.Value) == "" {
			return at(toolbar, nil, `thanh công cụ của editor cần alt: engine viết role="toolbar", alt là tên của thanh (<box alt="Định dạng">)`)
		}
		var stops []*Node
		var collect func(nodes []*Node, group *Node, menu *editorMenu) error
		collect = func(nodes []*Node, group *Node, menu *editorMenu) error {
			for _, ch := range nodes {
				if ch.IsText() {
					continue
				}
				inner := group
				_, component := componentState[ch.Tag]
				switch m := s.menus[ch]; {
				case m != nil && menu == nil:
				case menu != nil && ch == menu.popover:
				case ch.Tag == "dropdown" && menu == nil:
					return at(ch, nil, `<dropdown> trong thanh công cụ của editor (thanh ở %s:%d:%d) giữ thêm lệnh của editor: nút mở nó <button click="toggle()">, và trong <popover> các nút gọi editor qua alias (<button click="$body.toggle('heading', 4)">); một dropdown khác đặt cạnh thanh, trong <editor>`,
						toolbar.File, toolbar.Line, toolbar.Column)
				case ch.Tag == "popover" || component:
					return at(ch, nil, "<%s> không đặt trong thanh công cụ của editor (thanh ở %s:%d:%d): phím mũi tên của thanh đi qua từng nút của nó, còn nút của <%s> là của <%s>; đặt <%s> cạnh thanh, trong <editor>",
						ch.Tag, toolbar.File, toolbar.Line, toolbar.Column, ch.Tag, ch.Tag, ch.Tag)
				case ch.Tag == "group" && group != nil:
					return at(ch, nil, "<group> không lồng trong <group> (nhóm ngoài ở %s:%d:%d): mỗi nhóm là một hàng nút của thanh công cụ", group.File, group.Line, group.Column)
				case ch.Tag == "group" && menu == nil:
					s.groups[ch], inner = true, ch
				}
				for i := range ch.Attributes {
					a := &ch.Attributes[i]
					switch {
					case a.Name == "for":
						return at(ch, a, "for không có trong thanh công cụ của editor (thanh ở %s:%d:%d): nút của thanh viết từng nút một, nút đầu là điểm dừng của Tab", toolbar.File, toolbar.Line, toolbar.Column)
					case a.Name == "if", a.Name == "show", strings.HasSuffix(a.Name, ":show"):
						return at(ch, a, "%s không có trong thanh công cụ của editor (thanh ở %s:%d:%d): nút của thanh luôn có mặt, nút đầu là điểm dừng của Tab; viết %s trên cả thanh", a.Name, toolbar.File, toolbar.Line, toolbar.Column, a.Name)
					}
				}
				if ch.Tag == "button" && (menu == nil || ch == menu.trigger) {
					stops = append(stops, ch)
				}
				within := menu
				if within == nil {
					within = s.menus[ch]
				}
				if err := collect(ch.Children, inner, within); err != nil {
					return err
				}
			}
			return nil
		}
		if err := collect(toolbar.Children, nil, nil); err != nil {
			return err
		}
		for i, b := range stops {
			tab := "-1"
			if i == 0 {
				tab = "0"
			}
			s.roving[b] = tab
		}
		s.toolbars[toolbar] = true
	}
	return nil
}

// editorSelectOf reads a <select value="{{ format }}">, a switch of format: no name (it is never posted), alt
// unless a field around names it, an option for each format it offers, the one the editor starts in among them.
func editorSelectOf(f editorFound, s *editorScope) error {
	n := f.node
	a, ok := n.Attribute("value")
	if !ok {
		return nil
	}
	if source, whole := wrapped(strings.TrimSpace(a.Value)); !whole || source != "format" {
		return nil
	}
	if f.popover != nil {
		return at(n, &a, `<select value="{{ format }}"> đổi cách viết của editor: đặt nó cạnh thanh công cụ hay trong thanh, không trong <popover> của liên kết`)
	}
	for _, rep := range []*Node{n, f.repeat} {
		if rep == nil {
			continue
		}
		if repeat, ok := rep.Attribute("for"); ok {
			return at(n, &a, `<select value="{{ format }}"> không lặp bằng for (for ở %s:%d:%d): mỗi editor một cách viết`, rep.File, repeat.Line, repeat.Column)
		}
	}
	if name, ok := n.Attribute("name"); ok {
		return at(n, &name, `<select value="{{ format }}"> không có name: cách viết không gửi lên server, form gửi bài bằng HTML`)
	}
	labelled := false
	for _, p := range f.path {
		labelled = labelled || p.Tag == "field"
	}
	if alt, ok := n.Attribute("alt"); !labelled && (!ok || strings.TrimSpace(alt.Value) == "") {
		return at(n, nil, `<select value="{{ format }}"> cần alt: tên của nó (alt="Viết bằng"); <label> của <field> bao ngoài editor là tên của vùng viết`)
	}
	values := map[string]bool{}
	var options func(nodes []*Node) error
	options = func(nodes []*Node) error {
		for _, ch := range nodes {
			if ch.IsText() {
				continue
			}
			if ch.Tag == "option" {
				v, ok := ch.Attribute("value")
				if !ok {
					continue // an option needs a value: its own drawing tells it
				}
				value := strings.TrimSpace(v.Value)
				if strings.Contains(v.Value, "{{") || value == "" {
					return at(ch, &v, `option của <select value="{{ format }}"> là một cách viết viết sẵn: <option value="markdown">Markdown</option>`)
				}
				if problem := editorFormatProblem(value, func(v string) string { return `<option value="` + v + `">` }); problem != "" {
					return at(ch, &v, "%s", problem)
				}
				values[value] = true
				continue
			}
			if err := options(ch.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := options(n.Children); err != nil {
		return err
	}
	if !values[s.format] {
		return at(n, nil, `<select value="{{ format }}"> thiếu <option value="%s">: editor bắt đầu ở %s, select thiếu nó thì hiện sai cách viết đang dùng`, s.format, s.format)
	}
	s.selects[n] = true
	return nil
}

// editorPopover reads the link's popover: one address input, <input value="{{ address }}" alt="…">, and at most
// one error, which the component shows when link(address) refuses an address.
func editorPopover(s *editorScope, inputs, errorsFound []editorFound) error {
	const address = `<input value="{{ address }}" alt="Địa chỉ liên kết" />`
	if len(inputs) == 0 {
		return at(s.popover, nil, "popover của editor cần đúng một <input> làm ô địa chỉ: %s", address)
	}
	if len(inputs) > 1 {
		first := inputs[0].node
		return at(inputs[1].node, nil, "popover của editor có một ô địa chỉ: <input> ở %s:%d:%d đã là ô của nó", first.File, first.Line, first.Column)
	}
	in := inputs[0].node
	for i := range in.Attributes {
		a := &in.Attributes[i]
		switch a.Name {
		case "type":
			return at(in, a, `ô địa chỉ không viết type: engine viết inputmode="url"; type="url" chặn cả form khi ô giữ /trang`)
		case "name":
			return at(in, a, "ô địa chỉ không có name: nó không gửi lên server cùng bài viết")
		case "inputmode", "autocomplete", "autocapitalize", "spellcheck":
			return at(in, a, `%s của ô địa chỉ là việc của engine (inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false"): bỏ %s`, a.Name, a.Name)
		case "value":
			if source, whole := wrapped(strings.TrimSpace(a.Value)); !whole || source != "address" {
				return at(in, a, `ô địa chỉ của editor là %s: nó ghi chữ người dùng gõ vào address, link(address) đọc nó`, address)
			}
		case "alt":
			s.addressAlt = a
		}
	}
	if _, ok := in.Attribute("value"); !ok {
		return at(in, nil, `ô địa chỉ của editor là %s: nó ghi chữ người dùng gõ vào address, link(address) đọc nó`, address)
	}
	if s.addressAlt == nil || strings.TrimSpace(s.addressAlt.Value) == "" {
		return at(in, nil, `ô địa chỉ của editor cần alt: tên của ô, cũng là tên của khung liên kết (alt="Địa chỉ liên kết")`)
	}
	s.address = in
	if len(errorsFound) > 1 {
		first := errorsFound[0].node
		return at(errorsFound[1].node, nil, "popover của editor có một lời báo địa chỉ sai: <error> ở %s:%d:%d đã là của nó", first.File, first.Line, first.Column)
	}
	if len(errorsFound) == 1 {
		s.addressError = errorsFound[0].node
		s.errorID = fmt.Sprintf("kitwork-editor-%d-address-error", s.number)
	}
	return nil
}

// editorAttributes are what the editor around adds to an element it planned: a toolbar's role and the area it
// controls, a group's role, a button's command and state, the popover's id and role, the address input's words,
// its error's id; the classes the editor's look reads. live takes what follows the editor's state.
func (r *renderer) editorAttributes(n *Node, live *grouped) (attributes, classes []string, err error) {
	s := r.editor
	attribute := func(name, value string) { attributes = append(attributes, name+`="`+html.EscapeString(value)+`"`) }
	switch {
	case s.toolbars[n]:
		classes = append(classes, "editor-toolbar")
		attributes = append(attributes, `role="toolbar"`)
		attribute("aria-controls", s.areaID)
	case s.groups[n]:
		classes = append(classes, "editor-group")
		attributes = append(attributes, `role="group"`) // named by its alt
	case n == s.popover:
		classes = append(classes, "editor-popover")
		if id, ok := n.Attribute("id"); !ok || strings.TrimSpace(id.Value) == "" {
			attribute("id", s.popoverID)
		}
		attributes = append(attributes, `role="dialog"`)
		written, err := r.interpolate(s.address, s.addressAlt, s.addressAlt.Value) // the popover is named as its input
		if err != nil {
			return nil, nil, err
		}
		attribute("aria-label", written)
	case n == s.address:
		classes = append(classes, "editor-address")
		attributes = append(attributes, `type="text"`, `inputmode="url"`, `autocomplete="off"`, `autocapitalize="none"`, `spellcheck="false"`)
	case n == s.addressError:
		classes = append(classes, "editor-address-error")
		attribute("id", s.errorID)
		attributes = append(attributes, "hidden") // the component shows it when link(address) refuses an address
	case s.selects[n]:
		attribute("data-kitwork-command", "format")
		live.binds = append(live.binds, "value: format")
		if s.readonly || s.disabled {
			if _, ok := n.Attribute("disabled"); !ok {
				attributes = append(attributes, "disabled")
			}
			attributes = append(attributes, `aria-disabled="true"`)
		}
	}
	if tab, ok := s.roving[n]; ok {
		attribute("tabindex", tab)
	}
	if b := s.buttons[n]; b != nil {
		attributes = append(attributes, r.editorButtonWire(b, live)...)
	}
	if menu := s.triggers[n]; menu != nil {
		attribute("aria-controls", menu.popoverID)
		live.binds = append(live.binds, "aria-expanded: opened") // the dropdown's opened
		attributes = append(attributes, `aria-expanded="false"`)
	}
	if menu := s.popovers[n]; menu != nil {
		if id, ok := n.Attribute("id"); !ok || strings.TrimSpace(id.Value) == "" {
			attribute("id", menu.popoverID)
		}
	}
	return attributes, classes, nil
}

// editorButtonWire is what a command's button carries for the component: its format or command, aria-pressed,
// the keys that do the same, a link's popover, aria-disabled until the component knows better.
func (r *renderer) editorButtonWire(b *editorButton, live *grouped) []string {
	s := r.editor
	var attributes []string
	attribute := func(name, value string) { attributes = append(attributes, name+`="`+html.EscapeString(value)+`"`) }
	if b.command == "format" {
		attribute("data-kitwork-command", "format")
		attribute("data-kitwork-format-value", b.value)
		live.binds = append(live.binds, "aria-pressed: format == "+quote(b.value))
		attribute("aria-pressed", strconv.FormatBool(s.format == b.value))
		if s.readonly || s.disabled {
			attributes = append(attributes, `aria-disabled="true"`)
		}
		return attributes
	}
	if b.format != "" {
		attribute("data-kitwork-format", b.format)
	} else {
		attribute("data-kitwork-command", b.wire)
	}
	if b.pressed() {
		attributes = append(attributes, `aria-pressed="false"`)
	}
	shortcut := editorKeyshortcuts[b.format]
	if b.format == "" && !b.popover {
		shortcut = editorKeyshortcuts[b.command]
	}
	if shortcut != "" {
		attribute("aria-keyshortcuts", shortcut)
	}
	if b.command == "link" && !b.apply {
		attribute("aria-controls", s.popoverID)
		attributes = append(attributes, `aria-haspopup="dialog"`)
		live.binds = append(live.binds, "aria-expanded: opened")
		attributes = append(attributes, `aria-expanded="false"`)
	}
	if s.readonly || s.disabled || b.command == "undo" || b.command == "redo" || b.command == "link" && !b.apply {
		attributes = append(attributes, `aria-disabled="true"`)
	}
	return attributes
}

// editorArea draws the writing area in place of the <textarea> written: a div.richtext the component writes in,
// holding the value cleaned, a textbox named by the field's label (or the alt) and described by its hint and
// error; then the field the form posts, a <textarea> with the textarea's name holding the cleaned value too,
// which is all a page without JavaScript shows.
func (r *renderer) editorArea(n *Node) error {
	s := r.editor
	source := ""
	if s.value != nil {
		v, err := r.value(n, s.value, s.value.Value)
		if err != nil {
			return err
		}
		if !v.IsNullish() {
			source = v.ToString().Text()
		}
	}
	d := editorCleaned(source)
	// the look written on the textarea is both the area's and the field's
	look := *n
	look.Tag, look.Attributes, look.Children, look.source = "richtext", nil, nil, nil
	for _, a := range n.Attributes {
		if _, base := splitPrefix(a.Name); stylesheet.IsPresentation(base) && !isEvent(a.Name) {
			look.Attributes = append(look.Attributes, a)
		}
	}
	open, err := r.opening(&look, kindInline)
	if err != nil {
		return err
	}
	presentation := open.classes
	if len(presentation) > 0 && presentation[0] == stylesheet.RichtextClass {
		presentation = presentation[1:]
	}
	areaClasses := append([]string{stylesheet.RichtextClass, "editor-area"}, presentation...)
	if s.rows > 0 {
		areaClasses = append(areaClasses, stylesheet.EditorRowsClass(s.rows))
	}
	var area []string
	attribute := func(list *[]string, name, value string) {
		*list = append(*list, name+`="`+html.EscapeString(value)+`"`)
	}
	attribute(&area, "id", s.areaID)
	area = append(area, `role="textbox"`, `aria-multiline="true"`)
	if s.field != nil && s.field.label != "" {
		attribute(&area, "aria-labelledby", s.field.label)
	} else if s.alt != nil {
		written, err := r.interpolate(n, s.alt, s.alt.Value)
		if err != nil {
			return err
		}
		attribute(&area, "aria-label", written)
	}
	var described []string
	if s.field != nil {
		for _, id := range []string{s.field.hint, s.field.error} {
			if id != "" {
				described = append(described, id)
			}
		}
	}
	if len(described) > 0 {
		attribute(&area, "aria-describedby", strings.Join(described, " "))
	}
	placeholder := ""
	if s.placeholder != nil {
		if placeholder, err = r.interpolate(n, s.placeholder, s.placeholder.Value); err != nil {
			return err
		}
		attribute(&area, "aria-placeholder", placeholder)
	}
	if s.required {
		area = append(area, `aria-required="true"`)
	}
	area = append(area, `aria-readonly="true"`) // until the component starts, and for good when readonly
	if s.disabled {
		area = append(area, `aria-disabled="true"`)
	}
	area = append(area, `translate="no"`, `spellcheck="true"`)
	attribute(&area, "data-kitwork-name", s.name)
	area = append(area, open.attributes...) // what of the look follows a component's state
	r.write("div", start{classes: areaClasses, attributes: area})
	r.out.WriteString(d.HTML)
	r.out.WriteString("</div>")

	var field []string
	if s.field != nil {
		attribute(&field, "id", s.field.control)
	}
	attribute(&field, "name", s.name)
	for _, word := range []struct {
		on   bool
		name string
	}{{s.required, "required"}, {s.readonly, "readonly"}, {s.disabled, "disabled"}} {
		if word.on {
			field = append(field, word.name)
		}
	}
	if s.rows > 0 {
		attribute(&field, "rows", strconv.Itoa(s.rows))
	}
	if s.placeholder != nil {
		attribute(&field, "placeholder", placeholder)
	}
	if s.alt != nil {
		written, err := r.interpolate(n, s.alt, s.alt.Value)
		if err != nil {
			return err
		}
		attribute(&field, "aria-label", written)
	}
	if len(described) > 0 {
		attribute(&field, "aria-describedby", strings.Join(described, " "))
	}
	field = append(field, open.attributes...)
	r.write("textarea", start{classes: append([]string{"editor-field"}, presentation...), attributes: field})
	r.out.escape(d.HTML)
	r.out.WriteString("</textarea>")
	return nil
}

// cleanCache holds the documents editors' values cleaned to, by value, for every page: a post read on every
// request is cleaned once. Bounded as displayCache is. Safe for concurrent use.
type cleanCache struct {
	mu      sync.Mutex
	entries map[string]richtext.Document
	size    int
	limit   int
}

// cleaned is the cache of every page's editors.
var cleaned = &cleanCache{limit: 8 << 20}

// editorCleaned is source cleaned (richtext.Clean), from the cache when it is there.
func editorCleaned(source string) richtext.Document {
	cleaned.mu.Lock()
	d, found := cleaned.entries[source]
	cleaned.mu.Unlock()
	if found {
		return d
	}
	d = richtext.Clean(source)
	size := len(source) + len(d.HTML) + len(d.Text)
	if size > cleaned.limit/8 {
		return d
	}
	cleaned.mu.Lock()
	defer cleaned.mu.Unlock()
	if cleaned.size+size > cleaned.limit {
		cleaned.entries, cleaned.size = nil, 0
	}
	if cleaned.entries == nil {
		cleaned.entries = map[string]richtext.Document{}
	}
	if _, kept := cleaned.entries[source]; !kept {
		cleaned.entries[source] = d
		cleaned.size += size
	}
	return d
}

// nearestWords says the words of set nearest word, for a message ("; gần nhất: …"), "" when none is near.
func nearestWords(word string, set []string) string {
	limit := max(len(word)/3, 2)
	best := limit + 1
	var near []string
	for _, candidate := range set {
		d := editDistance(word, candidate, best)
		switch {
		case d < best:
			best, near = d, []string{candidate}
		case d == best && d <= limit:
			near = append(near, candidate)
		}
	}
	if len(near) == 0 {
		return ""
	}
	if len(near) > 3 {
		near = near[:3]
	}
	return "; gần nhất: " + strings.Join(near, ", ")
}

// editDistance is the number of single-letter edits between two words (Levenshtein), or more than limit as soon
// as it is.
func editDistance(a, b string, limit int) int {
	if d := len(a) - len(b); d > limit || -d > limit {
		return limit + 1
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		lowest := current[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
			lowest = min(lowest, current[j])
		}
		if lowest > limit {
			return limit + 1
		}
		previous = current
	}
	return previous[len(b)]
}
