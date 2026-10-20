# Grading pass 2 — after PR #11 was merged

Agent: grader, new session, clean context, the whole repo on `main` at `c1f539a`. Same brief as [grading pass 1](grading-1.md), plus: run `make ci`; list every claim in the README, design and decisions that the repo does not back or the code contradicts, as the main output. It ran the suite with the exclusion constraints dropped and with `LockDealership` made a no-op.

Scores it gave: design 3, execution 3, AI engineering 3, communication 3.

Outcome: PR `fix/grader-2`. Four of the fifteen claims were real bugs reachable from the public API. The verdicts below are the author's, including the ones not taken.

## Verdicts

| # | Claim the repo does not back | Verdict | What happened |
|---|---|---|---|
| 1 | DST "handled by the IANA tz library": `LocalWindow` builds hours as midnight plus a duration, so America/New_York opens at 09:00 on 2026-03-08 | **changed** (bug) | hours built with `time.Date(y, m, d, h, …, loc)`; DST tests added; the "no DST tests" line removed |
| 2 | "Same key with a different body is a 422": the hash formats `start` with `RFC3339`, dropping fractional seconds, so `09:00:00.5` replays the 201 | **changed** (bug) | hash uses `RFC3339Nano`; test added |
| 3 | "Nothing relies on session state": pgx's default statement cache uses named prepared statements per connection, which PgBouncer transaction mode breaks on | **changed** (bug) | `QueryExecModeCacheDescribe`; D12 and §8 say what pooler-safe means |
| 4 | §1.1 cites N36, M25 and N37 as proving the constraints; with all three constraints dropped they still pass 20/20 | **changed** | a no-lock wrapper runs M25, N36, N37 asserting never more than one overlapping row; §1.1 cites it, `testdb_test.go` and `TestVehicleAtTwoDealerships` |
| 5 | Wrong-context risk "mitigated by the spec tests": in tests the base DB is the test transaction, so a wrong context still runs inside one | **changed** | `ClaimKey`, `SaveKeyOutcome` and `InsertAppointment` error outside a transaction, like `LockDealership`; the doc names that guard, not the tests |
| 6 | "With an equal value the detector never picks a victim" next to "failed one run in sixty" | **changed** (doc) | "usually both lose" |
| 7 | "The re-run's worst case adds about 100 ms" against D8's ~4 s per attempt | **changed** (doc) | the re-run is a full second attempt |
| 8 | "Measured on localhost… about 70 a second": no benchmark; it is a test duration including fixtures | **changed** (doc) | figure removed |
| 9 | "Each dealership is serialised: its availability reads and inserts take one lock", where "availability reads" was defined as `GET /availability`, which takes no lock | **changed** (doc) | "the busy-list read inside the booking" |
| 10 | "Fine for years with an index on `(dealership_id, start)`": the overlap predicate cannot seek on it; EXPLAIN used the exclusion index | **changed** (doc) | the GiST indexes behind the constraints serve the overlap reads |
| 11 | "`DATABASE_URL` defaults to the Compose database": `main` exits when it is unset; `make run` supplies it | **changed** (doc) | wording |
| 12 | "`make races` runs them 20 times": the pattern skipped four of the ten pool tests | **changed** | `make races` runs all of `spectest` with `-count=20` |
| 13 | "A third agent reviewed every PR; I ruled on every finding": GitHub has no reviews or comments; rejected findings were not kept | **changed** (record + wording) | review and grading output kept here from PR #11 on, with every verdict including the kept ones; the README says earlier reviews survive only as `fix(ai)` commits |
| 14 | Red-team 1 "16 in, 3 out": the verdict table shows 17 in, 2 out | **changed** | the author's own counting error; fixed everywhere the numbers appear |
| 15 | "Nothing else calls `time.Now()`": the middleware does | **changed** (doc) | the stated exception |
| — | "Never thought through": the same car at two dealerships waits ~1 s in deadlock detection while holding a dealership lock, so unrelated customers queued there can get 503 | **changed** | a third advisory lock keyed by vehicle, taken after the dealership lock; fixed order key → dealership → vehicle; the cross-dealership race serialises instead of deadlocking |
| — | "Rewritten three times in one day, each from a reviewer, not from the author's own foresight" | **kept** | true; the rewrites and who found them are the record, not something to hide |
| — | README AI section repeats design §10 | **changed** (doc) | shortened to two paragraphs and a link |

## Agent output, verbatim

