package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// BytecodeVersion identifies the structural format understood by this runtime.
// Increment it when instruction encoding or constant semantics become incompatible.
//
// 3 (14/10/2026): the wide forms PUSH_WIDE … ITER_WIDE, a three-byte constant index or jump address,
// and a 2 MiB program limit (it was 65,535 bytes, the reach of a two-byte jump). A bytecode 2 program
// is the same bytes in bytecode 3 and means the same; a runtime of 2 cannot run a wide program, which
// is why the version moved.
//
// 4 (15/10/2026, the owner's L3): STEP, one instruction with a one-byte operand, makes a number of what
// ++ and -- step, as JavaScript's ToNumber does, and steps it. x++ was x = x + 1 (LOAD x; PUSH 1; ADD;
// STORE x), so "5"++ joined "51" and null-- failed; it is LOAD x; STEP 1; STORE x, two bytes shorter and
// the same energy (STEP costs 3, PUSH 1 and ADD cost 3). A runtime of 3 refuses STEP as an unknown
// opcode, which is why the version moved; no other instruction changed.
//
// 5 (03/11/2026): COMPARE 6 and 7 are JavaScript's loose == and != (value.LooseEqual), which compiled as
// the strict 0 and 1; and SET leaves the value it wrote, not its target, so o.x = v gives v as in
// JavaScript. A runtime of 4 refuses COMPARE 6 as an invalid operand, and a program of 4 that read the
// target SET left (a variable of a loop's iteration, assigned: SET; PUSH slot; GET) reads it wrong on a
// runtime of 5, which is why the version moved.
//
// 6 (04/11/2026): STORE_LOCAL and STORE_LOCAL_WIDE write a variable of the function running without
// looking in the functions around first; TYPEOF is typeof x, which was x; PUSH "#typeof"; INVOKE 0; STEP 3
// is -x, which was x; PUSH -1; MUL; and COMMIT, which nothing emitted, is gone (opcode.go). A runtime of
// 5 refuses the new opcodes and STEP 3, and this one refuses COMMIT and would run a program of 5's typeof
// as a method call named "#typeof", which is why the version moved.
//
// 7 (04/11/2026): a function's own variables live in its frame's slots, read and written by index
// (LOAD_SLOT, STORE_SLOT, and LOAD_OUTER, STORE_OUTER for a function around), as do the top level's block
// variables; the top level's own variables and the names nothing declares keep LOAD and STORE by name. A
// lambda template carries its slots' names (Locals) and what its closures keep (CapturesSlots,
// CapturesNames, WritesNames), the Program its top level's slots; STORE_LOCAL is gone. A runtime of 6
// refuses the new opcodes, and this one would read a program of 6's function variables by name in maps it
// no longer keeps, which is why the version moved.
//
// 8 (05/11/2026): TEMPLATE joins a template literal's parts, each made text by ToString as JavaScript's
// template makes it (an object's own toString before its valueOf); they were joined by ADD, whose default
// hint runs valueOf first. A runtime of 7 refuses TEMPLATE, and a program of 7 would join an object's
// valueOf here, which is why the version moved.
//
// 9 (15/11/2026, round 5 of the 13/11 review): SPREAD, CALL_SPREAD and INVOKE_SPREAD, the spread of an array and of
// a call, and REST, the rest of a pattern (spread.go), which were compile errors; a lambda's flags hold Rest, its
// last parameter gathering the arguments past the others. A runtime of 8 refuses them, which is why the version
// moved; no other instruction changed.
//
// 10 (18/11/2026): ITEMS, what an array pattern reads, the value made its iteration's items (spread.go): the
// pattern read the value by index, so a string gave half of a character outside the basic plane, and what is not
// iterable gave undefined where JavaScript throws; DELETE, delete o.k (delete.go), which was a compile error; and
// UNSET_SLOT and UNSET_NAME, a let or a const marked not yet made (unset.go), which a function read as undefined
// before its line had run. A runtime of 9 refuses them as unknown opcodes, which is why the version moved; STORE_OUTER
// refuses to write into a variable so marked; no other instruction changed. A lambda's flags may hold Defaulted (lambdaDefaulted), which a
// runtime of 9 refuses as an unknown flag.
const BytecodeVersion uint16 = 10

