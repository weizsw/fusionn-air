## 1. Configuration

- [x] 1.1 Add `ExcludedLibraries []string` field to `EmbyConfig` in `internal/config/config.go`
- [x] 1.2 Add `excluded_libraries` with example values to `config/config.example.yaml`

## 2. Emby Client

- [x] 2.1 Add `ParentId string` field to the `Item` struct in `internal/client/emby/types.go`
- [x] 2.2 Add `Library` struct (Name, ItemId) and `VirtualFoldersResponse` to `internal/client/emby/types.go`
- [x] 2.3 Add `GetLibraries(ctx)` method to the Emby client that calls `/Library/VirtualFolders`
- [x] 2.4 Update `GetAllSeries` and `GetAllMovies` to include `ParentId` in the `Fields` query param

## 3. Library Filtering Logic

- [x] 3.1 Add `ResolveExcludedLibraryIDs` helper that takes configured names and fetched libraries, returns `map[string]bool` of excluded IDs, and logs warnings for unmatched names
- [x] 3.2 Add `filterByLibrary` helper that takes items and excluded IDs, returns filtered items

## 4. Cleanup Integration

- [x] 4.1 Update `processEmbySeries` to accept excluded library IDs and filter items before orphan detection
- [x] 4.2 Update `processEmbyMovies` to accept excluded library IDs and filter items before orphan detection
- [x] 4.3 Update the cleanup orchestrator to call `GetLibraries` once, resolve excluded IDs, and pass to both Emby processing functions
- [x] 4.4 Add log line showing how many items were filtered by library exclusion

## 5. Tests

- [x] 5.1 Add unit tests for `ResolveExcludedLibraryIDs` (match, no match, case-insensitive, empty list)
- [x] 5.2 Add unit tests for `filterByLibrary` (items filtered, items preserved, empty exclusion set)
- [x] 5.3 Add unit tests for library filtering (no prior tests existed)
