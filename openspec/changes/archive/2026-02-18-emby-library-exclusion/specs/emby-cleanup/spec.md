## ADDED Requirements

### Requirement: Emby library exclusion configuration
The `emby` configuration block SHALL support an optional `excluded_libraries` field containing a list of Emby library names. When present, all items belonging to those libraries SHALL be excluded from Emby orphan cleanup processing.

#### Scenario: Excluded libraries configured
- **WHEN** `emby.excluded_libraries` contains `["Anime", "Kids"]`
- **THEN** all series and movies in the "Anime" and "Kids" libraries SHALL be excluded from orphan detection

#### Scenario: No excluded libraries configured
- **WHEN** `emby.excluded_libraries` is empty or not set
- **THEN** all Emby items SHALL be processed as before (no filtering)

#### Scenario: Library name matching is case-insensitive
- **WHEN** `emby.excluded_libraries` contains `"anime"`
- **AND** the Emby server has a library named `"Anime"`
- **THEN** the library SHALL be matched and excluded

#### Scenario: Configured library name does not match any Emby library
- **WHEN** `emby.excluded_libraries` contains a name that does not match any library in Emby
- **THEN** the system SHALL log a warning identifying the unmatched name
- **AND** cleanup SHALL proceed normally for all other libraries

### Requirement: Emby client fetches library list
The Emby client SHALL support fetching the list of libraries from the Emby server via the `/Library/VirtualFolders` API endpoint.

#### Scenario: Fetch libraries successfully
- **WHEN** the client calls `GetLibraries()`
- **THEN** it SHALL return a list of libraries with their name and item ID

#### Scenario: Library fetch fails
- **WHEN** the `/Library/VirtualFolders` API call fails
- **THEN** the system SHALL log a warning and proceed without library filtering (all items processed)

### Requirement: Items include parent library reference
The Emby client SHALL request the `ParentId` field when fetching series and movies, so each item can be associated with its parent library.

#### Scenario: Series items include ParentId
- **WHEN** `GetAllSeries()` is called
- **THEN** each returned item SHALL include its `ParentId` field

#### Scenario: Movie items include ParentId
- **WHEN** `GetAllMovies()` is called
- **THEN** each returned item SHALL include its `ParentId` field

### Requirement: Library filtering occurs before orphan detection
Items belonging to excluded libraries SHALL be filtered out before the orphan detection step (before Sonarr/Radarr ID subtraction). Filtered items never enter the orphan pipeline.

#### Scenario: Item in excluded library is not processed
- **WHEN** a series belongs to an excluded library
- **AND** the series is not in Sonarr
- **THEN** the series SHALL NOT appear as an orphan and SHALL NOT be checked for watch status

#### Scenario: Item in non-excluded library proceeds normally
- **WHEN** a movie belongs to a library not in the exclusion list
- **THEN** the movie SHALL proceed through the normal orphan detection and watch-checking pipeline

## MODIFIED Requirements

### Requirement: Emby configuration
The system SHALL support an optional `emby` configuration block with `enabled` (bool), `base_url` (string), `api_key` (string), and `excluded_libraries` (list of strings) fields. Emby cleanup SHALL only run when `emby.enabled` is true and both `base_url` and `api_key` are non-empty.

#### Scenario: Emby is configured and enabled
- **WHEN** the config contains `emby.enabled: true` with valid `base_url` and `api_key`
- **THEN** the cleanup service SHALL include Emby orphan processing

#### Scenario: Emby is not configured
- **WHEN** the config does not contain an `emby` block or `emby.enabled` is false
- **THEN** the cleanup service SHALL skip Emby processing entirely and existing Sonarr/Radarr cleanup SHALL be unaffected

#### Scenario: Emby is configured with excluded libraries
- **WHEN** the config contains `emby.enabled: true` with `excluded_libraries: ["Anime"]`
- **THEN** the cleanup service SHALL exclude all items in the "Anime" library from processing
