# TAPELOG — M5.1: Policy-Pack Correctness

**Assigned agent:** DeepSeek · **Role:** security-focused Go engineer, policy-engine specialist
**Branch:** `fix/m5-1-policy-pack-correctness`, created from `feat/m5-visual-policy-studio`
**Nature:** focused engineering repair, not a release milestone. Not pushed, not merged.

The M5 review reported that `packs/shell.yaml` and `packs/git.yaml` contained
`where:` conditions that could never match. This milestone verifies that claim
against the real evaluator, finds the rest of the same class of defect, repairs
every affected rule, and replaces the CI gate that let it through with tests
that prove enforcement.

---

## 1. Verified baseline

| Item | Value |
| --- | --- |
| Branch point | `feat/m5-visual-policy-studio` at `b2b905d` "docs(review): record the M5 final commit in the milestone table" |
| Working branch | `fix/m5-1-policy-pack-correctness` |
| Go | 1.27.3 windows/amd64 |
| Cedar engine | `github.com/cedar-policy/cedar-go`, via `internal/policy/cedar.go` |

```powershell
$env:GIT_PAGER='cat'
git switch -c fix/m5-1-policy-pack-correctness   # from b2b905d
go build ./...
go test ./...                                    # green before any change
node --test internal/web/static/app.test.cjs     # 17 pass before any change
```

Before editing a single YAML file, the M5 claims were reproduced through the
shipped evaluator (`packs` loaded with `policy.Load`, `Evaluate` called on
fictional arguments). No pack was taken on trust from the M5 write-up.

---

## 2. Root cause

There are two independent defects, one shared root cause.

**The shared root cause: CI proved compilation, not enforcement.**
`packs/packs_test.go` loads each pack, compiles its Cedar conditions and
requires at least one deny rule. A condition whose pattern can never match
satisfies all three checks, so the gate was green while the denies were inert.
`packs/README.md` claimed *"a pack that stops parsing or grows a broken
condition fails CI"* — the second half of that sentence was not true.

**Defect A — `%` used as a wildcard (shell, git, postgres).**
Cedar's `like` has exactly one wildcard, `*`. `%` is a literal character. So

```cedar
context.args.command like "%rm -rf /%"
```

matches a command that *contains a percent sign*. It never matches
`rm -rf / --no-preserve-root`, so the rule is skipped and the call falls to the
pack default.

**Defect B — `[6-9]` used as a character class (fetch).**
Cedar `like` has no character classes either. `http://172.1[6-9]*` only matches
a URL containing the literal text `[6-9]`, so the entire `172.16.0.0/12` private
range was uncovered. The same rule also spelled the scheme `http://` only, so
`https://169.254.169.254/` (the cloud metadata endpoint over TLS) was allowed.

**Defect C — deny placed after an overlapping confirm (slack).**
Not a wildcard problem, but the same class of "the policy does not do what it
documents". The engine is first-match-wins; `sends-confirm` matches `create_*`
and was listed before `no-channel-creation`, so a channel creation was offered
to a human instead of being refused.

`filesystem.yaml` and `starter.yaml` were audited and needed no change.

---

## 3. Affected rules and packs

Audited with the real evaluator before any edit ("before" column is observed
behaviour, not the documented intent):

| Pack | Rule | Condition class | Before M5.1 | Documented intent |
| --- | --- | --- | --- | --- |
| shell | `no-catastrophe` | `%` | `sudo rm -rf / --no-preserve-root` → **confirm** (default) | deny |
| shell | `no-privilege-escalation` | `%` | `sudo apt-get install nginx` → **confirm** (default) | deny |
| git | `no-history-destruction` | `%` | `git push --force origin main` → **allow** (default) | deny |
| git | `no-hook-or-config-tamper` | `%` | `git config core.hooksPath /tmp/h` → **allow** (default) | deny |
| git | `pushes-confirm` | `%` | `git push origin main` → **allow** (default) | confirm |
| postgres | `no-schema-destruction` | `%` | `DROP TABLE users` → **allow** (default) | deny |
| postgres | `writes-confirm` | `%` | `INSERT INTO t VALUES (1)` → **allow** (default) | confirm |
| fetch | `no-internal-urls` | `[6-9]`, `http://` only | `http://172.20.0.1/` → **allow**; `https://169.254.169.254/` → **allow** | deny |
| fetch | `https-only-for-posts` | missing-arg fail-open | `post` with no `url` → **allow** | deny |
| slack | `no-channel-creation` | rule order | `create_channel` → **confirm** (shadowed) | deny |
| filesystem | all | — | correct | — |
| starter | all | — | correct | — |

