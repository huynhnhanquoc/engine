package runtime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/kitwork/engine2/core/value"
)

const (
	// ProgramEncodingVersion identifies the binary envelope around a Program.
	// It is independent from BytecodeVersion so storage framing can evolve
	// without claiming that opcode semantics changed.
	//
	// 2 (04/11/2026, bytecode 7): the header counts the top level's slots, whose names follow the
	// constants, and a lambda constant writes its slots' names and a flags byte after its parameters.
	ProgramEncodingVersion uint16 = 2
)

var programBinaryMagic = [4]byte{'K', 'W', 'P', 'B'}

// MarshalBinary serializes a verified Program for a trusted local cache.
// Loading always verifies the envelope, bytecode version, checksum, constants,
// debug table, and instruction stream again before returning an executable.
func (p *Program) MarshalBinary() ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("encode program: nil program")
	}

	data := make([]byte, 0, 64+len(p.code))
	data = append(data, programBinaryMagic[:]...)
	data = appendUint16(data, ProgramEncodingVersion)
	data = appendUint16(data, p.version)
	data = append(data, p.checksum[:]...)
	data = appendUint32(data, uint32(len(p.code)))
	data = appendUint32(data, uint32(len(p.constants)))
	data = appendUint32(data, uint32(len(p.debug.entries)))
	data = appendUint32(data, uint32(len(p.locals)))
	data = append(data, p.code...)

	var err error
	for _, constant := range p.constants {
		data, err = appendBinaryConstant(data, constant)
		if err != nil {
			return nil, err
		}
	}
	for _, local := range p.locals {
		data, err = appendBinaryString(data, local)
		if err != nil {
			return nil, err
		}
	}
	for _, entry := range p.DebugEntries() {
		data = appendUint32(data, uint32(entry.IP))
		data, err = appendBinaryString(data, entry.File)
		if err != nil {
			return nil, err
		}
		data = appendUint32(data, uint32(entry.Line))
		data = appendUint32(data, uint32(entry.Column))
	}

	if len(data) > MaxProgramBinarySize {
		return nil, fmt.Errorf(
			"encode program: %d bytes exceeds %d",
			len(data),
			MaxProgramBinarySize,
		)
	}
	return data, nil
}

// EncodedSize is the length MarshalBinary gives the program, counted without writing it, and the index of
// the first debug entry that ends past MaxProgramBinarySize: len(DebugEntries()) when the envelope fits,
// -1 when the code and the constants alone do not. The debug table writes each entry's file name, so a
// dense module with a long path can pass the envelope while its code is under MaxBytecodeSize: the
// compiler refuses such a program where it crosses (15/10/2026; until then it compiled and ran, and was
// neither cached nor checked).
func (p *Program) EncodedSize() (size int, crossing int) {
	if p == nil {
		return 0, 0
	}
	size = len(programBinaryMagic) + 2 + 2 + len(p.checksum) + 4 + 4 + 4 + 4 + len(p.code)
	for _, constant := range p.constants {
		size += encodedConstantSize(constant)
	}
	for _, local := range p.locals {
		size += 4 + len(local)
	}
	crossing = len(p.debug.entries)
	if size > MaxProgramBinarySize {
		crossing = -1
	}
	for index, entry := range p.debug.entries {
		size += 4 + 4 + len(p.debug.files[entry.fileID]) + 4 + 4
		if crossing == len(p.debug.entries) && size > MaxProgramBinarySize {
			crossing = index
		}
	}
	return size, crossing
}

// encodedConstantSize is what appendBinaryConstant writes for a constant.
func encodedConstantSize(constant value.Value) int {
	size := 1 + 1 + 8
	switch {
	case constant.IsString():
		size += 4 + len(constant.Text())
	case constant.IsFunction():
		if lambda := constant.Lambda(); lambda != nil {
			size += 4 + 4 + len(lambda.Name) + 4 + len(lambda.SourceFile) + 4 + 4 + 2
			for _, parameter := range lambda.Params {
				size += 4 + len(parameter)
			}
			size += 4 + 1
			for _, local := range lambda.Locals {
				size += 4 + len(local)
			}
		}
	}
	return size
}

