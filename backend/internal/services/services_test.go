package services_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/evabharat/media-sequencer/internal/database"
	"github.com/evabharat/media-sequencer/internal/repository"
	"github.com/evabharat/media-sequencer/internal/services"

	_ "github.com/lib/pq"
)

// These are integration tests against a real PostgreSQL database, matching
// assignment section 33's requirement to test sync start/expiry, playlist
// persistence, dynamic updates, and invalid-input handling end to end
// (not just the pure playback math, which is covered separately in
// internal/playback).
//
// They require TEST_DATABASE_URL to be set (see Makefile / README) and are
// skipped otherwise so `go test ./...` still runs cleanly in environments
// without a database.

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	// Start every test from a clean slate so tests don't depend on
	// ordering or leak state into each other.
	for _, stmt := range []string{
		// CASCADE also empties sync_state, since it has an FK to media;
		// re-seed its single row afterwards so every test starts with
		// the expected id=1, active=false row in place.
		`TRUNCATE playlist_items RESTART IDENTITY CASCADE`,
		`TRUNCATE media CASCADE`,
		`TRUNCATE windows CASCADE`,
		`INSERT INTO sync_state (id, active) VALUES (1, false) ON CONFLICT (id) DO UPDATE SET media_id = NULL, started_at = NULL, duration_seconds = NULL, active = false`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("reset test db (%s): %v", stmt, err)
		}
	}
	return db
}

func fixedClock(t time.Time) services.Clock {
	return func() time.Time { return t }
}

func TestAddMediaToPlaylist_PersistsAndPreservesOrder(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO windows (id, name) VALUES ('W1','Window 1')`)
	if err != nil {
		t.Fatalf("seed window: %v", err)
	}
	for _, id := range []string{"M1", "M2", "M3", "M4"} {
		_, err := db.ExecContext(ctx, `INSERT INTO media (id, name, type, url, duration_seconds) VALUES ($1,$1,'image','https://x/y',10)`, id)
		if err != nil {
			t.Fatalf("seed media %s: %v", id, err)
		}
	}
	_, err = db.ExecContext(ctx, `INSERT INTO playlist_items (window_id, media_id, position) VALUES ('W1','M1',0), ('W1','M2',1), ('W1','M3',2)`)
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	svc := services.New(repo, nil)
	snap, err := svc.AddMediaToPlaylist(ctx, "W1", "M4")
	if err != nil {
		t.Fatalf("AddMediaToPlaylist: %v", err)
	}

	want := []string{"M1", "M2", "M3", "M4"}
	if len(snap.Playlist) != len(want) {
		t.Fatalf("playlist length = %d, want %d", len(snap.Playlist), len(want))
	}
	for i, id := range want {
		if snap.Playlist[i].ID != id {
			t.Errorf("playlist[%d] = %s, want %s", i, snap.Playlist[i].ID, id)
		}
	}

	// Re-fetch independently to prove it was actually persisted, not just
	// returned from in-memory state.
	reloaded, err := svc.GetWindow(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWindow: %v", err)
	}
	if len(reloaded.Playlist) != 4 {
		t.Fatalf("reloaded playlist length = %d, want 4", len(reloaded.Playlist))
	}
}

func TestAddMediaToPlaylist_ConcurrentAppendsGetDistinctPositions(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO windows (id, name) VALUES ('W1','Window 1')`); err != nil {
		t.Fatalf("seed window: %v", err)
	}
	ids := []string{"M1", "M2", "M3", "M4", "M5", "M6"}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO media (id, name, type, url, duration_seconds) VALUES ($1,$1,'image','https://x/y',10)`, id); err != nil {
			t.Fatalf("seed media %s: %v", id, err)
		}
	}

	svc := services.New(repo, nil)

	// Fire concurrent appends at the same window and confirm the
	// resulting positions are all distinct (assignment section 30).
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(i int, mediaID string) {
			defer wg.Done()
			_, err := svc.AddMediaToPlaylist(ctx, "W1", mediaID)
			errs[i] = err
		}(i, id)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent AddMediaToPlaylist failed: %v", err)
		}
	}

	snap, err := svc.GetWindow(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWindow: %v", err)
	}
	if len(snap.Playlist) != len(ids) {
		t.Fatalf("playlist length = %d, want %d (no lost or duplicated slots)", len(snap.Playlist), len(ids))
	}
	seen := map[string]bool{}
	for _, m := range snap.Playlist {
		if seen[m.ID] {
			t.Errorf("media %s appears more than once", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestSync_StartQueryAndExpire(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO windows (id, name) VALUES ('W1','Window 1'), ('W2','Window 2')`); err != nil {
		t.Fatalf("seed windows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO media (id, name, type, url, duration_seconds) VALUES ('M1','M1','image','https://x/1',10), ('M2','M2','image','https://x/2',10)`); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO playlist_items (window_id, media_id, position) VALUES ('W1','M1',0), ('W2','M1',0)`); err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	clockTime := start
	clock := func() time.Time { return clockTime }

	svc := services.New(repo, clock)

	state, err := svc.StartSync(ctx, "M2", 30)
	if err != nil {
		t.Fatalf("StartSync: %v", err)
	}
	if !state.Active || state.Media == nil || state.Media.ID != "M2" {
		t.Fatalf("expected active sync on M2, got %+v", state)
	}

	// During sync: every window must report M2, and playlists must be
	// completely unchanged in storage (assignment sections 14, 19).
	clockTime = start.Add(5 * time.Second)
	snapshot, err := svc.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState during sync: %v", err)
	}
	if !snapshot.Sync.Active {
		t.Fatalf("expected sync active in state snapshot")
	}
	for _, w := range snapshot.Windows {
		if w.CurrentMedia == nil || w.CurrentMedia.ID != "M2" {
			t.Errorf("window %s current media = %+v, want M2", w.ID, w.CurrentMedia)
		}
		if !w.Synchronized {
			t.Errorf("window %s should report synchronized=true", w.ID)
		}
	}

	w1, err := repo.GetWindow(ctx, "W1")
	if err != nil {
		t.Fatalf("GetWindow W1: %v", err)
	}
	if len(w1.Playlist) != 1 || w1.Playlist[0].Media.ID != "M1" {
		t.Fatalf("W1 playlist was mutated by sync: %+v", w1.Playlist)
	}

	// After expiry: sync clears itself based on timestamps, playback
	// returns to normal, playlists remain untouched.
	clockTime = start.Add(31 * time.Second)
	snapshot, err = svc.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState after expiry: %v", err)
	}
	if snapshot.Sync.Active {
		t.Fatalf("expected sync inactive after expiry, got %+v", snapshot.Sync)
	}
	for _, w := range snapshot.Windows {
		if w.Synchronized {
			t.Errorf("window %s still reports synchronized after expiry", w.ID)
		}
		if w.CurrentMedia == nil || w.CurrentMedia.ID != "M1" {
			t.Errorf("window %s current media = %+v, want back to M1 (normal sequence)", w.ID, w.CurrentMedia)
		}
	}
}

