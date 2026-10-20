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
| ServiceBay | exists; must be available | belongs to one dealership; generic (no type); hosts one vehicle at a time; its working window is the dealership's opening hours | D5 |
| Technician | exists; must be available and **qualified** | belongs to one dealership; has `skills[]`; has a working window per day, intersected with the dealership's opening hours | D3, D4 |
| ServiceType | exists; has a **duration** (implied by "entire service duration") | belongs to one dealership; has one `required_skill` | D3 |
| Customer | exists; appears in the Appointment record | has many vehicles | D11 |
| Vehicle | exists; a booking targets one | belongs to one customer, not to a dealership; the booking carries the vehicle, the customer is derived from it | D11 |
| Appointment | persisted; links customer, vehicle, technician, bay | also stores service type, dealership, `start`, `end`, `status` | D10 |
| User | "a user" requests a booking; not defined | whoever calls the API — a service advisor or a customer-facing client. The API does not distinguish; authentication is out of scope | §3 |

### 0.3 Terms, as used in this file and in the spec

- **qualified technician**: a technician whose `skills[]` contains the service type's `required_skill`. *(assumption: the scenario does not define "qualified")*
- **available** (bay or technician): no confirmed appointment overlaps the requested interval, **and** the interval lies inside the resource's working window. A bay's working window is the dealership's opening hours; a technician's effective window is the intersection of their own window with the dealership's opening hours.
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
| B4 | Tests that validate core business logic | spec examples → table-driven tests; concurrency integration tests on Postgres |
| B5 | Consider scalability, performance, reliability, maintainability, observability | D8, D12, D13, design doc §7–8 |

---

## 2. Decisions

### D1 — Time model
- **What:** 15-minute slots. `start` must be aligned to 15 min. Stored in UTC; each dealership has an IANA timezone used for business-hours checks and for display. `duration_minutes`, opening hours and technician working windows are multiples of 15 min, enforced by check constraints.
- **Why:** real workshops book in slots, not arbitrary minutes. Alignment makes availability a finite list, which `GET /availability` needs.
- **Considered:** arbitrary start times — rejected, makes availability search and UX harder for no benefit here.

### D2 — Business hours
- **What:** each dealership has one opening window per day (same every day, e.g. 08:00–17:00 local). An appointment must fit **entirely** inside it: `start >= open` and `end <= close`.
- **Why:** enough to express the "entire duration" rule without modelling calendars, holidays or lunch breaks.
- **Considered:** per-weekday hours, closures — deferred (see §3).

### D3 — Service type
- **What:** a service type has `duration_minutes` and one `required_skill`. Skills live in a `skills` reference table; `required_skill` and technician skills are foreign keys to it.
- **Why:** duration drives the interval; required skill drives "qualified technician". One skill per service keeps matching simple and testable.
- **Considered:** multiple required skills per service — deferred; the matching rule generalises to "technician has all required skills" without changing the API.

### D4 — Technician
- **What:** belongs to one dealership; has `skills[]` (FK to `skills`) and one working window per day (same shape as D2). The effective window is the intersection with the dealership's opening hours, and it is checked at booking: a technician is only a candidate if `[start, end)` lies inside it.
- **Why:** minimum needed for qualification + availability.
- **Considered:** shifts, breaks, leave — deferred; see "technician unavailable mid-day" in §3.

### D5 — Service bay
- **What:** belongs to one dealership; no type/capability. Any bay can host any service. A bay's working window is the dealership's opening hours.
- **Why:** the scenario does not distinguish bays. Keeping them generic avoids a second matching dimension.
- **Considered:** bay types (lift / EV / wash) — deferred; would become a second "required capability" matched like skills.

### D6 — Availability rule
- **What:** a resource (bay or technician) is available for `[start, end)` if it has no confirmed appointment whose interval overlaps `[start, end)`. Intervals are **half-open**: an appointment ending at 10:00 does not conflict with one starting at 10:00.
- **Why:** half-open intervals make back-to-back bookings work and avoid off-by-one at boundaries.
- **Considered:** buffer time between appointments — deferred; would be `end + buffer` in the same rule.

### D7 — Assignment
- **What:** the system picks the first available bay and the first available **qualified** technician, in deterministic order (by id). Candidates are only bays and technicians of the request's `dealership_id`. The caller does not choose resources.
- **Why:** R1 says the user requests a *time*, not a bay or a technician. Deterministic order makes tests and the demo predictable.
- **Considered:** load balancing, preferred technician — deferred; assignment is one function and can be swapped.

