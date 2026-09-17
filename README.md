# EVA Bharat — Multi-Window Media Sequencer with Sync Playback

A full-stack app where multiple display windows each loop their own media
playlist inside a fixed 5-hour cycle, support dynamic playlist edits, and
can be forced into sync so every window shows the same item at once.

- **Backend:** Go (standard library `net/http`, PostgreSQL via `lib/pq`)
- **Frontend:** React + Vite (no extra state library, no UI framework)
- **Storage:** PostgreSQL

---

## 1. Architecture

```
React (Vite)
   |  HTTP/JSON, polled every 4s
   v
Go REST API  (cmd/server, internal/handlers)
   |
   v
Service layer (internal/services)  <-- validation, sync orchestration
   |
   v
Repository   (internal/repository) <-- all SQL, parameterized
   |
   v
PostgreSQL
```

```
backend/
  cmd/server/main.go          entrypoint
  internal/
    handlers/                 HTTP layer (thin, maps to services)
    services/                 business logic + validation
    repository/                all SQL
    models/                   shared types
    playback/                 pure deterministic playback + sync math
    database/                 connection + embedded migrations
  migrations/ (embedded under internal/database/migrations)
  Dockerfile
frontend/
  src/
    components/                StatusBar, SyncConsole, WindowMonitor, ...
    hooks/useSequencerState.js  polling + local clock-driven playback
    lib/playback.js             frontend mirror of the Go playback math
    services/api.js             fetch wrapper
docker-compose.yml             Postgres + backend for local dev
```

---

## 2. Playback engine (the core of the assignment)

**Problem with the obvious approach:** chaining `setTimeout(nextItem, duration)`
calls drifts and breaks the moment a browser tab is backgrounded, a render
is delayed, or the page is refreshed — you have to rebuild state from
scratch anyway.

**What this does instead:** playback position is a pure function of
`(playlist, cycle start time, current time)` — see
`backend/internal/playback/playback.go`:

```go
elapsedInLoop := (now - cycleStart) % totalPlaylistDuration
// walk the playlist accumulating durations until elapsedInLoop
// falls inside an item -> that's what's playing
```

Refresh, a stalled tab, network delay, multiple browser tabs — none of it
matters, because the answer is re-derived from scratch every time, and a
correct formula evaluated twice always gives the same answer
(`TestCurrentPosition_RefreshInvariance`).

The **frontend has the identical calculation** in `frontend/src/lib/playback.js`.
It polls `GET /api/state` every 4s for the source of truth (playlists, sync,
server clock), then ticks locally every 500ms, recomputing "what's playing"
from the last-known playlists using a clock corrected against the server's
clock (`offset = server_time - Date.now()`). This gives a smooth per-second
countdown without hammering the backend, and if the backend goes
unreachable, the last known playlists keep playing locally while a banner
tells the evaluator the connection was lost — it never blanks the screen.

## 3. The 5-hour cycle

Playback position resets to the **start of the playlist at every 5-hour
boundary** (t = 0, 18000s, 36000s, ...) from the fixed epoch reference —
this is an explicit, enforced boundary in `internal/playback/playback.go`
(`CycleSeconds` / `CycleDuration`), not just a comment. Within one 5-hour
window, the configured playlist loops as many times as fits — a playlist
shorter than 5 hours is never auto-padded with blank; blank only plays if
you explicitly add a `type: "blank"` item (seed data does this on Window 3,
after `M6`, to prove the point).

```go
cycleElapsed := (now - CycleStart) % CycleDuration   // wraps every 5 hours
elapsedInLoop := cycleElapsed % totalPlaylistDuration  // playlist repeats within that
```

`TestCurrentPosition_FiveHourCycleResets` in `playback_test.go` verifies
this directly: playback just before t=18000s is mid-loop as expected, and
exactly at t=18000s it hard-resets to item 0 — a state an unbounded
`elapsed % totalDuration` calculation would never reach, since it would
just keep looping the playlist forever with no boundary at all. The
frontend's `lib/playback.js` mirrors this exact two-step modulo so both
sides always agree.

*(Edge case, documented rather than silently mishandled: if a single
playlist's own total duration exceeds 5 hours, only the first 5 hours of
it would ever play, on repeat. Not a scenario the seed data exercises.)*

## 4. Synchronization

Sync is one row in `sync_state` (`media_id`, `started_at`, `duration_seconds`,
`active`) — **not** a list of window-side timers. Starting a sync writes
that row; every client (every window, every browser tab) computes
`active = now ∈ [started_at, started_at + duration)` from the *same*
timestamps, returned verbatim by the API:

```json
{
  "active": true,
  "media": { "id": "M2", ... },
  "started_at": "2026-09-17T12:00:00Z",
  "ends_at": "2026-09-17T12:00:30Z"
}
```

This is why sync can never drift between windows: nobody is running their
own countdown, everyone is asking "is `now` inside this interval?"

