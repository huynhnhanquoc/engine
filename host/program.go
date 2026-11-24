package host

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/database"
)

// Thư viện của application.work là "application" (owner 23/11/2026): gọi bằng tên, không còn "kitwork/". Nó đưa ra đúng
// hai tên (owner 24/11/2026: các cửa gom về application): application, thứ duy nhất dựng được một cửa (app.website(…),
// app.database(…), app.schedule(…), app.command(…)), và environment, môi trường của host. Chỉ application.work import
// nó.

// BootEnergy là năng lượng của thân application.work lúc boot: đủ cho mọi bản thiết kế thật, chặn một chương trình
// chạy mãi.
const BootEnergy = 10_000_000

// Program là một application.work để boot.
type Program struct {
	File   string // tên trong lời báo: "application.work", hay đường dẫn của nó
	Source string
	// Folder là thư mục của application.work: environment(…) đọc file tính từ đây. "" là thư mục của File.
	Folder string
	// System là biến của hệ điều hành (thắng mọi file .env). nil là os.LookupEnv.
	System func(string) (string, bool)
	// Console là nơi console.log của application.work và các file nó import viết, lúc boot và trong mọi hàm của bản
	// thiết kế sau đó (guard, các hàm sau khi gửi…). nil là os.Stdout.
	Console io.Writer
}

// Booted là kết quả của một lần boot: bản thiết kế, các ghi chú (không phải lỗi) boot in ra, thư mục của
// application.work (routes của website tính từ đây), console của nó (nil là os.Stdout), và chỗ khai của application và
// của mỗi cửa ("website" → "application.work:17:1"), cho Describe.
type Booted struct {
	Blueprint value.Value
	Notes     []string
	Folder    string
	Console   io.Writer
	Places    map[string]string
	// Databases là các file database của lần boot (của host và của các app), mở ở câu lệnh đầu đọc mỗi file, đóng bằng
	// Close khi host dừng. nil khi Booted dựng tay.
	Databases *database.Files
}

// Close đóng các file database lần boot đã mở.
func (b Booted) Close() error {
	if b.Databases == nil {
		return nil
	}
	return b.Databases.Close()
}

// Boot là Program{File: file, Source: source}.Boot(), chỉ bản thiết kế.
func Boot(file, source string) (value.Value, error) {
	booted, err := Program{File: file, Source: source}.Boot()
	return booted.Blueprint, err
}

