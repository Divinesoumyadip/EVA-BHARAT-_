// This mirrors internal/playback/playback.go's CurrentPosition and
// EvaluateSync functions exactly, using the same reference epoch (Unix
// time zero) as the cycle start and the same explicit 5-hour cycle
// boundary.
//
// Why the frontend recomputes this instead of just displaying whatever
// GET /api/state last returned: polling happens every few seconds, but
// "what's currently playing" should update every second (for the visible
// countdown) without hammering the backend. Because the calculation is a
// pure function of (playlist, cycle start, now), running it locally with
// a clock corrected against the server's clock gives frame-accurate
// display between polls, and - just as importantly - survives a browser
// refresh or a backgrounded tab with zero special-case recovery code: the
// same formula, evaluated again, is always correct.

export const CYCLE_START_MS = 0;
export const CYCLE_MS = 5 * 60 * 60 * 1000; // 5 hours, matching backend CycleDuration

/**
 * @param {{id: string, duration_seconds: number}[]} playlist
 * @param {number} nowMs - epoch milliseconds, ideally clock-corrected against the server
 * @returns {{index: number, mediaId: string, elapsedMs: number, remainingMs: number} | null}
 */
export function currentPosition(playlist, nowMs) {
  if (!playlist || playlist.length === 0) return null;

  const totalMs = playlist.reduce((sum, item) => sum + item.duration_seconds * 1000, 0);
  if (totalMs <= 0) return null;

  let elapsedSinceStart = nowMs - CYCLE_START_MS;
  if (elapsedSinceStart < 0) elapsedSinceStart = 0;
  // Explicit 5-hour cycle boundary first (resets to playlist start at
  // every multiple of CYCLE_MS), then the playlist's own loop within it -
  // same nested-modulo as the backend, so both sides always agree.
  const cycleElapsed = elapsedSinceStart % CYCLE_MS;
  const elapsedInLoop = cycleElapsed % totalMs;

  let acc = 0;
  for (let i = 0; i < playlist.length; i++) {
    const durationMs = playlist[i].duration_seconds * 1000;
    if (elapsedInLoop < acc + durationMs) {
      return {
        index: i,
        mediaId: playlist[i].id,
        elapsedMs: elapsedInLoop - acc,
        remainingMs: acc + durationMs - elapsedInLoop,
      };
    }
    acc += durationMs;
  }

  // Rounding safety net, mirroring the Go implementation.
  const last = playlist[playlist.length - 1];
  return {
    index: playlist.length - 1,
    mediaId: last.id,
    elapsedMs: last.duration_seconds * 1000,
    remainingMs: 0,
  };
}

/**
 * @param {{active: boolean, media?: {id: string}, started_at?: string, ends_at?: string} | null} sync
 * @param {number} nowMs
 */
export function evaluateSync(sync, nowMs) {
  if (!sync || !sync.active || !sync.media || !sync.ends_at || !sync.started_at) {
    return { active: false };
  }
  const startedMs = new Date(sync.started_at).getTime();
  const endsMs = new Date(sync.ends_at).getTime();
  if (nowMs < startedMs || nowMs >= endsMs) {
    return { active: false };
  }
  return {
    active: true,
    media: sync.media,
    remainingMs: endsMs - nowMs,
  };
}

/** Formats milliseconds as M:SS for the countdown display. */
export function formatCountdown(ms) {
  const totalSeconds = Math.max(0, Math.ceil(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
