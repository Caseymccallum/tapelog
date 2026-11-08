# Independent Tapelog review

Reviewed 7 November 2026. **NO-GO for accepting this candidate as a safe persistent setup experience or for public release. GO for continued local remediation and fictional-data testing.** No push, merge, release, active configuration change, service installation or source modification was performed.

## Candidate and evidence identity

- Available canonical checkout: `C:\Users\Casey\Web Apps\agent-boundary`.
- Branch: `feat/developer-experience-evolution`; exact HEAD `eea7874025c9b76660d6903c4d5ac64e7decfe04`. Clean before and after review. No locally recorded remote-tracking branch contains this commit; no remote fetch or publication was performed.
- Reviewed the full 22-file diff against `f79d48a9c31fe1f50d7ccbf90dcd637f66b69caf`, including setup, doctor, summaries, TUI/dashboard text, demo, tests and documentation. The final commit changes only the report and three evidence transcripts; implementation ends at `77d736f`.
- Independently executed checks on a Git archive of this exact commit under this review's `work/`, preserving the original checkout. Review-built binaries are from that archive; they are not claimed to have the original installed binary's VCS metadata.
- Read the requested prior task's `outputs/TAPELOG_DEVELOPER_EXPERIENCE_EVOLUTION.md` and `outputs/FIRST-SESSION.md`, plus their committed counterparts and all three evidence transcripts. The guide is byte-identical. The report differs only in the relative link to the guide, appropriate to its different location.
- Referenced chat content was available only as bounded messages and unresolved content references. Candidate conclusions below are based on accessible local source and executable evidence.

## Severity-ranked findings

### 1. P1 — Generated persistent configuration destroys prior session evidence on restart

`cmd/tapelog/setup.go:132-140` checks that a log does not exist during generation, then writes a fixed absolute `record --log` path into a reusable client configuration. `internal/session/chain.go:40-44` warns and immediately truncates an existing log. There is no refusal or rotation at runtime. A routine client restart, reconnect or failed startup can therefore remove the previous recording before any useful new call occurs. Concurrent launches also target the same file.

Independent reproduction: generated a fresh Claude Code-shaped candidate around the repository's fictional fixture; invoked its exact command/arguments twice. The first session ID was `20261107T231404.300-29b52b1138574008`; the second was `20261107T231404.727-a9d922431a5345a3`. The first ID was absent afterward. Stderr reported overwriting a 602-byte recording. The guide discloses the risk; disclosure does not make ordinary reconnects preserve evidence. The truncating writer predates this branch, but the new persistent setup flow embeds that behavior.

**Required remediation:** give generated configurations a recording mode that creates a fresh file exclusively per launch, or refuses existing targets before modifying them. Preserve existing manual-command compatibility deliberately rather than changing all writer semantics accidentally. Add an integration check that runs one generated configuration twice and proves the first log remains byte-identical; cover simultaneous starts and startup failure. A warning alone is insufficient.

### 2. P1 release gate — Twelve reachable vulnerability IDs remain reproducible

Fresh `govulncheck ./...` scans of both baseline and candidate on the installed Go 1.27.0 Windows/amd64 toolchain exited **3**, with the same twelve symbol-level IDs. This establishes an inherited finding set for this platform/toolchain, not identical call graphs or proof of exploitability. The original report correctly did not claim a baseline scan; this review supplies that comparison.

| ID | Scanner finding | Runtime condition to establish during triage |
|---|---|---|
| GO-2026-6809 | Compromised intermediate certificate trust | Actual certificate verification and affected trust-store conditions |
| GO-2026-6617 | HTTP/2 HPACK encoder race/server crash | An attacker-accessible HTTP/2 server |
| GO-2026-6613 | HTTP/1 desynchronization after successful CONNECT | Relevant server CONNECT handling |
| GO-2026-6612 | HTTP/2 double flow-control refund | Affected HTTP/2 stream processing |
| GO-2026-6611 | Excessive CPU from HTTP/2 window changes | Peer-controlled HTTP/2 settings |
| GO-2026-6610 | HTTP/2 transport accepts malformed framing headers | Remote HTTP/2 upstream/client transport |
| GO-2026-6608 | MIME header memory-limit bypass | Applicable header or multipart parsing |
| GO-2026-6607 | Malformed ECH outer-extension references | Relevant TLS/ECH negotiation |
| GO-2026-6605 | HTTP/1 desynchronization after rejected CONNECT | Applicable client/proxy CONNECT exchange |
| GO-2026-6603 | HTTP/2 Trailer-header memory exhaustion | Affected HTTP/2 server request handling |
| GO-2026-6600 | Template escaping/context issue around `yield` | Relevant JavaScript template construction |
| GO-2026-6599 | Consecutive template-expression context issue | Relevant template construction |

