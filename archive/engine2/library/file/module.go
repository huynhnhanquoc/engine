package file

// The words of kitwork/file a program uses (docs/KITWORK_FILE.md of the root, which wins over engine2/docs/file.md:
// Quốc, "Làm theo bản gốc", 09/11/2026). Read: called on the module, file.json("settings.json"). Change: on a handle
// open() gives, file.open("notes.txt").write("Xin chào"). Two zones (mục 1): the app's data, .database/files/ of
// the app, made at the first write, never served, never deployed; and its code, "app/…", read only, where .env,
// .env.* and .database/ are never reached. The machinery (paths, the os.Root of a zone, writes renamed into place,
// copies that never replace) moved whole from the first engine's utilities/file: path.go, zone.go, copy.go,
// kind.go. A file that is not there reads as null (27/10/2026), never NOT_FOUND.

import (
	"path/filepath"
	"sync"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/internal/safepath"
)

// The ceilings of kitwork/file. The spec leaves the numbers open (mục 15.2): these are the first engine's, vars so
// tests lower them.
var (
	// ReadLimit is the most a read brings into the VM's memory (mục 4): past it TOO_LARGE, and the file is served
	// with context.file instead.
	ReadLimit int64 = 8 << 20
	// TreeLimit is the most entries clear(), and copy() of a folder, do in one call (mục 7, mục 8).
	TreeLimit = 10_000
)

// zones is what the words of an app's File reach the disk through: the app's folder with its links resolved, made
// at the first word, once, as many requests read the same File at once.
type zones struct {
	once     sync.Once
	boundary *safepath.Boundary
}

// zone is the code zone of f's app ("app/…"), or its data zone.
func (f *File) zone(code bool) *Zone {
	f.zones.once.Do(func() {
		f.zones.boundary, _ = safepath.NewBoundary(f.Root)
	})
	if code {
		return &Zone{App: f.Root, Boundary: f.zones.boundary, Code: true}
	}
	return &Zone{App: f.Root, Folder: filepath.Join(".database", "files"), Boundary: f.zones.boundary}
}

// reaching is the failure of a word of the app's files where there are none: the manifest's file reads only the
// environment, and a File with no folder (made for no file) reaches nothing.
func (f *File) reaching(word string) value.Value {
	switch {
	case f.Manifest:
		return value.Fail(value.UnsupportedCode, "file."+word+"(…) trong application.work: manifest chỉ đọc biến môi trường, file.env(\"./.env\"); file của một app đọc trong app đó")
	case f.Root == "":
		return value.Fail(value.UnsupportedCode, "file."+word+"(…): file này không thuộc app nào, nên không có file nào để đọc")
	}
	return value.Value{}
}

// Handle is f's handle of the path written, or the failure of reading it: the path is a string, written as
// mục 2 says.
func (f *File) handle(word string, arguments []value.Value) (*Handle, value.Value) {
	if failure := f.reaching(word); failure.IsInvalid() {
		return nil, failure
	}
	if len(arguments) != 1 || !arguments[0].IsString() {
		return nil, value.Fail(value.InvalidArgumentCode, "file."+word+"(đường_dẫn) nhận một đường dẫn là chuỗi: file."+word+"(\""+example(word)+"\")")
	}
	path, failed := Parse(arguments[0].Text())
	if failed != nil {
		return nil, fault(failed)
	}
	return &Handle{file: f, path: path}, value.Value{}
}

// example is a path a word is written with, for a message.
func example(word string) string {
	switch word {
	case "json":
		return "settings.json"
	case "list", "open":
		return "uploads/"
	case "bytes":
		return "uploads/a.png"
	}
	return "notes.txt"
}

// Open is file.open(path): the handle of the path, which touches nothing (mục 2), even for a path not there.
func (f *File) Open(arguments ...value.Value) value.Value {
	handle, failure := f.handle("open", arguments)
	if failure.IsInvalid() {
		return failure
	}
	return value.Struct(handle)
}

