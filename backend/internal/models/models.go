// Package models holds the plain data types shared across the backend
// layers (repository, services, handlers). They intentionally carry no
// behavior beyond JSON tags - business logic lives in internal/services and
// internal/playback.
package models

import "time"

// MediaType enumerates the kinds of media the system understands.
type MediaType string

const (
	MediaImage MediaType = "image"
	MediaVideo MediaType = "video"
	MediaBlank MediaType = "blank"
)

// IsValid reports whether t is one of the supported media types.
func (t MediaType) IsValid() bool {
	switch t {
	case MediaImage, MediaVideo, MediaBlank:
		return true
	default:
		return false
	}
}

// Media is a single playable item.
type Media struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            MediaType `json:"type"`
	URL             string    `json:"url"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

// PlaylistEntry is one slot in a window's playlist: a media item at a
// specific, gap-tolerant position.
type PlaylistEntry struct {
	Position int   `json:"position"`
	Media    Media `json:"media"`
}

// Window is a single display window with its own independent playlist.
type Window struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Playlist []PlaylistEntry `json:"playlist"`
}

// SyncState is the single shared synchronization override. Active is false
// when no sync has ever run, or the most recent sync has expired.
//
// StartedAt/DurationSeconds are the source of truth; EndsAt is derived and
// included purely for client convenience (see playback.SyncStatus).
type SyncState struct {
	Active          bool       `json:"active"`
	Media           *Media     `json:"media,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
}
