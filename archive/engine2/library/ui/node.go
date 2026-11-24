// Package ui is the .ui file of Kitwork (docs/KITWORK_UI.md): it reads a page, its frame and the layouts it
// calls, and renders them. A .ui file says what an interface is (its parts, their order, their meaning, the
// data and the behaviour), and the engine decides how it is drawn; HTML is the first target. It has no name
// to import: it keeps one kind of file (docs/KITWORK_ENGINE.md mục 3).
package ui

import "strings"

// Node is one piece of a .ui file as written: an element, or a run of text (Tag "").
type Node struct {
	// Tag is the element's name in lower case; "" for text.
	Tag string
	// Written is the tag as the file wrote it when that is not all lower case (<Box>): tag names do not
	// tell case apart, so it reads as its lower case and the compiler can name the habit.
	Written    string
	Attributes []Attribute
	Children   []*Node
	// Text is a run of text, {{ … }} kept whole inside it.
	Text string
	// Closed is an element written self-closing (<icon />) or one that never holds anything (<input>).
	Closed bool
	File   string
	Line   int
	Column int
	// source is the node of the tree its file parsed into that this one is a copy of (kept.go), the same for every
	// page of the boot; nil for a node no file holds as it is (one the engine made, a copy changed after it was made)
	source *Node
	// memo is what the site last kept of a source node, the node's own (frozen.go): read through source, a copy's
	// is its source's
	memo *nodeMemo
}

// IsText reports whether the node is a run of text.
func (n *Node) IsText() bool { return n.Tag == "" }

// Attribute is the first attribute of this exact name, and whether there is one.
func (n *Node) Attribute(name string) (Attribute, bool) {
	for _, attribute := range n.Attributes {
		if attribute.Name == name {
			return attribute, true
		}
	}
	return Attribute{}, false
}

// Elements are the element children, in order.
func (n *Node) Elements() []*Node {
	out := make([]*Node, 0, len(n.Children))
	for _, child := range n.Children {
		if !child.IsText() {
			out = append(out, child)
		}
	}
	return out
}

// Significant are the children without the runs of text that are only white space.
func (n *Node) Significant() []*Node {
	out := make([]*Node, 0, len(n.Children))
	for _, child := range n.Children {
		if child.IsText() && strings.TrimSpace(child.Text) == "" {
			continue
		}
		out = append(out, child)
	}
	return out
}
