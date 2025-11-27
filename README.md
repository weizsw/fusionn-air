# Fusionn-Air

Automated media request service that watches your Trakt calendar and requests new seasons via Overseerr when you've completed watching previous seasons.

## How It Works

1. **Monitors Trakt Calendar** - Periodically checks your "My Shows" calendar for upcoming episodes
2. **Checks Watch Progress** - For each upcoming show, verifies your watch progress via Trakt
3. **Smart Requesting** - Only requests new seasons when:
   - Previous season is 100% complete
   - Season isn't already requested/available in Overseerr
4. **Auto-Request via Overseerr** - Creates requests that flow to Sonarr automatically

## Quick Start

### 1. Create Trakt App

1. Go to <https://trakt.tv/oauth/applications>
2. Create a new application:
   - **Name**: `fusionn-air` (or anything)
   - **Redirect URI**: `urn:ietf:wg:oauth:2.0:oob`
3. Copy your **Client ID** and **Client Secret**

### 2. Configure

Edit `config/config.yaml`:

```yaml
server:
  port: 8080

trakt:
  client_id: "your-client-id"      # From step 1
  client_secret: "your-client-secret"
  base_url: "https://api.trakt.tv"

overseerr:
  base_url: "http://localhost:5055"
  api_key: "your-api-key"          # Overseerr > Settings > General

scheduler:
  cron: "0 */6 * * *"   # Every 6 hours
  calendar_days: 14      # Look 14 days ahead
  dry_run: true          # Start with true to test!
```

### 3. Run

```bash
go run ./cmd/fusionn
```

On first run, you'll see:

```
[trakt-auth] ╔════════════════════════════════════════════════════════════╗
[trakt-auth] ║              TRAKT AUTHORIZATION REQUIRED                  ║
[trakt-auth] ╠════════════════════════════════════════════════════════════╣
[trakt-auth] ║  1. Go to: https://trakt.tv/activate                       ║
[trakt-auth] ║  2. Enter code: A1B2C3D4                                   ║
[trakt-auth] ║  3. Click 'Authorize' on the Trakt website                 ║
[trakt-auth] ╚════════════════════════════════════════════════════════════╝
```

Just follow the instructions - tokens are saved automatically to `data/trakt_tokens.json` and auto-refreshed.

### 4. Test

```bash
# Trigger a manual run
curl -X POST http://localhost:8080/api/v1/process

# Check stats
curl http://localhost:8080/api/v1/stats
```

### 5. Deploy

Once happy with dry-run results, set `dry_run: false` and deploy:

```bash
docker compose up -d
```

## Configuration

```yaml
server:
  port: 8080

trakt:
  client_id: ""           # Required
  client_secret: ""       # Required
  base_url: "https://api.trakt.tv"

overseerr:
  base_url: ""            # Required - e.g. http://localhost:5055
  api_key: ""             # Required - from Overseerr settings

scheduler:
  cron: "0 */6 * * *"     # Cron schedule (every 6 hours)
  calendar_days: 14       # Days ahead to check
  dry_run: false          # True = log only, no actual requests
  run_on_start: true      # Run immediately on startup
```

### Environment Variables

Override any config with `FUSIONN_AIR_` prefix:

```bash
FUSIONN_AIR_TRAKT_CLIENT_ID=xxx
FUSIONN_AIR_OVERSEERR_API_KEY=xxx
FUSIONN_AIR_SCHEDULER_DRY_RUN=true
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/stats` | Processing statistics |
| POST | `/api/v1/process` | Manually trigger processing |

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

## Development

```bash
# Run locally (dev mode = verbose logging)
ENV=development go run ./cmd/fusionn

# Build binary
go build -o fusionn-air ./cmd/fusionn
```

## Logic Flow

```
┌─────────────────┐
│  Trakt Calendar │
│  (My Shows)     │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ For each show   │
│ with upcoming   │
│ episode/season  │
└────────┬────────┘
         │
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

## Tech Stack

- **HTTP Framework**: Gin
- **HTTP Client**: Resty
- **Logging**: Zap
- **Config**: Viper
- **Scheduler**: robfig/cron
