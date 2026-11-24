package compiler_test

import (
	"strings"
	"testing"

	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// instructions lists a program's instructions as "NAME" or "NAME operand", and its constants' texts.
func instructions(t *testing.T, source string) ([]string, []string) {
	t.Helper()
	bytecode, err := compiler.CompileSource(source)
	if err != nil {
		t.Fatalf("compile %q: %v", source, err)
	}
	inspection, err := runtime.InspectProgram(bytecode.Program)
	if err != nil {
		t.Fatal(err)
	}
	var names, constants []string
	for _, instruction := range inspection.Instructions {
		name := instruction.Name
		if instruction.Name == "STEP" {
			name = "STEP " + string(rune('0'+instruction.Operands[0]))
		}
		names = append(names, name)
	}
	for _, constant := range inspection.Constants {
		constants = append(constants, constant.Preview)
	}
	return names, constants
}

func contains(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

// typeof x is one instruction, TYPEOF, and -x is STEP 3 (value.Negate): typeof was a method call INVOKE
// answered first (PUSH "#typeof"; INVOKE 0), and -x a multiplication (PUSH -1; MUL).
func TestTypeofAndNegationAreInstructionsOfTheirOwn(t *testing.T) {
	names, constants := instructions(t, `x = 2; t = typeof x; n = -x;`)
	if !contains(names, "TYPEOF") || contains(names, "INVOKE") || contains(constants, `"#typeof"`) {
		t.Errorf("typeof x compiled to %v with constants %v, want TYPEOF and no INVOKE", names, constants)
	}
	if !contains(names, "STEP 3") || contains(names, "MUL") || contains(constants, "-1") {
		t.Errorf("-x compiled to %v with constants %v, want STEP 3 and no MUL", names, constants)
	}
}

// COMMIT, which nothing emitted, is no instruction any more: a program holding its byte is refused as
// an unknown opcode. It kept its number (MOD + 1), which no new instruction takes.
func TestCommitIsRefused(t *testing.T) {
	constants := []value.Value{value.Number(1)}
	code := []byte{byte(runtime.PUSH), 0, 0, byte(runtime.MOD) + 1, byte(runtime.RETURN)}
	err := runtime.Verify(code, constants)
	if err == nil || !strings.Contains(err.Error(), "UNKNOWN_OPCODE") {
		t.Errorf("a program with COMMIT: %v, want UNKNOWN_OPCODE", err)
	}
	if _, known := runtime.LookupInstruction(runtime.MOD + 1); known {
		t.Errorf("COMMIT's slot still names an instruction")
	}
}

// A program cached by a runtime of bytecode 6, 7, 8 or 9 is refused, not read: 6 kept a function's variables in
// a map by name (STORE_LOCAL), which this runtime keeps in slots by index, 7 joined a template's parts by ADD,
// which TEMPLATE joins since bytecode 8, 8 has no spread of an array or a call (SPREAD, bytecode 9), and 9 read an
// array pattern by index (ITEMS, bytecode 10).
func TestAProgramOfBytecode6IsRefused(t *testing.T) {
	if runtime.BytecodeVersion != 10 {
		t.Fatalf("BytecodeVersion is %d, want 10", runtime.BytecodeVersion)
	}
	bytecode, err := compiler.CompileSource(`const f = (x) => { let y = x; return () => y; }; t = typeof f(1);`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bytecode.Program.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := runtime.UnmarshalProgram(encoded)
	if err != nil {
		t.Fatalf("this runtime's own envelope: %v", err)
	}
	if decoded.Checksum() != bytecode.Program.Checksum() {
		t.Errorf("the decoded program's checksum differs")
	}
	// the envelope: magic (4 bytes), encoding version (2), bytecode version (2)
	old := append([]byte(nil), encoded...)
	old[6], old[7] = 0, 6
	if _, err := runtime.UnmarshalProgram(old); err == nil || !strings.Contains(err.Error(), "bytecode version 6") {
		t.Errorf("an envelope of bytecode 6: %v, want it refused for its version", err)
	}
	old[6], old[7] = 0, 7
	if _, err := runtime.UnmarshalProgram(old); err == nil || !strings.Contains(err.Error(), "bytecode version 7") {
		t.Errorf("an envelope of bytecode 7: %v, want it refused for its version", err)
	}
	old[6], old[7] = 0, 8
	if _, err := runtime.UnmarshalProgram(old); err == nil || !strings.Contains(err.Error(), "bytecode version 8") {
		t.Errorf("an envelope of bytecode 8: %v, want it refused for its version", err)
	}
	old[6], old[7] = 0, 9
	if _, err := runtime.UnmarshalProgram(old); err == nil || !strings.Contains(err.Error(), "bytecode version 9") {
		t.Errorf("an envelope of bytecode 9: %v, want it refused for its version", err)
	}
}

// SPREAD, CALL_SPREAD, INVOKE_SPREAD and REST (bytecode 9) are verified as every instruction is: SPREAD's mode and
// REST's kind are 0 or 1, each takes the values it reads from the stack (REST of an object its keys and the value),
// and a program that holds them right is verified.
func TestTheSpreadInstructionsAreVerified(t *testing.T) {
	constants := []value.Value{value.Number(1), value.String("max")}
	good := []byte{byte(runtime.MAKE), 1, byte(runtime.PUSH), 0, 0, byte(runtime.SPREAD), byte(runtime.SpreadItem),
		byte(runtime.MAKE), 1, byte(runtime.CALL_SPREAD),
		byte(runtime.MAKE), 1, byte(runtime.PUSH), 0, 1, byte(runtime.INVOKE_SPREAD),
		byte(runtime.MAKE), 0, byte(runtime.PUSH), 0, 1, byte(runtime.REST), byte(runtime.RestObject), 1, byte(runtime.POP), byte(runtime.POP),
		byte(runtime.MAKE), 1, byte(runtime.REST), byte(runtime.RestArray), 2, byte(runtime.POP), byte(runtime.POP), byte(runtime.RETURN)}
	if err := runtime.Verify(good, constants); err != nil {
		t.Errorf("a program spreading right: %v", err)
	}
	for _, c := range []struct {
		name string
		code []byte
		want string
	}{
		{"a mode past SpreadItem", []byte{byte(runtime.MAKE), 1, byte(runtime.PUSH), 0, 0, byte(runtime.SPREAD), 2, byte(runtime.RETURN)}, "spread mode 2"},
		{"SPREAD over one value", []byte{byte(runtime.MAKE), 1, byte(runtime.SPREAD), 0, byte(runtime.RETURN)}, "UNDERFLOW"},
		{"CALL_SPREAD over one value", []byte{byte(runtime.MAKE), 1, byte(runtime.CALL_SPREAD), byte(runtime.RETURN)}, "UNDERFLOW"},
		{"INVOKE_SPREAD over two values", []byte{byte(runtime.MAKE), 1, byte(runtime.MAKE), 1, byte(runtime.INVOKE_SPREAD), byte(runtime.RETURN)}, "UNDERFLOW"},
		{"a rest kind past RestArray", []byte{byte(runtime.MAKE), 1, byte(runtime.REST), 2, 0, byte(runtime.RETURN)}, "rest kind 2"},
		{"an object's rest short of its keys", []byte{byte(runtime.MAKE), 0, byte(runtime.PUSH), 0, 1, byte(runtime.REST), byte(runtime.RestObject), 2, byte(runtime.RETURN)}, "UNDERFLOW"},
	} {
		if err := runtime.Verify(c.code, constants); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error saying %s", c.name, err, c.want)
		}
	}
}

// UNSET_SLOT and UNSET_NAME (bytecode 10) take nothing from the stack: a slot of the frame and a name's constant, a
// name's constant; a program that holds them right is verified, one past its frame or its constants refused.
func TestUnsetIsVerified(t *testing.T) {
	constants := []value.Value{value.String("x"), value.Number(1)}
	if err := runtime.Verify([]byte{byte(runtime.UNSET_NAME), 0, 0, 0, byte(runtime.RETURN)}, constants); err != nil {
		t.Errorf("UNSET_NAME of a name: %v", err)
	}
	for _, c := range []struct {
		name string
		code []byte
		want string
	}{
		{"a name past the constants", []byte{byte(runtime.UNSET_NAME), 0, 0, 2, byte(runtime.RETURN)}, "pool size 2"},
		{"a name that is a number", []byte{byte(runtime.UNSET_NAME), 0, 0, 1, byte(runtime.RETURN)}, "variable-name string"},
		{"a slot past the frame", []byte{byte(runtime.UNSET_SLOT), 0, 0, 0, 0, 0, 0, byte(runtime.RETURN)}, "slot 0 of a frame of 0 slots"},
	} {
		if err := runtime.Verify(c.code, constants); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error saying %s", c.name, err, c.want)
		}
	}
}

// ITEMS (bytecode 10) takes the value on top and leaves its items in its place: a program that gives it one is
// verified, one that gives it none is refused.
func TestItemsIsVerified(t *testing.T) {
	constants := []value.Value{value.String("ab")}
	if err := runtime.Verify([]byte{byte(runtime.PUSH), 0, 0, byte(runtime.ITEMS), byte(runtime.RETURN)}, constants); err != nil {
		t.Errorf("ITEMS over a value: %v", err)
	}
	if err := runtime.Verify([]byte{byte(runtime.ITEMS), byte(runtime.RETURN)}, constants); err == nil || !strings.Contains(err.Error(), "UNDERFLOW") {
		t.Errorf("ITEMS over nothing: %v, want UNDERFLOW", err)
	}
}
