// Package handlers wires HTTP requests to the service layer. Handlers stay
// deliberately thin: parse input, call a service method, map the result
// (or error) to a status code and JSON body. No business logic lives here.
package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/evabharat/media-sequencer/internal/services"
)

type Handlers struct {
	svc *services.Service
}

func New(svc *services.Service) *Handlers {
	return &Handlers{svc: svc}
}

// Routes builds the full mux using Go 1.22's pattern-matching ServeMux,
// which is enough routing capability for this API's needs and avoids
// pulling in a router dependency for something the standard library
// already does cleanly.
func (h *Handlers) Routes(allowedOrigin string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /ready", h.ready)

	mux.HandleFunc("GET /api/state", h.getState)

	mux.HandleFunc("GET /api/windows", h.listWindows)
	mux.HandleFunc("GET /api/windows/{id}", h.getWindow)
	mux.HandleFunc("GET /api/windows/{id}/playlist", h.getWindowPlaylist)
	mux.HandleFunc("POST /api/windows/{id}/playlist", h.addToPlaylist)

	mux.HandleFunc("GET /api/media", h.listMedia)
	mux.HandleFunc("POST /api/media", h.createMedia)

	mux.HandleFunc("POST /api/sync", h.startSync)
	mux.HandleFunc("GET /api/sync", h.getSync)
	mux.HandleFunc("DELETE /api/sync", h.clearSync)

	return withCORS(withLogging(mux), allowedOrigin)
}

// -----------------------------------------------------------------------
// Health
// -----------------------------------------------------------------------

func (h *Handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) ready(w http.ResponseWriter, r *http.Request) {
	// A cheap read confirms both the app and the database are alive.
	if _, err := h.svc.ListWindows(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// -----------------------------------------------------------------------
// State
// -----------------------------------------------------------------------

func (h *Handlers) getState(w http.ResponseWriter, r *http.Request) {
	state, err := h.svc.GetState(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// -----------------------------------------------------------------------
// Windows
// -----------------------------------------------------------------------

func (h *Handlers) listWindows(w http.ResponseWriter, r *http.Request) {
	windows, err := h.svc.ListWindows(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, windows)
}

func (h *Handlers) getWindow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, err := h.svc.GetWindow(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (h *Handlers) getWindowPlaylist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, err := h.svc.GetWindow(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap.Playlist)
}

type addToPlaylistRequest struct {
	MediaID string `json:"media_id"`
}

func (h *Handlers) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req addToPlaylistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.MediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id is required")
		return
	}

	snap, err := h.svc.AddMediaToPlaylist(r.Context(), id, req.MediaID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

// -----------------------------------------------------------------------
// Media
// -----------------------------------------------------------------------

func (h *Handlers) listMedia(w http.ResponseWriter, r *http.Request) {
	media, err := h.svc.ListMedia(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, media)
}

type createMediaRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	URL             string `json:"url"`
	DurationSeconds int    `json:"duration_seconds"`
}

func (h *Handlers) createMedia(w http.ResponseWriter, r *http.Request) {
	var req createMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	media, err := h.svc.CreateMedia(r.Context(), services.CreateMediaInput{
		ID:              req.ID,
		Name:            req.Name,
		Type:            req.Type,
		URL:             req.URL,
		DurationSeconds: req.DurationSeconds,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, media)
}

// -----------------------------------------------------------------------
// Sync
// -----------------------------------------------------------------------

type startSyncRequest struct {
	MediaID         string `json:"media_id"`
	DurationSeconds int    `json:"duration_seconds"`
}

func (h *Handlers) startSync(w http.ResponseWriter, r *http.Request) {
	var req startSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.MediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id is required")
		return
	}

	state, err := h.svc.StartSync(r.Context(), req.MediaID, req.DurationSeconds)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	log.Printf("sync started: media=%s duration=%ds", req.MediaID, req.DurationSeconds)
	writeJSON(w, http.StatusCreated, state)
}

func (h *Handlers) getSync(w http.ResponseWriter, r *http.Request) {
	state, err := h.svc.GetSync(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) clearSync(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ClearSync(r.Context()); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -----------------------------------------------------------------------
// JSON / error helpers
// -----------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeServiceError maps service-layer sentinel errors to HTTP status
// codes. Raw internal errors are logged server-side but never leaked to
// the client (assignment section 20: don't expose database internals).
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, services.ErrConflict):
		writeError(w, http.StatusConflict, "resource already exists")
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
