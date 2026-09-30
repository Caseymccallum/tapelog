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

## Value-level taint (`flows` with `mode: value`) — experimental

Session-mode flow rules block **every** sink call after a source runs.
Value mode (CaMeL-inspired, ADR 0005) is precise: it fires only when the
sink call's arguments **actually carry values from a source tool's
result** (contamination matching):

```yaml
flows:
  - id: no-secret-exfil
    from: ["read_secrets"]
    to: ["send_*"]
    mode: value                    # session (default) | value
    action: deny
    reason: "secret values must not reach a sink"
```

A denied call's reason names the evidence:
`... [contaminated by: read_secrets]`. Clean sinks pass (e2e-proven).

**Honest limits**: contamination is detected by substring matching of
recorded string values (bounded store: 512 values/source, 8 KiB/value).
Base64, paraphrasing, splitting — any transformation — evades matching.
Use `mode: value` for *precision*, `mode: session` for *must-never* flows.
`tapelog fuzz` evaluates session semantics (documented).

## `where` — real Cedar

> **Combinator semantics:** multiple `where` entries are **ANDed** — *every*
> expression must hold. For alternatives ("this OR that"), write one
> expression joined with `||`:
> `where: 'context.args.path like "*.env*" || context.args.path like "*.key*"'`.
> (A pack authoring bug our own dogfooding caught: three OR-intended list
> entries could never all match, silently disabling the rule.)

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
## Session limits (budgets, rates, payload caps)

```yaml
limits:
  max_calls: 200              # total tool-call attempts per session
  max_calls_per_tool:         # attempts per tool glob
    "shell_*": 5
    "write_*": 20
  max_per_minute: 30          # sliding 60s window over all tools
  max_response_bytes: 1048576 # results over this are replaced with an error
```

- Zero/unset = unlimited. Violations deny with `rule_id` `limits.max_calls`,
  `limits.max_calls_per_tool`, `limits.max_per_minute` — explainable like any
  policy verdict.
- **Attempts count**, including calls a policy rule would deny — limits are
  anti-flood protection, not billing.
- `max_response_bytes` is enforced when the result comes *back*: the harness
  receives a structured `response_too_large` error instead of the payload,
  while the (redacted) original is still recorded as evidence.

## Schema firewall (inbound)

Tool-call arguments are validated against the JSON Schema the MCP server
## Gating resources & prompts (no unmediated surface)

Every client→server **request** except `initialize`, `ping`, and
`tools/list` is mediated as a *surface call* named after its method:
`resources/read`, `prompts/get`, `resources/templates/get`, and any future
method. They flow through the same pipeline as `tools/call` (drift, schema,
limits, flows, policy, confirm, taint, recording), so:

- **Rules match them by method name**; arguments are the MCP params:

```yaml
rules:
  - id: no-secret-reads
    tool: "resources/read"
    where: 'context.args.uri like "file://secrets/*"'
    action: deny
    reason: "secrets are out of bounds for agents"

  - id: no-admin-prompts
    tool: "prompts/get"
    where: 'context.args.name like "admin*"'
    action: deny
```

- Surface calls **consume `limits` budgets**, **feed taint** (`flows:` rules
  can name `resources/read` as a source), and are **recorded** in the
## Injection scanning (tool results)

Tool and surface results are scanned for **prompt-injection markers** —
text trying to rewrite the agent's instructions from inside tool output
(`"IGNORE ALL PREVIOUS INSTRUCTIONS"`, `"reveal your system prompt"`,
`"do not tell the user"`, smuggled tool-call JSON, and similar).
Detection is heuristic (imperative + AI-context phrases, to limit false
positives), so the default mode merely logs:

```yaml
injection:
  mode: log          # off | log | confirm | deny   (default: log)
  patterns:          # optional extra Go-regexp patterns
    - '(?i)\bcustomer-data-export\b'
```

