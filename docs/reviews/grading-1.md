# Grading pass 1 — after all nine tasks and the README were merged

Agent: grader, new session, clean context, the whole repo on `main` at `26ffdfd`. Given a paraphrase of the brief and the four scoring dimensions, told to be harsh, to back every score with `file:line` or a commit, and to say when a claim in the docs is not backed by the repo. It ran `make ci` and `make races`, and one experiment: it removed the per-dealership lock in `writes.go` and re-ran the race tests.

Scores it gave: design 3, execution 4, AI engineering 3, communication 4.

Outcome: PR #11 (`fix/concurrency-story`). The headline finding, that the code had drifted from the docs on the concurrency design after `ceb8c6d`, led to a design change (`ed17c38`), not a doc change. The rest is tracked below.

## Verdicts

| # | Finding | Verdict | What happened |
|---|---|---|---|
| 1 | Code takes a per-dealership lock on every insert (`ceb8c6d`); no doc mentions it; README calls the constraint alone "the decision that matters"; without the lock, ten racers get 500 | **changed** | lock moved to the start of the booking transaction, retries cut to one re-run, D8/design/README rewritten (`ed17c38`) |
| 2 | Lock is in the wrong place: after the checks, so the retry loop stays | **changed** | same commit; PR review then moved it after 404/422 (`8d23da7`) |
| 3 | README overclaims "correct for every code path and every number of instances" | **changed** | narrowed to overlaps; skill, hours and dealership membership are Go checks |
| 4 | First-by-id assignment wastes scarce skills and N9 pins that as correct | **kept** | known cost, recorded in decisions §3; optimisation out of scope |
| 5 | Service package has no unit tests; all logic tested through HTTP against Postgres | **kept** | deliberate: the bugs this system exists to prevent only show on a real database; pure functions have unit tests in `model/` |
| 6 | Deadlock test does two inserts per transaction, unlike production | **changed** (wording) | described as a mapping test, not a reproduction |
| 7 | Ten-racer test accepts 503 from losers | **changed** | every loser must get 409 `no_bay` |
| 8 | Key-holder rollback → waiting duplicate runs as new: promised, untested | **kept** | still untested; stated in the spec as a behaviour without a test |
| 9 | Spec says 24 h expiry has no test; `TestExpiredKeyRunsAsNew` exists | **changed** | spec line fixed |
| 10 | No Dockerfile although §8 describes ECS | **changed** | Dockerfile added |
| 11 | `time.LoadLocation` on every request | **changed** | cached per timezone name |
| 12 | `docs/reviews/README.md` lists `pr-<n>-review.md` files that do not exist | **changed** | row removed; PR review recorded as `fix(ai)` commits, and from PR #11 on as files here |
| 13 | "ending caught in PR review" is wrong for three commits | **changed** | "caught in <where>" |
| 14 | `ceb8c6d` changed the concurrency design as a "fix" with no doc update | **changed** | the drift this pass found; see 1 |
| 15 | "I read every diff": 64 commits in 5.5 hours | **changed** (wording) | "I read every PR diff and ruled on every finding" |
| 16 | Decisions challenger ran in the author's session, not clean | **kept** | true and already stated in `docs/reviews/README.md` |
| 17 | §8.1 presents the batched availability query as future work; it is built | **changed** | doc fixed |
| 18 | Five documents for three endpoints; rules spread across four files is why they drifted | **kept** | accepted as the cost of showing the process; discussed in the README's team section |

## Agent output, verbatim

