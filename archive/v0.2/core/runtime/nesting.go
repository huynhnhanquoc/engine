package runtime

import (
	"context"
	"fmt"
	"hash/maphash"
	"sync"
	"sync/atomic"

	"github.com/kitwork/engine2/core/value"
)

// A VM can run inside a host call of another VM: the query builder runs a predicate to read it,
// db.transaction runs its callback, and either can start another one. That VM is nested. It has no
// budget of its own; it draws on the VM it runs inside, so a request is one budget however deep its
// callbacks go.
//
//   - Energy: a nested VM may spend what the VM it runs inside has left, and what it spent is added
//     to that VM's own energy when the host call returns.
//   - Call depth: the frames held by the VMs it runs inside count against the same 64-frame ceiling,
//     its own root frame included. Each nesting level is Go stack as well, so nesting without a
//     bound would end the process.
//   - Cancellation: a nested VM without a context of its own takes the outer VM's.
//   - A failure that ends a run (energy, cancellation, stack overflow, a Go panic: every failure but
//     a runtime error the host may handle) ends the outer VM too, as the host call returns, whatever
//     the host did with the nested result. A query builder that ignores a predicate's result cannot
//     hide it, and safe(() => …) cannot catch it, exactly as in the outer VM itself.
//
// The host finds the outer VM with LambdaCaller: a host call records, while it runs, which VM passed
// each lambda among its arguments. A capability object knows no VM (one imported at module level is
// built before any request), but the lambda it runs is the one it was handed.
//
// The outer VM usually waits in its host call on the same goroutine, but a host object can reach
// another goroutine (a background task holding the request's database), so the VMs exchange nothing
// but atomics, and every exchange names the outer VM's lease: a VM back in the pool never receives
// what was meant for its previous owner.

const (
	// shownEnergyBits is the width of the remaining energy in nesting.shown; the call depth is above it.
	shownEnergyBits = 56
	// shownEnergyMask is both the mask of the remaining energy and its value for a VM without a ceiling.
	shownEnergyMask = uint64(1)<<shownEnergyBits - 1
	// shownDepthLimit is the largest call depth nesting.shown can carry.
	shownDepthLimit = 1<<(64-shownEnergyBits) - 1
	// nestingWalkLimit bounds the walk to the innermost VM. The call depth bounds real nesting well below it.
	nestingWalkLimit = defaultMaxCallDepth + 1
	// nestingLinkAttempts bounds how often NestIn retries when another goroutine links first.
	nestingLinkAttempts = 8
)

// nesting is the side of a VM that VMs nested in its host calls see. Only atomics: see above.
type nesting struct {
	// lease changes every time the VM goes back to the pool.
	lease atomic.Uint64
	// shown is what the VM had as of its latest host call: the energy it has left in the low
	// shownEnergyBits (shownEnergyMask when it has no ceiling) and above them the frames in use, its
	// own and those of the VMs it runs inside. Zero is nothing shown yet in this lease (a VM that
	// shows has at least its root frame in use).
	shown atomic.Uint64
	// shownBytes is the bytes the VM had left to make as of its latest host call (memory.go), noByteCeiling when it
	// has no ceiling; read only once shown is not zero.
	shownBytes atomic.Uint64
	// returned is what VMs nested in its host calls handed back that the VM has not taken yet.
	returned atomic.Pointer[handBack]
	// inner is the VM running inside the VM's current host call.
	inner atomic.Pointer[nestLink]
	// context is the VM's Context as of its latest host call that passed a lambda, for a nested VM
	// without one (VM.contextShown keeps it, so a new one is shown only when it changes).
	context atomic.Pointer[shownContext]
	// passed is the last lambda the VM's host calls in progress were passed (the top of VM.passing),
	// nil when none: runClosure's quick answer for the callback of a host call the VM that made it is
	// making, without the lock of LambdaCaller.
	passed atomic.Pointer[value.Lambda]
}

type shownContext struct{ context context.Context }

