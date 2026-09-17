import { useState } from "react";

/**
 * Renders whatever is currently playing. Failure handling is local and
 * silent to the rest of the app: a broken image/video shows a fallback
 * tile but playback timing is untouched, because timing is driven by the
 * deterministic clock in useSequencerState, not by media load events
 * (assignment section 26).
 *
 * MediaPreview itself is stateless and just picks a frame to render;
 * MediaFrame is mounted fresh per media id (via `key`), so its "did this
 * fail to load" state naturally resets when the item changes instead of
 * needing an effect to clear it manually.
 */
export default function MediaPreview({ media }) {
  if (!media) {
    return (
      <div className="monitor-screen monitor-screen--empty">
        <span>No media configured</span>
      </div>
    );
  }

  if (media.type === "blank") {
    return (
      <div className="monitor-screen monitor-screen--blank">
        <span className="monitor-blank-mark">BLANK</span>
      </div>
    );
  }

  return <MediaFrame key={media.id} media={media} />;
}

function MediaFrame({ media }) {
  const [failed, setFailed] = useState(false);

  if (failed) {
    return (
      <div className="monitor-screen monitor-screen--fallback">
        <span>Media unavailable</span>
        <span className="monitor-fallback-id">{media.id}</span>
      </div>
    );
  }

  if (media.type === "video") {
    return (
      <div className="monitor-screen">
        <video
          className="monitor-media"
          src={media.url}
          autoPlay
          muted
          loop
          playsInline
          onError={() => setFailed(true)}
        />
      </div>
    );
  }

  // image
  return (
    <div className="monitor-screen">
      <img
        className="monitor-media"
        src={media.url}
        alt={media.name}
        onError={() => setFailed(true)}
      />
    </div>
  );
}
