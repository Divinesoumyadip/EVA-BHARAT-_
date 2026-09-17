package playback

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

func TestCurrentPosition_BasicOrdering(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "M2", DurationSeconds: 20},
		{MediaID: "M3", DurationSeconds: 15},
	}

	cases := []struct {
		name      string
		offset    time.Duration
		wantMedia string
		wantIndex int
	}{
		{"start of M1", 0, "M1", 0},
		{"middle of M1", 5 * time.Second, "M1", 0},
		{"exact boundary M1->M2", 10 * time.Second, "M2", 1},
		{"middle of M2", 20 * time.Second, "M2", 1},
		{"exact boundary M2->M3", 30 * time.Second, "M3", 2},
		{"end of M3, wraps to M1", 45 * time.Second, "M1", 0},
		{"deep into second loop", 45*time.Second + 12*time.Second, "M2", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := CycleStart.Add(tc.offset)
			pos, ok := CurrentPosition(playlist, now)
			if !ok {
				t.Fatalf("expected a position, got none")
			}
			if pos.MediaID != tc.wantMedia {
				t.Errorf("media = %s, want %s", pos.MediaID, tc.wantMedia)
			}
			if pos.Index != tc.wantIndex {
				t.Errorf("index = %d, want %d", pos.Index, tc.wantIndex)
			}
		})
	}
}

// TestCurrentPosition_FiveHourCycleResets is the explicit regression test
// for the bug this review found: CycleSeconds must actually be enforced as
// a hard boundary, not just declared and left unused while the playlist's
// own total duration quietly does all the work.
func TestCurrentPosition_FiveHourCycleResets(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "M2", DurationSeconds: 20},
		{MediaID: "M3", DurationSeconds: 16},
	}
	// total = 46s, which does NOT evenly divide CycleSeconds (18000s) -
	// chosen deliberately so a hard reset at the cycle boundary is
	// actually observable and distinct from "just keep looping the
	// playlist forever," which is what the old (buggy) implementation did.

	t.Run("just before the boundary is mid-loop, not reset", func(t *testing.T) {
		now := CycleStart.Add(time.Duration(CycleSeconds-1) * time.Second) // 17999s
		pos, ok := CurrentPosition(playlist, now)
		if !ok {
			t.Fatalf("expected a position")
		}
		// 17999 % 46 = 13 -> falls in M2 (10-30)
		if pos.MediaID != "M2" {
			t.Errorf("media = %s, want M2 just before the cycle boundary", pos.MediaID)
		}
	})

	t.Run("exactly at the boundary resets to the start of the playlist", func(t *testing.T) {
		now := CycleStart.Add(time.Duration(CycleSeconds) * time.Second) // 18000s
		pos, ok := CurrentPosition(playlist, now)
		if !ok {
			t.Fatalf("expected a position")
		}
		if pos.Index != 0 || pos.MediaID != "M1" || pos.ElapsedInItem != 0 {
			t.Errorf("expected a hard reset to M1 at t=0-in-item, got %+v", pos)
		}
	})

	t.Run("second cycle behaves identically to the first at the same offset", func(t *testing.T) {
		offsets := []time.Duration{0, 5 * time.Second, 32 * time.Second, 44 * time.Second}
		for _, off := range offsets {
			first, ok1 := CurrentPosition(playlist, CycleStart.Add(off))
			second, ok2 := CurrentPosition(playlist, CycleStart.Add(CycleDuration+off))
			if !ok1 || !ok2 {
				t.Fatalf("expected both cycles to resolve a position at offset %v", off)
			}
			if first != second {
				t.Errorf("offset %v: cycle 1 = %+v, cycle 2 = %+v, want identical", off, first, second)
			}
		}
	})

	t.Run("many cycles later still resets cleanly", func(t *testing.T) {
		now := CycleStart.Add(7*CycleDuration + 3*time.Second)
		pos, ok := CurrentPosition(playlist, now)
		if !ok {
			t.Fatalf("expected a position")
		}
		want, _ := CurrentPosition(playlist, CycleStart.Add(3*time.Second))
		if pos != want {
			t.Errorf("7 cycles later = %+v, want same as 3s into cycle 0 = %+v", pos, want)
		}
	})
}

// TestCurrentPosition_LoopsRatherThanBlanking is the explicit regression
// test for assignment section 10: a playlist shorter than the 5-hour cycle
// must repeat forever within that cycle, never fall back to blank
// automatically.
func TestCurrentPosition_LoopsRatherThanBlanking(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "M2", DurationSeconds: 20},
	}
	total := 30 * time.Second

	// Jump far into the 5-hour cycle - many, many loops later - and
	// confirm we still land inside the configured playlist, never blank.
	loopsIn := CycleSeconds - 1 // 1 second before the nominal cycle boundary
	now := CycleStart.Add(time.Duration(loopsIn) * time.Second)

	pos, ok := CurrentPosition(playlist, now)
	if !ok {
		t.Fatalf("expected a position deep into the cycle, got none")
	}
	if pos.MediaID != "M1" && pos.MediaID != "M2" {
		t.Fatalf("expected configured media, got %q (blank never should appear unconfigured)", pos.MediaID)
	}

	// Sanity: the loop math should exactly match elapsed % total.
	elapsed := time.Duration(loopsIn) * time.Second
	wantElapsedInLoop := elapsed % total
	if wantElapsedInLoop >= 10*time.Second {
		if pos.MediaID != "M2" {
			t.Errorf("expected M2 at %v into the loop, got %s", wantElapsedInLoop, pos.MediaID)
		}
	} else {
		if pos.MediaID != "M1" {
			t.Errorf("expected M1 at %v into the loop, got %s", wantElapsedInLoop, pos.MediaID)
		}
	}
}

