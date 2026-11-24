package ui

// A page's warnings, moved from the first engine's ui/warning.go on 06/11/2026 (decision D, 12/10/2026): what a
// page writes that works, and renders as written, but is probably not what its author wants or leaves some
// readers out (a button no screen reader can name, a carousel no one can stop, a notice too quick to read). An
// error stops the page; a warning never does, and is told only while developing (a website that allows local):
// the host says each in the terminal once, and the page links /_kitwork/warnings.<hash>.js, which says them in
// the browser's console on every load, a file and never a script inside the page. In production neither happens.
//
// The first engine compiled a page once and warned of everything in it; engine2 draws a page at each request, so
// a page warns of what that drawing reached: what an if left out, or a for drew no row of, says nothing that time.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/kitwork/engine2/library/ui/kernel"
)

// Warning is one warning of a page: where it stands (the attribute or the element that decided it) and what to
// write instead.
type Warning struct {
	File    string
	Line    int
	Column  int
	Message string
}

// String is the warning on one line, file:line:col: message, as an error is written. It carries no "cảnh báo:":
// each place that shows a warning says that itself.
func (w Warning) String() string {
	return (&Error{File: w.File, Line: w.Line, Column: w.Column, Message: w.Message}).Error()
}

// warn records a warning at a node, or at one of its attributes, once a page however many times the node is drawn.
func (r *renderer) warn(n *Node, a *Attribute, format string, args ...any) {
	line, column := n.Line, n.Column
	if a != nil {
		line, column = a.Line, a.Column
	}
	r.say(Warning{File: n.File, Line: line, Column: column, Message: fmt.Sprintf(format, args...)})
}

// say keeps w among the page's warnings, once by its line; every warning said is in the log a part frozen of the
// page keeps (frozen.go), once or not.
func (r *renderer) say(w Warning) {
	r.warnLog = append(r.warnLog, w)
	said := w.String()
	if r.warned[said] {
		return
	}
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	r.warned[said] = true
	r.warnings = append(r.warnings, w)
}

// Told says whether w is yet to be said in the terminal for this site, and counts it said: each warning once
// while the site runs, not once a request or once a page that shares the frame it stands in.
func (s *Site) Told(w Warning) bool {
	_, said := s.told.LoadOrStore(w.String(), true)
	return !said
}

// warningsFile is the file that says a page's warnings in the browser's console, one console.warn each:
// "cảnh báo: file:line:col: message", a JavaScript string (quotes, </script>, U+2028 escaped).
func warningsFile(list []Warning) *kernel.File {
	var b strings.Builder
	for _, w := range list {
		message, _ := json.Marshal("cảnh báo: " + w.String())
		b.WriteString("console.warn(")
		b.Write(message)
		b.WriteString(");\n")
	}
	return kernel.New("warnings", "warnings", b.String())
}

// maxWarningFiles bounds the warnings files kept: past it the oldest are let go, and a page still open that
// linked one finds it gone (only while developing).
const maxWarningFiles = 256

// warningFiles are the warnings files the pages drawn while developing link, by path: the same warnings are the
// same name. Only a page drawn while developing puts one here, so in production a name finds nothing.
var warningFiles struct {
	sync.Mutex
	byPath map[string]*kernel.File
	order  []string
}

// keepWarnings keeps a warnings file to be served at its path.
func keepWarnings(file *kernel.File) {
	warningFiles.Lock()
	defer warningFiles.Unlock()
	if _, kept := warningFiles.byPath[file.Path]; kept {
		return
	}
	if warningFiles.byPath == nil {
		warningFiles.byPath = map[string]*kernel.File{}
	}
	if len(warningFiles.order) >= maxWarningFiles {
		delete(warningFiles.byPath, warningFiles.order[0])
		warningFiles.order = warningFiles.order[1:]
	}
	warningFiles.byPath[file.Path] = file
	warningFiles.order = append(warningFiles.order, file.Path)
}

// warningsAt is the warnings file kept at path, nil when there is none.
func warningsAt(path string) *kernel.File {
	warningFiles.Lock()
	defer warningFiles.Unlock()
	return warningFiles.byPath[path]
}
