## Why

The library exclusion feature logs that it's excluding a library (e.g., "午夜时间" with ID=52319), but items from that library are still being processed and marked for deletion. This defeats the purpose of the exclusion configuration and could result in unwanted content deletion. The current client-side filtering approach using `ParentId` doesn't work because movie items' `ParentId` points to immediate parent folders, not library roots.

## What Changes

- Replace client-side library filtering with server-side query approach using Emby's `ParentId` parameter
- Query each non-excluded library separately instead of fetching all items recursively
- Remove the broken `filterByLibrary` function that attempts to match `item.ParentId` against library IDs
- Update Emby client to support querying items by library ID
- Update cleanup service to iterate through non-excluded libraries when fetching movies and series

## Capabilities

### New Capabilities

None - this is a bug fix to existing functionality.

### Modified Capabilities

- `emby-cleanup`: The library exclusion requirement will change from "filter items after fetching" to "only fetch from non-excluded libraries"

## Impact

**Code Changes**:
- `internal/client/emby/client.go`: Modify `GetAllMovies()` and `GetAllSeries()` to accept optional `ParentId` parameter
- `internal/service/cleanup/emby_movies.go`: Replace single fetch + filter with per-library iteration
- `internal/service/cleanup/emby_series.go`: Replace single fetch + filter with per-library iteration  
- `internal/service/cleanup/library_filter.go`: Remove `filterByLibrary()` function (no longer needed)
- `internal/service/cleanup/library_filter_test.go`: Update tests to reflect new approach

**API Changes**: 
- More granular Emby API calls (N calls for N non-excluded libraries instead of 1 recursive call)
- Reduced data transfer for users with large excluded libraries

**Performance**:
- Slightly increased API call count but reduced response payload size
- Overall performance impact: neutral to positive for most configurations
