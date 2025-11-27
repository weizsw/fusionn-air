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
		sb.WriteString(fmt.Sprintf("*📥 REQUESTED (%d):*\n", len(requestedItems)))
		for _, item := range requestedItems {
			sb.WriteString(fmt.Sprintf("✅ %s S%02d ← %s\n", item.ShowTitle, item.Season, item.Reason))
		}
		sb.WriteString("\n")
	}

	// Skipped section
	if len(skippedItems) > 0 {
		sb.WriteString(fmt.Sprintf("*⏭️ SKIPPED (%d):*\n", len(skippedItems)))
		for _, item := range skippedItems {
			sb.WriteString(fmt.Sprintf("⏭️ %s S%02d ← %s\n", item.ShowTitle, item.Season, item.Reason))
		}
		sb.WriteString("\n")
	}

	// Errors section
	if len(errorItems) > 0 {
		sb.WriteString(fmt.Sprintf("*❌ ERRORS (%d):*\n", len(errorItems)))
		for _, item := range errorItems {
			sb.WriteString(fmt.Sprintf("❌ %s S%02d ← %s\n", item.ShowTitle, item.Season, item.Reason))
		}
	}

	return sb.String()
}

// FormatCleanupResults formats cleanup results for Slack
func (f *SlackFormatter) FormatCleanupResults(removed, queued, skipped, errors int, details []CleanupDetail, dryRun bool) string {
	var sb strings.Builder

	if dryRun {
		sb.WriteString("⚠️ *DRY RUN MODE*\n\n")
	}

	// Categorize details
	var removedItems []CleanupDetail
	var queuedItems []CleanupDetail
	var skippedItems []CleanupDetail
	var errorItems []CleanupDetail

	for _, d := range details {
		switch d.Action {
		case "removed", "dry_run_remove":
			removedItems = append(removedItems, d)
		case "queued":
			queuedItems = append(queuedItems, d)
		case "error":
			errorItems = append(errorItems, d)
		default:
			skippedItems = append(skippedItems, d)
		}
	}

	// Removed section
	if len(removedItems) > 0 {
		if dryRun {
			sb.WriteString(fmt.Sprintf("*🗑️ WOULD REMOVE (%d):*\n", len(removedItems)))
		} else {
			sb.WriteString(fmt.Sprintf("*🗑️ REMOVED (%d):*\n", len(removedItems)))
		}
		for _, item := range removedItems {
			sb.WriteString(fmt.Sprintf("✅ %s", item.Title))
			if item.SizeOnDisk != "" {
				sb.WriteString(fmt.Sprintf(" [%s]", item.SizeOnDisk))
			}
			sb.WriteString(fmt.Sprintf(" ← %s\n", item.Reason))
		}
		sb.WriteString("\n")
	}

	// Queued section
	if len(queuedItems) > 0 {
		sb.WriteString(fmt.Sprintf("*⏳ QUEUED FOR REMOVAL (%d):*\n", len(queuedItems)))
		for _, item := range queuedItems {
			sb.WriteString(fmt.Sprintf("⏳ %s", item.Title))
			if item.SizeOnDisk != "" {
				sb.WriteString(fmt.Sprintf(" [%s]", item.SizeOnDisk))
			}
			sb.WriteString(fmt.Sprintf(" ← %s", item.Reason))
			if item.DaysUntil > 0 {
				sb.WriteString(fmt.Sprintf(" (removes in %d days)", item.DaysUntil))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Skipped section
	if len(skippedItems) > 0 {
		sb.WriteString(fmt.Sprintf("*⏭️ SKIPPED (%d):*\n", len(skippedItems)))
		for _, item := range skippedItems {
			sb.WriteString(fmt.Sprintf("⏭️ %s ← %s\n", item.Title, item.Reason))
		}
		sb.WriteString("\n")
	}

	// Errors section
	if len(errorItems) > 0 {
		sb.WriteString(fmt.Sprintf("*❌ ERRORS (%d):*\n", len(errorItems)))
		for _, item := range errorItems {
			sb.WriteString(fmt.Sprintf("❌ %s ← %s\n", item.Title, item.Reason))
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
}

// CleanupDetail represents a single cleanup result item
type CleanupDetail struct {
	Title      string
	Action     string
	Reason     string
	DaysUntil  int
	SizeOnDisk string
}
