package napas_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/napas"
)

// A frozen Napas is read by every run of its file at once: freezing it again (Object.freeze(napas) at a
// request) writes nothing, so it is no data race with the chains reading it (05/11/2026; go test -race
// saw the mark written by each Object.freeze).
func TestFreezingAFrozenNapasWritesNothing(t *testing.T) {
	imported := value.Struct(&napas.Napas{}).Freeze()
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				imported.Freeze()
				payload := imported.Invoke("bank", []value.Value{value.String("970415"), value.String("1")}).Invoke("payload", nil)
				if !strings.Contains(payload.Text(), "970415") {
					t.Errorf("a chain from the import: %s", payload.Failure())
					return
				}
			}
		}()
	}
	group.Wait()
}
