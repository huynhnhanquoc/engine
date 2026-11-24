package value_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
)

// A failure carries how much of its work was done (WithProgress) beside the fields it names (WithFields): either
// added after the other keeps it, and neither sticks to a value that is no failure.
func TestFailureKeepsProgressAndFields(t *testing.T) {
	fields := value.Array([]value.Value{value.Object().Set("path", value.String("email"))})
	both := []value.Value{
		value.Fail(value.PartialCode, "stopped").WithProgress(value.Number(3)).WithFields(fields),
		value.Fail(value.PartialCode, "stopped").WithFields(fields).WithProgress(value.Number(3)),
	}
	for i, failure := range both {
		if !failure.Progress().IsNumber() || failure.Progress().Number() != 3 || !failure.Fields().IsArray() {
			t.Errorf("%d: progress %v, fields %v", i, failure.Progress(), failure.Fields())
		}
		if failure.Code() != value.PartialCode || failure.Message() != "stopped" {
			t.Errorf("%d: %s", i, failure.Failure())
		}
	}
	if kept := value.Number(1).WithProgress(value.Number(3)); kept.Progress().IsNumber() {
		t.Error("a number took a progress")
	}
	if kept := value.Fail(value.PartialCode, "x").WithProgress(value.String("3")); kept.Progress().IsNumber() || !kept.Progress().IsUndefined() {
		t.Error("a progress that is no number was kept")
	}
}
