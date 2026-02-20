# DESIGN.md – Event Registration & Ticketing System

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────┐
│                             CLIENT LAYER                                  │
│                                                                           │
│    Browser (HTML/JS)          curl / API Client           Test Harness   │
│    /templates/*.html          POST /events                goroutine×20   │
└────────────────┬──────────────────────┬────────────────────────┬─────────┘
                 │  HTTP                │                         │
                 ▼                      ▼                         ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                           HTTP SERVER (chi)                               │
│                                                                           │
│  Middleware stack:                                                        │
│    chi.Recoverer → chi.RequestID → chi.RealIP → Logger → CORS            │
│                                                                           │
│  Routes:                                                                  │
│    POST   /events                 → CreateEvent handler                   │
│    GET    /events                 → ListEvents handler                    │
│    GET    /events/{id}            → GetEvent handler                      │
│    POST   /events/{id}/register   → Register handler  ◄─ CRITICAL PATH   │
│    GET    /events/{id}/registrations → ListRegistrations handler          │
│    GET    /health                 → HealthCheck                           │
│    /*                             → Static file server (web/)             │
└────────────────┬─────────────────────────────────────────────────────────┘
                 │ decoded request struct
                 ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                         SERVICE LAYER                                     │
│                        service/service.go                                 │
│                                                                           │
│  • Input validation (non-empty name, valid email, capacity > 0)           │
│  • Domain error translation (ErrNotFound, ErrEventFull, …)               │
│  • Orchestrates repository calls                                          │
│  • No SQL, no HTTP – pure business logic                                  │
└────────────────┬─────────────────────────────────────────────────────────┘
                 │ validated domain structs
                 ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                       REPOSITORY LAYER                                    │
│                     repository/repository.go                              │
│                                                                           │
│  EventRepository        RegistrationRepository                            │
│  ─────────────────       ─────────────────────                            │
│  Create()               Book()   ◄─── SELECT … FOR UPDATE                │
│  List()                 ListByEvent()                                     │
│  GetByID()                                                                │
│                                                                           │
│  All DB access via pgxpool. Errors wrapped and returned.                  │
└────────────────┬─────────────────────────────────────────────────────────┘
                 │ pgx queries
                 ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                         PostgreSQL                                        │
│                                                                           │
│  events          registrations                                            │
│  ──────────────  ────────────────────────────────────────                 │
│  id (PK)         id (PK)                                                  │
│  name            event_id (FK → events.id)                                │
│  description     user_email                                               │
│  capacity        created_at                                               │
│  booked_count    UNIQUE(event_id, user_email)  ← hard idempotency guard   │
│  created_at                                                               │
│                                                                           │
│  CHECK: booked_count >= 0                                                 │
│  CHECK: booked_count <= capacity   ← last-resort DB-level guard           │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## The Race Condition Problem

### Time-Of-Check-Time-Of-Use (TOCTOU)

The canonical overbooking bug occurs when two transactions execute their capacity check before either writes back:

```
Time  Goroutine A (tx1)                    Goroutine B (tx2)
────  ─────────────────────────────────    ─────────────────────────────────
 t1   BEGIN
 t2                                        BEGIN
 t3   SELECT booked_count → 9
 t4                                        SELECT booked_count → 9  ← stale!
 t5   9 < 10 ✓ → INSERT reg_A
 t6   UPDATE booked_count = 10
 t7   COMMIT
 t8                                        9 < 10 ✓ → INSERT reg_B   ← BUG
 t9                                        UPDATE booked_count = 11  ← BUG
t10                                        COMMIT
```

**Result:** 2 registrations for 1 remaining seat. The event is overbooked.

This happens regardless of whether the `SELECT` and `UPDATE` are in the same transaction under the default `READ COMMITTED` isolation level, because PostgreSQL re-reads the latest committed snapshot for each statement within the transaction.

### Why `READ COMMITTED` Doesn't Save You

Many developers assume "I'm in a transaction so I'm safe." This is incorrect at `READ COMMITTED`:

- `BEGIN` only guarantees atomicity of the writes — not that intermediate reads are stable.
- Two statements in the same transaction can see different snapshots if rows are modified between statements.
- Therefore a plain `SELECT` followed by `UPDATE` in the same transaction is still a race.

---

## Pessimistic Locking Solution

### `SELECT … FOR UPDATE`

```sql
BEGIN;

-- Acquire an exclusive row-level lock IMMEDIATELY.
-- Any other transaction attempting this same query on the same row
-- will BLOCK here until we COMMIT or ROLLBACK.
SELECT capacity, booked_count
FROM events
WHERE id = $1
FOR UPDATE;

-- At this point we are the ONLY transaction reading this row
-- with intent to modify it.

-- Check capacity.
-- If full: ROLLBACK (lock released, next waiter gets it and also sees full).
-- Otherwise: proceed.

UPDATE events SET booked_count = booked_count + 1 WHERE id = $1;
INSERT INTO registrations (...) VALUES (...);

COMMIT;   -- Lock released. Next blocked transaction proceeds.
```

### How the Lock Serialises Concurrent Requests

```
Time  Goroutine A (tx1)               Goroutine B (tx2)
────  ─────────────────────────────   ─────────────────────────────
 t1   BEGIN
 t2   SELECT … FOR UPDATE             BEGIN
 t3   → acquires lock                 SELECT … FOR UPDATE
 t4   → booked_count = 9              → BLOCKED (waiting for lock)
 t5   9 < 10 ✓
 t6   UPDATE booked_count = 10
 t7   INSERT registration
 t8   COMMIT → lock released
 t9                                   → UNBLOCKED, reads booked_count = 10
t10                                   10 < 10? NO → ROLLBACK → 409
```

**Exactly 1 registration succeeds.** No overbooking possible.

---

## Why `SELECT FOR UPDATE` Over Alternatives

### Option 1: `SELECT FOR UPDATE` (Pessimistic Locking) ← Chosen

**How it works:** Lock the row before reading; serialize at DB level.

**Pros:**
- Guaranteed single-pass success — no retries needed.
- Simplest to reason about — the lock window is tiny (one transaction).
- Works perfectly under high contention.
- Zero application-level retry logic.

**Cons:**
- Under extremely high concurrency, lock contention can cause queue buildup.
- Not suitable if the locking event is across multiple tables without careful design.

**Best for:** Ticket/seat booking, inventory reservation, any limited resource allocation under high contention.

---

### Option 2: Optimistic Locking (version column + retry)

```sql
-- Read with version
SELECT ..., version FROM events WHERE id = $1;

-- Write only if version hasn't changed
UPDATE events
SET booked_count = booked_count + 1, version = version + 1
WHERE id = $1 AND version = $read_version;
-- If 0 rows updated → someone else got there first → retry
```

**Pros:** No lock held between read and write; better throughput under *low contention*.

**Cons:** Under high contention (1,000 users for 1 ticket), most transactions fail and retry — thundering herd causes exponential retry storms. More complex application code.

**Best for:** Low-contention scenarios, or when long-lived reads are needed before writing.

---

### Option 3: Database `SERIALIZABLE` Isolation

Setting `SET TRANSACTION ISOLATION LEVEL SERIALIZABLE` on every booking transaction also prevents the anomaly, but:
- Has higher overhead per transaction.
- Can cause spurious serialization failures requiring retries.
- More complex error handling.

---

### Option 4: Application-level mutex / Redis lock

A distributed lock (e.g., Redis SETNX) can work but:
- Introduces a new infrastructure dependency.
- Has failure modes (lock holder crashes → lock not released without TTL).
- Doesn't compose with database transactions.

---

## Database Constraints as Safety Net

The application-level lock is the primary guard. The DB constraints are a last resort:

```sql
CHECK (booked_count <= capacity)   -- prevents saving overbooked state
UNIQUE (event_id, user_email)      -- prevents double-registration at DB level
```

Even if a future code change accidentally removes the `FOR UPDATE`, the `CHECK` constraint means the database itself will reject the update with an error rather than silently corrupt data.

---

## How it Scales

### Current Architecture (Single Node)

```
N clients → 1 Go server → 1 PostgreSQL (max_conns=20)
```

This is appropriate for tens of thousands of registrations per day.

### Scaling Paths

**Horizontal scaling of the API server:**
- Go servers are stateless — add more instances behind a load balancer.
- The locking is in PostgreSQL, so all servers share the same lock space.
- Connection pooling (PgBouncer) can multiply effective throughput.

**Database read scaling:**
- `GET /events` and `GET /events/{id}` are pure reads — add a read replica and route `SELECT` queries there.
- Cache event details in Redis with a short TTL (e.g., 5s) to absorb read traffic.

**Booking under extreme load (10M concurrent requests):**
- Use a message queue (Kafka/SQS) as a booking buffer. Each registration request enqueues a job.
- A small pool of workers dequeue jobs and execute the `SELECT FOR UPDATE` transaction.
- This caps DB lock contention and provides backpressure.

**Sharding:**
- Partition events across PostgreSQL instances by `event_id` hash.
- Each shard handles its own locking in complete isolation.

---

## Possible Improvements

| Area | Improvement |
|------|-------------|
| **Auth** | Add JWT authentication. Split user roles: organizer vs attendee. |
| **Waiting list** | When event is full, offer a waiting list. Promote on cancellation. |
| **Cancellation** | Allow users to cancel; decrement `booked_count` and notify waitlist. |
| **Email notifications** | Send confirmation emails via SendGrid/SES after successful booking. |
| **Rate limiting** | Add per-IP / per-user rate limiting on the register endpoint (chi-throttle or redis-cell). |
| **Pagination** | Cursor-based pagination for large event/registration lists. |
| **Migrations** | Use golang-migrate for versioned, reversible migrations. |
| **Observability** | Structured JSON logging (slog), Prometheus metrics, OpenTelemetry traces. |
| **Testing** | Unit tests for service layer with mock repositories; table-driven handler tests. |
| **Deployment** | Dockerfile + docker-compose.yml for one-command local setup. |
| **Optimistic for reads** | Use a Redis cache for event detail reads to offload PostgreSQL for non-booking queries. |
