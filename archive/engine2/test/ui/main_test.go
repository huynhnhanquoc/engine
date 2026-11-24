package ui_test

import (
	"os"
	"testing"

	"github.com/kitwork/engine2/library/ui"
)

// TestMain runs every test with ui.CheckParts: each page drawn twice, the second from the parts the first kept, the
// two the same (library/ui frozen.go).
func TestMain(m *testing.M) {
	ui.CheckParts = true
	os.Exit(m.Run())
}
