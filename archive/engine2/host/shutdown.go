package host

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Stopping (12/11/2026): Ctrl+C or SIGTERM ends a server as its Close does, not as a killed process (run.go catches
// them and calls Stop). The servers take no new connection; the streams of context.sse close at once, a browser
// connects again to the next host; the requests going on get StopGrace to end; then Close: the queues and schedules
// stop, the routines of go() stop, and every database file closes, KitDB's writer last, so no -wal stays beside a file
// and kitwork.db copied alone holds every commit. Before, nothing caught the signal: the process died where it was and
// Close never ran. A statement going on through the database port is canceled, its transaction rolled back.

// defaultStopGrace is how long the requests going on get once Stop is called, when StopGrace is zero.
const defaultStopGrace = 10 * time.Second

// Stop asks Serve or ServeDatabase to end as above. It returns at once; the serving call returns once everything is
// closed. Calling it again does nothing more.
func (h *Host) Stop() {
	h.stopOnce.Do(func() { close(h.stopRequested()) })
}

// stopRequested is closed once Stop is called.
func (h *Host) stopRequested() chan struct{} {
	h.stopInit.Do(func() { h.stop = make(chan struct{}) })
	return h.stop
}

// stopGrace is StopGrace, or its default.
func (h *Host) stopGrace() time.Duration {
	if h.StopGrace > 0 {
		return h.StopGrace
	}
	return defaultStopGrace
}

// shutDown ends the servers gracefully: none takes a new connection, the open streams of context.sse close at once (a
// stream never ends by itself, so it would hold its server to the end of the grace), and the requests going on get the
// grace to end before the connections close.
func (h *Host) shutDown(servers []*http.Server) {
	grace := h.stopGrace()
	h.log.Line("[app] stopping: the requests going on get " + grace.String() + ", then the files close (Ctrl+C again stops at once)")
	h.reloading.Lock()
	streams := make([]*appStreams, 0, len(h.apps))
	for _, run := range h.apps {
		streams = append(streams, run.streams)
	}
	h.reloading.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	var wait sync.WaitGroup
	for _, server := range servers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = server.Shutdown(ctx)
		}()
	}
	for _, each := range streams {
		each.close() // after Shutdown began: no new stream opens meanwhile
	}
	wait.Wait()
}
