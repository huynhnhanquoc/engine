package runtime

import (
	"fmt"

	"github.com/kitwork/engine2/core/value"
)

type VerifyCode string

const (
	VerifyProgramTooLarge     VerifyCode = "PROGRAM_TOO_LARGE"
	VerifyConstantsTooLarge   VerifyCode = "CONSTANTS_TOO_LARGE"
	VerifyUnknownOpcode       VerifyCode = "UNKNOWN_OPCODE"
	VerifyTruncatedOperand    VerifyCode = "TRUNCATED_OPERAND"
	VerifyInvalidOperand      VerifyCode = "INVALID_OPERAND"
	VerifyConstantOutOfBounds VerifyCode = "CONSTANT_OUT_OF_BOUNDS"
	VerifyInvalidConstant     VerifyCode = "INVALID_CONSTANT"
	VerifyInvalidJump         VerifyCode = "INVALID_JUMP"
	VerifyInvalidLambda       VerifyCode = "INVALID_LAMBDA"
	VerifyStackUnderflow      VerifyCode = "STACK_UNDERFLOW"
	VerifyStackMismatch       VerifyCode = "STACK_MISMATCH"
	VerifyStackTooDeep        VerifyCode = "STACK_TOO_DEEP"
)

// VerifyError reports a deterministic bytecode rejection without executing the program.
type VerifyError struct {
	Code   VerifyCode
	IP     int
	Opcode Opcode
	Detail string
}

func (e *VerifyError) Error() string {
	location := ""
	if e.IP >= 0 {
		location = fmt.Sprintf(" at byte %d", e.IP)
	}
	if e.Detail == "" {
		return fmt.Sprintf("bytecode verify %s%s", e.Code, location)
	}
	return fmt.Sprintf("bytecode verify %s%s: %s", e.Code, location, e.Detail)
}

type decodedInstruction struct {
	ip   int
	next int
	// op is what the instruction does: PUSH for a PUSH_WIDE too, so every reader of the decoded
	// program treats a wide form as its instruction. encoded is the opcode as written.
	op      Opcode
	encoded Opcode
	// spec is the instruction's entry of instructionTable, operands a window on one block every
	// instruction of the program shares (decodeProgram)
	spec     *InstructionSpec
	operands []uint32
}

// OpcodeProfile describes one opcode's static presence in an immutable Program.
// EncodedEnergy is Count multiplied by the instruction's canonical energy cost;
// it does not predict loop iterations or runtime branch frequency.
type OpcodeProfile struct {
	Opcode        Opcode `json:"opcode"`
	Name          string `json:"name"`
	Count         int    `json:"count"`
	EncodedEnergy uint64 `json:"encoded_energy"`
}

// ProgramProfile is a static summary produced by the same pass that verifies
// bytecode. It describes encoded structure, not dynamic execution frequency.
type ProgramProfile struct {
	BytecodeBytes int             `json:"bytecode_bytes"`
	Constants     int             `json:"constants"`
	Instructions  int             `json:"instructions"`
	EntryPoints   int             `json:"entry_points"`
	MaxStackDepth int             `json:"max_stack_depth"`
	EncodedEnergy uint64          `json:"encoded_energy"`
	Opcodes       []OpcodeProfile `json:"opcodes"`
}

// Verify rejects structurally invalid bytecode before it reaches a VM. Its top level has no slots
// (VerifyWithLocals gives it some).
func Verify(code []byte, constants []value.Value) error {
	_, err := verifyAndProfile(code, constants, 0)
	return err
}

// VerifyWithLocals is Verify of a program whose top level holds rootSlots slots (bytecode 7).
func VerifyWithLocals(code []byte, constants []value.Value, rootSlots int) error {
	_, err := verifyAndProfile(code, constants, rootSlots)
	return err
}

