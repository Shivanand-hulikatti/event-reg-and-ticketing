// Package repository implements all database queries for the event booking system.
// It uses pgx directly (no ORM) for transparency and performance.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Shivanand-hulikatti/event-reg-and-ticketing/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrEventFull is returned when an event has no remaining capacity.
var ErrEventFull = errors.New("event is fully booked")

// ErrAlreadyRegistered is returned when the same email registers twice.
var ErrAlreadyRegistered = errors.New("email already registered for this event")

// EventRepository handles persistence for events.
type EventRepository struct {
	db *pgxpool.Pool
}

// NewEventRepository constructs an EventRepository.
func NewEventRepository(db *pgxpool.Pool) *EventRepository {
	return &EventRepository{db: db}
}

// Create inserts a new event and returns it with a generated UUID.
func (r *EventRepository) Create(ctx context.Context, req model.CreateEventRequest) (*model.Event, error) {
	event := &model.Event{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Capacity:    req.Capacity,
		BookedCount: 0,
		CreatedAt:   time.Now().UTC(),
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO events (id, name, description, capacity, booked_count, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ID, event.Name, event.Description, event.Capacity, event.BookedCount, event.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}
	return event, nil
}

// List returns all events ordered by creation time descending.
func (r *EventRepository) List(ctx context.Context) ([]model.Event, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, description, capacity, booked_count, created_at
		 FROM events
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.Capacity, &e.BookedCount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetByID returns a single event or ErrNotFound.
func (r *EventRepository) GetByID(ctx context.Context, id string) (*model.Event, error) {
	var e model.Event
	err := r.db.QueryRow(ctx,
		`SELECT id, name, description, capacity, booked_count, created_at
		 FROM events WHERE id = $1`,
		id,
	).Scan(&e.ID, &e.Name, &e.Description, &e.Capacity, &e.BookedCount, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get event: %w", err)
	}
	return &e, nil
}

// RegistrationRepository handles persistence for registrations.
type RegistrationRepository struct {
	db *pgxpool.Pool
}

// NewRegistrationRepository constructs a RegistrationRepository.
func NewRegistrationRepository(db *pgxpool.Pool) *RegistrationRepository {
	return &RegistrationRepository{db: db}
}

// Book performs a concurrency-safe registration inside a serialised transaction.
//
// ─────────────────────────────────────────────────────────────────────────────
// RACE CONDITION EXPLAINED
// ─────────────────────────────────────────────────────────────────────────────
//
// Naive read-then-write approach (BROKEN):
//
//	goroutine A: SELECT booked_count FROM events WHERE id = X  → returns 9
//	goroutine B: SELECT booked_count FROM events WHERE id = X  → returns 9
//	goroutine A: capacity=10, 9 < 10, OK → INSERT registration, UPDATE booked_count=10
//	goroutine B: capacity=10, 9 < 10, OK → INSERT registration, UPDATE booked_count=10
//	Result: 11 registrations for a 10-seat event. OVERBOOKED.
//
// Why it happens: two transactions read the same snapshot of the row before
// either has written back, so both see free capacity.
//
// SOLUTION: Pessimistic locking with SELECT … FOR UPDATE
//
//	SELECT … FOR UPDATE acquires a row-level exclusive lock on the event row
//	the moment the SELECT executes inside a transaction.  Any other transaction
//	that attempts the same SELECT … FOR UPDATE on that row is blocked until
//	the first transaction either COMMITs or ROLLBACKs.
//
//	This serialises concurrent booking attempts so only one goroutine at a
//	time can read-then-write the capacity counter, eliminating the race.
//
// ─────────────────────────────────────────────────────────────────────────────
func (r *RegistrationRepository) Book(ctx context.Context, eventID, userEmail string) (*model.Registration, error) {
	// Begin a transaction – all steps below are atomic.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	// Ensure the transaction is always resolved.
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// ── Step 1: Acquire an exclusive row-level lock on the event. ──────────
	//
	// SELECT … FOR UPDATE prevents any concurrent transaction from reading
	// this row (with FOR UPDATE) until we COMMIT or ROLLBACK.  This is
	// *pessimistic locking*: we assume contention will happen and prevent it
	// upfront rather than detecting and retrying after the fact.
	var capacity, bookedCount int
	err = tx.QueryRow(ctx,
		`SELECT capacity, booked_count
		 FROM events
		 WHERE id = $1
		 FOR UPDATE`,
		eventID,
	).Scan(&capacity, &bookedCount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock event row: %w", err)
	}

	// ── Step 2: Check for duplicate registration. ──────────────────────────
	var dupCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM registrations WHERE event_id = $1 AND user_email = $2`,
		eventID, userEmail,
	).Scan(&dupCount)
	if err != nil {
		return nil, fmt.Errorf("check duplicate: %w", err)
	}
	if dupCount > 0 {
		return nil, ErrAlreadyRegistered
	}

	// ── Step 3: Guard against overbooking. ────────────────────────────────
	if bookedCount >= capacity {
		return nil, ErrEventFull
	}

	// ── Step 4: Increment the counter atomically in the same transaction. ──
	_, err = tx.Exec(ctx,
		`UPDATE events SET booked_count = booked_count + 1 WHERE id = $1`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("increment booked_count: %w", err)
	}

	// ── Step 5: Create the registration record. ───────────────────────────
	reg := &model.Registration{
		ID:        uuid.New().String(),
		EventID:   eventID,
		UserEmail: userEmail,
		CreatedAt: time.Now().UTC(),
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO registrations (id, event_id, user_email, created_at)
		 VALUES ($1, $2, $3, $4)`,
		reg.ID, reg.EventID, reg.UserEmail, reg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert registration: %w", err)
	}

	// ── Step 6: Commit – only now does any other goroutine see the change. ─
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return reg, nil
}

// ListByEvent returns all registrations for a given event.
func (r *RegistrationRepository) ListByEvent(ctx context.Context, eventID string) ([]model.Registration, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, event_id, user_email, created_at
		 FROM registrations
		 WHERE event_id = $1
		 ORDER BY created_at ASC`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("list registrations: %w", err)
	}
	defer rows.Close()

	var regs []model.Registration
	for rows.Next() {
		var reg model.Registration
		if err := rows.Scan(&reg.ID, &reg.EventID, &reg.UserEmail, &reg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan registration: %w", err)
		}
		regs = append(regs, reg)
	}
	return regs, rows.Err()
}
