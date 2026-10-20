# PR #11 review 2 — `fix/concurrency-story`, second pass

Agent: the same kind of reviewer, new session, clean context, on `fix/concurrency-story` at `f5bdcf7` (eight commits after review 1, `8d23da7` through `f5bdcf7`). Same inputs and checks. It ran mutations again, one at a time from a clean tree.

Outcome: `48a739e` (`lock_timeout` before the key claim, 1 s, deadline → 503, cause logged, N39, timezone cache test) and `6b6604c` (insert wait 2 s so the deadlock detector can run — found by a race failing one run in sixty while fixing the first). One point signed off rather than changed.

The implementer corrected the worst-case arithmetic in finding 2 and the author's brief: one attempt can wait three times, not once, so the real bound is the 5 s request deadline, which now maps to 503.

## Verdicts

| # | Finding | Verdict | What happened |
|---|---|---|---|
| 1 | `lock_timeout` is set after `ClaimKey`, so a wait on a held key row is unbounded → 500 | **changed** | `SET LOCAL lock_timeout` is the first statement of the booking transaction; a held key → 503 |
| 2 | `lock_timeout` is per statement; one request can wait past the 5 s deadline → 500 | **changed** | `lock_timeout` 1 s on the key and dealership waits; `context.DeadlineExceeded` in the booking path → 503 with `Retry-After`; the deadline is the real bound |
| 3 | A lock-timeout 503 is logged as "unavailable after the re-run" | **changed** | `reason=lock_timeout | rerun_failed | deadline` |
| 4 | openapi 503 text still describes the old behaviour; no spec row for the lock timeout | **changed** | openapi updated; N39 added and `TestLockTimeoutIs503` renamed `TestN39` |
| 5 | Reference reads (dealership, vehicle, service type, bays, technicians) run before the lock, against the brief's "before any read" | **signed off** | reference data has no write path (D11); D8 now states that if it ever gets one, the lock moves before those reads |
| 6 | §8.5 "bounded by `lock_timeout` × pool size" is unclear | **changed** (doc) | a blocked dealership holds its waiters for one `lock_timeout` window; how many wait at once is bounded by the pool size |
| 7 | Timezone cache untested and undocumented | **changed** | pointer-identity test; one line in design §6 |

## Agent output, verbatim

I re-reviewed `origin/fix/concurrency-story` at `f5bdcf7` (8 new commits since the last review, `8d23da7` through `f5bdcf7`). `make layers` and `make ci` pass, and so does `go test -p 1 -count=1 ./...`.

**What I confirmed fixed**
- **404/422 no longer wait on the lock.** They run before it.
- **A lock wait that times out returns 503.** I removed `SET LOCAL lock_timeout` and `TestLockTimeoutIs503` then failed with 500 instead of 503.
- **`LockDealership` now errors outside a transaction.**
- **The lock order is pinned.** `TestBookCallOrder` fails 20/20 if the lock is removed, and 20/20 if it moves after `ListBusy`.
- **The cross-dealership vehicle race is now tested for real.** A scratch probe over 40 pairs of V1 at DL1 and DL2 reached the vehicle constraint and the re-run all 40 times, with no deadlocks and no 500s.
- **The remaining-error cases, plan note, caught-in list and throughput figure are done.** The re-run error matrix, the superseded note on the plan, the README caught-in list and the measured throughput are all in.

**Findings, most serious first**

1. **`internal/repository/writes.go:26`: the comment says `lock_timeout` "bounds every lock wait in a booking transaction", which is false.**
   - `SET LOCAL` runs at `writes.go:49`, after `ClaimKey` (`booking.go:101`), so a same-key request waiting on another request's key row is not bounded by it.
   - Probe: with the key row held open by another session and a 3 s handler timeout, the request returned 500 after 3 s.
   - So `design.md:111` ("Never 500"), `README.md:56` ("every loser gets a clean 409") and the "503, never a 500" framing still have a path that returns 500. Normally this wait is short, but a slow first request can push the waiter past the deadline.

2. **`writes.go:49`, `booking.go:64-80`: `lock_timeout` applies per statement, not per request.**
   - Worst case on one request: a lock wait just under 2 s, then a ~1 s deadlock wait on the insert, then the backoff, then up to 2 s more on the re-run. That exceeds the 5 s default deadline and ends in 500.
   - This comes from reading the code. I did not reproduce it, and it needs contention on both attempts.
   - The docs say "well inside the request deadline" for the single wait only.

3. **`booking.go:58`: a lock-timeout 503 is logged as "booking unavailable after the re-run".**
   - A lock timeout never re-runs (`booking.go:64-65`) and the cause (`ErrLockTimeout`) is not logged.
   - D13 says any rejected booking must be explainable from its log lines, and this line points at the wrong cause.

4. **`openapi.yaml:98`: the 503 text still reads "Hit a constraint violation or deadlock on the one re-run too."**
   - A lock wait past 2 s now also returns 503, and the PR's own D8 and spec rule 11 say so.
   - The API contract is the one doc left on the old behaviour. The spec examples also have no row for the lock timeout.

5. **You asked for the lock before any read. It now comes after these reads:**
   - **Read before the lock:** `GetDealership`, `GetVehicle`, `GetServiceType`, `ListBays` and `ListQualifiedTechnicians` (`booking.go:159` is the lock).
   - **Why I'm flagging it:** this departs from your brief's rule and needs your sign-off. The docs and the new tests state the new order consistently. It is only safe while that data is static, which D11 says it is, since there is no CRUD. Any later path that edits bays, technicians or hours would make those reads stale, and the docs don't say that.

6. **`design.md:263`: "bounded by `lock_timeout` × pool size" is unclear.**
   - The pool starvation is now documented as an accepted cost, which is fine.
   - With a 4-connection pool and DL1's queue holding all four, a DL2 availability call took 1.72 s, about one lock timeout, and returned 200. The four queued DL1 bookings all got 503.
   - The window looks like one `lock_timeout`, not that product, and the paragraph doesn't say which it means.

7. **`reads.go:124`: the timezone cache I flagged before is still untested and still not mentioned in any doc or commit message.** `docker-build` is now in the README, so that part is resolved.

**Gates and process**
- `make races` (×20) now includes `TestVehicleAtTwoDealerships` and passes.
- I ran the mutations one at a time after the first batch overlapped. A leftover change from one mutation contaminated the next, so I redid them from a clean tree and checked `git status` between each.
- The tree is clean and the scratch probe files are deleted. I truncated the test tables after each probe.

## The implementer's correction, while applying the verdicts

> The review's ≈3.1s isn't the true worst case. `lock_timeout` applies to each wait, and one transaction can wait three times: on the key, on the dealership lock, and on a row during the insert. That's up to about 3s per attempt, so about 6s with the re-run, which is past the 5s deadline. Point 2 is what bounds it: past 5s you get a 503, never a 500.

And, while running the race tests before pushing:

> When the same vehicle is booked at both dealerships at once and the two inserts truly deadlock, both transactions wait on each other. Postgres's `deadlock_timeout` (1s) is meant to pick one victim, but the equal 1s `lock_timeout` fires first, and it fires in **both** transactions. Both bookings get a 503 and nothing is booked. This happens about once in 60 runs.

Verdict: insert wait raised to 2 s so the detector runs (`6b6604c`). [Grading pass 2](grading-2.md) then found the better fix: a vehicle lock that removes the deadlock altogether.
