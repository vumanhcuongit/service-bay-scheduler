# Red-team pass 2 — running server, after all tasks merged

Agent: red-teamer, new session, clean context. Input: the spec and `openapi.yaml`, no code, no discussion. First asked for cases the spec is silent on or that can be checked with one request, each with a curl and the expected response. Then given the author's rulings on the silent cases and asked to run all of them against `localhost:8080` on a freshly seeded database.

Result: 19 cases run. 3 bugs by the rulings, fixed in `cb3d5ce` (empty `Idempotency-Key`), `36de09c` (`Content-Type`), `f86ea32` (repeated query parameter). 1 spec wording fix (`start_times` are UTC). 5 input rules recorded in D9 (`2b8d9be`). One of the author's rulings (#19) was withdrawn after the implementer showed the existing rule with evidence.

## Rulings and outcome

| # | Case | Ruling before running | Result |
|---|---|---|---|
| 1 | ids with different case or spaces | exact strings, no normalising → 404 | ok |
| 2 | same key, two payloads, at the same moment | spec: one 201, one 422 | ok |
| 3 | payload hash that cannot tell fields apart | spec: 422, not a replay | ok |
| 4 | duplicate JSON keys | out of scope, last key wins | ok, recorded in D9 |
| 5 | UUID key in other forms | hyphenated any case → replay; braces, no-hyphen, urn → 400 | ok |
| 6 | empty `Idempotency-Key:` header; nil UUID | empty → 400; nil UUID allowed | **bug**: empty header treated as no key → `cb3d5ce` |
| 7 | start in another offset landing on another local date | spec | ok |
| 8 | 15-minute grid in a +05:45 offset | spec | ok |
| 9 | availability for today, partly past | spec: only starts after now | ok |
| 10 | spec says local, openapi says UTC for `start_times` | UTC is correct; fix the spec | ok in code; spec wording fixed |
| 11 | extra data after the JSON body | spec: 400 | ok |
| 12 | body with `Content-Type: text/plain` | no ruling | **bug**: 201; ruled 415 → `36de09c` |
| 13 | 50 MB body | spec: fast 400/413 | ok, 400 in 4 ms |
| 14 | two failures at once | spec order | ok |
| 15 | unusual `start` formats | Feb 30, `+0700` → 400; lowercase t/z: either | ok, lowercase is 400, recorded in D9 |
| 16 | year 9999 | 201, not 500 | ok |
| 17 | repeated `date=` query parameter | no ruling | **bug**: first value won silently; ruled 400 → `f86ea32` |
| 18 | GET with a non-UUID id | spec: 400 | ok |
| 19 | `X-Request-ID` with spaces, tab, non-ASCII | author ruled "accept printable" | mismatch by that ruling; the implementer showed the rule has been `[A-Za-z0-9._-]{1,128}` since `923427d` and is in openapi; **ruling withdrawn**, code unchanged |

## Agent output, verbatim

I ran all 19 cases. Two of them are bugs under your rulings: 6 and 19. Each line gives what I expected after applying your rulings.

| # | Result | Actual, where it didn't match |
|---|---|---|
| 1 | ok | 201, then 404 `vehicle  V1 not found`, then 404 `vehicle v1 not found` |
| 2 | ok | one 201 (V2, B1/T1) and one 422; 02:00Z still listed in availability |
| 3 | ok | 201, then 422 for both shifted payloads (`V1O`/`IL` and `DL1V`/`1`) |
| 4 | ok | 201 with `vehicle_id: V2` (last key wins) |
| 5 | ok | the upper-case key replayed the same id; braces, no-hyphen and `urn:uuid:` forms all gave 400 |
| 6 | **mismatch** | empty `Idempotency-Key:` → **201** `{"id":"45fbb693-…","vehicle_id":"V1",…,"start":"2026-10-31T02:00:00Z",…}`. The nil UUID gave 201, which is ok. I checked with `curl -v` that the empty header really was sent. |
| 7 | ok | 201 at `2026-10-23T01:00:00Z`; 422 `would start at 07:45, before opening at 08:00` |
| 8 | ok | 422 for `+05:40` (`10:20 … not on the 15-minute grid`); 201 for `+05:45` at 03:15Z |
| 9 | ok | it was 07:04Z; the list ran 07:15Z … 09:30Z, nothing at or before now |
| 10 | ok | 35 UTC times, 01:00Z … 09:30Z |
| 11 | ok | 400 `body must be a single JSON object`, both variants |
| 12 | **mismatch** (no ruling) | `null`, `[]` and a non-JSON body gave 400. But a JSON body sent with `Content-Type: text/plain` → **201** `{"id":"bbc6202b-…","vehicle_id":"V1",…,"start":"2026-11-05T02:00:00Z",…}`. Content-Type isn't checked. |
| 13 | ok | 400 `body must be at most 1048576 bytes` in 4 ms |
| 14 | ok | 400 (no offset beats unknown DL9); 404 `vehicle V9 not found` |
| 15 | ok | Feb 30 and `+0700` gave 400. Lowercase `t`/`z` also gives 400 `start must be RFC 3339 with an offset`, which your ruling allows. |
| 16 | ok | 201, `9999-12-31T02:00:00Z` |
| 17 | **mismatch** (no ruling) | 422, 400, 400 and 404 were as expected. Duplicate `date=…&date=…` → **200** with the list for the first date (2026-11-09). |
| 18 | ok | 400 `id must be a UUID` |
| 19 | **mismatch** | 129 chars was replaced and 128 was echoed, as expected. `bad id with spaces` (printable, 18 chars) was **replaced** with `ff8ab1e57517d2f7f33bed7121f8a6a8`. Tab and `café` were also replaced. |

**Bugs under your rulings**
- **6:** an empty `Idempotency-Key` header is treated as no key, and the booking goes through. It should be 400.
- **19:** request ids with spaces or other printable characters are replaced. The server only accepts letters, digits, `.`, `_` and `-`, which matches what openapi says now, so this needs both a code change and a doc change. One more decision needed: "printable" could mean ASCII only (so `café` is replaced, as now) or include Unicode.

**Not covered by a ruling, so they need your call:** 12 (Content-Type not checked) and 17 (duplicate query parameter silently takes the first value).

**Doc changes only (the code already does what you ruled):**
- **10:** spec §2 says availability starts are "listed in local time". They're UTC, so fix the wording.
- **1:** say ids are exact, case-sensitive strings.
- **4:** say duplicate JSON keys are out of scope and the last one wins.
- **5:** say the key must be the hyphenated form (any case); braces, no-hyphen and `urn:` forms give 400.
- **6 (doc half):** say an empty key gives 400 and the nil UUID is allowed. The 400 itself still needs the code fix.
- **15:** say lowercase `t`/`z` gives 400.

> I didn't change any code. The database now holds the bookings these runs created, so reseed before running the spec's own tests.

## On #19, from the implementer

> The server is doing exactly what the current rule says, and that rule was never "printable". `internal/handler/middleware.go:18` sets the rule as `^[A-Za-z0-9._-]{1,128}$`. It has been the rule since `923427d`, and both `openapi.yaml` and the D9 line say the same thing. A space isn't in that set, so an id with spaces gets replaced. No commit, doc or test anywhere in the repo mentions "printable".

Ruling withdrawn; the documented rule stands.
