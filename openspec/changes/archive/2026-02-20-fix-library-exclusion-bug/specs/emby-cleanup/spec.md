## MODIFIED Requirements

### Requirement: Emby client lists all series with provider IDs
The Emby client SHALL fetch series from Emby with `ProviderIds` and `Path` fields included. The client SHALL support fetching all series recursively, or series from a specific library by `ParentId`.

#### Scenario: Fetch all series recursively
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Series&Recursive=true` and no ParentId
- **THEN** it SHALL return all series across all libraries with their Emby ID, name, provider IDs (Tvdb, Tmdb), and path

#### Scenario: Fetch series from a specific library
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Series&Recursive=true&ParentId={LibraryId}`
- **THEN** it SHALL return only series within that library with their Emby ID, name, provider IDs (Tvdb, Tmdb), and path

#### Scenario: Provider IDs are strings
- **WHEN** Emby returns provider IDs as strings (e.g., `"Tvdb": "393189"`)
- **THEN** the client SHALL convert them to integers for downstream matching

### Requirement: Emby client lists all movies with provider IDs
The Emby client SHALL fetch movies from Emby with `ProviderIds` and `Path` fields included. The client SHALL support fetching all movies recursively, or movies from a specific library by `ParentId`.

#### Scenario: Fetch all movies recursively
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Movie&Recursive=true` and no ParentId
- **THEN** it SHALL return all movies across all libraries with their Emby ID, name, provider IDs (Tmdb, Imdb), and path

#### Scenario: Fetch movies from a specific library
- **WHEN** the Emby client calls the Items API with `IncludeItemTypes=Movie&Recursive=true&ParentId={LibraryId}`
- **THEN** it SHALL return only movies within that library with their Emby ID, name, provider IDs (Tmdb, Imdb), and path

### Requirement: Library filtering occurs before orphan detection
Items from excluded libraries SHALL NOT be fetched from the Emby API. The system SHALL query each non-excluded library separately using the `ParentId` parameter, rather than fetching all items and filtering client-side.

#### Scenario: Only non-excluded libraries are queried
- **WHEN** `emby.excluded_libraries` contains `["Anime"]`
- **AND** the Emby server has libraries "Movies", "TV Shows", and "Anime"
- **THEN** the system SHALL make separate API requests with `ParentId` for "Movies" and "TV Shows" only
- **AND** the system SHALL NOT make any API request for the "Anime" library

#### Scenario: Item in excluded library is not fetched
- **WHEN** a movie belongs to an excluded library
- **THEN** the movie SHALL NOT be returned from the Emby API
- **AND** the movie SHALL NOT appear in orphan detection
- **AND** the movie SHALL NOT be checked for watch status

#### Scenario: Item in non-excluded library proceeds normally
- **WHEN** a movie belongs to a library not in the exclusion list
- **THEN** the movie SHALL be fetched via the library-scoped API call
- **AND** the movie SHALL proceed through the normal orphan detection and watch-checking pipeline

#### Scenario: All libraries processed when no exclusions configured
- **WHEN** `emby.excluded_libraries` is empty or not set
- **THEN** the system SHALL query each library separately using their respective `ParentId` values
- **AND** all Emby items across all libraries SHALL be processed

## REMOVED Requirements

### Requirement: Items include parent library reference
**Reason**: The `ParentId` field on items does not reliably identify which library an item belongs to (it points to immediate parent folders, not library roots). Library filtering is now achieved by scoping API queries with the `ParentId` parameter instead of inspecting item fields.

**Migration**: Replace client-side filtering logic with server-side query scoping. Use the `/Items` endpoint with `ParentId={LibraryId}` parameter to fetch items from specific libraries.
