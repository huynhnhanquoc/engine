package host

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/huynhnhanquoc/engine/blueprint/router"
	"github.com/huynhnhanquoc/engine/core/compiler"
	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/syntax"
	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/database"
)

// Site của một request (24/11/2026, lát 2 của "engine3 trả lời request thật"). routes của website chọn một thư mục site,
// tính từ thư mục manifest; gốc app là thư mục cha của nó (import "app/…" tính từ đó). Engine biên dịch site ở request
// đầu tới nó, một lần dù nhiều request cùng tới, rồi giữ: câu hỏi 6 của docs/KITWORK_APPLICATION.md (biên dịch lúc boot)
// còn mở, và bản thiết kế chưa nêu các site, nên engine không tự quét thư mục nào. Mỗi thư mục của site là một đường dẫn
// (thư mục bắt đầu bằng "." hay "_" không phải, trừ .well-known); router.work của nó là một bản thiết kế
// (blueprint/router, blueprint/README.md "Route"). File tĩnh của site: static.go (lát 4). Chưa có: đoạn động
// {id[number]}, trang content.ui, thân request; route.cache được đọc và kiểm, chưa giữ câu trả lời nào (một dòng log).

// site là một thư mục site đã biên dịch: cây thư mục của nó.
type site struct {
	folder string // "apps/03dt/shop.vn", tính từ thư mục manifest
	root   *node
	static siteStatic
}

// node là một thư mục của site: các chuỗi all(…) và methods(…) của router.work của nó, các cửa, các thư mục con.
type node struct {
	file     string // "shop.vn/don-hang/router.work", tính từ gốc app; "" khi thư mục không có router.work
	children map[string]*node
	chains   []siteChain
	doors    map[string]*door
}

// siteChain là một chuỗi all(…) (branch: thư mục này và mọi thư mục con) hay methods(…) (chỉ thư mục này).
type siteChain struct {
	branch  bool
	methods []string // tên method, hay ["*"]
	rules   []siteRule
}

// applies là chuỗi áp cho method.
func (c siteChain) applies(method string) bool {
	return c.methods[0] == "*" || slices.Contains(c.methods, method)
}

// siteRule là một luật: một guard, hay một limit.
type siteRule struct {
	guard   value.Value
	limiter *limiter
	at      string // "shop.vn/router.work", cho lời báo
	name    string // "a guard of shop.vn/router.work": dựng một lần, cho lời báo của guard
}

// door là một cửa: các luật đứng trước nó và hàm trả lời.
type door struct {
	rules   []siteRule
	handler value.Value
}

// siteLoad là một lần biên dịch một thư mục site: kết quả, và once để nhiều request cùng tới chỉ biên dịch một lần.
type siteLoad struct {
	once sync.Once
	site *site
	err  error
}

// siteOf là site ở folder: biên dịch lần đầu, giữ cả lỗi biên dịch của một thư mục có thật (request sau thấy cùng lỗi,
// không biên dịch lại). Thư mục không có thì không giữ gì: routes đọc từ request không làm phình bộ nhớ.
func (w *Website) siteOf(folder string) (*site, error) {
	w.sitesMu.Lock()
	load, found := w.sites[folder]
	w.sitesMu.Unlock()
	if !found { // lần đầu: hỏi đĩa một lần, không mỗi request (một lời gọi tới đĩa tốn hàng chục micro giây trên Windows)
		dir := filepath.Join(w.folder, filepath.FromSlash(folder))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("the site folder %s does not exist", folder)
		}
		w.sitesMu.Lock()
		if load, found = w.sites[folder]; !found {
			load = &siteLoad{}
			w.sites[folder] = load
		}
		w.sitesMu.Unlock()
	}
	load.once.Do(func() { load.site, load.err = w.compileSite(folder) })
	return load.site, load.err
}

// siteFiles nạp các file của một site: router.work của mỗi thư mục, và các file chúng import, mỗi file một lần, trên
// một lượt chạy (năng lượng boot của cả site).
type siteFiles struct {
	base    string // thư mục của gốc app trên đĩa
	machine *runtime.Machine
	globals map[string]value.Value // console của site: đầu dòng [tên site]
	done    map[string]map[string]value.Value
	loading []string
	shared  []value.Value // các route và export: đóng băng khi site xong
	cached  []string      // các router.work có route.cache (chưa phục vụ)
	// libraries là "router", "schema", "database" (connection() là database của gốc app)
	libraries map[string]map[string]value.Value
}

