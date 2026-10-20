# Scope & Decisions

Service appointment scheduler for automotive workshops. Backend only (Go + Postgres); the client layer is represented by an OpenAPI contract and cURL examples.

This file was written **before** the spec and before any code. It fixes what the system is and is not, so that the spec (`docs/superpowers/specs/`) can focus on behaviour and the design doc (`docs/design.md`) on why.

Format: each decision has what / why / what I considered. Short on purpose.

---

## 0. Domain

### 0.1 What the scenario states

An **Appointment Scheduler** application to replace manual booking systems. Three core requirements, kept close to the original wording:

1. **Resource-constrained booking.** A user can request a service appointment for a specific **vehicle**, **service type** and **dealership** at a **desired time**.
2. **Real-time availability check.** Before confirming, check the availability of both a **ServiceBay** and a **qualified Technician** for the **entire service duration**.
3. **Confirmed appointment record.** On success, create a persistent **Appointment** record associating the **customer, vehicle, technician and service bay**.

Entities the scenario names: Dealership, ServiceBay, Technician, ServiceType, Customer, Vehicle, Appointment. Nothing else is given: no attributes, no relations, no definition of "qualified", "available" or "user".

### 0.2 What I assume (the gaps, filled)

Everything in this table is an assumption. Each one is justified by a decision in §2.

| Entity | Given by scenario | Assumed by me | Decision |
|---|---|---|---|
| Dealership | exists; a booking targets one | has opening hours and an IANA timezone; owns bays, technicians, service types | D1, D2 |
| ServiceBay | exists; must be available | belongs to one dealership; generic (no type); hosts one vehicle at a time | D5 |
| Technician | exists; must be available and **qualified** | belongs to one dealership; has `skills[]`; has a working window per day | D3, D4 |
| ServiceType | exists; has a **duration** (implied by "entire service duration") | belongs to one dealership; has one `required_skill` | D3 |
| Customer | exists; appears in the Appointment record | has many vehicles | D11 |
| Vehicle | exists; a booking targets one | belongs to one customer; the booking must carry a vehicle that belongs to the stated customer | D11 |
| Appointment | persisted; links customer, vehicle, technician, bay | also stores service type, dealership, `start`, `end`, `status` | D10 |
| User | "a user" requests a booking; not defined | whoever calls the API — a service advisor or a customer-facing client. The API does not distinguish; authentication is out of scope | §3 |

### 0.3 Terms, as used in this file and in the spec

- **qualified technician**: a technician whose `skills[]` contains the service type's `required_skill`. *(assumption: the scenario does not define "qualified")*
- **available** (bay or technician): no confirmed appointment overlaps the requested interval, **and** the interval lies inside the resource's working window.
- **entire service duration**: the interval `[start, start + duration)`. Both checks apply to the whole interval, not only to `start`.
- **real-time**: availability is evaluated at request time against the current state of the database, inside the same transaction that writes the appointment. Nothing is cached or precomputed. *(this is what makes requirement 2 and the concurrency decision D8 one and the same problem)*
- **confirmed**: the appointment row exists with both a bay and a technician assigned. There is no "pending" state in this scope; the scenario says check, then confirm, in one step.

## 1. Requirements this must satisfy

From the scenario:

| # | Requirement | Covered by |
|---|---|---|
| R1 | A user can request an appointment for a specific **vehicle**, **service type** and **dealership** at a **desired time** | D1, D3, D9, D10 |
| R2 | Before confirming, check availability of **both** a ServiceBay **and** a qualified Technician for the **entire** service duration | D4, D5, D6, D7, D8 |
| R3 | On success, persist an Appointment record linking **customer, vehicle, technician, service bay** | D10, D11 |

From the "backend" option:

| # | Requirement | Covered by |
|---|---|---|
| B1 | RESTful API | D9 |
| B2 | Persistent database | D8, D11 |
| B3 | Client layer mocked: OpenAPI spec + cURL examples | `openapi.yaml`, README |
| B4 | Tests that validate core business logic | spec examples → table-driven tests; one concurrency integration test |
| B5 | Consider scalability, performance, reliability, maintainability, observability | D8, D12, D13, design doc §7–8 |

---

## 2. Decisions

### D1 — Time model
- **What:** 15-minute slots. `start` must be aligned to 15 min. Stored in UTC; each dealership has an IANA timezone used for business-hours checks and for display.
- **Why:** real workshops book in slots, not arbitrary minutes. Alignment makes availability a finite list, which `GET /availability` needs.
- **Considered:** arbitrary start times — rejected, makes availability search and UX harder for no benefit here.

### D2 — Business hours
- **What:** each dealership has one opening window per day (same every day, e.g. 08:00–17:00 local). An appointment must fit **entirely** inside it: `start >= open` and `end <= close`.
- **Why:** enough to express the "entire duration" rule without modelling calendars, holidays or lunch breaks.
- **Considered:** per-weekday hours, closures — deferred (see §3).

### D3 — Service type
- **What:** a service type has `duration_minutes` and one `required_skill`.
- **Why:** duration drives the interval; required skill drives "qualified technician". One skill per service keeps matching simple and testable.
- **Considered:** multiple required skills per service — deferred; the matching rule generalises to "technician has all required skills" without changing the API.

### D4 — Technician
- **What:** belongs to one dealership; has `skills[]` and one working window per day (same shape as D2).
- **Why:** minimum needed for qualification + availability.
- **Considered:** shifts, breaks, leave — deferred; see "technician unavailable mid-day" in §3.