// verifyAndProfile verifies code whose top level's frame holds rootSlots slots and whose lambdas' frames
// hold as many as their templates name (Locals): a slot instruction of a function never reaches past its
// frame, so the VM indexes a frame's slots without a test.
func verifyAndProfile(code []byte, constants []value.Value, rootSlots int) (ProgramProfile, error) {
	profile := ProgramProfile{
		BytecodeBytes: len(code),
		Constants:     len(constants),
	}
	if len(code) > MaxBytecodeSize {
		return ProgramProfile{}, verifyError(
			VerifyProgramTooLarge,
			-1,
			0,
			fmt.Sprintf("%d bytes exceeds %d", len(code), MaxBytecodeSize),
		)
	}
	if len(constants) > MaxConstants {
		return ProgramProfile{}, verifyError(
			VerifyConstantsTooLarge,
			-1,
			0,
			fmt.Sprintf("%d constants exceeds %d", len(constants), MaxConstants),
		)
	}
	if len(code) == 0 {
		return profile, nil
	}

	instructions, byIP, err := decodeProgram(code)
	if err != nil {
		return ProgramProfile{}, err
	}
	if err := validateOperands(instructions, constants, byIP, len(code)); err != nil {
		return ProgramProfile{}, err
	}
	if err := validateJumpWidths(instructions, byIP, len(code)); err != nil {
		return ProgramProfile{}, err
	}

	// the top level, then each lambda among the constants: as many at most as there are constants
	entries := append(make([]int, 0, 1+len(constants)), 0)
	slots := append(make([]int, 0, 1+len(constants)), rootSlots)
	for i, constant := range constants {
		lambda := constant.Lambda()
		if lambda == nil {
			// not a program's function: a Go function has no address here (and a program refuses one
			// as a constant before it is verified, program.go); value.Closure never holds a nil lambda
			continue
		}
		if _, ok := byIP.at(lambda.Address); !ok {
			return ProgramProfile{}, verifyError(
				VerifyInvalidLambda,
				-1,
				PUSH,
				fmt.Sprintf("constant %d points to non-instruction address %d", i, lambda.Address),
			)
		}
		entries = append(entries, lambda.Address)
		slots = append(slots, len(lambda.Locals))
	}

	maxStackDepth, err := verifyStack(entries, slots, instructions, byIP, len(code))
	if err != nil {
		return ProgramProfile{}, err
	}

	profile.Instructions = len(instructions)
	profile.EntryPoints = len(entries)
	profile.MaxStackDepth = maxStackDepth
	var counts [256]int
	for _, instruction := range instructions {
		counts[uint8(instruction.encoded)]++
		profile.EncodedEnergy += uint64(instruction.spec.Energy)
	}
	for rawOpcode, count := range counts {
		if count == 0 {
			continue
		}
		opcode := Opcode(rawOpcode)
		spec, _ := LookupInstruction(opcode)
		profile.Opcodes = append(profile.Opcodes, OpcodeProfile{
			Opcode:        opcode,
			Name:          spec.Name,
			Count:         count,
			EncodedEnergy: uint64(spec.Energy) * uint64(count),
		})
	}
	return profile, nil
}

// decodeProgram decodes code into its instructions and the table of where each starts. Every
// allocation is made once at its size: a first pass counts the instructions and their operands, so
// neither slice grows, and the operands of every instruction are windows on one block.
func decodeProgram(code []byte) ([]decodedInstruction, ipTable, error) {
	count, operandCount := 0, 0
	for ip := 0; ip < len(code); count++ {
		spec, ok := lookupInstructionSpec(Opcode(code[ip]))
		if !ok {
			break // the pass below reports it
		}
		operandCount += len(spec.OperandWidths)
		ip += 1 + spec.OperandSize()
	}
	instructions := make([]decodedInstruction, 0, count)
	block := make([]uint32, 0, operandCount)
	byIP := newIPTable(len(code))

	for ip := 0; ip < len(code); {
		start := ip
		op := Opcode(code[ip])
		spec, ok := lookupInstructionSpec(op)
		if !ok {
			return nil, nil, verifyError(
				VerifyUnknownOpcode,
				start,
				op,
				fmt.Sprintf("opcode %d is unknown, reserved, or unsupported", op),
			)
		}

		ip++
		operands, size, err := decodeOperandsInto(spec, code[ip:], &block)
		if err != nil {
			return nil, nil, verifyError(VerifyTruncatedOperand, start, op, err.Error())
		}
		ip += size

		byIP[start] = int32(len(instructions))
		instructions = append(instructions, decodedInstruction{
			ip:       start,
			next:     ip,
			op:       op.Narrow(),
			encoded:  op,
			spec:     spec,
			operands: operands,
		})
	}
	return instructions, byIP, nil
}

