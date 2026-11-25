//go:build !race

package host_test

// raced: test chạy với -race (sync.Pool bỏ ngẫu nhiên một phần tư thứ nó nhận, bộ dò cấp phát cho chính nó).
const raced = false
