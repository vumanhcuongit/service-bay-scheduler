# Review records

Verbatim output of the agents that were asked to find fault, with the verdict given to each point. Kept so that the counts in [design.md §10](../design.md#10-how-genai-was-used-in-design) and the README's AI collaboration section can be checked, not just read.

| File | Who | Input they were given | When |
|---|---|---|---|
| [decisions-challenge.md](decisions-challenge.md) | challenger agent, same session as the author | `docs/decisions.md` v1 only | before the spec, 2026-10-20 |
| [red-team-1.md](red-team-1.md) | red-team agent, clean context | the spec and `openapi.yaml`; no decisions, no discussion, no code | before the spec was frozen, 2026-10-20 |
| [red-team-2.md](red-team-2.md) | the same kind of agent, clean context, then run against the server | the spec and `openapi.yaml`; curl against `localhost:8080` | after all nine tasks were merged, 2026-10-20 |
| `pr-<n>-review.md` | reviewer agent, clean context, one per PR | spec, decisions, design, plan and `git diff main...<branch>` | at each PR |

The verdicts are the author's. "changed" means the point was taken and the spec, decisions or code changed; "kept" means the point was heard and the rule stayed, with the reason; "out" means out of scope, recorded in decisions §3.
