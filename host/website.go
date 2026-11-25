package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huynhnhanquoc/engine/blueprint/application"
	"github.com/huynhnhanquoc/engine/blueprint/website"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/database"
)

// Cửa website (24/11/2026, lát 1 và 2 của "engine3 trả lời request thật"): nghe các địa chỉ của website(), và mỗi
// request chạy chuỗi của host trên một lượt chạy riêng, có trần byte và năng lượng: các guard, rồi routes chọn thư mục
// site, memory và energy đặt trần cho lượt chạy của site (site.go), rồi file tĩnh hay một cửa của site (static.go,
// lát 4: warm chọn site giữ file trong RAM). Gửi câu trả lời; rồi đúng một trong success hay error, và finally
// (docs/KITWORK_APPLICATION.md mục 5). Chưa có, mỗi thứ một ghi chú lúc nạp: TLS (certificates, ssl: mọi cổng nói HTTP
// thường), concurrency.

// HostMemory, HostEnergy là trần của lượt chạy của host cho một request: chuỗi guard, routes, memory, energy, rồi các
// hàm sau khi gửi (một lượt cho cả chuỗi, mục 12); cũng là trần của site khi website không viết memory, energy. Bằng
// trần mặc định của một lượt chạy; bản thiết kế chưa có chữ nào đặt trần của chuỗi host.
const (
	HostMemory = runtime.DefaultMemory
	HostEnergy = runtime.DefaultEnergy
)

// Website là cửa website của một bản thiết kế đã boot: một http.Handler, và các địa chỉ nó nghe.
type Website struct {
	// Notes là các ghi chú lúc nạp (địa chỉ bị bỏ, chữ chưa phục vụ): không phải lỗi, engine in ra.
	Notes []string
	// Log là nơi engine viết lỗi của code (một guard ném, một hàm sau khi gửi ném); nil là os.Stderr.
	Log io.Writer
	// StaticCheck là khoảng giữa hai lần so một file giữ ấm với đĩa (static.go); 0 là một giây. Đặt trước request đầu.
	StaticCheck time.Duration

	folder     string    // thư mục của application.work: routes tính từ đây
	console    io.Writer // nơi console.log của các site viết, đầu dòng [tên site]
	addresses  []string
	guards     []value.Value
	guardNames []string // "application.website.guard[2]": dựng một lần, cho lời báo của guard ấy
	routes     value.Value
	memory     value.Value
	energy     value.Value
	warm       value.Value // true, false, hay (context) => boolean: site giữ file tĩnh trong RAM (static.go)
	success    []value.Value
	errors     []value.Value
	finally    []value.Value

	sitesMu sync.Mutex
	sites   map[string]*siteLoad // thư mục site → lần biên dịch của nó

	databases *database.Files // các file database của lần boot: thư viện "database" của các site mở file ở đây

	once   sync.Once
	server *http.Server
}

