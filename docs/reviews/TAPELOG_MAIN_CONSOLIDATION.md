# TAPELOG — MAIN CONSOLIDATION (DX1.1 → M9)

## MAIN CONSOLIDATED — VERIFIED AND PUSHED

| Item | Value |
| --- | --- |
| Previous `origin/main` | `f79d48a` (v0.4.0 + roadmap; the only branch on the remote) |
| Verified M9 tip | `befb356` "Document M9 workspace walkthrough and verification evidence" |
| Integration branch | `integration/m9-complete-main` = `befb356` |
| Local `main` | `befb356` → then this document's commit |
| Remote `origin/main` | `befb356` (verified twice by `git ls-remote`) |
| Strategy | **Fast-forward.** No merge commit, no squash, no cherry-pick |
| Conflicts | **None** |
| Push | `f79d48a..befb356  main -> main` (exit 0), then re-fetched and verified |
| Release / tag / deploy | **None created or triggered** |

Verification executed on the integrated tree: build, vet, **283 Go tests pass /
0 fail / 3 skipped** across 35 packages, **42/42 frontend tests**, **13/13
policy-pack enforcement tests**, the full CLI workflow set, and a browser
walkthrough including mobile widths. Details in §11–§14.

---

## 1. Scope

Consolidate all verified Tapelog development through M9 onto GitHub `main`, verify
the integrated application, and push the verified result. No new product work.

---

## 2. Discovered repository locations

Eight checkouts and one build directory. Only the primary repository has a
network remote; every milestone checkout used a **local bundle** or no remote,
which is why none of this work had ever reached GitHub.

| Location | HEAD | Branch | Remote | State |
| --- | --- | --- | --- | --- |
| `C:\Users\Casey\Web Apps\agent-boundary` | `a8dda8e` | `feat/m8-1-regression-lab-completion` | `origin` = github.com/Caseymccallum/tapelog | clean, 11 local branches |
| `…\Codex\2026-11-07\…-3\work\tapelog-dx1_1` | `ed871c1` | `feat/developer-experience-evolution` | `origin` = github | clean |
| `…\work\tapelog-m2` | `e9ad518` | `feat/session-workbench-m2` | `origin` = github | clean |
| `…\work\tapelog-m3` | `aa78754` | `feat/m3-investigation-intelligence` | `origin` = github | clean |
| `…\work\tapelog-m4` | `5b7614e` | `feat/m4-visual-investigation` | `origin` = github | clean |
| `…\work\tapelog-m7` | `269ab1d` | `feat/m7-behaviour-cli-and-workflows` | **bundle** `tapelog-m6-base.bundle` | clean |
| `…\work\tapelog-m8` | `32355bf` | `feat/m8-agent-regression-lab` | **bundle** `tapelog-m8-base.bundle` | clean |
| `…\work\tapelog-m9` | `befb356` | `feat/m9-unified-developer-workspace` | **bundle** `tapelog-m9-base.bundle` | clean |
| `…\work\m9-verified-build` | — | — | — | M9 build/verification directory |
| `…\Codex\2026-10-29\…\outputs\projects\Tapelog` | `3fac67b` | `codex/pullboard-local-setup-20261029` | **none** | clean, unrelated (see §7) |

No checkout was modified, reset, cleaned or deleted. References were imported with
read-only fetches (`git fetch <path> <branch>:<ref>`). M5 and M6 were never
cloned separately; they exist only as commits already inbound to the primary
repository (`b2b905d`, `198f5b7`).

---

## 3. Remote state before

`git ls-remote --heads origin` returned exactly one ref:

```
f79d48a9c31fe1f50d7ccbf90dcd637f66b69caf   refs/heads/main
```

**Nothing from DX1.1 through M9 had ever been pushed.** There were no remote
feature branches to reconcile, which is why the integration reduces to a single
fast-forward.

---

## 4. Milestone ancestry

Verified with `git merge-base --is-ancestor` against the M9 tip, not by trusting
the reference table. The actual 29-commit lineage `f79d48a..befb356`:

