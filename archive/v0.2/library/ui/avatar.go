package ui

import (
	"hash/fnv"
	"html"
	"strconv"
	"strings"
	"unicode"

	"github.com/kitwork/engine2/core/value"
)

// An avatar and a group of them (docs/components/avatar.md, avatars.md): the picture of a person or a place, and
// several of them overlapping with a "+N" for the rest.
//
//	<avatar size="large">
//	  <image value="{{ chat.avatar }}" alt="{{ chat.name }}" />
//	  <badge show="chat.online" place="bottom-right" color="success" alt="Online"><icon value="point-fill" /></badge>
//	</avatar>
//	<avatars max="3" size="small">
//	  <avatar for="member of members" key="member.id"><image value="{{ member.avatar }}" alt="{{ member.name }}" /></avatar>
//	</avatars>
//
// An avatar holds one image and the badges on its corners. It is two spans: the outer one placing the badges,
// never cut, isolated so what is inside never lies over the page; the face, cut round (or by radius), the image
// over the initials of its alt ("Lan Nguyễn": LN) on a tone its name picks, the same name the same tone on every
// page. With no image the server draws the initials only, no <img>; with one, the avatar's file
// (library/component/avatar.js) hides an image that fails, so the initials show. alt="" is a person's icon. Its size
// is a word, as a text's (16/11/2026, the owner chose "size bằng chữ"): mini, small, medium (the default), large,
// huge, 24 to 64 pixels, the initials and the badges in proportion. A group gives its avatars its size, draws max of
// them (a list of the page cut on the server) and a "+N" a screen reader skips, the group named once ("Lan, Minh and
// 5 more").

// avatarSizes are the words of an avatar's size, in order.
var avatarSizes = []string{"mini", "small", "medium", "large", "huge"}

// avatarCorners are the places of a badge on an avatar.
var avatarCorners = map[string]bool{"top-left": true, "top-right": true, "bottom-left": true, "bottom-right": true}

// avatarTones are how many tones an avatar's initials sit on (reboot.css avatar-tone-1…5: brand, info, success,
// warning, danger, mixed into paper).
const avatarTones = 5

// avatarsScope is a group whose avatars are being drawn: their size, how many it draws, how many it has drawn.
type avatarsScope struct {
	size       string
	max, drawn int
}

// avatarElement draws an avatar: its face (the image over the initials), then its badges.
func (r *renderer) avatarElement(n *Node) error {
	group := r.avatars
	if group != nil {
		if group.max > 0 && group.drawn >= group.max {
			return nil // past what the group shows: counted in its "+N"
		}
		group.drawn++
	}
	size := "medium"
	if group != nil {
		size = group.size
	}
	outer, face := *n, *n
	outer.Attributes, outer.Children, face.Attributes, face.Children = nil, nil, nil, nil
	outer.source, face.source = nil, nil // drawn as made here, never as its file's (frozen.go)
	for i := range n.Attributes {
		a := &n.Attributes[i]
		_, base := splitPrefix(a.Name)
		switch {
		case a.Name == "for" || a.Name == "if" || a.Name == "key":
			// read where the avatar is drawn
		case a.Name == "size":
			if group != nil {
				return at(n, a, "size trên avatar trong avatars: cả nhóm một cỡ, viết size trên <avatars>")
			}
			size = strings.TrimSpace(a.Value)
			if !contains(avatarSizes, size) {
				return at(n, a, "size của avatar là một chữ: %s (24 tới 64 điểm ảnh)", strings.Join(avatarSizes, ", "))
			}
		case base == "radius":
			face.Attributes = append(face.Attributes, *a) // the face is what is cut
		default:
			outer.Attributes = append(outer.Attributes, *a)
		}
	}
	var image *Node
	var badges []*Node
	corners := map[string]bool{}
	for _, child := range n.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return at(child, nil, "chữ không đặt thẳng trong avatar; tên là alt của image, chữ ở góc là một <badge>")
			}
			continue
		}
		switch child.Tag {
		case "image":
			if image != nil {
				return at(child, nil, "avatar có đúng một image; nhiều người thì dùng <avatars>")
			}
			image = child
		case "badge":
			place, ok := child.Attribute("place")
			if !ok {
				return at(child, nil, `badge trong avatar đặt ở một góc: place="bottom-right" (top-left, top-right, bottom-left, bottom-right)`)
			}
			corner := strings.TrimSpace(place.Value)
			if !avatarCorners[corner] {
				return at(child, &place, "badge trong avatar đặt ở một góc: top-left, top-right, bottom-left hay bottom-right, không %q", corner)
			}
			if corners[corner] {
				r.warn(child, &place, "hai badge cùng place=%q trên avatar: cái viết sau đè lên cái trước", corner)
			}
			corners[corner] = true
			badges = append(badges, child)
		case "avatar":
			return at(child, nil, "avatar trong avatar: con của avatar là một image và các badge ở góc; nhiều người thì dùng <avatars>, chồng mép và có ô +N")
		default:
			return at(child, nil, "<%s> trong avatar: con của avatar là một image và các badge ở góc; nút đặt ngoài avatar, bên cạnh nó", child.Tag)
		}
	}
	if image == nil {
		return at(n, nil, `avatar cần một image: <image value="{{ user.avatar }}" alt="{{ user.name }}" /> (alt là tên, cho chữ cái đầu khi không có ảnh)`)
	}
	alt, ok := image.Attribute("alt")
	if !ok {
		return at(image, nil, `image của avatar cần alt: tên của người hay nơi ấy (chữ cái đầu khi không có ảnh); alt="" thì là icon người`)
	}
	name, err := r.interpolate(image, &alt, alt.Value)
	if err != nil {
		return err
	}
	src := ""
	if a, ok := image.Attribute("value"); ok {
		if src, err = r.interpolate(image, &a, a.Value); err != nil {
			return err
		}
	}
	shown := strings.TrimSpace(src) != ""
	beside := group == nil && r.namedBeside(n, alt.Value) // read before entering it: its parent is the page's
	open, err := r.opening(&outer, kindInline)
	if err != nil {
		return err
	}
	open.classes = append([]string{"avatar", "avatar-" + size}, open.classes...)
	if shown {
		// an image that fails to load is hidden, so the initials under it show (avatar.js)
		open.attributes = append(open.attributes, `data-kitwork-component="avatar"`)
		r.load("avatar")
	}
	r.write("span", open)
	faceOpen, err := r.opening(&face, kindInline)
	if err != nil {
		return err
	}
	faceOpen.classes = append([]string{"avatar-face", "avatar-tone-" + strconv.Itoa(avatarTone(name))}, faceOpen.classes...)
	r.write("span", faceOpen)
	r.enter(n)
	defer r.leave()
	if shown {
		drawn := *image
		drawn.source = nil
		if beside {
			// the name is written beside it: a screen reader does not read it twice
			drawn.Attributes = append([]Attribute(nil), image.Attributes...)
			for i := range drawn.Attributes {
				if drawn.Attributes[i].Name == "alt" {
					drawn.Attributes[i].Value = ""
				}
			}
		}
		if err := r.tag(&drawn, "img", kindVoid); err != nil {
			return err
		}
	}
	if letters := initials(name); letters != "" {
		r.out.WriteString(`<span aria-hidden="true">`)
		r.out.escape(letters)
		r.out.WriteString(`</span>`)
	} else {
		r.icons = append(r.icons, "user")
		r.out.WriteString(`<i class="icon-user" aria-hidden="true"></i>`)
	}
	r.out.WriteString("</span>")
	for _, badge := range badges {
		if err := r.node(badge, true); err != nil {
			return err
		}
	}
	r.out.WriteString("</span>")
	return nil
}

