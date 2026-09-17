// Thin fetch wrapper around the backend REST API. Every function returns a
// parsed JSON body or throws an Error with the backend's message, so
// callers can show it directly instead of a generic "request failed".

const API_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

async function request(path, options = {}) {
  const res = await fetch(`${API_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });

  if (res.status === 204) return null;

  let body = null;
  try {
    body = await res.json();
  } catch {
    // No JSON body (e.g. network-level failure surfaced as a non-JSON
    // response); fall through to the status-based error below.
  }

  if (!res.ok) {
    const message = body?.error || `request failed (${res.status})`;
    throw new Error(message);
  }
  return body;
}

export function getState() {
  return request("/api/state");
}

export function listMedia() {
  return request("/api/media");
}

export function createMedia({ id, name, type, url, durationSeconds }) {
  return request("/api/media", {
    method: "POST",
    body: JSON.stringify({
      id: id || undefined,
      name,
      type,
      url,
      duration_seconds: durationSeconds,
    }),
  });
}

export function addToPlaylist(windowId, mediaId) {
  return request(`/api/windows/${encodeURIComponent(windowId)}/playlist`, {
    method: "POST",
    body: JSON.stringify({ media_id: mediaId }),
  });
}

export function startSync(mediaId, durationSeconds) {
  return request("/api/sync", {
    method: "POST",
    body: JSON.stringify({ media_id: mediaId, duration_seconds: durationSeconds }),
  });
}

export function clearSync() {
  return request("/api/sync", { method: "DELETE" });
}

export { API_URL };