func validateOperands(
	instructions []decodedInstruction,
	constants []value.Value,
	byIP ipTable,
	codeLen int,
) error {
	for _, ins := range instructions {
		switch ins.op {
		case LOAD_SLOT, STORE_SLOT, LOAD_OUTER, STORE_OUTER:
			// a frame's slot: its index is checked against the frame of the function whose code it is
			// (analyzeStack); an outer one's depth names a function around, 1 the nearest
			index := int(ins.operands[len(ins.operands)-1])
			if ins.encoded.IsWide() && index <= MaxNarrowOperand {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("wide slot index %d fits two bytes", index),
				)
			}
			if index >= MaxLocals {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("slot %d past the %d a frame holds", index, MaxLocals),
				)
			}
			if (ins.op == LOAD_OUTER || ins.op == STORE_OUTER) && ins.operands[0] == 0 {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					"depth 0 names the frame running, which LOAD_SLOT and STORE_SLOT reach",
				)
			}

		case UNSET_SLOT, UNSET_NAME:
			// a slot below MaxLocals (and the frame's size, analyzeStack) and a name's constant
			name := int(ins.operands[len(ins.operands)-1])
			if ins.op == UNSET_SLOT && int(ins.operands[0]) >= MaxLocals {
				return verifyError(VerifyInvalidOperand, ins.ip, ins.encoded, fmt.Sprintf("slot %d past the %d a frame holds", ins.operands[0], MaxLocals))
			}
			if name >= len(constants) {
				return verifyError(VerifyConstantOutOfBounds, ins.ip, ins.encoded, fmt.Sprintf("constant %d with pool size %d", name, len(constants)))
			}
			if !constants[name].IsString() {
				return verifyError(VerifyInvalidConstant, ins.ip, ins.encoded, fmt.Sprintf("constant %d must be a variable-name string", name))
			}

		case REST:
			// the rest of a pattern (spread.go): its kind; its count, the keys above the value for an object, the index it
			// starts at for an array
			if ins.operands[0] > RestArray {
				return verifyError(VerifyInvalidOperand, ins.ip, ins.encoded, fmt.Sprintf("rest kind %d is not supported", ins.operands[0]))
			}

		case PUSH, LOAD, STORE:
			index := int(ins.operands[0])
			if ins.encoded.IsWide() && index <= MaxNarrowOperand {
				// one encoding per program: the wide form only where two bytes cannot hold the index
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("wide constant index %d fits two bytes", index),
				)
			}
			if index >= len(constants) {
				return verifyError(
					VerifyConstantOutOfBounds,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("constant %d with pool size %d", index, len(constants)),
				)
			}
			if ins.op == LOAD || ins.op == STORE {
				// a String's payload is a string by construction (value.go), so its kind is the whole check
				if !constants[index].IsString() {
					return verifyError(
						VerifyInvalidConstant,
						ins.ip,
						ins.encoded,
						fmt.Sprintf("constant %d must be a variable-name string", index),
					)
				}
			}

		case JUMP, TRUE, FALSE, ITER:
			if ins.encoded.IsWide() && codeLen <= MaxNarrowOperand {
				// the compiler writes jumps wide only in a program two-byte addresses cannot reach
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("wide jump in a %d-byte program, which two-byte addresses reach", codeLen),
				)
			}
			target := int(ins.operands[0])
			if !byIP.boundary(target) {
				return verifyError(
					VerifyInvalidJump,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("target %d is not an instruction boundary", target),
				)
			}

		case COMPARE:
			if ins.operands[0] > CompareLooseNotEqual {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("comparison mode %d is not supported", ins.operands[0]),
				)
			}

		case STEP:
			if ins.operands[0] > StepNegate {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("step %d is not supported", ins.operands[0]),
				)
			}

		case SPREAD:
			if ins.operands[0] > SpreadItem {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("spread mode %d is not supported", ins.operands[0]),
				)
			}

		case MAKE:
			if ins.operands[0] > 1 {
				return verifyError(
					VerifyInvalidOperand,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("collection kind %d is not supported", ins.operands[0]),
				)
			}
		}
	}
	return nil
}