- **`log`** (default, also in observe-only sessions): the result is
  delivered and a `policy/decision` event with `rule_id: injection-scan`
  records the finding. A false positive costs one log line.
- **`confirm`**: delivery is paused for the human prompt (allow once /
  allow session / deny); the outcome is recorded either way.
- **`deny`**: the harness receives a structured `result_blocked` error
  instead of the payload; the (redacted) original is still recorded as
  evidence.
- Findings are auditable (`tapelog inspect` shows them like any
  decision), and `mode: off` disables scanning entirely.
## Remote approval queue ("quarantine queue")

`record`/`mux --approval-listen 127.0.0.1:8923` turns `confirm` verdicts
into **parked approvals**: the call is held at the boundary (MCP
request/response semantics preserved) until a human decides remotely —
or `--approval-timeout` expires and it **fails closed to deny**.

```bash
tapelog record --policy p.yaml --approval-listen 127.0.0.1:8923 -- npx -y @mcp/server
# ...elsewhere (browser or terminal):
open http://127.0.0.1:8923        # review dashboard: approvals + live session log
tapelog queue list   --url http://127.0.0.1:8923
tapelog queue allow 3 --note "reviewed the diff"
tapelog queue deny  4
```

- **Review dashboard** (`http://<listen>`): parked approvals with
  note-carrying allow/deny, plus a live tail of the session log
  (the `inspect` timeline in a browser). Dependency-free, embedded
  assets. Security posture: untrusted data rendered via `textContent`
  only (tool output is attacker-controlled — no HTML, strict
  `default-src 'self'` CSP), loopback Host pinning (DNS-rebinding
  defense), same-origin-only POSTs (CSRF defense), `no-store` responses.
- **API** (JSON): `GET /api/pending`, `POST /api/decide {"id","verdict":
  allow|allow_session|deny,"note"}`, `GET /api/log?after=<seq>`,
  `GET /healthz`. Add `--approval-token` to require
  `Authorization: Bearer <token>` (and to allow non-loopback binds).
- **Trust model**: localhost-first — the listener is plain HTTP and
  anyone who can reach it can decide. Keep it on 127.0.0.1 (or set a
  token and put it behind your own TLS). See docs/THREAT_MODEL.md.
- **Audit**: every decision lands in the hash-chained log with its
  provenance and note — `"deletes need a human (approved via the
  approval queue; e2e approved)"`. `allow_session` exempts the tool for
  the rest of the session (like the terminal prompt's `[s]`).
- Precedence: `--auto-confirm` > `--approval-listen` > terminal prompt >
  fail closed. Works for `injection: mode: confirm` reviews too.



Heuristics are not proof — treat hits as signals to review, not verdicts.


  session log as `tool_call`/`tool_result` events with `tool` set to the
  method name, so `verify`, `inspect`, `replay`, and `what-if` all work.
- In `tapelog mux`, catalogs are **aggregated**: `resources/list`,
  `prompts/list`, and `resources/templates/list` merge every upstream's
  entries (annotated `_tapelog_server`); prompt names are namespaced
  `<server>__<name>` and unwrapped on `prompts/get`. Resource URIs are
  left intact but the mux **remembers each URI's owner** from the catalog
  and routes `resources/read` precisely; only unknown URIs fall back to
  first-success in deterministic server-name order.


itself advertised in the tool's `inputSchema` — automatically, for every
`record` and `mux` session. "Allowing a tool name isn't enough; risk hides
in the payload."

- Invalid arguments (wrong types, missing required fields, properties the
  schema forbids) are **denied before forwarding** with
  `rule_id: schema-firewall`.
- **Fail-open where there is nothing to validate against**: tools with no
  `inputSchema`, or a schema that will not compile, pass through (the
  error is reported at pin time). Servers cannot brick themselves with
  broken schemas — but they also cannot smuggle bad arguments past a
  schema they declared.
- Validation happens after drift checks and before policy rules, so a
  schema violation never consumes a policy rule's semantics (or a confirm
  prompt's attention).

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
