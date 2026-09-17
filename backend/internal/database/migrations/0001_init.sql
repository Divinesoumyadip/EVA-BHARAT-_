-- 0001_init.sql
-- Core schema for the Multi-Window Media Sequencer.
--
-- Design notes:
--   * "position" on playlist_items defines deterministic ordering within a
--     window's playlist. It is unique per window so two items can never
--     tie for the same slot (this is what prevents the classic "two
--     concurrent inserts fight over slot 3" race condition, see the
--     repository layer for how positions are assigned inside a transaction).
--   * sync_state is a single-row table (id is always 1). Storing sync as a
--     row with started_at/duration_seconds, rather than "started_at + ends_at
--     in application memory", is what lets every backend replica and every
--     browser tab compute the exact same "is sync active / how long left"
--     answer from one shared source of truth. See internal/playback for the
--     calculation.

CREATE TABLE IF NOT EXISTS windows (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    type             TEXT NOT NULL CHECK (type IN ('image', 'video', 'blank')),
    url              TEXT NOT NULL DEFAULT '',
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS playlist_items (
    id         BIGSERIAL PRIMARY KEY,
    window_id  TEXT NOT NULL REFERENCES windows(id) ON DELETE CASCADE,
    media_id   TEXT NOT NULL REFERENCES media(id) ON DELETE RESTRICT,
    position   INTEGER NOT NULL CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (window_id, position)
);

CREATE INDEX IF NOT EXISTS idx_playlist_items_window
    ON playlist_items (window_id, position);

-- Single-row table holding the current (or most recent) sync override.
-- active=false rows are kept for audit/debugging but ignored by playback.
CREATE TABLE IF NOT EXISTS sync_state (
    id               INTEGER PRIMARY KEY DEFAULT 1,
    media_id         TEXT REFERENCES media(id) ON DELETE RESTRICT,
    started_at       TIMESTAMPTZ,
    duration_seconds INTEGER CHECK (duration_seconds IS NULL OR duration_seconds > 0),
    active           BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT single_row CHECK (id = 1)
);

INSERT INTO sync_state (id, active) VALUES (1, false)
ON CONFLICT (id) DO NOTHING;
