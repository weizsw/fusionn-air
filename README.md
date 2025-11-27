# Fusionn-Air

Automated media management service with three main features:

1. **Auto-Request** - Monitors Trakt calendar and requests new seasons via Overseerr when you've completed watching previous seasons
2. **Auto-Cleanup** - Removes fully watched series from Sonarr after a configurable delay
3. **Notifications** - Sends alerts via Apprise to Slack, Discord, Telegram, etc.

## Features

### 🎬 Auto-Request (Watcher)

- Monitors your Trakt "My Shows" calendar for upcoming episodes
- Checks watch progress - only requests when previous season is 100% complete
- Prevents duplicate requests by checking Overseerr status (shows who already requested)
- Supports requesting as a specific Overseerr user
- Shows total vs aired episode counts for better visibility

### 🧹 Auto-Cleanup

- Identifies fully watched series in Sonarr via Trakt
- Removes when all **on-disk episodes** are watched (works with continuing series too)
- Skips series with more episodes coming (ongoing seasons)
- Skips unmonitored series in Sonarr
- Configurable delay (default 3 days) before removal
- Exclusion list for series you want to keep forever
- Deletes files from disk when removing from Sonarr

### 🔔 Notifications (Apprise)

- Send notifications via [Apprise](https://github.com/caronc/apprise) to 80+ services
- Slack, Discord, Telegram, Email, Pushover, and more
- Notifies on new season requests and series removals
- Slack-optimized formatting for readability

## Quick Start

### 1. Create Trakt App

1. Go to <https://trakt.tv/oauth/applications>
2. Create a new application:
   - **Name**: `fusionn-air`
   - **Redirect URI**: `urn:ietf:wg:oauth:2.0:oob`
3. Copy your **Client ID** and **Client Secret**

### 2. Configure

```bash
cp config/config.example.yaml config/config.yaml
```

Edit `config/config.yaml`:

```yaml
server:
  port: 8080

trakt:
  client_id: "your-client-id"
  client_secret: "your-client-secret"
  base_url: "https://api.trakt.tv"

overseerr:
  base_url: "http://localhost:5055"
  api_key: "your-api-key"
  user_id: 0  # 0 = API key owner, or specific user ID

sonarr:
  base_url: "http://localhost:8989"
  api_key: "your-api-key"

# Shared scheduler settings (applies to both watcher and cleanup)
scheduler:
  cron: "0 */6 * * *"  # Every 6 hours
  dry_run: true        # Test first!
  run_on_start: true

# Watcher - auto-request new seasons
watcher:
  enabled: true
  calendar_days: 14

# Cleanup - auto-remove fully watched series
cleanup:
  enabled: false
  delay_days: 3
  exclusions: []  # e.g. ["Breaking Bad", "The Office"]

# Notifications via Apprise (optional)
apprise:
  enabled: false
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-air"
```

### 3. Run

```bash
go run ./cmd/fusionn-air
```

On first run, follow the Trakt authorization prompts.

### 4. Test

```bash
# Trigger watcher (new season requests)
curl -X POST http://localhost:8080/api/v1/watcher/run

# Trigger cleanup (remove watched series)
curl -X POST http://localhost:8080/api/v1/cleanup/run

# Check cleanup queue
curl http://localhost:8080/api/v1/cleanup/queue
```

### 5. Deploy

Once happy with dry-run results, set `scheduler.dry_run: false` and deploy:

```bash
docker compose up -d
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/watcher/stats` | Watcher statistics |
| POST | `/api/v1/watcher/run` | Trigger watcher manually |
| GET | `/api/v1/cleanup/stats` | Cleanup statistics |
| GET | `/api/v1/cleanup/queue` | View removal queue |
| POST | `/api/v1/cleanup/run` | Trigger cleanup manually |

## Configuration Reference

```yaml
server:
  port: 8080

trakt:
  client_id: ""           # Required
  client_secret: ""       # Required
  base_url: "https://api.trakt.tv"

overseerr:
  base_url: ""            # Required
  api_key: ""             # Required
  user_id: 0              # Request as specific user (0 = API key owner)

sonarr:
  base_url: ""            # Required for cleanup
  api_key: ""             # Required for cleanup

scheduler:
  cron: "0 */6 * * *"     # Cron schedule (applies to both watcher and cleanup)
  dry_run: false          # Log only, no actual requests/deletions
  run_on_start: true      # Run immediately on startup

watcher:
  enabled: true           # Enable watcher feature
  calendar_days: 14       # Days ahead to check for upcoming episodes

cleanup:
  enabled: false          # Enable cleanup feature
  delay_days: 3           # Days to wait after fully watched before removing
  exclusions: []          # Series titles to never remove

apprise:
  enabled: false          # Enable notifications
  base_url: ""            # Apprise API URL (e.g., http://apprise:8000)
  key: "apprise"          # Apprise config key
  tag: "fusionn-air"      # Tag to filter services
```

### Environment Variables

Override any config with `FUSIONN_AIR_` prefix:

```bash
FUSIONN_AIR_TRAKT_CLIENT_ID=xxx
FUSIONN_AIR_OVERSEERR_API_KEY=xxx
FUSIONN_AIR_SONARR_API_KEY=xxx
FUSIONN_AIR_WATCHER_ENABLED=true
FUSIONN_AIR_CLEANUP_ENABLED=true
FUSIONN_AIR_APPRISE_ENABLED=true
FUSIONN_AIR_APPRISE_BASE_URL=http://apprise:8000
FUSIONN_AIR_APPRISE_KEY=apprise
FUSIONN_AIR_APPRISE_TAG=fusionn-air
```

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

## Logic Flows

### Watcher (Auto-Request)

```
┌─────────────────┐
│  Trakt Calendar │
└────────┬────────┘
         ▼
┌─────────────────┐     No      ┌──────────┐
│ Previous season ├────────────►│  Skip    │
│ complete?       │             └──────────┘
└────────┬────────┘
         │ Yes
         ▼
┌─────────────────┐     Yes     ┌──────────────────┐
│ Already in      ├────────────►│ Skip (shows who  │
│ Overseerr?      │             │ requested it)    │
└────────┬────────┘             └──────────────────┘
         │ No
         ▼
┌─────────────────┐
│ Request season  │
│ via Overseerr   │
└─────────────────┘
```

### Cleanup (Auto-Remove)

```
┌─────────────────┐
│  Sonarr Series  │
└────────┬────────┘
         ▼
┌─────────────────┐     Yes     ┌──────────┐
│ In exclusion    ├────────────►│  Skip    │
│ list?           │             └──────────┘
└────────┬────────┘
         │ No
         ▼
┌─────────────────┐     No      ┌──────────┐
│ All on-disk     ├────────────►│  Skip    │
│ episodes watched│             │ (still   │
│ on Trakt?       │             │ watching)│
└────────┬────────┘             └──────────┘
         │ Yes
         ▼
┌─────────────────┐
│ Add to removal  │
│ queue           │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ After delay_days│────────────► Delete from Sonarr
│ in queue        │              (with files)
└─────────────────┘
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
- **Release** (`release.yml`) - Runs on tag push (`v*`): builds and pushes Docker images to GHCR and Docker Hub

### Release a new version

```bash
git tag v1.0.0
git push origin v1.0.0
```

Images will be available at:

- `ghcr.io/USERNAME/fusionn-air:v1.0.0`
- `USERNAME/fusionn-air:v1.0.0` (Docker Hub)
