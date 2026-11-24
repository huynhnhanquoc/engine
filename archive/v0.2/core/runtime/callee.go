package runtime

import (
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// Calling a value that is not a function is a runtime error (14/10/2026), JavaScript's "x is not a
// function". The message says what was called as the source wrote it (f, o.x, o["x"]) and what kind of
// value it is, never the value: "o.x không phải hàm (là số): không gọi được". Where the call starts is
// the diagnostic's place (its at, "router.kitwork.js:12:5"), which a printed diagnostic leads with.

const (
	// unknownCallee stands for a part of a call the bytecode cannot name: a value a branch chose
	// ((c ? f : g)()), or one an operator made ((a || b).x()).
	unknownCallee = "(…)"
	// calledValue is the callee of a CALL whose name is not known at all.
	calledValue = "giá trị được gọi"

	// calleeSearchLimit bounds how many instructions one name lookup may visit.
	calleeSearchLimit = 4096
	// calleeNestingLimit bounds how deep a name is read back (o.a.b(…).c is four).
	calleeNestingLimit = 8
	// calleeTextLimit bounds a written name, in characters.
	calleeTextLimit = 80
)

// calledNotAFunction is the error of the CALL at ip, whose callee is the value called.
func (vm *VM) calledNotAFunction(ip int, called value.Value) value.Value {
	name, root := calledValue, -1
	if sites := vm.program.callSites(); sites != nil {
		if text, at := sites.callee(ip); text != unknownCallee {
			name, root = text, at
		}
	}
	return vm.notAFunction(ip, root, notAFunctionMessage(shortenCallee(name), called))
}

// callbackNotAFunction is the error of the INVOKE at ip of an array method (map, filter, forEach…)
// whose callback, its first argument, is not a function: "items.forEach(…): đối số 1 không phải hàm
// (là số): không gọi được". JavaScript's "3 is not a function"; the method itself is there.
func (vm *VM) callbackNotAFunction(ip int, method string, count int, argument value.Value) value.Value {
	receiver, root := unknownCallee, -1
	if sites := vm.program.callSites(); sites != nil {
		receiver, root = sites.receiver(ip)
	}
	call := shortenCallee(receiver + "." + method + arguments(count))
	return vm.notAFunction(ip, root, call+": "+notAFunctionMessage("đối số 1", argument))
}

// notAFunctionMessage says what was called and what it is: `o.x không phải hàm (là number): không gọi
// được`. Neither a string's text nor a number is ever shown (14/10/2026): what a program calls by
// mistake can be a secret (env.API_KEY(), env.PIN()). A boolean shows its value.
func notAFunctionMessage(callee string, called value.Value) string {
	kind := called.Kind()
	if called.IsBoolean() {
		kind += " " + called.ToString().Text()
	}
	return callee + " không phải hàm (là " + kind + "): không gọi được"
}

// notAFunction makes the failure a runtime error like any other of the VM: inside safe(() => …) it is
// the error (its message the message led by where the call starts, its code TYPE, JavaScript's
// TypeError); anywhere else it ends the run. The call starts at root, the first instruction of what was
// called, when the bytecode tells; otherwise at the call itself.
func (vm *VM) notAFunction(ip, root int, message string) value.Value {
	at := ip
	if root >= 0 {
		at = root
	}
	diagnostic := vm.buildDiagnostic(DiagnosticRuntimeError, message, at)
	diagnostic.CauseCode = value.TypeCode
	return diagnosticResult(diagnostic)
}

// callSites answers, after the fact, what a call was written as: the variable or member that put the
// called value on the stack and the instruction where that expression starts. The VM keeps no names at
// run time, so this is read back from the verified bytecode, and only when a call fails: the hot path
// never pays for it. A Program builds it once, on its first such failure.
type callSites struct {
	once         sync.Once
	ok           bool
	instructions []decodedInstruction
	byIP         ipTable
	places       stackPlaces   // stack depth before each reachable instruction, in its own frame, and its function
	preds        map[int][]int // by instruction index: the instructions control can come from
	constants    []value.Value
	names        *variableNames // the variables slot instructions reach (bytecode 7)
}

// callSites is the Program's call-site table, built on first use; nil when it cannot be built.
func (p *Program) callSites() *callSites {
	if p == nil || p.calls == nil {
		return nil
	}
	p.calls.build(p)
	if !p.calls.ok {
		return nil
	}
	return p.calls
}

func (s *callSites) build(program *Program) {
	s.once.Do(func() {
		code, constants := program.code, program.constants
		instructions, byIP, err := decodeProgram(code)
		if err != nil {
			return
		}
		entries, slots, lambdas := programEntries(constants, program.rootSlots())
		_, places, err := analyzeStack(entries, slots, instructions, byIP, len(code))
		if err != nil {
			return
		}
		preds := make(map[int][]int, len(instructions))
		link := func(from, toIP int) {
			if to, ok := byIP.at(toIP); ok {
				preds[to] = append(preds[to], from)
			}
		}
		for index, ins := range instructions {
			if _, reachable := places.at(ins.ip); !reachable {
				continue
			}
			switch ins.op {
			case RETURN, HALT:
			case JUMP:
				link(index, int(ins.operands[0]))
			case TRUE, FALSE, ITER:
				link(index, ins.next)
				link(index, int(ins.operands[0]))
			default:
				link(index, ins.next)
			}
		}
		s.instructions = instructions
		s.byIP = byIP
		s.places = places
		s.preds = preds
		s.constants = constants
		s.names = newVariableNames(program.locals, lambdas, instructions, places, constants)
		s.ok = true
	})
}

// callee names what the CALL at ip calls, and the instruction where it starts (-1 when unknown).
func (s *callSites) callee(ip int) (string, int) {
	index, depth, count, ok := s.call(ip, CALL)
	if !ok {
		return unknownCallee, -1
	}
	budget := calleeSearchLimit
	return s.describe(index, depth-count-1, true, calleeNestingLimit, &budget)
}

// receiver names the object the INVOKE at ip calls a method on, and the instruction where it starts.
func (s *callSites) receiver(ip int) (string, int) {
	index, depth, count, ok := s.call(ip, INVOKE)
	if !ok {
		return unknownCallee, -1
	}
	budget := calleeSearchLimit
	return s.describe(index, depth-count-2, false, calleeNestingLimit, &budget)
}

func (s *callSites) call(ip int, op Opcode) (index, depth, count int, ok bool) {
	index, ok = s.byIP.at(ip)
	if !ok {
		return 0, 0, 0, false
	}
	ins := s.instructions[index]
	place, ok := s.places.at(ip)
	if !ok || ins.op != op {
		return 0, 0, 0, false
	}
	return index, int(place.depth), int(ins.operands[0]), true
}

// producer finds the instruction that left the value in stack slot `slot` (counted in the frame) as
// control reaches the instruction at index `at`. Every path to `at` must agree on it: a value a branch
// chose (c ? f : g) has no single producer.
func (s *callSites) producer(at, slot int, budget *int) (int, bool) {
	found := -1
	visited := make(map[int]bool)
	queue := append([]int(nil), s.preds[at]...)
	for len(queue) > 0 {
		index := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if visited[index] {
			continue
		}
		visited[index] = true
		*budget--
		if *budget < 0 {
			return -1, false
		}
		ins := s.instructions[index]
		if ins.op == ITER {
			return -1, false
		}
		place, _ := s.places.at(ins.ip)
		depth := int(place.depth)
		stackIn, stackOut := ins.spec.StackEffect(ins.operands)
		base := depth - stackIn
		switch {
		case ins.op == DUP && slot == base:
			// The lower of DUP's two copies is the value it was given, in the same slot: look past it.
			// A ?. tests its object with a DUP on each of two paths that meet again (o?.x()); both lead
			// back to the one instruction that made o.
			queue = append(queue, s.preds[index]...)
		case slot >= base && slot < base+stackOut:
			if found >= 0 && found != index {
				return -1, false
			}
			found = index
		case slot >= base:
			// It took the slot and left nothing there: the value seen at `at` did not come this way.
			return -1, false
		default:
			queue = append(queue, s.preds[index]...)
		}
	}
	return found, found >= 0
}

// describe writes the expression that left stack slot `slot` at instruction index `at`, and the
// instruction where it starts (-1 when unknown). A member read is written o["x"] when it is the called
// value itself (a call after a dot is an INVOKE, so a read then a call came from brackets) and o.x
// inside a chain.
func (s *callSites) describe(at, slot int, called bool, nesting int, budget *int) (string, int) {
	if nesting <= 0 || slot < 0 {
		return unknownCallee, -1
	}
	index, ok := s.producer(at, slot, budget)
	if !ok {
		return unknownCallee, -1
	}
	ins := s.instructions[index]
	place, _ := s.places.at(ins.ip)
	depth := int(place.depth)
	switch ins.op {
	case LOAD, LOAD_SLOT, LOAD_OUTER:
		// a slot is the variable the source wrote (14/10/2026): error@3 and error#7 are error
		if name := s.variable(ins); SlotName(name) != "" {
			return SlotName(name), ins.ip
		}
	case BUILTIN:
		// kitwork, or the builtin the compiler bound the name to: Math in Math()
		return builtinName(int(ins.operands[0])), ins.ip
	case PUSH:
		constant := s.constant(ins)
		if constant.Lambda() == nil {
			return literalText(constant), ins.ip
		}
	case MAKE:
		if ins.operands[0] == 0 {
			return "{…}", ins.ip
		}
		return "[…]", ins.ip
	case DUP, STORE, STORE_SLOT, STORE_OUTER:
		// each passes on the value it was given
		return s.describe(index, depth-1, called, nesting-1, budget)
	case GET:
		if variable, root, ok := s.iterationVariable(index, depth, budget); ok {
			return variable, root
		}
		target, root := s.describe(index, depth-2, false, nesting-1, budget)
		key, kind := s.key(index, depth-1, budget)
		switch {
		case kind == stringKey && !called && isPlainName(key):
			return target + "." + key, root
		case kind == stringKey:
			return target + "[" + strconv.Quote(key) + "]", root
		default:
			return target + "[" + key + "]", root
		}
	case INVOKE:
		count := int(ins.operands[0])
		target, root := s.describe(index, depth-count-2, false, nesting-1, budget)
		if method, kind := s.key(index, depth-1, budget); kind == stringKey {
			return target + "." + method + arguments(count), root
		}
	case CALL:
		count := int(ins.operands[0])
		callee, root := s.describe(index, depth-count-1, true, nesting-1, budget)
		if callee == unknownCallee {
			return unknownCallee, -1
		}
		return callee + arguments(count), root
	}
	return unknownCallee, -1
}

// iterationVariable reads back a variable of a loop's iteration that a closure reaches: it lives in the
// object the iteration made, so reading it is LOAD_SLOT #N; PUSH "x#5"; GET (LOAD_OUTER from the
// closure), which the source wrote x.
func (s *callSites) iterationVariable(get, depth int, budget *int) (string, int, bool) {
	index, ok := s.producer(get, depth-2, budget)
	if !ok {
		return "", -1, false
	}
	load := s.instructions[index]
	if !isIterationSlot(s.variable(load)) {
		return "", -1, false
	}
	key, kind := s.key(get, depth-1, budget)
	if kind != stringKey || SlotName(key) == "" {
		return "", -1, false
	}
	return SlotName(key), load.ip, true
}

// iterationWrite names the variable of a loop's iteration that the SET at ip writes, when it writes one:
// a closure writes such a variable into the object the iteration made, under its slot (LOAD_OUTER #N;
// PUSH "x#5"; the value; SET), which the source wrote x = ….
func (s *callSites) iterationWrite(ip int) (string, bool) {
	index, depth, ok := s.instruction(ip, SET)
	if !ok {
		return "", false
	}
	budget := calleeSearchLimit
	target, ok := s.producer(index, depth-3, &budget)
	if !ok || !isIterationSlot(s.variable(s.instructions[target])) {
		return "", false
	}
	key, kind := s.key(index, depth-2, &budget)
	if kind != stringKey || SlotName(key) == "" {
		return "", false
	}
	return SlotName(key), true
}

// instruction is the index of the instruction at ip, which is op, and the stack depth before it in its
// frame; ok is false for an ip of any other instruction.
func (s *callSites) instruction(ip int, op Opcode) (index, depth int, ok bool) {
	index, ok = s.byIP.at(ip)
	if !ok || s.instructions[index].op != op {
		return 0, 0, false
	}
	place, ok := s.places.at(ip)
	return index, int(place.depth), ok
}

// stringKey is the kind key gives a written string, the word value.Kind() says for it.
const stringKey = "string"

// key reads the key of a member read: a written string or number, or the name of a variable. Its kind
// is the word value.Kind() says for the written constant ("string", "number"); "" for a variable's name
// and for a key it cannot read.
func (s *callSites) key(at, slot int, budget *int) (string, string) {
	index, ok := s.producer(at, slot, budget)
	if !ok {
		return "…", ""
	}
	ins := s.instructions[index]
	switch ins.op {
	case PUSH:
		constant := s.constant(ins)
		switch {
		case constant.IsString():
			return constant.Text(), constant.Kind()
		case constant.IsNumber():
			return constant.ToString().Text(), constant.Kind()
		}
	case LOAD, LOAD_SLOT, LOAD_OUTER:
		if name := s.variable(ins); name != "" {
			return SlotName(name), ""
		}
	}
	return "…", ""
}

// variable is the slot name of the variable a LOAD reads by name, or a slot instruction reaches; ""
// for any other instruction.
func (s *callSites) variable(ins decodedInstruction) string {
	switch ins.op {
	case LOAD:
		if name := s.constant(ins); name.IsString() {
			return name.Text()
		}
	case LOAD_SLOT, LOAD_OUTER, STORE_SLOT, STORE_OUTER:
		place, _ := s.places.at(ins.ip)
		return s.names.name(ins, place)
	}
	return ""
}

func (s *callSites) constant(ins decodedInstruction) value.Value {
	index := int(ins.operands[0])
	if index < 0 || index >= len(s.constants) {
		return value.Undefined()
	}
	return s.constants[index]
}

func arguments(count int) string {
	if count == 0 {
		return "()"
	}
	return "(…)"
}

// literalText writes a constant as the source wrote it.
func literalText(constant value.Value) string {
	if constant.IsString() {
		return strconv.Quote(constant.Text())
	}
	return constant.ToString().Text()
}

func isPlainName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		switch {
		case char == '_' || char == '$':
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9' && index > 0:
		default:
			return false
		}
	}
	return true
}

// shortenCallee keeps a written name to one readable line.
func shortenCallee(name string) string {
	if utf8.RuneCountInString(name) <= calleeTextLimit {
		return name
	}
	runes := []rune(name)
	return string(runes[:calleeTextLimit-1]) + "…"
}
