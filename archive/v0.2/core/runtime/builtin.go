package runtime

import (
	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/value"
)

// sharedBuiltins are the builtins by their slot (builtins.Slots), the frozen set every run reads: what
// BUILTIN n reads when the host left vm.Builtins[n] empty or the table ends before it.
var sharedBuiltins = builtins.Slots()

// builtin is what BUILTIN index reads: the host's slot (vm.Builtins: kitwork in slot 0, a builtin it
// laid over the shared one, a site's console), else the shared builtin, else undefined. Until 04/11/2026
// the compiler emitted BUILTIN 0 alone and a program read every other builtin from its globals by name,
// three map lookups at the top level, as a function or a closure did through its chain of maps.
func (vm *VM) builtin(index int) value.Value {
	if index < len(vm.Builtins) {
		if item := vm.Builtins[index]; !item.IsUndefined() {
			return item
		}
	}
	if index < len(sharedBuiltins) {
		return sharedBuiltins[index]
	}
	return value.Undefined()
}

// builtinName is the name a program writes for BUILTIN index: kitwork for slot 0, else the builtin's.
func builtinName(index int) string {
	if names := builtins.Names(); index >= 1 && index <= len(names) {
		return names[index-1]
	}
	return "kitwork"
}