// Program is an immutable, verified unit of Kitwork execution.
//
// Its slices are private and copied at construction. Public accessors return
// snapshots; only the runtime hot path reads the owned storage directly.
type Program struct {
	code      []byte
	constants []value.Value
	// locals names the slots of the top level's frame (bytecode 7): its block variables, the objects its
	// loops make for each iteration, the compiler's temporaries. Its own variables are vm.Vars, by name.
	locals   []string
	debug    debugTable
	version  uint16
	checksum [sha256.Size]byte
	profile  ProgramProfile
	// calls names the callee of a failed call (callee.go); read back from the code on first use
	calls *callSites
}

// NewProgram copies and verifies a complete program before publishing it.
func NewProgram(code []byte, constants []value.Value, sourceMap []int32) (*Program, error) {
	if len(sourceMap) != 0 && len(sourceMap) != len(code) {
		return nil, fmt.Errorf(
			"program source map has %d entries for %d bytecode bytes",
			len(sourceMap),
			len(code),
		)
	}
	return NewProgramWithDebug(code, constants, debugEntriesFromSourceMap(sourceMap))
}

// NewProgramWithDebug copies and verifies code, constants, and a compressed
// source-location table before publishing the Program. Its top level has no slots.
func NewProgramWithDebug(
	code []byte,
	constants []value.Value,
	debugEntries []DebugEntry,
) (*Program, error) {
	return NewProgramWithLocals(code, constants, debugEntries, nil)
}

// NewProgramWithLocals is NewProgramWithDebug of a program whose top level keeps variables in slots:
// locals names them by index (the compiler's, bytecode 7).
func NewProgramWithLocals(
	code []byte,
	constants []value.Value,
	debugEntries []DebugEntry,
	locals []string,
) (*Program, error) {
	ownedCode := append([]byte(nil), code...)
	ownedConstants := cloneConstants(constants)
	var ownedLocals []string
	if len(locals) > 0 {
		ownedLocals = append([]string(nil), locals...)
	}

	if err := validateProgramConstants(ownedConstants); err != nil {
		return nil, err
	}
	if len(ownedLocals) > MaxLocals {
		return nil, fmt.Errorf("the top level has %d slots, more than %d", len(ownedLocals), MaxLocals)
	}
	profile, err := verifyAndProfile(ownedCode, ownedConstants, len(ownedLocals))
	if err != nil {
		return nil, err
	}
	debug, err := newDebugTable(len(ownedCode), debugEntries)
	if err != nil {
		return nil, err
	}

	program := &Program{
		code:      ownedCode,
		constants: ownedConstants,
		locals:    ownedLocals,
		debug:     debug,
		version:   BytecodeVersion,
		profile:   profile,
		calls:     &callSites{},
	}
	program.checksum = programChecksum(program)
	return program, nil
}

// EmptyProgram returns a verified empty program for VM pools and uninitialized tenants.
func EmptyProgram() *Program {
	program, err := NewProgram(nil, nil, nil)
	if err != nil {
		panic(err)
	}
	return program
}

// ProgramVersion reports the bytecode version without exposing program storage.
func (p *Program) ProgramVersion() uint16 {
	if p == nil {
		return 0
	}
	return p.version
}

func (p *Program) Len() int {
	if p == nil {
		return 0
	}
	return len(p.code)
}

func (p *Program) Checksum() string {
	if p == nil {
		return ""
	}
	return hex.EncodeToString(p.checksum[:])
}

// ChecksumDigest returns the fixed-size Program identity without allocating or
// exposing internal storage. Runtime telemetry can use it as a map key without
// retaining the Program or its generation.
func (p *Program) ChecksumDigest() [sha256.Size]byte {
	if p == nil {
		return [sha256.Size]byte{}
	}
	return p.checksum
}