| Milestone | Reference given | Commits in the lineage | In M9 |
| --- | --- | --- | --- |
| DX1.1 | `fcc56dff` | `dc1e39c`, `b3af42d`, `77d736f`, `eea7874`, `24fb92e`, `462e495`, `fcc56df` | ✅ |
| M2 — Session Workbench | `e9ad518` | `0632284`, `a989c77`, `e9ad518` | ✅ |
| M3 — Investigation Intelligence | `aa78754` | `b3a2d00`, `aa78754` | ✅ |
| M4 — Visual Investigation | `5b7614e` | `1bda7fb`, `5b7614e` | ✅ |
| M5 — Visual Policy Studio | `b2b905d` | `f8455a6`, `3b28a2f`, `605a538`, `b2b905d` | ✅ |
| M5.1 — Policy-Pack Correctness | `fix/m5-1-…` = `ba39ec0` | `232ea11` "make the shipped pack denies actually enforce", `ba39ec0` | ✅ |
| M6 — Behaviour Intelligence | `198f5b7` | `e76becb`, `198f5b7` | ✅ |
| M7 — Behaviour CLI | `269ab1d` | `c1b0126`, `269ab1d` | ✅ |
| M8 — Regression Lab Foundation | `32355bf` | `32355bf` | ✅ |
| M8.1 — Regression Lab Completion | `feat/m8-1-…` | `6ec356b`, `a8dda8e` | ✅ |
| M9 — Unified Developer Workspace | `befb356` | `0001224`, `befb356` | ✅ |

`befb356..f79d48a` = **0 commits**: `main` held nothing the milestone line
lacked. `f79d48a..befb356` = **29 commits**.

M9's own checkout pointed its `feat/m8-1-regression-lab-completion` at
`a8dda8e` — byte-identical to the M8.1 tip in this repository — so M9 was built
directly on the M8.1 work and no M8.1 change needed re-applying.

---

## 5. Milestone coverage matrix

Ancestry proves commits are present; this table proves the *capability* is
present in the final tree, by naming a concrete artifact in `befb356`.

| Requirement | Concrete artifact in the tree | Verified by |
| --- | --- | --- |
| Developer setup and diagnostics | `tapelog doctor`, `tapelog setup` | ran `doctor`: 7 checks, all required pass |
| Session Workbench | `internal/web/sessions.go`, `internal/web/static/app.js` | browser walkthrough §14 |
| Investigation Intelligence | `internal/web/behaviour.go`, evidence rail | "Start with the evidence" cards, §14 |
| Visual assertion creation | "Create case" builder tab | created a case in the UI, §14 |
| Policy Studio | `internal/web/policystudio.go` (24 KB) + 3 test files | simulation ran, §13 |
| Corrected policy packs | `packs/*.yaml` + 6 `enforcement_*.go` | 13/13 enforcement tests, §13 |
| Behaviour Intelligence | `internal/behaviour/`, `internal/web/behaviour.go` | `behaviour summary/repeats/failures`, §12 |
| Behaviour CLI | `cmd/tapelog/behaviour.go` | CLI runs above |
| Shared analysis changes | `internal/replay/`, `internal/scenario/` | `policy whatif`, legacy YAML scenario |
| Saved regression cases | `internal/lab/lab.go`, `internal/web/lab.go` | `lab run`, UI save, §12/§14 |
| Regression suites | `lab suite` + suite editor | 5-case suite, 3 outcomes, §12 |
| Correct evidence eligibility | claim-shape eligibility + reason codes | `TestEligibilityFollowsTheClaimShape`, §13 |
| Unified navigation | 8 nav destinations, badge `LOCAL` | snapshot §14 |
| Saved investigations | browser-local bookmarks (`#investigations`) | saved + reopened, §14 |
| Connected workflows | session → case / compare / policy draft | "Create regression case" from a call |
| M9 desktop/mobile refinements | fixed nav at 390 px, no overflow | device-metrics checks, §14 |
| Recording integrity | `internal/session/chain.go` `O_APPEND`/`O_EXCL` | 6 tamper tests, `verify` on two eras |

File-change summary against the previous `main`: **203 files, +26,400 / −489**.

---

## 6. Merge strategy

```
integration/m9-complete-main  =  befb356   (fast-forward from f79d48a)
```

Because `f79d48a` is an ancestor of `befb356` and the reverse set is empty, the
integration *is* the M9 commit history. Therefore:

