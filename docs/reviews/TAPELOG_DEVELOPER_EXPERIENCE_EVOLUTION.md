# Tapelog developer experience evolution

Date: 2026-11-07. **Scoped implementation: ready for independent review. Public release: NO-GO.**

The branch adds safe configuration generation, useful preflight diagnostics, honest session summaries and an offline first-session walkthrough. Functional regression checks pass. Release remains blocked by the vulnerability scan, live-client acceptance, native platform testing and unmeasured novice onboarding.

## 1. Verified baseline and scope

- Canonical repository: `C:\Users\Casey\Web Apps\agent-boundary`; origin `https://github.com/Caseymccallum/tapelog.git`.
- Original local main and live remote main: `f79d48a9c31fe1f50d7ccbf90dcd637f66b69caf`. Original worktree clean; no pre-existing changes were overwritten.
- Live remote v0.4.0 tag resolves to `e65e28a6b53b536ff6fd30652d58fc552d29bfe1`. Main contains subsequent documentation changes; the release tag is not the audited source head.
- Dedicated branch: `feat/developer-experience-evolution`. No push, merge, release, deployment, active client configuration change or permanent project copy.
- Implemented candidate: `77d736fb518c19196f012d5f7856a1cfe4e6cfa7`. The tested installed binary's Go build metadata reports that exact revision and `vcs.modified=false`. The final evidence commit is documentation-only.
- Go 1.27.0, windows/amd64, CGO disabled. Build-version output remains `0.4.0`; no release bump. These additions are not present in the published v0.4.0 binary.

The [baseline report](TAPELOG_DX_BASELINE.md) was committed **before implementation**. Reviewed README, QUICKSTART, CLIENT-SETUP, ROADMAP, ARCHITECTURE, THREAT_MODEL, POLICY, TESTING, the launch kit, CLI/doctor, TUI, dashboard and compatibility/integration tests. This was an implementation audit, not an independent acceptance of the resulting changes.

## 2. Initial UX audit

The core is capable; first value is obscured by manual nested JSON, relative paths, platform launcher advice, approval-mode choices and a large command tour. Existing inspection presents policy-decision totals without explicitly separating them from completed calls. Invalid JSON lines were silently skipped. Zero calls had no actionable explanation. Dashboard discovery and replay's server behavior were easy to miss. The five-minute schedule had no measured clean-machine evidence; the release checklist still had that gate unchecked.

| Persona | Attempt and observed friction | Evidence boundary |
|---|---|---|
| Developer new to MCP | Followed source-install and standalone demo workflow; original quickstart presumed a harness, and original no-agent demo required Go/source | New scripted workflow passes; no human novice study or fresh-machine installation |
| Roo Code / Cline user | Examined existing recipes and installed extension versions: Roo 3.54.0, Cline 4.1.26. Exercised both config schemas through setup tests | Extensions present, but no live agent session launched or settings activated; historical CMD issue not reproduced in these versions |
| Claude Code / Desktop user | Exercised both config formats; demo consumes a generated Claude Code-shaped entry through a scripted driver | Claude executable not on PATH; neither product's live integration tested. Config shape is not a client acceptance result |
| Multi-server MCP developer | Tested preservation of unrelated servers, client inputs, unknown fields, large integers, env, cwd and approval settings; ran existing transport/era matrix | Setup wraps one stdio server; remote/multi-server orchestration stays in mux |
| Security-conscious developer | Used an explicit fictional deny policy, inspected the rule/reason, verified the chain, ran behavioral assertions and what-if, replayed a recorded response | Existing guard tests pass; live human approval UX and a separate candidate security review remain pending |

Installation still requires matching the release archive or a suitable Go toolchain. Editing a large JSON document manually is now avoidable; safe activation still requires a local review, backup and replacement. Absolute log/executable paths remove current-directory ambiguity. Direct Windows executable spawning supports spaces, but this cannot establish every client's shell-launcher behavior. Advanced capabilities stay available in their existing commands.

## 3. Selected improvements and rationale

**DX1 — setup and diagnosis.** `tapelog setup --client ... --config ... --server ...` previews one existing named stdio entry. `--output` creates a new candidate file with exclusive creation; existing files and symlinks are not replaced. `--dry-run` suppresses output-file creation. Only command/args change; env, credentials, cwd, other servers and client-specific fields remain intact. Raw JSON values preserve numeric precision. Invalid JSON, duplicate keys, non-string argv, remote transports, double-wrapping, invalid policy, existing logs and missing Tapelog executables are rejected. Preview omits credentials and upstream argv. Candidate files contain original credentials: Unix creation mode is 0600; Windows inherits its directory ACL. There is no apply operation or silent active configuration modification.

`doctor --server` locates an executable without launching it. `--listen` checks then releases a loopback port and explains conflicts. Extensionless log filenames now use their parent correctly; a directory supplied as the log is diagnosed. Required failures remain nonzero. The no-terminal advice recommends human review rather than bypassing confirmation.

