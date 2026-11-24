package runtime_test

import (
	"testing"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/core/value"
)

// A failure a host call ran, wrapped as the call's own (WrapFailure), keeps how much of its work was done, as it keeps
// the fields it names: clear() of kitwork/file stopped midway is PARTIAL with its progress wherever it is told.
func TestWrappedFailureKeepsProgress(t *testing.T) {
	cause := value.Fail(value.PartialCode, "stopped after 3").WithProgress(value.Number(3))
	wrapped := runtime.WrapFailure(nil, "clear() stopped", cause)
	if wrapped.Code() != value.PartialCode || !wrapped.Progress().IsNumber() || wrapped.Progress().Number() != 3 {
		t.Fatalf("wrapped: %s, progress %v", wrapped.Failure(), wrapped.Progress())
	}
}
