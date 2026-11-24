package ui

import (
	"strings"
	"testing"
)

func TestGeneralAssignmentPreservesCountdownOwnership(t *testing.T) {
	for _, write := range []string{"remaining += 1", "remaining ??= 1", "playing ||= true", "duration--"} {
		source := `<content>` + countdownOf(` duration="5m"`+countdownStarted, countdownClock+`<button click="`+write+`">Set</button>`) + `</content>`
		_, err := memorySite("", countdownSite(source)).Compile("/site")
		if err == nil || !strings.Contains(err.Error(), "chỉ đọc") {
			t.Errorf("%s: expected ownership error, got %v", write, err)
		}
	}
	for _, write := range []string{"$tea.remaining = 1", "$tea['remaining']++", "$tea[key] ??= 1", "$tea.data.value = 1"} {
		source := `<content>` + countdownOf(` as="$tea" duration="5m"`+countdownStarted, countdownClock) + `<button click="` + write + `">Set</button></content>`
		_, err := memorySite("", countdownSite(source)).Compile("/site")
		if err == nil || !strings.Contains(err.Error(), "không ghi trực tiếp qua alias") {
			t.Errorf("%s: expected alias ownership error, got %v", write, err)
		}
	}
}
