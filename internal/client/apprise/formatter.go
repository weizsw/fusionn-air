package apprise

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const notificationLimit = 1800

// FormatWatcherResults lists errors and requests. Skipped seasons appear in the counts.
func FormatWatcherResults(requested, skipped, errors int, details []WatcherDetail) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Requests: %d · Skipped: %d · Errors: %d\n", requested, skipped, errors)
	omitted := 0
	for _, errorSection := range []bool{true, false} {
		for _, item := range details {
			if (item.Action == "error") != errorSection {
				continue
			}
			line := fmt.Sprintf("• %s S%02d · %s · %s", item.ShowTitle, item.Season, item.Action, item.Reason)
			if item.Route != "" {
				line += " [" + item.Route + "]"
			}
			line = truncate(line, 400) + "\n"
			if body.Len()+len(line) > notificationLimit-60 {
				omitted++
				continue
			}
			body.WriteString(line)
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&body, "… %d more. See watcher stats.", omitted)
	}
	return body.String()
}

// LimitNotification leaves room for the title in Discord's 2000-character limit.
func LimitNotification(text string) string {
	return truncate(text, notificationLimit)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit - len("…")
	for !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "…"
}

// WatcherDetail represents a request or a failed season check.
type WatcherDetail struct {
	ShowTitle string
	Season    int
	Action    string
	Reason    string
	Route     string
}
