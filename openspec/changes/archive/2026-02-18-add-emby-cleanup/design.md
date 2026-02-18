## Context

The cleanup service currently handles two media sources: Sonarr (series) and Radarr (movies). Each source has a dedicated client, processing logic, and file-backed queue. Content added through MoviePilot bypasses both and is invisible to cleanup.

Emby is the media server that aggregates all content. Its API returns items with provider IDs (`Tvdb`, `Tmdb`) and supports deletion. By fetching all media from Emby and subtracting what Sonarr/Radarr manage, we can identify "orphan" items — content only MoviePilot knows about — and clean them up.

Emby API details (confirmed via testing):
- `GET /emby/Items?IncludeItemTypes=Series&Recursive=true&Fields=ProviderIds,Path` — lists all series with TVDB/TMDB IDs
- `GET /emby/Shows/{SeriesId}/Seasons` — lists seasons for a series
- `GET /emby/Shows/{SeriesId}/Episodes?SeasonId={SeasonId}` — lists episodes per season (with `HasFile` field)
- `GET /emby/Items?IncludeItemTypes=Movie&Recursive=true&Fields=ProviderIds,Path` — lists all movies
- `DELETE /emby/Items/{Id}` — deletes an item and its files from disk
- Authentication: `api_key` query parameter on all requests
- Provider IDs are strings (e.g., `"Tvdb": "393189"`) — need conversion to int for matching

## Goals / Non-Goals

**Goals:**
- Clean up MoviePilot-managed content (anime, K-dramas, J-dramas, C-dramas) that Sonarr/Radarr don't track
- Same level of accuracy as existing cleanup: per-season episode-level watch checking for series
- Reuse existing patterns: queue with delay, exclusion list, dry run, notifications
- No changes to existing Sonarr/Radarr cleanup behavior

**Non-Goals:**
- Replacing Sonarr/Radarr cleanup with Emby (they coexist)
- Unmonitoring in MoviePilot before deletion (MoviePilot has no equivalent concept)
- Cleaning up content that exists in both Sonarr/Radarr and Emby (those are handled by existing cleanup)

## Decisions

### 1. Orphan detection via set subtraction

**Choice:** Fetch all series/movies from Emby, build a set of TVDB IDs (series) and TMDB IDs (movies) from Sonarr/Radarr, then process only Emby items NOT in those sets.

**Alternatives considered:**
- Tag-based: rely on Emby tags or library separation — rejected because it requires manual user setup and is fragile
- Path-based: match by download directory — rejected because it hardcodes directory assumptions

**Trade-off:** Requires fetching data from both Sonarr/Radarr AND Emby each cleanup run. Acceptable because we already fetch Sonarr/Radarr data, and the Emby call is one additional request.

### 2. Reuse existing queue infrastructure with separate queue files

**Choice:** Create new `Queue` instances for Emby series and movies with dedicated files: `data/cleanup_emby_series_queue.json` and `data/cleanup_emby_movie_queue.json`.

**Rationale:** Emby items use Emby internal IDs (not Sonarr/Radarr IDs), so they must be in separate queues to avoid ID collisions. The existing `Queue` struct works as-is.

### 3. Episode-level watch checking via Emby seasons/episodes API

**Choice:** For each orphan series, query Emby for seasons and count episodes with files per season. Compare against Trakt progress per season (same logic as `checkWatchedOnDisk`).

**Alternative considered:** Simpler "is the whole show watched" check — rejected because user requested same accuracy as Sonarr cleanup.

**Trade-off:** More API calls per orphan series (1 for seasons + 1 per season for episodes). Acceptable because orphan count is typically small (anime/drama catalog).

### 4. Matching by TVDB ID for series, TMDB ID for movies

**Choice:** Same ID types used by existing Sonarr/Radarr cleanup. Emby returns both in `ProviderIds`.

**Caveat:** Emby returns IDs as strings — need `strconv.Atoi` conversion. Items with missing or zero provider IDs are skipped.

### 5. No unmonitor step for Emby items

**Choice:** Unlike Sonarr/Radarr cleanup which unmonitors before deleting, Emby items go directly from queued to deleted. MoviePilot doesn't have an unmonitor concept exposed through the servarr API that we'd need.

## Risks / Trade-offs

- **[Emby API latency]** Emby may be slower than Sonarr/Radarr for large libraries → Mitigation: Emby call is paginated if needed; typical home libraries are small enough for a single request.
- **[Provider ID gaps]** Some Emby items may lack TVDB/TMDB IDs → Mitigation: skip items without provider IDs, log a warning. These can't be matched against Trakt anyway.
- **[Orphan false positives]** If Sonarr/Radarr is temporarily unreachable, all Emby items appear as orphans → Mitigation: if Sonarr/Radarr fetch fails, skip Emby cleanup entirely for that run. Only proceed when we have a reliable exclusion set.
- **[Queue ID namespace]** Emby IDs are different from Sonarr/Radarr IDs → Mitigation: separate queue files prevent any collision.
