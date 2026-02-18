## Why

The watcher currently sends all TV requests to Overseerr without specifying a target server. Overseerr uses its default server (Sonarr), but anime, K-dramas, J-dramas, and C-dramas should be routed to MoviePilot instead. Users must manually pick the correct server in the Overseerr UI, which defeats the purpose of automated requesting.

## What Changes

- Fetch Trakt calendar with extended info (`?extended=full`) to get show genres and country of origin
- Add configurable routing rules (`alternate_genres`, `alternate_countries`) to determine which Overseerr server receives each request
- Pass the appropriate `serverId` to the Overseerr request API based on routing rules
- Log and notify which server each request was routed to

## Capabilities

### New Capabilities
- `watcher-routing`: Genre and country-based routing of TV requests to different Overseerr backend servers (Sonarr vs MoviePilot)

### Modified Capabilities
<!-- No existing spec-level behavior changes. Cleanup specs are unaffected. -->

## Impact

- **Trakt client**: `Show` struct gains `Genres` and `Country` fields; calendar API call adds `?extended=full` query parameter
- **Overseerr client**: `TVRequest` struct gains optional `ServerID` field; `RequestTV` accepts a server ID parameter
- **Config**: New `routing` block under `watcher` with `default_server_id`, `alternate_server_id`, `alternate_genres`, `alternate_countries`
- **Watcher service**: Routing logic added before Overseerr request; `ProcessResult` gains `Route` field
- **Notifications**: Watcher results include routing info in logs and Apprise messages
- **Cleanup service**: Unaffected — talks directly to Sonarr/Radarr, not Overseerr
