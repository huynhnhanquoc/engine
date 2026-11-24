package host

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynhnhanquoc/engine/core/javascript"
	"github.com/huynhnhanquoc/engine/core/runtime"
	"github.com/huynhnhanquoc/engine/core/value"
)

// File tĩnh (24/11/2026, lát 4 của "engine trả lời request thật"; docs/KITWORK_APPLICATION.md mục 5 "warm" và mục 11
// việc 8; luật của engine2 host/static.go, host/prewarm.go, gọn lại). Một site phục vụ nguyên dạng assets/ ở /assets/…,
// .well-known/, và danh mục file ở gốc (/robots.txt, /favicon.ico, …); không file nào khác của site ra ngoài, dù tên gì:
// không router.work, không .ui, không .env. File tĩnh đi sau chuỗi của host như mọi request (guard, routes, memory,
// energy), rồi các luật của thư mục phủ nó (all của các thư mục cha, all và methods của chính thư mục): không luật nào
// áp thì không dựng lượt chạy nào (serveSite). GET và HEAD đọc file; file không có thì các cửa của site trả lời.
//
// warm của website chọn site được giữ ấm: file tĩnh của nó nằm trong RAM, mỗi file tới 256 KB, cả site tới 32 MB, mỗi
// file so với đĩa (cỡ, giờ ghi) nhiều nhất mỗi giây một lần, sửa tại chỗ thì phục vụ bản mới, xoá thì đọc đĩa (rồi các
// cửa). Site không giữ ấm đọc file từ đĩa ở mỗi request, chép thẳng ra kết nối. warm được hỏi khi site biên dịch, ở
// request đầu tới nó: câu hỏi 6 của spec (biên dịch lúc boot) còn mở.

// keptFileSize là file lớn nhất một site giữ ấm giữ trong RAM, keptFilesSize là tổng nó giữ: lớn hơn, hay quá tổng, thì
// đọc từ đĩa như site không giữ ấm.
const (
	keptFileSize  = 256 << 10
	keptFilesSize = 32 << 20
)

// staticLifetime là thời gian trình duyệt giữ một file mà tên không mang mã băm của nó trước khi hỏi lại: một giờ, đủ
// ngắn để file sửa tại chỗ tới mọi khách trong giờ ấy; ETag và Last-Modified làm lần hỏi sau thành 304 không có byte.
const staticLifetime = time.Hour

// defaultStaticCheck là khoảng giữa hai lần so một file giữ trong RAM với đĩa khi Website.StaticCheck là 0.
const defaultStaticCheck = time.Second

// rootFiles là danh mục file một site phục vụ ở gốc của nó.
var rootFiles = map[string]bool{
	"robots.txt": true, "llms.txt": true, "llms-full.txt": true, "sitemap.xml": true, "favicon.ico": true,
	"apple-touch-icon.png": true, "site.webmanifest": true, "ads.txt": true, "app-ads.txt": true, "humans.txt": true,
}

// staticName là file mà urlPath nêu trong thư mục site, ngăn bằng "/", khi đường dẫn là một đường site phục vụ nguyên
// dạng: dưới /assets/ hay /.well-known/, hay một tên của danh mục ở gốc. Một đường dẫn hệ thống file đọc được thành tên
// khác thì không phải, một file nguồn của site cũng không, dù ở thư mục nào:
//   - một đoạn rỗng, . hay .., dấu \ hay : (một stream của Windows, logo.svg::$DATA);
//   - một đoạn kết thúc bằng dấu chấm hay dấu cách, Windows bỏ đi, nên router.work. mở router.work;
//   - một tên ngắn của Windows, ROUTER~1.WOR, mở router.work dưới một tên không có .work;
//   - một đoạn bắt đầu bằng dấu chấm, trừ .well-known đứng đầu: .env, .shared, .database;
//   - một file .work, .ui hay *.kitwork.*.
func staticName(urlPath string) (string, bool) {
	name, ok := strings.CutPrefix(urlPath, "/")
	// danh sách cho phép trước: đường dẫn của một cửa chỉ tốn phép thử này, không cấp phát
	if !ok || !strings.HasPrefix(name, "assets/") && !strings.HasPrefix(name, ".well-known/") && !rootFiles[name] {
		return "", false
	}
	if strings.ContainsAny(name, "\\:\x00") {
		return "", false
	}
	rest := name
	for first := true; ; first = false {
		segment, after, more := strings.Cut(rest, "/")
		if segment == "" || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || shortName(segment) {
			return "", false
		}
		if segment[0] == '.' && (!first || segment != ".well-known") {
			return "", false
		}
		if !more {
			break
		}
		rest = after
	}
	lower := strings.ToLower(name)
	if extension := path.Ext(lower); extension == ".work" || extension == ".ui" || strings.Contains(lower, ".kitwork.") {
		return "", false
	}
	return name, true
}

