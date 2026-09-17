export default function PlaylistStrip({ playlist, currentIndex, synchronized }) {
  if (!playlist || playlist.length === 0) {
    return <p className="filmstrip-empty">Playlist is empty.</p>;
  }

  return (
    <ol className="filmstrip" aria-label="Playlist">
      {playlist.map((media, i) => {
        const isCurrent = !synchronized && i === currentIndex;
        return (
          <li
            key={`${media.id}-${i}`}
            className={`filmstrip-chip${isCurrent ? " filmstrip-chip--current" : ""}`}
          >
            <span className="filmstrip-chip-id">{media.id}</span>
            {isCurrent && <span className="filmstrip-chip-dot" aria-hidden="true" />}
          </li>
        );
      })}
    </ol>
  );
}
