## 1. Config — Emby Configuration

- [x] 1.1 Add `EmbyConfig` struct with `Enabled`, `BaseURL`, `APIKey` fields in `internal/config/config.go`
- [x] 1.2 Add `Emby EmbyConfig` field to the top-level `Config` struct
- [x] 1.3 Document the new `emby` section in `config/config.example.yaml` with comments and examples

## 2. Emby Client — Core

- [x] 2.1 Create `internal/client/emby/types.go` with Emby response types: `EmbyItemsResponse`, `EmbyItem`, `ProviderIds`, `EmbySeason`, `EmbyEpisode`
- [x] 2.2 Create `internal/client/emby/client.go` with `NewClient` constructor using resty, `api_key` query parameter auth
- [x] 2.3 Implement `GetAllSeries(ctx) ([]EmbyItem, error)` — fetches all series with ProviderIds and Path fields
- [x] 2.4 Implement `GetAllMovies(ctx) ([]EmbyItem, error)` — fetches all movies with ProviderIds and Path fields
- [x] 2.5 Implement `GetSeasons(ctx, seriesID) ([]EmbySeason, error)` — fetches seasons for a series
- [x] 2.6 Implement `GetEpisodes(ctx, seriesID, seasonID) ([]EmbyEpisode, error)` — fetches episodes for a season
- [x] 2.7 Implement `DeleteItem(ctx, itemID) error` — deletes an item and its files
- [x] 2.8 Add helper `ParseProviderID(providerIds, key) int` to convert string IDs to int

## 3. Cleanup Service — Emby Orphan Series

- [x] 3.1 Add `emby *emby.Client` field to the cleanup `Service` struct and update `NewService` constructor
- [x] 3.2 Register Emby queue files: `data/cleanup_emby_series_queue.json` and `data/cleanup_emby_movie_queue.json` with new `MediaType` constants
- [x] 3.3 Implement `processEmbySeries(ctx, result, cfg, dryRun, sonarrTvdbIDs)` — fetches Emby series, filters orphans, checks watch progress
- [x] 3.4 Implement `processOneEmbySeries(ctx, item, watchedByTvdb, queue, cfg)` — per-series logic: exclusion check, episode-level watch checking via Emby seasons/episodes API, queue if fully watched
- [x] 3.5 Implement `checkEmbyWatchedOnDisk(ctx, embyClient, seriesID, progress)` — get seasons and episodes from Emby, compare against Trakt progress per season
- [x] 3.6 Implement `processEmbySeriesRemovalQueue(ctx, result, queue, cfg, dryRun)` — delete ready items via Emby

## 4. Cleanup Service — Emby Orphan Movies

- [x] 4.1 Implement `processEmbyMovies(ctx, result, cfg, dryRun, radarrTmdbIDs)` — fetches Emby movies, filters orphans, checks Trakt watched
- [x] 4.2 Implement `processOneEmbyMovie(item, watchedByTmdb, queue, cfg)` — per-movie logic: exclusion check, watched check, queue
- [x] 4.3 Implement `processEmbyMovieRemovalQueue(ctx, result, queue, cfg, dryRun)` — delete ready items via Emby

## 5. Cleanup Orchestrator — Integration

- [x] 5.1 Update `processSeries` to return the set of TVDB IDs from Sonarr (for orphan exclusion)
- [x] 5.2 Update `processMovies` to return the set of TMDB IDs from Radarr (for orphan exclusion)
- [x] 5.3 Update `ProcessCleanup` to call `processEmbySeries` and `processEmbyMovies` after Sonarr/Radarr, passing the exclusion ID sets
- [x] 5.4 Guard Emby cleanup: skip if Emby client is nil or not enabled; skip if Sonarr/Radarr data fetch failed

## 6. Wiring — main.go

- [x] 6.1 Initialize Emby client in `main.go` when `emby.enabled` is true
- [x] 6.2 Pass Emby client to cleanup `NewService`

## 7. Logging and Notifications

- [x] 7.1 Include Emby orphan results in cleanup summary (log output) with source indication
- [x] 7.2 Include Emby orphan results in Apprise notifications
