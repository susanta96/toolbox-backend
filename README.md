# Toolbox Backend

A Go REST API for PDF utilities — lock (encrypt), unlock (decrypt), compress, merge, and split PDFs. Built for scalability with PostgreSQL (Neon), automatic file cleanup, and a clean layered architecture.

## Features

- **Lock PDF** — Encrypt a PDF with AES-256 password protection
- **Unlock PDF** — Decrypt a password-protected PDF
- **Compress PDF** — Reduce PDF file size with configurable quality levels (low / medium / high / maximum)
- **Merge PDF** — Combine multiple PDF files into one (up to 10 files, configurable)
- **Split PDF** — Split a PDF into individual pages or custom page ranges (output as ZIP)
- **Currency Convert** — Convert between top global currencies with rate source metadata
- **Currency History** — Fetch historical FX series with daily/weekly/monthly aggregation
- **FX cache + warmup** — In-memory + Postgres-backed caching with scheduled warmup
- **Auto-cleanup** — Cron job removes uploaded/generated files older than 1 hour
- **PostgreSQL** — Tracks file records via Neon cloud Postgres
- **Download API** — Retrieve processed files via secure download endpoint

## Tech Stack

| Component       | Library / Tool                                 |
|-----------------|------------------------------------------------|
| Router          | [chi](https://github.com/go-chi/chi)          |
| Encrypt/Decrypt | [qpdf](https://github.com/qpdf/qpdf) (CLI)   |
| Compression     | [Ghostscript](https://ghostscript.com) (CLI)  |
| Database        | [pgx](https://github.com/jackc/pgx) v5        |
| Scheduler       | [robfig/cron](https://github.com/robfig/cron) v3 |
| Logging         | `log/slog` (stdlib)                            |

## Project Structure

```
toolbox-backend/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── config/config.go        # Env-based configuration
│   ├── database/postgres.go    # DB connection + migrations
│   ├── handler/                # HTTP handlers & router
│   ├── middleware/              # Logging, recovery, body limit
│   ├── model/                  # Domain models
│   ├── repository/             # Data access layer
│   ├── scheduler/              # Cleanup cron job
│   └── service/                # Business logic (PDF ops)
├── migrations/                 # SQL migration files
├── pkg/response/               # Shared HTTP response helpers
├── Dockerfile                  # Multi-stage Docker build
├── Makefile                    # Build/run/test commands
└── .env.example                # Environment variable template
```

## Quick Start

### Prerequisites

- Go 1.23+
- PostgreSQL (or [Neon](https://neon.tech) free tier)
- [qpdf](https://github.com/qpdf/qpdf/releases) — for PDF lock/unlock
- [Ghostscript](https://ghostscript.com/releases/) — for PDF compression

### Setup

```bash
# Clone
git clone https://github.com/susanta96/toolbox-backend.git
cd toolbox-backend

# Configure
cp .env.example .env
# Edit .env with your Neon DATABASE_URL

# Install dependencies
go mod tidy

# Run
go run ./cmd/server
# or
make run
```

### Environment Variables

| Variable           | Default       | Description                        |
|--------------------|---------------|------------------------------------|
| `PORT`             | `8080`        | Server port                        |
| `DATABASE_URL`     | (required)    | PostgreSQL connection string       |
| `UPLOAD_DIR`       | `./uploads`   | Directory for uploaded files       |
| `GENERATED_DIR`    | `./generated` | Directory for processed files      |
| `MAX_UPLOAD_SIZE_MB` | `50`        | Max upload file size in MB         |
| `MAX_MERGE_FILES`    | `10`        | Max number of files for merge      |
| `CLEANUP_INTERVAL` | `10m`         | How often cleanup runs             |
| `FILE_RETENTION`   | `1h`          | How long files are kept            |
| `FX_PROVIDER_URL`  | `https://api.frankfurter.dev` | FX data provider base URL |
| `FX_CACHE_TTL`     | `30m`         | Fresh cache TTL for FX rates       |
| `FX_STALE_WINDOW`  | `6h`          | Allowed stale window if provider fails |
| `FX_WARMUP_EVERY`  | `30m`         | Warmup scheduler interval          |
| `FX_WARMUP_LIMIT`  | `30`          | Max most-requested pairs to warm each run |
| `FX_HTTP_TIMEOUT`  | `8s`          | HTTP timeout for provider calls    |
| `FX_HISTORY_KEEP`  | `8760h`       | Historical retention window (12 months) |

## API Endpoints

### `GET /hello`
Health check / demo endpoint.

```bash
curl http://localhost:8080/hello
```

```json
{
  "message": "Welcome to Toolbox Backend API! 🧰",
  "data": {
    "version": "1.0.0",
    "go_version": "go1.23.0",
    "status": "healthy"
  }
}
```

### `POST /api/v1/pdf/lock`
Lock (encrypt) a PDF with a password.

```bash
curl -X POST http://localhost:8080/api/v1/pdf/lock \
  -F "file=@document.pdf" \
  -F "password=mysecret123"
```

```json
{
  "message": "PDF locked successfully",
  "data": {
    "id": "uuid-here",
    "download": "/api/v1/pdf/download/uuid-here",
    "file_name": "uuid_document_locked.pdf"
  }
}
```

### `POST /api/v1/pdf/unlock`
Unlock (decrypt) a password-protected PDF.

```bash
curl -X POST http://localhost:8080/api/v1/pdf/unlock \
  -F "file=@locked_document.pdf" \
  -F "password=mysecret123"
```

```json
{
  "message": "PDF unlocked successfully",
  "data": {
    "id": "uuid-here",
    "download": "/api/v1/pdf/download/uuid-here",
    "file_name": "uuid_locked_document_unlocked.pdf"
  }
}
```

### `POST /api/v1/pdf/compress`
Compress a PDF to reduce file size.

**Form fields:**
- `file` — PDF file (required)
- `level` — Compression level: `low`, `medium` (default), `high`, or `maximum` (optional)

```bash
curl -X POST http://localhost:8080/api/v1/pdf/compress \
  -F "file=@large_document.pdf" \
  -F "level=high"
```

```json
{
  "message": "PDF compressed successfully",
  "data": {
    "id": "uuid-here",
    "download": "/api/v1/pdf/download/uuid-here",
    "file_name": "large_document_compressed.pdf",
    "original_size": 5242880,
    "compressed_size": 1048576,
    "saved_bytes": 4194304,
    "compression_percent": "80.0"
  }
}
```

### `GET /api/v1/pdf/download/{id}`
Download a processed PDF file (available for 1 hour).

```bash
curl -OJ http://localhost:8080/api/v1/pdf/download/uuid-here
```

### `POST /api/v1/pdf/merge`
Merge multiple PDF files into one. Files are combined in upload order.

**Form fields:**
- `files` — Multiple PDF files (2 to 10, configurable via `MAX_MERGE_FILES`)

```bash
curl -X POST http://localhost:8080/api/v1/pdf/merge \
  -F "files=@document1.pdf" \
  -F "files=@document2.pdf" \
  -F "files=@document3.pdf"
```

```json
{
  "message": "PDFs merged successfully",
  "data": {
    "id": "uuid-here",
    "download": "/api/v1/pdf/download/uuid-here",
    "file_name": "document1 (Merged).pdf",
    "file_count": 3
  }
}
```

### `POST /api/v1/pdf/split`
Split a PDF into multiple files, delivered as a ZIP archive.

**Form fields:**
- `file` — PDF file (required)
- `mode` — `all` (default, one PDF per page) or `ranges` (custom page ranges)
- `pages` — Comma-separated page ranges, required when mode is `ranges` (e.g. `1-3,4-6,7-10`)

**Split all pages:**
```bash
curl -X POST http://localhost:8080/api/v1/pdf/split \
  -F "file=@document.pdf" \
  -F "mode=all"
```

**Split by ranges:**
```bash
curl -X POST http://localhost:8080/api/v1/pdf/split \
  -F "file=@document.pdf" \
  -F "mode=ranges" \
  -F "pages=1-3,4-6,7-10"
```

```json
{
  "message": "PDF split successfully",
  "data": {
    "id": "uuid-here",
    "download": "/api/v1/pdf/download/uuid-here",
    "file_name": "document (Split).zip",
    "page_count": 10,
    "split_count": 3
  }
}
```

### `GET /api/v1/currency/supported`
Returns top supported currencies for the UI selector.

```bash
curl "http://localhost:8080/api/v1/currency/supported"
```

```json
{
  "message": "Supported currencies fetched",
  "data": {
    "currencies": [
      { "code": "USD", "name": "US Dollar" },
      { "code": "INR", "name": "Indian Rupee" },
      { "code": "EUR", "name": "Euro" }
    ]
  }
}
```

### `GET /api/v1/currency/convert`
Converts amount from one currency to another.

**Query params:**
- `from` (required): 3-letter currency code (for example `USD`)
- `to` (required): 3-letter currency code (for example `INR`)
- `amount` (required): non-negative number

```bash
curl "http://localhost:8080/api/v1/currency/convert?from=USD&to=INR&amount=100"
```

```json
{
  "message": "Conversion successful",
  "data": {
    "from": "USD",
    "to": "INR",
    "amount": 100,
    "rate": 83.12,
    "converted": 8312,
    "source": "provider",
    "stale": false,
    "updated_at": "2026-03-23T00:00:00Z"
  }
}
```

**Notes:**
- `source` can be `cache`, `db`, or `provider`.
- `stale=true` means provider was unavailable and a recent fallback rate was used.

### `GET /api/v1/currency/historical`
Returns historical points for a pair and aggregation mode.

**Query params:**
- `from` (required): 3-letter currency code
- `to` (required): 3-letter currency code
- `range` (optional): number of days with `d` suffix (`7d`, `30d`, `90d`, `365d`), default `30d`
- `aggregation` (optional): `daily` (default), `weekly`, or `monthly`

```bash
curl "http://localhost:8080/api/v1/currency/historical?from=USD&to=INR&range=30d&aggregation=daily"
```

```json
{
  "message": "Historical rates fetched",
  "data": {
    "from": "USD",
    "to": "INR",
    "aggregation": "daily",
    "source": "db",
    "stale": false,
    "updated_at": "2026-03-23T09:05:00Z",
    "points": [
      { "date": "2026-03-20", "rate": 83.05 },
      { "date": "2026-03-21", "rate": 83.11 },
      { "date": "2026-03-22", "rate": 83.09 }
    ]
  }
}
```

#### Currency API Error Cases

```json
{ "error": "from, to and amount are required" }
```

```json
{ "error": "amount must be a valid non-negative number" }
```

```json
{ "error": "aggregation must be one of daily, weekly, monthly" }
```

```json
{ "error": "invalid range value, expected format like 7d, 30d, 90d, 365d" }
```

```json
{ "error": "rate currently unavailable" }
```

## Currency API Quick Usage

1. Fetch selectable currencies:
```bash
curl "http://localhost:8080/api/v1/currency/supported"
```
2. Convert an amount for current rate display:
```bash
curl "http://localhost:8080/api/v1/currency/convert?from=EUR&to=INR&amount=250"
```
3. Load chart data for history view (weekly or monthly works too):
```bash
curl "http://localhost:8080/api/v1/currency/historical?from=EUR&to=INR&range=90d&aggregation=weekly"
```

## Build & Test

```bash
# Build binary
make build

# Run tests
make test

# Docker
make docker-build
make docker-run
```

## Deployment Guide

### Cheapest / Free Options (Recommended Order)

1. **[Render](https://render.com)** — Free tier for web services
   - Push Docker image or connect GitHub repo
   - Auto-deploy on push; free PostgreSQL (90 days)
   - Set env vars in dashboard
   ```
   render.yaml or just connect your repo → select Docker → set env vars
   ```

2. **[Fly.io](https://fly.io)** — Free tier (3 shared VMs)
   ```bash
   fly launch
   fly secrets set DATABASE_URL="your-neon-url"
   fly deploy
   ```

3. **[Railway](https://railway.app)** — $5 free credit/month
   - Connect GitHub → auto-detect Dockerfile → deploy
   - Set env vars in dashboard

4. **[Koyeb](https://koyeb.com)** — Free nano instance
   - Connect GitHub or push Docker image
   - Set env vars in dashboard

### Database: [Neon](https://neon.tech)
- **Free tier**: 0.5 GB storage, autoscaling, branching
- Get your connection string from Neon dashboard
- Use `?sslmode=require` in the connection URL

### Deploy Steps (Generic)

1. **Build**: `docker build -t toolbox-backend .`
2. **Push**: Push to GitHub Container Registry, Docker Hub, or your cloud's registry
3. **Configure**: Set `DATABASE_URL` and other env vars
4. **Run**: The container exposes port 8080 — map it in your cloud config
5. **Storage note**: `uploads/` and `generated/` are ephemeral in containers — files auto-expire in 1hr anyway, so this is fine for the current feature set. For persistent storage, mount a volume.

## License

MIT
