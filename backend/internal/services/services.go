// Package services holds business logic that sits between HTTP handlers
// and the repository: turning stored playlists + current time into "what
// should the client see right now", validating input, and orchestrating
// sync start/stop. Handlers stay thin; SQL stays in the repository.
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/evabharat/media-sequencer/internal/models"
	"github.com/evabharat/media-sequencer/internal/playback"
	"github.com/evabharat/media-sequencer/internal/repository"
)

var (
	ErrValidation = errors.New("validation error")
	ErrNotFound   = repository.ErrNotFound
	ErrConflict   = repository.ErrConflict
)

// Clock is injected so tests can control "now" instead of depending on
// wall-clock time; production wires time.Now.
type Clock func() time.Time

type Service struct {
	repo  *repository.Repository
	clock Clock
}

func New(repo *repository.Repository, clock Clock) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{repo: repo, clock: clock}
}

// -----------------------------------------------------------------------
// State snapshot (what the frontend polls)
// -----------------------------------------------------------------------

// WindowSnapshot is one window's current playback answer, already
// combining normal playback with any active sync override.
type WindowSnapshot struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Playlist      []models.Media `json:"playlist"`
	CurrentIndex  int            `json:"current_index"`
	CurrentMedia  *models.Media  `json:"current_media"`
	RemainingSecs float64        `json:"remaining_seconds"`
	Synchronized  bool           `json:"synchronized"`
}

// StateSnapshot is the payload for GET /api/state: everything a client
// needs to render every window plus sync, from one request.
type StateSnapshot struct {
	ServerTime time.Time        `json:"server_time"`
	Windows    []WindowSnapshot `json:"windows"`
	Sync       models.SyncState `json:"sync"`
}

// GetState computes the full snapshot. All windows and the sync status are
// evaluated against the *same* `now` value, so a window's "synchronized"
// flag and the top-level sync object are always consistent with each
// other even under concurrent requests.
func (s *Service) GetState(ctx context.Context) (*StateSnapshot, error) {
	now := s.clock()

	windows, err := s.repo.ListWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list windows: %w", err)
	}

	syncRow, err := s.repo.GetSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("get sync: %w", err)
	}
	syncStatus := evaluateSyncRow(syncRow, now)

	var syncMedia *models.Media
	if syncStatus.Active {
		m, err := s.repo.GetMedia(ctx, syncStatus.MediaID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("load sync media: %w", err)
		}
		syncMedia = m
	}

	snapshots := make([]WindowSnapshot, 0, len(windows))
	for _, w := range windows {
		snapshots = append(snapshots, buildWindowSnapshot(w, now, syncStatus, syncMedia))
	}

	return &StateSnapshot{
		ServerTime: now,
		Windows:    snapshots,
		Sync:       toSyncState(syncStatus, syncMedia),
	}, nil
}

// GetWindow returns a single window's snapshot (used by GET
// /api/windows/:id/playlist and similar single-window views).
func (s *Service) GetWindow(ctx context.Context, windowID string) (*WindowSnapshot, error) {
	now := s.clock()
	w, err := s.repo.GetWindow(ctx, windowID)
	if err != nil {
		return nil, err
	}
	syncRow, err := s.repo.GetSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("get sync: %w", err)
	}
	syncStatus := evaluateSyncRow(syncRow, now)

	var syncMedia *models.Media
	if syncStatus.Active {
		syncMedia, _ = s.repo.GetMedia(ctx, syncStatus.MediaID)
	}

	snap := buildWindowSnapshot(*w, now, syncStatus, syncMedia)
	return &snap, nil
}

func buildWindowSnapshot(w models.Window, now time.Time, sync playback.SyncStatus, syncMedia *models.Media) WindowSnapshot {
	playlist := make([]models.Media, len(w.Playlist))
	items := make([]playback.PlaylistItem, len(w.Playlist))
	for i, entry := range w.Playlist {
		playlist[i] = entry.Media
		items[i] = playback.PlaylistItem{MediaID: entry.Media.ID, DurationSeconds: entry.Media.DurationSeconds}
	}

	snap := WindowSnapshot{
		ID:       w.ID,
		Name:     w.Name,
		Playlist: playlist,
	}

	// Sync override takes precedence over normal playback, but normal
	// playback is still computed underneath (assignment section 18): we
	// simply don't use its result for display while sync is active. The
	// underlying position naturally becomes correct again the instant
	// sync expires, because it was never stopped or overwritten - it was
	// only computed and then not shown.
	pos, ok := playback.CurrentPosition(items, now)

	if sync.Active && syncMedia != nil {
		snap.CurrentMedia = syncMedia
		snap.Synchronized = true
		snap.RemainingSecs = sync.RemainingSecs
		snap.CurrentIndex = -1 // sync media may not even be in this window's playlist
		return snap
	}

	if !ok {
		snap.CurrentMedia = nil
		snap.CurrentIndex = -1
		return snap
	}

	snap.CurrentIndex = pos.Index
	snap.CurrentMedia = &playlist[pos.Index]
	snap.RemainingSecs = pos.RemainingInItem.Seconds()
	return snap
}

