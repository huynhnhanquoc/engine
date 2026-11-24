package ui

import (
	"hash/fnv"
	"html"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/compiler"
)

// A dialog (docs/components/dialog.md): an element of Kitwork with no look of its own, always modal, drawn once
// (often in overlay.ui) and opened by its alias from anywhere, in one line:
//
//	<dialog as="$dialog" keydown:escape="close()">
//	  <box place="fill" background="black/50" click="close()" />
//	  <card place="center" gap="4" padding="6">
//	    <text heading="2">{{ value.title }}</text>
//	    <button click="close()">No</button><button click="confirm()">Yes</button>
//	  </card>
//	</dialog>
//	<button click="$dialog.open({ title: 'Delete order ' + order.id + '?', confirm: () => remove(order.id) })">Delete</button>
//
// On the web it is the browser's <dialog>, which the kernel opens with showModal() (library/component/dialog.js): the
// top layer, the page behind it inert, focus kept inside and given back to what opened it. It fills the screen and
// is see-through (reboot.css), so what it holds is placed against the screen, inside it (center, bottom, right,
// fill…), the scrim one of them the author draws. Its value is what open(x) gives, in the browser: the server, which
// draws it closed, writes nothing of what reads it (value), and the kernel paints nothing inside it while it is
// closed.

// outsidePlaces are the places that float outside what holds them: a dialog's frame is the screen, which has no
// outside.
var outsidePlaces = map[string]bool{"below-left": true, "below": true, "below-right": true, "above-left": true, "above": true, "above-right": true}

// dialogHost reads a dialog's own words and what it holds: it stands outside every for (one dialog for the whole
// list, open(order) keeping the row in value), it has an alias (nothing else opens it), its show is refused (it
// opens by open()), what it holds is placed inside the screen; its first heading names it (aria-labelledby), and a
// dialog without one, or without keydown:escape, is told so.
func (r *renderer) dialogHost(n *Node) (*Node, *host, error) {
	if r.rows > 0 {
		return nil, nil, at(n, nil, "dialog trong for: mỗi dòng sẽ là một dialog; vẽ một dialog ngoài danh sách và mở nó với dòng ấy, $dialog.open(order), rồi đọc value.id bên trong")
	}
	if _, named := n.Attribute("as"); !named {
		return nil, nil, at(n, nil, "%s", `dialog không có as: không gì mở được nó; viết <dialog as="$dialog" keydown:escape="close()"> rồi gọi $dialog.open({ … }) ở bất cứ đâu`)
	}
	for i := range n.Attributes {
		a := &n.Attributes[i]
		if _, name := splitPrefix(a.Name); name == "show" {
			return nil, nil, at(n, a, "dialog mở bằng open() và đóng bằng close(), không bằng show; bỏ show")
		}
	}
	if err := dialogPlaces(n); err != nil {
		return nil, nil, err
	}
	for _, scrim := range dialogScrims(n) {
		// what covers the screen and holds nothing is the scrim the author draws: decoration a screen reader skips
		r.hosts[scrim] = &host{attributes: []string{`aria-hidden="true"`}}
	}
	own := &host{}
	if title := dialogTitle(n); title != nil {
		id := ""
		if a, ok := title.Attribute("id"); ok && !strings.Contains(a.Value, "{{") {
			id = strings.TrimSpace(a.Value)
		}
		if id == "" {
			id = dialogID(n) + "-title"
			r.hosts[title] = &host{attributes: []string{`id="` + id + `"`}}
		}
		own.attributes = append(own.attributes, `aria-labelledby="`+html.EscapeString(id)+`"`)
	} else {
		r.warn(n, nil, "%s", `dialog không có tiêu đề: <text heading="2"> đầu tiên bên trong là tên của nó (aria-labelledby), trình đọc màn hình đọc dialog không tên`)
	}
	escape := false
	for _, a := range n.Attributes {
		head, modifiers, _ := strings.Cut(a.Name, ":")
		if strings.Contains(","+head+",", ",keydown,") && contains(splitModifiers(modifiers), "escape") {
			escape = true
		}
	}
	if !escape {
		r.warn(n, nil, "%s", `dialog không có keydown:escape="close()": người dùng bàn phím cần Escape để thoát, và trình duyệt không tự đóng dialog của Kitwork (để opened luôn khớp)`)
	}
	return n, own, nil
}

// dialogPlaces refuses a place outside what holds it on what a dialog holds: its frame is the screen.
func dialogPlaces(n *Node) error {
	for _, child := range n.Elements() {
		if child.Tag == "fragment" {
			if err := dialogPlaces(child); err != nil {
				return err
			}
			continue
		}
		for i := range child.Attributes {
			a := &child.Attributes[i]
			if _, name := splitPrefix(a.Name); name == "place" && outsidePlaces[strings.TrimSpace(a.Value)] {
				return at(child, a, "place=\"%s\" trong dialog: khung của dialog là cả màn hình, không có bên ngoài; viết chỗ bên trong (center, bottom, right, fill…)", strings.TrimSpace(a.Value))
			}
		}
	}
	return nil
}

