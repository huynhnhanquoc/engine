package file

import (
	"math"
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// entry is one value of a .env file: its text, the quotes taken off, and whether it was quoted, which
// keeps it a string whatever it looks like.
type entry struct {
	text   string
	quoted bool
	// value is the entry coerced, once, as its file is read: env.KEY is read at every request and the
	// values never change after boot; coercing again at each read was 7% of a request that read three
	// keys (05/11/2026). Undefined for a variable of the system, coerced as it is read.
	value value.Value
}

// parse reads KEY=VALUE lines: blank lines and # comments are skipped, an `export ` before a key is
// allowed, and "double" or 'single' quotes around a value are taken off and remembered. An empty value
// is no value (mục 4).
func parse(source string) map[string]entry {
	values := map[string]entry{}
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, text, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		item := entry{text: strings.TrimSpace(text)}
		if t := item.text; len(t) >= 2 && (t[0] == '"' && t[len(t)-1] == '"' || t[0] == '\'' && t[len(t)-1] == '\'') {
			item = entry{text: t[1 : len(t)-1], quoted: true}
		}
		if item.text == "" {
			continue // KEY= is no value; an earlier file's value of KEY stands
		}
		item.value = coerce(item)
		values[key] = item
	}
	return values
}

// coerce is the value of an entry (mục 4): quoted is a string; true and false in any case are booleans;
// a number only in its canonical form, the one writing the number out again gives back (42, -7, 3.14);
// 0123, +84…, 1e3, 1.10, -0, Infinity and NaN stay strings, so a postal code or a version never loses a
// digit.
func coerce(item entry) value.Value {
	if item.quoted {
		return value.String(item.text)
	}
	switch strings.ToLower(item.text) {
	case "true":
		return value.Boolean(true)
	case "false":
		return value.Boolean(false)
	}
	number, err := strconv.ParseFloat(item.text, 64)
	finite := err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) && !(number == 0 && math.Signbit(number))
	if finite && strconv.FormatFloat(number, 'f', -1, 64) == item.text {
		return value.Number(number)
	}
	return value.String(item.text)
}