func evaluateSyncRow(row *repository.SyncRow, now time.Time) playback.SyncStatus {
	if row == nil || !row.Active || row.MediaID == nil || row.StartedAt == nil || row.DurationSeconds == nil {
		return playback.SyncStatus{Active: false}
	}
	return playback.EvaluateSync(true, *row.MediaID, *row.StartedAt, *row.DurationSeconds, now)
}

func toSyncState(status playback.SyncStatus, media *models.Media) models.SyncState {
	if !status.Active {
		return models.SyncState{Active: false}
	}
	started := status.StartedAt
	ends := status.EndsAt
	duration := int(status.EndsAt.Sub(status.StartedAt).Seconds())
	return models.SyncState{
		Active:          true,
		Media:           media,
		StartedAt:       &started,
		DurationSeconds: &duration,
		EndsAt:          &ends,
	}
}

// -----------------------------------------------------------------------
// Media & playlist mutation
// -----------------------------------------------------------------------

type CreateMediaInput struct {
	ID              string
	Name            string
	Type            string
	URL             string
	DurationSeconds int
}

// CreateMedia validates and persists a new media item. If ID is empty, one
// is generated - the assignment leaves media identifiers to the
// implementation, and auto-generating avoids client-side ID collisions.
func (s *Service) CreateMedia(ctx context.Context, in CreateMediaInput) (*models.Media, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	mediaType := models.MediaType(in.Type)
	if !mediaType.IsValid() {
		return nil, fmt.Errorf("%w: type must be image, video, or blank", ErrValidation)
	}
	if in.DurationSeconds <= 0 {
		return nil, fmt.Errorf("%w: duration_seconds must be positive", ErrValidation)
	}
	if mediaType != models.MediaBlank && in.URL == "" {
		return nil, fmt.Errorf("%w: url is required for image/video media", ErrValidation)
	}

	id := in.ID
	if id == "" {
		id = generateID("M")
	}

	return s.repo.CreateMedia(ctx, models.Media{
		ID:              id,
		Name:            in.Name,
		Type:            mediaType,
		URL:             in.URL,
		DurationSeconds: in.DurationSeconds,
	})
}

// AddMediaToPlaylist appends an existing media item to a window's
// playlist, validating both references first so the caller gets a clean
// 404 instead of a foreign-key error leaking out of the database.
func (s *Service) AddMediaToPlaylist(ctx context.Context, windowID, mediaID string) (*WindowSnapshot, error) {
	exists, err := s.repo.WindowExists(ctx, windowID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("window: %w", ErrNotFound)
	}
	if _, err := s.repo.GetMedia(ctx, mediaID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("media: %w", ErrNotFound)
		}
		return nil, err
	}

	if _, err := s.repo.AppendToPlaylist(ctx, windowID, mediaID); err != nil {
		return nil, err
	}
	return s.GetWindow(ctx, windowID)
}

// -----------------------------------------------------------------------
// Sync orchestration
// -----------------------------------------------------------------------

const (
	MinSyncDurationSeconds = 1
	MaxSyncDurationSeconds = 3600 // 1 hour ceiling: a sanity bound, not an assignment requirement.
)

// StartSync validates and begins a synchronized broadcast of mediaID
// across every window. It does not touch playlist_items at all - sync is
// purely a row in sync_state, which is exactly what makes "playlists are
// never modified by sync" true by construction rather than by convention
// (assignment sections 14/19).
func (s *Service) StartSync(ctx context.Context, mediaID string, durationSeconds int) (*models.SyncState, error) {
	if durationSeconds < MinSyncDurationSeconds || durationSeconds > MaxSyncDurationSeconds {
		return nil, fmt.Errorf("%w: duration_seconds must be between %d and %d", ErrValidation, MinSyncDurationSeconds, MaxSyncDurationSeconds)
	}
	media, err := s.repo.GetMedia(ctx, mediaID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("media: %w", ErrNotFound)
		}
		return nil, err
	}

	now := s.clock()
	if err := s.repo.StartSync(ctx, mediaID, durationSeconds, now); err != nil {
		return nil, err
	}

	status := playback.EvaluateSync(true, mediaID, now, durationSeconds, now)
	state := toSyncState(status, media)
	return &state, nil
}

func (s *Service) GetSync(ctx context.Context) (*models.SyncState, error) {
	row, err := s.repo.GetSync(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	status := evaluateSyncRow(row, now)
	if !status.Active {
		state := models.SyncState{Active: false}
		return &state, nil
	}
	media, err := s.repo.GetMedia(ctx, status.MediaID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	state := toSyncState(status, media)
	return &state, nil
}

func (s *Service) ClearSync(ctx context.Context) error {
	return s.repo.ClearSync(ctx)
}

// -----------------------------------------------------------------------
// Read-only passthroughs
// -----------------------------------------------------------------------

func (s *Service) ListWindows(ctx context.Context) ([]models.Window, error) {
	return s.repo.ListWindows(ctx)
}

func (s *Service) ListMedia(ctx context.Context) ([]models.Media, error) {
	return s.repo.ListMedia(ctx)
}

func generateID(prefix string) string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}
