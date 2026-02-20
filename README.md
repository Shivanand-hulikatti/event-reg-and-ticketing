# 🎟 Event Registration & Ticketing System

A production-quality REST API built in Go for creating events, managing registrations, and preventing overbooking under concurrent load — similar to Eventbrite.

---

## Table of Contents

1. [Overview](#overview)
2. [Tech Stack](#tech-stack)
3. [Project Structure](#project-structure)
4. [Database Setup](#database-setup)
5. [Running the Server](#running-the-server)
6. [API Documentation](#api-documentation)
7. [Static HTML UI](#static-html-ui)
8. [Concurrency Strategy](#concurrency-strategy)
9. [Running the Concurrent Booking Test](#running-the-concurrent-booking-test)

---

## Overview

This system allows:
- **Organizers** to create events with a fixed seat capacity.
- **Attendees** to browse events, view availability, and register.
- The server to **prevent overbooking** when many users register simultaneously — a critical correctness requirement in any ticketing platform.

The concurrency guarantee is achieved via **PostgreSQL's `SELECT ... FOR UPDATE`** (pessimistic row-level locking), ensuring that concurrent booking attempts are serialised at the database level.

---

## Tech Stack

| Concern        | Choice                    |
|----------------|---------------------------|
| Language       | Go 1.23                   |
| HTTP Router    | `github.com/go-chi/chi/v5`|
| Database       | PostgreSQL 15+            |
| DB Driver      | `github.com/jackc/pgx/v5` |
| Frontend       | Vanilla HTML + CSS + JS   |
| Dependencies   | `github.com/google/uuid`  |

---

## Project Structure

```
event-booking-api/
│
├── cmd/
│   └── main.go                  # Entry point: wires layers, starts server
│
├── internal/
│   ├── database/
│   │   └── database.go          # pgxpool connection management
│   ├── model/
│   │   └── model.go             # Domain types (Event, Registration, requests)
│   ├── repository/
│   │   └── repository.go        # SQL queries + concurrency-safe booking
│   ├── service/
│   │   └── service.go           # Business logic and validation
│   └── handler/
│       ├── handler.go           # HTTP handlers (chi)
│       └── middleware.go        # Logger, CORS middleware
│
├── web/
│   ├── templates/
│   │   ├── index.html           # Event listing
│   │   ├── create_event.html    # Event creation form
│   │   └── event_details.html   # Event detail + registration form
│   └── static/
│       └── styles.css           # Minimal CSS
│
├── migrations/
│   └── 001_init.sql             # Schema creation
│
├── test/
│   └── concurrent_booking_test.go  # Goroutine stress test
│
├── go.mod
├── README.md
└── DESIGN.md
```

---

## Database Setup

### 1. Create the database

```bash
psql -U postgres -c "CREATE DATABASE eventbooking;"
```

### 2. Run the migration

```bash
psql -U postgres -d eventbooking -f migrations/001_init.sql
```

### 3. Environment variables

The server reads these from the environment (defaults shown):

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=eventbooking
export DB_SSLMODE=disable
```

---

## Running the Server

### Prerequisites

- Go 1.23+
- PostgreSQL running with the migration applied (see above)

### Install dependencies

```bash
cd event-booking-api
go mod tidy
```

### Start the server

```bash
go run ./cmd/main.go
```

The server starts on **http://localhost:8080** by default. Set `PORT=<n>` to change.

Output:
```
✓ Connected to PostgreSQL
✓ Server listening on http://localhost:8080
```

Visit **http://localhost:8080/templates/index.html** in your browser.

---

## API Documentation

All API endpoints accept and return `application/json`.

---

### `POST /events`

Create a new event.

**Request body:**
```json
{
  "name": "Go Concurrency Workshop",
  "description": "Deep dive into goroutines and channels",
  "capacity": 50
}
```

**Response `201 Created`:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Go Concurrency Workshop",
  "description": "Deep dive into goroutines and channels",
  "capacity": 50,
  "booked_count": 0,
  "created_at": "2024-01-15T10:00:00Z"
}
```

**Error responses:** `400 Bad Request` (invalid input)

---

### `GET /events`

List all events, newest first.

**Response `200 OK`:** array of event objects.

---

### `GET /events/{id}`

Get a single event by ID.

**Response `200 OK`:** event object.

**Error responses:** `404 Not Found`

---

### `POST /events/{id}/register`

Register for an event. This is the **concurrency-safe** endpoint.

**Request body:**
```json
{
  "user_email": "alice@example.com"
}
```

**Response `201 Created`:**
```json
{
  "id": "reg-uuid",
  "event_id": "event-uuid",
  "user_email": "alice@example.com",
  "created_at": "2024-01-15T10:05:00Z"
}
```

**Error responses:**
| Status | Meaning                             |
|--------|-------------------------------------|
| `400`  | Invalid email or missing body       |
| `404`  | Event not found                     |
| `409`  | Event is fully booked               |
| `409`  | Email already registered            |

---

### `GET /events/{id}/registrations`

List all registrations for an event.

**Response `200 OK`:** array of registration objects.

---

### `GET /health`

Health check.

**Response `200 OK`:** `{"status": "ok"}`

---

## Static HTML UI

The server serves static files from the `./web` directory.

| URL                                        | Purpose                     |
|--------------------------------------------|-----------------------------|
| `/templates/index.html`                    | Browse all events           |
| `/templates/create_event.html`             | Create a new event          |
| `/templates/event_details.html?id=<uuid>`  | View event + register       |

All pages use the **Fetch API** to communicate with the REST API — no page reloads for form submissions.

---

## Concurrency Strategy

### The Problem

In a naïve implementation:

```
Goroutine A: SELECT booked_count WHERE id=X  → 9
Goroutine B: SELECT booked_count WHERE id=X  → 9   ← both see 9
Goroutine A: 9 < 10 capacity → INSERT registration, UPDATE booked_count=10
Goroutine B: 9 < 10 capacity → INSERT registration, UPDATE booked_count=10
Result: 11 registrations for a 10-seat event  ← OVERBOOKED
```

This is a **TOCTOU race** (Time Of Check, Time Of Use): you check a value, then act on that stale check. Between check and update, another transaction can change the data.

### The Solution: Pessimistic Locking (`SELECT ... FOR UPDATE`)

```sql
BEGIN;

SELECT capacity, booked_count
FROM events
WHERE id = $1
FOR UPDATE;            -- ← acquires exclusive row lock

-- No other transaction can read this row with FOR UPDATE until we COMMIT/ROLLBACK

-- Check: booked_count < capacity?
-- If not: ROLLBACK → return 409

UPDATE events SET booked_count = booked_count + 1 WHERE id = $1;
INSERT INTO registrations (id, event_id, user_email, ...) VALUES (...);

COMMIT;                -- ← releases lock
```

`FOR UPDATE` tells PostgreSQL: *"I am about to modify this row. Lock it exclusively so no concurrent transaction can read it for update until I'm done."*

This serialises all concurrent booking attempts through the database, making it **linearisable** for a given event row.

**Why not optimistic locking?** Optimistic locking (version column + retry) works well under *low contention*. For a hot ticket event where 10,000 users hammer the last seat, pessimistic locking avoids the thundering retry storm. See `DESIGN.md` for the full tradeoff analysis.

---

## Running the Concurrent Booking Test

The test at `test/concurrent_booking_test.go`:
1. Creates an event with **capacity = 1**
2. Launches **20 goroutines** simultaneously
3. Each goroutine attempts to register a unique email
4. Asserts **exactly 1 success**
5. Verifies the DB `booked_count = 1`

```bash
# Set DB env vars first, then:
go test ./test/ -v -run TestConcurrentBooking -count=1
```

Expected output:
```
════════════════════════════════════════
        CONCURRENT BOOKING SUMMARY
════════════════════════════════════════
  Total goroutines : 20
  Successes        : 1  (expected: 1)
  Event-full errors: 19
  Other errors     : 0
════════════════════════════════════════
✅ PASS: SELECT FOR UPDATE correctly serialised all concurrent bookings.
```

Run the multi-seat variant (capacity=5, 20 goroutines):
```bash
go test ./test/ -v -run TestConcurrentBookingMultipleSeats -count=1
```
