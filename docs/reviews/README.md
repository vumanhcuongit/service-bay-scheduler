# Review records

Verbatim output of the agents that were asked to find fault, with the verdict given to each point. Kept so that the counts in [design.md §10](../design.md#10-how-genai-was-used-in-design) and the README's AI collaboration section can be checked, not just read.

| File | Who | Input they were given | When |
|---|---|---|---|
| [decisions-challenge.md](decisions-challenge.md) | challenger agent, same session as the author | `docs/decisions.md` v1 only | before the spec, 2026-10-20 |
| [red-team-1.md](red-team-1.md) | red-team agent, clean context | the spec and `openapi.yaml`; no decisions, no discussion, no code | before the spec was frozen, 2026-10-20 |
| [red-team-2.md](red-team-2.md) | the same kind of agent, clean context, then run against the server | the spec and `openapi.yaml`; curl against `localhost:8080` | after all nine tasks were merged, 2026-10-20 |
| [grading-1.md](grading-1.md) | grader agent, clean context | the whole repo on `main`, a paraphrase of the brief, the four scoring dimensions | after the README was merged, 2026-10-20 |
| [pr-11-review-1.md](pr-11-review-1.md) | reviewer agent, clean context | spec, decisions, design, plan, `git diff` of `fix/concurrency-story` | PR #11, first pass |
| [pr-11-review-2.md](pr-11-review-2.md) | reviewer agent, clean context | same, eight commits later | PR #11, second pass |
| [grading-2.md](grading-2.md) | grader agent, clean context | the whole repo on `main` after PR #11; told to list every claim the repo does not back | after PR #11, 2026-10-20 |

PR review for PRs #1–#10 was not kept as a file: a reviewer agent in a clean context went over each one, given the spec, decisions, design, plan and `git diff main...<branch>`, and only the findings that were taken survive, as `fix(ai):` commits ending "caught in PR review". From PR #11 on, and for both grading passes, the full output is kept here with every verdict, including the points that were kept or signed off rather than changed, so a rejection is as auditable as a fix.

The verdicts are the author's. "changed" means the point was taken and the spec, decisions or code changed; "kept" means the point was heard and the rule stayed, with the reason; "out" means out of scope, recorded in decisions §3.