// compileSite biên dịch thư mục site folder và đóng băng mọi thứ nó dựng (các request chạy cùng lúc đọc chung).
func (w *Website) compileSite(folder string) (*site, error) {
	app, name := path.Dir(folder), path.Base(folder)
	base := filepath.Join(w.folder, filepath.FromSlash(app))
	l := &siteFiles{
		base:    base,
		machine: runtime.New(runtime.Options{Energy: BootEnergy}),
		globals: map[string]value.Value{"console": javascript.Console(w.console, "["+name+"] ")},
		done:    map[string]map[string]value.Value{},
		libraries: map[string]map[string]value.Value{
			"router":   {"router": constructor("router", router.New)},
			"schema":   database.SchemaLibrary(),
			"database": database.Library(w.databases, base, app),
		},
	}
	root, err := l.node(name)
	if err != nil {
		return nil, err
	}
	for _, v := range l.shared {
		v.FreezeShared()
	}
	for _, file := range l.cached {
		fmt.Fprintf(w.log(), "website: %s: route.cache(…) in %s is not served yet: its GET door runs on every request\n", folder, file)
	}
	s := &site{folder: folder, root: root, static: siteStatic{dir: filepath.Join(w.folder, filepath.FromSlash(folder))}}
	if w.warms(folder) {
		s.static.kept = &keptFiles{every: w.StaticCheck}
		held, size := s.static.warm()
		fmt.Fprintf(w.log(), "website: %s: warm: %d files in memory (%d KB)\n", folder, held, (size+1023)/1024)
	}
	return s, nil
}

// node biên dịch thư mục rel (tính từ gốc app) và các thư mục con của nó.
func (l *siteFiles) node(rel string) (*node, error) {
	n := &node{children: map[string]*node{}, doors: map[string]*door{}}
	dir := filepath.Join(l.base, filepath.FromSlash(rel))
	source, err := os.ReadFile(filepath.Join(dir, "router.work"))
	switch {
	case err == nil:
		n.file = rel + "/router.work"
		if err := l.route(n, string(source)); err != nil {
			return nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%s/router.work: %v", rel, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", rel, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || (strings.HasPrefix(name, ".") && name != ".well-known") || strings.HasPrefix(name, "_") {
			continue
		}
		if strings.ContainsAny(name, "{}") {
			return nil, fmt.Errorf("%s/%s: a folder written {…} matches a part of the path, which comes in a later slice of engine3", rel, name)
		}
		child, err := l.node(rel + "/" + name)
		if err != nil {
			return nil, err
		}
		n.children[name] = child
	}
	return n, nil
}

// route chạy router.work của n và đọc route nó trả: các chuỗi all(…), methods(…) và các cửa. Một chuỗi không có chữ kết
// thúc là lỗi.
func (l *siteFiles) route(n *node, source string) error {
	program, err := syntax.Parse(n.file, source)
	if err != nil {
		return err
	}
	modules, err := l.imports(program, n.file, path.Dir(n.file))
	if err != nil {
		return err
	}
	main, err := compiler.Compile(program, compiler.Options{Modules: modules, Globals: l.globals})
	if err != nil {
		return err
	}
	result := l.machine.Run(main)
	if result.IsInvalid() {
		return result.Error()
	}
	r, ok := value.As[router.Route](result)
	if !ok {
		return fmt.Errorf("%s: must return router(…), got %s", n.file, describe(result))
	}
	for i, c := range r.Chains() {
		var rules []siteRule
		var words []string
		for _, rule := range c.Rules() {
			words = append(words, rule.Kind+"(…)")
			switch rule.Kind {
			case "guard":
				rules = append(rules, siteRule{guard: rule.Value, at: n.file, name: "a guard of " + n.file})
			case "limit":
				rules = append(rules, siteRule{limiter: newLimiter(rule.Rate, rule.Per), at: n.file})
			case "cache":
				if !slices.Contains(l.cached, n.file) {
					l.cached = append(l.cached, n.file)
				}
			}
		}
		end := c.End()
		switch end.Kind {
		case "door":
			n.doors[end.Method] = &door{rules: rules, handler: end.Handler}
		case "all", "methods":
			n.chains = append(n.chains, siteChain{branch: end.Kind == "all", methods: end.Methods, rules: rules})
		default:
			return fmt.Errorf(`%s: chain %d, route.%s, has no end: write .all("*"), .methods("*") or a door after it`,
				n.file, i+1, strings.Join(words, "."))
		}
	}
	l.shared = append(l.shared, result)
	return nil
}

// imports là các module program (file, trong thư mục dir của gốc app) thấy: các thư viện "router", "schema",
// "database", và các file nó import: "app/…" tính từ gốc app, "./…", "../…" từ thư mục của nó, không ra ngoài gốc app.
func (l *siteFiles) imports(program *syntax.Program, file, dir string) (map[string]map[string]value.Value, error) {
	modules := make(map[string]map[string]value.Value, len(l.libraries)+len(program.Imports))
	for name, members := range l.libraries {
		modules[name] = members
	}
	for _, imp := range program.Imports {
		var name string
		switch {
		case strings.HasPrefix(imp.From, "app/"):
			name = path.Clean(strings.TrimPrefix(imp.From, "app/"))
		case relative(imp.From):
			name = path.Clean(path.Join(dir, imp.From))
		default:
			continue // một thư viện: compiler nói khi không có
		}
		fail := func(format string, arguments ...any) error {
			return &syntax.Error{File: file, Pos: imp.Pos, Message: fmt.Sprintf(format, arguments...)}
		}
		if name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return nil, fail("import %q: the file is outside the app's folder", imp.From)
		}
		if !strings.HasSuffix(name, ".work") {
			return nil, fail("import %q: write the whole name of the file, with .work: %q", imp.From, imp.From+".work")
		}
		exports, err := l.load(name, fail)
		if err != nil {
			return nil, err
		}
		modules[imp.From] = exports
	}
	return modules, nil
}

