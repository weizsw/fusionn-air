# Change: Fix duplicate status display for media items

## Why

When a movie or series is ready for deletion (delay period has passed), it appears twice in the summary output: once in the QUEUED section and once in the REMOVED section. This happens because the processing logic first checks all items and marks queued items as "queued" before processing the removal queue. This creates confusing output where the same item shows under two different statuses.

## What Changes

- Modify cleanup processing to exclude items from "queued" results if they will be removed in the same run
- Ensure each media item appears under exactly one status category: skipped, queued, or removed
- Apply the same fix to notification formatting so users don't see duplicate entries in notifications

## Impact

- Affected specs: cleanup, cleanup-notifications
- Affected code:
  - `internal/service/cleanup/movies.go` - skip queued items that are ready for removal
  - `internal/service/cleanup/series.go` - skip queued items that are ready for removal
  - Summary and notification output will show each item exactly once
