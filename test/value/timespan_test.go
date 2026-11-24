package value_test

import (
	"testing"
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// TimeSpan (core/value/helper.go): số nguyên dương ghép ngay với một đơn vị ms, s, m, h, d; chặt hơn
// time.ParseDuration của Go, và có d.

func TestTimeSpan(t *testing.T) {
	for text, want := range map[string]time.Duration{
		"500ms": 500 * time.Millisecond,
		"30s":   30 * time.Second,
		"2m":    2 * time.Minute,
		"1h":    time.Hour,
		"1d":    24 * time.Hour, // Go không có d
		"90s":   90 * time.Second,
		"030s":  30 * time.Second,
	} {
		if got, err := value.String(text).TimeSpan(); err != nil || got != want {
			t.Errorf("%q: %v %v, want %v", text, got, err, want)
		}
	}
	if got, err := value.Duration(5 * time.Second).TimeSpan(); err != nil || got != 5*time.Second {
		t.Errorf("a duration value: %v %v", got, err)
	}

	const write = `" is not a time: write "500ms", "30s", "2m", "1h" or "1d"`
	for _, text := range []string{
		"1h30m",   // Go nhận, ở đây một đơn vị
		"1.5s",    // không số lẻ
		"-1s",     // không số âm
		"0s",      // dương
		"30",      // thiếu đơn vị
		"30 s",    // dính liền
		"30S",     // chữ thường
		"forever", // không phải số
		"ms",
		"",
		"9999999999999999999d", // quá lớn
	} {
		if _, err := value.String(text).TimeSpan(); err == nil || err.Error() != `"`+text+write {
			t.Errorf("%q: %v", text, err)
		}
	}
	if _, err := value.Number(30).TimeSpan(); err == nil || err.Error() != `a time is a string like "500ms", "30s" or "2m", got number` {
		t.Errorf("a number: %v", err)
	}
	if _, err := value.Duration(-time.Second).TimeSpan(); err == nil {
		t.Error("a negative duration value is refused")
	}
}