// UnmarshalProgram restores a local cached Program. The returned Program is a
// new immutable owner; no slice or object points into data.
func UnmarshalProgram(data []byte) (*Program, error) {
	if len(data) > MaxProgramBinarySize {
		return nil, fmt.Errorf(
			"decode program: %d bytes exceeds %d",
			len(data),
			MaxProgramBinarySize,
		)
	}

	reader := programBinaryReader{data: data}
	magic, err := reader.bytes(len(programBinaryMagic))
	if err != nil || !bytes.Equal(magic, programBinaryMagic[:]) {
		return nil, fmt.Errorf("decode program: invalid magic")
	}
	encodingVersion, err := reader.uint16()
	if err != nil {
		return nil, err
	}
	if encodingVersion != ProgramEncodingVersion {
		return nil, fmt.Errorf(
			"decode program: encoding version %d is incompatible with %d",
			encodingVersion,
			ProgramEncodingVersion,
		)
	}
	bytecodeVersion, err := reader.uint16()
	if err != nil {
		return nil, err
	}
	if bytecodeVersion != BytecodeVersion {
		return nil, fmt.Errorf(
			"decode program: bytecode version %d is incompatible with %d",
			bytecodeVersion,
			BytecodeVersion,
		)
	}
	expectedChecksumBytes, err := reader.bytes(len([32]byte{}))
	if err != nil {
		return nil, err
	}
	var expectedChecksum [32]byte
	copy(expectedChecksum[:], expectedChecksumBytes)

	codeLength, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	constantCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	debugCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	localCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if codeLength > MaxBytecodeSize {
		return nil, fmt.Errorf("decode program: bytecode length %d exceeds %d", codeLength, MaxBytecodeSize)
	}
	if constantCount > MaxConstants {
		return nil, fmt.Errorf("decode program: constant count %d exceeds %d", constantCount, MaxConstants)
	}
	if localCount > MaxLocals {
		return nil, fmt.Errorf("decode program: slot count %d exceeds %d", localCount, MaxLocals)
	}
	if uint64(debugCount) > uint64(codeLength) {
		return nil, fmt.Errorf(
			"decode program: debug entry count %d exceeds bytecode length %d",
			debugCount,
			codeLength,
		)
	}

	codeBytes, err := reader.bytes(int(codeLength))
	if err != nil {
		return nil, err
	}
	code := append([]byte(nil), codeBytes...)
	constants := make([]value.Value, int(constantCount))
	for index := range constants {
		constants[index], err = reader.constant()
		if err != nil {
			return nil, fmt.Errorf("decode program constant %d: %w", index, err)
		}
	}
	locals := make([]string, int(localCount))
	for index := range locals {
		locals[index], err = reader.string()
		if err != nil {
			return nil, fmt.Errorf("decode program slot %d: %w", index, err)
		}
	}
	debugEntries := make([]DebugEntry, int(debugCount))
	for index := range debugEntries {
		ip, readErr := reader.uint32()
		if readErr != nil {
			return nil, fmt.Errorf("decode program debug entry %d: %w", index, readErr)
		}
		file, readErr := reader.string()
		if readErr != nil {
			return nil, fmt.Errorf("decode program debug entry %d: %w", index, readErr)
		}
		line, readErr := reader.uint32()
		if readErr != nil {
			return nil, fmt.Errorf("decode program debug entry %d: %w", index, readErr)
		}
		column, readErr := reader.uint32()
		if readErr != nil {
			return nil, fmt.Errorf("decode program debug entry %d: %w", index, readErr)
		}
		debugEntries[index] = DebugEntry{
			IP: int(ip),
			SourceLocation: SourceLocation{
				File:   file,
				Line:   int32(line),
				Column: int32(column),
			},
		}
	}
	if reader.remaining() != 0 {
		return nil, fmt.Errorf("decode program: %d trailing bytes", reader.remaining())
	}

	program, err := NewProgramWithLocals(code, constants, debugEntries, locals)
	if err != nil {
		return nil, fmt.Errorf("decode program: %w", err)
	}
	if program.checksum != expectedChecksum {
		return nil, fmt.Errorf("decode program: checksum mismatch")
	}
	return program, nil
}

