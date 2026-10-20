# Red-team pass 1 — spec, before freeze

Agent: red-teamer, new session, clean context. Input: the booking spec and `openapi.yaml`. It was not given `decisions.md`, the brainstorm transcript, or any code, and the prompt told it so. Asked for cases where the system could do the wrong thing and the spec does not say what should happen.

Result: 20 points. 16 in (spec or decisions changed), 3 out of scope, 1 deferred. The changes are logged in [decisions.md §6](../decisions.md#6-spec-review-log) and landed in `24776f8`.

## Verdicts

| # | Verdict | Decision |
|---|---|---|
| 1 | in | candidates filtered by `dealership_id` (D7); test with DL2 |
| 2 | in | concurrency case: same vehicle, no key, exactly one wins |
| 3 | in | after 3 lost races → 503 + `Retry-After`, nothing stored; caller retries with the same key |
| 4 | in | key row and appointment commit together; response written to the key row before commit |
| 5 | in | if the first request rolls back, the key row goes with it; the waiting duplicate runs as new |
| 6 | in | covered by the lock on the key row; stated |
| 7 | in | key is global, client UUID; collision is a client bug. Per-customer scope needs auth → out |
| 8 | in | same payload = hash of canonical JSON of the 4 schema fields, `start` normalised to UTC |
| 9 | in | key row older than 24 h treated as absent, overwritten |
| 10 | deferred | accepted consequence, noted as behaviour in decisions §3 |
| 11 | in | order: 400 → key lookup → 404 → 422 → 409 |
| 12 | in | technician hours checked (D4); seed changed so the rule is testable (T2 ends 13:00) |
| 13 | out | decided in §4: no upper bound |
| 14 | out | IANA tz library; no DST tests; one line in §3 |
| 15 | in | vehicle constraint violation → 409 `vehicle_busy` immediately, no retry; only bay/technician violations retry |
| 16 | in | `customer_id` copied at booking; vehicle owner NOT NULL |
| 17 | in | `X-Request-ID` is the current request's |
| 18 | in | wrong type/format → 400; well-formed unknown → 404 |
| 19 | in | 404 names the first unknown in the order dealership, vehicle, service type |
| 20 | in | "closed date" line dropped; past date → empty list |

## Agent output, verbatim

1. Nothing says the assigned bay and technician must belong to the booking's dealership, so "first free by id" could hand out another dealership's bay or technician.
2. Two requests at the same time for the same vehicle, at overlapping times and without a key, could land on different bays. Only the one-after-another case (N19) and the across-dealership case (N17) are tested, so we don't know the database stops a double booking.
3. After 3 failed attempts under contention, the spec doesn't say what the caller gets: a 409 (with which reason?), a 500, or something else.
4. Nothing says the appointment and its idempotency key are saved together. A crash between the two writes could leave a booking with no key, and a retry would then get `vehicle_busy`, or a second booking if the vehicle check also slips.
5. If the first request with a key fails with a 500 or a timeout while a duplicate is waiting on it, the spec doesn't say what the duplicate gets. Rule 8 only covers 201, 409 and 400/404/422.
6. Two requests with the same key but different payloads at the same moment: nothing says one must win and the other get a 422, instead of both going through.
7. The scope of a key isn't defined (global, per dealership, per customer). With no auth, a guessed or colliding key replays someone else's appointment, or blocks their booking with a 422.
8. "Same payload" isn't defined. It's unclear whether `09:00+07:00` and `02:00Z` match, and whether unknown fields like `bay_id`, key order, or whitespace count. A real retry could get a 422, or a different request could get a replay.
9. An expired key (over 24 h) that hasn't been deleted yet could still trip the unique constraint when the key is reused, instead of being "treated as new".
10. A client retry of a successful booking after the key expires gets 409 `vehicle_busy`, which reads as "your booking failed" when it actually succeeded.
11. A malformed or invalid body sent with an existing key: lookup comes "before any validation", so it's unclear whether that's 400, 422 (payload mismatch), or a replay.
12. Technicians have working hours in the seed data, but no rule says they're checked. A technician with shorter hours could be booked outside their shift.
13. There's no limit on how far ahead `start` can be (year 2099 or 9999 would be accepted).
14. Opening hours are local, but nothing covers days where the clock changes (DST) for any dealership that isn't on a fixed offset like Asia/Ho_Chi_Minh.
15. If the 1st attempt loses a race and the 2nd fails for another reason, it's unclear whether the reason is re-checked in order (`vehicle_busy` → `no_bay` → `no_technician`) or taken from the last attempt. It's also unclear whether a vehicle-overlap violation triggers pointless retries instead of an immediate `vehicle_busy`.
16. A vehicle with no customer, or one whose owner changes, has no defined `customer_id` behaviour (copied at booking time or looked up live?).
17. A replayed response doesn't say whether `X-Request-ID` is the original one or the new request's.
18. Edge inputs aren't classified as 400 vs 404 vs 422: empty-string ids, `null`, numbers in place of strings, `09:00:00.000` with zero fractional seconds.
19. When more than one reference is unknown, nothing says which one the 404 names.
20. The availability endpoint says "a closed date returns an empty list", but with no per-weekday hours or holidays there's no such thing as a closed date. It's unclear what that line refers to.

> I didn't read `docs/decisions.md`, so some of these may be settled there but not carried into the spec.