// shortName là một đoạn viết như tên ngắn của Windows: dấu ~ trước một chữ số.
func shortName(segment string) bool {
	for i := 0; i+1 < len(segment); i++ {
		if segment[i] == '~' && segment[i+1] >= '0' && segment[i+1] <= '9' {
			return true
		}
	}
	return false
}

// staticTypes là loại của các file site phục vụ nhiều nhất, viết ở đây chứ không đọc của hệ thống: registry của Windows
// có thể cho .js là text/plain, trình duyệt có nosniff sẽ không chạy.
var staticTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json",
	".txt": "text/plain; charset=utf-8", ".xml": "text/xml; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
	".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf",
	".wasm": "application/wasm", ".pdf": "application/pdf",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg",
}

// staticType là Content-Type của file name: của engine, của hệ thống cho phần mở rộng engine không có, và byte khi không
// có, không bao giờ đoán từ nội dung.
func staticType(name string) string {
	extension := strings.ToLower(path.Ext(name))
	if known, ok := staticTypes[extension]; ok {
		return known
	}
	if system := mime.TypeByExtension(extension); system != "" {
		return system
	}
	return "application/octet-stream"
}

// siteStatic là file tĩnh của một site: thư mục của nó trên đĩa, và các file giữ trong RAM khi site được giữ ấm.
type siteStatic struct {
	dir  string     // mọi file mở trong thư mục này (os.OpenInRoot): không tên nào ra ngoài, qua .. hay qua một link
	kept *keptFiles // nil: site không giữ ấm
}

// servedFile là một file sắp gửi: byte giữ trong RAM, hay file mở từ đĩa (closer đóng nó sau khi gửi).
type servedFile struct {
	content           io.ReadSeeker
	closer            io.Closer
	modified          time.Time
	kind, etag, cache string
}

func (f *servedFile) close() {
	if f != nil && f.closer != nil {
		f.closer.Close()
	}
}

// open là file name của site, nil khi không có (một thư mục, một link ra ngoài, không có gì).
func (s *siteStatic) open(name string) *servedFile {
	if s.kept != nil {
		if k := s.kept.get(s.dir, name); k != nil {
			return k.served()
		}
	}
	file, err := os.OpenInRoot(s.dir, filepath.FromSlash(name))
	if err != nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil
	}
	kind, etag, cache := s.headers(name, file, info)
	if s.kept != nil {
		if k := s.kept.keep(name, file, info, kind, etag, cache); k != nil {
			file.Close()
			return k.served()
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil { // carriesItsHash có thể đã đọc nó
		file.Close()
		return nil
	}
	return &servedFile{content: file, closer: file, modified: info.ModTime(), kind: kind, etag: etag, cache: cache}
}

// headers là câu trả lời của file name nói gì về nó: loại; một ETag cho file như nó nằm trên đĩa, từ giờ ghi và cỡ, mà
// http.ServeContent dùng cùng Last-Modified để trả 304; trình duyệt giữ nó bao lâu.
func (s *siteStatic) headers(name string, file *os.File, info os.FileInfo) (kind, etag, cache string) {
	kind = staticType(name)
	etag = `W/"` + strconv.FormatInt(info.ModTime().UnixNano(), 16) + "-" + strconv.FormatInt(info.Size(), 16) + `"`
	if s.carriesItsHash(name, file, info) {
		cache = "public, max-age=31536000, immutable"
	} else {
		cache = "public, max-age=" + strconv.Itoa(int(staticLifetime/time.Second))
	}
	return kind, etag, cache
}

// carriesItsHash là tên file mang mã băm của byte của nó, nên trình duyệt giữ được một năm: một phần của tên giữa các
// dấu chấm, gạch ngang hay gạch dưới (logo.3f2a9c1be04d77a1.svg, app-3f2a9c1b.js) là 8 tới 64 chữ số hex thường, có ít
// nhất một chữ cái, là đầu của SHA-256 của file. Kiểm bằng byte, không đoán từ cách viết: một năm giữ cho một tên không
// mang mã băm thì giữ file cũ sau khi sửa. Một phần chỉ có chữ số là ngày hay bộ đếm (logo.20261014.svg), không đọc.
func (s *siteStatic) carriesItsHash(name string, file *os.File, info os.FileInfo) bool {
	base := path.Base(name)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot] // phần mở rộng không bao giờ là mã băm
	}
	var candidates []string
	for _, part := range strings.FieldsFunc(base, func(r rune) bool { return r == '.' || r == '-' || r == '_' }) {
		if len(part) >= 8 && len(part) <= 64 && lowerHex(part) && strings.ContainsAny(part, "abcdef") {
			candidates = append(candidates, part)
		}
	}
	if len(candidates) == 0 {
		return false
	}
	key := s.dir + "\x00" + name + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	digest, ok := staticHashes.digest(key, func() (string, bool) {
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			return "", false
		}
		return hex.EncodeToString(sum.Sum(nil)), true
	})
	if !ok {
		return false
	}
	for _, candidate := range candidates {
		if strings.HasPrefix(digest, candidate) {
			return true
		}
	}
	return false
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// staticDigests giữ SHA-256 của các file tĩnh có tên trông như mang mã băm, theo file, cỡ và giờ ghi: một file được đọc
// một lần, đọc lại chỉ khi nó đổi; hai request cùng hỏi thì request sau chờ lần đọc của request trước. Của cả tiến
// trình, mọi site, nhiều nhất maxStaticDigests file: quá thì bỏ một file cho mỗi file mới.
type staticDigests struct {
	mutex   sync.Mutex
	known   map[string]string
	reading map[string]chan struct{}
}

