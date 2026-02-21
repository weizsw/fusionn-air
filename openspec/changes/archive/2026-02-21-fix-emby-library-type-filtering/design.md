## Context

**Current State:**
The cleanup service processes Emby libraries by calling `processEmbyMovies()` and `processEmbySeries()` separately. Each function iterates through ALL libraries and attempts to fetch its respective content type, resulting in:
- Movies being fetched from TV show libraries (returns empty results)
- Series being fetched from movie libraries (returns empty results)
- Two full library iterations per cleanup run
- Confusing logs showing mismatched library types

**Root Cause:**
The `VirtualFolder` struct lacks the `CollectionType` field that Emby's API provides. This field indicates library content type (`"movies"`, `"tvshows"`, `"music"`, etc.).

**Constraints:**
- Must preserve existing behavior: aggregate all items before processing to minimize Trakt API calls
- Cannot change queue file formats (data persistence)
- Must handle scenarios where Radarr/Sonarr data is unavailable
- No mixed content libraries exist in our setup

## Goals / Non-Goals

**Goals:**
- Eliminate incorrect library type fetches (movies from TV libraries, series from movie libraries)
- Reduce library iterations from 2 to 1 per cleanup run
- Add type-safe dispatch based on Emby's CollectionType
- Maintain single Trakt API call per media type (aggregation optimization)
- Improve log clarity by only showing appropriate library operations

**Non-Goals:**
- Supporting mixed content libraries (CollectionType = "")
- Changing external API contracts or response formats
- Modifying queue persistence format
- Adding new configuration options

## Decisions

### Decision 1: Add CollectionType to VirtualFolder struct

**Choice:** Add `CollectionType string` field to `emby.VirtualFolder`

**Rationale:** Emby's API already provides this field. By capturing it, we get type information without additional API calls.

**Alternatives considered:**
- Query library metadata separately → Adds API calls, less efficient
- Hardcode library name patterns → Fragile, language-dependent, breaks on renames

### Decision 2: Centralized dispatch in ProcessCleanup()

**Choice:** Move library iteration from individual processors to `ProcessCleanup()`, using a type-based switch statement

**Rationale:** 
- Single source of truth for library routing
- Eliminates redundant iterations
- Clear separation: dispatch logic vs. processing logic
- Easy to extend for future media types (music, books, etc.)

**Alternatives considered:**
- Keep filtering in individual processors → Still requires passing all libraries, less efficient
- Create separate dispatcher function → Adds abstraction without benefit for current scope

### Decision 3: Aggregate-then-process pattern

**Choice:** Collect all items from appropriate libraries into slices, then process once per media type

**Current flow:**
```
for each library:
  fetch items
  append to aggregate slice

process aggregate movies (single Trakt call)
process aggregate series (single Trakt call)
```

**Rationale:** 
- Preserves existing optimization: single Trakt API call per media type
- Trakt rate limits make aggregation important
- Cleaner separation of concerns (fetch vs. process)

**Alternatives considered:**
- Process items per library → Multiple Trakt API calls, hits rate limits
- Stream processing → Complicates error handling, less readable

### Decision 4: Refactor function signatures

**Old:** `processEmbyMovies(libraries, excludedLibNames, ...)`
**New:** `processEmbyMovieItems(movies []emby.Item, ...)`

**Rationale:**
- Processors become pure functions operating on data
- No library iteration responsibility
- Easier to test (pass mock item slices)
- Clear contract: "process these items"

### Decision 5: Nil checks at dispatch level

**Choice:** Check for nil Radarr/Sonarr data before fetching from libraries

**Rationale:**
- Fail fast - don't fetch items if we can't determine orphan status
- Clearer logs - warn per library instead of general warning
- Prevents false orphan detection

## Risks / Trade-offs

### Risk: Mixed content libraries not supported
**Impact:** If users have mixed libraries (CollectionType = ""), items won't be processed

**Mitigation:** Current setup has no mixed libraries. If needed later, add case "" that fetches both types. Log with DEBUG level to avoid noise.

### Risk: Emby API not returning CollectionType
**Impact:** All libraries would show empty CollectionType, dispatch would skip them

**Mitigation:** Add fallback behavior: if CollectionType is empty for ALL libraries, log warning and fall back to current behavior (fetch from all libraries)

### Trade-off: Less flexible library filtering
**Before:** Each processor independently decides what to fetch

**After:** Central dispatcher controls routing

**Rationale:** Centralization is correct here - library type is an inherent property, not a per-processor decision

### Trade-off: More code in ProcessCleanup()
**Impact:** ProcessCleanup() function grows from ~50 lines to ~90 lines

**Mitigation:** Acceptable - the logic is straightforward and keeps related concerns together. Alternative (separate dispatcher function) adds abstraction without clarity benefit.

## Migration Plan

**Deployment Steps:**
1. Deploy changes (no configuration or data format changes)
2. Monitor first cleanup run logs for:
   - Libraries being correctly routed by type
   - No "Failed to get movies/series" errors for type mismatches
   - Same number of items processed as before

**Rollback Strategy:**
- Git revert (no data migration needed)
- No queue format changes, so rollback is safe

**Validation:**
- Check logs for library type routing messages
- Verify no movies fetched from TV libraries, no series from movie libraries
- Confirm item counts match previous runs (no lost content)

## Open Questions

None - design validated through brainstorming session.
