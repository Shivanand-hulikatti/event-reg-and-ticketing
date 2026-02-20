-- migrations/001_init.sql
-- Initial schema for the Event Registration & Ticketing system.
-- Run with: psql -U postgres -d eventbooking -f migrations/001_init.sql

-- ─────────────────────────────────────────────────────────────────────────────
-- EVENTS
-- ─────────────────────────────────────────────────────────────────────────────
-- booked_count is stored on the events row (denormalised) so we can lock it
-- with SELECT … FOR UPDATE in a single round-trip.  A CHECK constraint ensures
-- the counter never goes negative or exceeds capacity at the database level.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS events (
    id           TEXT        PRIMARY KEY,
    name         TEXT        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description  TEXT        NOT NULL DEFAULT '',
    capacity     INTEGER     NOT NULL CHECK (capacity > 0),
    booked_count INTEGER     NOT NULL DEFAULT 0
                             CHECK (booked_count >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Enforce that the DB never allows overbooking even if application logic fails.
    CONSTRAINT no_overbooking CHECK (booked_count <= capacity)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- REGISTRATIONS
-- ─────────────────────────────────────────────────────────────────────────────
-- The UNIQUE constraint on (event_id, user_email) is a hard idempotency guard:
-- even if two concurrent requests for the same email slip past the application
-- check, the database will reject the second INSERT with a unique violation.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS registrations (
    id         TEXT        PRIMARY KEY,
    event_id   TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_email TEXT        NOT NULL CHECK (user_email LIKE '%@%'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT unique_registration UNIQUE (event_id, user_email)
);

-- Indexes for common query patterns.
CREATE INDEX IF NOT EXISTS idx_registrations_event_id ON registrations(event_id);
CREATE INDEX IF NOT EXISTS idx_registrations_email    ON registrations(user_email);
CREATE INDEX IF NOT EXISTS idx_events_created_at      ON events(created_at DESC);