// namedBeside says the element right before or after n is a text whose words are written as alt is ({{ user.name }}
// beside alt="{{ user.name }}"): the name is read there.
func (r *renderer) namedBeside(n *Node, alt string) bool {
	parent := r.parent()
	if parent == nil || strings.TrimSpace(alt) == "" {
		return false
	}
	siblings := parent.Elements()
	for i, sibling := range siblings {
		if sibling != n && (sibling.File != n.File || sibling.Line != n.Line || sibling.Column != n.Column) {
			continue // the same tag, drawn from a copy of the tree (frozen.go)
		}
		for _, j := range []int{i - 1, i + 1} {
			if j < 0 || j >= len(siblings) || siblings[j].Tag != "text" || len(siblings[j].Children) != 1 || !siblings[j].Children[0].IsText() {
				continue
			}
			if strings.TrimSpace(collapseSpace(siblings[j].Children[0].Text)) == strings.TrimSpace(collapseSpace(alt)) {
				return true
			}
		}
	}
	return false
}

// initials are the first letters of the first and the last word of name, upper case ("Lan Nguyễn": LN, "Quốc": Q);
// "" for a name with no letter.
func initials(name string) string {
	var words []string
	for _, word := range strings.FieldsFunc(name, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) {
		words = append(words, word)
	}
	if len(words) == 0 {
		return ""
	}
	first := []rune(words[0])[:1]
	if len(words) == 1 {
		return strings.ToUpper(string(first))
	}
	last := []rune(words[len(words)-1])[:1]
	return strings.ToUpper(string(first) + string(last))
}

// avatarTone is the tone a name's initials sit on, 1 to avatarTones: the same name the same tone on every page.
func avatarTone(name string) int {
	sum := fnv.New32a()
	sum.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return int(sum.Sum32()%avatarTones) + 1
}

