# Agent regression testing (`tapelog test`)

Behavioral regression **at the tool boundary**: assertions over the
tool-call trajectory of a recorded cassette. Deterministic — no LLM judge
required (research: docs/RESEARCH-EVOLUTION.md).

```yaml
version: 1
scenario: release-flow
fixture: ../fixtures/release-session.jsonl  # a recorded session (cassette)
policy: ../policies/prod.yaml               # optional: re-evaluate verdicts
assert:
  - called: { tool: publish_artifact, args: { channel: stable }, times: 1 }
  - never_called: "shell_*"
  - sequence: [build_artifact, test_artifact, publish_artifact]
  - taint_never: { from: read_secrets, to: "send_*" }
  - result_contains: { tool: deploy, text: "success" }
  - allowed: deploy        # verdict assertions (recorded, or re-evaluated
  - denied: delete_db      # when `policy:` is set — what-if semantics)
  - invariant: no_deny_bypassed
```

- **Checks**: `called` (glob + args subset + exact `times`), `never_called`,
  `sequence` (ordered subsequence), `taint_never` (no `to` after `from`),
  `result_contains`, `allowed`/`denied` (allow/confirm vs deny verdicts),
  `invariant` (`no_deny_bypassed`: a deny verdict must never produce a result).
- **Cassette-native**: failures name the exact call and args — the fixture
  *is* the repro case. Commit cassettes for your critical flows and diff
  them across releases (`tapelog diff`).
- **CI**: `tapelog test --plain agent-tests/` exits non-zero on any failure.

```bash
tapelog test agent-tests/            # every *.yaml in the directory
tapelog test agent-tests/release.yaml
tapelog test --junit junit.xml agent-tests/   # JUnit XML for CI ingestion
tapelog test --annotate agent-tests/          # GitHub ::error annotations
```

CI integration:
- **JUnit XML** (`--junit <path>`): ingested by GitLab, Jenkins, Azure
  DevOps, and GitHub Actions (via e.g. `dorny/test-reporter`). Written
  even when scenarios fail — pair with `if: always()`.
- **GitHub annotations** (`--annotate`, or automatic when
  `GITHUB_ACTIONS=true`): failures appear inline on the PR diff as
  `::error file=<scenario>,title=tapelog test::...`.
- The repo's own CI runs a **dogfood job** (`.github/workflows/ci.yml`)
  that runs `tapelog test` against its scenario fixtures and uploads the
  JUnit artifact — copy it as a starting point.

## Boundary fuzzing (`tapelog fuzz`)

The boundary hardening lab: **property-based testing for your policy**.
Every recorded call is mutated with attack-shaped transformations and
re-evaluated; a mutation that turns a `deny` into an `allow` is a policy
hole a real attacker could use.

```bash
tapelog fuzz --policy prod.yaml session.jsonl
tapelog fuzz --policy prod.yaml --operator tool_case --operator tool_homoglyph s.jsonl
tapelog fuzz --policy prod.yaml --json s.jsonl > findings.json   # CI artifacts
```

- **Operators** (all on by default; single-call + multi-call):
  `tool_case`, `tool_space` (trailing space / zero-width), `tool_homoglyph`
  (Cyrillic lookalikes), `tool_traversal` (`../` name games),
  `tool_namespace` (`server__tool` prefix confusion), `arg_traversal`
  (`../` escapes in string arguments — catches `like`-prefix rule
  evasions), `arg_type`, `arg_overflow`, `arg_unicode` (zero-width /
  homoglyph **value** evasions — escapes substring rules),
  `arg_boundary` (empty / null / extreme values escaping glob rules),
  `arg_encoding` (base64 / URL-encoded values), `swap_adjacent`
  (toxic-flow reordering), `swap_rotate` (non-adjacent reorder — sink
  moved before its source).
  Note: mutation *semantics* live in `internal/fuzz/mutate.go`; new
  operators must also be registered in the `Operators` list.
- **Oracle = the policy**: findings are comparative (baseline deny →
  mutant allow), so they're precise — mutations the policy holds are
  silent. Each finding names the operator, the mutation, the rule that
  broke, and an indicative MITRE ATLAS tactic.
- **CI**: exits non-zero on any finding; `--max-findings` caps a run;
  `--json` feeds dashboards. Run it on your critical cassettes in CI and
  a policy change that opens a hole fails the build.
- Example finding from a real cassette:

```
[tool_case] delete_file: upper-case name -> "DELETE_FILE"
    deny -> allow  (rule: no-del)  ATLAS: Defense Evasion
```

