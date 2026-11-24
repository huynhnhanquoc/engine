package http

import (
	"strconv"
	"strings"
	"time"
)

// readDuration reads a duration written as text: Go's ("500ms", "10s", "2m", "1h30m"), or a whole number of days
// or weeks ("7d", "2w"). None, zero or below is no duration.
func readDuration(text string) (time.Duration, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	if d, err := time.ParseDuration(s); err == nil {
		return d, d > 0
	}
	for _, unit := range []struct {
		suffix string
		size   time.Duration
	}{{"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}} {
		if number, ok := strings.CutSuffix(s, unit.suffix); ok {
			n, err := strconv.Atoi(number)
			if err != nil || n <= 0 || n > 3650 {
				return 0, false
			}
			return time.Duration(n) * unit.size, true
		}
	}
	return 0, false
}

// readSize reads a size written as text ("512kb", "50mb", "1gb", "100b") or a whole number of bytes.
func readSize(text string) (int64, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	for _, unit := range []struct {
		suffix string
		size   int64
	}{{"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"b", 1}} {
		if number, ok := strings.CutSuffix(s, unit.suffix); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
			if err != nil || n <= 0 || n > float64(hostMaxBytes) {
				return 0, false
			}
			return int64(n * float64(unit.size)), true
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// sizeText is a size as a person reads it: "10mb", "1kb", "100 bytes".
func sizeText(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return strconv.FormatInt(n>>20, 10) + "mb"
	case n >= 1<<10 && n%(1<<10) == 0:
		return strconv.FormatInt(n>>10, 10) + "kb"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}