func appendBinaryConstant(data []byte, constant value.Value) ([]byte, error) {
	// the tag and the number are the first engine's value.Kind and N (program.go), so the encoding
	// stays what it was
	tag, ok := constantTag(constant)
	if !ok {
		return nil, fmt.Errorf("encode program: unsupported constant kind %s", constant.Kind())
	}
	data = append(data, tag)
	// a flags byte, once IsError: always 0 since 25/10/2026 (a constant is never a failure), kept so
	// the encoding stays what it was
	data = append(data, 0)
	data = appendUint64(data, math.Float64bits(constantNumber(constant)))

	switch tag {
	case constantTagUndefined, constantTagNull, constantTagNumber, constantTagBoolean, constantTagTime,
		constantTagDuration:
		return data, nil
	case constantTagString:
		return appendBinaryString(data, constant.Text())
	case constantTagFunction:
		lambda := constant.Lambda()
		if lambda == nil {
			return nil, fmt.Errorf("encode program: function constant is a Go function, not a lambda template")
		}
		data = appendUint32(data, uint32(int32(lambda.Address)))
		var err error
		data, err = appendBinaryString(data, lambda.Name)
		if err != nil {
			return nil, err
		}
		data, err = appendBinaryString(data, lambda.SourceFile)
		if err != nil {
			return nil, err
		}
		data = appendUint32(data, uint32(lambda.SourceLine))
		data = appendUint32(data, uint32(lambda.SourceColumn))
		if len(lambda.Params) > math.MaxUint16 {
			return nil, fmt.Errorf("encode program: lambda has %d parameters", len(lambda.Params))
		}
		data = appendUint16(data, uint16(len(lambda.Params)))
		for _, parameter := range lambda.Params {
			data, err = appendBinaryString(data, parameter)
			if err != nil {
				return nil, err
			}
		}
		if len(lambda.Locals) > MaxLocals {
			return nil, fmt.Errorf("encode program: lambda has %d slots", len(lambda.Locals))
		}
		data = appendUint32(data, uint32(len(lambda.Locals)))
		for _, local := range lambda.Locals {
			data, err = appendBinaryString(data, local)
			if err != nil {
				return nil, err
			}
		}
		data = append(data, lambdaFlags(lambda))
		if lambda.Defaulted > 0 {
			if lambda.Defaulted > len(lambda.Params) {
				return nil, fmt.Errorf("encode program: lambda has %d defaulted parameters of %d", lambda.Defaulted, len(lambda.Params))
			}
			data = appendUint16(data, uint16(lambda.Defaulted))
		}
		return data, nil
	default:
		return nil, fmt.Errorf("encode program: unsupported constant kind %s", constant.Kind())
	}
}

func appendBinaryString(data []byte, text string) ([]byte, error) {
	if uint64(len(text)) > math.MaxUint32 || len(text) > MaxProgramBinarySize {
		return nil, fmt.Errorf("encode program: string is too large")
	}
	if len(data) > MaxProgramBinarySize-4-len(text) {
		// the envelope is past its limit, not the string (15/10/2026: both said "string is too large")
		return nil, fmt.Errorf("encode program: envelope exceeds %d bytes", MaxProgramBinarySize)
	}
	data = appendUint32(data, uint32(len(text)))
	data = append(data, text...)
	return data, nil
}

func appendUint16(data []byte, number uint16) []byte {
	return binary.BigEndian.AppendUint16(data, number)
}

func appendUint32(data []byte, number uint32) []byte {
	return binary.BigEndian.AppendUint32(data, number)
}

func appendUint64(data []byte, number uint64) []byte {
	return binary.BigEndian.AppendUint64(data, number)
}

type programBinaryReader struct {
	data   []byte
	offset int
}

func (r *programBinaryReader) remaining() int {
	return len(r.data) - r.offset
}

