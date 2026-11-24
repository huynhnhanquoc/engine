package runtime

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const DynamicStack = -1

// InstructionSpec is the canonical structural contract for one executable opcode.
// OperandWidths contains big-endian operand widths in bytes. StackIn and StackOut
// describe the fixed stack effect; dynamic instructions expose it through StackEffect.
type InstructionSpec struct {
	Name          string
	OperandWidths []uint8
	StackIn       int
	StackOut      int
	Energy        Cost

	operandSize uint8
	stackEffect func([]uint32) (int, int)
}

func instruction(name string, widths []uint8, stackIn, stackOut int, energy Cost) InstructionSpec {
	var operandSize uint8
	for _, width := range widths {
		operandSize += width
	}
	return InstructionSpec{
		Name:          name,
		OperandWidths: widths,
		StackIn:       stackIn,
		StackOut:      stackOut,
		Energy:        energy,
		operandSize:   operandSize,
	}
}

func dynamicInstruction(
	name string,
	widths []uint8,
	energy Cost,
	effect func([]uint32) (int, int),
) InstructionSpec {
	spec := instruction(name, widths, DynamicStack, DynamicStack, energy)
	spec.stackEffect = effect
	return spec
}

var instructionTable = [256]InstructionSpec{
	PUSH:    instruction("PUSH", []uint8{2}, 0, 1, 1),
	POP:     instruction("POP", nil, 1, 0, 1),
	LOAD:    instruction("LOAD", []uint8{2}, 0, 1, 2),
	STORE:   instruction("STORE", []uint8{2}, 1, 1, 2),
	GET:     instruction("GET", nil, 2, 1, 12),
	DUP:     instruction("DUP", nil, 1, 2, 1),
	BUILTIN: instruction("BUILTIN", []uint8{1}, 0, 1, 2),

	ADD: instruction("ADD", nil, 2, 1, 2),
	SUB: instruction("SUB", nil, 2, 1, 2),
	MUL: instruction("MUL", nil, 2, 1, 6),
	DIV: instruction("DIV", nil, 2, 1, 25),
	MOD: instruction("MOD", nil, 2, 1, 25),

	AND: instruction("AND", nil, 2, 1, 1),
	OR:  instruction("OR", nil, 2, 1, 1),
	NOT: instruction("NOT", nil, 1, 1, 1),

	COMPARE: instruction("COMPARE", []uint8{1}, 2, 1, 4),
	JUMP:    instruction("JUMP", []uint8{2}, 0, 0, 3),
	TRUE:    instruction("TRUE", []uint8{2}, 1, 0, 3),
	FALSE:   instruction("FALSE", []uint8{2}, 1, 0, 3),
	// ITER has branch-specific effects. StackIn/StackOut describes the successful
	// iteration path; Verify handles the exhausted path separately.
	ITER: instruction("ITER", []uint8{2}, 2, 3, 15),
	HALT: instruction("HALT", nil, 0, 0, 1),

	MAKE:  instruction("MAKE", []uint8{1}, 0, 1, 80),
	SET:   instruction("SET", nil, 3, 1, 10),
	MERGE: instruction("MERGE", nil, 2, 1, 20),

	CALL: dynamicInstruction("CALL", []uint8{1}, 150, func(operands []uint32) (int, int) {
		return int(operands[0]) + 1, 1
	}),
	INVOKE: dynamicInstruction("INVOKE", []uint8{1}, 150, func(operands []uint32) (int, int) {
		return int(operands[0]) + 2, 1
	}),
	RETURN: instruction("RETURN", nil, 0, 0, 5),
	DEFER:  instruction("DEFER", nil, 1, 0, 10),
	SPAWN:  instruction("SPAWN", nil, 1, 0, 200),

	// Each wide form is its instruction with a three-byte operand: same stack effect, same energy.
	PUSH_WIDE:  instruction("PUSH_WIDE", []uint8{3}, 0, 1, 1),
	LOAD_WIDE:  instruction("LOAD_WIDE", []uint8{3}, 0, 1, 2),
	STORE_WIDE: instruction("STORE_WIDE", []uint8{3}, 1, 1, 2),
	JUMP_WIDE:  instruction("JUMP_WIDE", []uint8{3}, 0, 0, 3),
	TRUE_WIDE:  instruction("TRUE_WIDE", []uint8{3}, 1, 0, 3),
	FALSE_WIDE: instruction("FALSE_WIDE", []uint8{3}, 1, 0, 3),
	ITER_WIDE:  instruction("ITER_WIDE", []uint8{3}, 2, 3, 15),

	// STEP costs what the PUSH 1; ADD it replaces in x++ cost, so no program's energy moved (bytecode 4)
	STEP: instruction("STEP", []uint8{1}, 1, 1, 3),

	// TYPEOF costs what a cheap operator does; the PUSH and INVOKE it replaces cost 151 (bytecode 6)
	TYPEOF: instruction("TYPEOF", nil, 1, 1, 2),

	// A slot instruction costs what the LOAD or STORE it replaces costs (bytecode 7), so no program's
	// energy moved. LOAD_OUTER and STORE_OUTER take a one-byte depth before the index.
	LOAD_SLOT:        instruction("LOAD_SLOT", []uint8{2}, 0, 1, 2),
	LOAD_SLOT_WIDE:   instruction("LOAD_SLOT_WIDE", []uint8{3}, 0, 1, 2),
	STORE_SLOT:       instruction("STORE_SLOT", []uint8{2}, 1, 1, 2),
	STORE_SLOT_WIDE:  instruction("STORE_SLOT_WIDE", []uint8{3}, 1, 1, 2),
	LOAD_OUTER:       instruction("LOAD_OUTER", []uint8{1, 2}, 0, 1, 2),
	LOAD_OUTER_WIDE:  instruction("LOAD_OUTER_WIDE", []uint8{1, 3}, 0, 1, 2),
	STORE_OUTER:      instruction("STORE_OUTER", []uint8{1, 2}, 1, 1, 2),
	STORE_OUTER_WIDE: instruction("STORE_OUTER_WIDE", []uint8{1, 3}, 1, 1, 2),

	// TEMPLATE costs what the ADD it replaces in a template costs (bytecode 8), so no program's energy moved
	TEMPLATE: instruction("TEMPLATE", nil, 2, 1, 2),

	// SPREAD costs what a SET does, and what it reads past it as a spread of as many keys (spread.go); a spread
	// call costs what a call does (bytecode 9)
	SPREAD:        instruction("SPREAD", []uint8{1}, 2, 1, 10),
	CALL_SPREAD:   instruction("CALL_SPREAD", nil, 2, 1, 150),
	INVOKE_SPREAD: instruction("INVOKE_SPREAD", nil, 3, 1, 150),
	REST: dynamicInstruction("REST", []uint8{1, 1}, 20, func(operands []uint32) (int, int) {
		if operands[0] == RestObject {
			return int(operands[1]) + 1, 2 // the keys read and the value; the value and the rest
		}
		return 1, 2
	}),

	// ITEMS costs what TYPEOF does for an array, which it leaves as it is, and what a spread of as many keys costs
	// for a string's or bytes' items (spread.go) (bytecode 10)
	ITEMS: instruction("ITEMS", nil, 1, 1, 2),
	// DELETE costs what the SET of a key does (bytecode 10)
	DELETE: instruction("DELETE", nil, 2, 1, 10),
	// UNSET_SLOT and UNSET_NAME cost what a STORE does (bytecode 10); the compiler writes them only for a variable a
	// function made before its line may read (scope.go, holes)
	UNSET_SLOT: instruction("UNSET_SLOT", []uint8{3, 3}, 0, 0, 2),
	UNSET_NAME: instruction("UNSET_NAME", []uint8{3}, 0, 0, 2),
}