// The read words, called on the module: what the handle of the path reads (mục 3).
func (f *File) Read(budget value.Budget, arguments ...value.Value) value.Value {
	return f.readingWithin("read", arguments, budget)
}
func (f *File) Text(arguments ...value.Value) value.Value  { return f.reading("text", arguments) }
func (f *File) Bytes(arguments ...value.Value) value.Value { return f.reading("bytes", arguments) }
func (f *File) Json(budget value.Budget, arguments ...value.Value) value.Value {
	return f.readingWithin("json", arguments, budget)
}
func (f *File) List(arguments ...value.Value) value.Value   { return f.reading("list", arguments) }
func (f *File) Info(arguments ...value.Value) value.Value   { return f.reading("info", arguments) }
func (f *File) Exists(arguments ...value.Value) value.Value { return f.reading("exists", arguments) }

func (f *File) reading(word string, arguments []value.Value) value.Value {
	return f.readingWithin(word, arguments, nil)
}

// readingWithin is reading by a run whose budget is budget: a JSON file's tree is made within it.
func (f *File) readingWithin(word string, arguments []value.Value, budget value.Budget) value.Value {
	handle, failure := f.handle(word, arguments)
	if failure.IsInvalid() {
		return failure
	}
	switch word {
	case "list":
		return handle.list()
	case "info":
		return handle.info()
	case "exists":
		return handle.exists()
	}
	return handle.read(word, budget)
}

// changes are the words that change the disk: only on a handle, so a line reads as what it changes (mục 3).
var changes = map[string]string{"write": "write(dữ_liệu)", "create": "create()", "copy": "copy(đích)", "move": "move(đích)", "delete": "delete()"}

// MissingMember is a word the module has not: an old spelling names what is written now (mục 14, mục 17).
func (f *File) MissingMember(name string) value.Value {
	if call, ok := changes[name]; ok {
		return value.Fail(value.TypeCode, "file."+name+"(…) không có: đổi thì open() trước, file.open(\"notes.txt\")."+call+" (KITWORK_FILE.md mục 3)")
	}
	switch name {
	case "save":
		return value.Fail(value.TypeCode, "file.save(…) không có: file.open(\"notes.txt\").write(dữ_liệu) (KITWORK_FILE.md mục 17)")
	case "remove":
		return value.Fail(value.TypeCode, "file.remove(…) không có: làm rỗng rồi xoá, file.open(\"tmp/\").clear() rồi delete() (KITWORK_FILE.md mục 7)")
	case "base64":
		return value.Fail(value.TypeCode, "file.base64(…) đã bỏ: ảnh trả cho trình duyệt bằng context.file(\"uploads/a.png\") (KITWORK_FILE.md mục 17)")
	case "directory", "folder", "path":
		return value.Fail(value.TypeCode, "file."+name+"(…) không có: một thư mục là đường dẫn có \"/\" cuối, file.open(\"uploads/\") (KITWORK_FILE.md mục 2)")
	}
	return value.Fail(value.TypeCode, "file."+name+" không có: kitwork/file có env, open, read, text, bytes, json, list, info, exists (KITWORK_FILE.md mục 3)")
}

// fault is a failure of the storage as a program reads it, of the code the storage said, each a constant of the
// closed set: PARTIAL carries error.progress, the entries done; a code the storage never says is DATABASE.
func fault(failed *Error) value.Value {
	switch failed.Code {
	case value.NotFoundCode:
		return value.Fail(value.NotFoundCode, failed.Message)
	case value.DeniedCode:
		return value.Fail(value.DeniedCode, failed.Message)
	case value.InvalidArgumentCode:
		return value.Fail(value.InvalidArgumentCode, failed.Message)
	case value.InvalidInputCode:
		return value.Fail(value.InvalidInputCode, failed.Message)
	case value.ConflictCode:
		return value.Fail(value.ConflictCode, failed.Message)
	case value.TooLargeCode:
		return value.Fail(value.TooLargeCode, failed.Message)
	case value.PartialCode:
		return value.Fail(value.PartialCode, failed.Message).WithProgress(value.Number(float64(failed.Done)))
	}
	return value.Fail(value.DatabaseCode, failed.Message)
}
