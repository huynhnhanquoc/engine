package runtime

import (
	"context"

	"github.com/kitwork/engine2/core/value"
)

const (
	initialStackCapacity   = 1_024
	maxPooledStackCapacity = 4_096
	maxReusableVariables   = 1_024
	maxReusableDefers      = 64
	maxReusableGlobals     = 1_024
	maxReusableBuiltins    = 256
)

// Frame đại diện cho một khung thực thi (Activation Record)
type Frame struct {
	IP     int
	LastIP int
	// Slots are the variables of the function running, by the index the compiler gave each (bytecode 7):
	// its parameters first, then its let and const of every block, the objects its loops make for each
	// iteration and the compiler's temporaries; the root frame's are the top level's block variables. A
	// slot is cleared as the function starts (a parameter is bound), so a slot read before its write
	// reads undefined, never what the frame held before.
	Slots []value.Value
	// Vars are the variables kept by name: the root frame's is vm.Vars, the top level's own variables;
	// a function's holds the names it writes that nothing declares (var x = …), nil when it writes none.
	Vars   map[string]value.Value
	Fn     *value.Lambda   // Hàm đang được thực thi
	Defers []*value.Lambda // Deferred functions
	// slotsCaptured and varsCaptured are set when a closure made in this frame holds its Slots or its
	// Vars (makeClosure): the frame's next function, and a reset, then make new ones and leave the old
	// to the closure, which would otherwise lose its variables (or read another call's) once the frame is
	// used again.
	slotsCaptured bool
	varsCaptured  bool
	StackBase     int // Stack depth when the function call started
}

type VM struct {
	Context   context.Context // Request execution context (for cancellation)
	program   *Program
	Stack     []value.Value
	Vars      map[string]value.Value // Biến của từng Request (Cho phép ghi)
	Globals   map[string]value.Value // Chung cho toàn bộ Host (Chỉ đọc)
	Builtins  []value.Value          // Mảng các hàm hệ thống (Siêu nhanh - Index lookup)
	Frames    []Frame                // Call Stack
	FrameIdx  int                    // Hiện tại đang ở Frame nào
	Energy    uint64                 // Năng lượng tiêu thụ
	MaxEnergy uint64                 // Per-execution energy ceiling; zero disables energy enforcement.
	Bytes     uint64                 // What the run made (memory.go)
	MaxBytes  uint64                 // Per-execution ceiling of what the run makes; zero disables it.
	Pool      *Pool                  // What every run of the host makes together (pool.go); nil for no ceiling.
	Spawner   func(s *value.Lambda)

	instructions   uint64
	frameHighWater int
	// taken is what the run took from Pool, given back as its outermost run ends (entered back to 0); hostFull marks
	// a refusal of the pool for the failure the meter makes (pool.go)
	taken          uint64
	counted        uint64 // Bytes when the run was last counted by what it holds (pool.go, cover)
	hostFull       bool
	entered        int
	stackHighWater int

	reusableGlobals  map[string]value.Value
	reusableBuiltins []value.Value
	hostStateOwned   bool

	// Nesting (nesting.go): nested is what VMs running inside this one's host calls see; outer and
	// callFloor belong to this VM's own run inside another, while NestIn holds; passing lists the
	// lambdas this VM's host calls in progress were passed, contextShown the Context it last showed.
	nested       nesting
	outer        *nestLink
	callFloor    int // frames held by the VMs this one runs inside
	passing      []*value.Lambda
	contextShown context.Context
	// topUnlisted: the last of passing is a closure this VM made and has no entry in the registry of
	// callers, which runClosure and LambdaCaller do without while it is the last (passLambdas).
	topUnlisted bool

	// origin is what the closures this VM makes keep of where they were made (origin.go).
	origin *closureOrigin

	// textCursor reads s[i] and s.length on from where the last read of the same string stood (GET,
	// ITER), made at the VM's first such read (cursor). It forgets the string as the VM resets, so a run
	// never holds the last run's text. It is a pointer: held whole, its 900 bytes made the VM's own loops
	// 2-5% slower (measured 04/11/2026).
	textCursor *value.TextCursor
	// joiner holds the texts the run builds by + and templates, with room to grow in place (value/join.go); a
	// pointer, as textCursor, so the VM's own fields stay as small as they were
	joiner *value.Joiner

	// stop is the diagnostic that ends this run whatever catches the failure it returns: the energy
	// limit, a stack overflow, cancellation, a native panic, a mismatched program. It is recorded the
	// moment it happens; the failure value (value.CancelledCode or value.FailCode) goes up the Go stack,
	// and the instruction loop reads stop after every CALL and INVOKE, so a safe that wrapped that
	// failure into { ok: false } does not keep the run going.
	stop *Diagnostic

	// runLambda is the Run of every closure this VM makes, made once in New: a closure then costs its
	// Lambda alone (interpreter.go, makeClosure). It runs the closure on the VM calling it (runClosure).
	runLambda func(lambda *value.Lambda, arguments []value.Value) value.Value
	// kept is what Go code of the engine holds for this run while program code it called runs (value.Kept,
	// memory.go): the array flatMap walks and the items it made so far, which holding counts.
	kept []keptValue
	// runHere runs a lambda on this VM: the run of a stand-in for a function another VM made that this
	// VM hands to a host call (passLambdas, value.RunningOn), made at the first stand-in of each lease,
	// runHereLease, which it runs for (leasedRun).
	runHere      func(lambda *value.Lambda, arguments []value.Value) value.Value
	runHereLease uint64
	// handHere is standIn of the lease handHereLease, which a Go function this VM hands to a host passes
	// the functions it is called with through (leasedHand): it runs them in that lease alone.
	handHere      func(argument value.Value) value.Value
	handHereLease uint64

	// frozen is set by Freeze: what the VM's run made is shared and only read, and the VM runs nothing
	// more (freeze.go).
	frozen bool
}

