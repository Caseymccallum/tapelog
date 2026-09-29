# cassette adapters (thin)

Language adapters that route MCP server configs through `cassette record`.
Both are single dependency-free files — copy them into your project or
vendor them; they only build config objects.

| Adapter | File | Entry point |
|---|---|---|
| TypeScript | `typescript/cassette.ts` | `withCassette(serverConfig, opts)` |
| Python | `python/cassette.py` | `with_cassette(server_config, **opts)` |

Example (TypeScript):

```ts
import { withCassette } from "./cassette";

const servers = {
  filesystem: withCassette(
    { command: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem", "."] },
    { policy: "policy.yaml", denyOnDrift: true, harness: "my-agent" },
  ),
};
```

The wrapped config points at `cassette` instead of the real server command;
cassette spawns the real server itself and mediates everything. Your agent
code is unchanged. The full CLI surface is documented in the top-level
README and docs/POLICY.md.

> Interface stability: the flag set these adapters pass is part of the
> v1 CLI contract; additions will be backward compatible.
