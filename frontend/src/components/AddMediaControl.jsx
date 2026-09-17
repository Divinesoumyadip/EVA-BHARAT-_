import { useState } from "react";
import { addToPlaylist, createMedia } from "../services/api";

const MEDIA_TYPES = ["image", "video", "blank"];

export default function AddMediaControl({ windowId, catalog, onChanged }) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState("existing"); // existing | new
  const [selectedId, setSelectedId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  const [newMedia, setNewMedia] = useState({
    name: "",
    type: "image",
    url: "",
    durationSeconds: 10,
  });

  async function handleAddExisting(e) {
    e.preventDefault();
    if (!selectedId) return;
    setBusy(true);
    setError(null);
    try {
      await addToPlaylist(windowId, selectedId);
      setSelectedId("");
      onChanged();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateAndAdd(e) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await createMedia(newMedia);
      await addToPlaylist(windowId, created.id);
      setNewMedia({ name: "", type: "image", url: "", durationSeconds: 10 });
      onChanged();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <button type="button" className="btn btn--ghost btn--block" onClick={() => setOpen(true)}>
        + Add media
      </button>
    );
  }

  return (
    <div className="add-media-panel">
      <div className="add-media-tabs">
        <button
          type="button"
          className={`tab${mode === "existing" ? " tab--active" : ""}`}
          onClick={() => setMode("existing")}
        >
          Use existing
        </button>
        <button
          type="button"
          className={`tab${mode === "new" ? " tab--active" : ""}`}
          onClick={() => setMode("new")}
        >
          Create new
        </button>
        <button type="button" className="btn-close" aria-label="Close" onClick={() => setOpen(false)}>
          ×
        </button>
      </div>

      {mode === "existing" ? (
        <form onSubmit={handleAddExisting} className="add-media-form">
          <select
            value={selectedId}
            onChange={(e) => setSelectedId(e.target.value)}
            required
          >
            <option value="" disabled>
              Select media…
            </option>
            {catalog.map((m) => (
              <option key={m.id} value={m.id}>
                {m.id} — {m.name} ({m.type}, {m.duration_seconds}s)
              </option>
            ))}
          </select>
          <button type="submit" className="btn btn--primary" disabled={busy || !selectedId}>
            {busy ? "Adding…" : "Add to playlist"}
          </button>
        </form>
      ) : (
        <form onSubmit={handleCreateAndAdd} className="add-media-form">
          <input
            type="text"
            placeholder="Name"
            value={newMedia.name}
            onChange={(e) => setNewMedia({ ...newMedia, name: e.target.value })}
            required
          />
          <select
            value={newMedia.type}
            onChange={(e) => setNewMedia({ ...newMedia, type: e.target.value })}
          >
            {MEDIA_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          {newMedia.type !== "blank" && (
            <input
              type="url"
              placeholder="Media URL"
              value={newMedia.url}
              onChange={(e) => setNewMedia({ ...newMedia, url: e.target.value })}
              required
            />
          )}
          <input
            type="number"
            min="1"
            placeholder="Duration (seconds)"
            value={newMedia.durationSeconds}
            onChange={(e) =>
              setNewMedia({ ...newMedia, durationSeconds: Number(e.target.value) })
            }
            required
          />
          <button type="submit" className="btn btn--primary" disabled={busy}>
            {busy ? "Creating…" : "Create and add"}
          </button>
        </form>
      )}

      {error && <p className="form-error">{error}</p>}
    </div>
  );
}
