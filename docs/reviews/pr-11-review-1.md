# PR #11 review 1 — `fix/concurrency-story`, first pass

Agent: reviewer, new session, clean context. Given the spec, `decisions.md`, `design.md`, the plan and `git diff origin/main...HEAD` on `fix/concurrency-story` at `ed17c38` plus docs commits. Asked to check: the code against D8; lock order; the one re-run; tests passing by luck; docs still describing the old design; layering. It ran mutations (removed the lock, moved it) and probed a 4-connection pool.

Outcome: `8d23da7` (lock after 404/422, `lock_timeout`, no-op guard, lock-order test, cross-dealership race test, re-run matrix) and docs commits. One point kept.

## Verdicts

| # | Finding | Verdict | What happened |
|---|---|---|---|
| 1 | Lock before the checks: 404/422 queue behind it; waiters hold pool connections; deadline expiry → 500; a hot dealership starves others on a shared pool | **changed** | lock moved after 404/422 and before the availability read; `SET LOCAL lock_timeout` with 55P03 → 503; pool cost documented in §8.5 |
| 2 | The cross-dealership vehicle race has no real concurrent test; the fixture uses a DL1 row while its comment says another dealership | **changed** | `TestVehicleAtTwoDealerships` in `make races`; fixture uses a DL2 row |
| 3 | Nothing pins the lock order; moving the lock before the key claim passes all tests | **changed** | `TestBookCallOrder` with a recording fake repository |
| 4 | Re-run error matrix only partly covered (deadlock on re-run, vehicle then bay, deadlock then vehicle) | **changed** | flaky repository takes a sequence of errors; three cases added |
| 5 | design §4 "without a key, steps 3–4 are skipped" now hides the lock | **changed** | wording |
| 6 | Old-design reasoning still in README, design §5 and D8 ("both see the bay free"); D8 history reversed | **changed** | rewritten: the constraint matters for any path that does not take the lock; "drifted from the previous version of this decision" |
| 7 | Plan file still describes the three-attempt loop | **changed** | one line at the top: superseded on concurrency by D8 |
| 8 | `LockDealership` is a silent no-op outside a transaction | **changed** | returns an error; precondition documented |
| 9 | D9 and D8 disagree on whether a re-run's 409 is stored | **changed** (doc) | not stored, because that transaction rolled back; a same-key retry is evaluated again |
| 10 | "hundreds of bookings a second" unmeasured | **changed** (doc) | replaced with a measured local figure, later removed again by grading pass 2 as a test duration, not a benchmark |
| 11 | Dockerfile, `.dockerignore`, `docker-build` and the timezone cache are bundled into the concurrency fix | **kept** | already committed; listed in the PR description; the reviewer was right about scope |
| 12 | Wording: "caught in" list incomplete; `ErrUnavailable` comment; 503 message "for this slot" | **changed** | all three |

## Agent output, verbatim

I reviewed `origin/main...HEAD` (17 files). Local `main` is stale at `1900457`, so the `git diff main...fix/concurrency-story` command from your brief shows the whole repo instead of this PR.

**Findings, most serious first**

1. **`internal/service/booking.go:109`, `internal/repository/repository.go:31`: a hot dealership's lock queue starves the shared pool and ends in 500s, not 409/503.**
   - Each booking waiting on the lock holds a pool connection and runs under the 5 s request deadline. A deadline expiry during the wait or during the backoff sleep at `booking.go:69` maps to 500 (`handler.go:158`). `timeout_test.go` asserts that 500.
   - 404 and 422 requests also queue, because the lock now comes before the checks.
   - I probed this with a 4-connection pool, the size CI uses. I held `dealership/DL1` from another session and queued 4 DL1 bookings. A DL2 booking started 0.3 s later waited 1.24 s and only got a connection when the DL1 waiters timed out. Availability uses the same pool, but I did not measure it.
   - This contradicts `design.md:263` ("dealerships don't share a lock, so adding dealerships doesn't make it worse"), `design.md:111` ("Never 500") and `README.md:56` ("every loser gets a clean 409").
   - The lock is now held for the whole transaction, not just the insert, so the queue is longer than before.

2. **`spectest/concurrency_test.go:137-152`, `env_test.go:212-219`: the one race the lock leaves has no real concurrent test, and the fixture does not do what its comment says.**
   - N37 (`:52-62`) is same-dealership, so the lock serialises it. It never reaches the constraint or the re-run.
   - `TestVehicleViolationRerunStores409` injects the error. Its competing row comes from `given("V1","OIL",…)`, and OIL belongs to DL1 (`00002_seed.sql:28`). That is a DL1 row, not "another dealership" as the comment at `:137-138` says.
   - My scratch probe (V1 at DL1 OIL ‖ V1 at DL2 TIRE, 60 pairs) gave 201+409 every time. Nothing shows the re-run path actually ran.

3. **`booking.go:93-111`: no test pins the lock order.**
   - I put the dealership lock before the key claim. `go test ./...` and `make races` (×20) both passed.
   - Moving the lock to just before the insert, which is the drifted design, passed a single `go test ./...`. `make races` caught it, with `TestTenRacers` failing in 13 of 20 iterations. Only the 20-run target protects the ordering, and only probabilistically.

