# EventMap (MVP)

Event management web app with a map UI:

- Go backend (`net/http`) with JWT auth + basic RBAC
- Optional Supabase Auth (Google / Gmail login)
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

## Supabase Auth (Google / Gmail)

EventMap can use Supabase Auth for Google login and then send the Supabase access token as a `Bearer` token to the Go API.

1) Create a Supabase project.
2) In Supabase Auth providers, enable Google and set redirect URLs to your app origin (local: `http://localhost:8080`).
3) Configure the server:

```bash
export AUTH_PROVIDER=supabase
export SUPABASE_URL="https://YOUR_PROJECT.supabase.co"
export SUPABASE_ANON_KEY="YOUR_SUPABASE_ANON_KEY"
export SUPABASE_JWT_SECRET="YOUR_SUPABASE_JWT_SECRET"

# Optional role allow-lists (comma-separated, case-insensitive)
export ADMIN_EMAILS="admin@gmail.com"
export ORGANIZER_EMAILS="organizer@gmail.com,other@gmail.com"
```

Notes:
- `SUPABASE_ANON_KEY` is public and is served to the browser via `/config.js`.
- The API ignores Supabase token `role` claims for app RBAC; admin/organizer access is only granted via the allow-lists above.

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

## Docker (deployment-ready)

```bash
docker build -t eventmap .
docker run --rm -p 8080:8080 \
  -e AUTH_PROVIDER=supabase \
  -e PUBLIC_ORIGIN=http://localhost:8080 \
  -e SUPABASE_URL="$SUPABASE_URL" \
  -e SUPABASE_ANON_KEY="$SUPABASE_ANON_KEY" \
  -e SUPABASE_JWT_SECRET="$SUPABASE_JWT_SECRET" \
  eventmap
```
