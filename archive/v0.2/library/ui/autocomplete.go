package ui

import (
	"html"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// An autocomplete (docs/components/autocomplete.md): type, see what matches, choose one. Written as an element, like
// an input of HTML, on what HTML and ARIA have: its input is where one types, its popover the list, each option a
// choice, an empty what it says when nothing matches.
//
//	<field>
//	  <label>District</label>
//	  <autocomplete name="district" value="q3" required="true" change="loadWards()">
//	    <input placeholder="Choose a district" />
//	    <popover place="below-left">
//	      <option value="q1">District 1</option>
//	      <option value="q3">District 3</option>
//	      <empty>No district matches</empty>
//	    </popover>
//	  </autocomplete>
//	</field>
//
// The autocomplete is the form's field, its input only where one types: its name goes on a hidden input written
// right after the input, which the form sends with the chosen option's value (q3), never the words shown (District
// 3); required is the input's, held by the component until an option is chosen. The input is a combobox
// (aria-autocomplete, aria-expanded following opened, aria-controls the list, aria-activedescendant the option lit
// by the arrows), showing the chosen option's words as the server draws it; the popover is the listbox, each option a
// div with role option, its value in data-value, the chosen one aria-selected; the empty a role="status" hidden until
// nothing matches. An input that writes no input event of its own is filtered by the component as one types,
// without accents or case ("quan 1" finds "Quận 1"); one that does is the page's to fill. Choosing sends change on
// the autocomplete, $event.value and $event.label (library/component/autocomplete.js).
//
// value="{{ district }}", district a var of a page that runs in the browser, is two-way (16/11/2026): the hidden input
// is the var's field, the kernel's two-way value, so choosing writes the var and the page writing it shows the option
// it names (its words). A page that searches writes the input's own value="{{ keyword }}" and input:debounce(300)=
// "search()", its options a list of its data the browser keeps (option for="item of districts"): the component reads
// the list after each paint, and choosing writes the keyword var with the option's words.
//
// Not yet: options in a list or a group beside other content, the opened: prefix on the input itself, the suggest
// mode (words typed kept, docs "Còn mở").

// autocompleteScope is an autocomplete whose inside is being drawn: the id of its list, how many options are drawn,
// the value chosen.
type autocompleteScope struct {
	id, value string
	input     *Node
	// rows counts the options drawn from each option written (a for's rows)
	rows map[*Node]int
}

// autocompleteHost reads an autocomplete's own words and its parts: one input, one popover holding the options
// (and one empty), its name and required moved from the input to the autocomplete; it gives its input the combobox's
// words and the chosen option's words, the hidden input after it, its popover the listbox's.
func (r *renderer) autocompleteHost(n *Node) (*Node, *host, *autocompleteScope, error) {
	if a, ok := n.Attribute("as"); ok {
		return nil, nil, nil, at(n, &a, `autocomplete không có as: giá trị đi vào và ra qua value="{{ biến }}", kết quả báo bằng change="…"`)
	}
	var input, popover *Node
	for _, child := range n.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return nil, nil, nil, at(child, nil, "chữ không đặt thẳng trong autocomplete; con của nó là một <input> và một <popover>")
			}
			continue
		}
		switch child.Tag {
		case "input":
			if input != nil {
				return nil, nil, nil, at(child, nil, "autocomplete có đúng một <input>: chỗ gõ để tìm")
			}
			input = child
		case "popover":
			if popover != nil {
				return nil, nil, nil, at(child, nil, "autocomplete có đúng một <popover>: khung các lựa chọn")
			}
			popover = child
		default:
			return nil, nil, nil, at(child, nil, "<%s> trong autocomplete: con của autocomplete là một <input> (chỗ gõ) và một <popover> (các option)", child.Tag)
		}
	}
	if input == nil || popover == nil {
		return nil, nil, nil, at(n, nil, "autocomplete cần một <input> (chỗ gõ) và một <popover> chứa các <option>")
	}
	for _, word := range []string{"name", "required"} {
		if a, ok := input.Attribute(word); ok {
			return nil, nil, nil, at(input, &a, "%s đặt trên <autocomplete>, không trên input bên trong: form gửi mục đã chọn, không phải chữ đang gõ", word)
		}
	}
	if err := r.autocompleteOptions(popover); err != nil {
		return nil, nil, nil, err
	}
	scope := &autocompleteScope{id: placeID("kitwork-autocomplete", n), input: input, rows: map[*Node]int{}}
	name, required, model := "", false, ""
	for i := range n.Attributes {
		a := &n.Attributes[i]
		switch a.Name {
		case "value":
			written, err := r.interpolate(n, a, a.Value)
			if err != nil {
				return nil, nil, nil, err
			}
			scope.value = written
			source, client, err := r.liveExpression(n, a, a.Value)
			if err != nil {
				return nil, nil, nil, err
			}
			if client {
				if !identifier(source) {
					return nil, nil, nil, at(n, a, `value của autocomplete đọc %s, thứ trình duyệt giữ: viết một var của trang, value="{{ district }}", để chọn thì biến đổi theo và gán biến thì ô đổi theo`, source)
				}
				model = source // two-way: the hidden input is the var's field (the kernel's two-way value)
			}
		case "name":
			written, err := r.interpolate(n, a, a.Value)
			if err != nil {
				return nil, nil, nil, err
			}
			name = written
		case "required":
			on, err := r.booleanOn(n, a)
			if err != nil {
				return nil, nil, nil, err
			}
			required = on
		}
	}
	label, err := r.chosenLabel(popover, scope.value)
	if err != nil {
		return nil, nil, nil, err
	}
	words := []string{`role="combobox"`, `aria-autocomplete="list"`, `aria-controls="` + scope.id + `"`, `autocomplete="off"`}
	if _, own := input.Attribute("value"); !own {
		words = append(words, `value="`+html.EscapeString(label)+`"`)
	}
	if required {
		words = append(words, "required")
	}
	r.hosts[input] = &host{attributes: words}
	hidden := `<input type="hidden"`
	if name != "" {
		hidden += ` name="` + html.EscapeString(name) + `"`
	}
	hidden += ` value="` + html.EscapeString(scope.value) + `"`
	if model != "" {
		hidden += ` data-kitwork-bind="value: ` + html.EscapeString(model) + `"`
	}
	r.after[input] = hidden + ">"
	r.hosts[popover] = &host{attributes: []string{`id="` + scope.id + `"`, `role="listbox"`}}
	filter := "true"
	for _, a := range input.Attributes {
		if head, _, _ := strings.Cut(a.Name, ":"); head == "input" {
			filter = "false" // the page fills the list as one types: shown as it comes
		}
	}
	drawn := *n
	drawn.source = nil
	drawn.Attributes = nil
	for _, a := range n.Attributes {
		if a.Name != "value" && a.Name != "name" && a.Name != "required" {
			drawn.Attributes = append(drawn.Attributes, a)
		}
	}
	drawn.Children = n.Children
	return &drawn, &host{inputs: []string{"filter: " + filter}}, scope, nil
}

