# Tapelog DX1.1 remediation report

Date: 2026-11-08. **GO for continued independent review of the local remediation. NO-GO for public release.** The two reproduced data-loss defects are fixed in the reviewed candidate, and the 12 baseline reachable Go findings no longer appear in the final scan. Live-client acceptance, native Unix execution, release-artifact provenance and clean-machine onboarding remain open.

## Candidate and preservation

- Requested base: `eea7874025c9b76660d6903c4d5ac64e7decfe04` (`eea7874`) on `feat/developer-experience-evolution`.
- Remediation was performed in an isolated, writable copy at `work/tapelog-dx1_1`, preserving the original checkout. This copy had the same base commit and branch name. The original working tree was verified clean before this work; no source configuration or session archive was modified.
- Candidate commits: `345992036e30ad9b6dcae3a25465d8ab6eb46d8d` (recording and path safety), then `4f6a17a725cb29cee2ca27d6dcbe92422c0075d7` (toolchain and module fixes). This report is a follow-up commit.
- No push, merge or release occurred. Release artifact status/provenance was not established, and this source candidate must not be represented as an already released binary.

## Confirmed defects and remediation

**P1 recording loss.** The independent review generated a persistent config and started it twice against the same `--log`. The second start printed that the 602-byte file would be overwritten; the first session ID disappeared. See the independent report and `work/restart-probe.txt` for the exact reproduction.

Generated configs now use `record --recordings-dir`, defaulting to `<client-config-directory>/tapelog-recordings`. Each launch uses a UTC timestamp plus 96 bits of cryptographic randomness and `O_CREATE|O_EXCL`, with up to 16 collision retries. Directory/file modes request 0700/0600 on Unix-like systems; Windows uses inherited ACLs. Each independent file starts a new hash chain. The chosen path is printed at startup and inspection/verification/replay accept the concrete file path. The offline first-session demo now exercises this mode.

Acceptance evidence: setup was exercised twice through generated configuration, and the test verifies the first file is byte-identical and both chains verify. Sixteen concurrent writer launches produced distinct, valid files. A failed upstream startup left a verifiable partial chain and did not alter the generated config. An uncreatable recording path fails with an error and leaves the blocking file unchanged. **A physical full-disk/write-after-open failure was not simulated.** Filesystem permission semantics, hostile directory replacement and Windows ACL hardening remain platform-specific risks.

Compatibility boundary: explicitly setting legacy `--log` still uses the prior fixed-file writer, which warns and overwrites on reuse. `setup --log` remains for one-off/fixed-path compatibility and its help text warns that repeated starts overwrite. The default persistent setup no longer selects that mode. Existing scripts that explicitly select `--log` must choose unique names or migrate to `--recordings-dir`.

**P2 path collision.** The independent review showed that candidate output and session log could be the same fresh filename, allowing the first start to replace generated JSON with a JSONL chain. Setup now canonicalizes absolute paths and existing symlink ancestors, normalizes lexical aliases, and compares Windows paths case-insensitively. It rejects collisions among the active client config, candidate output, fixed log, and recordings directory before candidate creation. Regression cases cover identical and relative aliases, Windows case variants, and configuration/candidate/recordings-directory collisions; rejected setup preserves the active config. Symlink-creation tests were not run because the Windows session lacks confirmed symlink privilege; arbitrary filesystem aliases cannot all be resolved portably.

## Vulnerability triage

The original independent review's Go 1.27.0 Windows/amd64 scan reported 12 reachable IDs. I reproduced those same IDs on the `eea7874` source archive with a fresh scan, then scanned the candidate using Go 1.27.3 and the same `govulncheck` configuration. Candidate `govulncheck -show verbose ./...` exits 0 and reports no vulnerabilities. Captures are retained in `evidence/`.

