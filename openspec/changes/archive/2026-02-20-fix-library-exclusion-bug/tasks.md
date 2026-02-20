## 1. Emby Client Updates

- [x] 1.1 Add `GetMovies(ctx, parentID)` method to Emby client that accepts optional ParentId parameter
- [x] 1.2 Add `GetSeries(ctx, parentID)` method to Emby client that accepts optional ParentId parameter
- [x] 1.3 Update `GetAllMovies()` to call `GetMovies(ctx, "")` for backward compatibility
- [x] 1.4 Update `GetAllSeries()` to call `GetSeries(ctx, "")` for backward compatibility
- [x] 1.5 Ensure ParentId is passed as query parameter when non-empty in both methods

## 2. Cleanup Service Refactor

- [x] 2.1 Update `processEmbyMovies()` to iterate through non-excluded libraries instead of single fetch
- [x] 2.2 Update `processEmbySeries()` to iterate through non-excluded libraries instead of single fetch
- [x] 2.3 For each non-excluded library, call `emby.GetMovies(ctx, libraryID)` and aggregate results
- [x] 2.4 For each non-excluded library, call `emby.GetSeries(ctx, libraryID)` and aggregate results
- [x] 2.5 Add logging to show which libraries are being queried (e.g., "Fetching movies from library 'Movies' (ID: 1628)")

## 3. Remove Client-Side Filtering

- [x] 3.1 Remove `filterByLibrary()` function from `library_filter.go`
- [x] 3.2 Remove the client-side filtering logic from `processEmbyMovies()` (lines that call filterByLibrary)
- [x] 3.3 Remove the client-side filtering logic from `processEmbySeries()` (lines that call filterByLibrary)
- [x] 3.4 Remove log line showing filtered count (e.g., "Filtered X/Y movies by library exclusion")

## 4. Update Tests

- [x] 4.1 Update `library_filter_test.go` to remove tests for `filterByLibrary()` function
- [x] 4.2 Add tests for new `GetMovies(ctx, parentID)` method with and without ParentId
- [x] 4.3 Add tests for new `GetSeries(ctx, parentID)` method with and without ParentId
- [x] 4.4 Update integration tests if any exist for Emby cleanup library exclusion

## 5. Verification

- [x] 5.1 Run existing tests to ensure no regressions
- [x] 5.2 Test with config containing excluded libraries to verify items are not fetched
- [x] 5.3 Verify log output shows libraries being queried individually
- [x] 5.4 Confirm excluded library items no longer appear in cleanup processing
- [x] 5.5 Test with empty excluded_libraries to ensure all libraries are still processed
