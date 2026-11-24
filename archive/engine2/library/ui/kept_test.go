package ui

import (
	"strings"
	"testing"
)

// shown is a tree as a line: each node's tag, attributes and text, its children inside brackets.
func shown(n *Node) string {
	var b strings.Builder
	var walk func(n *Node)
	walk = func(n *Node) {
		b.WriteString(n.Tag + "|" + n.Text)
		for _, a := range n.Attributes {
			b.WriteString(" " + a.Name + "=" + a.Value)
		}
		b.WriteString("[")
		for _, child := range n.Children {
			walk(child)
		}
		b.WriteString("]")
	}
	walk(n)
	return b.String()
}

// A clone is all its own (kept.go): the tree it copies and another clone stay as they are whatever is done to it, and
// an append to one of its slices makes its own array, never writing over the next node's attributes or children,
// which the three blocks of a clone hold side by side.
func TestACloneIsAllItsOwn(t *testing.T) {
	nodes, err := Parse("a.ui", []byte(`<box gap="4" padding="2"><text weight="bold">A</text><text color="ink">B</text></box>`))
	if err != nil {
		t.Fatal(err)
	}
	original := nodes[0]
	before := shown(original)
	one, two := original.clone(), original.clone()
	if shown(one) != before || shown(two) != before || one == original || one.Children[0] == original.Children[0] {
		t.Fatalf("a clone is %s, want a copy of %s", shown(one), before)
	}

	one.Attributes = append(one.Attributes, Attribute{Name: "show", Value: "false"})
	if got := one.Children[0].Attributes[0]; got.Name != "weight" {
		t.Errorf("an attribute appended to the root wrote over its child's: %s=%s", got.Name, got.Value)
	}
	one.Children[0].Children = append(one.Children[0].Children, &Node{Text: "C"})
	if got := one.Children[1].Children[0].Text; got != "B" {
		t.Errorf("a child appended to the first text wrote over the second's: %q", got)
	}

	one.Tag, one.Children[0].Attributes[0].Value, one.Children[1].Children[0].Text = "changed", "changed", "changed"
	if shown(original) != before || shown(two) != before {
		t.Errorf("a change to a clone reached the tree or another clone:\n%s\n%s\nwant %s", shown(original), shown(two), before)
	}
	if (*Node)(nil).clone() != nil {
		t.Errorf("the clone of no tree is a tree")
	}
}
