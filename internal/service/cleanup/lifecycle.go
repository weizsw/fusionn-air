package cleanup

import (
	"context"
	"fmt"

	"github.com/fusionn-air/pkg/logger"
)

func existingCandidateResult(queue *Queue, id, delayDays int, result MediaResult, queuedSuffix string) (MediaResult, bool) {
	item, state, daysUntil := queue.status(id, delayDays)
	if item == nil {
		return result, false
	}
	if state == candidateReady {
		return MediaResult{}, true
	}

	result.Action = "queued"
	result.DaysUntil = daysUntil
	if state == candidatePendingUnmonitor {
		result.Reason = item.Reason + " - pending unmonitor"
	} else {
		result.Reason = item.Reason + queuedSuffix
	}
	return result, true
}

func scheduleCandidate(queue *Queue, item *QueueItem, delayDays int, dryRun bool, result MediaResult, queuedSuffix string) (MediaResult, error) {
	if !dryRun {
		if _, err := queue.Add(item); err != nil {
			result.Action = "error"
			result.Reason = fmt.Sprintf("queue failed: %v", err)
			return result, err
		}
	}
	result.Action = "queued"
	result.Reason = item.Reason + queuedSuffix
	result.DaysUntil = delayDays
	return result, nil
}

func retryPendingUnmonitor(queue *Queue, result *ProcessingResult, mediaType MediaType, dryRun bool, unmonitor func(*QueueItem) (bool, error)) error {
	if dryRun {
		return nil
	}
	for _, item := range queue.GetAll() {
		if !queue.NeedsUnmonitor(item.ID) {
			continue
		}
		if _, err := unmonitor(item); err != nil {
			result.AddResult(MediaResult{Type: mediaType, Title: item.Title, ID: item.ID, Action: "error", Reason: err.Error()})
			return err
		}
	}
	return nil
}

type removalPlan struct {
	mediaType     MediaType
	label         string
	removedReason string
	formatSize    func(int64) string
	remove        func(context.Context, *QueueItem) (bool, error)
}

func processReadyCandidates(ctx context.Context, result *ProcessingResult, queue *Queue, delayDays int, exclusions []string, dryRun bool, plan removalPlan) error {
	ready := queue.GetReadyForRemoval(delayDays)
	if len(ready) == 0 {
		return nil
	}
	logger.Infof("🗑️  %d %s ready for removal", len(ready), plan.label)

	for _, item := range ready {
		if isExcluded(item.Title, exclusions) {
			if !dryRun {
				if err := queue.Remove(item.ID); err != nil {
					result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "error", Reason: fmt.Sprintf("queue persistence failed: %v", err)})
					return fmt.Errorf("dequeue excluded %s %q: %w", plan.label, item.Title, err)
				}
			}
			if !hasMediaResult(result, plan.mediaType, item.ID) {
				result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "skipped", Reason: "in exclusion list"})
			}
			continue
		}

		size := ""
		if plan.formatSize != nil {
			size = plan.formatSize(item.SizeOnDisk)
		}
		if dryRun {
			logger.Warnf("🗑️  [DRY RUN] Would delete: %s", item.Title)
			result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "dry_run_remove", Reason: "would be deleted", SizeOnDisk: size})
			continue
		}

		removed, err := plan.remove(ctx, item)
		if err != nil {
			logger.Errorf("❌ Failed to remove %s: %v", item.Title, err)
			result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "error", Reason: fmt.Sprintf("remove failed: %v", err)})
			continue
		}
		if err := queue.Remove(item.ID); err != nil {
			reason := fmt.Sprintf("queue persistence failed: %v", err)
			if removed {
				reason = "deleted but " + reason
			}
			result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "error", Reason: reason})
			return fmt.Errorf("persist removal of %s %q: %w", plan.label, item.Title, err)
		}
		if !removed {
			logger.Infof("ℹ️  %s already removed, cleared from queue", item.Title)
			continue
		}
		logger.Infof("✅ Deleted: %s", item.Title)
		result.AddResult(MediaResult{Type: plan.mediaType, Title: item.Title, ID: item.ID, Action: "removed", Reason: plan.removedReason, SizeOnDisk: size})
	}
	return nil
}

func hasMediaResult(result *ProcessingResult, mediaType MediaType, id int) bool {
	for _, existing := range result.Results {
		if existing.Type == mediaType && existing.ID == id {
			return true
		}
	}
	return false
}
