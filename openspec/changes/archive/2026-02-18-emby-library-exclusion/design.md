## Context

Emby cleanup identifies "orphan" content — items present in Emby but not managed by Sonarr (series) or Radarr (movies). It then checks Trakt watch status and removes fully watched orphans after a configurable delay.

The current implementation fetches **all** series/movies from Emby via `/Items?Recursive=true` with no library scoping. Users who have Emby libraries with content managed outside Sonarr/Radarr (e.g., Anime via a different tool, a Kids library, or Music Videos) find that everything in those libraries gets classified as orphans and eventually deleted.

The only existing exclusion mechanism is `cleanup.exclusions` — a title-level list. This doesn't scale when an entire library should be ignored.

## Goals / Non-Goals

**Goals:**
- Allow users to exclude entire Emby libraries from cleanup by name
- Preserve existing behavior when no libraries are excluded (empty list = process everything)
- Warn on misconfigured library names so typos don't silently fail

**Non-Goals:**
- Per-library cleanup rules (different delay_days per library)
- Include-list semantics (only process these libraries)
- Library-level filtering for non-Emby cleanup (Sonarr/Radarr operate on their own managed content, not by library)

## Decisions

### 1. Config placement: `emby.excluded_libraries`

The exclusion list lives under `emby` (not `cleanup`) because it controls which Emby content the system even sees. It's an Emby-specific concept, unlike `cleanup.exclusions` which is a cross-cutting title filter.

```yaml
emby:
  excluded_libraries:
    - "Anime"
    - "Kids"
```

**Alternative considered:** Placing under `cleanup.emby_excluded_libraries`. Rejected because this is about Emby library scoping, not cleanup policy.

### 2. Client-side filtering with library ID resolution

**Approach:** Fetch all items as today, but also fetch Emby's library list via `/Library/VirtualFolders`. Build a set of excluded library IDs by matching configured names (case-insensitive). Filter items by checking if `ParentId` is in the excluded set.

**Why this over server-side filtering:** The Emby `/Items` API accepts a single `ParentId` param, so scoping to multiple libraries requires N separate API calls. Data volumes are small (hundreds of items), so fetching all and filtering client-side is simpler and keeps one request per item type.

**Changes required:**
- New `GetLibraries()` method on the Emby client → calls `/Library/VirtualFolders`
- Add `ParentId` to the `Item` struct and to the `Fields` query param in `GetAllSeries` / `GetAllMovies`
- New `ResolveExcludedLibraryIDs()` helper that takes library names and the full library list, returns a `map[string]bool` of excluded IDs, and logs warnings for unmatched names

### 3. Filtering happens in the cleanup orchestrator, once per run

The orchestrator (`cleanup.go`) calls `GetLibraries()` once, resolves the excluded set, and passes it to `processEmbySeries` and `processEmbyMovies`. This avoids duplicating the library fetch and keeps the filtering logic co-located with the rest of the cleanup orchestration.

Filtering is applied after fetching items but **before** orphan detection (before the Sonarr/Radarr ID subtraction). Items in excluded libraries are removed from the list entirely — they never enter the orphan pipeline.

### 4. Case-insensitive library name matching

Library names are matched case-insensitively (`strings.EqualFold`) to be forgiving of config typos like "anime" vs "Anime". A warning is logged for any configured name that doesn't match any Emby library.

## Risks / Trade-offs

- **[Emby API availability]** The `/Library/VirtualFolders` endpoint is standard Emby API but may behave differently across versions. → Mitigation: fail gracefully — if the call fails, log a warning and proceed without filtering (process everything as before).
- **[ParentId semantics]** For top-level items (Series, Movie), `ParentId` should be the library ID. If Emby changes this in a future version, filtering would break silently. → Mitigation: log the count of filtered items so users can verify.
- **[Library renamed in Emby]** If a user renames a library in Emby but forgets to update the config, the old name stops matching and that library's content re-enters cleanup. → Mitigation: the warning log for unmatched names catches this immediately.