// nestLink records that inner runs inside a host call of outer, each named with its lease.
type nestLink struct {
	outer       *VM
	outerLease  uint64
	inner       *VM
	innerLease  uint64
	startEnergy uint64
	startBytes  uint64
}

// handBack is what nested VMs return to the VM they ran inside: the energy they spent, the bytes they made and the
// first failure that ends a run, for one lease of that VM.
type handBack struct {
	lease  uint64
	energy uint64
	bytes  uint64
	fault  *Diagnostic
}

// NestIn makes vm run inside the current host call of outer, or of the VM nested deepest inside
// outer: a callback can start another. Call it once vm is prepared for its run (reset, MaxEnergy set)
// and before the run, then Unnest when the run ends. vm then spends at most what that VM has left and
// holds frames only below the ceiling that VM leaves. NestIn does nothing when outer is nil or vm is
// nested already.
func (vm *VM) NestIn(outer *VM) {
	if vm == nil || outer == nil || outer == vm || vm.outer != nil {
		return
	}

	innerLease := vm.nested.lease.Load()
	var link *nestLink
	for attempt := 0; attempt < nestingLinkAttempts; attempt++ {
		host, hostLease, current := innermost(outer)
		link = &nestLink{
			outer:       host,
			outerLease:  hostLease,
			inner:       vm,
			innerLease:  innerLease,
			startEnergy: vm.Energy,
			startBytes:  vm.Bytes,
		}
		if host.nested.inner.CompareAndSwap(current, link) {
			break
		}
		// Another goroutine nested a VM in host meanwhile; with the last attempt this VM still draws on
		// host, it is only not found by VMs nested deeper (they draw on host as well).
	}
	vm.outer = link
	if vm.Context == nil {
		if shown := link.outer.nested.context.Load(); shown != nil {
			vm.Context = shown.context
		}
	}

	shown := link.outer.nested.shown.Load()
	if shown == 0 {
		// The outer VM has shown nothing in this lease (no host call yet, so this VM cannot be running
		// inside one of them): nothing to narrow by. What this VM spends still goes back to it.
		vm.showHost()
		return
	}
	vm.callFloor = int(shown >> shownEnergyBits)
	if remaining := shown & shownEnergyMask; remaining != shownEnergyMask {
		ceiling := vm.Energy + remaining
		if ceiling < vm.Energy {
			ceiling = ^uint64(0)
		}
		if vm.MaxEnergy == 0 || ceiling < vm.MaxEnergy {
			vm.MaxEnergy = ceiling
		}
		if vm.MaxEnergy == 0 {
			// Nothing is left to lend, and zero would mean no ceiling at all: the first instruction
			// fails on energy, as it would in the outer VM.
			vm.MaxEnergy, vm.Energy = 1, 1
		}
	}
	// the bytes the outer VM has left to make, lent as its energy is (memory.go)
	if remaining := link.outer.nested.shownBytes.Load(); remaining != noByteCeiling {
		ceiling := vm.Bytes + remaining
		if ceiling < vm.Bytes {
			ceiling = ^uint64(0)
		}
		if vm.MaxBytes == 0 || ceiling < vm.MaxBytes {
			vm.MaxBytes = ceiling
		}
		if vm.MaxBytes == 0 {
			// nothing left to lend, and zero would be no ceiling: the first thing made fails
			vm.MaxBytes, vm.Bytes = 1, 1
		}
	}
	vm.showHost()
}

// Unnest ends what NestIn began: the energy vm spent goes back to the VM it ran inside, and the stop
// that ended vm's run with it (vm.stop: a failure that ends a run, whatever caught its value), so that VM
// fails with it as its host call returns. result is what the run returned.
func (vm *VM) Unnest(result value.Value) {
	if vm == nil || vm.outer == nil {
		return
	}
	link := vm.outer
	vm.outer = nil
	vm.callFloor = 0

	// A VM nested in this one may have handed back after this one's latest host call returned; a stop
	// it handed back is vm's own stop then.
	if vm.nested.returned.Load() != nil {
		vm.takeBack(result)
	}
	fault := vm.stop
	link.outer.nested.inner.CompareAndSwap(link, nil)

	spent := uint64(0)
	if vm.Energy > link.startEnergy {
		spent = vm.Energy - link.startEnergy
	}
	made := uint64(0)
	if vm.Bytes > link.startBytes {
		made = vm.Bytes - link.startBytes
	}
	link.outer.receive(link.outerLease, spent, made, fault)
}

