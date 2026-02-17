## 1. Trakt Client — Extended Show Data

- [x] 1.1 Add `Genres []string` and `Country string` fields to `Show` struct in `internal/client/trakt/types.go`
- [x] 1.2 Add `?extended=full` query parameter to `GetMyShowsCalendar` in `internal/client/trakt/client.go`

## 2. Config — Routing Configuration

- [x] 2.1 Add `RoutingConfig` struct with `DefaultServerID`, `AlternateServerID`, `AlternateGenres`, `AlternateCountries` fields in `internal/config/config.go`
- [x] 2.2 Add `Routing RoutingConfig` field to `WatcherConfig` struct
- [x] 2.3 Document the new `routing` block in `config/config.example.yaml` with comments and examples

## 3. Overseerr Client — Server ID Support

- [x] 3.1 Add `ServerID *int` field with `json:"serverId,omitempty"` to `TVRequest` in `internal/client/overseerr/types.go`
- [x] 3.2 Update `RequestTV` signature to accept `serverID *int` parameter and set it on `TVRequest` in `internal/client/overseerr/client.go`

## 4. Watcher Service — Routing Logic

- [x] 4.1 Add `Route string` field to `ProcessResult` struct in `internal/service/watcher/watcher.go`
- [x] 4.2 Implement `determineServerID` function that takes show genres, country, and routing config — returns `*int` server ID and route label (`"default"`, `"alternate"`, or `""`)
- [x] 4.3 Update `processShow` to call `determineServerID` before the Overseerr request and pass the server ID to `RequestTV`
- [x] 4.4 Pass genres and country through `calendarItem` struct (add `genres` and `country` fields, populate in `groupByShowAndSeason`)

## 5. Logging and Notifications

- [x] 5.1 Update `printSummary` to include routing info (e.g. `[→ MoviePilot]` or `[→ Sonarr]`) in the request log lines
- [x] 5.2 Update Apprise notification formatting to include routing info in watcher details
- [x] 5.3 Update `RequestTV` log message in Overseerr client to include server ID when present

## 6. Callers — Pass-through Update

- [x] 6.1 Update all call sites of `RequestTV` to pass the new `serverID` parameter (watcher is the only caller)