// Boot kiểm, biên dịch và chạy application.work, rồi trả bản thiết kế nó trả về (một *application.Application trong
// một Value). Thứ tự: cú pháp; luật của environment (check.go) và của các cửa (doors.go); đọc các file môi trường; mọi
// env.require một lần; biên dịch; chạy; rồi hình dạng của cái file trả về (shape). Lỗi có chỗ ("application.work:3:7:
// …"); thân file trả thứ khác app là lỗi.
func (p Program) Boot() (Booted, error) {
	folder, system := p.Folder, p.System
	if folder == "" {
		folder = filepath.Dir(p.File)
	}
	if system == nil {
		system = os.LookupEnv
	}
	program, err := syntax.Parse(p.File, p.Source)
	if err != nil {
		return Booted{}, err
	}
	check, err := checkEnvironment(program)
	if err != nil {
		return Booted{}, err
	}
	declared, err := checkDoors(program)
	if err != nil {
		return Booted{}, err
	}
	places := make(map[string]string, len(declared))
	for name, at := range declared {
		places[name] = fmt.Sprintf("%s:%s", p.File, at)
	}

	// đọc mọi file môi trường trước khi chạy dòng nào, rồi kiểm mọi require một lần
	var notes []string
	read := map[string]*Environment{}   // theo các đường dẫn của lời gọi
	byName := map[string]*Environment{} // theo tên const
	for _, call := range check.calls {
		key := strings.Join(call.paths, "\x00")
		env, found := read[key]
		if !found {
			var more []string
			env, more, err = readEnvironment(folder, call.paths, system)
			if err != nil {
				return Booted{}, fmt.Errorf("%s:%s: %w", p.File, call.at, err)
			}
			notes = append(notes, more...)
			read[key] = env
		}
		byName[call.name] = env
		if _, found := env.values["require"]; found {
			notes = append(notes, fmt.Sprintf(`%s:%s: a file sets a key named require: %s.require is the word, read the key with %s.require("require")`,
				p.File, call.at, call.name, call.name))
		}
	}
	var missing []string
	for _, r := range check.requires {
		if !byName[r.binding].has(r.key) {
			missing = append(missing, fmt.Sprintf("%s (%s:%s)", r.key, p.File, r.at))
		}
	}
	if len(missing) > 0 {
		return Booted{}, errors.New(p.File + ": env.require: no file and no variable of the system sets " + strings.Join(missing, ", "))
	}
	noted := map[string]bool{}
	for _, r := range check.reads {
		env := byName[r.binding]
		if env.has(r.key) || noted[r.binding+"."+r.key] {
			continue
		}
		noted[r.binding+"."+r.key] = true
		note := fmt.Sprintf("%s.%s (%s:%s) has no value in any file nor in the system's variables: it reads null", r.binding, r.key, p.File, r.at)
		if near := nearest(r.key, env.values); near != "" {
			note += ": did you mean " + near + "?"
		}
		notes = append(notes, note)
	}

	libraries := map[string]map[string]value.Value{
		"application": {
			"application": constructor("application", application.New),
			"environment": javascript.NewNative("environment", 1, func(_ javascript.Run, arguments []value.Value) value.Value {
				paths := make([]string, len(arguments))
				for i, a := range arguments {
					paths[i] = a.String()
				}
				env, found := read[strings.Join(paths, "\x00")]
				if !found { // check.go chỉ cho environment(…) ở cấp cao nhất với chữ viết sẵn: đã đọc ở trên
					return value.Invalid("environment(…): the files were not read at boot")
				}
				return value.Struct(env)
			}),
		},
	}
	// các file application.work import chạy trước, mỗi file một lần, trên cùng một lượt chạy (chung năng lượng boot)
	var globals map[string]value.Value
	if p.Console != nil {
		globals = map[string]value.Value{"console": javascript.Console(p.Console, "")}
	}
	databases := database.NewFiles()
	l := &files{folder: folder, file: p.File, machine: runtime.New(runtime.Options{Energy: BootEnergy}), done: map[string]map[string]value.Value{},
		globals: globals, libraries: map[string]map[string]value.Value{
			"schema": database.SchemaLibrary(), "database": database.Library(databases, folder, "."),
		}}
	modules, err := l.imports(program, p.File, ".", libraries)
	if err != nil {
		databases.Close()
		return Booted{}, err
	}
	main, err := compiler.Compile(program, compiler.Options{Modules: modules, Globals: globals})
	if err != nil {
		databases.Close()
		return Booted{}, err
	}
	result := l.machine.Run(main)
	if result.IsInvalid() {
		databases.Close()
		return Booted{}, result.Error()
	}
	if err := shape(p.File, result); err != nil {
		databases.Close()
		return Booted{}, err
	}
	return Booted{Blueprint: result, Notes: notes, Folder: folder, Console: p.Console, Places: places, Databases: databases}, nil
}

// shape kiểm hình dạng của cái application.work trả về, sau khi nó chạy xong: phải là app; mỗi cửa có mặt phải có chữ
// cuối (một câu chưa đóng chỉ thấy được khi file đã chạy xong; doors.go báo cùng lỗi ấy trước, kèm dòng, cho mọi câu nó
// đọc được); và file phải xin một trong hai: app.run() hay app.check().
func shape(file string, result value.Value) error {
	a, ok := value.As[application.Application](result)
	if !ok {
		return errors.New(file + ": must return the application, got " + describe(result))
	}
	var problems []string
	for _, d := range a.Dangling() {
		problems = append(problems, fmt.Sprintf("%s: application.%s is not closed: end its statement with .%s(…)", file, d.Door, d.Word))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	if !a.Running() && !a.Checking() {
		return errors.New(file + ": end with return app.run(), or return app.check() to load and check only")
	}
	return nil
}

func constructor(name string, build func(...value.Value) value.Value) value.Value {
	return javascript.NewNative(name, 2, func(_ javascript.Run, arguments []value.Value) value.Value { return build(arguments...) })
}

// nearest là khoá của values gần name nhất, cách nhiều nhất hai chữ (gõ thiếu, thừa, sai một chữ), "" khi không có.
func nearest(name string, values map[string]value.Value) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	best, distance := "", 3
	for _, key := range keys {
		if d := editDistance(name, key); d < distance {
			best, distance = key, d
		}
	}
	return best
}

// editDistance là khoảng cách Levenshtein của a và b.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func describe(v value.Value) string {
	switch {
	case v.IsUndefined():
		return "nothing: end the file with return app.run()"
	case v.IsStruct():
		return fmt.Sprintf("%T", v.Struct())
	}
	return v.Typeof()
}
