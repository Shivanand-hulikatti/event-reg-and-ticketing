// Package service implements business logic, validation, and orchestration
// between HTTP handlers and the repository layer.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/model"
	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/repository"
)

// EventService orchestrates event-related business operations.
type EventService struct {
	events        *repository.EventRepository
	registrations *repository.RegistrationRepository
}

// NewEventService constructs an EventService with its dependencies.
func NewEventService(
	events *repository.EventRepository,
	registrations *repository.RegistrationRepository,
) *EventService {
	return &EventService{events: events, registrations: registrations}
}

// CreateEvent validates the request and delegates to the repository.
func (s *EventService) CreateEvent(ctx context.Context, req model.CreateEventRequest) (*model.Event, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("event name is required")
	}
	if req.Capacity <= 0 {
		return nil, fmt.Errorf("capacity must be a positive integer")
	}
	if req.Capacity > 100_000 {
		return nil, fmt.Errorf("capacity cannot exceed 100,000")
	}
	return s.events.Create(ctx, req)
}

// ListEvents returns all events.
func (s *EventService) ListEvents(ctx context.Context) ([]model.Event, error) {
	return s.events.List(ctx)
}

// GetEvent returns a single event by ID.
func (s *EventService) GetEvent(ctx context.Context, id string) (*model.Event, error) {
	if id == "" {
		return nil, fmt.Errorf("event id is required")
	}
	event, err := s.events.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get event: %w", err)
	}
	return event, nil
}

// Register validates the registration request and delegates the concurrency-safe
// booking to the repository layer.
func (s *EventService) Register(ctx context.Context, eventID string, req model.RegisterRequest) (*model.Registration, error) {
	req.UserEmail = strings.TrimSpace(strings.ToLower(req.UserEmail))
	if req.UserEmail == "" {
		return nil, fmt.Errorf("user_email is required")
	}
	if !isValidEmail(req.UserEmail) {
		return nil, fmt.Errorf("user_email is not a valid email address")
	}
	if eventID == "" {
		return nil, fmt.Errorf("event id is required")
	}

	reg, err := s.registrations.Book(ctx, eventID, req.UserEmail)
	if err != nil {
		// Surface domain errors directly so handlers can set correct HTTP status.
		if errors.Is(err, repository.ErrNotFound) ||
			errors.Is(err, repository.ErrEventFull) ||
			errors.Is(err, repository.ErrAlreadyRegistered) {
			return nil, err
		}
		return nil, fmt.Errorf("register for event: %w", err)
	}
	return reg, nil
}

// ListRegistrations returns all registrations for an event.
func (s *EventService) ListRegistrations(ctx context.Context, eventID string) ([]model.Registration, error) {
	if _, err := s.events.GetByID(ctx, eventID); err != nil {
		return nil, repository.ErrNotFound
	}
	return s.registrations.ListByEvent(ctx, eventID)
}

// isValidEmail does a basic structural check (no external deps).
func isValidEmail(email string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	return len(parts[0]) > 0 && strings.Contains(parts[1], ".")
}
