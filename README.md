# Event Execution Platform (MVP)

Scalable event execution + collaboration backend with a simple web UI:

- Go backend (`net/http`) with JWT auth + basic RBAC
- Event + participant + task + submission (proof) APIs
- Proof validation flow (approve/reject) with XP + activity logs
- In-app notifications + real-time chat via SSE
- CSV persistence for local/dev (swap for Postgres in production)

## Run

```bash
mkdir -p /tmp/go-cache /tmp/go-modcache
CGO_ENABLED=0 GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-modcache go run ./cmd/server
```

Open `http://localhost:8080`.

## CSV “Database” (local persistence)

By default the server stores data in CSV files under `data/` (relative to where the server runs).

Set a custom location:

```bash
export CSV_DB_DIR=./data
```

## Auth

- `POST /api/auth/dev` is enabled by default for local development (`DEV_AUTH_ENABLED=true`).
- `POST /api/auth/google` exists, but full Google ID token signature verification is **not** implemented in this MVP.
  - For local prototyping you can set `UNSAFE_SKIP_GOOGLE_VERIFY=true` (not recommended for production).

## Roles

- Default role on first login: `operator`
- Switch roles: `POST /api/me/role` (`operator` ↔ `commander`)

## Core API

- Auth/profile:
  - `POST /api/auth/dev` → `{ token, user }`
  - `POST /api/auth/google` → `{ token, user }`
  - `GET /api/me`
  - `GET /api/me/activity`
  - `POST /api/me/role`
- Events:
  - `GET /api/events`
  - `POST /api/events` (commander)
  - `GET /api/events/{id}`
  - `POST /api/events/{id}/join`
  - `GET /api/events/{id}/participants` (commander, owner)
  - `GET /api/events/{id}/dashboard` (commander, owner)
  - `POST /api/events/{id}/announce` (commander, owner)
- Tasks & proof:
  - `GET /api/events/{id}/tasks`
  - `POST /api/events/{id}/tasks` (commander, owner)
  - `POST /api/tasks/{id}/start`
  - `POST /api/tasks/{id}/submit` (multipart: `image`, optional `comment`, `lat`, `lng`)
  - `GET /api/tasks/{id}/latest-submission`
  - `POST /api/tasks/{id}/review` (commander, owner)
- Chat & notifications:
  - `GET/POST /api/events/{id}/chat`
  - `GET /api/events/{id}/chat/stream` (SSE; uses `?token=...` for EventSource)
  - `GET /api/notifications`
  - `POST /api/notifications` (mark read)
  - `GET /api/leaderboard`

### Fedora / GNAT toolchain note

If you have GNAT installed and its `gcc` is ahead of `/usr/bin` in your `PATH`, `go run` (with cgo enabled) may try to link using GNAT's older binutils and fail with errors mentioning `.relr.dyn` and `-lresolv`.

Fix by forcing the system toolchain:

```bash
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-modcache CC=/usr/bin/gcc CXX=/usr/bin/g++ go run ./cmd/server
```

Or use the helper script:

```bash
bash scripts/run-server.sh
```

## Docker (deployment-ready)

```bash
docker build -t eventmap .
docker run --rm -p 8080:8080 \
  -e PUBLIC_ORIGIN=http://localhost:8080 \
  eventmap
```

## Netlify (free frontend hosting)

Netlify can host the static frontend in `web/`. The Go backend must be deployed separately (Render/Fly/Cloud Run/VPS), because this app is a stateful server (in-memory store) and is not a good fit for serverless functions.

### Deploy frontend to Netlify

1) In Netlify, create a new site from this repo.
2) Keep defaults from `netlify.toml`:
   - Publish directory: `web`
   - Build command: `bash scripts/netlify-build.sh`
3) In Netlify site settings → Environment variables, set:
   - `EVENTMAP_API_BASE` = `https://YOUR_BACKEND_DOMAIN`

### Deploy backend (anywhere that runs Docker)

Deploy the container and set `PUBLIC_ORIGIN=https://YOUR_NETLIFY_SITE.netlify.app`.
