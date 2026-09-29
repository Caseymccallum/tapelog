"""tapelog adapter for Python MCP clients (thin, dependency-free).

Wraps an MCP stdio server config dict so all traffic flows through
`tapelog record` — session logging + policy enforcement without
touching your agent code. Works with any client that spawns servers
via ``{"command": ..., "args": [...], "env": {...}}`` params (e.g. the
official ``mcp`` python-sdk's stdio_client parameters):

    from tapelog import with_tapelog

    servers = {
        "filesystem": with_tapelog(
            {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]},
            policy="policy.yaml",
            deny_on_drift=True,
        ),
    }
"""

from __future__ import annotations

from typing import Any, Dict, Optional


def with_tapelog(
    server: Dict[str, Any],
    *,
    policy: Optional[str] = None,
    log: Optional[str] = None,
    task: Optional[str] = None,
    harness: Optional[str] = None,
    auto_confirm: bool = False,
    deny_on_drift: bool = False,
    binary: str = "tapelog",
) -> Dict[str, Any]:
    """Return a server config that routes through ``tapelog record``."""
    args = ["record"]

    if policy:
        args += ["--policy", policy]
    if log:
        args += ["--log", log]
    if task:
        args += ["--task", task]
    if harness:
        args += ["--harness", harness]
    if auto_confirm:
        args.append("--auto-confirm")
    if deny_on_drift:
        args.append("--deny-on-drift")

    args += ["--", server["command"], *server.get("args", [])]

    wrapped: Dict[str, Any] = {"command": binary, "args": args}
    if "env" in server:
        wrapped["env"] = server["env"]
    return wrapped
