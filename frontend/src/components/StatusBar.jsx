const CONNECTION_LABEL = {
  connecting: "Connecting…",
  connected: "Backend connected",
  reconnecting: "Reconnecting…",
};

export default function StatusBar({ connection, windowCount, syncStatus }) {
  return (
    <header className="statusbar">
      <div className="statusbar-brand">
        <span className="statusbar-mark">EVA BHARAT</span>
        <span className="statusbar-name">Media Sequencer</span>
      </div>

      <div className="statusbar-readouts">
        <div className={`statusbar-item statusbar-item--${connection}`}>
          <span className="statusbar-dot" aria-hidden="true" />
          {CONNECTION_LABEL[connection] || connection}
        </div>
        <div className="statusbar-item">Windows: {windowCount}</div>
        <div className="statusbar-item">
          Sync: {syncStatus.active ? `${syncStatus.media.id} live` : "Inactive"}
        </div>
      </div>
    </header>
  );
}
