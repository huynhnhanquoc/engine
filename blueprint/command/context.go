package command

import "github.com/huynhnhanquoc/engine/core/value"

// CommandContext là context của một hàm trong handle và của các hàm sau một lệnh.
type CommandContext struct {
	command   Invocation
	arguments []string               // các chữ sau tên lệnh: kitcli test shop.vn → ["shop.vn"]
	options   map[string]value.Value // các cờ: --verbose → { verbose: true }
}

// Invocation là một lần gõ lệnh, đọc là context.command.
type Invocation struct {
	name  string      // tên lệnh đã gõ: "backup"
	error value.Value // ở success, errors, finally: lỗi (loại invalid) hay undefined
}