func TestCurrentPosition_EmptyPlaylist(t *testing.T) {
	_, ok := CurrentPosition(nil, CycleStart)
	if ok {
		t.Fatalf("expected ok=false for an empty playlist")
	}
}

func TestCurrentPosition_ExplicitBlankIsHonored(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "BLANK", DurationSeconds: 5},
	}
	now := CycleStart.Add(12 * time.Second)
	pos, ok := CurrentPosition(playlist, now)
	if !ok {
		t.Fatalf("expected a position")
	}
	if pos.MediaID != "BLANK" {
		t.Errorf("media = %s, want BLANK (explicitly configured)", pos.MediaID)
	}
}

// TestCurrentPosition_RefreshInvariance is the "browser refresh /
// tab-inactive / delayed timer" robustness case from assignment section 9:
// calling the function twice with the same `now` (however that `now` was
// obtained - fresh page load, reconnect after being backgrounded, etc.)
// always returns the same answer.
func TestCurrentPosition_RefreshInvariance(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "M2", DurationSeconds: 20},
		{MediaID: "M3", DurationSeconds: 15},
	}
	now := CycleStart.Add(7*time.Hour + 33*time.Minute + 41*time.Second)

	first, ok1 := CurrentPosition(playlist, now)
	second, ok2 := CurrentPosition(playlist, now)
	if !ok1 || !ok2 {
		t.Fatalf("expected both calls to succeed")
	}
	if first != second {
		t.Errorf("position differs across identical calls: %+v vs %+v", first, second)
	}
}

func TestEvaluateSync(t *testing.T) {
	started := mustTime(t, "2026-09-17T12:00:00Z")

	t.Run("before start (clock skew) is inactive", func(t *testing.T) {
		status := EvaluateSync(true, "M2", started, 30, started.Add(-1*time.Second))
		if status.Active {
			t.Errorf("expected inactive before start")
		}
	})

	t.Run("just after start is active", func(t *testing.T) {
		status := EvaluateSync(true, "M2", started, 30, started.Add(1*time.Second))
		if !status.Active {
			t.Fatalf("expected active")
		}
		if status.MediaID != "M2" {
			t.Errorf("media = %s, want M2", status.MediaID)
		}
		if status.RemainingSecs <= 28 || status.RemainingSecs > 30 {
			t.Errorf("remaining = %v, want ~29", status.RemainingSecs)
		}
	})

	t.Run("exactly at end is expired", func(t *testing.T) {
		status := EvaluateSync(true, "M2", started, 30, started.Add(30*time.Second))
		if status.Active {
			t.Errorf("expected expired exactly at boundary")
		}
	})

	t.Run("well after end is expired", func(t *testing.T) {
		status := EvaluateSync(true, "M2", started, 30, started.Add(5*time.Minute))
		if status.Active {
			t.Errorf("expected expired well after duration")
		}
	})

	t.Run("stored active=false is always inactive regardless of timestamps", func(t *testing.T) {
		status := EvaluateSync(false, "M2", started, 30, started.Add(1*time.Second))
		if status.Active {
			t.Errorf("expected inactive when active flag is false")
		}
	})

	t.Run("no media id is inactive", func(t *testing.T) {
		status := EvaluateSync(true, "", started, 30, started.Add(1*time.Second))
		if status.Active {
			t.Errorf("expected inactive with empty media id")
		}
	})
}

// TestSyncDoesNotMutatePlaylistLogic is a documentation-as-test: it proves
// CurrentPosition (the normal playback path) is entirely unaffected by
// sync - sync is applied only by the caller (see services.WindowSnapshot)
// choosing to override the display, never by mutating playlist input.
func TestSyncDoesNotMutatePlaylistLogic(t *testing.T) {
	playlist := []PlaylistItem{
		{MediaID: "M1", DurationSeconds: 10},
		{MediaID: "M2", DurationSeconds: 20},
	}
	now := CycleStart.Add(5 * time.Second)

	before, _ := CurrentPosition(playlist, now)
	_ = EvaluateSync(true, "M2", now, 30, now.Add(1*time.Second)) // sync active elsewhere
	after, _ := CurrentPosition(playlist, now)

	if before != after {
		t.Errorf("normal playback calculation changed after evaluating sync: %+v vs %+v", before, after)
	}
}