// checkpoint runs every cancellationCheckInterval instructions: a cancelled context ends the run; what
// nested VMs handed back outside a host call (one started by a property read, or on another
// goroutine) is taken; and the VM shows again what it has left, so that such a VM never sees
// figures older than one interval.
func (vm *VM) checkpoint(ip int) (value.Value, bool) {
	if vm.Context != nil {
		if err := vm.Context.Err(); err != nil {
			return vm.diagnosticValue(
				DiagnosticCancelled,
				fmt.Sprintf("Execution Cancelled: %v", err),
				ip,
			), true
		}
	}
	if vm.nested.returned.Load() != nil {
		if failure := vm.takeBack(value.Undefined()); failure.IsInvalid() {
			return failure, true
		}
	}
	// what the run made past what it took from the host's pool, taken now (pool.go)
	if vm.Pool != nil && !vm.covered(0) && !vm.cover(0) {
		return vm.memoryLimit(ip), true
	}
	vm.showHost()
	return value.Undefined(), false
}

// innermost follows the inner links from outer to the VM nested deepest inside it, the one whose host
// call runs now, and returns it with its lease and its current inner link (nil, or one to replace).
// A link is not followed once either of its VMs has gone back to the pool.
func innermost(outer *VM) (*VM, uint64, *nestLink) {
	host, lease := outer, outer.nested.lease.Load()
	for range nestingWalkLimit {
		link := host.nested.inner.Load()
		if link == nil ||
			link.outerLease != lease ||
			link.inner.nested.lease.Load() != link.innerLease {
			return host, lease, link
		}
		host, lease = link.inner, link.innerLease
	}
	return host, lease, host.nested.inner.Load()
}

// showHost tells the VMs that may run inside the coming host call what this VM has left.
func (vm *VM) showHost() {
	remaining := shownEnergyMask
	if vm.MaxEnergy > 0 {
		remaining = 0
		if vm.Energy < vm.MaxEnergy {
			remaining = min(vm.MaxEnergy-vm.Energy, shownEnergyMask-1)
		}
	}
	bytes := noByteCeiling
	if vm.MaxBytes > 0 {
		bytes = 0
		if vm.Bytes < vm.MaxBytes {
			bytes = vm.MaxBytes - vm.Bytes
		}
	}
	// the bytes first: a VM that sees shown reads shownBytes of the same host call or a later one
	vm.nested.shownBytes.Store(bytes)
	depth := min(max(vm.callFloor+vm.FrameIdx+1, 1), shownDepthLimit) // never 0: see nesting.shown
	vm.nested.shown.Store(uint64(depth)<<shownEnergyBits | remaining)
}

// receive keeps what a nested VM handed back to lease of vm until vm takes it.
func (vm *VM) receive(lease, energy, bytes uint64, fault *Diagnostic) {
	if energy == 0 && bytes == 0 && fault == nil {
		return
	}
	for {
		current := vm.nested.returned.Load()
		next := &handBack{lease: lease, energy: energy, bytes: bytes, fault: fault}
		if current != nil && current.lease == lease {
			next.energy = current.energy + energy
			if next.energy < current.energy {
				next.energy = ^uint64(0)
			}
			next.bytes = current.bytes + bytes
			if next.bytes < current.bytes {
				next.bytes = ^uint64(0)
			}
			if current.fault != nil {
				next.fault = current.fault
			}
		}
		if vm.nested.returned.CompareAndSwap(current, next) {
			return
		}
	}
}