// load nạp file name (tính từ gốc app) một lần và trả các export của nó.
func (l *siteFiles) load(name string, fail func(string, ...any) error) (map[string]value.Value, error) {
	if exports, found := l.done[name]; found {
		return exports, nil
	}
	if slices.Contains(l.loading, name) {
		return nil, fail("import cycle: %s → %s", strings.Join(l.loading, " → "), name)
	}
	source, err := os.ReadFile(filepath.Join(l.base, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fail("import of %s: no such file", name)
	}
	if err != nil {
		return nil, fail("import of %s: %v", name, err)
	}
	program, err := syntax.Parse(name, string(source))
	if err != nil {
		return nil, err
	}
	l.loading = append(l.loading, name)
	modules, err := l.imports(program, name, path.Dir(name))
	l.loading = l.loading[:len(l.loading)-1]
	if err != nil {
		return nil, err
	}
	main, err := compiler.Compile(program, compiler.Options{Modules: modules, Globals: l.globals, Module: true})
	if err != nil {
		return nil, err
	}
	result := l.machine.Run(main)
	if result.IsInvalid() {
		return nil, result.Error()
	}
	exports := map[string]value.Value{}
	for _, key := range result.Keys() {
		exports[key], _ = result.Get(key)
	}
	l.done[name] = exports
	l.shared = append(l.shared, result)
	return exports, nil
}

// serveSite trả lời request c bằng site s, trên một lượt chạy mới với trần memory, energy của website: tìm thư mục theo
// đường dẫn, chạy luật all(…) của các thư mục cha từ gốc xuống, các luật của chính thư mục theo thứ tự viết, luật đứng
// trước cửa, rồi cửa. HEAD đi theo GET (Go bỏ thân); OPTIONS là 204 kèm Allow.
func (w *Website) serveSite(s *site, c *requestContext, memory, energy int64) answer {
	method := strings.ToLower(c.request.http.Method)
	if method == "head" {
		method = "get"
	}
	if method == "get" {
		if name, ok := staticName(c.request.http.URL.Path); ok {
			if file := s.static.open(name); file != nil {
				if a, stop := w.staticRules(s, c, name, memory, energy); stop {
					file.close()
					return a
				}
				return answer{status: http.StatusOK, file: file}
			}
		}
	}
	var segments []string
	for _, segment := range strings.Split(c.request.http.URL.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	nodes := []*node{s.root}
	for _, segment := range segments {
		child, found := nodes[len(nodes)-1].children[segment]
		if !found {
			return notFound(c)
		}
		nodes = append(nodes, child)
	}
	target := nodes[len(nodes)-1]
	if len(target.doors) == 0 {
		return notFound(c)
	}
	if method == "options" {
		return answer{status: 204, allow: allowOf(target)}
	}
	m := runtime.Take(runtime.Options{Memory: memory, Energy: energy, Context: c.request.http.Context()})
	defer m.Release()
	c.stage = siteGuard
	this := value.Struct(c)
	for i, n := range nodes {
		for _, chain := range n.chains {
			if (chain.branch || i == len(nodes)-1) && chain.applies(method) {
				if a, stop := w.rules(m, c, this, chain.rules, c.request.ip); stop {
					return a
				}
			}
		}
	}
	d := target.doors[method]
	if d == nil {
		message := c.request.http.Method + " is not a door of " + c.request.http.URL.Path
		return answer{status: 405, body: message, allow: allowOf(target), code: "METHOD_NOT_ALLOWED", message: message}
	}
	if a, stop := w.rules(m, c, this, d.rules, c.request.ip+"\x00/"+strings.Join(segments, "/")); stop {
		return a
	}
	c.stage = siteDoor
	r := m.Call(d.handler, this)
	if r.IsInvalid() {
		return w.failed(c, r)
	}
	decided, ok := value.As[decision](r)
	if !ok { // next, error: allows() đã từ chối chúng trong một cửa
		return w.failed(c, value.Fail(value.TypeCode, "the %s door of %s returned %s: a door returns context.text(…), "+
			"context.json(…) or context.redirect(url)", method, target.file, describeReturn(r)).Within(where(d.handler)))
	}
	return answerOf(decided)
}

// staticRules chạy các luật phủ file tĩnh name: all(…) của các thư mục từ gốc tới thư mục của file, rồi all(…) và
// methods(…) của chính thư mục ấy, theo thứ tự viết, như trước một cửa ở thư mục ấy (blueprint/README.md "Route": luật
// của thư mục áp cả cho file tĩnh của các thư mục nó phủ). Một thư mục site không biên dịch (bắt đầu bằng "_") không có
// luật của riêng nó; luật all của các thư mục cha vẫn phủ. Không luật nào áp thì không dựng lượt chạy nào.
func (w *Website) staticRules(s *site, c *requestContext, name string, memory, energy int64) (answer, bool) {
	var buffer [8]*node
	nodes := append(buffer[:0], s.root)
	own := true // thư mục của file có trong cây: methods của nó phủ file
	for rest := name; ; {
		segment, after, folder := strings.Cut(rest, "/")
		if !folder {
			break // đoạn cuối là file
		}
		rest = after
		child, found := nodes[len(nodes)-1].children[segment]
		if !found {
			own = false
			break
		}
		nodes = append(nodes, child)
	}
	var m *runtime.Machine
	defer func() {
		if m != nil {
			m.Release()
		}
	}()
	var this value.Value
	for i, n := range nodes {
		for _, chain := range n.chains {
			if (chain.branch || own && i == len(nodes)-1) && chain.applies("get") {
				if m == nil {
					m = runtime.Take(runtime.Options{Memory: memory, Energy: energy, Context: c.request.http.Context()})
					c.stage = siteGuard
					this = value.Struct(c)
				}
				if a, stop := w.rules(m, c, this, chain.rules, c.request.ip); stop {
					return a, true
				}
			}
		}
	}
	return answer{}, false
}

// rules chạy các luật theo thứ tự: một limit quá trần là 429; một guard đi tiếp (các khoá nó đặt vào request), hay trả
// lời và dừng ở đó. stop là có câu trả lời.
func (w *Website) rules(m *runtime.Machine, c *requestContext, this value.Value, rules []siteRule, key string) (answer, bool) {
	for _, rule := range rules {
		if rule.limiter != nil {
			if ok, retry := rule.limiter.allow(key, time.Now()); !ok {
				message := "too many requests: a limit of " + rule.at + ", try again later"
				return answer{status: 429, body: message, retryAfter: max(1, int((retry+time.Second-1)/time.Second)),
					code: "TOO_MANY_REQUESTS", message: message}, true
			}
			continue
		}
		if a, stop := w.guard(m, c, this, rule.guard, rule.name); stop {
			return a, true
		}
	}
	return answer{}, false
}

// notFound là 404 của một đường dẫn không có thư mục, hay một thư mục không có cửa.
func notFound(c *requestContext) answer {
	message := "no route for " + c.request.http.URL.Path
	return answer{status: 404, body: message, code: "NOT_FOUND", message: message}
}

// allowOf là header Allow của một thư mục: các cửa của nó, HEAD theo GET, và OPTIONS.
func allowOf(n *node) string {
	var methods []string
	for _, m := range []string{"get", "post", "put", "patch", "delete"} {
		if n.doors[m] != nil {
			methods = append(methods, strings.ToUpper(m))
			if m == "get" {
				methods = append(methods, "HEAD")
			}
		}
	}
	return strings.Join(append(methods, "OPTIONS"), ", ")
}

// where là chỗ của một hàm của chương trình ("shop.vn/router.work:3:11"), "" khi không biết.
func where(f value.Value) string {
	if closure, ok := f.Function().(*runtime.Closure); ok {
		return closure.At()
	}
	return ""
}