// Profile returns a detached static profile produced during bytecode
// verification. It performs no decoding and exposes no Program storage.
func (p *Program) Profile() ProgramProfile {
	if p == nil {
		return ProgramProfile{}
	}
	profile := p.profile
	profile.Opcodes = append([]OpcodeProfile(nil), p.profile.Opcodes...)
	return profile
}

// Instructions returns a copy suitable for diagnostics and serialization.
func (p *Program) Instructions() []byte {
	if p == nil {
		return nil
	}
	return append([]byte(nil), p.code...)
}

// Constants returns a detached copy suitable for diagnostics and serialization.
func (p *Program) Constants() []value.Value {
	if p == nil {
		return nil
	}
	return cloneConstants(p.constants)
}

// Locals returns a copy of the names of the top level's slots, by index.
func (p *Program) Locals() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.locals...)
}

// rootSlots is how many slots the top level's frame holds.
func (p *Program) rootSlots() int {
	if p == nil {
		return 0
	}
	return len(p.locals)
}

// SourceMap returns a copy of the byte-offset to source-line mapping.
func (p *Program) SourceMap() []int32 {
	if p == nil {
		return nil
	}
	sourceMap := make([]int32, len(p.code))
	for ip := range sourceMap {
		sourceMap[ip] = p.debug.location(ip).Line
	}
	return sourceMap
}

// DebugEntries returns a detached snapshot of the compressed source table.
func (p *Program) DebugEntries() []DebugEntry {
	if p == nil {
		return nil
	}
	return p.debug.snapshot()
}

// SourceAt resolves the original source location for a bytecode offset.
func (p *Program) SourceAt(ip int) SourceLocation {
	if p == nil {
		return SourceLocation{}
	}
	return p.debug.location(ip)
}

// ProgramFromRef resolves the opaque handle stored by a detached lambda (value.Lambda.Program): the
// origin a closure of a VM holds (origin.go), or a Program itself.
func ProgramFromRef(ref any) (*Program, bool) {
	switch handle := ref.(type) {
	case *closureOrigin:
		return handle.program, handle != nil && handle.program != nil
	case *Program:
		return handle, handle != nil
	}
	return nil, false
}

func cloneConstants(constants []value.Value) []value.Value {
	if constants == nil {
		return nil
	}
	// the names of every lambda's parameters and slots are copied into one array, cut per lambda
	names := 0
	for _, constant := range constants {
		if lambda := constant.Lambda(); lambda != nil {
			names += len(lambda.Params) + len(lambda.Locals)
		}
	}
	arena := make([]string, 0, names)
	cloned := make([]value.Value, len(constants))
	for i, constant := range constants {
		cloned[i] = cloneConstant(constant, &arena)
	}
	return cloned
}

// cloneConstant copies a constant; a lambda's names go into arena, which has room for them.
func cloneConstant(constant value.Value, arena *[]string) value.Value {
	switch {
	case constant.IsBytes():
		return value.Bytes(append([]byte(nil), constant.Bytes()...))
	case constant.IsFunction():
		lambda := constant.Lambda()
		if lambda == nil {
			return constant // a Go function has nothing of the program to copy
		}
		return value.Closure(&value.Lambda{
			Address:       lambda.Address,
			Name:          lambda.Name,
			SourceFile:    lambda.SourceFile,
			SourceLine:    lambda.SourceLine,
			SourceColumn:  lambda.SourceColumn,
			Params:        copyNames(arena, lambda.Params),
			Locals:        copyNames(arena, lambda.Locals),
			CapturesSlots: lambda.CapturesSlots,
			CapturesNames: lambda.CapturesNames,
			WritesNames:   lambda.WritesNames,
			Rest:          lambda.Rest,
			Defaulted:     lambda.Defaulted,
		})
	case constant.IsArray():
		return value.Array(cloneConstants(constant.Items()))
	case constant.IsObject():
		cloned := value.Object()
		for _, key := range constant.Keys() {
			cloned.Set(key, cloneConstant(constant.Get(key), arena))
		}
		return cloned
	}
	return constant
}

// copyNames copies names to the end of arena and gives the copy, capped so an append to it copies; nil
// for none. An arena without room (a lambda inside an object) grows as append does.
func copyNames(arena *[]string, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	start := len(*arena)
	*arena = append(*arena, names...)
	return (*arena)[start:len(*arena):len(*arena)]
}