### D8 — Concurrency & correctness
- **What:** booking runs in one DB transaction. It claims the idempotency key first (D9) and runs the 404 and 422 checks, then takes a **per-dealership advisory lock** (`pg_advisory_xact_lock`, held until commit or rollback), then checks availability (to produce a clear 409 reason), then inserts. The lock is taken with or without a key. Postgres **exclusion constraints** on `(bay_id, tstzrange(start, end))`, `(technician_id, tstzrange(start, end))` and `(vehicle_id, tstzrange(start, end))`, partial `WHERE status = 'confirmed'`, are the final guard.
  - **Constraints are the correctness guard.** No code path, current or future, can store an overlap.
  - **The lock is the liveness guard.** Concurrent inserts that clash on an exclusion constraint each wait on the other's uncommitted row and deadlock, in chains under contention; Postgres breaks one cycle per second, so losers ran past the request deadline and got 500 (ceb8c6d). With the lock, a dealership's bookings run one at a time, every check sees the rows committed before it, and bay and technician violations cannot happen within a dealership.
  - **Lock order is fixed: key, then dealership.** Every booking takes the key lock before the dealership lock and never the other way round, so the two can never form a cycle. A booking waiting on a key holds no dealership lock yet.
  - **Reference reads come before the lock.** The dealership, vehicle, service type, bays and technicians are read before the lock because reference data has no write path (D11): nothing can change them between the read and the insert. If reference data ever gets a write path, the lock moves before those reads.
  - **Every wait is bounded; the request deadline is the real bound.** `SET LOCAL lock_timeout = '1s'` is the first statement of the booking transaction, before the key claim, so the key-row wait and the dealership-lock wait are each bounded by 1 s. These two cannot deadlock, because the key always comes first. The insert's row wait can: the same vehicle at two dealerships. So the insert first raises `lock_timeout` to 2 s. A wait past its timeout → 503 with `Retry-After: 1`, no re-run.
  - **The rule: on any wait that can deadlock, `lock_timeout` must exceed `deadlock_timeout`.** Postgres's `deadlock_timeout` is 1 s by default and superuser-only, so it can't be changed from the app on RDS. If `lock_timeout` is not longer, it fires first in both transactions: the detector never picks a victim and both sides lose. With 1 s everywhere, a cross-dealership race failed one run in sixty, both bookings 503 (`6b6604c`). With 2 s on the insert, the detector aborts one side at 1 s; it gets `ErrDeadlock` and the re-run, and the other booking commits.
  - **Arithmetic.** Per attempt, up to 1 s (key) + 1 s (dealership lock) + 2 s (insert) = 4 s; with the 50–100 ms backoff and the re-run, about 8 s in theory over two attempts. The 5 s request deadline is the real bound: a deadline that passes during any wait → 503 with `Retry-After: 1`, never 500. A 500 is left for real errors.
  - **One defensive re-run.** The lock cannot cover the same vehicle booked at two dealerships at once. Any exclusion violation or deadlock gets one re-run in a new transaction, re-running the full check, after a jittered backoff of 50–100 ms. A vehicle violation on the re-run → 409 `vehicle_busy`, not stored, because that transaction was rolled back: a same-key retry is evaluated again and that result is stored. Any other → 503 with `Retry-After: 1`, nothing stored, the caller retries with the same key.
- **Why:** check-then-insert is a race. The lock closes it only for code that takes it, at one dealership. Any code path that does not take the lock, now or later (a bulk import, a reschedule), still cannot store an overlap because of the constraints, and across dealerships the vehicle constraint is the only guard; they hold across any number of instances. The app-level check exists for a good error message, not for correctness. Changed after a grading pass found the code had drifted from the previous version of this decision: after ceb8c6d the lock was taken inside the insert, after the checks, the 3-attempt retry loop stayed, and no doc mentioned the lock.
- **Considered:** constraints alone with retries — the original design; correct, but deadlocked in chains under contention. `SELECT ... FOR UPDATE` on resource rows — works, but locks more rows and the exclusion constraint is still needed as the declarative guard. Application-level mutex — rejected, breaks with more than one instance. A lock per bay and technician — finer, but needs the candidates known before locking; worth it only when one dealership saturates its lock (design §6).

