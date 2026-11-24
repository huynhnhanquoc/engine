package ui

// Attribute is one attribute of an element as written, in the order of the file.
type Attribute struct {
	// Name is the name as written, its prefixes and modifiers in it: medium:padding, input:debounce(300).
	Name  string
	Value string
	// Bare is an attribute written without a value (required). What that means is the tag's to say: a
	// true-or-false attribute of Kitwork reads as "true", an input at the call of a layout is an error.
	Bare   bool
	Line   int
	Column int
}
