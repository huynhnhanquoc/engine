//go:build !windows

package host_test

// heldOpen chỉ Windows nói được (handles_windows_test.go): ở hệ khác một file mở không chặn ai.
func heldOpen(string) bool { return false }