func TestSync_InvalidMediaIsRejected(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	svc := services.New(repo, fixedClock(time.Now()))

	_, err := svc.StartSync(context.Background(), "does-not-exist", 30)
	if err == nil {
		t.Fatalf("expected error for unknown media id")
	}
}

func TestSync_InvalidDurationIsRejected(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO media (id, name, type, url, duration_seconds) VALUES ('M1','M1','image','https://x/1',10)`); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	svc := services.New(repo, fixedClock(time.Now()))

	if _, err := svc.StartSync(ctx, "M1", 0); err == nil {
		t.Fatalf("expected error for zero duration")
	}
	if _, err := svc.StartSync(ctx, "M1", -5); err == nil {
		t.Fatalf("expected error for negative duration")
	}
}

func TestCreateMedia_ValidatesType(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	svc := services.New(repo, nil)

	_, err := svc.CreateMedia(context.Background(), services.CreateMediaInput{
		Name: "Bad", Type: "audio", URL: "https://x", DurationSeconds: 5,
	})
	if err == nil {
		t.Fatalf("expected validation error for invalid media type")
	}
}

func TestCreateMedia_ValidatesDuration(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	svc := services.New(repo, nil)

	_, err := svc.CreateMedia(context.Background(), services.CreateMediaInput{
		Name: "Bad", Type: "image", URL: "https://x", DurationSeconds: 0,
	})
	if err == nil {
		t.Fatalf("expected validation error for zero duration")
	}
}

func TestAddMediaToPlaylist_UnknownWindowIs404(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO media (id, name, type, url, duration_seconds) VALUES ('M1','M1','image','https://x/1',10)`); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	svc := services.New(repo, nil)

	_, err := svc.AddMediaToPlaylist(ctx, "does-not-exist", "M1")
	if err == nil {
		t.Fatalf("expected not-found error for unknown window")
	}
}

func TestAddMediaToPlaylist_UnknownMediaIs404(t *testing.T) {
	db := testDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO windows (id, name) VALUES ('W1','Window 1')`); err != nil {
		t.Fatalf("seed window: %v", err)
	}
	svc := services.New(repo, nil)

	_, err := svc.AddMediaToPlaylist(ctx, "W1", "does-not-exist")
	if err == nil {
		t.Fatalf("expected not-found error for unknown media")
	}
}
