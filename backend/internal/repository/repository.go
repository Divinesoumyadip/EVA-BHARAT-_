// Package repository is the only layer that talks SQL. Every query is
// parameterized; nothing here ever builds SQL by string concatenation of
// caller-provided values.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/evabharat/media-sequencer/internal/models"
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned for state conflicts, e.g. duplicate IDs.
var ErrConflict = errors.New("conflict")

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// -----------------------------------------------------------------------
// Windows
// -----------------------------------------------------------------------

// ListWindows returns every window with its playlist, ordered by position.
// A single query with a JOIN keeps this to one round trip regardless of
// window/playlist size.
func (r *Repository) ListWindows(ctx context.Context) ([]models.Window, error) {
	const q = `
		SELECT w.id, w.name,
		       COALESCE(pi.position, -1) AS position,
		       m.id, m.name, m.type, m.url, m.duration_seconds, m.created_at
		FROM windows w
		LEFT JOIN playlist_items pi ON pi.window_id = w.id
		LEFT JOIN media m ON m.id = pi.media_id
		ORDER BY w.id, pi.position`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list windows: %w", err)
	}
	defer rows.Close()

	order := []string{}
	byID := map[string]*models.Window{}

	for rows.Next() {
		var (
			wID, wName              string
			position                sql.NullInt64
			mID, mName, mType, mURL sql.NullString
			mDuration               sql.NullInt64
			mCreatedAt              sql.NullTime
		)
		if err := rows.Scan(&wID, &wName, &position, &mID, &mName, &mType, &mURL, &mDuration, &mCreatedAt); err != nil {
			return nil, fmt.Errorf("scan window row: %w", err)
		}
		w, ok := byID[wID]
		if !ok {
			w = &models.Window{ID: wID, Name: wName, Playlist: []models.PlaylistEntry{}}
			byID[wID] = w
			order = append(order, wID)
		}
		if mID.Valid {
			w.Playlist = append(w.Playlist, models.PlaylistEntry{
				Position: int(position.Int64),
				Media: models.Media{
					ID:              mID.String,
					Name:            mName.String,
					Type:            models.MediaType(mType.String),
					URL:             mURL.String,
					DurationSeconds: int(mDuration.Int64),
					CreatedAt:       mCreatedAt.Time,
				},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]models.Window, 0, len(order))
	for _, id := range order {
		result = append(result, *byID[id])
	}
	return result, nil
}

// GetWindow returns a single window with its playlist.
func (r *Repository) GetWindow(ctx context.Context, id string) (*models.Window, error) {
	windows, err := r.ListWindows(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range windows {
		if w.ID == id {
			return &w, nil
		}
	}
	return nil, ErrNotFound
}

// WindowExists is a lightweight existence check, used for validating
// input before doing heavier work.
func (r *Repository) WindowExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM windows WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check window exists: %w", err)
	}
	return exists, nil
}

// -----------------------------------------------------------------------
// Media
// -----------------------------------------------------------------------

