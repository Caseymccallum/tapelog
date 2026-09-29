# Policy Language Reference (v0/v1)

Tapelog policies are **YAML**: ordered rules with explainable outcomes. The
first rule that is *in scope*, whose tool patterns match, and whose `where`
conditions pass — wins. Decisions always carry a rule id + reason into the
session log.

## File shape

```yaml
version: 1
default: deny            # omitted => deny (fail-closed). Use `allow` for observe-only.
rules:
  - id: allow-reads      # optional; auto-assigned rule-N
    tool: ["read*", "list*"]   # glob(s): * and ? ; string or list
    action: allow        # allow | confirm | deny
    reason: "read-only tools are safe"

  - id: temp-deploy
    tool: "deploy*"
    action: allow
    reason: "temporary deployment grant"
    expires: "2026-12-31T23:59:59Z"   # RFC 3339; expired rules are skipped
    tasks: ["release"]               # rule applies only to this task label

  - id: tmp-writes
    tool: "write*"
    action: allow
    reason: "writes restricted to /tmp"
    where: 'context.args.path like "/tmp/*"'   # Cedar expression(s)
```

## Rule semantics (evaluation order)

A rule applies when **all** of the following hold — otherwise it is skipped:
1. **Task scope**: `tasks` omitted, or the request's `--task` label is listed
2. **Not expired**: `expires` omitted, or still in the future
3. **Tool match**: any `tool` glob matches the tool name
4. **Conditions**: every `where` expression evaluates true

If no rule applies, `default` decides (fail-closed `deny` unless set).

### `confirm` — the human in the loop

A `confirm` verdict pauses the call and asks the operator on the
controlling terminal (git-style prompt): **allow once**, **allow for the
session** (per-tool), or **deny**. Without a terminal and without
`--auto-confirm`, the call is denied (fail-closed) and the reason says so.
Every treatment is recorded in the session log.

## Flow rules — toxic-flow guards

Cross-tool data-flow rules: data produced by `from` tools (sources) must
not reach `to` tools (sinks). They answer what per-call rules cannot see —
"`read_database` and `send_slack_message` are *each* allowed, but together
they exfiltrate your customer list."

```yaml
flows:
  - id: no-exfil
    from: ["read_file", "query_db"]   # source tools (glob)
    to: ["send_*", "post_*"]          # sink tools (glob)
    action: deny                       # deny | confirm
    reason: "file/db data must not be sent anywhere"
```

Semantics (session-scoped taint — deliberately conservative):
- A permitted tool call **taints the session** with its tool name (at call
  time; call order in the proxy is serialized, so this is deterministic).
- A later call whose tool matches a flow's `to` is restricted when any
  prior tool matches that flow's `from`. The deny reason names the sources.
- `flow` rules take precedence over regular rules when they apply.
- Flow `confirm` verdicts go through the same human prompt.
- Over-conservative by design: the model's actual data flow is opaque to
  the boundary, so any permitted source call is presumed to contribute
  data. Value-level tracking through the model (CaMeL-style) is explicitly
  out of scope (docs/THREAT_MODEL.md).

`tapelog policy whatif` is sequence-aware: it walks the recorded session
in order and builds taint from calls the candidate policy would permit.

## `where` — real Cedar

Conditions are **Cedar expressions** (CNCF sandbox project) evaluated by
`cedar-go` — we do not invent our own expression language (ADR 0002).

Available names:

| Name | Type | Meaning |
|---|---|---|
| `context.tool` | string | tool name |
| `context.args` | record | (redacted) tool arguments |

Examples:

```yaml
where: 'context.args.path like "/tmp/*"'
where: 'context.args.domain like "*.internal"'
where: 'context.tool == "fetch_url" && context.args.method == "GET"'
where:   # lists are AND-ed
  - 'context.args.path like "/tmp/*"'
  - '!context.args.path like "*.env"'
```

Notes:
- JSON objects → Cedar records, arrays → sets (order not preserved), numbers → long/decimal. `null` values are skipped (Cedar has no null).
- Evaluation errors **fail closed** (the rule does not apply).
- Validated at load: a bad expression refuses to load the policy.

## Scoped grants

- `expires` — time-boxed authority (a rule with `expires` in the past is simply skipped; audit trail keeps the historical verdicts).
- `tasks` — per-task delegation: pass `tapelog record --task release ...` and only `tasks: [release]` rules apply to un-scoped requests.
- Together these mitigate *residual authority replay*: authority is granted narrowly and dies on schedule.

## Tool-name globs

`*` = any run, `?` = exactly one character. Anchored to the whole name
(`read*` matches `read_file`, not `pread`).

**Mux mode:** tool names are namespaced `<server>__<tool>` (e.g.
`fs__read_file`), so rules must match the namespaced form:
`*__read*`, `git__*`, etc. Server names never contain `__`.

## Commands

```bash
tapelog policy test   --policy p.yaml --calls samples.jsonl   # dry-run verdicts
tapelog policy whatif --policy p.yaml session.jsonl           # re-evaluate a recording (CI: exits non-zero on verdict changes)
tapelog policy compile --policy p.yaml                        # export portable Cedar text
```

`what-if` re-runs recorded calls against a candidate policy and diffs the
verdicts against what was recorded at run time — **policy regression tests
for agents**. Re-evaluation runs on the recorded (redacted) arguments.

## Cedar export & approximations

`policy compile` renders the policy as Cedar `permit`/`forbid` text for
interop with Cedar-aware infrastructure. Known approximations of the
export (the YAML engine is authoritative):
- ordered first-match-wins is flattened to Cedar effects (permit/forbid)
- `?` globs are widened to `*` (Cedar `like` has no single-char wildcard)
- `confirm` exports as `permit` with a comment
- `expires`/`tasks` are emitted as comments (evaluation-time concerns)

## Deny-on-drift (record time)

`tapelog record --deny-on-drift` denies tool calls whose descriptor changed
since first listing (possible tool poisoning / rug pull). Descriptor pins are
hashes of the *redacted canonical* descriptor; tool calls wait for in-flight
`tools/list` responses so enforcement cannot be raced.
