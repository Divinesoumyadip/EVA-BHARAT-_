import MediaPreview from "./MediaPreview";
import PlaylistStrip from "./PlaylistStrip";
import AddMediaControl from "./AddMediaControl";
import { formatCountdown } from "../lib/playback";

export default function WindowMonitor({ window, catalog, onChanged }) {
  const { id, name, playlist, currentMedia, currentIndex, remainingMs, synchronized } = window;

  return (
    <article className={`monitor${synchronized ? " monitor--synced" : ""}`}>
      <header className="monitor-header">
        <h2 className="monitor-title">{name}</h2>
        <span className="monitor-id">{id}</span>
      </header>

      <MediaPreview media={currentMedia} />

      <div className="monitor-readout">
        <div className="monitor-readout-row">
          <span className="monitor-readout-label">Now playing</span>
          <span className="monitor-readout-value">
            {currentMedia ? currentMedia.id : "—"}
          </span>
        </div>
        <div className="monitor-readout-row">
          <span className="monitor-readout-label">
            {synchronized ? "Sync ends in" : "Next in"}
          </span>
          <span className="monitor-readout-value monitor-readout-value--mono">
            {currentMedia ? formatCountdown(remainingMs) : "—"}
          </span>
        </div>
        {synchronized && <span className="badge badge--sync">SYNCHRONIZED</span>}
      </div>

      <div className="monitor-playlist">
        <PlaylistStrip playlist={playlist} currentIndex={currentIndex} synchronized={synchronized} />
      </div>

      <AddMediaControl windowId={id} catalog={catalog} onChanged={onChanged} />
    </article>
  );
}
