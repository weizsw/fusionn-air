# Fusionn-Air

Automated media management service with two main features:

1. **Auto-Request** - Monitors Trakt calendar and requests new seasons via Overseerr when you've completed watching previous seasons
2. **Auto-Cleanup** - Removes fully watched series from Sonarr after a configurable delay

## Features

### 🎬 Auto-Request (Watcher)

- Monitors your Trakt "My Shows" calendar for upcoming episodes
- Checks watch progress - only requests when previous season is 100% complete
- Prevents duplicate requests by checking Overseerr status
- Requests flow through Overseerr → Sonarr automatically

### 🧹 Auto-Cleanup

- Identifies fully watched series in Sonarr via Trakt
- Only removes **ended** series (not continuing/airing shows)
- Configurable delay (default 3 days) before removal
- Exclusion list for series you want to keep forever
- Deletes files from disk when removing from Sonarr

## Quick Start

### 1. Create Trakt App

1. Go to <https://trakt.tv/oauth/applications>
2. Create a new application:
   - **Name**: `fusionn-air`
   - **Redirect URI**: `urn:ietf:wg:oauth:2.0:oob`
3. Copy your **Client ID** and **Client Secret**

### 2. Configure

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

sonarr:
  base_url: "http://localhost:8989"
  api_key: "your-api-key"

scheduler:
  cron: "0 */6 * * *"     # Every 6 hours
  calendar_days: 14
  dry_run: true           # Test first!
  run_on_start: true

cleanup:
  enabled: true
  delay_days: 3           # Wait 3 days after fully watched
  dry_run: true           # Test first!
  exclusions:             # Never delete these
    - "Breaking Bad"
    - "Game of Thrones"
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

Once happy with dry-run results:

1. Set `scheduler.dry_run: false`
2. Set `cleanup.dry_run: false`
3. Deploy: `docker compose up -d`

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

sonarr:
  base_url: ""            # Required for cleanup
  api_key: ""             # Required for cleanup

scheduler:
  cron: "0 */6 * * *"     # Cron schedule
  calendar_days: 14       # Days ahead to check
  dry_run: false          # Log only, no requests
  run_on_start: true      # Run on startup

cleanup:
  enabled: false          # Enable cleanup feature
  delay_days: 3           # Days to wait after fully watched
  dry_run: false          # Log only, no deletions
  exclusions: []          # Series titles to never remove
```

### Environment Variables

Override any config with `FUSIONN_AIR_` prefix:

```bash
FUSIONN_AIR_TRAKT_CLIENT_ID=xxx
FUSIONN_AIR_OVERSEERR_API_KEY=xxx
FUSIONN_AIR_SONARR_API_KEY=xxx
FUSIONN_AIR_CLEANUP_ENABLED=true
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
┌─────────────────┐     Yes     ┌──────────┐
│ Already in      ├────────────►│  Skip    │
│ Overseerr?      │             └──────────┘
└────────┬────────┘
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
┌─────────────────┐     No      ┌──────────┐
│ Series ended?   ├────────────►│  Skip    │
└────────┬────────┘             └──────────┘
         │ Yes
         ▼
┌─────────────────┐     No      ┌──────────┐
│ Fully watched   ├────────────►│  Skip    │
│ on Trakt?       │             └──────────┘
└────────┬────────┘
         │ Yes
         ▼
┌─────────────────┐     No      ┌──────────┐
│ In exclusion    ├────────────►│ Add to   │
│ list?           │             │ queue    │
└────────┬────────┘             └──────────┘
         │ Yes
         ▼
┌─────────────────┐
│     Skip        │
└─────────────────┘

┌─────────────────┐
│ Queue items     │
│ older than      │────────────► Delete from Sonarr
│ delay_days      │              (with files)
└─────────────────┘
```

## Tech Stack

- **HTTP Framework**: Gin
- **HTTP Client**: Resty
- **Logging**: Zap
- **Config**: Viper
- **Scheduler**: robfig/cron
