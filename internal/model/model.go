// Package model defines the core domain types for the event booking system.
package model

import "time"

// Event represents a bookable event created by an organizer.
type Event struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Capacity    int       `json:"capacity"`
	BookedCount int       `json:"booked_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// Remaining returns the number of available seats.
func (e *Event) Remaining() int {
	return e.Capacity - e.BookedCount
}

// IsFull returns true when no seats remain.
func (e *Event) IsFull() bool {
	return e.BookedCount >= e.Capacity
}

// Registration represents a user's registration for an event.
type Registration struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	UserEmail string    `json:"user_email"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateEventRequest is the payload for creating a new event.
type CreateEventRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Capacity    int    `json:"capacity"`
}

// RegisterRequest is the payload for registering for an event.
type RegisterRequest struct {
	UserEmail string `json:"user_email"`
}

// ErrorResponse is a standard JSON error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// BookingResult summarises the outcome of a single registration attempt.
// Used in the concurrent test harness.
type BookingResult struct {
	UserEmail string
	Success   bool
	Error     error
}