**DX2 — session clarity.** `inspect --summary` shows events, mediated calls, failed results, discovery and policy-decision counts. It distinguishes empty, discovery-only, zero-call and missing-end states; explicitly says built-in activity is unobserved and inspection has not verified integrity. `inspect --plain` includes this overview before the timeline and existing denial provenance. Malformed log JSON now reports the line instead of silently dropping evidence. TUI/footer and dashboard explain recording scope. No dashboard handlers, authentication, rendering logic or network defaults changed.

**DX3 — guided documentation and demo.** [FIRST-SESSION.md](../FIRST-SESSION.md) covers installation/build, one offline demonstration, client config choices, candidate review/activation/undo, Windows limits, verification, upstream-free replay and behavioral regression. Starter examples no longer silently opt into auto-confirm. Historical Windows advice is qualified, and the launch testing guide no longer says logs are inherently safe to share. Roadmap and README identify this as development-branch work.

The new `examples/first-session` driver reuses the existing compatibility fixture. It creates a fresh output directory and refuses to reuse an existing one. It uses fictional tool data, starts no external network service, and does not alter an owner's policy. It is a source-checkout demo requiring Go, not a binary-only embedded demo.

Deferred: interactive/apply/restore configuration management, automatic client activation, a new frontend/desktop app, policy redesign and denial replay synthesis. These require additional design/approval or independent security review. No architectural rewrite or unnecessary dependency was added.

## 4. Local implementation commits

| Commit | Milestone |
|---|---|
| `dc1e39c` | Pre-implementation evidence and bounded scope |
| `b3af42d` | DX1/DX2: setup, diagnostics, summaries and acceptance tests |
| `77d736f` | DX3: first-session demonstration, tutorials and documentation corrections |

DX4 evidence is recorded in this report and its companion transcripts. No external independent reviewer has yet accepted this candidate.

## 5. Actual workflow evidence and before/after

See the [complete CLI transcript](evidence/first-session.txt) and [regression results](evidence/regression.txt). The transcript is synthetic fixture data, not a genuine agent's recording.

Final demo observation: 8 events, 2 mediated calls, 1 allow decision, 1 deny decision, 1 recorded successful result. The deny names `demo-no-delete`; no result exists for that request. Verify reports an intact chain with head `74cf3304310716a487aea6fa3ea45cf4edfb3e18844a24293528c6b1f1437243`. What-if: 2 same, 0 changed. Behavioral scenario passes. Replay: 1 played, 0 missed, 0 unused; the tool result compares identically. Discovery-only observation: 3 events, 0 calls, 1 discovery; the overview explicitly explains that outside-MCP agent activity is unknown.

The CLI acceptance test additionally renames the fixture executable before replay, proving the successful recording is replayable with that upstream unavailable. A separate unrecorded call returns `-32011`. Existing compatibility tests establish deny-before-execution from upstream wire observations; absence of a result alone is not the sole proof.

| Step | Before | After / measured evidence |
|---|---|---|
| Standalone first value | Longer trajectory script, root-level output filenames and build steps | One `go run ./examples/first-session --tapelog <binary>` invocation, isolated outputs |
| Existing-server config | Hand-author Tapelog argv in nested JSON | Two setup invocations for preview/save; zero hand-authored JSON required; review/backup/activation still manual |
| Paths | Relative examples and blanket short-name warning | Absolute Tapelog/log/policy paths; argv preserved; real direct-spawn paths with spaces tested |
| Empty recording | Timeline/counts require interpretation | Explicit discovery-only and unobserved-activity explanation |
| Replay | Easy to mistake for a playback command | Described as a waiting MCP server; recorded-result/miss limits explicit |
| Time | Five-minute schedule without acceptance measurement | Warm-cache isolated source install: 3.396 seconds; demo runtime: 2.416 seconds, excluding installation and `go run` compilation |

Those timings are developer-script timings on an already configured machine, **not time-to-value for a new user**. No five-minute onboarding claim. The actual source workflow used `go install`, `version` and `go run`; it did not activate a real client or require manual edits to the demo's generated config.

## 6. Testing and acceptance