// takeBack adds to vm what the VMs nested in its host calls handed back. result is what the host
// call returned; takeBack returns it, or the failure that replaces it: a nested VM's failure that ends
// a run, or the energy limit when what they spent leaves vm past its own. Either is vm's stop from then
// on. A failure the host call returned beside a nested stop is dropped: a failure value carries no
// Diagnostic to add to the stop's Suppressed.
func (vm *VM) takeBack(result value.Value) value.Value {
	back := vm.nested.returned.Swap(nil)
	if back == nil || back.lease != vm.nested.lease.Load() {
		return result
	}
	if ^uint64(0)-vm.Energy < back.energy {
		vm.Energy = ^uint64(0)
	} else {
		vm.Energy += back.energy
	}
	if ^uint64(0)-vm.Bytes < back.bytes {
		vm.Bytes = ^uint64(0)
	} else {
		vm.Bytes += back.bytes
	}
	switch {
	case back.fault != nil:
		return vm.nestedFailure(back.fault)
	case vm.MaxEnergy > 0 && vm.Energy > vm.MaxEnergy:
		return vm.diagnosticValue(
			DiagnosticEnergyLimit,
			"Energy Limit Exceeded: Execution halted",
			vm.currentInstruction(),
		)
	case vm.MaxBytes > 0 && vm.Bytes > vm.MaxBytes:
		// what the nested runs made leaves vm past its own ceiling (memory.go)
		return vm.memoryLimit(vm.currentInstruction())
	}
	return result
}

// nestedFailure is fault, the failure that ended a VM nested in vm's current host call, as vm's own:
// the same code and message, the nested frames first and vm's below them, so the whole way down shows.
// It is vm's stop from then on.
func (vm *VM) nestedFailure(fault *Diagnostic) value.Value {
	own := vm.buildDiagnostic(fault.Code, fault.Message, vm.currentInstruction())
	merged := *fault
	merged.Stack = append(append([]StackFrame(nil), fault.Stack...), own.Stack...)
	merged.Suppressed = append([]*Diagnostic(nil), fault.Suppressed...)
	if len(fault.Stack) == 0 {
		merged.IP = own.IP
		merged.File = own.File
		merged.Line = own.Line
		merged.Column = own.Column
		merged.Function = own.Function
	}
	vm.stopWith(&merged)
	return diagnosticResult(&merged)
}

// resetNesting drops every tie of a VM going back to the pool. The new lease makes whatever still
// names the old one (a link, a hand-back) inert.
func (vm *VM) resetNesting() {
	vm.withdrawLambdas(0) // none after a normal run; an interpreter panic can leave some
	if cap(vm.passing) > maxReusableDefers {
		vm.passing = nil
	}
	vm.nested.lease.Add(1)
	vm.nested.shown.Store(0)
	vm.nested.shownBytes.Store(noByteCeiling)
	vm.nested.returned.Store(nil)
	vm.nested.inner.Store(nil)
	vm.nested.context.Store(nil)
	vm.nested.passed.Store(nil)
	vm.contextShown = nil
	vm.outer = nil
	vm.callFloor = 0
}

// lambdaCallers records, for each lambda passed to a host call in progress, the VMs that passed it.
// An entry lives exactly as long as its host call; shards keep unrelated calls off one lock.
var (
	lambdaCallers    [lambdaCallerShards]lambdaCallerShard
	lambdaCallerSeed = maphash.MakeSeed()
)

const lambdaCallerShards = 64

type lambdaCallerShard struct {
	mu      sync.Mutex
	callers map[*value.Lambda][]*VM
}

func lambdaShard(lambda *value.Lambda) *lambdaCallerShard {
	return &lambdaCallers[maphash.Comparable(lambdaCallerSeed, lambda)%lambdaCallerShards]
}