| ID and baseline issue | Baseline module/version → fixed candidate | Reachability and runtime relevance | Disposition |
|---|---|---|---|
| [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809), compromised intermediate trust | Go `crypto/x509` 1.27 → 1.27.3 | Scanner trace reaches certificate verification through JUnit report writing. Actual hostile certificate verification path was not exercised. | Fixed by Go 1.27.3 toolchain. No demonstrated exploit. |
| [GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617), HTTP/2 HPACK race/server crash | `x/net` 0.58.0 → 0.62.0 | Baseline trace reaches HTTP/2 encoder/framer symbols through logging, approval formatting and OTLP. Advisory requires affected HTTP/2 server behavior; Tapelog exposure/precondition was not demonstrated. | Fixed by x/net update. No HTTP/2 exploit test. |
| [GO-2026-6613](https://pkg.go.dev/vuln/GO-2026-6613), HTTP/1 desynchronization after successful CONNECT | Go `net/http` 1.27 → 1.27.3 | Trace reaches generic client/header and OTLP paths. Relevant server-side CONNECT exchange was not demonstrated. | Fixed by toolchain. |
| [GO-2026-6612](https://pkg.go.dev/vuln/GO-2026-6612), HTTP/2 flow-control refund | `x/net` 0.58.0 → 0.62.0 | Symbol traces are mostly formatting/framing; affected stream sequence was not demonstrated. | Fixed by x/net update. |
| [GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611), HTTP/2 window-change CPU use | `x/net` 0.58.0 → 0.62.0 | Trace reaches HTTP/2 framer methods; attacker-controlled SETTINGS sequence was not tested. | Fixed by x/net update. |
| [GO-2026-6610](https://pkg.go.dev/vuln/GO-2026-6610), malformed HTTP/2 framing headers | Go HTTP/2 and `x/net` 0.58.0 → Go 1.27.3 and x/net 0.62.0 | `transport.HTTP.Send` reaches HTTP/2 client transport; real remote HTTP/2 upstream behavior was not tested. | Both affected components updated. |
| [GO-2026-6608](https://pkg.go.dev/vuln/GO-2026-6608), MIME header memory-limit bypass | Go `net/textproto` 1.27 → 1.27.3 | Trace reaches HTTP client MIME parsing and web multipart parsing. Exploit-sized inputs were not tested. | Fixed by toolchain. |
| [GO-2026-6607](https://pkg.go.dev/vuln/GO-2026-6607), malformed ECH extension references | Go `crypto/tls` 1.27 → 1.27.3 | Trace reaches TLS through generic HTTP client, verify and report writer paths. ECH negotiation/precondition was not established. | Fixed by toolchain. |
| [GO-2026-6605](https://pkg.go.dev/vuln/GO-2026-6605), client desynchronization after rejected CONNECT | Go `net/http` 1.27 → 1.27.3 | Trace reaches `http.Client.Do`; relevant CONNECT proxy exchange was not exercised. | Fixed by toolchain. |
| [GO-2026-6603](https://pkg.go.dev/vuln/GO-2026-6603), HTTP/2 Trailer memory exhaustion | `x/net` 0.58.0 → 0.62.0 | Scanner shows symbol reachability through HTTP/2 functions; attacker-controlled server request/trailer path was not demonstrated. | Fixed by x/net update. |
| [GO-2026-6600](https://pkg.go.dev/vuln/GO-2026-6600), `html/template` `yield` escaping/context | Go `html/template` 1.27 → 1.27.3 | Scanner trace reaches web template execution. No attacker-controlled template construction was identified or tested; dashboard uses embedded assets. | Fixed by toolchain. |
| [GO-2026-6599](https://pkg.go.dev/vuln/GO-2026-6599), consecutive template-expression context tracking | Go `html/template` 1.27 → 1.27.3 | Same generic web template execution trace; exploitable template input was not demonstrated. | Fixed by toolchain. |

The original 12 were inherited from the baseline; neither remediation commit changes the relevant call sites. No exploit was demonstrated and the symbol-level scan is not proof that each runtime precondition exists. The Go toolchain directive is now 1.27.3; x/net is 0.62.0, with related x/crypto, x/sys, x/text and grpc updates. The final verbose scan also clears the previously observed non-reachable package/module advisories. Fixed versions address the scanner's current database findings, not future advisories.

Binary exposure is unresolved: no trustworthy official release artifact was available for identity comparison in this review. Any binary built from the baseline source using Go 1.27.0 contains the affected standard library; actual exploitability depends on each advisory's conditions. Rebuild and scan the exact release artifacts before any release decision.

## Security, compatibility and regression results

The change touches recording path selection, setup preflight, tests, docs/demo and Go dependencies. It does not alter policy evaluation, approval decisions, mediation/proxy semantics, redaction rules, replay format, dashboard protection or the hash-chain event format. Existing policy-denial and replay regression suites passed. Explicit fixed `--log` mode retains overwrite semantics as described above.

- `go test -count=1 -timeout 10m ./...`: PASS; all 24 test-bearing packages and all 30 package result lines passed.
- `go build ./...`, `go vet ./...`, `git diff --check`: PASS on Windows/amd64, Go 1.27.3.
- `CGO_ENABLED=0` Linux/amd64 and macOS/amd64 `go build ./...`: PASS cross-compilation only; no native execution.
- Offline first-session demo: PASS with fictional fixture data; generated config recorded discovery, one allowed result and one policy denial, then inspection and verification. Demo runtime was 2.403 seconds excluding compilation. This is not novice usability or clean-machine install evidence.
- Vulnerability scan: baseline reproduced 12 reachable IDs; final candidate verbose scan exits 0, no vulnerabilities reported.
- WSL native execution unavailable (`wsl.exe --list --quiet` returned access denied); Linux race tests were not run. No Docker fallback was used.
- Roo Code 3.54.0 and Cline 4.1.26 are installed, but no live client was launched. Claude Code was not available on PATH. Active client settings were left untouched. All real-client compatibility remains **UNVERIFIED**.

The beginner journey is materially easier for a developer with Go and a working stdio fixture: setup previews a safe per-start recording directory, creates a new candidate file, and prints inspection/verification commands. The offline walkthrough demonstrates record → inspect → verify → replay. It does not establish actual client connection, approval-queue interaction, a clean-machine install, time-to-first real tool call, or success for novice developers. Keep the five-minute/all-developers UX claim unaccepted.

## Severity-ranked release assessment and remediation tasks

**GO:** local code remediation is ready for independent review against the specified defects. **NO-GO:** public release or release approval.

1. **P1 — Release artifact assurance.** Identify the intended release source/toolchain and exact Windows/Linux/macOS binaries; rebuild with Go 1.27.3+, run `govulncheck`/binary metadata checks on each artifact, and retain hashes and provenance. Do not infer release-binary safety from this source scan.
2. **P1 — Native platform/security regression.** Run Linux/macOS native tests and Linux race-enabled tests; add realistic recording write-error/disk-full coverage where reliable, and symlink alias cases where supported. Verify file modes and recovery after interrupted writes.
3. **P2 — Real client acceptance.** In isolated fictional configs, run installed Roo/Cline/VS Code supported paths and a Claude-compatible local client where available. Exercise paths with spaces, explicit candidate review/apply, preservation, restart, mediated allow/deny, human approval, verification/replay, failed startup and rollback. Never change active owner settings or enable auto-approval.
4. **P2 — Onboarding acceptance.** Have a first-time developer follow the guide on a clean machine/archive, record commands/manual edits/failures/time to first mediated call, and assess whether they can locate recordings after restart.
5. **P2 — Legacy fixed path migration.** Decide in a separately reviewed compatibility change whether explicit `--log` should refuse existing files by default. Until then, document and retain the warning; generated persistent configs are safe by default.

## Rollback

In the isolated copy, revert the two remediation commits in reverse order: `git revert 4f6a17a725cb29cee2ca27d6dcbe92422c0075d7` followed by `git revert 345992036e30ad9b6dcae3a25465d8ab6eb46d8d`. This removes the remediation without resetting or altering prior feature-branch history. Do not restore over the original checkout's files. No remote rollback is applicable because nothing was pushed.

## Evidence files

The `evidence/` directory contains the original independent review, baseline and candidate scan captures, restart and collision reproductions, test output and offline demo transcript. The final candidate verbose scan is the controlling scan evidence; `govulncheck` used Go 1.27.3 and the current vulnerability database available during this run.
