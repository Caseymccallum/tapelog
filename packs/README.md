# tapelog policy packs

Batteries-included policies for the MCP servers people actually run.
Philosophy per pack: **deny the dangerous, confirm the risky, allow the
routine** — safe defaults you can read in one screen and tune.

| Pack | Targets | Highlights |
|---|---|---|
| `starter.yaml` | anything (copy first) | default-deny baseline, taint anti-exfil, limits |
| `filesystem.yaml` | `server-filesystem` & compatible | secret/supply-chain hard denies, writes confirm |
| `git.yaml` | git MCP servers | history-destruction + hook tamper denies, push confirm |
| `fetch.yaml` | fetch/HTTP servers | SSRF deny (localhost/RFC1918/metadata), TLS-for-posts |
| `postgres.yaml` | SQL servers | schema-destruction denies, write confirm |
| `slack.yaml` | messaging servers | send confirm, **cross-tool exfil taint rule** |
| `shell.yaml` | shell/exec servers | catastrophe + privilege-escalation denies, injection `confirm` |

## Use

```bash
tapelog record --policy packs/filesystem.yaml --log session.jsonl -- <your mcp server>
tapelog mux --config mux.yaml --policy packs/starter.yaml --log session.jsonl
```

Packs are ordinary policy files: **copy one and edit it** for your estate
(paths, table names, channel names). Validate your edits like code:

```bash
tapelog policy test --policy my-policy.yaml --calls samples.jsonl
tapelog test agent-tests/                 # trajectory regression
tapelog fuzz --policy my-policy.yaml session.jsonl   # hole-hunting
```

## Why packs ship with tests

Two layers, because they catch different failures:

1. **Compilation** — `packs/packs_test.go` loads every pack, compiles its Cedar
   `where` conditions and requires at least one hard deny.
2. **Enforcement** — `packs/enforcement_*_test.go` evaluates fictional calls
   against every security-sensitive rule and asserts the documented verdict,
   the rule that matched, and that a legitimate neighbouring call is still
   handled as documented.

Compilation alone is not enough, and M5.1 exists because we learned that the
expensive way. Cedar's `like` has exactly one wildcard, `*`. `%`, `?` and
`[a-z]` are **literal characters**, so `like "%rm -rf /%"` compiles perfectly,
passes a compile-only test, and can never match. Three packs shipped that way
and every deny in them was inert. Two more guards now make that class of
mistake loud:

- `TestNoPackUsesPercentInACondition` fails on any `%` inside a condition.
- `TestEveryDenyRuleCanFire` fails if a deny rule in any pack is never reached
  by the enforcement tables — a rule that cannot match, or is untested.

`internal/policy/cedar_semantics_test.go` pins the `like` behaviour these tests
assume, so a change in the engine cannot silently disarm the packs.

Validate your own edits the same way:

```bash
tapelog policy test --policy my-policy.yaml --calls samples.jsonl
tapelog test agent-tests/                 # trajectory regression
tapelog fuzz --policy my-policy.yaml session.jsonl   # hole-hunting
```

Contributions welcome: a pack for a popular server is the ideal first PR — with
an enforcement test for each deny.

