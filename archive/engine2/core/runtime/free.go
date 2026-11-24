package runtime

import "github.com/kitwork/engine2/core/value"

// FreeValue is the value of name as fn reads it from outside itself: a variable of a function around it, or of the
// program's top level, which its closure holds by reference (LOAD_OUTER). A host that reads a lambda as data, not
// running it, needs the values of the names the lambda does not declare: kitwork/database's statements, whose
// lambda is translated to SQL and whose names from outside are the statement's values
// (db.list((users) => users.email === email), 06/11/2026). Frames are read innermost first; in one frame, the slot
// declared last of that name. A variable of a loop's iteration that the closure reaches is no slot of its own: it
// lives in the object the loop made for that iteration (#N, slot.go), which the maker the closure was made in holds
// as a parameter, so the object is read for it, the iteration the closure was made in (11/11/2026). false when no
// frame around fn has the name.
func FreeValue(fn *value.Lambda, name string) (value.Value, bool) {
	for owner := fn; owner != nil; owner = owner.Parent {
		if owner.Scope != nil {
			if item, ok := owner.Scope[name]; ok {
				return item, true
			}
		}
		// owner.Slots are the slots of the frame owner was made in: its parent's, or the program's top level
		var locals []string
		if owner.Parent != nil {
			locals = owner.Parent.Locals
		} else if program, ok := ProgramFromRef(owner.Program); ok {
			locals = program.locals
		}
		for index := len(locals) - 1; index >= 0; index-- {
			if index >= len(owner.Slots) {
				continue
			}
			if isIterationSlot(locals[index]) {
				if item, ok := iterationValue(owner.Slots[index], name); ok {
					return item, true
				}
				continue
			}
			if SlotName(locals[index]) == name {
				return owner.Slots[index], true
			}
		}
	}
	return value.Value{}, false
}

// iterationValue is name in the object of a loop's iteration, its variables under their slots (x#5): of two of that
// name in one iteration (one in a block of the body), the one declared last, as in a frame's slots, the object holding
// them in the order they are written; false when the iteration holds none.
func iterationValue(iteration value.Value, name string) (value.Value, bool) {
	if !iteration.IsObject() {
		return value.Value{}, false
	}
	keys := iteration.Keys()
	for i := len(keys) - 1; i >= 0; i-- {
		if SlotName(keys[i]) == name {
			return iteration.Get(keys[i]), true
		}
	}
	return value.Value{}, false
}
