import { useCallback, useEffect, useState } from "react";
import StatusBar from "./components/StatusBar";
import SyncConsole from "./components/SyncConsole";
import WindowMonitor from "./components/WindowMonitor";
import { useSequencerState } from "./hooks/useSequencerState";
import { listMedia } from "./services/api";

export default function App() {
  const { windows, syncStatus, connection, error, refresh } = useSequencerState();
  const [catalog, setCatalog] = useState([]);

  const refreshCatalog = useCallback(() => {
    listMedia()
      .then(setCatalog)
      .catch(() => {
        // Non-fatal: the media dropdowns just stay at their last known
        // list. The main state poll already surfaces connectivity issues.
      });
  }, []);

  useEffect(() => {
    refreshCatalog();
  }, [refreshCatalog]);

  // Playlist/media mutations refresh both the catalog (new media items)
  // and force an immediate state poll, so the change is visible without
  // waiting for the next scheduled poll.
  const handleChanged = useCallback(() => {
    refreshCatalog();
    refresh();
  }, [refreshCatalog, refresh]);

  return (
    <div className="app">
      <StatusBar connection={connection} windowCount={windows.length} syncStatus={syncStatus} />

      {connection === "reconnecting" && (
        <div className="banner banner--warning">
          Lost contact with the backend{error ? `: ${error}` : ""}. Showing the last known
          state and retrying…
        </div>
      )}

      <main className="app-main">
        <SyncConsole catalog={catalog} syncStatus={syncStatus} onChanged={handleChanged} />

        <section className="monitor-grid">
          {windows.length === 0 && connection === "connected" && (
            <p className="empty-state">No windows configured.</p>
          )}
          {windows.map((w) => (
            <WindowMonitor key={w.id} window={w} catalog={catalog} onChanged={handleChanged} />
          ))}
        </section>
      </main>
    </div>
  );
}
