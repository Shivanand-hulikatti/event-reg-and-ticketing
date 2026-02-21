# 🎟 Event Registration & Ticketing System

> A production-ready event booking API that solves the hardest problem in ticketing: **preventing overbooking under high concurrent load**.

Built with Go, PostgreSQL, and clean architecture principles — demonstrating real-world concurrency solutions for distributed systems.

---

## 🎯 Why This Project Stands Out

**Solves a Real Problem:** Implements pessimistic locking with `SELECT ... FOR UPDATE` to prevent race conditions when thousands of users compete for the last ticket — a critical challenge in any booking system (Eventbrite, ticket sales, seat reservations).

**Production Patterns:**
- **Clean Architecture** — Layered design with clear separation: Handlers → Service → Repository
- **Concurrency-Safe** — Database-level locking guarantees zero overbooking
- **Error Handling** — Proper domain errors with meaningful HTTP status codes
- **Database Constraints** — Defense-in-depth with CHECK constraints and unique indexes

**Technologies:**
- **Backend:** Go 1.23 + Chi Router + pgx/v5
- **Database:** PostgreSQL 15+ with row-level locking
- **Frontend:** Vanilla HTML/CSS/JS (no frameworks)
- **Deployment:** Docker Compose ready

---

## 🚀 Quick Start

```bash
# 1. Start services
docker-compose up -d

# 2. Server runs at http://localhost:8080
# Visit http://localhost:8080/templates/index.html
```

**Manual Setup:**
```bash
# Create database
psql -U postgres -c "CREATE DATABASE eventbooking;"
psql -U postgres -d eventbooking -f migrations/001_init.sql

# Run server
go run ./cmd/main.go
```

---

## 📁 Architecture

## 📁 Architecture

```
Client (Browser/API) → Chi Router → Service Layer → Repository → PostgreSQL
                           ↓
                    Static Files (web/)
```

**Layered Design:**
- **Handlers** (`internal/handler/`) — HTTP routing, request/response
- **Service** (`internal/service/`) — Business logic, validation
- **Repository** (`internal/repository/`) — Database queries, transactions
- **Models** (`internal/model/`) — Domain types

**Key Files:**
```
cmd/main.go                    # Application entry point
internal/repository/repository.go   # ⚡ Concurrency-safe booking logic
migrations/001_init.sql        # Database schema
web/templates/                 # HTML UI
```

See [DESIGN.md](DESIGN.md) for detailed architecture diagrams and concurrency analysis.

---

## 🔥 The Concurrency Solution

**The Problem:** Classic race condition where two users booking the last seat both see "1 available" and both succeed → overbooked event.

**The Solution:** PostgreSQL's `SELECT ... FOR UPDATE` provides row-level pessimistic locking:

```sql
BEGIN;
SELECT capacity, booked_count FROM events WHERE id = $1 FOR UPDATE;  -- 🔒 Lock acquired
-- Other concurrent requests BLOCK here until we commit
UPDATE events SET booked_count = booked_count + 1 WHERE id = $1;
INSERT INTO registrations (...) VALUES (...);
COMMIT;  -- 🔓 Lock released
```

**Result:** Serialized booking — exactly 1 winner for the last seat. No race conditions, no retries needed.

> **Why this approach?** Compared to optimistic locking, pessimistic locking excels under high contention (hot ticket sales) by eliminating retry storms. See [DESIGN.md](DESIGN.md) for full tradeoff analysis.

---

## 📡 API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/events` | POST | Create event |
| `/events` | GET | List all events |
| `/events/{id}` | GET | Get event details |
| `/events/{id}/register` | POST | Register for event 🔒 |
| `/events/{id}/registrations` | GET | List registrations |
| `/health` | GET | Health check |

**Example Registration:**
```bash
curl -X POST http://localhost:8080/events/{id}/register \
  -H "Content-Type: application/json" \
  -d '{"user_email": "alice@example.com"}'
```

**Response Codes:**
- `201` — Registration successful
- `409` — Event full or email already registered
- `400` — Invalid input
- `404` — Event not found

Full API documentation in [DESIGN.md](DESIGN.md).

---

## 🎨 Web Interface

Visit `http://localhost:8080/templates/index.html` for the interactive UI:
- **Browse Events** — See all events with live availability
- **Create Event** — Set name, description, capacity
- **Register** — One-click registration with email

Built with vanilla JavaScript + Fetch API — no frameworks required.

---

## 🛡️ Production-Ready Features

✅ **Concurrency Safety** — Row-level locking prevents race conditions  
✅ **Database Constraints** — `CHECK (booked_count <= capacity)` as last-resort guard  
✅ **Idempotency** — `UNIQUE(event_id, user_email)` prevents double-booking  
✅ **Clean Architecture** — Testable, maintainable, scalable  
✅ **Error Handling** — Domain errors mapped to proper HTTP codes  
✅ **Connection Pooling** — pgxpool for efficient DB connections  
✅ **Middleware Stack** — Logging, CORS, recovery, request IDs  
✅ **Docker Ready** — One-command deployment with docker-compose  

---

## 🧪 Environment Variables

```bash
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=eventbooking
DB_SSLMODE=disable
PORT=8080
```

---

## 📚 Learning Resources

This project demonstrates:
- **Concurrency Patterns** in Go and PostgreSQL
- **Clean Architecture** for maintainable web services
- **ACID Transactions** and isolation levels
- **Pessimistic vs Optimistic Locking** tradeoffs
- **RESTful API Design** best practices

Perfect for learning production-grade Go development and database concurrency control.

---

## 🔗 Learn More

- [DESIGN.md](DESIGN.md) — In-depth architecture, diagrams, and concurrency analysis
- [migrations/001_init.sql](migrations/001_init.sql) — Database schema with constraints

---

## 📄 License

MIT License — Feel free to use this project for learning and building.

---

**Built to solve real-world concurrency challenges in distributed systems.**