### D5 — Service bay
- **What:** belongs to one dealership; no type/capability. Any bay can host any service.
- **Why:** the scenario does not distinguish bays. Keeping them generic avoids a second matching dimension.
- **Considered:** bay types (lift / EV / wash) — deferred; would become a second "required capability" matched like skills.

### D6 — Availability rule
- **What:** a resource (bay or technician) is available for `[start, end)` if it has no confirmed appointment whose interval overlaps `[start, end)`. Intervals are **half-open**: an appointment ending at 10:00 does not conflict with one starting at 10:00.
- **Why:** half-open intervals make back-to-back bookings work and avoid off-by-one at boundaries.
- **Considered:** buffer time between appointments — deferred; would be `end + buffer` in the same rule.

### D7 — Assignment
- **What:** the system picks the first available bay and the first available **qualified** technician, in deterministic order (by id). The caller does not choose resources.
- **Why:** R1 says the user requests a *time*, not a bay or a technician. Deterministic order makes tests and the demo predictable.
- **Considered:** load balancing, preferred technician — deferred; assignment is one function and can be swapped.

### D8 — Concurrency & correctness
- **What:** booking runs in one DB transaction. The application checks availability first (to produce a clear 409 reason), then inserts. Postgres **exclusion constraints** on `(bay_id, tstzrange(start, end))` and `(technician_id, tstzrange(start, end))` are the final guard. A constraint violation is mapped to 409.
- **Why:** check-then-insert is a race; two concurrent requests can both pass the check. The database is the only place that can serialise correctly. The app-level check exists for a good error message, not for correctness.
- **Considered:** `SELECT ... FOR UPDATE` on resource rows — works, but the exclusion constraint is declarative and cannot be bypassed by a future code path. Application-level mutex — rejected, breaks with more than one instance.

### D9 — API
- **What:**
  - `POST /appointments` → 201 with the appointment; 409 `{reason, message}` when no bay / no qualified technician / outside hours; 400 on invalid input; 404 on unknown references.
  - `GET /appointments/{id}`
  - `GET /availability?dealership_id&service_type_id&date` → list of bookable start times.
  - `POST /appointments` accepts an `Idempotency-Key` header. Same key + same payload → same appointment, same response. Same key + different payload → 422.
- **Why:** availability is what a real client needs before it can offer the user a time. 409 reasons are machine-readable so a UI can react. Idempotency makes retries safe (mobile clients, timeouts).
- **Considered:** returning alternative slots in the 409 body — stretch goal, not required.

### D10 — Appointment record
- **What:** stores `dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, end, status`. `status` is always `confirmed` in this scope.
- **Why:** exactly R3, plus `end` denormalised so the exclusion constraint and queries do not recompute it.
- **Considered:** `pending` state with later confirmation — rejected; the scenario says check, then confirm, in one step.

### D11 — Reference data & persistence
- **What:** dealerships, bays, technicians, service types, customers, vehicles are reference data loaded by a seed script. No CRUD endpoints for them. The request carries `customer_id` and `vehicle_id`; the vehicle must belong to the customer.
- **Why:** the scenario is about booking, not managing the dealership. Seeding keeps the demo and tests on the same data.
- **Considered:** deriving the customer from the vehicle — rejected; explicit ids make the validation rule visible.

### D12 — Reliability
- **What:** stateless service; all state in Postgres. Idempotent writes. Timeouts on DB calls. Graceful shutdown.
- **Why:** so more than one instance can run behind a load balancer without coordination.

### D13 — Observability
- **What:** request-id middleware (`X-Request-ID` in / out), structured logs (slog) with `request_id, method, path, dealership_id`, logs at boundaries only (request in/out, each domain decision with its reason, each DB error). Two metrics: `booking_attempts_total{result}` and `booking_latency_seconds`. No tracing SDK; the middleware sits where OpenTelemetry would.
- **Why:** every rejected booking must be explainable from one log line. Two metrics are enough to alert on "bookings failing" and "bookings slow".
- **Considered:** full OpenTelemetry — rejected for this scope; adds setup with no reader on the other end.

---

## 3. Out of scope (deliberate)

| Not doing | Why | If I had one more sprint |
|---|---|---|
| Cancel / reschedule | Not required; adds state transitions | `DELETE /appointments/{id}` sets `status=cancelled`; constraint only considers `confirmed` |
| Notifications | Different concern, different system | Publish `AppointmentConfirmed` event; consumer sends SMS/email |
| Authentication / authorisation | Not required; would hide the booking logic behind setup | Gateway-level JWT; `dealership_id` from token, not from body |
| Multi-tenant isolation beyond `dealership_id` | One schema is fine at this size | Row-level security or schema-per-tenant once a tenant is large |
| Technician leave / sickness mid-day | Operational reality, but a separate feature | `technician_unavailability` table joined into the availability rule; existing appointments flagged for rebooking |
| Per-weekday hours, holidays, lunch breaks | Calendar modelling | Replace single window with a calendar table; rule stays the same |
| Bay types, multiple skills, buffer time | Extensions of existing rules | See D3, D5, D6 |
| Alternative slots in 409 | Nice for UX, not required | Stretch if time remains |
| Managing reference data via API | Not the scenario | Standard CRUD, nothing interesting |

---

## 4. Open questions (to resolve in the spec)

- Lower bound: can a booking be in the past? → proposed: no, `start` must be in the future (dealership local time).
- Upper bound: how far ahead? → proposed: no limit in scope.
- Same vehicle, overlapping appointments? → proposed: reject, a car cannot be in two bays.
- `GET /availability` when the date is closed / in the past → proposed: empty list, 200.

---

## 5. Challenge log

*Filled after the AI challenge pass (prompt 0). Format: question → changed / kept → why.*