The sharpest consequence: **under `packs/git.yaml`, a force-push was allowed**,
because `default: allow` and every deny was inert.

---

## 4. Exact corrections

### 4.1 Semantics established before rewriting anything

The milestone said not to assume that replacing `%` with another symbol produces
the intended semantics. Every behaviour below was verified against the shipped
engine first, and is now pinned by
`internal/policy/cedar_semantics_test.go`:

| Property | Verified behaviour |
| --- | --- |
| Wildcards | `*` matches any sequence (including empty). That is the only wildcard. |
| Anchoring | A `like` pattern is anchored: `*rm -rf /` means *the string ends with `rm -rf /`*. A pattern with no `*` is an equality test. |
| `%`, `?` | Literal characters. |
| `[...]` | Literal characters; **no character classes**. |
| Case | Case-sensitive. No `lower()`/`upper()` in Cedar. |
| `has` | `context.args has url` tests key presence. |
| `\t`, `\n` | Understood as escapes inside a `like` pattern. |
| `\|\|` / `&&` over an evaluation error | `true \|\| error` is `true`; `false && error` fails closed. This is what makes a missing-argument guard expressible. |

The anchoring property is the one that changed the design: it makes
`*rm -rf /` a *precise* root-token match rather than a prefix match, which is
why `rm -rf /tmp/build` can stay on the pack's confirm default.

### 4.2 `packs/shell.yaml`

- `%` → `*` for every pattern (mkfs, dd, fork bomb, curl/wget pipe, chmod,
  redirect to a disk device, sudo, su, doas).
- Root/home wipes are matched as a **target token followed by a boundary**:
  end-of-string, a space, `;`, `&`, `|`, or a trailing slash, for both `-rf` and
  `-fr`. Quoted forms `"/"`, `'/'` and `"$HOME"` are covered, as are `$HOME` and
  `${HOME}`.
- `--no-preserve-root` is denied on its own; the flag exists only to permit
  deleting the root filesystem.
- Added `mke2fs` (alongside `mkfs`) and `chmod *777*` (covers `-R 777`).

Deliberately unchanged: `default: confirm`, the limits, the injection mode, and
the decision that **ordinary recursive deletes are confirmed, not denied**
(`rm -rf /tmp/build`, `rm -rf ~/proj/build`). That is the pack's documented
policy and it is asserted by tests.

### 4.3 `packs/git.yaml`

- `%` → `*` for reset --hard, filter-branch, filter-repo, update-ref -d, clean.
- Force pushes now match `*push *--force*` (covers `push --force`,
  `push origin main --force`, `push --force-with-lease`, and `push "--force"`)
  plus `*push -f*`, `*push "-f"*`, `*push '-f'*`.
- `clean -*f*` covers `-f`, `-fd`, `-xdf` in one pattern.

Deliberately unchanged: `default: allow`, the push confirm rule (still after the
denies, so a force push is denied rather than confirmed), and text matching on
the command string.

### 4.4 `packs/postgres.yaml`

- `%` → `*`, and **case is enumerated**: SQL keywords are case-insensitive while
  `like` is not, so each keyword now appears in UPPER, lower and Title case.
- The keyword/target separator is matched as a space **and** a tab
  (`\t`), because whitespace is not a bypass in SQL.

Deliberately unchanged: `default: allow`, and the confirm rule for writes.

Known and documented gap: a deliberately mixed case (`DrOp`) or an SQL comment
between the letters (`DR/**/OP`) is not matched. See §8.

### 4.5 `packs/fetch.yaml`

- `[6-9]` → the **16 third octets of 172.16/12 enumerated explicitly**
  (`172.16.` … `172.31.`).
- Every pattern is now `*://…` instead of `http://…`. One pattern covers http,
  https, ftp and any scheme casing for a numeric host.
- `https-only-for-posts` now fails closed:
  `!(context.args has url) || (!(url like "https://*") && !(...))`. A post whose
  arguments carry no `url` can no longer fall through to `default: allow`.

Deliberately unchanged: `default: allow`, the limits, and the decision to keep
the CGNAT rule at `100.64.*` rather than over-matching public `100.x` space.

### 4.6 `packs/slack.yaml`

- `no-channel-creation` (deny) moved **before** `sends-confirm` (confirm).
  Nothing else changed; the flows and limits blocks are untouched.

### 4.7 Duplicate keys removed

Four packs briefly carried duplicate `flows:` / `limits:` / `injection:` keys
while being edited. `yaml.v3` rejects duplicate mapping keys, so the packs could
not load at all; the enforcement tables caught it on the first run and each was
corrected before any commit. The final files contain one block per key, and
`TestEmbeddedPacksMatchTheirFiles` plus the load-everything tests would fail if
that regressed.