I pulled `origin/main`. It fast-forwarded this branch to `26ffdfd` (the merge of PR #10), with no conflicts and a clean tree.

To check claims I ran `make vet layers test` and it passed. `make races` (the concurrency tests 20 times each) passed in about 10s. I also confirmed the counts against the repo: 63 spec examples, 106 test functions, 20 `fix(ai):` commits, 13 of them "caught in PR review". Then I ran one experiment: I took out the per-dealership lock in `internal/repository/writes.go:37` and re-ran the race tests. `TestTenRacersForTwoSlots` returned 500s in 10 of 10 runs and booked nothing. I reverted the change afterwards. That result drives most of what follows.

### 1. Problem solving & system design: 3/5

**What it gets right**
- The overlap rule is enforced in the database, as three exclusion constraints (`migrations/00001_schema.sql:70-75`).
- The idempotency key is saved in the same transaction as the booking (`internal/service/booking.go:92-121`).
- Every "must not happen" is listed with the test that proves it (`docs/design.md:19-28`).

**What costs it**
- **The headline decision doesn't hold on its own, and the doc describes a system that isn't the one shipped.**
  - The README calls the exclusion constraint "the design decision that matters" and argues against lock-based fixes (`README.md:46-54`).
  - The design doc lists a per-dealership lock only as a later option, marked **Unsure** (`docs/design.md:199`, `:265`). It also calls that lock "wasteful under load" (`:181`).
  - The code takes exactly that lock on every insert (`writes.go:31-38`). It was added in `ceb8c6d` after concurrent inserts deadlocked.
  - Without the lock, the system fails, as my experiment showed. `docs/design.md` was never updated after `ceb8c6d`; its last concurrency edit is `54313f5`, which came before the code.
- **The lock is in the wrong place.** It is taken after the availability checks, so the code pays for serialising each dealership and still keeps the full retry loop (`booking.go:64-89`). Taking it at the start of the transaction would make the bay and technician retries unnecessary. Nobody seems to have weighed this.
- **README overclaims.** "The booking is correct for every code path and every number of instances" (`README.md:54`) is only true for overlaps. The database does not enforce that:
  - the bay or technician belongs to the booking's dealership (no composite foreign key, `00001_schema.sql:58-76`);
  - the technician has the required skill;
  - the booking fits opening hours or the technician's shift.

  All of those are checks in Go only (`booking.go:211`, `internal/model/slots.go:33`).
- **A known-bad business rule is locked in by a test.** Picking the first technician by id spends scarce skills on easy jobs, and example N9 expects that cost as correct behaviour (`docs/superpowers/specs/2026-10-20-booking.md:152`).

### 2. Technical execution: 4/5

**Strengths:** the layering is clean and enforced by `make layers`. Errors are typed, and the races pass 20 times in a row.

**Weaknesses**
- **The service package has no tests at all.** All booking logic is tested through HTTP against a real Postgres. Without Docker, nothing in the core logic can be tested. The pure function `PickResources` gets 4 cases (`internal/model/slots_test.go:72-77`).
- **The deadlock test doesn't match production.** It does two inserts per transaction (`internal/repository/repository_test.go:46-59`); production does one. So it proves that error code `40P01` maps to the right error, not that the real-world cause it describes actually happens.
- **The ten-racer test accepts 503 from losers** (`spectest/concurrency_test.go:93`). It can't tell a correct "no bay" 409 from a customer turned away after retries.
- **One promised path is untested.** If the first request holding a key rolls back, a waiting request with the same key should run as new (`docs/design.md:117`). The spec admits there's no test (`spec:221`).
- **The spec is stale.** It says 24-hour key expiry has no test (`spec:220`), but `TestExpiredKeyRunsAsNew` exists (`spectest/idempotency_test.go:111`).
- **No Dockerfile**, although the design describes deploying to ECS Fargate (`docs/design.md:269`).
- Minor: `time.LoadLocation` runs on every request (`internal/repository/reads.go:25`).

### 3. AI engineering & verification: 3/5

**Strengths:** decisions were written first (`3dc31d7`). The red-team agent ran in a clean context. Raw agent output is kept with the author's ruling on each point. Every AI fix is tagged `fix(ai):`.

**Claims the repo doesn't back**
- **The PR review records are missing.** `docs/reviews/README.md:3` says the records are kept "so that the counts… can be checked", and `:10` lists `pr-<n>-review.md` files. None exist, on any branch. So the 13 "caught in PR review" fixes (`README.md:67`, `docs/design.md:283`) can't be checked.
- **"Each fix from a review… ending 'caught in PR review'"** (`README.md:67`) is wrong for `ceb8c6d` and `055b85e` ("final review") and `8b060bc` ("CI").
- **The biggest change the AI made wasn't owned.**
  - `ceb8c6d` changed the concurrency design, and it went in as a "fix" with no update to the decisions or design docs.
  - That gap sits exactly where the author says AI output is checked.
  - The design doc's own lesson (`docs/design.md:285`: check every claim in §7/§8 next time) still leaves §6 and §8.5 wrong.
- **"I read every diff" is hard to believe.** All 64 commits land on 2026-10-20, between 10:37 and 16:09. That is 64 commits and roughly 2,000 lines of Go, plus about 1,300 lines of docs, in about 5.5 hours. Nothing in the repo backs the claim.
- The decisions challenger ran in the author's own session, not a clean one (`docs/reviews/README.md:7`). That weakens the "clean context" story for that pass.

### 4. Communication: 4/5

**Strengths:** the README works as a landing page. The curl examples are realistic, and the "Where to look first" reading order (`README.md:56-63`) is good.

**Weaknesses**
- **Accuracy drift.** Beyond the lock and the overclaim above, `docs/design.md:261` presents the batched availability query as a future change. It is already built (`internal/service/availability.go:13` cites it).
- **Too much paperwork for three endpoints.** There are five documents (decisions, spec, plan, design, reviews), and it's hard to follow them all. Rules are spread by decision number across `docs/decisions.md`, the spec, `openapi.yaml` and the design doc, and that spread is exactly why they drifted.

### Three interview questions

1. **"Your README says the constraint is the decision that matters, and the design calls a per-dealership lock a future option. `writes.go:37` takes that lock on every insert, and without it ten concurrent bookings return 500. Why didn't the constraint hold alone? Why take the lock after the checks instead of first? Why does no doc mention it?"** This tests whether they understand the system the AI built, or only the system they planned.
2. **"Explain `b24f9c4` and `8b060bc` without reading the commit messages: the bug, why it happened, and why the fix is right. And where are the `pr-<n>-review.md` files?"** This tests the "read every diff" claim, given 64 commits in 5.5 hours.
3. **"This process produced about 1,300 lines of docs for three endpoints, and it still drifted from the code at the most important point. Your team has six engineers shipping weekly. Which parts of this do you keep, which do you cut, and who keeps the docs true?"** This is the manager-level doubt: the README proposes standardising this process for a team (`README.md:91`), but the repo shows it doesn't stay accurate even for one person in one day.

### The one fix, with two hours

**Make the concurrency story true, then pin it with a test.**
- Move the per-dealership lock to the start of the booking transaction.
- Write it into D8 (`docs/decisions.md`) with the reason: concurrent inserts that clash on an exclusion constraint deadlock (`40P01`, `ceb8c6d`).
- Update `README.md:42-54` and `docs/design.md` §5, §6 and §8.5.
- Cut the bay and technician retries back to the cross-dealership vehicle case, which is the one deadlock the lock can't prevent.
- Tighten `TestTenRacersForTwoSlots` so every loser must get 409 `no_bay`, never 503.
- In the same pass, either commit the PR review records or remove the claim that they exist.

Right now the submission's strongest selling point is the very thing it got wrong without saying so, and that's what an interviewer will press on first.
