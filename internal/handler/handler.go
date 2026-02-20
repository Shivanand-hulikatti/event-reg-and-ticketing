// Package handler contains chi HTTP handlers that translate HTTP
// requests/responses to and from the service layer.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/model"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/repository"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/service"
	"github.com/go-chi/chi/v5"
)

// EventHandler holds all HTTP handlers for the event booking API.
type EventHandler struct {
	svc *service.EventService
}

// NewEventHandler constructs an EventHandler.
func NewEventHandler(svc *service.EventService) *EventHandler {
	return &EventHandler{svc: svc}
}

// ─── Helper utilities ─────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, model.ErrorResponse{Error: msg})
}

func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1 MB limit
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// CreateEvent handles POST /events
// Creates a new event with the given name, description, and capacity.
func (h *EventHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req model.CreateEventRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	event, err := h.svc.CreateEvent(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, event)
}

// ListEvents handles GET /events
// Returns a JSON array of all events.
func (h *EventHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.svc.ListEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list events")
		return
	}

	// Return an empty array rather than null for better client compatibility.
	if events == nil {
		events = []model.Event{}
	}

	writeJSON(w, http.StatusOK, events)
}

// GetEvent handles GET /events/{id}
// Returns a single event by its UUID.
func (h *EventHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	event, err := h.svc.GetEvent(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get event")
		return
	}

	writeJSON(w, http.StatusOK, event)
}

// Register handles POST /events/{id}/register
// Performs a concurrency-safe registration for the specified event.
func (h *EventHandler) Register(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req model.RegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	reg, err := h.svc.Register(r.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			writeError(w, http.StatusNotFound, "event not found")
		case errors.Is(err, repository.ErrEventFull):
			writeError(w, http.StatusConflict, "event is fully booked")
		case errors.Is(err, repository.ErrAlreadyRegistered):
			writeError(w, http.StatusConflict, "you are already registered for this event")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, reg)
}

// ListRegistrations handles GET /events/{id}/registrations
// Returns all registrations for a given event.
func (h *EventHandler) ListRegistrations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	regs, err := h.svc.ListRegistrations(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list registrations")
		return
	}

	if regs == nil {
		regs = []model.Registration{}
	}

	writeJSON(w, http.StatusOK, regs)
}

// ─── Health check ─────────────────────────────────────────────────────────────

// HealthCheck handles GET /health
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