| Check/scenario | Result |
|---|---|
| Pre-change full Go suite | PASS |
| Candidate `go test -count=1 -timeout 10m ./...` | PASS: 24 test-bearing packages; 30 total package result lines |
| `go build ./...`, `go vet ./...`, diff whitespace checks | PASS |
| Linux amd64 and macOS amd64, CGO=0, `go build ./...` | PASS compilation only; no native execution |
| Existing CLI dogfood | Passing fixture exits 0; expected-failure fixture exits 1 |
| A — first installation | PARTIAL: isolated source install and offline demo pass; fresh-machine archive install and binary-only demo not established |
| B — first integration | PASS for generated stdio configuration consumed by a real subprocess driver; live Roo/Cline/VS Code/Claude acceptance pending |
| C — Windows spaces | PASS for direct executable spawning from paths containing `My Projects\Tapelog` and `Installed Tapelog`; shell/client launchers unverified |
| D — meaningful recording | PASS: actual MCP messages/calls through Tapelog to scripted upstream, summary and verification |
| E — zero calls | PASS: demo plus empty/discovery/interruption summary tests |
| F — policy denial | PASS: deny reason visible; existing boundary/wire tests and no-result invariant pass |
| G — deterministic replay | PASS for recorded response with upstream unavailable; denied/unanswered calls remain misses |
| H — failed setup | PASS: malformed/duplicate JSON, invalid argv/transport, absent executable, existing log/output, directory log and occupied port; source preserved |
| I — backward compatibility | PASS for existing suite, including protocol-era/transport matrix, historical fixtures, conformance, blobs and checkpoints; no exhaustive external log census |
| J — local safety | PASS for scoped changes/tests: no active config writes, no auto-confirm, new candidates only, no new data-serving endpoint; external traffic not packet-captured |
| `govulncheck ./...` | **FAIL, exit 3**: see next section |

Race-enabled Linux CI was not run: this is a Windows session with CGO disabled and no detected C compiler. No remote CI was triggered. Browser/TUI interactive usability, human approvals and pinned third-party live-server tests were not executed. No UI screenshots are claimed; CLI transcripts satisfy the evidence requirement.

## 7. Security and compatibility assessment

The diff against baseline is empty for policy/mediator/proxy/transport/replay/session/checkpoint/schema/limits code, approval code, session specifications, Go dependency manifests, and dashboard API/security handlers. Existing tests for fail-closed behavior, deny-before-forwarding, schema/drift, limits/redaction, hash chains/blobs, signed checkpoints and replay verification remain green. The new behavior is configuration generation and observation presentation, not a change to these guarantees. Existing confirmation handling is not weakened. No telemetry, dependency, account or publicly bound dashboard was introduced. The demo runtime is local stdio; installation/builds can contact Go infrastructure, and the vulnerability check contacted public vulnerability data.

The [vulnerability checker output](evidence/vulnerability-check.txt) reports 12 reachable vulnerability IDs: GO-2026-6809, GO-2026-6617, GO-2026-6613, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6608, GO-2026-6607, GO-2026-6605, GO-2026-6603, GO-2026-6600 and GO-2026-6599. It also reports additional imported/module findings without apparent calls. The output names fixes in Go 1.27.2/1.27.3 and `golang.org/x/net` v0.60.0. These are scanner results, not demonstrated exploits. This milestone leaves the original toolchain/dependencies unchanged; baseline was not separately vulnerability-scanned, so no exact before/after reachability comparison is claimed. Remediate and rescan separately before release.

## 8. Limitations and remaining problems

- The helper starts from an existing working stdio server; it neither installs servers nor creates a novice's first MCP integration from nothing.
- Strict JSON only. JSONC, remote transports, legacy VS Code root shapes, environment-variable command interpolation and all client-specific launch behavior are not validated by schema-generation tests.
- Configuration activation and backup/restore remain manual. Candidate files can contain credentials and must be kept private. Policies requiring human review still need the existing approval queue configured explicitly.
- The configured log path is reused on restart; the existing recorder warns and truncates. Setup rejects a currently existing log, but does not implement automatic per-start rotation. Archive/change the path before re-recording.
- Replay serves recorded results. Denied and interrupted calls without results are not synthesized into replayed policy denials. This was discovered by an initially failing acceptance test and documented rather than changing replay semantics.
- Overview does not attribute upstream server identity: the existing session-start payload lacks it. Counts include mediated resource/prompt surface activity and policy decisions can include result-side decisions, so they are not unique successful-tool counts.
- Inspection is not verification. A missing end may mean live recording or interruption; an intact chain does not establish complete agent monitoring or authorship.
- The dashboard change is prominent explanatory text, not a new overview/filter/details system. Interactive UI evaluation remains open.

## 9. Follow-up and release recommendation

**GO for independent review of the bounded local DX milestone. NO-GO for public release.** Keep this branch local until owner approval; no push/merge is authorized by this report.

Priority follow-up: (1) separately review/remediate toolchain and dependency findings, rerun scanner and Linux race CI; (2) exercise a safe named MCP call in the installed Roo/Cline versions and actual VS Code/Claude clients without exposing credentials; (3) native macOS/Linux archive and launcher smoke tests; (4) measure a first-time developer completing the guide on a clean machine. Then assess whether an interactive picker, explicit approval-queue generation or safe per-start log naming earns its complexity. Any security-sensitive config-apply or replay-semantic change needs isolated design and independent review.
