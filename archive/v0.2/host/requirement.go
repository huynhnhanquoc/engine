package host

import (
	"errors"
	"strings"
	"sync"
)

// Requirements are the keys env.require(…) did not find while the host booted, each with where it is
// required, so that one boot error names them all at once (docs/KITWORK_ENVIRONMENT.md mục 3), never a
// value. Once the boot is over a missing key is the failure of the call, not a record.
type Requirements struct {
	mutex   sync.Mutex
	closed  bool
	missing []string
}

// Report records key, required at where, and says whether the boot is still running to read it.
func (r *Requirements) Report(key, where string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return false
	}
	r.missing = append(r.missing, key+" ("+where+")")
	return true
}

// Close ends the boot's records: the error naming every missing key, nil when none is missing.
func (r *Requirements) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.closed = true
	if len(r.missing) == 0 {
		return nil
	}
	return errors.New("env.require: no file sets " + strings.Join(r.missing, ", "))
}