Sync **never touches `playlist_items`**. It's applied only at read time —
`services.buildWindowSnapshot` overlays the sync media on top of the
normal playback calculation, which keeps running underneath, unused, the
whole time sync is active. The instant sync expires, the normal
calculation is already correct — nothing needs to be "resumed."
`TestSync_StartQueryAndExpire` proves the underlying playlists are
byte-for-byte unchanged in the database after a full sync + expiry cycle.

Polling (not WebSockets) is used to detect sync start/expiry and playlist
changes, per the assignment's explicit guidance — it's simple, easy to
explain, and sufficient at a several-second cadence.

## 5. Database schema

```
windows(id, name)
media(id, name, type CHECK image|video|blank, url, duration_seconds CHECK > 0)
playlist_items(id, window_id FK, media_id FK, position, UNIQUE(window_id, position))
sync_state(id=1 fixed row, media_id, started_at, duration_seconds, active)
```

Concurrent "add media to the same window" requests are serialized by
locking the parent `windows` row (`SELECT ... FOR UPDATE`) inside a
transaction before computing `MAX(position)+1` — the second request simply
waits for the first to commit, so two simultaneous adds always get
distinct positions (`TestAddMediaToPlaylist_ConcurrentAppendsGetDistinctPositions`,
run with `go test -race`).

## 6. Seed data

Matches the assignment's example shape:

| Window | Playlist |
|---|---|
| W1 | M1 → M2 → M3 |
| W2 | M2 → M4 → M5 |
| W3 | M1 → M5 → M6 → BLANK |