// LambdaCaller is the VM that passed lambda to a host call in progress, the VM whose budget a host
// running lambda in a VM of its own nests it in (NestIn); nil when no host call in progress was
// passed lambda as an argument. When several VMs were (one lambda value used by several requests at
// once), it is the one with the most frames in use: nested there, a VM has no more room than in its
// real caller, so nesting stays bounded either way. The VM that made lambda is one of them when the
// last lambda its host calls in progress were passed is lambda, which the registry does not list
// (passLambdas).
func LambdaCaller(lambda *value.Lambda) *VM {
	if lambda == nil {
		return nil
	}
	var deepest *VM
	var depth uint64
	consider := func(caller *VM) {
		if callerDepth := caller.nested.shown.Load() >> shownEnergyBits; deepest == nil || callerDepth > depth {
			deepest, depth = caller, callerDepth
		}
	}
	if made := maker(lambda); made != nil && made.nested.passed.Load() == lambda {
		consider(made)
	}
	shard := lambdaShard(lambda)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	for _, caller := range shard.callers[lambda] {
		consider(caller)
	}
	return deepest
}

// passLambdas records vm as the caller of the lambdas among args for the host call it is starting,
// and shows its Context for the VMs that may run them. It returns the mark withdrawLambdas takes the
// records back to when the call returns.
//
// A closure vm made, passed last, gets no entry in the shared registry of callers (topUnlisted):
// runClosure finds it as nested.passed, and LambdaCaller through its maker, without the registry's lock
// and map, which were most of what a call of an array method with a callback cost beside its callbacks
// (04/11/2026). It is listed as soon as another lambda is passed above it, by this call or one inside
// it, so every other lambda of passing is listed.
//
// A closure another VM made is handed to the host as a stand-in that runs on vm (value.RunningOn), the
// VM making the host call on this goroutine (05/11/2026): its maker may be a site's boot VM, frozen and
// shared by every request at once (Freeze), or another request's VM, and the registry cannot tell which
// of the VMs that passed one shared function calls it on this goroutine. Before, a function of a file's
// top level given to sort by two requests at once ran on whichever the registry named first, from both
// goroutines. args is the host's window on the stack, so the stand-in takes the argument's place. A Go
// function among args is handed over as a stand-in too (value.HandingOver), which hands each function it
// is called with through standIn of this lease (leasedHand): safe given to map receives the array's
// functions, not the call.
func (vm *VM) passLambdas(args []value.Value) int {
	mark := len(vm.passing)
	for index, argument := range args {
		if !argument.IsFunction() {
			continue // most arguments: one test of the kind
		}
		lambda := argument.Lambda()
		if lambda == nil {
			if !vm.frozen {
				// a Go function may call back what it is handed (safe given to map, to Array.from): it
				// hands a function another VM made to the host through standIn, as this loop does
				// (05/11/2026; tasks.map(safe) of a frozen file's tasks was a TYPE failure each)
				args[index] = argument.HandingOver(vm.leasedHand())
			}
			continue
		}
		if vm.topUnlisted {
			listCaller(vm.passing[len(vm.passing)-1], vm)
			vm.topUnlisted = false
		}
		vm.passing = append(vm.passing, lambda)
		if vm.made(lambda) {
			vm.topUnlisted = true
		} else {
			listCaller(lambda, vm)
			args[index] = argument.RunningOn(vm.leasedRun())
		}
	}
	if len(vm.passing) > mark {
		vm.nested.passed.Store(vm.passing[len(vm.passing)-1])
		if vm.contextShown != vm.Context {
			vm.contextShown = vm.Context
			vm.nested.context.Store(&shownContext{context: vm.Context})
		}
	}
	return mark
}

// standIn is argument, a function another VM made, as a stand-in that runs on vm (value.RunningOn), what
// passLambdas hands a host for one: a function of a frozen file's top level, which no VM but the one
// using it may run. Any other value, and every value once vm is frozen (its stand-ins would run on it),
// is itself.
func (vm *VM) standIn(argument value.Value) value.Value {
	if lambda := argument.Lambda(); lambda != nil && !vm.frozen && !vm.made(lambda) {
		return argument.RunningOn(vm.leasedRun())
	}
	return argument
}

