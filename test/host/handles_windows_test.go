//go:build windows

package host_test

import "syscall"

// sharingViolation là ERROR_SHARING_VIOLATION của Windows (syscall không đặt tên cho nó).
const sharingViolation syscall.Errno = 32

// heldOpen là một handle tới path còn mở: Windows từ chối một handle không chia sẻ gì khi một handle khác đang mở (Go
// mở file có chia sẻ cả xoá, nên xoá được dù file còn mở: xoá không nói được gì).
func heldOpen(path string) bool {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err == sharingViolation
	}
	syscall.CloseHandle(h)
	return false
}
