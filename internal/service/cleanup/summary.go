package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) printSummary(result *ProcessingResult, startTime time.Time, dryRun bool) {
	// Separate results by media type
	seriesResults := make(map[string][]MediaResult)
	movieResults := make(map[string][]MediaResult)

	embySeriesResults := make(map[string][]MediaResult)
	embyMovieResults := make(map[string][]MediaResult)

	for _, r := range result.Results {
		switch r.Type {
		case MediaTypeSeries:
			seriesResults[r.Action] = append(seriesResults[r.Action], r)
		case MediaTypeMovie:
			movieResults[r.Action] = append(movieResults[r.Action], r)
		case MediaTypeEmbySeries:
			embySeriesResults[r.Action] = append(embySeriesResults[r.Action], r)
		case MediaTypeEmbyMovie:
			embyMovieResults[r.Action] = append(embyMovieResults[r.Action], r)
		}
	}

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│                    CLEANUP RESULTS                           │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	printMediaSection("📺 SERIES (Sonarr)", seriesResults, dryRun)
	printMediaSection("🎬 MOVIES (Radarr)", movieResults, dryRun)
	printMediaSection("📺 SERIES (Emby)", embySeriesResults, dryRun)
	printMediaSection("🎬 MOVIES (Emby)", embyMovieResults, dryRun)

	// Print per-type stats
	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	for t, stats := range result.Stats {
		logger.Infof("%s %s: %d scanned, %d queued, %d removed, %d skipped",
			mediaIcon(t), t, stats.Scanned, stats.MarkedForQueue, stats.Removed, stats.Skipped)
	}
	logger.Infof("⏱️  Completed in %v", time.Since(startTime).Round(time.Millisecond))
	logger.Info("")
}

func printMediaSection(header string, results map[string][]MediaResult, dryRun bool) {
	// Check if there's anything to print
	hasContent := false
	for _, items := range results {
		if len(items) > 0 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return
	}

	logger.Info("")
	logger.Infof("── %s ──", header)

	// Removed items
	removed := results["removed"]
	removed = append(removed, results["dry_run_remove"]...)
	if len(removed) > 0 {
		if dryRun {
			logger.Warnf("  WOULD REMOVE (%d):", len(removed))
		} else {
			logger.Infof("  REMOVED (%d):", len(removed))
		}
		for _, r := range removed {
			title := formatTitle(r)
			info := fmt.Sprintf("   • %-35s", title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			info += fmt.Sprintf("  ← %s", r.Reason)
			logger.Info(info)
		}
	}

	// Queued items
	if queued := results["queued"]; len(queued) > 0 {
		if dryRun {
			logger.Warnf("  WOULD QUEUE (%d):", len(queued))
		} else {
			logger.Infof("  QUEUED (%d):", len(queued))
		}
		for _, r := range queued {
			title := formatTitle(r)
			info := fmt.Sprintf("   • %-35s", title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			if r.DaysUntil > 0 {
				info += fmt.Sprintf("  ← %s (in %d days)", r.Reason, r.DaysUntil)
			} else {
				info += fmt.Sprintf("  ← %s (ready)", r.Reason)
			}
			logger.Info(info)
		}
	}

	// Skipped items
	if skipped := results["skipped"]; len(skipped) > 0 {
		logger.Infof("  SKIPPED (%d):", len(skipped))
		for _, r := range skipped {
			title := formatTitle(r)
			info := fmt.Sprintf("   • %-35s  ← %s", title, r.Reason)
			logger.Info(info)
		}
	}

	// Error items
	if errors := results["error"]; len(errors) > 0 {
		logger.Errorf("  ERRORS (%d):", len(errors))
		for _, r := range errors {
			title := formatTitle(r)
			info := fmt.Sprintf("   • %-35s  ← %s", title, r.Reason)
			logger.Error(info)
		}
	}
}

func formatTitle(r MediaResult) string {
	if r.Year > 0 {
		return fmt.Sprintf("%s (%d)", r.Title, r.Year)
	}
	return r.Title
}

func (s *Service) sendNotification(ctx context.Context, result *ProcessingResult, dryRun bool) {
	if s.apprise == nil || !s.apprise.IsEnabled() {
		return
	}

	logger.Info("🔔 Sending notification...")
	formatter := &apprise.SlackFormatter{}

	var details []apprise.CleanupDetail
	for _, r := range result.Results {
		details = append(details, apprise.CleanupDetail{
			Title:      r.Title,
			Action:     r.Action,
			Reason:     r.Reason,
			DaysUntil:  r.DaysUntil,
			SizeOnDisk: r.SizeOnDisk,
			MediaType:  string(r.Type),
		})
	}

	// Calculate totals
	var totalRemoved, totalQueued, totalSkipped int
	for _, stats := range result.Stats {
		totalRemoved += stats.Removed
		totalQueued += stats.MarkedForQueue
		totalSkipped += stats.Skipped
	}

	title := "🧹 Cleanup Results"
	if dryRun {
		title = "🧹 Cleanup Results (DRY RUN)"
	}

	body := formatter.FormatCleanupResults(totalRemoved, totalQueued, totalSkipped, result.Errors, details, dryRun)

	notifyType := "info"
	if totalRemoved > 0 {
		notifyType = "success"
	}

	if err := s.apprise.Notify(ctx, title, body, notifyType); err != nil {
		logger.Warnf("🔔 Failed to send notification: %v", err)
	} else {
		logger.Info("🔔 Notification sent successfully")
	}
}

func mediaIcon(t MediaType) string {
	switch t {
	case MediaTypeSeries, MediaTypeEmbySeries:
		return "📺"
	case MediaTypeMovie, MediaTypeEmbyMovie:
		return "🎬"
	default:
		return "📦"
	}
}
