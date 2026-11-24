package compiler

import (
	"fmt"
	"strings"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// Builtins bound when the program compiles (04/11/2026). A name nothing declares where it is read, and
// that neither the function reading it nor any function around it assigns by name (var Math = …), is a
// builtin's when core/builtins has one of that name: the compiler reads it from the builtin's slot
// (BUILTIN n, which the VM fills from the frozen shared set), never from the globals by name, which took
// three map lookups at the top level and every map of the closures around inside a function. The
// builtins are frozen all the way down, so what a program reads of one is known here:
//
//   - a number or a string a builtin holds (Math.PI, Number.MAX_SAFE_INTEGER, and Infinity and NaN
//     themselves) is a constant;
//   - a call of a member a builtin does not have (Math.sqr(2)) is a compile error naming it, where
//     JavaScript throws when the call runs (and so did Kitwork, for a member JavaScript has and Kitwork
//     does not: Object.seal, JSON.rawJSON); a read of one (Math.sqr) stays undefined, as in JavaScript.
//
// A variable or a parameter of that name turns all of this off for it where it is in scope, and an
// assignment of the name by a function turns it off in that function and every function inside it: a
// read there may see the assignment (the VM looks the name up along the maps of the functions around),
// so it stays a read by name, as every read of a builtin was.

// builtinSlots are the shared builtins by their slot, as the compiler reads them.
var builtinSlots = builtins.Slots()

// builtinSlot is the slot a name reads when it is a builtin's: nothing declares it where it is read, and
// neither the function it is read in nor any function around that one assigns it by name. A name the
// resolver did not record as such a read is read by name.
func (c *Compiler) builtinSlot(name *Identifier) (int, bool) {
	if c.scopes == nil {
		return 0, false
	}
	function, read := c.scopes.builtinReads[name]
	if !read {
		return 0, false
	}
	for ; function != nil; function = function.parent {
		if function.written[name.Value] {
			return 0, false
		}
	}
	return builtins.Slot(name.Value)
}

// knownBuiltin is the value an expression reads when it is a builtin or a member a builtin holds as its
// own (Number.parseInt, Math.max.length): what it is whatever runs, since every builtin is frozen.
func (c *Compiler) knownBuiltin(expression Expression) (value.Value, bool) {
	switch node := expression.(type) {
	case *Identifier:
		if slot, ok := c.builtinSlot(node); ok {
			return builtinSlots[slot], true
		}
	case *MemberExpression:
		if node.Optional {
			return value.Value{}, false
		}
		if object, ok := c.knownBuiltin(node.Object); ok && object.Has(node.Property.Value) {
			return object.Get(node.Property.Value), true
		}
	}
	return value.Value{}, false
}

// constantBuiltin is the number or the string an expression reads of a builtin, which it compiles to.
func (c *Compiler) constantBuiltin(expression Expression) (value.Value, bool) {
	known, ok := c.knownBuiltin(expression)
	if !ok || !(known.IsNumber() || known.IsString()) {
		return value.Value{}, false
	}
	return known, true
}

// loadBuiltin reads a name that is a builtin's: its constant, or its slot. false for any other name.
func (c *Compiler) loadBuiltin(name *Identifier) bool {
	slot, ok := c.builtinSlot(name)
	if !ok {
		return false
	}
	if known := builtinSlots[slot]; known.IsNumber() {
		c.emitIndex(runtime.PUSH, c.addConstant(known))
		return true
	}
	c.emit(runtime.BUILTIN, byte(slot))
	return true
}

// checkBuiltinCall refuses a call of a member a builtin does not have, which can never be a function:
// Math.sqr(2). A call through ?. of the member (Math.sqr?.()) calls nothing and stays.
func (c *Compiler) checkBuiltinCall(call *MethodCallExpression) error {
	if call.OptionalCall {
		return nil
	}
	object, ok := c.knownBuiltin(call.Object)
	if !ok {
		return nil
	}
	// a member it does not have reads undefined, or the TYPE error of a name only JavaScript's prototype holds
	// (Number.isInteger.call, 19/11/2026)
	if member := object.Get(call.Method.Value); !member.IsUndefined() && !member.IsInvalid() {
		return nil
	}
	receiver, method := call.Object.String(), call.Method.Value
	message := fmt.Sprintf("%s không có %s, nên %s.%s(…) không gọi được", receiver, method, receiver, method)
	if near := nearestMember(object, method); near != "" {
		message += ": có phải " + receiver + "." + near + "?"
	}
	where := c.getSourceLocation(call.Method.Token.Source, call.Method.Token.Position)
	return fmt.Errorf("%s: %s", locationText(where), message)
}

// nearestMember is the member of object whose name is closest to name, at most two edits away (case
// counts as half an edit); "" when none is.
func nearestMember(object value.Value, name string) string {
	// a new list: Keys is the frozen builtin's own, shared by every compile and run, which an append past
	// its length would write into
	keys, methods := object.Keys(), object.MethodNames()
	members := make([]string, 0, len(keys)+len(methods))
	members = append(append(members, keys...), methods...)
	best, bestDistance := "", 5 // in half edits
	for _, member := range members {
		if distance := editDistance(strings.ToLower(member), strings.ToLower(name))*2 + caseDifference(member, name); distance < bestDistance {
			best, bestDistance = member, distance
		}
	}
	return best
}

// caseDifference is 1 when two names differ only by case somewhere, else 0.
func caseDifference(a, b string) int {
	if a != b && strings.EqualFold(a, b) {
		return 1
	}
	return 0
}

// editDistance is the Levenshtein distance of two short names, by byte.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