---

## 5. Before/after policy decisions

Produced by the shipped CLI, not by a bespoke harness, against fictional sample
calls in `docs/reviews/evidence/m5-1/representative-calls.jsonl`:

```powershell
tapelog policy test --policy packs/git.yaml --calls representative-calls.jsonl
```

Full matrix in `docs/reviews/evidence/m5-1/cli-before-after.txt`. The headline
rows:

```text
## git.yaml
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ deny     run_git_command    no-history-destruction   history-destroying git commands are out of bounds
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ deny     run_git_command    no-history-destruction   history-destroying git commands are out of bounds
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ deny     run_git_command    no-history-destruction   history-destroying git commands are out of bounds
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ deny     run_git_command    no-history-destruction   history-destroying git commands are out of bounds
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ deny     run_git_command    no-hook-or-config-tamper hook and transport config tampering is a supply-chain attack
- allow    run_git_command    default                  no rule matched; policy default is "allow"
+ confirm  run_git_command    pushes-confirm           pushes change shared state

## postgres.yaml
- allow    query   default                 no rule matched; policy default is "allow"
+ deny     query   no-schema-destruction   schema destruction and privilege changes are out of bounds   (x4)
- allow    query   default                 no rule matched; policy default is "allow"
+ confirm  query   writes-confirm          data writes need a human in the loop

## fetch.yaml
- allow    fetch   default                no rule matched; policy default is "allow"
+ deny     fetch   no-internal-urls       internal/metadata URLs are SSRF pivots        (https localhost, 172.20)
- allow    post    default                no rule matched; policy default is "allow"
+ deny     post    https-only-for-posts   outbound data must travel over TLS             (no url argument)

## shell.yaml
- confirm  exec_cmd  default                  no rule matched; policy default is "confirm"
+ deny     exec_cmd  no-catastrophe           catastrophic or self-installing commands are out of bounds   (x6)
- confirm  bash      default                  no rule matched; policy default is "confirm"
+ deny     bash      no-privilege-escalation  privilege escalation is not delegated to agents

## slack.yaml
- confirm  create_channel  sends-confirm          messages to humans need a human in the loop
+ deny     create_channel  no-channel-creation    workspace structure is not an agent's to change

## filesystem.yaml
(no verdict changes)
```

Every intentional behaviour change in that list is a call moving from allow or
confirm **to deny** for an operation the pack's own comment describes as out of
bounds. `filesystem.yaml` produced no change, which is the control: it was
already correct.

### The M5 scenario, explicitly

`docs/reviews/evidence/m5-1` also contains the Studio simulation of the same
scenario through the Product surface. `TestStudioWhatIfShowsTheRepairedDenies`
records eight fictional calls (one dangerous and one legitimate per tool) and
asserts, per pack, exactly one call flips `allow -> deny` with the expected rule,
and that the legitimate neighbour is untouched. For git it then asserts
`recorded: allow -> candidate: deny` with the history reason, i.e. **the
previously allowed force-push is now denied** — which is what the pack's
documented intent says must happen.

---

## 6. Regression tests

391 subtests pass across the repository; 133 of them are in `packs/`.

| File | What it proves |
| --- | --- |
| `packs/enforcement_test.go` | the shared harness: verdict, exact rule id, non-default origin for denies, and a non-empty explainable reason |
| `packs/enforcement_shell_test.go` | 33 cases: catastrophe, escalation, and the legitimate calls that must stay on the confirm default |
| `packs/enforcement_git_test.go` | 21 cases: history/hook denies, push confirm, and safe git operations |
| `packs/enforcement_postgres_test.go` | 20 cases: schema/privilege denies, write confirm, and free reads |
| `packs/enforcement_fetch_test.go` | 24 cases: SSRF across every private range, scheme and shape, plus TLS-for-posts |
| `packs/enforcement_filesystem_slack_test.go` | 15 cases: secrets/hooks, the corrected slack ordering, and legitimate neighbours |
| `packs/condition_lint_test.go` | `%` lint, "no dead conditions" via the Studio analyser, and **every deny rule in every pack is reached by at least one enforcement case** |
| `internal/policy/cedar_semantics_test.go` | the engine semantics the packs depend on (`*` only wildcard, `%`/`?`/`[]` literal, case-sensitive, `has`, `\t`, `||` over error) |
| `internal/web/policystudio_packs_test.go` | the Studio reports no dead conditions for any shipped pack |
| `internal/web/policystudio_packs_simulation_test.go` | Studio descriptions match the repaired conditions; Studio what-if denies the previously allowed calls with the expected rule and reason |