// leasedHand is standIn bound to vm's current lease, what passLambdas hands a Go function to pass the
// functions it is called with through (value.HandingOver), made at the first hand-over of a lease. A host
// may keep that Go function and call it later, from any goroutine, once vm went back to the pool and serves
// another request: the stand-ins it then makes run in the lease the function was handed over in, so they
// run nothing (runStandIn), and making one writes nothing of vm. Until 05/11/2026 it was standIn itself,
// called as the host called: a stand-in of whatever lease vm held by then, which ran the boot's function on
// the VM another request held, and leasedRun wrote vm.runHere from the host's goroutine.
func (vm *VM) leasedHand() func(argument value.Value) value.Value {
	run := vm.leasedRun()
	if vm.handHere == nil || vm.handHereLease != vm.runHereLease {
		vm.handHere = func(argument value.Value) value.Value {
			if lambda := argument.Lambda(); lambda != nil && !vm.frozen && !vm.made(lambda) {
				return argument.RunningOn(run)
			}
			return argument
		}
		vm.handHereLease = vm.runHereLease
	}
	return vm.handHere
}

// leasedRun is the run of the stand-ins vm makes in its current lease (runHere), made at the first stand-in
// of a lease: a stand-in runs on vm only while vm serves the request that made it. A host may keep what a
// request handed it, an object holding a stand-in among them ({ toString: label }), past the request
// (05/11/2026: converted after vm went back to the pool, it ran on vm while vm served another request,
// from another goroutine: panics, and that request's wrong answers).
func (vm *VM) leasedRun() func(lambda *value.Lambda, arguments []value.Value) value.Value {
	lease := vm.nested.lease.Load()
	if vm.runHere == nil || vm.runHereLease != lease {
		vm.runHere = func(lambda *value.Lambda, arguments []value.Value) value.Value {
			return vm.runStandIn(lease, lambda, arguments)
		}
		vm.runHereLease = lease
	}
	return vm.runHere
}

// runStandIn is the run of vm's stand-ins of lease (leasedRun): the lambda on vm, unless vm went back to
// the pool since, or is frozen, and runs nothing more for it (a stand-in a site's boot made and its file
// kept): a TYPE failure then, as runClosure gives. The lease is read first, an atomic: vm may be serving
// another request on another goroutine.
func (vm *VM) runStandIn(lease uint64, lambda *value.Lambda, arguments []value.Value) value.Value {
	if vm.nested.lease.Load() != lease {
		return endedRun(lambda)
	}
	if vm.frozen {
		return frozenRun(lambda)
	}
	return vm.ExecuteLambda(lambda, arguments)
}

// listCaller enters caller in the registry as a VM passing lambda to a host call in progress.
func listCaller(lambda *value.Lambda, caller *VM) {
	shard := lambdaShard(lambda)
	shard.mu.Lock()
	if shard.callers == nil {
		shard.callers = make(map[*value.Lambda][]*VM)
	}
	shard.callers[lambda] = append(shard.callers[lambda], caller)
	shard.mu.Unlock()
}

// unlistCaller removes one entry listCaller made.
func unlistCaller(lambda *value.Lambda, caller *VM) {
	shard := lambdaShard(lambda)
	shard.mu.Lock()
	callers := shard.callers[lambda]
	for position := len(callers) - 1; position >= 0; position-- {
		if callers[position] == caller {
			callers = append(callers[:position], callers[position+1:]...)
			break
		}
	}
	if len(callers) == 0 {
		delete(shard.callers, lambda)
	} else {
		shard.callers[lambda] = callers
	}
	shard.mu.Unlock()
}

