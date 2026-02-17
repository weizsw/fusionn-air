## Context

The watcher service monitors the Trakt calendar for upcoming episodes and auto-requests new seasons via Overseerr. Currently, all requests go to Overseerr's default server (Sonarr). The user's setup has two download backends behind Overseerr:

- **Sonarr** — for regular Western TV shows
- **MoviePilot** — for anime, K-dramas, J-dramas, and C-dramas

Overseerr supports a `serverId` parameter in its request API to target a specific backend, but Fusionn-Air does not use it today.

The Trakt API supports `?extended=full` on calendar endpoints, which returns `genres` (e.g. `["anime", "drama"]`) and `country` (e.g. `"jp"`, `"kr"`) per show — sufficient to automate routing decisions.

## Goals / Non-Goals

**Goals:**
- Route TV requests to the correct Overseerr backend based on show genre and/or country of origin
- Make routing rules configurable (genres, countries, server IDs) with hot-reload support
- Provide visibility into routing decisions via logs and notifications

**Non-Goals:**
- MoviePilot-managed cleanup (separate future feature)
- Per-show override rules or manual routing lists
- Radarr/movie routing (movies are not affected by this change)

## Decisions

### 1. Use Trakt `?extended=full` for genre/country data

**Choice:** Add `?extended=full` query parameter to the existing `GetMyShowsCalendar` call.

**Alternatives considered:**
- Fetch show details separately per show via `/shows/{id}?extended=full` — rejected because it adds N extra API calls per calendar check, hitting rate limits faster.
- Use TMDB API to get genre/country — rejected because it adds an external dependency when Trakt already provides this data.

**Trade-off:** The `extended=full` response is larger, but we're already parsing the response into structs. The additional fields (`genres`, `country`) are small strings. The benefit of zero extra API calls outweighs the marginal payload increase.

### 2. Config-driven routing with two tiers: default and alternate

**Choice:** Simple two-server routing with genre and country matchers.

```yaml
watcher:
  routing:
    default_server_id: 0
    alternate_server_id: 1
    alternate_genres: ["anime"]
    alternate_countries: ["jp", "kr", "cn"]
```

**Alternatives considered:**
- Full rule-based engine (genre→server mapping) — rejected as YAGNI; only two backends exist.
- Hardcoded anime detection — rejected because user needs to control which countries route where.

**Routing logic:** If show genres intersect `alternate_genres` OR show country is in `alternate_countries`, use `alternate_server_id`. Otherwise, use `default_server_id`. When the `routing` block is absent or empty, omit `serverId` from requests (preserving current behavior).

### 3. Optional `serverId` in Overseerr request

**Choice:** Add `ServerID *int` (pointer) to `TVRequest` so it's omitted from JSON when nil (backward-compatible).

**Rationale:** When routing is not configured, the field is nil and excluded via `omitempty`, so Overseerr uses its default. When routing is configured, the integer value is included.

### 4. Signature change for `RequestTV`

**Choice:** Change `RequestTV(ctx, tmdbID, seasons)` to `RequestTV(ctx, tmdbID, seasons, serverID)` where `serverID` is `*int`.

**Alternative considered:** Passing a full options struct — rejected because there's only one new parameter and the function has 4 args total, which is reasonable.

## Risks / Trade-offs

- **[Trakt genre accuracy]** Trakt's genre tagging may not be 100% accurate for all shows → Mitigation: users can adjust `alternate_genres` and `alternate_countries` in config. The routing rules are a best-effort heuristic, same as manual selection.
- **[Extended response size]** `?extended=full` returns more data per show → Mitigation: negligible impact; genres and country are small string fields. No performance concern at the scale of a personal calendar (typically <50 shows).
- **[Overseerr serverId compatibility]** The `serverId` field behavior depends on Overseerr version → Mitigation: when omitted (nil), Overseerr falls back to its default server, preserving current behavior. Only users who configure routing are affected.
- **[Hot-reload of routing config]** Routing config is read from `cfgMgr.Get()` at execution time → Already supported by the existing config hot-reload mechanism. No additional work needed.