- **No merge commit.** One would have recorded a divergence that does not exist.
- **No squash.** The 29 commits carry the milestone narrative (`232ea11` "make the
  shipped pack denies actually enforce", `6ec356b` "finish the agent regression
  lab"); collapsing them would destroy the audit trail the reviews reference.
- **No cherry-picking.** The verified branch already contains the complete
  ancestral history, so replaying individual commits could only duplicate or
  drop work.
- **No rebase, no force-push.** The M8/M8.1 branches are unchanged.

Local `main` was advanced with `git fetch . integration/m9-complete-main:main`,
which is **fast-forward-only by construction** — it would have refused any
non-FF update. It moved `f79d48a → befb356`.

---

## 7. Independence check — does `main` hold work to preserve?

`f79d48a` contributed **zero** commits not already in M9, so nothing was
overwritten. Two findings needed investigation rather than assumption:

### 7.1 The DX1.1 branch that is *not* an ancestor (`ed871c1`)

`codex/dx1.1-remediation` is not an ancestor of M9, and `fcc56df` is not its
ancestor either — two parallel DX1.1 lineages. Since `ed871c1` carries
`fix(security): update vulnerable Go toolchain and modules`, dropping it silently
would have been the wrong call. It is not missing:

```
fcc56df tree: 1cd870edc61457978dd1f2ae49b22cb76d70c103
ed871c1 tree: 1cd870edc61457978dd1f2ae49b22cb76d70c103
git diff --numstat fcc56df ed871c1 -> 0 lines
git diff ed871c1 befb356 -- go.mod go.sum -> empty
```

The two branches are **content-identical** implementations of the same three
commits (`24fb92e/462e495/fcc56df` versus `3459920/4f6a17a/ed871c1`). The
security remediation is present in `main` as `go 1.27.0` + `toolchain go1.27.3`,
which is also the toolchain this verification ran on (`go1.27.3 windows/amd64`).
No cherry-pick was needed and nothing was lost.

### 7.2 The one commit in the estate that is not in `main` (`3fac67b`)

`C:\Users\Casey\Documents\Codex\2026-10-29\…\projects\Tapelog` sits on the old
`main` plus one commit, `3fac67b` "chore: configure isolated local Pullboard
workflow". Its diff is only:

```
.claude/skills/pullboard-{decompose,plan,run,signoff,spec-review,triage,verify}/SKILL.md
.codex/config.toml
.githooks/{commit-msg,pre-commit,pre-merge-commit}
```

That is local agent tooling from an unrelated install experiment, with no remote
configured. It is **deliberately excluded** from `main`: it is not Tapelog
product code, the consolidation mandate forbids introducing unrelated changes, and
the checkout is left intact and untouched. If those hooks are wanted later they
belong in their own commit with their own rationale.

---

## 8. Conflicts

**None.** No file in `main` diverged from M9, so no conflict resolution was
performed and no side of any file was chosen by hand.

---

## 9. Verification environment

| | |
| --- | --- |
| Where | `C:\Users\Casey\Web Apps\tapelog-integration-m9` (`git worktree` on `integration/m9-complete-main`, clean) |
| Platform | Windows, `go1.27.3 windows/amd64`, Node via `node --test` |
| Data | fictional, generated in `C:\Users\Casey\Web Apps\tapelog-m9-fictional` (outside the checkout) |
| Tool calls executed | **none** — every run reads recordings; `policy whatif` and Studio simulation re-evaluate recorded calls only |

The fictional library was produced by the repository's own generators
(`go run ./tools/gen-m6-demo <dir>`, `go run ./tools/gen-m9-navigation <dir>`):
8 named recordings plus one 2,000-call recording for pagination, 2,039 calls,
2,034 results, 8 tool errors and 4 deny decisions in total.

---

## 10. Build and static checks

```
go build ./...                       exit 0
go vet ./...                          exit 0
go build -o tapelog-m9.exe ./cmd/tapelog   exit 0  (34,315,264 bytes)
```

---

## 11. Test results

| Suite | Result |
| --- | --- |
| `go test ./... -count=1` | **26 packages ok, 9 without test files, 0 failures** |
| `go test ./... -v` (individual tests) | **283 passed, 0 failed, 3 skipped** |
| `node --test internal/web/static/app.test.cjs` | **42 tests, 42 pass, 0 fail** |
| `go test ./packs/ -v` | **13 pass, 0 fail**, incl. `TestEveryDenyRuleCanFire` |
| Replay | included in the 283 (`internal/replay`, 11 files) |
| Scenario / legacy DSL | included in the 283 (`internal/scenario`) |
| CLI | included in the 283 (`cmd/tapelog`) |
| Regression lab | included in the 283 (`internal/lab`, `internal/web` lab tests) |

The 3 skips are pre-existing platform-conditional tests, not disabled coverage.

---

## 12. Executed workflows

Every item was run against the integrated build, not inherited from a report.

**1. Recording discovery.** `web --dir …` listed 9 recordings through
`/api/sessions`; the browser library header read "Session library 9" with per-row
`✓ Chain verified`, `Recorded end`, call counts, deny decisions and tool errors.

**2. Session investigation.** `tapelog inspect` is an interactive TUI and was not
driven; the equivalent surface was exercised in the browser (§14): evidence
cards, activity filter, selected-call details.

**3. Behaviour Intelligence.**

```
tapelog behaviour summary --dir <demo>
Behaviour summary · partial
Sessions 9 · calls 2039 · results 2034 · tool errors 8 · denied calls 4 · …
failures (2):  db__query ×5 (2 sessions), net__fetch ×3
repeats  (6):  git__push repeated_denial ×4, db__query possible_retry_loop, …
Coverage: 9 analysed / 9 discovered                              exit 2
```

**4. CLI summaries and comparisons.**

```
tapelog behaviour compare --dir <demo> --candidate 02-retry-loop.jsonl \
                          --reference 01-baseline-release.jsonl
Observed attempts · reference: 5 / candidate: 6 · 11 outcome or count differences
Repeated identical call keys are paired by occurrence; the pairing is uncertain.
New failure groups (1) · Possible retry loop · db__query
```

The uncertainty caveat is printed by the tool itself, not inferred.

**5. Policy Studio simulation.** In the browser: selecting the `git` pack against
`04-denials.jsonl` reported *"the candidate changes 4 of 6 recorded decisions ·
Newly allowed (4) · Block → Allow"*. That is the **correct** verdict: the
recorded args are `{"remote":"origin"}` with no `--force`, so
`no-history-destruction` legitimately does not match. The Studio did not flatter
the change.

**6–8. Regression cases and suites.** Five cases authored in the library; CLI and
UI agree exactly:

| Case | Outcome | Exit |
| --- | --- | --- |
| Force push must be denied (`04-denials`) | **PASS** | 0 |
| Polling must not be used (`03-polling`, deliberate) | **FAIL** — `no_attempts: matched 6 attempt(s), want at most 0` | 1 |
| Interrupted session (`07-interrupted`) | **INCONCLUSIVE** — "the recording has no complete session/start and session/end" | 2 |
| Release run denies a force push (`01-baseline`) | **INCONCLUSIVE** — tool never reached the boundary | 2 |
| Discovery-only recording (`08-discovery-only`) | **INCONCLUSIVE** — "no call to files__read reached the boundary" | 2 |

Suite: `1 pass, 1 fail, 3 inconclusive` — the three outcomes are distinguished
with their own reasons, and a FAIL dominates the exit code. The CLI and the UI
produced the identical table.

**9. Saved investigations.** Saved from the investigation bar, appeared under
`#investigations`, reopened to `#session=04-denials.jsonl&tab=test&seq=6` with the
breadcrumb "Sessions / force a hotfix / Event #6".

**10. M9 navigation and deep links.** `#session=…&tab=…&seq=…`, `#behaviour`,
`#compare&a=…&b=…`, `#lab&suite=…&outcome=latest`, `#investigations`, `#studio`
all restored the intended view; §14.

**Cross-checks.** A case created through the browser was then run from the CLI
(`PASS`, exit 0), proving one library and one semantics across both front ends. A
legacy YAML scenario (`called` / `attempted` / `denied` / `result_never_contains` /
`never_called`) passed, so pre-M8 test files still behave as documented.

---

## 13. Security and compatibility evidence

Each requirement was traced to a named rule and a passing test in the integrated
tree, not to a milestone report.

| Requirement | Rule / artifact | Evidence |
| --- | --- | --- |
| Git force-push denial | `no-history-destruction` | `enforcement_git_test.go:11` "force push" — `git push --force origin main` → `VerdictDeny`; also `embed_test.go:85` |
| Destructive SQL denial | `no-schema-destruction` | `enforcement_postgres_test.go` — `DROP` upper/lower/title case, and after a `--` comment line |
| Catastrophic shell denial | `no-catastrophe` | `enforcement_shell_test.go` — `rm -rf /`, `sudo rm -rf / --no-preserve-root`, `rm -rf ~/projects` |
| Internal URL restrictions | `no-internal-urls` | `enforcement_fetch_test.go` — loopback, `169.254.169.254`, `metadata.google.internal`, `https://` and `HTTPS://`, `ftp://` |
| Slack channel-creation restriction | `no-channel-creation` (plus `sends-confirm`, `no-exfil-to-chat`) in `packs/slack.yaml` | `TestFilesystemAndSlackEnforce` |
| Every bundled deny rule exercised | `condition_lint_test.go` fails any deny no enforcement test reaches | `TestEveryDenyRuleCanFire` |
| Policy Studio uses the real evaluator | `policystudio.go:439` → `replay.Simulate(tape, p, task)`; `analyse()` → `policy.Parse` + `policy.Analyze` | simulation produced real verdict deltas (§12.5) |
| Historical recordings readable | `examples/calls.jsonl` from the v0.2–v0.4 era | `tapelog verify` → `OK … 1 events, chain intact`, exit 0 |
| Recording integrity preserved | `chain.go`: `O_EXCL` on create, `O_APPEND` on write | `TestVerifyDetectsTamper/Deletion/Reorder/RewrittenChain`, `TestLoadVerifiedRefusesTampered` |
| Workbench never rewrites recordings | `internal/web` opens no `.jsonl` for write; `api.go:188` rejects any non-plain leaf name | read-only by construction; `verify` unchanged after the walkthrough |
| Existing test-file semantics | legacy DSL `called` vs `attempted` (documented in `scenario.go`) | scenario using all five check kinds passed |
| M8.1 claim-specific eligibility intact | `TestEligibilityFollowsTheClaimShape` | 4 lab eligibility tests pass; `denied` on `git__push` = PASS, not INCONCLUSIVE |
| Missing evidence → INCONCLUSIVE | `TestEligibilityDoesNotConvertMissingEvidenceToPass`, `TestEveryInconclusiveOutcomeCarriesACode` | 3 distinct inconclusive reasons observed live (§12) |
| Case overwrite safeguards | `evidence_sha256` + revision, else refused | `TestLabSaveCreatesRefusesOverwriteAndRespectsRevisions`; the UI-written case recorded `evidence_sha256` and `evidence_seqs` |

Nothing in this list was relaxed, and no policy semantics were altered: the pushed
tree is byte-identical to the verified M9 tree.

---

## 14. Browser verification

Full walkthrough in a real browser against the integrated build
(`web --dir <fictional> --listen 127.0.0.1:8955`).

| Check | Result |
| --- | --- |
| Navigation | 8 destinations — Sessions, Investigations, Behaviour, Compare, Regression cases, Test suites, Policies, Getting started |
| Milestone badge | `LOCAL` (not `M6`) |
| Library | 9 recordings, each `✓ Chain verified`, with deny/error/end-state badges |
| Session view | investigation bar, "Start with the evidence" (4 deny decisions), tabs Activity / All events / Repeated activity / Create case |
| Evidence deep link | selecting a denial set `…&seq=6` in the URL |
| Connected actions | Create regression case · Compare recording · Draft policy, on the selected call |
| Case builder | rule preselected; preview → **PASS · Expectation satisfied by eligible recorded evidence**; save enabled only after preview |
| Save | wrote `integration-check-force-push-denied.json` with `eligibility`, `evidence_sha256`, `evidence_seqs`; filename sanitised (`:` → `-`) |
| Regression lab | 6 cases with plain-language restatement of each expectation; suite editor with add/remove/save/run |
| Suite run | 1 PASS / 1 FAIL / 3 INCONCLUSIVE, outcome in the URL (`&outcome=latest`), FAIL badge on the suite |
| Investigations | saved a bookmark, reopened it, revision-aware restoration |
| Behaviour | failures, repeats, per-tool table, deny-rule table (`no-force-push` × 4) |
| Compare | `#compare&a=…&b=…`, boundary + behaviour deltas with explicit uncertainty |
| Studio | 7 packs, per-pack simulation, rule precedence, draft-a-rule, real simulation result |
| Mobile 390 × 844 | session / behaviour / lab / studio: `scrollWidth == innerWidth == 390`, 0 overflowing elements, nav `position: fixed`, `display: flex` |
| Console | **no errors and no warnings** across the whole walkthrough |

---

## 15. Findings noticed but not fixed

Recorded so they are not lost, deliberately **not** patched: this is a
consolidation, and changing code would make the pushed tree differ from the
verified milestone work.

1. **`undefined` in the per-recording rate column (cosmetic).** In Behaviour the
   zero-call recording (`08-discovery-only`) renders `undefined · no results` and
   `undefined · no calls` where other rows show `0% of N`. Copy only; no
   computation is affected.
2. **Sticky investigation bar can overlap the session tab row.** At some scroll
   offsets the tab strip sits beneath the sticky bar and an automated click on
   "Create case" was refused because a `<span.eyebrow>` owned that point. Scrolling
   cleared it and keyboard focus is unaffected, so this is most likely ordinary
   sticky-header behaviour — but it was observed, so it is recorded.
3. **Version string still reads 0.4.0.** `doctor` reports `tapelog 0.4.0` while the
   tree contains M9. Correct for an unreleased branch; a release would bump
   `internal/buildinfo` and tag `v*`.

---

## 16. What was deliberately not done

- **No release, tag or deployment.** `.github/workflows/release.yml` fires only on
  `v*` **tags**, so pushing `main` cannot release. `ci.yml` runs on push to `main`
  with `permissions: contents: read` (build/vet/test only) — it will start, and it
  is expected to pass on the same commands verified here. `compat-live.yml` is
  scheduled/manual and `continue-on-error`.
- **No branch deletion.** All 11 local branches, all 7 milestone checkouts and the
  M9 bundle references are untouched. No remote branches were created either: the
  milestone branches remain local, reachable from `main`.
- **No force-push, no reset, no rebase, no squash.**
- **No security test removed or weakened**, no policy semantics changed.
- **No new product work.** The two findings in §15 were left for a deliberate
  follow-up rather than smuggled into a consolidation commit.
- **Not verified:** native Linux/macOS runs (only Windows), a live-client MCP
  server, and a screen-reader audit. `compat-live` and the `os-sandbox` preflight
  check both require Linux/CI, so they are inherited expectations, not evidence
  produced here.

---

## 17. Final state

| Item | Value |
| --- | --- |
| Local `main` before | `f79d48a` |
| Verified M9 | `befb356` |
| Integration branch | `integration/m9-complete-main` = `befb356` (clean worktree) |
| Push | `f79d48a..befb356  main -> main`, exit 0 |
| `origin/main` after re-fetch | `befb356cff234631e996f8f4ff223a6fc96c1e28` |
| Milestones reachable from `origin/main` | all 11 (verified individually after the push) |
| This document | committed on `main` immediately after `befb356` |
| Working state | clean; local `main` == `origin/main` |

Re-checked immediately before pushing (`git ls-remote` still returned `f79d48a`),
and again afterwards (returned `befb356`).

---

## 18. Reproduction

```powershell
# integration worktree
git worktree add ..\tapelog-integration-m9 -b integration/m9-complete-main befb356
cd ..\tapelog-integration-m9

# build and test
go build ./...; go vet ./...; go test ./... -count=1
node --test internal/web/static/app.test.cjs

# fictional data (never touches real recordings)
go run ./tools/gen-m6-demo C:\tapelog-demo
go run ./tools/gen-m9-navigation C:\tapelog-demo

# workflows
.\tapelog.exe behaviour summary --dir C:\tapelog-demo
.\tapelog.exe lab suite --dir C:\tapelog-demo --suite suite.json
.\tapelog.exe policy whatif C:\tapelog-demo\04-denials.jsonl --policy examples\policy.yaml
.\tapelog.exe web --dir C:\tapelog-demo --listen 127.0.0.1:8955
```

**MAIN CONSOLIDATED — VERIFIED AND PUSHED**




