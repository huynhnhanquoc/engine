package host

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Môi trường của host (owner 23/11/2026): environment("./.env", …) của thư viện "application", chỉ trong
// application.work. Theo luật của docs/KITWORK_ENVIRONMENT.md (bản file.env 28/10, nay đổi tên): các file nêu bằng chữ
// viết sẵn, trong thư mục của application.work; file sau đè file trước theo từng khoá; biến của hệ điều hành thắng mọi
// file (ở host); không có file không phải lỗi (production thường không có); đọc một lần, lúc boot, trước khi chạy dòng
// nào. env.KEY là giá trị đã đổi kiểu, null khi không ai đặt nó; env.require("KEY") nói khoá phải có, và boot kiểm mọi
// require của file trước khi chạy (check.go). Chỉ đọc theo tên: Environment là một struct, không phải object, nên
// chương trình không liệt kê được khoá.

// Environment là kết quả của environment(…): các khoá của các file, và biến của hệ điều hành phủ lên.
type Environment struct {
	values map[string]value.Value // đã đổi kiểu, file sau đè file trước
	system func(string) (string, bool)
}

// Key là env.KEY (runtime.Keyed): biến của hệ điều hành (không rỗng) thắng, rồi các file; không ai đặt là null.
func (e *Environment) Key(name string) value.Value {
	if v, found := e.lookup(name); found {
		return v
	}
	return value.Null()
}

// Require là env.require("KEY"): giá trị của khoá. Boot đã kiểm mọi env.require viết trong file trước khi chạy, nên
// thiếu ở đây là biến của hệ điều hành mất đi giữa chừng.
func (e *Environment) Require(name value.Value) value.Value {
	if !name.IsString() {
		return value.Fail(value.TypeCode, `env.require(…) takes the key's name written in place: env.require("STRIPE_SECRET")`)
	}
	if v, found := e.lookup(name.String()); found {
		return v
	}
	return value.Invalid("env.require(%q): no file and no variable of the system sets the key", name.String())
}

func (e *Environment) lookup(name string) (value.Value, bool) {
	if e.system != nil {
		if text, found := e.system(name); found && text != "" {
			return coerce(text, false), true // biến của hệ điều hành không có ngoặc
		}
	}
	v, found := e.values[name]
	return v, found
}

// has là khoá name có giá trị không (cho kiểm require và ghi chú lúc boot).
func (e *Environment) has(name string) bool {
	_, found := e.lookup(name)
	return found
}

// readEnvironment đọc các file paths (tính từ folder) thành một Environment. Một file không có là một ghi chú, không
// phải lỗi; một đường dẫn ra ngoài folder, tuyệt đối, hay một thư mục là lỗi.
func readEnvironment(folder string, paths []string, system func(string) (string, bool)) (*Environment, []string, error) {
	e := &Environment{values: map[string]value.Value{}, system: system}
	var notes []string
	for _, path := range paths {
		full, err := inside(folder, path)
		if err != nil {
			return nil, nil, err
		}
		text, err := os.ReadFile(full)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			notes = append(notes, "environment(\""+path+"\"): no such file, the system's variables only")
			continue
		case err != nil:
			if info, statErr := os.Stat(full); statErr == nil && info.IsDir() {
				return nil, nil, errors.New("environment(\"" + path + "\") is a folder: name a file, like \"./.env\"")
			}
			return nil, nil, err
		}
		for key, v := range parseEnvironment(string(text)) {
			e.values[key] = v
		}
	}
	return e, notes, nil
}

// inside là đường dẫn đầy đủ của path tính từ folder, hay lỗi khi path tuyệt đối hay ra ngoài folder.
func inside(folder, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return "", errors.New("environment(\"" + path + "\"): the path is outside the folder of application.work: write it from there, like \"./.env\"")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("environment(\"" + path + "\"): the path is outside the folder of application.work: write it from there, like \"./.env\"")
	}
	return filepath.Join(folder, clean), nil
}

// parseEnvironment đọc các dòng KEY=VALUE: dòng trống và dòng # bỏ qua, "export " trước khoá được, ngoặc kép hay ngoặc
// đơn quanh giá trị được bỏ và nhớ (giá trị trong ngoặc luôn là chuỗi); KEY= rỗng là không có giá trị, giá trị của file
// trước giữ nguyên.
func parseEnvironment(source string) map[string]value.Value {
	values := map[string]value.Value{}
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, text, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		text = strings.TrimSpace(text)
		quoted := len(text) >= 2 && (text[0] == '"' && text[len(text)-1] == '"' || text[0] == '\'' && text[len(text)-1] == '\'')
		if quoted {
			text = text[1 : len(text)-1]
		}
		if text == "" {
			continue
		}
		values[key] = coerce(text, quoted)
	}
	return values
}

// coerce là giá trị của một chữ trong .env (mục 4 của spec): trong ngoặc là chuỗi; true, false (mọi kiểu hoa thường)
// là boolean; một số chỉ ở dạng chuẩn, dạng viết lại số ấy ra đúng chữ cũ (42, -7, 3.14); 0123, +84…, 1e3, 1.10, -0,
// Infinity, NaN giữ là chuỗi, để mã bưu chính, số phiên bản không mất chữ số nào.
func coerce(text string, quoted bool) value.Value {
	if quoted {
		return value.String(text)
	}
	switch strings.ToLower(text) {
	case "true":
		return value.Boolean(true)
	case "false":
		return value.Boolean(false)
	}
	n, err := strconv.ParseFloat(text, 64)
	finite := err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && !(n == 0 && math.Signbit(n))
	if finite && strconv.FormatFloat(n, 'f', -1, 64) == text {
		return value.Number(n)
	}
	return value.String(text)
}
