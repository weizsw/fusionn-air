## Why

Emby cleanup treats all content not managed by Sonarr/Radarr as orphans and deletes them when fully watched. Some Emby libraries contain content managed outside Sonarr/Radarr (e.g., Anime, Kids, Music Videos) that should never be touched by cleanup. Without library-level exclusion, everything in those libraries is flagged as orphans and eventually deleted.

## What Changes

- Add an `excluded_libraries` configuration field under the `emby` config block, accepting a list of Emby library names to skip during cleanup
- Introduce a new Emby API call to fetch the list of libraries (`/Library/VirtualFolders`) and resolve configured names to library IDs
- Filter out items belonging to excluded libraries before orphan detection runs, for both series and movies
- Log warnings when a configured library name doesn't match any actual Emby library (catches typos)

## Capabilities

### New Capabilities

_None_ — this extends existing emby-cleanup behavior.

### Modified Capabilities

- `emby-cleanup`: Adding library-level exclusion as a filtering step before orphan detection. Items in excluded libraries are never considered for cleanup.

## Impact

- **Config**: `EmbyConfig` struct gains `ExcludedLibraries` field; `config.example.yaml` updated
- **Emby client**: New `GetLibraries()` method; `Item` struct gains `ParentId` field; existing `GetAllSeries`/`GetAllMovies` request the `ParentId` field
- **Cleanup service**: `emby_series.go` and `emby_movies.go` gain library filtering before orphan loop
- **No breaking changes**: Empty `excluded_libraries` preserves current behavior exactly
