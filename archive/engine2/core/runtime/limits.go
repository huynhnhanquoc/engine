package runtime

const (
	// MaxProgramBinarySize bounds the complete persisted Program envelope. Its debug table writes each
	// entry's file name, so a route bundle's envelope is about 15 times its code (apptop.vn 14/10/2026:
	// 993,905 bytes for 65,038), and 64 MiB holds such a bundle at MaxBytecodeSize. It does not hold every
	// program under that limit: a module with a position every 2 or 3 bytes of code, on a path of some 70
	// characters, passes it first. The compiler refuses such a program where it crosses
	// (Program.EncodedSize, 15/10/2026; until then it ran, but was never cached and failed every check).
	MaxProgramBinarySize = 64 << 20
	// MaxBytecodeSize is the largest verified instruction stream in one Program: 2 MiB (bytecode 3,
	// 14/10/2026; it was 65,535, the reach of a two-byte jump, and apptop.vn's largest route compiled
	// to 65,038). A jump in a program longer than MaxNarrowOperand is written wide (three bytes).
	MaxBytecodeSize = 2 << 20
	// MaxConstants is the largest immutable constant pool in one Program. An index above
	// MaxNarrowOperand is written wide; a program at MaxBytecodeSize references at most about half a
	// million constants, so the code limit is met first.
	MaxConstants = 1 << 20
	// MaxNarrowOperand is the largest constant index or jump address a two-byte operand holds. The
	// compiler writes the wide form of an instruction only past it.
	MaxNarrowOperand = 1<<16 - 1
	// maxWideOperand is the largest a three-byte operand holds.
	maxWideOperand = 1<<24 - 1
	// MaxStackDepth bounds the verifier-proven operand depth of one entry point.
	MaxStackDepth = 1<<16 - 1
	// MaxLocals bounds the slots of one function's frame, or of the top level's (bytecode 7): one per
	// variable, iteration object and temporary of the compiler, as many as a constant pool holds. An index
	// past MaxNarrowOperand is written wide.
	MaxLocals = 1 << 20
	// freeSlots is how many slots a function's frame holds before the call pays for them: a call of a
	// function of more slots spends one unit of energy for each past these as it starts, since its frame
	// is made (or cleared) whole whatever the function runs. Without it, 64 frames of a function of
	// 300,000 variables would cost a few hundred megabytes for a few calls.
	freeSlots = 256

	defaultMaxCallDepth         = 64
	defaultMaxEnergy     uint64 = 10_000_000
	cleanupEnergyReserve uint64 = 10_000
	// defaultMaxBytes is what a run makes at most (memory.go): the pages of kitwork.io and the demo make 0.14 to
	// 0.69 MB a request, Go's own count with the engine's buffers (12/11/2026), about a hundredth of it.
	defaultMaxBytes uint64 = 64 << 20
	// cleanupBytesReserve is what the defers of a run stopped by its bytes may still make as it unwinds.
	cleanupBytesReserve       uint64 = 1 << 20
	cancellationCheckInterval uint64 = 64
	cancellationCheckMask            = cancellationCheckInterval - 1
)

// A wide operand reaches every address and constant index these limits allow: raising a limit past
// three bytes is a compile error here, not a jump that silently lands short.
const (
	_ = uint32(maxWideOperand - MaxBytecodeSize)
	_ = uint32(maxWideOperand + 1 - MaxConstants)
)

// LimitsSnapshot is the immutable, detached resource policy enforced by the
// Program decoder, verifier, and VM. MaxEnergy remains host-configurable per
// execution; DefaultMaxEnergy is the host fallback, not an implicit VM limit.
type LimitsSnapshot struct {
	MaxProgramBinaryBytes     int    `json:"max_program_binary_bytes"`
	MaxBytecodeBytes          int    `json:"max_bytecode_bytes"`
	MaxConstants              int    `json:"max_constants"`
	MaxVerifiedStackDepth     int    `json:"max_verified_stack_depth"`
	MaxCallDepth              int    `json:"max_call_depth"`
	DefaultMaxEnergy          uint64 `json:"default_max_energy"`
	CleanupEnergyReserve      uint64 `json:"cleanup_energy_reserve"`
	DefaultMaxBytes           uint64 `json:"default_max_bytes"`
	CancellationCheckInterval uint64 `json:"cancellation_check_interval"`
}

// Limits returns a read-only policy snapshot. There is deliberately no
// mutation API: tenant code cannot widen the runtime's structural limits.
func Limits() LimitsSnapshot {
	return LimitsSnapshot{
		MaxProgramBinaryBytes:     MaxProgramBinarySize,
		MaxBytecodeBytes:          MaxBytecodeSize,
		MaxConstants:              MaxConstants,
		MaxVerifiedStackDepth:     MaxStackDepth,
		MaxCallDepth:              defaultMaxCallDepth,
		DefaultMaxEnergy:          defaultMaxEnergy,
		CleanupEnergyReserve:      cleanupEnergyReserve,
		DefaultMaxBytes:           defaultMaxBytes,
		CancellationCheckInterval: cancellationCheckInterval,
	}
}
