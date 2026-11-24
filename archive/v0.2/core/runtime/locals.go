package runtime

import "github.com/kitwork/engine2/core/value"

// variableNames reads back, after the fact, the variable a slot instruction reaches (bytecode 7): the VM
// reads slots by index and keeps no name, so a failed call's message (callee.go) and a program's
// inspection name the variable from the slots' names the program carries, the top level's (Program
// locals) and each lambda template's (Locals). LOAD_OUTER's variable is a slot of a function around: the
// one whose code makes the closure, as many times out as its depth says. The hot path never builds this.
type variableNames struct {
	// locals are the slots' names of each entry point's frame, by its index among the entries.
	locals [][]string
	// parents gives, by entry index, the entry whose code makes that entry's closures (the PUSH of its
	// template); -1 for the top level and for a lambda nothing in the program makes.
	parents []int
}

// programEntries lists a program's entry points as the verifier walks them: the top level at address 0
// with rootSlots slots, then every lambda template of constants in their order, each with the slots its
// Locals name. lambdas[i] is entry i's template, nil for the top level.
func programEntries(constants []value.Value, rootSlots int) (entries []int, slots []int, lambdas []*value.Lambda) {
	entries = []int{0}
	slots = []int{rootSlots}
	lambdas = []*value.Lambda{nil}
	for _, constant := range constants {
		lambda := constant.Lambda()
		if lambda == nil {
			continue
		}
		entries = append(entries, lambda.Address)
		slots = append(slots, len(lambda.Locals))
		lambdas = append(lambdas, lambda)
	}
	return entries, slots, lambdas
}

// newVariableNames builds the names of a verified program's slots from what analyzeStack proved of its
// instructions (places) and the entries programEntries listed.
func newVariableNames(
	rootLocals []string,
	lambdas []*value.Lambda,
	instructions []decodedInstruction,
	places stackPlaces,
	constants []value.Value,
) *variableNames {
	names := &variableNames{
		locals:  make([][]string, len(lambdas)),
		parents: make([]int, len(lambdas)),
	}
	byTemplate := make(map[*value.Lambda]int, len(lambdas))
	for index, lambda := range lambdas {
		names.parents[index] = -1
		if lambda == nil {
			names.locals[index] = rootLocals
			continue
		}
		names.locals[index] = lambda.Locals
		byTemplate[lambda] = index
	}
	for _, ins := range instructions {
		if ins.op != PUSH {
			continue
		}
		place, reachable := places.at(ins.ip)
		if !reachable || int(ins.operands[0]) >= len(constants) {
			continue
		}
		template := constants[ins.operands[0]].Lambda()
		if template == nil {
			continue
		}
		if entry, ok := byTemplate[template]; ok && names.parents[entry] < 0 {
			names.parents[entry] = int(place.entry)
		}
	}
	return names
}

// name is the slot name of the variable the slot instruction ins reaches (x#5, #4, total), or "" when it
// is not a slot instruction or the program does not tell.
func (names *variableNames) name(ins decodedInstruction, place stackState) string {
	if names == nil {
		return ""
	}
	entry, index := int(place.entry), 0
	switch ins.op {
	case LOAD_SLOT, STORE_SLOT:
		index = int(ins.operands[0])
	case LOAD_OUTER, STORE_OUTER:
		index = int(ins.operands[1])
		for depth := int(ins.operands[0]); depth > 0; depth-- {
			if entry < 0 || entry >= len(names.parents) {
				return ""
			}
			entry = names.parents[entry]
		}
	default:
		return ""
	}
	if entry < 0 || entry >= len(names.locals) || index >= len(names.locals[entry]) {
		return ""
	}
	return names.locals[entry][index]
}
