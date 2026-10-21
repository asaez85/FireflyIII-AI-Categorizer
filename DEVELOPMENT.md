# FireflyIII-AI-Categorizer — Development Setup

## Prerequisites

- Go 1.22+ (installed at `/opt/data/tools/go/go/bin/go`)
- Docker (optional, for containerized builds)

## Quick Start

```bash
# 1. Add Go to PATH
export PATH=/opt/data/tools/go/go/bin:$PATH

# 2. Download dependencies
go mod download

# 3. Build the binary
go build -o firefly-ai-categorize ./cmd/server

# 4. Run with test config
PORT=3000 CONFIG_FILE=./config.json ./firefly-ai-categorize
```

## Connecting to Firefly III (test instance)

Set these environment variables or configure via the Settings UI at http://localhost:3000:

| Variable | Description |
|----------|-------------|
| `FIREFLY_URL` | URL of your Firefly III instance |
| `FIREFLY_PERSONAL_TOKEN` | Firefly III Personal Access Token |

## AI Provider Configuration

At least one AI provider must be configured:

| Provider | Variables |
|----------|-----------|
| OpenAI | `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` (optional) |
| Gemini | `GEMINI_API_KEY`, `GEMINI_MODEL` |
| DeepSeek | `DEEPSEEK_API_KEY`, `DEEPSEEK_MODEL` |

## Project Structure

```
cmd/server/          — Entry point (main.go)
internal/
  api/               — HTTP handlers (chi router)
  cache/             — In-memory caching
  classifier/        — AI provider integrations (OpenAI, Gemini, DeepSeek)
  config/            — Configuration loading and persistence
  firefly/           — Firefly III API client
  job/               — Job queue and registry
  pipeline/          — Transaction processing pipeline
  worker/            — Background webhook worker
public/              — Embedded web UI (HTML/CSS/JS)
```

## Key Files for Review Screen/API Optimization

- `internal/api/handler.go` — Review API endpoints
- `internal/firefly/client.go` — Firefly III API calls
- `internal/cache/review_cache.go` — TTL cache for review groups
- `public/` — Frontend assets (Review page JS/HTML)

## Performance Tuning

| Variable | Default | Description |
|----------|---------|-------------|
| `REVIEW_CACHE_TTL` | `30s` | TTL for review groups in-memory cache. Frontend polls `/api/review` every 30s, so cached hits are ~1ms instead of ~1s. Set `0` to disable. |
| `REVIEW_LOOKBACK_DAYS` | `365` | Days of transaction history scanned for review-tagged items. 365 days = ~3 API pages (~1s); 0 = full history (~20s with limit=200). |

## Testing

```bash
# Run tests
go test ./...

# Build with race detector
go build -race -o firefly-ai-categorize ./cmd/server

# Docker build
docker build -t firefly-ai-categorize:dev .
```

## Git Remotes

- `origin` — This fork (set after GitHub fork is created)
- `upstream` — https://github.com/ejagombar/FireflyIII-AI-Categorizer (original)
