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
```

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

- **Operators** (all on by default): `tool_case`, `tool_space` (trailing
  space / zero-width), `tool_homoglyph` (Cyrillic lookalikes),
  `tool_traversal` (`../` name games), `arg_traversal` (`../` escapes in
  string arguments — catches `like`-prefix rule evasions),
  `arg_type`, `arg_overflow`, `swap_adjacent` (toxic-flow reordering).
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