// LoadWebsite nạp cửa website của một lần boot: nil, không lỗi, khi application không có website. Bản thiết kế được
// đóng băng ở đây (value.FreezeShared) trước khi request nào chạy: các request chạy cùng lúc gọi chung các hàm của nó.
// Mọi trường sai được kể một lần, theo đường dẫn trường.
func LoadWebsite(booted Booted) (*Website, error) {
	blueprint := booted.Blueprint
	a, ok := value.As[application.Application](blueprint)
	if !ok {
		return nil, errors.New("the blueprint is not the application of application.work")
	}
	blueprint.FreezeShared()
	field := a.Fields().Website
	if field.IsUndefined() {
		return nil, nil
	}
	ws, ok := value.As[website.Website](field)
	if !ok {
		return nil, fmt.Errorf("application.website is written app.website(…).listen(…), got %s", describe(field))
	}
	f := ws.Fields()
	w := &Website{folder: booted.Folder, console: booted.Console, sites: map[string]*siteLoad{}, databases: booted.Databases}
	if w.databases == nil {
		w.databases = database.NewFiles()
	}
	if w.console == nil {
		w.console = os.Stdout
	}
	list, problems := Addresses("application.website.listen", f.Listen)
	kept, dropped := Filter(list)
	w.addresses = kept
	for _, d := range dropped {
		w.Notes = append(w.Notes, d.String())
	}
	functions := func(path string, items []value.Value) []value.Value {
		for i, item := range items {
			if !item.IsFunction() {
				problems = append(problems, fmt.Errorf("%s[%d]: a function (context) => …, got %s", path, i, describe(item)))
			}
		}
		return items
	}
	w.guards = functions("application.website.guard", f.Guards)
	for i := range w.guards {
		w.guardNames = append(w.guardNames, fmt.Sprintf("application.website.guard[%d]", i))
	}
	w.success = functions("application.website.success", f.Success)
	w.errors = functions("application.website.error", f.Errors)
	w.finally = functions("application.website.finally", f.Finally)
	w.routes, w.memory, w.energy, w.warm = f.Routes, f.Memory, f.Energy, f.Warm
	if !w.warm.IsUndefined() && !w.warm.IsBoolean() && !w.warm.IsFunction() {
		problems = append(problems, fmt.Errorf("application.website.warm: true, false, or (context) => boolean, got %s", describe(w.warm)))
	}
	switch {
	case w.routes.IsUndefined():
		w.Notes = append(w.Notes, "application.website has no routes: a request the guards let through gets 404")
	case !w.routes.IsString() && !w.routes.IsFunction():
		problems = append(problems, fmt.Errorf(`application.website.routes: the site's folder, "apps/…", or (context) => "apps/…", got %s`, describe(w.routes)))
	}
	if !w.memory.IsUndefined() && !w.memory.IsFunction() {
		if _, err := w.memory.ByteSize(); err != nil {
			problems = append(problems, fmt.Errorf("application.website.memory: %v", err))
		}
	}
	if !w.energy.IsUndefined() && !w.energy.IsFunction() && !positiveWhole(w.energy) {
		problems = append(problems, fmt.Errorf("application.website.energy: a whole number of instructions, or (context) => number, got %s", describe(w.energy)))
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	for _, later := range []struct {
		name string
		v    value.Value
	}{
		{"certificates", f.Certificates}, {"ssl", f.SSL}, {"concurrency", f.Concurrency},
	} {
		if !later.v.IsUndefined() {
			w.Notes = append(w.Notes, fmt.Sprintf("application.website.%s is not served by this engine yet: every port speaks plain HTTP", later.name))
		}
	}
	return w, nil
}

// positiveWhole là v là một số nguyên dương.
func positiveWhole(v value.Value) bool {
	return v.IsNumber() && v.Number() >= 1 && v.Number() == float64(int64(v.Number()))
}

// Addresses là các địa chỉ website nghe, đã lọc (listen.go).
func (w *Website) Addresses() []string { return w.addresses }

// answer là câu trả lời engine gửi cho một request, và thứ các hàm sau khi gửi đọc ở context.response.
type answer struct {
	status      int
	location    string // chuyển hướng
	body        string
	contentType string      // "" là chữ thuần
	allow       string      // header Allow (405, OPTIONS)
	retryAfter  int         // header Retry-After, giây (429)
	file        *servedFile // một file tĩnh: http.ServeContent gửi nó (mã của nó: 200, 206, 304, 416)
	// response.error: code "" là thành công
	code, message, at string
}

// ServeHTTP là một request: chuỗi của host trên một lượt chạy mới, site trên lượt của nó, câu trả lời, rồi các hàm sau
// khi gửi trên lượt của host (một lượt mới nếu chuỗi đã dùng hết năng lượng hay bộ nhớ của nó).
func (w *Website) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	c := &requestContext{stage: hostGuard}
	c.request = &c.own
	newRequest(c.request, r)
	m := runtime.Take(runtime.Options{Memory: HostMemory, Energy: HostEnergy, Context: r.Context()})
	defer m.Release()
	a := w.decide(m, c)
	a.status = send(rw, r, a)
	a.file.close()
	if f, ok := rw.(http.Flusher); ok {
		f.Flush() // đã gửi: các hàm sau khi gửi không đổi được nó
	}
	w.after(m, c, a)
}

