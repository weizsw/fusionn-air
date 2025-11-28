package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/fusionn-air/internal/client/apprise"
	"github.com/fusionn-air/pkg/logger"
)

func (s *Service) printSummary(result *ProcessingResult, startTime time.Time) {
	var toRemove, queued, skipped, errors []string

	for _, r := range result.Results {
		icon := mediaIcon(r.Type)
		title := r.Title
		if r.Year > 0 {
			title = fmt.Sprintf("%s (%d)", r.Title, r.Year)
		}

		switch r.Action {
		case "removed", "dry_run_remove":
			info := fmt.Sprintf("   ✅ %s %-33s", icon, title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			info += fmt.Sprintf("  ← %s", r.Reason)
			toRemove = append(toRemove, info)

		case "queued":
			info := fmt.Sprintf("   ⏳ %s %-33s", icon, title)
			if r.SizeOnDisk != "" {
				info += fmt.Sprintf(" [%s]", r.SizeOnDisk)
			}
			if r.DaysUntil > 0 {
				info += fmt.Sprintf("  ← %s (in %d days)", r.Reason, r.DaysUntil)
			} else {
				info += fmt.Sprintf("  ← %s (ready)", r.Reason)
			}
			queued = append(queued, info)

		case "skipped":
			info := fmt.Sprintf("   ⏭️  %s %-33s", icon, title)
			info += fmt.Sprintf("  ← %s", r.Reason)
			skipped = append(skipped, info)

		case "error":
			info := fmt.Sprintf("   ❌ %s %-33s", icon, title)
			info += fmt.Sprintf("  ← %s", r.Reason)
			errors = append(errors, info)
		}
	}

	logger.Info("")
	logger.Info("┌──────────────────────────────────────────────────────────────┐")
	logger.Info("│                    CLEANUP RESULTS                           │")
	logger.Info("└──────────────────────────────────────────────────────────────┘")

	if len(toRemove) > 0 {
		logger.Info("")
		if s.dryRun {
			logger.Warnf("🗑️  WOULD REMOVE (%d):", len(toRemove))
		} else {
			logger.Infof("🗑️  REMOVED (%d):", len(toRemove))
		}
		for _, line := range toRemove {
			logger.Info(line)
		}
	}

	if len(queued) > 0 {
		logger.Info("")
		logger.Infof("⏳ QUEUED FOR REMOVAL (%d):", len(queued))
		for _, line := range queued {
			logger.Info(line)
		}
	}

	if len(skipped) > 0 {
		logger.Info("")
		logger.Infof("⏭️  SKIPPED (%d):", len(skipped))
		for _, line := range skipped {
			logger.Info(line)
		}
	}

	if len(errors) > 0 {
		logger.Info("")
		logger.Errorf("❌ ERRORS (%d):", len(errors))
		for _, line := range errors {
			logger.Error(line)
		}
	}

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

func (s *Service) sendNotification(ctx context.Context, result *ProcessingResult) {
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
	if s.dryRun {
		title = "🧹 Cleanup Results (DRY RUN)"
	}

	body := formatter.FormatCleanupResults(totalRemoved, totalQueued, totalSkipped, result.Errors, details, s.dryRun)

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
	case MediaTypeSeries:
		return "📺"
	case MediaTypeMovie:
		return "🎬"
	default:
		return "📦"
	}
}