// VMStats is a point-in-time execution snapshot. Read it after execution; a VM
// is intentionally single-owner and is not safe for concurrent inspection.
type VMStats struct {
	Instructions   uint64
	Energy         uint64
	StackDepth     int
	StackCapacity  int
	PeakStackDepth int
	FrameDepth     int
	PeakFrameDepth int
}

func New(program *Program) *VM {
	vm := &VM{
		program: program,
		Stack:   make([]value.Value, 0, initialStackCapacity),
		Vars:    make(map[string]value.Value),
		Globals: make(map[string]value.Value),
		Frames:  make([]Frame, defaultMaxCallDepth),
	}
	vm.runLambda = vm.runClosure
	vm.joiner = &value.Joiner{}
	vm.joiner.Spare = vm.spareBytes
	// Khởi tạo Frame gốc (Main entry)
	vm.FrameIdx = 0
	vm.Frames[0] = Frame{IP: 0, Vars: vm.Vars, StackBase: 0} // TRANG BỊ VŨ KHÍ: Frame 0 chính là vm.Vars
	vm.Frames[0].LastIP = -1
	vm.Frames[0].Slots = make([]value.Value, program.rootSlots())
	return vm
}

// FastReset loads a new Program and caller-owned globals. It does not clear the
// execution context; pool hygiene belongs to ResetForPool. Production request
// paths use PrepareHostState followed by FastResetPrepared so container
// ownership remains explicit.
func (vm *VM) FastReset(program *Program, globals map[string]value.Value) {
	vm.resetExecution(program)
	vm.Globals = globals
	vm.hostStateOwned = false
}

// FastResetPrepared resets execution while retaining the VM-owned host state
// installed by PrepareHostState.
func (vm *VM) FastResetPrepared(program *Program) {
	vm.resetExecution(program)
}

