package schedule

import "github.com/huynhnhanquoc/engine/core/value"

// JobContext là context của memory, energy và các hàm sau một job.
type JobContext struct {
	job Job
}

// Job là một lần chạy của một lịch. Engine không biết tenant nên không có job.tenant: thứ nó biết chắc là thư mục của
// job, đúng chuỗi folders đã cho; host tự quyết theo thư mục.
type Job struct {
	name   string      // tên export của lịch: "baoCao"
	folder string      // một chuỗi của folders: "apps/03dt/.schedules", ".schedules"
	error  value.Value // ở success, errors, finally: lỗi (loại invalid) hay undefined
}
