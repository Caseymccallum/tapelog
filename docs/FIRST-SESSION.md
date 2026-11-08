# Your first Tapelog session

Tapelog records and can enforce policy on calls between an MCP client and an MCP server. MCP is the protocol an agent uses to discover and call external tools. Built-in agent tools do not cross this connection and remain unobserved.

## 1. Install or build

Download the archive matching your OS and CPU from [GitHub Releases](https://github.com/Caseymccallum/tapelog/releases); extract it locally. On Windows, invoke a path containing spaces with PowerShell's `&` operator. On macOS/Linux, mark the extracted binary executable with `chmod +x ./tapelog`. Follow [SECURITY.md](../SECURITY.md) to verify release provenance. The DX commands in this branch are **not in the published v0.4.0 binary**; build this branch to try them:

```powershell
go build -o bin/tapelog.exe ./cmd/tapelog
& ./bin/tapelog.exe version
```

On macOS/Linux use `go build -o bin/tapelog ./cmd/tapelog` and `./bin/tapelog`. Source builds require the Go version in `go.mod`. Put your chosen binary on PATH or use its absolute path in every command below.

## 2. Try it without an agent or external server

From this source checkout, with Go installed:

```powershell
go run ./examples/first-session --tapelog ./bin/tapelog.exe
```

On macOS/Linux replace the final path with `./bin/tapelog`. This creates a fresh temporary directory and prints its location. It drives a scripted MCP server through the **actual recording boundary**, inspects a successful call and a denied call, verifies the chain, runs a regression test and policy what-if, then replays the recorded response. No account, LLM, third-party server download, auto-confirm or external network service is needed at runtime. First source builds may download Go dependencies.

The demonstration tools return fictional fixture data; they do not read/delete your files or send network requests. The candidate config is an example for the scripted driver and references its temporary executable; do not install it in a real client. Run the demo again for a new directory, or pass `--out <new directory>` (an existing directory is refused). Keep the printed transcript for review. This is not a measured five-minute install or a live-client test.

## 3. Wrap one existing, working MCP server

Use a server that already works in your client. Choose its exact existing name and configuration file:

| Client selector | Config to select | JSON server object | Activate after local review |
|---|---|---|---|
| `roo` | Project `.roo/mcp.json`, or file opened by Roo's MCP settings | `mcpServers` | Restart that server in Roo |
| `cline` | Existing file opened by Cline's MCP settings | `mcpServers` | Restart that server in Cline |
| `vscode` | Project `.vscode/mcp.json` | `servers` | MCP: List Servers, then restart |
| `claude-code` | Project `.mcp.json` | `mcpServers` | Restart/reconnect the project MCP server |
| `claude-desktop` | Existing `claude_desktop_config.json` opened from its settings | `mcpServers` | Restart Desktop |

These are configuration-generation targets, **not a claim that all current client versions were tested**. The tool accepts strict JSON only: no JSONC/comments, duplicate keys, remote URL servers or ambiguous transport types. VS Code's legacy `mcpServers` shape is not generated; use the current `servers` shape. Remote/multi-server transport remains in `tapelog mux`. The helper does not guess global configuration locations.

```powershell
tapelog setup --client roo --config .roo/mcp.json --server filesystem
tapelog setup --client roo --config .roo/mcp.json --server filesystem --output candidate.json
```

The first command previews changes without writing anything. The second saves a **new file**. By default every client start creates a separate, collision-resistant recording in `tapelog-recordings` beside the config. Use `--recordings-dir <directory>` to choose another location. Only the selected entry's command/args change; its environment, working directory, approval settings, other servers and unrelated client settings are retained. Server arguments and credentials are omitted from terminal output. The candidate contains the original credentials: keep it private, outside source control. Unix mode is 0600; Windows access follows the directory ACL, so choose a private directory.

Review `candidate.json` locally, back up your original config, then copy the reviewed contents into the original file and restart the selected server. There is no automatic apply operation. Restore the backup to undo. Do not activate the demo's temporary config. The recordings directory is created when the server starts; its parent must already exist. Each start gets its own file, so earlier sessions remain available for inspection, verification and replay. The legacy setup `--log <file>` option remains available for a single fixed-path recording and overwrites that path on subsequent starts; do not use it for a persistent client configuration.

With an existing policy, add `--policy <your-policy.yaml>` to **both** setup commands. Setup validates it and retains that policy path without editing it. Without `--policy`, the new recording is default-allow; existing schema checks, descriptor tracking, audit behavior and redaction remain unchanged; no deny-on-drift option or policy limits are enabled automatically. This does not provide a restrictive policy automatically. Confirm requirements remain fail closed: for policies requiring review, add the existing `--approval-listen 127.0.0.1:8923` flags **before** the `--` in the generated `record` arguments, then review via `tapelog queue` or its loopback dashboard. Never add auto-confirm just to make setup work. See [POLICY.md](POLICY.md).

## 4. Make a call and inspect the evidence

Ask your client explicitly: “Using the MCP server filesystem, call its read tool on README.md.” Choose an appropriate safe tool and allowed path for your actual server.

```powershell
tapelog inspect first.jsonl --summary
tapelog inspect first.jsonl --plain
tapelog verify first.jsonl
tapelog web --dir .
```

The dashboard defaults to `http://127.0.0.1:8923`. Use a directory containing only logs you want to browse. Policy decision counts are not successful-call counts. Failed results are separate. Zero calls means zero **observed mediated calls**, not zero agent activity. A discovery-only session may mean the client listed tools but used built-in tools instead. Session start does not record the upstream server identity, so do not infer server attribution from the overview.

Inspection is not integrity verification. `verify` checks the recorded hash chain and referenced blobs; keep the printed chain head separately and use `verify --expect <head>` to detect a later consistent rewrite/truncation. An intact chain alone does not prove completeness, authorship or that a session ended normally.

## 5. Replay and reproduce

`tapelog replay first.jsonl --strict` starts an MCP server and **waits for client input**; it is not a playback animation. In a reviewed copy of your client config, set the server command to the absolute Tapelog executable and its args to `["replay", "<absolute log path>", "--strict"]`. Reconnect and request the same recorded tool arguments. The original upstream is not started. Replay serves recorded results, once per recorded interaction. Unrecorded or unanswered calls (including policy-denied calls without results) return `-32011`; strict mode exits nonzero after a miss. It does not re-enforce or synthesize the original denial response.

For a reproducible behavioral test, use the demo's generated `scenario.yaml` as a small example and see [TESTING.md](TESTING.md). `policy whatif --policy <candidate> <log>` compares decisions on recorded, redacted arguments without changing a live policy. Behavioral tests and what-if do not replace `verify`.

## When setup fails

```powershell
tapelog doctor --server node --log first.jsonl --listen 127.0.0.1:8923
```

This checks executable discovery, the log parent, and temporary loopback port availability. It does not start a server, validate its protocol or prove the MCP client's environment matches your terminal. A missing executable needs an absolute path or client PATH correction. A busy port needs another port or stopping its known owner. Invalid JSON/config/policy leaves the original untouched. A startup crash requires inspecting upstream stderr; “Connection closed” alone does not identify the cause.

On Windows, the tested path uses direct executable spawning with separate argument strings, including paths containing spaces. Setup does not add shell quoting, convert to 8.3 names or rewrite the existing upstream command. `npx.cmd`/batch files require a client-tested CMD wrapper; prefer an absolute `node.exe` plus an already installed server's JS entry point when available. Roo/Cline launcher versions may mishandle quoted paths: the repository's historical short-name workaround is not a universal fix, and 8.3 names may be disabled. A space-free Tapelog install can be tried if that specific launcher is proven to fail. Live Roo/Cline, Claude and VS Code launcher testing remains a release gate. Native macOS/Linux execution has not been verified in this Windows session.