func validateProgramConstants(constants []value.Value) error {
	for index, constant := range constants {
		if constant.IsInvalid() {
			return fmt.Errorf("program constant %d is a failure", index)
		}
		switch {
		case constant.IsUndefined(), constant.IsNull(), constant.IsNumber(), constant.IsBoolean(),
			constant.IsTime(), constant.IsDuration(), constant.IsString():
			// a scalar: its kind says its payload (value.go), so nothing mutable can hide under one
		case constant.IsFunction():
			lambda := constant.Lambda()
			if lambda == nil {
				return fmt.Errorf(
					"program constant %d must contain a lambda template, got a Go function",
					index,
				)
			}
			if lambda.Scope != nil || lambda.Slots != nil || lambda.Parent != nil || lambda.Program != nil ||
				lambda.Run != nil {
				return fmt.Errorf("program constant %d contains a bound closure", index)
			}
			// the parameters are bound into the first slots, so a frame holds at least one per parameter
			if len(lambda.Locals) < len(lambda.Params) {
				return fmt.Errorf(
					"program constant %d has %d slots for %d parameters",
					index,
					len(lambda.Locals),
					len(lambda.Params),
				)
			}
			if len(lambda.Locals) > MaxLocals {
				return fmt.Errorf("program constant %d has %d slots, more than %d", index, len(lambda.Locals), MaxLocals)
			}
		default:
			return fmt.Errorf(
				"program constant %d uses mutable or unsupported kind %s",
				index,
				constant.Kind(),
			)
		}
	}
	return nil
}

// A constant's tag in the checksum and in the binary envelope (program_binary.go): the numbers of
// the first engine's value.Kind, kept so every checksum and every cached program stays what it was
// now that value keeps its kind to itself. undefined takes 0, the Invalid the first engine never
// wrote (it refused a failure as a constant), so no cached program of its era reads differently.
const (
	constantTagUndefined byte = 0
	constantTagNull      byte = 1
	constantTagNumber    byte = 2
	constantTagBoolean   byte = 3
	constantTagTime      byte = 4
	constantTagDuration  byte = 5
	constantTagString    byte = 6
	constantTagFunction  byte = 11
)

// constantTag is the tag of a constant's kind; false for a kind no program constant may have.
func constantTag(constant value.Value) (byte, bool) {
	switch {
	case constant.IsUndefined():
		return constantTagUndefined, true
	case constant.IsNull():
		return constantTagNull, true
	case constant.IsNumber():
		return constantTagNumber, true
	case constant.IsBoolean():
		return constantTagBoolean, true
	case constant.IsTime():
		return constantTagTime, true
	case constant.IsDuration():
		return constantTagDuration, true
	case constant.IsString():
		return constantTagString, true
	case constant.IsFunction():
		return constantTagFunction, true
	}
	return 0, false
}

// constantNumber is the number the first engine kept beside a scalar constant, written to the
// checksum and the envelope as its float64 bits: the number itself, 1 or 0 for a boolean, the
// nanoseconds of a time or a duration, 0 for everything else.
func constantNumber(constant value.Value) float64 {
	switch {
	case constant.IsNumber():
		return constant.Number()
	case constant.IsBoolean():
		if constant.Boolean() {
			return 1
		}
		return 0
	case constant.IsTime():
		return float64(constant.Time().UnixNano())
	case constant.IsDuration():
		return float64(constant.Duration().Nanoseconds())
	}
	return 0
}

// scalarConstant is the constant a tag and its number name when the tag is a scalar's; false for a
// string's, a function's or an unknown tag.
func scalarConstant(tag byte, number float64) (value.Value, bool) {
	switch tag {
	case constantTagUndefined:
		return value.Undefined(), true
	case constantTagNull:
		return value.Null(), true
	case constantTagNumber:
		return value.Number(number), true
	case constantTagBoolean:
		return value.Boolean(number != 0), true
	case constantTagTime:
		return value.Time(time.Unix(0, int64(number)).UTC()), true
	case constantTagDuration:
		return value.Duration(time.Duration(number)), true
	}
	return value.Value{}, false
}