The harness asserts all five properties the milestone asked for:

1. **An intended prohibited operation is denied** — the verdict assertion.
2. **A relevant legitimate operation is handled as documented** — the
   legitimate rows in every table, plus `shellLegitimateCases` as its own
   assertion (shell keeps `rm -rf /tmp/build` on confirm; git keeps `git status`
   allowed; postgres keeps `SELECT` allowed; fetch keeps a public `https` URL
   allowed).
3. **The correct rule matches** — exact `dec.RuleID` comparison, so a deny that
   fires for the wrong reason fails.
4. **The action is blocked before execution** — every deny must come from a rule
   (`IsDefault == false`, `RuleID != "default"`) and carry a non-empty reason,
   which is what the mediator records.
5. **An unexpected argument shape does not bypass** — `post` with no `url`, a
   nested `{"request":{"url":...}}`, extra arguments alongside `url`,
   quoting, `-fr` flag order, tab separators, and `$HOME`/`${HOME}`/`"$HOME"`
   spellings.

### The tests fail on the old packs

That claim is itself tested. Restoring the M5-baseline YAML and running the suite:

```text
go test ./packs/           -> 74 failing assertions
go test ./packs/ -run TestNoPackUsesPercentInACondition
  -> names all 7 affected rules in git, postgres and shell
```

Evidence: `docs/reviews/evidence/m5-1/enforcement-fails-on-prefix-content.txt`
and `guard-fails-on-prefix-content.txt`. The corrected packs then pass with the
same tests unchanged.

### Guards against the same class of mistake

- `TestNoPackUsesPercentInACondition` — a `%` inside any condition fails CI with
  a message naming `*` as the wildcard.
- `TestEveryDenyRuleCanFire` — a deny rule that no enforcement case reaches
  fails CI. This is the assertion that makes `packs/packs_test.go`'s
  compile-only check insufficient visible in the build log.
- `TestCedarLikeSemanticsThePacksRelyOn` — if `cedar-go` ever changes `like`,
  the packs would silently disarm again; this test is the alarm.

---

## 7. Compatibility findings

| Area | Command | Result |
| --- | --- | --- |
| Whole suite | `go test ./...` | pass (0 failures, 391 subtests) |
| Static analysis | `go vet ./...` | clean |
| Frontend | `node --test internal/web/static/app.test.cjs` | 17 pass (unchanged) |
| Pack loading | `packs/packs_test.go` | all 7 packs load, compile, and retain a deny |
| Cedar export | `tapelog policy compile --policy packs/<each>.yaml` | all 7 render policy text (`cli-policy-compile.txt`) |
| Policy dry-run | `tapelog policy test --policy ... --calls ...` | used to produce the before/after matrix |
| What-if | `tapelog policy whatif --policy packs/<each>.yaml <recording>` | all 7 re-evaluate; summaries in `cli-policy-whatif.txt` |
| Trajectory assertions | `tapelog test examples/trajectory-demo/scenario.yaml` | `✔ canonical-trajectory-demo (9 assertions)` |
| Replay | `go test ./internal/replay/ -v` | pass, including tamper refusal and value-taint parity |
| Security invariants | `internal/mediator`, `internal/proxy`, `internal/approval`, `internal/policy` | pass; no engine or mediator code was modified |
| M5 Studio | browser, `--policy packs/shell.yaml` | 0 findings, "no conditions that cannot match" (`studio-shell-after-repair.png`) |

**No production code changed.** The diff is YAML, tests, pack documentation,
`.gitignore` (to commit the fictional sample-calls fixture) and this review.
`internal/policy` gained only a test file. The evaluator, the mediator, the CLI
and the Studio are untouched, so the packs are the only thing whose behaviour
changes — which is what the milestone set out to repair.

**Trust boundary unchanged.** Limits are still enforced by `internal/limits` and
the injection mode is untouched; only `where:` conditions changed.

**Cost.** The repaired conditions are longer: `shell/no-catastrophe` is a
40-pattern compound expression and `fetch/no-internal-urls` is 25. Cedar only
evaluates a rule's conditions after its tool glob matches, and the expressions
are linear `like` chains, so the practical cost is a string scan per matching
rule. No benchmark regression was observed in `go test ./...` timings.

---

## 8. Remaining limitations

These are properties of string matching, not of this repair. They are now
documented in the pack headers and, where practical, encoded as tests.

1. **Mixed-case SQL keywords.** `postgres.yaml` covers UPPER, lower and Title
   case. A deliberate `DrOp` is not matched, and neither is `DR/**/OP`. A
   database role without DDL privileges is the reliable control.