Media uses real, publicly reachable URLs (Picsum photos for images,
Google's public sample-video bucket for video) so the deployed demo shows
real content immediately.

## 7. API

All responses are JSON; errors are `{"error": "message"}`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | liveness |
| GET | `/ready` | liveness + DB check |
| GET | `/api/state` | **main polling endpoint** — server time, all windows (with playlists), sync |
| GET | `/api/windows` | list windows + playlists |
| GET | `/api/windows/:id` | one window |
| GET | `/api/windows/:id/playlist` | just that window's playlist |
| POST | `/api/windows/:id/playlist` | `{"media_id": "M4"}` → append to playlist |
| GET | `/api/media` | list media catalog |
| POST | `/api/media` | create media `{name, type, url, duration_seconds}` |
| POST | `/api/sync` | `{"media_id": "M2", "duration_seconds": 30}` → start sync |
| GET | `/api/sync` | current sync status |
| DELETE | `/api/sync` | clear sync early |

Status codes: `200/201/204` success, `400` validation, `404` unknown
window/media, `500` internal (never leaks SQL/internals).

## 8. Local setup

Requires Go 1.22+, Node 20+, PostgreSQL.

```bash
# 1. Database
createdb eva_media

# 2. Backend
cd backend
cp .env.example .env    # edit DATABASE_URL if needed
export $(cat .env | xargs)
go run ./cmd/server      # migrates + seeds automatically, listens on :8080

# 3. Frontend (separate terminal)
cd frontend
cp .env.example .env.local
npm install
npm run dev               # http://localhost:5173
```

Or with Docker Compose (Postgres + backend only; run the frontend with
`npm run dev` for hot reload):

```bash
docker compose up --build
```

## 9. Tests

```bash
cd backend
gofmt -l .                 # should print nothing
go vet ./...
go test ./...               # unit tests always run
TEST_DATABASE_URL="postgres://eva:eva_dev_pw@localhost:5432/eva_media_test?sslmode=disable" \
  go test ./... -race       # add integration tests against a real Postgres
```

`internal/playback` has pure unit tests (no DB) for ordering, looping vs.
auto-blanking, refresh-invariance, and sync expiry math.
`internal/services` has integration tests against real Postgres for
playlist persistence, concurrent appends, the full sync lifecycle
(start → active on every window → expire → playlists unchanged), and
input validation (unknown window/media, bad duration, bad type).

```bash
cd frontend
npm install
npm run build    # production build
npm run lint      # static checks
```

## 10. Docker

```bash
cd backend
docker build -t eva-media-backend .
docker run -p 8080:8080 \
  -e DATABASE_URL="postgres://..." \
  -e PORT=8080 \
  -e ALLOWED_ORIGIN="https://your-frontend.example.com" \
  eva-media-backend
```

*(The Dockerfile follows the standard Go multi-stage pattern — build in
`golang:1.22-alpine`, run in `alpine:3.19` as a non-root user — and both
`PORT`/`DATABASE_URL` are read from the environment, never hardcoded. The
actual `docker build`/`docker run` has not been executed in this
environment, which has no Docker daemon; the binary itself is built and
tested directly with `go build`/`go test` instead — see section 9.)*

## 11. Environment variables

**Backend** (`backend/.env.example`): `PORT`, `DATABASE_URL`, `ALLOWED_ORIGIN`
(defaults to `http://localhost:5173` if unset — deliberately not a
wildcard, so a deployment doesn't silently end up wide open; set it to
your deployed frontend's URL in production)
**Frontend** (`frontend/.env.example`): `VITE_API_URL`

## 12. Deployment

The backend is a single stateless binary + Postgres, so any of these work
well on a free tier. **Note: this has not yet been deployed live** — the
steps below are what deploying it involves; run them to get real URLs
and fill in the Live URLs section below.

**Backend + Postgres (Render, Railway, Fly.io — pick one):**
1. Push this repo to GitHub.
2. Create a PostgreSQL instance on the host; copy its connection string.
3. Create a new Web Service from the `backend/` directory using the
   included `Dockerfile`.
4. Set env vars: `DATABASE_URL` (from step 2), `PORT` (host usually sets
   this automatically), `ALLOWED_ORIGIN` = your frontend's deployed URL.
5. Deploy. Confirm `GET https://<backend-url>/health` returns `{"status":"ok"}`.
   Migrations + seed data run automatically on startup — confirmed
   locally: `internal/database.Migrate` runs on every boot and is
   idempotent (`IF NOT EXISTS` / `ON CONFLICT DO NOTHING` throughout).

**Frontend (Vercel, Netlify, or Render Static Site):**
1. New site from `frontend/` directory.
2. Build command `npm run build`, publish directory `dist`.
3. Set env var `VITE_API_URL` = your deployed backend URL.
4. Deploy. Confirm the dashboard loads and shows windows.

**Live URLs:**
Frontend: _not yet deployed — fill in after running the steps above_
Backend: _not yet deployed — fill in after running the steps above_

## 13. Assumptions & tradeoffs

- **Blank is opt-in.** A playlist shorter than 5 hours loops; it never
  auto-fills with blank. Documented in section 3 above and in code
  comments in `playback.go`.
- **Cycle start is the Unix epoch**, not server-boot or first-request
  time, so the calculation needs no persisted "when did this cycle begin"
  state and is identical across restarts/instances/tabs.
- **Polling over WebSockets**, per the assignment's own preference — a
  4-second poll plus a 500ms local tick is simple, explainable, and
  accurate enough for a media sequencer (not a real-time collab tool).
- **Video advancing is timer-driven, not `ended`-driven.** The configured
  `duration_seconds` — not the video file's natural length — governs when
  the deterministic clock moves to the next item; the `<video>` element
  loops if the file is shorter than configured, and gets swapped out
  regardless once the timer says to advance. This matches "respect
  natural duration where appropriate" while keeping the timeline
  fully deterministic and testable.
- **Media IDs are free-form strings** the caller may supply (`M1`, `M2`,
  matching the assignment's examples) or omit (auto-generated). No
  additional uniqueness scheme beyond the primary key.
- **No auth.** Out of scope per the assignment ("no unnecessary features");
  anyone with the URL can add media or trigger sync, appropriate for an
  internal evaluator demo.
- **Cycle boundary is a hard reset, not a soft rollover.** At every 5-hour
  boundary, playback resets to item 0 of the playlist regardless of where
  the loop was — this is a deliberate reading of "restarts its media list
  after the sequence ends" from the assignment's evaluation criteria, and
  is the only behavior that makes the 5-hour figure an enforced boundary
  rather than an arbitrary number nobody ever hits. In practice, since
  every included playlist is far shorter than 5 hours, this only matters
  once every 5 hours of continuous uptime and is invisible the rest of the
  time.
- **Adding media to a playlist can shift its phase.** Because playback
  position is recomputed from scratch as `elapsed % totalDuration`,
  appending an item changes `totalDuration`, which can shift which item
  appears to be "currently playing" the instant the add takes effect —
  there's no special-cased "don't disturb what's on screen" logic. This
  is an accepted consequence of the fully stateless, timestamp-driven
  design (the same property that makes refresh/reconnect trivially
  correct); it settles immediately and never produces a wrong or stuck
  state, just a possible jump at the moment of the edit.
- **Sync duration is capped at 1 hour** as a sanity bound, not an
  assignment requirement — prevents an accidental "forever" sync.

## 14. Quick demo (5 minutes)

1. Open the deployed frontend. Confirm the status bar says "Backend
   connected" and "Windows: 3."
2. Watch Window 1, 2, 3 — each cycles through its own playlist
   independently (different current media, different countdowns).
3. Expand a window's playlist strip — the current item is highlighted.
4. Click **+ Add media** on Window 1 → "Use existing" → pick a media item
   → Add to playlist. Confirm it appears at the end of the filmstrip
   within a few seconds (next poll).
5. In the Sync control panel, pick **M2** and **30 sec**, click **Sync now**.
6. Confirm every window immediately shows M2, each marked
   "SYNCHRONIZED," with a shared countdown.
7. Wait ~30 seconds. Confirm every window returns to its own normal
   sequence.
8. Refresh the browser. Confirm playlists (including the item you added
   in step 4) are still there — proving persistence, not just in-memory
   state.
