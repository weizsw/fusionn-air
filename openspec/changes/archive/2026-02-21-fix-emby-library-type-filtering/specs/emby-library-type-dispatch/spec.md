# emby-library-type-dispatch Specification

## Purpose
Defines type-aware library processing that identifies Emby library content types and routes fetching to appropriate handlers based on CollectionType.

## Requirements

### Requirement: VirtualFolder includes CollectionType
The Emby VirtualFolder data structure SHALL include a `CollectionType` field that captures the library type from Emby's API response.

#### Scenario: Movie library has CollectionType movies
- **WHEN** Emby's `/Library/VirtualFolders` API returns a library with `CollectionType: "movies"`
- **THEN** the VirtualFolder struct SHALL capture and expose this value as `CollectionType: "movies"`

#### Scenario: TV show library has CollectionType tvshows
- **WHEN** Emby's `/Library/VirtualFolders` API returns a library with `CollectionType: "tvshows"`
- **THEN** the VirtualFolder struct SHALL capture and expose this value as `CollectionType: "tvshows"`

#### Scenario: Other library types are captured
- **WHEN** Emby's `/Library/VirtualFolders` API returns a library with `CollectionType` of `"music"`, `"books"`, or any other value
- **THEN** the VirtualFolder struct SHALL capture and expose the value unchanged

#### Scenario: Empty CollectionType for mixed libraries
- **WHEN** Emby's `/Library/VirtualFolders` API returns a library with empty or missing `CollectionType`
- **THEN** the VirtualFolder struct SHALL have an empty string for `CollectionType`

### Requirement: Centralized library iteration with type-based dispatch
The cleanup process SHALL iterate through Emby libraries once and dispatch to appropriate fetchers based on the library's CollectionType.

#### Scenario: Movie library dispatches to movie fetcher
- **WHEN** processing a library with `CollectionType: "movies"`
- **THEN** the system SHALL call the Emby client's `GetMovies()` method with that library's ItemID
- **AND** the system SHALL NOT call `GetSeries()` for that library

#### Scenario: TV show library dispatches to series fetcher
- **WHEN** processing a library with `CollectionType: "tvshows"`
- **THEN** the system SHALL call the Emby client's `GetSeries()` method with that library's ItemID
- **AND** the system SHALL NOT call `GetMovies()` for that library

#### Scenario: Unsupported library types are skipped
- **WHEN** processing a library with `CollectionType: "music"` or any other non-media type
- **THEN** the system SHALL skip the library with a debug-level log message
- **AND** the system SHALL NOT attempt to fetch movies or series from that library

#### Scenario: Empty CollectionType is skipped
- **WHEN** processing a library with empty `CollectionType`
- **THEN** the system SHALL skip the library with a debug-level log message indicating mixed content is not supported

### Requirement: Type check occurs before fetching
The system SHALL check library CollectionType and skip mismatched types before making any Emby API calls to fetch items.

#### Scenario: No API call for type mismatch
- **WHEN** evaluating a library with `CollectionType: "tvshows"` during movie processing
- **THEN** the system SHALL skip that library without calling `GetMovies()`
- **AND** no Emby API request SHALL be made for that library

#### Scenario: Excluded libraries skip type checking
- **WHEN** a library name is in the `excluded_libraries` configuration
- **THEN** the system SHALL skip that library before checking its CollectionType
- **AND** no type-based dispatch SHALL occur for that library

### Requirement: Aggregation before processing
The system SHALL aggregate all items of a given type from all appropriate libraries before processing them as a batch.

#### Scenario: Multiple movie libraries aggregate into one slice
- **WHEN** there are three libraries with `CollectionType: "movies"`
- **THEN** the system SHALL fetch movies from each library separately
- **AND** append all movies to a single aggregate slice
- **AND** process all movies together in one batch

#### Scenario: Multiple TV show libraries aggregate into one slice
- **WHEN** there are two libraries with `CollectionType: "tvshows"`
- **THEN** the system SHALL fetch series from each library separately
- **AND** append all series to a single aggregate slice
- **AND** process all series together in one batch

#### Scenario: Empty fetch results do not break aggregation
- **WHEN** a movie library returns zero movies
- **THEN** the system SHALL log the empty result
- **AND** continue processing other movie libraries
- **AND** include the empty result in the aggregate count

### Requirement: Nil check for manager data at dispatch level
The system SHALL check for nil Radarr/Sonarr data before dispatching to fetchers for each library.

#### Scenario: Skip movie library when Radarr data unavailable
- **WHEN** processing a library with `CollectionType: "movies"`
- **AND** Radarr data is nil (Radarr fetch failed)
- **THEN** the system SHALL skip that library with a warning log
- **AND** the system SHALL NOT call `GetMovies()` for that library

#### Scenario: Skip TV show library when Sonarr data unavailable
- **WHEN** processing a library with `CollectionType: "tvshows"`
- **AND** Sonarr data is nil (Sonarr fetch failed)
- **THEN** the system SHALL skip that library with a warning log
- **AND** the system SHALL NOT call `GetSeries()` for that library

#### Scenario: Process continues for available manager data
- **WHEN** Radarr data is nil but Sonarr data is available
- **THEN** movie libraries SHALL be skipped with warnings
- **AND** TV show libraries SHALL be processed normally

### Requirement: Single Trakt API call per media type
The system SHALL maintain the optimization of fetching Trakt watch data once per media type, regardless of how many libraries are processed.

#### Scenario: Multiple movie libraries result in one Trakt movies call
- **WHEN** processing three movie libraries
- **THEN** items SHALL be aggregated from all three libraries
- **AND** the system SHALL make exactly one call to Trakt's `GetWatchedMovies()`
- **AND** all aggregated movies SHALL be checked against that single result

#### Scenario: Multiple TV show libraries result in one Trakt shows call
- **WHEN** processing two TV show libraries
- **THEN** items SHALL be aggregated from both libraries
- **AND** the system SHALL make exactly one call to Trakt's `GetWatchedShows()`
- **AND** all aggregated series SHALL be checked against that single result

### Requirement: Clear logging for library type routing
The system SHALL log library processing decisions clearly, indicating library name, type, and item counts.

#### Scenario: Log movie library fetch with count
- **WHEN** successfully fetching from a movie library named "电影"
- **THEN** the system SHALL log "🎬 Found N movies in library \"电影\""

#### Scenario: Log TV show library fetch with count
- **WHEN** successfully fetching from a TV show library named "电视节目"
- **THEN** the system SHALL log "📺 Found N series in library \"电视节目\""

#### Scenario: Log excluded library skip
- **WHEN** skipping a library in the exclusion list
- **THEN** the system SHALL log "📚 Skipping excluded library \"NAME\" (ID: ID)"

#### Scenario: Log type mismatch skip at debug level
- **WHEN** skipping a library due to unsupported CollectionType
- **THEN** the system SHALL log at DEBUG level "📚 Skipping library \"NAME\" (unsupported type: TYPE)"

#### Scenario: Log manager data unavailable warning
- **WHEN** skipping a movie library because Radarr data is nil
- **THEN** the system SHALL log at WARN level "⚠️ Skipping movie library \"NAME\" - Radarr data unavailable"