func (vm *VM) resetExecution(program *Program) {
	vm.program = program
	vm.resetStack()
	if vm.textCursor != nil {
		vm.textCursor.Reset()
	}
	if vm.joiner != nil {
		vm.joiner.Forget() // no buffer of the last run's texts is written again, nor held by a pooled VM
	}
	vm.FrameIdx = 0
	vm.Energy = 0
	vm.giveBack() // what a run that did not end through Run or ExecuteLambda still holds of the pool
	vm.Bytes = 0
	vm.counted = 0
	vm.hostFull = false
	vm.instructions = 0
	vm.stop = nil
	clear(vm.kept) // a run stopped inside a host call let go of nothing it kept
	vm.kept = vm.kept[:0]

	for index := 1; index <= vm.frameHighWater; index++ {
		resetFrame(&vm.Frames[index])
	}
	vm.frameHighWater = 0

	root := &vm.Frames[0]
	if root.varsCaptured || len(vm.Vars) > maxReusableVariables {
		// A top-level closure still owns the old map. Detach the VM instead of
		// clearing data that escaped with that closure. The new map is made for
		// as many names as the last run held (a program's top level usually
		// declares the same ones each run), so it does not grow step by step.
		vm.Vars = make(map[string]value.Value, min(len(vm.Vars), maxReusableVariables))
		root.varsCaptured = false
	} else {
		clear(vm.Vars)
	}
	// The top level's slots, as the map: detached when a closure holds them, else cleared whole (the
	// last program may have had more), so no slot shows what the last run (another app's, maybe) left.
	slots := program.rootSlots()
	if root.slotsCaptured || cap(root.Slots) < slots || cap(root.Slots) > maxReusableVariables {
		root.Slots = make([]value.Value, slots)
		root.slotsCaptured = false
	} else {
		clear(root.Slots[:cap(root.Slots)])
		root.Slots = root.Slots[:slots]
	}

	// Đồng bộ lại Frame gốc
	root.IP = 0
	root.LastIP = -1
	root.Vars = vm.Vars
	root.Fn = nil
	resetDefers(root)
	root.StackBase = 0
}

// PrepareHostState copies one owner's host environment into VM-owned reusable
// storage. ResetForPool clears every value before retaining only the empty
// containers, so the pool can reduce allocations without exposing references
// from the previous app.
func (vm *VM) PrepareHostState(
	globals map[string]value.Value,
	builtins []value.Value,
) {
	if vm == nil {
		return
	}

	ownedGlobals := vm.reusableGlobals
	vm.reusableGlobals = nil
	if ownedGlobals == nil {
		ownedGlobals = make(map[string]value.Value, len(globals)+1)
	} else {
		clear(ownedGlobals)
	}
	for name, item := range globals {
		ownedGlobals[name] = item
	}
	vm.Globals = ownedGlobals

	ownedBuiltins := vm.reusableBuiltins
	vm.reusableBuiltins = nil
	if cap(ownedBuiltins) < len(builtins) {
		ownedBuiltins = make([]value.Value, len(builtins))
	} else {
		ownedBuiltins = ownedBuiltins[:len(builtins)]
	}
	copy(ownedBuiltins, builtins)
	vm.Builtins = ownedBuiltins
	vm.hostStateOwned = true
}

// ResetForPool drops tenant-owned references before a VM becomes visible to
// another app. Captured frame slots and maps are detached, never cleared.
//
// Its energy ceiling goes back to the default (Limits().DefaultMaxEnergy), not to zero, which is no
// ceiling at all: a host that takes the VM from the pool and runs it without setting MaxEnergy runs
// within the default, never without a bound (05/11/2026).
//
// The frames are reset by resetExecution, up to the highest the run used (frameHighWater): a frame is
// pushed only by prepareLambdaFrame, which raises the mark, so none above it holds anything. Until
// 05/11/2026 every frame of the call stack (63) was reset again here, a fifth of a small request's run.
func (vm *VM) ResetForPool() {
	globals := vm.Globals
	builtins := vm.Builtins
	hostStateOwned := vm.hostStateOwned
	vm.resetExecution(nil)
	vm.Globals = nil
	vm.Context = nil // a pooled VM must never inherit the previous owner's request context
	vm.Builtins = nil
	vm.MaxEnergy = defaultMaxEnergy
	vm.MaxBytes = defaultMaxBytes // as energy's: never no ceiling for a VM from the pool
	vm.giveBack()
	vm.Pool = nil
	vm.Spawner = nil
	vm.hostStateOwned = false
	vm.resetNesting()

	if hostStateOwned && len(globals) <= maxReusableGlobals {
		clear(globals)
		vm.reusableGlobals = globals
	} else if hostStateOwned {
		vm.reusableGlobals = nil
	}
	if hostStateOwned && cap(builtins) <= maxReusableBuiltins {
		clear(builtins)
		vm.reusableBuiltins = builtins[:0]
	} else if hostStateOwned {
		vm.reusableBuiltins = nil
	}
}

