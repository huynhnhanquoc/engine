package runtime

import (
	"strings"
	"sync/atomic"

	"github.com/kitwork/engine2/core/value"
)

// Pool is what every run of a host makes together at most (12/11/2026, the third slice of the tenants' limits,
// docs/KITWORK_BOOT.md): each run has its ceiling (MaxBytes, memory.go), so a site holds no more than its requests at
// once times that, and a host of many sites no more than all of theirs; a small machine wants less. A run takes from
// the pool what it makes, in steps of poolStep, and gives it all back as it ends. A run the pool cannot give to is
// counted again by what it holds, gives back what it took past that, and asks again; refused still, it stops with
// HOST_MEMORY_LIMIT, which nothing catches, and whose failure is UNAVAILABLE: the host is busy, not the site wrong, so
// the site answers 503 (HostBusy).
//
// A run asks at its checkpoint, every 64 instructions, and on the meter's way past what it took (room): a run makes
// at most its ceiling between two asks, so the pool is passed by at most that much, for that long.
type Pool struct {
	limit uint64
	held  atomic.Uint64
}

// poolStep is how much a run takes from the pool at a time: one atomic addition a megabyte made.
const poolStep = 1 << 20

// NewPool is a pool of limit bytes.
func NewPool(limit uint64) *Pool { return &Pool{limit: limit} }

// Held is what the runs going on took from the pool.
func (p *Pool) Held() uint64 { return p.held.Load() }

// Limit is what the pool gives at most.
func (p *Pool) Limit() uint64 { return p.limit }

// take takes amount from the pool: false, taking nothing, when it would pass the limit.
func (p *Pool) take(amount uint64) bool {
	if p.held.Add(amount) <= p.limit {
		return true
	}
	p.held.Add(^(amount - 1))
	return false
}

// give gives amount back.
func (p *Pool) give(amount uint64) {
	if amount > 0 {
		p.held.Add(^(amount - 1))
	}
}

const hostMemoryMessage = "Host Memory Limit Exceeded: the host's runs hold all it gives, Execution halted"

// HostBusy reports whether result is the failure of a run the host's pool could not give to: a request to answer
// 503, not 500.
func HostBusy(result value.Value) bool {
	return result.IsInvalid() && result.Code() == value.UnavailableCode && strings.Contains(result.Message(), hostMemoryMessage)
}

// covered reports whether what the run took from the pool covers amount more; a run without a pool always is.
func (vm *VM) covered(amount uint64) bool {
	return vm.Pool == nil || vm.Bytes+amount <= vm.taken
}

// cover takes from the pool what the run needs for amount more, in steps; when the pool cannot give, the run is
// counted again by what it holds, gives back what it took past that, and asks again. False, with hostFull set for the
// failure, when it is refused still.
//
// A run that made at least as much as it held when last counted, and a step at least, is counted again before it takes
// more, and gives back what it took past what it holds: what it takes follows what it holds, not what it made, so a run
// building a text by += holds a step or two of the pool, not all of it, while the others wait. As the collector of Go
// paces itself, a count costs at most what was made since the last.
func (vm *VM) cover(amount uint64) bool {
	if vm.covered(amount) {
		return true
	}
	if vm.Bytes >= vm.counted && vm.Bytes-vm.counted >= max(poolStep, vm.counted) {
		vm.recount(amount)
		if vm.covered(amount) {
			return true
		}
	}
	if vm.ask(vm.Bytes + amount) {
		return true
	}
	if vm.recount(amount) && (vm.covered(amount) || vm.ask(vm.Bytes+amount)) {
		return true
	}
	vm.hostFull = true
	return false
}

// recount counts the run again by what it holds and gives the pool back what it took past that: whether it holds less
// than it counted.
func (vm *VM) recount(amount uint64) bool {
	vm.spendMade()
	held := vm.holding(max(vm.MaxBytes, vm.Bytes+amount))
	less := held < vm.Bytes
	if less {
		vm.Bytes = held
	}
	vm.counted = vm.Bytes
	vm.settle()
	return less
}

// ask takes from the pool up to need, in whole steps past what the run took.
func (vm *VM) ask(need uint64) bool {
	more := (need - vm.taken + poolStep - 1) / poolStep * poolStep
	if !vm.Pool.take(more) {
		return false
	}
	vm.taken += more
	return true
}

// settle gives the pool back what the run took past what it counts now, in whole steps: after a count by what it
// holds.
func (vm *VM) settle() {
	if vm.Pool == nil {
		return
	}
	keep := (vm.Bytes + poolStep - 1) / poolStep * poolStep
	if vm.taken > keep {
		vm.Pool.give(vm.taken - keep)
		vm.taken = keep
	}
}

// giveBack gives the pool all the run took: as its outermost run ends, and as the VM goes back to its pool.
func (vm *VM) giveBack() {
	if vm.Pool != nil && vm.taken > 0 {
		vm.Pool.give(vm.taken)
	}
	vm.taken = 0
}