const maxStaticDigests = 4096

var staticHashes staticDigests

func (d *staticDigests) digest(key string, read func() (string, bool)) (string, bool) {
	d.mutex.Lock()
	if digest, ok := d.known[key]; ok {
		d.mutex.Unlock()
		return digest, true
	}
	if done, ok := d.reading[key]; ok {
		d.mutex.Unlock()
		<-done
		d.mutex.Lock()
		digest, ok := d.known[key]
		d.mutex.Unlock()
		return digest, ok
	}
	if d.reading == nil {
		d.known, d.reading = map[string]string{}, map[string]chan struct{}{}
	}
	done := make(chan struct{})
	d.reading[key] = done
	d.mutex.Unlock()
	digest, ok := read()
	d.mutex.Lock()
	delete(d.reading, key)
	if ok {
		if len(d.known) >= maxStaticDigests {
			for other := range d.known {
				delete(d.known, other)
				break
			}
		}
		d.known[key] = digest
	}
	d.mutex.Unlock()
	close(done)
	return digest, ok
}

// heldSince là gốc thời gian của các lần so file giữ với đĩa: đồng hồ đơn điệu, đổi giờ của máy không làm nó nhảy.
var heldSince = time.Now()

func heldNow() int64 { return int64(time.Since(heldSince)) }

// keptFiles là các file của một site giữ ấm nằm trong RAM: request không đọc đĩa. Mỗi file so với đĩa nhiều nhất mỗi
// every một lần, bằng cỡ và giờ ghi: sửa tại chỗ, hay xoá, thì được phục vụ như nó đang là trong khoảng ấy.
type keptFiles struct {
	files sync.Map // tên trong thư mục site → *keptFile
	size  atomic.Int64
	most  int64         // keptFilesSize khi 0
	every time.Duration // Website.StaticCheck; defaultStaticCheck khi 0
}

// keptFile là một file giữ: byte của nó và câu trả lời nói gì về nó.
type keptFile struct {
	data              []byte
	modified          time.Time
	kind, etag, cache string
	checked           atomic.Int64 // lần so với đĩa sau cùng (heldNow)
}

func (k *keptFile) served() *servedFile {
	return &servedFile{content: bytes.NewReader(k.data), modified: k.modified, kind: k.kind, etag: k.etag, cache: k.cache}
}

// get là file name khi nó được giữ. Request so nó với đĩa (một request mỗi every) thì bỏ nó khi nó đổi hay mất:
// request ấy đọc đĩa như mọi request của site không giữ ấm.
func (m *keptFiles) get(dir, name string) *keptFile {
	held, ok := m.files.Load(name)
	if !ok {
		return nil
	}
	k := held.(*keptFile)
	every := m.every
	if every <= 0 {
		every = defaultStaticCheck
	}
	now := heldNow()
	if last := k.checked.Load(); now-last >= int64(every) && k.checked.CompareAndSwap(last, now) {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(k.data)) || !info.ModTime().Equal(k.modified) {
			m.drop(name, k)
			return nil
		}
	}
	return k
}