// validateJumpWidths keeps one encoding per program for its jumps (15/10/2026; until then only a wide
// jump in a program of 65,535 bytes or less was refused). The compiler writes every jump of a program in
// two bytes, or every jump in three when one address of the two-byte program would not fit two bytes
// (compiler.go, patchJump). So the widths do not mix, and wide jumps stand only where the same program
// with two-byte jumps, each one byte shorter and every address after it one byte lower, has an address
// past MaxNarrowOperand.
func validateJumpWidths(instructions []decodedInstruction, byIP ipTable, codeLen int) error {
	var wide, narrow *decodedInstruction
	for index := range instructions {
		ins := &instructions[index]
		if !isJumpInstruction(ins.op) {
			continue
		}
		if ins.encoded.IsWide() {
			if wide == nil {
				wide = ins
			}
		} else if narrow == nil {
			narrow = ins
		}
	}
	if wide == nil {
		return nil
	}
	if narrow != nil {
		later := wide
		if narrow.ip > wide.ip {
			later = narrow
		}
		return verifyError(
			VerifyInvalidOperand,
			later.ip,
			later.encoded,
			"two-byte and three-byte jumps in one program, which the compiler writes in one width",
		)
	}
	// before[i] counts the jumps of instructions[:i]: written in two bytes, an address loses one byte for each
	before := make([]int, len(instructions)+1)
	jumps := 0
	for index, ins := range instructions {
		before[index] = jumps
		if isJumpInstruction(ins.op) {
			jumps++
		}
	}
	before[len(instructions)] = jumps
	for _, ins := range instructions {
		if !isJumpInstruction(ins.op) {
			continue
		}
		target := int(ins.operands[0])
		at := len(instructions)
		if target < codeLen {
			at, _ = byIP.at(target)
		}
		if target-before[at] > MaxNarrowOperand {
			return nil
		}
	}
	return verifyError(
		VerifyInvalidOperand,
		wide.ip,
		wide.encoded,
		"wide jumps in a program whose every address fits two bytes when its jumps are written in two bytes",
	)
}

// stackState is what the verifier proved of an instruction: the stack depth before it, in its own frame,
// and entry, the index among the entry points of the function whose code it is. Its fields are 32 bits
// (an address, a depth and an entry all fit), so the map of a program's instructions stays small.
type stackState struct {
	ip    int32
	depth int32
	entry int32
}

func verifyStack(
	entries []int,
	slots []int,
	instructions []decodedInstruction,
	byIP ipTable,
	codeLen int,
) (int, error) {
	maxDepth, _, err := analyzeStack(entries, slots, instructions, byIP, codeLen)
	return maxDepth, err
}