// autocompleteOptions reads what a popover of an autocomplete holds: options and one empty, directly; two options
// written with one value are an error, one written without a value is warned of (its words are its value), a link or
// a button in an option is an error.
func (r *renderer) autocompleteOptions(popover *Node) error {
	seen := map[string]bool{}
	empties := 0
	for _, child := range popover.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return at(child, nil, "chữ không đặt thẳng trong popover của autocomplete; mỗi lựa chọn là một <option>, không có kết quả là <empty>")
			}
			continue
		}
		switch child.Tag {
		case "option":
			if inner := firstOf(child, func(m *Node) bool { return m != child && (m.Tag == "button" || m.Tag == "link") }); inner != nil {
				return at(inner, nil, "<%s> trong option: chọn cả dòng là chọn mục ấy (nút trong nút); đặt nó ngoài option", inner.Tag)
			}
			if _, looped := child.Attribute("for"); looped {
				continue
			}
			a, ok := child.Attribute("value")
			if !ok {
				r.warn(child, nil, "%s", `option không có value: chữ của nó là value; viết value="…" để form gửi một mã, không phải chữ hiện ra`)
				continue
			}
			if strings.Contains(a.Value, "{{") {
				continue
			}
			if seen[a.Value] {
				return at(child, &a, "hai option cùng value=%q: chọn cái nào thì form cũng gửi một thứ", a.Value)
			}
			seen[a.Value] = true
		case "empty":
			empties++
			if empties > 1 {
				return at(child, nil, "popover của autocomplete có một <empty>: điều nói khi không có mục nào khớp")
			}
		case "list", "group":
			return at(child, nil, "<%s> trong popover của autocomplete: option gom trong list hay group chưa có; đặt các <option> thẳng trong popover", child.Tag)
		default:
			return at(child, nil, "<%s> trong popover của autocomplete: popover giữ các <option> và một <empty>; nội dung khác cạnh option chưa có", child.Tag)
		}
	}
	return nil
}