// dialogScrims are what a dialog holds that covers the screen and holds nothing (place="fill", no prefix, empty):
// the scrim behind what it asks.
func dialogScrims(n *Node) []*Node {
	var found []*Node
	for _, child := range n.Elements() {
		if child.Tag == "fragment" {
			found = append(found, dialogScrims(child)...)
			continue
		}
		if a, ok := child.Attribute("place"); ok && strings.TrimSpace(a.Value) == "fill" && len(child.Children) == 0 {
			found = append(found, child)
		}
	}
	return found
}

// dialogTitle is the first <text heading="…"> inside a dialog, in the order written; nil when there is none.
func dialogTitle(n *Node) *Node {
	for _, child := range n.Elements() {
		if child.Tag == "text" {
			if _, ok := child.Attribute("heading"); ok {
				return child
			}
		}
		if found := dialogTitle(child); found != nil {
			return found
		}
	}
	return nil
}

// dialogID is the name of a dialog's ids: kitwork-dialog- and a hash of the file, line and column of its tag, one
// dialog a place (none stands in a for).
func dialogID(n *Node) string {
	sum := fnv.New32a()
	sum.Write([]byte(strings.ReplaceAll(n.File, `\`, "/") + ":" + strconv.Itoa(n.Line) + ":" + strconv.Itoa(n.Column)))
	return "kitwork-dialog-" + strconv.FormatUint(uint64(sum.Sum32()), 36)
}

// dialogValue says source reads the value of a dialog around it, which only the browser has: the server draws the
// dialog closed and writes nothing of it.
func (r *renderer) dialogValue(reads []string) bool {
	for _, name := range reads {
		if name == "value" {
			if owner := r.declaring(name); owner != nil && owner.component == "dialog" {
				return true
			}
		}
	}
	return false
}

// dialogUnread refuses an if or a for that reads a dialog's value: the server, drawing the dialog closed, would
// draw nothing of it, and the browser has nothing to show when it opens. show hides and shows what is drawn.
func (r *renderer) dialogUnread(n *Node, a *Attribute, source, word string) error {
	source = strings.TrimSpace(source)
	if bare, ok := wrapped(source); ok {
		source = bare
	}
	compiled, err := r.site.compile(source)
	if err != nil || !r.dialogValue(compiled.reads) {
		return nil // its own error, where it is read
	}
	if word == "if" {
		return at(n, a, "if đọc value của dialog, thứ chỉ có ở trình duyệt khi open(x) chạy, nên server không vẽ gì để hiện: viết show=\"%s\"", source)
	}
	return at(n, a, "for đọc value của dialog, thứ chỉ có ở trình duyệt khi open(x) chạy: danh sách trong dialog chưa có; đưa vào value những gì dialog hiện (value.title, value.count)")
}

// browserDialogs are the browser's and the dropped service's words for asking and telling: each an error saying
// what a page writes instead (26/10/2026).
var browserDialogs = map[string]string{
	"confirm": `confirm("…") của trình duyệt không có trong Kitwork: hỏi trước khi làm là $dialog.open({ title: '…', confirm: () => … }), với <dialog as="$dialog"> vẽ một lần trong overlay.ui`,
	"alert":   `alert("…") của trình duyệt không có trong Kitwork: báo một câu là $toast.show('…'); cần chặn trang tới khi đọc xong thì vẽ một <dialog> chỉ có nút close() và mở nó bằng open({ title })`,
	"dialog":  `dialog.confirm và dialog.alert đã bỏ (26/10/2026): hỏi trước khi làm là $dialog.open({ title: '…', confirm: () => … }), với <dialog as="$dialog"> vẽ một lần trong overlay.ui`,
}

// dialogCall checks what each $x.open(…) of a dialog's alias in source is given: its confirm is an arrow written
// there with no parameter, which the dialog calls with none (docs/components/dialog.md, "Compiler kiểm").
func dialogCall(n *Node, a *Attribute, source, alias string) error {
	for _, arguments := range compiler.MethodCallArguments(source, alias, "open") {
		if len(arguments) == 0 {
			continue
		}
		object, ok := arguments[0].(*compiler.ObjectLiteral)
		if !ok {
			continue
		}
		for _, entry := range object.Entries {
			if entry.IsSpread || keyName(entry.Key) != "confirm" {
				continue
			}
			fn, ok := entry.Value.(*compiler.FunctionLiteral)
			if !ok {
				return at(n, a, "confirm của %s.open là việc chạy khi người dùng đồng ý, một hàm viết tại chỗ: confirm: () => remove(order.id)", alias)
			}
			if len(fn.Parameters) > 0 {
				return at(n, a, "confirm nhận tham số (dialog gọi nó không đối số): viết () => remove(order.id), lấy thứ nó cần từ chỗ gọi")
			}
		}
	}
	return nil
}

// keyName is the name of an object literal's key written as a name or a string, "" for any other.
func keyName(key compiler.Expression) string {
	switch k := key.(type) {
	case *compiler.Identifier:
		return k.Value
	case *compiler.Literal:
		if k.Value.IsString() {
			return k.Value.ToString().Text()
		}
	}
	return ""
}