### D9 — API
- **What:**
  - `POST /appointments` → 201 with the appointment; 409 `{reason, message}` when the current state blocks the booking; 422 when outside hours, `start` not aligned to 15 min, `start` not in the future, or the service type does not belong to `dealership_id`; 400 on malformed input (missing field, wrong type or format, empty or null id, `start` without offset, `Idempotency-Key` not a UUID); 404 on well-formed but unknown references, naming the first unknown in the order dealership, vehicle, service type. Unknown body fields are ignored.
  - Checks run in this order: 415 (a `Content-Type` that is not `application/json`; a missing one is accepted) → 400 → idempotency key lookup → 404 → 422 → 409.
  - 422 rules, checked in this order, first failure wins: `start` aligned to 15 min → `start` in the future → service type belongs to `dealership_id` → inside opening hours (`start >= open`, `end <= close`). Ownership comes before hours because the hours check uses the service type's duration: a service type from another dealership would give an "ends after closing" message based on a duration this dealership does not offer.
  - 409 reasons, checked in this order, first failure wins: `vehicle_busy` (the vehicle has an overlapping appointment), `no_bay`, `no_technician` (no qualified technician free). Bay and technician both busy → `no_bay`.
  - `GET /appointments/{id}`
  - `GET /availability?dealership_id&service_type_id&date` → list of bookable start times: every 15-min-aligned `start` inside opening hours where at least one bay and one qualified technician of the dealership are free for the whole `[start, end)` (D4, D6), and `start > now`. The vehicle is not considered. `date` is a local date in the dealership's timezone. A past date → empty list, 200.
  - `POST /appointments` accepts an `Idempotency-Key` header: a client-generated UUID, global (not scoped to dealership or customer); a collision is a client bug. Keys are stored in an `idempotency_keys` table (`key` PK, `request_hash`, `status`, `body`, `created_at`).
    - The key row is inserted first, inside the booking transaction; the response is written to the key row before commit, so the key row and the appointment commit together. A 409 commits the key row alone. 400/404/422 store nothing. The one 409 that is not stored comes from a vehicle violation on D8's re-run: that transaction was rolled back, so a same-key retry is evaluated again and that result is stored.
    - `request_hash` = hash of the canonical JSON of the four schema fields (`dealership_id, vehicle_id, service_type_id, start`), `start` normalised to UTC. Unknown fields, key order and whitespace do not count.
    - Same key + same hash → the stored response is replayed, including a 409. Same key + different hash → 422.
    - A concurrent request with the same key blocks on the key row's lock until the first commits or rolls back. Commit → it replays or gets 422 (this also covers same key + different payload at the same moment). Rollback (500, timeout, 503) → the key row is gone and the waiting request runs as new.
    - A key row older than 24 h is treated as absent and overwritten. A periodic job deletes rows older than 24 h, so the table does not grow without bound. It runs as a scheduled task outside the service (ECS scheduled task or cron; locally a make target), not as a loop in each instance, which would run it once per instance.
    - `X-Request-ID` on any response, including a replay, is the current request's.
  - Input rules, from red-team pass 2:
    - Ids are exact, case-sensitive strings: `" V1"` and `"v1"` are unknown ids, not `V1`.
    - Duplicate JSON keys are out of scope: the last one wins.
    - `Idempotency-Key` must be the hyphenated UUID form, in any case. Braces, no hyphens and `urn:uuid:` forms are a 400, and so is an empty header. The nil UUID is allowed.
    - A lowercase `t` or `z` in `start` is a 400.
    - `X-Request-ID` is used as given only if it matches `[A-Za-z0-9._-]{1,128}`; otherwise it is replaced with a generated one (as in `openapi.yaml`).
- **Why:** availability is what a real client needs before it can offer the user a time. 409 reasons are machine-readable so a UI can react. Idempotency makes retries safe (mobile clients, timeouts).
- **Considered:** returning alternative slots in the 409 body — stretch goal, not required.

### D10 — Appointment record
- **What:** stores `dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, end, status`. `status` is always `confirmed` in this scope. `customer_id` is copied from the vehicle's owner at booking time and does not follow later ownership changes.
- **Why:** exactly R3, plus `end` denormalised so the booked interval does not move if the service type's duration is edited later.
- **Considered:** `pending` state with later confirmation — rejected; the scenario says check, then confirm, in one step.

### D11 — Reference data & persistence
- **What:** dealerships, bays, technicians, service types, customers, vehicles are reference data loaded by a seed script. No CRUD endpoints for them. The request carries `vehicle_id`; `customer_id` is derived from the vehicle and still stored on the appointment (D10). Every vehicle has an owner (`vehicles.customer_id NOT NULL`). Vehicles belong to customers, not dealerships: any vehicle can be booked at any dealership. Only the service type, bay and technician must belong to `dealership_id`; the service type is checked (else 422), bays and technicians are only ever picked from that dealership (D7).
- **Why:** the scenario is about booking, not managing the dealership. Seeding keeps the demo and tests on the same data.
- **Considered:** carrying `customer_id` in the request — rejected; it only adds a mismatch failure mode.

