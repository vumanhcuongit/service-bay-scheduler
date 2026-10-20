-- +goose Up
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Hours and durations sit on the 15-minute grid (D1).
CREATE TABLE dealerships (
    id         text PRIMARY KEY,
    timezone   text NOT NULL,
    open_time  time NOT NULL,
    close_time time NOT NULL,
    CHECK (open_time < close_time),
    CHECK (EXTRACT(EPOCH FROM open_time)::int % 900 = 0),
    CHECK (EXTRACT(EPOCH FROM close_time)::int % 900 = 0)
);

CREATE TABLE skills (
    id text PRIMARY KEY
);

CREATE TABLE bays (
    id            text PRIMARY KEY,
    dealership_id text NOT NULL REFERENCES dealerships (id)
);

CREATE TABLE technicians (
    id            text PRIMARY KEY,
    dealership_id text NOT NULL REFERENCES dealerships (id),
    work_start    time NOT NULL,
    work_end      time NOT NULL,
    CHECK (work_start < work_end),
    CHECK (EXTRACT(EPOCH FROM work_start)::int % 900 = 0),
    CHECK (EXTRACT(EPOCH FROM work_end)::int % 900 = 0)
);

CREATE TABLE technician_skills (
    technician_id text NOT NULL REFERENCES technicians (id),
    skill_id      text NOT NULL REFERENCES skills (id),
    PRIMARY KEY (technician_id, skill_id)
);

CREATE TABLE service_types (
    id               text PRIMARY KEY,
    dealership_id    text NOT NULL REFERENCES dealerships (id),
    duration_minutes int  NOT NULL CHECK (duration_minutes > 0 AND duration_minutes % 15 = 0),
    required_skill   text NOT NULL REFERENCES skills (id)
);

CREATE TABLE customers (
    id text PRIMARY KEY
);

CREATE TABLE vehicles (
    id          text PRIMARY KEY,
    customer_id text NOT NULL REFERENCES customers (id)
);

-- The three exclusion constraints are the final guard against overlaps (D8).
-- tstzrange defaults to [), the half-open rule (D6).
CREATE TABLE appointments (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    dealership_id   text        NOT NULL REFERENCES dealerships (id),
    customer_id     text        NOT NULL REFERENCES customers (id),
    vehicle_id      text        NOT NULL REFERENCES vehicles (id),
    service_type_id text        NOT NULL REFERENCES service_types (id),
    bay_id          text        NOT NULL REFERENCES bays (id),
    technician_id   text        NOT NULL REFERENCES technicians (id),
    start           timestamptz NOT NULL,
    "end"           timestamptz NOT NULL,
    status          text        NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed')),
    CHECK (start < "end"),
    CONSTRAINT appointments_bay_excl
        EXCLUDE USING gist (bay_id WITH =, tstzrange(start, "end") WITH &&) WHERE (status = 'confirmed'),
    CONSTRAINT appointments_technician_excl
        EXCLUDE USING gist (technician_id WITH =, tstzrange(start, "end") WITH &&) WHERE (status = 'confirmed'),
    CONSTRAINT appointments_vehicle_excl
        EXCLUDE USING gist (vehicle_id WITH =, tstzrange(start, "end") WITH &&) WHERE (status = 'confirmed')
);

CREATE INDEX appointments_dealership_start_idx ON appointments (dealership_id, start);

-- status and body are NULL while the booking transaction is in flight (D9).
CREATE TABLE idempotency_keys (
    key          uuid        PRIMARY KEY,
    request_hash text        NOT NULL,
    status       text        CHECK (status IN ('created', 'conflict')),
    body         jsonb,
    created_at   timestamptz NOT NULL
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE appointments;
DROP TABLE vehicles;
DROP TABLE customers;
DROP TABLE service_types;
DROP TABLE technician_skills;
DROP TABLE technicians;
DROP TABLE skills;
DROP TABLE bays;
DROP TABLE dealerships;
DROP EXTENSION IF EXISTS btree_gist;
