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
export DEFAULT_ADMIN_EMAIL=admin@example.com
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
