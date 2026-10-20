# Service Bay Scheduler

A Go service that books service appointments at a dealership, assigning a free bay and a
qualified technician for the whole service duration. Postgres exclusion constraints are the
final guard against overlapping bookings.

- Behaviour: [`docs/superpowers/specs/2026-10-20-booking.md`](docs/superpowers/specs/2026-10-20-booking.md)
- API: [`openapi.yaml`](openapi.yaml)
- Design and decisions: [`docs/design.md`](docs/design.md), [`docs/decisions.md`](docs/decisions.md)

## Run

Needs Go 1.24 and Docker.

```sh
make db-up migrate run   # Postgres 16 in Docker, migrations + seed, server on :8080
make test                # every spec example against Postgres (database scheduler_test)
make ci                  # vet, layer check, lint, test, as CI runs them
make cleanup-keys        # delete Idempotency-Key rows older than 24 h
```

`DATABASE_URL` defaults to the Compose database; `ADDR` defaults to `:8080`.

The seed data (dealerships DL1/DL2, bays, technicians, service types, vehicles V1–V3) is
migration `00002`, so tests and the demo share it. It is applied wherever migrations run, AWS
included. That is accepted because reference data has no API (D11).

## Examples

Times are sent with an offset and returned in UTC. DL1 is in `Asia/Ho_Chi_Minh` (+07:00) and
opens 08:00–17:00. A start must be in the future, so pick a date after today.

Book an oil change; the bay and technician are assigned:

```sh
curl -i -X POST localhost:8080/appointments \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 6f1c2b7e-4c1a-4e8e-9a51-3d2b8f0c9a11' \
  -d '{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00"}'
# 201 {"id":"…","bay_id":"B1","technician_id":"T1","start":"2026-10-21T02:00:00Z","end":"2026-10-21T02:30:00Z","status":"confirmed",…}
```

Replay: the same request with the same `Idempotency-Key` returns the stored response and books
nothing new. The same key with a different body is a 422.

```sh
# run the command above again
# 201 with the same id
```

Conflict: V1 already has an appointment at 09:00, so a second booking overlapping it is a 409:

```sh
curl -i -X POST localhost:8080/appointments \
  -H 'Content-Type: application/json' \
  -d '{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:15:00+07:00"}'
# 409 {"reason":"vehicle_busy","message":"vehicle V1 already has an appointment overlapping 09:15–09:45"}
```

Read an appointment:

```sh
curl -i localhost:8080/appointments/<id>
```

Bookable start times for a service type on a local date:

```sh
curl -i 'localhost:8080/availability?dealership_id=DL1&service_type_id=OIL&date=2026-10-21'
# 200 {"dealership_id":"DL1","service_type_id":"OIL","date":"2026-10-21","start_times":["2026-10-21T01:00:00Z",…]}
```

Health and metrics:

```sh
curl localhost:8080/healthz                                  # {"status":"ok"}
curl -s localhost:8080/metrics | grep booking_attempts_total # by result: created, replayed, conflict, invalid, unavailable, error
```

Every response carries `X-Request-ID` (sent or generated), and every log line for the request
carries it as `request_id`. A sent id is kept if it is 1–128 characters of letters, digits, `.`,
`_` and `-`; anything else is replaced by a generated one. `/healthz` and `/metrics` are not
access logged.
