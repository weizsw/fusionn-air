# Project Context

## Purpose
Fusionn-Air is an automated media management service that monitors your Trakt watch progress and orchestrates actions across your media stack. It handles three main workflows:

1. **Auto-Request (Watcher)** - Monitors Trakt calendar, auto-requests new seasons via Overseerr when you've completed watching previous seasons
2. **Auto-Cleanup** - Removes fully watched TV shows (Sonarr) and movies (Radarr) after a configurable delay
3. **Notifications** - Sends alerts via Apprise to 80+ services (Slack, Discord, Telegram, etc.)

## Tech Stack
- **Language**: Go 1.23
- **HTTP Framework**: [Gin](https://github.com/gin-gonic/gin)
- **HTTP Client**: [Resty](https://github.com/go-resty/resty)
- **Logging**: [Zap](https://github.com/uber-go/zap) (structured logging)
- **Config**: [Viper](https://github.com/spf13/viper) (YAML + env vars, hot-reload)
- **Scheduler**: [robfig/cron](https://github.com/robfig/cron)
- **Rate Limiting**: golang.org/x/time/rate
- **Container**: Docker (multi-stage Alpine build)

## Project Conventions

### Code Style
- Follow [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md), [Effective Go](https://go.dev/doc/effective_go), [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- File naming: `snake_case.go` (e.g., `watcher.go`, `config.go`)
- Package naming: short, lowercase, single-word where possible
- Error messages: lowercase, no punctuation
- Use `fmt.Errorf("context: %w", err)` for error wrapping

### Architecture Patterns
- **Directory Structure**:
  - `cmd/fusionn-air/` - Application entry point
  - `internal/client/` - External API clients (Trakt, Overseerr, Sonarr, Radarr, Apprise)
  - `internal/config/` - Configuration management with hot-reload polling
  - `internal/handler/` - HTTP handlers (Gin)
  - `internal/scheduler/` - Cron job orchestration
  - `internal/service/` - Business logic (watcher, cleanup)
  - `pkg/logger/` - Shared logging utilities

- **Dependency Injection**: Constructor injection via `NewXxx()` factory functions
- **Layered Flow**: `main.go` → `handler` → `service` → `client`
- **Config Hot-Reload**: Polling-based (10s interval) for Docker bind mount compatibility
- **Graceful Shutdown**: Signal handling (SIGINT/SIGTERM) with context cancellation

### API Design
- RESTful JSON API with `/api/v1/` prefix
- Gin router with recovery middleware and custom request logging
- Endpoints: health, stats, queue, manual triggers

### Testing Strategy
- Unit tests with table-driven patterns (`_test.go` suffix)
- Test coverage for business logic in `internal/service/`
- Use interfaces for external dependencies to enable mocking

### Git Workflow
- Main branch: `main`
- Semantic versioning via Git tags (`v1.0.0`)
- CI/CD via GitHub Actions (build, test, lint, release)
- Docker images pushed to GHCR and Docker Hub on tag

## Domain Context
- **Trakt**: Tracks watch progress, calendar for upcoming episodes
- **Overseerr**: Media request management system (requests to Sonarr/Radarr)
- **Sonarr**: TV show management (download, organize)
- **Radarr**: Movie management (download, organize)
- **Apprise**: Notification aggregator supporting 80+ services
- **Watch Progress Logic**: Auto-request S02 only if S01 is 100% complete (aired episodes)
- **Cleanup Logic**: Remove content from Sonarr/Radarr after configurable delay post-watch-completion

## Important Constraints
- **No database**: Stateless service, state derived from external APIs
- **Single binary**: Compiled Go binary with config YAML
- **Dry-run mode**: All destructive operations (requests, deletions) respect `scheduler.dry_run` flag
- **Rate limiting**: Respect API limits for external services
- **OAuth flow**: Trakt requires interactive OAuth device code flow on first run

## External Dependencies

| Service   | Purpose                          | Auth Method      |
|-----------|----------------------------------|------------------|
| Trakt     | Watch history, calendar          | OAuth 2.0 device |
| Overseerr | Media requests                   | API key          |
| Sonarr    | TV show management               | API key          |
| Radarr    | Movie management                 | API key          |
| Apprise   | Notifications (via apprise-api)  | URL + key/tag    |

### Configuration
- Primary: `config/config.yaml` (YAML)
- Override: Environment variables with `FUSIONN_AIR_` prefix (e.g., `FUSIONN_AIR_TRAKT_CLIENT_ID`)
- Token storage: `data/trakt_tokens.json` (persisted OAuth tokens)
