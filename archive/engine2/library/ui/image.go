package ui

// The size of an image of the site (moved from the first engine's ui/image.go on 06/11/2026): an <image> whose
// value is a path of the site, written as it is, carries the width and height its file says, so the browser keeps
// its room before it arrives and nothing below it jumps. Only the head of the file is read (the header of a PNG,
// JPEG or GIF, a WebP's, an SVG's root element), opened inside the site's folder, so no path leaves it. A page is
// drawn at each request, so each file's size is kept by its path for the boot's life, as the site's .ui files are
// (kept.go, 10/11/2026: until then each page opened the file again to see whether it had changed); an image changed
// on the disk is measured at the next boot. Every image loads lazily and decodes off the main thread
// (loading="lazy" decoding="async").

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif" // DecodeConfig reads the header of each format it knows
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// maxImageHeader is how much of a file is read for its size: every format's header is well within it, and an
// SVG's root element nearly always is.
const maxImageHeader = 64 << 10

// keptSize is what the site keeps of one file's size: its width and height, ok false for a file whose size cannot
// be read or that is not there.
type keptSize struct {
	width, height int
	ok            bool
}

// imageSize is the width and height of the image at src, a path of the site ("/assets/logo.svg"); false for
// anything else (a web address, a path from data, {{ }} still in it, a file that is no image the reader knows).
func (s *Site) imageSize(src string) (int, int, bool) {
	if !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") || strings.ContainsAny(src, `?#{\`) {
		return 0, 0, false
	}
	name := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(src, "/")))
	if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
		return 0, 0, false
	}
	if known, ok := s.images.Load(name); ok {
		kept := known.(keptSize)
		return kept.width, kept.height, kept.ok
	}
	width, height, ok, err := s.measure(name)
	if keepable(err) {
		s.images.Store(name, keptSize{width: width, height: height, ok: ok})
	}
	return width, height, ok
}

// measure reads the size of the image at name, a path inside the site's folder, and what the read failed on.
func (s *Site) measure(name string) (int, int, bool, error) {
	if s.Read != nil {
		// a test's site: its files as given
		data, err := s.Read(filepath.Join(s.Root, name))
		if err != nil {
			return 0, 0, false, err
		}
		width, height, ok := sizeOf(name, data[:min(len(data), maxImageHeader)])
		return width, height, ok, nil
	}
	file, err := os.OpenInRoot(s.Root, name)
	if err != nil {
		return 0, 0, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, 0, false, err
	}
	if info.IsDir() {
		return 0, 0, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImageHeader))
	if err != nil {
		return 0, 0, false, err
	}
	width, height, ok := sizeOf(name, data)
	return width, height, ok, nil
}

// sizeOf reads the size an image's head says, by the file's extension for an SVG and a WebP, by its header for
// the others.
func sizeOf(name string, data []byte) (int, int, bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".svg":
		return svgSize(data)
	case ".webp":
		return webpSize(data)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

var (
	svgRoot      = regexp.MustCompile(`(?is)<svg\b[^>]*>`)
	svgLength    = regexp.MustCompile(`^\s*([0-9]*\.?[0-9]+)\s*(px)?\s*$`)
	svgNumbers   = regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+(?:[eE][-+]?[0-9]+)?`)
	svgAttribute = map[string]*regexp.Regexp{
		"width":   regexp.MustCompile(`(?i)\swidth\s*=\s*["']([^"']*)["']`),
		"height":  regexp.MustCompile(`(?i)\sheight\s*=\s*["']([^"']*)["']`),
		"viewBox": regexp.MustCompile(`(?i)\sviewBox\s*=\s*["']([^"']*)["']`),
	}
)

// svgSize is an SVG's size: its root's width and height when both are lengths in px or without a unit, else the
// size of its viewBox.
func svgSize(data []byte) (int, int, bool) {
	root := svgRoot.Find(data)
	if root == nil {
		return 0, 0, false
	}
	attribute := func(name string) string {
		m := svgAttribute[name].FindSubmatch(root)
		if m == nil {
			return ""
		}
		return string(m[1])
	}
	width, height := svgLength.FindStringSubmatch(attribute("width")), svgLength.FindStringSubmatch(attribute("height"))
	if width != nil && height != nil {
		w, _ := strconv.ParseFloat(width[1], 64)
		h, _ := strconv.ParseFloat(height[1], 64)
		if w > 0 && h > 0 {
			return int(math.Round(w)), int(math.Round(h)), true
		}
	}
	box := svgNumbers.FindAllString(attribute("viewBox"), -1)
	if len(box) != 4 {
		return 0, 0, false
	}
	w, _ := strconv.ParseFloat(box[2], 64)
	h, _ := strconv.ParseFloat(box[3], 64)
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return int(math.Round(w)), int(math.Round(h)), true
}

// webpSize is a WebP's size, from the header of each of its three kinds (lossy, lossless, extended).
func webpSize(data []byte) (int, int, bool) {
	if len(data) < 30 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(data[12:16]) {
	case "VP8 ":
		if data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, false
		}
		w := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
		h := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
		return w, h, w > 0 && h > 0
	case "VP8L":
		if data[20] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		return int(bits&0x3fff) + 1, int(bits>>14&0x3fff) + 1, true
	case "VP8X":
		w := int(uint32(data[24])|uint32(data[25])<<8|uint32(data[26])<<16) + 1
		h := int(uint32(data[27])|uint32(data[28])<<8|uint32(data[29])<<16) + 1
		return w, h, true
	}
	return 0, 0, false
}
