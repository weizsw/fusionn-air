## ADDED Requirements

### Requirement: Emby configuration
The system SHALL support an optional `emby` configuration block with `enabled` (bool), `base_url` (string), and `api_key` (string) fields. Emby cleanup SHALL only run when `emby.enabled` is true and both `base_url` and `api_key` are non-empty.

#### Scenario: Emby is configured and enabled
- **WHEN** the config contains `emby.enabled: true` with valid `base_url` and `api_key`
- **THEN** the cleanup service SHALL include Emby orphan processing

#### Scenario: Emby is not configured
- **WHEN** the config does not contain an `emby` block or `emby.enabled` is false
- **THEN** the cleanup service SHALL skip Emby processing entirely and existing Sonarr/Radarr cleanup SHALL be unaffected

### Requirement: Emby client lists all series with provider IDs
The Emby client SHALL fetch all series from Emby with `ProviderIds` and `Path` fields included.

#### Scenario: Fetch all series
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Series`
- **THEN** it SHALL return all series with their Emby ID, name, provider IDs (Tvdb, Tmdb), and path

#### Scenario: Provider IDs are strings
- **WHEN** Emby returns provider IDs as strings (e.g., `"Tvdb": "393189"`)
- **THEN** the client SHALL convert them to integers for downstream matching

### Requirement: Emby client lists all movies with provider IDs
The Emby client SHALL fetch all movies from Emby with `ProviderIds` and `Path` fields included.

#### Scenario: Fetch all movies
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Movie`
- **THEN** it SHALL return all movies with their Emby ID, name, provider IDs (Tmdb, Imdb), and path

### Requirement: Emby client lists seasons and episodes for a series
The Emby client SHALL support fetching seasons for a series and episodes for a season, including whether each episode has a file on disk.

#### Scenario: Fetch seasons for a series
- **WHEN** the client requests seasons for a given Emby series ID
- **THEN** it SHALL return all seasons with their Emby ID and season number (IndexNumber)

#### Scenario: Fetch episodes for a season
- **WHEN** the client requests episodes for a given Emby series ID and season ID
- **THEN** it SHALL return all episodes with their episode number and whether they have a file (LocationType or similar)

### Requirement: Emby client deletes items
The Emby client SHALL support deleting an item by its Emby internal ID, which removes the item and its files from disk.

#### Scenario: Delete a series
- **WHEN** `DeleteItem` is called with a valid Emby item ID
- **THEN** the item and its associated files SHALL be removed from Emby and disk

#### Scenario: Delete a movie
- **WHEN** `DeleteItem` is called with a valid Emby movie item ID
- **THEN** the movie and its file SHALL be removed from Emby and disk

### Requirement: Orphan detection by set subtraction
The cleanup service SHALL identify orphan items by fetching all series/movies from Emby and excluding any that exist in Sonarr (by TVDB ID) or Radarr (by TMDB ID).

#### Scenario: Series exists in both Emby and Sonarr
- **WHEN** an Emby series has a TVDB ID that matches a Sonarr series
- **THEN** it SHALL be excluded from Emby cleanup (Sonarr handles it)

#### Scenario: Series exists in Emby but not Sonarr
- **WHEN** an Emby series has a TVDB ID that does NOT match any Sonarr series
- **THEN** it SHALL be processed as an orphan for Emby cleanup

#### Scenario: Movie exists in both Emby and Radarr
- **WHEN** an Emby movie has a TMDB ID that matches a Radarr movie
- **THEN** it SHALL be excluded from Emby cleanup (Radarr handles it)

#### Scenario: Movie exists in Emby but not Radarr
- **WHEN** an Emby movie has a TMDB ID that does NOT match any Radarr movie
- **THEN** it SHALL be processed as an orphan for Emby cleanup

#### Scenario: Emby item has no provider ID
- **WHEN** an Emby item lacks a TVDB ID (series) or TMDB ID (movie)
- **THEN** it SHALL be skipped with a warning log

#### Scenario: Sonarr or Radarr is unreachable
- **WHEN** the Sonarr or Radarr fetch fails during cleanup
- **THEN** the Emby orphan cleanup for that media type SHALL be skipped entirely to avoid false positives

### Requirement: Orphan series episode-level watch checking
For each orphan series, the cleanup service SHALL check Trakt watch progress at the per-season, per-episode level — comparing episodes on disk (from Emby) against episodes watched (from Trakt).

#### Scenario: All episodes on disk are watched
- **WHEN** every episode with a file in Emby has been watched according to Trakt
- **AND** no more episodes are coming (series ended or season fully aired)
- **THEN** the series SHALL be queued for removal

#### Scenario: Some episodes on disk are unwatched
- **WHEN** at least one episode with a file in Emby has NOT been watched on Trakt
- **THEN** the series SHALL be skipped with a reason indicating which season is still being watched

#### Scenario: More episodes are coming
- **WHEN** all on-disk episodes are watched but the season has more episodes to air
- **THEN** the series SHALL be skipped (ongoing season)

### Requirement: Orphan movie watch checking
For each orphan movie, the cleanup service SHALL check if it has been watched on Trakt.

#### Scenario: Movie is watched
- **WHEN** the Emby movie's TMDB ID appears in the user's Trakt watched movies
- **THEN** the movie SHALL be queued for removal

#### Scenario: Movie is not watched
- **WHEN** the Emby movie's TMDB ID does NOT appear in Trakt watched movies
- **THEN** the movie SHALL be skipped with reason "not watched"

### Requirement: Emby orphan cleanup uses queue with delay
Emby orphan items SHALL use the same queue-and-delay pattern as Sonarr/Radarr cleanup, with separate queue files (`data/cleanup_emby_series_queue.json`, `data/cleanup_emby_movie_queue.json`).

#### Scenario: Item queued for first time
- **WHEN** an orphan item is fully watched and not yet queued
- **THEN** it SHALL be added to the Emby queue with the current timestamp

#### Scenario: Item ready for removal after delay
- **WHEN** a queued item has been in the queue for at least `cleanup.delay_days`
- **THEN** it SHALL be deleted from Emby via `DeleteItem`

#### Scenario: Dry run mode
- **WHEN** `scheduler.dry_run` is true
- **THEN** queuing and deletion SHALL be logged but not executed

### Requirement: Emby cleanup respects exclusion list
Emby orphan items SHALL be checked against the shared `cleanup.exclusions` list (case-insensitive title match).

#### Scenario: Orphan title is in exclusion list
- **WHEN** an orphan item's title matches an entry in `cleanup.exclusions`
- **THEN** it SHALL be skipped with reason "in exclusion list"

### Requirement: Emby cleanup results appear in summary and notifications
Emby cleanup results SHALL be included in the cleanup summary logs and Apprise notifications, using the same format as Sonarr/Radarr results.

#### Scenario: Emby series removed
- **WHEN** an Emby orphan series is removed
- **THEN** it SHALL appear in the cleanup summary under a series section with source indicated

#### Scenario: Emby movie removed
- **WHEN** an Emby orphan movie is removed
- **THEN** it SHALL appear in the cleanup summary under a movies section with source indicated
