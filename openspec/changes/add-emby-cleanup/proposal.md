## Why

Content added through MoviePilot (anime, K-dramas, J-dramas, C-dramas) does not exist in Sonarr or Radarr, so the current cleanup service never sees it. These items accumulate on disk indefinitely. Emby is the single media server that contains all content regardless of source, making it the right place to discover and delete MoviePilot-managed media.

## What Changes

- Add an Emby client to query series, movies, seasons, and episodes, and to delete items
- During cleanup, fetch all series/movies from Emby and subtract those managed by Sonarr/Radarr (by TVDB/TMDB ID) to identify "orphan" items — content that only MoviePilot knows about
- For orphan series: check Trakt watch progress at the per-season/per-episode level (same accuracy as Sonarr cleanup), then queue and delete via Emby
- For orphan movies: check Trakt watched status, then queue and delete via Emby
- Add Emby configuration (base URL, API key) to `config.yaml`
- Separate queue files for Emby-managed series and movies

## Capabilities

### New Capabilities
- `emby-cleanup`: Discovery and deletion of non-Sonarr/non-Radarr media through Emby, with per-episode watch progress checking via Trakt

### Modified Capabilities
- `cleanup`: The cleanup orchestrator gains a new processing step for Emby orphans, running after the existing Sonarr and Radarr cleanup steps

## Impact

- **New client**: `internal/client/emby/` — Emby API client for listing items, seasons, episodes, and deleting items
- **Config**: New `emby` section in config with `base_url`, `api_key`, and `enabled` toggle
- **Cleanup service**: New `processEmby*` methods for series and movies, new queue files (`data/cleanup_emby_series_queue.json`, `data/cleanup_emby_movie_queue.json`)
- **Cleanup orchestrator**: Modified to call Emby cleanup after Sonarr/Radarr cleanup, passing Sonarr/Radarr ID sets for exclusion
- **Dependencies**: No new external Go dependencies — Emby API uses simple REST with the existing resty client