// Program reports the immutable program currently loaded by the VM.
func (vm *VM) Program() *Program {
	if vm == nil {
		return nil
	}
	return vm.program
}

func (vm *VM) Stats() VMStats {
	if vm == nil {
		return VMStats{}
	}
	frameDepth := 0
	if vm.FrameIdx >= 0 {
		frameDepth = vm.FrameIdx + 1
	}
	return VMStats{
		Instructions:   vm.instructions,
		Energy:         vm.Energy,
		StackDepth:     len(vm.Stack),
		StackCapacity:  cap(vm.Stack),
		PeakStackDepth: vm.stackHighWater,
		FrameDepth:     frameDepth,
		PeakFrameDepth: vm.frameHighWater + 1,
	}
}

func (vm *VM) Stop() {
	vm.FrameIdx = -1
}

func (vm *VM) resetStack() {
	if cap(vm.Stack) > maxPooledStackCapacity {
		vm.Stack = make([]value.Value, 0, initialStackCapacity)
		vm.stackHighWater = 0
		return
	}
	used := vm.stackHighWater
	if len(vm.Stack) > used {
		used = len(vm.Stack)
	}
	if used > cap(vm.Stack) {
		used = cap(vm.Stack)
	}
	clear(vm.Stack[:used])
	vm.Stack = vm.Stack[:0]
	vm.stackHighWater = 0
}

// resetFrame empties a frame between runs: its slots and its map are detached when a closure holds
// them, else cleared (the slots to their whole capacity, which a bigger function may have used), so a
// slot read before its write in the next run never shows this run's value.
func resetFrame(frame *Frame) {
	frame.IP = 0
	frame.LastIP = -1
	frame.Fn = nil
	frame.StackBase = 0

	if frame.slotsCaptured || cap(frame.Slots) > maxReusableVariables {
		frame.Slots = nil
	} else {
		clear(frame.Slots[:cap(frame.Slots)])
		frame.Slots = frame.Slots[:0]
	}
	if frame.varsCaptured || len(frame.Vars) > maxReusableVariables {
		frame.Vars = nil
	} else {
		clear(frame.Vars)
	}
	frame.slotsCaptured = false
	frame.varsCaptured = false
	resetDefers(frame)
}

func resetDefers(frame *Frame) {
	if cap(frame.Defers) > maxReusableDefers {
		frame.Defers = nil
		return
	}
	clear(frame.Defers)
	frame.Defers = frame.Defers[:0]
}

// Helper methods for Stack manipulation
func (vm *VM) push(v value.Value) {
	vm.Stack = append(vm.Stack, v)
	if len(vm.Stack) > vm.stackHighWater {
		vm.stackHighWater = len(vm.Stack)
	}
}

func (vm *VM) pop() value.Value {
	var base int
	if vm.FrameIdx >= 0 && vm.FrameIdx < len(vm.Frames) {
		base = vm.Frames[vm.FrameIdx].StackBase
	}
	if len(vm.Stack) <= base {
		return value.Undefined()
	}
	v := vm.Stack[len(vm.Stack)-1]
	vm.Stack = vm.Stack[:len(vm.Stack)-1]
	return v
}

func (vm *VM) peek() value.Value {
	var base int
	if vm.FrameIdx >= 0 && vm.FrameIdx < len(vm.Frames) {
		base = vm.Frames[vm.FrameIdx].StackBase
	}
	if len(vm.Stack) <= base {
		return value.Undefined()
	}
	return vm.Stack[len(vm.Stack)-1]
}
