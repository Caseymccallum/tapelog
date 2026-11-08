# Tapelog M3 — Investigation Intelligence

Implemented on `feat/m3-investigation-intelligence`, based on M2 `e9ad51808680eb8dfdf23e1fc499e1c65d450574`. Product code and automated tests are at `b3a2d0016b1ff65797b0a6b80b96f2714022e4c5`; this report and screenshot follow in a documentation commit. Development used an isolated copy; no push, merge, release, live MCP invocation, or external service was used.

## What changed

The session overview links the earliest recorded error result and up to five calls whose last recorded policy decision was deny to their activity details. It distinguishes observed calls, non-error results, tool errors and deny decisions, retains integrity and incomplete-recording warnings, and does not infer cause or execution from a denial. Selected failures offer investigation steps and preserve existing expandable recorded arguments, results, decisions, preceding events and correlation provenance. Error content remains in the recorded JSON rather than being interpreted as a diagnosis.

Comparison adds attempted and unanswered calls, missing/additional occurrences, last recorded policy verdict and rule, error flags, recorded-result hashes and attempt ordering. Matching uses tool plus canonical argument hash and occurrence order; repeated keys are explicitly uncertain. Results are capped at 100 displayed differences and are not labelled regressions. The existing answered-only CLI diff is unchanged and remains shown separately. Comparisons and tests still require complete, verified, unambiguous, inline recordings with at most 10,000 events.

The test builder now offers zero boundary attempts, an ordered subsequence of selected answered calls, and the existing `no_deny_bypassed` invariant. The user selects the desired assertion; a zero-attempt assertion for an observed tool will fail on this cassette. Assertion semantics are tool-level, and a specific error's absence is not expressible by the existing engine. No argument/result content is copied into YAML, no upstream tool runs, and saving remains an explicit action.

All-events pages now fetch 50 summaries and load the capped raw preview only when expanded, with a revision check. The detail endpoint avoids formatting every preceding event when only the final three are displayed. Existing byte/event limits and re-verification are retained.

## Verification

- Windows Go suite `go test ./...` passed after the product changes, including session, redaction, policy, replay, scenario and web tests. Node workbench tests passed (6). `git diff --check` passed apart from a line-ending advisory.
- Focused M3 tests cover unanswered/policy comparison, repeated-key ambiguity, first error and denial links, summary payload omission, zero-attempt and sequence generation, unsupported assertions, and a synthetic large recording.
- On this machine, one fictional 4,002-event, 2,000-call test returned a 50-event summary of 5,157 bytes in 379 ms versus 205,657 bytes in 370 ms for the old full page; a late-event detail took 368 ms. These are single-run local measurements, not benchmarks or general latency guarantees. Each request still reads, parses and verifies the bounded recording.
- BrowserOS Neo with fictional fixtures: opened a session, followed its first-error link, selected a second session and compared recorded outcomes, then generated and ran an answered-call sequence assertion. Screenshot: [fictional comparison](evidence/m3/comparison.jpg). The screenshot was captured immediately before the final labels were clarified; the underlying workflow is the same. The native save dialog, live MCP client and native Linux/macOS execution were not exercised in this milestone.

## Limitations and next work

- Overview links cover the last decision on each call, not every historical policy decision. Comparison also aligns only the last decision and rule; unassociated policy events must be inspected in raw evidence. Multiple decisions and reason changes deserve a future explicit event-level comparison.
- Occurrence pairing is a deterministic heuristic, not causal identity. Identical attempts, redaction, missing events or changed arguments can make corresponding calls ambiguous. The digest is a representation of recorded result bytes; equivalent JSON representations can compare differently. Timestamps are not compared.
- Failure message and arguments are still inside expandable raw JSON; there is no separate structured error extraction or visual selection of multiple sequence calls. The sequence text field requires known event numbers. These are usability gaps relative to the ideal guided experience.
- Browser rendering is paginated, but per-request server parsing, search/filtering, and large fields within a single activity are not independently bounded below the 16 MiB recording cap. Comparison output is capped, not paginated. Larger sessions and blob-backed recordings still need the CLI.
- Synthetic throughput and the browser flow do not establish performance on all machines, screen sizes or genuine recordings. Chain verification establishes internal consistency, not authorship, completeness or execution of actions outside this MCP boundary.
- Recommended next: structured error/result display with redaction-preserving bounds; select sequence calls directly in the timeline; compare every linked policy decision with event-level ambiguity and pagination; profile large, long-payload recordings and improve repeated verified reads without introducing stale-snapshot acceptance; independent review and real-client testing.
