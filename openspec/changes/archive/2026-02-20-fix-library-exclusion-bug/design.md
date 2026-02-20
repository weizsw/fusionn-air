## Context

The library exclusion feature was implemented to allow users to exclude entire Emby libraries from cleanup processing. The implementation fetches all items recursively via `/Items?Recursive=true` and then attempts to filter them client-side by checking if `item.ParentId` matches an excluded library's `ItemId`.

**The Problem**: Through API testing, we discovered that:
1. A movie's `ParentId` points to its immediate parent folder (e.g., "/Movies/2012 (2009)/"), not the library root
2. The `/Items/{Id}/Ancestors` endpoint returns folder ancestors but they don't include the library ID
3. No direct field on items identifies which library they belong to

**The Solution**: The Emby API documentation and common practice in other Emby tools (per research) shows the correct approach is to use the `ParentId` query parameter to fetch items from specific libraries. Instead of fetching everything and filtering, we query each non-excluded library separately.

**Constraints**:
- Must maintain backward compatibility with existing exclusion configuration
- Must preserve existing logging behavior (log when libraries are excluded)
- Should minimize API calls where practical

## Goals / Non-Goals

**Goals:**
- Fix the library exclusion bug so excluded libraries' items are never fetched
- Use server-side filtering via `ParentId` parameter (Emby's intended API usage)
- Maintain clear logging of which libraries are excluded vs processed
- Keep the code simple and aligned with Emby API best practices

**Non-Goals:**
- Optimizing to reduce the number of API calls (N calls for N libraries is acceptable and standard)
- Adding library-level configuration beyond exclusion (e.g., per-library delay times)
- Supporting include-list semantics (only exclusion is supported)
- Changing the exclusion configuration format

## Decisions

### Decision 1: Server-side filtering via ParentId parameter

**Chosen Approach**: Query each library separately using `/Items?ParentId={LibraryId}&Recursive=true&IncludeItemTypes=Movie`

**Why**: 
- This is the documented Emby API approach for scoping queries to specific libraries
- Other Emby automation tools use this pattern
- Server does the filtering, eliminating client-side complexity
- Only fetches data we actually need

**Alternatives Considered**:
1. **Walk ancestor chain for each item** (fetch item → fetch parent → fetch parent's parent → ...) 
   - ❌ Rejected: Requires O(depth * items) API calls, very inefficient
   - ❌ Complex to implement and maintain
   
2. **Use `/Items/{Id}/Ancestors` and check all ancestors**
   - ❌ Rejected: Ancestors don't go all the way to library root in testing
   - ❌ Still requires fetching all items first, then making additional calls per item

3. **Add a new field like `TopParentId` to items**
   - ❌ Rejected: Field doesn't exist in Emby API, would require Emby server changes

### Decision 2: Iterate libraries in cleanup service, not Emby client

**Chosen Approach**: The cleanup service (`processEmbyMovies`, `processEmbySeries`) will:
1. Get the list of libraries
2. Filter to non-excluded libraries
3. For each library, call `emby.GetMovies(ctx, libraryID)` 
4. Aggregate results

**Why**:
- Keeps library filtering logic in the cleanup service where it belongs
- Emby client methods stay simple and reusable
- Clear separation of concerns: client = API wrapper, service = business logic

**Alternative Considered**:
- Have `GetAllMovies()` accept `excludedLibraryIds` and handle iteration internally
  - ❌ Rejected: Mixes business logic into client layer
  - ❌ Reduces reusability of client methods

### Decision 3: Update GetMovies/GetSeries to accept optional ParentId

**Chosen Approach**: 
```go
// Before
GetAllMovies(ctx) ([]Item, error)

// After  
GetMovies(ctx context.Context, parentID string) ([]Item, error)
GetAllMovies(ctx) ([]Item, error) // Calls GetMovies(ctx, "")
```

Add a new `GetMovies(ctx, parentID)` method that accepts an optional parent ID. Keep `GetAllMovies()` as a wrapper that calls `GetMovies(ctx, "")` for backward compatibility if needed elsewhere.

**Why**:
- Clean API that explicitly shows intent
- Empty string = root (all items), non-empty = specific library
- Maintains backward compatibility

**Alternative Considered**:
- Use variadic parameters or options struct
  - ❌ Rejected: Overkill for a single parameter
  - Simple positional parameter is clearer

## Risks / Trade-offs

**[Risk]** Slightly more API calls (N calls for N non-excluded libraries instead of 1)  
→ **Mitigation**: This is acceptable and standard for Emby API usage. The reduced payload size (not fetching excluded libraries' items) likely offsets the overhead of additional HTTP requests. For most users with 1-3 excluded libraries, the impact is minimal.

**[Risk]** If a user has many libraries and only excludes 1-2, we'll make more calls than before  
→ **Mitigation**: This is the intended Emby API usage pattern. The previous approach was buggy and didn't work anyway. We could add future optimization to detect "include mode" (if excluded > included, query excluded and subtract), but YAGNI for now.

**[Risk]** If Emby's `/Library/VirtualFolders` API fails, we can't determine which libraries exist  
→ **Mitigation**: Already handled in existing code - logs warning and proceeds without filtering (fetches from root). This behavior is preserved.

**[Trade-off]** More explicit API calls vs. "fetch everything and filter"  
→ **Accepted**: Explicitness is good. Server-side filtering is correct. The previous approach was a premature optimization that didn't work.