func (r *programBinaryReader) bytes(length int) ([]byte, error) {
	if length < 0 || length > r.remaining() {
		return nil, fmt.Errorf(
			"decode program: truncated payload at byte %d, need %d bytes",
			r.offset,
			length,
		)
	}
	start := r.offset
	r.offset += length
	return r.data[start:r.offset], nil
}

func (r *programBinaryReader) uint16() (uint16, error) {
	data, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (r *programBinaryReader) uint32() (uint32, error) {
	data, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

func (r *programBinaryReader) uint64() (uint64, error) {
	data, err := r.bytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data), nil
}

func (r *programBinaryReader) string() (string, error) {
	length, err := r.uint32()
	if err != nil {
		return "", err
	}
	data, err := r.bytes(int(length))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *programBinaryReader) constant() (value.Value, error) {
	header, err := r.bytes(2)
	if err != nil {
		return value.Value{}, err
	}
	tag := header[0]
	if header[1] != 0 {
		return value.Value{}, fmt.Errorf("unsupported flags 0x%x", header[1])
	}
	number, err := r.uint64()
	if err != nil {
		return value.Value{}, err
	}
	if constant, ok := scalarConstant(tag, math.Float64frombits(number)); ok {
		return constant, nil
	}

	switch tag {
	case constantTagString:
		text, readErr := r.string()
		if readErr != nil {
			return value.Value{}, readErr
		}
		return value.String(text), nil
	case constantTagFunction:
		address, readErr := r.uint32()
		if readErr != nil {
			return value.Value{}, readErr
		}
		name, readErr := r.string()
		if readErr != nil {
			return value.Value{}, readErr
		}
		sourceFile, readErr := r.string()
		if readErr != nil {
			return value.Value{}, readErr
		}
		sourceLine, readErr := r.uint32()
		if readErr != nil {
			return value.Value{}, readErr
		}
		sourceColumn, readErr := r.uint32()
		if readErr != nil {
			return value.Value{}, readErr
		}
		parameterCount, readErr := r.uint16()
		if readErr != nil {
			return value.Value{}, readErr
		}
		parameters := make([]string, int(parameterCount))
		for index := range parameters {
			parameters[index], readErr = r.string()
			if readErr != nil {
				return value.Value{}, readErr
			}
		}
		localCount, readErr := r.uint32()
		if readErr != nil {
			return value.Value{}, readErr
		}
		if localCount > MaxLocals {
			return value.Value{}, fmt.Errorf("lambda slot count %d exceeds %d", localCount, MaxLocals)
		}
		locals := make([]string, int(localCount))
		for index := range locals {
			locals[index], readErr = r.string()
			if readErr != nil {
				return value.Value{}, readErr
			}
		}
		flags, readErr := r.bytes(1)
		if readErr != nil {
			return value.Value{}, readErr
		}
		if flags[0]&^lambdaKnownFlags != 0 {
			return value.Value{}, fmt.Errorf("unsupported lambda flags 0x%x", flags[0])
		}
		defaulted := 0
		if flags[0]&lambdaDefaulted != 0 {
			count, readErr := r.uint16()
			if readErr != nil {
				return value.Value{}, readErr
			}
			if count == 0 || int(count) > len(parameters) {
				return value.Value{}, fmt.Errorf("lambda has %d defaulted parameters of %d", count, len(parameters))
			}
			defaulted = int(count)
		}
		return value.Closure(&value.Lambda{
			Address:       int(int32(address)),
			Name:          name,
			SourceFile:    sourceFile,
			SourceLine:    int32(sourceLine),
			SourceColumn:  int32(sourceColumn),
			Params:        parameters,
			Locals:        locals,
			CapturesSlots: flags[0]&lambdaCapturesSlots != 0,
			CapturesNames: flags[0]&lambdaCapturesNames != 0,
			WritesNames:   flags[0]&lambdaWritesNames != 0,
			Rest:          flags[0]&lambdaRest != 0,
			Defaulted:     defaulted,
		}), nil
	default:
		return value.Value{}, fmt.Errorf("unsupported constant tag %d", tag)
	}
}