// decide chạy các guard của host theo thứ tự viết (next đi tiếp, error và redirect dừng ở đó), rồi routes, memory,
// energy, rồi site.
func (w *Website) decide(m *runtime.Machine, c *requestContext) answer {
	this := value.Struct(c)
	for i, guard := range w.guards {
		if a, stop := w.guard(m, c, this, guard, w.guardNames[i]); stop {
			return a
		}
	}
	c.stage = hostChoice
	if w.routes.IsUndefined() {
		return notFound(c)
	}
	folder, failure := w.folderOf(m, this)
	if failure.IsInvalid() {
		return w.failed(c, failure)
	}
	memory, failure := w.memoryOf(m, this)
	if failure.IsInvalid() {
		return w.failed(c, failure)
	}
	energy, failure := w.energyOf(m, this)
	if failure.IsInvalid() {
		return w.failed(c, failure)
	}
	s, err := w.siteOf(folder)
	if err != nil {
		return w.failed(c, value.Error(err))
	}
	return w.serveSite(s, c, memory, energy)
}

// guard chạy một guard (của host hay của site): context.next đi tiếp và các khoá nó đặt vào request; một quyết định khác
// là câu trả lời (stop). Một guard ném, hay trả thứ khác một quyết định, là 500. name là guard ấy trong lời báo.
func (w *Website) guard(m *runtime.Machine, c *requestContext, this, guard value.Value, name string) (answer, bool) {
	r := m.Call(guard, this)
	if r.IsInvalid() {
		return w.failed(c, r), true
	}
	d, ok := value.As[decision](r)
	if !ok {
		return w.failed(c, value.Fail(value.TypeCode, "%s returned %s: a guard returns context.next(), context.error(reason), "+
			"context.redirect(url) or an answer", name, describeReturn(r)).Within(where(guard))), true
	}
	if d.kind == next {
		c.request.set = append(c.request.set, d.set...)
		return answer{}, false
	}
	return answerOf(d), true
}

// answerOf là câu trả lời của một quyết định: từ chối (REFUSED), chuyển hướng, hay một câu trả lời của site.
func answerOf(d *decision) answer {
	switch d.kind {
	case refusal:
		return answer{status: d.status, body: d.message, code: "REFUSED", message: d.message}
	case redirection:
		return answer{status: d.status, location: d.url}
	}
	return answer{status: d.status, body: d.message, contentType: d.contentType}
}

// folderOf là thư mục site routes chọn: một chuỗi, hay hàm trả một chuỗi; tính từ thư mục manifest, không ra ngoài nó,
// và có một thư mục cha (gốc app) không phải chính thư mục manifest: "apps/03dt/shop.vn".
func (w *Website) folderOf(m *runtime.Machine, this value.Value) (string, value.Value) {
	v := w.routes
	if v.IsFunction() {
		if v = m.Call(v, this); v.IsInvalid() {
			return "", v
		}
	}
	if !v.IsString() || v.String() == "" {
		return "", value.Fail(value.TypeCode, `application.website.routes returned %s: the site's folder, like "apps/03dt/shop.vn"`, describeReturn(v)).Within(where(w.routes))
	}
	folder := path.Clean(v.String())
	if strings.Contains(v.String(), "\\") || path.IsAbs(folder) || folder == ".." || strings.HasPrefix(folder, "../") ||
		strings.Contains(folder, ":") {
		return "", value.Fail(value.TypeCode, "application.website.routes returned %q: a folder inside the folder of application.work, "+
			"written with /", v.String()).Within(where(w.routes))
	}
	if path.Dir(folder) == "." {
		return "", value.Fail(value.TypeCode, "application.website.routes returned %q: a site sits in an app's folder, like "+
			"apps/<identity>/<domain>: its parent is the app's root, where app/… imports start", v.String()).Within(where(w.routes))
	}
	return folder, value.Value{}
}

