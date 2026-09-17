# EVA Bharat — Multi-Window Media Sequencer with Sync Playback

A full-stack app where multiple display windows each loop their own media playlist inside a fixed 5-hour cycle, support dynamic playlist edits, and can be forced into sync so every window shows the same item at once.

## Tech Stack

- **Backend:** Go (standard library `net/http`, PostgreSQL via `lib/pq`)
- **Frontend:** React + Vite
- **Storage:** PostgreSQL

## Architecture

```text
React (Vite)
   |
   | HTTP/JSON, polled every 4s
   v
Go REST API
   |
   v
Service Layer
   |
   v
Repository
   |
   v
PostgreSQL
```

## Playback Engine

Playback position is a pure deterministic function of:

* Playlist
* Cycle start time
* Current time

The frontend mirrors the same playback calculation as the Go backend.

The frontend polls `GET /api/state` every 4 seconds and uses the server clock to keep playback synchronized across windows.

## 5-Hour Cycle

Playback uses an explicit 5-hour cycle.

```go
cycleElapsed := (now - CycleStart) % CycleDuration
elapsedInLoop := cycleElapsed % totalPlaylistDuration
```

At every 5-hour boundary, playback resets to the beginning of the playlist.

A playlist shorter than 5 hours repeats within the cycle. Blank media is only played when explicitly included in the playlist.

## Synchronization

Sync is stored centrally in PostgreSQL using:

```text
media_id
started_at
duration_seconds
active
```

When sync starts, every client receives the same server timestamps and calculates whether the sync interval is currently active.

Sync does not modify the underlying playlists.

After the sync duration expires, each window automatically returns to its normal playlist position.

The application uses polling instead of WebSockets. This keeps the architecture simple and reliable for the assignment.

## Database Schema

```text
windows
media
playlist_items
sync_state
```

Concurrent playlist additions are protected using a transaction and row locking so simultaneous additions receive distinct positions.

## Seed Data

The deployed application contains the assignment-style seed playlists:

| Window | Playlist             |
| ------ | --------------------- |
| W1     | M1 → M2 → M3          |
| W2     | M2 → M4 → M5           |
| W3     | M1 → M5 → M6 → BLANK  |

Media uses publicly reachable URLs so the deployed demo can display real content immediately.

## API

| Method | Endpoint                    | Purpose                    |
| ------ | ---------------------------- | --------------------------- |
| GET    | `/health`                    | Liveness check              |
| GET    | `/ready`                     | Liveness + database check   |
| GET    | `/api/state`                 | Main polling endpoint       |
| GET    | `/api/windows`               | List windows and playlists  |
| GET    | `/api/windows/:id`           | Get one window              |
| GET    | `/api/windows/:id/playlist`  | Get one playlist            |
| POST   | `/api/windows/:id/playlist`  | Append media to playlist    |
| GET    | `/api/media`                 | List media catalog          |
| POST   | `/api/media`                 | Create media                |
| POST   | `/api/sync`                  | Start synchronization       |
| GET    | `/api/sync`                  | Get current sync status     |
| DELETE | `/api/sync`                  | Clear active sync           |

All responses are JSON.

Success status codes include `200`, `201`, and `204`.

Validation errors return `400`, unknown resources return `404`, and internal errors return `500`.

## Local Setup

### Requirements

* Go 1.22+
* Node.js 20+
* PostgreSQL

### Backend

```bash
cd backend
cp .env.example .env
```

Configure `DATABASE_URL` in `.env`, then:

```bash
go run ./cmd/server
```

The backend runs on:

```text
http://localhost:8080
```

Migrations and seed data are applied automatically on startup.

### Frontend

Open another terminal:

```bash
cd frontend
cp .env.example .env.local
npm install
npm run dev
```

The frontend runs on:

```text
http://localhost:5173
```

## Docker

```bash
docker compose up --build
```

The Docker setup runs PostgreSQL and the backend. The frontend can be started separately with:

```bash
cd frontend
npm run dev
```

## Testing

### Backend

```bash
cd backend
gofmt -l .
go vet ./...
go test ./...
```

### Frontend

```bash
cd frontend
npm install
npm run build
npm run lint
```

The backend includes tests covering playback ordering, looping, the 5-hour boundary, sync lifecycle, playlist persistence, concurrent playlist additions, and input validation.

## Environment Variables

### Backend

```text
PORT
DATABASE_URL
ALLOWED_ORIGIN
```

`ALLOWED_ORIGIN` defaults to:

```text
http://localhost:5173
```

For production it should be set to the deployed frontend URL.

### Frontend

```text
VITE_API_URL
```

## Deployment

The application is deployed on Render.

### Backend

* Runtime: Docker
* Root Directory: `backend`
* Dockerfile: `Dockerfile`
* PostgreSQL: Render PostgreSQL
* Compute: Free

### Frontend

* Runtime: Render Static Site
* Root Directory: `frontend`
* Build Command:

```bash
npm install && npm run build
```

* Publish Directory:

```text
dist
```

* Compute: Free

## Live URLs

### Frontend

[https://eva-bharat-1.onrender.com](https://eva-bharat-1.onrender.com)

### Backend

[https://eva-bharat-qvko.onrender.com](https://eva-bharat-qvko.onrender.com)

### Backend Health Check

[https://eva-bharat-qvko.onrender.com/health](https://eva-bharat-qvko.onrender.com/health)

## Assumptions & Tradeoffs

* **Blank is opt-in.** A playlist shorter than 5 hours loops and is never automatically padded with blank.
* The cycle uses a fixed epoch-based reference so playback remains deterministic across refreshes and restarts.
* Polling is used instead of WebSockets because the assignment allows it and it is simpler to explain and maintain.
* Video advancement is controlled by the configured `duration_seconds`.
* Sync overlays the normal playlist without modifying playlist data.
* No authentication is included because it is outside the assignment scope.
* Sync duration is capped at one hour as a sanity bound.
* Adding media can change the calculated playlist phase because playback is derived from the current playlist and elapsed time.

## Quick Demo

1. Open the deployed frontend.
2. Confirm the status bar shows **Backend connected** and **Windows: 3**.
3. Observe Windows 1, 2, and 3 playing their independent playlists.
4. Add media to a playlist using **+ Add media**.
5. Confirm the new media appears after the next state poll.
6. In **Sync Control**, select `M2`.
7. Select `30 sec`.
8. Click **Sync now**.
9. Confirm all windows display M2 simultaneously.
10. Wait approximately 30 seconds.
11. Confirm each window returns to its own normal sequence.
12. Refresh the browser.
13. Confirm playlist changes are still present, demonstrating persistent PostgreSQL storage.

## Project Structure

```text
backend/
├── cmd/server/main.go
├── internal/
│   ├── database/
│   ├── handlers/
│   ├── models/
│   ├── playback/
│   ├── repository/
│   └── services/
├── Dockerfile
├── go.mod
└── go.sum

frontend/
├── src/
│   ├── components/
│   ├── hooks/
│   ├── lib/
│   └── services/
├── package.json
└── vite.config.js

docker-compose.yml
README.md
```
