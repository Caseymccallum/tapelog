# Tapelog M2 — Agent Session Workbench

Implemented 8 November 2026 on `feat/session-workbench-m2`, based on the verified clean development baseline `fcc56dff267f27cff5b3528d4caa1655ad9327e3`. This is a product-development milestone. It adds a usable workbench to the existing embedded Go dashboard, with no new runtime dependency, framework, hosted service or protocol-engine rewrite.

## Delivery and identity

| Commit | Scope |
|---|---|
| `06322840d68e25a8b3e4842689c0133199c1518a` | Bounded session library, evidence-derived activity model, inspection/comparison/assertion APIs, confined reads and Go regressions |
| `a989c779bb75d627be71bc994ed1f9bfc624a63e` | Workbench UI, dark/light appearance, test save/preview/run, rendering/state regressions, fictional tour and user guide |

The final report/evidence is a subsequent documentation commit. The local preview binary was rebuilt from `a989c779…`; Go build metadata reports Go 1.27.3, that exact revision, and `vcs.modified=false`. Work was isolated in this task's `work/tapelog-m2` copy. The original DX branch and its recordings/configurations were preserved. Nothing was pushed or merged, and no active MCP configuration was changed.

Start with [the workbench guide](../WORKBENCH.md). The temporary local preview at `http://127.0.0.1:8939/` serves **fictional recordings only** while its process is running. Recreate the tour using `go run ./examples/workbench-demo --out <NEW-directory>` followed by `tapelog web --dir <directory>`.

## Research: observations, requests and proposals

Research was conducted against public source and documentation, not installed competitor runtimes. Functionality below is source-observed where identified; broad product promises are not treated as independently verified performance or compatibility results. Community posts are qualitative signals, including self-promotional material, not a representative demand survey.

**Confirmed source-observed competitor surfaces.** MCPJam's current application wires Tools, Evals and Sessions routes. Its SessionsPanel implements search and source/status filtering through a backend feed. A session library is therefore useful product parity, not a claim of uniqueness. The README also describes a larger playground and evaluation offering; no claim is made that every advertised client/model or hosted option was exercised. [Application source](https://github.com/MCPJam/inspector/blob/main/mcpjam-inspector/client/src/App.tsx), [SessionsPanel source](https://github.com/MCPJam/inspector/blob/main/mcpjam-inspector/client/src/components/sessions/SessionsPanel.tsx), [MCPJam README](https://github.com/MCPJam/inspector).

