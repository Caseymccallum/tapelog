# tapelog adapters (thin)

Language adapters that route MCP server configs through `tapelog record`.
Both are single dependency-free files — copy them into your project or
vendor them; they only build config objects.

| Adapter | File | Entry point |
|---|---|---|
| TypeScript | `typescript/tapelog.ts` | `withTapelog(serverConfig, opts)` |
| Python | `python/tapelog.py` | `with_tapelog(server_config, **opts)` |

Example (TypeScript):

```ts
import { withTapelog } from "./tapelog";

const servers = {
  filesystem: withTapelog(
    { command: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem", "."] },
    { policy: "policy.yaml", denyOnDrift: true, harness: "my-agent" },
  ),
};
```

The wrapped config points at `tapelog` instead of the real server command;
tapelog spawns the real server itself and mediates everything. Your agent
code is unchanged. The full CLI surface is documented in the top-level
README and docs/POLICY.md.

> Interface stability: the flag set these adapters pass is part of the
> v1 CLI contract; additions will be backward compatible.
