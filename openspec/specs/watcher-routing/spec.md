# watcher-routing Specification

## Purpose
Defines genre/country-based routing for Trakt watcher TV requests, allowing shows to be directed to different Overseerr server IDs based on configurable criteria.

## Requirements
### Requirement: Trakt calendar returns genre and country metadata
The Trakt calendar API call SHALL use `?extended=full` to include `genres` and `country` fields in the show data for each calendar item.

#### Scenario: Calendar show includes genre and country
- **WHEN** the watcher fetches the Trakt calendar
- **THEN** each show in the response SHALL have `genres` (string array) and `country` (string) fields populated

#### Scenario: Show has no genre or country data
- **WHEN** Trakt returns a show with empty `genres` or empty `country`
- **THEN** the watcher SHALL treat the show as a default-route show (no alternate routing)

### Requirement: Routing configuration
The watcher config SHALL support an optional `routing` block with the following fields:
- `default_server_id` (int): Overseerr server ID for regular TV shows
- `alternate_server_id` (int): Overseerr server ID for shows matching alternate criteria
- `alternate_genres` (string array): Genre names that trigger alternate routing (case-insensitive match)
- `alternate_countries` (string array): Country codes that trigger alternate routing (case-insensitive match)

#### Scenario: Routing block is configured
- **WHEN** the config contains a `watcher.routing` block with all four fields
- **THEN** the watcher SHALL use the routing rules to determine which server ID to pass to Overseerr for each request

#### Scenario: Routing block is absent
- **WHEN** the config does not contain a `watcher.routing` block
- **THEN** the watcher SHALL omit the `serverId` field from Overseerr requests, preserving current default behavior

#### Scenario: Routing config is hot-reloaded
- **WHEN** the user edits the `watcher.routing` config while the service is running
- **THEN** the next watcher run SHALL use the updated routing rules without requiring a restart

### Requirement: Genre-based routing to alternate server
The watcher SHALL route a TV request to the alternate server when any of the show's genres match an entry in `alternate_genres`.

#### Scenario: Show genre matches alternate list
- **WHEN** a show has genres `["anime", "action"]` and `alternate_genres` contains `"anime"`
- **THEN** the request SHALL be sent to Overseerr with `serverId` set to `alternate_server_id`

#### Scenario: Show genre does not match
- **WHEN** a show has genres `["drama", "thriller"]` and `alternate_genres` contains `"anime"`
- **THEN** the request SHALL be sent to Overseerr with `serverId` set to `default_server_id`

#### Scenario: Genre matching is case-insensitive
- **WHEN** a show has genres `["Anime"]` and `alternate_genres` contains `"anime"`
- **THEN** the show SHALL match and route to the alternate server

### Requirement: Country-based routing to alternate server
The watcher SHALL route a TV request to the alternate server when the show's country matches an entry in `alternate_countries`.

#### Scenario: Show country matches alternate list
- **WHEN** a show has country `"kr"` and `alternate_countries` contains `"kr"`
- **THEN** the request SHALL be sent to Overseerr with `serverId` set to `alternate_server_id`

#### Scenario: Show country does not match
- **WHEN** a show has country `"us"` and `alternate_countries` contains `["jp", "kr", "cn"]`
- **THEN** the request SHALL be sent to Overseerr with `serverId` set to `default_server_id`

#### Scenario: Country matching is case-insensitive
- **WHEN** a show has country `"JP"` and `alternate_countries` contains `"jp"`
- **THEN** the show SHALL match and route to the alternate server

### Requirement: Genre takes precedence over country (OR logic)
The watcher SHALL route to the alternate server if EITHER genre OR country matches. Both conditions do not need to be true simultaneously.

#### Scenario: Genre matches but country does not
- **WHEN** a show has genres `["anime"]` and country `"us"`, with `alternate_genres: ["anime"]`
- **THEN** the request SHALL route to the alternate server (genre match is sufficient)

#### Scenario: Country matches but genre does not
- **WHEN** a show has genres `["drama"]` and country `"kr"`, with `alternate_countries: ["kr"]`
- **THEN** the request SHALL route to the alternate server (country match is sufficient)

#### Scenario: Neither matches
- **WHEN** a show has genres `["comedy"]` and country `"us"`, with `alternate_genres: ["anime"]` and `alternate_countries: ["jp", "kr", "cn"]`
- **THEN** the request SHALL route to the default server

### Requirement: Overseerr request includes server ID
The Overseerr client SHALL support an optional `serverId` field in the TV request payload.

#### Scenario: Server ID is provided
- **WHEN** `RequestTV` is called with a non-nil server ID
- **THEN** the request body SHALL include `"serverId": <value>`

#### Scenario: Server ID is not provided
- **WHEN** `RequestTV` is called with a nil server ID
- **THEN** the request body SHALL NOT include the `serverId` field (omitted via `omitempty`)

### Requirement: Routing decision is visible in results
Each `ProcessResult` SHALL include the routing decision so it appears in logs and notifications.

#### Scenario: Request routed to alternate server
- **WHEN** a show is requested and routed to the alternate server
- **THEN** the `ProcessResult.Route` field SHALL be set to `"alternate"` and the log/notification SHALL indicate the target server

#### Scenario: Request routed to default server
- **WHEN** a show is requested and routed to the default server
- **THEN** the `ProcessResult.Route` field SHALL be set to `"default"`

#### Scenario: Routing not configured
- **WHEN** the routing block is absent from config
- **THEN** the `ProcessResult.Route` field SHALL be empty
