package compiler

import (
	"fmt"

	"github.com/kitwork/engine2/core/runtime"
)

// Slots (bytecode 7, 04/11/2026). The VM keeps a function's own variables in its frame's slots, read
// and written by an index the compiler gives each (LOAD_SLOT, STORE_SLOT), never by name. A closure
// reaches a variable of a function around through the closures that made it (LOAD_OUTER, STORE_OUTER:
// how many functions out, then the index); the one directly inside the variable's function holds that
// function's frame's slots by reference, so JavaScript's capture by reference holds, and the frame's next
// function gets new slots. The top level's block variables live in its frame's slots too; its own
// variables (const, let and function of its body, its imports) and every name nothing declares (var x =
// …, a global of the host) keep LOAD and STORE by name, in the maps hosts read (vm.Vars).
//
// A variable of a loop's iteration that a closure reaches stays in its iteration's object (scope.go);
// the object lives in a slot of the loop's function, and the closure is made inside a small function,
// the maker, whose parameters are those objects (compileHandedClosure), so the maker is a level of its
// own between the loop's function and the closure.

// slotLevel is one frame as the VM runs it: the program's top level, a function literal, or a maker.
// It gives every variable the frame keeps an index among its slots, and records what a closure made of
// its function must keep of the frame it is made in.
type slotLevel struct {
	parent *slotLevel
	// depth counts the levels around it: 0 for the top level.
	depth int
	// function is the scope the resolver made of the level's function; nil for a maker.
	function *functionScope
	// locals names the slots by index (the template's Locals, the Program's for the top level); indexes
	// gives the slot of a name looked up by name, an iteration's object or a temporary (a variable is
	// found by its binding), made when the first is added.
	locals  []string
	indexes map[string]int
	// capturesSlots: code in the function, or in one inside it, reaches a variable of the function
	// around (the parent level), so a closure of it holds that frame's slots. capturesNames: such code
	// reads or writes a variable by name, so a closure of it holds that frame's map.
	capturesSlots bool
	capturesNames bool
}

// add gives name a new slot and returns its index.
func (level *slotLevel) add(name string) int {
	level.locals = append(level.locals, name)
	return len(level.locals) - 1
}

// addNamed gives name a new slot that a lookup by name finds (lookup).
func (level *slotLevel) addNamed(name string) int {
	if level.indexes == nil {
		level.indexes = map[string]int{}
	}
	index := level.add(name)
	level.indexes[name] = index
	return index
}

// lookup is the slot a name was given by addNamed.
func (level *slotLevel) lookup(name string) (int, bool) {
	index, known := level.indexes[name]
	return index, known
}

// enterLevel opens the frame of a function (or of the top level, function being the program's scope;
// or of a maker, function nil), as the code of its body starts, and gives its variables their slots:
// the parameters first, in their order, which the VM binds there; then every other variable of the
// function, of its body and of its blocks, in the order the source declares them; then the objects its
// loops make for each iteration. The top level's own variables stay by name. The compiler's temporaries
// get theirs as they are first written (temporary).
func (c *Compiler) enterLevel(function *functionScope) *slotLevel {
	level := &slotLevel{parent: c.level, function: function}
	if c.level != nil {
		level.depth = c.level.depth + 1
	}
	if function != nil {
		if size := len(function.bindings) + len(function.loops); size > 0 {
			level.locals = make([]string, 0, size)
		}
		for _, declared := range function.bindings {
			declared.level, declared.index = nil, 0
			if declared.environment || (function.literal == nil && !declared.scope.block) {
				continue
			}
			declared.level = level
			declared.index = level.add(declared.slot)
		}
		for _, loop := range function.loops {
			level.addNamed(loop.environment)
		}
	}
	c.level = level
	return level
}

// temporary is the slot of one of the compiler's temporaries (#N: an object or key evaluated once, a
// value passed through on its way into an iteration's object) in the frame running, given on first use:
// it is written and read by the code of one function only.
func (c *Compiler) temporary(name string) int {
	level := c.rootLevel()
	if index, known := level.lookup(name); known {
		return index
	}
	return level.addNamed(name)
}

// rootLevel is the level code is written in, made for the top level when a compile did not start from a
// Program (and so never entered one).
func (c *Compiler) rootLevel() *slotLevel {
	if c.level == nil {
		c.enterLevel(nil)
	}
	return c.level
}

// emitSlot reads (load) or writes the slot index of level target from the code being written: LOAD_SLOT
// or STORE_SLOT in the frame running, else LOAD_OUTER or STORE_OUTER as many functions out as target
// is. The level directly inside target on the way then keeps target's slots (capturesSlots).
func (c *Compiler) emitSlot(load bool, target *slotLevel, index int) error {
	current := c.rootLevel()
	if target == current {
		op := runtime.STORE_SLOT
		if load {
			op = runtime.LOAD_SLOT
		}
		c.emitIndex(op, index)
		return nil
	}
	inner := current
	for inner != nil && inner.parent != target {
		inner = inner.parent
	}
	if inner == nil {
		return fmt.Errorf("compiler: a slot of a function that is not around the code reaching it")
	}
	inner.capturesSlots = true
	depth := current.depth - target.depth
	if depth > 255 {
		return fmt.Errorf("compiler: a variable %d functions out, more than the 255 an instruction reaches: nest fewer functions", depth)
	}
	op := runtime.STORE_OUTER
	if load {
		op = runtime.LOAD_OUTER
	}
	if index > runtime.MaxNarrowOperand {
		c.emit(op.Wide(), byte(depth), byte(index>>16), byte(index>>8), byte(index))
	} else {
		c.emit(op, byte(depth), byte(index>>8), byte(index))
	}
	return nil
}

// emitNamed reads (LOAD) or writes (STORE) a variable by name: the top level's own, or a name nothing
// declares. Every function from the code being written out to the top level then keeps the map of the
// frame it is made in (capturesNames), since the VM looks such a name up along the closures that made
// the function running.
func (c *Compiler) emitNamed(op runtime.Opcode, name string) {
	for level := c.rootLevel(); level != nil && !level.capturesNames; level = level.parent {
		level.capturesNames = true
	}
	c.emitName(op, name)
}

// emitReached reads (load) or writes the slot a name holds in the nearest level that has it: an
// iteration's object (#N), which a maker holds as a parameter and the loop's function in a slot of its
// own, so a closure reads the object of the iteration it was made in.
func (c *Compiler) emitReached(load bool, name string) error {
	for level := c.rootLevel(); level != nil; level = level.parent {
		if index, known := level.lookup(name); known {
			return c.emitSlot(load, level, index)
		}
	}
	return fmt.Errorf("compiler: the object of an iteration (%s) has no slot where it is reached", name)
}
