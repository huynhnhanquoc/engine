package ui

import (
	"errors"
	"strconv"
)

// Error is a problem of a .ui file at the place it is written: file:line:column: message. A column counts
// characters, not bytes, so a line of Vietnamese points where an editor does.
type Error struct {
	File    string
	Line    int
	Column  int
	Message string
	// Busy is an expression stopped because the host's runs hold all it gives (runtime.Pool): errors.Is(err, ErrBusy)
	Busy bool
}

// ErrBusy is the error of a page an expression of which the host could not give to (core/runtime, pool.go): the host
// is busy, not the page wrong, and the site answers 503.
var ErrBusy = errors.New("the host is busy: its runs hold all it gives")

// Is reports a busy host's error as ErrBusy.
func (e *Error) Is(target error) bool { return target == ErrBusy && e.Busy }

func (e *Error) Error() string {
	place := strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Column)
	if e.File != "" {
		place = e.File + ":" + place
	}
	return place + ": " + e.Message
}
