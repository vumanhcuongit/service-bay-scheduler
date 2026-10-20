# Service Bay Scheduler

A Go and Postgres backend that books service appointments for a car dealership: it assigns a free bay and a qualified technician for the whole job, and serves the advisor or app that takes the booking.

## The problem

A dealership that books service work by phone and by hand double-books bays and technicians, or puts a job on someone who cannot do it ([docs/design.md](docs/design.md#1-problem)). The advisor needs to know, while the customer is still on the line, which times actually work for this service. Once a time is confirmed, it has to hold: a bay and a qualified technician are reserved for the whole job, and no other booking can take them.

## What it does

- `POST /appointments` books a vehicle for a service type at a dealership and a start time, and picks the bay and the technician itself.
- `GET /appointments/{id}` returns a confirmed appointment.
- `GET /availability` lists the start times on a local date where a bay and a qualified technician are both free for the whole service.

A booking for 09:00 local time in Ho Chi Minh City (+07:00) gets bay B1 and technician T1, and the times come back in UTC:

```sh
curl -i -X POST localhost:8080/appointments \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 6f1c2b7e-4c1a-4e8e-9a51-3d2b8f0c9a11' \
  -d '{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00"}'
```

```
HTTP/1.1 201 Created
{"id":"72dce296-a25c-49f7-8093-7138a81538dd","dealership_id":"DL1","customer_id":"C1","vehicle_id":"V1","service_type_id":"OIL","bay_id":"B1","technician_id":"T1","start":"2026-10-21T02:00:00Z","end":"2026-10-21T02:30:00Z","status":"confirmed"}
```

The same vehicle at 09:15 overlaps that booking. The 409 says why in a machine-readable `reason`:

```sh
curl -i -X POST localhost:8080/appointments \
  -H 'Content-Type: application/json' \
  -d '{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:15:00+07:00"}'
```

```
HTTP/1.1 409 Conflict
{"reason":"vehicle_busy","message":"vehicle V1 already has an appointment overlapping 09:15–09:45"}
```

## The design decision that matters

Two requests ask for 09:00 tomorrow at the same dealership while only one bay is left. Both run the availability check, both see the same bay free, and both go on to insert. Without a guard below the application, that bay now has two cars at 09:00, which is the double booking the dealership had by hand ([docs/design.md](docs/design.md#1-problem)).

A check in Go cannot close that gap, because it reads the calendar before it writes, and the other request can commit in between. An in-process lock does not close it either: it serialises requests inside one process, so with two instances behind a load balancer each holds its own lock and both still insert. Decision D8 in [docs/decisions.md](docs/decisions.md) lists the alternatives and why they were rejected.

The guard is in Postgres, as three exclusion constraints on `appointments`, one each for the bay, the technician and the vehicle ([migrations/00001_schema.sql](migrations/00001_schema.sql)). The bay one reads:

```sql
EXCLUDE USING gist (bay_id WITH =, tstzrange(start, "end") WITH &&) WHERE (status = 'confirmed')
```

Postgres rejects a second confirmed row whose time range overlaps another on the same bay, whichever instance or code path wrote it, so no code path and no number of instances can store an overlap, and the Go overlap check only exists to say why a booking was refused. Skill, opening hours and dealership membership are checked in Go, not by the database. The schema is in [docs/design.md](docs/design.md#5-data-model).

The constraint keeps the calendar correct; a per-dealership advisory lock, taken right after the idempotency key and before any check, keeps it live. Both are needed. On its own the constraint deadlocks under contention: inserts that clash on it wait on each other's uncommitted rows, in chains, and losers ran past the deadline into 500s (`ceb8c6d`). On its own the lock is a convention a future code path could skip. Together, a dealership's bookings run one at a time, every loser gets a clean 409, and the one race left, the same vehicle at two dealerships, gets one re-run.

## Where to look first

With ten minutes, read these in order:

1. [migrations/00001_schema.sql](migrations/00001_schema.sql): the three exclusion constraints at the end of the `appointments` table.
2. [internal/service/booking.go](internal/service/booking.go): the check order (key, dealership lock, 404, 422, 409) and the one re-run after a constraint violation.
3. [spectest/concurrency_test.go](spectest/concurrency_test.go): the races, run against a real Postgres with real commits.
4. [docs/reviews/](docs/reviews/): what the AI found in the decisions and the spec, and what I ruled on each point.

## How it was built

I wrote [docs/decisions.md](docs/decisions.md) first (`3dc31d7`), along with the seed data and the key examples. From them, the AI wrote the [spec](docs/superpowers/specs/2026-10-20-booking.md), the [plan](docs/superpowers/plans/2026-10-20-backend.md) and the code. A second agent with no context red-teamed the spec twice, once before it was frozen and once against the running server, and a third reviewed every PR. The red-team output is in [docs/reviews/](docs/reviews/), and each fix from a review is a `fix(ai):` commit ending "caught in <where>": PR review, final review, red-team or CI. I read every PR diff and ruled on every finding.

The spec has 63 examples, and every one is a test with the same name, `TestM1` to `TestM25` and `TestN1` to `TestN38` in [spectest/](spectest), out of 106 test functions in total. `make layers` enforces the import rule between the layers in CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)). The concurrency tests commit to a real Postgres instead of rolling back ([internal/testdb/testdb.go](internal/testdb/testdb.go)), and one of them fires ten bookings at the same 09:00 slot, where two bays are free, and expects exactly two to win ([spectest/concurrency_test.go](spectest/concurrency_test.go)). `make races` runs them 20 times in a row, in `make ci` and in CI.

## Run it

You need Go 1.24 and Docker.

```sh
make db-up migrate run   # Postgres 16 in Docker, migrations + seed, server on :8080
make test                # every spec example against Postgres
make ci                  # vet, layer check, lint, test, 20 concurrency runs, as CI runs them
```

The commands, the seed data and the full curl list are in [More examples](#more-examples) at the end.

## AI collaboration

How the AI was directed in the design phase, where I decided and it challenged, red-teamed and questioned, is in [docs/design.md](docs/design.md#10-how-genai-was-used-in-design).

The checking did not rely on the AI's own account of its work. The tests came from the spec, so "done" meant the spec examples passed, not that the code looked right, and the plan made the AI stop after each task and show the diff, which I read ([docs/superpowers/plans/2026-10-20-backend.md](docs/superpowers/plans/2026-10-20-backend.md)). `make layers` fails the build on an import that breaks the layer rules, handler → service → model and repository → model ([Makefile](Makefile)). A reviewer agent in a clean context went over each PR and a red-team agent went over the spec twice ([docs/reviews/](docs/reviews/)), and every fix that came out of review, red-team or CI is a `fix(ai):` commit that says what was wrong and how it was caught.

Three of the 20 `fix(ai):` commits show where it was wrong. In `ceb8c6d`, concurrent inserts on one slot deadlocked (40P01) and ran past the request deadline, so the losing requests got a 500; the final review caught it. In `526882b`, the check order was built but not tested, so swapping any two checks still passed; PR review caught it. In `cb3d5ce`, an empty `Idempotency-Key` header was treated as no key, so the booking ran without one; the second red-team pass caught it ([docs/reviews/red-team-2.md](docs/reviews/red-team-2.md) #6). It also pushed back on me and was right: my first D9 returned 409 for a booking outside opening hours (`3dc31d7`), it argued that 409 means the current state blocks a booking while this request is invalid in itself, and the decision changed to 422 ([decisions.md §5](docs/decisions.md#5-challenge-log) #3).

For a team, I would standardise four things from this. Decisions and an example-based spec are written by a person and frozen before any AI writes code. Every spec example becomes a test with the same name. Review and red-teaming run in a separate agent with a clean context. AI fixes are committed as `fix(ai): <what was wrong>, caught in <where>`, so the history records where the AI fails and which check caught it.

## Out of scope

Cancelling, rescheduling, notifications and authentication are left out on purpose. So are calendars with holidays and breaks, technician leave and smarter assignment. Each one has a reason and a next step in [docs/decisions.md](docs/decisions.md#3-out-of-scope-deliberate).

## Where to read more

- [docs/design.md](docs/design.md): the architecture, the data flow, the tech choices and how the service scales.
- [docs/decisions.md](docs/decisions.md): every rule with its reason and the alternatives, plus the AI challenge log.
- [docs/superpowers/specs/2026-10-20-booking.md](docs/superpowers/specs/2026-10-20-booking.md): the behaviour, as the 63 examples the tests run.
- [openapi.yaml](openapi.yaml): the API contract.
- [docs/reviews/](docs/reviews/): the verbatim output of the challenge and red-team agents, with my ruling on each point.

## More examples

`DATABASE_URL` defaults to the Compose database and `ADDR` to `:8080`. `make cleanup-keys` deletes `Idempotency-Key` rows older than 24 hours.

The seed data (dealerships DL1 and DL2, their bays, technicians and service types, and vehicles V1 to V3) is migration `00002`, so the tests and the demo use the same data. It is applied wherever migrations run, AWS included. That is accepted because reference data has no API (D11).

Times are sent with an offset and returned in UTC. DL1 is in `Asia/Ho_Chi_Minh` (+07:00) and opens 08:00–17:00. A start must be in the future, so pick a date after today.

- Book: the first `POST /appointments` above.
- Replay: send the same request with the same `Idempotency-Key` again. It returns the stored 201 with the same id and books nothing new. The same key with a different body is a 422.
- Conflict: the second `POST /appointments` above.
- Read an appointment:

  ```sh
  curl -i localhost:8080/appointments/<id>
  ```

- Bookable start times for a service type on a local date:

  ```sh
  curl -i 'localhost:8080/availability?dealership_id=DL1&service_type_id=OIL&date=2026-10-21'
  # 200 {"dealership_id":"DL1","service_type_id":"OIL","date":"2026-10-21","start_times":["2026-10-21T01:00:00Z",…]}
  ```

- Health and metrics:

  ```sh
  curl localhost:8080/healthz                                  # {"status":"ok"}
  curl -s localhost:8080/metrics | grep booking_attempts_total # by result: created, replayed, conflict, invalid, unavailable, error
  ```

Every response carries `X-Request-ID`, either the one sent or a generated one, and every log line for the request carries it as `request_id`. A sent id is kept if it is 1–128 characters of letters, digits, `.`, `_` and `-`; anything else is replaced with a generated one. `/healthz` and `/metrics` are not access logged.