The scanner reports Go fixes at 1.27.2/1.27.3 and `golang.org/x/net` v0.60.0. The Go project's current records independently confirm the certificate fix in 1.27.3 and the HPACK attack's HTTP/2-server precondition: [certificate advisory](https://pkg.go.dev/vuln/GO-2026-6809), [HPACK advisory](https://pkg.go.dev/vuln/GO-2026-6617).

Tapelog has real HTTP-client paths in transport, OTLP export and checkpoint submission. However, call-graph traces through generic interfaces do not prove that every listed runtime condition occurs. For example, the dashboard uses embedded static assets, and `web` starts a plain `ListenAndServe`; no added template execution or explicit HTTP/2 configuration was found in the changed code. Do not report twelve demonstrated exploits, or infer vulnerability-specific CVSS values from scanner order. No exploit attempts were made.

**Required remediation:** separately update the build/release toolchain to at least Go 1.27.3 within this series and x/net to at least v0.60.0, reconcile transitive requirements, and rescan the resulting exact commit and release binaries. These are minimum fixes identified by this scan, not a promise that those versions clear every future advisory. Retain scanner version, platform, toolchain, commit and exit status; scan target platforms too. Rerun regression/build/vet checks and Linux race tests. Investigate the additional three imported-package and three module findings with verbose output; they were not listed as reachable by these scans. Do not suppress advisories merely because the offline stdio demo passes.

### 3. P2 — Output and log paths may name the same new file

`cmd/tapelog/setup.go:132-173` independently accepts a fresh log path and exclusively creates the candidate, but never rejects a collision between them. `--log <fresh-path> --output <same-path>` succeeds. Starting the generated entry then replaces its own candidate JSON with session events. This occurs on the first start, without a reconnect.

Independent reproduction returned setup exit 0; invoking the saved command replaced the candidate with `session/start` events. The original input configuration remained untouched.

**Required remediation:** reject equivalent log/output paths before creating anything, accounting for absolute/relative spellings, Windows case equivalence and resolvable parent-directory aliases. Add an integration test proving failure leaves both source and output unchanged/absent, and a normal separate-path case still works.

### 4. P2 acceptance gate — Client compatibility is schema-tested, not accepted

The preservation tests cover all five selectors, but mostly inspect generated JSON. The executable integration uses the internal fixture with a modern `server/discover` exchange, not installed Roo, Cline, VS Code or Claude launchers. Preserving `cwd`, env and client settings syntactically does not prove each client's interpolation, environment filtering, approval UX or launch behavior. Windows `npx.cmd`/batch handling remains an explicit limitation. This is honestly disclosed, not evidence fabrication.

**Required acceptance work:** use fictional data and an already installed safe server in each supported client/version; record discovery, one allowed named call, one deny, one human-confirm decision, a failed startup, and reconnect behavior after finding 1 is fixed. Include spaced executable/upstream/working-directory paths and the client-supported batch launcher route where applicable. Compare inherited env/cwd behavior without printing real secrets. Do not activate owner settings as part of this review.

## Independent verification results

| Check | Result and boundary |
|---|---|
| Exact candidate checkout/status and final documentation-only commit | Confirmed; source remained clean |
| `git diff --check f79d48a eea7874` | PASS |
| `go test -count=1 -timeout 10m ./...` | PASS: 24 test-bearing packages, 30 package result lines |
| Windows `go build ./...`, `go vet ./...` | PASS |
| Linux amd64 and macOS amd64, CGO disabled | PASS compilation; no native execution |
| Fresh offline first-session demo | PASS: recording, deny, verification, what-if, behavioral scenario, replay and discovery-only guidance |
| Demo runtime | 1.257 seconds excluding compilation/install; not novice onboarding timing |
| Candidate and baseline vulnerability scans | Both FAIL, exit 3; same twelve reachable IDs |
| Generated candidate started twice | FAIL evidence retention: previous session replaced |
| Candidate output equals log path | FAIL path isolation: candidate replaced on first start |
| Real-client/native/archive/interactive usability acceptance | NOT RUN |
| Linux race-enabled tests | NOT RUN; compile success is not a substitute |

No dependency installation was needed; verification used cached dependencies with automatic toolchain downloads disabled. The vulnerability scanner queried its public advisory database. Temporary loopback activity in the existing tests did not install a service.

## Security, compatibility and DX assessment

The bounded implementation is sensible: exclusive candidate creation, strict duplicate-key rejection, raw JSON preservation, secret omission in normal previews, explicit default-allow messaging, no auto-confirm insertion, no automatic active-config writes, and clearer distinctions between observation and verification. No diff was present in policy evaluation, mediator/proxy, transport/replay, log/checkpoint/schema/limits or approval implementation, dependency manifests, or dashboard security handlers. Existing safety and historical-fixture tests passed independently. This supports scoped regression confidence, not comprehensive independent security acceptance.

Unix candidate creation requests mode 0600; Windows relies on inherited ACLs. Candidate files intentionally contain original credentials. No private Windows ACL creation or hostile-parent-directory guarantee is established. This is disclosed and does not constitute a new verified credential leak in the reviewed normal preview path.

Compatibility changes include strict rejection of malformed session JSON instead of silently skipping it, and an overview prepended to `inspect --plain` with the former trailing summary removed. Protocol/log format is unchanged, but scripts parsing plain text and inspection of partially written tails may behave differently. Document these changes; preserve historical fixture coverage. Do not advertise plain text as a stable machine-readable interface without a contract.

The new path to first value is easier for a developer who already has Go and a working stdio server. It remains a source-checkout demo followed by manual candidate review, backup and activation. Setup supplies no first MCP server, automatic approval configuration or binary-only demo. The guide correctly limits replay to recorded results, warns that denied/unanswered calls miss, distinguishes integrity from completeness, and disclaims five-minute and live-client claims. A fresh-machine first-time-user study is still needed before calling this easy for all developers.

The report's regression transcript is package-level output, not a complete build/vet/platform execution ledger; this review independently reproduced those checks. Invalid-policy, symlink and file-permission acceptance are not all represented by explicit cases in the new setup test file. Add targeted tests when changing those guarantees; do not describe five schema selectors as five accepted client integrations.

## Remediation order and exit criteria

1. **Fix evidence retention in generated configurations.** Two starts, concurrent starts and failed upstream startup must preserve earlier logs. Keep the change isolated and independently reviewed because it affects evidence semantics.
2. **Reject log/output aliases.** Prove no candidate is created or overwritten on rejection, including Windows path variants.
3. **Clear the vulnerability gate in a separate dependency/toolchain change.** Capture before/after scans and exact artifact identities; complete Linux race tests. No public release with the present reachable finding set.
4. **Qualify real client launch and human approval flows.** Record exact client versions and fictional-data transcripts; do not infer this from JSON tests.
5. **Qualify native Linux/macOS and clean-machine archive installation.** Exercise launcher/permissions, then have a first-time developer follow the guide without coaching. Measure installation separately from first successful mediated call; record friction and failures.
6. **Refresh the final report at the remediated commit.** State remaining gates, changed plain-output behavior, and the distinction between code acceptance and release acceptance.

Exit decision: reconsider bounded DX acceptance after items 1–2 and their regressions pass. Reconsider public release only after item 3 and the supported-client/platform acceptance gates pass. No publication action is authorized by this review.

Independent transcripts are in the adjacent `evidence/` directory. All reproductions used fictional fixtures in this review workspace.
