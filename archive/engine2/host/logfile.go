package host

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A file of the host's log (app.logger, logger.go of library/application), as the first engine's lumberjack kept one,
// with nothing but the standard library: written to its end; grown past its size, set aside under the time it was
// set aside, name-2006-01-02T15-04-05.000.ext, and begun again; MaxBackups of the files set aside kept at most and
// none older than MaxAge, gzipped when Compress says. Setting aside, gzipping and letting go run apart from the
// writes, one at a time.

// backupStamp is the layout of the time in a file set aside's name.
const backupStamp = "2006-01-02T15-04-05.000"

// logFile is one file of the log.
type logFile struct {
	path     string
	maxBytes int64
	backups  int
	maxAge   time.Duration
	compress bool
	now      func() time.Time

	file *os.File
	size int64
	// mill gzips and lets go of the files set aside, one at a time; tidying waits for it
	mill    sync.Mutex
	tidying sync.WaitGroup
}

// openLogFile opens the file at path to write at its end, its folder made.
func openLogFile(path string, maxMegabytes, backups, maxAgeDays int, compress bool, now func() time.Time) (*logFile, error) {
	f := &logFile{path: path, maxBytes: int64(maxMegabytes) << 20, backups: backups,
		maxAge: time.Duration(maxAgeDays) * 24 * time.Hour, compress: compress, now: now}
	if err := f.open(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *logFile) open() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	f.file, f.size = file, info.Size()
	return nil
}

// Write writes p at the file's end, the file set aside first when p would grow it past its size. The Log's lock is
// held: one write at a time.
func (f *logFile) Write(p []byte) (int, error) {
	if f.file == nil {
		return 0, os.ErrClosed
	}
	if f.size > 0 && f.size+int64(len(p)) > f.maxBytes {
		f.setAside()
	}
	n, err := f.file.Write(p)
	f.size += int64(n)
	return n, err
}

// setAside renames the file under the time and begins it again; gzipping and letting go of the oldest run apart.
func (f *logFile) setAside() {
	f.file.Close()
	f.file = nil
	extension := filepath.Ext(f.path)
	aside := strings.TrimSuffix(f.path, extension) + "-" + f.now().Format(backupStamp) + extension
	renamed := os.Rename(f.path, aside) == nil
	if err := f.open(); err != nil {
		return
	}
	f.tidying.Add(1)
	go func() {
		defer f.tidying.Done()
		f.mill.Lock()
		defer f.mill.Unlock()
		if renamed && f.compress {
			gzipFile(aside)
		}
		f.letGo()
	}()
}

// gzipFile writes file as file.gz and removes it; a failure leaves it as it was.
func gzipFile(file string) {
	source, err := os.Open(file)
	if err != nil {
		return
	}
	target, err := os.OpenFile(file+".gz", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		source.Close()
		return
	}
	zipped := gzip.NewWriter(target)
	_, copyErr := io.Copy(zipped, source)
	closeErr := zipped.Close()
	source.Close()
	if err := target.Close(); err != nil || copyErr != nil || closeErr != nil {
		os.Remove(file + ".gz")
		return
	}
	os.Remove(file)
}

// letGo removes the files set aside past backups, newest kept, and those older than maxAge.
func (f *logFile) letGo() {
	folder := filepath.Dir(f.path)
	extension := filepath.Ext(f.path)
	prefix := strings.TrimSuffix(filepath.Base(f.path), extension) + "-"
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	type aside struct {
		name string
		at   time.Time
	}
	var kept []aside
	for _, entry := range entries {
		name := entry.Name()
		stamp, ok := strings.CutPrefix(name, prefix)
		if !ok || entry.IsDir() {
			continue
		}
		stamp = strings.TrimSuffix(strings.TrimSuffix(stamp, ".gz"), extension)
		at, err := time.ParseInLocation(backupStamp, stamp, time.Local)
		if err != nil {
			continue
		}
		kept = append(kept, aside{name, at})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
	cutoff := f.now().Add(-f.maxAge)
	for i, each := range kept {
		if i >= f.backups || each.at.Before(cutoff) {
			os.Remove(filepath.Join(folder, each.name))
		}
	}
}

// Close closes the file once its tidying is done.
func (f *logFile) Close() error {
	f.tidying.Wait()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}
