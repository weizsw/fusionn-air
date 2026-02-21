## 1. Update Data Structures

- [x] 1.1 Add `CollectionType` field to `VirtualFolder` struct in `internal/client/emby/types.go`
- [x] 1.2 Verify field is properly unmarshaled from Emby API JSON response

## 2. Refactor Emby Movie Processing

- [x] 2.1 Rename `processEmbyMovies` to `processEmbyMovieItems` in `internal/service/cleanup/emby_movies.go`
- [x] 2.2 Change function signature to accept `movies []emby.Item` instead of `libraries` and `excludedLibNames`
- [x] 2.3 Remove library iteration loop (lines 24-38)
- [x] 2.4 Remove `var allMovies []emby.Item` aggregation logic
- [x] 2.5 Update function to directly process the provided movies slice
- [x] 2.6 Update callers to pass movies slice instead of libraries

## 3. Refactor Emby Series Processing

- [x] 3.1 Rename `processEmbySeries` to `processEmbySeriesItems` in `internal/service/cleanup/emby_series.go`
- [x] 3.2 Change function signature to accept `series []emby.Item` instead of `libraries` and `excludedLibNames`
- [x] 3.3 Remove library iteration loop (lines 24-38)
- [x] 3.4 Remove `var allSeries []emby.Item` aggregation logic
- [x] 3.5 Update function to directly process the provided series slice
- [x] 3.6 Update callers to pass series slice instead of libraries

## 4. Implement Centralized Dispatch Logic

- [x] 4.1 In `internal/service/cleanup/cleanup.go`, locate the Emby processing section in `ProcessCleanup()`
- [x] 4.2 Add variables for item aggregation: `var allMovies []emby.Item` and `var allSeries []emby.Item`
- [x] 4.3 Implement library iteration loop over `libraries`
- [x] 4.4 Add exclusion check: skip if `excludedLibNames[lib.Name]` is true
- [x] 4.5 Add switch statement on `lib.CollectionType`
- [x] 4.6 Implement `case "movies"`: check Radarr data, fetch movies, aggregate to `allMovies`
- [x] 4.7 Implement `case "tvshows"`: check Sonarr data, fetch series, aggregate to `allSeries`
- [x] 4.8 Implement `default` case: log debug message for unsupported types
- [x] 4.9 Add error handling for fetch failures (log and continue)
- [x] 4.10 Add logging for item counts per library

## 5. Update Processor Invocations

- [x] 5.1 Replace call to `s.processEmbyMovies(...)` with aggregation check and `s.processEmbyMovieItems(...)`
- [x] 5.2 Replace call to `s.processEmbySeries(...)` with aggregation check and `s.processEmbySeriesItems(...)`
- [x] 5.3 Verify only `allMovies` and `allSeries` are passed (no libraries, no exclusion map)
- [x] 5.4 Ensure Trakt fetching still happens once per media type inside processors

## 6. Add Nil Checks for Manager Data

- [x] 6.1 In movie library case, add check: `if radarrTmdbIDs == nil` → log warning and continue
- [x] 6.2 In TV show library case, add check: `if sonarrTvdbIDs == nil` → log warning and continue
- [x] 6.3 Ensure warning logs include library name for clarity

## 7. Update Logging

- [x] 7.1 Add log for movie libraries: "🎬 Found N movies in library \"NAME\""
- [x] 7.2 Add log for TV show libraries: "📺 Found N series in library \"NAME\""
- [x] 7.3 Add debug log for skipped library types: "📚 Skipping library \"NAME\" (unsupported type: TYPE)"
- [x] 7.4 Add warning log for manager data unavailable: "⚠️ Skipping movie library \"NAME\" - Radarr data unavailable"
- [x] 7.5 Verify exclusion logs remain: "📚 Skipping excluded library \"NAME\" (ID: ID)"

## 8. Testing and Validation

- [x] 8.1 Run cleanup and verify logs show correct library type routing
- [x] 8.2 Verify no "Fetching movies from library" logs for TV show libraries
- [x] 8.3 Verify no "Fetching series from library" logs for movie libraries
- [x] 8.4 Verify item counts match previous runs (no data loss)
- [x] 8.5 Verify Trakt API is called once per media type (check debug logs or network)
- [x] 8.6 Test with Radarr unavailable: verify movie libraries are skipped with warnings
- [x] 8.7 Test with Sonarr unavailable: verify TV show libraries are skipped with warnings
- [x] 8.8 Verify excluded libraries are still skipped correctly
