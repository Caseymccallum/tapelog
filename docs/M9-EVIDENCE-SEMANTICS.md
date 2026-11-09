# Explicit complete-boundary eligibility

M9 adds an opt-in case field, `eligibility: "complete-boundary"`. Existing version-1 cases without that field retain M8.1 eligibility, and legacy YAML scenario files are unchanged. New visual cases explicitly select this mode and explain it in the case preview.

All lab evaluations still require an intact, unambiguous recording with exactly one recorded start and end, matching revision when required, and supported inline results. Damaged, incomplete, unreadable or ambiguous recordings are INCONCLUSIVE for every expectation, even if a witness appears in the partial evidence. "Complete" describes this recording's boundaries, not all agent activity.

## The four evidence concepts

Every reader of a session log must keep four properties apart. They are orthogonal: a file can be intact but invalid, valid but incomplete, complete but ineligible for a given claim.

| Concept | Question | Established by |
|---|---|---|
| **Cryptographic integrity** | Are the bytes exactly what the writer wrote? | `session.Verify` — hash chain, sequence contiguity, schema version, blob digests. Tamper-evidence, not tamper-proofing |
| **Structural validity** | Is the file one session log? | `session.InspectStructure` — at least one event, one non-empty `session_id` shared by every event, exactly one `session/start` first, at most one `session/end` last |
| **Completeness** | Was the recording's own end observed? | `session.InspectStructure` — exactly one `session/end`, last |
| **Per-assertion eligibility** | May this claim be asserted from this evidence? | `internal/lab` — claim-relative, decided last |

A structural defect makes the recording INCONCLUSIVE with code `structure_invalid`. A missing end makes it INCONCLUSIVE with code `incomplete_recording`. Neither is ever a FAIL, and neither is repaired by an intact chain: a correctly hashed file can still be two sessions, or have no start, or no end.

`tapelog verify` prints the structure verdict next to the chain verdict and keeps its exit code chain-only unless `--require-structure` is passed (exit 3). `tapelog inspect` prints all three verdicts: it exits 0 for a file it can render, 1 for a file whose JSON cannot be parsed (there is nothing to render — the reason is printed to stderr), and with `--check` it exits 3 for a broken chain, 4 for a structurally invalid file and 5 for an incomplete one. `tapelog replay` refuses a structurally invalid file unless `--allow-invalid-structure` is passed.


| Assertion shape | Verified complete, no matching calls | Matching calls present | Incomplete recording |
| --- | --- | --- | --- |
| Existential `attempted` | FAIL | PASS | INCONCLUSIVE |
| Existential `answered` | FAIL | PASS with a recorded result; otherwise INCONCLUSIVE | INCONCLUSIVE |
| Existential `denied` | FAIL | PASS with a recorded deny; FAIL if all decisions are known and none denies; INCONCLUSIVE if a decision is missing and there is no witness | INCONCLUSIVE |
| Existential `result_contains` | FAIL | PASS with a matching recorded result; FAIL when all matching results are available and clear but none matches; otherwise INCONCLUSIVE | INCONCLUSIVE |
| Universal `result_never_contains` | INCONCLUSIVE: no searchable result | FAIL with a recorded counterexample; PASS only when every matching call has a clear result and none matches; otherwise INCONCLUSIVE | INCONCLUSIVE |
| Counting `no_attempts` | PASS: zero observed attempts | FAIL | INCONCLUSIVE |
| Counting `attempt_ceiling` | PASS: zero is within the nonnegative maximum | PASS or FAIL by the observed count | INCONCLUSIVE |
| Ordered `sequence` | FAIL | PASS if an answered subsequence exists; otherwise FAIL, including insufficient recorded results | INCONCLUSIVE |
| Boundary `no_deny_bypassed` | INCONCLUSIVE | Existing non-vacuous invariant eligibility retained | INCONCLUSIVE |

Outcomes still come from the real scenario engine after eligibility. Known witnesses and counterexamples can make a claim decidable despite missing sibling results, but cannot bypass the global recording verification gate. Policy claims use the final recorded policy decision per call, as the existing engine does. No outcome implies that a tool executed outside the recorded MCP boundary. Comparisons remain observations and never alter an explicit assertion's outcome. Older binaries reject the new optional field rather than silently interpreting it with weaker semantics.