// LookupInstruction returns metadata only for opcodes implemented by the VM.
// Reserved or historical numeric slots deliberately return false.
func LookupInstruction(op Opcode) (InstructionSpec, bool) {
	spec, ok := lookupInstructionSpec(op)
	if !ok {
		return InstructionSpec{}, false
	}
	return *spec, true
}

func lookupInstructionSpec(op Opcode) (*InstructionSpec, bool) {
	spec := &instructionTable[uint8(op)]
	return spec, spec.Name != ""
}

// InstructionSetChecksum fingerprints the encoded instruction contract.
// BytecodeVersion remains the explicit compatibility decision; this checksum
// lets compiler caches also notice accidental metadata drift.
func InstructionSetChecksum() string {
	return instructionSetChecksum(BytecodeVersion, len(instructionTable))
}

// instructionSetChecksum hashes a version and the instructions in the slots below limit: bytecode 2's
// checksum was instructionSetChecksum(2, int(PUSH_WIDE)) while COMMIT held its slot, which bytecode 6
// emptied.
func instructionSetChecksum(version uint16, limit int) string {
	hash := sha256.New()
	var number [8]byte
	binary.BigEndian.PutUint16(number[:2], version)
	hash.Write(number[:2])
	for raw := 0; raw < limit; raw++ {
		spec := instructionTable[raw]
		if spec.Name == "" {
			continue
		}
		hash.Write([]byte{byte(raw)})
		binary.BigEndian.PutUint16(number[:2], uint16(len(spec.Name)))
		hash.Write(number[:2])
		hash.Write([]byte(spec.Name))
		hash.Write([]byte{byte(len(spec.OperandWidths))})
		hash.Write(spec.OperandWidths)
		binary.BigEndian.PutUint32(number[:4], uint32(int32(spec.StackIn)))
		hash.Write(number[:4])
		binary.BigEndian.PutUint32(number[:4], uint32(int32(spec.StackOut)))
		hash.Write(number[:4])
		binary.BigEndian.PutUint64(number[:], uint64(spec.Energy))
		hash.Write(number[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (s InstructionSpec) OperandSize() int {
	return int(s.operandSize)
}

func (s InstructionSpec) StackEffect(operands []uint32) (int, int) {
	if s.stackEffect != nil {
		return s.stackEffect(operands)
	}
	return s.StackIn, s.StackOut
}

// DecodeOperands decodes one instruction's operands from the bytes immediately
// following its opcode. An operand is one, two or three bytes (three in a wide form).
func DecodeOperands(spec InstructionSpec, data []byte) ([]uint32, int, error) {
	need := spec.OperandSize()
	if len(data) < need {
		return nil, 0, fmt.Errorf("need %d operand bytes, have %d", need, len(data))
	}

	operands := make([]uint32, len(spec.OperandWidths))
	offset := 0
	for i, width := range spec.OperandWidths {
		switch width {
		case 1:
			operands[i] = uint32(data[offset])
		case 2:
			operands[i] = uint32(data[offset])<<8 | uint32(data[offset+1])
		case 3:
			operands[i] = uint32(data[offset])<<16 | uint32(data[offset+1])<<8 | uint32(data[offset+2])
		default:
			return nil, 0, fmt.Errorf("unsupported operand width %d", width)
		}
		offset += int(width)
	}
	return operands, offset, nil
}

// decodeOperandsInto is DecodeOperands writing into block, whose capacity the caller sized for every
// instruction of a program: the result is a full slice expression of block, so a later append to block
// never writes into it.
func decodeOperandsInto(spec *InstructionSpec, data []byte, block *[]uint32) ([]uint32, int, error) {
	need := spec.OperandSize()
	if len(data) < need {
		return nil, 0, fmt.Errorf("need %d operand bytes, have %d", need, len(data))
	}
	operands := *block
	start := len(operands)
	offset := 0
	for _, width := range spec.OperandWidths {
		var operand uint32
		switch width {
		case 1:
			operand = uint32(data[offset])
		case 2:
			operand = uint32(data[offset])<<8 | uint32(data[offset+1])
		case 3:
			operand = uint32(data[offset])<<16 | uint32(data[offset+1])<<8 | uint32(data[offset+2])
		default:
			return nil, 0, fmt.Errorf("unsupported operand width %d", width)
		}
		operands = append(operands, operand)
		offset += int(width)
	}
	*block = operands
	return operands[start:len(operands):len(operands)], offset, nil
}
