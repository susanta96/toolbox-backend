# Deployment

How to deploy and update `toolbox-backend` on a Linux VPS with Docker Compose.

## Prerequisites

- Docker Engine with the Compose plugin (`docker compose version`)
- The repository cloned on the VPS
- A `.env` file next to `docker-compose.yml` (see `.env.example`)
- `DATABASE_URL` pointing at a reachable PostgreSQL instance (e.g. Neon)

## What persists across a rebuild

| Data | Location | Survives rebuild |
|---|---|---|
| Environment config | `.env` on the VPS (gitignored) | Yes |
| Uploaded / generated files | named volumes `app_uploads`, `app_generated` | Yes |
| Database schema and data | external PostgreSQL | Yes |
| Application binary | Docker image `toolbox-backend:latest` | Replaced (expected) |

> Never run `docker compose down -v`. The `-v` flag deletes the named volumes and with them any files stored there.

## First deploy

```bash
cd /path/to/toolbox-backend
cp .env.example .env
# edit .env: DATABASE_URL, IMAGEKIT_PRIVATE_KEY, APP_PORT, etc.

docker compose up -d --build
```

Migrations run automatically at startup (`database.RunMigrations` in `cmd/server/main.go`), so no separate migration step is required.

## Updating to the latest code

```bash
cd /path/to/toolbox-backend

# 1. Inspect the current checkout
git status
git log --oneline -3

# 2. Pull the latest code (fast-forward only)
git pull --ff-only

# 3. Tag the running image as a rollback point
docker tag toolbox-backend:latest toolbox-backend:previous

# 4. Build the new image while the old container keeps serving
docker compose build

# 5. Recreate the container with the new image
docker compose up -d
```

Steps 4 and 5 are split so the container is only stopped during the short recreate, not during the build. `docker compose up -d --build` does both in one command if a brief downtime is acceptable.

### Verify the update

```bash
docker compose ps
docker compose logs --tail=50 app
curl -fsS http://127.0.0.1:8088/hello
```

The container's healthcheck polls `/hello`; `docker compose ps` should show `healthy`.

## Rollback

```bash
docker compose down
docker tag toolbox-backend:previous toolbox-backend:latest
docker compose up -d
```

`down` without `-v` leaves the volumes intact.

## Notes

- **`.env` is required.** `docker-compose.yml` declares `env_file: .env`; if the file is missing, `docker compose up` fails before starting anything.
- **Host port** comes from `APP_PORT` in `.env` (default `8088`). Changing it requires updating the reverse proxy and firewall too.
- **Build cache** is preserved via BuildKit cache mounts in the `Dockerfile`. Use `docker compose build --no-cache` only when a build appears stale.
- **Local changes on the VPS** can block `git pull`. Check `git status` first; if the changes are disposable, `git stash` before pulling.
- **Low-memory VPS**: the Go build stage can be memory-hungry. If it is OOM-killed, add swap or build the image elsewhere and transfer it with `docker save` / `docker load`.
- **Disk cleanup**: `docker image prune -f` and `docker builder prune -f` reclaim space from old builds. Do not run `docker system prune -a --volumes`, which can delete application volumes.