### D12 — Reliability
- **What:** stateless service; all state in Postgres. Idempotent writes. Timeouts on DB calls. Graceful shutdown. `now` comes from an injected clock; tests fix it and never read the wall clock.
- **Why:** so more than one instance can run behind a load balancer without coordination.

### D13 — Observability
- **What:** request-id middleware (`X-Request-ID` in / out), structured logs (slog) with `request_id, method, path, dealership_id` (when known), logs at boundaries only (request in/out, each domain decision with its reason, each DB error). Two metrics: `booking_attempts_total{result}`, `result` one of `created`, `replayed`, `conflict`, `invalid`, `unavailable`, `error`; and `booking_latency_seconds`. No tracing SDK; the middleware sits where OpenTelemetry would.
- **Why:** every rejected booking must be explainable from one log line. Two metrics are enough to alert on "bookings failing" and "bookings slow".
- **Considered:** full OpenTelemetry — rejected for this scope; adds setup with no reader on the other end.

---

## 3. Out of scope (deliberate)

| Not doing | Why | If I had one more sprint |
|---|---|---|
| Cancel / reschedule | Not required; adds state transitions | `DELETE /appointments/{id}` sets `status=cancelled`; constraints already only consider `confirmed` |
| Notifications | Different concern, different system | Publish `AppointmentConfirmed` event; consumer sends SMS/email |
| Authentication / authorisation | Not required; would hide the booking logic behind setup | Gateway-level JWT; `dealership_id` from token, not from body |
| Idempotency key scoped per customer | Needs authentication to know the customer | Scope the key to the authenticated caller |
| Retry after key expiry | A retry of a successful booking more than 24 h later gets 409 `vehicle_busy`, though the booking exists | On `vehicle_busy`, return the existing appointment if it matches the payload |
| Multi-tenant isolation beyond `dealership_id` | One schema is fine at this size | Row-level security or schema-per-tenant once a tenant is large |
| Technician leave / sickness mid-day | Operational reality, but a separate feature | `technician_unavailability` table joined into the availability rule; existing appointments flagged for rebooking |
| DST tests | The seed timezone has no DST; the IANA tz library handles it | Add a dealership in a DST zone and test the change days |
| Per-weekday hours, holidays, lunch breaks | Calendar modelling | Replace single window with a calendar table; rule stays the same |
| Bay types, multiple skills, buffer time | Extensions of existing rules | See D3, D5, D6 |
| Smarter technician assignment | Optimisation. Cost of D7: first-by-id can put the only technician with a rare skill on a job anyone could do, so a later booking that would have fit gets a 409 | Prefer the least-specialised qualified technician |
| Alternative slots in 409 | Nice for UX, not required | Stretch if time remains |
| Managing reference data via API | Not the scenario | Standard CRUD, nothing interesting |

---

## 4. Open questions (to resolve in the spec)

- ~~Lower bound: can a booking be in the past?~~ → resolved: no, `start > now`, else 422 (D9).
- ~~Upper bound: how far ahead?~~ → resolved: no upper bound.
- ~~Same vehicle, overlapping appointments?~~ → resolved: reject with 409 `vehicle_busy`, across dealerships too. Enforced by the `(vehicle_id, range)` exclusion constraint (D8).
- ~~`GET /availability` when the date is in the past?~~ → resolved: empty list, 200 (D9). There are no closed dates (D2).

---

## 5. Challenge log

*Filled after the AI challenge pass (prompt 0). Format: question → changed / kept → why.*

