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