2. **Hostname casing in URLs.** `*://localhost*` matches any scheme casing but
   not `LOCALHOST`, because `like` is case-sensitive. Numeric hosts (`127.x`,
   `10.x`, `169.254.x`, `172.x`) have no case and are unaffected.
3. **Non-canonical IP forms.** `http://2130706433/`, octal or hex encodings of
   `127.0.0.1` are not matched. A network egress policy is the reliable control;
   `fetch.yaml` is a heuristic and says so.
4. **CGNAT breadth.** `100.64/10` is covered at `100.64.*` only. Expressing
   64-127 needs a character class, and over-matching `100.1*` would deny public
   `100.x` addresses.
5. **Windows destructive commands.** `format C:`, `rd /s /q C:\` are not
   covered, and a case-sensitive matcher cannot cover cmd.exe's
   case-insensitivity. Use `--sandbox-*` or an OS-level boundary.
6. **SQL/command parsing.** These are text matches. A keyword in a string literal
   or a column named `delete_flag` can produce a false positive; a statement
   assembled at run time can produce a false negative.
7. **Tool arguments that omit the verb.** `git.yaml` matches on the command
   string. A `git_push` tool called with `{"command":"--force origin main"}`
   never says "push", so the history rule does not fire; that call is asserted
   as *allowed* in `enforcement_git_test.go` so the gap is visible rather than
   assumed closed.
8. **`git clean -nf`** (a dry run) is denied, because `clean -*f*` cannot tell a
   dry run from a real one. Fail-safe, documented.
9. **Unquoted `rm -fr` with `$HOME`.** Covered for `-rf`; `-fr $HOME` is covered
   for end-of-string and trailing space only. Exotic, documented.
10. **The packs remain starting points.** The README has always told operators to
    copy and tune them; a pack cannot know your repository layout, table names
    or channel names.

---

## 9. What this milestone did not do

- No global default was changed to conceal an ineffective rule. `shell` stays
  `confirm` by default, `git`/`postgres`/`fetch`/`slack`/`filesystem` stay
  `allow`. Every repair is in a `where:` condition or a rule's position.
- No restriction was widened without a documented effect. The one place a
  reviewer might expect a broader pattern — `rm -rf /anything` — is deliberately
  *not* denied, because the pack's documented policy confirms non-catastrophic
  deletes and a test asserts that.
- Nothing was pushed or merged.

---

## 10. Evidence index

All in `docs/reviews/evidence/m5-1/`:

| File | Contents |
| --- | --- |
| `representative-calls.jsonl` | 29 fictional sample calls; no tool was executed |
| `cli-before-<pack>.txt` / `cli-after-<pack>.txt` | `tapelog policy test` output, M5 baseline vs corrected |
| `cli-before-after.txt` | the same, reduced to the verdict changes |
| `cli-policy-compile.txt` | Cedar export for all 7 packs |
| `cli-policy-whatif.txt` | what-if summaries for all 7 packs against a fictional recording |
| `cli-trajectory-test.txt` | trajectory assertions still pass |
| `guard-fails-on-prefix-content.txt` | the `%` lint failing on the pre-fix YAML |
| `enforcement-fails-on-prefix-content.txt` | 74 enforcement assertions failing on the pre-fix YAML |
| `go-tests.txt`, `targeted-tests.txt`, `replay-tests.txt`, `node-tests.txt` | test transcripts |
| `studio-shell-after-repair.png` | the M5 Studio showing 0 findings for the repaired pack (contrast with the M5 `studio-shell-findings.png`) |

## 11. Reproduction

```powershell
git switch fix/m5-1-policy-pack-correctness
go build -o bin/tapelog.exe ./cmd/tapelog

# enforcement, lint and semantics
go test ./packs/ ./internal/policy/ ./internal/web/ -v

# the before/after matrix (fictional calls, nothing executed)
.\bin\tapelog.exe policy test --policy packs/git.yaml `
    --calls docs\reviews\evidence\m5-1\representative-calls.jsonl

# the Studio, now reporting no dead conditions for this pack
.\bin\tapelog.exe web --dir m5-demo-sessions --policy packs/shell.yaml
# open http://127.0.0.1:8923 and choose Policy Studio
```

## 12. Final commit

| Role | Commit |
| --- | --- |
| Baseline (M5 final) | `b2b905d` |
| M5.1 repair, tests and evidence | `232ea11` |
| M5.1 review (this document) | the commit that adds this file |

Not pushed. Not merged. Awaiting approval.