// memoryOf là trần byte của lượt chạy của site: memory của website ("64mb", hay hàm trả nó), HostMemory khi không viết.
func (w *Website) memoryOf(m *runtime.Machine, this value.Value) (int64, value.Value) {
	v := w.memory
	if v.IsUndefined() {
		return HostMemory, value.Value{}
	}
	if v.IsFunction() {
		if v = m.Call(v, this); v.IsInvalid() {
			return 0, v
		}
	}
	n, err := v.ByteSize()
	if err != nil {
		return 0, value.Fail(value.TypeCode, "application.website.memory returned %s: %v", describeReturn(v), err).Within(where(w.memory))
	}
	return n, value.Value{}
}

// energyOf là trần năng lượng của lượt chạy của site: energy của website (một số, hay hàm trả nó), HostEnergy khi
// không viết.
func (w *Website) energyOf(m *runtime.Machine, this value.Value) (int64, value.Value) {
	v := w.energy
	if v.IsUndefined() {
		return HostEnergy, value.Value{}
	}
	if v.IsFunction() {
		if v = m.Call(v, this); v.IsInvalid() {
			return 0, v
		}
	}
	if !positiveWhole(v) {
		return 0, value.Fail(value.TypeCode, "application.website.energy returned %s: a whole number of instructions, 1 or more",
			describeReturn(v)).Within(where(w.energy))
	}
	return int64(v.Number()), value.Value{}
}

// failed là 500 cho một lỗi của code: khách chỉ nhận "Internal Server Error" (lời báo có đường dẫn file), engine viết
// lỗi đầy đủ vào Log, các hàm error đọc nó ở context.response.error.
func (w *Website) failed(c *requestContext, failure value.Value) answer {
	fmt.Fprintf(w.log(), "website: %s %s%s: %v\n", c.request.http.Method, c.request.hostname, c.request.http.URL.Path, failure.Error())
	return answer{status: http.StatusInternalServerError, body: http.StatusText(http.StatusInternalServerError),
		code: failure.Code(), message: failure.Message(), at: failure.At()}
}

// describeReturn là thứ một hàm trả, cho lời báo.
func describeReturn(v value.Value) string {
	if v.IsUndefined() {
		return "undefined (no return?)"
	}
	if v.IsString() {
		return strconv.Quote(v.String())
	}
	return describe(v)
}

// send viết câu trả lời và trả mã đã gửi: chuyển hướng là Location và mã; một file tĩnh là loại, nosniff, ETag,
// Cache-Control, rồi http.ServeContent (Range, If-None-Match, If-Modified-Since, HEAD); 204, 304 không có thân; còn lại
// là mã, Content-Type và thân, trừ thân của một HEAD.
func send(rw http.ResponseWriter, r *http.Request, a answer) int {
	h := rw.Header()
	if a.file != nil {
		h["Content-Type"] = a.file.kind
		h["X-Content-Type-Options"] = nosniff
		h["Etag"] = a.file.etag
		h["Cache-Control"] = a.file.cache
		sw := &statusWriter{ResponseWriter: rw}
		http.ServeContent(sw, r, "", a.file.modified, a.file.content)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		return sw.status
	}
	head := r.Method == http.MethodHead
	if a.location != "" {
		h.Set("Location", a.location)
		rw.WriteHeader(a.status)
		return a.status
	}
	if a.allow != "" {
		h.Set("Allow", a.allow)
	}
	if a.retryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(a.retryAfter))
	}
	if a.status == http.StatusNoContent || a.status == http.StatusNotModified {
		rw.WriteHeader(a.status)
		return a.status
	}
	contentType := a.contentType
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	rw.WriteHeader(a.status)
	if !head {
		io.WriteString(rw, a.body)
	}
	return a.status
}

