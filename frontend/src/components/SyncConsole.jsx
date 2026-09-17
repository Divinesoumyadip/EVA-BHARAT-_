import { useState } from "react";
import { startSync } from "../services/api";
import { formatCountdown } from "../lib/playback";

const DURATION_PRESETS = [15, 30, 60, 120];

export default function SyncConsole({ catalog, syncStatus, onChanged }) {
  const [mediaId, setMediaId] = useState("");
  const [duration, setDuration] = useState(30);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  async function handleSync(e) {
    e.preventDefault();
    if (!mediaId) return;
    setBusy(true);
    setError(null);
    try {
      await startSync(mediaId, duration);
      onChanged();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="sync-console">
      <div className="sync-console-main">
        <h2 className="sync-console-title">Sync control</h2>
        <form onSubmit={handleSync} className="sync-form">
          <label className="sync-field">
            <span>Media</span>
            <select value={mediaId} onChange={(e) => setMediaId(e.target.value)} required>
              <option value="" disabled>
                Select media…
              </option>
              {catalog.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.id} — {m.name}
                </option>
              ))}
            </select>
          </label>

          <label className="sync-field">
            <span>Duration</span>
            <select value={duration} onChange={(e) => setDuration(Number(e.target.value))}>
              {DURATION_PRESETS.map((s) => (
                <option key={s} value={s}>
                  {s} sec
                </option>
              ))}
            </select>
          </label>

          <button type="submit" className="btn btn--sync" disabled={busy || !mediaId}>
            {busy ? "Starting…" : "Sync now"}
          </button>
        </form>
        {error && <p className="form-error">{error}</p>}
      </div>

      <div className={`sync-tally${syncStatus.active ? " sync-tally--live" : ""}`}>
        <span className="sync-tally-dot" aria-hidden="true" />
        {syncStatus.active ? (
          <>
            <span className="sync-tally-label">LIVE</span>
            <span className="sync-tally-media">{syncStatus.media.id}</span>
            <span className="sync-tally-time">{formatCountdown(syncStatus.remainingMs)}</span>
          </>
        ) : (
          <span className="sync-tally-label">Sync inactive</span>
        )}
      </div>
    </section>
  );
}