4. **`concurrency_test.go:101-135`: the re-run matrix is only partly covered.**
   - Covered: bay→bay (N38), vehicle→vehicle, and any of bay, technician or deadlock followed by success.
   - Not covered: deadlock on the re-run → 503, vehicle then bay/technician/deadlock → 503, deadlock then vehicle → 409.
   - Spec rule 11 and `openapi.yaml:98` both promise the deadlock-on-re-run case.

5. **`design.md:121`: "Without a key, steps 3–4 … are skipped."** Step 4 now contains the dealership lock. The code locks whether or not there is a key (`booking.go:93` vs `:108`). A reader would conclude keyless requests skip the lock.

6. **Old-design reasoning still in the text, contradicting the new design.**
   - `design.md:161` says two requests "can both see a free bay and both try to take it". `design.md:106` says that "cannot start" within a dealership.
   - `README.md:44` and `:46` still say both requests see the bay free and that "a check in Go cannot close that gap". That is false within a dealership now.
   - `decisions.md:111` still says "two concurrent requests can both pass the check".
   - `decisions.md:111` also says the code "had drifted from this decision". That decision was written in `ba9a339`, after the code change in `ed17c38`, so the history is reversed.

7. **`docs/superpowers/plans/2026-10-20-backend.md:9, 29, 78, 475-477, 488, 497-499, 549, 581-583` still describe the old design.**
   - It has the 3-attempt loop, `f.inserts == 3`, and the vehicle re-run "not counted toward the 3".
   - It is not marked superseded. It is also one of the inputs the reviewer is told to read (`design.md:281`).

8. **`writes.go:35-36`, `repository.go:44-49`: `LockDealership` is a silent no-op outside a transaction.**
   - `q(ctx)` falls back to the pool, and a transaction-scoped advisory lock taken in autocommit is released when the statement ends. It returns nil and locks nothing.
   - `service/repository.go:34-35` states no precondition, and `InsertAppointment` does not require the lock.
   - This is based on Postgres semantics; I did not run it.

9. **`decisions.md:123` vs `booking.go:74-75`: D9 and D8 disagree on whether the re-run's 409 is stored.**
   - D9 says "a 409 commits the key row alone" and replays it. The re-run's second `vehicle_busy` returns 409 without storing it, and `:112-118` asserts 0 key rows.
   - Only `design.md:111` says so. D8 (`decisions.md:110`) and spec rule 11 are silent.
   - A same-key retry gets a fresh evaluation, not a replay.

10. **`design.md:263`: "a few milliseconds each … hundreds of bookings a second" is unmeasured.**
    - `design.md:283` says every claim in §7–8 must be checked or marked as an estimate. The "estimates" caveat at `:255` sits above the list, not on this item.
    - Locally, `TenRacers` runs 10 serialised bookings in 0.12–0.15 s, including setup. That is roughly 12–15 ms each, so about 70 a second on localhost.

11. **`ed17c38` bundles changes unrelated to concurrency:** `Dockerfile`, `.dockerignore`, the Makefile `docker-build` target, and the `sync.Map` timezone cache at `reads.go:121-134`.
    - None is mentioned in the commit subject, the docs or the README. The cache has no test, and `docker-build` is not in `make ci` or CI.
    - They make the PR's scope hard to review.

12. **Smaller wording drift.**
    - `README.md:69` lists the "caught in" values as "PR review, final review, red-team or CI". It omits "grading pass" (`ed17c38`, this PR) and "reading the readme" (`cf92fa6`).
    - `model.go:113` says `ErrUnavailable` means "constraint violation on the re-run too". Deadlock also lands there, and `openapi.yaml:98` says both.
    - The 503 message at `handler.go:156` says "for this slot", but only the cross-dealership vehicle case can reach it now.

**Error handling**
- **First attempt:**
  - Bay, technician, vehicle or deadlock errors trigger the one re-run.
  - 404 and 422 return as they are.
  - Anything else returns 500: a ctx error, a pg error, or a 23P01 whose constraint name is not in `constraintErrors` (`writes.go:57-58`). Today only the three mapped exclusion constraints exist (`00001_schema.sql:71-75`).
- **Re-run:**
  - Vehicle violation → 409, not stored.
  - Bay, technician or deadlock → 503.
  - Anything else → 500.
- **Paths to 500 on a violation or deadlock:**
  - Deadline expiry (item 1).
  - An unmapped constraint name.
  - I reasoned, without testing it, that a 40P01 can only surface in an insert, never in `ClaimKey` or the lock.

**D8 and lock order**
- The code matches D8: key claim at `booking.go:93-107`, then the lock at `:109`, then the reads at `:113`.
- `LockDealership` has one production call site, and `availability.go` has none, so availability stays unlocked.
- Appointment inserts that skip the lock exist only in test code: `env_test.go:215`, `testdb_test.go:61`, `repository_test.go:48,53`, and the fakes.

**Test mutations**
- **Lock removed:** `TestTenRacersForTwoSlots` fails, with 503s and 500s. Restored.
- **N38:** it proves exactly two `InsertAppointment` calls. `env_test.go:99-101` counts every injected failure, and here every call fails.

**Gates**
- `make layers` passes.
- `make ci` failed on its first run at `make races`: `TestM17` got `appointments = 2, want 1`.
  - My own scratch probe caused it. It used its own pool and left a committed DL2 booking, and pool tests only truncate at cleanup.
  - After the table was cleared, `make ci` passed, `make races` passed 3 more times, and `go test -p 1 -count=1 ./...` passed.
- I removed both scratch files, so the tree is clean. `make docker-build` also left a local `service-bay-scheduler` image.
