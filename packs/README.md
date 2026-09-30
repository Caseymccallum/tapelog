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

Every pack in this directory is loaded and compiled (including its Cedar
`where` conditions) by `packs/packs_test.go` on every commit — a pack
that stops parsing or grows a broken condition fails CI. Rule hygiene
you can trust, enforced by the same tooling we sell.

Contributions welcome: a pack for a popular server is the ideal first PR.
