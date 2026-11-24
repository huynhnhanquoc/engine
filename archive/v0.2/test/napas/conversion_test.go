package napas_test

import (
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/napas"
)

// A step of the chain reads the text or the number of what it is given, or is the failure of making it:
// an object whose own toString or valueOf fails, as one a site's boot made does at a request, gives its
// failure (05/11/2026: read as "" or NaN, so receiver, info and city wrote "" into the payload, and bank
// said the BIN was "").
func TestAStepIsTheFailureOfAnArgumentThatCannotConvert(t *testing.T) {
	failure := value.Function(func(...value.Value) value.Value {
		return value.Fail(value.TypeCode, "its own conversion failed")
	})
	failing := value.Object().Set("toString", failure).Set("valueOf", failure)
	bank := func(arguments ...value.Value) value.Value {
		return value.Struct(&napas.Napas{}).Invoke("bank", arguments)
	}
	start := bank(value.String("970415"), value.String("1234567890"))
	for name, got := range map[string]value.Value{
		"bank(code)":    bank(failing, value.String("1234567890")),
		"bank(account)": bank(value.String("970415"), failing),
		"amount":        start.Invoke("amount", []value.Value{failing}),
		"info":          start.Invoke("info", []value.Value{failing}),
		"receiver":      start.Invoke("receiver", []value.Value{failing}),
		"city":          start.Invoke("city", []value.Value{failing}),
	} {
		if !got.IsInvalid() || got.Message() != "its own conversion failed" {
			t.Errorf("%s of an object whose conversion fails: %s, want that failure", name, describeStep(got))
		}
	}
}

func describeStep(v value.Value) string {
	if v.IsInvalid() {
		return v.Failure()
	}
	return "no failure"
}
