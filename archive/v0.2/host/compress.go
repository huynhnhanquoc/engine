package host

// Transport compression, moved from the first engine's utilities/compress on 07/11/2026: a page, a stylesheet, a
// script, a JSON answer goes out gzipped to a browser that takes gzip. HTML of a .ui page carries its CSS, so it
// shrinks most. What it must not break:
//
//   - a stream (a handler that flushes) goes out as written, never held back to fill a window;
//   - a body compressed already (an image, a face, an archive) and a type the engine does not name are left alone:
//     compressing them burns time to make them larger;
//   - a body under a kilobyte goes out as it is: the gzip frame costs more than it saves;
//   - a part of a file (Range, 206) is bytes of the file as it is, so it is never compressed; a strong ETag of the
//     file becomes weak on the compressed body, which is the same file in another encoding.
//
// The type is known only once the handler writes, so the choice is made at the first write.

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// compressAtLeast is the body below which compressing loses.
const compressAtLeast = 1024

var gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

// compressed is w compressing what is written to it when r takes gzip, and the function that ends it, which runs
// after the handler (even one that wrote nothing, so its status still goes out).
func compressed(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	if !takesGzip(r) {
		return w, func() {}
	}
	// the answer differs by what the client takes, compressed or not: a shared cache keeps one of each
	w.Header().Add("Vary", "Accept-Encoding")
	writer := &compressWriter{ResponseWriter: w}
	return writer, writer.finish
}

// takesGzip says the request takes a gzipped body: gzip among its Accept-Encoding, not refused with q=0. It reads
// the header in place: a request allocates nothing for it.
func takesGzip(r *http.Request) bool {
	rest := r.Header.Get("Accept-Encoding")
	for rest != "" {
		var each string
		each, rest, _ = strings.Cut(rest, ",")
		name, parameters, _ := strings.Cut(strings.TrimSpace(each), ";")
		if strings.TrimSpace(name) != "gzip" {
			continue
		}
		quality := strings.ReplaceAll(strings.TrimSpace(parameters), " ", "")
		return quality != "q=0" && quality != "q=0.0" && quality != "q=0.00" && quality != "q=0.000"
	}
	return false
}

// compressWriter waits to see the status, the headers and enough of the body before it compresses or not.
type compressWriter struct {
	http.ResponseWriter
	status            int
	wroteHeader, done bool
	gzip              *gzip.Writer
	held              []byte
}

func (c *compressWriter) WriteHeader(status int) {
	if c.wroteHeader {
		return
	}
	c.status, c.wroteHeader = status, true // sent with the choice: compressing changes the headers
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if c.done {
		if c.gzip != nil {
			return c.gzip.Write(p)
		}
		return c.ResponseWriter.Write(p)
	}
	c.held = append(c.held, p...) // at most a kilobyte and one write
	if len(c.held) < compressAtLeast {
		return len(p), nil
	}
	if err := c.choose(true); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush puts bytes on the wire now: a stream chooses to go out as it is, so nothing waits for a window to fill.
func (c *compressWriter) Flush() {
	if !c.done {
		_ = c.choose(false)
	}
	if c.gzip != nil {
		_ = c.gzip.Flush()
	}
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// choose compresses or not, once, sends the headers and what was held. large is false when the handler ended or
// flushed before a kilobyte: what there is goes out as it is.
func (c *compressWriter) choose(large bool) error {
	c.done = true
	header := c.ResponseWriter.Header()
	if large && worthCompressing(c.status, header) {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length") // the length of the file as it is, not of the compressed body
		if etag := header.Get("ETag"); strings.HasPrefix(etag, `"`) {
			header.Set("ETag", "W/"+etag)
		}
		c.ResponseWriter.WriteHeader(c.status)
		writer := gzipWriters.Get().(*gzip.Writer)
		writer.Reset(c.ResponseWriter)
		c.gzip = writer
		_, err := writer.Write(c.held)
		c.held = nil
		return err
	}
	c.ResponseWriter.WriteHeader(c.status)
	var err error
	if len(c.held) > 0 {
		_, err = c.ResponseWriter.Write(c.held)
	}
	c.held = nil
	return err
}

// finish sends what is held and closes the compressed stream.
func (c *compressWriter) finish() {
	if !c.done {
		if !c.wroteHeader {
			return // the handler wrote nothing: the server sends its 200
		}
		_ = c.choose(false)
	}
	if c.gzip != nil {
		_ = c.gzip.Close()
		gzipWriters.Put(c.gzip)
		c.gzip = nil
	}
}

// Unwrap gives http.ResponseController the writer underneath, for its deadlines.
func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// worthCompressing says the answer the handler is sending is one gzip shrinks: a whole body (no part of a file,
// not compressed already) of a type of text.
func worthCompressing(status int, header http.Header) bool {
	if status == http.StatusPartialContent || header.Get("Content-Range") != "" || header.Get("Content-Encoding") != "" {
		return false
	}
	media, _, _ := strings.Cut(header.Get("Content-Type"), ";")
	media = strings.ToLower(strings.TrimSpace(media))
	switch {
	case media == "text/event-stream":
		return false // a stream, even one that never flushes
	case strings.HasPrefix(media, "text/"):
		return true
	}
	switch media {
	case "application/json", "application/javascript", "application/xml", "application/xhtml+xml", "application/rss+xml",
		"application/atom+xml", "application/manifest+json", "application/ld+json", "image/svg+xml":
		return true
	}
	return false
}