I pulled main (fast-forwarded to `c1f539a`, clean tree) and ran `make ci`. It passed in 7.4 s: vet, layers, lint (0 issues), tests, and 20 runs of the race tests. I also probed things on my own: ran the server, ran the suite against a database with the exclusion constraints dropped, and ran it with `LockDealership` made a no-op. Everything I changed is reverted and the scratch database is dropped.

### Scores

**1. Problem solving & system design: 3/5**
- **For it:** declarative constraints for correctness, the lock for liveness, and the lock order are argued clearly (`decisions.md:106-116`). Removing the lock is caught: TenRacers returns 503 instead of 409 in 7 of 10 runs.
- **Rewritten three times in one day:** `ed17c38` → `8d23da7` → `48a739e` → `6b6604c`. Each rewrite came from a reviewer, not from the author's own foresight.
- **Never thought through:** when one car is booked at two dealerships at once, the insert can wait about 1 s for deadlock detection while holding DL1's lock (`booking.go:172-194`, `writes.go:63`). Every other DL1 booking waiting on that lock has a 1 s `lock_timeout` (`repository.go:57`), so unrelated customers can get 503s.
- **DST is broken while the docs say it is handled.** See claim 1.

**2. Technical execution: 3/5**
- **For it:** clean layering enforced by `make layers`, typed errors, and race tests against real Postgres with real commits.
- **Against it:** two correctness bugs anyone can hit through the public API (claims 1 and 2), and a pooler incompatibility the docs say is solved (claim 3).
- **The headline proof table cites tests that cannot fail on the thing they claim to prove** (claim 4).

**3. AI engineering & verification: 3/5**
- **For it:** the `fix(ai): …, caught in <where>` convention (24 commits, counts check out), clean-context red-teams, and spec examples mapped 1:1 to tests (64/64 verified).
- **Against it:** the verification produced confident claims that were never checked:
  - the test-to-mechanism table (claim 4)
  - "IANA handles DST", accepted from red-team pass 1 #14 without looking at `LocalWindow`
  - "no session state", re-asserted after `b24f9c4` fixed only one cause
- **No record of PR review.** PRs #1–#11 have 0 reviews and 0 comments on GitHub, and `docs/reviews/README.md:11` says rejected findings were never kept.
- **Doc/code drift was caught late:** the code drifted from the docs on the headline decision, and the drift got past PR review until a "grading pass" (`ed17c38`).

**4. Communication: 3/5**
- **For it:** the README landing page and "The design decision that matters" (`README.md:42-56`) are strong.
- **Against it:** too dense and self-referential. The README AI section copies design §10 almost word for word (`README.md:94-96` ≈ `design.md:285`).
- **Design §10 is mostly build-phase narrative, not design.** About 50 of 91 commits are `docs:`, many of them recounts.
- **Several numbers in the docs contradict each other** (below).

### Claims the repo doesn't back, or the code contradicts

1. **DST "handled by the IANA tz library"** (`decisions.md:174`, `:210` "DST days stay correct", `:237`, spec `:236`).
   - `model/slots.go:23-27` builds opening hours as `midnight.Add(8h)`, which counts elapsed time, not clock time.
   - I ran it for America/New_York on 2026-03-08: open = **09:00**, close = 18:00. On 2026-11-01: 07:00–16:00.
   - Booking, availability and technician shift checks all go through it.

2. **"The same key with a different body is a 422"** (`README.md:119`; `decisions.md:128-129`).
   - `model/hash.go:18` formats `start` with `RFC3339`, which drops fractional seconds.
   - Live test:
     - first request: 201 at `09:00:00+07:00`;
     - same key with `09:00:00.5+07:00`: replays the **201**;
     - same body without a key: **422** "not on the 15-minute grid".

3. **"A pooler in transaction mode… works here because nothing relies on session state"** (`design.md:260`; repeated at `repository.go:29-30`).
   - `repository.go:32` uses `pgxpool.New` with pgx v5's default statement cache. That means named prepared statements per connection, which is session state.
   - PgBouncer in transaction mode breaks on them unless prepared-statement support is configured. `b24f9c4` fixed `statement_timeout` but not this.

4. **§1.1 says N36 and M25 prove "Postgres refuses the second overlapping row" and N37 proves the vehicle constraint** (`design.md:19-20`).
   - With all three constraints dropped, M17, M25, N36, N37 and TenRacers **still pass 20/20**. The lock plus the Go check produce the same results.
   - Only `testdb_test.go:72-86` (direct inserts) and `TestVehicleAtTwoDealerships` (which failed 50/50 without the constraint) exercise the constraints. Neither is cited.

