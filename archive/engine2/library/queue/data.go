package queue

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kitwork/engine2/core/value"
)

// DataLimit is the most a run's data may be, written as JSON (docs/KITWORK_QUEUE.md mục 4: "có trần kích thước").
const DataLimit = 64 << 10

// dataDepth is how deep the data of a run may nest; past it the data holds itself or is too deep.
const dataDepth = 64

// encodeData writes v as a run's data, JSON: null, booleans, finite numbers, strings, arrays and plain objects of
// them; a time as its toISOString. A function, a handle of the engine, bytes, a failure are refused by name and where,
// never written as {} (mục 4: "Không gửi kết nối database, hàm, request, hay quyền chạy").
func encodeData(v value.Value) (string, value.Value) {
	room := DataLimit
	if problem := plain(v, "data", 0, &room); problem != "" {
		if room < 0 {
			return "", value.Fail(value.TooLargeCode, problem)
		}
		return "", value.Fail(value.InvalidArgumentCode, problem)
	}
	written := value.Stringify(v, "")
	if written.IsInvalid() {
		return "", written
	}
	if written.IsUndefined() {
		return "null", value.Value{}
	}
	return written.Text(), value.Value{}
}

// plain is "" when v is data JSON writes as itself, what is wrong and where otherwise. room is what is left of
// DataLimit as v is walked, taken by each value at least as much as JSON writes of it: a byte for any value, a
// string's text and quotes, an array's or an object's brackets and commas, a key's text, quotes and colon (a time
// its 26; a key whose value is undefined, which JSON leaves out, is taken too). Past it the data is too large, said at
// once: a value an array holds twice is walked twice, so ten nested a = [a, a] are 1,024 paths, fifty would be
// 2^50, and until 13/11/2026 only the depth was bounded (the JSON's own length was read after the walk).
func plain(v value.Value, at string, depth int, room *int) string {
	if depth > dataDepth {
		return at + " lồng quá " + strconv.Itoa(dataDepth) + " tầng, hay chứa chính nó: dữ liệu của job là dữ liệu thuần"
	}
	*room--
	switch {
	case v.IsString():
		*room -= len(v.Text()) + 1
	case v.IsTime():
		*room -= 25
	case v.IsArray(), v.IsObject():
		*room -= v.Count() // with the byte above, the brackets and the commas
	}
	if *room < 0 {
		return "dữ liệu viết thành JSON quá trần " + strconv.Itoa(DataLimit>>10) + " KiB của một lần chạy (đã quá ở " + at + "); lưu phần lớn vào database rồi gửi id"
	}
	switch {
	case v.IsUndefined(), v.IsNull(), v.IsBoolean(), v.IsString(), v.IsTime():
		return ""
	case v.IsNumber():
		if n := v.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
			return at + " là " + strconv.FormatFloat(n, 'g', -1, 64) + ", JSON không có số này"
		}
		return ""
	case v.IsArray():
		for index, item := range v.Items() {
			if problem := plain(item, at+"["+strconv.Itoa(index)+"]", depth+1, room); problem != "" {
				return problem
			}
		}
		return ""
	case v.IsObject():
		for _, key := range v.Keys() {
			if *room -= len(key) + 3; *room < 0 {
				return "dữ liệu viết thành JSON quá trần " + strconv.Itoa(DataLimit>>10) + " KiB của một lần chạy (đã quá ở " + at + "." + key + "); lưu phần lớn vào database rồi gửi id"
			}
			if problem := plain(v.Get(key), at+"."+key, depth+1, room); problem != "" {
				return problem
			}
		}
		return ""
	}
	return at + " là " + v.Kind() + ", không phải dữ liệu: dữ liệu của job viết được thành JSON (số, chuỗi, mảng, object), không gửi hàm, kết nối hay request"
}

// decodeData is a run's stored data as a value; data that does not read is null.
func decodeData(data string) value.Value {
	if data == "" {
		return value.Null()
	}
	read := value.ParseJSON(data)
	if read.IsInvalid() {
		return value.Null()
	}
	return read
}

var durationDays = regexp.MustCompile(`^(\d+)(d|w)$`)

// ParseDuration reads a duration as Go writes one ("30s", "1h30m", "250ms"), or a whole number of days or weeks ("30d",
// "2w"): the .timeout(…), delay, keep(…) of the queue and the schedules.
func ParseDuration(text string) (time.Duration, bool) {
	text = strings.ToLower(strings.TrimSpace(text))
	if duration, err := time.ParseDuration(text); err == nil {
		return duration, true
	}
	match := durationDays.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	count, err := strconv.Atoi(match[1])
	if err != nil || count <= 0 || count > 100_000 {
		return 0, false
	}
	unit := 24 * time.Hour
	if match[2] == "w" {
		unit *= 7
	}
	return time.Duration(count) * unit, true
}

// logLimit is the most a run's log keeps; past it the oldest lines go.
const logLimit = 64 << 10

// Log is a run's log: what its attempts wrote with context.job.log(…), and a line for each attempt that failed. It
// keeps the last logLimit bytes, valid UTF-8 only (a line that is not is mended, never refused: a log the file refused
// would leave the run unsettled).
type Log struct {
	mu   sync.Mutex
	text strings.Builder
}

// Write adds a line.
func (l *Log) Write(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.text.WriteString(strings.ToValidUTF8(line, string(utf8.RuneError)))
	l.text.WriteByte('\n')
	if l.text.Len() > logLimit {
		kept := l.text.String()
		kept = kept[len(kept)-logLimit:]
		if cut := strings.IndexByte(kept, '\n'); cut >= 0 {
			kept = kept[cut+1:]
		}
		l.text.Reset()
		l.text.WriteString(kept)
	}
}

func (l *Log) attemptFailed(attempt int, code, message string) {
	l.Write("lần thử " + strconv.Itoa(attempt) + ": " + code + " " + message)
}

// String is the log as the run keeps it.
func (l *Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// Function is context.job.log(…): it writes its arguments, each as a string, joined by a space, as one line.
func (l *Log) Function() value.Value {
	return value.Function(func(arguments ...value.Value) value.Value {
		parts := make([]string, len(arguments))
		for index, argument := range arguments {
			parts[index] = text(argument)
		}
		l.Write(strings.Join(parts, " "))
		return value.Undefined()
	})
}

// text is a value as a line of a log reads it: a string as itself, anything else as JSON writes it.
func text(v value.Value) string {
	if v.IsString() {
		return v.Text()
	}
	if v.IsUndefined() {
		return "undefined"
	}
	written := value.Stringify(v, "")
	if written.IsString() {
		return written.Text()
	}
	return v.Kind()
}
