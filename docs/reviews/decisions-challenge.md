# Decisions challenge

Agent: challenger, same session as the author. Input: `docs/decisions.md` v1, nothing else. The prompt asked it not to rewrite the file, only to find contradictions, weak reasons, and things that would hurt later within scope.

The verdict on each point is in [decisions.md §5](../decisions.md#5-challenge-log): 16 points, 15 changed, 1 kept (#6, first-by-id assignment).

## Agent output, verbatim

**Contradictions**
- **§4 vs D8:** you say no overlapping bookings for the same vehicle, but D8 only puts the database guard on bays and technicians. Under concurrent requests that check can be bypassed, which goes against D8's own point that "the DB is the only place that can serialise correctly."
- **D10 vs §3:** `status` is always `confirmed`, yet your cancel plan relies on "the constraint only considers confirmed." Either make the exclusion constraints partial now (`WHERE status='confirmed'`) or drop the column. As written it's a column that never changes.
- **D9:** returning 409 for "outside hours" is inconsistent. 409 means a conflict with current state, but outside-hours is wrong regardless of state, the same kind of error as your 422 for a payload mismatch.
- **§0.3 vs D5:** "available" includes "inside the resource's working window," but bays have no window. Say that a bay's window is the dealership's hours.
- **D2 vs D4:** nothing says how a technician's window relates to the dealership's. The effective window should be the overlap of the two. Say so.

**No reason, or a weak one**
- **D7:** "deterministic for tests" helps tests, not the domain. Picking the lowest id can put your only brake-certified technician on an oil change, so a later brake job that should have fit gets turned away.
- **D11:** "explicit ids make the rule visible" is a weak reason. Sending `customer_id` with the vehicle adds a way for the request to fail (mismatched ids) and gives nothing back.
- **D10:** the real reason to store `end` is that the booked interval must not change if the service type's duration is edited later. "So the constraint doesn't recompute" is the weaker reason.
- **§4:** "future in dealership local time" doesn't mean anything. "Now" is the same instant everywhere. The timezone only matters for hours and the `date` parameter.

**Will hurt you later, within this scope**
- **D7 + D8:** if two requests race for bay 1 and one hits the constraint, you return 409 even though bay 2 is free. You need to retry with the next candidate, or you'll get false rejections under load.
- **D9 idempotency:** you haven't said where keys are stored, what they're scoped to, how long they live, what happens when two requests with the same key arrive at once (needs a unique constraint), or whether a replayed 409 stays a 409.
- **D1:** `duration_minutes` and the opening hours aren't required to be multiples of 15. A 20-minute service would leave gaps and misaligned end times.
- **D1 / D9:** you haven't said whether `GET /availability?date=` is a local or UTC date. Daylight-saving days will break one or the other.
- **D3 / D4:** if skills are free-text strings, one typo ("brake" vs "brakes") means no technician is ever qualified, and nothing reports an error. Use a reference table or enum.
- **D9 / D11:** a service type from dealership B combined with dealership A isn't covered. Decide whether that's a 404 or 400, and make sure there's a check.
- **D13:** `GET /appointments/{id}` has no `dealership_id` until after the lookup, so the "every log line has dealership_id" field will be empty on that route.
