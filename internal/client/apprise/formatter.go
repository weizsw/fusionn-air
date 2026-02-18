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

// FormatCleanupResults formats cleanup results for Slack
func (f *SlackFormatter) FormatCleanupResults(removed, queued, skipped, errors int, details []CleanupDetail, dryRun bool) string {
	var sb strings.Builder

	if dryRun {
		sb.WriteString("⚠️ *DRY RUN MODE*\n\n")
	}

	seriesDetails := make(map[string][]CleanupDetail)
	movieDetails := make(map[string][]CleanupDetail)
	embySeriesDetails := make(map[string][]CleanupDetail)
	embyMovieDetails := make(map[string][]CleanupDetail)

	for _, d := range details {
		switch d.MediaType {
		case "movie":
			movieDetails[d.Action] = append(movieDetails[d.Action], d)
		case "emby_series":
			embySeriesDetails[d.Action] = append(embySeriesDetails[d.Action], d)
		case "emby_movie":
			embyMovieDetails[d.Action] = append(embyMovieDetails[d.Action], d)
		default:
			seriesDetails[d.Action] = append(seriesDetails[d.Action], d)
		}
	}

	f.formatMediaTypeSection(&sb, "📺 SERIES (Sonarr)", seriesDetails, dryRun)
	f.formatMediaTypeSection(&sb, "🎬 MOVIES (Radarr)", movieDetails, dryRun)
	f.formatMediaTypeSection(&sb, "📺 SERIES (Emby)", embySeriesDetails, dryRun)
	f.formatMediaTypeSection(&sb, "🎬 MOVIES (Emby)", embyMovieDetails, dryRun)

	return sb.String()
}

func (f *SlackFormatter) formatMediaTypeSection(sb *strings.Builder, header string, details map[string][]CleanupDetail, dryRun bool) {
	// Check if there's anything to print
	hasContent := false
	for _, items := range details {
		if len(items) > 0 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return
	}

	fmt.Fprintf(sb, "*%s*\n", header)

	// Removed section
	removed := details["removed"]
	removed = append(removed, details["dry_run_remove"]...)
	if len(removed) > 0 {
		if dryRun {
			fmt.Fprintf(sb, "WOULD REMOVE (%d):\n", len(removed))
		} else {
			fmt.Fprintf(sb, "REMOVED (%d):\n", len(removed))
		}
		for _, item := range removed {
			fmt.Fprintf(sb, "• %s", item.Title)
			if item.SizeOnDisk != "" {
				fmt.Fprintf(sb, " [%s]", item.SizeOnDisk)
			}
			fmt.Fprintf(sb, " ← %s\n", item.Reason)
		}
		sb.WriteString("\n")
	}

	// Queued section
	if queued := details["queued"]; len(queued) > 0 {
		fmt.Fprintf(sb, "QUEUED (%d):\n", len(queued))
		for _, item := range queued {
			fmt.Fprintf(sb, "• %s", item.Title)
			if item.SizeOnDisk != "" {
				fmt.Fprintf(sb, " [%s]", item.SizeOnDisk)
			}
			fmt.Fprintf(sb, " ← %s", item.Reason)
			if item.DaysUntil > 0 {
				fmt.Fprintf(sb, " (in %d days)", item.DaysUntil)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Skipped section
	if skipped := details["skipped"]; len(skipped) > 0 {
		fmt.Fprintf(sb, "SKIPPED (%d):\n", len(skipped))
		for _, item := range skipped {
			fmt.Fprintf(sb, "• %s ← %s\n", item.Title, item.Reason)
		}
		sb.WriteString("\n")
	}

	// Errors section
	if errors := details["error"]; len(errors) > 0 {
		fmt.Fprintf(sb, "ERRORS (%d):\n", len(errors))
		for _, item := range errors {
			fmt.Fprintf(sb, "• %s ← %s\n", item.Title, item.Reason)
		}
		sb.WriteString("\n")
	}
}

// WatcherDetail represents a single watcher result item
type WatcherDetail struct {
	ShowTitle string
	Season    int
	Action    string
	Reason    string
	Route     string // "default", "alternate", or "" (no routing)
}

// CleanupDetail represents a single cleanup result item
type CleanupDetail struct {
	Title      string
	Action     string
	Reason     string
	DaysUntil  int
	SizeOnDisk string
	MediaType  string // "series" or "movie"
}
