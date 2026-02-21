# emby-cleanup Delta Specification

## MODIFIED Requirements

### Requirement: Emby client fetches library list
The Emby client SHALL support fetching the list of libraries from the Emby server via the `/Library/VirtualFolders` API endpoint, including the `CollectionType` field for each library.

#### Scenario: Fetch libraries successfully with CollectionType
- **WHEN** the client calls `GetLibraries()`
- **THEN** it SHALL return a list of libraries with their name, item ID, and CollectionType

#### Scenario: Library fetch fails
- **WHEN** the `/Library/VirtualFolders` API call fails
- **THEN** the system SHALL log a warning and proceed without library filtering (all items processed)

### Requirement: Library filtering occurs before orphan detection
Items SHALL be fetched from libraries based on CollectionType matching. The system SHALL query each non-excluded library with matching CollectionType using the `ParentId` parameter.

#### Scenario: Only movie libraries are queried for movie cleanup
- **WHEN** processing Emby movies
- **AND** the Emby server has libraries "Movies" (CollectionType: "movies"), "TV Shows" (CollectionType: "tvshows"), and "Anime" (CollectionType: "movies")
- **THEN** the system SHALL make API requests with `ParentId` for "Movies" and "Anime" only
- **AND** the system SHALL NOT make any movie API request for the "TV Shows" library

#### Scenario: Only TV show libraries are queried for series cleanup
- **WHEN** processing Emby series
- **AND** the Emby server has libraries "Movies" (CollectionType: "movies") and "TV Shows" (CollectionType: "tvshows")
- **THEN** the system SHALL make API requests with `ParentId` for "TV Shows" only
- **AND** the system SHALL NOT make any series API request for the "Movies" library

#### Scenario: Excluded libraries are skipped before type checking
- **WHEN** `emby.excluded_libraries` contains `["Anime"]`
- **AND** "Anime" is a movie library
- **THEN** the system SHALL skip the library before checking CollectionType
- **AND** the system SHALL NOT make any API request for "Anime"

#### Scenario: Libraries with unsupported CollectionType are skipped
- **WHEN** processing a library with `CollectionType: "music"`
- **THEN** the system SHALL skip the library with a debug log
- **AND** no orphan detection SHALL occur for that library

#### Scenario: Empty CollectionType libraries are skipped
- **WHEN** processing a library with empty or missing CollectionType
- **THEN** the system SHALL skip the library with a debug log indicating mixed content is not supported

#### Scenario: All appropriate libraries processed when no exclusions configured
- **WHEN** `emby.excluded_libraries` is empty or not set
- **THEN** the system SHALL query each library matching the current media type (movies or tvshows)
- **AND** all matching Emby items across matching libraries SHALL be processed
