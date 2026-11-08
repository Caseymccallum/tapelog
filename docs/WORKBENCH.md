# Agent Session Workbench

Build the M2 branch, then point the workbench at a directory of recordings:

```powershell
go build -o bin/tapelog.exe ./cmd/tapelog
.\bin\tapelog.exe web --dir 'C:\My Projects\tapelog-recordings'
```

Open the printed loopback URL (default `http://127.0.0.1:8923`). No account,
external assets, uploads or frontend build step is needed. Use your generated
setup config's recordings directory; DX1.1 still creates a fresh log per start.

For a fictional product tour without an MCP client:

```powershell
go run ./examples/workbench-demo --out workbench-demo-sessions
.\bin\tapelog.exe web --dir workbench-demo-sessions
```

The generator refuses an existing output directory. These fictional recordings
include successes, a tool error, a denial, a confirmation, discovery-only and
interrupted sessions, a deliberately altered chain, and malicious text rendered
as plain data. The generator never starts a live upstream server.

## Find a session

Search by name, filename, session ID, harness or recorded server namespace.
Filter for deny decisions, tool errors, missing ends or integrity warnings.
Sort by recorded date, activity or issue count. The library displays readable
task names when present, falling back to the harness or filename; it never
renames logs or stores labels inside them. Dates are displayed in your browser's
local timezone. Duration is elapsed recorded time, not CPU/tool-execution time.

Refresh explicitly to discover new recordings or update an open session. The
local-connection indicator refers to the workbench API, not a current MCP server.
Historical recordings cannot establish present client connectivity.

## Investigate a call

Open a session and select a call in Activity. Filter by tool/rule/explanation,
server or outcome. The inspector shows recorded decisions, rule and reason,
call-to-result elapsed time when timestamps permit it, and related raw events.
Tools with a recorded `server__tool` namespace identify that source; unrecorded
server identity stays unknown. A recorded `parent_seq` is preferred; older logs
use only a unique preceding request ID. Missing and ambiguous links are not guessed.

Overlapping calls stay separate and are matched to their recorded results, not
the nearest timestamp. File order remains authoritative in All events. The
preceding-context panel is context, not a causal claim. Raw JSON previews are
collapsed by default, limited to 32 KiB each and inserted as text. They may still
contain secrets: redaction is best effort. Blob references are displayed without
fetching external files.

A denial does not independently prove that an action never reached the server.
A recorded result is not proof of a downstream side effect. Tool errors, missing
results, missing session ends and integrity failures are distinct states. An
intact unanchored chain establishes internal consistency, not authorship or
complete monitoring. Built-in agent operations bypassing MCP are unobserved.

## Compare two recordings

In Compare sessions, select another completed recording and run the comparison.
The existing CLI diff engine compares answered calls using tool/argument hashes,
recorded results and call order. It ignores timestamps and request IDs. Added or
missing calls also change the compared order; changed counts/outcomes appear
under changed keys. Denied/unanswered calls and policy verdicts are not aligned
by this engine; whole-session decision/error totals are shown separately.

The UI refuses corrupt/incomplete/empty logs, ambiguous request IDs, blob-backed
payloads, sessions over 10,000 events and pairs with no shared answered tool
names. Use the CLI for workflows outside these explicit UI limits.

## Turn evidence into a regression test

1. Select a call in Activity, then choose Make a regression test.
2. Deliberately choose the assertion you want to keep: a recorded denial, an
   observed attempt, a recorded result, or no recorded results for that tool.
3. Set the fixture path relative to where you will save the YAML. Saving beside
   the recording allows the filename default.
4. Preview the YAML; Run assertion locally uses the existing scenario engine
   against the same verified snapshot. It performs no tool calls.
5. Save test as opens a browser save picker where supported. A non-empty selected
   file is refused. Other browsers use a uniquely named YAML download controlled
   by browser save settings. The server has no file-writing endpoint.
6. Run the saved file later with `tapelog test <test.yaml>`.

Only a tool selector and assertion are generated. No arguments, results or
cassette contents are copied. Assertions apply across the cassette, not one
selected request ID. `never_called` in the existing engine means no recorded
result: it does **not** prove that the tool never executed. A changed recording
invalidates the UI preview. Saved tests reference the fixture rather than
pinning its hash; verify evidence separately when integrity matters.

## Replay and live approvals

Replay & CLI shows safely quoted commands for inspection, verification and
`tapelog replay <recording> --strict`. Replay serves recorded responses as an
MCP server and waits for client input; denied/unanswered calls are misses.
There is no workbench action that executes a recording against a live server.

The same UI still hosts a live approval queue when launched through the existing
`--approval-listen` recording path. Allow once, allow for session and deny remain
explicit choices; no approvals are automatic. Directory mode has no approval queue.
The existing Host pinning, same-origin POST checks, CSP and bearer-token rules
apply to every new endpoint. Token-protected static pages still require the
existing authorization-header setup; this milestone adds no token-in-URL bypass.

## Bounded local reads

- Library: 200 most recently modified files from up to 10,000 directory entries;
  summaries sorted by recorded time. At most 32 MiB is scanned per refresh.
  Deferred summaries remain visible and can be opened individually.
- Recording: 16 MiB and 50,000 readable events. Oversized files are refused with
  CLI guidance; event-limited files show a partial view and an explicit warning.
- Browser pages: 50 rows (API maximum 100); raw previews 32 KiB; related-event
  details 20 at once. All events provides the rest. The UI never accumulates an
  unbounded tail; refresh is explicit.
- Two simultaneous snapshot requests per handler; excess requests return a
  retryable response. No snapshot cache can hide a changed recording.
- Directory reads use plain `.jsonl` names and `os.Root` confinement; symbolic
  links and non-regular files are refused. Blob paths are never followed by the
  workbench's test/comparison loader. CLI blob behavior is unchanged.

Run the ordinary tests with `go test ./...` and the dependency-free rendering
tests with `node --test internal/web/static/app.test.cjs`.