// analyzeStack follows the code of every entry point (the top level and each lambda, entries) and proves
// each reachable instruction's stack depth, and that a slot instruction stays inside the frame of the
// entry whose code it is: slots[i] is how many slots entry i's frame holds.
func analyzeStack(
	entries []int,
	slots []int,
	instructions []decodedInstruction,
	byIP ipTable,
	codeLen int,
) (int, stackPlaces, error) {
	// what is proved of each instruction, by its index; entry -1 until an entry point reaches it
	states := make([]stackState, len(instructions))
	for index := range states {
		states[index].entry = -1
	}
	places := stackPlaces{byIP: byIP, states: states}
	queue := make([]stackState, 0, len(instructions))
	maxDepth := 0
	entry := 0

	enqueue := func(ip, depth int) error {
		if depth < 0 {
			return verifyError(VerifyStackUnderflow, ip, 0, "negative stack depth")
		}
		if depth > MaxStackDepth {
			return verifyError(
				VerifyStackTooDeep,
				ip,
				0,
				fmt.Sprintf("depth %d exceeds %d", depth, MaxStackDepth),
			)
		}
		if depth > maxDepth {
			maxDepth = depth
		}
		if ip == codeLen {
			if depth > 1 {
				return verifyError(
					VerifyStackMismatch,
					ip,
					0,
					fmt.Sprintf("program exit retains %d stack values", depth),
				)
			}
			return nil
		}
		// every address enqueued is an entry point's or a jump's target, both proved boundaries, or the
		// address after an instruction
		index := byIP[ip]
		if existing := states[index]; existing.entry >= 0 {
			if int(existing.depth) != depth {
				return verifyError(
					VerifyStackMismatch,
					ip,
					instructions[index].encoded,
					fmt.Sprintf("control-flow join has depths %d and %d", existing.depth, depth),
				)
			}
			if int(existing.entry) != entry && slots[existing.entry] != slots[entry] {
				// the compiler never shares code between functions: code two frames of different sizes reach
				// would need its slots checked against both
				return verifyError(
					VerifyStackMismatch,
					ip,
					instructions[index].encoded,
					fmt.Sprintf("code of two functions whose frames hold %d and %d slots", slots[existing.entry], slots[entry]),
				)
			}
			return nil
		}
		state := stackState{ip: int32(ip), depth: int32(depth), entry: int32(entry)}
		states[index] = state
		queue = append(queue, state)
		return nil
	}

	for index, address := range entries {
		entry = index
		if err := enqueue(address, 0); err != nil {
			return 0, stackPlaces{}, err
		}
	}

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		entry = int(state.entry)
		depth := int(state.depth)
		ins := &instructions[byIP[state.ip]]
		if (ins.op == LOAD_SLOT || ins.op == STORE_SLOT || ins.op == UNSET_SLOT) && int(ins.operands[0]) >= slots[entry] {
			return 0, stackPlaces{}, verifyError(
				VerifyInvalidOperand,
				ins.ip,
				ins.encoded,
				fmt.Sprintf("slot %d of a frame of %d slots", ins.operands[0], slots[entry]),
			)
		}

		stackIn, stackOut := ins.spec.StackEffect(ins.operands)
		if depth < stackIn {
			return 0, stackPlaces{}, verifyError(
				VerifyStackUnderflow,
				ins.ip,
				ins.encoded,
				fmt.Sprintf("%s needs %d values, stack has %d", ins.spec.Name, stackIn, depth),
			)
		}

		nextDepth := depth - stackIn + stackOut
		switch ins.op {
		case RETURN, HALT:
			if depth > 1 {
				return 0, stackPlaces{}, verifyError(
					VerifyStackMismatch,
					ins.ip,
					ins.encoded,
					fmt.Sprintf("%s retains %d stack values", ins.spec.Name, depth),
				)
			}
			continue

		case JUMP:
			if err := enqueue(int(ins.operands[0]), nextDepth); err != nil {
				return 0, stackPlaces{}, err
			}

		case TRUE, FALSE:
			if err := enqueue(ins.next, nextDepth); err != nil {
				return 0, stackPlaces{}, err
			}
			if err := enqueue(int(ins.operands[0]), nextDepth); err != nil {
				return 0, stackPlaces{}, err
			}

		case ITER:
			if err := enqueue(ins.next, nextDepth); err != nil {
				return 0, stackPlaces{}, err
			}
			if err := enqueue(int(ins.operands[0]), depth-2); err != nil {
				return 0, stackPlaces{}, err
			}

		default:
			if err := enqueue(ins.next, nextDepth); err != nil {
				return 0, stackPlaces{}, err
			}
		}
	}
	return maxDepth, places, nil
}

func verifyError(code VerifyCode, ip int, op Opcode, detail string) error {
	return &VerifyError{Code: code, IP: ip, Opcode: op, Detail: detail}
}