5. **Wrong-context risk "mitigated" because "the spec tests run every path inside one [transaction]"** (`design.md:83`).
   - That setup hides the bug it claims to catch. In tests the repository's base DB *is* the test transaction (`env_test.go:45-47`, `testdb.go:55-64`), so `q()` falls back to it (`repository.go:44-49`).
   - A call made with the wrong context therefore still runs inside a transaction in tests, and only outside one in production.

6. **"With an equal value the detector never picks a victim and both sides lose"** (`design.md:100`; `decisions.md:112`).
   - The same paragraph (`decisions.md:112`) reports the race failed "one run in sixty". "Never" would mean every run.

7. **The re-run's "worst case adds about 100 ms"** (`design.md:195`).
   - `decisions.md:113` puts it at about 4 s per attempt and about 8 s over two. The re-run is a full second attempt (`booking.go:91`), not just the backoff.

8. **"Measured on localhost, ten serialised bookings took 0.12–0.15 s, about 70 a second"** (`design.md:263`).
   - No benchmark exists in the repo. The figure matches TenRacers' `go test` duration, which includes fixture inserts.
   - With the lock removed, that test also took 0.12–0.15 s, so the number doesn't measure the lock's cost.

9. **"Each dealership is serialised: its availability reads and inserts take one lock"** (`design.md:263`, where item 1 at `:259` defines "availability reads" as `GET /availability`).
   - `service/availability.go:14-52` takes no lock and opens no transaction.

10. **"Fine for years… with an index on `(dealership_id, start)`"** (`design.md:262`).
    - `ListBusy` uses a range-overlap predicate (`reads.go:95-98`), so that btree can't seek on time.
    - I ran EXPLAIN over 105k rows: the planner used `appointments_vehicle_excl`. The cited index serves nothing.

11. **"`DATABASE_URL` defaults to the Compose database"** (`README.md:112`).
    - `cmd/server/main.go:35-38` exits with "DATABASE_URL is not set". Only `make run` supplies it (`Makefile:1`). I reproduced the exit.

12. **"`make races` runs them [the concurrency tests] 20 times"** (`README.md:71`).
    - `Makefile:42` skips 4 of the 10 pool tests: N39, HeldKeyIs503, DeadlineIs503, VehicleViolationRerunStores409.

13. **"A third [agent] reviewed every PR… I read every PR diff and ruled on every finding"** (`README.md:69`; "findings as `file:line` for me to judge", `design.md:281`).
    - GitHub has 0 reviews and 0 comments on all 11 PRs. Only accepted findings survive, as commits. No artifact backs "every finding".

14. **Red-team pass 1: "16 went into the spec… 3 were ruled out of scope"** (`design.md:279`; `red-team-1.md:5`).
    - The verdict table in the same file shows 17 "in" and 2 "out" (#13, #14). The 36/31/4/1 totals at `design.md:283` inherit the error.

15. **"Nothing else in the code calls `time.Now()`"** (`design.md:81`).
    - `handler/middleware.go:44` and `:94` do.

### Three interview questions

1. "Your §1.1 table maps each failure to the test that proves it. I dropped all three exclusion constraints and N36, M25 and N37 still pass. Which claims did you verify yourself, and how would you find the others like it?"
2. "Same car, DL1 and DL2, same minute. The DL1 booking waits about 1 s in deadlock detection while holding the DL1 lock. What happens to the other customers queued at DL1, and what would you change?" This tests whether they own the lock-timeout design or are reading it back from D8.
3. "The README says a reviewer agent went over every PR and you ruled on every finding, but the PRs have no reviews and rejected findings weren't kept. Show me one you rejected and why. And how would you run this process on a team, where a reviewer's rejections need to be auditable?"

### The one thing to fix with two hours

Make §1.1 true by proving the constraints when the lock is bypassed:
- Add a repository wrapper that turns `LockDealership` into a no-op.
- Run M25, N36 and N37 through it, asserting there is never more than one overlapping row and never a 500.
- Point §1.1 rows 1–2 at it and at `testdb_test.go:72-86`.

That table is the doc's spine and the first thing an interviewer will check. Right now its headline claim is asserted, not tested. If there's time left, the DST fix in `LocalWindow` is about 10 lines.
