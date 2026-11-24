package compiler

import (
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// The code of block scope (14/10/2026): how the compiler reads and writes a variable once scope.go has
// given it its slot name, and slots.go its slot (bytecode 7).

// variable is what a name resolved to; nil for a name nothing declares (and before resolution).
func (c *Compiler) variable(name *Identifier) *binding {
	if c.scopes == nil || name == nil {
		return nil
	}
	return c.scopes.slots[name]
}

// slotOf is the name LOAD and STORE use for a variable kept by name.
func (c *Compiler) slotOf(name *Identifier) string {
	if found := c.variable(name); found != nil && found.slot != "" {
		return found.slot
	}
	return name.Value
}

// isTemporary reports a name only the compiler writes (#N: an object or a key evaluated once): a source
// never spells #.
func isTemporary(name string) bool {
	return len(name) > 0 && name[0] == runtime.SlotBlockMark
}

func (c *Compiler) emitName(op runtime.Opcode, name string) {
	index := c.addString(name)
	c.emitIndex(op, index)
}

// loadName reads a variable: from its slot, from its iteration's object, a builtin from its slot (or as
// the number it is), or by name.
//
//	LOAD_SLOT i  or  LOAD_OUTER d i  or  LOAD_SLOT #N; PUSH "x#5"; GET  or  BUILTIN n  or  LOAD name
func (c *Compiler) loadName(name *Identifier) error {
	found := c.variable(name)
	switch {
	case found != nil && found.environment:
		if err := c.emitReached(true, found.loop.environment); err != nil {
			return err
		}
		c.emitName(runtime.PUSH, found.slot)
		c.emit(runtime.GET)
		return nil
	case found != nil && found.level != nil:
		return c.emitSlot(true, found.level, found.index)
	case found == nil && isTemporary(name.Value):
		return c.emitSlot(true, c.rootLevel(), c.temporary(name.Value))
	case found == nil && c.loadBuiltin(name):
		return nil
	}
	c.emitNamed(runtime.LOAD, c.slotOf(name))
	return nil
}

// storeValue writes the value on top of the stack to a variable that is not in an iteration's object and
// leaves it there: its slot, a temporary's, or by name.
func (c *Compiler) storeValue(name *Identifier, found *binding) error {
	switch {
	case found != nil && found.level != nil:
		return c.emitSlot(false, found.level, found.index)
	case found == nil && isTemporary(name.Value):
		return c.emitSlot(false, c.rootLevel(), c.temporary(name.Value))
	}
	c.emitNamed(runtime.STORE, c.slotOf(name))
	return nil
}

// storeName writes the value on top of the stack to a variable and leaves it there, as STORE does. A
// variable of an iteration's object is stored in a temporary of the function running the code first,
// then copied into the object:
//
//	STORE_SLOT i                    or   STORE_SLOT temp; LOAD_SLOT #N; PUSH "x#5"; LOAD_SLOT temp; SET; POP
func (c *Compiler) storeName(name *Identifier) error {
	found := c.variable(name)
	if found == nil || !found.environment {
		return c.storeValue(name, found)
	}
	temp := found.slot
	if foreign, ok := c.scopes.temps[name]; ok {
		temp = foreign
	}
	index := c.temporary(temp)
	if err := c.emitSlot(false, c.level, index); err != nil {
		return err
	}
	if err := c.emitReached(true, found.loop.environment); err != nil {
		return err
	}
	c.emitName(runtime.PUSH, found.slot)
	if err := c.emitSlot(true, c.level, index); err != nil {
		return err
	}
	c.emit(runtime.SET)
	c.emit(runtime.POP)
	return nil
}

// assignName compiles name = value, leaving the value as the expression's result (SET leaves the value
// it wrote, bytecode 5).
//
//	value; STORE_SLOT i             or   LOAD_SLOT #N; PUSH "x#5"; value; SET
func (c *Compiler) assignName(name *Identifier, assigned Expression) error {
	found := c.variable(name)
	if found == nil || !found.environment {
		if err := c.Compile(assigned); err != nil {
			return err
		}
		return c.storeValue(name, found)
	}
	if err := c.emitReached(true, found.loop.environment); err != nil {
		return err
	}
	c.emitName(runtime.PUSH, found.slot)
	if err := c.Compile(assigned); err != nil {
		return err
	}
	c.emit(runtime.SET)
	return nil
}

// loopEnvironment is the slot of the object a for statement makes each iteration; "" when none.
func (c *Compiler) loopEnvironment(node Statement) string {
	if c.scopes == nil {
		return ""
	}
	if loop := c.scopes.loops[node]; loop != nil {
		return loop.environment
	}
	return ""
}

// newIteration makes the object of an iteration: MAKE 0; STORE_SLOT #N; POP.
func (c *Compiler) newIteration(environment string) error {
	c.emit(runtime.MAKE, 0)
	if err := c.emitReached(false, environment); err != nil {
		return err
	}
	c.emit(runtime.POP)
	return nil
}

// nextIteration makes the next iteration's object with this iteration's counter in it, before the
// update changes it, as JavaScript copies a for's let into each iteration: a closure of this iteration
// keeps the value it saw.
//
//	MAKE 0; DUP; PUSH "i#3"; LOAD_SLOT #N; PUSH "i#3"; GET; SET; POP; STORE_SLOT #N; POP
func (c *Compiler) nextIteration(environment, counter string) error {
	c.emit(runtime.MAKE, 0)
	c.emit(runtime.DUP)
	c.emitName(runtime.PUSH, counter)
	if err := c.emitReached(true, environment); err != nil {
		return err
	}
	c.emitName(runtime.PUSH, counter)
	c.emit(runtime.GET)
	c.emit(runtime.SET)
	c.emit(runtime.POP)
	if err := c.emitReached(false, environment); err != nil {
		return err
	}
	c.emit(runtime.POP)
	return nil
}

// compileHandedClosure makes a closure that reaches variables of a loop's iteration inside a function,
// the maker, whose parameters are the objects of those iterations, called at once: the closure keeps
// the maker's frame, so it keeps the iteration it was made in. The maker is a frame of its own, whose
// slots are those objects (slots.go).
//
//	JUMP over; <the closure as usual>; RETURN; over: PUSH maker; LOAD_SLOT #N…; CALL n
func (c *Compiler) compileHandedClosure(literal *FunctionLiteral, environments []string) error {
	jumpOver := c.emitJump(runtime.JUMP)
	start := len(c.instructions)
	outer := c.rootLevel()
	maker := c.enterLevel(nil)
	for _, environment := range environments {
		maker.addNamed(environment)
	}
	err := c.compileFunctionLiteral(literal)
	c.level = outer
	if err != nil {
		return err
	}
	c.emit(runtime.RETURN)
	c.patchJump(jumpOver, len(c.instructions))

	name := literal.DebugName
	if name == "" {
		name = "<anonymous>"
	}
	location := c.getSourceLocation(literal.Token.Source, literal.Token.Position)
	template := &value.Lambda{
		Address:       start,
		Name:          name,
		SourceFile:    location.File,
		SourceLine:    location.Line,
		SourceColumn:  location.Column,
		Params:        append([]string(nil), environments...),
		Locals:        maker.locals,
		CapturesSlots: maker.capturesSlots,
		CapturesNames: maker.capturesNames,
	}
	c.at(literal.Token.Source, literal.Token.Position)
	index := c.addConstant(value.Closure(template))
	c.emitIndex(runtime.PUSH, index)
	for _, environment := range environments {
		if err := c.emitReached(true, environment); err != nil {
			return err
		}
	}
	c.emit(runtime.CALL, byte(len(environments)))
	return nil
}
