package ui

import "strings"

// prefix is the "when" of a presentation attribute's name (KITWORK_UI mục 8): ios,android+large+hover:gap
// reads as a target, a width and a state. A target the page is not built for drops the attribute; widths and
// states go to the stylesheet, which names the class after them.
type prefix struct {
	keep   bool
	widths []string
	states []string
}

var (
	targetWords = map[string]bool{
		"website": true, "desktop": true, "mobile": true, "ios": true, "android": true, "windows": true, "macos": true, "linux": true,
	}
	// targetFamily is the surface a platform belongs to: ios: is mobile too.
	targetFamily = map[string]string{"ios": "mobile", "android": "mobile", "windows": "desktop", "macos": "desktop", "linux": "desktop"}
	widthWords   = map[string]bool{"small": true, "medium": true, "large": true, "wide": true, "huge": true}
	stateWords   = map[string]bool{
		"opened": true, "active": true, "selected": true, "scrolled": true, "hover": true, "focus": true, "pressed": true, "disabled": true,
	}
)

// readPrefix reads chain, what stands before the name's last ':', for a page built for target. Words of one
// axis join with ',', axes with '+', in the order target, width, state, each axis once; anything else is the
// problem it gives back.
func readPrefix(chain, target string) (prefix, string) {
	p := prefix{keep: true}
	if chain == "" {
		return p, ""
	}
	order := map[string]int{"target": 0, "width": 1, "state": 2}
	seen := map[string]bool{}
	last := -1
	for _, group := range strings.Split(chain, "+") {
		words := strings.Split(group, ",")
		axis := ""
		for _, word := range words {
			kind := ""
			switch {
			case targetWords[word]:
				kind = "target"
			case widthWords[word]:
				kind = "width"
			case stateWords[word]:
				kind = "state"
			case word == "open":
				// the state's word until 26/10/2026: a verb is a function, a participle a state
				return p, "tiền tố open: nay là opened: (trạng thái là phân từ, như opened của dropdown)"
			default:
				return p, "tiền tố không tồn tại: " + word + ":"
			}
			if axis == "" {
				axis = kind
			} else if axis != kind {
				return p, "dấu , chỉ nối tiền tố cùng một trục; khác trục thì nối bằng + (" + group + ")"
			}
		}
		if seen[axis] {
			return p, "một trục chỉ viết một lần; " + group + " lặp lại trục đã có"
		}
		seen[axis] = true
		if order[axis] < last {
			return p, "thứ tự tiền tố là đích → độ rộng → trạng thái (" + chain + ")"
		}
		last = order[axis]
		switch axis {
		case "target":
			match := false
			for _, word := range words {
				match = match || word == target || targetFamily[target] == word
			}
			p.keep = match
		case "width":
			p.widths = append(p.widths, words...)
		case "state":
			p.states = append(p.states, words...)
		}
	}
	return p, ""
}

// splitPrefix is a name's prefix chain and its own name: medium+hover:background is "medium+hover" and
// "background". An event's modifiers (keydown:escape) read the other way and are the caller's to tell.
func splitPrefix(name string) (string, string) {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "", name
}
