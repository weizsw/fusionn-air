package apprise

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWatcherSummaryKeepsFailureVisibleAndFitsDiscord(t *testing.T) {
	var details []WatcherDetail
	for range 20 {
		details = append(details, WatcherDetail{ShowTitle: strings.Repeat("剧", 400), Season: 2, Action: "requested"})
	}
	details = append(details, WatcherDetail{ShowTitle: "Example", Season: 3, Action: "error", Reason: "upstream unavailable"})
	body := FormatWatcherResults(20, 150, 1, details)
	if len(body) > 1800 || !utf8.ValidString(body) || !strings.Contains(body, "upstream unavailable") || !strings.Contains(body, "Skipped: 150") || !strings.Contains(body, "more. See watcher stats.") {
		t.Fatalf("invalid notification: %q", body)
	}
}
