package application

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Logger is app.logger({ level, format, logfile, errorfile, max_size, max_backups, max_age, compress, console }): where
// the host's lines go besides standard output (host/logfile.go). Level is the least level written, to standard output
// as to LogFile; ErrorFile takes the lines of level error alone. A file grows to MaxSize megabytes, is then kept aside
// under the time it was set aside, MaxBackups of them at most and none older than MaxAge days, gzipped with Compress.
type Logger struct {
	// Level is "debug", "info", "warn" or "error": "info" when the manifest says none
	Level string
	// Format is "text" (a time, a level, the line) or "json" (one object a line) for the files: "text" when none
	Format string
	// LogFile and ErrorFile are paths from the manifest's folder; "" for none. ErrorFile is errors/<LogFile's name>
	// when the manifest names LogFile and not it.
	LogFile   string
	ErrorFile string
	// MaxSize is in megabytes, MaxAge in days: 10, 5 and 28 when the manifest says none or 0.
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
	// Console says the lines go to standard output too: true unless the manifest writes console: false.
	Console bool
}

const loggerExample = `app.logger({ level: "info", format: "text", logfile: "logs/kitwork.log", errorfile: "errors/kitwork.log", max_size: 10, max_backups: 5, max_age: 28, compress: true })`

// Logger is app.logger({ … }): once; every key is one the logger reads, written as the manifest of the first engine
// wrote it, so a misspelt key never passes as one left unread.
func (a *Application) Logger(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("logger(…)")
	}
	if a.manifest.Logger != nil {
		return value.Fail(value.InvalidArgumentCode, "app.logger(…) is declared twice: one manifest, one logger")
	}
	declared := argument(arguments, 0)
	if len(arguments) != 1 || !declared.IsObject() || declared.IsArray() {
		return value.Fail(value.InvalidArgumentCode, "app.logger(…) takes one object: "+loggerExample)
	}
	logger := &Logger{Level: "info", Format: "text", MaxSize: 10, MaxBackups: 5, MaxAge: 28, Console: true}
	path := func(key string, item value.Value) (string, value.Value) {
		if !item.IsString() || strings.TrimSpace(item.Text()) == "" {
			return "", value.Fail(value.InvalidArgumentCode, "app.logger({ "+key+" }): the path of a file, from the manifest's folder: "+loggerExample)
		}
		file := filepath.FromSlash(item.Text())
		if !filepath.IsAbs(file) {
			file = filepath.Join(a.folder, file)
		}
		return filepath.Clean(file), value.Value{}
	}
	for _, key := range declared.Keys() {
		item := declared.Get(key)
		switch key {
		case "level":
			level := strings.ToLower(item.Text())
			if !item.IsString() || level != "debug" && level != "info" && level != "warn" && level != "error" {
				return value.Fail(value.InvalidArgumentCode, `app.logger({ level }): "debug", "info", "warn" or "error"`)
			}
			logger.Level = level
		case "format":
			format := strings.ToLower(item.Text())
			if !item.IsString() || format != "text" && format != "json" {
				return value.Fail(value.InvalidArgumentCode, `app.logger({ format }): "text" or "json"`)
			}
			logger.Format = format
		case "logfile", "errorfile":
			file, failure := path(key, item)
			if failure.IsInvalid() {
				return failure
			}
			if key == "logfile" {
				logger.LogFile = file
			} else {
				logger.ErrorFile = file
			}
		case "max_size", "max_backups", "max_age":
			n := item.Number()
			if !item.IsNumber() || n != math.Trunc(n) || n < 0 || n > 100_000 {
				return value.Fail(value.InvalidArgumentCode, "app.logger({ "+key+" }): a whole number from 0 (0 is the default): "+loggerExample)
			}
			if n == 0 {
				continue
			}
			switch key {
			case "max_size":
				logger.MaxSize = int(n)
			case "max_backups":
				logger.MaxBackups = int(n)
			default:
				logger.MaxAge = int(n)
			}
		case "compress", "console":
			if !item.IsBoolean() {
				return value.Fail(value.InvalidArgumentCode, "app.logger({ "+key+" }): true or false")
			}
			if key == "compress" {
				logger.Compress = item.Boolean()
			} else {
				logger.Console = item.Boolean()
			}
		default:
			return value.Fail(value.InvalidArgumentCode, "app.logger({ "+key+" }): no key "+key+"; the keys are level, format, logfile, errorfile, max_size, max_backups, max_age, compress, console")
		}
	}
	if logger.ErrorFile == "" && logger.LogFile != "" {
		logger.ErrorFile = filepath.Join(a.folder, "errors", filepath.Base(logger.LogFile))
	}
	a.manifest.Logger = logger
	return value.Struct(a)
}