func (r *Repository) ListMedia(ctx context.Context) ([]models.Media, error) {
	const q = `SELECT id, name, type, url, duration_seconds, created_at FROM media ORDER BY id`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	var out []models.Media
	for rows.Next() {
		var m models.Media
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.URL, &m.DurationSeconds, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan media row: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) GetMedia(ctx context.Context, id string) (*models.Media, error) {
	const q = `SELECT id, name, type, url, duration_seconds, created_at FROM media WHERE id = $1`
	var m models.Media
	err := r.db.QueryRowContext(ctx, q, id).Scan(&m.ID, &m.Name, &m.Type, &m.URL, &m.DurationSeconds, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	return &m, nil
}

// CreateMedia inserts a new media item. The caller is responsible for
// generating a unique ID (see services.GenerateMediaID); a duplicate ID
// surfaces as ErrConflict via the primary key constraint.
func (r *Repository) CreateMedia(ctx context.Context, m models.Media) (*models.Media, error) {
	const q = `
		INSERT INTO media (id, name, type, url, duration_seconds)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, type, url, duration_seconds, created_at`
	var out models.Media
	err := r.db.QueryRowContext(ctx, q, m.ID, m.Name, m.Type, m.URL, m.DurationSeconds).
		Scan(&out.ID, &out.Name, &out.Type, &out.URL, &out.DurationSeconds, &out.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("create media: %w", err)
	}
	return &out, nil
}

// -----------------------------------------------------------------------
// Playlist items
// -----------------------------------------------------------------------

// AppendToPlaylist adds mediaID to the end of windowID's playlist.
//
// This runs inside a single serializable-enough transaction: it locks the
// window's existing playlist rows (SELECT ... FOR UPDATE), computes
// max(position)+1, and inserts. Locking the row set prevents two
// concurrent "add media" requests for the same window from both reading
// the same max(position) and racing to insert at the same slot - the
// second request simply waits for the first transaction to commit, then
// sees the updated max. The UNIQUE(window_id, position) constraint is a
// second line of defense if that logic is ever bypassed.
func (r *Repository) AppendToPlaylist(ctx context.Context, windowID, mediaID string) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed

	// Lock the parent window row first. This serializes concurrent
	// appends to the *same* window (the second transaction blocks here
	// until the first commits) while leaving other windows' inserts
	// completely unaffected. Locking the window row - rather than the
	// playlist_items rows, via plain "FOR UPDATE" which Postgres
	// disallows combined with an aggregate in the same SELECT - also
	// correctly serializes the very first insert into an empty playlist,
	// where there would otherwise be no playlist row yet to lock.
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM windows WHERE id = $1 FOR UPDATE`, windowID); err != nil {
		return 0, fmt.Errorf("lock window row: %w", err)
	}

	var nextPos int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(position), -1) + 1
		FROM playlist_items
		WHERE window_id = $1`, windowID).Scan(&nextPos)
	if err != nil {
		return 0, fmt.Errorf("compute next position: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO playlist_items (window_id, media_id, position)
		VALUES ($1, $2, $3)`, windowID, mediaID, nextPos)
	if err != nil {
		return 0, fmt.Errorf("insert playlist item: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return nextPos, nil
}

// -----------------------------------------------------------------------
// Sync state
// -----------------------------------------------------------------------

// StartSync writes the shared sync override. Using a fixed id=1 row (see
// migration 0001) rather than inserting a new row per sync keeps "what is
// the current sync" a trivial single-row lookup, and means starting a new
// sync atomically replaces any prior one.
func (r *Repository) StartSync(ctx context.Context, mediaID string, durationSeconds int, startedAt time.Time) error {
	const q = `
		UPDATE sync_state
		SET media_id = $1, started_at = $2, duration_seconds = $3, active = true
		WHERE id = 1`
	res, err := r.db.ExecContext(ctx, q, mediaID, startedAt, durationSeconds)
	if err != nil {
		return fmt.Errorf("start sync: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sync_state row missing (migrations not applied?)")
	}
	return nil
}

// ClearSync deactivates the sync override without deleting the row, so the
// last sync remains inspectable for debugging/demo purposes.
func (r *Repository) ClearSync(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sync_state SET active = false WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("clear sync: %w", err)
	}
	return nil
}

// SyncRow is the raw persisted sync row; the playback package turns this
// into a models.SyncState with expiry already evaluated against "now".
type SyncRow struct {
	MediaID         *string
	StartedAt       *time.Time
	DurationSeconds *int
	Active          bool
}

func (r *Repository) GetSync(ctx context.Context) (*SyncRow, error) {
	const q = `SELECT media_id, started_at, duration_seconds, active FROM sync_state WHERE id = 1`
	var row SyncRow
	err := r.db.QueryRowContext(ctx, q).Scan(&row.MediaID, &row.StartedAt, &row.DurationSeconds, &row.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return &SyncRow{Active: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sync: %w", err)
	}
	return &row, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// lib/pq exposes *pq.Error with Code "23505" for unique_violation.
	// Comparing the message substring keeps this file free of a direct
	// pq.Error type assertion import cycle concern, while still being
	// precise enough for our controlled insert paths.
	type sqlStater interface{ SQLState() string }
	var s sqlStater
	if errors.As(err, &s) {
		return s.SQLState() == "23505"
	}
	return false
}