// chosenLabel is the words of the option whose value is value, as the server draws them ("" for none): an option
// written, or a row of a list the server holds.
func (r *renderer) chosenLabel(popover *Node, chosen string) (string, error) {
	if chosen == "" {
		return "", nil
	}
	labelOf := func(option *Node) (string, bool, error) {
		value, label := "", ""
		for _, child := range option.Children {
			if child.IsText() {
				written, err := r.interpolate(option, nil, child.Text)
				if err != nil {
					return "", false, err
				}
				label += written
			} else if label == "" {
				var b strings.Builder
				for _, inner := range child.Children {
					if inner.IsText() {
						written, err := r.interpolate(child, nil, inner.Text)
						if err != nil {
							return "", false, err
						}
						b.WriteString(written)
					}
				}
				label = b.String()
			}
		}
		label = strings.TrimSpace(collapseSpace(label))
		value = label
		if a, ok := option.Attribute("value"); ok {
			written, err := r.interpolate(option, &a, a.Value)
			if err != nil {
				return "", false, err
			}
			value = written
		}
		return label, value == chosen, nil
	}
	for _, option := range popover.Elements() {
		if option.Tag != "option" {
			continue
		}
		a, looped := option.Attribute("for")
		if !looped {
			label, found, err := labelOf(option)
			if err != nil || found {
				return label, err
			}
			continue
		}
		source := strings.TrimSpace(a.Value)
		if bare, ok := wrapped(source); ok {
			source = bare
		}
		head, _, _ := strings.Cut(source, ";")
		names, list, ok := strings.Cut(head, " of ")
		if !ok {
			continue
		}
		items, err := r.value(option, &a, list) // a list the browser keeps too: as it is when the page is drawn
		if err != nil {
			return "", err
		}
		item, index, _ := strings.Cut(names, ",")
		item, index = strings.TrimSpace(item), strings.TrimSpace(index)
		for i, each := range items.Items() {
			takeItem := r.eval.put(item, each)
			takeIndex := func() {}
			if index != "" {
				takeIndex = r.eval.put(index, value.Number(float64(i)))
			}
			label, found, err := labelOf(option)
			takeIndex()
			takeItem()
			if err != nil || found {
				return label, err
			}
		}
	}
	return "", nil
}

// autocompleteOption draws an option of the autocomplete being drawn: a div with role option, its id (the place it is
// written, and its row), whether it is the one chosen, its value (data-value; the words when none is written). It
// reads the autocomplete's value, so it is drawn again at each request (frozen.go, scopeAutocomplete).
func (r *renderer) autocompleteOption(n *Node) error {
	scope := r.autocomplete
	r.readScope(scopeAutocomplete)
	scope.rows[n]++
	id := placeID(scope.id+"-option", n)
	if row := scope.rows[n]; row > 1 {
		id += "-" + strconv.Itoa(row)
	}
	words := []string{`role="option"`, `id="` + id + `"`}
	chosen := ""
	if a, ok := n.Attribute("value"); ok {
		written, err := r.interpolate(n, &a, a.Value)
		if err != nil {
			return err
		}
		chosen = written
	} else {
		chosen = strings.TrimSpace(collapseSpace(plainChildren(n)))
		words = append(words, `data-value="`+html.EscapeString(chosen)+`"`)
	}
	selected := "false"
	if scope.value != "" && chosen == scope.value {
		selected = "true"
	}
	words = append(words, `aria-selected="`+selected+`"`)
	r.hosts[n] = &host{attributes: words}
	return r.tag(n, "div", kindInline)
}

// plainChildren are the words written right inside n, without what {{ }} would give.
func plainChildren(n *Node) string {
	var b strings.Builder
	for _, child := range n.Children {
		if child.IsText() {
			b.WriteString(child.Text)
		}
	}
	return b.String()
}
