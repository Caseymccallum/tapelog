# Put tapelog in front of your MCP client

tapelog is transparent to your agent: it speaks MCP on both sides. Your
client launches `tapelog record` *instead of* the real server, and
tapelog launches the real server behind itself:

```
your MCP client (Roo Code, Claude Code, VS Code, …)
      │  MCP (stdio)
      ▼
tapelog record        ← policy verdicts + hash-chained session log
      │  MCP (stdio)
      ▼
your real MCP server (filesystem, github, postgres, …)
```

Everything you change is **one config entry**: wrap the server's
`command`/`args` with `tapelog record … -- <original command>`.

## ⚠ Read this first: `confirm` verdicts under a client

When a client (not you) drives tapelog over stdio, there is no terminal
to prompt on — the MCP stream *is* stdin. Pick one:

| Mode | Flags | Use when |
|---|---|---|
| **Approval queue** (recommended) | `--approval-listen 127.0.0.1:8923` | you want human review; approve via `tapelog queue` or the web UI on that port. Parked calls **fail closed** after `--approval-timeout` (default 5m) |
| **Auto-confirm** | `--auto-confirm` | testing/demo; confirm verdicts are treated as allow and **recorded as such** |
| **Deny-only policy** | policy uses `action: deny` rules only | you never want a pause in the flow |

Never rely on the interactive prompt in client-driven setups.

---

## Roo Code (VS Code) — full recipe

Roo Code loads MCP servers from two files (same JSON shape):

- **Project:** `.roo/mcp.json` in your workspace root (committed to the
  repo; wins on name conflicts)
- **Global:** `mcp_settings.json` (open it from the Roo pane → ⚙️ →
  **MCP Servers** → **Edit Global MCP**)

### 1. Wrap your server

`.roo/mcp.json` — filesystem server behind tapelog, approval queue on
port 8923:

```json
{
  "mcpServers": {
    "filesystem-logged": {
      "command": "C:\\Users\\you\\tapelog\\bin\\tapelog.exe",
      "args": [
        "record",
        "--policy", "C:\\you\\project\\packs\\starter.yaml",
        "--log",    "C:\\you\\project\\sessions\\roo.jsonl",
        "--approval-listen", "127.0.0.1:8923",
        "--harness", "roo-code",
        "--",
        "cmd", "/c", "npx", "-y", "@modelcontextprotocol/server-filesystem", "C:\\you\\project"
      ],
      "cwd": "C:\\you\\project"
    }
  }
}
```

Notes that matter on Windows:

- Use the **absolute path** to `tapelog.exe` (spawn doesn't run PATH
  lookup the way your shell does). Spaces in the path are fine in JSON.
- `npx` needs the `cmd /c` wrapper on Windows (Roo's own docs do the
  same); everything after `--` is the original server command **verbatim**.
- Drop `--policy` for a first observe-only run (everything is recorded,
  nothing is enforced) — see step 3.
- Optional `"alwaysAllow": ["tool_a"]` in the entry is *Roo's* auto-
  approve list; it does not affect tapelog policy.

### 2. Start it

Roo pane → ⚙️ → **MCP Servers** → find `filesystem-logged` → **Start**
(or **Restart** after edits). The tool list should populate as normal —
from the agent's view nothing changed.

### 3. Prove the pipe, then enforce

First run **observe-only** (no `--policy`): ask Roo to read a file,
then check the evidence:

```powershell
.\bin\tapelog.exe verify sessions\roo.jsonl
.\bin\tapelog.exe inspect sessions\roo.jsonl --plain
.\bin\tapelog.exe web --dir .\sessions
```

Then add `--policy packs\starter.yaml` and repeat: allowed calls flow
through, denied ones return `-32010 tool call denied by policy` to the
agent with the rule id and reason, and `confirm` calls land in the
approval queue (`http://127.0.0.1:8923`):

```powershell
.\bin\tapelog.exe queue list
.\bin\tapelog.exe queue allow 1 --note "reviewed"
```

---

## VS Code (GitHub Copilot Chat)

`.vscode/mcp.json` (current format uses `servers`; older files use
`mcpServers` — both accepted):

```json
{
  "servers": {
    "filesystem-logged": {
      "type": "stdio",
      "command": "C:\\Users\\you\\tapelog\\bin\\tapelog.exe",
      "args": [
        "record",
        "--log", "C:\\you\\project\\sessions\\vscode.jsonl",
        "--auto-confirm",
        "--",
        "cmd", "/c", "npx", "-y", "@modelcontextprotocol/server-filesystem", "C:\\you\\project"
      ]
    }
  }
}
```

Start it via **MCP: List Servers** from the Command Palette.

---

## Claude Code / Claude Desktop

Claude Code — project scope `.mcp.json` (or `claude mcp add`):

```json
{
  "mcpServers": {
    "filesystem-logged": {
      "type": "stdio",
      "command": "C:\\Users\\you\\tapelog\\bin\\tapelog.exe",
      "args": ["record", "--log", "sessions/claude.jsonl", "--auto-confirm",
               "--", "npx", "-y", "@modelcontextprotocol/server-filesystem", "."]
    }
  }
}
```

Claude Desktop — same shape in `%APPDATA%\Claude\claude_desktop_config.json`
(global `mcpServers`). On macOS/Linux drop the `cmd /c` wrapper and use
the plain server command.

---

## Any other stdio client (Cursor, custom harnesses)

The recipe is always the same:

1. Take the client's existing entry for your server.
2. Set `command` = tapelog, `args` = `record … -- ` + the original
   command/args appended verbatim.
3. Pick your confirm mode (queue / auto-confirm / deny-only).
4. Restart the server in the client.

## Verify it's working

- [ ] The client shows the server's tools (unchanged from before)
- [ ] After a few prompts, `tapelog verify <log>` reports `chain intact`
- [ ] `tapelog inspect <log>` shows every `tools/call` with its verdict
- [ ] A deny (if any) appears in the client as a tool error with rule id
- [ ] `tapelog web --dir <dir>` shows the session (⚠ badge = broken chain)

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Server won't start in client | relative path to tapelog | use absolute path in `command` |
| Server starts, zero tools | inner command broken (`npx` w/o `cmd /c` on Windows) | run the inner command alone in a terminal first |
| Agent hangs on a call | `confirm` policy + no approval path | add `--approval-listen` or `--auto-confirm` |
| Calls denied immediately | policy pack too strict for your tools | first run observe-only; then `tapelog policy whatif` |
| Log grows but verify fails | something rewrote the log | check the ⚠ banner in `tapelog web`; that's tampering or a tool editing files |


