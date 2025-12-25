# Change: Show queued media as queued even when unmonitored

## Why

When media is queued for deletion, it's immediately unmonitored. On subsequent cleanup runs, the "not monitored" check (lines 80-84 in series.go, 81-85 in movies.go) skips it, causing it to disappear from "QUEUED" and reappear in "SKIPPED (not monitored)". Users think deletion was cancelled, but it's still queued and will be deleted after the delay period - they just don't see it in notifications anymore.

**Real-world example:**

- Day 1: `QUEUED (1): IT: Welcome to Derry - added to queue (in 3 days)`
- Day 2: `SKIPPED: IT: Welcome to Derry ← not monitored` (looks cancelled!)
- Day 3: Deletes without warning

## What Changes

- Check if media is in the queue BEFORE checking if it's monitored
- If already queued (even if unmonitored), return as "queued" not "skipped"
- Update reason text to show "queued for deletion (unmonitored)" instead of "added to queue"
- Update unmonitor log messages to include deletion context
- Apply to both series (Sonarr) and movies (Radarr)

## Impact

- Affected specs: cleanup-notifications (new spec focusing on notification consistency)
- Affected code:
  - `internal/service/cleanup/series.go` - reorder checks, update reason text, update logs
  - `internal/service/cleanup/movies.go` - reorder checks, update reason text, update logs

## Example Changes

### Notification - Day 1 (newly queued)

**Before:**

```
QUEUED (1):
• IT: Welcome to Derry [53.0 GB] ← fully watched (S01) - added to queue (in 3 days)
```

**After:**

```
QUEUED (1):
• IT: Welcome to Derry [53.0 GB] ← fully watched (S01) - queued for deletion (unmonitored) (in 3 days)
```

### Notification - Day 2 (still queued)

**Before (PROBLEM):**

```
SKIPPED:
• IT: Welcome to Derry ← not monitored
```

**After (FIXED):**

```
QUEUED (1):
• IT: Welcome to Derry [53.0 GB] ← fully watched (S01) - queued for deletion (unmonitored) (in 2 days)
```

### Log Message

**Before:**

```
🔕 Unmonitored series: Breaking Bad
```

**After:**

```
🔕 Unmonitored series: Breaking Bad (queued for deletion)
```