// avatarsElement draws a group of avatars: its size and max read, the names of the avatars it shows and how many
// more read first (a screen reader hears the group once), then the avatars it shows and a "+N".
func (r *renderer) avatarsElement(n *Node) error {
	scope := &avatarsScope{size: "medium"}
	outer := *n
	outer.Attributes, outer.Children = nil, nil
	outer.source = nil
	for i := range n.Attributes {
		a := &n.Attributes[i]
		_, base := splitPrefix(a.Name)
		switch {
		case a.Name == "for" || a.Name == "if" || a.Name == "key":
		case a.Name == "size":
			scope.size = strings.TrimSpace(a.Value)
			if !contains(avatarSizes, scope.size) {
				return at(n, a, "size của avatars là một chữ: %s (24 tới 64 điểm ảnh)", strings.Join(avatarSizes, ", "))
			}
		case a.Name == "max":
			max, err := strconv.Atoi(strings.TrimSpace(a.Value))
			if err != nil || max < 1 {
				return at(n, a, "max của avatars là số avatar hiện ra, từ 1; còn lại gộp thành ô +N")
			}
			scope.max = max
		case base == "gap":
			return at(n, a, "gap trên avatars: các avatar chồng mép theo cỡ của nhóm; độ chồng riêng chưa có")
		default:
			outer.Attributes = append(outer.Attributes, *a)
		}
	}
	names, total, err := r.avatarNames(n)
	if err != nil {
		return err
	}
	shown := total
	if scope.max > 0 && shown > scope.max {
		shown = scope.max
	}
	if len(names) > shown {
		names = names[:shown]
	}
	var named []string
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			named = append(named, strings.TrimSpace(name))
		}
	}
	label := strings.Join(named, ", ")
	if more := total - shown; more > 0 {
		and, others := " và ", " người khác"
		if r.english() {
			and, others = " and ", " more"
		}
		if label == "" {
			label = strconv.Itoa(more) + strings.TrimPrefix(others, " ")
		} else {
			label += and + strconv.Itoa(more) + others
		}
	}
	open, err := r.opening(&outer, kindInline)
	if err != nil {
		return err
	}
	open.classes = append([]string{"avatars"}, open.classes...)
	open.attributes = append(open.attributes, `role="group"`)
	if label != "" {
		open.attributes = append(open.attributes, `aria-label="`+html.EscapeString(label)+`"`)
	}
	r.write("span", open)
	outerScope := r.avatars
	r.avatars = scope
	r.enter(n)
	err = r.children(n.Children, true)
	r.leave()
	r.avatars = outerScope
	if err != nil {
		return err
	}
	if more := total - shown; more > 0 {
		r.out.WriteString(`<span class="avatar avatar-` + scope.size + `" aria-hidden="true"><span class="avatar-face">+` + strconv.Itoa(more) + `</span></span>`)
	}
	r.out.WriteString("</span>")
	return nil
}

// avatarNames are the alt of each avatar a group would draw, in order, and how many it has: its children are
// avatars only, a list of the page's data read as the server holds it (a list the browser keeps is not yet).
func (r *renderer) avatarNames(n *Node) ([]string, int, error) {
	var names []string
	total := 0
	nameOf := func(avatar *Node) error {
		if a, ok := avatar.Attribute("if"); ok {
			shown, err := r.value(avatar, &a, a.Value)
			if err != nil {
				return err
			}
			if !shown.ToBoolean() {
				return nil
			}
		}
		total++
		for _, child := range avatar.Elements() {
			if child.Tag != "image" {
				continue
			}
			if alt, ok := child.Attribute("alt"); ok {
				name, err := r.interpolate(child, &alt, alt.Value)
				if err != nil {
					return err
				}
				names = append(names, name)
				return nil
			}
		}
		names = append(names, "")
		return nil
	}
	for _, child := range n.Children {
		if child.IsText() {
			if strings.TrimSpace(child.Text) != "" {
				return nil, 0, at(child, nil, "chữ không đặt thẳng trong avatars; con của avatars là các avatar")
			}
			continue
		}
		if child.Tag != "avatar" {
			return nil, 0, at(child, nil, "<%s> trong avatars: con của avatars là các avatar", child.Tag)
		}
		a, ok := child.Attribute("for")
		if !ok {
			if err := nameOf(child); err != nil {
				return nil, 0, err
			}
			continue
		}
		source := strings.TrimSpace(a.Value)
		if bare, ok := wrapped(source); ok {
			source = bare
		}
		head, _, _ := strings.Cut(source, ";")
		names2, list, ok := strings.Cut(head, " of ")
		if !ok {
			continue // the for's own error, where it is drawn
		}
		if live, err := r.liveList(child, &a, list); err != nil {
			return nil, 0, err
		} else if live {
			return nil, 0, at(child, &a, "avatars trên một danh sách trình duyệt giữ (đổi khi đang xem): ô +N tính lại ở trình duyệt chưa có")
		}
		items, err := r.value(child, &a, list)
		if err != nil {
			return nil, 0, err
		}
		item, index, _ := strings.Cut(names2, ",")
		item, index = strings.TrimSpace(item), strings.TrimSpace(index)
		for i, each := range items.Items() {
			takeItem := r.eval.put(item, each)
			takeIndex := func() {}
			if index != "" {
				takeIndex = r.eval.put(index, value.Number(float64(i)))
			}
			err := nameOf(child)
			takeIndex()
			takeItem()
			if err != nil {
				return nil, 0, err
			}
		}
	}
	return names, total, nil
}
