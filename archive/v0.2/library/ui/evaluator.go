package ui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/core/compiler"
	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// expressionEnergy is what one expression of a page may spend: a page's expressions read what the page is
// given, they compute little, and a loop over a page's thousand rows is the page's for, not an expression's.
const expressionEnergy = 1_000_000

// expressionBytes is what one expression of a page may make (core/runtime, memory.go), a request's own ceiling.
var expressionBytes = runtime.Limits().DefaultMaxBytes

// evaluator runs the expressions of one render of a page: one VM, the builtins, and the names in scope, each
// put as the page goes down into a for or a piece and taken back as it comes out.
type evaluator struct {
	vm      *runtime.VM
	globals map[string]value.Value
	// given are the names in scope now, so an expression that reads another is refused, not read as nothing
	given map[string]int
	// machines are the site's VMs, which vm comes from and goes back to (release); nil makes one
	machines *sync.Pool
	// pool is the host's, which each expression takes from (nil for none); bytes what one makes at most, 0 the default
	pool  *runtime.Pool
	bytes uint64
}

func newEvaluator(machines *sync.Pool, pool *runtime.Pool, bytes uint64) *evaluator {
	globals := map[string]value.Value{}
	builtins.Install(globals)
	if bytes == 0 {
		bytes = expressionBytes
	}
	return &evaluator{globals: globals, given: map[string]int{}, machines: machines, pool: pool, bytes: bytes}
}

// release gives the evaluator's machine back to the site's, for the next page (FastReset readies it for each
// expression it runs).
func (e *evaluator) release() {
	if e.vm != nil && e.machines != nil {
		e.machines.Put(e.vm)
	}
	e.vm = nil
}

// put gives the page name, until the returned function takes it back and gives back what it hid.
func (e *evaluator) put(name string, v value.Value) func() {
	previous, had := e.globals[name]
	e.globals[name] = v
	e.given[name]++
	return func() {
		e.given[name]--
		if had {
			e.globals[name] = previous
			return
		}
		delete(e.globals, name)
	}
}

// evaluate runs x with the names in scope and gives its value. A name x reads that the page is not given is
// an error (KITWORK_LANG mục 3: a name not declared is an error; slice 1 tells it as the page renders, the
// compile-time check waiting on what a route gives its page).
func (e *evaluator) evaluate(x *expression) (value.Value, error) {
	for _, name := range x.reads {
		if e.given[name] == 0 {
			return value.Value{}, errors.New("trang không được đưa tên " + name + ": {{ " + strings.TrimSpace(x.source) + " }}")
		}
	}
	if e.vm == nil {
		if e.machines != nil {
			e.vm, _ = e.machines.Get().(*runtime.VM)
		}
		if e.vm == nil {
			e.vm = runtime.New(x.program)
		}
	}
	e.vm.MaxEnergy = expressionEnergy
	e.vm.MaxBytes = e.bytes
	e.vm.FastReset(x.program, e.globals)
	e.vm.Pool = e.pool
	if result := e.vm.Run(); result.IsInvalid() {
		if runtime.HostBusy(result) {
			return value.Value{}, fmt.Errorf("%w (%s)", ErrBusy, result.Message())
		}
		return value.Value{}, errors.New(result.Message())
	}
	return e.vm.Vars[compiler.ExpressionValue], nil
}
