# EventMap (MVP)

Event management web app with a map UI:

- Go backend (`net/http`) with JWT auth + basic RBAC
- Event + session + participant APIs
- Background (async) notifications + analytics workers (goroutines)
- Leaflet + OpenStreetMap tiles frontend

## Run

```bash
mkdir -p /tmp/go-cache /tmp/go-modcache
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-modcache go run ./cmd/server
```

## CSV “Database” (local persistence)

By default the server stores data in CSV files under `data/` (relative to where the server runs).

Set a custom location:

```bash
export CSV_DB_DIR=./data
```

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

Open `http://localhost:8080`.

## Roles

- Register as `attendee` or `organizer`
- Only `organizer`/`admin` can create events and sessions

Optional default admin user:

```bash
export DEFAULT_ADMIN_USERNAME=admin
export DEFAULT_ADMIN_PASSWORD=changeme
```

## API quick peek

- `POST /api/auth/register`
- `POST /api/auth/login` → `{ token }`
- `GET /api/me`
- `GET /api/events`
- `GET /api/events/nearby?lat=..&lng=..&radius_km=..`
- `POST /api/events`
- `POST /api/events/{id}/join`
- `POST /api/events/{id}/checkin` (location-gated)
- `POST /api/events/{id}/tag` (location-gated)
- `GET/POST /api/events/{id}/sessions`
- `GET /api/events/{id}/participants`
- `GET /api/leaderboard`
- `GET /api/events/{id}/leaderboard`

## Docker (deployment-ready)

```bash
docker build -t eventmap .
docker run --rm -p 8080:8080 \
  -e PUBLIC_ORIGIN=http://localhost:8080 \
  -e DEFAULT_ADMIN_USERNAME=admin \
  -e DEFAULT_ADMIN_PASSWORD=changeme \
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
