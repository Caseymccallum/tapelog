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