// statusWriter giữ mã http.ServeContent viết (200, 206, 304, 416): context.response.status của một file tĩnh.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// after chạy đúng một trong success hay error (theo response.error), rồi finally, theo thứ tự viết. Một hàm ném thì
// engine viết lỗi ấy vào Log và chạy tiếp các hàm còn lại; câu trả lời đã gửi không đổi.
func (w *Website) after(m *runtime.Machine, c *requestContext, a answer) {
	hooks, name := w.success, "success"
	if a.code != "" {
		hooks, name = w.errors, "error"
	}
	if len(hooks) == 0 && len(w.finally) == 0 {
		return
	}
	if m.Energy() <= 0 { // chuỗi của host dừng vì hết năng lượng hay bộ nhớ: các hàm sau khi gửi có lượt của chúng
		m = runtime.Take(runtime.Options{Memory: HostMemory, Energy: HostEnergy, Context: c.request.http.Context()})
		defer m.Release()
	}
	c.stage, c.response = afterSend, responseOf(a)
	this := value.Struct(c)
	w.hooks(m, this, name, hooks)
	w.hooks(m, this, "finally", w.finally)
}

// hooks chạy các hàm sau khi gửi của một danh sách; một hàm ném thì viết lỗi vào Log và chạy tiếp.
func (w *Website) hooks(m *runtime.Machine, this value.Value, name string, list []value.Value) {
	for i, hook := range list {
		if r := m.Call(hook, this); r.IsInvalid() {
			fmt.Fprintf(w.log(), "website: application.website.%s[%d]: %v\n", name, i, r.Error())
		}
	}
}

// responseOf là context.response: { status, error }, error null khi thành công, hay { code, message, at } (at null
// khi lỗi không nằm trong code, như một guard từ chối). Đã đóng băng.
func responseOf(a answer) value.Value {
	failure := value.Null()
	if a.code != "" {
		failure = value.ObjectFrom(failureShape, []value.Value{text(a.code), value.String(a.message), text(a.at)}).Freeze()
	}
	return value.ObjectFrom(responseShape, []value.Value{value.Number(float64(a.status)), failure}).Freeze()
}

// responseShape, failureShape là khoá của context.response và của response.error, dùng chung.
var (
	responseShape = value.NewShape([]string{"status", "error"})
	failureShape  = value.NewShape([]string{"code", "message", "at"})
)

// text là s, null khi rỗng.
func text(s string) value.Value {
	if s == "" {
		return value.Null()
	}
	return value.String(s)
}

func (w *Website) log() io.Writer {
	if w.Log == nil {
		return os.Stderr
	}
	return w.Log
}

// Open mở một socket cho mỗi địa chỉ website nghe (listen.go: địa chỉ bị một địa chỉ lớn hơn chứa thì đi qua socket
// lớn). Lỗi ở một địa chỉ thì đóng các socket đã mở.
func (w *Website) Open() ([]net.Listener, error) {
	var listeners []net.Listener
	for _, s := range Sockets([]Door{{Name: "website", Addresses: w.addresses}}) {
		l, err := net.Listen("tcp", s.Address)
		if err != nil {
			for _, opened := range listeners {
				opened.Close()
			}
			return nil, fmt.Errorf("website: listen %s: %w", s.Address, err)
		}
		listeners = append(listeners, l)
	}
	return listeners, nil
}

// Serve phục vụ request trên listener tới khi Shutdown (trả http.ErrServerClosed). Gọi được cho nhiều listener cùng lúc.
func (w *Website) Serve(listener net.Listener) error { return w.httpServer().Serve(listener) }

// Shutdown đóng các listener, chờ các request đang chạy xong (tới khi ctx hết), rồi trả.
func (w *Website) Shutdown(ctx context.Context) error { return w.httpServer().Shutdown(ctx) }

func (w *Website) httpServer() *http.Server {
	w.once.Do(func() {
		w.server = &http.Server{
			Handler:           w,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
			ErrorLog:          log.New(w.log(), "website: ", 0),
		}
	})
	return w.server
}