The official Inspector's current source wires protocol replay through its application. `protocolReplay.ts` dispatches supported requests through the Inspector client and logs a fresh exchange. This is live request re-issue, which differs from Tapelog's upstream-free cassette replay. Its main README documents Web, CLI and TUI clients. [Application source](https://github.com/modelcontextprotocol/inspector/blob/main/clients/web/src/App.tsx), [replay implementation](https://github.com/modelcontextprotocol/inspector/blob/main/clients/web/src/lib/protocolReplay.ts), [README](https://github.com/modelcontextprotocol/inspector).

**Public requests.** Inspector issue #1857 requests registry/ServerCard-driven configuration forms; it was shown open in the consulted page. That is not evidence that those forms are complete. Issue #1438 documents formerly stubbed Pin/Replay controls, but is closed and must not be used to claim a current defect. The present replay source supplies stronger current evidence. [Configuration request](https://github.com/modelcontextprotocol/inspector/issues/1857), [historical Pin/Replay issue](https://github.com/modelcontextprotocol/inspector/issues/1438).

Developers in two Reddit discussions ask for reproducible traces, request/response association, interruption visibility and enough context to investigate a failure. Those are requests and anecdotes. A recording of tool traffic cannot establish an agent's unrecorded internal reasoning, even when someone asks why a tool was selected. [After-the-fact debugging discussion](https://www.reddit.com/r/mcp/comments/1psu0b9/for_larger_mcp_setups_how_do_you_debug_why_this/), [interruption and paired-trace requests](https://www.reddit.com/r/mcp/comments/1wrl3i3/i_built_an_mcp_clientinspector_where_you_can_see/).

**Competitor roadmap proposals.** The official Inspector H2 roadmap identifies a zoomable timeline, session artifacts, comparison, assertions and connection diagnostics as experience work. The document explicitly identifies itself as a draft and separates delivered items from future work. These proposals help identify useful workflows; their presence is not proof of shipped functionality. [Roadmap](https://github.com/modelcontextprotocol/inspector/blob/main/docs/inspector-roadmap-2026-h2.md).

**Our proposed differentiation.** Bring existing tamper-evident recordings, recorded policy provenance, hermetic replay and deterministic assertions into one local investigation flow. The differentiator is the combination and accurate evidence boundaries, not an unsupported claim that other tools lack timelines or tests.

### Opportunity scoring

Scores are engineering judgment, 1–5. Higher value/differentiation/fit is better; higher effort is harder. Core investigation was prioritized over an elaborate settings system, despite the low effort of adding isolated diagnostic text.

| Opportunity | User value | Differentiation | Effort | Architecture fit | Decision |
|---|---:|---:|---:|---:|---|
| Searchable session library | 5 | 2 | 3 | 5 | Complete core flow |
| Linked activity + policy investigation | 5 | 4 | 4 | 5 | Complete core flow |
| Session → deterministic assertion | 5 | 4 | 3 | 5 | Complete bounded v1 |
| Answered-call/result comparison | 4 | 3 | 2 | 5 | Reuse existing engine |
| First-session / incomplete-recording diagnostics | 4 | 2 | 1 | 5 | Integrate into above |
| Registry discovery and active configuration management | 3 | 1 | 5 | 2 | Defer |
| Full zoomable, multi-lane causal waterfall | 4 | 3 | 5 | 3 | Defer; avoid unsupported causal inference |

## Existing product gaps and implemented experience

The previous web UI showed a filename dropdown, raw chronological rows and a large empty approvals panel in directory mode. Useful engines existed in `internal/session`, `internal/replay` and `internal/scenario`; the dashboard did little to connect them to developer questions. The initial architecture, inspection summaries, replay/diff, scenario runner, policy/mediator event metadata, recording writer and dashboard security were inspected before implementing the new surfaces.

The new library presents readable recorded task/harness names, recorded time, elapsed recording span, server namespace when available, call/result/decision/error counts, explicit integrity states, useful empty states, search, filters and sorting. Date ordering uses parsed instants across timezone offsets. No evidence files are renamed or annotated. User-defined persistent labels are deferred.

Activity is a paged call list with outcome/server/tool filters, a selected-call inspector, exact recorded policy rule/reason, elapsed call-to-result time, parent/ID link provenance, raw related events and preceding context. Overlapping calls remain separate. Raw events expand in place in All events. Missing server identity or timing remains unknown. Timing includes intervening policy/approval/transport delays; it is not claimed to be server execution time. A missing result is not converted into a guessed failure or a non-execution claim.

Policy denial, tool error, no result, missing session end and failed integrity are visibly distinct. Historical end reasons are shown as recorded. A missing end can mean an active recording, interruption or failed startup; the UI does not invent a diagnosis. Discovery-only sessions explain how to explicitly invoke a wrapped MCP tool. Workbench API connectivity is not represented as a current MCP-server connection.

Comparison is one completed end-to-end workflow using `replay.Diff`, including added/missing answered calls, changed result/count keys and call-sequence changes. Whole-session denial/error totals remain separate. The engine does not align denied/unanswered calls or policy verdicts, and the UI says so. Sessions with no shared answered tool names have an explanatory refusal. Timestamps and generated request/session IDs do not create noise. Switching tabs retains the selected pair; changing the pair clears stale results and invalidates in-flight requests.

The test builder deliberately asks which assertion should be retained. It previews standard scenario YAML, runs the existing scenario engine against the reviewed snapshot, and saves only after an explicit browser action. Assertions can describe an observed attempt, recorded result, deny verdict or absence of recorded results. They apply to a tool across the cassette, not one selected request ID. The UI explains the existing `never_called` limitation: absence of a recorded result does not prove non-execution. No arguments/results or fixture contents are copied into YAML. A changed file invalidates the preview. Non-empty selected output files are refused by the save-picker path; browser download fallback uses a unique filename.

Replay & CLI provides safely quoted inspection/verification and hermetic replay commands. It does not launch recorded actions or an upstream server. Existing live approval controls are retained when an approval queue is supplied; directory mode hides that surface rather than presenting an irrelevant empty panel.

## Architecture and safety decisions

The application remains three embedded HTML/CSS/JS assets served by the existing Go HTTP handler. There is no frontend build dependency, external font, CDN, account, upload or telemetry addition. Appearance preference is the only localStorage value; logs are not persisted in browser storage. Untrusted strings reach the DOM through textContent, never HTML sinks. Raw data remains potentially sensitive and is collapsed by default. Existing CSP, no-store, Host pinning, bearer auth and same-origin POST checks protect the new APIs. The legacy token-protected static-shell authorization behavior is unchanged.

Directory-mode file reads accept only plain `.jsonl` names, reject links/non-regular files and use `os.Root` confinement. Tests/comparison use a bounded verified in-memory snapshot. The existing replay loader was factored to expose `LoadInline`, reusing the same parsing engine while refusing blob-backed filesystem reads. CLI blob loading, policy decisions, recording format, redaction, approval semantics and generated-config unique-recording behavior are unchanged.

Bounds: 16 MiB / 50,000 readable events per recording; 50 browser rows (API cap 100); 32 KiB per raw preview; 20 related-event previews. Library enumeration caps at 10,000 entries / newest 200 regular recordings and a 32 MiB read budget. Deferred summaries are visible. Two simultaneous scans per handler are allowed; additional requests return a retryable response. There is no stale snapshot cache or unbounded browser tail. Comparison/assertion creation additionally caps at 10,000 events and requires a complete, nonempty, verified, unambiguous cassette. Blob-backed, ambiguous or oversized engine workflows receive CLI guidance.

CLI commands and recorded formats remain compatible. Existing HTTP consumers must accommodate the new bounded `/api/log` page (maximum 100 events / 1 MiB rather than the former 500-row tail) and paginated session listing. The new UI uses dedicated paginated activity/event endpoints. Verification is unanchored chain consistency, not proof of authorship, completeness or blob-store integrity.

## Demonstrated workflows and validation

All demonstrations use seven fictional recordings, including a deliberately altered one. Before/after screenshots use the same fixture directory. They are real BrowserOS Neo captures, not mockups. The two screenshots illustrating comparison/test generation were captured during the walkthrough; the final comparison-selection correction was then independently exercised in the browser and covered by a state regression.

| Developer question | Demonstrated interaction | Result |
|---|---|---|
| Which run contains a tool error? | Library → With tool errors | One matching audit session |
| Why was deletion denied? | Open audit → select delete call | Recorded `protect-project-files` rule and explanation; no inferred non-execution |
| What changed after a fix? | Compare → choose after-fix run → compare | One changed `packages__lookup` outcome; same answered-call sequence; errors 1 → 0 |
| Can this denial become a test? | Select call → Make a test → Run assertion | Valid scenario YAML; existing engine passes; mutated allow verdict fails the automated regression |
| Is this a complete trusted recording? | Read integrity and end-state banners | Corrupted chain and missing end remain different states |
| Can I inspect underlying evidence? | All events → expand event; inspector → raw related events | Bounded literal text, not executable HTML |

Once the workbench is running, these questions require browser actions rather than manually opening JSONL or composing several CLI commands. This is a scripted developer walkthrough, not a novice-user study. Seven warm local activity API requests had a median **1.75 ms**, maximum **2.87 ms**, on the small fictional data; this is not a browser or large-file performance benchmark. All seven fixture SHA-256 values remained identical after the library/filter/details/comparison/test workflows. The generated YAML also passed `tapelog test --plain` through the actual CLI.

| Check | Result |
|---|---|
| `go test -count=1 -timeout 10m ./...` | PASS, 31 package result lines / 24 test-bearing packages |
| Added Go workbench tests | 10 pass, 1 symlink test skipped: Windows privilege unavailable |
| `node --test internal/web/static/app.test.cjs` | PASS, 6 tests: literal malicious rendering, empty/outcome states, command quoting, save overwrite guard, stale test preview, comparison state |
| `go build ./...`, `go vet ./...`, `node --check`, diff whitespace check | PASS |
| Linux/amd64 and macOS/amd64 builds, CGO disabled | PASS cross-compilation only |
| BrowserOS Neo desktop walkthrough | PASS for library, denial inspector, test preview/run, result comparison, light/dark appearance and arrow-key tab navigation |
| Recording preservation | PASS, byte-identical SHA-256 values for all seven fictional logs |
| Existing policy, approval, redaction, hash-chain, replay, compatibility and DX1.1 regressions | PASS in full Go suite; no diff in their enforcement/recording implementations |

Automated cases cover discovery/filtering, historical missing fields, invalid JSON and truncated tails, altered chains, file/event/payload bounds, timezone ordering, overlapping calls, unique legacy IDs, invalid explicit parents, duplicate IDs, exact policy explanations, method/origin/token/path protection, engine comparison parity, YAML parsing/behavior, stale revisions and preservation. Rendering tests exercise real UI functions in a minimal DOM adapter; they are not a substitute for a full browser automation suite.

### Screenshots and evidence

- [Before: DX1.1 dashboard](evidence/workbench-m2/before.jpg)
- [After: session library](evidence/workbench-m2/session-library.jpg)
- [Denial investigation](evidence/workbench-m2/denial-investigation.jpg)
- [Test builder and passing assertion](evidence/workbench-m2/test-builder.jpg)
- [Comparison in light appearance](evidence/workbench-m2/comparison-light.jpg)
- [Go results](evidence/workbench-m2/go-tests.txt), [UI unit results](evidence/workbench-m2/node-tests.txt), [workflow/CLI/preservation evidence](evidence/workbench-m2/workflows.txt)

## Known limits and next product milestone

No full zoom/pan waterfall, user-defined persistent labels, server registry, active configuration apply, live-server execution, policy-aware per-call comparison or general assertion editor was added. Some historical recordings lack server identity and explicit relationship metadata. The workbench intentionally refuses ambiguous engine workflows instead of changing the engines' semantics.

Native Linux/macOS execution, Linux race tests, mobile viewport validation, a screen-reader audit and a novice-user study were not performed. The native browser Save As dialog was not driven through OS automation; the underlying explicit-save and overwrite logic is covered by unit tests, and generated files were validated separately through the CLI. No end-to-end live approval UI session or new actual MCP-client session is claimed in this milestone; existing approval/security tests passed.

Recommended M3: validate this investigation flow with developers using their own local recordings; then add a bounded on-disk index for larger libraries, additive non-secret upstream identity in new recordings, and policy-aware comparison. Extend the assertion editor only after observing which test patterns users actually need. Preserve historical compatibility, snapshot integrity and the clear distinction between hermetic replay and live execution throughout.
