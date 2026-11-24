package host

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// app.prewarm(…) (11/11/2026): a site the manifest names is read at its boot and kept for it, before the host serves.
// Every .ui file is parsed, every page assembled, every small file the site serves as it is held in memory, so no
// request of it reads the disk. A site it does not name is read as requests come: a page at its first, a file from the
// disk at each, streamed as it is read. Keeping costs memory, which the host spends site by site; none by default.

// keptFileSize is the largest file a prewarmed site holds in memory, keptFilesSize all it holds: a larger file, or one
// past the sum, is read from the disk at each request, as a site not prewarmed reads every file.
const (
	keptFileSize  = 256 << 10
	keptFilesSize = 32 << 20
)

// defaultStaticCheck is how often a file held in memory is checked against the disk when StaticCheck says nothing.
const defaultStaticCheck = time.Second

// heldSince is when the files held are timed from: their checks read the monotonic clock, which a change of the
// wall clock never moves. On Windows it moves by about half a millisecond: a request within the tick of the last check
// is not checked again, whatever StaticCheck says below that.
var heldSince = time.Now()

// heldNow is the time of a check, in nanoseconds since heldSince.
func heldNow() int64 { return int64(time.Since(heldSince)) }

// prewarms says the manifest names hostname in app.prewarm(…), or every site.
func (h *Host) prewarms(hostname string) bool {
	return slices.Contains(h.Manifest.Prewarm, "*") || slices.Contains(h.Manifest.Prewarm, hostname)
}

// prewarm reads site for its boot: its .ui files and its pages (library/ui Site.Prewarm), and the files it serves as
// they are, held in memory from now on. What could not be read is said in the log; the request of the page that holds
// it says it again.
func (h *Host) prewarm(site *Site) {
	var routes []string
	var pagesOf func(f *Folder)
	pagesOf = func(f *Folder) {
		if f.page {
			routes = append(routes, filepath.Join(site.Folder, filepath.FromSlash(f.Path)))
		}
		for _, child := range f.children {
			pagesOf(child)
		}
	}
	pagesOf(site.root)
	pages, files, failed := site.ui.Prewarm(routes)
	for _, err := range failed {
		site.log.Led(site.lead, "prewarm: "+err.Error())
	}
	site.static.memory = &keptFiles{host: h}
	held, size := site.static.warm()
	site.log.Led(site.lead, fmt.Sprintf("prewarm: %d pages, %d .ui files, %d files in memory (%d KB)", pages, files, held, (size+1023)/1024))
}

// prewarmUnknown says each site app.prewarm(…) names that no app serves: a name mistyped warms nothing.
func (h *Host) prewarmUnknown(sites map[string]*Site) {
	for _, name := range h.Manifest.Prewarm {
		if name != "*" && sites[name] == nil {
			h.log.Line("app.prewarm names " + name + ", which no app serves: nothing is warmed for it")
		}
	}
}

// keptFiles are the files of a prewarmed site held in memory for its boot: a request reads none of the disk. A file is
// checked against it at most once every StaticCheck, by its size and its time of change, so one edited in place, or
// gone, is served as it is within that.
type keptFiles struct {
	host  *Host
	files sync.Map // its name in the site's folder → *keptFile
	size  atomic.Int64
	// most is all it holds at most, in bytes; keptFilesSize when 0
	most int64
}

// keptFile is a file held: its bytes and what its answer says of it.
type keptFile struct {
	data              []byte
	modified          time.Time
	kind, etag, cache string
	// checked is when it was last checked against the disk (heldNow)
	checked atomic.Int64
}

// every is how often a file held is checked against the disk.
func (m *keptFiles) every() time.Duration {
	if m.host != nil && m.host.StaticCheck > 0 {
		return m.host.StaticCheck
	}
	return defaultStaticCheck
}

// serve answers r with the file name when it is held, and reports whether it did. The one request that checks it
// against the disk lets it go when it changed or went: the request reads the disk, in the site's folder, as any does.
func (m *keptFiles) serve(w http.ResponseWriter, r *http.Request, folder, name string) bool {
	held, ok := m.files.Load(name)
	if !ok {
		return false
	}
	k := held.(*keptFile)
	now := heldNow()
	if last := k.checked.Load(); now-last >= int64(m.every()) && k.checked.CompareAndSwap(last, now) {
		info, err := os.Stat(filepath.Join(folder, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(k.data)) || !info.ModTime().Equal(k.modified) {
			m.drop(name, k)
			return false
		}
	}
	header := w.Header()
	header.Set("Content-Type", k.kind)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("ETag", k.etag)
	header.Set("Cache-Control", k.cache)
	http.ServeContent(w, r, "", k.modified, bytes.NewReader(k.data))
	return true
}

// keep holds the file name, open as file and as info says of it, when it is small enough and the site holds room for
// it; nil when it does not, and the file is read from the disk. A file held again takes the room of the one it
// replaces: the room is counted against what is held as it starts, and set right by what it took the place of.
func (m *keptFiles) keep(name string, file *os.File, info os.FileInfo, kind, etag, cache string) *keptFile {
	size := info.Size()
	if size > keptFileSize {
		return nil
	}
	most := m.most
	if most == 0 {
		most = keptFilesSize
	}
	var was int64 // the room of the file held as name now
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
	m.size.Add(was - replaced) // another request kept or dropped name meanwhile: what the swap let go counts
	return k
}

// drop lets go of k, the file held as name, unless another took its place.
func (m *keptFiles) drop(name string, k *keptFile) {
	if m.files.CompareAndDelete(name, k) {
		m.size.Add(-int64(len(k.data)))
	}
}

// warm holds every file the site serves as it is, as far as the sizes allow: its assets/, its .well-known/ and the
// files of the root's catalogue (static.go). It gives how many it holds and their bytes.
func (s *Static) warm() (held int, size int64) {
	hold := func(name string) {
		if _, ok := staticName("/" + name); !ok {
			return // a name the site never serves (a .ui, a dot file)
		}
		file, err := os.OpenInRoot(s.folder, filepath.FromSlash(name))
		if err != nil {
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		kind, etag, cache := s.headers(name, file, info)
		if k := s.memory.keep(name, file, info, kind, etag, cache); k != nil {
			held++
			size += int64(len(k.data))
		}
	}
	for _, top := range []string{"assets", ".well-known"} {
		_ = filepath.WalkDir(filepath.Join(s.folder, top), func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() {
				if relative, err := filepath.Rel(s.folder, path); err == nil {
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
