// Package playback contains the pure, deterministic calculations that
// decide "what is playing right now". It has no database or HTTP
// dependency, which is what makes it trivially unit-testable without a
// clock, a server, or a browser: every function takes "now" as an explicit
// argument instead of calling time.Now() internally.
//
// Why deterministic (timestamp-based) rather than timer-chained:
//
// A naive implementation schedules "play M1 for 10s, then setTimeout to
// play M2 for 20s, then...". That approach drifts and breaks under
// anything that pauses JS execution: a backgrounded browser tab, a slow
// render, a refresh, a dropped request. Recovering requires re-deriving
// state from scratch anyway, so the calculation is done that way from the
// start: playback position is always computed as a pure function of
// (playlist, cycle start time, current time). Refresh, tab-switch, and
// clock drift are non-events - the same formula just gets evaluated again
// and returns the same correct answer.
package playback

import "time"

// CycleSeconds is the assignment's fixed 5-hour play window that each
// window's playlist repeats within.
//
// This is an explicit, enforced boundary, not just documentation: playback
// position resets to the start of the playlist at every 5-hour boundary
// (t = 0, 18000s, 36000s, ...), matching the assignment's own framing
// ("each window restarts its media list after the sequence ends"). Within
// one 5-hour window the configured playlist loops as many times as fits
// (assignment section 10: loop, never auto-insert blank) - so for the
// common case of a playlist much shorter than 5 hours, this is invisible
// during normal play and only matters at the boundary itself, where the
// cycle deliberately restarts from position 0 rather than continuing
// wherever the loop happened to be.
//
// Edge case, documented rather than silently mishandled: if a configured
// playlist's own total duration exceeds 5 hours, only the first 5 hours of
// it would ever play, on repeat - not a scenario the seed data or
// assignment examples exercise, but worth knowing if playlists grow long.
const CycleSeconds = 5 * 60 * 60

// CycleDuration is CycleSeconds as a time.Duration, for arithmetic below.
var CycleDuration = time.Duration(CycleSeconds) * time.Second

// CycleStart is the fixed reference epoch every window's playhead is
// computed relative to. Using the Unix epoch (rather than "server startup
// time" or "first request time") means the calculation is stateless and
// gives identical results across server restarts, multiple backend
// instances, and every connected browser tab - nobody needs to agree on or
// persist "when did this cycle begin".
var CycleStart = time.Unix(0, 0).UTC()

// PlaylistItem is the minimal shape playback needs: an identifier plus a
// duration. Kept separate from models.Media so this package has zero
// dependency on the rest of the backend.
type PlaylistItem struct {
	MediaID         string
	DurationSeconds int
}

// Position describes what should be playing at a given moment.
type Position struct {
	Index           int // index into the playlist slice
	MediaID         string
	ElapsedInItem   time.Duration // how far into the current item we are
	RemainingInItem time.Duration
}

// CurrentPosition returns what should be playing in playlist at time now.
//
// The algorithm:
//  1. cycleElapsed = seconds since CycleStart, wrapped into [0, CycleSeconds)
//     - this is the explicit 5-hour cycle boundary. At every multiple of
//     CycleSeconds, cycleElapsed resets to 0 and so does playback,
//     regardless of where in the playlist loop it was.
//  2. elapsed = cycleElapsed wrapped again into [0, totalPlaylistDuration)
//     - within one 5-hour cycle, the playlist repeats as many times as
//     fits (assignment section 10: loop, never auto-insert blank).
//  3. walk the playlist accumulating durations until elapsed falls inside
//     an item; that item is currently playing.
//
// Returns false if the playlist is empty (caller should show
// "No media configured", not a synthetic blank item - blank is only ever
// shown when explicitly configured, per the assignment).
func CurrentPosition(playlist []PlaylistItem, now time.Time) (Position, bool) {
	total := totalDuration(playlist)
	if total <= 0 {
		return Position{}, false
	}

	elapsedSinceStart := now.Sub(CycleStart)
	if elapsedSinceStart < 0 {
		elapsedSinceStart = 0
	}
	cycleElapsed := elapsedSinceStart % CycleDuration
	elapsedInLoop := cycleElapsed % total

	var acc time.Duration
	for i, item := range playlist {
		d := time.Duration(item.DurationSeconds) * time.Second
		if elapsedInLoop < acc+d {
			return Position{
				Index:           i,
				MediaID:         item.MediaID,
				ElapsedInItem:   elapsedInLoop - acc,
				RemainingInItem: acc + d - elapsedInLoop,
			}, true
		}
		acc += d
	}
	// Floating point / rounding safety net: fall back to the last item
	// rather than panicking if accumulation doesn't perfectly reach total.
	last := playlist[len(playlist)-1]
	return Position{
		Index:           len(playlist) - 1,
		MediaID:         last.MediaID,
		ElapsedInItem:   time.Duration(last.DurationSeconds) * time.Second,
		RemainingInItem: 0,
	}, true
}

func totalDuration(playlist []PlaylistItem) time.Duration {
	var total time.Duration
	for _, item := range playlist {
		total += time.Duration(item.DurationSeconds) * time.Second
	}
	return total
}

// SyncStatus is the server-computed sync answer shared verbatim with every
// client, so all windows and all browser tabs derive identical state
// instead of each running their own timer (assignment section 15).
type SyncStatus struct {
	Active        bool
	MediaID       string
	StartedAt     time.Time
	EndsAt        time.Time
	RemainingSecs float64
}

// EvaluateSync determines whether a stored sync row is currently active as
// of now. A sync is active only while now is within
// [startedAt, startedAt+duration) - expiry is computed here, from
// timestamps, rather than trusted from a boolean flag alone, so a stale
// "active=true" row left over from a crashed cleanup job never lies about
// current status.
func EvaluateSync(active bool, mediaID string, startedAt time.Time, durationSeconds int, now time.Time) SyncStatus {
	if !active || mediaID == "" {
		return SyncStatus{Active: false}
	}
	ends := startedAt.Add(time.Duration(durationSeconds) * time.Second)
	if now.Before(startedAt) || !now.Before(ends) {
		return SyncStatus{Active: false}
	}
	return SyncStatus{
		Active:        true,
		MediaID:       mediaID,
		StartedAt:     startedAt,
		EndsAt:        ends,
		RemainingSecs: ends.Sub(now).Seconds(),
	}
}
