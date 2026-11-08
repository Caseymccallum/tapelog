# TAPELOG — M5: Visual Policy Studio

**Assigned agent:** Terra · **Role:** lead product engineer, Go developer, security-aware UX
**Branch:** `feat/m5-visual-policy-studio` (created from Sol's M4 baseline; not pushed, not merged)

A developer can now investigate a recorded agent call, draft a narrowly scoped
policy proposal, simulate its effect on recorded activity and understand the
result — without writing a Cedar expression or touching an active configuration
file. Nothing in Tapelog's evaluation engine was rebuilt: the Studio reads and
reports what `internal/policy` and `internal/replay` already decide.

---

## 1. Verified commits

| Role | Commit | Note |
| --- | --- | --- |
| Starting point (M4) | `5b7614e` "Document visual investigation evidence" on `feat/m4-visual-investigation` | verified with `git log feat/m4-visual-investigation` |
| M5 implementation | `f8455a6` "feat(studio): add a read-only visual policy studio to the session workbench" | this milestone's code |
| M5 documentation | this commit | review, docs, evidence |

Branch creation was `git switch -c feat/m5-visual-policy-studio 5b7614e`, so M2,
M3, M4, the DX branches and all existing recordings/configurations are untouched.
`main` was not modified and nothing was pushed or merged.

```powershell
$env:GIT_PAGER='cat'
git log --oneline -3 --no-decorate feat/m4-visual-investigation
git merge-base --is-ancestor 5b7614e HEAD        # M4 is contained in the M5 branch
git --no-pager diff --stat 5b7614e HEAD
```

---

## 2. Phase 0 — research and verification

### 2.1 What Tapelog's policy engine actually does (read from the code, not the docs)

Verified in `internal/policy/policy.go`, `internal/policy/cedar.go`,
`internal/policy/flows.go`, `internal/limits` and `internal/replay/whatif.go`:

- **Ordered, first-match-wins.** `Evaluate` walks rules in order; the first rule
  that is *in scope* (`tasks`), *unexpired* (`expires`), whose glob matches the
  tool and whose `where` clauses all pass, decides. Otherwise `default`
  (fail-closed `deny`).
- **Tool matching is a glob** over `*` and `?` compiled to an anchored regexp
  (`globToRegex`), not a regex the author writes.
- **`where` is real Cedar.** Each clause is joined with `&&` into
  `permit(principal, action, resource) when { ... }` and evaluated by
  `cedar-go` against `context.tool` and `context.args`. Evaluation errors fail
  closed: the rule is skipped.
- **`confirm` parks the call** for a human (allow once / allow for session /
  deny) and fails closed without a terminal.
- **Flows run before rules**, in `session` taint (conservative) or `value` taint
  (experimental) mode.
- **Limits are enforced around the call** by `internal/limits`, not by rule
  evaluation, so they never appear as a rule decision.
- **What-if is sequence-aware** and reproduces value taint, so it works as a
  regression check.

Two consequences shaped this milestone. First, the builder may only express what
those fields express. Second, an explanation is only honest if it is derived
from the same fields `Evaluate` reads.

### 2.2 A verified defect the Explorer now reports: `%` is not a Cedar wildcard

Cedar's `like` has exactly one wildcard, `*`. `%` and `?` are literal
characters. Several shipped packs author `like` patterns with `%` wildcards, so
those conditions can never match their targets.

Reproduction (`docs/reviews/evidence/m5/cedar-like-percent.txt`):

```text
percent-wildcard-intent  want=true  got=false  context.args.command like "%rm -rf /%"
star-wildcard            want=true  got=true   context.args.command like "*rm -rf /*"
pack=shell.yaml  tool=exec_cmd        cmd="sudo rm -rf /..."          -> confirm rule=default
pack=git.yaml    tool=run_git_command cmd="git push --force origin main" -> allow   rule=default
```

With `packs/git.yaml`, a force-push falls through every deny/confirm rule to
`default: allow`. This is an enforcement defect in the packs, not a Studio
limitation, and it is **out of scope to change here**: fixing it changes
enforcement behaviour, and this milestone was told to preserve existing
enforcement. The Studio therefore does the useful thing instead of the silent
thing — it flags the condition:

> `Cannot match as written: This pattern contains % , which Cedar's like operator
> treats as a literal character rather than a wildcard, so the condition only
> matches an argument that really contains a percent sign. Cedar's wildcard is * .`

Two tests hold the analyser to the engine rather than to its own opinion:
`packs/embed_test.go` asserts that any rule the Studio calls unable to match is
never the rule the engine chooses, and `internal/policy/explain_test.go` asserts
that a compound `||` expression is never simplified. See
`studio-shell-findings.png` for the rendered finding, and §9 for the follow-up.

### 2.3 Competitor and ecosystem research

**Observed features (from documentation), not our ideas**

| Product | What it documents | What it does not appear to offer |
| --- | --- | --- |
| **Microsoft Agent Governance Toolkit** (<https://github.com/microsoft/agent-governance-toolkit>) | Policy enforcement, zero-trust identity, execution sandboxing across Python/TS/Go/Rust/.NET; 10 formal specs, 992 conformance tests, 29 ADRs | No visual policy authoring or what-if over recorded agent traffic surfaced in its documentation index |
| **Docker MCP policy** (<https://docs.docker.com/ai/governance/reference/mcp-policy/>) | Cedar in a Docker `MCP` namespace; governed activity is **default deny**; a matching `forbid` beats a `permit`, including an approval-annotated one; `@requireApproval("reason")` maps approval into MCP elicitation; actions `register`, `invokeTool`, `invokePrimordial`, `readResource`, `getPrompt`; documented limits (e.g. tool listings are not Cedar-gated) | No documented simulation of a candidate policy against recorded calls |
| **Docker Sandboxes handbook ch. 19** (<https://supraj.dev/guides/docker-sandbox-19-mcp-policy/>) | "translate these intentions using Docker's current policy editor and reference. Validate the policy there before rollout." — an editor exists for entitled organizations; recommends an explicit deny/approve test matrix | Validation is described as manual reasoning plus live checks; no recorded-evidence replay |
| **Cedarling Authorization Policy Playground** (<https://cedarling.dev/playground>) | A browser workbench: pick an example, edit Cedar policies and request facts, compare ALLOW/DENY; "The Playground evaluates requests without performing the protected actions"; ~5–6 hand-authored request cases per example | Requests are hand-written facts, not recorded agent activity; no candidate-vs-active diffing; no drafting from an event; not MCP tool-call shaped |

**Published user requests (evidence of demand)**

- Docker MCP Gateway issue **#575**, "Policy engine is stateless and
  payload-blind: can't express combination, argument-level, or return-path
  policy" (opened 6 Sep 2026; open, no maintainer reply at time of reading). The
  author reports that evaluation "is evaluated per call … with no arguments, no
  results, and no call history" and asks for argument-level conditions, cross-call
  combination policy and return-path controls.
  <https://github.com/docker/mcp-gateway/issues/575>
  Tapelog already has argument conditions (`where:`) and cross-tool flows, so
  this is where our Studio adds the most value.

**Our own product ideas (not observed in the above)**

1. **Findings from the engine's own semantics** — conditions that cannot match
   (§2.2), rules provably shadowed by an earlier unconditional rule, broad
   permissions, and YAML keys the decoder silently ignores.
2. **Drafting from a recorded event**, prefilled only from what the recording
   supports, refusing glob tool names so a rule cannot silently widen.
3. **Comment-preserving insertion** into an existing policy, verified by
   re-parsing, instead of a lossy YAML round-trip.
4. **Candidate-vs-recorded safety warnings** — "would permit N call(s) that the
   recording shows as denied" and "would skip human approval for N".

---

## 3. What shipped

`tapelog web --dir <recordings> --policy <policy.yaml>` now opens a **Policy
Studio** in the same workbench: same sidebar, same visual language, no new build
step, no new dependency.

### Phase 1 — Policy Explorer

- Lists the workspace policy (read from `--policy`) and the seven shipped packs.
  A policy that does not compile is listed with the compiler error instead of
  being hidden.
- Per rule: plain-language sentence, the boundary's actual effect, tools, scope
  (task labels, expiry, condition count), recorded explanation, and per-condition
  explanations.
- **Precedence is explicit** — rules are numbered in evaluation order, flows are
  labelled "evaluated before per-call rules", and a rule an earlier unconditional
  rule always shadows says so.
- **Broad permissions** are called out with the reason (`*`, patterns with no
  literal characters, prefixes shorter than four characters).
- **Unsupported content is preserved and reported** — YAML keys the decoder
  ignores are listed under "Fields the engine ignores", not dropped.
- **Original source is one click away**, byte for byte, with its SHA-256 prefix.
  Cedar `where` expressions are always shown verbatim under "Original Cedar
  condition".

Only engine-supported semantics are described. A `where:` expression is
simplified *only* when the entire expression is one comparison against a single
literal (`equals`, `starts with`, `ends with`, `contains`, whole number, boolean).
Anything else renders as "Advanced condition: shown as source below, not
simplified."

### Phase 2 — Guided Policy Builder

One supported rule at a time, starting from the four common needs the milestone
names: block a tool, require approval, allow explicitly selected tools, and match
a supported argument condition. Plus task scope, RFC 3339 expiry and the session
limits the boundary enforces.

- Choosing an outcome explains what the boundary does for it.
- Argument conditions are progressive disclosure (inside "Advanced"), one
  top-level identifier argument per clause, with the missing-argument semantics
  stated: a call without that key does not match the rule.
- **Unsupported is refused, not approximated**: nested (`a.b`) or hyphenated
  argument names, decimal numbers, ranges, set membership and compound
  expressions produce a clear error pointing at the policy source.
- The base can be a **new candidate file**, the **workspace policy**, or any
  **shipped pack**.
- Extending a policy inserts the rule as text and leaves every other byte alone;
  inline `rules: [...]` shapes are refused with an explanation.

### Phase 3 — Create a policy from an event

`Create policy proposal →` in the session inspector carries the selected call
into the Studio:

- Prefills the tool name **exactly as recorded** (refused if it contains `*`,
  `?` or control characters) and a starting outcome derived from the recorded
  decision — never applied automatically.
- Lists the recorded argument *names* (with type and redaction state) so the
  operator can match on one; it does **not** copy values for them.
- States its own uncertainty: one call is one observation; a rule governs every
  future call to that tool; `deny` does not remove recorded evidence; redacted
  arguments may not match at run time.
- Binds the proposal to the recording revision, so a recording that changed
  underneath produces `409` instead of a stale draft.

### Phase 4 — Visual what-if

Simulation re-evaluates the recording with the boundary's own pipeline
(`internal/replay`), never a frontend approximation:

- Current recorded decision **and** candidate decision per call, with the
  matching rule and reason.
- Filters: changes only, newly blocked, newly allowed, new approvals, all calls.
- Counts, plus the scope statement and the explicit limitation: *"This shows
  what the candidate would have decided about activity that was already
  recorded. It does not prove how future, unseen calls will behave."*
- Any pack or the active policy can be simulated as-is from the policy list, so
  "what would this pack have done to my session?" is one click.

### Phase 5 — Safer policy workflows

- **Read-only by default.** There is no file-writing endpoint. Saving is an
  explicit browser action through the save picker, which refuses a file that
  already has content.
- **Candidate and active are separate** — the candidate panel is headed
  "CANDIDATE — NOT ACTIVE" and says nothing is active until `tapelog record
  --policy` is pointed at the saved file.
- **Nothing activates automatically**, and the active policy is only ever read.
- **No silent replacement**: extending refuses duplicate rule ids, refuses inline
  rules, requires the new rule to be first after re-parsing, refuses a candidate
  whose flows changed, and refuses limits on an extended policy (its own limits
  stay untouched).
- **Warnings when a candidate loosens**: "would permit N call(s) that the
  recording shows as denied", "would skip human approval for N", and the inverse
  "would block N call(s) that were recorded as allowed".
- **Validation is the real compiler** (`policy.Parse` → `normalize()` → Cedar
  compilation); invalid conditions produce the compiler's own error text.

### Phase 6 — Product experience

- Sidebar entry, breadcrumb, page heading and eyebrow consistent with M4.
- Keyboard: native `<details>`/`<summary>` for progressive disclosure, real
  `<button>`s with `aria-pressed` for selectors and filters, `aria-live` for
  status, labelled form controls, visible focus rings.
- Responsive: one column below 1100 px, two-column condition rows below 800 px.
- Light and dark palettes both verified; no state is carried by colour alone
  (every chip has a text label).
- Helpful empty states everywhere: no policy configured, no rules, no findings,
  no recording selected, simulation unavailable, no calls match a filter.
- The default surface is **not** a YAML editor: source is behind a disclosure.

---

## 4. Architectural decisions

**1. Additive only.** No change to `Evaluate`, Cedar compilation, flows, limits,
the mediator or the CLI's existing output. New pure functions: `policy.Parse`
(in-memory validation), `policy.Analyze`, the draft/render helpers, and
`replay.Simulate`. `replay.WhatIf` keeps its exact CLI output; both now share one
`orderedCalls` helper so their counts cannot drift.

**2. No server-side writes at all.** The Studio cannot corrupt an active
configuration because it has no code path that opens a policy for writing. The
candidate is text in a JSON response; persistence is the browser's save picker,
which refuses a non-empty target. This mirrors M4's "Make a test" decision and
keeps the trust model simple.

**3. Text insertion instead of a YAML round-trip.** Decoding a policy into a
struct and re-encoding it would silently drop comments, unknown fields and any
future syntax. `InsertRule` copies the source, splices a generated block at the
top of `rules:`, and then the result is **re-parsed and re-analysed**; if the new
rule is not first, or the flow count changed, the candidate is refused. Sample
output in `docs/reviews/evidence/m5/sample-candidate-extend-starter.yaml` shows
the pack's comments and formatting untouched.

**4. Explain narrowly and refuse to guess.** The condition recogniser requires
the *whole* expression to be one comparison with a single literal operand. This
was tightened during the browser review: a compound `||` chain whose last clause
ends in a quote was initially flattened into `equals "…"` — exactly the
misrepresentation the milestone forbids. `TestDescribeConditionOnlySimplifiesSingleComparisons`
and `packs/embed_test.go` now pin the rule.

**5. Findings must be provable.** Shadowing is reported only when an earlier rule
is unconditional *and* either matches every tool or has an identical tool set.
The analyser never claims a semantic superset it cannot prove.

**6. One evaluation engine, one ordering.** `Simulate` and the CLI what-if call
the same `reevaluateOrders`, so value taint, session taint and rule precedence
cannot diverge between the product and CI.

**7. Packs are embedded.** `packs/embed.go` embeds the shipped YAML so the Studio
shows and extends the exact bytes an operator would copy, and
`packs/embed_test.go` asserts the embedded copy still equals the file on disk.

**8. Bounded I/O.** 512 KiB policy files, 32 KiB request bodies, 64 KiB
candidate responses, the existing 16 MiB / 50,000-event recording limits, the
workbench's two-concurrent-snapshot budget, `DisallowUnknownFields`, POST-only
verbs, and the inherited loopback-Host / CSP / same-origin-POST / bearer-token
posture on every new endpoint.

---

## 5. Screenshots and browser demonstrations

All evidence is in `docs/reviews/evidence/m5/`. Recordings are fictional output
of `go run ./examples/workbench-demo --out m5-demo-sessions`; no live tool ran.

| File | Shows |
| --- | --- |
| `studio-active-policy.png` | Policy Studio over the workspace policy: 2 rules, 1 flow, default explained, precedence, limits, builder |
| `studio-shell-findings.png` | The Explorer catching `%`-based conditions in the shipped shell pack, with the raw Cedar one click away |
| `studio-builder-limits.png` | Full page including the builder and its advanced/limits sections |
| `walkthrough-1-candidate-and-simulation-default-deny.pdf` | From-event draft → candidate → simulation with `default: deny` (3 of 4 decisions change, 2 calls newly blocked) |
| `walkthrough-2-candidate-and-simulation-default-allow.pdf` | The same rule with `default: allow`: 1 approval added, and the warning "would permit 1 call(s) that the recording shows as denied" |
| `studio-light-theme.pdf` | Light palette: rule cards, chips, builder, notes |

### Browser walkthrough (condensed; full transcript in `browser-walkthrough.txt`)

1. Library → *Workspace audit* → selected the recorded deny `workspace__delete_file` (event 12).
2. Inspector shows the recorded decision and, new in M5, **Create policy proposal →**.
3. Clicking it opened Policy Studio with `From recorded call #9 · packages__lookup · recorded decision: allow`, the exact tool name prefilled, and four uncertainty notes.
4. Chose *Require human approval*, wrote an explanation, previewed.
   `CANDIDATE — NOT ACTIVE`, `Validated as a standalone candidate`,
   `This candidate would block 2 call(s) that were recorded as allowed.`,
   `sha256 89148f4f…`, `changes 3 of 4 recorded decisions`.
5. Switched the default to `allow`, previewed again: `Newly blocked (0)`,
   `Newly allowed (1)`, `New approvals (1)`, row `Allow → Require approval`
   with rule `confirm-packages-lookup`, plus
   `This candidate would permit 1 call(s) that the recording shows as denied.`
6. Opened the shell pack: `2 condition(s) contain a pattern that cannot match…`,
   and per rule `Advanced condition: shown as source below, not simplified.`
7. Toggled Appearance; the Studio renders in light mode.
8. Save was **not** executed in the walkthrough (it opens the OS picker); the
   refusal of a non-empty target is covered by the node test.

---

## 6. Verification

```powershell
go build -o bin/tapelog.exe ./cmd/tapelog
go vet ./...
go test ./...                                        # all packages pass
node --test internal/web/static/app.test.cjs         # 17 pass
powershell -File docs\reviews\evidence\m5\verify-no-write.ps1
```

Evidence files: `go-tests.txt`, `targeted-tests.txt`, `node-tests.txt`,
`no-write-verification.txt`, `cedar-like-percent.txt`, `browser-walkthrough.txt`,
`sample-candidate-extend-starter.yaml`.

### Milestone requirements → where they are proven

| Requirement | Proof |
| --- | --- |
| Generated rules compile correctly | `TestNewCandidateIsNarrowAndCompiles`, `TestGeneratedConditionsMeanWhatTheySay`, `TestNewCandidateWithLimits` — every candidate goes through `policy.Parse`, i.e. the boundary's own `normalize()` plus Cedar compile |
| Visual controls preserve rule semantics | `TestGeneratedConditionsMeanWhatTheySay` evaluates each generated condition against matching and non-matching arguments; `TestConditionValuesCannotInjectOrWiden` proves a `*` in a value does not become a wildcard and that a missing argument does not match |
| What-if agrees with the existing evaluator | `TestStudioSimulationAgreesWithTheReplayEngine` compares the Studio's `/api/policy/preview` simulation with a direct `replay.Simulate`; `TestSimulateClassifiesChangesLikeTheCLI` compares `Simulate` with `WhatIf`; `TestSimulateReproducesValueTaint` pins taint parity |
| Unsupported conditions are not silently lost | `TestDescribeConditionOnlySimplifiesSingleComparisons`, `TestStudioNeverSimplifiesACompoundCondition`, `TestAnalyzeReportsFieldsTheEngineIgnores`, `an unsupported condition is never given an invented summary` |
| Existing policies remain unchanged | `TestInsertRulePreservesExistingSource` (comments, unknown fields, existing rules, flows), `TestStudioPreviewExtendsAPolicyWithoutRewritingIt`, `TestInsertRuleKeepsCustomIndentation`, `TestInsertRuleRefusesInlineRules` |
| Active configuration cannot be modified unintentionally | `TestStudioInspectsTheActivePolicyWithoutTouchingIt` (bytes + mtime), `verify-no-write.ps1` (policy and every recording hashed before and after all endpoints), `an invalid candidate can never be saved`, `the candidate save refuses a non-empty file` |
| Recorded data remains unmodified | `TestStudioFromEventProposesANarrowRuleAndLeavesTheRecordingAlone`, `verify-no-write.ps1` |
| Historical sessions still work | Existing `internal/web` tests (library, activity, events, compare, test builder) pass unchanged; the M2/M3/M4 fixtures still load and simulate |
| Untrusted event content cannot inject code or corrupt generated policy output | `TestConditionValuesCannotInjectOrWiden`, `TestValidateRuleDraftRejectsWhatTheEngineCannotExpress`, `TestStudioFromEventRefusesUnsafeRecordedToolNames`, `policy cards render hostile rule text literally`, `malicious payloads and filenames stay literal text` — there is no HTML sink (`textContent` only) and CSP is `script-src 'self'` |
| Existing security decisions unchanged unless a candidate is evaluated | `go test ./...` green, including `internal/mediator`, `internal/proxy`, `internal/approval`, `internal/policy`; no evaluation code was modified |
| Studio endpoints respect the workbench posture | `TestStudioSimulateScoresAListedPolicy` (unknown source → 404), `TestStudioRejectsOversizedAndMalformedBodies`, `TestCSRFRefusesCrossOriginPost`, existing host-pinning and CSP tests |

### Defects found and fixed during this milestone

1. **Compound conditions were flattened.** A `||` chain ending in a quoted
   literal was summarised as a single `equals`, because the recogniser checked
   only the first and last characters of the operand. Fixed by requiring the
   operand to match a fully anchored literal pattern; regression tests added at
   unit level and across every shipped pack.
2. **`el(tag, class, text)` was called with four arguments** in three new render
   paths, silently dropping the rule and tool ids. Caught by the hostile-content
   test; fixed.
3. **`GET /api/policy/preview` returned 400 instead of 405.** Fixed with an
   explicit `postOnly` guard.

---

## 7. Known limitations

- **The shipped `shell`, `git` and `filesystem` packs contain `%`-style `like`
  patterns that cannot match** (§2.2). Left unchanged here because fixing them
  changes enforcement; the Studio flags them, and §8 recommends the fix. Until
  then, treat those packs' `where:` denies as inert.
- **One rule at a time.** There is no multi-rule draft, no reordering, no editing
  or deleting of an existing rule, and no `flows:` authoring.
- **Limits are authorable only on a new candidate.** An extended policy keeps its
  own limits untouched (refused with an explanation), and `max_calls_per_tool`
  (a map) is display-only.
- **Argument conditions are deliberately narrow**: one top-level identifier
  argument, `equals` / starts / ends / contains, whole numbers and booleans. No
  nested paths (`a.b`), no sets, no numeric ranges, no compound expressions —
  those stay in the source.
- **Shadowing detection is intentionally conservative.** It reports only provable
  cases (a universal unconditional rule, or an identical tool set); it does not
  attempt glob-language inclusion.
- **Simulation needs a usable recording** — verified, complete, unambiguous,
  non-inline, within the 16 MiB / 50,000-event limits — as M4 already requires. A
  stale revision is refused rather than silently simulated.
- **Simulation is bounded by the recording**: it evaluates recorded (redacted)
  arguments, so it reproduces the live engine but cannot reveal raw values, and it
  says nothing about unseen calls.
- **The Studio cannot know which policy produced a historical recording.** It
  shows the recorded verdict and rule id as evidence about that session, not as a
  claim about the policy being drafted.
- **Saving depends on the browser.** With File System Access the operator gets a
  picker that refuses a non-empty target; without it, a download is offered.
- **No candidate history or rule-by-rule diff** against the active policy.
- **`--policy` is explicit.** The workbench does not discover a policy from the
  recordings directory, and a policy larger than 512 KiB is refused with CLI
  guidance.

---

## 8. Recommended next development opportunities

1. **P0 — repair the pack conditions.** Replace `%`/`?` wildcards in
   `packs/*.yaml` with `*` and add a pack test asserting each deny actually fires
   for a representative command. Small change, real security payoff; the single
   highest-value follow-up.
2. **Promote the analyser to the CLI**: `tapelog policy lint --policy p.yaml`
   reusing `policy.Analyze`, so findings (dead conditions, shadowing, broad
   permissions, ignored fields) can fail CI the way `whatif` already does. The
   analysis is already a pure function with no Studio dependency.
3. **Quick fixes from findings.** Each finding could offer a one-click corrected
   candidate (`%` → `*`, drop an unreachable rule, narrow a broad pattern)
   through the same generation path, so the safety properties carry over.
4. **Flow and limit authoring.** `flows:` is exactly where Tapelog differs from
   the Docker gateway (issue #575); a toxic-flow builder ("these tools must not
   feed those tools") is the natural next control.
5. **Candidate-versus-active comparison**, rule by rule: "this candidate adds 1
   rule, leaves 3 unchanged, changes the default".
6. **Multi-rule drafts and a candidate manifest**, so one investigation can
   accumulate several proposals before anything is saved.
7. **Opt-in activation**, only if operators ask for it, and only explicitly
   designed: digest-pinned, audited, never automatic.

---

## 9. Reproduction

```powershell
git switch feat/m5-visual-policy-studio
go build -o bin/tapelog.exe ./cmd/tapelog
go run ./examples/workbench-demo --out m5-demo-sessions
.\bin\tapelog.exe web --dir m5-demo-sessions --policy packs/starter.yaml
# open http://127.0.0.1:8923, choose Policy Studio (or append #studio to the URL)

# optional: prove read-only behaviour end to end
powershell -File docs\reviews\evidence\m5\verify-no-write.ps1

# tests
go test ./...
node --test internal/web/static/app.test.cjs
```

Not pushed. Not merged. Awaiting owner approval.





