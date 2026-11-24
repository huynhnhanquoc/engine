package runtime

import "github.com/kitwork/engine2/core/value"

// A let or a const a function may use before the line declaring it has run (UNSET_SLOT, UNSET_NAME, bytecode 10,
// 18/11/2026; decided 27/10/2026): JavaScript's temporal dead zone. The compiler marks such a variable as its scope
// begins, only where a function made before the line reads or writes it (compiler scope.go, holes), and the mark is
// this failure: LOAD_SLOT, LOAD_OUTER and LOAD meet it as the failure test every read of a value that may be one
// takes, so a read costs nothing more; STORE_OUTER refuses to write over it. The line declaring the variable writes
// it, by STORE_SLOT or STORE, which writes over the mark. A read or a write before it is the failure NOT_INITIALIZED
// where it stands, which safe() catches, as try catches JavaScript's ReferenceError; the variable read undefined.

// notInitialized is the mark of the variable named name.
func notInitialized(name string) value.Value {
	return value.Fail(value.NotInitializedCode, SlotName(name)+" chưa có: một hàm dùng nó trước khi dòng khai nó chạy; JavaScript ném ReferenceError ở đây, gọi hàm ấy sau dòng khai")
}