// keep giữ file name (mở là file, info nói về nó) khi nó đủ nhỏ và site còn chỗ; nil khi không, file được đọc từ đĩa.
// Một file giữ lại lấy chỗ của bản nó thay: chỗ được đếm trước theo cái đang giữ, rồi sửa theo cái nó thay thật.
func (m *keptFiles) keep(name string, file *os.File, info os.FileInfo, kind, etag, cache string) *keptFile {
	size := info.Size()
	if size > keptFileSize {
		return nil
	}
	most := m.most
	if most == 0 {
		most = keptFilesSize
	}
	var was int64
	if held, ok := m.files.Load(name); ok {
		was = int64(len(held.(*keptFile).data))
	}
	if m.size.Add(size-was) > most {
		m.size.Add(was - size)
		return nil
	}
	data := make([]byte, size)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		m.size.Add(was - size)
		return nil
	}
	if _, err := io.ReadFull(file, data); err != nil {
		m.size.Add(was - size)
		return nil
	}
	k := &keptFile{data: data, modified: info.ModTime(), kind: kind, etag: etag, cache: cache}
	k.checked.Store(heldNow())
	var replaced int64
	if previous, loaded := m.files.Swap(name, k); loaded {
		replaced = int64(len(previous.(*keptFile).data))
	}
	m.size.Add(was - replaced) // một request khác đã giữ hay bỏ name trong lúc ấy: cái swap thả ra mới được tính
	return k
}

// drop bỏ k, file giữ dưới tên name, trừ khi một bản khác đã thay nó.
func (m *keptFiles) drop(name string, k *keptFile) {
	if m.files.CompareAndDelete(name, k) {
		m.size.Add(-int64(len(k.data)))
	}
}

// warm giữ mọi file site phục vụ nguyên dạng, trong chừng cỡ cho phép: assets/, .well-known/ và các file của danh mục ở
// gốc. Trả số file giữ và số byte.
func (s *siteStatic) warm() (held int, size int64) {
	hold := func(name string) {
		if _, ok := staticName("/" + name); !ok {
			return // một tên site không bao giờ phục vụ (một .ui, một file chấm)
		}
		file, err := os.OpenInRoot(s.dir, filepath.FromSlash(name))
		if err != nil {
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		kind, etag, cache := s.headers(name, file, info)
		if k := s.kept.keep(name, file, info, kind, etag, cache); k != nil {
			held++
			size += int64(len(k.data))
		}
	}
	for _, top := range []string{"assets", ".well-known"} {
		_ = filepath.WalkDir(filepath.Join(s.dir, top), func(p string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() {
				if relative, err := filepath.Rel(s.dir, p); err == nil {
					hold(filepath.ToSlash(relative))
				}
			}
			return nil
		})
	}
	for name := range rootFiles {
		hold(name)
	}
	return held, size
}

// warms là site ở folder được giữ ấm: warm của website là true, false (hay không viết), hay hàm nhận context.site
// (hostname, folder) và trả true hay false. Hàm chạy trên một lượt chạy của host khi site biên dịch; ném lỗi hay trả thứ
// khác boolean thì engine viết lý do vào Log và site không giữ ấm (warm chỉ đổi tốc độ, không đổi câu trả lời).
func (w *Website) warms(folder string) bool {
	v := w.warm
	if v.IsBoolean() || v.IsUndefined() {
		return v.IsBoolean() && v.Boolean()
	}
	m := runtime.New(runtime.Options{Memory: HostMemory, Energy: HostEnergy})
	r := m.Call(v, value.Struct(&warmContext{folder: folder, hostname: path.Base(folder)}))
	switch {
	case r.IsInvalid():
		fmt.Fprintf(w.log(), "website: %s: application.website.warm: %v: the site is not kept warm\n", folder, r.Error())
		return false
	case !r.IsBoolean():
		fmt.Fprintf(w.log(), "website: %s: application.website.warm returned %s, not true or false (at %s): the site is not kept warm\n",
			folder, describeReturn(r), where(v))
		return false
	}
	return r.Boolean()
}

// warmContext là context của warm: chỉ có site (hostname, folder), chưa có request nào.
type warmContext struct{ folder, hostname string }

func (c *warmContext) Key(name string) value.Value {
	if name == "site" {
		return value.Object().Set("hostname", value.String(c.hostname)).Set("folder", value.String(c.folder)).Freeze()
	}
	return value.Fail(value.TypeCode, "context has no key %q: warm runs as a site is compiled, before any request, and reads context.site (hostname, folder)", name)
}

func (c *warmContext) Word(_ javascript.Run, name string, _ []value.Value) value.Value {
	if item := c.Key(name); item.IsInvalid() {
		return item
	}
	return value.Fail(value.TypeCode, "context.%s is not a function", name)
}
