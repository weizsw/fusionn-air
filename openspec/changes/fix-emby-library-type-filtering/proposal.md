## Why

The current implementation fetches movies from TV show libraries and TV shows from movie libraries, causing confusion and wasted API calls. The log shows "Fetching movies from library '电视节目' (TV Shows)" and "Fetching series from library '电影' (Movies)" - this is incorrect. Emby's API provides a `CollectionType` field to identify library content types, but we're not using it.

## What Changes

- Add `CollectionType` field to the `VirtualFolder` struct to capture library type from Emby API
- Implement centralized library iteration with type-based dispatch in `ProcessCleanup()`
- Refactor `processEmbyMovies` → `processEmbyMovieItems` to process pre-fetched items instead of iterating libraries
- Refactor `processEmbySeries` → `processEmbySeriesItems` to process pre-fetched items instead of iterating libraries
- Aggregate items from all appropriate libraries before processing (preserves single Trakt API call optimization)
- Add nil checks for Radarr/Sonarr data at dispatch level
- Filter libraries by type: `"movies"` libraries → movie processing, `"tvshows"` libraries → series processing

## Capabilities

### New Capabilities
- `emby-library-type-dispatch`: Type-aware library processing that identifies library content type and routes to appropriate handlers

### Modified Capabilities
- `emby-cleanup`: Change from per-processor library iteration to centralized type-based dispatch

## Impact

**Affected Files:**
- `internal/client/emby/types.go` - Add `CollectionType` field
- `internal/service/cleanup/cleanup.go` - Add centralized dispatch logic in `ProcessCleanup()`
- `internal/service/cleanup/emby_movies.go` - Refactor to process items instead of libraries
- `internal/service/cleanup/emby_series.go` - Refactor to process items instead of libraries

**Behavior Changes:**
- Logs will no longer show incorrect library type mismatches
- Only movie libraries will be queried for movies
- Only TV show libraries will be queried for series
- Single library iteration instead of two (efficiency improvement)
- Reduced unnecessary API calls to Emby

**No Breaking Changes:**
- External API contracts unchanged
- Queue file formats unchanged
- Configuration unchanged