// withdrawLambdas removes the records passLambdas made since mark.
func (vm *VM) withdrawLambdas(mark int) {
	top := len(vm.passing) - 1
	for index := top; index >= mark; index-- {
		lambda := vm.passing[index]
		vm.passing[index] = nil
		if index == top && vm.topUnlisted {
			vm.topUnlisted = false
			continue
		}
		unlistCaller(lambda, vm)
	}
	if len(vm.passing) > mark {
		vm.passing = vm.passing[:mark]
		if mark > 0 {
			vm.nested.passed.Store(vm.passing[mark-1])
		} else {
			vm.nested.passed.Store(nil)
		}
	}
}

// runClosure is the Run of a closure vm made (makeClosure): what a host calls, fn.Call(args), from Go or
// from a builtin (map, sort, safe, Array.from). It runs the lambda on the VM whose host call was passed
// it, the one calling it: vm itself when vm is making the host call (the common case, told by
// nested.passed without a lock), else the VM LambdaCaller finds, and vm when no host call in progress
// was passed it (Go calling a function it kept). A closure made while a module loaded and handed to a
// host by a request ran on the VM that loaded the module, which by then ran something else (04/11/2026).
//
// A frozen VM (Freeze) runs nothing more: a host that runs one of its closures without having been handed
// it (value's own valueOf or toString of an object the file made as the site booted) gets a TYPE failure.
// The VM calling cannot be told from here, and the registry may name another request's VM.
func (vm *VM) runClosure(lambda *value.Lambda, arguments []value.Value) value.Value {
	if vm.frozen {
		return frozenRun(lambda)
	}
	return vm.runnerOf(lambda).ExecuteLambda(lambda, arguments)
}

// runnerOf is the VM that runs lambda, a closure this VM made, when a Go function calls it (runClosure):
// a VM whose host call it was passed to, running inside this one, or else this VM.
func (vm *VM) runnerOf(lambda *value.Lambda) *VM {
	if vm.nested.passed.Load() != lambda {
		if caller := LambdaCaller(lambda); caller != nil {
			return caller
		}
	}
	return vm
}

// Keep is value.Holder's, through the origin of a closure this VM made (origin.go): Go code about to call
// lambda holds kept for the VM that will run it (runnerOf), which holds it until LetGo with the mark it
// gives (value.Kept, memory.go). A frozen VM runs nothing, and keeps nothing.
func (vm *VM) Keep(lambda *value.Lambda, kept value.Kept) (value.Holder, int) {
	if vm.frozen {
		return nil, 0
	}
	runner := vm.runnerOf(lambda)
	return runner, runner.keep(keptValue{value: kept.Value, items: kept.Items, size: kept.Size})
}

// LetGo is value.Holder's: what was kept from mark on is forgotten.
func (vm *VM) LetGo(mark int) { vm.letGo(mark) }

// NativePanic is the failure of a Go panic that escaped a nested run of vm outside any host call of
// it: NATIVE_PANIC, the failure a panic in a host call is, which ends the run and safe() cannot catch.
// A failure value cannot say so itself, so it is recorded as vm's stop, which Unnest hands to the VM vm
// runs inside; vm may be nil (a panic before a VM was taken), and then only the value is made.
func NativePanic(vm *VM, label string, recovered any) value.Value {
	diagnostic := &Diagnostic{
		Code:    DiagnosticNativePanic,
		Message: nativePanicMessage(label, recovered),
		IP:      -1,
	}
	vm.stopWith(diagnostic)
	return diagnosticResult(diagnostic)
}

func nativePanicMessage(label string, recovered any) string {
	return fmt.Sprintf("Native panic in %s: %v", label, recovered)
}

// RunsInside reports whether vm is other, or runs nested in a host call of other at any depth: a host that
// waits for what other is doing (a value of storage another call of the same key is computing) would wait
// for itself when asked from inside it. The links are made before the nested run begins and read on the
// goroutine of vm, which runs inside them.
func (vm *VM) RunsInside(other *VM) bool {
	current := vm
	for range nestingWalkLimit {
		if current == nil {
			return false
		}
		if current == other {
			return true
		}
		link := current.outer
		if link == nil {
			return false
		}
		current = link.outer
	}
	return false
}
