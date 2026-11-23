# Fusionn-Air

Fusionn-Air requests upcoming TV seasons through Seerr or Overseerr. It uses your Scrob or Trakt calendar and watch history to decide when to request the next season. Apprise can send run summaries and failures.

## Quick start

```bash
cp config/config.example.yaml config/config.yaml
```

For Scrob, set `watcher.source: scrob`. Set `scrob.base_url` to the Scrob web URL and `scrob.api_key` to the key from Scrob **Connections → API Key**. Fusionn-Air reads completed episodes from every source in that Scrob account.

For Trakt, set `watcher.source: trakt` and enter your Trakt app client ID and secret. Follow the device authorization prompt on the first run. Configuration files without `watcher.source` use Trakt.

Set `overseerr.base_url` to the Seerr or Overseerr web URL and `overseerr.api_key` to its API key. If Fusionn-Air runs in Docker, use URLs that the container can reach. Use a shared network service name or a host address to reach services outside that container.

Keep `scheduler.dry_run: true` while you check which seasons qualify. For a manual test, set `scheduler.run_on_start: false`. Set `server.trigger_api_key` to a separate secret and store the same value in `AIR_TRIGGER_KEY`. Start Fusionn-Air.

```bash
go run ./cmd/fusionn-air
```

```bash
curl -X POST -H "X-API-Key: $AIR_TRIGGER_KEY" http://localhost:8080/api/v1/watcher/run
curl http://localhost:8080/api/v1/watcher/stats
```

If the dry-run results look right, set `scheduler.dry_run: false` to send requests. Docker Compose exposes the API on port `4554`.

## Request rules

- The watcher checks seasons with an episode scheduled today or later within `watcher.calendar_days`. Seasons with all episodes outside this window are not candidates. In Scrob mode, a show must have a completed episode in your watch history. Scrob episodes must use TMDB-compatible numbering.
- The watcher requires every episode of the previous season to have aired. You must also have watched every episode, including any recap listed as a regular episode.
- In Scrob mode, the watcher reads the episode count and air dates from Scrob. It skips seasons with missing details or a season order other than TMDB aired order.
- The watcher skips specials and first seasons if you only watched specials. In Trakt mode, it also skips hidden seasons and uses your current watch progress after a reset.
- `watcher.excluded_genres` and `watcher.allowed_languages` use Seerr or Overseerr metadata for both sources. If an enabled filter needs metadata that is missing, the watcher skips the series.
- The watcher skips seasons you already started and seasons with a pending or approved regular-quality request or available media. A 4K-only request does not block a regular-quality request. The watcher skips declined, blocklisted, and deleted seasons. It reports failed requests with the Seerr or Overseerr request ID.
- If you configure `watcher.routing`, the watcher uses genres and countries from Seerr or Overseerr to select a server. Animation from Japan also matches `anime`. Animation from China also matches `donghua`.

## API endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/watcher/stats` | Whether a run is active, the last run results, and any error that stopped that run. |
| POST | `/api/v1/watcher/run` | Start a background run. Send `X-API-Key`. Returns HTTP 202 if accepted or HTTP 409 if a run is active. |

The legacy `POST /api/v1/process` uses the same authentication and response. If `server.trigger_api_key` is empty, the POST endpoints return HTTP 503.

## Configuration

See [the example configuration](config/config.example.yaml) for all fields. Restart Fusionn-Air after you change the watch source or API credentials.

Use `FUSIONN_AIR_WATCHER_SOURCE`, `FUSIONN_AIR_SCROB_BASE_URL`, `FUSIONN_AIR_SCROB_API_KEY`, and `FUSIONN_AIR_OVERSEERR_API_KEY` to override these values in the process environment. For Trakt, use `FUSIONN_AIR_TRAKT_CLIENT_ID` and `FUSIONN_AIR_TRAKT_CLIENT_SECRET`. Use `FUSIONN_AIR_SERVER_TRIGGER_API_KEY` to set the manual run key in the process environment.

If Trakt rejects the refresh token, Fusionn-Air starts device authorization. Open the verification URL and enter the user code from the service log.

## Notifications (Apprise)

To enable notifications, run an [Apprise](https://github.com/caronc/apprise-api) container and configure your notification services.

### 1. Add Apprise to docker-compose.yaml

```yaml
services:
  apprise:
    image: caronc/apprise:latest
    container_name: apprise
    ports:
      - "8000:8000"
    volumes:
      - ./apprise:/config
    restart: unless-stopped
```

### 2. Create Apprise config

Create `apprise/apprise.yml`:

```yaml
urls:
  # Slack webhook
  - slack://tokenA/tokenB/tokenC:
      tag: fusionn-air

  # Discord webhook
  - discord://webhook_id/webhook_token:
      tag: fusionn-air

  # Telegram
  - tgram://bot_token/chat_id:
      tag: fusionn-air
```

See [Apprise Wiki](https://github.com/caronc/apprise/wiki) for all supported services.

### 3. Enable in fusionn-air

```yaml
apprise:
  enabled: true
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-air"
```

## Docker

```bash
# Docker Compose (recommended)
docker compose up -d

# Or manual Docker
docker build -t fusionn-air .
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config:/app/config:ro \
  -v $(pwd)/data:/app/data \
  --name fusionn-air \
  fusionn-air
```

## Tech Stack

- **HTTP Framework**: Gin
- **HTTP Client**: Resty
- **Logging**: Zap
- **Config**: Viper
- **Scheduler**: robfig/cron

## GitHub Actions

This project includes CI/CD workflows:

- **CI** (`ci.yml`) - Runs on push/PR: build, test, lint
- **Build** (`build.yml`) publishes Docker images to GHCR and Docker Hub on branch and version-tag pushes.

### Release a new version

```bash
git tag v1.0.0
git push origin v1.0.0
```

Images will be available at:

- `ghcr.io/USERNAME/fusionn-air:v1.0.0`
- `USERNAME/fusionn-air:v1.0.0` (Docker Hub)
