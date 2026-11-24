package queue

import (
	"strings"
	"testing"
	"time"
)

func TestADurationIsGosOrWholeDaysAndWeeks(t *testing.T) {
	for text, want := range map[string]time.Duration{
		"30s": 30 * time.Second, " 5m ": 5 * time.Minute, "1h30m": 90 * time.Minute, "250ms": 250 * time.Millisecond,
		"2d": 48 * time.Hour, "1w": 7 * 24 * time.Hour, "30D": 30 * 24 * time.Hour,
	} {
		if got, ok := ParseDuration(text); !ok || got != want {
			t.Errorf("%q: %s %v, want %s", text, got, ok, want)
		}
	}
	for _, text := range []string{"", "soon", "0d", "1.5d", "1mo", "100001d"} {
		if got, ok := ParseDuration(text); ok {
			t.Errorf("%q: %s, want refused", text, got)
		}
	}
}

func TestARunsLogKeepsItsLastLines(t *testing.T) {
	log := &Log{}
	line := strings.Repeat("x", 1000)
	for index := 0; index < 70; index++ {
		log.Write(line)
	}
	kept := log.String()
	if len(kept) > logLimit || !strings.HasPrefix(kept, line) || strings.Count(kept, "\n") != len(kept)/(len(line)+1) {
		t.Fatalf("a log past its limit keeps whole lines, the last: %d bytes", len(kept))
	}
	log = &Log{}
	log.Write("a" + string([]byte{0xff}) + "b")
	if got := log.String(); got != "a"+string([]rune{0xfffd})+"b\n" {
		t.Fatalf("a line not UTF-8: %q", got)
	}
}
