# Getting started

This page is the shortest path from "nothing installed" to "I understand what this tool would do
for me". Every command below was run on the build this repository describes; where a platform has
not been tested, it says so instead of implying support.

Tapelog sits between an MCP client (an agent harness such as Roo, Cline, Claude Code or Cursor)
and the MCP servers it calls. It records every tool call into a hash-chained log, can deny calls
by policy **before** they execute, replays a recording as a hermetic server in CI, and runs
regression cases over recorded evidence.

Two things it does **not** do, which matter more than any feature list:

- It sees **only traffic crossing its boundary**. An agent's built-in tools never appear, so a clean
  recording is not evidence of clean behaviour.
- A verified chain proves the bytes were not altered. It does not prove who wrote them or that the
  session is complete.

## What you need

- **To build:** Go, at the version in [`go.mod`](../go.mod) (currently 1.27.x). `go version` must
  print at least that.
- **To run:** a terminal. The beta offers **Windows amd64** and **Linux amd64**; macOS and linux/arm64
  compile but were not executed, so they are not offered. See
  [supported platforms](#supported-platforms).
- **Optional:** an existing MCP client with a working server, for step 4.

## 1. Get a binary

**Download** the archive for your platform from the [releases page](https://github.com/Caseymccallum/tapelog/releases)
and extract it. Windows is a `.zip`; Linux is a `.tar.gz`. Each archive contains the
binary, this guide, `docs/SECURITY-AND-LIMITATIONS.md`, `docs/REGRESSION-LAB.md`, the licence, the
third-party notices and the fictional `examples/regression-demo/` suite. Nothing else is needed —
no Go toolchain, no account, no network.

Verify the download against the `checksums.txt` published beside it:

```bash
# Linux
sha256sum -c checksums.txt --ignore-missing
# Windows (PowerShell)
Get-FileHash .\tapelog_0.5.0-beta.1_windows_amd64.zip -Algorithm SHA256
```

You can run the binary from the extracted directory, or put it on `PATH`.

**Building from source** is supported and documented, but it is not required to try the beta:

```bash
git clone https://github.com/Caseymccallum/tapelog && cd tapelog
go build -o bin/tapelog ./cmd/tapelog          # Windows: -o bin/tapelog.exe
```

## 2. Check the binary you actually have

```console
$ ./bin/tapelog version
tapelog 0.5.0-beta.1
$ ./bin/tapelog doctor
✓ runtime — tapelog 0.5.0-beta.1, windows/amd64
! policy — none given — record/mux will run with no policy rules (boundary protections stay active; add --policy to enforce rules)
! plugins — none configured (optional)
! os-sandbox — no OS enforcement on this platform (Linux required); policy still enforces
✓ terminal — interactive approval prompts available
✓ log-location — . is writable
✓ node — on PATH (for npx-based MCP servers)
all required checks passed (preflight only; no MCP server started)
```

`tapelog version` from a release archive also prints the commit it was built from —
`tapelog 0.5.0-beta.1 (d4950d3…)` — while `doctor` prints the version only. A plain `go build`
prints no commit, which is the signal that the binary carries no build provenance.

## 3. See it work without an agent

```bash
tapelog demo                        # Windows: .\tapelog.exe demo
```

That single command runs a complete fictional session with no agent, no account and no network: it
starts the boundary, connects a scripted client to a fictional MCP server, records one permitted
call, records one **policy-denied** call and one recorded tool error, then verifies the chain it just
wrote. It prints what to look at next, with the paths filled in:

| Step | Command it prints | What you are looking at |
|---|---|---|
| 1–2 | `tapelog demo` | the boundary starting and the fictional server connecting |
| 3 | `tapelog inspect <log>` | the permitted call and its recorded result |
| 4 | `tapelog inspect <log>` | the denied call: verdict, rule id, reason, and no upstream result |
| 5 | `tapelog verify <log>` | chain intact, structure valid, and the chain head |
| 6 | `tapelog web --dir <recordings>` | the Developer Workspace in a browser |
| 7 | the workspace | the same evidence, per call, with the denial highlighted |
| 8 | `tapelog lab suite --dir examples/regression-demo --suite demo-suite.json` | the fictional Regression Lab suite |
| 9 | its output | **2 pass, 1 fail, 2 inconclusive** — a FAIL and an INCONCLUSIVE are answers, not errors, and each says why |

Measured runtime of the demo itself: **≈2 s** on this machine, in a fresh directory. To keep a
session to look at afterwards, pass `--dir my-demo`; the demo refuses to overwrite an existing
directory rather than silently replacing evidence.

A source checkout can additionally run `go run ./examples/first-session --tapelog ./bin/tapelog`,
which exercises policy what-if and replay as well. That step needs Go; the demo above does not.

The source demo drives the boundary, inspects a successful call, a denied call, the hash chain, a
regression check and a policy what-if, and finally replays the recorded response. No account, no LLM,
no network service and no third-party server are needed for either path.

> **Before you record anything real**
>
> - A recording stores tool **arguments and results**. A server that echoes a request back writes
>   that value into the log.
> - Redaction is **bounded and not infallible**: it masks known credential shapes and sensitive
>   field names, including inside a JSON document returned as result text, within stated limits
>   (256 KiB, 24 nested JSON layers, one well-formed document). It cannot find a secret that has no
>   recognisable shape in prose.
> - `--omit-result-text` stops result text being stored at all; the trade is **reduced replay
>   fidelity** for anything that depended on the result body. The shape of each result (types, ids,
>   error codes) is kept.
> - For the beta, start with **fictional or disposable data** until you have read
>   [Security and limitations](SECURITY-AND-LIMITATIONS.md) and know which mode you want.
> - Tapelog **does not send recordings anywhere**. There is no telemetry, no account and no automatic
>   network call: it writes local files. Two commands can contact the network, and only when you run
>   them: `tapelog checkpoint` submits the checkpoint's chain head to a public transparency log
>   (Rekor is the default — pass `--witness none` to keep it local), and
>   `tapelog export otel <session.jsonl> --endpoint <url>` sends OTLP traces to the endpoint you name
>   (with no endpoint it prints to stdout).


## 4. Put it in front of a server you already use

`tapelog setup` previews the change to a client's JSON config and can write a separate candidate
file; it never edits your config in place:

```bash
tapelog setup --client claude-code --config .mcp.json --server filesystem
tapelog setup --client claude-code --config .mcp.json --server filesystem --output candidate.json
```

Review the candidate, back up the original, copy the entry in, restart that server. Full detail —
including which clients are supported, what is preserved, and why you must review before applying —
is in [Your first Tapelog session](FIRST-SESSION.md) §3. **Do not use `confirm` rules for a first
harness wiring**: with no terminal they fail closed and look like a broken server.

## 5. Look at what you recorded

```bash
tapelog verify session.jsonl                 # chain only; structure is printed beside it
tapelog inspect session.jsonl --plain        # timeline, policy verdicts, denied calls
tapelog web --dir ./tapelog-recordings       # the same evidence in a browser
> The workspace has **no password by default**: it refuses non-loopback `Host` headers, so only this
> machine can reach it, but any local process can read the recordings it serves. On a shared machine
> start it with `--token <secret>` and send `Authorization: Bearer <secret>`.
```

`verify` exits 0 on an intact chain **even if the file is not a valid session** — that is deliberate.
Use `verify --require-structure` or `inspect --check` when you need the stronger answer. The four
questions (integrity, structure, completeness, eligibility) are separate on purpose; see
[M9 evidence semantics](M9-EVIDENCE-SEMANTICS.md).

## 6. Turn a recording into a regression test

```bash
# from a source checkout:
./bin/tapelog lab suite --dir examples/regression-demo --suite demo-suite.json
# from a release archive (different binary name):
./tapelog lab suite --dir examples/regression-demo --suite demo-suite.json
```

Expected: **2 pass, 1 fail, 2 inconclusive** and exit code 1. That is the demo working — one case
asserts something the evidence contradicts, and one cannot be decided from an interrupted recording.
See [The Regression Lab](REGRESSION-LAB.md) for the claim kinds, the two eligibility modes and the
10,000-event limit.

## Supported platforms

| Platform | Build | Functional acceptance |
|---|---|---|
| Windows amd64 | yes | **yes** — every command in this repository's docs was run here |
| Linux amd64 | yes | **partly** — the full test suite under `-race` runs in `golang:1.27`; the CLI was not driven end to end on a Linux desktop |
| macOS (amd64, arm64) | compiles; **no archive published for the beta** | **no** — no macOS machine was available; nothing is claimed |

## Troubleshooting the things that actually go wrong

| Symptom | Cause |
|---|---|
| A command in this repo "does not exist" | you are running the published `v0.4.0` binary; use the M15 candidate archive or rebuild from source |
| `session/start` and `tools/list` but zero calls | the client did the work natively, or never invoked an MCP tool. The boundary observes only MCP traffic |
| `bind: Only one usage of each socket address` | the web port is taken; pass `--listen 127.0.0.1:0` and it reports the port it got |
| "cannot find the path specified" from Git Bash on Windows | pass native `C:/…` paths to the binary, not `/c/…` |
| A path with spaces fails to launch | quote it, and on PowerShell invoke with `& "C:\path with spaces\tapelog.exe"` |
| A `confirm` policy rule hangs | no terminal is attached; use a deny-only ruleset or an approval queue |
| `ENOENT` with a doubled drive letter (`C:\c\Users\…`) | you passed a Git Bash path (`/c/…`) to a Windows program such as `npx` — wrap it: `npx … "$(cygpath -w "$(pwd)/dir")"` |
| The source demo fails on an archive install | that step runs Go and needs the repository; use the bundled `examples/regression-demo/` instead |

## Where to go next

| You want to | Read |
|---|---|
| Wrap a real client safely | [FIRST-SESSION.md](FIRST-SESSION.md) |
| Understand a recording's verdicts | [M9-EVIDENCE-SEMANTICS.md](M9-EVIDENCE-SEMANTICS.md) |
| Write policies | [POLICY.md](POLICY.md) |
| Use the browser workbench | [WORKBENCH.md](WORKBENCH.md) |
| Write regression cases | [REGRESSION-LAB.md](REGRESSION-LAB.md) |
| Group activity by conversation | [CONVERSATION-ATTRIBUTION.md](CONVERSATION-ATTRIBUTION.md) |
| Know what it does and does not protect | [SECURITY-AND-LIMITATIONS.md](SECURITY-AND-LIMITATIONS.md) |
| Build, test and contribute | [CONTRIBUTING.md](../CONTRIBUTING.md), [TESTING.md](TESTING.md) |