func programChecksum(program *Program) [sha256.Size]byte {
	h := newChecksumWriter(sha256.New())
	writeUint16(h, program.version)
	writeBytes(h, program.code)
	writeUint64(h, uint64(len(program.constants)))
	for _, constant := range program.constants {
		writeConstant(h, constant)
	}
	writeUint64(h, uint64(len(program.debug.files)))
	for _, file := range program.debug.files {
		writeString(h, file)
	}
	writeUint64(h, uint64(len(program.debug.entries)))
	for _, entry := range program.debug.entries {
		writeUint64(h, uint64(entry.ip))
		writeUint32(h, entry.fileID)
		writeUint32(h, uint32(entry.line))
		writeUint32(h, uint32(entry.column))
	}
	// the top level's slots (bytecode 7)
	writeUint64(h, uint64(len(program.locals)))
	for _, local := range program.locals {
		writeString(h, local)
	}

	var checksum [sha256.Size]byte
	copy(checksum[:], h.sum())
	return checksum
}

func writeConstant(h *checksumWriter, constant value.Value) {
	tag, _ := constantTag(constant) // validated before the checksum, so every constant has one
	h.writeByte(tag)
	writeUint64(h, math.Float64bits(constantNumber(constant)))
	// a flags byte, once IsError: always 0 since 25/10/2026 (a constant is never a failure), kept so
	// every checksum stays what it was
	h.writeByte(0)

	switch {
	case constant.IsString():
		h.writeByte(1)
		writeString(h, constant.Text())
	case constant.IsFunction():
		h.writeByte(2)
		lambda := constant.Lambda()
		if lambda == nil {
			writeUint64(h, ^uint64(0))
			return
		}
		writeUint64(h, uint64(lambda.Address))
		writeString(h, lambda.Name)
		writeString(h, lambda.SourceFile)
		writeUint32(h, uint32(lambda.SourceLine))
		writeUint32(h, uint32(lambda.SourceColumn))
		writeUint64(h, uint64(len(lambda.Params)))
		for _, parameter := range lambda.Params {
			writeString(h, parameter)
		}
		// its slots and what its closures keep (bytecode 7)
		writeUint64(h, uint64(len(lambda.Locals)))
		for _, local := range lambda.Locals {
			writeString(h, local)
		}
		h.writeByte(lambdaFlags(lambda))
		if lambda.Defaulted > 0 {
			writeUint64(h, uint64(lambda.Defaulted)) // lambdaDefaulted
		}
	default:
		// a scalar carried no payload: the first engine wrote 0 for its nil
		h.writeByte(0)
	}
}

// The bits of a lambda template's flags byte, in the checksum and the binary envelope (bytecode 7).
const (
	lambdaCapturesSlots byte = 1 << iota
	lambdaCapturesNames
	lambdaWritesNames
	// lambdaRest (bytecode 9): the last parameter gathers the arguments past the others
	lambdaRest
	// lambdaDefaulted (18/11/2026): Defaulted follows the flags, two bytes, the parameters from the first with a
	// default up to the rest, which the function's length leaves out. A function without one is the bytes it was;
	// a runtime that does not know the bit refuses the function as an unknown flag, so the version stays.
	lambdaDefaulted
	lambdaKnownFlags = lambdaCapturesSlots | lambdaCapturesNames | lambdaWritesNames | lambdaRest | lambdaDefaulted
)

// lambdaFlags packs what a template's closures keep into one byte.
func lambdaFlags(lambda *value.Lambda) byte {
	var flags byte
	if lambda.CapturesSlots {
		flags |= lambdaCapturesSlots
	}
	if lambda.CapturesNames {
		flags |= lambdaCapturesNames
	}
	if lambda.WritesNames {
		flags |= lambdaWritesNames
	}
	if lambda.Rest {
		flags |= lambdaRest
	}
	if lambda.Defaulted > 0 {
		flags |= lambdaDefaulted
	}
	return flags
}
