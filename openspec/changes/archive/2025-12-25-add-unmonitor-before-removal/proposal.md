# Change: Unmonitor media before removal

## Why
When media is queued for cleanup, it remains monitored in Sonarr/Radarr. This means new versions (remux, better quality) could still be downloaded during the delay period, wasting bandwidth and disk space for content that will be deleted anyway.

## What Changes
- Add "unmonitor" step immediately when media is queued for cleanup
- Track unmonitor state in queue (`UnmonitoredAt` timestamp)
- Only proceed to deletion after delay_days have passed
- Respect dry_run mode for unmonitoring as well
- Add `UnmonitorSeries` to Sonarr client
- Add `UnmonitorMovie` to Radarr client

## Impact
- Affected specs: cleanup
- Affected code:
  - `internal/client/sonarr/client.go` - add UnmonitorSeries
  - `internal/client/radarr/client.go` - add UnmonitorMovie
  - `internal/service/cleanup/queue.go` - add UnmonitoredAt field
  - `internal/service/cleanup/series.go` - unmonitor when queuing
  - `internal/service/cleanup/movies.go` - unmonitor when queuing

## Flow Change

**Before:**
```
Watched → Queue (wait delay_days) → Delete
```

**After:**
```
Watched → Queue + Unmonitor → (wait delay_days) → Delete
```

