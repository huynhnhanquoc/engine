package host

import (
	"context"
	"time"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/routine"
)

// routineStopGrace is how long a host that closes waits for the routines of go() its sites still run, which it has
// told to stop: one stuck in a call of the host that does not hear the stop runs on, and the host does not wait for it.
const routineStopGrace = 2 * time.Second

// routines is what the site gives kitwork/routine: a VM of its own for each attempt, a life that ends as the host
// closes, its log.
func (s *Site) routinesOf() *routine.Site {
	if s.routines != nil {
		return s.routines
	}
	lifetime, stop := context.WithCancel(context.Background())
	s.stopRoutines = stop
	s.routines = &routine.Site{
		Run:      s.runRoutine,
		Lifetime: lifetime,
		Log:      func(line string) { s.log.Led(s.lead, line) },
	}
	// the site runs as many routines at once as requests (gate.go)
	if s.gate != nil {
		s.routines.Running = int(s.gate.limit)
	}
	return s.routines
}

// runRoutine runs a snapshot on a VM of the site's pool, with the site's globals, a request's budget and ctx: the
// run ends when ctx does (the VM reads it every 64 instructions).
func (s *Site) runRoutine(ctx context.Context, snapshot value.Value) (value.Value, bool) {
	lambda := snapshot.Lambda()
	program, ok := runtime.ProgramFromRef(lambda.Program)
	if !ok {
		return value.Fail(value.TypeCode, "a routine's function has no program"), false
	}
	work, _ := s.machines.Get().(*worker)
	if work == nil {
		work = &worker{machine: runtime.New(program)}
	}
	machine := work.machine
	machine.FastReset(program, s.globals)
	machine.Builtins = s.builtins
	machine.MaxEnergy = s.energy
	machine.MaxBytes = s.made
	machine.Pool = s.pool
	machine.Context = ctx
	result, stopped := machine.RunSnapshot(snapshot)
	machine.ResetForPool()
	s.machines.Put(work)
	return result, stopped
}

// closeRoutines tells the routines of go() the site runs to stop and waits for them up to the grace, saying in the
// log when some run on.
func (s *Site) closeRoutines() {
	if s.routines == nil {
		return
	}
	s.stopRoutines()
	if !s.routines.Wait(routineStopGrace) {
		s.log.Led(s.lead, "routines of routine.go(…) still run as the host closes: told to stop, not waited for longer")
	}
}