| # | Point | Verdict | Why |
|---|---|---|---|
| 1 | §4 rejects overlapping bookings for one vehicle, but D8 only guards bay and technician | changed: exclusion constraint on `(vehicle_id, range)` | an app-level check alone races, same argument as D8 |
| 2 | `status` is always `confirmed`, yet §3 relies on constraints considering only `confirmed` | changed: constraints partial `WHERE status='confirmed'`, column kept | cancel later needs no exclusion-constraint change; widening the status CHECK is one `ALTER` |
| 3 | 409 for "outside hours" is not a state conflict | changed: 409 only for no bay / no technician; 422 for outside hours, unaligned, cross-dealership; 400 malformed | 409 means current state blocks it; 422 means the request itself is invalid |
| 4 | "available" uses the resource's working window, but bays have none | changed: bay window = dealership hours | makes §0.3 apply to bays |
| 5 | relation between technician window and dealership hours undefined | changed: technician effective window = intersection with dealership hours | a technician cannot work while the dealership is closed |
| 6 | D7 first-by-id can waste a scarce skill and reject a later booking that would fit | kept: cost noted in §3 | real cost, but an optimisation |
| 7 | carrying `customer_id` adds a mismatch failure mode for no benefit | changed: dropped from request, derived from vehicle, still stored | vehicle already determines the customer |
| 8 | reason for denormalised `end` was weak | changed: booked interval must not move if duration is edited later | the stronger, correctness reason |
| 9 | "future in dealership local time" is meaningless; now is an instant | changed: `start > now` | timezone only matters for hours and dates |
| 10 | constraint violation on candidate 1 returns 409 while candidate 2 is free | changed: retry with next candidate in a new tx, max 3, then 409 (superseded by §6 #6: 503; then superseded by D8 after the grading pass: per-dealership lock, one defensive re-run) | avoids false 409s under concurrency |
| 11 | idempotency storage, scope, TTL, concurrent same key, replayed 409 undefined | changed: `idempotency_keys` table (key PK, request_hash, status, body, created_at), key inserted first in tx, same key waits then replays incl. 409, different hash 422, TTL 24 h | PK serialises same-key requests; stored response makes replays exact |
| 12 | durations and hours not required to be multiples of 15 | changed: check constraints, duration and hours multiples of 15 | keeps D1's slot grid valid |
| 13 | `date` in `GET /availability` local or UTC undefined | changed: dealership local date | DST days stay correct |
| 14 | free-text skills: one typo means no qualified technician, silently | changed: `skills` reference table + FK | typos fail at write time |
| 15 | references from another dealership not handled | changed: all referenced ids must belong to `dealership_id`, else 422 | no cross-dealership bookings |
| 16 | `dealership_id` unknown on `GET /appointments/{id}` before lookup | changed: `dealership_id` in logs when known | field is honest, not empty |

---

## 6. Spec review log

*Filled while turning the cases into the booking spec. Same format as §5.*

| # | Point | Verdict | Why |
|---|---|---|---|
| 1 | 409 reason when bay and technician are both busy undefined | changed: order bay → technician, both busy → `no_bay` | one deterministic reason per state |
| 2 | vehicle overlap had no 409 reason | changed: `vehicle_busy` as third reason, checked first | the caller's own car is the blocker, not the workshop |
| 3 | §5 #15 required the vehicle to belong to the dealership, but vehicles belong to customers | changed: only service type, bay, technician are dealership-scoped | a customer can take a car to any dealership |
| 4 | unknown body fields (e.g. `bay_id`) undefined | changed: ignored | the caller does not choose resources (D7) |
| 5 | candidates could come from another dealership | changed: bays and technicians filtered by `dealership_id` (D7) | the request names one dealership |
| 6 | outcome after 3 lost races undefined | changed: 503 + `Retry-After`, nothing stored (D8; now after the one re-run) | not a state conflict; a retry with the same key can succeed |
| 7 | vehicle constraint violation retried pointlessly | changed: 409 `vehicle_busy` immediately (D8; superseded: it gets the one re-run, whose fresh check returns the 409) | no other candidate can fix it |
| 8 | key row and appointment could be written apart | changed: one transaction, response on key row before commit; rollback removes the key row (D9) | no booking without its key |
| 9 | key scope, payload equality, expiry and request id on replay undefined | changed: global client UUID; hash of 4 canonical fields, `start` in UTC; >24 h treated as absent; current `X-Request-ID` (D9) | each retry case has one answer |
| 10 | check order with an existing key unclear | changed: 400 → key lookup → 404 → 422 → 409 (D9) | a replay must not fail validation that passed the first time |
| 11 | technician hours not stated as checked | changed: checked at booking (D4) | the seed has a short shift |
| 12 | 400 vs 404 for edge inputs; which unknown the 404 names | changed: wrong type/format 400, well-formed unknown 404, first unknown in order dealership, vehicle, service type (D9) | deterministic error per input |
| 13 | `customer_id` live or copied | changed: copied at booking; vehicle owner NOT NULL (D10, D11) | the record does not change after the fact |
| 14 | "closed date" in availability has no meaning (D2) | changed: line dropped, past date → empty list | no calendar in scope |
| 15 | no upper bound on `start`; DST | kept: no upper bound; DST left to the IANA tz library, no DST tests | out of scope |
