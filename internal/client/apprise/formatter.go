package apprise

import (
	"fmt"
	"strings"
)

// SlackFormatter formats messages for Slack readability
type SlackFormatter struct{}

// FormatWatcherResults formats watcher results for Slack
func (f *SlackFormatter) FormatWatcherResults(requested, skipped, errors int, details []WatcherDetail) string {
	var sb strings.Builder

	// Categorize
	var requestedItems []WatcherDetail
	var skippedItems []WatcherDetail
	var errorItems []WatcherDetail

	for _, d := range details {
		switch d.Action {
		case "requested", "dry_run":
			requestedItems = append(requestedItems, d)
		case "error":
			errorItems = append(errorItems, d)
		default:
			skippedItems = append(skippedItems, d)
		}
	}

	// Requested section
	if len(requestedItems) > 0 {
		fmt.Fprintf(&sb, "*📥 REQUESTED (%d):*\n", len(requestedItems))
		for _, item := range requestedItems {
			routeTag := ""
			if item.Route != "" {
				routeTag = fmt.Sprintf(" [→ %s]", item.Route)
			}
			fmt.Fprintf(&sb, "• %s S%02d ← %s%s\n", item.ShowTitle, item.Season, item.Reason, routeTag)
		}
		sb.WriteString("\n")
	}

	// Skipped section
	if len(skippedItems) > 0 {
		fmt.Fprintf(&sb, "*SKIPPED (%d):*\n", len(skippedItems))
		for _, item := range skippedItems {
			fmt.Fprintf(&sb, "• %s S%02d ← %s\n", item.ShowTitle, item.Season, item.Reason)
		}
		sb.WriteString("\n")
	}

	// Errors section
	if len(errorItems) > 0 {
		fmt.Fprintf(&sb, "*ERRORS (%d):*\n", len(errorItems))
		for _, item := range errorItems {
			fmt.Fprintf(&sb, "• %s S%02d ← %s\n", item.ShowTitle, item.Season, item.Reason)
		}
	}

	return sb.String()
}

// WatcherDetail represents a single watcher result item
type WatcherDetail struct {
	ShowTitle string
	Season    int
	Action    string
	Reason    string
	Route     string // "default", "alternate", or "" (no routing)
}
