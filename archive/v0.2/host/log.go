package host

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/builtins"
	"github.com/kitwork/engine2/library/application"
)

// Log is where every line of the host goes: the boot's lines, the console of every site, the failures
// of requests. One lock, so the lines of requests running at once never mix: each writer hands it one
// whole line per Write.
//
// Each line has a level (app.logger, logfile.go): info unless it says another, an answer of 500 or a panic or an app
// closed is error, a refusal of 403 or 400 is warn, the console's error, warn and debug theirs. A line below the
// manifest's level is written nowhere; the others go to out (standard output) unless console: false, to the logfile
// with its time and level, and when error to the errorfile too. With no app.logger every line goes to out, as it did.
type Log struct {
	mutex sync.Mutex
	out   io.Writer
	// least is the least level written; console says out is written; json the files' format
	least   level
	console bool
	json    bool
	// file and errors are the logfile and the errorfile, nil for none
	file, errors *logFile
	now          func() time.Time
}

// level is a line's level, least to most.
type level int

const (
	levelDebug level = iota
	levelInfo
	levelWarn
	levelError
)

var levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}

// newLog is a log writing every line to out, as a host with no app.logger does.
func newLog(out io.Writer) *Log {
	return &Log{out: out, least: levelDebug, console: true, now: time.Now}
}

// open makes the log what logger says, its files opened: a file that cannot be opened is the error.
func (l *Log) open(logger *application.Logger) error {
	if logger == nil {
		return nil
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.least = map[string]level{"debug": levelDebug, "info": levelInfo, "warn": levelWarn, "error": levelError}[logger.Level]
	l.console, l.json = logger.Console, logger.Format == "json"
	var err error
	if logger.LogFile != "" {
		if l.file, err = openLogFile(logger.LogFile, logger.MaxSize, logger.MaxBackups, logger.MaxAge, logger.Compress, l.now); err != nil {
			return errors.New("app.logger({ logfile }): " + err.Error())
		}
	}
	if logger.ErrorFile != "" {
		if l.errors, err = openLogFile(logger.ErrorFile, logger.MaxSize, logger.MaxBackups, logger.MaxAge, logger.Compress, l.now); err != nil {
			return errors.New("app.logger({ errorfile }): " + err.Error())
		}
	}
	return nil
}

// close closes the log's files, once their tidying is done.
func (l *Log) close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var failures []error
	for _, file := range []*logFile{l.file, l.errors} {
		if file != nil {
			failures = append(failures, file.Close())
		}
	}
	return errors.Join(failures...)
}

// Write writes lines of level info: the console's log and info, a module's lines.
func (l *Log) Write(lines []byte) (int, error) {
	l.write(levelInfo, lines)
	return len(lines), nil
}

// write writes lines, each ended by a newline, at lvl.
func (l *Log) write(lvl level, lines []byte) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if lvl < l.least {
		return
	}
	if l.console {
		l.out.Write(lines)
	}
	if l.file == nil && (l.errors == nil || lvl < levelError) {
		return
	}
	stamped := l.stamped(lvl, lines)
	if l.file != nil {
		l.file.Write(stamped)
	}
	if l.errors != nil && lvl >= levelError {
		l.errors.Write(stamped)
	}
}

// stamped is lines as a file of the log keeps them: each led by its time and level, or one JSON object a line.
func (l *Log) stamped(lvl level, lines []byte) []byte {
	at := l.now().Format("2006-01-02T15:04:05.000Z07:00")
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(lines), "\n") {
		text := strings.TrimSuffix(line, "\n")
		if text == "" {
			continue
		}
		if l.json {
			encoded, _ := json.Marshal(struct {
				Time  string `json:"time"`
				Level string `json:"level"`
				Msg   string `json:"msg"`
			}{at, levelNames[lvl], text})
			out.Write(encoded)
		} else {
			out.WriteString(at + " " + levelNames[lvl] + " " + text)
		}
		out.WriteString("\n")
	}
	return []byte(out.String())
}

// Level is the writer of the console's word name (builtins.ConsoleTo): its lines at that word's level.
func (l *Log) Level(name string) io.Writer {
	switch name {
	case "error":
		return leveled{l, levelError}
	case "warn":
		return leveled{l, levelWarn}
	case "debug":
		return leveled{l, levelDebug}
	}
	return leveled{l, levelInfo}
}

// leveled writes to a log at one level.
type leveled struct {
	log *Log
	lvl level
}

func (w leveled) Write(lines []byte) (int, error) {
	w.log.write(w.lvl, lines)
	return len(lines), nil
}

// Line writes text as one line, of level info.
func (l *Log) Line(text string) {
	l.write(levelInfo, []byte(text+"\n"))
}

// Led writes text that a site's program or a request may have written (a failure's message, a URL's
// path) as the lines builtins.LogText makes of it: each led by lead, what would end a line or draw over
// one escaped, as a site's console writes. Until 05/11/2026 a path /x%0A[beta.test] … or a failure's text
// with a newline in it wrote a line led by another site's name. Its level is info; Warn and Error are Led at theirs.
func (l *Log) Led(lead, text string) {
	l.write(levelInfo, builtins.LogText(lead, text))
}

// Warn is Led of level warn: a request refused.
func (l *Log) Warn(lead, text string) {
	l.write(levelWarn, builtins.LogText(lead, text))
}

// Error is Led of level error: a request answered 500, a panic, an app closed.
func (l *Log) Error(lead, text string) {
	l.write(levelError, builtins.LogText(lead, text))
}

// maxVisitorText is how many bytes of what a visitor wrote (a path, a method, a Host) a line of the log
// quotes; the rest is counted. LogText cuts a whole line at 64 KiB, so a path of 70,000 bytes before a
// failure's reason pushed the reason out of the line (05/11/2026).
const maxVisitorText = 2 << 10

// visitorText is text a visitor wrote, past maxVisitorText bytes cut at a character with the count of the
// bytes left out, as LogText cuts a line: what the line says after it, the failure, stays in it.
func visitorText(text string) string {
	if len(text) <= maxVisitorText {
		return text
	}
	end := maxVisitorText
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + " … (" + strconv.Itoa(len(text)-end) + " bytes more)"
}
